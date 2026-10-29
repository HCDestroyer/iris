package identidad

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
	identidadbd "github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad/bd"
)

// Autologin desde Argos («Ver en Iris», contrato argosisp.md §1.3 → contrato Argos ↔ Horus §1.5):
//
//  1. El servidor de Argos pide POST /api/v1/integraciones/argosisp/sso/tickets con el iris_pat_
//     que Iris le entregó en el canje del conector activo (token_entregado_id; cualquier otro
//     token, 403) y el permiso sso.emitir, con su usuario (id_externo, correo, nombre) y el
//     destino (ticket, orden o prospecto, ids de Iris). Recibe <IRIS_BASE_URL>/sso/entrar#<ticket>:
//     32 bytes aleatorios, 60 s, un solo uso, atado al conector; solo se guarda su SHA-256.
//  2. La página /sso/entrar (I3) canjea el ticket con POST /api/v1/sso/canjear { ticket }:
//     - con vínculo (conector, id_externo) → usuario de Iris: abre una sesión normal del usuario,
//       pendiente de MFA si lo tiene activo (como el login), y devuelve la ruta del destino;
//     - sin vínculo: deja la cookie iris_sso_vincular (HttpOnly, solo /api/v1/sso, 10 min) y el
//       usuario entra con su login normal (con su MFA); luego POST /api/v1/sso/vincular guarda el
//       vínculo. La sesión debe ser posterior al canje: una ya abierta en el navegador no sirve.
//     Nunca se vincula por correo ni se crean usuarios.
//  3. Cada usuario ve y quita sus vínculos; un administrador ve y borra los de otros.
//
// Sin límite por IP (como el portal: detrás del proxy del panel todos comparten la IP y el ticket
// no se puede adivinar): los canjes fallidos no se auditan (se cuentan y van al log con muestreo)
// y pasan por el mismo freno global que el portal (PortalTope por PortalVentanaTope → 429).

const (
	// sistemaSSO: el conector que emite tickets de autologin.
	sistemaSSO = "argosisp"
	// VigenciaTicketSSO: vida del ticket de autologin (un solo uso).
	VigenciaTicketSSO = 60 * time.Second
	// VigenciaVinculacionSSO: plazo para iniciar sesión y vincular tras canjear sin vínculo.
	VigenciaVinculacionSSO = 10 * time.Minute
	// CookieVinculacionSSO guarda la vinculación pendiente (solo en /api/v1/sso).
	CookieVinculacionSSO = "iris_sso_vincular"
	// RutasSSO: prefijo de las rutas del navegador del autologin.
	RutasSSO       = "/api/v1/sso"
	rutaCanjearSSO = RutasSSO + "/canjear"
)

// Emitir ---------------------------------------------------------------------------------

// UsuarioExternoSSO es el usuario de Argos que pide entrar.
type UsuarioExternoSSO struct {
	IDExterno string  `json:"id_externo" minLength:"1" maxLength:"100" doc:"Id del usuario en Argos (u-<id>)"`
	Correo    *string `json:"correo,omitempty" required:"false" nullable:"true" maxLength:"254" doc:"Solo para mostrarlo al vincular: nunca se vincula por correo"`
	Nombre    *string `json:"nombre,omitempty" required:"false" nullable:"true" maxLength:"200"`
}

// DestinoSSO es el recurso de Iris al que va el usuario.
type DestinoSSO struct {
	Tipo      string `json:"tipo" enum:"ticket,orden,prospecto"`
	IDExterno string `json:"id_externo" pattern:"^[1-9][0-9]{0,17}$" doc:"Id de Iris del ticket, la orden o el prospecto"`
}

// EntradaEmitirTicketSSO es el cuerpo que manda Argos (snake_case, como el contrato §1.5).
type EntradaEmitirTicketSSO struct {
	Usuario UsuarioExternoSSO `json:"usuario"`
	Destino DestinoSSO        `json:"destino"`
}

// TicketSSOEmitido es la URL que Argos abre en el navegador del usuario.
type TicketSSOEmitido struct {
	URL      string    `json:"url" doc:"<IRIS_BASE_URL>/sso/entrar#<ticket>, 60 s, un solo uso"`
	ExpiraEn time.Time `json:"expira_en"`
}

func conectorApagadoSSO() *nucleo.Problema {
	return nucleo.Conflicto("conector_apagado", "No hay un conector activo con Argos: el autologin está apagado")
}

func nuevoEmitirTicketSSO(cfg Config) *auditoria.Caso[EntradaEmitirTicketSSO, TicketSSOEmitido] {
	return auditoria.Nuevo(auditoria.Caso[EntradaEmitirTicketSSO, TicketSSOEmitido]{
		Accion: "sso.emitir", Permiso: nucleo.PermSSOEmitir, SinEmpresa: true,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaEmitirTicketSSO) (TicketSSOEmitido, error) {
			q := identidadbd.New(t)
			destino, err := strconv.ParseInt(in.Destino.IDExterno, 10, 64)
			if err != nil || destino <= 0 {
				return TicketSSOEmitido{}, nucleo.Validacion(nucleo.ErrorCampo{Field: "destino.id_externo", Message: "Id inválido", Rule: "pattern"})
			}
			c, err := q.ConectorActivoDeSistema(ctx, sistemaSSO)
			if errors.Is(err, pgx.ErrNoRows) {
				return TicketSSOEmitido{}, conectorApagadoSSO()
			}
			if err != nil {
				return TicketSSOEmitido{}, err
			}
			// Solo el token que Iris entregó a ese conector: ni otro token con sso.emitir ni una sesión.
			if a := t.Actor(); a.TokenID == 0 || c.TokenEntregadoID == nil || *c.TokenEntregadoID != a.TokenID {
				return TicketSSOEmitido{}, nucleo.Prohibido("Solo el token que Iris entregó al conector activo de Argos emite tickets de autologin")
			}
			ticket, err := NuevoSecreto()
			if err != nil {
				return TicketSSOEmitido{}, err
			}
			f, err := q.CrearTicketSSO(ctx, identidadbd.CrearTicketSSOParams{
				ConectorID: c.ID, TicketHash: Hash256(ticket), UsuarioExterno: in.Usuario.IDExterno,
				Correo: valorODefecto(in.Usuario.Correo), Nombre: valorODefecto(in.Usuario.Nombre),
				DestinoTipo: in.Destino.Tipo, DestinoID: destino, EmitidoTokenID: c.TokenEntregadoID,
				ExpiraEn: time.Now().Add(VigenciaTicketSSO),
			})
			if err != nil {
				return TicketSSOEmitido{}, err
			}
			t.FijarEntidad("sso_ticket", f.ID)
			t.Anotar(map[string]any{"conector_id": c.ID})
			return TicketSSOEmitido{URL: cfg.BaseURL + "/sso/entrar#" + ticket, ExpiraEn: f.ExpiraEn}, nil
		},
	})
}

func valorODefecto(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// Canjear --------------------------------------------------------------------------------

// EntradaCanjearSSO es el ticket del fragmento de /sso/entrar.
type EntradaCanjearSSO struct {
	Ticket    string `json:"ticket" minLength:"16" maxLength:"512"`
	IP        string `json:"-"`
	UserAgent string `json:"-"`
}

// DestinoSSOResuelto es a dónde lleva el panel al usuario (ruta interna, nunca una URL externa).
type DestinoSSOResuelto struct {
	Tipo string `json:"tipo" enum:"ticket,orden,prospecto"`
	ID   int64  `json:"id"`
	Ruta string `json:"ruta" doc:"Ruta del panel: /tickets/{id}, /ordenes/{id} o /comercial/{id}"`
}

// UsuarioExternoVisto es el usuario de Argos, para la pantalla de vincular.
type UsuarioExternoVisto struct {
	IDExterno string `json:"idExterno"`
	Correo    string `json:"correo"`
	Nombre    string `json:"nombre"`
}

// CanjeSSO es el resultado del canje.
type CanjeSSO struct {
	Estado  string             `json:"estado" enum:"sesion,mfa,vincular" doc:"sesion: entró; mfa: falta POST /api/v1/sesion/mfa; vincular: inicia sesión y luego POST /api/v1/sso/vincular"`
	Destino DestinoSSOResuelto `json:"destino"`
	// Sesion: con estado sesion o mfa (como POST /api/v1/sesion).
	Sesion *SesionIniciada `json:"sesion,omitempty" required:"false"`
	// Externo y VinculacionExpiraEn: con estado vincular.
	Externo             *UsuarioExternoVisto `json:"externo,omitempty" required:"false"`
	VinculacionExpiraEn *time.Time           `json:"vinculacionExpiraEn,omitempty" required:"false"`
	vinculacion         string
}

func rutaDestino(tipo string, id int64) string {
	switch tipo {
	case "orden":
		return "/ordenes/" + strconv.FormatInt(id, 10)
	case "prospecto":
		return "/comercial/" + strconv.FormatInt(id, 10)
	default:
		return "/tickets/" + strconv.FormatInt(id, 10)
	}
}

func ticketSSOInvalido() *nucleo.Problema {
	return nucleo.NuevoProblema(http.StatusUnauthorized, "ticket_invalido", "Acceso inválido",
		"El enlace de acceso no existe, venció o ya se usó; vuelve a abrirlo desde Argos")
}

func nuevoCanjearSSO(m *Modulo) *auditoria.Caso[EntradaCanjearSSO, CanjeSSO] {
	cfg := m.cfg
	return auditoria.Nuevo(auditoria.Caso[EntradaCanjearSSO, CanjeSSO]{
		Accion: "sso.canjear", Acceso: auditoria.Publico,
		// Los 401 y 429 son de anónimos: no dejan fila; se cuentan y van al log con muestreo.
		AuditarError: m.auditarRechazoPortal,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaCanjearSSO) (CanjeSSO, error) {
			q := identidadbd.New(t)
			t.Anotar(map[string]any{"ip": in.IP, "user_agent": in.UserAgent})
			f, err := q.TomarTicketSSO(ctx, Hash256(in.Ticket))
			if errors.Is(err, pgx.ErrNoRows) {
				ahora := time.Now()
				m.topePortal.sumar(ahora, cfg.PortalVentanaTope)
				if hasta, excedido := m.topePortal.excedido(ahora, cfg.PortalTope); excedido {
					return CanjeSSO{}, nucleo.DemasiadosIntentos("demasiados_intentos",
						"Demasiados intentos fallidos de entrar; vuelve a intentar en un momento", time.Until(hasta))
				}
				return CanjeSSO{}, ticketSSOInvalido()
			}
			if err != nil {
				return CanjeSSO{}, err
			}
			t.FijarEntidad("sso_ticket", f.ID)
			t.Anotar(map[string]any{"conector_id": f.ConectorID, "usuario_externo": f.UsuarioExterno,
				"destino_tipo": f.DestinoTipo, "destino_id": f.DestinoID})
			activo, err := q.ConectorSigueActivo(ctx, f.ConectorID)
			if err != nil {
				return CanjeSSO{}, err
			}
			if !activo {
				return CanjeSSO{}, conectorApagadoSSO()
			}
			salida := CanjeSSO{Destino: DestinoSSOResuelto{Tipo: f.DestinoTipo, ID: f.DestinoID, Ruta: rutaDestino(f.DestinoTipo, f.DestinoID)}}
			v, err := q.VinculoSSO(ctx, identidadbd.VinculoSSOParams{ConectorID: f.ConectorID, UsuarioExterno: f.UsuarioExterno})
			switch {
			case errors.Is(err, pgx.ErrNoRows):
				vinculacion, err := NuevoSecreto()
				if err != nil {
					return CanjeSSO{}, err
				}
				expira := time.Now().Add(VigenciaVinculacionSSO)
				if err := q.PrepararVinculacionSSO(ctx, identidadbd.PrepararVinculacionSSOParams{
					ID: f.ID, VinculacionHash: Hash256(vinculacion), VinculacionExpiraEn: &expira,
				}); err != nil {
					return CanjeSSO{}, err
				}
				t.Anotar(map[string]any{"resultado": "vincular"})
				salida.Estado, salida.vinculacion, salida.VinculacionExpiraEn = "vincular", vinculacion, &expira
				salida.Externo = &UsuarioExternoVisto{IDExterno: f.UsuarioExterno, Correo: f.Correo, Nombre: f.Nombre}
				return salida, nil
			case err != nil:
				return CanjeSSO{}, err
			}
			if !v.Activo || v.Tipo != "persona" {
				return CanjeSSO{}, nucleo.Prohibido("La cuenta de Iris vinculada no está activa")
			}
			u, err := q.UsuarioPorID(ctx, v.UsuarioID)
			if err != nil {
				return CanjeSSO{}, err
			}
			t.IdentificarActor(contexto.Actor{Tipo: contexto.ActorUsuario, ID: u.ID, Nombre: u.Nombre})
			s, err := abrirSesion(ctx, q, cfg, u, u.TotpActivo, in.IP, in.UserAgent)
			if err != nil {
				return CanjeSSO{}, err
			}
			salida.Estado, salida.Sesion = "sesion", &s
			if s.MFARequerido {
				salida.Estado = "mfa"
			}
			t.Anotar(map[string]any{"resultado": salida.Estado, "vinculo_id": v.ID})
			return salida, nil
		},
	})
}

// Vincular -------------------------------------------------------------------------------

// EntradaVincularSSO lleva la vinculación pendiente (cookie iris_sso_vincular).
type EntradaVincularSSO struct {
	Vinculacion string `json:"-"`
}

// VinculoSSO es un vínculo entre un usuario de Iris y uno de Argos.
type VinculoSSO struct {
	ID             int64     `json:"id"`
	ConectorID     int64     `json:"conectorId"`
	Sistema        string    `json:"sistema"`
	UsuarioExterno string    `json:"usuarioExterno" doc:"Id del usuario en el otro sistema"`
	CreadoEn       time.Time `json:"creadoEn"`
}

// VinculoSSOCreado es el vínculo recién guardado y el destino al que seguir.
type VinculoSSOCreado struct {
	Vinculo VinculoSSO         `json:"vinculo"`
	Destino DestinoSSOResuelto `json:"destino"`
}

func vinculacionInvalida() *nucleo.Problema {
	return nucleo.ReglaNegocio("vinculacion_invalida",
		"No hay una vinculación pendiente vigente: vuelve a abrir el enlace desde Argos")
}

func nuevoVincularSSO() *auditoria.Caso[EntradaVincularSSO, VinculoSSOCreado] {
	return auditoria.Nuevo(auditoria.Caso[EntradaVincularSSO, VinculoSSOCreado]{
		Accion: "sso.vincular", Acceso: auditoria.Autenticado,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaVincularSSO) (VinculoSSOCreado, error) {
			q := identidadbd.New(t)
			a, s := t.Actor(), contexto.SesionDe(ctx)
			if a.Tipo != contexto.ActorUsuario || s.ID == 0 {
				return VinculoSSOCreado{}, nucleo.Prohibido("Se vincula desde el panel, con tu sesión")
			}
			if in.Vinculacion == "" {
				return VinculoSSOCreado{}, vinculacionInvalida()
			}
			// Si algo falla, la transacción se deshace y la vinculación sigue pendiente.
			f, err := q.TomarVinculacionSSO(ctx, Hash256(in.Vinculacion))
			if errors.Is(err, pgx.ErrNoRows) {
				return VinculoSSOCreado{}, vinculacionInvalida()
			}
			if err != nil {
				return VinculoSSOCreado{}, err
			}
			t.Anotar(map[string]any{"sso_ticket": f.ID, "conector_id": f.ConectorID, "usuario_externo": f.UsuarioExterno})
			creada, err := q.SesionCreadaEn(ctx, s.ID)
			if err != nil {
				return VinculoSSOCreado{}, err
			}
			if f.CanjeadoEn == nil || !creada.After(*f.CanjeadoEn) {
				return VinculoSSOCreado{}, nucleo.ReglaNegocio("reautenticacion_requerida",
					"Inicia sesión de nuevo para vincular: la sesión debe abrirse después de entrar desde Argos")
			}
			activo, err := q.ConectorSigueActivo(ctx, f.ConectorID)
			if err != nil {
				return VinculoSSOCreado{}, err
			}
			if !activo {
				return VinculoSSOCreado{}, conectorApagadoSSO()
			}
			u, err := q.UsuarioPorID(ctx, a.ID)
			if err != nil {
				return VinculoSSOCreado{}, err
			}
			if u.Tipo != "persona" {
				return VinculoSSOCreado{}, nucleo.Prohibido("Solo un usuario persona se vincula")
			}
			if _, err := q.VinculoSSODeUsuarioEnConector(ctx, identidadbd.VinculoSSODeUsuarioEnConectorParams{ConectorID: f.ConectorID, UsuarioID: u.ID}); err == nil {
				return VinculoSSOCreado{}, nucleo.Conflicto("vinculo_existente", "Tu usuario ya está vinculado con otra cuenta de Argos: quita ese vínculo primero")
			} else if !errors.Is(err, pgx.ErrNoRows) {
				return VinculoSSOCreado{}, err
			}
			if _, err := q.VinculoSSO(ctx, identidadbd.VinculoSSOParams{ConectorID: f.ConectorID, UsuarioExterno: f.UsuarioExterno}); err == nil {
				return VinculoSSOCreado{}, nucleo.Conflicto("cuenta_externa_vinculada", "Esa cuenta de Argos ya está vinculada con otro usuario de Iris")
			} else if !errors.Is(err, pgx.ErrNoRows) {
				return VinculoSSOCreado{}, err
			}
			ticketID := f.ID
			v, err := q.CrearVinculoSSO(ctx, identidadbd.CrearVinculoSSOParams{
				ConectorID: f.ConectorID, UsuarioExterno: f.UsuarioExterno, UsuarioID: u.ID, TicketID: &ticketID,
			})
			if err != nil {
				return VinculoSSOCreado{}, err
			}
			t.FijarEntidad("usuario", u.ID)
			if err := t.Emitir("usuario.sso_vinculado", map[string]any{
				"usuario_id": u.ID, "vinculo_id": v.ID, "conector_id": v.ConectorID, "sistema": sistemaSSO, "usuario_externo": v.UsuarioExterno,
			}); err != nil {
				return VinculoSSOCreado{}, err
			}
			return VinculoSSOCreado{
				Vinculo: VinculoSSO{ID: v.ID, ConectorID: v.ConectorID, Sistema: sistemaSSO, UsuarioExterno: v.UsuarioExterno, CreadoEn: v.CreatedAt},
				Destino: DestinoSSOResuelto{Tipo: f.DestinoTipo, ID: f.DestinoID, Ruta: rutaDestino(f.DestinoTipo, f.DestinoID)},
			}, nil
		},
	})
}

// Ver y borrar vínculos ------------------------------------------------------------------

// EntradaVinculosSSO: los vínculos de un usuario (el propio, o con usuarios.ver).
type EntradaVinculosSSO struct {
	UsuarioID int64 `path:"id" minimum:"1"`
}

// EntradaBorrarVinculoSSO: quitar un vínculo (el propio, o con usuarios.editar).
type EntradaBorrarVinculoSSO struct {
	UsuarioID int64  `json:"-"`
	VinculoID int64  `json:"-"`
	Motivo    string `json:"motivo,omitempty" required:"false" maxLength:"500"`
}

func nuevoListarVinculosSSO() *auditoria.Caso[EntradaVinculosSSO, nucleo.Pagina[VinculoSSO]] {
	return auditoria.Nuevo(auditoria.Caso[EntradaVinculosSSO, nucleo.Pagina[VinculoSSO]]{
		Accion: "usuarios.vinculos_sso", Acceso: auditoria.Autenticado,
		AuditarExito: soloLecturas[nucleo.Pagina[VinculoSSO]],
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaVinculosSSO) (nucleo.Pagina[VinculoSSO], error) {
			q := identidadbd.New(t)
			if err := exigirSobreVinculos(ctx, t, q, in.UsuarioID, nucleo.PermUsuariosVer, "ver sus vínculos"); err != nil {
				return nucleo.Pagina[VinculoSSO]{}, err
			}
			filas, err := q.VinculosSSODeUsuario(ctx, in.UsuarioID)
			if err != nil {
				return nucleo.Pagina[VinculoSSO]{}, err
			}
			datos := make([]VinculoSSO, 0, len(filas))
			for _, f := range filas {
				datos = append(datos, VinculoSSO{ID: f.ID, ConectorID: f.ConectorID, Sistema: f.Sistema, UsuarioExterno: f.UsuarioExterno, CreadoEn: f.CreatedAt})
			}
			return nucleo.Pagina[VinculoSSO]{Data: datos}, nil
		},
	})
}

func nuevoBorrarVinculoSSO() *auditoria.Caso[EntradaBorrarVinculoSSO, VinculoSSO] {
	return auditoria.Nuevo(auditoria.Caso[EntradaBorrarVinculoSSO, VinculoSSO]{
		Accion: "usuarios.borrar_vinculo_sso", Acceso: auditoria.Autenticado,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaBorrarVinculoSSO) (VinculoSSO, error) {
			q := identidadbd.New(t)
			t.FijarEntidad("usuario", in.UsuarioID)
			if err := exigirSobreVinculos(ctx, t, q, in.UsuarioID, nucleo.PermUsuariosEditar, "quitar sus vínculos"); err != nil {
				return VinculoSSO{}, err
			}
			v, err := q.VinculoSSOParaBorrar(ctx, identidadbd.VinculoSSOParaBorrarParams{ID: in.VinculoID, UsuarioID: in.UsuarioID})
			if errors.Is(err, pgx.ErrNoRows) {
				return VinculoSSO{}, nucleo.NoEncontrado("")
			}
			if err != nil {
				return VinculoSSO{}, err
			}
			if err := q.BorrarVinculoSSO(ctx, v.ID); err != nil {
				return VinculoSSO{}, err
			}
			t.Anotar(map[string]any{"vinculo_id": v.ID, "conector_id": v.ConectorID, "usuario_externo": v.UsuarioExterno})
			if err := t.Emitir("usuario.sso_desvinculado", map[string]any{
				"usuario_id": in.UsuarioID, "vinculo_id": v.ID, "conector_id": v.ConectorID, "usuario_externo": v.UsuarioExterno,
				"motivo": in.Motivo,
			}); err != nil {
				return VinculoSSO{}, err
			}
			return VinculoSSO{ID: v.ID, ConectorID: v.ConectorID, Sistema: sistemaSSO, UsuarioExterno: v.UsuarioExterno, CreadoEn: v.CreatedAt}, nil
		},
	})
}

// exigirSobreVinculos: los propios vínculos los ve y quita el usuario (con su sesión o su token);
// los de otro exigen el permiso y el alcance de la administración de usuarios (fuera de alcance,
// 404 al ver; 403 al quitar si sus permisos superan los tuyos).
func exigirSobreVinculos(ctx context.Context, t *auditoria.Tx, q *identidadbd.Queries, objetivo int64, permiso nucleo.Permiso, que string) error {
	a := t.Actor()
	if !a.EsUsuario() {
		return nucleo.Prohibido("Solo usuarios")
	}
	if a.ID == objetivo {
		return nil
	}
	permisos := contexto.PermisosDe(ctx)
	if !permisos.Tiene(string(permiso)) {
		return nucleo.Prohibido("Falta el permiso " + string(permiso))
	}
	if err := exigirVisible(ctx, q, objetivo, que); err != nil {
		return err
	}
	if permiso == nucleo.PermUsuariosVer {
		return nil
	}
	return exigirSuperior(ctx, q, permisos, objetivo, que)
}

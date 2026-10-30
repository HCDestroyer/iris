package identidad

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

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
//     32 bytes aleatorios, 60 s, un solo uso, atado al conector; solo se guarda su SHA-256. Tope de
//     SSOTopeEmision por conector y minuto (429) y alerta si pide tickets para muchos id_externo.
//  2. La página /sso/entrar (I3) canjea el ticket con POST /api/v1/sso/canjear { ticket }:
//     - con vínculo (conector, id_externo) → usuario de Iris: abre una sesión de origen sso (vida
//       más corta; las acciones sensibles piden la contraseña), pendiente de MFA si lo tiene
//       (como el login). Un usuario privilegiado sin TOTP no entra (403 sso_requiere_mfa). Si el
//       navegador ya tiene la sesión de otro usuario, pide confirmar (estado confirmar_cambio)
//       sin consumir el ticket; si es del mismo, la reutiliza;
//     - sin vínculo: deja la cookie iris_sso_vincular (HttpOnly, solo /api/v1/sso, 10 min). El
//       usuario entra con su login normal (con su MFA), ve una confirmación con los datos de Argos
//       y POST /api/v1/sso/vincular { id_externo } guarda el vínculo, solo si la sesión es
//       posterior al canje y el correo de Argos es el de su cuenta (si no, 409
//       vinculo_requiere_admin: lo crea un administrador). El correo es una condición extra:
//       nunca se vincula solo por correo, ni se crean usuarios.
//  3. Cada usuario ve y quita sus vínculos; un administrador los crea, ve y borra. Se borran al
//     desactivar al usuario o al reiniciar su MFA o su contraseña.
//
// Sin límite por IP (como el portal: detrás del proxy del panel todos comparten la IP y el ticket
// no se puede adivinar): los canjes fallidos no se auditan (se cuentan y van al log con muestreo)
// y tienen su propio freno global (PortalTope por PortalVentanaTope → 429).

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
	// rutaDescartarSSO: «No fui yo» (público y sin CSRF, como el canje).
	rutaDescartarSSO = RutasSSO + "/descartar"
	// ventanaTopeSSO: ventana del tope de emisión por conector.
	ventanaTopeSSO = time.Minute
)

// Emitir ---------------------------------------------------------------------------------

// UsuarioExternoSSO es el usuario de Argos que pide entrar.
type UsuarioExternoSSO struct {
	IDExterno string  `json:"id_externo" minLength:"1" maxLength:"100" doc:"Id del usuario en Argos (u-<id>)"`
	Correo    *string `json:"correo,omitempty" required:"false" nullable:"true" maxLength:"254" doc:"Para mostrarlo al vincular y como condición extra de la autovinculación"`
	// CorreoVerificado: Argos verificó el correo (contrato 0.3.1). Sin true, no hay autovinculación.
	CorreoVerificado *bool   `json:"correo_verificado,omitempty" required:"false" nullable:"true" doc:"true si Argos verificó el correo; sin él la vinculación la hace un administrador"`
	Nombre           *string `json:"nombre,omitempty" required:"false" nullable:"true" maxLength:"200"`
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
			// Tope por conector (en la base: vale con varias réplicas de la API), contado con la
			// emisión del conector serializada para que pedidos simultáneos no lo pasen.
			if err := q.BloquearEmisionSSO(ctx, int32(c.ID)); err != nil { //nolint:gosec // ids de conector pequeños
				return TicketSSOEmitido{}, err
			}
			tope, err := q.TicketsSSORecientes(ctx, identidadbd.TicketsSSORecientesParams{ConectorID: c.ID, VentanaSeg: int32(ventanaTopeSSO / time.Second)})
			if err != nil {
				return TicketSSOEmitido{}, err
			}
			if int(tope.Emitidos) >= cfg.SSOTopeEmision {
				espera := ventanaTopeSSO - time.Duration(tope.EdadSeg)*time.Second
				if espera < time.Second {
					espera = time.Second
				}
				return TicketSSOEmitido{}, nucleo.DemasiadosIntentos("demasiados_tickets",
					"Demasiados tickets de autologin de este conector en el último minuto", espera)
			}
			ticket, err := NuevoSecreto()
			if err != nil {
				return TicketSSOEmitido{}, err
			}
			f, err := q.CrearTicketSSO(ctx, identidadbd.CrearTicketSSOParams{
				ConectorID: c.ID, TicketHash: Hash256(ticket), UsuarioExterno: in.Usuario.IDExterno,
				Correo: valorODefecto(in.Usuario.Correo), Nombre: valorODefecto(in.Usuario.Nombre),
				CorreoVerificado: in.Usuario.CorreoVerificado != nil && *in.Usuario.CorreoVerificado,
				DestinoTipo:      in.Destino.Tipo, DestinoID: destino, EmitidoTokenID: c.TokenEntregadoID,
				VigenciaSeg: int32(VigenciaTicketSSO / time.Second),
			})
			if err != nil {
				return TicketSSOEmitido{}, err
			}
			t.FijarEntidad("sso_ticket", f.ID)
			t.Anotar(map[string]any{"conector_id": c.ID})
			// Alerta: un conector que pide tickets para muchos usuarios distintos en poco tiempo
			// (Argos o su token comprometidos recorriendo cuentas).
			reciente, err := q.TicketsSSORecientes(ctx, identidadbd.TicketsSSORecientesParams{ConectorID: c.ID, VentanaSeg: segundos(cfg.SSOVentanaAlerta)})
			if err != nil {
				return TicketSSOEmitido{}, err
			}
			if int(reciente.Externos) > cfg.SSOAlertaExternos {
				t.Anotar(map[string]any{"alerta_externos_distintos": reciente.Externos})
				slog.WarnContext(ctx, "identidad: el conector pide tickets de autologin para muchos usuarios distintos",
					"conector_id", c.ID, "externos", reciente.Externos, "umbral", cfg.SSOAlertaExternos, "ventana", cfg.SSOVentanaAlerta)
			}
			return TicketSSOEmitido{URL: cfg.BaseURL + "/sso/entrar#" + ticket, ExpiraEn: f.ExpiraEn}, nil
		},
	})
}

// segundos convierte una ventana a segundos para la base (acotada a un día).
func segundos(d time.Duration) int32 {
	if d > 24*time.Hour {
		d = 24 * time.Hour
	}
	return int32(d / time.Second) //nolint:gosec // acotado arriba
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
	Ticket    string `json:"ticket" minLength:"16" maxLength:"512" auditoria:"sensible"`
	Confirmar bool   `json:"confirmar,omitempty" required:"false" doc:"true tras estado confirmar_cambio: cerrar la sesión del otro usuario y entrar"`
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
	IDExterno        string `json:"idExterno"`
	Correo           string `json:"correo"`
	CorreoVerificado bool   `json:"correoVerificado" doc:"Argos dice que verificó el correo; sin eso, vincula un administrador"`
	Nombre           string `json:"nombre"`
}

// EntrarComoSSO: para confirmar el cambio de sesión, la cuenta de Iris y la de Argos.
type EntrarComoSSO struct {
	NombreIris    string `json:"nombreIris"`
	IDExterno     string `json:"idExterno"`
	NombreExterno string `json:"nombreExterno"`
}

// CanjeSSO es el resultado del canje.
type CanjeSSO struct {
	Estado  string             `json:"estado" enum:"sesion,mfa,vincular,confirmar_cambio" doc:"sesion: entró (o se reutilizó la sesión del mismo usuario); mfa: falta POST /api/v1/sesion/mfa; vincular: inicia sesión, confirma y POST /api/v1/sso/vincular; confirmar_cambio: el navegador tiene la sesión de otro usuario (repetir con confirmar: true)"`
	Destino DestinoSSOResuelto `json:"destino"`
	// Sesion: con estado sesion (nueva) o mfa (como POST /api/v1/sesion).
	Sesion *SesionIniciada `json:"sesion,omitempty" required:"false"`
	// SesionReutilizada: estado sesion con la sesión que ya tenía el navegador (mismo usuario).
	SesionReutilizada bool `json:"sesionReutilizada,omitempty" required:"false"`
	// EntrarComo: con estado confirmar_cambio, la cuenta con que se entraría.
	EntrarComo *EntrarComoSSO `json:"entrarComo,omitempty" required:"false"`
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

// esPrivilegiado: con algún permiso de administración (nucleo.PermisosSensibles, la misma lista
// que exige re-autenticación) o empresas.todas, el autologin exige TOTP activo.
func esPrivilegiado(p contexto.Permisos) bool {
	for _, n := range p.Lista() {
		if nucleo.EsSensible(nucleo.Permiso(n)) || n == string(nucleo.PermEmpresasTodas) {
			return true
		}
	}
	return false
}

// usuarioParaSSO aplica al usuario vinculado las barreras del login (activo, persona, con
// contraseña, sin bloqueo por fuerza bruta) y exige TOTP a los privilegiados.
func usuarioParaSSO(ctx context.Context, q *identidadbd.Queries, id int64) (identidadbd.NucleoUsuario, error) {
	u, err := q.UsuarioPorID(ctx, id)
	if err != nil {
		return u, err
	}
	if !u.Activo || u.Tipo != "persona" || u.ContrasenaHash == nil {
		return u, nucleo.Prohibido("La cuenta de Iris vinculada no está disponible para entrar")
	}
	hasta, err := q.BloqueoLogin(ctx, u.Correo)
	switch {
	case err == nil:
		return u, nucleo.DemasiadosIntentos("demasiados_intentos",
			"La cuenta está bloqueada por intentos fallidos; espera antes de volver a intentar", time.Until(hasta))
	case !errors.Is(err, pgx.ErrNoRows):
		return u, err
	}
	if !u.TotpActivo {
		permisos, err := cargarPermisos(ctx, q, u.ID, nil)
		if err != nil {
			return u, err
		}
		if esPrivilegiado(permisos) {
			return u, nucleo.NuevoProblema(http.StatusForbidden, "sso_requiere_mfa", "MFA requerido",
				"Tu usuario tiene permisos de administración: activa el MFA en Iris para entrar desde Argos")
		}
	}
	return u, nil
}

// auditarRechazoSSO: como auditarRechazoPortal, con su propio contador.
func (m *Modulo) auditarRechazoSSO(err error) bool {
	p, _ := nucleo.AProblema(err)
	if p.Status != http.StatusUnauthorized && p.Status != http.StatusTooManyRequests {
		return true
	}
	if p.Codigo() != "ticket_invalido" && p.Codigo() != "demasiados_canjes" {
		return true // un 429 por bloqueo de la cuenta vinculada sí se audita
	}
	if n := m.rechazosSSO.Add(1); n == 1 || n%100 == 0 {
		slog.Warn("identidad: canjes del autologin rechazados sin auditar", "codigo", p.Codigo(), "total", n)
	}
	return false
}

// RechazosSSOSinAuditar devuelve cuántos canjes del autologin se rechazaron sin auditar.
func (m *Modulo) RechazosSSOSinAuditar() int64 { return m.rechazosSSO.Load() }

func nuevoCanjearSSO(m *Modulo) *auditoria.Caso[EntradaCanjearSSO, CanjeSSO] {
	cfg := m.cfg
	return auditoria.Nuevo(auditoria.Caso[EntradaCanjearSSO, CanjeSSO]{
		Accion: "sso.canjear", Acceso: auditoria.Publico,
		// Los 401 y 429 de un ticket inválido son de anónimos: no dejan fila; se cuentan.
		AuditarError: m.auditarRechazoSSO,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaCanjearSSO) (CanjeSSO, error) {
			q := identidadbd.New(t)
			// Quien canjea es anónimo hasta que entra (una sesión previa del navegador no es el actor).
			t.IdentificarActor(contexto.Actor{Tipo: contexto.ActorAnonimo})
			t.Anotar(map[string]any{"ip": in.IP, "user_agent": in.UserAgent})
			fallo := func() (CanjeSSO, error) {
				ahora := time.Now()
				m.topeSSO.sumar(ahora, cfg.PortalVentanaTope)
				if hasta, excedido := m.topeSSO.excedido(ahora, cfg.PortalTope); excedido {
					return CanjeSSO{}, nucleo.DemasiadosIntentos("demasiados_canjes",
						"Demasiados intentos fallidos de entrar; vuelve a intentar en un momento", time.Until(hasta))
				}
				return CanjeSSO{}, ticketSSOInvalido()
			}
			previa := contexto.SesionDe(ctx)
			if previa.MFAPendiente {
				previa = contexto.Sesion{} // una sesión a medio entrar no cuenta
			}

			// Sesión de otro usuario en el navegador: confirmar antes de consumir el ticket.
			if previa.ID != 0 && !in.Confirmar {
				vista, err := q.VerTicketSSO(ctx, Hash256(in.Ticket))
				if errors.Is(err, pgx.ErrNoRows) {
					return fallo()
				}
				if err != nil {
					return CanjeSSO{}, err
				}
				v, err := q.VinculoSSO(ctx, identidadbd.VinculoSSOParams{ConectorID: vista.ConectorID, UsuarioExterno: vista.UsuarioExterno})
				if err == nil && v.UsuarioID != previa.UsuarioID {
					u, err := q.UsuarioPorID(ctx, v.UsuarioID)
					if err != nil {
						return CanjeSSO{}, err
					}
					t.FijarEntidad("sso_ticket", vista.ID)
					t.Anotar(map[string]any{"resultado": "confirmar_cambio"})
					return CanjeSSO{Estado: "confirmar_cambio",
						EntrarComo: &EntrarComoSSO{NombreIris: u.Nombre, IDExterno: vista.UsuarioExterno, NombreExterno: vista.Nombre},
						Destino:    DestinoSSOResuelto{Tipo: vista.DestinoTipo, ID: vista.DestinoID, Ruta: rutaDestino(vista.DestinoTipo, vista.DestinoID)}}, nil
				} else if err != nil && !errors.Is(err, pgx.ErrNoRows) {
					return CanjeSSO{}, err
				}
			}

			f, err := q.TomarTicketSSO(ctx, Hash256(in.Ticket))
			if errors.Is(err, pgx.ErrNoRows) {
				return fallo()
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
				expira, err := q.PrepararVinculacionSSO(ctx, identidadbd.PrepararVinculacionSSOParams{
					ID: f.ID, VinculacionHash: Hash256(vinculacion), VigenciaSeg: int32(VigenciaVinculacionSSO / time.Second),
				})
				if err != nil {
					return CanjeSSO{}, err
				}
				t.Anotar(map[string]any{"resultado": "vincular"})
				salida.Estado, salida.vinculacion, salida.VinculacionExpiraEn = "vincular", vinculacion, &expira
				salida.Externo = &UsuarioExternoVisto{IDExterno: f.UsuarioExterno, Correo: f.Correo, CorreoVerificado: f.CorreoVerificado, Nombre: f.Nombre}
				return salida, nil
			case err != nil:
				return CanjeSSO{}, err
			}
			t.Anotar(map[string]any{"vinculo_id": v.ID})
			u, err := usuarioParaSSO(ctx, q, v.UsuarioID)
			if err != nil {
				return CanjeSSO{}, err
			}
			t.IdentificarActor(contexto.Actor{Tipo: contexto.ActorUsuario, ID: u.ID, Nombre: u.Nombre})
			if previa.ID != 0 && previa.UsuarioID == u.ID {
				t.Anotar(map[string]any{"resultado": "sesion_reutilizada"})
				salida.Estado, salida.SesionReutilizada = "sesion", true
				return salida, nil
			}
			if previa.ID != 0 {
				// Confirmado: la sesión del otro usuario se cierra.
				if _, err := q.RevocarSesion(ctx, previa.ID); err != nil {
					return CanjeSSO{}, err
				}
				t.Anotar(map[string]any{"sesion_cerrada": previa.ID})
			}
			s, err := abrirSesion(ctx, q, cfg, u, u.TotpActivo, in.IP, in.UserAgent, contexto.OrigenSesionSSO)
			if err != nil {
				return CanjeSSO{}, err
			}
			salida.Estado, salida.Sesion = "sesion", &s
			if s.MFARequerido {
				salida.Estado = "mfa"
			}
			t.Anotar(map[string]any{"resultado": salida.Estado})
			return salida, nil
		},
	})
}

// Vincular -------------------------------------------------------------------------------

// EntradaVincularSSO: el id de Argos que el usuario confirmó en pantalla y la vinculación
// pendiente (cookie iris_sso_vincular).
type EntradaVincularSSO struct {
	IDExterno   string `json:"id_externo" minLength:"1" maxLength:"100" doc:"El id de Argos que se mostró al usuario (externo.idExterno del canje)"`
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
		"No hay una vinculación pendiente vigente para esa cuenta de Argos: vuelve a abrir el enlace desde Argos")
}

// crearVinculo guarda el vínculo (409 si el usuario o la cuenta de Argos ya tienen uno) y emite
// usuario.sso_vinculado.
func crearVinculo(ctx context.Context, t *auditoria.Tx, q *identidadbd.Queries, conector int64, externo string, usuario int64, ticket *int64, origen string) (VinculoSSO, error) {
	if _, err := q.VinculoSSODeUsuarioEnConector(ctx, identidadbd.VinculoSSODeUsuarioEnConectorParams{ConectorID: conector, UsuarioID: usuario}); err == nil {
		return VinculoSSO{}, nucleo.Conflicto("vinculo_existente", "El usuario ya está vinculado con otra cuenta de Argos: quita ese vínculo primero")
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return VinculoSSO{}, err
	}
	if _, err := q.VinculoSSO(ctx, identidadbd.VinculoSSOParams{ConectorID: conector, UsuarioExterno: externo}); err == nil {
		return VinculoSSO{}, nucleo.Conflicto("cuenta_externa_vinculada", "Esa cuenta de Argos ya está vinculada con otro usuario de Iris")
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return VinculoSSO{}, err
	}
	v, err := q.CrearVinculoSSO(ctx, identidadbd.CrearVinculoSSOParams{ConectorID: conector, UsuarioExterno: externo, UsuarioID: usuario, TicketID: ticket})
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		// Carrera con otra vinculación simultánea.
		return VinculoSSO{}, nucleo.Conflicto("vinculo_existente", "El usuario o la cuenta de Argos ya tienen un vínculo")
	}
	if err != nil {
		return VinculoSSO{}, err
	}
	t.FijarEntidad("usuario", usuario)
	t.Anotar(map[string]any{"vinculo_id": v.ID, "conector_id": conector, "usuario_externo": externo})
	// Aviso por correo al usuario: pendiente (Iris aún no tiene correo saliente del sistema;
	// IRIS_SMTP_* reservadas). Lo podrá enviar un consumidor de este evento.
	if err := t.Emitir("usuario.sso_vinculado", map[string]any{
		"usuario_id": usuario, "vinculo_id": v.ID, "conector_id": conector, "sistema": sistemaSSO,
		"usuario_externo": externo, "origen": origen,
	}); err != nil {
		return VinculoSSO{}, err
	}
	return VinculoSSO{ID: v.ID, ConectorID: v.ConectorID, Sistema: sistemaSSO, UsuarioExterno: v.UsuarioExterno, CreadoEn: v.CreatedAt}, nil
}

// exigirSesionPanel: la acción solo con la sesión del panel (no con un token).
func exigirSesionPanel(ctx context.Context, a contexto.Actor) error {
	if a.Tipo != contexto.ActorUsuario || contexto.SesionDe(ctx).ID == 0 {
		return nucleo.Prohibido("Solo con la sesión del panel")
	}
	return nil
}

func nuevoVincularSSO() *auditoria.Caso[EntradaVincularSSO, VinculoSSOCreado] {
	return auditoria.Nuevo(auditoria.Caso[EntradaVincularSSO, VinculoSSOCreado]{
		Accion: "sso.vincular", Acceso: auditoria.Autenticado,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaVincularSSO) (VinculoSSOCreado, error) {
			q := identidadbd.New(t)
			a, s := t.Actor(), contexto.SesionDe(ctx)
			if err := exigirSesionPanel(ctx, a); err != nil {
				return VinculoSSOCreado{}, err
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
			t.Anotar(map[string]any{"sso_ticket_id": f.ID, "conector_id": f.ConectorID, "usuario_externo": f.UsuarioExterno})
			// Lo que el usuario confirmó en pantalla debe ser la cuenta del ticket.
			if in.IDExterno != f.UsuarioExterno {
				return VinculoSSOCreado{}, vinculacionInvalida()
			}
			creada, err := q.SesionCreadaEn(ctx, s.ID)
			if err != nil {
				return VinculoSSOCreado{}, err
			}
			if f.CanjeadoEn == nil || !creada.After(*f.CanjeadoEn) {
				return VinculoSSOCreado{}, nucleo.ReglaNegocio("sesion_anterior_al_canje",
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
			// Condiciones extra (nunca suficientes solas): Argos verificó el correo y es el de la
			// cuenta (un empleado de Argos podría poner el correo de otro en su perfil).
			if c := strings.ToLower(strings.TrimSpace(f.Correo)); !f.CorreoVerificado || c == "" || c != strings.ToLower(strings.TrimSpace(u.Correo)) {
				return VinculoSSOCreado{}, nucleo.Conflicto("vinculo_requiere_admin",
					"Argos no confirma que el correo de tu cuenta allá sea verificado y el mismo de tu usuario de Iris: pide a un administrador que vincule las cuentas")
			}
			ticketID := f.ID
			v, err := crearVinculo(ctx, t, q, f.ConectorID, f.UsuarioExterno, u.ID, &ticketID, "usuario")
			if err != nil {
				return VinculoSSOCreado{}, err
			}
			return VinculoSSOCreado{Vinculo: v, Destino: DestinoSSOResuelto{Tipo: f.DestinoTipo, ID: f.DestinoID, Ruta: rutaDestino(f.DestinoTipo, f.DestinoID)}}, nil
		},
	})
}

// Ver, crear (admin) y borrar vínculos ---------------------------------------------------

// EntradaVinculosSSO: los vínculos de un usuario (el propio, o con usuarios.ver).
type EntradaVinculosSSO struct {
	UsuarioID int64 `path:"id" minimum:"1"`
}

// EntradaCrearVinculoSSO: un administrador vincula un usuario con una cuenta de Argos.
type EntradaCrearVinculoSSO struct {
	UsuarioID  int64  `json:"-"`
	ConectorID int64  `json:"conector_id" minimum:"1"`
	IDExterno  string `json:"id_externo" minLength:"1" maxLength:"100"`
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

func nuevoCrearVinculoSSO() *auditoria.Caso[EntradaCrearVinculoSSO, VinculoSSO] {
	return auditoria.Nuevo(auditoria.Caso[EntradaCrearVinculoSSO, VinculoSSO]{
		Accion: "usuarios.vincular_sso", Permiso: nucleo.PermUsuariosEditar, SinEmpresa: true,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaCrearVinculoSSO) (VinculoSSO, error) {
			q := identidadbd.New(t)
			t.FijarEntidad("usuario", in.UsuarioID)
			if err := exigirSesionPanel(ctx, t.Actor()); err != nil {
				return VinculoSSO{}, err
			}
			if err := exigirVisible(ctx, q, in.UsuarioID, "vincularlo"); err != nil {
				return VinculoSSO{}, err
			}
			if err := exigirSuperior(ctx, q, contexto.PermisosDe(ctx), in.UsuarioID, "vincularlo"); err != nil {
				return VinculoSSO{}, err
			}
			u, err := q.UsuarioPorID(ctx, in.UsuarioID)
			if err != nil {
				return VinculoSSO{}, err
			}
			if u.Tipo != "persona" || !u.Activo {
				return VinculoSSO{}, nucleo.ReglaNegocio("usuario_no_vinculable", "Solo un usuario persona activo se vincula")
			}
			sistema, err := q.SistemaDeConector(ctx, in.ConectorID)
			if errors.Is(err, pgx.ErrNoRows) || (err == nil && sistema != sistemaSSO) {
				return VinculoSSO{}, nucleo.ReglaNegocio("conector_invalido", "El conector no existe o no es de Argos")
			}
			if err != nil {
				return VinculoSSO{}, err
			}
			return crearVinculo(ctx, t, q, in.ConectorID, in.IDExterno, u.ID, nil, "admin")
		},
	})
}

func nuevoBorrarVinculoSSO() *auditoria.Caso[EntradaBorrarVinculoSSO, VinculoSSO] {
	return auditoria.Nuevo(auditoria.Caso[EntradaBorrarVinculoSSO, VinculoSSO]{
		Accion: "usuarios.borrar_vinculo_sso", Acceso: auditoria.Autenticado,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaBorrarVinculoSSO) (VinculoSSO, error) {
			q := identidadbd.New(t)
			t.FijarEntidad("usuario", in.UsuarioID)
			if err := exigirSesionPanel(ctx, t.Actor()); err != nil {
				return VinculoSSO{}, err
			}
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
			if err := emitirDesvinculado(t, v, in.Motivo); err != nil {
				return VinculoSSO{}, err
			}
			return VinculoSSO{ID: v.ID, ConectorID: v.ConectorID, Sistema: sistemaSSO, UsuarioExterno: v.UsuarioExterno, CreadoEn: v.CreatedAt}, nil
		},
	})
}

func emitirDesvinculado(t *auditoria.Tx, v identidadbd.NucleoSsoVinculo, motivo string) error {
	t.Anotar(map[string]any{"vinculo_id": v.ID, "conector_id": v.ConectorID, "usuario_externo": v.UsuarioExterno})
	return t.Emitir("usuario.sso_desvinculado", map[string]any{
		"usuario_id": v.UsuarioID, "vinculo_id": v.ID, "conector_id": v.ConectorID, "usuario_externo": v.UsuarioExterno,
		"motivo": motivo,
	})
}

// borrarVinculosSSO borra los vínculos del usuario (al desactivarlo o al reiniciar su MFA o su
// contraseña: quien tome la cuenta no hereda el autologin) y emite un usuario.sso_desvinculado
// por cada uno.
func borrarVinculosSSO(ctx context.Context, t *auditoria.Tx, q *identidadbd.Queries, usuario int64, motivo string) error {
	borrados, err := q.BorrarVinculosSSODeUsuario(ctx, usuario)
	if err != nil {
		return err
	}
	for _, v := range borrados {
		if err := emitirDesvinculado(t, v, motivo); err != nil {
			return err
		}
	}
	if len(borrados) > 0 {
		t.Anotar(map[string]any{"vinculos_sso_borrados": len(borrados)})
	}
	return nil
}

// exigirSobreVinculos: los propios vínculos los ve y quita el usuario; los de otro exigen el
// permiso y el alcance de la administración de usuarios (fuera de alcance, 404 al ver; 403 al
// quitar si sus permisos superan los tuyos).
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

// Descartar ---------------------------------------------------------------------------------

// EntradaDescartarSSO lleva la vinculación pendiente (cookie iris_sso_vincular), si hay.
type EntradaDescartarSSO struct {
	Vinculacion string `json:"-"`
}

// nuevoDescartarSSO: «No fui yo» en la pantalla de vincular. La vinculación pendiente deja de
// servir (se borra su hash). Idempotente: sin cookie, o ya descartada o usada, no hace nada y no
// deja fila de auditoría.
func nuevoDescartarSSO() *auditoria.Caso[EntradaDescartarSSO, bool] {
	return auditoria.Nuevo(auditoria.Caso[EntradaDescartarSSO, bool]{
		Accion: "sso.descartar", Acceso: auditoria.Publico,
		AuditarExito: func(descartada bool) bool { return descartada },
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaDescartarSSO) (bool, error) {
			if in.Vinculacion == "" {
				return false, nil
			}
			// Quien descarta es anónimo (una sesión previa del navegador no es el actor).
			t.IdentificarActor(contexto.Actor{Tipo: contexto.ActorAnonimo})
			id, err := identidadbd.New(t).DescartarVinculacionSSO(ctx, Hash256(in.Vinculacion))
			if errors.Is(err, pgx.ErrNoRows) {
				return false, nil
			}
			if err != nil {
				return false, err
			}
			t.FijarEntidad("sso_ticket", id)
			return true, nil
		},
	})
}

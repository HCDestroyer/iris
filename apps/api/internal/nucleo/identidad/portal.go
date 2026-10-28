package identidad

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
	identidadbd "github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad/bd"
)

// Portal de soporte (/soporte). El cliente final no tiene usuario en Iris: lo identifica Argos,
// con el patrón de autologin por ticket del contrato (§1.3):
//
//  1. Argos, con su token de integración iris_pat_ y el permiso portal.emitir, pide
//     POST /api/v1/portal/tickets-acceso { clienteId, empresaId, nombre? } y recibe la URL
//     <IRIS_BASE_URL>/soporte/entrar#<ticket>, válida 60 s y de un solo uso (pedido A9).
//  2. El portal canjea el ticket con POST /api/v1/portal/sesion { ticket }: se abre una sesión
//     de «actor cliente» (contexto.ActorCliente, con el cliente y la empresa de Argos fijos),
//     con cookie propia (iris_portal, HttpOnly, SameSite=Lax) y CSRF de doble envío
//     (iris_portal_csrf ↔ X-CSRF-Token), de vida corta (2 h, IRIS_DURACION_SESION_PORTAL).
//  3. Los casos de uso del portal usan Acceso auditoria.Cliente: exigen el actor cliente y fijan
//     la empresa activa a la de su sesión. Ninguna otra acción acepta al actor cliente, y la
//     cookie del portal solo se lee en /api/v1/portal/…, donde la del panel no cuenta
//     (middleware.go).
//
// Los canjes fallidos cuentan por IP (como el inicio de sesión por correo): al llegar al máximo
// la IP queda bloqueada (429 demasiados_intentos con Retry-After, aun con un ticket válido).

// Cookies del portal.
const (
	CookiePortal     = "iris_portal"
	CookiePortalCSRF = "iris_portal_csrf"
	// PrefijoRutasPortal: la cookie del portal solo autentica aquí.
	PrefijoRutasPortal = "/api/v1/portal/"
	rutaSesionPortal   = "/api/v1/portal/sesion"
)

// ClientePortal es el cliente de Argos de una sesión del portal.
type ClientePortal struct {
	ClienteID int64  `json:"clienteId" doc:"Id del cliente en Argos"`
	EmpresaID int64  `json:"empresaId" doc:"Empresa del cliente en Argos"`
	Nombre    string `json:"nombre" doc:"Nombre a mostrar (lo manda Argos al emitir el acceso)"`
}

// actorCliente arma el actor de una sesión del portal.
func actorCliente(clienteID, empresaID int64, nombre string) contexto.Actor {
	n := "cliente " + strconv.FormatInt(clienteID, 10)
	if nombre != "" {
		n += " (" + nombre + ")"
	}
	return contexto.Actor{Tipo: contexto.ActorCliente, ClienteID: clienteID, EmpresaID: empresaID, Nombre: n}
}

// Emitir un ticket de acceso -------------------------------------------------------------------

// EntradaEmitirAccesoPortal es la entrada de portal.emitir.
type EntradaEmitirAccesoPortal struct {
	ClienteID int64  `json:"clienteId" minimum:"1" doc:"Id del cliente en Argos"`
	EmpresaID int64  `json:"empresaId" minimum:"1" doc:"Empresa del cliente en Argos; debe ser una en que opera quien emite"`
	Nombre    string `json:"nombre,omitempty" maxLength:"200" doc:"Nombre a mostrar en el portal"`
}

// AccesoPortalEmitido es la URL de entrada al portal. Lleva un secreto (el ticket).
type AccesoPortalEmitido struct {
	URL      string    `json:"url" doc:"<IRIS_BASE_URL>/soporte/entrar#<ticket>: válida 60 s y de un solo uso; se muestra una sola vez"`
	ExpiraEn time.Time `json:"expiraEn"`
}

func nuevoEmitirAccesoPortal(cfg Config) *auditoria.Caso[EntradaEmitirAccesoPortal, AccesoPortalEmitido] {
	return auditoria.Nuevo(auditoria.Caso[EntradaEmitirAccesoPortal, AccesoPortalEmitido]{
		Accion: "portal.emitir", Permiso: nucleo.PermPortalEmitir,
		// La empresa no es la activa de la petición: es la del cliente, que se valida contra las
		// del actor (una integración puede operar en varias sin mandar X-Iris-Empresa).
		SinEmpresa: true,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaEmitirAccesoPortal) (AccesoPortalEmitido, error) {
			q := identidadbd.New(t)
			t.FijarEmpresa(in.EmpresaID)
			t.Anotar(map[string]any{"cliente_id": in.ClienteID})
			if err := empresaParaEmitir(ctx, q, in.EmpresaID); err != nil {
				return AccesoPortalEmitido{}, err
			}
			// Si Iris ya conoce al cliente (instantánea de Argos), debe ser de esa empresa.
			switch e, err := q.EmpresaDeClienteArgos(ctx, in.ClienteID); {
			case err == nil && e != in.EmpresaID:
				return AccesoPortalEmitido{}, nucleo.ReglaNegocio("empresa_no_coincide", "El cliente es de otra empresa según Argos")
			case err != nil && !errors.Is(err, pgx.ErrNoRows):
				return AccesoPortalEmitido{}, err
			}
			ticket, err := NuevoSecreto()
			if err != nil {
				return AccesoPortalEmitido{}, err
			}
			actor := t.Actor()
			var emitidoPor, tokenID *int64
			if actor.EsUsuario() {
				emitidoPor = &actor.ID
			}
			if actor.TokenID != 0 {
				tokenID = &actor.TokenID
			}
			f, err := q.CrearAccesoPortal(ctx, identidadbd.CrearAccesoPortalParams{
				TicketHash: Hash256(ticket), ClienteID: in.ClienteID, EmpresaID: in.EmpresaID, Nombre: in.Nombre,
				EmitidoPor: emitidoPor, TokenID: tokenID, ExpiraEn: time.Now().Add(cfg.VigenciaAccesoPortal),
			})
			if err != nil {
				return AccesoPortalEmitido{}, err
			}
			t.FijarEntidad("acceso_portal", f.ID)
			return AccesoPortalEmitido{URL: cfg.BaseURL + "/soporte/entrar#" + ticket, ExpiraEn: f.ExpiraEn}, nil
		},
	})
}

// empresaParaEmitir: un usuario, token o integración solo emite para una empresa en que opera
// (403 empresa_no_permitida); si mandó X-Iris-Empresa, debe ser esa (422 empresa_no_coincide).
// El actor sistema (consola) solo exige que esté registrada (422 empresa_no_registrada).
func empresaParaEmitir(ctx context.Context, q *identidadbd.Queries, empresa int64) error {
	noRegistrada := nucleo.ReglaNegocio("empresa_no_registrada", "La empresa no está registrada en Iris (POST /api/v1/empresas o conector Argos)")
	if !contexto.ActorDe(ctx).EsUsuario() {
		if _, err := q.EmpresaPorID(ctx, empresa); errors.Is(err, pgx.ErrNoRows) {
			return noRegistrada
		} else if err != nil {
			return err
		}
		return nil
	}
	e := contexto.EmpresasDe(ctx)
	if e.Estado == contexto.EmpresaNoResuelta {
		var err error
		if e, err = ResolverEmpresa(ctx, q, ""); err != nil {
			return err
		}
	}
	switch {
	case e.Estado == contexto.EmpresaSinConector:
		return noRegistrada
	case !slices.Contains(e.Disponibles, empresa):
		return nucleo.EmpresaNoPermitida()
	case e.Estado == contexto.EmpresaActiva && e.Activa != empresa:
		return nucleo.ReglaNegocio("empresa_no_coincide", "empresaId no es la empresa activa (X-Iris-Empresa)")
	}
	return nil
}

// Canjear el ticket (abrir la sesión del portal) ---------------------------------------------

// EntradaEntrarPortal es la entrada de portal.entrar.
type EntradaEntrarPortal struct {
	Ticket    string `json:"ticket" minLength:"16" maxLength:"200" auditoria:"sensible" doc:"Lo que sigue a # en /soporte/entrar#<ticket>"`
	IP        string `json:"-"`
	UserAgent string `json:"-"`
}

// SesionPortal es la sesión abierta. El valor de la cookie nunca sale en el cuerpo.
type SesionPortal struct {
	Token     string        `json:"-"`
	CSRFToken string        `json:"csrfToken" doc:"Valor para la cabecera X-CSRF-Token (también va en la cookie iris_portal_csrf)"`
	ExpiraEn  time.Time     `json:"expiraEn"`
	Cliente   ClientePortal `json:"cliente"`
}

func ticketInvalido() *nucleo.Problema {
	return nucleo.NuevoProblema(http.StatusUnauthorized, "ticket_invalido", "Acceso inválido",
		"El enlace de acceso no existe, venció o ya se usó; vuelve a entrar desde el portal de Argos")
}

func nuevoEntrarPortal(m *Modulo) *auditoria.Caso[EntradaEntrarPortal, SesionPortal] {
	cfg := m.cfg
	return auditoria.Nuevo(auditoria.Caso[EntradaEntrarPortal, SesionPortal]{
		Accion: "portal.entrar", Acceso: auditoria.Publico,
		// Los 401 (ticket inválido) y 429 (IP bloqueada) son de anónimos: no dejan fila (se
		// podría inundar la tabla); se cuentan y van al log con muestreo. El conteo por IP
		// (AlFallar) corre igual.
		AuditarError: m.auditarRechazoPortal,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaEntrarPortal) (SesionPortal, error) {
			q := identidadbd.New(t)
			t.Anotar(map[string]any{"ip": in.IP, "user_agent": in.UserAgent})
			hasta, err := q.BloqueoPortal(ctx, in.IP)
			switch {
			case err == nil:
				return SesionPortal{}, nucleo.DemasiadosIntentos("demasiados_intentos",
					"Demasiados intentos fallidos de entrar al portal; espera antes de volver a intentar", time.Until(hasta))
			case !errors.Is(err, pgx.ErrNoRows):
				return SesionPortal{}, err
			}
			acceso, err := q.TomarAccesoPortal(ctx, Hash256(in.Ticket))
			if errors.Is(err, pgx.ErrNoRows) {
				contarFalloPortal(t, cfg, in.IP)
				return SesionPortal{}, ticketInvalido()
			}
			if err != nil {
				return SesionPortal{}, err
			}
			// Una sesión del portal anterior en el mismo navegador se cierra.
			if previa := contexto.SesionPortalDe(ctx); previa != 0 {
				if _, err := q.RevocarSesionPortal(ctx, previa); err != nil {
					return SesionPortal{}, err
				}
			}
			token, err := NuevoSecreto()
			if err != nil {
				return SesionPortal{}, err
			}
			csrf, err := NuevoSecreto()
			if err != nil {
				return SesionPortal{}, err
			}
			s, err := q.CrearSesionPortal(ctx, identidadbd.CrearSesionPortalParams{
				AccesoID: acceso.ID, ClienteID: acceso.ClienteID, EmpresaID: acceso.EmpresaID, Nombre: acceso.Nombre,
				TokenHash: Hash256(token), CsrfHash: Hash256(csrf), Ip: recortar(in.IP, 64), UserAgent: recortar(in.UserAgent, 400),
				ExpiraEn: time.Now().Add(cfg.DuracionSesionPortal),
			})
			if err != nil {
				return SesionPortal{}, err
			}
			t.IdentificarActor(actorCliente(s.ClienteID, s.EmpresaID, s.Nombre))
			t.FijarEmpresa(s.EmpresaID)
			t.FijarEntidad("sesion_portal", s.ID)
			return SesionPortal{
				Token: token, CSRFToken: csrf, ExpiraEn: s.ExpiraEn,
				Cliente: ClientePortal{ClienteID: s.ClienteID, EmpresaID: s.EmpresaID, Nombre: s.Nombre},
			}, nil
		},
	})
}

// contarFalloPortal suma el fallo a la IP (en la transacción aparte del error, que sí se
// confirma) y, al llegar a PortalMaxFallos dentro de PortalVentana, la bloquea PortalBloqueo.
func contarFalloPortal(t *auditoria.Tx, cfg Config, ip string) {
	t.AlFallar(func(ctx context.Context, tx pgx.Tx) error {
		q := identidadbd.New(tx)
		ahora := time.Now()
		fallos, err := q.RegistrarFalloPortal(ctx, identidadbd.RegistrarFalloPortalParams{Ip: ip, Desde: ahora.Add(-cfg.PortalVentana)})
		if err != nil || fallos < cfg.PortalMaxFallos {
			return err
		}
		hasta := ahora.Add(cfg.PortalBloqueo)
		if err := q.BloquearPortal(ctx, identidadbd.BloquearPortalParams{Ip: ip, Hasta: &hasta}); err != nil {
			return err
		}
		slog.WarnContext(ctx, "identidad: canje del portal bloqueado por fallos", "ip", ip, "fallos", fallos, "hasta", hasta)
		return nil
	})
}

// auditarRechazoPortal: los 401 y 429 del canje no se auditan; se cuentan y se registran con
// muestreo (el primero y uno de cada 100).
func (m *Modulo) auditarRechazoPortal(err error) bool {
	p, _ := nucleo.AProblema(err)
	if p.Status != http.StatusUnauthorized && p.Status != http.StatusTooManyRequests {
		return true
	}
	if n := m.rechazosPortal.Add(1); n == 1 || n%100 == 0 {
		slog.Warn("identidad: canjes del portal rechazados sin auditar", "codigo", p.Codigo(), "total", n)
	}
	return false
}

// RechazosPortalSinAuditar devuelve cuántos canjes del portal se rechazaron sin auditar.
func (m *Modulo) RechazosPortalSinAuditar() int64 { return m.rechazosPortal.Load() }

// Ver y cerrar la sesión del portal --------------------------------------------------------

// SesionPortalActual es la sesión con que llegó la petición del portal.
type SesionPortalActual struct {
	Cliente  ClientePortal `json:"cliente"`
	ExpiraEn time.Time     `json:"expiraEn"`
}

func nuevoObtenerSesionPortal() *auditoria.Caso[struct{}, SesionPortalActual] {
	return auditoria.Nuevo(auditoria.Caso[struct{}, SesionPortalActual]{
		Accion: "portal.sesion", Acceso: auditoria.Cliente, AuditarExito: soloLecturas[SesionPortalActual],
		Logica: func(ctx context.Context, t *auditoria.Tx, _ struct{}) (SesionPortalActual, error) {
			id := contexto.SesionPortalDe(ctx)
			if id == 0 {
				return SesionPortalActual{}, nucleo.NoAutenticado("No hay una sesión del portal")
			}
			s, err := identidadbd.New(t).SesionPortalPorID(ctx, id)
			if err != nil {
				return SesionPortalActual{}, err
			}
			return SesionPortalActual{
				Cliente:  ClientePortal{ClienteID: s.ClienteID, EmpresaID: s.EmpresaID, Nombre: s.Nombre},
				ExpiraEn: s.ExpiraEn,
			}, nil
		},
	})
}

func nuevoCerrarSesionPortal() *auditoria.Caso[struct{}, struct{}] {
	return auditoria.Nuevo(auditoria.Caso[struct{}, struct{}]{
		Accion: "portal.salir", Acceso: auditoria.Cliente,
		Logica: func(ctx context.Context, t *auditoria.Tx, _ struct{}) (struct{}, error) {
			id := contexto.SesionPortalDe(ctx)
			if id == 0 {
				return struct{}{}, nucleo.NoAutenticado("No hay una sesión del portal")
			}
			t.FijarEntidad("sesion_portal", id)
			_, err := identidadbd.New(t).RevocarSesionPortal(ctx, id)
			return struct{}{}, err
		},
	})
}

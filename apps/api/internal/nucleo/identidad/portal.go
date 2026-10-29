package identidad

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"sync"
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
// Sin límite por IP (detrás del proxy del panel todos los clientes comparten la IP, y el ticket
// de 256 bits, 60 s y un solo uso no se puede adivinar): los canjes rechazados se cuentan y van
// al log con muestreo, sin auditar. Como freno de abuso hay un tope global muy alto de canjes
// fallidos por ventana (PortalTope por PortalVentanaTope, 1000/min): pasado, los canjes fallidos
// de esa ventana responden 429 demasiados_intentos con Retry-After (alarma en el log); un ticket
// válido entra siempre.

// Cookies del portal.
const (
	CookiePortal     = "iris_portal"
	CookiePortalCSRF = "iris_portal_csrf"
	// PrefijoRutasPortal: la cookie del portal solo autentica aquí.
	PrefijoRutasPortal = "/api/v1/portal/"
	// PrefijoRutasEncuestaPublica: la encuesta de satisfacción pública; ninguna cookie autentica.
	PrefijoRutasEncuestaPublica = "/api/v1/encuestas/publica/"
	rutaSesionPortal            = "/api/v1/portal/sesion"
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

// ValidacionCliente es lo que Argos dice de un cliente (Config.ValidarCliente). Validado =
// false: no se pudo preguntar (Motivo dice por qué).
type ValidacionCliente struct {
	Validado bool
	Existe   bool
	// Empresas del cliente: las de sus servicios en Argos (el cliente no tiene empresa; sus
	// servicios sí). SinServicios: Argos dijo, con la lista de todas las empresas, que no tiene
	// ninguno (una lista vacía por el alcance del token no cuenta).
	Empresas     []int64
	SinServicios bool
	Motivo       string
}

// EnEmpresa indica si el cliente es de la empresa: uno de sus servicios lo es. Un cliente sin
// servicios se ve en todas (como en Argos), pero su acceso al portal (y su estado de cuenta) solo
// lo pide quien tiene empresas.todas global (todas).
func EnEmpresa(empresas []int64, sinServicios bool, empresa int64, todas bool) bool {
	return slices.Contains(empresas, empresa) || (sinServicios && todas)
}

// ValidadorCliente lee un cliente en Argos (lo cablea cmd/iris con el conector Argos). Solo
// devuelve error si falla la base.
type ValidadorCliente func(ctx context.Context, clienteID int64) (ValidacionCliente, error)

type entradaEmitirFinal struct {
	EntradaEmitirAccesoPortal
	conocido   bool
	validacion *ValidacionCliente
}

// EmitirAccesoPortal (portal.emitir) en dos fases, sin transacción abierta durante la llamada a
// Argos: un caso corto verifica el permiso y la empresa y mira la instantánea de Argos; si Iris
// no conoce al cliente y hay validador, se lee el cliente en Argos (debe existir y ser de la
// empresa); el segundo caso emite el ticket y audita cliente_validado (false si no se pudo
// validar: conector apagado, Argos caído).
type EmitirAccesoPortal struct {
	cfg    Config
	previo *auditoria.Caso[EntradaEmitirAccesoPortal, bool]
	final  *auditoria.Caso[entradaEmitirFinal, AccesoPortalEmitido]
}

// clienteConocido: si Iris ya conoce al cliente (instantánea de Argos), debe ser de esa empresa.
func clienteConocido(ctx context.Context, q *identidadbd.Queries, in EntradaEmitirAccesoPortal) (bool, error) {
	c, err := q.EmpresasDeClienteArgos(ctx, in.ClienteID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return false, nil
	case err != nil:
		return false, err
	case !EnEmpresa(c.Empresas, c.SinServicios, in.EmpresaID, todasGlobal(ctx)):
		return true, empresaNoCoincide()
	}
	return true, nil
}

func empresaNoCoincide() *nucleo.Problema {
	return nucleo.ReglaNegocio("empresa_no_coincide", "El cliente es de otra empresa según Argos")
}

func nuevoEmitirAccesoPortal(cfg Config) *EmitirAccesoPortal {
	k := &EmitirAccesoPortal{cfg: cfg}
	// La empresa no es la activa de la petición: es la del cliente, que se valida contra las del
	// actor (una integración puede operar en varias sin mandar X-Iris-Empresa).
	k.previo = auditoria.Nuevo(auditoria.Caso[EntradaEmitirAccesoPortal, bool]{
		Accion: "portal.emitir", Permiso: nucleo.PermPortalEmitir, SinEmpresa: true,
		AuditarExito: func(bool) bool { return false },
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaEmitirAccesoPortal) (bool, error) {
			q := identidadbd.New(t)
			t.FijarEmpresa(in.EmpresaID)
			t.Anotar(map[string]any{"cliente_id": in.ClienteID})
			if err := empresaParaEmitir(ctx, q, in.EmpresaID); err != nil {
				return false, err
			}
			return clienteConocido(ctx, q, in)
		},
	})
	k.final = auditoria.Nuevo(auditoria.Caso[entradaEmitirFinal, AccesoPortalEmitido]{
		Accion: "portal.emitir", Permiso: nucleo.PermPortalEmitir, SinEmpresa: true,
		Logica: func(ctx context.Context, t *auditoria.Tx, in entradaEmitirFinal) (AccesoPortalEmitido, error) {
			q := identidadbd.New(t)
			t.FijarEmpresa(in.EmpresaID)
			t.Anotar(map[string]any{"cliente_id": in.ClienteID})
			if err := empresaParaEmitir(ctx, q, in.EmpresaID); err != nil {
				return AccesoPortalEmitido{}, err
			}
			switch v := in.validacion; {
			case in.conocido:
				t.Anotar(map[string]any{"cliente_validado": true, "validacion": "instantanea"})
			case v != nil && v.Validado && !v.Existe:
				return AccesoPortalEmitido{}, nucleo.ReglaNegocio("cliente_no_existe", "Argos no tiene ese cliente")
			case v != nil && v.Validado && !EnEmpresa(v.Empresas, v.SinServicios, in.EmpresaID, todasGlobal(ctx)):
				return AccesoPortalEmitido{}, empresaNoCoincide()
			case v != nil && v.Validado:
				t.Anotar(map[string]any{"cliente_validado": true, "validacion": "argos"})
			case v != nil:
				t.Anotar(map[string]any{"cliente_validado": false, "validacion": v.Motivo})
			default:
				t.Anotar(map[string]any{"cliente_validado": false, "validacion": "sin_validador"})
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
	return k
}

// Ejecutar emite el acceso.
func (k *EmitirAccesoPortal) Ejecutar(ctx context.Context, in EntradaEmitirAccesoPortal) (AccesoPortalEmitido, error) {
	conocido, err := k.previo.Ejecutar(ctx, in)
	if err != nil {
		return AccesoPortalEmitido{}, err
	}
	f := entradaEmitirFinal{EntradaEmitirAccesoPortal: in, conocido: conocido}
	if !conocido && k.cfg.ValidarCliente != nil {
		v, err := k.cfg.ValidarCliente(ctx, in.ClienteID)
		if err != nil {
			return AccesoPortalEmitido{}, err
		}
		f.validacion = &v
	}
	return k.final.Ejecutar(ctx, f)
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
		// Los 401 (ticket inválido) y 429 (tope global) son de anónimos: no dejan fila (se
		// podría inundar la tabla); se cuentan y van al log con muestreo.
		AuditarError: m.auditarRechazoPortal,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaEntrarPortal) (SesionPortal, error) {
			q := identidadbd.New(t)
			t.Anotar(map[string]any{"ip": in.IP, "user_agent": in.UserAgent})
			// El canje se intenta siempre: el tope global nunca bloquea un ticket válido. Pasado el
			// tope, los fallidos responden 429 (freno y alarma) en vez de 401.
			acceso, err := q.TomarAccesoPortal(ctx, Hash256(in.Ticket))
			if errors.Is(err, pgx.ErrNoRows) {
				ahora := time.Now()
				m.topePortal.sumar(ahora, cfg.PortalVentanaTope)
				if hasta, excedido := m.topePortal.excedido(ahora, cfg.PortalTope); excedido {
					return SesionPortal{}, nucleo.DemasiadosIntentos("demasiados_intentos",
						"Demasiados intentos fallidos de entrar al portal; vuelve a intentar en un momento", time.Until(hasta))
				}
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

// topeVentana cuenta eventos en una ventana fija (en memoria, por proceso): el freno global
// de canjes fallidos del portal.
type topeVentana struct {
	mu      sync.Mutex
	fin     time.Time
	cuenta  int64
	avisado bool
}

// sumar cuenta un fallo; abre una ventana nueva si la anterior terminó.
func (v *topeVentana) sumar(ahora time.Time, ventana time.Duration) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if !ahora.Before(v.fin) {
		v.fin, v.cuenta, v.avisado = ahora.Add(ventana), 0, false
	}
	v.cuenta++
}

// excedido indica si la ventana en curso ya superó el tope y hasta cuándo dura.
func (v *topeVentana) excedido(ahora time.Time, tope int64) (time.Time, bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if ahora.Before(v.fin) && v.cuenta > tope {
		if !v.avisado {
			v.avisado = true
			slog.Warn("identidad: tope global de canjes fallidos del portal alcanzado", "tope", tope, "hasta", v.fin)
		}
		return v.fin, true
	}
	return time.Time{}, false
}

// auditarRechazoPortal: los 401 y 429 del canje no se auditan; se cuentan y se registran con
// muestreo (el primero y uno de cada 100).
func (m *Modulo) auditarRechazoPortal(err error) bool {
	p, _ := nucleo.AProblema(err)
	if p.Status != http.StatusUnauthorized && p.Status != http.StatusTooManyRequests {
		return true
	}
	if n := m.rechazosPortal.Add(1); n == 1 || n%100 == 0 {
		slog.Warn("identidad: canjes del portal o del autologin rechazados sin auditar", "codigo", p.Codigo(), "total", n)
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

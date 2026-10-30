package identidad

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
)

// Rutas del portal de soporte (portal.go). Handlers delgados.

const etiquetaPortal = "portal"

// seguridadPortal: la cookie iris_portal (solo en /api/v1/portal/…).
var seguridadPortal = []map[string][]string{{"portal": {}}}

type entradaEntrarPortal struct {
	cliente
	Body EntradaEntrarPortal
}

type salidaSesionPortal struct {
	SetCookie []http.Cookie `header:"Set-Cookie"`
	Body      SesionPortal
}

// registrarPortal publica las rutas del portal (la llama Registrar).
func (m *Modulo) registrarPortal(api huma.API) {
	emitir := operacion("emitirAccesoPortal", http.MethodPost, "/api/v1/portal/tickets-acceso",
		"Emitir un acceso al portal de soporte para un cliente de Argos", nucleo.PermPortalEmitir, http.StatusCreated, http.StatusConflict)
	emitir.Tags = []string{etiquetaPortal}
	emitir.Security = []map[string][]string{{"bearer": {}}}
	emitir.Description += " Solo con token (`Authorization: Bearer`; en `/api/v1/portal/` la sesión del panel no cuenta). Lo llama Argos (token de integración `iris_pat_`, pedido A9) desde su portal del cliente: devuelve `<IRIS_BASE_URL>/soporte/entrar#<ticket>`, válida 60 s y de un solo uso, a la que Argos redirige al cliente. `empresaId` debe ser una empresa en que opera quien emite (403 `empresa_no_permitida`); si Iris ya conoce al cliente (instantánea) o, si no, Argos dice que es de otra empresa, 422 `empresa_no_coincide`; si Argos no lo tiene, 422 `cliente_no_existe`; si no se pudo preguntar (conector apagado o Argos caído), solo la integración o quien tiene `empresas.todas` lo emite (queda `cliente_validado: false` en la auditoría); un usuario del panel recibe 422 `cliente_no_verificado`. " + descNoRepetible
	huma.Register(api, emitir, func(ctx context.Context, in *conClave[EntradaEmitirAccesoPortal]) (*cuerpo[AccesoPortalEmitido], error) {
		v, err := m.EmitirAccesoPortal.Ejecutar(ctx, in.Body)
		return responderSecreto(ctx, v, err)
	})

	entrar := operacion("entrarPortal", http.MethodPost, "/api/v1/portal/sesion", "Entrar al portal con el ticket de acceso", "", http.StatusOK, http.StatusTooManyRequests)
	entrar.Tags = []string{etiquetaPortal}
	entrar.Security = nil
	entrar.Description = "Canjea el ticket de `/soporte/entrar#<ticket>` (60 s, un solo uso; 401 `ticket_invalido`) y abre la sesión del cliente: cookie `iris_portal` (HttpOnly, SameSite=Lax) e `iris_portal_csrf` (para `X-CSRF-Token`), de 2 h. La cookie solo vale en `/api/v1/portal/…`, donde a su vez la sesión del panel no cuenta. Sin límite por IP; freno global de abuso: pasados 1000 canjes fallidos en un minuto (de todos), los fallidos de ese minuto responden 429 `demasiados_intentos` con `Retry-After`; un ticket válido entra siempre."
	huma.Register(api, entrar, func(ctx context.Context, in *entradaEntrarPortal) (*salidaSesionPortal, error) {
		e := in.Body
		e.IP, e.UserAgent = in.ip, in.userAgent
		s, err := m.EntrarPortal.Ejecutar(ctx, e)
		if err != nil {
			return nil, err
		}
		return &salidaSesionPortal{SetCookie: m.cookiesPortal(s.Token, s.CSRFToken, s.ExpiraEn), Body: s}, nil
	})

	ver := operacion("obtenerSesionPortal", http.MethodGet, "/api/v1/portal/sesion", "Sesión del portal: cliente y vencimiento", "", http.StatusOK)
	ver.Tags = []string{etiquetaPortal}
	ver.Security = seguridadPortal
	ver.Errors = append(ver.Errors, http.StatusForbidden)
	ver.Description = "Solo con la sesión del portal (actor cliente); cualquier otra credencial, 403."
	huma.Register(api, ver, func(ctx context.Context, _ *vacio) (*cuerpo[SesionPortalActual], error) {
		return responder(m.ObtenerSesionPortal.Ejecutar(ctx, struct{}{}))
	})

	salir := operacion("cerrarSesionPortal", http.MethodDelete, "/api/v1/portal/sesion", "Cerrar la sesión del portal", "", http.StatusNoContent)
	salir.Tags = []string{etiquetaPortal}
	salir.Security = seguridadPortal
	salir.Errors = append(salir.Errors, http.StatusForbidden)
	salir.Description = "Con `X-CSRF-Token` (valor de la cookie `iris_portal_csrf`)."
	huma.Register(api, salir, func(ctx context.Context, _ *vacio) (*salidaCookies, error) {
		if _, err := m.CerrarSesionPortal.Ejecutar(ctx, struct{}{}); err != nil {
			return nil, err
		}
		return &salidaCookies{SetCookie: m.cookiesPortal("", "", time.Time{})}, nil
	})
}

// cookiesPortal arma las cookies del portal; token vacío = borrarlas.
func (m *Modulo) cookiesPortal(token, csrf string, expira time.Time) []http.Cookie {
	maxAge := 0
	if token == "" {
		expira, maxAge = time.Unix(0, 0), -1
	}
	return []http.Cookie{
		{Name: CookiePortal, Value: token, Path: "/", Expires: expira, MaxAge: maxAge, HttpOnly: true, Secure: m.cfg.CookieSegura, SameSite: http.SameSiteLaxMode},
		{Name: CookiePortalCSRF, Value: csrf, Path: "/", Expires: expira, MaxAge: maxAge, Secure: m.cfg.CookieSegura, SameSite: http.SameSiteLaxMode},
	}
}

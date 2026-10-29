package identidad

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
)

// Rutas del autologin desde Argos (sso.go). Handlers delgados.

const etiquetaSSO = "sso"

type entradaCanjearSSO struct {
	cliente
	Body EntradaCanjearSSO
}

type salidaCanjeSSO struct {
	SetCookie []http.Cookie `header:"Set-Cookie"`
	Body      CanjeSSO
}

type entradaVincularSSO struct {
	nucleo.ConIdempotencia
	Vinculacion string `cookie:"iris_sso_vincular" required:"false" doc:"La deja POST /api/v1/sso/canjear (HttpOnly); el navegador la manda solo"`
}

type salidaVinculoSSO struct {
	SetCookie []http.Cookie `header:"Set-Cookie"`
	Body      VinculoSSOCreado
}

type entradaBorrarVinculoSSO struct {
	nucleo.ConIdempotencia
	ID        int64 `path:"id" minimum:"1"`
	VinculoID int64 `path:"vinculoId" minimum:"1"`
	Body      EntradaBorrarVinculoSSO
}

// registrarSSO publica las rutas del autologin (la llama Registrar).
func (m *Modulo) registrarSSO(api huma.API) {
	emitir := operacion("emitirTicketSSO", http.MethodPost, "/api/v1/integraciones/argosisp/sso/tickets",
		"Emitir un ticket de autologin para un usuario de Argos («Ver en Iris»)", nucleo.PermSSOEmitir, http.StatusCreated, http.StatusConflict)
	emitir.Tags = []string{etiquetaSSO}
	emitir.Security = []map[string][]string{{"bearer": {}}}
	emitir.Description += " Lo llama el servidor de Argos (contrato §1.3 → §1.5) con el `iris_pat_` que Iris le entregó al conectar: cualquier otro token o una sesión, 403. Cuerpo en snake_case: `{ usuario: { id_externo, correo?, nombre? }, destino: { tipo: ticket|orden|prospecto, id_externo } }` (ids de Iris). Devuelve `{ url: <IRIS_BASE_URL>/sso/entrar#<ticket>, expira_en }`: 60 s, un solo uso, atado al conector. Sin conector activo con Argos, 409 `conector_apagado`. No comprueba que el destino exista: al entrar, el usuario ve el recurso con sus propios permisos (o el error de acceso de siempre). " + descNoRepetible
	huma.Register(api, emitir, func(ctx context.Context, in *conClave[EntradaEmitirTicketSSO]) (*cuerpo[TicketSSOEmitido], error) {
		v, err := m.EmitirTicketSSO.Ejecutar(ctx, in.Body)
		return responderSecreto(ctx, v, err)
	})

	canjear := operacion("canjearSSO", http.MethodPost, rutaCanjearSSO, "Entrar con el ticket de autologin de Argos", "", http.StatusOK, http.StatusForbidden, http.StatusConflict, http.StatusTooManyRequests)
	canjear.Tags = []string{etiquetaSSO}
	canjear.Security = nil
	canjear.Description = "Lo llama la página `/sso/entrar#<ticket>` (sin CSRF, como el login). Ticket vencido, usado o inexistente: 401 `ticket_invalido`; conector apagado: 409 `conector_apagado`. Con vínculo: abre la sesión del usuario de Iris (cookies `iris_sesion` e `iris_csrf`, como `POST /api/v1/sesion`) y responde `estado: sesion`, o `estado: mfa` si tiene MFA (falta `POST /api/v1/sesion/mfa`); cuenta vinculada desactivada: 403. Sin vínculo: responde `estado: vincular` con `externo` (id, correo y nombre en Argos, solo para mostrar) y deja la cookie `iris_sso_vincular` (HttpOnly, solo `/api/v1/sso`, 10 min): el usuario inicia sesión normalmente y luego llama `POST /api/v1/sso/vincular`. `destino.ruta` es la ruta del panel a la que ir después (`/tickets/{id}`, `/ordenes/{id}` o `/comercial/{id}`): siempre interna. Freno global como el portal: pasados 1000 canjes fallidos en un minuto, 429 `demasiados_intentos`."
	huma.Register(api, canjear, func(ctx context.Context, in *entradaCanjearSSO) (*salidaCanjeSSO, error) {
		e := in.Body
		e.IP, e.UserAgent = in.ip, in.userAgent
		c, err := m.CanjearSSO.Ejecutar(ctx, e)
		if err != nil {
			return nil, err
		}
		var cookies []http.Cookie
		if c.Sesion != nil {
			cookies = append(cookies, m.cookiesSesion(*c.Sesion)...)
		}
		if c.vinculacion != "" {
			cookies = append(cookies, m.cookieVinculacion(c.vinculacion, *c.VinculacionExpiraEn))
		}
		return &salidaCanjeSSO{SetCookie: cookies, Body: c}, nil
	})

	vincular := operacion("vincularSSO", http.MethodPost, RutasSSO+"/vincular", "Vincular tu usuario con el de Argos del último autologin", "", http.StatusOK, http.StatusForbidden, http.StatusConflict)
	vincular.Tags = []string{etiquetaSSO}
	vincular.Security = []map[string][]string{{"sesion": {}}}
	vincular.Description = "Con la sesión completa del panel (después del MFA) y `X-CSRF-Token`, y la cookie `iris_sso_vincular` del canje. La sesión debe haberse abierto después del canje (422 `reautenticacion_requerida`: una sesión que ya estaba abierta en el navegador no vincula). Sin vinculación pendiente vigente, 422 `vinculacion_invalida`. 409 `vinculo_existente` si tu usuario ya está vinculado con otra cuenta de ese Argos, 409 `cuenta_externa_vinculada` si esa cuenta de Argos ya es de otro usuario, 409 `conector_apagado`. Nunca se vincula por correo. Borra la cookie y responde el vínculo y el destino. Emite `usuario.sso_vinculado`."
	huma.Register(api, vincular, func(ctx context.Context, in *entradaVincularSSO) (*salidaVinculoSSO, error) {
		v, err := m.VincularSSO.Ejecutar(ctx, EntradaVincularSSO{Vinculacion: in.Vinculacion})
		if err != nil {
			return nil, err
		}
		return &salidaVinculoSSO{SetCookie: []http.Cookie{m.cookieVinculacion("", time.Time{})}, Body: v}, nil
	})

	ver := operacion("listarVinculosSSO", http.MethodGet, "/api/v1/usuarios/{id}/vinculos-sso", "Vínculos de autologin de un usuario", "", http.StatusOK, http.StatusForbidden, http.StatusNotFound)
	ver.Tags = []string{etiquetaSSO}
	ver.Description = "Los propios, sin permiso; los de otro usuario, con `usuarios.ver` y el mismo alcance que `GET /usuarios/{id}` (fuera de él, 404)."
	huma.Register(api, ver, func(ctx context.Context, in *EntradaVinculosSSO) (*cuerpo[nucleo.Pagina[VinculoSSO]], error) {
		return responder(m.ListarVinculosSSO.Ejecutar(ctx, *in))
	})

	borrar := operacion("borrarVinculoSSO", http.MethodPost, "/api/v1/usuarios/{id}/vinculos-sso/{vinculoId}/borrar", "Quitar un vínculo de autologin", "", http.StatusOK, http.StatusForbidden, http.StatusNotFound)
	borrar.Tags = []string{etiquetaSSO}
	borrar.Description = "Los propios, sin permiso; los de otro usuario, con `usuarios.editar` y la regla de la administración de usuarios (" + descAdminPorEmpresa + " Además, sus permisos no pueden superar los tuyos). Con el vínculo quitado, el próximo «Ver en Iris» vuelve a pedir iniciar sesión y vincular. Emite `usuario.sso_desvinculado`."
	huma.Register(api, borrar, func(ctx context.Context, in *entradaBorrarVinculoSSO) (*cuerpo[VinculoSSO], error) {
		e := in.Body
		e.UsuarioID, e.VinculoID = in.ID, in.VinculoID
		return responder(m.BorrarVinculoSSO.Ejecutar(ctx, e))
	})
}

// cookieVinculacion arma la cookie de la vinculación pendiente; valor vacío = borrarla.
func (m *Modulo) cookieVinculacion(valor string, expira time.Time) http.Cookie {
	maxAge := 0
	if valor == "" {
		expira, maxAge = time.Unix(0, 0), -1
	}
	return http.Cookie{Name: CookieVinculacionSSO, Value: valor, Path: RutasSSO, Expires: expira, MaxAge: maxAge,
		HttpOnly: true, Secure: m.cfg.CookieSegura, SameSite: http.SameSiteLaxMode}
}

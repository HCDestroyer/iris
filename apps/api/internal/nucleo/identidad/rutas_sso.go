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
	Body        EntradaVincularSSO
}

type entradaCrearVinculoSSO struct {
	nucleo.ConIdempotencia
	ID   int64 `path:"id" minimum:"1"`
	Body EntradaCrearVinculoSSO
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
	canjear.Description = "Lo llama la página `/sso/entrar#<ticket>` (sin CSRF, como el login). Ticket vencido, usado o inexistente: 401 `ticket_invalido`; conector apagado: 409 `conector_apagado`. Con vínculo: abre una sesión de origen `sso` del usuario de Iris (cookies `iris_sesion` e `iris_csrf`; dura a lo más 8 h y las acciones sensibles piden `POST /api/v1/sesion/reautenticar`) y responde `estado: sesion`, o `estado: mfa` si tiene MFA (falta `POST /api/v1/sesion/mfa`). Barreras del login: cuenta inactiva, de integración o sin contraseña, 403; bloqueada por fallos, 429 `demasiados_intentos`; con permisos de administración y sin MFA, 403 `sso_requiere_mfa`. Si el navegador ya tiene la sesión completa de **otro** usuario, responde `estado: confirmar_cambio` sin consumir el ticket: repetir con `confirmar: true` cierra esa sesión y entra; si es del mismo usuario, la reutiliza (`estado: sesion`, `sesionReutilizada: true`, sin `sesion`). Sin vínculo: `estado: vincular` con `externo` (id, correo y nombre en Argos) y la cookie `iris_sso_vincular` (HttpOnly, solo `/api/v1/sso`, 10 min). `destino.ruta` es la ruta del panel (`/tickets/{id}`, `/ordenes/{id}` o `/comercial/{id}`): siempre interna. Freno global propio: pasados 1000 canjes fallidos en un minuto, 429 `demasiados_canjes`."
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
	vincular.Description = "Solo tras la confirmación explícita del usuario (nunca automático): con la sesión completa del panel (después del MFA), `X-CSRF-Token`, la cookie `iris_sso_vincular` del canje y `{ id_externo }` igual al `externo.idExterno` que se le mostró (si no, o sin vinculación pendiente vigente, 422 `vinculacion_invalida`). La sesión debe haberse abierto después del canje (422 `sesion_anterior_al_canje`). Además, el correo de Argos debe ser el de la cuenta de Iris (sin distinguir mayúsculas); si no, 409 `vinculo_requiere_admin` (lo vincula un administrador con `POST /api/v1/usuarios/{id}/vinculos-sso`). Nunca se vincula solo por correo. 409 `vinculo_existente` (tu usuario ya tiene un vínculo con ese Argos), `cuenta_externa_vinculada` o `conector_apagado`. Borra la cookie y responde el vínculo y el destino. Emite `usuario.sso_vinculado`."
	huma.Register(api, vincular, func(ctx context.Context, in *entradaVincularSSO) (*salidaVinculoSSO, error) {
		e := in.Body
		e.Vinculacion = in.Vinculacion
		v, err := m.VincularSSO.Ejecutar(ctx, e)
		if err != nil {
			return nil, err
		}
		return &salidaVinculoSSO{SetCookie: []http.Cookie{m.cookieVinculacion("", time.Time{})}, Body: v}, nil
	})

	crear := operacion("crearVinculoSSO", http.MethodPost, "/api/v1/usuarios/{id}/vinculos-sso", "Vincular un usuario con una cuenta de Argos (administrador)", nucleo.PermUsuariosEditar, http.StatusCreated, http.StatusNotFound, http.StatusConflict)
	crear.Tags = []string{etiquetaSSO}
	crear.Security = []map[string][]string{{"sesion": {}}}
	crear.Description += " Solo con la sesión del panel. Para cuando el correo de Argos no es el de la cuenta (409 `vinculo_requiere_admin` en `POST /sso/vincular`). Cuerpo `{ conector_id, id_externo }` (el conector de Argos; si no, 422 `conector_invalido`). Mismo alcance que editar el usuario (" + descAdminPorEmpresa + " Además, sus permisos no pueden superar los tuyos). 409 `vinculo_existente` / `cuenta_externa_vinculada`. Emite `usuario.sso_vinculado` con `origen: admin`."
	huma.Register(api, crear, func(ctx context.Context, in *entradaCrearVinculoSSO) (*cuerpo[VinculoSSO], error) {
		e := in.Body
		e.UsuarioID = in.ID
		return responder(m.CrearVinculoSSO.Ejecutar(ctx, e))
	})

	ver := operacion("listarVinculosSSO", http.MethodGet, "/api/v1/usuarios/{id}/vinculos-sso", "Vínculos de autologin de un usuario", "", http.StatusOK, http.StatusForbidden, http.StatusNotFound)
	ver.Tags = []string{etiquetaSSO}
	ver.Description = "Los propios, sin permiso; los de otro usuario, con `usuarios.ver` y el mismo alcance que `GET /usuarios/{id}` (fuera de él, 404)."
	huma.Register(api, ver, func(ctx context.Context, in *EntradaVinculosSSO) (*cuerpo[nucleo.Pagina[VinculoSSO]], error) {
		return responder(m.ListarVinculosSSO.Ejecutar(ctx, *in))
	})

	borrar := operacion("borrarVinculoSSO", http.MethodPost, "/api/v1/usuarios/{id}/vinculos-sso/{vinculoId}/borrar", "Quitar un vínculo de autologin", "", http.StatusOK, http.StatusForbidden, http.StatusNotFound)
	borrar.Tags = []string{etiquetaSSO}
	borrar.Security = []map[string][]string{{"sesion": {}}}
	borrar.Description = "Solo con la sesión del panel (no con un token). Los propios, sin permiso; los de otro usuario, con `usuarios.editar` y la regla de la administración de usuarios (" + descAdminPorEmpresa + " Además, sus permisos no pueden superar los tuyos). Con el vínculo quitado, el próximo «Ver en Iris» vuelve a pedir iniciar sesión y vincular. Emite `usuario.sso_desvinculado`."
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

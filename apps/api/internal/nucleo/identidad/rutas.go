package identidad

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
)

// Handlers delgados: leen la petición, llaman al caso de uso y arman la respuesta. Ni una
// regla de negocio aquí.

const etiqueta = "identidad"

// Seguridad de las operaciones que exigen credenciales: cookie de sesión o Bearer.
var seguridad = []map[string][]string{{"sesion": {}}, {"bearer": {}}}

func operacion(id, metodo, ruta, resumen string, permiso nucleo.Permiso, status int, errores ...int) huma.Operation {
	op := huma.Operation{
		OperationID:   id,
		Method:        metodo,
		Path:          ruta,
		Summary:       resumen,
		Tags:          []string{etiqueta},
		DefaultStatus: status,
		Errors:        append([]int{http.StatusUnauthorized, http.StatusUnprocessableEntity}, errores...),
		Security:      seguridad,
	}
	if permiso != "" {
		op.Extensions = map[string]any{"x-permiso": string(permiso)}
		op.Errors = append(op.Errors, http.StatusForbidden)
		op.Description = "Permiso: `" + string(permiso) + "`."
	}
	return op
}

// Entradas y salidas HTTP.

// cliente es la IP y el navegador de quien abre una sesión. Va embebido (sin exportar) en las
// entradas del login, el MFA y el canje del portal. Huma no lee las etiquetas header:"…" de un
// struct embebido no exportado (el User-Agent llegaba siempre vacío), pero sí llama a su
// Resolve: los dos se leen ahí. No es un parámetro del OpenAPI: el navegador lo manda solo.
type cliente struct {
	userAgent string
	ip        string
}

// Resolve toma la IP remota y el User-Agent (huma.Resolver).
func (c *cliente) Resolve(ctx huma.Context) []error {
	// RemoteAddr ya viene corregido por el servidor con los proxies confiables.
	c.ip = nucleo.HostDe(ctx.RemoteAddr())
	c.userAgent = ctx.Header("User-Agent")
	return nil
}

type entradaLogin struct {
	cliente
	Body EntradaIniciarSesion
}

type entradaMFA struct {
	cliente
	Body EntradaVerificarMFA
}

type salidaSesion struct {
	SetCookie []http.Cookie `header:"Set-Cookie"`
	Body      SesionIniciada
}

type salidaCookies struct {
	SetCookie []http.Cookie `header:"Set-Cookie"`
}

type cuerpo[T any] struct {
	Body T
}

type conClave[T any] struct {
	nucleo.ConIdempotencia
	Body T
}

type conID[T any] struct {
	nucleo.ConIdempotencia
	ID   int64 `path:"id" minimum:"1"`
	Body T
}

type vacio struct{}

type soloID struct {
	nucleo.ConIdempotencia
	ID int64 `path:"id" minimum:"1"`
}

type soloClave struct {
	nucleo.ConIdempotencia
}

// Registrar publica las rutas de identidad en la API.
func (m *Modulo) Registrar(api huma.API) {
	// Sesión ----------------------------------------------------------------------------
	login := operacion("iniciarSesion", http.MethodPost, "/api/v1/sesion", "Iniciar sesión", "", http.StatusOK, http.StatusTooManyRequests)
	login.Security = nil
	login.Description = "Valida correo y contraseña y abre la sesión: cookie `iris_sesion` (HttpOnly, SameSite=Lax) y `iris_csrf` (para la cabecera `X-CSRF-Token`). Con MFA activo la sesión queda pendiente hasta `POST /api/v1/sesion/mfa`. Tras 10 fallos seguidos con un correo (exista o no) en 15 min, ese correo queda bloqueado 15 min: 429 `demasiados_intentos` con `Retry-After`, aun con la contraseña correcta. Entrar bien reinicia la cuenta."
	huma.Register(api, login, func(ctx context.Context, in *entradaLogin) (*salidaSesion, error) {
		e := in.Body
		e.IP, e.UserAgent = in.ip, in.userAgent
		s, err := m.IniciarSesion.Ejecutar(ctx, e)
		if err != nil {
			return nil, err
		}
		return &salidaSesion{SetCookie: m.cookiesSesion(s), Body: s}, nil
	})

	mfa := operacion("verificarMFA", http.MethodPost, "/api/v1/sesion/mfa", "Completar el inicio de sesión con el código TOTP", "", http.StatusOK)
	mfa.Description = "Con la sesión pendiente de MFA (cookie) y `X-CSRF-Token`. Cambia la sesión por una nueva. Al 5.º código inválido (o ya usado) la sesión pendiente se revoca (401 `mfa_bloqueado`) y hay que volver a iniciar sesión."
	huma.Register(api, mfa, func(ctx context.Context, in *entradaMFA) (*salidaSesion, error) {
		e := in.Body
		e.IP, e.UserAgent = in.ip, in.userAgent
		s, err := m.VerificarMFA.Ejecutar(ctx, e)
		if err != nil {
			return nil, err
		}
		return &salidaSesion{SetCookie: m.cookiesSesion(s), Body: s}, nil
	})

	reaut := operacion("reautenticar", http.MethodPost, "/api/v1/sesion/reautenticar", "Confirmar la contraseña dentro de la sesión", "", http.StatusOK, http.StatusForbidden, http.StatusTooManyRequests)
	reaut.Security = []map[string][]string{{"sesion": {}}}
	reaut.Description = "Con la sesión del panel y `X-CSRF-Token`. Una sesión abierta por el autologin desde Argos (origen `sso`) debe confirmar la contraseña antes de las acciones sensibles (las de los permisos de administración de `nucleo.PermisosSensibles`, el MFA propio y los vínculos de autologin), que si no responden 403 `reautenticacion_requerida`; vale 15 min. Cuenta los fallos y bloquea como el login (401 `credenciales_invalidas`, 429 `demasiados_intentos`); desde una sesión sso con un contador propio, que no bloquea el login con contraseña."
	huma.Register(api, reaut, func(ctx context.Context, in *conClave[EntradaReautenticar]) (*cuerpo[Reautenticado], error) {
		return responder(m.Reautenticar.Ejecutar(ctx, in.Body))
	})

	huma.Register(api, operacion("cerrarSesion", http.MethodDelete, "/api/v1/sesion", "Cerrar la sesión", "", http.StatusNoContent),
		func(ctx context.Context, _ *vacio) (*salidaCookies, error) {
			if _, err := m.CerrarSesion.Ejecutar(ctx, struct{}{}); err != nil {
				return nil, err
			}
			return &salidaCookies{SetCookie: m.cookiesBorradas()}, nil
		})

	yo := operacion("obtenerYo", http.MethodGet, "/api/v1/yo", "Usuario autenticado, permisos, alcance y empresas", "", http.StatusOK)
	yo.Description = "Incluye `empresas` (para el selector), `empresaActiva` y `modoEmpresa`. La empresa activa se elige con la cabecera `X-Iris-Empresa: <id>` en cada petición; con una sola empresa disponible es esa por defecto."
	huma.Register(api, yo,
		func(ctx context.Context, _ *vacio) (*cuerpo[Yo], error) {
			return responder(m.ObtenerYo.Ejecutar(ctx, struct{}{}))
		})

	configMFA := operacion("configurarMFA", http.MethodPost, "/api/v1/yo/mfa", "Generar el secreto TOTP del usuario", "", http.StatusOK, http.StatusConflict)
	configMFA.Description = descNoRepetible
	huma.Register(api, configMFA,
		func(ctx context.Context, _ *soloClave) (*cuerpo[MFAConfigurado], error) {
			v, err := m.ConfigurarMFA.Ejecutar(ctx, struct{}{})
			return responderSecreto(ctx, v, err)
		})

	huma.Register(api, operacion("activarMFA", http.MethodPost, "/api/v1/yo/mfa/activar", "Activar el MFA con un código válido", "", http.StatusOK),
		func(ctx context.Context, in *conClave[EntradaActivarMFA]) (*cuerpo[Usuario], error) {
			return responder(m.ActivarMFA.Ejecutar(ctx, in.Body))
		})

	cambiar := operacion("cambiarContrasenaPropia", http.MethodPost, RutaCambiarContrasena, "Cambiar la contraseña propia", "", http.StatusOK, http.StatusTooManyRequests)
	cambiar.Description = "Solo con sesión del panel. Exige la contraseña actual; la nueva debe tener 12 caracteres o más y ser distinta. Cierra las demás sesiones del usuario (la actual sigue). Límite como el login: tras 10 contraseñas actuales incorrectas seguidas (15 min) responde 429 `demasiados_intentos` con `Retry-After`, cierra la sesión y bloquea el cambio 15 min. Con una contraseña temporal (`cambioContrasenaRequerido`), es lo único que la sesión puede hacer además de `GET /yo` y cerrar sesión: el resto responde 403 `cambio_contrasena_requerido`."
	huma.Register(api, cambiar,
		func(ctx context.Context, in *conClave[EntradaCambiarContrasena]) (*cuerpo[Usuario], error) {
			return responder(m.CambiarContrasena.Ejecutar(ctx, in.Body))
		})

	// Usuarios --------------------------------------------------------------------------
	listarUsuarios := operacion("listarUsuarios", http.MethodGet, "/api/v1/usuarios", "Listar usuarios", nucleo.PermUsuariosVer, http.StatusOK)
	listarUsuarios.Description += " " + descPorEmpresa
	huma.Register(api, listarUsuarios,
		func(ctx context.Context, in *EntradaListarUsuarios) (*cuerpo[nucleo.Pagina[Usuario]], error) {
			return responder(m.ListarUsuarios.Ejecutar(ctx, *in))
		})

	verUsuario := operacion("verUsuario", http.MethodGet, "/api/v1/usuarios/{id}", "Ver un usuario", nucleo.PermUsuariosVer, http.StatusOK, http.StatusNotFound)
	verUsuario.Description += " Mismo alcance que la lista: " + descPorEmpresa + " Fuera de eso, 404 como un id inexistente."
	huma.Register(api, verUsuario,
		func(ctx context.Context, in *EntradaVerUsuario) (*cuerpo[Usuario], error) {
			return responder(m.VerUsuario.Ejecutar(ctx, *in))
		})

	crearUsuario := operacion("crearUsuario", http.MethodPost, "/api/v1/usuarios", "Crear un usuario", nucleo.PermUsuariosCrear, http.StatusCreated, http.StatusConflict)
	crearUsuario.Description += " Una persona queda con su contraseña como temporal (`cambioContrasenaRequerido`): al entrar debe cambiarla (`POST /api/v1/yo/contrasena`) antes de cualquier otra cosa. Un usuario de tipo `integracion` exige `empresas.todas` e `integraciones.conectar` sin límite de alcance (403 `integracion_no_permitida`) y solo admite su rol de contrato (`integracion_argos`, `integracion_horus`; si no, 422). 409 `correo_en_uso`."
	huma.Register(api, crearUsuario,
		func(ctx context.Context, in *conClave[EntradaCrearUsuario]) (*cuerpo[Usuario], error) {
			return responder(m.CrearUsuario.Ejecutar(ctx, in.Body))
		})

	editarUsuario := operacion("editarUsuario", http.MethodPost, "/api/v1/usuarios/{id}/editar", "Editar un usuario", nucleo.PermUsuariosEditar, http.StatusOK, http.StatusNotFound, http.StatusConflict)
	editarUsuario.Description += " Una contraseña puesta a otro usuario queda temporal (`cambioContrasenaRequerido`) y cierra sus sesiones; cambiarle el correo también cierra sus sesiones y borra sus vínculos de autologin. Cambiar el nombre, el correo o la contraseña de otro exige superarlo (403). Cambiar el correo propio exige re-autenticarse (403 `reautenticacion_requerida`). " + descAdminPorEmpresa + " Un usuario fuera de tu alcance de empresas responde 404, como si no existiera (no se revela su versión)."
	huma.Register(api, editarUsuario,
		func(ctx context.Context, in *conID[EntradaEditarUsuario]) (*cuerpo[Usuario], error) {
			e := in.Body
			e.ID = in.ID
			return responder(m.EditarUsuario.Ejecutar(ctx, e))
		})

	desactivarUsuario := operacion("desactivarUsuario", http.MethodPost, "/api/v1/usuarios/{id}/desactivar", "Desactivar un usuario", nucleo.PermUsuariosDesactivar, http.StatusOK, http.StatusNotFound, http.StatusConflict)
	desactivarUsuario.Description += " " + descAdminPorEmpresa + " Un usuario fuera de tu alcance de empresas responde 404, como si no existiera (no se revela su versión)."
	huma.Register(api, desactivarUsuario,
		func(ctx context.Context, in *conID[EntradaDesactivarUsuario]) (*cuerpo[Usuario], error) {
			e := in.Body
			e.ID = in.ID
			return responder(m.DesactivarUsuario.Ejecutar(ctx, e))
		})

	reiniciarMFA := operacion("reiniciarMFAUsuario", http.MethodPost, "/api/v1/usuarios/{id}/reiniciar-mfa", "Reiniciar el MFA de otro usuario", nucleo.PermUsuariosReiniciarMFA, http.StatusOK, http.StatusNotFound, http.StatusConflict)
	reiniciarMFA.Description += " Borra su secreto TOTP (activo o a medio configurar), reemplaza su contraseña por `contrasenaTemporal` (que el admin le entrega fuera de banda), cierra sus sesiones y, salvo `revocarTokens: false`, revoca sus tokens (`token.revocado` por cada uno). Al entrar con la temporal, el usuario debe cambiarla (`POST /api/v1/yo/contrasena`) antes de cualquier otra cosa, y luego configura el MFA. Un usuario fuera de tu alcance de empresas responde 404, como si no existiera. No sirve para el propio usuario (422 `reinicio_mfa_propio`); sin MFA, 422 `mfa_no_configurado`. Responde el `Usuario` actualizado con `sesionesCerradas` y `tokensRevocados` (los mismos números del evento). Emite `usuario.mfa_reiniciado`. " + descAdminPorEmpresa
	huma.Register(api, reiniciarMFA,
		func(ctx context.Context, in *conID[EntradaReiniciarMFA]) (*cuerpo[MFAReiniciado], error) {
			e := in.Body
			e.ID = in.ID
			return responder(m.ReiniciarMFA.Ejecutar(ctx, e))
		})

	// Empresas y membresía --------------------------------------------------------------
	huma.Register(api, operacion("listarEmpresas", http.MethodGet, "/api/v1/empresas", "Listar las empresas registradas", nucleo.PermEmpresasVer, http.StatusOK),
		func(ctx context.Context, in *nucleo.ParamsPagina) (*cuerpo[nucleo.Pagina[Empresa]], error) {
			return responder(m.ListarEmpresas.Ejecutar(ctx, *in))
		})

	crearEmpresa := operacion("crearEmpresa", http.MethodPost, "/api/v1/empresas", "Registrar una empresa de Argos", nucleo.PermEmpresasCrear, http.StatusCreated, http.StatusConflict)
	crearEmpresa.Description += " Para cuando el conector no la trajo (el canje con Argos registra las empresas que cubre su token). 409 si el id ya existe."
	huma.Register(api, crearEmpresa, func(ctx context.Context, in *conClave[EntradaCrearEmpresa]) (*cuerpo[Empresa], error) {
		return responder(m.CrearEmpresa.Ejecutar(ctx, in.Body))
	})

	huma.Register(api, operacion("editarEmpresa", http.MethodPost, "/api/v1/empresas/{id}/editar", "Editar nombre y NIT de una empresa", nucleo.PermEmpresasEditar, http.StatusOK, http.StatusNotFound, http.StatusConflict),
		func(ctx context.Context, in *conID[EntradaEditarEmpresa]) (*cuerpo[Empresa], error) {
			e := in.Body
			e.ID = in.ID
			return responder(m.EditarEmpresa.Ejecutar(ctx, e))
		})

	asignar := operacion("asignarEmpresaUsuario", http.MethodPost, "/api/v1/usuarios/{id}/asignar-empresa", "Asignar una empresa a un usuario", nucleo.PermUsuariosAsignarEmpresa, http.StatusOK, http.StatusNotFound)
	asignar.Description += " Solo empresas en que operas tú y, sin `empresas.todas` global, solo a usuarios cuyas empresas son todas tuyas (403; un usuario sin empresas solo lo administra un admin global). 422 `empresa_ya_asignada`. Los tokens del usuario operan en las mismas empresas."
	huma.Register(api, asignar, func(ctx context.Context, in *conID[EntradaEmpresaUsuario]) (*cuerpo[Usuario], error) {
		e := in.Body
		e.UsuarioID = in.ID
		return responder(m.AsignarEmpresaUsuario.Ejecutar(ctx, e))
	})

	quitar := operacion("quitarEmpresaUsuario", http.MethodPost, "/api/v1/usuarios/{id}/quitar-empresa", "Quitar una empresa a un usuario", nucleo.PermUsuariosQuitarEmpresa, http.StatusOK, http.StatusNotFound)
	quitar.Description += " Solo empresas en que operas tú y usuarios cuyos permisos tienes sin límite de alcance y cuyas empresas son todas tuyas (403). 422 `empresa_no_asignada`."
	huma.Register(api, quitar, func(ctx context.Context, in *conID[EntradaEmpresaUsuario]) (*cuerpo[Usuario], error) {
		e := in.Body
		e.UsuarioID = in.ID
		return responder(m.QuitarEmpresaUsuario.Ejecutar(ctx, e))
	})

	adoptar := operacion("adoptarSinEmpresa", http.MethodPost, "/api/v1/empresas/{id}/adoptar-sin-empresa", "Asignar a una empresa los datos guardados sin empresa", nucleo.PermEmpresasAdoptar, http.StatusOK, http.StatusNotFound)
	adoptar.Description += " Solo admin global (además, `empresas.todas` sin límite; 403 si no). Al registrar la primera empresa, los datos operativos guardados en modo sin conector (`empresa_id` null) dejan de verse; esta acción los asigna, en una transacción, a la empresa `{id}` en cada tabla operativa registrada. Emite `empresa.datos_adoptados`. `GET /yo` avisa con `hayDatosSinEmpresa`."
	huma.Register(api, adoptar, func(ctx context.Context, in *soloID) (*cuerpo[DatosAdoptados], error) {
		return responder(m.AdoptarSinEmpresa.Ejecutar(ctx, EntradaAdoptarSinEmpresa{ID: in.ID}))
	})

	// Roles y permisos ------------------------------------------------------------------
	huma.Register(api, operacion("listarRoles", http.MethodGet, "/api/v1/roles", "Listar roles con sus permisos", nucleo.PermRolesVer, http.StatusOK),
		func(ctx context.Context, in *nucleo.ParamsPagina) (*cuerpo[nucleo.Pagina[Rol]], error) {
			return responder(m.ListarRoles.Ejecutar(ctx, *in))
		})

	huma.Register(api, operacion("listarPermisos", http.MethodGet, "/api/v1/permisos", "Catálogo de permisos", nucleo.PermPermisosVer, http.StatusOK),
		func(ctx context.Context, _ *vacio) (*cuerpo[nucleo.Pagina[PermisoInfo]], error) {
			return responder(m.ListarPermisos.Ejecutar(ctx, struct{}{}))
		})

	// Tokens ----------------------------------------------------------------------------
	huma.Register(api, operacion("listarTokens", http.MethodGet, "/api/v1/tokens", "Listar tokens de API", nucleo.PermTokensVer, http.StatusOK),
		func(ctx context.Context, in *EntradaListarTokens) (*cuerpo[nucleo.Pagina[TokenAPI]], error) {
			return responder(m.ListarTokens.Ejecutar(ctx, *in))
		})

	crearToken := operacion("crearToken", http.MethodPost, "/api/v1/tokens", "Crear un token de API", nucleo.PermTokensCrear, http.StatusCreated, http.StatusNotFound, http.StatusConflict)
	crearToken.Description += " Para otro usuario (`usuarioId`), solo si es de tipo integración (422 `token_para_persona`) y si tienes sin límite de alcance cada permiso suyo. Desde un token, el nuevo no puede tener más permisos que el que lo crea. " + descNoRepetible
	huma.Register(api, crearToken,
		func(ctx context.Context, in *conClave[EntradaCrearToken]) (*cuerpo[TokenCreado], error) {
			v, err := m.CrearToken.Ejecutar(ctx, in.Body)
			return responderSecreto(ctx, v, err)
		})

	huma.Register(api, operacion("revocarToken", http.MethodPost, "/api/v1/tokens/{id}/revocar", "Revocar un token de API", nucleo.PermTokensRevocar, http.StatusOK, http.StatusNotFound),
		func(ctx context.Context, in *conID[EntradaRevocarToken]) (*cuerpo[TokenAPI], error) {
			e := in.Body
			e.ID = in.ID
			return responder(m.RevocarToken.Ejecutar(ctx, e))
		})

	// Accesos, actividad de la app de campo y roles propios ------------------------------
	m.registrarAccesosYRoles(api)

	// Portal de soporte (actor cliente) -------------------------------------------------
	m.registrarPortal(api)

	// Autologin desde Argos -------------------------------------------------------------
	m.registrarSSO(api)
}

// descAdminPorEmpresa y descPorEmpresa documentan la regla de empresas en la administración.
const (
	descAdminPorEmpresa = "Sin `empresas.todas` global, solo usuarios cuyas empresas son todas tuyas; uno sin empresas, o con `empresas.todas`, solo lo administra un admin global (403)."
	descPorEmpresa      = "Sin `empresas.todas` global, solo tú y los usuarios que comparten alguna empresa contigo."
)

// descNoRepetible documenta las operaciones cuya respuesta lleva un secreto.
const descNoRepetible = "La respuesta lleva un secreto que no se guarda para `Idempotency-Key`: repetir la clave responde 409 `idempotencia_no_repetible`."

// responderSecreto es responder para una respuesta que lleva un secreto: si salió bien, la
// marca como no repetible (nucleo.NoRepetible) para que no quede en nucleo.idempotencia.
func responderSecreto[T any](ctx context.Context, v T, err error) (*cuerpo[T], error) {
	if err == nil {
		nucleo.NoRepetible(ctx)
	}
	return responder(v, err)
}

func responder[T any](v T, err error) (*cuerpo[T], error) {
	if err != nil {
		return nil, err
	}
	return &cuerpo[T]{Body: v}, nil
}

func (m *Modulo) cookiesSesion(s SesionIniciada) []http.Cookie {
	return []http.Cookie{
		{Name: CookieSesion, Value: s.Token, Path: "/", Expires: s.ExpiraEn, HttpOnly: true, Secure: m.cfg.CookieSegura, SameSite: http.SameSiteLaxMode},
		{Name: CookieCSRF, Value: s.CSRFToken, Path: "/", Expires: s.ExpiraEn, Secure: m.cfg.CookieSegura, SameSite: http.SameSiteLaxMode},
	}
}

func (m *Modulo) cookiesBorradas() []http.Cookie {
	pasado := time.Unix(0, 0)
	return []http.Cookie{
		{Name: CookieSesion, Value: "", Path: "/", Expires: pasado, MaxAge: -1, HttpOnly: true, Secure: m.cfg.CookieSegura, SameSite: http.SameSiteLaxMode},
		{Name: CookieCSRF, Value: "", Path: "/", Expires: pasado, MaxAge: -1, Secure: m.cfg.CookieSegura, SameSite: http.SameSiteLaxMode},
	}
}

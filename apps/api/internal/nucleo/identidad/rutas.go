package identidad

import (
	"context"
	"net"
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

type cliente struct {
	UserAgent string `header:"User-Agent"`
	ip        string
}

// Resolve toma la IP remota (huma.Resolver).
func (c *cliente) Resolve(ctx huma.Context) []error {
	host, _, err := net.SplitHostPort(ctx.RemoteAddr())
	if err != nil {
		host = ctx.RemoteAddr()
	}
	c.ip = host
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

// Registrar publica las rutas de identidad en la API.
func (m *Modulo) Registrar(api huma.API) {
	// Sesión ----------------------------------------------------------------------------
	login := operacion("iniciarSesion", http.MethodPost, "/api/v1/sesion", "Iniciar sesión", "", http.StatusOK, http.StatusTooManyRequests)
	login.Security = nil
	login.Description = "Valida correo y contraseña y abre la sesión: cookie `iris_sesion` (HttpOnly, SameSite=Lax) y `iris_csrf` (para la cabecera `X-CSRF-Token`). Con MFA activo la sesión queda pendiente hasta `POST /api/v1/sesion/mfa`."
	huma.Register(api, login, func(ctx context.Context, in *entradaLogin) (*salidaSesion, error) {
		e := in.Body
		e.IP, e.UserAgent = in.ip, in.UserAgent
		s, err := m.IniciarSesion.Ejecutar(ctx, e)
		if err != nil {
			return nil, err
		}
		return &salidaSesion{SetCookie: m.cookiesSesion(s), Body: s}, nil
	})

	mfa := operacion("verificarMFA", http.MethodPost, "/api/v1/sesion/mfa", "Completar el inicio de sesión con el código TOTP", "", http.StatusOK)
	mfa.Description = "Con la sesión pendiente de MFA (cookie) y `X-CSRF-Token`. Cambia la sesión por una nueva."
	huma.Register(api, mfa, func(ctx context.Context, in *entradaMFA) (*salidaSesion, error) {
		e := in.Body
		e.IP, e.UserAgent = in.ip, in.UserAgent
		s, err := m.VerificarMFA.Ejecutar(ctx, e)
		if err != nil {
			return nil, err
		}
		return &salidaSesion{SetCookie: m.cookiesSesion(s), Body: s}, nil
	})

	huma.Register(api, operacion("cerrarSesion", http.MethodDelete, "/api/v1/sesion", "Cerrar la sesión", "", http.StatusNoContent),
		func(ctx context.Context, _ *vacio) (*salidaCookies, error) {
			if _, err := m.CerrarSesion.Ejecutar(ctx, struct{}{}); err != nil {
				return nil, err
			}
			return &salidaCookies{SetCookie: m.cookiesBorradas()}, nil
		})

	huma.Register(api, operacion("obtenerYo", http.MethodGet, "/api/v1/yo", "Usuario autenticado, permisos y alcance", "", http.StatusOK),
		func(ctx context.Context, _ *vacio) (*cuerpo[Yo], error) {
			return responder(m.ObtenerYo.Ejecutar(ctx, struct{}{}))
		})

	huma.Register(api, operacion("configurarMFA", http.MethodPost, "/api/v1/yo/mfa", "Generar el secreto TOTP del usuario", "", http.StatusOK),
		func(ctx context.Context, _ *vacio) (*cuerpo[MFAConfigurado], error) {
			return responder(m.ConfigurarMFA.Ejecutar(ctx, struct{}{}))
		})

	huma.Register(api, operacion("activarMFA", http.MethodPost, "/api/v1/yo/mfa/activar", "Activar el MFA con un código válido", "", http.StatusOK),
		func(ctx context.Context, in *conClave[EntradaActivarMFA]) (*cuerpo[Usuario], error) {
			return responder(m.ActivarMFA.Ejecutar(ctx, in.Body))
		})

	// Usuarios --------------------------------------------------------------------------
	huma.Register(api, operacion("listarUsuarios", http.MethodGet, "/api/v1/usuarios", "Listar usuarios", nucleo.PermUsuariosVer, http.StatusOK),
		func(ctx context.Context, in *EntradaListarUsuarios) (*cuerpo[nucleo.Pagina[Usuario]], error) {
			return responder(m.ListarUsuarios.Ejecutar(ctx, *in))
		})

	huma.Register(api, operacion("crearUsuario", http.MethodPost, "/api/v1/usuarios", "Crear un usuario", nucleo.PermUsuariosCrear, http.StatusCreated, http.StatusConflict),
		func(ctx context.Context, in *conClave[EntradaCrearUsuario]) (*cuerpo[Usuario], error) {
			return responder(m.CrearUsuario.Ejecutar(ctx, in.Body))
		})

	huma.Register(api, operacion("editarUsuario", http.MethodPost, "/api/v1/usuarios/{id}/editar", "Editar un usuario", nucleo.PermUsuariosEditar, http.StatusOK, http.StatusNotFound, http.StatusConflict),
		func(ctx context.Context, in *conID[EntradaEditarUsuario]) (*cuerpo[Usuario], error) {
			e := in.Body
			e.ID = in.ID
			return responder(m.EditarUsuario.Ejecutar(ctx, e))
		})

	huma.Register(api, operacion("desactivarUsuario", http.MethodPost, "/api/v1/usuarios/{id}/desactivar", "Desactivar un usuario", nucleo.PermUsuariosDesactivar, http.StatusOK, http.StatusNotFound, http.StatusConflict),
		func(ctx context.Context, in *conID[EntradaDesactivarUsuario]) (*cuerpo[Usuario], error) {
			e := in.Body
			e.ID = in.ID
			return responder(m.DesactivarUsuario.Ejecutar(ctx, e))
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

	huma.Register(api, operacion("crearToken", http.MethodPost, "/api/v1/tokens", "Crear un token de API", nucleo.PermTokensCrear, http.StatusCreated, http.StatusNotFound),
		func(ctx context.Context, in *conClave[EntradaCrearToken]) (*cuerpo[TokenCreado], error) {
			return responder(m.CrearToken.Ejecutar(ctx, in.Body))
		})

	huma.Register(api, operacion("revocarToken", http.MethodPost, "/api/v1/tokens/{id}/revocar", "Revocar un token de API", nucleo.PermTokensRevocar, http.StatusOK, http.StatusNotFound),
		func(ctx context.Context, in *conID[EntradaRevocarToken]) (*cuerpo[TokenAPI], error) {
			e := in.Body
			e.ID = in.ID
			return responder(m.RevocarToken.Ejecutar(ctx, e))
		})
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

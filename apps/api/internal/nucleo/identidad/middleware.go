package identidad

import (
	"context"
	"crypto/subtle"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
	identidadbd "github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad/bd"
)

// Nombres de cookies y cabeceras.
const (
	CookieSesion  = "iris_sesion"
	CookieCSRF    = "iris_csrf"
	CabeceraCSRF  = "X-CSRF-Token"
	CabeceraCanal = "X-Iris-Canal"
	rutaIniciar   = "/api/v1/sesion"
	// RutaCambiarContrasena: lo único que cambia estado que puede hacer una sesión con
	// contraseña temporal (además de cerrar sesión).
	RutaCambiarContrasena = "/api/v1/yo/contrasena"
)

// permitidaConTemporal dice si una sesión con contraseña temporal puede hacer la petición:
// cambiar la contraseña, ver /yo, entrar o salir, y las rutas sin datos (salud, listo, docs).
// Todo lo demás (también mfa.configurar) responde 403 cambio_contrasena_requerido, así quien
// solo tuviera la contraseña vieja (ya no sirve) ni la temporal antes que el usuario no puede
// quedarse con el MFA sin cambiarla primero.
func permitidaConTemporal(r *http.Request) bool {
	switch {
	case r.Method == http.MethodPost && (r.URL.Path == RutaCambiarContrasena || r.URL.Path == rutaIniciar || r.URL.Path == rutaCanjearSSO || r.URL.Path == rutaDescartarSSO):
		return true
	case r.Method == http.MethodDelete && r.URL.Path == rutaIniciar:
		return true
	case r.Method == http.MethodGet && (r.URL.Path == "/api/v1/yo" || r.URL.Path == "/api/v1/salud" ||
		r.URL.Path == "/api/v1/listo" || strings.HasPrefix(r.URL.Path, "/api/v1/docs") || strings.HasPrefix(r.URL.Path, "/api/v1/openapi")):
		return true
	}
	return !strings.HasPrefix(r.URL.Path, "/api/")
}

// Middleware resuelve las credenciales de cada petición y deja en el contexto el actor, sus
// permisos efectivos, el canal y la sesión. No decide si la petición puede pasar: eso lo hace
// cada caso de uso (401/403). Solo corta aquí:
//
//   - un Bearer inválido, vencido o revocado → 401 (nunca se degrada a anónimo);
//   - una petición que cambia estado con cookie de sesión sin X-CSRF-Token correcto → 403.
//
// CSRF por doble envío: la cabecera X-CSRF-Token debe ser igual a la cookie iris_csrf y a lo
// que la sesión guardó al iniciarse. El inicio de sesión (POST /api/v1/sesion) y el canje del
// autologin (POST /api/v1/sso/canjear y /descartar) no lo exigen.
//
// Portal de soporte: en /api/v1/portal/… la cookie iris_portal autentica al actor cliente
// (portal.go), con su propio CSRF (iris_portal_csrf), y la cookie del panel no cuenta (una
// sesión del panel abierta en el mismo navegador no estorba al portal). Fuera de esas rutas la
// cookie del portal no cuenta: el actor cliente no llega a ningún endpoint del panel. El Bearer
// vale en las dos (Argos emite los accesos al portal con su token).
// Va después de nucleo.ConBD.
func Middleware(log *slog.Logger) func(http.Handler) http.Handler {
	if log == nil {
		log = slog.Default()
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			bd := nucleo.BDDe(ctx)
			if bd == nil {
				next.ServeHTTP(w, r)
				return
			}
			q := identidadbd.New(bd)

			if auth := r.Header.Get("Authorization"); auth != "" {
				nctx, prob := autenticarBearer(ctx, q, auth)
				if prob != nil {
					w.Header().Set("WWW-Authenticate", `Bearer realm="iris"`)
					nucleo.EscribirProblema(w, prob)
					return
				}
				next.ServeHTTP(w, r.WithContext(nctx))
				return
			}

			// Encuesta pública (soporte, I2): sin sesión. La cookie del panel no cuenta aquí (sin
			// CSRF ni actor usuario), como la del panel en el portal.
			if strings.HasPrefix(r.URL.Path, PrefijoRutasEncuestaPublica) {
				next.ServeHTTP(w, r)
				return
			}

			if strings.HasPrefix(r.URL.Path, PrefijoRutasPortal) {
				if c, err := r.Cookie(CookiePortal); err == nil && c.Value != "" {
					nctx, prob := autenticarPortal(ctx, q, r, c.Value)
					if prob != nil {
						nucleo.EscribirProblema(w, prob)
						return
					}
					ctx = nctx
				}
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			if c, err := r.Cookie(CookieSesion); err == nil && c.Value != "" {
				fila, err := q.SesionVigentePorHash(ctx, identidadbd.SesionVigentePorHashParams{
					TokenHash: Hash256(c.Value), VentanaReautenticacionSeg: int32(VentanaReautenticacion / time.Second),
				})
				switch {
				case errors.Is(err, pgx.ErrNoRows):
					// sesión vencida o revocada: la petición sigue como anónima
				case err != nil:
					log.ErrorContext(ctx, "identidad: leer sesión", "err", err)
					nucleo.EscribirProblema(w, nucleo.ErrorInterno())
					return
				default:
					// El login y el canje del autologin no exigen CSRF: abren una sesión nueva.
					if metodoConEstado(r.Method) && (r.Method != http.MethodPost || (r.URL.Path != rutaIniciar && r.URL.Path != rutaCanjearSSO && r.URL.Path != rutaDescartarSSO)) {
						if !csrfValido(r, fila.NucleoSesione.CsrfHash) {
							nucleo.EscribirProblema(w, nucleo.NuevoProblema(http.StatusForbidden, "csrf_invalido",
								"CSRF inválido", "Falta la cabecera X-CSRF-Token o no coincide con la sesión"))
							return
						}
					}
					s, u := fila.NucleoSesione, fila.NucleoUsuario
					ctx = contexto.ConSesion(ctx, contexto.Sesion{ID: s.ID, UsuarioID: s.UsuarioID, MFAPendiente: s.MfaPendiente,
						CambioContrasena: u.DebeCambiarContrasena, Origen: s.Origen,
						Reautenticada: fila.Reautenticada})
					if u.DebeCambiarContrasena && !s.MfaPendiente && !permitidaConTemporal(r) {
						nucleo.EscribirProblema(w, nucleo.NuevoProblema(http.StatusForbidden, "cambio_contrasena_requerido",
							"Cambio de contraseña requerido", "Tienes una contraseña temporal: cámbiala (POST /api/v1/yo/contrasena) antes de continuar"))
						return
					}
					if !s.MfaPendiente {
						permisos, err := cargarPermisos(ctx, q, u.ID, nil)
						if err != nil {
							log.ErrorContext(ctx, "identidad: cargar permisos", "err", err)
							nucleo.EscribirProblema(w, nucleo.ErrorInterno())
							return
						}
						ctx = contexto.ConActor(ctx, contexto.Actor{Tipo: contexto.ActorUsuario, ID: u.ID, Nombre: u.Nombre}, permisos)
						ctx = contexto.ConCanal(ctx, canalDeSesion(r))
						if err := q.TocarSesion(ctx, s.ID); err != nil {
							log.WarnContext(ctx, "identidad: actualizar último uso de sesión", "err", err)
						}
					}
				}
			}
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func autenticarBearer(ctx context.Context, q *identidadbd.Queries, auth string) (context.Context, *nucleo.Problema) {
	esquema, valor, ok := strings.Cut(auth, " ")
	if !ok || !strings.EqualFold(esquema, "Bearer") || !strings.HasPrefix(valor, PrefijoToken) {
		return ctx, nucleo.NoAutenticado("Authorization debe ser «Bearer iris_pat_…»")
	}
	fila, err := q.TokenVigentePorHash(ctx, Hash256(strings.TrimSpace(valor)))
	if errors.Is(err, pgx.ErrNoRows) {
		return ctx, nucleo.NoAutenticado("Token inválido, vencido o revocado")
	}
	if err != nil {
		slog.ErrorContext(ctx, "identidad: leer token", "err", err)
		return ctx, nucleo.ErrorInterno()
	}
	t, u := fila.NucleoTokensApi, fila.NucleoUsuario
	permisos, err := cargarPermisos(ctx, q, u.ID, t.Permisos)
	if err != nil {
		slog.ErrorContext(ctx, "identidad: cargar permisos del token", "err", err)
		return ctx, nucleo.ErrorInterno()
	}
	tipo, canal := contexto.ActorToken, contexto.CanalAPI
	if u.Tipo == "integracion" {
		tipo, canal = contexto.ActorIntegracion, contexto.CanalIntegracion
	}
	ctx = contexto.ConActor(ctx, contexto.Actor{Tipo: tipo, ID: u.ID, Nombre: u.Nombre + " (" + t.Nombre + ")", TokenID: t.ID}, permisos)
	ctx = contexto.ConCanal(ctx, canal)
	if err := q.TocarToken(ctx, t.ID); err != nil {
		slog.WarnContext(ctx, "identidad: actualizar último uso de token", "err", err)
	}
	return ctx, nil
}

// autenticarPortal resuelve la cookie del portal: sesión vigente → actor cliente (canal portal,
// su empresa como activa); vencida o revocada → la petición sigue como anónima. Las peticiones
// que cambian estado exigen el CSRF del portal, salvo el canje (POST /api/v1/portal/sesion).
func autenticarPortal(ctx context.Context, q *identidadbd.Queries, r *http.Request, valor string) (context.Context, *nucleo.Problema) {
	s, err := q.SesionPortalVigentePorHash(ctx, Hash256(valor))
	if errors.Is(err, pgx.ErrNoRows) {
		return ctx, nil
	}
	if err != nil {
		slog.ErrorContext(ctx, "identidad: leer sesión del portal", "err", err)
		return ctx, nucleo.ErrorInterno()
	}
	if metodoConEstado(r.Method) && (r.Method != http.MethodPost || r.URL.Path != rutaSesionPortal) {
		if !csrfCookieValido(r, CookiePortalCSRF, s.CsrfHash) {
			return ctx, nucleo.NuevoProblema(http.StatusForbidden, "csrf_invalido",
				"CSRF inválido", "Falta la cabecera X-CSRF-Token o no coincide con la sesión del portal")
		}
	}
	ctx = contexto.ComoCliente(ctx, actorCliente(s.ClienteID, s.EmpresaID, s.Nombre))
	ctx = contexto.ConSesionPortal(ctx, s.ID)
	if err := q.TocarSesionPortal(ctx, s.ID); err != nil {
		slog.WarnContext(ctx, "identidad: actualizar último uso de sesión del portal", "err", err)
	}
	return ctx, nil
}

func csrfValido(r *http.Request, esperado []byte) bool {
	return csrfCookieValido(r, CookieCSRF, esperado)
}

func csrfCookieValido(r *http.Request, nombreCookie string, esperado []byte) bool {
	cabecera := r.Header.Get(CabeceraCSRF)
	cookie, err := r.Cookie(nombreCookie)
	if cabecera == "" || err != nil {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(cabecera), []byte(cookie.Value)) == 1 &&
		subtle.ConstantTimeCompare(Hash256(cabecera), esperado) == 1
}

// canalDeSesion: panel por defecto; la app de campo se identifica con X-Iris-Canal: campo. Una
// sesión del panel no puede declarar el canal portal (solo lo fija contexto.ComoCliente para el
// actor cliente): si no, una persona vería por ese canal lo que solo es del cliente.
func canalDeSesion(r *http.Request) contexto.Canal {
	if contexto.Canal(r.Header.Get(CabeceraCanal)) == contexto.CanalCampo {
		return contexto.CanalCampo
	}
	return contexto.CanalPanel
}

func metodoConEstado(m string) bool {
	return m == http.MethodPost || m == http.MethodPut || m == http.MethodPatch || m == http.MethodDelete
}

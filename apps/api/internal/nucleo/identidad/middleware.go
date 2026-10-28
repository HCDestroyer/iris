package identidad

import (
	"context"
	"crypto/subtle"
	"errors"
	"log/slog"
	"net/http"
	"strings"

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
)

// Middleware resuelve las credenciales de cada petición y deja en el contexto el actor, sus
// permisos efectivos, el canal y la sesión. No decide si la petición puede pasar: eso lo hace
// cada caso de uso (401/403). Solo corta aquí:
//
//   - un Bearer inválido, vencido o revocado → 401 (nunca se degrada a anónimo);
//   - una petición que cambia estado con cookie de sesión sin X-CSRF-Token correcto → 403.
//
// CSRF por doble envío: la cabecera X-CSRF-Token debe ser igual a la cookie iris_csrf y a lo
// que la sesión guardó al iniciarse. El inicio de sesión (POST /api/v1/sesion) no lo exige.
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
				fila, err := q.SesionVigentePorHash(ctx, Hash256(c.Value))
				switch {
				case errors.Is(err, pgx.ErrNoRows):
					// sesión vencida o revocada: la petición sigue como anónima
				case err != nil:
					log.ErrorContext(ctx, "identidad: leer sesión", "err", err)
					nucleo.EscribirProblema(w, nucleo.ErrorInterno())
					return
				default:
					if metodoConEstado(r.Method) && (r.Method != http.MethodPost || r.URL.Path != rutaIniciar) {
						if !csrfValido(r, fila.NucleoSesione.CsrfHash) {
							nucleo.EscribirProblema(w, nucleo.NuevoProblema(http.StatusForbidden, "csrf_invalido",
								"CSRF inválido", "Falta la cabecera X-CSRF-Token o no coincide con la sesión"))
							return
						}
					}
					s := fila.NucleoSesione
					ctx = contexto.ConSesion(ctx, contexto.Sesion{ID: s.ID, UsuarioID: s.UsuarioID, MFAPendiente: s.MfaPendiente})
					if !s.MfaPendiente {
						u := fila.NucleoUsuario
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

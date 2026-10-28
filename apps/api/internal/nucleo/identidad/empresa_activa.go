package identidad

import (
	"context"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
	identidadbd "github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad/bd"
)

// CabeceraEmpresa elige la empresa activa de la petición (id de la empresa en Argos).
const CabeceraEmpresa = "X-Iris-Empresa"

// Empresa activa (multiempresa igual que Argos). Cada petición de un usuario, token o
// integración resuelve en qué empresa opera:
//
//   - Empresas disponibles: con el permiso empresas.todas (admin, rol global), todas las de
//     nucleo.empresas; si no, las asignadas en nucleo.usuarios_empresas. Un token o una
//     integración usa las de su usuario dueño (y empresas.todas solo si el token lo conserva).
//   - Con X-Iris-Empresa: debe ser una de las disponibles; si no (o no es un id), 403
//     empresa_no_permitida en cualquier ruta.
//   - Sin cabecera: con una sola disponible, esa es la activa; con varias, la petición sigue
//     «por elegir» y las acciones operativas responden 422 empresa_requerida; sin ninguna (y
//     con empresas registradas), las operativas responden 403 sin_empresa.
//   - Sin empresas registradas (nucleo.empresas vacía): modo «sin conector». No hay empresa
//     activa (EmpresaDe = 0) y lo operativo se guarda y se lee con empresa_id null. Mandar la
//     cabecera en este modo es 403 empresa_no_permitida.
//
// Las acciones de administración (usuarios, roles, tokens, empresas, conectores) y las de
// Acceso Autenticado (GET /yo, MFA) no exigen empresa (auditoria.Caso.SinEmpresa), así el
// panel puede pedir /yo para armar el selector antes de elegir.

// MiddlewareEmpresa resuelve la empresa activa y la deja en el contexto
// (contexto.ConEmpresas / ConEmpresa). Va después de Middleware (necesita el actor). Las
// peticiones anónimas pasan sin empresa.
func MiddlewareEmpresa(log *slog.Logger) func(http.Handler) http.Handler {
	if log == nil {
		log = slog.Default()
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			bd := nucleo.BDDe(ctx)
			if bd == nil || !contexto.ActorDe(ctx).EsUsuario() {
				next.ServeHTTP(w, r)
				return
			}
			e, err := ResolverEmpresa(ctx, identidadbd.New(bd), r.Header.Get(CabeceraEmpresa))
			if err != nil {
				if p, interno := nucleo.AProblema(err); !interno {
					nucleo.EscribirProblema(w, p)
					return
				}
				log.ErrorContext(ctx, "identidad: resolver la empresa activa", "err", err)
				nucleo.EscribirProblema(w, nucleo.ErrorInterno())
				return
			}
			next.ServeHTTP(w, r.WithContext(contexto.ConEmpresas(ctx, e)))
		})
	}
}

// ResolverEmpresa aplica las reglas de arriba al actor del contexto y al valor de la cabecera
// ("" = no vino). Devuelve *nucleo.Problema 403 empresa_no_permitida si la cabecera no vale.
func ResolverEmpresa(ctx context.Context, q *identidadbd.Queries, cabecera string) (contexto.Empresas, error) {
	actor := contexto.ActorDe(ctx)
	todas := contexto.PermisosDe(ctx).Tiene(string(nucleo.PermEmpresasTodas))
	disponibles, err := q.EmpresasAccesibles(ctx, identidadbd.EmpresasAccesiblesParams{Todas: todas, UsuarioID: actor.ID})
	if err != nil {
		return contexto.Empresas{}, err
	}
	e := contexto.Empresas{Disponibles: disponibles, Todas: todas}
	if cabecera = strings.TrimSpace(cabecera); cabecera != "" {
		id, err := strconv.ParseInt(cabecera, 10, 64)
		if err != nil || id <= 0 || !slices.Contains(disponibles, id) {
			return contexto.Empresas{}, nucleo.EmpresaNoPermitida()
		}
		e.Estado, e.Activa = contexto.EmpresaActiva, id
		return e, nil
	}
	switch len(disponibles) {
	case 1:
		e.Estado, e.Activa = contexto.EmpresaActiva, disponibles[0]
		return e, nil
	case 0:
		hay, err := q.HayEmpresas(ctx)
		if err != nil {
			return contexto.Empresas{}, err
		}
		e.Estado = contexto.EmpresaSinAsignar
		if !hay {
			e.Estado = contexto.EmpresaSinConector
		}
		return e, nil
	}
	e.Estado = contexto.EmpresaPorElegir
	return e, nil
}

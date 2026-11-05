package identidad

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
	identidadbd "github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad/bd"
)

// Revalidar vuelve a leer la credencial con que se autenticó ctx (el token de API o la sesión del
// panel): si sigue vigente, el usuario está activo y aún opera en la empresa activa, devuelve ctx
// con sus permisos actuales (los de sus roles de ahora, con el límite del token). Si no, 401
// no_autenticado. Para las conexiones largas, que el middleware solo autentica al abrir (el flujo
// SSE del chat la llama en cada latido y antes de entregar eventos).
func Revalidar(ctx context.Context, bd identidadbd.DBTX) (context.Context, error) {
	actor := contexto.ActorDe(ctx)
	if !actor.EsUsuario() {
		return ctx, nucleo.NoAutenticado("")
	}
	q := identidadbd.New(bd)
	var limite []string
	switch s := contexto.SesionDe(ctx); {
	case actor.TokenID != 0:
		t, err := q.TokenVigentePorID(ctx, actor.TokenID)
		if errors.Is(err, pgx.ErrNoRows) || (err == nil && t.UsuarioID != actor.ID) {
			return ctx, nucleo.NoAutenticado("El token se revocó o venció")
		}
		if err != nil {
			return ctx, err
		}
		limite = t.Permisos
	case s.ID != 0:
		u, err := q.SesionVigentePorID(ctx, s.ID)
		if errors.Is(err, pgx.ErrNoRows) || (err == nil && u != actor.ID) {
			return ctx, nucleo.NoAutenticado("La sesión se cerró o venció")
		}
		if err != nil {
			return ctx, err
		}
	default:
		return ctx, nucleo.NoAutenticado("")
	}
	if e := contexto.EmpresasDe(ctx); e.Estado == contexto.EmpresaActiva {
		en, err := UsuarioEnEmpresa(ctx, bd, actor.ID, e.Activa)
		if err != nil {
			return ctx, err
		}
		if !en {
			return ctx, nucleo.NoAutenticado("Ya no operas en esta empresa")
		}
	}
	permisos, err := cargarPermisos(ctx, q, actor.ID, limite)
	if err != nil {
		return ctx, err
	}
	return contexto.ConActor(ctx, actor, permisos), nil
}

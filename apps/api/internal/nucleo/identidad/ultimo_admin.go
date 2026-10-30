package identidad

import (
	"context"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	identidadbd "github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad/bd"
)

// permisosAdministrarRoles: quien los tiene todos sin límite puede arreglar cualquier rol y
// asignarlo. Iris nunca se queda sin al menos una persona activa con ellos.
var permisosAdministrarRoles = []string{
	string(nucleo.PermUsuariosAsignarRol), string(nucleo.PermRolesCrear), string(nucleo.PermRolesEditar),
	string(nucleo.PermRolesActivar), string(nucleo.PermRolesDesactivar),
}

// guardiaUltimoAdmin mira, antes de un cambio, si existe un administrador de roles; despues()
// vuelve a mirar dentro de la misma transacción y responde 422 ultimo_admin si el cambio dejó a
// Iris sin ninguno (si ya no había, no bloquea: no empeora nada).
type guardiaUltimoAdmin struct {
	q     *identidadbd.Queries
	habia bool
}

// candadoAdministradores serializa los cambios que pueden quitar al último administrador de
// roles: sin él, dos admins que se quitan el rol a la vez verían cada uno al otro y los dos
// confirmarían. pg_advisory_xact_lock se suelta al terminar la transacción.
const candadoAdministradores int64 = 0x1715_0003

func nuevaGuardiaUltimoAdmin(ctx context.Context, q *identidadbd.Queries) (guardiaUltimoAdmin, error) {
	if err := q.CandadoSemilla(ctx, candadoAdministradores); err != nil {
		return guardiaUltimoAdmin{}, err
	}
	habia, err := q.HayAdministradorDeRoles(ctx, permisosAdministrarRoles)
	return guardiaUltimoAdmin{q: q, habia: habia}, err
}

func (g guardiaUltimoAdmin) despues(ctx context.Context) error {
	if !g.habia {
		return nil
	}
	hay, err := g.q.HayAdministradorDeRoles(ctx, permisosAdministrarRoles)
	if err != nil || hay {
		return err
	}
	return nucleo.ReglaNegocio("ultimo_admin",
		"No puede quedar Iris sin ninguna persona activa que administre roles (usuarios.asignar_rol y roles.* sin límite de alcance)")
}

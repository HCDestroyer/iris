package identidad

import (
	"context"
	"slices"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
	identidadbd "github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad/bd"
)

// EntradaVerUsuario: GET /api/v1/usuarios/{id}.
type EntradaVerUsuario struct {
	ID int64 `path:"id" minimum:"1"`
}

// nuevoVerUsuario lee un usuario por id con la misma regla que la lista (GET /usuarios): uno
// mismo, o alguien que comparte alguna empresa con el actor (todos con empresas.todas global o
// sin empresas registradas). Fuera de eso, 404 como un id inexistente.
func nuevoVerUsuario() *auditoria.Caso[EntradaVerUsuario, Usuario] {
	return auditoria.Nuevo(auditoria.Caso[EntradaVerUsuario, Usuario]{
		Accion:       "usuarios.ver",
		Permiso:      nucleo.PermUsuariosVer,
		SinEmpresa:   true, // administración: no es operativa
		AuditarExito: soloLecturas[Usuario],
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaVerUsuario) (Usuario, error) {
			q := identidadbd.New(t)
			u, err := q.UsuarioPorID(ctx, in.ID)
			if err != nil {
				return Usuario{}, err
			}
			empresas, err := empresasDe(ctx, q, in.ID)
			if err != nil {
				return Usuario{}, err
			}
			a, err := alcanceDelActor(ctx, q)
			if err != nil {
				return Usuario{}, err
			}
			propio := contexto.ActorDe(ctx).ID == in.ID
			comparte := slices.ContainsFunc(empresas[in.ID], func(e int64) bool { return slices.Contains(a.propias, e) })
			if !a.todas && !propio && !comparte {
				return Usuario{}, nucleo.NoEncontrado("")
			}
			roles, err := rolesDe(ctx, q, in.ID)
			if err != nil {
				return Usuario{}, err
			}
			return aUsuario(u, roles[in.ID], empresas[in.ID]), nil
		},
	})
}

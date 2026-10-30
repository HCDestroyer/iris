package identidad

import (
	"context"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	identidadbd "github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad/bd"
)

func nuevoListarRoles() *auditoria.Caso[nucleo.ParamsPagina, nucleo.Pagina[Rol]] {
	return auditoria.Nuevo(auditoria.Caso[nucleo.ParamsPagina, nucleo.Pagina[Rol]]{
		Accion:       "roles.ver",
		Permiso:      nucleo.PermRolesVer,
		SinEmpresa:   true, // administración: no es operativa
		AuditarExito: soloLecturas[nucleo.Pagina[Rol]],
		Logica: func(ctx context.Context, t *auditoria.Tx, in nucleo.ParamsPagina) (nucleo.Pagina[Rol], error) {
			var despues int64
			if _, err := nucleo.DecodificarCursor(in.Cursor, &despues); err != nil {
				return nucleo.Pagina[Rol]{}, err
			}
			q := identidadbd.New(t)
			filas, err := q.ListarRoles(ctx, identidadbd.ListarRolesParams{DespuesDe: despues, Limite: int32(in.Limite() + 1)}) //nolint:gosec // ≤ 201
			if err != nil {
				return nucleo.Pagina[Rol]{}, err
			}
			ids := make([]int64, 0, len(filas))
			for _, f := range filas {
				ids = append(ids, f.ID)
			}
			permisos, err := q.PermisosDeRoles(ctx, ids)
			if err != nil {
				return nucleo.Pagina[Rol]{}, err
			}
			porRol := map[int64][]string{}
			for _, p := range permisos {
				porRol[p.RolID] = append(porRol[p.RolID], p.Permiso)
			}
			usos, err := q.UsuariosPorRol(ctx, ids)
			if err != nil {
				return nucleo.Pagina[Rol]{}, err
			}
			usuarios := map[int64]int32{}
			for _, u := range usos {
				usuarios[u.RolID] = u.Usuarios
			}
			return nucleo.Paginar(filas, in.Limite(), func(r identidadbd.NucleoRole) Rol {
				return aRol(r, porRol[r.ID], usuarios[r.ID])
			}, func(r identidadbd.NucleoRole) []any { return []any{r.ID} }), nil
		},
	})
}

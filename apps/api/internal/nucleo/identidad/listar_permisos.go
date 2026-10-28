package identidad

import (
	"context"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	identidadbd "github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad/bd"
)

func nuevoListarPermisos() *auditoria.Caso[struct{}, nucleo.Pagina[PermisoInfo]] {
	return auditoria.Nuevo(auditoria.Caso[struct{}, nucleo.Pagina[PermisoInfo]]{
		Accion:       "permisos.ver",
		Permiso:      nucleo.PermPermisosVer,
		SinEmpresa:   true, // administración: no es operativa
		AuditarExito: soloLecturas[nucleo.Pagina[PermisoInfo]],
		Logica: func(ctx context.Context, t *auditoria.Tx, _ struct{}) (nucleo.Pagina[PermisoInfo], error) {
			filas, err := identidadbd.New(t).ListarPermisos(ctx)
			if err != nil {
				return nucleo.Pagina[PermisoInfo]{}, err
			}
			data := make([]PermisoInfo, 0, len(filas))
			for _, f := range filas {
				data = append(data, PermisoInfo{Codigo: nucleo.Permiso(f.Codigo), Descripcion: f.Descripcion})
			}
			return nucleo.PaginaDe(data), nil
		},
	})
}

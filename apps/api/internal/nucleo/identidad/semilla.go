package identidad

import (
	"context"
	"log/slog"

	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	identidadbd "github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad/bd"
)

const candadoSemilla int64 = 0x1715_0002

// SincronizarCatalogo deja nucleo.permisos igual a nucleo.Catalogo y los roles base
// (nucleo.RolesBase) con exactamente sus permisos. Es idempotente y la corre `iris migrar`
// (y las pruebas). Usa un advisory lock para que dos instancias no choquen.
//
// No es un caso de uso: es la semilla del sistema, igual que una migración.
func SincronizarCatalogo(ctx context.Context, bd nucleo.BD) error {
	ctx = nucleo.ConBD(ctx, bd)
	return nucleo.EnTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		q := identidadbd.New(tx)
		if err := q.CandadoSemilla(ctx, candadoSemilla); err != nil {
			return err
		}
		codigos := make([]string, 0, len(nucleo.Catalogo))
		for _, p := range nucleo.Catalogo {
			if err := q.GuardarPermiso(ctx, identidadbd.GuardarPermisoParams{Codigo: string(p.Codigo), Descripcion: p.Descripcion}); err != nil {
				return err
			}
			codigos = append(codigos, string(p.Codigo))
		}
		quitados, err := q.QuitarPermisosFueraDeCatalogo(ctx, codigos)
		if err != nil {
			return err
		}
		if quitados > 0 {
			slog.InfoContext(ctx, "semilla: permisos fuera del catálogo eliminados", "cantidad", quitados)
		}
		for _, r := range nucleo.RolesBase {
			id, err := q.GuardarRolSistema(ctx, identidadbd.GuardarRolSistemaParams{Codigo: r.Codigo, Nombre: r.Nombre, Descripcion: r.Descripcion})
			if err != nil {
				return err
			}
			permisos := make([]string, 0, len(r.PermisosDeRol()))
			for _, p := range r.PermisosDeRol() {
				permisos = append(permisos, string(p))
			}
			if err := q.QuitarPermisosDeRol(ctx, identidadbd.QuitarPermisosDeRolParams{RolID: id, Permisos: permisos}); err != nil {
				return err
			}
			if err := q.AgregarPermisosARol(ctx, identidadbd.AgregarPermisosARolParams{RolID: id, Permisos: permisos}); err != nil {
				return err
			}
		}
		return nil
	})
}

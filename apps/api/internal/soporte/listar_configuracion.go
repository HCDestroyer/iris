package soporte

import (
	"context"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	soportebd "github.com/HCDestroyer/iris/apps/api/internal/soporte/bd"
)

// EntradaListarCategorias: categorías globales y de la empresa activa.
type EntradaListarCategorias struct {
	Todas bool `query:"todas" doc:"Incluye las inactivas"`
}

func nuevoListarCategorias() *auditoria.Caso[EntradaListarCategorias, nucleo.Pagina[Categoria]] {
	return auditoria.Nuevo(auditoria.Caso[EntradaListarCategorias, nucleo.Pagina[Categoria]]{
		Accion:       "tickets.ver",
		Permiso:      nucleo.PermTicketsVer,
		AuditarExito: soloLecturas[nucleo.Pagina[Categoria]],
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaListarCategorias) (nucleo.Pagina[Categoria], error) {
			filas, err := soportebd.New(t).ListarCategorias(ctx, soportebd.ListarCategoriasParams{
				EmpresaID: empresaActiva(ctx), SoloActivas: !in.Todas,
			})
			if err != nil {
				return nucleo.Pagina[Categoria]{}, err
			}
			salida := make([]Categoria, 0, len(filas))
			for _, c := range filas {
				salida = append(salida, aCategoria(c))
			}
			return nucleo.PaginaDe(salida), nil
		},
	})
}

func nuevoListarSLAs() *auditoria.Caso[struct{}, nucleo.Pagina[SLA]] {
	return auditoria.Nuevo(auditoria.Caso[struct{}, nucleo.Pagina[SLA]]{
		Accion:       "tickets.ver",
		Permiso:      nucleo.PermTicketsVer,
		AuditarExito: soloLecturas[nucleo.Pagina[SLA]],
		Logica: func(ctx context.Context, t *auditoria.Tx, _ struct{}) (nucleo.Pagina[SLA], error) {
			q := soportebd.New(t)
			filas, err := q.ListarSLAs(ctx, empresaActiva(ctx))
			if err != nil {
				return nucleo.Pagina[SLA]{}, err
			}
			ids := make([]int64, 0, len(filas))
			for _, s := range filas {
				ids = append(ids, s.ID)
			}
			objetivos, err := q.ObjetivosDeSLAs(ctx, ids)
			if err != nil {
				return nucleo.Pagina[SLA]{}, err
			}
			porSLA := map[int64][]ObjetivoSLA{}
			for _, o := range objetivos {
				porSLA[o.SlaID] = append(porSLA[o.SlaID], ObjetivoSLA{Prioridad: o.Prioridad, RespuestaMin: o.RespuestaMin, ResolucionMin: o.ResolucionMin})
			}
			salida := make([]SLA, 0, len(filas))
			for _, s := range filas {
				salida = append(salida, SLA{
					ID: s.ID, Codigo: s.Codigo, Nombre: s.Nombre, Continuo: s.CalendarioID == nil, CalendarioID: s.CalendarioID,
					CalendarioNombre: s.CalendarioNombre, ZonaHoraria: s.CalendarioZona, PorDefecto: s.PorDefecto,
					Activo: s.Activo, Objetivos: noNulo(porSLA[s.ID]), Version: s.Version,
				})
			}
			return nucleo.PaginaDe(salida), nil
		},
	})
}

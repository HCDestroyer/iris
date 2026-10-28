package campo

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	campobd "github.com/HCDestroyer/iris/apps/api/internal/campo/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
)

// Lecturas. Cada una existe en dos casos de uso: con ordenes.ver (según el alcance) y con
// ordenes.ver_propias (solo las del actor, el técnico); el handler elige según los permisos.

// EntradaVerOrden es la entrada de GET /ordenes/{id}.
type EntradaVerOrden struct {
	ID int64 `path:"id" minimum:"1"`
}

// EntradaListarOrdenes: tablero con filtros, más recientes primero.
type EntradaListarOrdenes struct {
	nucleo.ParamsPagina
	Estado      []string  `query:"estado" enum:"pendiente,programada,en_camino,en_sitio,cerrada,fallida,cancelada" doc:"Uno o varios estados (estado=pendiente,programada)"`
	Tipo        []string  `query:"tipo" enum:"instalacion,visita,retiro,cambio_equipo,mantenimiento" doc:"Uno o varios tipos"`
	TecnicoID   int64     `query:"tecnicoId" minimum:"0"`
	SinTecnico  bool      `query:"sinTecnico" doc:"Solo órdenes sin técnico"`
	TicketID    int64     `query:"ticketId" minimum:"0"`
	ClienteID   int64     `query:"clienteId" minimum:"0" doc:"Cliente de Argos (historial del cliente)"`
	ServicioID  int64     `query:"servicioId" minimum:"0"`
	ZonaID      int64     `query:"zonaId" minimum:"0"`
	CuadrillaID int64     `query:"cuadrillaId" minimum:"0"`
	Desde       time.Time `query:"desde" doc:"Programadas desde (inclusive, UTC)"`
	Hasta       time.Time `query:"hasta" doc:"Programadas hasta (exclusive, UTC)"`
	Q           string    `query:"q" maxLength:"120" doc:"Busca en número, dirección y contacto"`
}

// EntradaListarEventos: historial de la orden.
type EntradaListarEventos struct {
	nucleo.ParamsPagina
	ID int64 `path:"id" minimum:"1"`
}

func filtro(v int64) *int64 {
	if v <= 0 {
		return nil
	}
	return &v
}

func filtroHora(v time.Time) *time.Time {
	if v.IsZero() {
		return nil
	}
	u := v.UTC()
	return &u
}

func noNulo[T any](v []T) []T {
	if v == nil {
		return []T{}
	}
	return v
}

// leerVisible lee la orden sin candado y exige que el actor la vea (404 si no).
func leerVisible(ctx context.Context, t *auditoria.Tx, q *campobd.Queries, id int64) (campobd.CampoOrden, error) {
	t.FijarEntidad("orden", id)
	f, err := q.OrdenPorID(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return f, noEncontrada()
	}
	if err != nil {
		return f, err
	}
	if err := verificarAcceso(ctx, nucleo.PermOrdenesVer, f); err != nil {
		return f, err
	}
	t.FijarEmpresa(deref(f.EmpresaID))
	return f, nil
}

func (m *Modulo) nuevoVerOrden(permiso nucleo.Permiso) *auditoria.Caso[EntradaVerOrden, OrdenDetalle] {
	return auditoria.Nuevo(auditoria.Caso[EntradaVerOrden, OrdenDetalle]{
		Accion:       string(permiso),
		Permiso:      permiso,
		AuditarExito: soloLecturas[OrdenDetalle],
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaVerOrden) (OrdenDetalle, error) {
			q := campobd.New(t)
			f, err := leerVisible(ctx, t, q, in.ID)
			if err != nil {
				return OrdenDetalle{}, err
			}
			d, err := detalles(ctx, q, []campobd.CampoOrden{f})
			if err != nil {
				return OrdenDetalle{}, err
			}
			return d[0], nil
		},
	})
}

func (m *Modulo) nuevoListarOrdenes(permiso nucleo.Permiso) *auditoria.Caso[EntradaListarOrdenes, nucleo.Pagina[Orden]] {
	return auditoria.Nuevo(auditoria.Caso[EntradaListarOrdenes, nucleo.Pagina[Orden]]{
		Accion:       string(permiso),
		Permiso:      permiso,
		AuditarExito: soloLecturas[nucleo.Pagina[Orden]],
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaListarOrdenes) (nucleo.Pagina[Orden], error) {
			q := campobd.New(t)
			var antesDe int64
			hay, err := nucleo.DecodificarCursor(in.Cursor, &antesDe)
			if err != nil {
				return nucleo.Pagina[Orden]{}, err
			}
			tipos, err := q.ListarTipos(ctx)
			if err != nil {
				return nucleo.Pagina[Orden]{}, err
			}
			codigos := map[int64]string{}
			tipoIDs := []int64{}
			for _, tp := range tipos {
				codigos[tp.ID] = tp.Codigo
				for _, pedido := range in.Tipo {
					if pedido == tp.Codigo {
						tipoIDs = append(tipoIDs, tp.ID)
					}
				}
			}
			if len(in.Tipo) > 0 && len(tipoIDs) == 0 {
				tipoIDs = []int64{0} // ningún tipo activo con ese código
			}
			global, zonas, cuadrillas := contexto.PermisosDe(ctx).Alcances(string(nucleo.PermOrdenesVer))
			todas, empresa := filtroEmpresa(ctx)
			p := campobd.ListarOrdenesParams{
				TodasEmpresas: todas, EmpresaID: empresa, Estados: noNulo(in.Estado), Tipos: tipoIDs,
				TecnicoID: filtro(in.TecnicoID), SinTecnico: in.SinTecnico, TicketID: filtro(in.TicketID),
				ClienteID: filtro(in.ClienteID), ServicioID: filtro(in.ServicioID), ZonaID: filtro(in.ZonaID),
				CuadrillaID: filtro(in.CuadrillaID), Desde: filtroHora(in.Desde), Hasta: filtroHora(in.Hasta),
				SoloPropias:   permiso == nucleo.PermOrdenesVerPropias,
				AlcanceGlobal: global, Zonas: noNulo(zonas), Cuadrillas: noNulo(cuadrillas),
				Limite: int32(in.Limite() + 1), //nolint:gosec // ≤ 201
			}
			if hay {
				p.AntesDe = &antesDe
			}
			if qq := strings.TrimSpace(in.Q); qq != "" {
				p.Q = &qq
			}
			if actor := contexto.ActorDe(ctx); actor.EsUsuario() {
				p.ActorID = &actor.ID
			}
			filas, err := q.ListarOrdenes(ctx, p)
			if err != nil {
				return nucleo.Pagina[Orden]{}, err
			}
			return nucleo.Paginar(filas, in.Limite(),
				func(f campobd.CampoOrden) Orden { return aOrden(f, codigos[f.TipoID]) },
				func(f campobd.CampoOrden) []any { return []any{f.ID} }), nil
		},
	})
}

func (m *Modulo) nuevoListarEventos(permiso nucleo.Permiso) *auditoria.Caso[EntradaListarEventos, nucleo.Pagina[EventoOrden]] {
	return auditoria.Nuevo(auditoria.Caso[EntradaListarEventos, nucleo.Pagina[EventoOrden]]{
		Accion:       string(permiso),
		Permiso:      permiso,
		AuditarExito: soloLecturas[nucleo.Pagina[EventoOrden]],
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaListarEventos) (nucleo.Pagina[EventoOrden], error) {
			q := campobd.New(t)
			if _, err := leerVisible(ctx, t, q, in.ID); err != nil {
				return nucleo.Pagina[EventoOrden]{}, err
			}
			var despues int64
			if _, err := nucleo.DecodificarCursor(in.Cursor, &despues); err != nil {
				return nucleo.Pagina[EventoOrden]{}, err
			}
			filas, err := q.ListarEventosOrden(ctx, campobd.ListarEventosOrdenParams{
				OrdenID: in.ID, DespuesDe: despues, Limite: int32(in.Limite() + 1), //nolint:gosec // ≤ 201
			})
			if err != nil {
				return nucleo.Pagina[EventoOrden]{}, err
			}
			return nucleo.Paginar(filas, in.Limite(), aEvento,
				func(f campobd.CampoOrdenEvento) []any { return []any{f.ID} }), nil
		},
	})
}

func nuevoListarTipos() *auditoria.Caso[struct{}, nucleo.Pagina[TipoOrden]] {
	return auditoria.Nuevo(auditoria.Caso[struct{}, nucleo.Pagina[TipoOrden]]{
		Accion:       "ordenes.tipos",
		Acceso:       auditoria.Autenticado,
		AuditarExito: soloLecturas[nucleo.Pagina[TipoOrden]],
		Logica: func(ctx context.Context, t *auditoria.Tx, _ struct{}) (nucleo.Pagina[TipoOrden], error) {
			q := campobd.New(t)
			tipos, err := q.ListarTipos(ctx)
			if err != nil {
				return nucleo.Pagina[TipoOrden]{}, err
			}
			plantillas, err := q.ListarPlantillas(ctx)
			if err != nil {
				return nucleo.Pagina[TipoOrden]{}, err
			}
			ids := make([]int64, 0, len(plantillas))
			for _, p := range plantillas {
				ids = append(ids, p.ID)
			}
			items, err := q.ItemsDePlantillas(ctx, ids)
			if err != nil {
				return nucleo.Pagina[TipoOrden]{}, err
			}
			porPlantilla := map[int64][]ItemPlantilla{}
			for _, it := range items {
				porPlantilla[it.PlantillaID] = append(porPlantilla[it.PlantillaID], ItemPlantilla{
					ID: it.ID, Codigo: it.Codigo, Etiqueta: it.Etiqueta, Tipo: it.Tipo, Obligatorio: it.Obligatorio,
					Unidad: it.Unidad, Posicion: it.Posicion,
				})
			}
			salida := make([]TipoOrden, 0, len(tipos))
			for _, tp := range tipos {
				if !tp.Activo {
					continue
				}
				o := TipoOrden{ID: tp.ID, Codigo: tp.Codigo, Nombre: tp.Nombre, DuracionMin: tp.DuracionMin,
					PlantillaID: tp.PlantillaID, ExigeInstalar: tp.ExigeInstalar, ExigeRetirar: tp.ExigeRetirar,
					Plantillas: []Plantilla{}}
				for _, p := range plantillas {
					if p.TipoCodigo == tp.Codigo {
						o.Plantillas = append(o.Plantillas, Plantilla{ID: p.ID, Codigo: p.Codigo, Nombre: p.Nombre,
							RequiereFirma: p.RequiereFirma, Items: noNulo(porPlantilla[p.ID])})
					}
				}
				salida = append(salida, o)
			}
			return nucleo.PaginaDe(salida), nil
		},
	})
}

// detalles arma las órdenes con su checklist (plantilla + marcas vigentes), notas,
// materiales, equipos, firma y adjuntos, con pocas consultas para cualquier cantidad de
// órdenes (paquete y cambios).
func detalles(ctx context.Context, q *campobd.Queries, ordenes []campobd.CampoOrden) ([]OrdenDetalle, error) {
	ids := make([]int64, 0, len(ordenes))
	plantillaIDs := []int64{}
	for _, f := range ordenes {
		ids = append(ids, f.ID)
		if f.PlantillaID != nil {
			plantillaIDs = append(plantillaIDs, *f.PlantillaID)
		}
	}
	tipos, err := q.ListarTipos(ctx)
	if err != nil {
		return nil, err
	}
	codigos := map[int64]string{}
	for _, tp := range tipos {
		codigos[tp.ID] = tp.Codigo
	}
	plantillas := map[int64]bool{}
	for _, id := range plantillaIDs {
		if _, ok := plantillas[id]; ok {
			continue
		}
		p, err := q.PlantillaPorID(ctx, id)
		if err != nil {
			return nil, err
		}
		plantillas[id] = p.RequiereFirma
	}
	items, err := q.ItemsDePlantillas(ctx, plantillaIDs)
	if err != nil {
		return nil, err
	}
	marcas, err := q.ChecklistDeOrdenes(ctx, ids)
	if err != nil {
		return nil, err
	}
	notas, err := q.NotasDeOrdenes(ctx, ids)
	if err != nil {
		return nil, err
	}
	materiales, err := q.MaterialesDeOrdenes(ctx, ids)
	if err != nil {
		return nil, err
	}
	equipos, err := q.EquiposDeOrdenes(ctx, ids)
	if err != nil {
		return nil, err
	}
	firmas, err := q.FirmasDeOrdenes(ctx, ids)
	if err != nil {
		return nil, err
	}
	adjuntos, err := q.AdjuntosDeOrdenes(ctx, ids)
	if err != nil {
		return nil, err
	}

	marcaDe := map[[2]int64]campobd.CampoOrdenChecklist{}
	for _, mc := range marcas {
		marcaDe[[2]int64{mc.OrdenID, mc.ItemID}] = mc
	}
	salida := make([]OrdenDetalle, 0, len(ordenes))
	for _, f := range ordenes {
		d := OrdenDetalle{Orden: aOrden(f, codigos[f.TipoID]), Checklist: []ItemChecklist{}, Notas: []NotaOrden{},
			Materiales: []MaterialOrden{}, Equipos: []EquipoOrden{}, Adjuntos: []AdjuntoOrden{}}
		if f.PlantillaID != nil {
			d.RequiereFirma = plantillas[*f.PlantillaID]
			for _, it := range items {
				if it.PlantillaID != *f.PlantillaID {
					continue
				}
				ic := ItemChecklist{ItemID: it.ID, Codigo: it.Codigo, Etiqueta: it.Etiqueta, Tipo: it.Tipo,
					Obligatorio: it.Obligatorio, Unidad: it.Unidad, Posicion: it.Posicion}
				if mc, ok := marcaDe[[2]int64{f.ID, it.ID}]; ok {
					var v any
					_ = json.Unmarshal(mc.Valor, &v)
					ic.Valor, ic.MarcadoPor, ic.OcurridoEn = v, mc.MarcadoPor, &mc.OcurridoEn
				}
				d.Checklist = append(d.Checklist, ic)
			}
		}
		for _, n := range notas {
			if n.OrdenID == f.ID {
				d.Notas = append(d.Notas, NotaOrden{ID: n.ID, Texto: n.Texto, AutorID: n.AutorID, AutorNombre: n.AutorNombre,
					Canal: n.Canal, OcurridoEn: n.OcurridoEn})
			}
		}
		for _, mt := range materiales {
			if mt.OrdenID == f.ID {
				d.Materiales = append(d.Materiales, MaterialOrden{ID: mt.ID, ProductoID: mt.ProductoID, Descripcion: mt.Descripcion,
					Cantidad: numero(mt.Cantidad), BodegaID: mt.BodegaID, ExcedeExistencia: mt.ExcedeExistencia, OcurridoEn: mt.OcurridoEn})
			}
		}
		for _, e := range equipos {
			if e.OrdenID == f.ID {
				d.Equipos = append(d.Equipos, EquipoOrden{ID: e.ID, Accion: e.Accion, UnidadID: e.UnidadID, Serie: e.Serie,
					MAC: e.Mac, Modelo: e.Modelo, Condicion: e.Condicion, OcurridoEn: e.OcurridoEn})
			}
		}
		for _, fi := range firmas {
			if fi.OrdenID == f.ID {
				d.Firma = &FirmaOrden{Nombre: fi.Nombre, Documento: fi.Documento, AdjuntoID: fi.AdjuntoID, OcurridoEn: fi.OcurridoEn}
			}
		}
		for _, a := range adjuntos {
			if a.OrdenID == f.ID {
				d.Adjuntos = append(d.Adjuntos, AdjuntoOrden{ID: a.ID, Tipo: a.Tipo, ItemID: a.ItemID, Descripcion: a.Descripcion, OcurridoEn: a.OcurridoEn})
			}
		}
		salida = append(salida, d)
	}
	return salida, nil
}

package campo

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	campobd "github.com/HCDestroyer/iris/apps/api/internal/campo/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos"
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
	Estado      []string  `query:"estado" enum:"pendiente,programada,en_camino,en_sitio,pausada,cerrada,fallida,cancelada" doc:"Uno o varios estados (estado=pendiente,programada)"`
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
			// La copia trae datos personales del cliente (documento, correo, teléfonos): solo con
			// clientes.ver, como la ficha.
			if contexto.PermisosDe(ctx).Tiene(string(nucleo.PermClientesVer)) {
				if d[0].DatosArgos, err = argos.CopiaDe(ctx, t, argos.CopiaOrden, f.ID, f.EmpresaID); err != nil {
					return OrdenDetalle{}, err
				}
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
			catalogo, err := catalogoActivo(ctx, q)
			if err != nil {
				return nucleo.Pagina[TipoOrden]{}, err
			}
			salida := make([]TipoOrden, 0, len(tipos))
			for _, tp := range tipos {
				if !tp.Activo {
					continue
				}
				o := TipoOrden{ID: tp.ID, Codigo: tp.Codigo, Nombre: tp.Nombre, DuracionMin: tp.DuracionMin,
					PlantillaID: tp.PlantillaID, ExigeInstalar: tp.ExigeInstalar, ExigeRetirar: tp.ExigeRetirar,
					Plantillas: []Plantilla{}}
				for _, p := range catalogo {
					if p.TipoCodigo != nil && *p.TipoCodigo == tp.Codigo {
						o.Plantillas = append(o.Plantillas, p.Plantilla)
					}
				}
				salida = append(salida, o)
			}
			return nucleo.PaginaDe(salida), nil
		},
	})
}

// detalles arma las órdenes con sus formularios (plantilla en su versión + marcas vigentes),
// notas, materiales, equipos, firma, adjuntos, pausas y cita, con pocas consultas para
// cualquier cantidad de órdenes (paquete y cambios).
func detalles(ctx context.Context, q *campobd.Queries, ordenes []campobd.CampoOrden) ([]OrdenDetalle, error) {
	ids := make([]int64, 0, len(ordenes))
	for _, f := range ordenes {
		ids = append(ids, f.ID)
	}
	tipos, err := q.ListarTipos(ctx)
	if err != nil {
		return nil, err
	}
	codigos := map[int64]string{}
	for _, tp := range tipos {
		codigos[tp.ID] = tp.Codigo
	}
	forms, err := cargarFormularios(ctx, q, ids)
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
	citas, err := q.CitasDeOrdenes(ctx, ids)
	if err != nil {
		return nil, err
	}
	pausas, err := q.PausasDeOrdenes(ctx, ids)
	if err != nil {
		return nil, err
	}

	salida := make([]OrdenDetalle, 0, len(ordenes))
	for _, f := range ordenes {
		d := OrdenDetalle{Orden: aOrden(f, codigos[f.TipoID]), Checklist: []ItemChecklist{}, Notas: []NotaOrden{},
			Materiales: []MaterialOrden{}, Equipos: []EquipoOrden{}, Adjuntos: []AdjuntoOrden{},
			Formularios: []FormularioOrden{}, Pausas: []PausaOrden{}}
		d.Formularios, d.Checklist, d.RequiereFirma = forms.deOrden(f.ID)
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
					MAC: e.Mac, Modelo: e.Modelo, Condicion: e.Condicion, Validado: e.Validado, OcurridoEn: e.OcurridoEn})
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
		for _, p := range pausas {
			if p.OrdenID == f.ID {
				d.Pausas = append(d.Pausas, aPausa(p))
			}
		}
		for _, c := range citas {
			if c.OrdenID == f.ID && slices.Contains(estadosConCita, f.Estado) {
				d.Cita = &CitaOrden{TecnicoID: c.TecnicoID, Inicio: c.Inicio, Fin: c.Fin, VentanaInicio: c.VentanaInicio, VentanaFin: c.VentanaFin}
			}
		}
		salida = append(salida, d)
	}
	return salida, nil
}

// estadosConCita: la orden ocupa su cita de la agenda (también pausada: sigue abierta).
var estadosConCita = []string{EstadoProgramada, EstadoEnCamino, EstadoEnSitio, EstadoPausada}

// formulariosCargados: los formularios vigentes de unas órdenes con sus versiones, ítems y
// marcas.
type formulariosCargados struct {
	formularios   []campobd.CampoOrdenFormulario
	versiones     map[int64]campobd.CampoPlantillaVersion
	codigos       map[int64]string // plantilla → código
	items         map[int64][]campobd.CampoPlantillaItem
	marcas        map[[2]string]campobd.CampoOrdenChecklist // (formulario, item)
	conRespuestas map[uuid.UUID]bool
}

func cargarFormularios(ctx context.Context, q *campobd.Queries, ordenes []int64) (formulariosCargados, error) {
	c := formulariosCargados{versiones: map[int64]campobd.CampoPlantillaVersion{}, codigos: map[int64]string{},
		items: map[int64][]campobd.CampoPlantillaItem{}, marcas: map[[2]string]campobd.CampoOrdenChecklist{},
		conRespuestas: map[uuid.UUID]bool{}}
	var err error
	if c.formularios, err = q.FormulariosDeOrdenes(ctx, ordenes); err != nil {
		return c, err
	}
	versionIDs, plantillaIDs, formIDs := []int64{}, []int64{}, []uuid.UUID{}
	for _, f := range c.formularios {
		versionIDs = append(versionIDs, f.VersionID)
		plantillaIDs = append(plantillaIDs, f.PlantillaID)
		formIDs = append(formIDs, f.ID)
	}
	versiones, err := q.VersionesPorIDs(ctx, versionIDs)
	if err != nil {
		return c, err
	}
	for _, v := range versiones {
		c.versiones[v.ID] = v
	}
	plantillas, err := q.PlantillasPorIDs(ctx, plantillaIDs)
	if err != nil {
		return c, err
	}
	for _, p := range plantillas {
		c.codigos[p.ID] = p.Codigo
	}
	items, err := q.ItemsDeVersiones(ctx, versionIDs)
	if err != nil {
		return c, err
	}
	for _, it := range items {
		c.items[it.VersionID] = append(c.items[it.VersionID], it)
	}
	marcas, err := q.ChecklistDeOrdenes(ctx, ordenes)
	if err != nil {
		return c, err
	}
	for _, mc := range marcas {
		c.marcas[[2]string{mc.FormularioID.String(), strconv.FormatInt(mc.ItemID, 10)}] = mc
	}
	con, err := q.FormulariosConRespuestas(ctx, formIDs)
	if err != nil {
		return c, err
	}
	for _, id := range con {
		c.conRespuestas[id] = true
	}
	return c, nil
}

// deOrden: los formularios de la orden, su checklist (ítems de todos, en orden) y si alguno pide
// la firma.
func (c formulariosCargados) deOrden(ordenID int64) ([]FormularioOrden, []ItemChecklist, bool) {
	forms, checklist, firma := []FormularioOrden{}, []ItemChecklist{}, false
	for _, f := range c.formularios {
		if f.OrdenID != ordenID {
			continue
		}
		v := c.versiones[f.VersionID]
		firma = firma || v.RequiereFirma
		forms = append(forms, FormularioOrden{ID: f.ID, PlantillaID: f.PlantillaID, Codigo: c.codigos[f.PlantillaID],
			Nombre: v.Nombre, VersionID: v.ID, NumeroVersion: v.Numero, RequiereFirma: v.RequiereFirma, Origen: f.Origen,
			AgregadoPor: f.AgregadoPor, AgregadoEn: f.AgregadoEn, ConRespuestas: c.conRespuestas[f.ID]})
		for _, it := range c.items[f.VersionID] {
			ic := ItemChecklist{ItemID: it.ID, FormularioID: f.ID, Codigo: it.Codigo, Etiqueta: it.Etiqueta, Tipo: it.Tipo,
				Obligatorio: it.Obligatorio, Unidad: it.Unidad, Opciones: opcionesDe(it.Opciones), Posicion: it.Posicion}
			if mc, ok := c.marcas[[2]string{f.ID.String(), strconv.FormatInt(it.ID, 10)}]; ok {
				var v any
				_ = json.Unmarshal(mc.Valor, &v)
				ic.Valor, ic.MarcadoPor, ic.OcurridoEn = v, mc.MarcadoPor, &mc.OcurridoEn
			}
			checklist = append(checklist, ic)
		}
	}
	return forms, checklist, firma
}

// aPausa convierte una fila de pausa.
func aPausa(p campobd.CampoOrdenPausa) PausaOrden {
	o := PausaOrden{ID: p.ID, Motivo: p.Motivo, Nota: p.Nota, DesdeEstado: p.DesdeEstado, Inicio: p.Inicio, Fin: p.Fin,
		Cierre: p.Cierre, PausadaPor: p.PausadaPor, CerradaPor: p.CerradaPor}
	if p.Fin != nil {
		o.DuracionSeg = ptr(int64(p.Fin.Sub(p.Inicio) / time.Second))
	}
	return o
}

package campo

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	campobd "github.com/HCDestroyer/iris/apps/api/internal/campo/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
)

// Formularios de una orden: la plantilla del tipo (al crearla) y los que agrega despacho
// (ordenes.asignar_formulario) o el técnico en campo, sin señal (operación del lote
// orden.agregar_formulario, con ordenes.registrar sobre una orden propia). Cada formulario fija la
// versión de su plantilla: editarla después no cambia lo que la orden ya tiene. Un formulario se
// quita solo mientras no tenga respuestas (marcas o adjuntos de sus ítems).

// Orígenes de un formulario.
const (
	OrigenTipo     = "tipo"
	OrigenDespacho = "despacho"
	OrigenTecnico  = "tecnico"
)

// AvisoVersionAnterior: el técnico agregó el formulario con la versión que veía su teléfono,
// que ya no es la actual de la plantilla (se respeta la suya; contratos/sync.md §3).
const AvisoVersionAnterior = "version_anterior"

// estadosAbiertos: la orden admite formularios nuevos o quitarlos.
var estadosAbiertos = []string{EstadoPendiente, EstadoProgramada, EstadoEnCamino, EstadoEnSitio, EstadoPausada}

// EntradaAgregarFormulario es la entrada de ordenes.asignar_formulario (despacho).
type EntradaAgregarFormulario struct {
	ID          int64 `json:"-"`
	Version     int32 `json:"version" minimum:"1" doc:"Versión leída de la orden; si cambió, 409"`
	PlantillaID int64 `json:"plantillaId" minimum:"1" doc:"Plantilla activa, común o de la empresa de la orden (se agrega su versión actual)"`
}

// EntradaQuitarFormulario es la entrada de quitar un formulario (despacho).
type EntradaQuitarFormulario struct {
	ID           int64     `json:"-"`
	FormularioID uuid.UUID `json:"-"`
	Version      int32     `json:"version" minimum:"1" doc:"Versión leída de la orden; si cambió, 409"`
}

// EntradaAgregarFormularioCampo son los datos de orden.agregar_formulario (lote). El id del
// formulario es el de la operación.
type EntradaAgregarFormularioCampo struct {
	ID          int64   `json:"-"`
	PlantillaID int64   `json:"plantillaId" minimum:"1" doc:"Plantilla del catálogo del paquete"`
	VersionID   *int64  `json:"versionId,omitempty" minimum:"1" doc:"Versión que veía el teléfono (la del catálogo); ausente = la actual"`
	Sync        *OpSync `json:"-"`
}

// EntradaQuitarFormularioCampo son los datos de orden.quitar_formulario (lote).
type EntradaQuitarFormularioCampo struct {
	ID           int64     `json:"-"`
	FormularioID uuid.UUID `json:"formularioId" doc:"Un formulario que agregó el mismo técnico, sin respuestas"`
	Sync         *OpSync   `json:"-"`
}

func (e EntradaAgregarFormulario) ordenID() int64      { return e.ID }
func (e EntradaQuitarFormulario) ordenID() int64       { return e.ID }
func (e EntradaAgregarFormularioCampo) ordenID() int64 { return e.ID }
func (e EntradaQuitarFormularioCampo) ordenID() int64  { return e.ID }

func plantillaInvalida(detalle string) *nucleo.Problema {
	return campoInvalido("plantillaId", detalle, "plantilla")
}

// plantillaParaOrden toma la plantilla (candado compartido: una edición concurrente espera y,
// como su versión ya tiene uso, crea otra) y verifica que la orden la pueda usar: existe, es
// común o de la empresa de la orden y está activa.
func plantillaParaOrden(ctx context.Context, q *campobd.Queries, plantillaID int64, f campobd.CampoOrden) (campobd.CampoPlantillasChecklist, error) {
	p, err := q.PlantillaPorIDCompartida(ctx, plantillaID)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && !plantillaSirveA(p, f.EmpresaID)) {
		return p, plantillaInvalida("La plantilla no existe o no es de la empresa de la orden")
	}
	if err != nil {
		return p, err
	}
	if !p.Activa {
		return p, nucleo.ReglaNegocio("plantilla_inactiva", fmt.Sprintf("La plantilla «%s» está desactivada: no se puede agregar", p.Nombre))
	}
	return p, nil
}

// agregarFormulario guarda el formulario en la orden con la versión dada, su historial y, salvo
// el del tipo (va con orden.creada), el evento orden.formulario_agregado.
func agregarFormulario(ctx context.Context, t *auditoria.Tx, q *campobd.Queries, f campobd.CampoOrden, p campobd.CampoPlantillasChecklist,
	version campobd.CampoPlantillaVersion, origen string, id uuid.UUID, cuando time.Time, s *OpSync) (campobd.CampoOrdenFormulario, error) {
	previo, err := q.FormularioVigenteDePlantilla(ctx, campobd.FormularioVigenteDePlantillaParams{OrdenID: f.ID, PlantillaID: p.ID})
	switch {
	case err == nil:
		return previo, formularioDuplicado(p, previo)
	case !errors.Is(err, pgx.ErrNoRows):
		return previo, err
	}
	fo, err := q.CrearFormulario(ctx, campobd.CrearFormularioParams{ID: id, OrdenID: f.ID, PlantillaID: p.ID, VersionID: version.ID,
		Origen: origen, AgregadoPor: actorUsuario(t), AgregadoEn: cuando, Canal: string(contexto.CanalDe(ctx)), OperacionID: operacionDe(s)})
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "23505" {
		if pg.ConstraintName == "orden_formularios_pkey" {
			return fo, nucleo.Conflicto("formulario_existente", "Ya existe un formulario con ese id")
		}
		return fo, nucleo.Conflicto("formulario_duplicado", fmt.Sprintf("La orden ya tiene el formulario «%s»", p.Nombre))
	}
	if err != nil {
		return fo, err
	}
	t.RegistrarCambios("orden_formulario", fo.ID.String(), nil, fo)
	if err := historial(ctx, t, q, f.ID, "formulario", nil, nil, nil, map[string]any{
		"accion": "agregado", "formulario_id": fo.ID, "plantilla_id": p.ID, "version_id": version.ID,
		"numero_version": version.Numero, "nombre": version.Nombre, "origen": origen,
	}, cuando); err != nil {
		return fo, err
	}
	if origen == OrigenTipo {
		return fo, nil
	}
	return fo, t.Emitir("orden.formulario_agregado", map[string]any{
		"orden_id": f.ID, "formulario_id": fo.ID, "plantilla_id": p.ID, "version_id": version.ID,
		"numero_version": version.Numero, "origen": origen, "tecnico_id": f.TecnicoID,
	})
}

// formularioDuplicado: la plantilla ya está en la orden (409, con el id del que existe para que
// el teléfono lo use).
func formularioDuplicado(p campobd.CampoPlantillasChecklist, previo campobd.CampoOrdenFormulario) *nucleo.Problema {
	return nucleo.Conflicto("formulario_duplicado",
		fmt.Sprintf("La orden ya tiene el formulario «%s» (%s)", p.Nombre, previo.ID))
}

// formularioDeTipo agrega a una orden recién creada el formulario de su plantilla (la del tipo
// o la elegida al crearla) en la versión actual.
func formularioDeTipo(ctx context.Context, t *auditoria.Tx, q *campobd.Queries, f campobd.CampoOrden, cuando time.Time) error {
	if f.PlantillaID == nil {
		return nil
	}
	p, err := q.PlantillaPorIDCompartida(ctx, *f.PlantillaID)
	if err != nil {
		return err
	}
	v, err := q.VersionPorID(ctx, p.VersionActualID)
	if err != nil {
		return err
	}
	id, err := uuid.NewV7()
	if err != nil {
		return err
	}
	_, err = agregarFormulario(ctx, t, q, f, p, v, OrigenTipo, id, cuando, nil)
	return err
}

func (m *Modulo) nuevoAsignarFormulario() *auditoria.Caso[EntradaAgregarFormulario, Orden] {
	return accionOrden("ordenes.asignar_formulario", nucleo.PermOrdenesAsignarForm, sinLote[EntradaAgregarFormulario],
		func(ctx context.Context, t *auditoria.Tx, q *campobd.Queries, in EntradaAgregarFormulario) (campobd.CampoOrden, string, error) {
			f, err := tomar(ctx, t, q, in.ID, nucleo.PermOrdenesAsignarForm, &in.Version, nil)
			if err != nil {
				return f, "", err
			}
			if err := exigirEstado(f, "agregar formularios a", estadosAbiertos...); err != nil {
				return f, "", err
			}
			p, err := plantillaParaOrden(ctx, q, in.PlantillaID, f)
			if err != nil {
				return f, "", err
			}
			v, err := q.VersionPorID(ctx, p.VersionActualID)
			if err != nil {
				return f, "", err
			}
			id, err := uuid.NewV7()
			if err != nil {
				return f, "", err
			}
			fo, err := agregarFormulario(ctx, t, q, f, p, v, OrigenDespacho, id, m.ahora(), nil)
			if err != nil {
				return f, "", err
			}
			t.Anotar(map[string]any{"formulario_id": fo.ID})
			f, err = tocar(ctx, t, q, f)
			return f, "", err
		})
}

func (m *Modulo) nuevoAgregarFormularioCampo() *auditoria.Caso[EntradaAgregarFormularioCampo, Orden] {
	return accionOrden("ordenes.agregar_formulario", nucleo.PermOrdenesRegistrar, func(e EntradaAgregarFormularioCampo) *OpSync { return e.Sync },
		func(ctx context.Context, t *auditoria.Tx, q *campobd.Queries, in EntradaAgregarFormularioCampo) (campobd.CampoOrden, string, error) {
			f, err := tomar(ctx, t, q, in.ID, nucleo.PermOrdenesRegistrar, nil, in.Sync)
			if err != nil {
				return f, "", err
			}
			if in.Sync == nil {
				return f, "", nucleo.Prohibido("Agregar formularios desde el panel es ordenes.asignar_formulario")
			}
			if !propia(ctx, f) {
				return f, "", nucleo.Prohibido("Solo el técnico de la orden agrega formularios desde la app de campo")
			}
			if err := exigirEstado(f, "agregar formularios a", estadosAbiertos...); err != nil {
				return f, "", err
			}
			p, err := plantillaParaOrden(ctx, q, in.PlantillaID, f)
			if err != nil {
				return f, "", err
			}
			// La versión que veía el teléfono (sus marcas sin señal son de esos ítems), aunque ya
			// no sea la actual: se avisa (version_anterior), nunca se cambia en silencio. Solo la
			// actual o la que estaba vigente a la hora de la operación: una más vieja dejaría la
			// orden con una versión superada y bloquearía la actual (una plantilla una vez).
			cuando := m.cuandoEn(in.Sync, f)
			v, err := q.VersionPorID(ctx, p.VersionActualID)
			if err != nil {
				return f, "", err
			}
			aviso := ""
			if in.VersionID != nil && *in.VersionID != p.VersionActualID {
				vigente, err := q.VersionVigenteEn(ctx, campobd.VersionVigenteEnParams{PlantillaID: p.ID, Momento: cuando})
				if errors.Is(err, pgx.ErrNoRows) || (err == nil && vigente.ID != *in.VersionID) {
					return f, "", campoInvalido("versionId", "La versión no es la actual de la plantilla ni la vigente a la hora de la operación", "version")
				}
				if err != nil {
					return f, "", err
				}
				v, aviso = vigente, AvisoVersionAnterior
			}
			fo, err := agregarFormulario(ctx, t, q, f, p, v, OrigenTecnico, in.Sync.ID, cuando, in.Sync)
			if err != nil {
				return f, "", err
			}
			t.Anotar(map[string]any{"formulario_id": fo.ID, "version_anterior": aviso != ""})
			f, err = tocar(ctx, t, q, f)
			return f, aviso, err
		})
}

// quitarFormulario: el formulario es de la orden, sigue vigente y no tiene respuestas.
func quitarFormulario(ctx context.Context, t *auditoria.Tx, q *campobd.Queries, f campobd.CampoOrden, id uuid.UUID, cuando time.Time,
	autorizar func(campobd.CampoOrdenFormulario) error) error {
	fo, err := q.FormularioPorID(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && fo.OrdenID != f.ID) {
		return campoInvalido("formularioId", "El formulario no es de esta orden", "formulario")
	}
	if err != nil {
		return err
	}
	if fo.QuitadoEn != nil {
		return nucleo.Conflicto("formulario_ya_quitado", "El formulario ya se quitó de la orden")
	}
	// El del tipo es el mínimo de la orden: no se quita (otro checklist = otra orden con otra
	// plantilla del mismo tipo).
	if fo.Origen == OrigenTipo {
		return nucleo.ReglaNegocio("formulario_del_tipo", "El formulario del tipo de orden no se quita; crea la orden con otra plantilla del mismo tipo")
	}
	if err := autorizar(fo); err != nil {
		return err
	}
	con, err := q.FormulariosConRespuestas(ctx, []uuid.UUID{fo.ID})
	if err != nil {
		return err
	}
	if len(con) > 0 {
		return nucleo.ReglaNegocio("formulario_con_respuestas", "El formulario ya tiene respuestas: no se puede quitar")
	}
	quitado, err := q.QuitarFormulario(ctx, campobd.QuitarFormularioParams{ID: fo.ID, QuitadoEn: &cuando, QuitadoPor: actorUsuario(t)})
	if err != nil {
		return err
	}
	t.RegistrarCambios("orden_formulario", fo.ID.String(), fo, quitado)
	v, err := q.VersionPorID(ctx, fo.VersionID)
	if err != nil {
		return err
	}
	if err := historial(ctx, t, q, f.ID, "formulario", nil, nil, nil, map[string]any{
		"accion": "quitado", "formulario_id": fo.ID, "plantilla_id": fo.PlantillaID, "version_id": fo.VersionID,
		"numero_version": v.Numero, "nombre": v.Nombre, "origen": fo.Origen,
	}, cuando); err != nil {
		return err
	}
	return t.Emitir("orden.formulario_quitado", map[string]any{
		"orden_id": f.ID, "formulario_id": fo.ID, "plantilla_id": fo.PlantillaID, "origen": fo.Origen,
		"canal": string(contexto.CanalDe(ctx)),
	})
}

func (m *Modulo) nuevoQuitarFormulario() *auditoria.Caso[EntradaQuitarFormulario, Orden] {
	return accionOrden("ordenes.quitar_formulario", nucleo.PermOrdenesAsignarForm, sinLote[EntradaQuitarFormulario],
		func(ctx context.Context, t *auditoria.Tx, q *campobd.Queries, in EntradaQuitarFormulario) (campobd.CampoOrden, string, error) {
			f, err := tomar(ctx, t, q, in.ID, nucleo.PermOrdenesAsignarForm, &in.Version, nil)
			if err != nil {
				return f, "", err
			}
			if err := exigirEstado(f, "quitar formularios de", estadosAbiertos...); err != nil {
				return f, "", err
			}
			t.Anotar(map[string]any{"formulario_id": in.FormularioID})
			if err := quitarFormulario(ctx, t, q, f, in.FormularioID, m.ahora(), func(campobd.CampoOrdenFormulario) error { return nil }); err != nil {
				return f, "", err
			}
			f, err = tocar(ctx, t, q, f)
			return f, "", err
		})
}

func (m *Modulo) nuevoQuitarFormularioCampo() *auditoria.Caso[EntradaQuitarFormularioCampo, Orden] {
	return accionOrden("ordenes.quitar_formulario", nucleo.PermOrdenesRegistrar, func(e EntradaQuitarFormularioCampo) *OpSync { return e.Sync },
		func(ctx context.Context, t *auditoria.Tx, q *campobd.Queries, in EntradaQuitarFormularioCampo) (campobd.CampoOrden, string, error) {
			f, err := tomar(ctx, t, q, in.ID, nucleo.PermOrdenesRegistrar, nil, in.Sync)
			if err != nil {
				return f, "", err
			}
			if in.Sync == nil {
				return f, "", nucleo.Prohibido("Quitar formularios desde el panel es ordenes.asignar_formulario")
			}
			if !propia(ctx, f) {
				return f, "", nucleo.Prohibido("Solo el técnico de la orden quita formularios desde la app de campo")
			}
			if err := exigirEstado(f, "quitar formularios de", estadosAbiertos...); err != nil {
				return f, "", err
			}
			actor := contexto.ActorDe(ctx).ID
			t.Anotar(map[string]any{"formulario_id": in.FormularioID})
			if err := quitarFormulario(ctx, t, q, f, in.FormularioID, m.cuandoEn(in.Sync, f), func(fo campobd.CampoOrdenFormulario) error {
				if fo.Origen != OrigenTecnico || fo.AgregadoPor == nil || *fo.AgregadoPor != actor {
					return nucleo.NuevoProblema(http.StatusForbidden, "formulario_ajeno", "Formulario ajeno",
						"Desde la app solo se quita un formulario que agregaste tú")
				}
				return nil
			}); err != nil {
				return f, "", err
			}
			f, err = tocar(ctx, t, q, f)
			return f, "", err
		})
}

// itemDeOrden busca el ítem en los formularios vigentes de la orden: por id (identifica su
// formulario: una plantilla está una sola vez) o por código (si se repite en varios
// formularios, hace falta formularioId). Un ítem de un formulario ya quitado: 409
// formulario_quitado (despacho lo quitó mientras el técnico estaba sin señal).
func itemDeOrden(ctx context.Context, q *campobd.Queries, f campobd.CampoOrden, id *int64, codigo *string, formularioID *uuid.UUID) (campobd.CampoPlantillaItem, campobd.CampoOrdenFormulario, error) {
	invalido := campoInvalido("itemId", "El ítem no es de un formulario de la orden", "item")
	quitado := nucleo.Conflicto("formulario_quitado", "El formulario de ese ítem se quitó de la orden")
	var item campobd.CampoPlantillaItem
	var fo campobd.CampoOrdenFormulario
	switch {
	case id != nil:
		it, err := q.ItemPorID(ctx, *id)
		if errors.Is(err, pgx.ErrNoRows) {
			return item, fo, invalido
		}
		if err != nil {
			return item, fo, err
		}
		fo, err = q.FormularioDeVersion(ctx, campobd.FormularioDeVersionParams{OrdenID: f.ID, VersionID: it.VersionID})
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return item, fo, invalido
		case err != nil:
			return item, fo, err
		case formularioID != nil && *formularioID != fo.ID:
			return item, fo, campoInvalido("formularioId", "El ítem no es de ese formulario", "formulario")
		case fo.QuitadoEn != nil:
			return item, fo, quitado
		}
		return it, fo, nil
	case codigo != nil:
		c := strings.TrimSpace(*codigo)
		forms, err := q.FormulariosDeOrdenes(ctx, []int64{f.ID})
		if err != nil {
			return item, fo, err
		}
		if formularioID != nil {
			uno, err := q.FormularioPorID(ctx, *formularioID)
			switch {
			case errors.Is(err, pgx.ErrNoRows) || (err == nil && uno.OrdenID != f.ID):
				return item, fo, campoInvalido("formularioId", "El formulario no es de esta orden", "formulario")
			case err != nil:
				return item, fo, err
			case uno.QuitadoEn != nil:
				return item, fo, quitado
			}
			forms = []campobd.CampoOrdenFormulario{uno}
		}
		versiones := make([]int64, 0, len(forms))
		for _, x := range forms {
			versiones = append(versiones, x.VersionID)
		}
		items, err := q.ItemsDeVersiones(ctx, versiones)
		if err != nil {
			return item, fo, err
		}
		encontrados := 0
		for _, it := range items {
			if it.Codigo != c {
				continue
			}
			for _, x := range forms {
				if x.VersionID == it.VersionID {
					item, fo = it, x
					encontrados++
				}
			}
		}
		switch encontrados {
		case 0:
			return item, fo, campoInvalido("codigo", "Ningún formulario de la orden tiene ese ítem", "item")
		case 1:
			return item, fo, nil
		}
		return item, fo, campoInvalido("formularioId", "Varios formularios tienen ese código: indica formularioId", "formulario")
	}
	return item, fo, campoObligatorio("itemId")
}

// requisitosFormularios: los obligatorios sin responder de cada formulario (field
// formularios.<id>.checklist.<código>, con el nombre del formulario en el mensaje).
func requisitosFormularios(d OrdenDetalle) []nucleo.ErrorCampo {
	nombres := map[uuid.UUID]string{}
	for _, fo := range d.Formularios {
		nombres[fo.ID] = fo.Nombre
	}
	var errores []nucleo.ErrorCampo
	for _, it := range d.Checklist {
		if it.Obligatorio && it.Valor == nil {
			errores = append(errores, nucleo.ErrorCampo{
				Field:   fmt.Sprintf("formularios.%s.checklist.%s", it.FormularioID, it.Codigo),
				Message: fmt.Sprintf("Falta: %s (%s)", it.Etiqueta, nombres[it.FormularioID]), Rule: "obligatorio",
			})
		}
	}
	return errores
}

// fotosObligatorias: una foto completa por ítem foto obligatorio (clave = su código; si el
// código se repite en varios formularios, una por cada uno).
func fotosObligatorias(d OrdenDetalle) []Requisito {
	r := []Requisito{}
	for _, it := range d.Checklist {
		if it.Tipo != ItemFoto || !it.Obligatorio {
			continue
		}
		i := slices.IndexFunc(r, func(x Requisito) bool { return x.ClaveChecklist == it.Codigo })
		if i >= 0 {
			r[i].Minimo++
			continue
		}
		r = append(r, Requisito{Tipo: "foto", ClaveChecklist: it.Codigo, Minimo: 1})
	}
	return r
}

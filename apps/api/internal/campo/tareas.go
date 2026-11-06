package campo

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/agenda"
	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	campobd "github.com/HCDestroyer/iris/apps/api/internal/campo/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
)

// Tareas propias del técnico desde el lote (contratos/sync.md §2 y §3). Las tareas son de agenda
// (agenda.CrearTareaEnTx…); aquí solo se reclama la operación (idempotencia por su id, como las
// de las órdenes) y se guarda su resultado, en la misma transacción. No llevan ordenId ni
// versionBase: se agrupan por tarea (un rechazo bloquea las siguientes de esa tarea, nunca una
// orden). En tarea.crear el id de la operación es el id de la tarea.

// Tipos de operación de las tareas.
const (
	OpTareaCrear     = "tarea.crear"
	OpTareaCompletar = "tarea.completar"
	OpTareaAnular    = "tarea.anular"
)

// AvisoSolapaCon: la tarea se guardó, pero choca con una cita del técnico (resultado.tarea.solapaCon).
const AvisoSolapaCon = "solapa_con"

func esOpTarea(tipo string) bool {
	return tipo == OpTareaCrear || tipo == OpTareaCompletar || tipo == OpTareaAnular
}

// DatosTareaLote son los datos de tarea.completar.
type DatosTareaLote struct {
	TareaID uuid.UUID `json:"tareaId" format:"uuid" doc:"La tarea (el id de su tarea.crear)"`
}

// DatosAnularTareaLote son los datos de tarea.anular.
type DatosAnularTareaLote struct {
	TareaID uuid.UUID `json:"tareaId" format:"uuid" doc:"La tarea (el id de su tarea.crear)"`
	Motivo  *string   `json:"motivo,omitempty" maxLength:"500"`
}

type entradaTareaLote struct {
	sync     *OpSync
	crear    agenda.DatosTarea
	tareaID  uuid.UUID
	motivo   *string
	ejecutar func(ctx context.Context, t *auditoria.Tx, in entradaTareaLote) (agenda.RespuestaTarea, error)
}

func (m *Modulo) nuevoTareaLote(accion string, permiso nucleo.Permiso) *auditoria.Caso[entradaTareaLote, agenda.RespuestaTarea] {
	return auditoria.Nuevo(auditoria.Caso[entradaTareaLote, agenda.RespuestaTarea]{
		Accion: accion, Permiso: permiso,
		AuditarError: func(err error) bool {
			var d *duplicada
			return !errors.As(err, &d)
		},
		Logica: func(ctx context.Context, t *auditoria.Tx, in entradaTareaLote) (agenda.RespuestaTarea, error) {
			q := campobd.New(t)
			if err := reclamar(ctx, q, in.sync, nil); err != nil {
				return agenda.RespuestaTarea{}, err
			}
			t.Anotar(map[string]any{"operacion_id": in.sync.ID, "dispositivo_id": in.sync.DispositivoID, "tipo": in.sync.Tipo,
				"ocurrido_en": in.sync.OcurridoEn, "ocurrido_en_telefono": in.sync.OcurridoEnTelefono})
			r, err := in.ejecutar(ctx, t, in)
			if err != nil {
				return r, sinDeadlock(err)
			}
			res := ResultadoOperacion{ID: in.sync.ID.String(), Estado: "aplicada", Aviso: avisoTarea(r), Tarea: &r}
			crudo, err := json.Marshal(res)
			if err != nil {
				return r, err
			}
			return r, q.FijarResultadoOperacion(ctx, campobd.FijarResultadoOperacionParams{ID: in.sync.ID, Resultado: crudo})
		},
	})
}

func avisoTarea(r agenda.RespuestaTarea) *string {
	switch {
	case r.Aviso != nil:
		return r.Aviso
	case len(r.SolapaCon) > 0 && r.Tarea.Estado == agenda.TareaPendiente:
		return ptr(AvisoSolapaCon)
	}
	return nil
}

// aplicarTarea corre tarea.crear, tarea.completar o tarea.anular con su caso de uso.
func (m *Modulo) aplicarTarea(ctx context.Context, s *OpSync, op OperacionLote) ResultadoOperacion {
	if err := sinNulos(op.Datos); err != nil {
		p, _ := nucleo.AProblema(err)
		return m.rechazar(ctx, s, 0, p)
	}
	agd := m.cfg.Agenda
	var caso *auditoria.Caso[entradaTareaLote, agenda.RespuestaTarea]
	in := entradaTareaLote{sync: s}
	var err error
	switch op.Tipo {
	case OpTareaCrear:
		caso = m.TareaCrearLote
		in.crear, err = datosDe[agenda.DatosTarea](op)
		in.ejecutar = func(ctx context.Context, t *auditoria.Tx, in entradaTareaLote) (agenda.RespuestaTarea, error) {
			return agd.CrearTareaEnTx(ctx, t, agenda.EntradaCrearTarea{ID: in.sync.ID, DatosTarea: in.crear, Origen: "campo"})
		}
	case OpTareaCompletar:
		caso = m.TareaCompletarLote
		var d DatosTareaLote
		d, err = datosDe[DatosTareaLote](op)
		in.tareaID = d.TareaID
		in.ejecutar = func(ctx context.Context, t *auditoria.Tx, in entradaTareaLote) (agenda.RespuestaTarea, error) {
			return agd.CompletarTareaEnTx(ctx, t, agenda.EntradaCompletarTarea{ID: in.tareaID, SinVersion: true, En: in.sync.OcurridoEn})
		}
	default:
		caso = m.TareaAnularLote
		var d DatosAnularTareaLote
		d, err = datosDe[DatosAnularTareaLote](op)
		in.tareaID, in.motivo = d.TareaID, d.Motivo
		in.ejecutar = func(ctx context.Context, t *auditoria.Tx, in entradaTareaLote) (agenda.RespuestaTarea, error) {
			return agd.AnularTareaEnTx(ctx, t, agenda.EntradaAnularTarea{ID: in.tareaID, Motivo: in.motivo, SinVersion: true, En: in.sync.OcurridoEn})
		}
	}
	if err == nil {
		var r agenda.RespuestaTarea
		r, err = caso.Ejecutar(ctx, in)
		if err == nil {
			return ResultadoOperacion{ID: s.ID.String(), Estado: "aplicada", Aviso: avisoTarea(r), Tarea: &r}
		}
	}
	var d *duplicada
	if errors.As(err, &d) {
		r := d.resultado
		r.Estado = "duplicada"
		return r
	}
	p, _ := nucleo.AProblema(err)
	if codigosReintentables[p.Codigo()] {
		return reintentable(s.ID.String(), p)
	}
	return m.rechazar(ctx, s, 0, p)
}

// tareaDe: el id de la tarea de una operación del lote (vacío si no se puede leer).
func tareaDe(op OperacionLote) string {
	if op.Tipo == OpTareaCrear {
		return op.ID
	}
	if v, ok := op.Datos["tareaId"].(string); ok {
		if id, err := uuid.Parse(v); err == nil {
			return id.String()
		}
	}
	return ""
}

// OrdenParaTarea implementa agenda.OrdenesTareas: la orden ligada a una tarea existe, es de la
// empresa activa y el actor la ve (ordenes.ver con alcance, o propia con ordenes.ver_propias).
func (m *Modulo) OrdenParaTarea(ctx context.Context, t *auditoria.Tx, ordenID int64) (agenda.OrdenParaTarea, error) {
	f, err := campobd.New(t).OrdenPorID(ctx, ordenID)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && (!enEmpresa(ctx, f) || !visible(ctx, f))) {
		return agenda.OrdenParaTarea{}, agenda.TareaOrdenInvalida()
	}
	if err != nil {
		return agenda.OrdenParaTarea{}, err
	}
	return agenda.OrdenParaTarea{ID: f.ID, Numero: f.Numero, EmpresaID: f.EmpresaID, ClienteID: f.ClienteID}, nil
}

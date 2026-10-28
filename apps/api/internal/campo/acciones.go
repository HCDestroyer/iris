package campo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	campobd "github.com/HCDestroyer/iris/apps/api/internal/campo/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
)

// Tipos de operación del lote (contratos/sync.md §2).
const (
	OpIniciarCamino    = "orden.iniciar_camino"
	OpLlegar           = "orden.llegar"
	OpChecklistMarcar  = "checklist.marcar"
	OpNotaAgregar      = "nota.agregar"
	OpMaterialUsar     = "material.usar"
	OpEquipoInstalar   = "equipo.instalar"
	OpEquipoRetirar    = "equipo.retirar"
	OpFirmaCapturar    = "firma.capturar"
	OpAdjuntoRegistrar = "adjunto.registrar"
	OpCerrar           = "orden.cerrar"
	OpFallar           = "orden.fallar"
)

// Avisos de una acción aplicada.
const (
	AvisoChecklistNoVigente     = "checklist_no_vigente"
	AvisoExistenciaInsuficiente = "existencia_insuficiente"
)

// duplicada: la operación del lote ya se aplicó (su resultado original). No es un error de la
// acción: no se audita y el lote la responde como «duplicada».
type duplicada struct{ resultado ResultadoOperacion }

func (d *duplicada) Error() string { return "operación duplicada" }

// logicaOrden es el cuerpo de una acción sobre una orden: devuelve la orden guardada y, si
// aplica, un aviso.
type logicaOrden[In any] func(ctx context.Context, t *auditoria.Tx, q *campobd.Queries, in In) (campobd.CampoOrden, string, error)

// accionOrden arma el caso de uso de una acción sobre una orden, desde el panel o desde el lote.
// Desde el lote (op ≠ nil) reclama la operación en la misma transacción (idempotencia por id:
// una ya aplicada responde «duplicada» sin volver a aplicarse, también con dos lotes a la vez)
// y guarda su resultado; la auditoría es la de la acción, con el id de la operación anotado.
func accionOrden[In any](accion string, permiso nucleo.Permiso, op func(In) *OpSync, logica logicaOrden[In]) *auditoria.Caso[In, Orden] {
	return auditoria.Nuevo(auditoria.Caso[In, Orden]{
		Accion:  accion,
		Permiso: permiso,
		AuditarError: func(err error) bool {
			var d *duplicada
			return !errors.As(err, &d)
		},
		Logica: func(ctx context.Context, t *auditoria.Tx, in In) (Orden, error) {
			if err := sinNulos(in); err != nil {
				return Orden{}, err
			}
			q := campobd.New(t)
			s := op(in)
			if s != nil {
				if err := reclamar(ctx, q, s, ordenDeEntrada(in)); err != nil {
					return Orden{}, err
				}
				t.Anotar(map[string]any{"operacion_id": s.ID, "dispositivo_id": s.DispositivoID, "tipo": s.Tipo,
					"ocurrido_en": s.OcurridoEn, "ocurrido_en_telefono": s.OcurridoEnTelefono})
			}
			f, aviso, err := logica(ctx, t, q, in)
			if err != nil {
				return Orden{}, err
			}
			tipo, err := tipoDe(ctx, q, f)
			if err != nil {
				return Orden{}, err
			}
			o := aOrden(f, tipo.Codigo)
			if aviso != "" {
				o.Aviso = &aviso
			}
			if s != nil {
				res := ResultadoOperacion{ID: s.ID.String(), Estado: "aplicada", Aviso: o.Aviso,
					Orden: &EstadoOrden{Version: o.Version, Estado: o.Estado}}
				crudo, err := json.Marshal(res)
				if err != nil {
					return Orden{}, err
				}
				if err := q.FijarResultadoOperacion(ctx, campobd.FijarResultadoOperacionParams{ID: s.ID, Resultado: crudo}); err != nil {
					return Orden{}, err
				}
			}
			return o, nil
		},
	})
}

// conOrden lo implementan las entradas de las acciones (el id de la orden, para el registro
// de la operación).
type conOrden interface{ ordenID() int64 }

func ordenDeEntrada(in any) *int64 {
	if c, ok := in.(conOrden); ok {
		id := c.ordenID()
		return &id
	}
	return nil
}

// reclamar registra la operación como aplicada en la transacción de la acción. Si ya estaba
// aplicada (este u otro lote), devuelve duplicada con su resultado original; si es de otro
// usuario, 409.
func reclamar(ctx context.Context, q *campobd.Queries, s *OpSync, ordenID *int64) error {
	actor := contexto.ActorDe(ctx)
	if !actor.EsUsuario() {
		return nucleo.Prohibido("El lote de campo es de un usuario")
	}
	n, err := q.ReclamarOperacion(ctx, campobd.ReclamarOperacionParams{
		ID: s.ID, DispositivoID: s.DispositivoID, UsuarioID: actor.ID, OrdenID: ordenID, Tipo: s.Tipo,
		VersionBase: s.VersionBase, OcurridoEnTelefono: s.OcurridoEnTelefono, OcurridoEn: &s.OcurridoEn,
	})
	if err != nil {
		return err
	}
	if n == 1 {
		return nil
	}
	previa, err := q.OperacionPorID(ctx, s.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("campo: la operación %s no se pudo reclamar", s.ID)
	}
	if err != nil {
		return err
	}
	if previa.UsuarioID != actor.ID {
		return nucleo.Conflicto("operacion_ajena", "Ese id de operación ya lo usó otro usuario")
	}
	return &duplicada{resultado: resultadoGuardado(previa)}
}

// resultadoGuardado lee el resultado original de una operación aplicada.
func resultadoGuardado(f campobd.CampoOperacionesSync) ResultadoOperacion {
	var r ResultadoOperacion
	if err := json.Unmarshal(f.Resultado, &r); err != nil || r.ID == "" {
		r = ResultadoOperacion{ID: f.ID.String(), Estado: f.Estado}
	}
	return r
}

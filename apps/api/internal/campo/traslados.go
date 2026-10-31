package campo

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	campobd "github.com/HCDestroyer/iris/apps/api/internal/campo/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/inventario"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
)

// Recepción de órdenes de traslado desde el lote (contrato Iris ↔ ArgosISP 0.5.0 §3.3; lo escribió
// I1 sobre el lote de campo). La operación traslado.recibir no es de una orden: no lleva ordenId ni
// versionBase, se agrupa por traslado (un rechazo bloquea las siguientes del mismo traslado, nunca
// una orden) y es idempotente por el id de la operación. La regla de conflictos está en
// contratos/sync.md §3.

// Recepciones encola la recepción (en la transacción de la operación) y lee su estado para cambios.
// Lo implementa inventario.Modulo (I1).
type Recepciones interface {
	Encolar(ctx context.Context, t *auditoria.Tx, in inventario.EntradaRecibirTraslado) (inventario.EstadoRecepcion, error)
	Recepciones(ctx context.Context, q inventario.DBTX, usuarioID int64) ([]inventario.EstadoRecepcion, error)
}

type entradaRecibirTraslado struct {
	inventario.EntradaRecibirTraslado
	sync *OpSync
}

func (m *Modulo) nuevoRecibirTraslado() *auditoria.Caso[entradaRecibirTraslado, inventario.EstadoRecepcion] {
	return auditoria.Nuevo(auditoria.Caso[entradaRecibirTraslado, inventario.EstadoRecepcion]{
		Accion: "campo.recibir_traslado", Permiso: nucleo.PermCampoRecibirTraslado,
		AuditarError: func(err error) bool {
			var d *duplicada
			return !errors.As(err, &d)
		},
		Logica: func(ctx context.Context, t *auditoria.Tx, in entradaRecibirTraslado) (inventario.EstadoRecepcion, error) {
			q := campobd.New(t)
			if err := reclamar(ctx, q, in.sync, nil); err != nil {
				return inventario.EstadoRecepcion{}, err
			}
			t.Anotar(map[string]any{"operacion_id": in.sync.ID, "dispositivo_id": in.sync.DispositivoID, "tipo": in.sync.Tipo,
				"ocurrido_en": in.sync.OcurridoEn})
			if m.cfg.Recepciones == nil {
				return inventario.EstadoRecepcion{}, argos.ConectorApagado()
			}
			r, err := m.cfg.Recepciones.Encolar(ctx, t, in.EntradaRecibirTraslado)
			if err != nil {
				return r, sinDeadlock(err)
			}
			res := ResultadoOperacion{ID: in.sync.ID.String(), Estado: "aplicada", Recepcion: &r}
			crudo, err := json.Marshal(res)
			if err != nil {
				return r, err
			}
			return r, q.FijarResultadoOperacion(ctx, campobd.FijarResultadoOperacionParams{ID: in.sync.ID, Resultado: crudo})
		},
	})
}

// aplicarTraslado corre traslado.recibir con su caso de uso.
func (m *Modulo) aplicarTraslado(ctx context.Context, s *OpSync, op OperacionLote) ResultadoOperacion {
	if err := sinNulos(op.Datos); err != nil {
		p, _ := nucleo.AProblema(err)
		return m.rechazar(ctx, s, 0, p)
	}
	e, err := datosDe[inventario.EntradaRecibirTraslado](op)
	if err == nil {
		var r inventario.EstadoRecepcion
		r, err = m.RecibirTraslado.Ejecutar(ctx, entradaRecibirTraslado{EntradaRecibirTraslado: e, sync: s})
		if err == nil {
			return ResultadoOperacion{ID: s.ID.String(), Estado: "aplicada", Recepcion: &r}
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

// grupoDe: la orden de la operación o, en traslado.recibir, el traslado como clave negativa.
func grupoDe(op OperacionLote) int64 {
	if op.Tipo != OpTrasladoRecibir {
		return op.OrdenID
	}
	if v, ok := op.Datos["trasladoId"]; ok {
		if n, err := strconv.ParseInt(stringDe(v), 10, 64); err == nil && n > 0 {
			return -n
		}
	}
	return -1 << 62
}

func stringDe(v any) string {
	switch x := v.(type) {
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case string:
		return x
	case json.Number:
		return x.String()
	}
	return ""
}

func queBloquea(op OperacionLote) string {
	if op.Tipo == OpTrasladoRecibir {
		return "este traslado"
	}
	return "esta orden"
}

// Package integracion: outbox de eventos, relay (River) y, más adelante, los conectores con
// ArgosISP y Horus Flow (webhooks, pendientes, instantáneas).
package integracion

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	integracionbd "github.com/HCDestroyer/iris/apps/api/internal/integracion/bd"
)

// CanalNotificacion es el canal de LISTEN/NOTIFY que despierta al relay. El NOTIFY sale con el
// COMMIT de la transacción que escribió el evento (Postgres no lo entrega si hay ROLLBACK).
const CanalNotificacion = "iris_outbox"

// Evento es un hecho de negocio tal como viaja a consumidores y webhooks:
// { id (uuid v7), evento, ocurrido_en, empresa_id, payload }.
type Evento struct {
	ID         uuid.UUID       `json:"id"`
	Evento     string          `json:"evento"`
	OcurridoEn time.Time       `json:"ocurrido_en"`
	EmpresaID  *int64          `json:"empresa_id"`
	Payload    json.RawMessage `json:"payload"`
	// RequestID de la acción que lo emitió (no viaja en webhooks; sirve para rastrear).
	RequestID string `json:"-"`
	// Intentos de entrega previos (0 en la primera).
	Intentos int32 `json:"-"`
}

// Insertar escribe el evento en integracion.outbox dentro de tx. Úsalo solo desde la base de
// casos de uso (auditoria.Tx.Emitir), que ya pone empresa y request id.
func Insertar(ctx context.Context, tx pgx.Tx, evento string, empresaID *int64, requestID string, payload any) (Evento, error) {
	cuerpo, err := json.Marshal(payload)
	if err != nil {
		return Evento{}, fmt.Errorf("outbox: payload de %s: %w", evento, err)
	}
	if string(cuerpo) == "null" {
		cuerpo = []byte("{}")
	}
	id, err := uuid.NewV7()
	if err != nil {
		return Evento{}, err
	}
	e := Evento{
		ID: id, Evento: evento, OcurridoEn: time.Now().UTC().Truncate(time.Microsecond),
		EmpresaID: empresaID, Payload: cuerpo, RequestID: requestID,
	}
	if err := integracionbd.New(tx).InsertarEvento(ctx, integracionbd.InsertarEventoParams{
		ID: e.ID, Evento: e.Evento, EmpresaID: e.EmpresaID, Payload: e.Payload,
		OcurridoEn: e.OcurridoEn, RequestID: e.RequestID,
	}); err != nil {
		return Evento{}, fmt.Errorf("outbox: insertar %s: %w", evento, err)
	}
	if _, err := tx.Exec(ctx, "SELECT pg_notify($1, '')", CanalNotificacion); err != nil {
		return Evento{}, fmt.Errorf("outbox: notificar: %w", err)
	}
	return e, nil
}

func desdeFila(f integracionbd.IntegracionOutbox) Evento {
	return Evento{
		ID: f.ID, Evento: f.Evento, OcurridoEn: f.OcurridoEn, EmpresaID: f.EmpresaID,
		Payload: f.Payload, RequestID: f.RequestID, Intentos: f.Intentos,
	}
}

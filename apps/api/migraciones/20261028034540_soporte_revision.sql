-- +goose Up

-- Alertas de SLA: una por ticket, reloj, umbral y vencimiento. Si el vencimiento se corre
-- (reanudar, reabrir, escalar), el nuevo plazo vuelve a alertar; con el mismo, el cron sigue
-- siendo idempotente.
ALTER TABLE soporte.ticket_sla_alertas DROP CONSTRAINT ticket_sla_alertas_pkey;
ALTER TABLE soporte.ticket_sla_alertas ADD PRIMARY KEY (ticket_id, sla, umbral, vence_en);

-- Idempotencia de las acciones que crean filas: «<credencial>|<Idempotency-Key>». Si la
-- acción se repite tras un reclamo de la clave (el proceso cayó entre el COMMIT y guardar la
-- respuesta), se devuelve la fila existente en vez de duplicarla.
ALTER TABLE soporte.tickets ADD COLUMN idempotencia text;
CREATE UNIQUE INDEX tickets_idempotencia_idx ON soporte.tickets (idempotencia) WHERE idempotencia IS NOT NULL;

ALTER TABLE soporte.ticket_mensajes ADD COLUMN idempotencia text;
CREATE UNIQUE INDEX ticket_mensajes_idempotencia_idx ON soporte.ticket_mensajes (ticket_id, idempotencia)
    WHERE idempotencia IS NOT NULL;

-- +goose Down
DROP INDEX soporte.ticket_mensajes_idempotencia_idx;
ALTER TABLE soporte.ticket_mensajes DROP COLUMN idempotencia;
DROP INDEX soporte.tickets_idempotencia_idx;
ALTER TABLE soporte.tickets DROP COLUMN idempotencia;
DELETE FROM soporte.ticket_sla_alertas a
USING soporte.ticket_sla_alertas b
WHERE a.ticket_id = b.ticket_id AND a.sla = b.sla AND a.umbral = b.umbral
  AND (a.created_at, a.vence_en) > (b.created_at, b.vence_en);
ALTER TABLE soporte.ticket_sla_alertas DROP CONSTRAINT ticket_sla_alertas_pkey;
ALTER TABLE soporte.ticket_sla_alertas ADD PRIMARY KEY (ticket_id, sla, umbral);

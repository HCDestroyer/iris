-- +goose Up
-- Encuestas (revisión de la PR #41): una encuesta pendiente se anula si su ticket se reabre o se
-- fusiona (la siguiente resolución genera otra), y se guarda desde dónde se respondió.
ALTER TABLE soporte.encuestas
    DROP CONSTRAINT encuestas_estado_check,
    ADD CONSTRAINT encuestas_estado_check CHECK (estado IN ('pendiente', 'respondida', 'anulada')),
    ADD COLUMN anulada_en            timestamptz,
    ADD COLUMN motivo_anulacion      text CHECK (motivo_anulacion IN ('reabierto', 'fusionado')),
    ADD COLUMN respondida_ip         text,
    ADD COLUMN respondida_user_agent text CHECK (char_length(respondida_user_agent) <= 255),
    ADD CONSTRAINT encuestas_anulada_ck CHECK ((estado = 'anulada') = (anulada_en IS NOT NULL)
                                               AND (estado = 'anulada') = (motivo_anulacion IS NOT NULL));
DROP INDEX soporte.encuestas_ticket_uq;
DROP INDEX soporte.encuestas_orden_uq;
CREATE UNIQUE INDEX encuestas_ticket_uq ON soporte.encuestas (ticket_id) WHERE ticket_id IS NOT NULL AND estado <> 'anulada';
CREATE UNIQUE INDEX encuestas_orden_uq ON soporte.encuestas (orden_id) WHERE orden_id IS NOT NULL AND estado <> 'anulada';

-- +goose Down
DELETE FROM soporte.correos_salientes s USING soporte.encuestas e WHERE s.encuesta_id = e.id AND e.estado = 'anulada';
DELETE FROM soporte.encuestas WHERE estado = 'anulada';
DROP INDEX soporte.encuestas_orden_uq;
DROP INDEX soporte.encuestas_ticket_uq;
CREATE UNIQUE INDEX encuestas_ticket_uq ON soporte.encuestas (ticket_id) WHERE ticket_id IS NOT NULL;
CREATE UNIQUE INDEX encuestas_orden_uq ON soporte.encuestas (orden_id) WHERE orden_id IS NOT NULL;
ALTER TABLE soporte.encuestas
    DROP CONSTRAINT encuestas_anulada_ck,
    DROP COLUMN respondida_user_agent,
    DROP COLUMN respondida_ip,
    DROP COLUMN motivo_anulacion,
    DROP COLUMN anulada_en,
    DROP CONSTRAINT encuestas_estado_check,
    ADD CONSTRAINT encuestas_estado_check CHECK (estado IN ('pendiente', 'respondida'));

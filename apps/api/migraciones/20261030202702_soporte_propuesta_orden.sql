-- +goose Up
-- Soporte (I2): la propuesta de resolver un ticket también nace del cierre de su orden de
-- trabajo (campo), no solo de Horus.
--
-- - tickets.propuesta_origen: quién propuso ('horus' | 'orden'). Las propuestas que ya había
--   son de Horus.
-- - tickets.propuesta_orden_id: la orden cuyo cierre la propuso (sin llave foránea: campo
--   referencia a soporte, nunca al revés). Si la orden se reabre, la propuesta se descarta.
ALTER TABLE soporte.tickets
    ADD COLUMN propuesta_origen   text CHECK (propuesta_origen IN ('horus', 'orden')),
    ADD COLUMN propuesta_orden_id bigint;

UPDATE soporte.tickets SET propuesta_origen = 'horus' WHERE propuesta IS NOT NULL;

ALTER TABLE soporte.tickets
    ADD CONSTRAINT tickets_propuesta_origen_datos_check CHECK (
        (propuesta IS NULL) = (propuesta_origen IS NULL)
        AND (propuesta_orden_id IS NULL OR propuesta_origen = 'orden'));

-- +goose Down
ALTER TABLE soporte.tickets
    DROP CONSTRAINT tickets_propuesta_origen_datos_check,
    DROP COLUMN propuesta_orden_id,
    DROP COLUMN propuesta_origen;

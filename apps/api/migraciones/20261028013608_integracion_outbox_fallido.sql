-- +goose Up
-- Un evento que agotó sus intentos queda fallido: el relay deja de tomarlo (se revisa a mano;
-- para reintentarlo, fallido_en = NULL y proximo_intento = now()).
ALTER TABLE integracion.outbox ADD COLUMN fallido_en timestamptz;
DROP INDEX integracion.outbox_pendientes_idx;
CREATE INDEX outbox_pendientes_idx ON integracion.outbox (proximo_intento, id)
    WHERE publicado_en IS NULL AND fallido_en IS NULL;

-- +goose Down
DROP INDEX integracion.outbox_pendientes_idx;
CREATE INDEX outbox_pendientes_idx ON integracion.outbox (proximo_intento, id) WHERE publicado_en IS NULL;
ALTER TABLE integracion.outbox DROP COLUMN fallido_en;

-- +goose Up
-- Revisión de seguridad de la #160 (B3): la resolución que avisa el webhook de Argos se confirma
-- releyendo el comprobante (GET /comprobantes/:id) antes de aplicarla, con intentos contados y espera
-- creciente; y un comprobante es de una sola boleta por empresa.
ALTER TABLE mensajeria.boletas
    ADD COLUMN resolucion_proximo    timestamptz,
    ADD COLUMN resolucion_intentos   integer NOT NULL DEFAULT 0,
    ADD COLUMN resolucion_error      text CHECK (resolucion_error IS NULL OR char_length(resolucion_error) <= 300),
    -- Se agotaron los intentos de confirmar con Argos: queda marcada hasta que alguien la abra o
    -- llegue otro webhook.
    ADD COLUMN resolucion_agotada_en timestamptz;
UPDATE mensajeria.boletas SET resolucion_proximo = resolucion_en WHERE resolucion IS NOT NULL;
DROP INDEX mensajeria.boletas_por_resolver_idx;
CREATE INDEX boletas_por_resolver_idx ON mensajeria.boletas (resolucion_proximo) WHERE resolucion IS NOT NULL AND resolucion_agotada_en IS NULL;
DROP INDEX mensajeria.boletas_comprobante_idx;
CREATE UNIQUE INDEX boletas_comprobante_uidx ON mensajeria.boletas (coalesce(empresa_id, 0), comprobante_id) WHERE comprobante_id IS NOT NULL;

-- +goose Down
DROP INDEX mensajeria.boletas_comprobante_uidx;
CREATE INDEX boletas_comprobante_idx ON mensajeria.boletas (comprobante_id) WHERE comprobante_id IS NOT NULL;
DROP INDEX mensajeria.boletas_por_resolver_idx;
CREATE INDEX boletas_por_resolver_idx ON mensajeria.boletas (resolucion_en) WHERE resolucion IS NOT NULL;
ALTER TABLE mensajeria.boletas
    DROP COLUMN resolucion_agotada_en,
    DROP COLUMN resolucion_error,
    DROP COLUMN resolucion_intentos,
    DROP COLUMN resolucion_proximo;

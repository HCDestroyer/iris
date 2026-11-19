-- +goose Up
-- Un token eliminado estaba revocado o vencido: nunca se elimina uno que aún valía (revisión de
-- seguridad de #229, M5). Con vencimiento basta expira_en no nulo: el UPDATE de EliminarToken ya
-- exige que haya pasado, con la hora de la base.
ALTER TABLE nucleo.tokens_api ADD CONSTRAINT tokens_api_eliminado_inactivo
    CHECK (eliminado_en IS NULL OR revocado_en IS NOT NULL OR expira_en IS NOT NULL);

-- +goose Down
ALTER TABLE nucleo.tokens_api DROP CONSTRAINT tokens_api_eliminado_inactivo;

-- +goose NO TRANSACTION
-- +goose Up
-- Valida, fuera de una transacción, las restricciones que …_asistente_pausa agregó sin validar:
-- VALIDATE recorre la tabla una vez con un candado que no bloquea lecturas ni escrituras.
ALTER TABLE mensajeria.conversaciones VALIDATE CONSTRAINT conversaciones_asistente_pausado_por_fk;
ALTER TABLE mensajeria.conversaciones VALIDATE CONSTRAINT conversaciones_asistente_pausa_motivo_ck;
ALTER TABLE mensajeria.conversaciones VALIDATE CONSTRAINT conversaciones_asistente_pausa_ck;

-- +goose Down
-- Nada que deshacer: una restricción validada no se «desvalida».
SELECT 1;

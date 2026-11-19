-- +goose NO TRANSACTION
-- +goose Up
-- Valida, fuera de una transacción, la restricción que …_voz_rafaga_levantada agregó sin validar:
-- VALIDATE recorre la tabla una vez con un candado que no bloquea lecturas ni escrituras.
ALTER TABLE mensajeria.llamadas VALIDATE CONSTRAINT llamadas_rafaga_intentos_ck;

-- +goose Down
-- Nada que deshacer: una restricción validada no se «desvalida».
SELECT 1;

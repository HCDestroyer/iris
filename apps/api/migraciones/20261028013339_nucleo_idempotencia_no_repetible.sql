-- +goose Up
-- Respuestas con secretos (valor de un token, secreto TOTP) no se guardan: la clave queda
-- 'no_repetible' (status sin cuerpo) y repetirla responde 409 idempotencia_no_repetible.
-- La columna actor pasa a ser "token:<token_id>" para los actores token e integración y
-- "<tipo>:<usuario_id>" para el resto (las filas viejas vencen solas en 24 h).
ALTER TABLE nucleo.idempotencia DROP CONSTRAINT idempotencia_estado_check;
ALTER TABLE nucleo.idempotencia ADD CONSTRAINT idempotencia_estado_check
    CHECK (estado IN ('en_proceso', 'completa', 'no_repetible'));

-- +goose Down
DELETE FROM nucleo.idempotencia WHERE estado = 'no_repetible';
ALTER TABLE nucleo.idempotencia DROP CONSTRAINT idempotencia_estado_check;
ALTER TABLE nucleo.idempotencia ADD CONSTRAINT idempotencia_estado_check
    CHECK (estado IN ('en_proceso', 'completa'));

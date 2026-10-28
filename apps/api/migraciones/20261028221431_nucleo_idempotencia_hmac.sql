-- +goose Up
-- hash_peticion pasa de SHA-256 plano del cuerpo (que puede llevar contraseñas en claro) a
-- HMAC-SHA256 con una clave derivada de IRIS_CLAVE_CIFRADO (HKDF, «iris:idempotencia»).
--   hash_formato 0: SHA-256 plano (filas anteriores; se comparan igual hasta que vencen).
--   hash_formato 1: HMAC; hash_clave_id identifica la clave (huella, no la clave).
ALTER TABLE nucleo.idempotencia
    ADD COLUMN hash_formato  smallint NOT NULL DEFAULT 0 CHECK (hash_formato IN (0, 1)),
    ADD COLUMN hash_clave_id bytea;
-- Las vencidas no sirven para nada y guardan el SHA-256 plano: fuera.
DELETE FROM nucleo.idempotencia WHERE expira_en < now();

-- +goose Down
-- Las filas HMAC se conservan: el código anterior no las reconoce (compara SHA-256 plano) y
-- responde 422 ante un reintento, en vez de repetir la acción. Vencen solas en 24 h.
ALTER TABLE nucleo.idempotencia DROP COLUMN hash_clave_id, DROP COLUMN hash_formato;

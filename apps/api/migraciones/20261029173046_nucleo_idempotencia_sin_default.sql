-- +goose Up
-- hash_formato sin DEFAULT: el 0 (SHA-256 plano) solo era para las filas de antes del HMAC, que
-- ya vencieron (24 h) y la purga se llevó. Con el DEFAULT, un INSERT que olvidara la columna
-- guardaría un hash HMAC marcado como SHA-256 plano. Todo el código la escribe explícita
-- (ReservarIdempotencia y ReclamarIdempotencia, siempre 1); sin DEFAULT, olvidarla falla.
ALTER TABLE nucleo.idempotencia ALTER COLUMN hash_formato DROP DEFAULT;

-- +goose Down
-- Restaura el DEFAULT; no toca ninguna fila.
ALTER TABLE nucleo.idempotencia ALTER COLUMN hash_formato SET DEFAULT 0;

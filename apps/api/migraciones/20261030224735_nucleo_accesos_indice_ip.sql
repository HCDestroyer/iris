-- +goose NO TRANSACTION
-- +goose Up
-- Búsqueda de los fallos sin usuario por IP (deduplicación y tope por IP). CONCURRENTLY: no bloquea
-- los inicios de sesión mientras se construye (por eso va sin transacción y aparte).
CREATE INDEX CONCURRENTLY IF NOT EXISTS accesos_ip_sin_usuario_idx
    ON nucleo.accesos (ip, created_at) WHERE usuario_id IS NULL;

-- +goose Down
DROP INDEX CONCURRENTLY IF EXISTS nucleo.accesos_ip_sin_usuario_idx;

-- +goose Up
-- Búsqueda de tickets (GET /tickets?q=, I2, Fase 5): ILIKE '%…%' sobre número, asunto y nombre del
-- contacto usa índices GIN de trigramas (pg_trgm, contrib estándar de Postgres; viene en
-- postgres:16.15-alpine del compose y de CI) en vez de recorrer toda la tabla.
CREATE EXTENSION IF NOT EXISTS pg_trgm;
-- fastupdate = off: sin lista pendiente (los tickets entran de a uno; con la lista pendiente sin
-- vaciar, el planificador estima caro el índice y vuelve al recorrido completo).
CREATE INDEX tickets_numero_trgm_idx ON soporte.tickets USING gin (numero gin_trgm_ops) WITH (fastupdate = off);
CREATE INDEX tickets_asunto_trgm_idx ON soporte.tickets USING gin (asunto gin_trgm_ops) WITH (fastupdate = off);
CREATE INDEX tickets_contacto_nombre_trgm_idx ON soporte.tickets USING gin (contacto_nombre gin_trgm_ops) WITH (fastupdate = off);

-- +goose Down
DROP INDEX soporte.tickets_contacto_nombre_trgm_idx;
DROP INDEX soporte.tickets_asunto_trgm_idx;
DROP INDEX soporte.tickets_numero_trgm_idx;
DROP EXTENSION IF EXISTS pg_trgm;

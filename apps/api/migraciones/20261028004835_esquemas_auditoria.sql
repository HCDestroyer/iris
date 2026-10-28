-- +goose Up
-- Esquemas de I1 y auditoría de solo inserción (CLAUDE.md §9).
CREATE SCHEMA IF NOT EXISTS nucleo;
CREATE SCHEMA IF NOT EXISTS auditoria;
CREATE SCHEMA IF NOT EXISTS integracion;

-- Una fila por ejecución de un caso de uso (éxito o error).
CREATE TABLE auditoria.acciones (
    id           bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    actor_tipo   text        NOT NULL CHECK (actor_tipo IN ('usuario', 'token', 'integracion', 'sistema', 'anonimo')),
    actor_id     bigint,
    actor_nombre text        NOT NULL DEFAULT '',
    token_id     bigint,
    canal        text        NOT NULL,
    accion       text        NOT NULL,
    entidad      text,
    entidad_id   text,
    empresa_id   bigint,
    entrada      jsonb       NOT NULL DEFAULT 'null'::jsonb,
    resultado    text        NOT NULL CHECK (resultado IN ('ok', 'error')),
    error        jsonb,
    request_id   text        NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX acciones_entidad_idx ON auditoria.acciones (entidad, entidad_id);
CREATE INDEX acciones_actor_idx ON auditoria.acciones (actor_tipo, actor_id, created_at);
CREATE INDEX acciones_request_idx ON auditoria.acciones (request_id);

-- Diff por campo; lo sensible se guarda como "***".
CREATE TABLE auditoria.cambios (
    id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    accion_id  bigint      NOT NULL REFERENCES auditoria.acciones (id),
    entidad    text        NOT NULL,
    entidad_id text        NOT NULL,
    campo      text        NOT NULL,
    antes      jsonb,
    despues    jsonb,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX cambios_accion_idx ON auditoria.cambios (accion_id);
CREATE INDEX cambios_entidad_idx ON auditoria.cambios (entidad, entidad_id);

-- Nunca UPDATE, DELETE ni TRUNCATE sobre auditoria.*
-- +goose StatementBegin
CREATE FUNCTION auditoria.solo_insercion() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'auditoria.% es de solo inserción (% prohibido)', TG_TABLE_NAME, TG_OP
        USING ERRCODE = 'insufficient_privilege';
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER acciones_solo_insercion BEFORE UPDATE OR DELETE ON auditoria.acciones
    FOR EACH ROW EXECUTE FUNCTION auditoria.solo_insercion();
CREATE TRIGGER acciones_sin_truncate BEFORE TRUNCATE ON auditoria.acciones
    FOR EACH STATEMENT EXECUTE FUNCTION auditoria.solo_insercion();
CREATE TRIGGER cambios_solo_insercion BEFORE UPDATE OR DELETE ON auditoria.cambios
    FOR EACH ROW EXECUTE FUNCTION auditoria.solo_insercion();
CREATE TRIGGER cambios_sin_truncate BEFORE TRUNCATE ON auditoria.cambios
    FOR EACH STATEMENT EXECUTE FUNCTION auditoria.solo_insercion();

-- +goose Down
DROP TABLE auditoria.cambios;
DROP TABLE auditoria.acciones;
DROP FUNCTION auditoria.solo_insercion();
DROP SCHEMA integracion;
DROP SCHEMA auditoria;
DROP SCHEMA nucleo;

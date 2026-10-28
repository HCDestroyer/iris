-- +goose Up
-- Idempotency-Key (CLAUDE.md §6): misma clave y mismo cuerpo → misma respuesta;
-- misma clave y otro cuerpo → 422.
CREATE TABLE nucleo.idempotencia (
    actor         text        NOT NULL,                       -- "<tipo>:<id>" del actor
    clave         text        NOT NULL,
    metodo        text        NOT NULL,
    ruta          text        NOT NULL,
    hash_peticion bytea       NOT NULL,
    estado        text        NOT NULL DEFAULT 'en_proceso' CHECK (estado IN ('en_proceso', 'completa')),
    status        integer,
    content_type  text,
    respuesta     bytea,
    expira_en     timestamptz NOT NULL,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (actor, clave)
);
CREATE INDEX idempotencia_expira_idx ON nucleo.idempotencia (expira_en);

-- +goose Down
DROP TABLE nucleo.idempotencia;

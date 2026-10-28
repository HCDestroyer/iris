-- +goose Up
-- Eventos escritos en la misma transacción que el cambio (CLAUDE.md §3).
-- El relay (River, único) los entrega a los suscriptores y marca publicado_en.
CREATE TABLE integracion.outbox (
    id              uuid        PRIMARY KEY,               -- uuid v7: ordena por tiempo
    evento          text        NOT NULL,
    empresa_id      bigint,
    payload         jsonb       NOT NULL,
    ocurrido_en     timestamptz NOT NULL DEFAULT now(),
    request_id      text        NOT NULL DEFAULT '',
    publicado_en    timestamptz,
    intentos        integer     NOT NULL DEFAULT 0,
    proximo_intento timestamptz NOT NULL DEFAULT now(),
    ultimo_error    text,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX outbox_pendientes_idx ON integracion.outbox (proximo_intento, id) WHERE publicado_en IS NULL;
CREATE INDEX outbox_evento_idx ON integracion.outbox (evento, ocurrido_en);

-- +goose Down
DROP TABLE integracion.outbox;

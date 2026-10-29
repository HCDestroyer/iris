-- +goose Up
-- Autologin desde Argos («Ver en Iris», contrato argosisp.md §1.3 → Horus §1.5). El servidor de
-- Argos pide un ticket con el iris_pat_ que Iris le entregó en el canje; el navegador lo canjea en
-- Iris (60 s, un solo uso, solo se guarda su SHA-256). Sin vínculo, el usuario entra con su login
-- normal y vincula su cuenta; nunca se vincula por correo ni se crean usuarios.
CREATE TABLE nucleo.sso_tickets (
    id                    bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    conector_id           bigint      NOT NULL REFERENCES integracion.conectores (id),
    ticket_hash           bytea       NOT NULL UNIQUE,
    usuario_externo       text        NOT NULL CHECK (length(usuario_externo) BETWEEN 1 AND 100),
    correo                text        NOT NULL DEFAULT '',
    nombre                text        NOT NULL DEFAULT '',
    destino_tipo          text        NOT NULL CHECK (destino_tipo IN ('ticket', 'orden', 'prospecto')),
    destino_id            bigint      NOT NULL CHECK (destino_id > 0),
    emitido_token_id      bigint      REFERENCES nucleo.tokens_api (id),
    expira_en             timestamptz NOT NULL,
    canjeado_en           timestamptz,
    -- Canje sin vínculo: la vinculación pendiente (cookie iris_sso_vincular, solo su SHA-256).
    vinculacion_hash      bytea       UNIQUE,
    vinculacion_expira_en timestamptz,
    vinculado_en          timestamptz,
    created_at            timestamptz NOT NULL DEFAULT now(),
    updated_at            timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX sso_tickets_expira_idx ON nucleo.sso_tickets (expira_en);

CREATE TABLE nucleo.sso_vinculos (
    id              bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    conector_id     bigint      NOT NULL REFERENCES integracion.conectores (id),
    usuario_externo text        NOT NULL CHECK (length(usuario_externo) BETWEEN 1 AND 100),
    usuario_id      bigint      NOT NULL REFERENCES nucleo.usuarios (id),
    ticket_id       bigint      REFERENCES nucleo.sso_tickets (id) ON DELETE SET NULL,
    created_at      timestamptz NOT NULL DEFAULT now(),
    UNIQUE (conector_id, usuario_externo),
    UNIQUE (conector_id, usuario_id)
);
CREATE INDEX sso_vinculos_usuario_idx ON nucleo.sso_vinculos (usuario_id);

-- +goose Down
DROP TABLE nucleo.sso_vinculos;
DROP TABLE nucleo.sso_tickets;

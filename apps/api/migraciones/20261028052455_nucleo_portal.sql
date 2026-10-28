-- +goose Up
-- Portal de soporte (/soporte): el cliente final no tiene usuario en Iris; lo identifica Argos.
-- Argos (token de integración con portal.emitir) pide un ticket de acceso de un solo uso y 60 s
-- y redirige al cliente a <iris>/soporte/entrar#<ticket>; el canje abre una sesión de «actor
-- cliente» (cookie iris_portal) con el cliente y la empresa de Argos fijos.

-- Tickets de acceso: se guarda solo el SHA-256.
CREATE TABLE nucleo.portal_accesos (
    id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    ticket_hash bytea       NOT NULL UNIQUE,
    cliente_id  bigint      NOT NULL CHECK (cliente_id > 0),       -- id del cliente en Argos
    empresa_id  bigint      NOT NULL REFERENCES nucleo.empresas (id),
    nombre      text        NOT NULL DEFAULT '',                   -- nombre a mostrar (lo manda Argos)
    emitido_por bigint      REFERENCES nucleo.usuarios (id),       -- null = consola o sistema
    token_id    bigint      REFERENCES nucleo.tokens_api (id),
    expira_en   timestamptz NOT NULL,
    usado_en    timestamptz,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX portal_accesos_expira_idx ON nucleo.portal_accesos (expira_en);

-- Sesiones del portal (cookie iris_portal, CSRF por doble envío con iris_portal_csrf).
CREATE TABLE nucleo.portal_sesiones (
    id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    acceso_id   bigint      NOT NULL REFERENCES nucleo.portal_accesos (id),
    cliente_id  bigint      NOT NULL CHECK (cliente_id > 0),
    empresa_id  bigint      NOT NULL REFERENCES nucleo.empresas (id),
    nombre      text        NOT NULL DEFAULT '',
    token_hash  bytea       NOT NULL UNIQUE,
    csrf_hash   bytea       NOT NULL,
    ip          text        NOT NULL DEFAULT '',
    user_agent  text        NOT NULL DEFAULT '',
    expira_en   timestamptz NOT NULL,
    ultimo_uso  timestamptz NOT NULL DEFAULT now(),
    revocada_en timestamptz,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX portal_sesiones_cliente_idx ON nucleo.portal_sesiones (empresa_id, cliente_id);

-- La auditoría acepta el actor cliente (actor_id = id del cliente en Argos).
ALTER TABLE auditoria.acciones DROP CONSTRAINT acciones_actor_tipo_check;
ALTER TABLE auditoria.acciones ADD CONSTRAINT acciones_actor_tipo_check
    CHECK (actor_tipo IN ('usuario', 'token', 'integracion', 'sistema', 'anonimo', 'cliente'));

-- +goose Down
ALTER TABLE auditoria.acciones DROP CONSTRAINT acciones_actor_tipo_check;
ALTER TABLE auditoria.acciones ADD CONSTRAINT acciones_actor_tipo_check
    CHECK (actor_tipo IN ('usuario', 'token', 'integracion', 'sistema', 'anonimo'));
DROP TABLE nucleo.portal_sesiones;
DROP TABLE nucleo.portal_accesos;

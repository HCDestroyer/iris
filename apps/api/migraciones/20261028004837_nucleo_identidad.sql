-- +goose Up
-- Identidad: usuarios, roles, permisos con alcance, sesiones y tokens de API (CLAUDE.md §8).
-- El catálogo de permisos y los roles base los sincroniza `iris migrar` desde
-- internal/nucleo/permisos.go (único catálogo).

CREATE TABLE nucleo.usuarios (
    id               bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    nombre           text        NOT NULL,
    correo           text        NOT NULL CHECK (correo = lower(correo)),
    tipo             text        NOT NULL DEFAULT 'persona' CHECK (tipo IN ('persona', 'integracion')),
    contrasena_hash  text,                                   -- argon2id (PHC); null = sin login por contraseña
    totp_secreto     bytea,                                  -- cifrado con IRIS_CLAVE_CIFRADO (AES-256-GCM)
    totp_activo      boolean     NOT NULL DEFAULT false,
    totp_ultimo_paso bigint      NOT NULL DEFAULT 0,         -- evita reusar un código TOTP
    activo           boolean     NOT NULL DEFAULT true,
    usuario_argos_id bigint,
    version          integer     NOT NULL DEFAULT 1,
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX usuarios_correo_uq ON nucleo.usuarios (correo);

CREATE TABLE nucleo.permisos (
    codigo      text PRIMARY KEY,
    descripcion text        NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE nucleo.roles (
    id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    codigo      text        NOT NULL UNIQUE,
    nombre      text        NOT NULL,
    descripcion text        NOT NULL DEFAULT '',
    sistema     boolean     NOT NULL DEFAULT false,          -- rol base: lo mantiene el seed
    version     integer     NOT NULL DEFAULT 1,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE nucleo.roles_permisos (
    rol_id     bigint      NOT NULL REFERENCES nucleo.roles (id) ON DELETE CASCADE,
    permiso    text        NOT NULL REFERENCES nucleo.permisos (codigo) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (rol_id, permiso)
);

-- Alcance opcional: zona o cuadrilla (agenda.*, de I2; sin FK hasta que existan).
CREATE TABLE nucleo.usuarios_roles (
    id           bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    usuario_id   bigint      NOT NULL REFERENCES nucleo.usuarios (id),
    rol_id       bigint      NOT NULL REFERENCES nucleo.roles (id),
    zona_id      bigint,
    cuadrilla_id bigint,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    CHECK (zona_id IS NULL OR cuadrilla_id IS NULL)
);
CREATE UNIQUE INDEX usuarios_roles_uq
    ON nucleo.usuarios_roles (usuario_id, rol_id, coalesce(zona_id, 0), coalesce(cuadrilla_id, 0));

-- Sesiones del panel: cookie iris_sesion (se guarda solo el SHA-256) y CSRF por doble envío.
CREATE TABLE nucleo.sesiones (
    id            bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    usuario_id    bigint      NOT NULL REFERENCES nucleo.usuarios (id),
    token_hash    bytea       NOT NULL UNIQUE,
    csrf_hash     bytea       NOT NULL,
    mfa_pendiente boolean     NOT NULL DEFAULT false,
    ip            text        NOT NULL DEFAULT '',
    user_agent    text        NOT NULL DEFAULT '',
    expira_en     timestamptz NOT NULL,
    ultimo_uso    timestamptz NOT NULL DEFAULT now(),
    revocada_en   timestamptz,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX sesiones_usuario_idx ON nucleo.sesiones (usuario_id);

-- Tokens de API iris_pat_<32B base62>; se guarda solo el SHA-256.
-- permisos null = los del usuario; si no, la intersección.
CREATE TABLE nucleo.tokens_api (
    id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    usuario_id  bigint      NOT NULL REFERENCES nucleo.usuarios (id),
    nombre      text        NOT NULL,
    prefijo     text        NOT NULL,
    hash        bytea       NOT NULL UNIQUE,
    permisos    text[],
    expira_en   timestamptz,
    ultimo_uso  timestamptz,
    revocado_en timestamptz,
    creado_por  bigint      REFERENCES nucleo.usuarios (id),
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX tokens_api_usuario_idx ON nucleo.tokens_api (usuario_id);

-- +goose Down
DROP TABLE nucleo.tokens_api;
DROP TABLE nucleo.sesiones;
DROP TABLE nucleo.usuarios_roles;
DROP TABLE nucleo.roles_permisos;
DROP TABLE nucleo.roles;
DROP TABLE nucleo.permisos;
DROP TABLE nucleo.usuarios;

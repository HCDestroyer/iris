-- +goose Up
-- Correo del sistema configurable desde el panel (contratos/correo-sistema.md). Una fila por
-- ámbito: la global (empresa_id null) y, opcional, una por empresa que la reemplaza. La contraseña
-- va cifrada con IRIS_CLAVE_CIFRADO (datos asociados a la fila) y nunca sale por la API.
CREATE TABLE notificaciones.correo_ajustes (
    id                 bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    empresa_id         bigint,
    host               text        NOT NULL CHECK (char_length(host) BETWEEN 1 AND 253),
    puerto             integer     NOT NULL CHECK (puerto BETWEEN 1 AND 65535),
    seguridad          text        NOT NULL CHECK (seguridad IN ('starttls', 'tls', 'ninguna')),
    usuario            text        NOT NULL DEFAULT '' CHECK (char_length(usuario) <= 254),
    contrasena_cifrada bytea,
    remitente_nombre   text        NOT NULL DEFAULT '' CHECK (char_length(remitente_nombre) <= 80),
    remitente_correo   text        NOT NULL CHECK (char_length(remitente_correo) BETWEEN 3 AND 254),
    activo             boolean     NOT NULL DEFAULT true,
    version            integer     NOT NULL DEFAULT 1,
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now(),
    -- En claro (un relay interno) nunca viajan credenciales.
    CHECK (seguridad <> 'ninguna' OR (usuario = '' AND contrasena_cifrada IS NULL))
);
CREATE UNIQUE INDEX correo_ajustes_ambito_uq ON notificaciones.correo_ajustes ((coalesce(empresa_id, 0)));

-- Pruebas de envío («probar antes de guardar»): el tope por usuario y por instalación y la última
-- prueba de cada ámbito. Nunca guarda el host, el destinatario ni credenciales.
CREATE TABLE notificaciones.correo_pruebas (
    id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    empresa_id bigint,
    usuario_id bigint      NOT NULL REFERENCES nucleo.usuarios (id),
    estado     text        NOT NULL CHECK (estado IN ('en_curso', 'ok', 'error')),
    causa      text        CHECK (causa IS NULL OR char_length(causa) <= 40),
    creado_en  timestamptz NOT NULL
);
CREATE INDEX correo_pruebas_usuario_idx ON notificaciones.correo_pruebas (usuario_id, creado_en);
CREATE INDEX correo_pruebas_creado_idx ON notificaciones.correo_pruebas (creado_en);

-- +goose Down
DROP TABLE notificaciones.correo_pruebas;
DROP TABLE notificaciones.correo_ajustes;

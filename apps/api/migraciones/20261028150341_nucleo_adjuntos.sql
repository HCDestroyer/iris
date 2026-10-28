-- +goose Up
-- Adjuntos (fotos, firmas, documentos) de órdenes y tickets, con subida reanudable estilo tus
-- (contratos/sync.md §4). Viven en nucleo porque los usan varios módulos: cada uno registra su
-- entidad adjuntable (internal/nucleo/adjuntos). Sin FK a campo ni a soporte: entidad +
-- entidad_id (texto).
CREATE TABLE nucleo.adjuntos (
    id              uuid        PRIMARY KEY,                       -- UUID generado en el cliente
    empresa_id      bigint,                                        -- la de la entidad (null sin conector)
    entidad         text        NOT NULL CHECK (entidad ~ '^[a-z][a-z_]{0,39}$'),
    entidad_id      text        NOT NULL CHECK (length(entidad_id) BETWEEN 1 AND 64),
    tipo            text        NOT NULL CHECK (tipo IN ('foto', 'firma', 'documento')),
    clave_checklist text        CHECK (length(clave_checklist) BETWEEN 1 AND 80),
    mime            text        NOT NULL,
    tamano          bigint      NOT NULL CHECK (tamano > 0),
    sha256          text        NOT NULL CHECK (sha256 ~ '^[0-9a-f]{64}$'),
    recibido        bigint      NOT NULL DEFAULT 0 CHECK (recibido >= 0 AND recibido <= tamano),
    estado          text        NOT NULL DEFAULT 'pendiente'
                                CHECK (estado IN ('pendiente', 'subiendo', 'completo', 'rechazado')),
    motivo_rechazo  text,
    lat             double precision CHECK (lat BETWEEN -90 AND 90),
    lon             double precision CHECK (lon BETWEEN -180 AND 180),
    tomado_en       timestamptz,
    subido_por      bigint      REFERENCES nucleo.usuarios (id),
    almacenamiento  text        NOT NULL CHECK (almacenamiento IN ('local', 's3')),
    clave           text        NOT NULL UNIQUE,                   -- clave del objeto en el almacenamiento
    completado_en   timestamptz,
    version         integer     NOT NULL DEFAULT 1,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX adjuntos_entidad_idx ON nucleo.adjuntos (entidad, entidad_id);
CREATE INDEX adjuntos_empresa_idx ON nucleo.adjuntos (empresa_id);

-- Partes recibidas de una subida en curso (PATCH de tus). Se guardan en la base para que la
-- subida se pueda reanudar contra cualquier réplica de la API; al completar se pasan al
-- almacenamiento (disco o S3) y se borran.
CREATE TABLE nucleo.adjuntos_partes (
    adjunto_id uuid   NOT NULL REFERENCES nucleo.adjuntos (id) ON DELETE CASCADE,
    desde      bigint NOT NULL CHECK (desde >= 0),
    datos      bytea  NOT NULL,
    PRIMARY KEY (adjunto_id, desde)
);

-- +goose Down
DROP TABLE nucleo.adjuntos_partes;
DROP TABLE nucleo.adjuntos;

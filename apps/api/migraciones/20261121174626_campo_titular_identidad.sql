-- +goose Up
-- Paso «Confirmar titular y plan» de la orden de instalación (contrato argosisp.md 0.10.0 §3.4,
-- pedidos B18 y B19). Lo que el técnico registra sale a Argos por la cola de pendientes, en orden y
-- con dependencias: identidad (A41) o cambio de titular (A39) → completar datos (A43) → cambiar el
-- plan (A40) → activar al cerrar.

-- Depósito de tránsito (§3.4.8): el documento, los datos de contacto y las fotos del documento
-- viajan cifrados (AES-GCM, datos asociados = transito:<id>), fuera de los adjuntos, de la
-- auditoría y del payload de los pendientes. Se borra al 2xx de Argos o a los 7 días de tomado.
CREATE TABLE integracion.datos_transito (
    id            uuid        PRIMARY KEY,
    empresa_id    bigint,
    orden_id      bigint      NOT NULL,
    cifrado       bytea,                    -- null = borrado
    tomado_en     timestamptz NOT NULL,
    borrado_en    timestamptz,
    motivo_borrado text       CHECK (motivo_borrado IN ('entregado', 'vencido', 'resuelto_manual', 'cancelado')),
    created_at    timestamptz NOT NULL DEFAULT now(),
    CHECK ((cifrado IS NULL) = (borrado_en IS NOT NULL))
);
CREATE INDEX datos_transito_vigentes_idx ON integracion.datos_transito (tomado_en) WHERE cifrado IS NOT NULL;

-- Pendientes: la referencia al depósito. La persona que actúa (X-Iris-Actor de §3.4) va en la
-- columna actor, que agregó 20261121161750_comercial_prospectos_argos (§3.5).
ALTER TABLE integracion.pendientes
    ADD COLUMN transito_id uuid REFERENCES integracion.datos_transito (id);

-- Estado del paso en la orden: lo que ya se registró (sin el documento ni los datos de contacto).
CREATE TABLE campo.orden_titular (
    orden_id               bigint      PRIMARY KEY REFERENCES campo.ordenes (id),
    empresa_id             bigint,
    quien_firma            text        CHECK (quien_firma IN ('misma', 'otra')),
    documento_tipo         text        CHECK (documento_tipo IN ('dpi', 'pasaporte')),
    nombre                 text,
    identidad_pendiente_id bigint,      -- A41 o A39 (lleva la identidad)
    ultimo_pendiente_id    bigint,      -- el último del paso: de él dependen el siguiente y la activación
    tomas                  integer     NOT NULL DEFAULT 0,
    cambios_titular        integer     NOT NULL DEFAULT 0,
    datos_n                integer     NOT NULL DEFAULT 0,
    cambios_plan           integer     NOT NULL DEFAULT 0,
    plan_id                bigint,
    validada_por           bigint      REFERENCES nucleo.usuarios (id),
    validada_motivo        text,
    validada_en            timestamptz,
    version                integer     NOT NULL DEFAULT 1,
    created_at             timestamptz NOT NULL DEFAULT now(),
    updated_at             timestamptz NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE campo.orden_titular;
ALTER TABLE integracion.pendientes DROP COLUMN transito_id;
DROP TABLE integracion.datos_transito;

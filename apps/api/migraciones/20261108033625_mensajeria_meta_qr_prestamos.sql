-- +goose Up
-- Conexión con Meta, WhatsApp por QR (coexistencia) y préstamo de números entre empresas
-- (contratos/conversaciones.md §15).

-- 1. Conexión con Meta: una sola fila por instalación (id = 1). Los secretos (app secret, clave de
--    enlace y verify token del webhook de la app) van cifrados con IRIS_CLAVE_CIFRADO en un JSON
--    atado a la fila; nunca se devuelven.
CREATE TABLE mensajeria.conexion_meta (
    id                         smallint    PRIMARY KEY CHECK (id = 1),
    modo                       text        NOT NULL CHECK (modo IN ('propia', 'compartida')),
    app_id                     text CHECK (app_id IS NULL OR app_id ~ '^[0-9]{1,30}$'),
    config_id                  text CHECK (config_id IS NULL OR config_id ~ '^[0-9]{1,30}$'),
    graph_version              text CHECK (graph_version IS NULL OR graph_version ~ '^v[0-9]{1,2}\.[0-9]$'),
    servicio_url               text CHECK (servicio_url IS NULL OR (servicio_url ~ '^https://' AND length(servicio_url) <= 300)),
    secretos_cifrados          bytea       NOT NULL,
    app_secret_guardado_en     timestamptz,
    clave_enlace_guardada_en   timestamptz,
    verify_token_guardado_en   timestamptz,
    webhook_verificado_en      timestamptz,
    ultimo_webhook_en          timestamptz,
    ultima_prueba_en           timestamptz,
    ultimo_estado_prueba       text CHECK (ultimo_estado_prueba IS NULL OR ultimo_estado_prueba IN ('lista', 'error')),
    comprobaciones             jsonb       NOT NULL DEFAULT '[]' CHECK (jsonb_typeof(comprobaciones) = 'array'),
    actualizado_por            bigint REFERENCES nucleo.usuarios (id),
    version                    integer     NOT NULL DEFAULT 1,
    created_at                 timestamptz NOT NULL DEFAULT now(),
    updated_at                 timestamptz NOT NULL DEFAULT now()
);

-- 2. Números conectados con QR (coexistencia con la app WhatsApp Business).
ALTER TABLE mensajeria.canales
    ADD COLUMN tipo_conexion        text        NOT NULL DEFAULT 'api' CHECK (tipo_conexion IN ('api', 'qr')),
    ADD COLUMN coexistencia         boolean     NOT NULL DEFAULT false,
    ADD COLUMN conectado_en         timestamptz,
    ADD COLUMN desconectado_en      timestamptz,
    ADD COLUMN desconectado_motivo  text CHECK (desconectado_motivo IS NULL OR length(desconectado_motivo) <= 60),
    ADD COLUMN ultimo_eco_en        timestamptz,
    -- { contactos: { estado, pedido_en, total, asociados, detalle, request_id }, mensajes: { … } }.
    ADD COLUMN sincronizacion       jsonb       NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(sincronizacion) = 'object'),
    ADD CONSTRAINT canales_qr_coexistencia CHECK (tipo_conexion = 'api' OR conectado_en IS NOT NULL);
CREATE INDEX canales_waba_idx ON mensajeria.canales (waba_id) WHERE tipo_conexion = 'qr';

-- Canje del código de Embedded Signup: un código se canjea una sola vez (solo su SHA-256).
CREATE TABLE mensajeria.registros_qr (
    hash_codigo bytea       PRIMARY KEY CHECK (length(hash_codigo) = 32),
    empresa_id  bigint,
    usuario_id  bigint REFERENCES nucleo.usuarios (id),
    created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX registros_qr_creado_idx ON mensajeria.registros_qr (created_at);

-- 3. Mensajes que no salieron de Iris: mandados desde el teléfono (eco) o traídos del historial.
ALTER TABLE mensajeria.conversacion_mensajes
    ADD COLUMN origen text CHECK (origen IS NULL OR origen IN ('telefono', 'historial'));

-- 4. Préstamo de un número a otra empresa de la instalación.
CREATE TABLE mensajeria.canal_prestamos (
    canal_id          bigint      NOT NULL REFERENCES mensajeria.canales (id),
    empresa_id        bigint      NOT NULL REFERENCES nucleo.empresas (id),
    usos              text[]      NOT NULL CHECK (cardinality(usos) BETWEEN 1 AND 4
                                                 AND usos <@ ARRAY['avisos', 'soporte', 'cobros', 'ventas']::text[]),
    cola_id           bigint REFERENCES mensajeria.colas (id),
    avisos_empresa    boolean     NOT NULL DEFAULT false,
    activo            boolean     NOT NULL DEFAULT true,
    prestado_en       timestamptz NOT NULL,
    prestado_por      bigint REFERENCES nucleo.usuarios (id),
    revocado_en       timestamptz,
    revocado_por      bigint REFERENCES nucleo.usuarios (id),
    motivo_revocacion text CHECK (motivo_revocacion IS NULL OR char_length(motivo_revocacion) BETWEEN 5 AND 300),
    version           integer     NOT NULL DEFAULT 1,
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (canal_id, empresa_id),
    CHECK (NOT avisos_empresa OR (activo AND 'avisos' = ANY (usos))),
    CHECK (activo = (revocado_en IS NULL))
);
CREATE INDEX canal_prestamos_empresa_idx ON mensajeria.canal_prestamos (empresa_id) WHERE activo;
-- Un solo número de avisos prestado por empresa (con el propio lo cuida el caso de uso).
CREATE UNIQUE INDEX canal_prestamos_avisos_uk ON mensajeria.canal_prestamos (empresa_id) WHERE avisos_empresa;

-- 5. Contactos de Iris que vienen del teléfono (smb_app_state_sync): solo esos se renombran desde el
--    teléfono; los que creó una persona no se tocan.
ALTER TABLE mensajeria.contactos_iris
    ADD COLUMN origen text NOT NULL DEFAULT 'agente' CHECK (origen IN ('agente', 'telefono'));

-- +goose Down
ALTER TABLE mensajeria.contactos_iris DROP COLUMN origen;
DROP TABLE mensajeria.canal_prestamos;
ALTER TABLE mensajeria.conversacion_mensajes DROP COLUMN origen;
DROP TABLE mensajeria.registros_qr;
DROP INDEX mensajeria.canales_waba_idx;
ALTER TABLE mensajeria.canales
    DROP CONSTRAINT canales_qr_coexistencia,
    DROP COLUMN sincronizacion,
    DROP COLUMN ultimo_eco_en,
    DROP COLUMN desconectado_motivo,
    DROP COLUMN desconectado_en,
    DROP COLUMN conectado_en,
    DROP COLUMN coexistencia,
    DROP COLUMN tipo_conexion;
DROP TABLE mensajeria.conexion_meta;

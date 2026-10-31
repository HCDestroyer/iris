-- +goose Up
-- Contrato Iris ↔ ArgosISP 0.5.0 (Argos ≥ 1.6.0): inventario del técnico, datos de contacto en sitio
-- y webhooks nuevos (servicio.datos_cambiados, catalogo.cambiado, inventario.traslado_*).

-- Versión que informa el otro sistema (canje §1.3/§1.4 y GET /integraciones/iris/estado) y el
-- contrato que Iris le informó (canje o PUT /integraciones/iris/contrato). null = no informado
-- (Argos anterior a 1.6.0 o todavía no consultado).
ALTER TABLE integracion.conectores
    ADD COLUMN version_remota        text CHECK (version_remota IS NULL OR length(version_remota) BETWEEN 1 AND 40),
    ADD COLUMN contrato_remoto       text CHECK (contrato_remoto IS NULL OR length(contrato_remoto) BETWEEN 1 AND 20),
    ADD COLUMN contrato_informado    text CHECK (contrato_informado IS NULL OR length(contrato_informado) BETWEEN 1 AND 20),
    ADD COLUMN version_consultada_en timestamptz;

-- Pendientes hacia Argos: el técnico de Iris cuyo vínculo con Argos va en el cuerpo (tecnico, 0.5.0)
-- para corregirlo al reintentar a mano, y el type del problem+json del último error de Argos
-- (tecnico_no_encontrado, existencia_insuficiente…).
ALTER TABLE integracion.pendientes
    ADD COLUMN tecnico_usuario_id bigint REFERENCES nucleo.usuarios (id),
    ADD COLUMN codigo_error       text CHECK (codigo_error IS NULL OR length(codigo_error) BETWEEN 1 AND 100);
CREATE INDEX pendientes_tecnico_idx ON integracion.pendientes (tecnico_usuario_id) WHERE tecnico_usuario_id IS NOT NULL;

-- Instantáneas (§2.2, 0.5.0): punto de referencia del cliente y del servicio, contacto en sitio del
-- servicio. Vacío = Argos no lo tiene (o es anterior a 1.6.0).
ALTER TABLE integracion.argos_clientes
    ADD COLUMN referencia text NOT NULL DEFAULT '';
ALTER TABLE integracion.argos_servicios
    ADD COLUMN referencia        text NOT NULL DEFAULT '',
    ADD COLUMN contacto_nombre   text NOT NULL DEFAULT '',
    ADD COLUMN contacto_telefono text NOT NULL DEFAULT '';

-- catalogo.cambiado (0.5.0): Argos desactiva o reactiva zonas y routers.
ALTER TABLE integracion.argos_zonas ADD COLUMN activo boolean NOT NULL DEFAULT true;
ALTER TABLE integracion.argos_routers ADD COLUMN activo boolean NOT NULL DEFAULT true;

-- Órdenes de traslado hacia la bodega de un técnico (inventario.traslado_enviado|recibido|cancelado):
-- modelo de lectura para la app de campo («Por recibir») y sus avisos. El id es el de Argos. El
-- técnico se identifica como lo manda Argos (id_externo o correo); Iris lo cruza con el vínculo de
-- agenda.tecnicos al leer. Un estado final (recibido, cancelado) nunca vuelve a enviado.
CREATE TABLE integracion.argos_traslados (
    id                 bigint      PRIMARY KEY,
    empresa_id         bigint,
    bodega_destino_id  bigint,
    tecnico_id_externo text CHECK (tecnico_id_externo IS NULL OR length(tecnico_id_externo) BETWEEN 1 AND 100),
    tecnico_correo     text CHECK (tecnico_correo IS NULL OR length(tecnico_correo) BETWEEN 1 AND 320),
    estado             text        NOT NULL CHECK (estado IN ('enviado', 'recibido', 'cancelado')),
    lineas             jsonb       NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(lineas) = 'array'),
    enviado_en         timestamptz,
    recibido_en        timestamptz,
    recibido_por       text,
    metodo_recepcion   text,
    origen_recepcion   text,
    cancelado_en       timestamptz,
    motivo             text,
    entrante_id        bigint      REFERENCES integracion.entrantes (id) ON DELETE SET NULL,
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX argos_traslados_externo_idx ON integracion.argos_traslados (tecnico_id_externo, estado) WHERE tecnico_id_externo IS NOT NULL;
CREATE INDEX argos_traslados_correo_idx ON integracion.argos_traslados (lower(tecnico_correo), estado) WHERE tecnico_correo IS NOT NULL;

-- +goose Down
DROP TABLE integracion.argos_traslados;
ALTER TABLE integracion.argos_routers DROP COLUMN activo;
ALTER TABLE integracion.argos_zonas DROP COLUMN activo;
ALTER TABLE integracion.argos_servicios DROP COLUMN referencia, DROP COLUMN contacto_nombre, DROP COLUMN contacto_telefono;
ALTER TABLE integracion.argos_clientes DROP COLUMN referencia;
DROP INDEX integracion.pendientes_tecnico_idx;
ALTER TABLE integracion.pendientes DROP COLUMN tecnico_usuario_id, DROP COLUMN codigo_error;
ALTER TABLE integracion.conectores
    DROP COLUMN version_remota, DROP COLUMN contrato_remoto, DROP COLUMN contrato_informado, DROP COLUMN version_consultada_en;

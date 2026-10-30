-- +goose Up
-- Datos de Argos coherentes en todo Iris (pedido 5 del PM): la instantánea guarda todo lo que el
-- panel muestra del cliente y del servicio, tal como lo manda Argos 1.4.0 (el formato para mostrar
-- se calcula al leer: internal/integracion/argos/formato).
--
-- argos_clientes: tipo (persona | empresa), documento fiscal (tipo y número sin guiones), móvil,
-- correo y el nombre de la zona del cliente (Argos lo manda en cada servicio como zona_nombre).
ALTER TABLE integracion.argos_clientes
    ADD COLUMN tipo                  text     NOT NULL DEFAULT '',
    ADD COLUMN tipo_documento_fiscal text     NOT NULL DEFAULT '',
    ADD COLUMN nit_cui               text     NOT NULL DEFAULT '',
    ADD COLUMN movil                 text     NOT NULL DEFAULT '',
    ADD COLUMN correo                text     NOT NULL DEFAULT '',
    ADD COLUMN zona_nombre           text     NOT NULL DEFAULT '',
    -- version_datos: qué tanto de Argos guarda la fila. Las anteriores a esta migración (1) no
    -- tienen los campos nuevos ni la IP de sus servicios: la ficha las relee de Argos aunque
    -- tengan menos de 24 h (ObtenerFicha, VersionDatos).
    ADD COLUMN version_datos         smallint NOT NULL DEFAULT 1;

-- argos_servicios: usuario PPPoE y tipo de conexión. La IP (ip) ahora sale de ipv4, que es como la
-- manda Argos 1.4.0 (antes quedaba vacía).
ALTER TABLE integracion.argos_servicios
    ADD COLUMN user_ppp      text NOT NULL DEFAULT '',
    ADD COLUMN tipo_conexion text NOT NULL DEFAULT '';

-- argos_copias: lo que Iris sabía del cliente y del servicio al crear un ticket o una orden. Es
-- una copia que se conserva (solo inserción): si el servicio se elimina en Argos, el conector está
-- apagado o Argos no responde, el ticket y la orden siguen mostrando la dirección y el teléfono
-- con los que se crearon. datos es el mismo JSON que la API expone (CopiaArgos), con el cliente y
-- el servicio en la forma de la ficha. sincronizado_en es el de la instantánea copiada.
CREATE TABLE integracion.argos_copias (
    entidad         text        NOT NULL CHECK (entidad IN ('ticket', 'orden')),
    entidad_id      bigint      NOT NULL,
    cliente_id      bigint      NOT NULL,
    servicio_id     bigint,
    datos           jsonb       NOT NULL,
    sincronizado_en timestamptz NOT NULL,
    copiado_en      timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (entidad, entidad_id)
);
CREATE INDEX argos_copias_cliente_idx ON integracion.argos_copias (cliente_id);

-- +goose Down
DROP TABLE integracion.argos_copias;
ALTER TABLE integracion.argos_servicios
    DROP COLUMN tipo_conexion,
    DROP COLUMN user_ppp;
ALTER TABLE integracion.argos_clientes
    DROP COLUMN version_datos,
    DROP COLUMN zona_nombre,
    DROP COLUMN correo,
    DROP COLUMN movil,
    DROP COLUMN nit_cui,
    DROP COLUMN tipo_documento_fiscal,
    DROP COLUMN tipo;

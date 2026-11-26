-- +goose Up
-- Estado e historial de los equipos del cliente (contrato con ArgosISP 0.12.0 §2.3 y §3.10, B27):
-- instantánea de lectura que llenan los webhooks inventario.unidad_estado_cambiado e
-- inventario.equipo_cambiado. Sin costos, proveedor ni RMA (Argos no los manda).
--
--  - argos_unidades: el último estado conocido de cada unidad y el servicio al que está asociada.
--    estado_en y servicio_en son solo horas de Argos (el ocurrido_en del webhook, si vino de Argos y no
--    está en el futuro; nunca la hora de Iris). Un webhook más viejo que el guardado no pisa el estado
--    (sí queda en el historial). Uno sin hora de Argos se aplica por orden de llegada solo si lo
--    guardado tampoco tiene hora de Argos; nunca pisa una hora que vino de Argos.
--  - argos_unidades_historial: un renglón por hecho y unidad (estado, o sale/entra de un cambio),
--    único por entrante: un webhook repetido no duplica. ocurrido_en es la hora de Argos (o null);
--    recibido_en, la llegada a Iris.
CREATE TABLE integracion.argos_unidades (
    unidad_id      bigint      PRIMARY KEY CHECK (unidad_id > 0),
    empresa_id     bigint      NOT NULL,
    servicio_id    bigint,
    estado         text        CHECK (estado IS NULL OR estado IN ('disponible', 'en_comodato', 'por_recuperar', 'revision',
                                                                   'garantia', 'dada_de_baja', 'no_recuperada')),
    estado_en      timestamptz,
    servicio_en    timestamptz,
    entrante_id    bigint      NOT NULL,
    actualizado_en timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX argos_unidades_servicio_idx ON integracion.argos_unidades (servicio_id) WHERE servicio_id IS NOT NULL;

CREATE TABLE integracion.argos_unidades_historial (
    id           bigserial   PRIMARY KEY,
    unidad_id    bigint      NOT NULL CHECK (unidad_id > 0),
    empresa_id   bigint      NOT NULL,
    servicio_id  bigint,
    tipo         text        NOT NULL CHECK (tipo IN ('estado', 'sale', 'entra')),
    de           text,
    a            text,
    motivo       text,
    destino_sale text,
    origen       text,
    origen_id    bigint,
    ocurrido_en  timestamptz,
    recibido_en  timestamptz NOT NULL,
    entrante_id  bigint      NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    UNIQUE (entrante_id, unidad_id, tipo)
);
CREATE INDEX argos_unidades_historial_unidad_idx ON integracion.argos_unidades_historial (unidad_id, id DESC);

-- +goose Down
DROP TABLE integracion.argos_unidades_historial;
DROP TABLE integracion.argos_unidades;

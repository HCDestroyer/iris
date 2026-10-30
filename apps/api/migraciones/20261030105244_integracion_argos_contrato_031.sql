-- +goose Up
-- Contrato Iris ↔ ArgosISP 0.3.1.
--
-- integracion.argos_eliminados: servicios y routers que Argos eliminó (webhooks servicio.eliminado
-- y router.eliminado, §2.3; Argos solo elimina lo que nunca se usó). Es la lápida: la instantánea
-- (argos_servicios, argos_routers) ya no los tiene y un evento o una lectura atrasados no los
-- recrean. Del servicio queda lo mínimo para que órdenes y tickets que lo referencian lo muestren
-- «eliminado en Argos» (código, plan, cliente y empresa, copiados de la instantánea si la había).
-- empresa_id null: un servicio sin empresa o un router compartido (sin empresa sugerida).
-- eliminado_en es la hora de Argos (ocurrido_en del webhook, para mostrar); confirmado_en, la de
-- Postgres (clock_timestamp()) antes de pedir el 404 que lo confirmó: solo un GET por id con 200
-- pedido después resucita la instantánea y borra la lápida; una lista nunca la recrea.
CREATE TABLE integracion.argos_eliminados (
    tipo          text        NOT NULL CHECK (tipo IN ('servicio', 'router')),
    id            bigint      NOT NULL,
    empresa_id    bigint,
    cliente_id    bigint,
    codigo        text        NOT NULL DEFAULT '',
    nombre        text        NOT NULL DEFAULT '',
    -- de_instantanea: empresa, cliente y copia salieron de la instantánea de Iris (no del payload);
    -- solo esas lápidas sirven de pista de empresa del cliente.
    de_instantanea boolean    NOT NULL DEFAULT false,
    conector_id   bigint      NOT NULL REFERENCES integracion.conectores (id),
    entrante_id   bigint      REFERENCES integracion.entrantes (id),
    eliminado_en  timestamptz NOT NULL,
    confirmado_en timestamptz NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tipo, id),
    CHECK (tipo = 'servicio' OR cliente_id IS NULL)
);
CREATE INDEX argos_eliminados_cliente_idx ON integracion.argos_eliminados (cliente_id) WHERE tipo = 'servicio';

-- alcance_global: lo que informó el canje de Argos (§1.3, 0.3.1). true = Iris ve también las
-- empresas que se creen después; false = solo las de empresas; null = Argos no lo informó
-- (versiones anteriores).
ALTER TABLE integracion.conectores ADD COLUMN alcance_global boolean;

-- +goose Down
ALTER TABLE integracion.conectores DROP COLUMN alcance_global;
DROP TABLE integracion.argos_eliminados;

-- +goose Up
-- Tipo de servicio (contrato argosisp.md 0.15.0 §13.1, A53 / B33). Lista blanca: solo estos dos
-- campos, como Argos los manda en GET /servicios, GET /planes, la primera sincronización y las
-- relecturas por webhook. null = Argos no los informó (anterior a 0.15.0): el servicio se trata como
-- internet, con router y velocidad, como hasta ahora (§4.6).
--   tipo_servicio: internet, cable, iptv, mixto u otro que Argos agregue después (se guarda para
--     aplicar A1: un tipo desconocido cuenta como internet, salvo que router_id sea null).
--   componentes: solo internet, cable e iptv (lo demás se descarta al leer).
ALTER TABLE integracion.argos_servicios
    ADD COLUMN tipo_servicio text CHECK (tipo_servicio ~ '^[a-z][a-z0-9_]{0,29}$'),
    ADD COLUMN componentes   text[] CHECK (componentes <@ ARRAY['internet', 'cable', 'iptv']::text[]);

-- Instantánea de los planes de Argos (§13.1): hasta ahora los planes se leían en vivo y solo se
-- copiaba su nombre en cada servicio. La factibilidad y el alta necesitan saber si el plan de un
-- prospecto es de cable sin llamar a Argos. Se llena en la primera sincronización y se refresca con
-- catalogo.cambiado de plan; sincronizado_en evita que una lectura vieja pise una nueva.
CREATE TABLE integracion.argos_planes (
    id              bigint PRIMARY KEY,
    nombre          text        NOT NULL DEFAULT '',
    tipo_servicio   text        CHECK (tipo_servicio ~ '^[a-z][a-z0-9_]{0,29}$'),
    componentes     text[]      CHECK (componentes <@ ARRAY['internet', 'cable', 'iptv']::text[]),
    activo          boolean     NOT NULL DEFAULT true,
    sincronizado_en timestamptz NOT NULL,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE integracion.argos_planes;
ALTER TABLE integracion.argos_servicios DROP COLUMN componentes, DROP COLUMN tipo_servicio;

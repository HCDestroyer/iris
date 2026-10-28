-- +goose Up
-- Soporte (I2): tickets desde Horus (incidentes maestros por router o servicio de
-- infraestructura, tickets por hallazgo con reglas configurables, propuestas de resolución) y
-- categorías visibles en el portal del cliente.
--
-- - tickets.maestro: 'router' | 'infra' = incidente maestro abierto por Horus (los tickets de
--   clientes afectados se vinculan como hijos con ticket_padre_id). router_id es el id del
--   router en Argos; infra_id, el del servicio de infraestructura en Horus; incidente_nombre, el
--   nombre que mandó Horus.
-- - tickets.hallazgo_id / url_horus: ticket abierto por un hallazgo de seguridad de Horus (uno
--   por hallazgo) y su enlace «Ver en Horus».
-- - tickets.propuesta: Horus avisó que el router volvió o que el hallazgo se resolvió; Iris no
--   resuelve solo: deja la propuesta para que un agente la confirme. Se limpia al cambiar de
--   estado.
-- - horus_eventos: un registro por evento interno horus.* procesado (deduplicado por
--   entrante_id): qué hizo soporte con él.
-- - reglas_hallazgos: qué categorías de hallazgo de Horus abren ticket (categoría y prioridad
--   de Iris). Semilla con ejemplos inactivos.
-- - categorias.visible_portal: el cliente puede elegirla en el portal.
ALTER TABLE soporte.tickets
    ADD COLUMN maestro          text CHECK (maestro IN ('router', 'infra')),
    ADD COLUMN router_id        bigint,
    ADD COLUMN infra_id         text,
    ADD COLUMN incidente_nombre text,
    ADD COLUMN hallazgo_id      text,
    ADD COLUMN url_horus        text,
    ADD COLUMN propuesta        text CHECK (propuesta IN ('resolver', 'cerrar')),
    ADD COLUMN propuesta_en     timestamptz,
    ADD COLUMN propuesta_motivo text,
    ADD CONSTRAINT tickets_maestro_datos_check CHECK (
        maestro IS NULL OR (maestro = 'router' AND router_id IS NOT NULL) OR (maestro = 'infra' AND infra_id IS NOT NULL)),
    ADD CONSTRAINT tickets_propuesta_datos_check CHECK ((propuesta IS NULL) = (propuesta_en IS NULL));

-- Un solo maestro no cerrado por router (o servicio de infraestructura) y empresa.
CREATE UNIQUE INDEX tickets_maestro_router_uq ON soporte.tickets (coalesce(empresa_id, 0), router_id)
    WHERE maestro = 'router' AND estado <> 'cerrado';
CREATE UNIQUE INDEX tickets_maestro_infra_uq ON soporte.tickets (coalesce(empresa_id, 0), infra_id)
    WHERE maestro = 'infra' AND estado <> 'cerrado';
-- Vinculación de hijos: maestros abiertos por router.
CREATE INDEX tickets_maestro_abierto_idx ON soporte.tickets (router_id)
    WHERE maestro IS NOT NULL AND estado IN ('nuevo', 'abierto', 'en_espera');
-- Un ticket por hallazgo de Horus.
CREATE UNIQUE INDEX tickets_hallazgo_uq ON soporte.tickets (coalesce(empresa_id, 0), hallazgo_id)
    WHERE hallazgo_id IS NOT NULL;

CREATE TABLE soporte.horus_eventos (
    entrante_id bigint      PRIMARY KEY,             -- integracion.entrantes.id
    evento      text        NOT NULL,
    empresa_id  bigint,
    resultado   text        NOT NULL,
    ticket_id   bigint      REFERENCES soporte.tickets (id),
    detalle     text,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX horus_eventos_ticket_idx ON soporte.horus_eventos (ticket_id) WHERE ticket_id IS NOT NULL;

CREATE TABLE soporte.reglas_hallazgos (
    id              bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    empresa_id      bigint,                              -- null = de todas las empresas
    categoria_horus text        NOT NULL CHECK (categoria_horus = lower(categoria_horus) AND length(categoria_horus) > 0),
    categoria_id    bigint      NOT NULL REFERENCES soporte.categorias (id),
    prioridad       text        NOT NULL DEFAULT 'media' CHECK (prioridad IN ('baja', 'media', 'alta', 'critica')),
    activa          boolean     NOT NULL DEFAULT false,
    version         integer     NOT NULL DEFAULT 1,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX reglas_hallazgos_uq ON soporte.reglas_hallazgos (coalesce(empresa_id, 0), categoria_horus);

-- Ejemplos inactivos: un administrador los activa (hoy por SQL) cuando quiera tickets por esos
-- hallazgos.
INSERT INTO soporte.reglas_hallazgos (categoria_horus, categoria_id, prioridad, activa)
SELECT r.categoria_horus, c.id, r.prioridad, false
FROM (VALUES
    ('equipo_infectado', 'equipo', 'alta'),
    ('cpe_firmware_vulnerable', 'equipo', 'media')
) AS r (categoria_horus, categoria, prioridad)
JOIN soporte.categorias c ON c.codigo = r.categoria AND c.empresa_id IS NULL;

ALTER TABLE soporte.categorias ADD COLUMN visible_portal boolean NOT NULL DEFAULT false;
UPDATE soporte.categorias SET visible_portal = true
WHERE empresa_id IS NULL
  AND codigo IN ('sin_servicio', 'intermitencia', 'lentitud', 'equipo', 'traslado', 'facturacion', 'cambio_plan', 'otro');

-- +goose Down
ALTER TABLE soporte.categorias DROP COLUMN visible_portal;
DROP TABLE soporte.reglas_hallazgos;
DROP TABLE soporte.horus_eventos;
DROP INDEX soporte.tickets_hallazgo_uq;
DROP INDEX soporte.tickets_maestro_abierto_idx;
DROP INDEX soporte.tickets_maestro_infra_uq;
DROP INDEX soporte.tickets_maestro_router_uq;
ALTER TABLE soporte.tickets
    DROP CONSTRAINT tickets_propuesta_datos_check,
    DROP CONSTRAINT tickets_maestro_datos_check,
    DROP COLUMN propuesta_motivo,
    DROP COLUMN propuesta_en,
    DROP COLUMN propuesta,
    DROP COLUMN url_horus,
    DROP COLUMN hallazgo_id,
    DROP COLUMN incidente_nombre,
    DROP COLUMN infra_id,
    DROP COLUMN router_id,
    DROP COLUMN maestro;

-- +goose Up
-- Equipos del cliente (contrato con ArgosISP 0.12.0 §3.10 y §10.7, pedido B27).
--
--  - cambio: la unidad que sale (sale_unidad_id) y la que entra (unidad_id, serie, mac), con motivo,
--    destino de la que sale y un cobro propuesto sin monto (lo decide una persona en Argos).
--  - no_recuperado: un intento de recuperación de la unidad (unidad_id) en un retiro, con motivo,
--    foto (obligatoria si se negó o se perdió), ubicación tomada en sitio y nota.
--
-- recibido_en: la hora del servidor de Iris al aceptar la operación (recibido_en_iris en Argos);
-- ocurrido_en es la fecha del teléfono. Las notas (hasta 500) nunca van en eventos ni en logs.
ALTER TABLE campo.orden_equipos
    DROP CONSTRAINT orden_equipos_accion_check,
    ADD CONSTRAINT orden_equipos_accion_check CHECK (accion IN ('instalar', 'retirar', 'cambio', 'no_recuperado')),
    ADD COLUMN sale_unidad_id  bigint,
    ADD COLUMN motivo          text,
    ADD COLUMN destino_sale    text CHECK (destino_sale IS NULL OR destino_sale IN ('funciona', 'revision', 'garantia', 'danado')),
    ADD COLUMN cobro_motivo    text CHECK (cobro_motivo IS NULL OR cobro_motivo IN ('golpe', 'humedad', 'manipulado', 'otro')),
    ADD COLUMN cobro_nota      text CHECK (cobro_nota IS NULL OR length(cobro_nota) <= 500),
    ADD COLUMN nota            text CHECK (nota IS NULL OR length(nota) <= 500),
    ADD COLUMN foto_adjunto_id uuid,
    ADD COLUMN lat             double precision CHECK (lat IS NULL OR lat BETWEEN -90 AND 90),
    ADD COLUMN lon             double precision CHECK (lon IS NULL OR lon BETWEEN -180 AND 180),
    ADD COLUMN precision_m     real CHECK (precision_m IS NULL OR precision_m >= 0),
    ADD COLUMN recibido_en     timestamptz,
    ADD CONSTRAINT orden_equipos_cambio CHECK (accion <> 'cambio' OR (
        sale_unidad_id IS NOT NULL AND destino_sale IS NOT NULL
        AND motivo IN ('danado', 'actualizacion', 'garantia', 'otro')
        AND NOT (motivo IN ('danado', 'garantia') AND destino_sale = 'funciona'))),
    ADD CONSTRAINT orden_equipos_no_recuperado CHECK (accion <> 'no_recuperado' OR (
        unidad_id IS NOT NULL AND lat IS NOT NULL AND lon IS NOT NULL
        AND motivo IN ('cliente_ausente', 'se_nego', 'equipo_perdido', 'otro')
        AND (motivo NOT IN ('se_nego', 'equipo_perdido') OR foto_adjunto_id IS NOT NULL))),
    ADD CONSTRAINT orden_equipos_cobro CHECK (cobro_motivo IS NULL OR accion = 'cambio');

-- Un intento por unidad al día (hora de Guatemala) desde Iris, en cualquier orden.
CREATE INDEX orden_equipos_no_recuperado_idx ON campo.orden_equipos (unidad_id, ocurrido_en) WHERE accion = 'no_recuperado';

-- +goose Down
DROP INDEX IF EXISTS campo.orden_equipos_no_recuperado_idx;
DELETE FROM campo.orden_equipos WHERE accion IN ('cambio', 'no_recuperado');
ALTER TABLE campo.orden_equipos
    DROP CONSTRAINT orden_equipos_cobro,
    DROP CONSTRAINT orden_equipos_no_recuperado,
    DROP CONSTRAINT orden_equipos_cambio,
    DROP COLUMN recibido_en, DROP COLUMN precision_m, DROP COLUMN lon, DROP COLUMN lat, DROP COLUMN foto_adjunto_id,
    DROP COLUMN nota, DROP COLUMN cobro_nota, DROP COLUMN cobro_motivo, DROP COLUMN destino_sale, DROP COLUMN motivo,
    DROP COLUMN sale_unidad_id,
    DROP CONSTRAINT orden_equipos_accion_check,
    ADD CONSTRAINT orden_equipos_accion_check CHECK (accion IN ('instalar', 'retirar'));

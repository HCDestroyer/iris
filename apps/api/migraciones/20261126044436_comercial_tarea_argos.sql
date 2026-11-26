-- +goose Up
-- Solicitudes del portal de Argos (contrato 0.9.0 §11.7 y §10.4 punto 10): un cambio de plan, un
-- traslado u otro ganado no se aplica por API en Argos (hasta A34). Queda la tarea «Abrir en Argos»
-- para que una persona lo aplique allá; la solicitud sigue en_proceso hasta que alguien la marca hecha.
ALTER TABLE comercial.prospectos
    ADD COLUMN tarea_argos text CHECK (tarea_argos IN ('pendiente', 'hecha')),
    ADD COLUMN tarea_argos_hecha_en timestamptz,
    ADD COLUMN tarea_argos_hecha_por bigint REFERENCES nucleo.usuarios (id),
    ADD CONSTRAINT prospectos_tarea_argos_datos_check CHECK (
        (tarea_argos IS NULL OR solicitud_tipo IN ('cambio_plan', 'traslado', 'otro'))
        AND ((tarea_argos = 'hecha') = (tarea_argos_hecha_en IS NOT NULL)));
-- Ganado sin llamar a Argos: alta_modo solicitud (alta_estado completa: nada pendiente en Argos).
ALTER TABLE comercial.prospectos DROP CONSTRAINT prospectos_alta_modo_check;
ALTER TABLE comercial.prospectos ADD CONSTRAINT prospectos_alta_modo_check CHECK (alta_modo IN ('local', 'argos', 'solicitud'));
CREATE INDEX prospectos_tarea_argos_idx ON comercial.prospectos (empresa_id, id) WHERE tarea_argos = 'pendiente';

-- +goose Down
UPDATE comercial.prospectos SET alta_modo = 'local' WHERE alta_modo = 'solicitud';
ALTER TABLE comercial.prospectos DROP CONSTRAINT prospectos_alta_modo_check;
ALTER TABLE comercial.prospectos ADD CONSTRAINT prospectos_alta_modo_check CHECK (alta_modo IN ('local', 'argos'));
DROP INDEX comercial.prospectos_tarea_argos_idx;
ALTER TABLE comercial.prospectos DROP CONSTRAINT prospectos_tarea_argos_datos_check,
    DROP COLUMN tarea_argos_hecha_por, DROP COLUMN tarea_argos_hecha_en, DROP COLUMN tarea_argos;

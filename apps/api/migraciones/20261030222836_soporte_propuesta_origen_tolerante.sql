-- +goose Up
-- Soporte (I2): la restricción de 20261030202702 exigía propuesta_origen junto con propuesta, y
-- durante un despliegue la versión anterior (que no conoce la columna) sigue escribiendo
-- propuestas de Horus sin origen: su UPDATE fallaba. Ahora se tolera el origen null (se lee como
-- horus, igual que en el código); solo se exige que no haya origen sin propuesta y que la orden
-- vaya solo con origen orden.
ALTER TABLE soporte.tickets
    DROP CONSTRAINT tickets_propuesta_origen_datos_check,
    ADD CONSTRAINT tickets_propuesta_origen_datos_check CHECK (
        (propuesta IS NOT NULL OR (propuesta_origen IS NULL AND propuesta_orden_id IS NULL))
        AND (propuesta_orden_id IS NULL OR propuesta_origen = 'orden'));

-- +goose Down
UPDATE soporte.tickets SET propuesta_origen = 'horus' WHERE propuesta IS NOT NULL AND propuesta_origen IS NULL;
ALTER TABLE soporte.tickets
    DROP CONSTRAINT tickets_propuesta_origen_datos_check,
    ADD CONSTRAINT tickets_propuesta_origen_datos_check CHECK (
        (propuesta IS NULL) = (propuesta_origen IS NULL)
        AND (propuesta_orden_id IS NULL OR propuesta_origen = 'orden'));

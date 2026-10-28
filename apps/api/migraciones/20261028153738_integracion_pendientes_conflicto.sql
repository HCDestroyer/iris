-- +goose Up
-- Cierre repetido de una orden y dependencias fallidas (revisión de la PR #14):
--  - estado «bloqueado»: un pendiente cuya dependencia quedó fallido_negocio o agotado. Vuelve a
--    «pendiente» cuando la dependencia termina «hecho» (p. ej. tras un reintento manual).
--  - conflicto: la orden se volvió a cerrar con otros datos para una acción ya hecha, en curso o
--    encolada con los datos anteriores. No se pisa (la Idempotency-Key ya viajó a Argos con el
--    payload anterior): se marca para corregir a mano en Argos (motivo y payload nuevo).
--  Un pendiente fallido_negocio con datos_incompletos sí se corrige con el cierre nuevo (vuelve
--  a «pendiente» con el payload completo).
ALTER TABLE integracion.pendientes DROP CONSTRAINT pendientes_estado_check;
ALTER TABLE integracion.pendientes ADD CONSTRAINT pendientes_estado_check
    CHECK (estado IN ('pendiente', 'en_curso', 'hecho', 'fallido_negocio', 'agotado', 'bloqueado'));
ALTER TABLE integracion.pendientes
    ADD COLUMN conflicto         text CHECK (conflicto IN ('cierre_repetido_con_otros_datos')),
    ADD COLUMN conflicto_payload jsonb,
    ADD COLUMN conflicto_en      timestamptz;
CREATE INDEX pendientes_depende_idx ON integracion.pendientes USING gin (depende_de);

-- +goose Down
DROP INDEX integracion.pendientes_depende_idx;
ALTER TABLE integracion.pendientes DROP COLUMN conflicto_en, DROP COLUMN conflicto_payload, DROP COLUMN conflicto;
UPDATE integracion.pendientes SET estado = 'fallido_negocio' WHERE estado = 'bloqueado';
ALTER TABLE integracion.pendientes DROP CONSTRAINT pendientes_estado_check;
ALTER TABLE integracion.pendientes ADD CONSTRAINT pendientes_estado_check
    CHECK (estado IN ('pendiente', 'en_curso', 'hecho', 'fallido_negocio', 'agotado'));

-- +goose Up
-- Referencia de la dirección (cómo llegar) copiada de Argos al crear la orden y al aplicar los datos
-- nuevos de Argos (ordenes.aplicar_datos_argos). Null si Argos no la tiene o la orden no es de Argos.
-- La columna nace sin CHECK (no reescribe la tabla) y la restricción se agrega NOT VALID y se valida
-- aparte (sin bloquear escrituras mientras recorre las filas).
ALTER TABLE campo.ordenes ADD COLUMN referencia text;
ALTER TABLE campo.ordenes ADD CONSTRAINT ordenes_referencia_largo
    CHECK (referencia IS NULL OR length(referencia) BETWEEN 1 AND 500) NOT VALID;
ALTER TABLE campo.ordenes VALIDATE CONSTRAINT ordenes_referencia_largo;

-- +goose Down
ALTER TABLE campo.ordenes DROP CONSTRAINT ordenes_referencia_largo;
ALTER TABLE campo.ordenes DROP COLUMN referencia;

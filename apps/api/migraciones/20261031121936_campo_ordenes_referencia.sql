-- +goose Up
-- Referencia de la dirección (cómo llegar) copiada de Argos al crear la orden y al aplicar los datos
-- nuevos de Argos (ordenes.aplicar_datos_argos). Null si Argos no la tiene o la orden no es de Argos.
ALTER TABLE campo.ordenes
    ADD COLUMN referencia text CHECK (referencia IS NULL OR length(referencia) BETWEEN 1 AND 500);

-- +goose Down
ALTER TABLE campo.ordenes DROP COLUMN referencia;

-- +goose Up
-- Voz E1, revisión de seguridad: la intención con Meta recuerda de quién era la reserva (el resultado
-- de un pre_accept o un accept solo vale si la llamada sigue en manos de esa persona).
ALTER TABLE mensajeria.llamadas ADD COLUMN accion_meta_por bigint;

-- +goose Down
ALTER TABLE mensajeria.llamadas DROP COLUMN accion_meta_por;

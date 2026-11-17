-- +goose Up
-- Lo «siguiente» de una rotación solo se descarta ante un rechazo explícito del servidor repetido en
-- varios latidos seguidos (nunca por un fallo de red): cuántos lleva.
ALTER TABLE mensajeria.voz_motores ADD COLUMN siguiente_rechazos integer NOT NULL DEFAULT 0;
-- La rotación automática que falla no se reintenta en cada latido: cuándo falló la última.
ALTER TABLE mensajeria.voz_motores ADD COLUMN rotacion_fallida_en timestamptz;

-- +goose Down
ALTER TABLE mensajeria.voz_motores DROP COLUMN rotacion_fallida_en;
ALTER TABLE mensajeria.voz_motores DROP COLUMN siguiente_rechazos;

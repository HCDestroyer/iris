-- +goose Up
-- servicio.titular_cambiado en orden (MEDIA 2 de la revisión de #280): la hora que dio Argos
-- (ocurrido_en del webhook) del último cambio de titular aplicado a la instantánea del servicio. Un
-- aviso atrasado (A→B que llega después de B→C) no hace retroceder al titular ni mueve órdenes ni
-- prospectos. Solo guarda y compara horas de Argos: nunca la de recepción de Iris (otro reloj).
ALTER TABLE integracion.argos_servicios ADD COLUMN titular_cambiado_en timestamptz;

-- De dónde vino ocurrido_en de un webhook: argos (el cuerpo trae una hora RFC 3339) o iris (no la
-- trae o no se entiende: la hora de recepción). NULL en los anteriores a esta migración.
ALTER TABLE integracion.entrantes ADD COLUMN ocurrido_origen text
    CHECK (ocurrido_origen IN ('argos', 'iris'));

-- +goose Down
ALTER TABLE integracion.entrantes DROP COLUMN ocurrido_origen;
ALTER TABLE integracion.argos_servicios DROP COLUMN titular_cambiado_en;

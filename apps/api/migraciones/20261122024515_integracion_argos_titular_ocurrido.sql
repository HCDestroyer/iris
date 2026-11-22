-- +goose Up
-- servicio.titular_cambiado en orden (MEDIA 2 de la revisión de #280): la hora (ocurrido_en del
-- webhook) del último cambio de titular aplicado a la instantánea del servicio. Un aviso atrasado
-- (A→B que llega después de B→C) no hace retroceder al titular ni mueve órdenes ni prospectos.
ALTER TABLE integracion.argos_servicios ADD COLUMN titular_cambiado_en timestamptz;

-- +goose Down
ALTER TABLE integracion.argos_servicios DROP COLUMN titular_cambiado_en;

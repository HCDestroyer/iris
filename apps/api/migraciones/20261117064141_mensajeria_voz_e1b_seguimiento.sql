-- +goose Up
-- Voz E1b, seguimiento: una solicitud de permiso de llamada cuyo envío quedó sin respuesta de Meta
-- (pudo llegar al cliente) no se descuenta a ciegas: queda «sin confirmar» hasta resolverla con
-- call_permissions o con la respuesta del cliente.
ALTER TABLE mensajeria.llamada_permisos ADD COLUMN solicitud_sin_confirmar boolean NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE mensajeria.llamada_permisos DROP COLUMN solicitud_sin_confirmar;

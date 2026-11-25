-- +goose Up
-- Alta pendiente hasta activar (contrato argosisp.md 0.10.0 §3.4.8, pedido B17): con el servicio
-- creado en Argos, el prospecto queda «ganado, alta pendiente» (esperando_activacion), ligado al
-- cliente y al servicio, hasta que la activación de la instalación llega a Argos; ahí pasa a
-- completa. Si cambia el titular (servicio.titular_cambiado), pasa al cliente nuevo.
ALTER TABLE comercial.prospectos DROP CONSTRAINT prospectos_alta_estado_check;
ALTER TABLE comercial.prospectos ADD CONSTRAINT prospectos_alta_estado_check
    CHECK (alta_estado IN ('esperando_cliente', 'esperando_servicio', 'esperando_activacion', 'completa'));

-- Prospectos por servicio de Argos (cambio de titular) y por orden (activación).
CREATE INDEX prospectos_servicio_argos_idx ON comercial.prospectos (servicio_argos_id) WHERE servicio_argos_id IS NOT NULL;
CREATE INDEX prospectos_orden_activacion_idx ON comercial.prospectos (orden_id) WHERE alta_estado = 'esperando_activacion';

-- Aviso de campana al vendedor cuando la instalación cambia el plan o el titular (B20).
ALTER TABLE notificaciones.avisos DROP CONSTRAINT avisos_tipo_check;
ALTER TABLE notificaciones.avisos ADD CONSTRAINT avisos_tipo_check
    CHECK (tipo IN ('asistente_tope', 'servidor_ia_caido', 'servidor_ia_incompatible', 'servidor_ia_umbral', 'argos_certificado',
                    'venta_revisada'));

-- +goose Down
DELETE FROM notificaciones.avisos WHERE tipo = 'venta_revisada';
ALTER TABLE notificaciones.avisos DROP CONSTRAINT avisos_tipo_check;
ALTER TABLE notificaciones.avisos ADD CONSTRAINT avisos_tipo_check
    CHECK (tipo IN ('asistente_tope', 'servidor_ia_caido', 'servidor_ia_incompatible', 'servidor_ia_umbral', 'argos_certificado'));
DROP INDEX comercial.prospectos_orden_activacion_idx;
DROP INDEX comercial.prospectos_servicio_argos_idx;
UPDATE comercial.prospectos SET alta_estado = 'completa' WHERE alta_estado = 'esperando_activacion';
ALTER TABLE comercial.prospectos DROP CONSTRAINT prospectos_alta_estado_check;
ALTER TABLE comercial.prospectos ADD CONSTRAINT prospectos_alta_estado_check
    CHECK (alta_estado IN ('esperando_cliente', 'esperando_servicio', 'completa'));

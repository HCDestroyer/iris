-- +goose Up
-- Avisos de campana a los supervisores de la orden (B20, verificación de #280): un paso del
-- titular hecho en Argos con la orden ya terminada (orden.paso_titular_tras_cancelar) y una orden
-- cerrada con el contrato regenerado sin firmar (orden.contrato_sin_firma).
ALTER TABLE notificaciones.avisos DROP CONSTRAINT avisos_tipo_check;
ALTER TABLE notificaciones.avisos ADD CONSTRAINT avisos_tipo_check
    CHECK (tipo IN ('asistente_tope', 'servidor_ia_caido', 'servidor_ia_incompatible', 'servidor_ia_umbral', 'argos_certificado',
                    'venta_revisada', 'paso_titular', 'contrato_sin_firma'));

-- +goose Down
DELETE FROM notificaciones.avisos WHERE tipo IN ('paso_titular', 'contrato_sin_firma');
ALTER TABLE notificaciones.avisos DROP CONSTRAINT avisos_tipo_check;
ALTER TABLE notificaciones.avisos ADD CONSTRAINT avisos_tipo_check
    CHECK (tipo IN ('asistente_tope', 'servidor_ia_caido', 'servidor_ia_incompatible', 'servidor_ia_umbral', 'argos_certificado',
                    'venta_revisada'));

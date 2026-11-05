-- +goose Up
-- Receptor de webhooks del otro sistema (Iris → Argos, contrato Iris ↔ ArgosISP 0.6.0 §6.6): la URL y
-- el secreto de entrada que Argos entrega en el canje (`webhook: { url, secreto }`, §1.3). El secreto
-- va cifrado con IRIS_CLAVE_CIFRADO y atado a la fila ("conector:<id>:webhook_salida_secreto"), como
-- el token; se rota con `iris clave rotar`. Null en conectores anteriores a 0.6.0 (hay que reconectar
-- para recibir el receptor) y en Horus.
ALTER TABLE integracion.conectores ADD COLUMN webhook_salida_url text;
ALTER TABLE integracion.conectores ADD COLUMN webhook_salida_secreto_cifrado bytea;
ALTER TABLE integracion.conectores ADD CONSTRAINT conectores_webhook_salida_completo
    CHECK ((webhook_salida_url IS NULL) = (webhook_salida_secreto_cifrado IS NULL));

-- +goose Down
ALTER TABLE integracion.conectores DROP CONSTRAINT conectores_webhook_salida_completo;
ALTER TABLE integracion.conectores DROP COLUMN webhook_salida_secreto_cifrado;
ALTER TABLE integracion.conectores DROP COLUMN webhook_salida_url;

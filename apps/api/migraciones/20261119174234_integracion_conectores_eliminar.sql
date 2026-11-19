-- +goose Up
-- Eliminar conectores apagados que ya no sirven (integraciones.eliminar): borrado lógico. La fila
-- queda (entrantes, pendientes, auditoría y el historial la siguen referenciando), pero sus
-- credenciales cifradas (token atk_, secreto de webhooks y secreto de salida) se borran, y deja de
-- aparecer en la lista. Solo un conector apagado se elimina.
ALTER TABLE integracion.conectores ALTER COLUMN token_cifrado DROP NOT NULL;
ALTER TABLE integracion.conectores ALTER COLUMN secreto_webhook_cifrado DROP NOT NULL;
ALTER TABLE integracion.conectores ADD COLUMN eliminado_en  timestamptz;
ALTER TABLE integracion.conectores ADD COLUMN eliminado_por bigint REFERENCES nucleo.usuarios (id);
ALTER TABLE integracion.conectores ADD CONSTRAINT conectores_credenciales_si_no_eliminado
    CHECK (CASE WHEN eliminado_en IS NULL THEN token_cifrado IS NOT NULL AND secreto_webhook_cifrado IS NOT NULL
                ELSE token_cifrado IS NULL AND secreto_webhook_cifrado IS NULL AND webhook_salida_secreto_cifrado IS NULL
                     AND estado = 'apagado' END);

-- +goose Down
-- Los eliminados no tienen credenciales: reciben bytes inservibles (no descifran) para volver a NOT NULL.
ALTER TABLE integracion.conectores DROP CONSTRAINT conectores_credenciales_si_no_eliminado;
UPDATE integracion.conectores SET token_cifrado = '\x00'::bytea, secreto_webhook_cifrado = '\x00'::bytea
WHERE token_cifrado IS NULL OR secreto_webhook_cifrado IS NULL;
ALTER TABLE integracion.conectores DROP COLUMN eliminado_por;
ALTER TABLE integracion.conectores DROP COLUMN eliminado_en;
ALTER TABLE integracion.conectores ALTER COLUMN secreto_webhook_cifrado SET NOT NULL;
ALTER TABLE integracion.conectores ALTER COLUMN token_cifrado SET NOT NULL;

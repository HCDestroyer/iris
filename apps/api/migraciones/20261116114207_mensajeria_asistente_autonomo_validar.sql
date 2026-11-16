-- +goose NO TRANSACTION
-- +goose Up
-- Valida, fuera de una transacción, las restricciones que la fase 3b agregó sin validar: VALIDATE
-- escanea la tabla una vez con un candado que no bloquea lecturas ni escrituras, y al no ir dentro de
-- la transacción de la migración no retiene el candado del ALTER que la agregó.
ALTER TABLE auditoria.acciones VALIDATE CONSTRAINT acciones_actor_tipo_check;
ALTER TABLE mensajeria.conversacion_mensajes VALIDATE CONSTRAINT conversacion_mensajes_automatico_check;
ALTER TABLE mensajeria.asistente_acciones VALIDATE CONSTRAINT asistente_acciones_conversacion_fk;

-- +goose Down
-- Nada que deshacer: una restricción validada no se «desvalida».
SELECT 1;

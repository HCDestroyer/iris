-- +goose NO TRANSACTION
-- +goose Up
-- Valida, fuera de una transacción, las restricciones que …_asistente_pausa agregó sin validar:
-- VALIDATE recorre la tabla una vez con un candado que no bloquea lecturas ni escrituras.
ALTER TABLE mensajeria.conversaciones VALIDATE CONSTRAINT conversaciones_asistente_pausado_por_fk;
ALTER TABLE mensajeria.conversaciones VALIDATE CONSTRAINT conversaciones_asistente_pausa_motivo_ck;
ALTER TABLE mensajeria.conversaciones VALIDATE CONSTRAINT conversaciones_asistente_pausa_ck;

-- Lo que ya quedó mintiendo en la bandeja: conversaciones con el asistente detenido que todavía
-- muestran su última actividad. Aquí, fuera de la transacción del ALTER TABLE, solo bloquea las filas
-- que cambia.
UPDATE mensajeria.conversaciones
SET asistente_actividad_tipo = NULL, asistente_actividad_texto = NULL, asistente_actividad_en = NULL
WHERE asistente_estado = 'detenido' AND asistente_actividad_tipo IS NOT NULL;

-- +goose Down
-- Nada que deshacer: una restricción validada no se «desvalida».
SELECT 1;

-- +goose Up
-- Revisión de seguridad de #244 (contratos/conversaciones.md §17.11): pausar deja detenido al
-- asistente aunque antes esperara a una persona (espera_persona) o estuviera detenido por una
-- persona. asistente_pausa_previo guarda el estado del asistente justo antes de la pausa, para que
-- al reanudar se sepa si la devuelven después de una persona (atención nueva: maxTurnos se
-- reinicia) o si solo se quita una pausa (no se reinicia). Solo se lee con la pausa puesta: cada
-- pausa lo vuelve a escribir. Columna nula, sin valor por defecto: el ALTER no recorre la tabla.
ALTER TABLE mensajeria.conversaciones ADD COLUMN asistente_pausa_previo text;
ALTER TABLE mensajeria.conversaciones ADD CONSTRAINT conversaciones_asistente_pausa_previo_ck
    CHECK (asistente_pausa_previo IS NULL OR asistente_pausa_previo IN ('atendiendo', 'espera_persona', 'detenido')) NOT VALID;

-- +goose Down
ALTER TABLE mensajeria.conversaciones DROP CONSTRAINT conversaciones_asistente_pausa_previo_ck;
ALTER TABLE mensajeria.conversaciones DROP COLUMN asistente_pausa_previo;

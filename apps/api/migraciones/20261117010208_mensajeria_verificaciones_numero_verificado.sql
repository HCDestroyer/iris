-- +goose Up
-- Cada intento del desafío de identidad recuerda si el número de su conversación estaba verificado
-- como del cliente. Los de números verificados comparten el tope por cliente; los de un número
-- asociado a mano (sin verificar) se cuentan aparte y no le gastan los intentos al titular
-- (contratos/conversaciones.md §17.8.2).
ALTER TABLE mensajeria.asistente_verificaciones ADD COLUMN numero_verificado boolean NOT NULL DEFAULT true;
UPDATE mensajeria.asistente_verificaciones v SET numero_verificado = c.cliente_verificado
FROM mensajeria.conversaciones c WHERE c.id = v.conversacion_id;

-- +goose Down
ALTER TABLE mensajeria.asistente_verificaciones DROP COLUMN numero_verificado;

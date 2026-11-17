-- +goose Up
-- Voz E1, seguimiento: la reserva de una llamada es de un usuario EN un navegador (el de «Recibir
-- llamadas»), y un navegador apagado por el servidor dice por qué.
ALTER TABLE mensajeria.llamadas ADD COLUMN reclamada_navegador text;
ALTER TABLE mensajeria.llamada_receptores ADD COLUMN apagado_motivo text CHECK (apagado_motivo IN ('tope', 'inactivo'));

-- +goose Down
ALTER TABLE mensajeria.llamada_receptores DROP COLUMN apagado_motivo;
ALTER TABLE mensajeria.llamadas DROP COLUMN reclamada_navegador;

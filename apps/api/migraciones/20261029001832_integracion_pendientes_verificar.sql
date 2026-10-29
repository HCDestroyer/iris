-- +goose Up
-- Campos de identidad que la respuesta del 2xx debe repetir igual que el cuerpo enviado (p. ej.
-- codigo_externo en POST /clientes). Si no coinciden, el pendiente queda fallido_negocio
-- (respuesta_inconsistente) y no resuelve a sus dependientes.
ALTER TABLE integracion.pendientes ADD COLUMN verificar text[] NOT NULL DEFAULT '{}';

-- +goose Down
ALTER TABLE integracion.pendientes DROP COLUMN verificar;

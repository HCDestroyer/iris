-- +goose Up
-- El estado del número (ultimo_estado) lo puso un envío que Meta rechazó por el token o los permisos,
-- no «Probar». Solo esa marca la limpia sola el siguiente envío que Meta acepta.
ALTER TABLE mensajeria.canales ADD COLUMN rechazado_por_envio boolean NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE mensajeria.canales DROP COLUMN rechazado_por_envio;

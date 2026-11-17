-- +goose Up
-- El estado del servidor después de cada latido, junto a su muestra: un episodio «caído» o
-- «incompatible» solo se cierra tras varias lecturas seguidas fuera de ese estado (histéresis).
ALTER TABLE mensajeria.servidor_ia_muestras ADD COLUMN estado text;

-- +goose Down
ALTER TABLE mensajeria.servidor_ia_muestras DROP COLUMN estado;

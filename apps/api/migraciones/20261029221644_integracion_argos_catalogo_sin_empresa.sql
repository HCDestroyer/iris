-- +goose Up
-- Zonas y routers de Argos sin empresa sugerida (los usan todas las empresas; multiempresa.md de
-- Argos): empresa_id NULL, como agenda.zonas, en vez de 0.
ALTER TABLE integracion.argos_zonas ALTER COLUMN empresa_id DROP NOT NULL;
ALTER TABLE integracion.argos_routers ALTER COLUMN empresa_id DROP NOT NULL;
UPDATE integracion.argos_zonas SET empresa_id = NULL WHERE empresa_id = 0;
UPDATE integracion.argos_routers SET empresa_id = NULL WHERE empresa_id = 0;
UPDATE agenda.zonas SET empresa_id = NULL WHERE empresa_id = 0 AND origen = 'argos';

-- +goose Down
UPDATE integracion.argos_zonas SET empresa_id = 0 WHERE empresa_id IS NULL;
UPDATE integracion.argos_routers SET empresa_id = 0 WHERE empresa_id IS NULL;
ALTER TABLE integracion.argos_zonas ALTER COLUMN empresa_id SET NOT NULL;
ALTER TABLE integracion.argos_routers ALTER COLUMN empresa_id SET NOT NULL;

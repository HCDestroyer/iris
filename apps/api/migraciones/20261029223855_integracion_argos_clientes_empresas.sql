-- +goose Up
-- En Argos el cliente no tiene empresa: la tienen sus servicios (multiempresa.md de Argos). La
-- instantánea guarda las empresas del cliente (las de sus servicios) y Iris lo muestra en una
-- empresa si es una de ellas, o en todas si no tiene servicios (como las listas de Argos).
-- empresa_id queda como la primera, por compatibilidad.
ALTER TABLE integracion.argos_clientes ADD COLUMN empresas bigint[] NOT NULL DEFAULT '{}';
UPDATE integracion.argos_clientes SET empresas = ARRAY[empresa_id] WHERE empresa_id <> 0;
CREATE INDEX argos_clientes_empresas_idx ON integracion.argos_clientes USING gin (empresas);

-- +goose Down
DROP INDEX integracion.argos_clientes_empresas_idx;
ALTER TABLE integracion.argos_clientes DROP COLUMN empresas;

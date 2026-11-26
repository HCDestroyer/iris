-- +goose Up
-- Páginas leídas de los servicios de una falla general (contrato 0.13.0 §3.11): la lectura paginada
-- tiene un máximo y no se repite sin fin si Argos devuelve el mismo cursor.
ALTER TABLE integracion.argos_incidente_lecturas ADD COLUMN paginas integer NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE integracion.argos_incidente_lecturas DROP COLUMN paginas;

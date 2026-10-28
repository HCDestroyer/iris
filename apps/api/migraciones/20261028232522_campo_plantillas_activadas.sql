-- +goose Up
-- Campo (I2, revisión de la PR #29): una plantilla que estuvo activa alguna vez ya viajó en el
-- catálogo de los teléfonos; editarla siempre crea una versión nueva (solo un borrador que nunca
-- se activó se reescribe en el lugar). Las existentes cuentan como activadas.
ALTER TABLE campo.plantillas_checklist ADD COLUMN alguna_vez_activa boolean NOT NULL DEFAULT true;

-- +goose Down
ALTER TABLE campo.plantillas_checklist DROP COLUMN alguna_vez_activa;

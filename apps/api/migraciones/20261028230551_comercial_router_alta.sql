-- +goose Up
-- Revisión de la PR #28:
--  - router_id: el router del alta en Argos (POST /servicios lo exige): el sugerido por la
--    factibilidad o el que se elige al ganar.
--  - alta_error: el alta en Argos no pudo avanzar y necesita revisión manual (id recuperado que
--    no es del prospecto, rechazo de Argos al recuperar, pendiente hecho hace más de 30 días).
--  - factibilidades.anula_id: la manual que contradice una automática no_viable la anula con motivo.
ALTER TABLE comercial.prospectos ADD COLUMN router_id bigint, ADD COLUMN alta_error text;
ALTER TABLE comercial.factibilidades
    ADD COLUMN anula_id bigint REFERENCES comercial.factibilidades (id),
    ADD COLUMN motivo_anulacion text;

-- +goose Down
ALTER TABLE comercial.factibilidades DROP COLUMN motivo_anulacion, DROP COLUMN anula_id;
ALTER TABLE comercial.prospectos DROP COLUMN alta_error, DROP COLUMN router_id;

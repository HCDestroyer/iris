-- +goose Up
-- El plan que pidió el cliente en su solicitud del portal de Argos (§11.7), fijo: plan_interes_id se
-- puede editar (prospectos.editar), así que ganar un cambio de plan compara contra este (revisión M1).
-- solicitud_plan_no_validado: Argos no respondió al validarlo (la solicitud se guardó sin plan).
ALTER TABLE comercial.prospectos
    ADD COLUMN solicitud_plan_id bigint,
    ADD COLUMN solicitud_plan_no_validado boolean NOT NULL DEFAULT false,
    ADD CONSTRAINT prospectos_solicitud_plan_check CHECK (
        solicitud_tipo IS NOT NULL OR (solicitud_plan_id IS NULL AND NOT solicitud_plan_no_validado));
-- Las solicitudes que ya existen: no se sabe si su plan_interes_id se editó después de crearlas, así que
-- se guarda como referencia pero se marcan sin validar (falla cerrado: ganar un cambio de plan pide motivo).
UPDATE comercial.prospectos SET solicitud_plan_id = plan_interes_id, solicitud_plan_no_validado = true
WHERE solicitud_tipo IS NOT NULL;

-- +goose Down
ALTER TABLE comercial.prospectos DROP CONSTRAINT prospectos_solicitud_plan_check,
    DROP COLUMN solicitud_plan_no_validado, DROP COLUMN solicitud_plan_id;

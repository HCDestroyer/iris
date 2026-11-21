-- +goose Up
-- Webhooks Iris → Argos de la bodega de cuadrilla (contrato 0.11.0 §6.6, B26): cuadrilla.creada y
-- cuadrilla.actualizada, solo a conectores con contrato ≥ 0.11.0.
ALTER TABLE mensajeria.webhooks_salida DROP CONSTRAINT webhooks_salida_evento_check;
ALTER TABLE mensajeria.webhooks_salida ADD CONSTRAINT webhooks_salida_evento_check
    CHECK (evento IN ('aviso.entregado', 'aviso.fallido', 'contacto.baja', 'cuadrilla.creada', 'cuadrilla.actualizada'));

-- +goose Down
DELETE FROM mensajeria.webhooks_salida WHERE evento IN ('cuadrilla.creada', 'cuadrilla.actualizada');
ALTER TABLE mensajeria.webhooks_salida DROP CONSTRAINT webhooks_salida_evento_check;
ALTER TABLE mensajeria.webhooks_salida ADD CONSTRAINT webhooks_salida_evento_check
    CHECK (evento IN ('aviso.entregado', 'aviso.fallido', 'contacto.baja'));

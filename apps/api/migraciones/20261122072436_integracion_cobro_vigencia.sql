-- +goose Up
-- Vigencia de los cargos (contrato 0.14.3 §3, revisión de #286):
--
--   * exige_por_anular: el cobro por confirmar tuvo una factura por anular; aunque se quite, «no existe»
--     sigue bloqueado hasta que otra factura por anular se lea anulada.
--   * factura_anulada: Argos anuló la factura ligada al cargo (factura.anulada): deja de contar como
--     vigente.
--   * Cargos que ya salieron antes de esta versión: primer_envio_en = created_at y, si no están hechos,
--     envio_sin_respuesta (no se sabe si el envío anterior tuvo respuesta: falla cerrado).
ALTER TABLE integracion.pendientes
    ADD COLUMN exige_por_anular boolean NOT NULL DEFAULT false,
    ADD COLUMN factura_anulada boolean NOT NULL DEFAULT false;
UPDATE integracion.pendientes SET exige_por_anular = true WHERE factura_por_anular_id IS NOT NULL;
UPDATE integracion.pendientes SET primer_envio_en = created_at WHERE intentos > 0 AND primer_envio_en IS NULL;
UPDATE integracion.pendientes SET envio_sin_respuesta = true WHERE intentos > 0 AND estado <> 'hecho' AND operacion = 'cargo';

-- +goose Down
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM integracion.pendientes WHERE exige_por_anular OR factura_anulada) THEN
        RAISE EXCEPTION 'hay cobros que exigen factura por anular o con factura anulada: no se puede bajar esta migración';
    END IF;
END $$;
-- +goose StatementEnd
ALTER TABLE integracion.pendientes DROP COLUMN factura_anulada, DROP COLUMN exige_por_anular;

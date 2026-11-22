-- +goose Up
-- Conciliación del «cobro por confirmar» y cargos que esperan al consumo (contrato 0.14.3 §3):
--
--   * primer_envio_en: cuándo salió por primera vez la llamada (la factura conciliada debe ser de
--     después, con 5 min de tolerancia).
--   * factura_id: la factura de Argos ligada a un cargo de Iris (por su 2xx o por la conciliación de un
--     supervisor). Única: una factura nunca concilia dos cargos.
--   * factura_por_anular_id: factura anterior a la 0.14.3 (líneas sin concepto_id) que el supervisor
--     indicó para un cobro por confirmar. No liga el cargo (no es su factura_id); «no existe» se habilita
--     solo cuando una lectura la ve anulada. Nunca está en dos cargos.
--   * espera_consumo: cargo (forma anterior) de un producto consumible_cobro del consumo de la orden.
--     Nunca sale: queda resuelto cuando el consumo confirma el cobro, o visible al supervisor si no.
ALTER TABLE integracion.pendientes
    ADD COLUMN primer_envio_en timestamptz,
    ADD COLUMN factura_id bigint,
    ADD COLUMN espera_consumo boolean NOT NULL DEFAULT false,
    ADD COLUMN factura_por_anular_id bigint,
    ADD COLUMN factura_por_anular_motivo text;
CREATE UNIQUE INDEX pendientes_factura_uq ON integracion.pendientes (factura_id) WHERE factura_id IS NOT NULL;
ALTER TABLE integracion.pendientes DROP CONSTRAINT pendientes_resuelto_como_check,
    ADD CONSTRAINT pendientes_resuelto_como_check CHECK (resuelto_como IN ('manual', 'reemplazado', 'orden_cancelada', 'orden_fallida',
        'validado_panel', 'cobrado_con_consumo'));
CREATE UNIQUE INDEX pendientes_factura_por_anular_uq ON integracion.pendientes (factura_por_anular_id) WHERE factura_por_anular_id IS NOT NULL;

-- +goose Down
UPDATE integracion.pendientes SET resuelto_como = 'manual' WHERE resuelto_como = 'cobrado_con_consumo';
ALTER TABLE integracion.pendientes DROP CONSTRAINT pendientes_resuelto_como_check,
    ADD CONSTRAINT pendientes_resuelto_como_check CHECK (resuelto_como IN ('manual', 'reemplazado', 'orden_cancelada', 'orden_fallida',
        'validado_panel'));
DROP INDEX integracion.pendientes_factura_por_anular_uq;
DROP INDEX integracion.pendientes_factura_uq;
ALTER TABLE integracion.pendientes DROP COLUMN factura_por_anular_motivo, DROP COLUMN factura_por_anular_id, DROP COLUMN espera_consumo, DROP COLUMN factura_id, DROP COLUMN primer_envio_en;

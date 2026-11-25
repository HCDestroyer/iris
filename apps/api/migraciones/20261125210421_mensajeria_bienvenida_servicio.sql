-- +goose Up
-- Bienvenida al activar (contrato argosisp.md 0.9.0 §12.5 y §6.3, pedido B16).
--  - mensajeria.avisos.orden_id: el `origen` { tipo: iris_orden, id } del aviso bienvenida_servicio
--    (la orden de Iris de ese servicio desde la que se activó).
--  - Cuentas para pagar: Iris las inserta de su instantánea (§7.8). Una cuenta creada o editada en
--    Argos no entra durante 24 h (cambiada_en), o hasta que la confirme un admin de Iris.
ALTER TABLE mensajeria.avisos ADD COLUMN orden_id bigint;

ALTER TABLE integracion.argos_cuentas_bancarias
    ADD COLUMN cambiada_en    timestamptz,
    ADD COLUMN confirmada_por bigint REFERENCES nucleo.usuarios (id),
    ADD COLUMN confirmada_en  timestamptz;

-- +goose Down
ALTER TABLE integracion.argos_cuentas_bancarias DROP COLUMN confirmada_en, DROP COLUMN confirmada_por, DROP COLUMN cambiada_en;
ALTER TABLE mensajeria.avisos DROP COLUMN orden_id;

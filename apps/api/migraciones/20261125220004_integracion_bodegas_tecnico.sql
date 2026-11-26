-- +goose Up
-- Bodegas personales de los técnicos por empresa (contrato con ArgosISP 0.11.0 §3.9 y 0.13.1, B26).
-- argos_bodegas_tecnico: las bodegas personales del técnico en cada empresa, vistas en
-- GET /inventario/mi-bodega. El bodegaId personal del lote se valida contra la empresa de la orden
-- (M-268-1): una bodega personal de otra empresa no vale.
CREATE TABLE integracion.argos_bodegas_tecnico (
    bodega_id  bigint      PRIMARY KEY,
    empresa_id bigint      NOT NULL,
    usuario_id bigint      NOT NULL REFERENCES nucleo.usuarios (id),
    visto_en   timestamptz NOT NULL DEFAULT now(),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX argos_bodegas_tecnico_idx ON integracion.argos_bodegas_tecnico (usuario_id, empresa_id);

-- +goose Down
DROP TABLE IF EXISTS integracion.argos_bodegas_tecnico;

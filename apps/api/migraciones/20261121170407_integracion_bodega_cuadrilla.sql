-- +goose Up
-- Bodega de cuadrilla en el lado de Iris (contrato con ArgosISP 0.11.0 §3.9, B26).
--
-- argos_bodegas_cuadrilla: las bodegas `cuadrilla` de Argos que Iris vio en GET /inventario/mi-bodega
-- (bodega.cuadrilla_id_iris) o en un webhook de traslado (cuadrilla_id). Con ellas el servidor valida,
-- al recibir el lote, que el bodega_id de un material sea la bodega personal del técnico o la de su
-- cuadrilla actual en la empresa de la orden (403 no_es_miembro si no), y sabe a qué cuadrilla va
-- un traslado confirmado con GET /inventario/traslados/:id.
CREATE TABLE integracion.argos_bodegas_cuadrilla (
    bodega_id    bigint      PRIMARY KEY,
    empresa_id   bigint      NOT NULL,
    cuadrilla_id bigint      NOT NULL,
    nombre       text        CHECK (nombre IS NULL OR length(nombre) <= 200),
    visto_en     timestamptz NOT NULL DEFAULT now(),
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX argos_bodegas_cuadrilla_idx ON integracion.argos_bodegas_cuadrilla (cuadrilla_id, empresa_id);

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

-- El traslado a una bodega de cuadrilla lo recibe y firma el jefe (§3.9).
ALTER TABLE integracion.argos_traslados ADD COLUMN cuadrilla_id bigint;
CREATE INDEX argos_traslados_cuadrilla_idx ON integracion.argos_traslados (cuadrilla_id, estado) WHERE cuadrilla_id IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS integracion.argos_traslados_cuadrilla_idx;
ALTER TABLE integracion.argos_traslados DROP COLUMN IF EXISTS cuadrilla_id;
DROP TABLE IF EXISTS integracion.argos_bodegas_tecnico;
DROP TABLE IF EXISTS integracion.argos_bodegas_cuadrilla;

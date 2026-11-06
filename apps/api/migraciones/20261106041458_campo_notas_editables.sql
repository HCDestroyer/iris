-- +goose Up
-- Notas de la orden editables y eliminables (ordenes.editar_nota y ordenes.eliminar_nota).
-- version: control de concurrencia por nota (409 si cambió). Eliminar es lógico: la nota sale de
-- la orden pero queda la fila (y su historial en auditoria.cambios).
ALTER TABLE campo.orden_notas
    ADD COLUMN version       integer     NOT NULL DEFAULT 1 CHECK (version >= 1),
    ADD COLUMN editada_en    timestamptz,
    ADD COLUMN editada_por   bigint      REFERENCES nucleo.usuarios (id),
    ADD COLUMN eliminada_en  timestamptz,
    ADD COLUMN eliminada_por bigint      REFERENCES nucleo.usuarios (id);

-- +goose Down
ALTER TABLE campo.orden_notas
    DROP COLUMN eliminada_por,
    DROP COLUMN eliminada_en,
    DROP COLUMN editada_por,
    DROP COLUMN editada_en,
    DROP COLUMN version;

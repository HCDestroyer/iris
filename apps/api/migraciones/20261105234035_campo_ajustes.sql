-- +goose Up
-- Ajustes de la app de campo por empresa (empresa_id null = los de Iris sin conector), con el
-- mismo patrón que comercial.umbrales_factibilidad: una fila por empresa, versión y una acción
-- explícita para fijarlos (POST /campo/ajustes, permiso campo.configurar). Sin fila, los valores
-- por defecto (todo apagado).
--  - tecnicos_toman_libres: el técnico puede tomar una orden libre de su zona o cuadrilla
--    (POST /ordenes/{id}/tomar, permiso ordenes.tomar).
CREATE TABLE campo.ajustes (
    id                    bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    empresa_id            bigint,
    tecnicos_toman_libres boolean     NOT NULL DEFAULT false,
    version               integer     NOT NULL DEFAULT 1,
    created_at            timestamptz NOT NULL DEFAULT now(),
    updated_at            timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX ajustes_empresa_uq ON campo.ajustes (coalesce(empresa_id, 0));

-- Órdenes libres por zona (GET /campo/ordenes-libres).
CREATE INDEX ordenes_libres_idx ON campo.ordenes (empresa_id, zona_id) WHERE tecnico_id IS NULL AND estado = 'pendiente';

-- +goose Down
DROP INDEX campo.ordenes_libres_idx;
DROP TABLE campo.ajustes;

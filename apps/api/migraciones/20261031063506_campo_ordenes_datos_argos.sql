-- +goose Up
-- servicio.datos_cambiados / cliente.actualizado (contrato 0.5.0): una orden abierta conserva la
-- dirección y el contacto que copió al crearse; se marca «dato de Argos cambió» (cuándo y qué
-- campos) para que despacho lo aplique. No sube la versión de la orden (no choca con la app).
ALTER TABLE campo.ordenes
    ADD COLUMN datos_argos_cambiados_en timestamptz,
    ADD COLUMN datos_argos_campos       text[] NOT NULL DEFAULT '{}';

-- Modalidad del producto según Argos (0.5.0) al registrar el material: un comodato no va en el
-- consumo (va por POST /servicios/:id/equipos). null = desconocida (Argos anterior o sin conector).
ALTER TABLE campo.orden_materiales
    ADD COLUMN modalidad text CHECK (modalidad IS NULL OR modalidad IN ('comodato', 'consumible_gasto', 'consumible_cobro'));

-- +goose Down
ALTER TABLE campo.orden_materiales DROP COLUMN modalidad;
ALTER TABLE campo.ordenes DROP COLUMN datos_argos_cambiados_en, DROP COLUMN datos_argos_campos;

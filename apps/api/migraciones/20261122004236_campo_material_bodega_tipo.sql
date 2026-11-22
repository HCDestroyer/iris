-- +goose Up
-- El tipo de la bodega según Argos al registrar un material desde el panel (0.13.1 §3.9, M-4):
-- despacho solo usa bodegas tipo = bodega; el plan deja incompleto un tipo desconocido.
ALTER TABLE campo.orden_materiales ADD COLUMN bodega_tipo text CHECK (bodega_tipo IS NULL OR length(bodega_tipo) <= 30);
-- desde_tecnico: lo registró el técnico de la orden (desde la app o desde la API con su sesión):
-- sale con su tecnico y su X-Iris-Actor; si no, lo indicó despacho (solo bodegas tipo = bodega).
ALTER TABLE campo.orden_materiales ADD COLUMN desde_tecnico boolean NOT NULL DEFAULT false;
UPDATE campo.orden_materiales SET desde_tecnico = true WHERE operacion_id IS NOT NULL;

-- +goose Down
ALTER TABLE campo.orden_materiales DROP COLUMN IF EXISTS desde_tecnico;
ALTER TABLE campo.orden_materiales DROP COLUMN IF EXISTS bodega_tipo;

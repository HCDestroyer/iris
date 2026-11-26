-- +goose Up
-- Contrato con ArgosISP 0.14.0 §3.10: con un Argos ≥ 0.14.0, el cambio y el intento de recuperación
-- se encolan hacia Argos al aplicarse la operación del lote; el cierre de la orden solo los resume.
ALTER TABLE campo.orden_equipos ADD COLUMN enviado_al_aplicar boolean NOT NULL DEFAULT false;

-- Una orden puede llevar más de un intento de recuperación de la misma unidad, en días distintos
-- (0.14.0 §3.10; el límite de uno al día lo aplica el caso de uso): el índice único por unidad ya no
-- vale para no_recuperado.
DROP INDEX campo.orden_equipos_unidad_uq;
CREATE UNIQUE INDEX orden_equipos_unidad_uq ON campo.orden_equipos (orden_id, accion, unidad_id)
    WHERE unidad_id IS NOT NULL AND accion <> 'no_recuperado';

-- +goose Down
-- Antes de volver al índice único por unidad: varios intentos sobre la misma unidad no caben en él
-- (el Down de 20261125220003 los borraría igual, pero corre después).
DELETE FROM campo.orden_equipos WHERE accion = 'no_recuperado';
DROP INDEX campo.orden_equipos_unidad_uq;
CREATE UNIQUE INDEX orden_equipos_unidad_uq ON campo.orden_equipos (orden_id, accion, unidad_id) WHERE unidad_id IS NOT NULL;
ALTER TABLE campo.orden_equipos DROP COLUMN IF EXISTS enviado_al_aplicar;

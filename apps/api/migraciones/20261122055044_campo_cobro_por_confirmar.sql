-- +goose Up
-- Cobro de la instalación, contrato argosisp.md 0.14.3 (§3, «0.9.0:»):
--
--   * El cargo del cierre lleva lineas[{ concepto_id, precio_unitario }] (cargo_concepto_id del tipo o
--     de la plantilla; el precio es cargo_monto). La línea de instalación es la del concepto con
--     codigo_sistema = "instalacion" (GET /conceptos-cargo), nunca por el nombre.
--   * El cobro de A36 guarda si su concepto es el de instalación (es_instalacion): el cargo del cierre
--     sale sin esa línea si hay uno vigente (pendiente, facturado, pagado o por confirmar).
--   * Un cargo que recibe 422 capacidad_no_disponible queda terminal (nunca se reintenta con su clave);
--     las demás líneas salen como un cobro de reemplazo (otra clave), que nunca genera otro.
--   * Si ese 422 llega tras un envío sin respuesta (timeout o 5xx), el cargo queda «por confirmar»
--     hasta que un supervisor diga si la factura existe en Argos.

ALTER TABLE campo.tipos_orden ADD COLUMN cargo_concepto_id bigint CHECK (cargo_concepto_id > 0);
ALTER TABLE campo.plantillas_checklist ADD COLUMN cargo_concepto_id bigint CHECK (cargo_concepto_id > 0);

ALTER TABLE campo.cobros
    ADD COLUMN es_instalacion boolean NOT NULL DEFAULT true,
    DROP CONSTRAINT cobros_estado_check,
    ADD CONSTRAINT cobros_estado_check CHECK (estado IN ('pendiente', 'facturado', 'pagado', 'anulado', 'fallido', 'por_confirmar'));
DROP INDEX campo.cobros_vivo_uq;
CREATE UNIQUE INDEX cobros_vivo_uq ON campo.cobros (orden_id) WHERE estado IN ('pendiente', 'facturado', 'pagado', 'por_confirmar');

ALTER TABLE integracion.pendientes
    -- Algún intento quedó sin respuesta de Argos (timeout o 5xx): Argos pudo haberlo hecho.
    ADD COLUMN envio_sin_respuesta boolean NOT NULL DEFAULT false,
    -- Cargo rechazado con 422 capacidad_no_disponible: nunca se reintenta con su clave.
    ADD COLUMN cargo_terminal boolean NOT NULL DEFAULT false,
    -- Cargo terminal tras un envío sin respuesta: espera la confirmación de un supervisor.
    ADD COLUMN por_confirmar boolean NOT NULL DEFAULT false,
    ADD COLUMN confirmado_motivo text,
    ADD COLUMN confirmado_por bigint REFERENCES nucleo.usuarios (id),
    ADD COLUMN confirmado_en timestamptz,
    -- Cobro de reemplazo (sin la línea de instalación) y su original.
    ADD COLUMN reemplazo_de bigint REFERENCES integracion.pendientes (id),
    ADD COLUMN reemplazado_por bigint REFERENCES integracion.pendientes (id),
    ADD CONSTRAINT pendientes_por_confirmar_ck CHECK (NOT por_confirmar OR cargo_terminal);
CREATE UNIQUE INDEX pendientes_reemplazo_uq ON integracion.pendientes (reemplazo_de) WHERE reemplazo_de IS NOT NULL;

ALTER TABLE campo.orden_eventos
    DROP CONSTRAINT orden_eventos_tipo_check,
    ADD CONSTRAINT orden_eventos_tipo_check CHECK (tipo IN ('creada', 'estado', 'asignada', 'programada', 'formulario', 'activacion', 'cobro'));

-- +goose Down
-- Un cargo terminal, por confirmar o con su reemplazo no se puede representar sin estas columnas:
-- bajar la migración los dejaría reintentables (cobro doble). Se resuelven antes, o no se baja.
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM integracion.pendientes WHERE cargo_terminal OR por_confirmar OR reemplazo_de IS NOT NULL)
        OR EXISTS (SELECT 1 FROM campo.cobros WHERE estado = 'por_confirmar') THEN
        RAISE EXCEPTION 'hay cargos terminales, por confirmar o de reemplazo: no se puede bajar esta migración';
    END IF;
END $$;
-- +goose StatementEnd
DELETE FROM campo.orden_eventos WHERE tipo = 'cobro';
ALTER TABLE campo.orden_eventos
    DROP CONSTRAINT orden_eventos_tipo_check,
    ADD CONSTRAINT orden_eventos_tipo_check CHECK (tipo IN ('creada', 'estado', 'asignada', 'programada', 'formulario', 'activacion'));
DROP INDEX integracion.pendientes_reemplazo_uq;
ALTER TABLE integracion.pendientes
    DROP CONSTRAINT pendientes_por_confirmar_ck,
    DROP COLUMN reemplazado_por, DROP COLUMN reemplazo_de, DROP COLUMN confirmado_en, DROP COLUMN confirmado_por,
    DROP COLUMN confirmado_motivo, DROP COLUMN por_confirmar, DROP COLUMN cargo_terminal, DROP COLUMN envio_sin_respuesta;
DROP INDEX campo.cobros_vivo_uq;
CREATE UNIQUE INDEX cobros_vivo_uq ON campo.cobros (orden_id) WHERE estado IN ('pendiente', 'facturado', 'pagado');
ALTER TABLE campo.cobros
    DROP CONSTRAINT cobros_estado_check,
    ADD CONSTRAINT cobros_estado_check CHECK (estado IN ('pendiente', 'facturado', 'pagado', 'anulado', 'fallido')),
    DROP COLUMN es_instalacion;
ALTER TABLE campo.plantillas_checklist DROP COLUMN cargo_concepto_id;
ALTER TABLE campo.tipos_orden DROP COLUMN cargo_concepto_id;

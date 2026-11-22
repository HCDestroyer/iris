-- +goose Up
-- Cobro de la instalación desde la orden (contrato argosisp.md 0.9.0 §3 y A36, pedido B15). Un agente
-- lo registra (concepto y precio de la plantilla; cambiar el precio exige ordenes.cobro_editar_monto,
-- con motivo) y sale en el momento a Argos (POST /facturas/cargo-unico) por la cola de pendientes con
-- Idempotency-Key iris:cargo:<orden_id>:<cargo_id>. Nunca Q0. La orden sigue la factura por los
-- webhooks factura.* y pago.* (emitida, pagada, anulada). Un cobro nuevo tras un anulado lleva otro
-- cargo_id (nunca se reusa la clave).

ALTER TABLE campo.plantillas_checklist
    ADD COLUMN cobro_concepto_id bigint CHECK (cobro_concepto_id > 0),
    ADD COLUMN cobro_precio      numeric(12, 2) CHECK (cobro_precio > 0);

CREATE TABLE campo.cobros (
    id                uuid           PRIMARY KEY,                  -- cargo_id de la clave
    orden_id          bigint         NOT NULL REFERENCES campo.ordenes (id),
    empresa_id        bigint,
    servicio_id       bigint         NOT NULL,
    cliente_id        bigint         NOT NULL,
    concepto_id       bigint         NOT NULL CHECK (concepto_id > 0),
    precio_unitario   numeric(12, 2) NOT NULL CHECK (precio_unitario > 0),
    precio_plantilla  numeric(12, 2),
    motivo_precio     text,
    descripcion       text,
    registrado_por    bigint         REFERENCES nucleo.usuarios (id),
    estado            text           NOT NULL CHECK (estado IN ('pendiente', 'facturado', 'pagado', 'anulado', 'fallido')),
    idempotency_key   text           NOT NULL UNIQUE,
    pendiente_id      bigint,
    factura_id        bigint,
    numero            text,
    total             numeric(14, 2),
    moneda            text,
    detalle           text,
    version           integer        NOT NULL DEFAULT 1,
    created_at        timestamptz    NOT NULL DEFAULT now(),
    updated_at        timestamptz    NOT NULL DEFAULT now()
);
-- Un cobro de instalación vivo por orden: el anulado o el fallido dejan cobrar otra vez (otro cargo_id).
CREATE UNIQUE INDEX cobros_vivo_uq ON campo.cobros (orden_id) WHERE estado IN ('pendiente', 'facturado', 'pagado');
CREATE INDEX cobros_factura_idx ON campo.cobros (factura_id) WHERE factura_id IS NOT NULL;

-- +goose Down
DROP TABLE campo.cobros;
ALTER TABLE campo.plantillas_checklist DROP COLUMN cobro_precio, DROP COLUMN cobro_concepto_id;

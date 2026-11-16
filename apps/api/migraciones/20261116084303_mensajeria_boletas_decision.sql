-- +goose Up
-- Aprobar y rechazar boletas desde la conversación (contrato con Argos 0.7.0 §7.7, pedido B3).
-- La boleta guarda quién decidió y lo que respondió Argos; `resolucion` es lo que trajo el webhook
-- comprobante.aprobado / comprobante.rechazado y que el proceso de mensajería todavía no aplica.
ALTER TABLE mensajeria.boletas
    ADD COLUMN decidida_en           timestamptz,
    -- El usuario de Iris que decidió desde la conversación; null si decidió caja en el panel de Argos.
    ADD COLUMN decidida_por          bigint REFERENCES nucleo.usuarios (id),
    ADD COLUMN pago_id               bigint,
    ADD COLUMN motivo_rechazo        text CHECK (motivo_rechazo IS NULL OR char_length(motivo_rechazo) <= 500),
    ADD COLUMN causa_rechazo         text CHECK (causa_rechazo IS NULL OR causa_rechazo IN ('no_aparece', 'monto_distinto', 'ilegible', 'falta_dato', 'duplicado', 'otro')),
    -- El usuario de Argos que decidió, como lo informa Argos (verificado_por_nombre).
    ADD COLUMN verificado_por_nombre text CHECK (verificado_por_nombre IS NULL OR char_length(verificado_por_nombre) <= 200),
    -- El aviso al cliente sale una sola vez, al resolver la boleta: enviado (con su mensaje) o sin_aviso
    -- (ventana de 24 h cerrada sin plantilla aprobada ni permiso, baja o número apagado).
    ADD COLUMN aviso_cliente         text CHECK (aviso_cliente IS NULL OR aviso_cliente IN ('enviado', 'sin_aviso')),
    ADD COLUMN aviso_mensaje_id      uuid,
    ADD COLUMN resolucion            jsonb,
    ADD COLUMN resolucion_en         timestamptz;
CREATE INDEX boletas_comprobante_idx ON mensajeria.boletas (comprobante_id) WHERE comprobante_id IS NOT NULL;
CREATE INDEX boletas_por_resolver_idx ON mensajeria.boletas (resolucion_en) WHERE resolucion IS NOT NULL;
CREATE INDEX boletas_conversacion_idx ON mensajeria.boletas (conversacion_id, created_at);

-- +goose Down
DROP INDEX mensajeria.boletas_conversacion_idx;
DROP INDEX mensajeria.boletas_por_resolver_idx;
DROP INDEX mensajeria.boletas_comprobante_idx;
ALTER TABLE mensajeria.boletas
    DROP COLUMN resolucion_en,
    DROP COLUMN resolucion,
    DROP COLUMN aviso_mensaje_id,
    DROP COLUMN aviso_cliente,
    DROP COLUMN verificado_por_nombre,
    DROP COLUMN causa_rechazo,
    DROP COLUMN motivo_rechazo,
    DROP COLUMN pago_id,
    DROP COLUMN decidida_por,
    DROP COLUMN decidida_en;

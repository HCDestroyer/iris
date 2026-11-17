-- +goose Up
-- Voz E1: los ajustes de llamadas del número se escriben en WhatsApp como un pendiente con
-- reintentos, con su estado visible en el panel; y el audio del buzón de voz queda marcado.
ALTER TABLE mensajeria.voz_ajustes_canal
    ADD COLUMN meta_estado     text        NOT NULL DEFAULT 'aplicado' CHECK (meta_estado IN ('aplicado', 'pendiente', 'error')),
    ADD COLUMN meta_intentos   integer     NOT NULL DEFAULT 0,
    ADD COLUMN meta_proximo_en timestamptz,
    ADD COLUMN meta_detalle    text CHECK (meta_detalle IS NULL OR char_length(meta_detalle) <= 300);
CREATE INDEX voz_ajustes_canal_meta_idx ON mensajeria.voz_ajustes_canal (meta_proximo_en) WHERE meta_estado = 'pendiente';

ALTER TABLE mensajeria.conversacion_mensajes ADD COLUMN buzon_de_voz boolean NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE mensajeria.conversacion_mensajes DROP COLUMN buzon_de_voz;
DROP INDEX mensajeria.voz_ajustes_canal_meta_idx;
ALTER TABLE mensajeria.voz_ajustes_canal DROP COLUMN meta_detalle, DROP COLUMN meta_proximo_en, DROP COLUMN meta_intentos, DROP COLUMN meta_estado;

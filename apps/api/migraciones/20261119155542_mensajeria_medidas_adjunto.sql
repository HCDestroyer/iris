-- +goose Up
-- Medidas de la imagen de un mensaje de la bandeja (contratos/conversaciones.md §7.2.1): ancho y
-- alto en píxeles tal como se ve (con la orientación EXIF ya aplicada), leídos de la cabecera al
-- guardar el adjunto (envío del panel o descarga desde WhatsApp). El panel reserva el espacio de la
-- burbuja con esa proporción antes de que cargue la foto. null = sin medidas (documento, WebP,
-- mensajes anteriores a esta migración o cabecera ilegible): el panel reserva 4:3.
ALTER TABLE mensajeria.conversacion_mensajes
    ADD COLUMN adjunto_ancho integer,
    ADD COLUMN adjunto_alto  integer,
    ADD CONSTRAINT conversacion_mensajes_medidas_ck CHECK (
        (adjunto_ancho IS NULL) = (adjunto_alto IS NULL)
        AND (adjunto_ancho IS NULL OR (adjunto_ancho BETWEEN 1 AND 20000 AND adjunto_alto BETWEEN 1 AND 20000))
    );

-- +goose Down
ALTER TABLE mensajeria.conversacion_mensajes
    DROP CONSTRAINT conversacion_mensajes_medidas_ck,
    DROP COLUMN adjunto_alto,
    DROP COLUMN adjunto_ancho;

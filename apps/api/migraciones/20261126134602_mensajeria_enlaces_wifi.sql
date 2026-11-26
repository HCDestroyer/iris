-- +goose Up
-- Asistente de IA con los equipos (contrato argosisp.md 0.9.0 §12.3 y §12.4, pedido B14). El asistente
-- nunca pide ni recibe la clave Wi-Fi: con el cliente verificado en la conversación, Iris le manda un
-- enlace de un solo uso (10 min, ligado a la conversación, al servicio y a esa verificación) a un
-- formulario de Iris donde escribe el SSID y la clave. Del enlace solo se guarda la huella (sha256);
-- la verificación vale para un solo cambio (una verificación, un enlace) y caduca si cambia el cliente
-- asociado a la conversación.
CREATE TABLE mensajeria.enlaces_wifi (
    id              uuid        PRIMARY KEY,
    huella          bytea       NOT NULL UNIQUE,
    empresa_id      bigint,
    conversacion_id bigint      NOT NULL REFERENCES mensajeria.conversaciones (id),
    cliente_id      bigint      NOT NULL,
    servicio_id     bigint      NOT NULL,
    verificacion_id bigint      NOT NULL UNIQUE REFERENCES mensajeria.asistente_verificaciones (id),
    metodo          text        NOT NULL,
    verificado_en   timestamptz NOT NULL,
    vence_en        timestamptz NOT NULL,
    usado_en        timestamptz,
    cambio_id       uuid,
    -- El token cifrado solo para ponerlo en el mensaje al mandarlo a Meta (el mensaje guardado lleva
    -- /wifi/***); se borra al usarse y no sirve después de vence_en.
    token_cifrado   bytea,
    created_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX enlaces_wifi_conv_idx ON mensajeria.enlaces_wifi (conversacion_id, created_at);

-- El cambio desde el enlace sale a Argos con X-Iris-Actor: asistente_ia (§7.1 y §12.4); pendientes_actor_check
-- ya lo admite desde la migración de B31 (#282, gestiones de cobro).

-- +goose Down
DROP TABLE mensajeria.enlaces_wifi;

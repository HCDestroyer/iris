-- +goose Up
-- Notificaciones push a la app de campo por Firebase Cloud Messaging HTTP v1
-- (contratos/push.md). Schema propio: dispositivos registrados, preferencias de la cuenta y envíos
-- (uno por evento y dispositivo: idempotencia del emisor).
CREATE SCHEMA notificaciones;

-- Un dispositivo es un token FCM de un usuario en una empresa (la activa al registrarlo; null =
-- sin conector), atado a la sesión o al token de API con que se registró: si esa credencial deja de
-- valer, el dispositivo se quita. El token FCM nunca se devuelve por la API ni va a la auditoría ni
-- al log (allí va token_fin). Quitar un dispositivo borra la fila (queda su auditoría y su evento).
CREATE TABLE notificaciones.dispositivos (
    id              bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    usuario_id      bigint      NOT NULL REFERENCES nucleo.usuarios (id),
    empresa_id      bigint,
    sesion_id       bigint REFERENCES nucleo.sesiones (id) ON DELETE CASCADE,
    token_api_id    bigint REFERENCES nucleo.tokens_api (id) ON DELETE CASCADE,
    plataforma      text        NOT NULL CHECK (plataforma IN ('android')),
    token           text        NOT NULL CHECK (char_length(token) BETWEEN 1 AND 4096),
    token_hash      bytea       NOT NULL CHECK (length(token_hash) = 32),
    token_fin       text        NOT NULL CHECK (char_length(token_fin) BETWEEN 1 AND 6),
    app_version     text        NOT NULL CHECK (char_length(app_version) BETWEEN 1 AND 40),
    ultimo_envio_en timestamptz,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    CHECK ((sesion_id IS NULL) <> (token_api_id IS NULL))
);
-- Idempotente por token: uno por token y empresa (el mismo teléfono puede estar en varias empresas).
CREATE UNIQUE INDEX dispositivos_token_empresa_uq ON notificaciones.dispositivos (token_hash, coalesce(empresa_id, 0));
CREATE INDEX dispositivos_usuario_idx ON notificaciones.dispositivos (usuario_id, empresa_id);
CREATE INDEX dispositivos_sesion_idx ON notificaciones.dispositivos (sesion_id) WHERE sesion_id IS NOT NULL;

-- Preferencias de la cuenta (valen en cualquier teléfono). Sin fila: los valores por defecto.
CREATE TABLE notificaciones.preferencias (
    usuario_id          bigint PRIMARY KEY REFERENCES nucleo.usuarios (id),
    chat                boolean     NOT NULL DEFAULT true,
    mencion             boolean     NOT NULL DEFAULT true,
    orden               boolean     NOT NULL DEFAULT true,
    bandeja             boolean     NOT NULL DEFAULT true,
    contenido           text        NOT NULL DEFAULT 'completo' CHECK (contenido IN ('completo', 'remitente')),
    no_molestar         boolean     NOT NULL DEFAULT false,
    -- Minutos desde la medianoche local (zona_horaria); desde > hasta cruza la medianoche.
    no_molestar_desde   integer     NOT NULL DEFAULT 1260 CHECK (no_molestar_desde BETWEEN 0 AND 1439),
    no_molestar_hasta   integer     NOT NULL DEFAULT 360 CHECK (no_molestar_hasta BETWEEN 0 AND 1439),
    urgente_igual       boolean     NOT NULL DEFAULT true,
    zona_horaria        text        NOT NULL DEFAULT 'America/Guatemala' CHECK (char_length(zona_horaria) BETWEEN 1 AND 60),
    version             integer     NOT NULL DEFAULT 1,
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now(),
    CHECK (no_molestar_desde <> no_molestar_hasta)
);

-- Envíos: uno por (evento, dispositivo). datos lleva solo referencias (ids y banderas), nunca el
-- texto: el mensaje se arma al enviar con los datos y los permisos de ese momento. clave agrupa los
-- eventos de una misma acción que avisarían lo mismo (asignar y reprogramar una orden a la vez).
CREATE TABLE notificaciones.envios (
    id             bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    evento_id      uuid        NOT NULL,
    evento         text        NOT NULL,
    dispositivo_id bigint      NOT NULL,
    usuario_id     bigint      NOT NULL REFERENCES nucleo.usuarios (id),
    empresa_id     bigint,
    tipo           text        NOT NULL CHECK (tipo IN ('chat', 'mencion', 'orden', 'bandeja')),
    clave          text,
    datos          jsonb       NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(datos) = 'object'),
    estado         text        NOT NULL DEFAULT 'pendiente' CHECK (estado IN ('pendiente', 'enviado', 'omitido', 'fallido')),
    motivo         text,
    intentos       integer     NOT NULL DEFAULT 0,
    fcm_mensaje    text,
    enviado_en     timestamptz,
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX envios_evento_dispositivo_uq ON notificaciones.envios (evento_id, dispositivo_id);
CREATE UNIQUE INDEX envios_clave_dispositivo_uq ON notificaciones.envios (clave, dispositivo_id) WHERE clave IS NOT NULL;
CREATE INDEX envios_creado_idx ON notificaciones.envios (created_at);

-- +goose Down
DROP SCHEMA notificaciones CASCADE;

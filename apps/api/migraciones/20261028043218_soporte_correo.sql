-- +goose Up
-- Soporte (I2): entrada de tickets por correo (IMAP) y respuesta mínima por SMTP.
--
-- Un buzón es una cuenta IMAP (y opcionalmente SMTP) de una empresa. Las contraseñas se
-- guardan cifradas con IRIS_CLAVE_CIFRADO (AES-256-GCM, atadas al buzón) y nunca salen en
-- respuestas, logs ni auditoría. Solo TLS: IMAPS/SMTPS implícito o STARTTLS obligatorio.
CREATE TABLE soporte.buzones (
    id                      bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    empresa_id              bigint,                               -- null = sin conector
    nombre                  text        NOT NULL,
    correo                  text        NOT NULL,                 -- dirección del buzón (From de las respuestas)
    imap_host               text        NOT NULL,
    imap_puerto             integer     NOT NULL CHECK (imap_puerto BETWEEN 1 AND 65535),
    imap_tls                text        NOT NULL CHECK (imap_tls IN ('tls', 'starttls')),
    imap_usuario            text        NOT NULL,
    imap_contrasena         bytea,                                -- cifrada (nonce || texto); se llena tras el INSERT
    imap_carpeta            text        NOT NULL DEFAULT 'INBOX',
    imap_carpeta_procesados text,                                 -- null = solo marcar leído
    smtp_host               text,                                 -- null = sin salida por correo
    smtp_puerto             integer     CHECK (smtp_puerto BETWEEN 1 AND 65535),
    smtp_tls                text        CHECK (smtp_tls IN ('tls', 'starttls')),
    smtp_usuario            text,                                 -- null = sin AUTH
    smtp_contrasena         bytea,                                -- cifrada
    categoria_id            bigint      REFERENCES soporte.categorias (id),
    prioridad               text        NOT NULL DEFAULT 'media' CHECK (prioridad IN ('baja', 'media', 'alta', 'critica')),
    activo                  boolean     NOT NULL DEFAULT false,
    -- Estado de la lectura (no sube version: lo escribe el worker).
    uid_validity            bigint,                               -- null = nunca leído: la primera lectura toma solo los no leídos
    ultimo_uid              bigint      NOT NULL DEFAULT 0,
    ultima_lectura_en       timestamptz,
    ultimo_error            text,
    version                 integer     NOT NULL DEFAULT 1,
    created_at              timestamptz NOT NULL DEFAULT now(),
    updated_at              timestamptz NOT NULL DEFAULT now(),
    CHECK ((smtp_host IS NULL) = (smtp_puerto IS NULL) AND (smtp_host IS NULL) = (smtp_tls IS NULL)),
    CHECK (smtp_usuario IS NOT NULL OR smtp_contrasena IS NULL)
);
CREATE UNIQUE INDEX buzones_correo_uq ON soporte.buzones (coalesce(empresa_id, 0), lower(correo));
CREATE INDEX buzones_activos_idx ON soporte.buzones (id) WHERE activo;

-- Correos leídos: se guardan primero (deduplicados por Message-ID y buzón) y luego un job los
-- convierte en ticket nuevo o en respuesta del cliente. El cuerpo ya es texto seguro (sin HTML).
CREATE TABLE soporte.correos_entrantes (
    id               bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    buzon_id         bigint      NOT NULL REFERENCES soporte.buzones (id),
    empresa_id       bigint,                                      -- la del buzón
    message_id       text        NOT NULL,
    uid_validity     bigint      NOT NULL,
    uid              bigint      NOT NULL,
    remitente        text        NOT NULL,                        -- dirección (minúsculas)
    remitente_nombre text        NOT NULL DEFAULT '',
    asunto           text        NOT NULL DEFAULT '',
    in_reply_to      text,
    referencias      text[]      NOT NULL DEFAULT '{}',
    cuerpo           text        NOT NULL DEFAULT '',
    adjuntos         jsonb       NOT NULL DEFAULT '[]'::jsonb,    -- metadatos: nombre, tipo, tamano
    automatico       text,                                        -- motivo si es autorespuesta o rebote
    truncado         boolean     NOT NULL DEFAULT false,          -- superó el tamaño máximo
    fecha            timestamptz,                                 -- cabecera Date
    estado           text        NOT NULL DEFAULT 'pendiente'
                                 CHECK (estado IN ('pendiente', 'procesado', 'ignorado', 'error')),
    resultado        text        CHECK (resultado IN ('ticket_creado', 'respuesta')),
    motivo           text,                                        -- por qué se ignoró o falló
    ticket_id        bigint      REFERENCES soporte.tickets (id),
    mensaje_id       bigint      REFERENCES soporte.ticket_mensajes (id),
    procesado_en     timestamptz,
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now(),
    UNIQUE (buzon_id, message_id)
);
CREATE INDEX correos_entrantes_pendientes_idx ON soporte.correos_entrantes (buzon_id, id) WHERE estado = 'pendiente';
CREATE INDEX correos_entrantes_ticket_idx ON soporte.correos_entrantes (ticket_id, id) WHERE ticket_id IS NOT NULL;

-- Respuestas enviadas por SMTP: una por mensaje público del agente (consumidor idempotente de
-- ticket.respondido). El Message-ID se fija al encolar: un reintento lleva el mismo.
CREATE TABLE soporte.correos_salientes (
    id           bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    buzon_id     bigint      NOT NULL REFERENCES soporte.buzones (id),
    empresa_id   bigint,
    ticket_id    bigint      NOT NULL REFERENCES soporte.tickets (id),
    mensaje_id   bigint      NOT NULL UNIQUE REFERENCES soporte.ticket_mensajes (id),
    message_id   text        NOT NULL UNIQUE,
    in_reply_to  text,
    destinatario text        NOT NULL,
    asunto       text        NOT NULL,
    estado       text        NOT NULL DEFAULT 'pendiente' CHECK (estado IN ('pendiente', 'enviado')),
    intentos     integer     NOT NULL DEFAULT 0,
    ultimo_error text,
    enviado_en   timestamptz,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX correos_salientes_ticket_idx ON soporte.correos_salientes (ticket_id);

-- +goose Down
DROP TABLE soporte.correos_salientes;
DROP TABLE soporte.correos_entrantes;
DROP TABLE soporte.buzones;

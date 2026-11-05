-- +goose Up
-- Servicio de mensajería de Iris (contrato Iris ↔ ArgosISP 0.6.0 §6, fase 1). Corre en su propio
-- proceso (cmd/mensajeria) con su propio schema `mensajeria` en la base de Iris: comparte con la API
-- la identidad (tokens iris_pat_ y ACL), la auditoría y el outbox, en la misma transacción.
CREATE SCHEMA mensajeria;

-- Canales configurados por empresa (credenciales del proveedor cifradas con IRIS_CLAVE_CIFRADO,
-- atadas a la fila: "mensajeria.canal:<id>:credenciales"). Fase 1: WhatsApp Cloud API (Meta). El
-- canal simulado (dev y CI) no tiene fila: lo habilita IRIS_MENSAJERIA_SIMULADO.
CREATE TABLE mensajeria.canales (
    id                    bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    empresa_id            bigint,
    tipo                  text        NOT NULL CHECK (tipo IN ('whatsapp_cloud')),
    canal                 text        NOT NULL CHECK (canal IN ('whatsapp')),
    nombre                text        NOT NULL CHECK (length(nombre) BETWEEN 1 AND 120),
    -- Número del remitente (E.164) y los ids de Meta.
    numero                text        NOT NULL CHECK (numero ~ '^\+[1-9][0-9]{7,14}$'),
    phone_number_id       text        NOT NULL CHECK (phone_number_id ~ '^[0-9]{1,30}$'),
    waba_id               text        NOT NULL DEFAULT '' CHECK (waba_id ~ '^[0-9]{0,30}$'),
    -- JSON cifrado { token, app_secret, verify_token }.
    credenciales_cifradas bytea       NOT NULL,
    -- Plantillas aprobadas en Meta: { "<codigo de Iris>": { "nombre": "<plantilla en Meta>", "idioma": "es" } }.
    plantillas            jsonb       NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(plantillas) = 'object'),
    -- Pausa automática del número (calidad baja o reportes de spam de Meta, §6.5).
    pausado_hasta         timestamptz,
    activo                boolean     NOT NULL DEFAULT true,
    creado_por            bigint REFERENCES nucleo.usuarios (id),
    version               integer     NOT NULL DEFAULT 1,
    created_at            timestamptz NOT NULL DEFAULT now(),
    updated_at            timestamptz NOT NULL DEFAULT now()
);
-- Un canal activo por empresa y medio.
CREATE UNIQUE INDEX canales_activo_uk ON mensajeria.canales (coalesce(empresa_id, 0), canal) WHERE activo;
CREATE UNIQUE INDEX canales_phone_number_uk ON mensajeria.canales (phone_number_id) WHERE activo;

-- Permisos de contacto (§6.4). Solo inserción: el vigente es el de ocurrido_en más reciente (a
-- igual ocurrido_en, una baja manda). Iris es la fuente; Argos no lleva un registro propio.
CREATE TABLE mensajeria.consentimientos (
    id              bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    empresa_id      bigint      NOT NULL,
    cliente_id      bigint      NOT NULL CHECK (cliente_id > 0),
    canal           text        NOT NULL CHECK (canal IN ('whatsapp', 'telegram', 'correo')),
    estado          text        NOT NULL CHECK (estado IN ('acepta', 'baja')),
    origen          text        NOT NULL CHECK (origen IN ('argos_panel', 'argos_portal', 'cliente', 'agente')),
    ocurrido_en     timestamptz NOT NULL,
    registrado_por  bigint REFERENCES nucleo.usuarios (id),
    idempotency_key text CHECK (idempotency_key IS NULL OR length(idempotency_key) BETWEEN 1 AND 255),
    hash_peticion   bytea,
    -- Mensaje entrante (BAJA/ALTO) que originó una baja del cliente.
    entrante_id     bigint,
    created_at      timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX consentimientos_clave_uk ON mensajeria.consentimientos (registrado_por, idempotency_key)
    WHERE idempotency_key IS NOT NULL;
CREATE UNIQUE INDEX consentimientos_hecho_uk ON mensajeria.consentimientos (empresa_id, cliente_id, canal, ocurrido_en, estado, origen);
CREATE INDEX consentimientos_vigente_idx ON mensajeria.consentimientos (empresa_id, cliente_id, canal, ocurrido_en DESC);

-- Avisos (§6.2). El aviso_id es un uuid v7. destino es el teléfono E.164 que Iris tomó de la
-- instantánea de Argos (§2.2): nunca lo manda quien pide el aviso, nunca sale en eventos.
CREATE TABLE mensajeria.avisos (
    id              uuid PRIMARY KEY,
    empresa_id      bigint      NOT NULL,
    cliente_id      bigint      NOT NULL CHECK (cliente_id > 0),
    servicio_id     bigint CHECK (servicio_id IS NULL OR servicio_id > 0),
    plantilla       text        NOT NULL,
    variables       jsonb       NOT NULL DEFAULT '{}',
    canal_preferido text        NOT NULL CHECK (canal_preferido IN ('whatsapp', 'telegram', 'correo')),
    prioridad       text        NOT NULL CHECK (prioridad IN ('normal', 'urgente')),
    origen          text        NOT NULL CHECK (origen IN ('argos', 'iris')),
    estado          text        NOT NULL CHECK (estado IN ('en_cola', 'bloqueado', 'entregado', 'fallido')),
    motivo          text,
    detalle         text CHECK (detalle IS NULL OR length(detalle) <= 500),
    canal           text CHECK (canal IS NULL OR canal IN ('whatsapp', 'telegram', 'correo')),
    -- Canal configurado por el que sale (null con el simulado o si se bloqueó antes de elegirlo).
    canal_id        bigint REFERENCES mensajeria.canales (id),
    -- Clave de envío por número: «canal:<id>» o «simulado:<empresa>» (límite por minuto, §6.5).
    remitente       text,
    destino         text,
    proveedor_id    text,
    usuario_id      bigint REFERENCES nucleo.usuarios (id),
    token_id        bigint,
    idempotency_key text CHECK (idempotency_key IS NULL OR length(idempotency_key) BETWEEN 1 AND 255),
    hash_peticion   bytea,
    -- Hasta cuándo tiene sentido (variable de caducidad de la plantilla, fin del día en Guatemala).
    vence_en        timestamptz,
    programado_en   timestamptz,
    enviado_en      timestamptz,
    entregado_en    timestamptz,
    leido_en        timestamptz,
    finalizado_en   timestamptz,
    intentos        integer     NOT NULL DEFAULT 0,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT avisos_motivo CHECK ((estado IN ('bloqueado', 'fallido')) = (motivo IS NOT NULL))
);
CREATE UNIQUE INDEX avisos_clave_uk ON mensajeria.avisos (usuario_id, idempotency_key) WHERE idempotency_key IS NOT NULL;
CREATE UNIQUE INDEX avisos_proveedor_uk ON mensajeria.avisos (canal_id, proveedor_id) WHERE proveedor_id IS NOT NULL;
CREATE INDEX avisos_cliente_dia_idx ON mensajeria.avisos (empresa_id, cliente_id, created_at);
CREATE INDEX avisos_remitente_idx ON mensajeria.avisos (remitente, enviado_en) WHERE enviado_en IS NOT NULL;
CREATE INDEX avisos_en_cola_idx ON mensajeria.avisos (created_at) WHERE estado = 'en_cola';
CREATE INDEX avisos_destino_idx ON mensajeria.avisos (canal_id, destino) WHERE destino IS NOT NULL;

-- Webhooks entrantes del proveedor (estados de entrega y mensajes de Meta): se guardan primero
-- (deduplicados por el SHA-256 del cuerpo firmado) y se procesan en un job (CLAUDE.md §3).
CREATE TABLE mensajeria.entrantes (
    id           bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    canal_id     bigint      NOT NULL REFERENCES mensajeria.canales (id),
    hash_cuerpo  bytea       NOT NULL,
    payload      jsonb       NOT NULL,
    procesado_en timestamptz,
    error        text,
    intentos     integer     NOT NULL DEFAULT 0,
    created_at   timestamptz NOT NULL DEFAULT now(),
    UNIQUE (canal_id, hash_cuerpo)
);
CREATE INDEX entrantes_pendientes_idx ON mensajeria.entrantes (created_at) WHERE procesado_en IS NULL;

-- Webhooks salientes hacia Argos (§6.6). Los escribe el suscriptor del outbox (relay del worker) con
-- el id del evento del outbox (= X-Webhook-Id = id del cuerpo), y los entrega el proceso de
-- mensajería con reintentos a 1 min, 5 min, 30 min, 2 h y 12 h.
CREATE TABLE mensajeria.webhooks_salida (
    id              uuid PRIMARY KEY,
    evento          text        NOT NULL CHECK (evento IN ('aviso.entregado', 'aviso.fallido', 'contacto.baja')),
    empresa_id      bigint,
    payload         jsonb       NOT NULL,
    ocurrido_en     timestamptz NOT NULL,
    estado          text        NOT NULL DEFAULT 'pendiente' CHECK (estado IN ('pendiente', 'entregado', 'agotado', 'omitido')),
    intentos        integer     NOT NULL DEFAULT 0,
    proximo_intento timestamptz NOT NULL DEFAULT now(),
    ultimo_error    text,
    conector_id     bigint,
    entregado_en    timestamptz,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX webhooks_salida_pendientes_idx ON mensajeria.webhooks_salida (proximo_intento) WHERE estado = 'pendiente';

-- +goose Down
DROP SCHEMA mensajeria CASCADE;

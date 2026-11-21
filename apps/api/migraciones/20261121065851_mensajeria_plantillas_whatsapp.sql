-- +goose Up
-- Plantillas de WhatsApp administrables (contratos/plantillas-whatsapp.md). Viven por cuenta de
-- WhatsApp (WABA), no por número: la empresa dueña de la cuenta decide en qué usos sirve cada una
-- (avisos, conversaciones, asistente) y qué código del catálogo de Iris atiende (el contrato con
-- Argos §6.3 sigue pidiendo avisos por código). Lo que Meta borra NO se borra aquí: queda en
-- estado DELETED con su historial. La columna mensajeria.canales.plantillas se conserva (los
-- números sin WABA siguen usándola) y su contenido se copia aquí sin perder nada.

CREATE TABLE mensajeria.plantillas_whatsapp (
    id                     bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    -- Empresa dueña de la cuenta (null sin conector de Argos).
    empresa_id             bigint,
    waba_id                text        NOT NULL CHECK (waba_id ~ '^[0-9]{1,30}$'),
    -- Id de la plantilla en Meta (hsm_id); null mientras es borrador.
    meta_id                text        CHECK (meta_id IS NULL OR meta_id ~ '^[0-9]{1,30}$'),
    nombre                 text        NOT NULL CHECK (nombre ~ '^[a-z0-9_]+$' AND char_length(nombre) <= 512),
    idioma                 text        NOT NULL CHECK (idioma ~ '^[a-z]{2,3}([_-][A-Za-z]{2,4})?$'),
    -- Nombre para personas («Recordatorio de pago»).
    etiqueta               text        NOT NULL CHECK (char_length(etiqueta) BETWEEN 1 AND 80),
    categoria              text        NOT NULL CHECK (categoria IN ('UTILITY', 'MARKETING', 'AUTHENTICATION')),
    -- La que tenía antes de que Meta la reclasificara (aviso en el panel hasta que alguien la revise).
    categoria_anterior     text        CHECK (categoria_anterior IS NULL OR categoria_anterior IN ('UTILITY', 'MARKETING', 'AUTHENTICATION')),
    categoria_cambiada_en  timestamptz,
    -- Meta avisó que la va a reclasificar (template_category_update con correct_category).
    categoria_proxima      text        CHECK (categoria_proxima IS NULL OR categoria_proxima IN ('UTILITY', 'MARKETING', 'AUTHENTICATION')),
    categoria_proxima_en   timestamptz,
    -- BORRADOR (solo en Iris) o el estado de Meta.
    estado                 text        NOT NULL CHECK (estado IN ('BORRADOR', 'PENDING', 'APPROVED', 'REJECTED', 'PAUSED', 'DISABLED', 'FLAGGED',
                                                                  'IN_APPEAL', 'LIMIT_EXCEEDED', 'LOCKED', 'ARCHIVED', 'PENDING_DELETION', 'DELETED')),
    estado_en              timestamptz NOT NULL DEFAULT now(),
    -- reason y rejection_info de Meta (sin copiar texto libre largo).
    motivo                 text        CHECK (motivo IS NULL OR char_length(motivo) <= 60),
    rechazo                jsonb,
    calidad                text        NOT NULL DEFAULT 'UNKNOWN' CHECK (calidad IN ('GREEN', 'YELLOW', 'RED', 'UNKNOWN')),
    pausada_hasta          timestamptz,
    formato                text        NOT NULL DEFAULT 'NAMED' CHECK (formato IN ('NAMED', 'POSITIONAL')),
    -- Definición en la forma de Iris (encabezado, cuerpo, pie, botones, variables con ejemplo).
    componentes            jsonb       NOT NULL,
    -- Lo que devolvió Meta la última vez (tal cual).
    componentes_meta       jsonb,
    -- Código del catálogo de Iris que atiende (factura_por_vencer…) y qué dato de Iris va en cada variable.
    codigo                 text        CHECK (codigo IS NULL OR codigo ~ '^[a-z][a-z0-9_]{0,63}$'),
    parametros             jsonb       NOT NULL DEFAULT '[]',
    usos                   text[]      NOT NULL DEFAULT '{}' CHECK (usos <@ ARRAY['avisos', 'conversaciones', 'asistente']::text[]),
    origen                 text        NOT NULL CHECK (origen IN ('iris', 'sugerida', 'meta', 'migrada', 'canal')),
    sugerida               text,
    -- Ediciones de una plantilla aprobada (Meta: 1 cada 24 h y 10 en 30 días).
    ediciones              timestamptz[] NOT NULL DEFAULT '{}',
    enviada_en             timestamptz,
    -- Meta la borró (o ya no la devuelve) o se borró desde Iris.
    borrada_en             timestamptz,
    borrada_por            text        CHECK (borrada_por IS NULL OR borrada_por IN ('iris', 'meta')),
    -- Meta no deja reusar el nombre de una aprobada borrada por 30 días.
    nombre_libre_en        timestamptz,
    sincronizada_en        timestamptz,
    creada_por             bigint      REFERENCES nucleo.usuarios (id),
    version                integer     NOT NULL DEFAULT 1,
    created_at             timestamptz NOT NULL DEFAULT now(),
    updated_at             timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT plantillas_whatsapp_borrada_ck CHECK ((estado = 'DELETED') = (borrada_en IS NOT NULL))
);
CREATE UNIQUE INDEX plantillas_whatsapp_nombre_uq ON mensajeria.plantillas_whatsapp (waba_id, nombre, idioma) WHERE estado <> 'DELETED';
CREATE UNIQUE INDEX plantillas_whatsapp_codigo_uq ON mensajeria.plantillas_whatsapp (waba_id, codigo) WHERE codigo IS NOT NULL AND estado <> 'DELETED';
CREATE UNIQUE INDEX plantillas_whatsapp_meta_uq ON mensajeria.plantillas_whatsapp (meta_id) WHERE meta_id IS NOT NULL AND estado <> 'DELETED';
CREATE INDEX plantillas_whatsapp_empresa_idx ON mensajeria.plantillas_whatsapp (empresa_id, waba_id, id);

-- Historial por plantilla (lo que muestra la ficha). Solo inserción.
CREATE TABLE mensajeria.plantillas_whatsapp_historial (
    id            bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    plantilla_id  bigint      NOT NULL REFERENCES mensajeria.plantillas_whatsapp (id),
    tipo          text        NOT NULL CHECK (tipo IN ('creada', 'editada', 'enviada', 'estado', 'categoria', 'calidad', 'usos', 'borrada', 'reanudada')),
    de            text,
    a             text,
    detalle       jsonb       NOT NULL DEFAULT '{}',
    origen        text        NOT NULL CHECK (origen IN ('usuario', 'webhook', 'sincronizacion', 'sistema')),
    actor_id      bigint      REFERENCES nucleo.usuarios (id),
    ocurrido_en   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX plantillas_whatsapp_historial_idx ON mensajeria.plantillas_whatsapp_historial (plantilla_id, id DESC);

-- Estado de la sincronización de cada cuenta.
CREATE TABLE mensajeria.plantillas_whatsapp_cuentas (
    waba_id          text PRIMARY KEY CHECK (waba_id ~ '^[0-9]{1,30}$'),
    empresa_id       bigint,
    sincronizada_en  timestamptz,
    -- Resumen de la última sincronización (nuevas, cambiadas, borradas).
    resumen          jsonb,
    -- Último error al hablar con Meta (código propio: meta_sin_permiso, meta_no_responde…).
    error            text        CHECK (error IS NULL OR char_length(error) <= 60),
    error_en         timestamptz,
    updated_at       timestamptz NOT NULL DEFAULT now()
);

-- Ajustes por empresa (0 = sin conector). Marketing apagado de fábrica.
CREATE TABLE mensajeria.plantillas_whatsapp_ajustes (
    empresa_id           bigint PRIMARY KEY,
    marketing_permitido  boolean     NOT NULL DEFAULT false,
    version              integer     NOT NULL DEFAULT 1,
    updated_at           timestamptz NOT NULL DEFAULT now()
);

-- Migración de mensajeria.canales.plantillas (código → {nombre, idioma}) de los números con WABA. El
-- primero de cada cuenta fija el código; los demás quedan igual como plantillas de la cuenta. Las
-- variables del catálogo van por posición en el orden declarado (como se enviaban hasta hoy). La
-- columna de los canales NO se toca.
INSERT INTO mensajeria.plantillas_whatsapp (empresa_id, waba_id, nombre, idioma, etiqueta, categoria, estado, formato, componentes, usos, origen)
SELECT DISTINCT ON (c.waba_id, p.value ->> 'nombre', p.value ->> 'idioma')
       c.empresa_id, c.waba_id, p.value ->> 'nombre', p.value ->> 'idioma', left(p.key, 80), 'UTILITY', 'APPROVED', 'POSITIONAL',
       '{"cuerpo": "", "botones": [], "variables": []}'::jsonb, ARRAY['avisos', 'conversaciones'], 'migrada'
FROM mensajeria.canales c
CROSS JOIN LATERAL jsonb_each(CASE WHEN jsonb_typeof(c.plantillas) = 'object' THEN c.plantillas ELSE '{}'::jsonb END) p
WHERE c.waba_id <> '' AND c.tipo = 'whatsapp_cloud'
  AND (p.value ->> 'nombre') ~ '^[a-z0-9_]+$' AND char_length(p.value ->> 'nombre') <= 512
  AND (p.value ->> 'idioma') ~ '^[a-z]{2,3}([_-][A-Za-z]{2,4})?$'
ORDER BY c.waba_id, p.value ->> 'nombre', p.value ->> 'idioma', c.activo DESC, c.id;

-- El código: el del número de avisos de la empresa primero, luego el más antiguo.
WITH candidatos AS (
    SELECT DISTINCT ON (c.waba_id, p.key) c.waba_id, p.key AS codigo, p.value ->> 'nombre' AS nombre, p.value ->> 'idioma' AS idioma
    FROM mensajeria.canales c
    CROSS JOIN LATERAL jsonb_each(CASE WHEN jsonb_typeof(c.plantillas) = 'object' THEN c.plantillas ELSE '{}'::jsonb END) p
    WHERE c.waba_id <> '' AND c.tipo = 'whatsapp_cloud' AND p.key ~ '^[a-z][a-z0-9_]{0,63}$'
    ORDER BY c.waba_id, p.key, c.avisos_empresa DESC, c.activo DESC, c.id
)
UPDATE mensajeria.plantillas_whatsapp t SET codigo = x.codigo
FROM (
    -- Una plantilla atiende un solo código: si el mismo nombre servía a dos, el primero (alfabético).
    SELECT DISTINCT ON (waba_id, nombre, idioma) waba_id, codigo, nombre, idioma FROM candidatos ORDER BY waba_id, nombre, idioma, codigo
) x
WHERE t.waba_id = x.waba_id AND t.nombre = x.nombre AND t.idioma = x.idioma AND t.origen = 'migrada';

INSERT INTO mensajeria.plantillas_whatsapp_historial (plantilla_id, tipo, a, detalle, origen)
SELECT id, 'creada', estado, jsonb_build_object('origen', 'migrada', 'codigo', codigo), 'sistema'
FROM mensajeria.plantillas_whatsapp WHERE origen = 'migrada';

-- +goose Down
DROP TABLE mensajeria.plantillas_whatsapp_ajustes;
DROP TABLE mensajeria.plantillas_whatsapp_cuentas;
DROP TABLE mensajeria.plantillas_whatsapp_historial;
DROP TABLE mensajeria.plantillas_whatsapp;

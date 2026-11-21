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
    -- Códigos del catálogo de Iris que atiende (factura_por_vencer…; uno por cuenta, lo cuida Iris con
    -- el candado de la cuenta) y qué dato de Iris va en cada variable.
    codigos                text[]      NOT NULL DEFAULT '{}'
                                       CHECK (array_to_string(codigos, ',') ~ '^([a-z][a-z0-9_]{0,63}(,[a-z][a-z0-9_]{0,63})*)?$'),
    parametros             jsonb       NOT NULL DEFAULT '[]',
    usos                   text[]      NOT NULL DEFAULT '{}' CHECK (usos <@ ARRAY['avisos', 'conversaciones', 'asistente']::text[]),
    origen                 text        NOT NULL CHECK (origen IN ('iris', 'sugerida', 'meta', 'migrada', 'canal')),
    sugerida               text,
    -- Ediciones de una plantilla aprobada (Meta: 1 cada 24 h y 10 en 30 días).
    ediciones              timestamptz[] NOT NULL DEFAULT '{}',
    enviada_en             timestamptz,
    -- Meta la borró (o ya no la devuelve), se borró desde Iris o el sistema la archivó (la cuenta
    -- cambió de empresa dueña).
    borrada_en             timestamptz,
    borrada_por            text        CHECK (borrada_por IS NULL OR borrada_por IN ('iris', 'meta', 'sistema')),
    -- Meta no deja reusar el nombre de una aprobada borrada por 30 días.
    nombre_libre_en        timestamptz,
    sincronizada_en        timestamptz,
    -- El último entry[].time de Meta aplicado (segundos Unix, reloj de Meta): un aviso más viejo se
    -- descarta y se relee la cuenta. Nunca se compara con estado_en (reloj de Iris).
    meta_evento_en         bigint      NOT NULL DEFAULT 0,
    creada_por             bigint      REFERENCES nucleo.usuarios (id),
    version                integer     NOT NULL DEFAULT 1,
    created_at             timestamptz NOT NULL DEFAULT now(),
    updated_at             timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT plantillas_whatsapp_borrada_ck CHECK ((estado = 'DELETED') = (borrada_en IS NOT NULL))
);
CREATE UNIQUE INDEX plantillas_whatsapp_nombre_uq ON mensajeria.plantillas_whatsapp (waba_id, nombre, idioma) WHERE estado <> 'DELETED';
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

-- Dueña de cada cuenta (una WABA es de una sola empresa: la de sus números; un préstamo de número no
-- la cambia) y estado de su sincronización. Toda consulta de plantillas filtra por esta empresa.
CREATE TABLE mensajeria.plantillas_whatsapp_cuentas (
    waba_id          text PRIMARY KEY CHECK (waba_id ~ '^[0-9]{1,30}$'),
    -- 0 = sin conector (números sin empresa).
    empresa_id       bigint      NOT NULL,
    sincronizada_en  timestamptz,
    -- Resumen de la última sincronización (nuevas, cambiadas, borradas).
    resumen          jsonb,
    -- Último error al hablar con Meta (código propio: meta_sin_permiso, meta_no_responde…).
    error            text        CHECK (error IS NULL OR char_length(error) <= 60),
    error_en         timestamptz,
    updated_at       timestamptz NOT NULL DEFAULT now()
);

-- Cambios de plantillas avisados por Meta ya aplicados: el mismo aviso puede llegar por varios números
-- de la cuenta (o repetido) y se aplica una sola vez.
CREATE TABLE mensajeria.plantillas_whatsapp_avisos_meta (
    waba_id      text        NOT NULL,
    meta_id      text        NOT NULL,
    campo        text        NOT NULL,
    evento       text        NOT NULL,
    ocurrido     bigint      NOT NULL,
    recibido_en  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (waba_id, meta_id, campo, evento, ocurrido)
);
CREATE INDEX plantillas_whatsapp_avisos_meta_recibido_idx ON mensajeria.plantillas_whatsapp_avisos_meta (recibido_en);

-- Ajustes por empresa (0 = sin conector). Marketing apagado de fábrica.
CREATE TABLE mensajeria.plantillas_whatsapp_ajustes (
    empresa_id           bigint PRIMARY KEY,
    marketing_permitido  boolean     NOT NULL DEFAULT false,
    version              integer     NOT NULL DEFAULT 1,
    updated_at           timestamptz NOT NULL DEFAULT now()
);

-- Migración de mensajeria.canales.plantillas (código → {nombre, idioma}) de los números activos con
-- WABA. La dueña de cada cuenta es la empresa de su número de avisos (o el más antiguo); los números de
-- otra empresa con la misma WABA no se migran (quedan en el informe). Una fila por nombre e idioma, con
-- todos los códigos que atendía; cada código a una sola plantilla (la del número de avisos primero).
-- Las variables del catálogo van por posición (como se enviaban hasta hoy). La columna de los canales
-- NO se toca. Lo que no cumple las reglas se descarta y se informa (nunca aborta el despliegue).
INSERT INTO mensajeria.plantillas_whatsapp_cuentas (waba_id, empresa_id)
SELECT DISTINCT ON (c.waba_id) c.waba_id, coalesce(c.empresa_id, 0)
FROM mensajeria.canales c
WHERE c.waba_id ~ '^[0-9]{1,30}$' AND c.desconectado_en IS NULL
ORDER BY c.waba_id, c.activo DESC, c.avisos_empresa DESC, c.id
ON CONFLICT (waba_id) DO NOTHING;

-- +goose StatementBegin
DO $$
DECLARE
    r record;
BEGIN
    CREATE TEMPORARY TABLE plantillas_migrables AS
    SELECT c.id AS canal_id, c.empresa_id, c.waba_id, c.avisos_empresa, p.key AS codigo, p.value ->> 'nombre' AS nombre, p.value ->> 'idioma' AS idioma,
           (p.key ~ '^[a-z][a-z0-9_]{0,63}$'
            AND coalesce(p.value ->> 'nombre', '') ~ '^[a-z0-9_]+$' AND char_length(p.value ->> 'nombre') <= 512
            AND coalesce(p.value ->> 'idioma', '') ~ '^[a-z]{2,3}([_-][A-Za-z]{2,4})?$') AS valida,
           coalesce(c.empresa_id, 0) = k.empresa_id AS de_la_duena
    FROM mensajeria.canales c
    JOIN mensajeria.plantillas_whatsapp_cuentas k ON k.waba_id = c.waba_id
    CROSS JOIN LATERAL jsonb_each(CASE WHEN jsonb_typeof(c.plantillas) = 'object' THEN c.plantillas ELSE '{}'::jsonb END) AS p(key, value)
    WHERE c.tipo = 'whatsapp_cloud' AND c.activo AND c.desconectado_en IS NULL;

    INSERT INTO mensajeria.plantillas_whatsapp (empresa_id, waba_id, nombre, idioma, etiqueta, categoria, estado, formato, componentes, usos, origen)
    SELECT DISTINCT ON (m.waba_id, m.nombre, m.idioma)
           nullif(k.empresa_id, 0), m.waba_id, m.nombre, m.idioma, left(m.nombre, 80), 'UTILITY', 'APPROVED', 'POSITIONAL',
           '{"cuerpo": "", "botones": [], "variables": []}'::jsonb, ARRAY['avisos', 'conversaciones'], 'migrada'
    FROM plantillas_migrables m
    JOIN mensajeria.plantillas_whatsapp_cuentas k ON k.waba_id = m.waba_id
    WHERE m.valida AND m.de_la_duena
    ORDER BY m.waba_id, m.nombre, m.idioma, m.avisos_empresa DESC, m.canal_id
    ON CONFLICT DO NOTHING;

    -- Cada código, a la plantilla del número de avisos (o del más antiguo); una plantilla puede atender varios.
    UPDATE mensajeria.plantillas_whatsapp t SET codigos = x.codigos
    FROM (
        SELECT waba_id, nombre, idioma, array_agg(codigo ORDER BY codigo) AS codigos
        FROM (
            SELECT DISTINCT ON (waba_id, codigo) waba_id, codigo, nombre, idioma
            FROM plantillas_migrables
            WHERE valida AND de_la_duena
            ORDER BY waba_id, codigo, avisos_empresa DESC, canal_id
        ) c
        GROUP BY waba_id, nombre, idioma
    ) x
    WHERE t.waba_id = x.waba_id AND t.nombre = x.nombre AND t.idioma = x.idioma AND t.origen = 'migrada' AND cardinality(t.codigos) = 0
      AND NOT EXISTS (SELECT 1 FROM mensajeria.plantillas_whatsapp o WHERE o.waba_id = x.waba_id AND o.codigos && x.codigos AND o.estado <> 'DELETED' AND o.id <> t.id);

    INSERT INTO mensajeria.plantillas_whatsapp_historial (plantilla_id, tipo, a, detalle, origen)
    SELECT p.id, 'creada', p.estado, jsonb_build_object('origen', 'migrada', 'codigos', p.codigos), 'sistema'
    FROM mensajeria.plantillas_whatsapp p
    WHERE p.origen = 'migrada' AND NOT EXISTS (SELECT 1 FROM mensajeria.plantillas_whatsapp_historial h WHERE h.plantilla_id = p.id);

    -- Informe de lo que no se migró (sale en el registro de `iris migrar`).
    FOR r IN
        SELECT canal_id, waba_id, codigo, CASE WHEN NOT valida THEN 'nombre, idioma o código no válidos' ELSE 'el número es de otra empresa que la dueña de la cuenta (sigue usando su propio mapa)' END AS motivo
        FROM plantillas_migrables WHERE NOT valida OR NOT de_la_duena
        ORDER BY canal_id, codigo
    LOOP
        -- %L cita el texto (un código guardado a mano puede traer cualquier cosa) y se recorta.
        RAISE NOTICE '%', format('plantillas de WhatsApp: no se migró el código %L del número %s (cuenta %L): %s',
            left(r.codigo, 64), r.canal_id, left(r.waba_id, 30), r.motivo);
    END LOOP;
    DROP TABLE plantillas_migrables;
END
$$;
-- +goose StatementEnd

-- +goose Down
-- Borra las plantillas por cuenta, su historial, la dueña y el estado de cada cuenta y los ajustes de
-- marketing (docs/runbooks/plantillas-whatsapp.md). mensajeria.canales.plantillas no se tocó: los
-- números siguen saliendo con su mapa propio.
DROP TABLE mensajeria.plantillas_whatsapp_avisos_meta;
DROP TABLE mensajeria.plantillas_whatsapp_ajustes;
DROP TABLE mensajeria.plantillas_whatsapp_cuentas;
DROP TABLE mensajeria.plantillas_whatsapp_historial;
DROP TABLE mensajeria.plantillas_whatsapp;

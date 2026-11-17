-- +goose Up
-- Servidor de IA («Iris Voz», contratos/voz.md §4): un servidor dedicado con GPU, de la instalación
-- (no de una empresa), que Iris usa como proveedor del asistente de chat por un canal autenticado
-- (mTLS con huellas fijadas y firma por petición). Solo Iris abre conexiones hacia él.

-- 1. El servidor. Uno por instalación en esta entrega (la tabla ya admite más). Los secretos van
-- cifrados con IRIS_CLAVE_CIFRADO (dato asociado "mensajeria.voz_motor:<id>:<campo>") y nunca se
-- devuelven: el token de la firma, la clave privada del certificado de cliente y, solo hasta
-- enlazar o 15 minutos, el código de enlace.
CREATE TABLE mensajeria.voz_motores (
    id                      bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    nombre                  text        NOT NULL CHECK (char_length(nombre) BETWEEN 1 AND 80),
    url                     text        NOT NULL CHECK (char_length(url) BETWEEN 1 AND 300),
    estado                  text        NOT NULL DEFAULT 'sin_enlazar' CHECK (estado IN ('sin_enlazar', 'listo', 'degradado', 'no_disponible', 'incompatible', 'desactivado')),
    -- enlazado: el intercambio con código se completó (falta activar si activado_en es null).
    enlazado_en             timestamptz,
    activado_en             timestamptz,
    enlace_codigo           bytea,
    enlace_vence_en         timestamptz,
    enlace_usado_en         timestamptz,
    token                   bytea,
    -- token_siguiente: el que se está entregando al servidor; si la respuesta se pierde, Iris
    -- prueba con él antes de dar el canal por roto.
    token_siguiente         bytea,
    token_rotado_en         timestamptz,
    cert_cliente            text,
    cert_cliente_clave      bytea,
    cert_cliente_vence_en   timestamptz,
    huella_tls              text CHECK (huella_tls IS NULL OR huella_tls ~ '^sha256:[0-9a-f]{64}$'),
    huella_tls_siguiente    text CHECK (huella_tls_siguiente IS NULL OR huella_tls_siguiente ~ '^sha256:[0-9a-f]{64}$'),
    -- ip_observada: la IP de Iris que vio el servidor al enlazar (informativa; la restricción por
    -- IP se enciende en el servidor, nunca desde Iris).
    ip_observada            text,
    restringe_ip            boolean     NOT NULL DEFAULT false,
    -- empresas_permitidas: null = todas.
    empresas_permitidas     bigint[],
    simultaneas_por_empresa integer     NOT NULL DEFAULT 2 CHECK (simultaneas_por_empresa BETWEEN 1 AND 64),
    -- modelos_permitidos: de los que declara el servidor (catalogo), los que las empresas pueden
    -- elegir; null = todos.
    modelos_permitidos      text[],
    -- Umbrales de los avisos y retención de las series del tablero (voz.md §4.12).
    vram_pct                integer     NOT NULL DEFAULT 95 CHECK (vram_pct BETWEEN 50 AND 100),
    vram_minutos            integer     NOT NULL DEFAULT 5 CHECK (vram_minutos BETWEEN 1 AND 60),
    disco_pct               integer     NOT NULL DEFAULT 90 CHECK (disco_pct BETWEEN 50 AND 99),
    temperatura_c           integer     NOT NULL DEFAULT 85 CHECK (temperatura_c BETWEEN 60 AND 100),
    temperatura_minutos     integer     NOT NULL DEFAULT 5 CHECK (temperatura_minutos BETWEEN 1 AND 60),
    saturacion_rechazos     integer     NOT NULL DEFAULT 10 CHECK (saturacion_rechazos BETWEEN 1 AND 1000),
    saturacion_minutos      integer     NOT NULL DEFAULT 5 CHECK (saturacion_minutos BETWEEN 1 AND 60),
    retencion_detalle_dias  integer     NOT NULL DEFAULT 7 CHECK (retencion_detalle_dias BETWEEN 1 AND 30),
    retencion_hora_dias     integer     NOT NULL DEFAULT 90 CHECK (retencion_hora_dias BETWEEN 7 AND 365),
    umbrales_version        integer     NOT NULL DEFAULT 1,
    max_simultaneas         integer     NOT NULL DEFAULT 0 CHECK (max_simultaneas >= 0),
    version_motor           text,
    version_protocolo       text,
    iris_min                text,
    iris_max                text,
    compatible              boolean     NOT NULL DEFAULT false,
    motivo_incompatible     text,
    -- catalogo: modelos permitidos que declara el servidor y su GPU (ya validados por Iris).
    catalogo                jsonb       NOT NULL DEFAULT '{}',
    detalle                 text,
    fallos_seguidos         integer     NOT NULL DEFAULT 0,
    ultimo_latido_en        timestamptz,
    ultima_prueba           jsonb,
    version                 integer     NOT NULL DEFAULT 1,
    created_at              timestamptz NOT NULL DEFAULT now(),
    updated_at              timestamptz NOT NULL DEFAULT now(),
    CHECK ((enlace_codigo IS NULL) = (enlace_vence_en IS NULL))
);
CREATE UNIQUE INDEX voz_motores_uno_uk ON mensajeria.voz_motores ((true));

-- 2. Plazas en curso: una fila por petición al modelo que está en vuelo. Se reserva en la misma
-- sentencia en que se decide llamar (tope del servidor, tope por empresa y plaza garantizada por
-- empresa activa) y se borra al terminar; una que quedó huérfana vence sola.
CREATE TABLE mensajeria.servidor_ia_plazas (
    id         uuid PRIMARY KEY,
    motor_id   bigint      NOT NULL REFERENCES mensajeria.voz_motores (id) ON DELETE CASCADE,
    empresa_id bigint      NOT NULL,
    tomada_en  timestamptz NOT NULL DEFAULT now(),
    vence_en   timestamptz NOT NULL
);
CREATE INDEX servidor_ia_plazas_motor_idx ON mensajeria.servidor_ia_plazas (motor_id, vence_en);

-- 3. Uso por hora, empresa y modelo, medido por Iris con su propio reloj (nunca con lo que diga el
-- servidor). Sin texto de prompts ni de respuestas. latencia_cubetas: histograma de cubetas fijas
-- (para la media y el p95); errores: cuenta por motivo.
CREATE TABLE mensajeria.servidor_ia_uso (
    motor_id         bigint      NOT NULL REFERENCES mensajeria.voz_motores (id) ON DELETE CASCADE,
    hora             timestamptz NOT NULL,
    empresa_id       bigint      NOT NULL,
    modelo           text        NOT NULL CHECK (char_length(modelo) <= 200),
    peticiones       bigint      NOT NULL DEFAULT 0,
    tokens_entrada   bigint      NOT NULL DEFAULT 0,
    tokens_salida    bigint      NOT NULL DEFAULT 0,
    latencia_ms_suma bigint      NOT NULL DEFAULT 0,
    latencia_cubetas bigint[]    NOT NULL DEFAULT '{}',
    errores          jsonb       NOT NULL DEFAULT '{}',
    rechazos         bigint      NOT NULL DEFAULT 0,
    PRIMARY KEY (motor_id, hora, empresa_id, modelo)
);
CREATE INDEX servidor_ia_uso_empresa_idx ON mensajeria.servidor_ia_uso (empresa_id, hora);

-- 4. El asistente de cada empresa puede elegir el servidor de IA como proveedor (sin URL ni clave).
ALTER TABLE mensajeria.asistente_ajustes DROP CONSTRAINT asistente_ajustes_proveedor_check;
ALTER TABLE mensajeria.asistente_ajustes ADD CONSTRAINT asistente_ajustes_proveedor_check
    CHECK (proveedor IN ('ollama', 'openai_compatible', 'anthropic', 'servidor_ia'));

-- +goose Down
UPDATE mensajeria.asistente_ajustes SET proveedor = 'ollama', activo = false WHERE proveedor = 'servidor_ia';
ALTER TABLE mensajeria.asistente_ajustes DROP CONSTRAINT asistente_ajustes_proveedor_check;
ALTER TABLE mensajeria.asistente_ajustes ADD CONSTRAINT asistente_ajustes_proveedor_check
    CHECK (proveedor IN ('ollama', 'openai_compatible', 'anthropic'));
DROP TABLE mensajeria.servidor_ia_uso;
DROP TABLE mensajeria.servidor_ia_plazas;
DROP TABLE mensajeria.voz_motores;

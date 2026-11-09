-- +goose Up
-- Asistente de IA que sugiere respuestas (contratos/conversaciones.md §16). Nunca envía mensajes:
-- guarda la sugerencia para que una persona la use.

-- 1. Ajustes por empresa (empresa_id null = sin conector). La clave del proveedor va cifrada con
-- IRIS_CLAVE_CIFRADO (dato asociado "mensajeria.asistente:<id>:clave") y nunca se devuelve.
CREATE TABLE mensajeria.asistente_ajustes (
    id                    bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    empresa_id            bigint,
    activo                boolean     NOT NULL DEFAULT false,
    proveedor             text        NOT NULL DEFAULT 'ollama' CHECK (proveedor IN ('ollama', 'openai_compatible', 'anthropic')),
    url                   text        NOT NULL DEFAULT '' CHECK (char_length(url) <= 500),
    clave_cifrada         bytea,
    clave_guardada_en     timestamptz,
    modelo                text        NOT NULL DEFAULT '' CHECK (char_length(modelo) <= 200),
    modo                  text        NOT NULL DEFAULT 'automatico' CHECK (modo IN ('automatico', 'boton')),
    fuente_manuales       boolean     NOT NULL DEFAULT true,
    fuente_argos          boolean     NOT NULL DEFAULT true,
    fuente_horus          boolean     NOT NULL DEFAULT true,
    fuente_historial      boolean     NOT NULL DEFAULT true,
    tope_tokens_mes       bigint      NOT NULL DEFAULT 2000000 CHECK (tope_tokens_mes BETWEEN 1000 AND 1000000000),
    precio_entrada_millon numeric(14, 4) CHECK (precio_entrada_millon IS NULL OR precio_entrada_millon >= 0),
    precio_salida_millon  numeric(14, 4) CHECK (precio_salida_millon IS NULL OR precio_salida_millon >= 0),
    version               integer     NOT NULL DEFAULT 1,
    created_at            timestamptz NOT NULL DEFAULT now(),
    updated_at            timestamptz NOT NULL DEFAULT now(),
    CHECK ((clave_cifrada IS NULL) = (clave_guardada_en IS NULL))
);
CREATE UNIQUE INDEX asistente_ajustes_empresa_uk ON mensajeria.asistente_ajustes (coalesce(empresa_id, 0));

-- 2. Uso por empresa y mes (mes calendario en America/Guatemala, primer día). avisado_80/100: el
-- evento asistente.tope_alcanzado ya salió en ese mes con el tope vigente.
CREATE TABLE mensajeria.asistente_uso (
    empresa_id     bigint,
    mes            date        NOT NULL,
    tokens_entrada bigint      NOT NULL DEFAULT 0,
    tokens_salida  bigint      NOT NULL DEFAULT 0,
    sugerencias    integer     NOT NULL DEFAULT 0,
    resumenes      integer     NOT NULL DEFAULT 0,
    -- Tokens reservados por generaciones en curso (estimados; se ajustan al terminar).
    tokens_reservados bigint   NOT NULL DEFAULT 0 CHECK (tokens_reservados >= 0),
    avisado_80     boolean     NOT NULL DEFAULT false,
    avisado_100    boolean     NOT NULL DEFAULT false,
    updated_at     timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX asistente_uso_uk ON mensajeria.asistente_uso (coalesce(empresa_id, 0), mes);

-- Reservas de tokens en curso (sugerencia o resumen): el mantenimiento libera las que quedaron
-- huérfanas.
CREATE TABLE mensajeria.asistente_reservas (
    id         uuid PRIMARY KEY,
    empresa_id bigint,
    mes        date        NOT NULL,
    tokens     bigint      NOT NULL CHECK (tokens >= 0),
    creada_en  timestamptz NOT NULL
);
CREATE INDEX asistente_reservas_creada_idx ON mensajeria.asistente_reservas (creada_en);

-- Pedidos por persona (botón, «Otra» y resumen) para el límite por minuto.
CREATE TABLE mensajeria.asistente_pedidos (
    id              bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    usuario_id      bigint      NOT NULL REFERENCES nucleo.usuarios (id),
    empresa_id      bigint,
    conversacion_id bigint      NOT NULL,
    tipo            text        NOT NULL CHECK (tipo IN ('sugerencia', 'resumen')),
    creado_en       timestamptz NOT NULL
);
CREATE INDEX asistente_pedidos_usuario_idx ON mensajeria.asistente_pedidos (usuario_id, creado_en);

-- 3. Sugerencias. El texto vive solo aquí (nunca en eventos ni auditoría). Una vigente
-- (generando, lista o error) por conversación.
CREATE TABLE mensajeria.sugerencias (
    id              uuid PRIMARY KEY,
    empresa_id      bigint,
    conversacion_id bigint      NOT NULL REFERENCES mensajeria.conversaciones (id),
    para_mensaje_id uuid,
    estado          text        NOT NULL CHECK (estado IN ('generando', 'lista', 'error', 'descartada', 'usada', 'vencida')),
    origen          text        NOT NULL CHECK (origen IN ('automatico', 'boton', 'otra')),
    texto           text CHECK (texto IS NULL OR char_length(texto) <= 4096),
    fuentes         jsonb       NOT NULL DEFAULT '[]',
    accion          jsonb,
    error_codigo    text,
    error_detalle   text,
    calificacion    text CHECK (calificacion IS NULL OR calificacion IN ('util', 'no_util')),
    proveedor       text,
    modelo          text,
    tokens_entrada  bigint      NOT NULL DEFAULT 0,
    tokens_salida   bigint      NOT NULL DEFAULT 0,
    pedida_por      bigint REFERENCES nucleo.usuarios (id),
    usada_por       bigint REFERENCES nucleo.usuarios (id),
    usada_en        timestamptz,
    enviada_editada boolean,
    mensaje_enviado_id uuid,
    generada_en     timestamptz,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX sugerencias_vigente_uk ON mensajeria.sugerencias (conversacion_id) WHERE estado IN ('generando', 'lista', 'error');
CREATE INDEX sugerencias_conversacion_idx ON mensajeria.sugerencias (conversacion_id, created_at DESC);
CREATE INDEX sugerencias_generando_idx ON mensajeria.sugerencias (created_at) WHERE estado = 'generando';
CREATE INDEX sugerencias_empresa_mes_idx ON mensajeria.sugerencias (empresa_id, created_at);

-- 4. Feed de la bandeja: el evento «sugerencia» lleva su id (solo lo reciben quienes tienen
-- conversaciones.sugerencias).
ALTER TABLE mensajeria.conv_eventos ADD COLUMN sugerencia_id uuid;
ALTER TABLE mensajeria.conv_eventos DROP CONSTRAINT conv_eventos_tipo_check;
ALTER TABLE mensajeria.conv_eventos ADD CONSTRAINT conv_eventos_tipo_check
    CHECK (tipo IN ('mensaje_nuevo', 'mensaje_actualizado', 'conversacion_actualizada', 'mencion', 'guardia', 'aviso_sin_atender', 'sugerencia'));

-- +goose Down
DELETE FROM mensajeria.conv_eventos WHERE tipo = 'sugerencia';
ALTER TABLE mensajeria.conv_eventos DROP CONSTRAINT conv_eventos_tipo_check;
ALTER TABLE mensajeria.conv_eventos ADD CONSTRAINT conv_eventos_tipo_check
    CHECK (tipo IN ('mensaje_nuevo', 'mensaje_actualizado', 'conversacion_actualizada', 'mencion', 'guardia', 'aviso_sin_atender'));
ALTER TABLE mensajeria.conv_eventos DROP COLUMN sugerencia_id;
DROP TABLE mensajeria.sugerencias;
DROP TABLE mensajeria.asistente_pedidos;
DROP TABLE mensajeria.asistente_reservas;
DROP TABLE mensajeria.asistente_uso;
DROP TABLE mensajeria.asistente_ajustes;

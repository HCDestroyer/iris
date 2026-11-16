-- +goose Up
-- Asistente de IA, fase 3a (contratos/conversaciones.md §17): modo autónomo y perfiles (se guardan;
-- actuar solo llega en 3b), categorías de mensajes por empresa y la clasificación de cada conversación.

-- 1. Ajustes: cuándo atiende solo, límites, perfiles (acciones y límites con nombre) y acciones
-- apagadas por cola. activado_en: la primera vez que la empresa activó el asistente (métrica «antes»).
ALTER TABLE mensajeria.asistente_ajustes
    ADD COLUMN modo_autonomo text NOT NULL DEFAULT 'nunca' CHECK (modo_autonomo IN ('nunca', 'sin_nadie', 'siempre_primero')),
    ADD COLUMN espera_min integer NOT NULL DEFAULT 3 CHECK (espera_min BETWEEN 1 AND 60),
    ADD COLUMN max_turnos integer NOT NULL DEFAULT 8 CHECK (max_turnos BETWEEN 2 AND 20),
    ADD COLUMN perfiles jsonb NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN por_cola jsonb NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN activado_en timestamptz;
UPDATE mensajeria.asistente_ajustes SET activado_en = created_at WHERE activo;

-- 2. Categorías de mensajes (por empresa; «Otro» no se desactiva).
CREATE TABLE mensajeria.asistente_categorias (
    id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    empresa_id  bigint,
    nombre      text        NOT NULL CHECK (char_length(nombre) BETWEEN 1 AND 40),
    descripcion text        NOT NULL DEFAULT '' CHECK (char_length(descripcion) <= 300),
    tono        text        NOT NULL DEFAULT 'neutro' CHECK (tono IN ('info', 'error', 'ok', 'aviso', 'neutro')),
    cola_id     bigint REFERENCES mensajeria.colas (id),
    sla_primera_respuesta_min integer CHECK (sla_primera_respuesta_min IS NULL OR sla_primera_respuesta_min BETWEEN 1 AND 10080),
    modo        text        NOT NULL DEFAULT 'sugiere' CHECK (modo IN ('responde_solo', 'sugiere')),
    condicion   text CHECK (condicion IS NULL OR condicion IN ('si_hay_falla', 'solo_verificados')),
    manual_ids  bigint[]    NOT NULL DEFAULT '{}',
    es_otro     boolean     NOT NULL DEFAULT false,
    activa      boolean     NOT NULL DEFAULT true,
    orden       integer     NOT NULL DEFAULT 0,
    version     integer     NOT NULL DEFAULT 1,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    CHECK (NOT es_otro OR activa)
);
CREATE UNIQUE INDEX asistente_categorias_nombre_uk ON mensajeria.asistente_categorias (coalesce(empresa_id, 0), lower(nombre));
CREATE UNIQUE INDEX asistente_categorias_otro_uk ON mensajeria.asistente_categorias (coalesce(empresa_id, 0)) WHERE es_otro;

-- 3. La conversación guarda su categoría y lo último que hizo el asistente (una línea armada por el
-- servidor; nunca texto del cliente).
ALTER TABLE mensajeria.conversaciones
    ADD COLUMN categoria_id bigint REFERENCES mensajeria.asistente_categorias (id),
    ADD COLUMN categoria_confianza real CHECK (categoria_confianza IS NULL OR categoria_confianza BETWEEN 0 AND 1),
    ADD COLUMN categoria_origen text CHECK (categoria_origen IS NULL OR categoria_origen IN ('asistente', 'agente')),
    ADD COLUMN categoria_en timestamptz,
    ADD COLUMN clasificado_mensaje_id uuid,
    ADD COLUMN asistente_actividad_tipo text,
    ADD COLUMN asistente_actividad_texto text CHECK (asistente_actividad_texto IS NULL OR char_length(asistente_actividad_texto) <= 120),
    ADD COLUMN asistente_actividad_en timestamptz;
CREATE INDEX conversaciones_categoria_idx ON mensajeria.conversaciones (categoria_id) WHERE categoria_id IS NOT NULL;

-- 4. Clasificaciones hechas con una llamada aparte (sin sugerencia): para el límite por hora.
CREATE TABLE mensajeria.asistente_clasificaciones (
    id              uuid PRIMARY KEY,
    empresa_id      bigint,
    conversacion_id bigint      NOT NULL REFERENCES mensajeria.conversaciones (id) ON DELETE CASCADE,
    creada_en       timestamptz NOT NULL
);
CREATE INDEX asistente_clasificaciones_conv_idx ON mensajeria.asistente_clasificaciones (conversacion_id, creada_en);

-- 5. «Pruébalo antes de guardar» cuenta para el límite de pedidos por persona (sin conversación: 0).
ALTER TABLE mensajeria.asistente_pedidos DROP CONSTRAINT asistente_pedidos_tipo_check;
ALTER TABLE mensajeria.asistente_pedidos ADD CONSTRAINT asistente_pedidos_tipo_check CHECK (tipo IN ('sugerencia', 'resumen', 'prueba'));

-- +goose Down
DELETE FROM mensajeria.asistente_pedidos WHERE tipo = 'prueba';
ALTER TABLE mensajeria.asistente_pedidos DROP CONSTRAINT asistente_pedidos_tipo_check;
ALTER TABLE mensajeria.asistente_pedidos ADD CONSTRAINT asistente_pedidos_tipo_check CHECK (tipo IN ('sugerencia', 'resumen'));
DROP TABLE mensajeria.asistente_clasificaciones;
DROP INDEX mensajeria.conversaciones_categoria_idx;
ALTER TABLE mensajeria.conversaciones
    DROP COLUMN categoria_id, DROP COLUMN categoria_confianza, DROP COLUMN categoria_origen, DROP COLUMN categoria_en, DROP COLUMN clasificado_mensaje_id,
    DROP COLUMN asistente_actividad_tipo, DROP COLUMN asistente_actividad_texto, DROP COLUMN asistente_actividad_en;
DROP TABLE mensajeria.asistente_categorias;
ALTER TABLE mensajeria.asistente_ajustes
    DROP COLUMN modo_autonomo, DROP COLUMN espera_min, DROP COLUMN max_turnos, DROP COLUMN perfiles, DROP COLUMN por_cola, DROP COLUMN activado_en;

-- +goose Up
-- Bandeja 1.1 (contratos/conversaciones.md §14): supervisores por cola, conversaciones sin cola
-- visibles solo mientras nadie las tiene, disponibilidad de los agentes, «¿Alguien la atiende?» y
-- el bot «Cuando no hay agentes» con menú.

-- Supervisores por cola. tipo deja lugar al asistente de IA (fase 3): hoy solo personas.
CREATE TABLE mensajeria.cola_supervisores (
    id           bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    cola_id      bigint      NOT NULL REFERENCES mensajeria.colas (id) ON DELETE CASCADE,
    tipo         text        NOT NULL DEFAULT 'persona' CHECK (tipo IN ('persona', 'asistente')),
    usuario_id   bigint REFERENCES nucleo.usuarios (id),
    -- Fase 3: el asistente que supervisa (sin tabla todavía).
    asistente_id bigint,
    created_at   timestamptz NOT NULL DEFAULT now(),
    CHECK ((tipo = 'persona') = (usuario_id IS NOT NULL))
);
CREATE UNIQUE INDEX cola_supervisores_uk ON mensajeria.cola_supervisores (cola_id, usuario_id) WHERE usuario_id IS NOT NULL;
CREATE INDEX cola_supervisores_usuario_idx ON mensajeria.cola_supervisores (usuario_id) WHERE usuario_id IS NOT NULL;

-- Minutos del bot por cola (null = los del número).
ALTER TABLE mensajeria.colas
    ADD COLUMN bot_sin_confirmacion_min integer CHECK (bot_sin_confirmacion_min IS NULL OR bot_sin_confirmacion_min BETWEEN 1 AND 1440),
    ADD COLUMN bot_sin_disponibles_min integer CHECK (bot_sin_disponibles_min IS NULL OR bot_sin_disponibles_min BETWEEN 1 AND 1440);

-- Bot «Cuando no hay agentes» por número:
-- { activo, atiende, minutosSinConfirmacion, minutosSinDisponibles, saludo, menu: [{ texto, colaId, etiquetaIds }] }.
ALTER TABLE mensajeria.canales ADD COLUMN bot jsonb NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(bot) = 'object');
-- Los números que ya existían: bot apagado (nada cambia en horario) y el mensaje de fuera de
-- horario como saludo.
UPDATE mensajeria.canales SET bot = jsonb_build_object(
    'activo', false, 'atiende', 'bot', 'minutosSinConfirmacion', 30, 'minutosSinDisponibles', 3,
    'saludo', left(coalesce(nullif(btrim(fuera_horario->>'mensaje'), ''),
        'Gracias por escribir. En este momento todos nuestros agentes están ocupados; te respondemos en cuanto se libere uno.'), 1024),
    'menu', '[]'::jsonb);

-- Espera de una conversación sin asignar: cuándo empezó el aviso o el bot (una vez por espera),
-- el temporizador pendiente, cuándo contestó el bot y el menú que espera respuesta.
ALTER TABLE mensajeria.conversaciones
    ADD COLUMN bot_espera_en  timestamptz,
    ADD COLUMN bot_vence_en   timestamptz,
    ADD COLUMN bot_motivo     text CHECK (bot_motivo IS NULL OR bot_motivo IN ('sin_confirmacion', 'sin_disponibles')),
    ADD COLUMN bot_atendio_en timestamptz,
    ADD COLUMN bot_menu       text CHECK (bot_menu IS NULL OR bot_menu IN ('esperando', 'repetido')),
    ADD CONSTRAINT conversaciones_bot_timer CHECK ((bot_vence_en IS NULL) = (bot_motivo IS NULL));
CREATE INDEX conversaciones_bot_idx ON mensajeria.conversaciones (bot_vence_en) WHERE bot_vence_en IS NOT NULL;

-- Mensajes automáticos: el bot o la respuesta de fuera de horario (salientes sin autor).
ALTER TABLE mensajeria.conversacion_mensajes
    ADD COLUMN automatico text CHECK (automatico IS NULL OR automatico IN ('bot', 'fuera_de_horario'));
UPDATE mensajeria.conversacion_mensajes SET automatico = 'fuera_de_horario' WHERE direccion = 'saliente' AND autor_id IS NULL;

-- Disponibilidad de los agentes por empresa. Los latidos solo actualizan ultima_actividad (sin
-- auditoría: alta frecuencia); los cambios manuales se auditan.
CREATE TABLE mensajeria.disponibilidad (
    id               bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    usuario_id       bigint      NOT NULL REFERENCES nucleo.usuarios (id),
    empresa_id       bigint,
    estado           text        NOT NULL CHECK (estado IN ('disponible', 'no_disponible')),
    origen           text        NOT NULL CHECK (origen IN ('manual', 'cierre_sesion', 'inactividad', 'horario')),
    desde            timestamptz NOT NULL,
    ultima_actividad timestamptz NOT NULL,
    -- La sesión del panel con que se puso disponible o del último latido.
    sesion_id        bigint,
    -- Cierre del horario de trabajo (null = sin cierre conocido).
    horario_hasta    timestamptz,
    version          integer     NOT NULL DEFAULT 1,
    updated_at       timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX disponibilidad_uk ON mensajeria.disponibilidad (usuario_id, coalesce(empresa_id, 0));
CREATE INDEX disponibilidad_disponibles_idx ON mensajeria.disponibilidad (coalesce(empresa_id, 0)) WHERE estado = 'disponible';

-- Ajustes de la bandeja por empresa.
CREATE TABLE mensajeria.ajustes_bandeja (
    id                  bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    empresa_id          bigint,
    minutos_inactividad integer     NOT NULL DEFAULT 15 CHECK (minutos_inactividad BETWEEN 1 AND 480),
    version             integer     NOT NULL DEFAULT 1,
    updated_at          timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX ajustes_bandeja_uk ON mensajeria.ajustes_bandeja (coalesce(empresa_id, 0));

ALTER TABLE mensajeria.conv_eventos DROP CONSTRAINT conv_eventos_tipo_check;
ALTER TABLE mensajeria.conv_eventos ADD CONSTRAINT conv_eventos_tipo_check
    CHECK (tipo IN ('mensaje_nuevo', 'mensaje_actualizado', 'conversacion_actualizada', 'mencion', 'guardia', 'aviso_sin_atender'));

-- Quién ve una conversación: supervisión (todas), asignada a mí, de mis colas (miembro o
-- supervisor de la cola), sin cola mientras nadie la tiene, o participante (mencionado o guardia).
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION mensajeria.conv_visible(p_usuario bigint, p_supervisa boolean, p_conv bigint, p_cola bigint, p_asignado bigint)
RETURNS boolean LANGUAGE sql STABLE AS $$
    SELECT p_supervisa
        OR p_asignado = p_usuario
        OR (p_cola IS NULL AND p_asignado IS NULL)
        OR EXISTS (SELECT 1 FROM mensajeria.cola_miembros m WHERE m.cola_id = p_cola AND m.usuario_id = p_usuario)
        OR EXISTS (SELECT 1 FROM mensajeria.cola_supervisores s WHERE s.cola_id = p_cola AND s.usuario_id = p_usuario)
        OR EXISTS (SELECT 1 FROM mensajeria.conversacion_participantes p WHERE p.conversacion_id = p_conv AND p.usuario_id = p_usuario)
$$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION mensajeria.conv_visible(p_usuario bigint, p_supervisa boolean, p_conv bigint, p_cola bigint, p_asignado bigint)
RETURNS boolean LANGUAGE sql STABLE AS $$
    SELECT p_supervisa
        OR p_asignado = p_usuario
        OR p_cola IS NULL
        OR EXISTS (SELECT 1 FROM mensajeria.cola_miembros m WHERE m.cola_id = p_cola AND m.usuario_id = p_usuario)
        OR EXISTS (SELECT 1 FROM mensajeria.conversacion_participantes p WHERE p.conversacion_id = p_conv AND p.usuario_id = p_usuario)
$$;
-- +goose StatementEnd
DELETE FROM mensajeria.conv_eventos WHERE tipo = 'aviso_sin_atender';
ALTER TABLE mensajeria.conv_eventos DROP CONSTRAINT conv_eventos_tipo_check;
ALTER TABLE mensajeria.conv_eventos ADD CONSTRAINT conv_eventos_tipo_check
    CHECK (tipo IN ('mensaje_nuevo', 'mensaje_actualizado', 'conversacion_actualizada', 'mencion', 'guardia'));
DROP TABLE mensajeria.ajustes_bandeja;
DROP TABLE mensajeria.disponibilidad;
ALTER TABLE mensajeria.conversacion_mensajes DROP COLUMN automatico;
DROP INDEX mensajeria.conversaciones_bot_idx;
ALTER TABLE mensajeria.conversaciones DROP CONSTRAINT conversaciones_bot_timer, DROP COLUMN bot_menu, DROP COLUMN bot_atendio_en,
    DROP COLUMN bot_motivo, DROP COLUMN bot_vence_en, DROP COLUMN bot_espera_en;
ALTER TABLE mensajeria.canales DROP COLUMN bot;
ALTER TABLE mensajeria.colas DROP COLUMN bot_sin_disponibles_min, DROP COLUMN bot_sin_confirmacion_min;
DROP TABLE mensajeria.cola_supervisores;

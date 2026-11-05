-- +goose Up
-- Chat interno del equipo (fase 1): conversaciones directas, de cuadrilla y de orden, mensajes con
-- id del cliente (uuid v7, cola sin conexión de la app), lecturas y el feed de eventos para SSE y
-- sondeo. Contrato: .claude/integration/contratos/chat.md.
--
-- Los miembros de una conversación de cuadrilla u orden NO se copian: se derivan al leer de
-- agenda.tecnicos (cuadrilla), campo.ordenes (técnico asignado) y del alcance de chat.supervisar
-- (ver mensajeria.chat_visibles). chat_miembros guarda solo los miembros explícitos: los dos de una
-- directa y quien se une al hilo de una orden (despacho).

CREATE TABLE mensajeria.chat_conversaciones (
    id                bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    empresa_id        bigint,
    tipo              text        NOT NULL CHECK (tipo IN ('directa', 'cuadrilla', 'orden')),
    cuadrilla_id      bigint REFERENCES agenda.cuadrillas (id),
    orden_id          bigint REFERENCES campo.ordenes (id),
    -- Directa: «<usuario menor>:<usuario mayor>» (una por par y empresa).
    clave_directa     text,
    creada_por        bigint REFERENCES nucleo.usuarios (id),
    -- Último mensaje (vista previa) y secuencia de mensajes de la conversación (orden del servidor).
    ultimo_seq        bigint      NOT NULL DEFAULT 0,
    ultimo_mensaje_id uuid,
    ultima_actividad  timestamptz NOT NULL,
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now(),
    CHECK ((tipo = 'cuadrilla') = (cuadrilla_id IS NOT NULL)),
    CHECK ((tipo = 'orden') = (orden_id IS NOT NULL)),
    CHECK ((tipo = 'directa') = (clave_directa IS NOT NULL))
);
CREATE UNIQUE INDEX chat_conversaciones_cuadrilla_uk ON mensajeria.chat_conversaciones (empresa_id, cuadrilla_id) NULLS NOT DISTINCT
    WHERE tipo = 'cuadrilla';
CREATE UNIQUE INDEX chat_conversaciones_orden_uk ON mensajeria.chat_conversaciones (orden_id) WHERE tipo = 'orden';
CREATE UNIQUE INDEX chat_conversaciones_directa_uk ON mensajeria.chat_conversaciones (empresa_id, clave_directa) NULLS NOT DISTINCT
    WHERE tipo = 'directa';
CREATE INDEX chat_conversaciones_actividad_idx ON mensajeria.chat_conversaciones (empresa_id, ultima_actividad DESC, id DESC);

-- Miembros explícitos (directa: los dos; orden: quien se unió desde despacho).
CREATE TABLE mensajeria.chat_miembros (
    conversacion_id bigint      NOT NULL REFERENCES mensajeria.chat_conversaciones (id) ON DELETE CASCADE,
    usuario_id      bigint      NOT NULL REFERENCES nucleo.usuarios (id),
    origen          text        NOT NULL CHECK (origen IN ('directa', 'unido')),
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (conversacion_id, usuario_id)
);
CREATE INDEX chat_miembros_usuario_idx ON mensajeria.chat_miembros (usuario_id);

-- Mensajes. id lo genera el cliente (uuid v7): reenviar el mismo id no duplica. Nunca se editan;
-- eliminar (su autor, 15 min) borra el contenido y deja la lápida (eliminado_en).
CREATE TABLE mensajeria.chat_mensajes (
    id              uuid PRIMARY KEY,
    conversacion_id bigint           NOT NULL REFERENCES mensajeria.chat_conversaciones (id),
    empresa_id      bigint,
    seq             bigint           NOT NULL CHECK (seq > 0),
    autor_id        bigint           NOT NULL REFERENCES nucleo.usuarios (id),
    -- Texto plano (nunca HTML), hasta 4000 caracteres.
    texto           text CHECK (texto IS NULL OR char_length(texto) BETWEEN 1 AND 4000),
    adjunto_id      uuid REFERENCES nucleo.adjuntos (id),
    lat             double precision CHECK (lat IS NULL OR lat BETWEEN -90 AND 90),
    lon             double precision CHECK (lon IS NULL OR lon BETWEEN -180 AND 180),
    -- Hora en el teléfono al escribirlo (la cola sin conexión lo manda después); enviado_en es la
    -- del servidor (reloj del módulo) al recibirlo.
    creado_en       timestamptz,
    enviado_en      timestamptz      NOT NULL,
    -- SHA-256 del contenido (conversación, texto, adjunto, ubicación): un reenvío con el mismo id y
    -- otro contenido es 409.
    hash_contenido  bytea            NOT NULL,
    eliminado_en    timestamptz,
    created_at      timestamptz      NOT NULL DEFAULT now(),
    updated_at      timestamptz      NOT NULL DEFAULT now(),
    CHECK ((lat IS NULL) = (lon IS NULL)),
    CHECK (eliminado_en IS NOT NULL OR texto IS NOT NULL OR adjunto_id IS NOT NULL OR lat IS NOT NULL),
    CHECK (eliminado_en IS NULL OR (texto IS NULL AND adjunto_id IS NULL AND lat IS NULL))
);
CREATE UNIQUE INDEX chat_mensajes_seq_uk ON mensajeria.chat_mensajes (conversacion_id, seq);
CREATE UNIQUE INDEX chat_mensajes_adjunto_uk ON mensajeria.chat_mensajes (adjunto_id) WHERE adjunto_id IS NOT NULL;
-- Límite de envío por usuario (30 por minuto).
CREATE INDEX chat_mensajes_autor_idx ON mensajeria.chat_mensajes (autor_id, enviado_en DESC);

-- Hasta dónde leyó cada usuario (miembro explícito o derivado). Solo avanza.
CREATE TABLE mensajeria.chat_lecturas (
    conversacion_id bigint      NOT NULL REFERENCES mensajeria.chat_conversaciones (id) ON DELETE CASCADE,
    usuario_id      bigint      NOT NULL REFERENCES nucleo.usuarios (id),
    hasta_seq       bigint      NOT NULL CHECK (hasta_seq > 0),
    hasta_mensaje_id uuid       NOT NULL,
    leido_en        timestamptz NOT NULL,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (conversacion_id, usuario_id)
);
CREATE INDEX chat_lecturas_usuario_idx ON mensajeria.chat_lecturas (usuario_id);

-- Feed de cambios para SSE (Last-Event-ID) y sondeo (GET /chat/cambios). id es el cursor: se
-- asigna con el candado de la empresa tomado hasta el COMMIT (mensajeria.chat_eventos_candado), así
-- dentro de una empresa los ids quedan en orden de COMMIT y un cursor nunca salta un evento que
-- confirma tarde. Cada inserción hace NOTIFY iris_chat '<empresa_id o 0>'. Se purga a los 7 días.
CREATE TABLE mensajeria.chat_eventos (
    id              bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    empresa_id      bigint,
    conversacion_id bigint      NOT NULL REFERENCES mensajeria.chat_conversaciones (id),
    tipo            text        NOT NULL CHECK (tipo IN ('mensaje_nuevo', 'mensaje_eliminado', 'leido', 'conversacion_actualizada')),
    mensaje_id      uuid,
    usuario_id      bigint,
    ocurrido_en     timestamptz NOT NULL,
    created_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX chat_eventos_empresa_idx ON mensajeria.chat_eventos (empresa_id, id);
CREATE INDEX chat_eventos_ocurrido_idx ON mensajeria.chat_eventos (ocurrido_en);

-- Hasta qué id se purgó el feed: un cursor anterior es 410 cursor_vencido (el cliente recarga).
CREATE TABLE mensajeria.chat_estado (
    id                     smallint PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    eventos_purgados_hasta bigint   NOT NULL DEFAULT 0
);
INSERT INTO mensajeria.chat_estado DEFAULT VALUES;

-- Conversaciones que ve un usuario en la empresa (p_todas: cualquier empresa, solo el sistema):
--   - miembro explícito (directa, o se unió al hilo de la orden);
--   - cuadrilla: es técnico activo de ella, o chat.supervisar la cubre (global, la cuadrilla, o una
--     zona que la cuadrilla atiende; una cuadrilla sin zonas no la cubre el alcance por zona);
--   - orden: es el técnico asignado, o chat.supervisar cubre la orden (global, su zona o cuadrilla).
-- Las directas nunca se ven por alcance: solo sus dos miembros. p_solo limita a una conversación.
-- Lee agenda y campo (I2) sin copiar nada: ver .claude/integration/i1.md («Chat»).
-- +goose StatementBegin
CREATE FUNCTION mensajeria.chat_visibles(p_usuario bigint, p_empresa bigint, p_todas boolean, p_sup_global boolean,
    p_sup_zonas bigint[], p_sup_cuadrillas bigint[], p_solo bigint)
RETURNS SETOF bigint LANGUAGE sql STABLE AS $$
    SELECT c.id FROM mensajeria.chat_conversaciones c
    WHERE (p_todas OR c.empresa_id IS NOT DISTINCT FROM p_empresa)
      AND (p_solo IS NULL OR c.id = p_solo)
      AND (
        EXISTS (SELECT 1 FROM mensajeria.chat_miembros m WHERE m.conversacion_id = c.id AND m.usuario_id = p_usuario)
        OR (c.tipo = 'cuadrilla' AND (
            p_sup_global
            OR c.cuadrilla_id = ANY (p_sup_cuadrillas)
            OR EXISTS (SELECT 1 FROM agenda.tecnicos t WHERE t.usuario_id = p_usuario AND t.activo AND t.cuadrilla_id = c.cuadrilla_id)
            OR EXISTS (SELECT 1 FROM agenda.cuadrilla_zonas cz WHERE cz.cuadrilla_id = c.cuadrilla_id AND cz.zona_id = ANY (p_sup_zonas))))
        OR (c.tipo = 'orden' AND EXISTS (
            SELECT 1 FROM campo.ordenes o WHERE o.id = c.orden_id AND (
                o.tecnico_id = p_usuario OR p_sup_global
                OR o.zona_id = ANY (p_sup_zonas) OR o.cuadrilla_id = ANY (p_sup_cuadrillas))))
      )
$$;
-- +goose StatementEnd

-- +goose Down
DROP FUNCTION mensajeria.chat_visibles(bigint, bigint, boolean, boolean, bigint[], bigint[], bigint);
DROP TABLE mensajeria.chat_estado;
DROP TABLE mensajeria.chat_eventos;
DROP TABLE mensajeria.chat_lecturas;
DROP TABLE mensajeria.chat_mensajes;
DROP TABLE mensajeria.chat_miembros;
DROP TABLE mensajeria.chat_conversaciones;

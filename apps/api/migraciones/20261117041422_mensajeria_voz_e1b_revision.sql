-- +goose Up
-- Voz E1b, revisión de seguridad.

-- Una sola saliente viva por persona, también ante peticiones en paralelo.
CREATE UNIQUE INDEX llamadas_saliente_viva_uq ON mensajeria.llamadas (usuario_id) WHERE direccion = 'saliente' AND estado IN ('marcando', 'en_curso');

-- El mantenimiento corta una saliente al cruzar el tope de minutos del mes.
ALTER TABLE mensajeria.llamadas DROP CONSTRAINT llamadas_motivo_fin_check;
ALTER TABLE mensajeria.llamadas ADD CONSTRAINT llamadas_motivo_fin_check CHECK (motivo_fin IN ('cliente_colgo', 'agente_colgo', 'asistente_termino',
    'nadie_contesto', 'rechazada_agente', 'fuera_de_horario', 'sin_capacidad', 'motor_no_disponible', 'duracion_maxima', 'silencio', 'mal_uso', 'tope',
    'limite', 'error_medio', 'error_meta', 'sin_permiso', 'transferencia_fallida', 'tope_alcanzado'));

-- El call_id de un webhook que llegó antes que la respuesta del connect: queda apartado hasta saber
-- si es el de esta llamada (la respuesta del connect es la que manda).
ALTER TABLE mensajeria.llamadas ADD COLUMN externo_webhook text;

-- Llamadas que existen en Meta y no en Iris (una saliente cerrada mientras el connect estaba en
-- vuelo, o un call_id saliente sin llamada viva pero con rastro de ser de Iris): el job de
-- escrituras las termina. Las que no tienen rastro de ser de Iris quedan anotadas como «ajena»
-- (el número puede hacer salientes desde otro sistema) y no se tocan. Nunca el número del cliente.
CREATE TABLE mensajeria.llamadas_huerfanas (
    id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    canal_id   bigint      NOT NULL REFERENCES mensajeria.canales (id),
    call_id    text        NOT NULL CHECK (char_length(call_id) BETWEEN 1 AND 200),
    estado     text        NOT NULL DEFAULT 'pendiente' CHECK (estado IN ('pendiente', 'hecha', 'agotada', 'ajena')),
    intentos   integer     NOT NULL DEFAULT 0,
    proximo_en timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (canal_id, call_id)
);
CREATE INDEX llamadas_huerfanas_pendientes_idx ON mensajeria.llamadas_huerfanas (proximo_en) WHERE estado = 'pendiente';

-- Timbrar y colgar: salientes colgadas mientras marcaban, por contacto y día.
ALTER TABLE mensajeria.voz_ajustes ADD COLUMN max_colgadas_marcando_dia integer NOT NULL DEFAULT 3 CHECK (max_colgadas_marcando_dia BETWEEN 0 AND 20);

-- +goose Down
ALTER TABLE mensajeria.voz_ajustes DROP COLUMN max_colgadas_marcando_dia;
DROP TABLE mensajeria.llamadas_huerfanas;
ALTER TABLE mensajeria.llamadas DROP COLUMN externo_webhook;
UPDATE mensajeria.llamadas SET motivo_fin = 'tope' WHERE motivo_fin = 'tope_alcanzado';
ALTER TABLE mensajeria.llamadas DROP CONSTRAINT llamadas_motivo_fin_check;
ALTER TABLE mensajeria.llamadas ADD CONSTRAINT llamadas_motivo_fin_check CHECK (motivo_fin IN ('cliente_colgo', 'agente_colgo', 'asistente_termino',
    'nadie_contesto', 'rechazada_agente', 'fuera_de_horario', 'sin_capacidad', 'motor_no_disponible', 'duracion_maxima', 'silencio', 'mal_uso', 'tope',
    'limite', 'error_medio', 'error_meta', 'sin_permiso', 'transferencia_fallida'));
DROP INDEX mensajeria.llamadas_saliente_viva_uq;

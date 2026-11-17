-- +goose Up
-- Voz E1b (contratos/voz.md §2.5 y §3.7): llamadas salientes del negocio por WhatsApp y el permiso
-- del contacto para que lo llamemos.
CREATE TABLE mensajeria.llamada_permisos (
    id             bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    empresa_id     bigint,
    canal_id       bigint      NOT NULL REFERENCES mensajeria.canales (id),
    numero         text        NOT NULL,
    estado         text        NOT NULL DEFAULT 'sin_pedir' CHECK (estado IN ('sin_pedir', 'pedido', 'temporal', 'permanente', 'negado', 'revocado')),
    vence_en       timestamptz,
    -- Marcas de tiempo de las últimas solicitudes (1 por 24 h y 2 por 7 días).
    solicitudes    jsonb       NOT NULL DEFAULT '[]',
    -- Salientes seguidas sin contestar (Meta revoca el permiso a las 4).
    sin_contestar  integer     NOT NULL DEFAULT 0 CHECK (sin_contestar >= 0),
    ultima_solicitud_mensaje_id uuid,
    sincronizado_en timestamptz,
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX llamada_permisos_uq ON mensajeria.llamada_permisos ((coalesce(empresa_id, 0)), canal_id, numero);

-- La saliente: la respuesta SDP de Meta (solo mientras marca o está en curso), la devolución que
-- atiende y el navegador desde el que se llamó.
ALTER TABLE mensajeria.llamadas ADD COLUMN sdp_respuesta text;
ALTER TABLE mensajeria.llamadas ADD COLUMN devolucion_id bigint;
ALTER TABLE mensajeria.llamadas ADD COLUMN navegador text;
CREATE INDEX llamadas_salientes_idx ON mensajeria.llamadas ((coalesce(empresa_id, 0)), sono_en) WHERE direccion = 'saliente';

ALTER TABLE mensajeria.voz_ajustes ADD COLUMN max_salientes_por_dia integer NOT NULL DEFAULT 50 CHECK (max_salientes_por_dia BETWEEN 0 AND 1000);
ALTER TABLE mensajeria.voz_ajustes ADD COLUMN tope_minutos_salientes_mes integer NOT NULL DEFAULT 300 CHECK (tope_minutos_salientes_mes BETWEEN 0 AND 100000);
-- La plantilla call_permission_request aprobada del número (para pedir permiso fuera de la ventana).
ALTER TABLE mensajeria.voz_ajustes_canal ADD COLUMN plantilla_permiso text CHECK (plantilla_permiso IS NULL OR (char_length(plantilla_permiso) BETWEEN 1 AND 200 AND plantilla_permiso ~ '^[a-z0-9_]+$'));
ALTER TABLE mensajeria.voz_ajustes_canal ADD COLUMN plantilla_permiso_idioma text NOT NULL DEFAULT 'es' CHECK (plantilla_permiso_idioma ~ '^[a-z]{2}(_[A-Z]{2})?$');

ALTER TABLE mensajeria.conv_eventos DROP CONSTRAINT conv_eventos_tipo_check;
ALTER TABLE mensajeria.conv_eventos ADD CONSTRAINT conv_eventos_tipo_check
    CHECK (tipo IN ('mensaje_nuevo', 'mensaje_actualizado', 'conversacion_actualizada', 'mencion', 'guardia', 'aviso_sin_atender', 'sugerencia',
                    'llamada_entrante', 'llamada_actualizada', 'llamada_terminada', 'devolucion_actualizada', 'llamada_respuesta'));
ALTER TABLE mensajeria.conversacion_mensajes DROP CONSTRAINT conversacion_mensajes_automatico_check;
ALTER TABLE mensajeria.conversacion_mensajes ADD CONSTRAINT conversacion_mensajes_automatico_check
    CHECK (automatico IS NULL OR automatico IN ('bot', 'fuera_de_horario', 'asistente', 'permiso_llamada'));

-- +goose Down
UPDATE mensajeria.conversacion_mensajes SET automatico = 'bot' WHERE automatico = 'permiso_llamada';
ALTER TABLE mensajeria.conversacion_mensajes DROP CONSTRAINT conversacion_mensajes_automatico_check;
ALTER TABLE mensajeria.conversacion_mensajes ADD CONSTRAINT conversacion_mensajes_automatico_check
    CHECK (automatico IS NULL OR automatico IN ('bot', 'fuera_de_horario', 'asistente'));
DELETE FROM mensajeria.conv_eventos WHERE tipo = 'llamada_respuesta';
ALTER TABLE mensajeria.conv_eventos DROP CONSTRAINT conv_eventos_tipo_check;
ALTER TABLE mensajeria.conv_eventos ADD CONSTRAINT conv_eventos_tipo_check
    CHECK (tipo IN ('mensaje_nuevo', 'mensaje_actualizado', 'conversacion_actualizada', 'mencion', 'guardia', 'aviso_sin_atender', 'sugerencia',
                    'llamada_entrante', 'llamada_actualizada', 'llamada_terminada', 'devolucion_actualizada'));
ALTER TABLE mensajeria.voz_ajustes_canal DROP COLUMN plantilla_permiso_idioma;
ALTER TABLE mensajeria.voz_ajustes_canal DROP COLUMN plantilla_permiso;
ALTER TABLE mensajeria.voz_ajustes DROP COLUMN tope_minutos_salientes_mes;
ALTER TABLE mensajeria.voz_ajustes DROP COLUMN max_salientes_por_dia;
DROP INDEX mensajeria.llamadas_salientes_idx;
ALTER TABLE mensajeria.llamadas DROP COLUMN navegador;
ALTER TABLE mensajeria.llamadas DROP COLUMN devolucion_id;
ALTER TABLE mensajeria.llamadas DROP COLUMN sdp_respuesta;
DROP TABLE mensajeria.llamada_permisos;

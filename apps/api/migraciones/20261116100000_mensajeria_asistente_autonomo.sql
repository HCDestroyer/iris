-- +goose Up
-- Asistente de IA, fase 3b (contratos/conversaciones.md §17.8): el asistente atiende solo.

-- 1. Actor propio en la auditoría: las acciones del asistente quedan como asistente_ia (canal ia),
-- nunca como una persona ni como el sistema. La restricción entra sin validar (no escanea ni bloquea
-- la tabla); la valida, fuera de una transacción, la migración …_asistente_autonomo_validar.
ALTER TABLE auditoria.acciones DROP CONSTRAINT acciones_actor_tipo_check;
ALTER TABLE auditoria.acciones ADD CONSTRAINT acciones_actor_tipo_check
    CHECK (actor_tipo IN ('usuario', 'token', 'integracion', 'sistema', 'anonimo', 'cliente', 'asistente_ia')) NOT VALID;

-- 2. Mensajes del asistente: saliente sin autor con automatico = 'asistente'.
ALTER TABLE mensajeria.conversacion_mensajes DROP CONSTRAINT conversacion_mensajes_automatico_check;
ALTER TABLE mensajeria.conversacion_mensajes ADD CONSTRAINT conversacion_mensajes_automatico_check
    CHECK (automatico IS NULL OR automatico IN ('bot', 'fuera_de_horario', 'asistente')) NOT VALID;

-- 3. Estado del asistente en la conversación. NULL = no la atiende. «detenido»: una persona la tomó o
-- escribió (hasta que se resuelva o se reabra sin asignar).
ALTER TABLE mensajeria.conversaciones
    ADD COLUMN asistente_estado text CHECK (asistente_estado IS NULL OR asistente_estado IN ('atendiendo', 'espera_persona', 'detenido')),
    ADD COLUMN asistente_turnos integer NOT NULL DEFAULT 0 CHECK (asistente_turnos >= 0),
    ADD COLUMN asistente_presento_en timestamptz;

-- 4. Un turno por conversación y mensaje del cliente: la idempotencia de un job duplicado o de un
-- reintento. «tomado_en» es el arriendo del job que lo corre.
CREATE TABLE mensajeria.asistente_turnos (
    id              uuid PRIMARY KEY,
    empresa_id      bigint,
    conversacion_id bigint      NOT NULL REFERENCES mensajeria.conversaciones (id),
    mensaje_id      uuid        NOT NULL,
    numero          integer     NOT NULL,
    estado          text        NOT NULL CHECK (estado IN ('en_curso', 'respondio', 'paso_a_persona', 'descartado')),
    motivo          text,
    herramientas    integer     NOT NULL DEFAULT 0,
    tokens_entrada  bigint      NOT NULL DEFAULT 0,
    tokens_salida   bigint      NOT NULL DEFAULT 0,
    tomado_en       timestamptz NOT NULL,
    creado_en       timestamptz NOT NULL,
    terminado_en    timestamptz,
    UNIQUE (conversacion_id, mensaje_id)
);
CREATE INDEX asistente_turnos_empresa_idx ON mensajeria.asistente_turnos (coalesce(empresa_id, 0), creado_en);
CREATE INDEX asistente_turnos_creado_idx ON mensajeria.asistente_turnos (creado_en);

-- 5. Lo que ejecutó cada turno (sin argumentos ni texto): para no repetir una acción en un
-- reintento y para el resumen de «pasar a persona».
CREATE TABLE mensajeria.asistente_acciones (
    id              bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    turno_id        uuid        NOT NULL REFERENCES mensajeria.asistente_turnos (id) ON DELETE CASCADE,
    empresa_id      bigint,
    conversacion_id bigint      NOT NULL,
    paso            integer     NOT NULL,
    accion          text        NOT NULL,
    resultado       text        NOT NULL CHECK (resultado IN ('ok', 'rechazada', 'fallo')),
    entidad         text,
    entidad_id      text,
    creada_en       timestamptz NOT NULL,
    UNIQUE (turno_id, paso)
);
CREATE INDEX asistente_acciones_conv_idx ON mensajeria.asistente_acciones (conversacion_id, creada_en);

-- 6. Invariantes del asistente, por cualquier camino que cambie la conversación (tomar, asignar,
-- pasar, responder, resolver, préstamo revocado): con una persona asignada el asistente queda
-- detenido, y al resolverse (o al pasar a otra empresa) su estado vuelve a cero.
-- +goose StatementBegin
CREATE FUNCTION mensajeria.conv_asistente_estado() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF (NEW.estado = 'resuelta' AND OLD.estado <> 'resuelta') OR NEW.empresa_id IS DISTINCT FROM OLD.empresa_id THEN
        NEW.asistente_estado := NULL;
        NEW.asistente_turnos := 0;
        NEW.asistente_presento_en := NULL;
    ELSIF NEW.asignado_a IS NOT NULL AND NEW.asistente_estado IN ('atendiendo', 'espera_persona') THEN
        NEW.asistente_estado := 'detenido';
    END IF;
    RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER conv_asistente_estado BEFORE UPDATE OF estado, asignado_a, empresa_id, asistente_estado ON mensajeria.conversaciones
    FOR EACH ROW EXECUTE FUNCTION mensajeria.conv_asistente_estado();

-- +goose Down
DROP TRIGGER conv_asistente_estado ON mensajeria.conversaciones;
DROP FUNCTION mensajeria.conv_asistente_estado();
DROP TABLE mensajeria.asistente_acciones;
DROP TABLE mensajeria.asistente_turnos;
ALTER TABLE mensajeria.conversaciones
    DROP COLUMN asistente_estado, DROP COLUMN asistente_turnos, DROP COLUMN asistente_presento_en;
UPDATE mensajeria.conversacion_mensajes SET automatico = 'bot' WHERE automatico = 'asistente';
ALTER TABLE mensajeria.conversacion_mensajes DROP CONSTRAINT conversacion_mensajes_automatico_check;
ALTER TABLE mensajeria.conversacion_mensajes ADD CONSTRAINT conversacion_mensajes_automatico_check
    CHECK (automatico IS NULL OR automatico IN ('bot', 'fuera_de_horario'));
-- Las filas de auditoría del asistente no se tocan (auditoria.* nunca se actualiza): la restricción
-- vuelve sin validar las filas viejas.
ALTER TABLE auditoria.acciones DROP CONSTRAINT acciones_actor_tipo_check;
ALTER TABLE auditoria.acciones ADD CONSTRAINT acciones_actor_tipo_check
    CHECK (actor_tipo IN ('usuario', 'token', 'integracion', 'sistema', 'anonimo', 'cliente', 'asistente_ia')) NOT VALID;

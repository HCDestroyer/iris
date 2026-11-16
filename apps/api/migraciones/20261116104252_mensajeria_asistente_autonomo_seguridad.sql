-- +goose Up
-- Asistente de IA, fase 3b: correcciones de la revisión de seguridad de la #163.

-- 1. Verificación en dos niveles. «numero»: el número coincide con un teléfono del cliente (lo que
-- ya había). «fuerte»: identidad_verificada_en, que se pone cuando lo confirma una persona con permiso
-- o cuando pasa el desafío de identidad de Argos. Vale para ese cliente y solo en el periodo abierto
-- en que se obtuvo: al resolverse la conversación se pierde (el trigger de abajo).
ALTER TABLE mensajeria.conversaciones
    ADD COLUMN identidad_verificada_en timestamptz,
    -- Hasta cuándo vale la verificación fuerte dentro del periodo (verificacion_fuerte_min de la empresa).
    ADD COLUMN identidad_vence_en timestamptz,
    -- El bot y fuera de horario esperan a la clasificación cuando el modo autónomo está encendido:
    -- el mensaje del cliente que quedó pendiente de ese paso.
    ADD COLUMN bot_diferido_mensaje_id uuid;

-- Intentos del desafío de identidad (nunca el valor): máximo 3 por conversación y día.
CREATE TABLE mensajeria.asistente_verificaciones (
    id              bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    empresa_id      bigint,
    conversacion_id bigint      NOT NULL REFERENCES mensajeria.conversaciones (id),
    cliente_id      bigint      NOT NULL,
    resultado       text        NOT NULL CHECK (resultado IN ('coincide', 'no_coincide', 'no_disponible', 'error')),
    creada_en       timestamptz NOT NULL
);
CREATE INDEX asistente_verificaciones_conv_idx ON mensajeria.asistente_verificaciones (conversacion_id, creada_en);

-- 2. Reconexión temporal pedida a Argos (contrato con Argos §7.3): registro local de lo pedido y de
-- lo que decidió Argos. Una por comprobante.
CREATE TABLE mensajeria.reconexiones (
    id               uuid PRIMARY KEY,
    empresa_id       bigint,
    conversacion_id  bigint      NOT NULL REFERENCES mensajeria.conversaciones (id),
    boleta_id        uuid        NOT NULL REFERENCES mensajeria.boletas (id),
    comprobante_id   bigint      NOT NULL,
    cliente_id       bigint      NOT NULL,
    servicio_id      bigint,
    -- Argos solo acepta fuera_de_horario (§7.3).
    motivo           text        NOT NULL CHECK (motivo IN ('fuera_de_horario')),
    -- pendiente: se registró el intento y todavía no se sabe qué dijo Argos (se reintenta con la
    -- misma clave, iris:reconexion:<comprobante>).
    estado           text        NOT NULL CHECK (estado IN ('pendiente', 'aceptada', 'rechazada')),
    intentos         integer     NOT NULL DEFAULT 0,
    resuelta_en      timestamptz,
    promesa_argos_id bigint,
    hasta            timestamptz,
    detalle          text,
    solicitada_por   bigint REFERENCES nucleo.usuarios (id),
    por_asistente    boolean     NOT NULL DEFAULT false,
    created_at       timestamptz NOT NULL DEFAULT now(),
    UNIQUE (comprobante_id)
);
CREATE INDEX reconexiones_conv_idx ON mensajeria.reconexiones (conversacion_id);
CREATE INDEX reconexiones_pendientes_idx ON mensajeria.reconexiones (created_at) WHERE estado = 'pendiente';

-- 3. Quién registró una boleta cuando no fue una persona.
ALTER TABLE mensajeria.boletas ADD COLUMN por_asistente boolean NOT NULL DEFAULT false;
UPDATE mensajeria.boletas SET por_asistente = true WHERE registrada_por IS NULL;

-- 4. El mensaje del asistente que llevó el monto del saldo: al armar el historial para un modelo, el
-- monto va enmascarado.
ALTER TABLE mensajeria.conversacion_mensajes ADD COLUMN con_saldo boolean NOT NULL DEFAULT false;

-- 5. Presupuesto propio del modo autónomo (porcentaje del tope del mes) y su uso, para que atender
-- solo no apague las sugerencias a los agentes.
ALTER TABLE mensajeria.asistente_ajustes
    ADD COLUMN presupuesto_autonomo_pct integer NOT NULL DEFAULT 70 CHECK (presupuesto_autonomo_pct BETWEEN 1 AND 100);
ALTER TABLE mensajeria.asistente_uso
    ADD COLUMN tokens_autonomo bigint NOT NULL DEFAULT 0 CHECK (tokens_autonomo >= 0),
    -- Lo reservado por turnos autónomos en curso: el presupuesto se reserva de forma atómica.
    ADD COLUMN tokens_autonomo_reservados bigint NOT NULL DEFAULT 0 CHECK (tokens_autonomo_reservados >= 0);
ALTER TABLE mensajeria.asistente_reservas ADD COLUMN autonomo boolean NOT NULL DEFAULT false;
-- Vigencia de la verificación fuerte dentro de un periodo (minutos).
ALTER TABLE mensajeria.asistente_ajustes
    ADD COLUMN verificacion_fuerte_min integer NOT NULL DEFAULT 60 CHECK (verificacion_fuerte_min BETWEEN 5 AND 1440);

-- 5b. Perfiles guardados en la fase 3a: las acciones de dinero que venían encendidas por defecto
-- (dar_saldo y recibir_boleta) se apagan; cada empresa las enciende cuando quiera usarlas.
-- B-2 inicio
UPDATE mensajeria.asistente_ajustes a
SET perfiles = (SELECT jsonb_agg(CASE WHEN jsonb_typeof(p -> 'acciones') = 'object'
                                      THEN jsonb_set(p, '{acciones}', (p -> 'acciones') || '{"dar_saldo": false, "recibir_boleta": false}'::jsonb)
                                      ELSE p END ORDER BY n)
                FROM jsonb_array_elements(a.perfiles) WITH ORDINALITY AS e(p, n))
WHERE jsonb_typeof(a.perfiles) = 'array' AND jsonb_array_length(a.perfiles) > 0;
-- B-2 fin

-- 6. El estado del asistente y la verificación fuerte, por cualquier camino: al cambiar de cliente o
-- de empresa, o al resolverse, la verificación fuerte del desafío se pierde.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION mensajeria.conv_asistente_estado() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF (NEW.estado = 'resuelta' AND OLD.estado <> 'resuelta') OR NEW.empresa_id IS DISTINCT FROM OLD.empresa_id THEN
        NEW.asistente_estado := NULL;
        NEW.asistente_turnos := 0;
        NEW.asistente_presento_en := NULL;
        NEW.identidad_verificada_en := NULL;
        NEW.identidad_vence_en := NULL;
        NEW.bot_diferido_mensaje_id := NULL;
    ELSIF NEW.asignado_a IS NOT NULL AND NEW.asistente_estado IN ('atendiendo', 'espera_persona') THEN
        NEW.asistente_estado := 'detenido';
    END IF;
    -- Al reabrirse empieza un periodo nuevo: la verificación fuerte no viene de la conversación
    -- resuelta (tampoco si alguien la confirmó mientras estaba resuelta).
    IF OLD.estado = 'resuelta' AND NEW.estado <> 'resuelta' THEN
        NEW.identidad_verificada_en := NULL;
        NEW.identidad_vence_en := NULL;
    END IF;
    IF NEW.cliente_id IS DISTINCT FROM OLD.cliente_id THEN
        NEW.identidad_verificada_en := NULL;
        NEW.identidad_vence_en := NULL;
    END IF;
    RETURN NEW;
END $$;
-- +goose StatementEnd
DROP TRIGGER conv_asistente_estado ON mensajeria.conversaciones;
CREATE TRIGGER conv_asistente_estado BEFORE UPDATE OF estado, asignado_a, empresa_id, cliente_id, asistente_estado ON mensajeria.conversaciones
    FOR EACH ROW EXECUTE FUNCTION mensajeria.conv_asistente_estado();

-- 7. Revisión de la migración anterior: la clave foránea que faltaba (sin validar: la valida la
-- migración …_asistente_autonomo_validar, fuera de una transacción) y el índice de los turnos en curso.
ALTER TABLE mensajeria.asistente_acciones
    ADD CONSTRAINT asistente_acciones_conversacion_fk FOREIGN KEY (conversacion_id) REFERENCES mensajeria.conversaciones (id) NOT VALID;
CREATE INDEX asistente_turnos_en_curso_idx ON mensajeria.asistente_turnos (tomado_en) WHERE estado = 'en_curso';

-- +goose Down
DROP INDEX mensajeria.asistente_turnos_en_curso_idx;
ALTER TABLE mensajeria.asistente_acciones DROP CONSTRAINT asistente_acciones_conversacion_fk;
DROP TRIGGER conv_asistente_estado ON mensajeria.conversaciones;
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION mensajeria.conv_asistente_estado() RETURNS trigger LANGUAGE plpgsql AS $$
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
ALTER TABLE mensajeria.asistente_reservas DROP COLUMN autonomo;
ALTER TABLE mensajeria.asistente_ajustes DROP COLUMN verificacion_fuerte_min;
ALTER TABLE mensajeria.asistente_uso DROP COLUMN tokens_autonomo, DROP COLUMN tokens_autonomo_reservados;
ALTER TABLE mensajeria.asistente_ajustes DROP COLUMN presupuesto_autonomo_pct;
ALTER TABLE mensajeria.conversacion_mensajes DROP COLUMN con_saldo;
ALTER TABLE mensajeria.boletas DROP COLUMN por_asistente;
DROP TABLE mensajeria.reconexiones;
DROP TABLE mensajeria.asistente_verificaciones;
ALTER TABLE mensajeria.conversaciones DROP COLUMN identidad_verificada_en, DROP COLUMN identidad_vence_en, DROP COLUMN bot_diferido_mensaje_id;

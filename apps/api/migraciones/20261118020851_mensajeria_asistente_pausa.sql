-- +goose Up
-- Pausar y reanudar el asistente de IA en una conversación (contratos/conversaciones.md §17.11).
-- La pausa vive en la conversación: quién, cuándo y por qué. Mientras está, el asistente no atiende
-- solo esa conversación (las sugerencias al agente siguen: no llegan al cliente). Se quita al
-- reanudar o cuando la conversación se resuelve.
-- asistente_reanudado_en: desde cuándo cuenta «una persona ya escribió» después de que alguien le
-- devolvió la conversación al asistente.
-- Las restricciones nacen sin validar (NOT VALID): así el ALTER no recorre la tabla con su candado
-- ACCESS EXCLUSIVE. Valen desde ya para cada fila que se escriba, y las valida la migración
-- …_asistente_pausa_validar, fuera de una transacción.
ALTER TABLE mensajeria.conversaciones
    ADD COLUMN asistente_pausado_en timestamptz,
    ADD COLUMN asistente_pausado_por bigint,
    ADD COLUMN asistente_pausa_motivo text,
    ADD COLUMN asistente_reanudado_en timestamptz;
ALTER TABLE mensajeria.conversaciones
    ADD CONSTRAINT conversaciones_asistente_pausado_por_fk FOREIGN KEY (asistente_pausado_por) REFERENCES nucleo.usuarios (id) NOT VALID,
    ADD CONSTRAINT conversaciones_asistente_pausa_motivo_ck CHECK (asistente_pausa_motivo IS NULL OR asistente_pausa_motivo IN
        ('manual', 'caso_delicado', 'respuesta_incorrecta', 'cliente_lo_pidio')) NOT VALID,
    ADD CONSTRAINT conversaciones_asistente_pausa_ck CHECK ((asistente_pausado_en IS NULL) = (asistente_pausa_motivo IS NULL)) NOT VALID;

-- Un turno del asistente que se descartó por una pausa (o porque una persona la tomó) se puede
-- reabrir al reanudar, pero una sola vez: reaperturas lo cuenta. Un turno reabierto pasa otra vez
-- por los límites por hora y por maxTurnos (pausar y reanudar en bucle no los salta).
ALTER TABLE mensajeria.asistente_turnos ADD COLUMN reaperturas smallint NOT NULL DEFAULT 0;

-- El estado del asistente, por cualquier camino. Lo nuevo:
--   * al resolverse o cambiar de empresa se quitan la pausa y la reanudación (son del periodo);
--   * cuando el asistente queda detenido (una persona la tomó, escribió o lo pausó) y cuando la
--     conversación se reabre, se borra «lo último que hizo el asistente»: esa línea de la bandeja
--     («Respondió al cliente») ya no describe lo que pasa y hacía creer que el asistente seguía
--     contestando.
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
        NEW.asistente_pausado_en := NULL;
        NEW.asistente_pausado_por := NULL;
        NEW.asistente_pausa_motivo := NULL;
        NEW.asistente_reanudado_en := NULL;
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
    IF (NEW.asistente_estado = 'detenido' AND OLD.asistente_estado IS DISTINCT FROM 'detenido')
        OR (OLD.estado = 'resuelta' AND NEW.estado <> 'resuelta') THEN
        NEW.asistente_actividad_tipo := NULL;
        NEW.asistente_actividad_texto := NULL;
        NEW.asistente_actividad_en := NULL;
    END IF;
    RETURN NEW;
END $$;
-- +goose StatementEnd

-- Lo que ya quedó mintiendo en la bandeja: conversaciones con el asistente detenido que todavía
-- muestran su última actividad.
UPDATE mensajeria.conversaciones
SET asistente_actividad_tipo = NULL, asistente_actividad_texto = NULL, asistente_actividad_en = NULL
WHERE asistente_estado = 'detenido' AND asistente_actividad_tipo IS NOT NULL;

-- +goose Down
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
ALTER TABLE mensajeria.asistente_turnos DROP COLUMN reaperturas;
ALTER TABLE mensajeria.conversaciones
    DROP CONSTRAINT conversaciones_asistente_pausa_ck,
    DROP CONSTRAINT conversaciones_asistente_pausa_motivo_ck,
    DROP CONSTRAINT conversaciones_asistente_pausado_por_fk,
    DROP COLUMN asistente_pausado_en, DROP COLUMN asistente_pausado_por, DROP COLUMN asistente_pausa_motivo, DROP COLUMN asistente_reanudado_en;

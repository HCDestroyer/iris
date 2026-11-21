-- +goose Up
-- Reporte del PM (0.34.0): después de que el asistente pasara una conversación a una persona por
-- max_turnos, «Que siga el asistente» (reanudar, contratos/conversaciones.md §17.11) no servía: con
-- el siguiente mensaje del cliente volvía a contar los turnos de antes y la volvía a pasar.
-- asistente_atencion_desde: desde cuándo cuenta maxTurnos cuando una persona le devolvió la
-- conversación al asistente después de pasarla a una persona o de tomarla una persona (una atención
-- nueva). Una pausa no la cambia: pausar y reanudar no reinicia el máximo. Columna nula, sin
-- valor por defecto: el ALTER no recorre la tabla.
ALTER TABLE mensajeria.conversaciones ADD COLUMN asistente_atencion_desde timestamptz;

-- El estado del asistente, por cualquier camino (igual que …_asistente_pausa): lo nuevo es que al
-- resolverse o cambiar de empresa también se quita asistente_atencion_desde (es del periodo).
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
        NEW.asistente_atencion_desde := NULL;
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
        NEW.asistente_pausado_en := NULL;
        NEW.asistente_pausado_por := NULL;
        NEW.asistente_pausa_motivo := NULL;
        NEW.asistente_reanudado_en := NULL;
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
    IF (NEW.asistente_estado = 'detenido' AND OLD.asistente_estado IS DISTINCT FROM 'detenido')
        OR (OLD.estado = 'resuelta' AND NEW.estado <> 'resuelta') THEN
        NEW.asistente_actividad_tipo := NULL;
        NEW.asistente_actividad_texto := NULL;
        NEW.asistente_actividad_en := NULL;
    END IF;
    RETURN NEW;
END $$;
-- +goose StatementEnd
ALTER TABLE mensajeria.conversaciones DROP COLUMN asistente_atencion_desde;

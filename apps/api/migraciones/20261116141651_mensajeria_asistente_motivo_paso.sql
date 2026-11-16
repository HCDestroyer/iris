-- +goose Up
-- Por qué el asistente de IA pasó la conversación a una persona (§17.8.2, asistente.motivoPaso): vive en
-- la conversación, junto a su estado, y solo vale mientras ese estado es espera_persona. Así nunca
-- sale el motivo de un periodo anterior ni el de otra empresa (un número prestado).
ALTER TABLE mensajeria.conversaciones
    ADD COLUMN asistente_motivo_paso text CHECK (asistente_motivo_paso IS NULL OR asistente_motivo_paso IN
        ('cliente_lo_pidio', 'enojo', 'sin_respuesta', 'herramienta_fallo', 'max_turnos', 'tope', 'asistente_apagado'));

-- Corre después de conv_asistente_estado (los triggers de una tabla van por orden de nombre): cuando el
-- estado del asistente deja de ser espera_persona (se resolvió, se reabrió, cambió de empresa, la tomó
-- una persona o el asistente volvió a atender), el motivo se borra.
-- +goose StatementBegin
CREATE FUNCTION mensajeria.conv_asistente_motivo() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.asistente_estado IS DISTINCT FROM 'espera_persona'
        OR (OLD.estado = 'resuelta' AND NEW.estado <> 'resuelta')
        OR NEW.empresa_id IS DISTINCT FROM OLD.empresa_id THEN
        NEW.asistente_motivo_paso := NULL;
    END IF;
    RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER conv_asistente_motivo BEFORE UPDATE OF estado, asignado_a, empresa_id, cliente_id, asistente_estado ON mensajeria.conversaciones
    FOR EACH ROW EXECUTE FUNCTION mensajeria.conv_asistente_motivo();

-- +goose Down
DROP TRIGGER conv_asistente_motivo ON mensajeria.conversaciones;
DROP FUNCTION mensajeria.conv_asistente_motivo();
ALTER TABLE mensajeria.conversaciones DROP COLUMN asistente_motivo_paso;

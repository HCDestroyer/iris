-- +goose Up
-- Historial de la membresía de las cuadrillas (contrato con ArgosISP 0.14.0 §3.10, M-7): el técnico
-- que dejó la orden como miembro de su cuadrilla envía sus cambios e intentos hasta 12 h después de
-- lo primero que pase (que le quiten la orden o que lo quiten de la cuadrilla), y el corte inmediato
-- de §3.9 sabe de qué cuadrillas era miembro en una fecha. Lo lleva un trigger sobre la ficha con
-- la hora del servidor: cualquier cambio de cuadrilla o de activo cierra el periodo y abre otro.
CREATE TABLE agenda.cuadrilla_miembros (
    id           bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    usuario_id   bigint      NOT NULL REFERENCES nucleo.usuarios (id) ON DELETE CASCADE,
    cuadrilla_id bigint      NOT NULL REFERENCES agenda.cuadrillas (id) ON DELETE CASCADE,
    desde        timestamptz NOT NULL DEFAULT now(),
    hasta        timestamptz,
    CHECK (hasta IS NULL OR hasta >= desde)
);
CREATE INDEX cuadrilla_miembros_usuario_idx ON agenda.cuadrilla_miembros (usuario_id, cuadrilla_id, desde);
CREATE UNIQUE INDEX cuadrilla_miembros_vigente_uq ON agenda.cuadrilla_miembros (usuario_id) WHERE hasta IS NULL;
INSERT INTO agenda.cuadrilla_miembros (usuario_id, cuadrilla_id)
SELECT usuario_id, cuadrilla_id FROM agenda.tecnicos WHERE cuadrilla_id IS NOT NULL AND activo;

-- +goose StatementBegin
CREATE FUNCTION agenda.historial_cuadrilla() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    antes bigint := NULL;
    despues bigint := NULL;
BEGIN
    IF TG_OP = 'UPDATE' AND OLD.activo THEN
        antes := OLD.cuadrilla_id;
    END IF;
    IF NEW.activo THEN
        despues := NEW.cuadrilla_id;
    END IF;
    IF antes IS NOT DISTINCT FROM despues THEN
        RETURN NEW;
    END IF;
    UPDATE agenda.cuadrilla_miembros SET hasta = now() WHERE usuario_id = NEW.usuario_id AND hasta IS NULL;
    IF despues IS NOT NULL THEN
        INSERT INTO agenda.cuadrilla_miembros (usuario_id, cuadrilla_id) VALUES (NEW.usuario_id, despues);
    END IF;
    RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER tecnicos_historial_cuadrilla AFTER INSERT OR UPDATE OF cuadrilla_id, activo ON agenda.tecnicos
    FOR EACH ROW EXECUTE FUNCTION agenda.historial_cuadrilla();

-- +goose Down
DROP TRIGGER IF EXISTS tecnicos_historial_cuadrilla ON agenda.tecnicos;
DROP FUNCTION IF EXISTS agenda.historial_cuadrilla();
DROP TABLE IF EXISTS agenda.cuadrilla_miembros;

-- +goose Up
-- Vínculo de cada técnico con Argos (contrato con ArgosISP 0.14.0 §6.6, §3.9 y §3.10).
--
-- agenda.tecnicos: version_vinculo (entera y creciente por técnico: cada tecnico.argos_* sube una) y
-- argos_conector_id (el conector del que salió el id_externo: solo a él se mandan los webhooks y
-- solo él concilia con GET …/tecnicos/:usuario_id).
ALTER TABLE agenda.tecnicos ADD COLUMN version_vinculo integer NOT NULL DEFAULT 0;
ALTER TABLE agenda.tecnicos ADD COLUMN argos_conector_id bigint REFERENCES integracion.conectores (id);
UPDATE agenda.tecnicos t SET argos_conector_id = (
    SELECT c.id FROM integracion.conectores c WHERE c.sistema = 'argosisp' AND c.estado = 'activo' ORDER BY c.id DESC LIMIT 1)
WHERE t.argos_id_externo IS NOT NULL;

-- agenda.cuadrilla_miembros: historial de la membresía (hora del servidor) para la gracia de 12 h del
-- técnico que dejó la orden como miembro de su cuadrilla (0.14.0 §3.10). Lo lleva un trigger sobre
-- la ficha: cualquier cambio de cuadrilla o de activo cierra el periodo y abre otro.
CREATE TABLE agenda.cuadrilla_miembros (
    id           bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    usuario_id   bigint      NOT NULL REFERENCES nucleo.usuarios (id),
    cuadrilla_id bigint      NOT NULL REFERENCES agenda.cuadrillas (id),
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

-- Carga inicial de vínculos al informar 0.14.0 (§6.6): la carga en curso (sus eventos llevan
-- carga_id), cuántos vínculos tiene y cuándo se cerró con PUT …/contrato { vinculos_cargados }.
ALTER TABLE integracion.conectores ADD COLUMN vinculos_carga_id uuid;
ALTER TABLE integracion.conectores ADD COLUMN vinculos_carga_n integer;
ALTER TABLE integracion.conectores ADD COLUMN vinculos_cargados_en timestamptz;

-- Webhooks tecnico.argos_* hacia Argos: el conector de origen del vínculo (solo a él se mandan).
ALTER TABLE mensajeria.webhooks_salida ADD COLUMN conector_origen bigint;
ALTER TABLE mensajeria.webhooks_salida DROP CONSTRAINT webhooks_salida_evento_check;
ALTER TABLE mensajeria.webhooks_salida ADD CONSTRAINT webhooks_salida_evento_check
    CHECK (evento IN ('aviso.entregado', 'aviso.fallido', 'contacto.baja', 'cuadrilla.creada', 'cuadrilla.actualizada',
                      'tecnico.argos_vinculado', 'tecnico.argos_desvinculado'));

-- Aviso de campana cuando Argos tiene otro cuerpo para la clave de un cambio o intento (0.14.0 §3.10).
ALTER TABLE notificaciones.avisos DROP CONSTRAINT avisos_tipo_check;
ALTER TABLE notificaciones.avisos ADD CONSTRAINT avisos_tipo_check
    CHECK (tipo IN ('asistente_tope', 'servidor_ia_caido', 'servidor_ia_incompatible', 'servidor_ia_umbral', 'argos_certificado',
                    'argos_clave_reutilizada'));

-- +goose Down
DELETE FROM notificaciones.avisos WHERE tipo = 'argos_clave_reutilizada';
ALTER TABLE notificaciones.avisos DROP CONSTRAINT avisos_tipo_check;
ALTER TABLE notificaciones.avisos ADD CONSTRAINT avisos_tipo_check
    CHECK (tipo IN ('asistente_tope', 'servidor_ia_caido', 'servidor_ia_incompatible', 'servidor_ia_umbral', 'argos_certificado'));
DELETE FROM mensajeria.webhooks_salida WHERE evento IN ('tecnico.argos_vinculado', 'tecnico.argos_desvinculado');
ALTER TABLE mensajeria.webhooks_salida DROP CONSTRAINT webhooks_salida_evento_check;
ALTER TABLE mensajeria.webhooks_salida ADD CONSTRAINT webhooks_salida_evento_check
    CHECK (evento IN ('aviso.entregado', 'aviso.fallido', 'contacto.baja', 'cuadrilla.creada', 'cuadrilla.actualizada'));
ALTER TABLE mensajeria.webhooks_salida DROP COLUMN IF EXISTS conector_origen;
ALTER TABLE integracion.conectores DROP COLUMN IF EXISTS vinculos_cargados_en;
ALTER TABLE integracion.conectores DROP COLUMN IF EXISTS vinculos_carga_n;
ALTER TABLE integracion.conectores DROP COLUMN IF EXISTS vinculos_carga_id;
DROP TRIGGER IF EXISTS tecnicos_historial_cuadrilla ON agenda.tecnicos;
DROP FUNCTION IF EXISTS agenda.historial_cuadrilla();
DROP TABLE IF EXISTS agenda.cuadrilla_miembros;
ALTER TABLE agenda.tecnicos DROP COLUMN IF EXISTS argos_conector_id;
ALTER TABLE agenda.tecnicos DROP COLUMN IF EXISTS version_vinculo;

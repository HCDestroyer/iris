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

-- Carga inicial de vínculos al informar 0.14.0 (§6.6): la carga en curso (sus eventos llevan
-- carga_id), cuántos vínculos tiene y cuándo se cerró con PUT …/contrato { vinculos_cargados }.
ALTER TABLE integracion.conectores ADD COLUMN vinculos_carga_id uuid;
ALTER TABLE integracion.conectores ADD COLUMN vinculos_carga_n integer;
ALTER TABLE integracion.conectores ADD COLUMN vinculos_cargados_en timestamptz;

-- Webhooks tecnico.argos_* hacia Argos: el conector de origen del vínculo (solo a él se mandan).
ALTER TABLE mensajeria.webhooks_salida ADD COLUMN conector_origen bigint;
-- La carga inicial a la que pertenece un vinculado (se cierra con el 2xx de todos).
ALTER TABLE mensajeria.webhooks_salida ADD COLUMN carga_id uuid;
CREATE INDEX webhooks_salida_carga_idx ON mensajeria.webhooks_salida (carga_id) WHERE carga_id IS NOT NULL;
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
ALTER TABLE mensajeria.webhooks_salida DROP COLUMN IF EXISTS carga_id;
ALTER TABLE mensajeria.webhooks_salida DROP COLUMN IF EXISTS conector_origen;
ALTER TABLE integracion.conectores DROP COLUMN IF EXISTS vinculos_cargados_en;
ALTER TABLE integracion.conectores DROP COLUMN IF EXISTS vinculos_carga_n;
ALTER TABLE integracion.conectores DROP COLUMN IF EXISTS vinculos_carga_id;
ALTER TABLE agenda.tecnicos DROP COLUMN IF EXISTS argos_conector_id;
ALTER TABLE agenda.tecnicos DROP COLUMN IF EXISTS version_vinculo;

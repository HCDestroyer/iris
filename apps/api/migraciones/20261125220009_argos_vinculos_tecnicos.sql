-- +goose Up
-- Vínculo de cada técnico con Argos (contrato con ArgosISP 0.14.0 §6.6, §3.9 y §3.10).
--
-- agenda.tecnicos: version_vinculo (entera y creciente por técnico: cada cambio del vínculo sube una,
-- la misma en los eventos de todas sus empresas) y argos_conector_id (el conector de la instancia de
-- Argos de la que salió el id_externo: solo a él se mandan los webhooks y solo él concilia con
-- GET …/tecnicos/:usuario_id; al reconectar la misma instancia —misma base_url— pasa al conector nuevo).
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
-- Cuándo se avisó al admin que la carga está detenida por vinculados agotados (uno por episodio).
ALTER TABLE integracion.conectores ADD COLUMN vinculos_carga_avisada_en timestamptz;

-- Una operación con `tecnico` que espera el 2xx del vinculado de su técnico (depende_de, §6.6):
-- desde cuándo espera (tope de 24 h; luego queda bloqueada a la vista).
ALTER TABLE integracion.pendientes ADD COLUMN esperando_vinculo_desde timestamptz;

-- Webhooks tecnico.argos_* hacia Argos: el conector de origen del vínculo (solo a él se mandan).
ALTER TABLE mensajeria.webhooks_salida ADD COLUMN conector_origen bigint;
-- La carga inicial a la que pertenece un vinculado (se cierra con el 2xx de todos).
ALTER TABLE mensajeria.webhooks_salida ADD COLUMN carga_id uuid;
CREATE INDEX webhooks_salida_carga_idx ON mensajeria.webhooks_salida (carga_id) WHERE carga_id IS NOT NULL;
ALTER TABLE mensajeria.webhooks_salida DROP CONSTRAINT webhooks_salida_evento_check;
ALTER TABLE mensajeria.webhooks_salida ADD CONSTRAINT webhooks_salida_evento_check
    CHECK (evento IN ('aviso.entregado', 'aviso.fallido', 'contacto.baja', 'prospecto.estado_cambiado', 'cuadrilla.creada', 'cuadrilla.actualizada',
                      'tecnico.argos_vinculado', 'tecnico.argos_desvinculado'));

-- Aviso de campana cuando Argos tiene otro cuerpo para la clave de un cambio o intento (0.14.0 §3.10).
ALTER TABLE notificaciones.avisos DROP CONSTRAINT avisos_tipo_check;
ALTER TABLE notificaciones.avisos ADD CONSTRAINT avisos_tipo_check
    CHECK (tipo IN ('asistente_tope', 'servidor_ia_caido', 'servidor_ia_incompatible', 'servidor_ia_umbral', 'argos_certificado',
                    'venta_revisada', 'paso_titular', 'contrato_sin_firma', 'posible_falla_general', 'verificacion_vencida',
                    'argos_clave_reutilizada', 'argos_carga_vinculos'));

-- +goose Down
DELETE FROM notificaciones.avisos WHERE tipo IN ('argos_clave_reutilizada', 'argos_carga_vinculos');
ALTER TABLE notificaciones.avisos DROP CONSTRAINT avisos_tipo_check;
ALTER TABLE notificaciones.avisos ADD CONSTRAINT avisos_tipo_check
    CHECK (tipo IN ('asistente_tope', 'servidor_ia_caido', 'servidor_ia_incompatible', 'servidor_ia_umbral', 'argos_certificado',
                    'venta_revisada', 'paso_titular', 'contrato_sin_firma', 'posible_falla_general', 'verificacion_vencida'));
DELETE FROM mensajeria.webhooks_salida WHERE evento IN ('tecnico.argos_vinculado', 'tecnico.argos_desvinculado');
ALTER TABLE mensajeria.webhooks_salida DROP CONSTRAINT webhooks_salida_evento_check;
ALTER TABLE mensajeria.webhooks_salida ADD CONSTRAINT webhooks_salida_evento_check
    CHECK (evento IN ('aviso.entregado', 'aviso.fallido', 'contacto.baja', 'prospecto.estado_cambiado', 'cuadrilla.creada', 'cuadrilla.actualizada'));
ALTER TABLE mensajeria.webhooks_salida DROP COLUMN IF EXISTS carga_id;
ALTER TABLE mensajeria.webhooks_salida DROP COLUMN IF EXISTS conector_origen;
ALTER TABLE integracion.pendientes DROP COLUMN IF EXISTS esperando_vinculo_desde;
ALTER TABLE integracion.conectores DROP COLUMN IF EXISTS vinculos_carga_avisada_en;
ALTER TABLE integracion.conectores DROP COLUMN IF EXISTS vinculos_cargados_en;
ALTER TABLE integracion.conectores DROP COLUMN IF EXISTS vinculos_carga_n;
ALTER TABLE integracion.conectores DROP COLUMN IF EXISTS vinculos_carga_id;
ALTER TABLE agenda.tecnicos DROP COLUMN IF EXISTS argos_conector_id;
ALTER TABLE agenda.tecnicos DROP COLUMN IF EXISTS version_vinculo;

-- Comprobaciones al terminar la prueba de carga (docs/runbooks/carga.md).
--
--   psql -v ON_ERROR_STOP=1 -v desde='2026-10-29T17:00:00Z' -f verificar.sql
--
-- 1. Ninguna operación duplicada en la base desde :desde (inicio de la corrida). El lote de sync
--    es idempotente por id de operación y POST /tickets/{id}/responder por Idempotency-Key; k6
--    reenvía a propósito lotes y respuestas, así que cualquier efecto repetido es un error:
--      - notas de campo con el mismo texto (k6 pone el id de la operación en el texto);
--      - marcas de checklist con el mismo operacion_id;
--      - transiciones de una orden que solo pasan una vez (programada → en_camino, en_camino →
--        en_sitio, → fallida; reanudar vuelve a en_sitio desde pausada y no cuenta), en
--        campo.orden_eventos y en el outbox (orden.estado_cambiada);
--      - mensajes de ticket con el mismo cuerpo (k6 pone su Idempotency-Key en el cuerpo).
--    Si hay alguno, termina con error (make carga falla).
-- 2. Informativo: operaciones aplicadas, posiciones, respuestas y cómo corrió el cron de SLA
--    (soporte.revisar_sla en River: corridas, duración, errores y alertas emitidas).

\set ON_ERROR_STOP on
\pset footer off

CREATE TEMP TABLE carga_duplicados (comprobacion text, repetidos bigint, ejemplo text);

INSERT INTO carga_duplicados
SELECT 'notas de campo repetidas', count(*), min(texto)
FROM (SELECT texto FROM campo.orden_notas
      WHERE created_at >= :'desde' AND texto LIKE '%[carga %'
      GROUP BY texto HAVING count(*) > 1) d;

INSERT INTO carga_duplicados
SELECT 'marcas de checklist repetidas (operacion_id)', count(*), min(operacion_id::text)
FROM (SELECT operacion_id FROM campo.orden_checklist_historial
      WHERE created_at >= :'desde' AND operacion_id IS NOT NULL
      GROUP BY operacion_id HAVING count(*) > 1) d;

INSERT INTO carga_duplicados
SELECT 'transiciones de orden repetidas (orden_eventos)', count(*), min(orden_id || ': ' || de || ' → ' || a)
FROM (SELECT orden_id, de, a FROM campo.orden_eventos
      WHERE created_at >= :'desde' AND tipo = 'estado' AND (de, a) IN (('programada', 'en_camino'), ('en_camino', 'en_sitio'))
         OR (created_at >= :'desde' AND tipo = 'estado' AND a = 'fallida')
      GROUP BY orden_id, de, a HAVING count(*) > 1) d;

INSERT INTO carga_duplicados
SELECT 'eventos orden.estado_cambiada repetidos (outbox)', count(*), min(orden || ': ' || de || ' → ' || a)
FROM (SELECT payload ->> 'orden_id' AS orden, payload ->> 'de' AS de, payload ->> 'a' AS a FROM integracion.outbox
      WHERE created_at >= :'desde' AND evento = 'orden.estado_cambiada'
        AND ((payload ->> 'de', payload ->> 'a') IN (('programada', 'en_camino'), ('en_camino', 'en_sitio'))
             OR payload ->> 'a' = 'fallida')
      GROUP BY 1, 2, 3 HAVING count(*) > 1) d;

INSERT INTO carga_duplicados
SELECT 'respuestas de ticket repetidas', count(*), min(cuerpo)
FROM (SELECT ticket_id, cuerpo FROM soporte.ticket_mensajes
      WHERE created_at >= :'desde' AND cuerpo LIKE '%[carga %'
      GROUP BY ticket_id, cuerpo HAVING count(*) > 1) d;

\echo
\echo '== Duplicados (todo debe ser 0)'
SELECT comprobacion, repetidos, coalesce(ejemplo, '') AS ejemplo FROM carga_duplicados;

\echo '== Volumen de la corrida'
SELECT
    (SELECT count(*) FROM campo.operaciones_sync WHERE created_at >= :'desde' AND estado = 'aplicada') AS ops_aplicadas,
    (SELECT count(*) FROM campo.operaciones_sync WHERE created_at >= :'desde' AND estado = 'rechazada') AS ops_rechazadas,
    (SELECT count(*) FROM campo.posiciones WHERE created_at >= :'desde') AS posiciones,
    (SELECT count(*) FROM soporte.ticket_mensajes WHERE created_at >= :'desde' AND cuerpo LIKE '%[carga %') AS respuestas,
    (SELECT count(*) FROM integracion.outbox WHERE created_at >= :'desde') AS eventos_outbox,
    (SELECT count(*) FROM integracion.outbox WHERE created_at >= :'desde' AND publicado_en IS NULL) AS outbox_sin_publicar,
    (SELECT count(*) FROM soporte.tickets WHERE estado IN ('nuevo', 'abierto', 'en_espera')) AS tickets_abiertos;

\echo '== Cron de SLA (soporte.revisar_sla) durante la corrida'
SELECT count(*) AS corridas,
       count(*) FILTER (WHERE state = 'completed') AS completas,
       count(*) FILTER (WHERE state NOT IN ('completed', 'running', 'available', 'scheduled')) AS con_error,
       round(avg(extract(epoch FROM finalized_at - attempted_at)) * 1000) AS ms_promedio,
       round(max(extract(epoch FROM finalized_at - attempted_at)) * 1000) AS ms_max,
       (SELECT count(*) FROM soporte.ticket_sla_alertas WHERE created_at >= :'desde') AS alertas_emitidas
FROM river.river_job
WHERE kind = 'soporte.revisar_sla' AND created_at >= :'desde';

-- Falla si hubo duplicados.
SELECT set_config('carga.desde', :'desde', false) \gset
DO $$
DECLARE
    total bigint;
BEGIN
    SELECT sum(repetidos) INTO total FROM carga_duplicados;
    IF total > 0 THEN
        RAISE EXCEPTION 'carga: % efectos duplicados en la base (ver la tabla de arriba)', total;
    END IF;
    -- Sin operaciones aplicadas la prueba no corrió (o no llegó a la base): no es un verde.
    IF NOT EXISTS (SELECT 1 FROM campo.operaciones_sync WHERE created_at >= current_setting('carga.desde')::timestamptz
                   AND estado = 'aplicada') THEN
        RAISE EXCEPTION 'carga: ninguna operación de sync aplicada desde %', current_setting('carga.desde');
    END IF;
END $$;
\echo 'verificar: sin duplicados'

-- Siembra de la prueba de carga, parte SQL (docs/runbooks/carga.md): tickets de la empresa de carga.
--
--   psql -v ON_ERROR_STOP=1 -v empresa=9001 -v abiertos=10000 -v resueltos=50000 \
--        -v soporte='{2,3}' -v zonas='{1000000000000}' -v cuadrillas='{1,2}' -f tickets.sql
--
-- Por SQL y no por la API porque hace falta historia: ~1 año de tickets resueltos y cerrados con
-- sus fechas reales, y abiertos con relojes de SLA ya corridos. Son datos de prueba en una base
-- propia (iris-carga): no pasan por los casos de uso, así que no dejan auditoría ni outbox (el
-- historial no se vuelve a emitir) — sí su evento «creado» y sus mensajes, para que el detalle se
-- lea como en producción.
--
-- Repetible: completa hasta :abiertos tickets abiertos (nuevo, abierto, en_espera) y :resueltos
-- resueltos o cerrados de la empresa; si ya están, no crea nada (y re-arma los relojes de SLA,
-- ver al final). Mezcla:
--   - prioridad: baja 25 %, media 45 %, alta 22 %, crítica 8 %;
--   - categoría: las 9 globales (incidente_red y sin_servicio con el SLA 24/7, el resto con el
--     estándar laboral);
--   - abiertos: nuevo 30 %, abierto 55 %, en_espera 15 %; 25 % sin asignar.
--   - SLA de los abiertos: 85 % con los plazos ya vencidos y alertados (como si el cron hubiera
--     corrido) y 15 % «frescos», con el 80 % o el vencimiento de la respuesta cayendo entre 2 min
--     antes y 20 min después de sembrar: el cron de SLA tiene trabajo en cada minuto de la prueba.
-- Los plazos se calculan en minutos corridos (sin el calendario laboral): para la carga basta con
-- que el cron vea relojes cruzando; el cálculo exacto lo prueban las pruebas de soporte.

\set ON_ERROR_STOP on

BEGIN;

-- Objetivos por categoría y prioridad (SLA de la categoría o el por defecto).
CREATE TEMP TABLE carga_objetivos ON COMMIT DROP AS
SELECT c.id AS categoria_id, s.id AS sla_id, s.calendario_id, o.prioridad, o.respuesta_min, o.resolucion_min,
       row_number() OVER (PARTITION BY o.prioridad ORDER BY c.id) - 1 AS n
FROM soporte.categorias c
JOIN soporte.slas s ON s.id = coalesce(c.sla_id, (SELECT id FROM soporte.slas WHERE por_defecto AND empresa_id IS NULL))
JOIN soporte.sla_objetivos o ON o.sla_id = s.id
WHERE c.empresa_id IS NULL AND c.activa;

SELECT count(DISTINCT categoria_id) AS categorias FROM carga_objetivos \gset

SELECT
    greatest(0, :abiertos - count(*) FILTER (WHERE estado IN ('nuevo', 'abierto', 'en_espera'))) AS faltan_abiertos,
    greatest(0, :resueltos - count(*) FILTER (WHERE estado IN ('resuelto', 'cerrado'))) AS faltan_resueltos
FROM soporte.tickets WHERE empresa_id = :empresa \gset

\echo 'siembra tickets: faltan' :faltan_abiertos 'abiertos y' :faltan_resueltos 'resueltos'

-- Generador común: g = número de fila, abierto = si va entre los abiertos.
CREATE TEMP TABLE carga_nuevos ON COMMIT DROP AS
WITH filas AS (
    SELECT g, true AS abierto FROM generate_series(1, :faltan_abiertos) g
    UNION ALL
    SELECT g, false FROM generate_series(1, :faltan_resueltos) g
), azar AS (
    SELECT g, abierto, random() AS r1, random() AS r2, random() AS r3, random() AS r4, random() AS r5, random() AS r6
    FROM filas
), base AS (
    SELECT a.*,
        CASE WHEN r1 < 0.25 THEN 'baja' WHEN r1 < 0.70 THEN 'media' WHEN r1 < 0.92 THEN 'alta' ELSE 'critica' END AS prioridad,
        floor(r2 * :categorias)::int AS cat_n,
        CASE WHEN NOT abierto THEN (CASE WHEN r3 < 0.85 THEN 'cerrado' ELSE 'resuelto' END)
             WHEN r3 < 0.30 THEN 'nuevo' WHEN r3 < 0.85 THEN 'abierto' ELSE 'en_espera' END AS estado,
        -- abiertos frescos: el reloj de respuesta cruza durante la prueba
        abierto AND r4 < 0.15 AS fresco
    FROM azar a
)
SELECT b.*, o.categoria_id, o.sla_id, o.calendario_id, o.respuesta_min, o.resolucion_min,
    CASE
        -- vence la respuesta entre 2 min antes y 20 min después de ahora
        WHEN b.fresco THEN now() + make_interval(secs => (-120 + r5 * 1320)) - make_interval(mins => o.respuesta_min)
        -- abiertos viejos: de 2 a 30 días
        WHEN b.abierto THEN now() - make_interval(days => 2) - make_interval(secs => r5 * 28 * 86400)
        -- resueltos y cerrados: el último año
        ELSE now() - make_interval(days => 3) - make_interval(secs => r5 * 362 * 86400)
    END AS creado
FROM base b
JOIN carga_objetivos o ON o.prioridad = b.prioridad AND o.n = b.cat_n;

INSERT INTO soporte.tickets (
    empresa_id, cliente_id, servicio_id, zona_id, contacto_nombre, contacto_telefono, contacto_correo,
    categoria_id, prioridad, estado, origen, asunto, asignado_a, cuadrilla_id,
    sla_id, calendario_id, sla_inicio, sla_respuesta_min, sla_resolucion_min,
    riesgo_respuesta, vence_respuesta, riesgo_resolucion, vence_resolucion,
    primera_respuesta_en, en_espera_desde, resuelto_en, cerrado_en, created_at, updated_at)
SELECT
    :empresa,
    CASE WHEN n.r6 < 0.7 THEN 100000 + (n.g::bigint * 7919) % 20000 END,
    CASE WHEN n.r6 < 0.7 THEN 500000 + (n.g::bigint * 7919) % 20000 END,
    (:'zonas'::bigint[])[1 + n.g % cardinality(:'zonas'::bigint[])],
    'Cliente ' || (n.g % 5000), '5' || lpad(((n.g::bigint * 104729) % 10000000)::text, 7, '0'),
    CASE WHEN n.r6 >= 0.7 THEN 'cliente' || (n.g % 5000) || '@correo.carga.local' END,
    n.categoria_id, n.prioridad, n.estado,
    (ARRAY['panel', 'portal', 'correo', 'api', 'argos', 'panel'])[1 + n.g % 6],
    (ARRAY['Sin internet desde la mañana', 'Navegación lenta por las noches', 'Corte intermitente del servicio',
           'Router reinicia solo', 'Cobro duplicado en la factura', 'Cambio de plan a 100 Mbps',
           'Traslado de servicio a otra dirección', 'Antena desalineada tras la lluvia', 'Sin señal en el segundo piso',
           'ONT con luz LOS en rojo'])[1 + n.g % 10] || ' · ' || n.g,
    CASE WHEN n.abierto AND n.r4 >= 0.15 AND n.r2 < 0.25 THEN NULL
         ELSE (:'soporte'::bigint[])[1 + n.g % cardinality(:'soporte'::bigint[])] END,
    CASE WHEN n.g % 4 = 0 THEN (:'cuadrillas'::bigint[])[1 + n.g % cardinality(:'cuadrillas'::bigint[])] END,
    n.sla_id, n.calendario_id, n.creado, n.respuesta_min, n.resolucion_min,
    -- Relojes: null cuando no corren (en espera para la resolución; resueltos y cerrados conservan
    -- sus plazos, que el cron ignora por estado).
    n.creado + make_interval(secs => 0.8 * 60 * n.respuesta_min),
    n.creado + make_interval(mins => n.respuesta_min),
    CASE WHEN n.estado <> 'en_espera' THEN n.creado + make_interval(secs => 0.8 * 60 * n.resolucion_min) END,
    CASE WHEN n.estado <> 'en_espera' THEN n.creado + make_interval(mins => n.resolucion_min) END,
    -- Primera respuesta: todos los resueltos y en espera, el 70 % de los abiertos viejos; ningún
    -- nuevo ni fresco (su reloj de respuesta sigue corriendo).
    CASE WHEN n.estado IN ('resuelto', 'cerrado', 'en_espera')
              OR (n.estado = 'abierto' AND NOT n.fresco AND n.r5 < 0.7)
         THEN n.creado + make_interval(secs => 60 + n.r5 * 0.9 * 60 * n.respuesta_min) END,
    CASE WHEN n.estado = 'en_espera' THEN n.creado + make_interval(hours => 1) END,
    CASE WHEN n.estado IN ('resuelto', 'cerrado') THEN n.creado + make_interval(secs => 600 + n.r5 * 60 * n.resolucion_min) END,
    CASE WHEN n.estado = 'cerrado' THEN n.creado + make_interval(secs => 600 + n.r5 * 60 * n.resolucion_min) + interval '2 days' END,
    n.creado, n.creado
FROM carga_nuevos n
ORDER BY n.creado;

-- Historial y conversación de los recién creados (los que aún no tienen evento «creado»).
CREATE TEMP TABLE carga_ids ON COMMIT DROP AS
SELECT t.id, t.empresa_id, t.estado, t.created_at, t.primera_respuesta_en, t.resuelto_en, t.asignado_a
FROM soporte.tickets t
WHERE t.empresa_id = :empresa
  AND NOT EXISTS (SELECT 1 FROM soporte.ticket_eventos e WHERE e.ticket_id = t.id AND e.tipo = 'creado');

INSERT INTO soporte.ticket_eventos (ticket_id, empresa_id, tipo, a, actor_tipo, actor_nombre, created_at, updated_at)
SELECT id, empresa_id, 'creado', 'nuevo', 'sistema', 'Siembra de carga', created_at, created_at FROM carga_ids;

INSERT INTO soporte.ticket_eventos (ticket_id, empresa_id, tipo, de, a, actor_tipo, actor_id, actor_nombre, created_at, updated_at)
SELECT id, empresa_id, 'estado', 'nuevo', estado, 'usuario', asignado_a, 'Soporte carga', coalesce(resuelto_en, primera_respuesta_en, created_at),
       coalesce(resuelto_en, primera_respuesta_en, created_at)
FROM carga_ids WHERE estado <> 'nuevo';

-- Mensaje del cliente al abrir y la respuesta de soporte cuando la hubo.
INSERT INTO soporte.ticket_mensajes (ticket_id, empresa_id, publico, autor_tipo, autor_nombre, canal, cuerpo, created_at, updated_at)
SELECT id, empresa_id, true, 'cliente', 'Cliente', 'portal',
       'Buenas, desde hoy el servicio falla. Ya reinicié el router y sigue igual. ¿Me pueden ayudar?', created_at, created_at
FROM carga_ids;

INSERT INTO soporte.ticket_mensajes (ticket_id, empresa_id, publico, autor_tipo, autor_id, autor_nombre, canal, cuerpo, created_at, updated_at)
SELECT id, empresa_id, true, 'usuario', asignado_a, 'Soporte carga', 'panel',
       'Gracias por escribirnos. Revisamos su servicio y le confirmamos en cuanto tengamos el diagnóstico.',
       primera_respuesta_en, primera_respuesta_en
FROM carga_ids WHERE primera_respuesta_en IS NOT NULL;

-- Alertas de SLA ya emitidas para lo viejo (como si el cron hubiera corrido): la misma regla de
-- soporte.AlertasSLAPendientes, solo para relojes que cruzaron hace más de 30 min. Lo fresco queda
-- para el cron durante la prueba.
INSERT INTO soporte.ticket_sla_alertas (ticket_id, sla, umbral, vence_en, created_at, updated_at)
SELECT t.id, r.sla, r.umbral, r.vence_en, now(), now()
FROM soporte.tickets t
CROSS JOIN LATERAL (VALUES
    ('respuesta', 'vencido', t.vence_respuesta,
     t.primera_respuesta_en IS NULL AND t.vence_respuesta <= now() - interval '30 minutes'),
    ('resolucion', 'vencido', t.vence_resolucion,
     t.estado IN ('nuevo', 'abierto') AND t.vence_resolucion <= now() - interval '30 minutes'),
    ('resolucion', 'en_riesgo', t.vence_resolucion,
     t.estado IN ('nuevo', 'abierto') AND t.riesgo_resolucion <= now() - interval '30 minutes' AND t.vence_resolucion > now())
) AS r (sla, umbral, vence_en, aplica)
WHERE t.empresa_id = :empresa AND t.estado IN ('nuevo', 'abierto', 'en_espera') AND r.aplica
ON CONFLICT DO NOTHING;

-- Re-armar el cron en cada corrida: lo «fresco» de una siembra anterior ya se alertó. Se mueve el
-- plazo de respuesta de una muestra de abiertos sin primera respuesta (un plazo nuevo vuelve a
-- alertar: la alerta es única por vencimiento) para que vuelva a cruzar durante la prueba. Así
-- cada corrida tiene el mismo trabajo de SLA, sea la primera o la décima.
WITH muestra AS (
    SELECT id FROM soporte.tickets
    WHERE empresa_id = :empresa AND estado IN ('nuevo', 'abierto') AND primera_respuesta_en IS NULL
      AND vence_respuesta < now() - interval '30 minutes'
    ORDER BY random()
    LIMIT greatest(0, round(:abiertos * 0.15)::int - (
        SELECT count(*) FROM soporte.tickets
        WHERE empresa_id = :empresa AND estado IN ('nuevo', 'abierto', 'en_espera') AND primera_respuesta_en IS NULL
          AND vence_respuesta > now() - interval '2 minutes'))
), plazo AS (
    SELECT m.id, now() + make_interval(secs => (-120 + random() * 1320)) AS vence FROM muestra m
)
UPDATE soporte.tickets t
SET vence_respuesta = p.vence,
    riesgo_respuesta = p.vence - make_interval(secs => 0.2 * 60 * t.sla_respuesta_min)
FROM plazo p
WHERE t.id = p.id;

COMMIT;

ANALYZE soporte.tickets;
ANALYZE soporte.ticket_mensajes;
ANALYZE soporte.ticket_eventos;
ANALYZE soporte.ticket_sla_alertas;

SELECT estado, count(*) FROM soporte.tickets WHERE empresa_id = :empresa GROUP BY estado ORDER BY estado;

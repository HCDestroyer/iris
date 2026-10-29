-- Reportes (solo lectura). Filtros comunes:
--   - empresa: @todas_empresas (actor sistema) o la empresa activa (null = sin conector: solo
--     las filas con empresa_id null; nunca «sin filtro»);
--   - alcance de reportes.ver: global o las zonas / cuadrillas del rol (una fila sin zona ni
--     cuadrilla no la ve un alcance limitado: falla cerrado);
--   - opcionales: zona, cuadrilla, técnico (usuario: asignado del ticket o técnico de la orden),
--     categoría y prioridades (tickets).
-- Los tickets fusionados en otro no cuentan nunca.

-- name: ReporteSLA :many
-- Tickets resueltos en [inicio, fin): primera respuesta y resolución dentro del vencimiento.
-- Un reloj sin SLA (vence null) no cuenta en su porcentaje. Total, por prioridad y por categoría.
SELECT GROUPING(t.prioridad)::int AS por_prioridad_g, GROUPING(t.categoria_id)::int AS por_categoria_g,
       coalesce(t.prioridad, '')::text AS prioridad, coalesce(t.categoria_id, 0)::bigint AS categoria_id,
       coalesce(c.nombre, '')::text AS categoria,
       count(*)::bigint AS resueltos,
       count(*) FILTER (WHERE t.vence_respuesta IS NOT NULL)::bigint AS con_sla_respuesta,
       count(*) FILTER (WHERE t.vence_respuesta IS NOT NULL AND t.primera_respuesta_en IS NOT NULL
                        AND t.primera_respuesta_en <= t.vence_respuesta)::bigint AS respuesta_en_tiempo,
       count(*) FILTER (WHERE t.vence_resolucion IS NOT NULL)::bigint AS con_sla_resolucion,
       count(*) FILTER (WHERE t.vence_resolucion IS NOT NULL AND t.resuelto_en <= t.vence_resolucion)::bigint AS resolucion_en_tiempo
FROM soporte.tickets t
LEFT JOIN soporte.categorias c ON c.id = t.categoria_id
WHERE t.resuelto_en IS NOT NULL AND t.fusionado_en_id IS NULL
  AND t.resuelto_en >= @inicio AND t.resuelto_en < @fin
  AND (@todas_empresas::boolean
       OR (sqlc.narg(empresa_id)::bigint IS NULL AND t.empresa_id IS NULL)
       OR t.empresa_id = sqlc.narg(empresa_id))
  AND (@alcance_global::boolean OR t.zona_id = ANY(@zonas::bigint[]) OR t.cuadrilla_id = ANY(@cuadrillas::bigint[]))
  AND (sqlc.narg(zona_id)::bigint IS NULL OR t.zona_id = sqlc.narg(zona_id))
  AND (sqlc.narg(cuadrilla_id)::bigint IS NULL OR t.cuadrilla_id = sqlc.narg(cuadrilla_id))
  AND (sqlc.narg(tecnico_id)::bigint IS NULL OR t.asignado_a = sqlc.narg(tecnico_id))
  AND (sqlc.narg(categoria_id)::bigint IS NULL OR t.categoria_id = sqlc.narg(categoria_id))
  AND (cardinality(@prioridades::text[]) = 0 OR t.prioridad = ANY(@prioridades::text[]))
GROUP BY GROUPING SETS ((), (t.prioridad), (t.categoria_id, c.nombre))
ORDER BY 1, 2, 3, 4;

-- name: ReporteResolucion :many
-- Tickets resueltos en [inicio, fin): promedio y mediana de minutos de reloj desde la creación
-- hasta la resolución. Total, por prioridad y por categoría.
SELECT GROUPING(t.prioridad)::int AS por_prioridad_g, GROUPING(t.categoria_id)::int AS por_categoria_g,
       coalesce(t.prioridad, '')::text AS prioridad, coalesce(t.categoria_id, 0)::bigint AS categoria_id,
       coalesce(c.nombre, '')::text AS categoria,
       count(*)::bigint AS resueltos,
       coalesce(avg(extract(epoch FROM t.resuelto_en - t.created_at) / 60), 0)::float8 AS promedio_min,
       coalesce(percentile_cont(0.5) WITHIN GROUP (ORDER BY extract(epoch FROM t.resuelto_en - t.created_at) / 60), 0)::float8 AS mediana_min
FROM soporte.tickets t
LEFT JOIN soporte.categorias c ON c.id = t.categoria_id
WHERE t.resuelto_en IS NOT NULL AND t.fusionado_en_id IS NULL
  AND t.resuelto_en >= @inicio AND t.resuelto_en < @fin
  AND (@todas_empresas::boolean
       OR (sqlc.narg(empresa_id)::bigint IS NULL AND t.empresa_id IS NULL)
       OR t.empresa_id = sqlc.narg(empresa_id))
  AND (@alcance_global::boolean OR t.zona_id = ANY(@zonas::bigint[]) OR t.cuadrilla_id = ANY(@cuadrillas::bigint[]))
  AND (sqlc.narg(zona_id)::bigint IS NULL OR t.zona_id = sqlc.narg(zona_id))
  AND (sqlc.narg(cuadrilla_id)::bigint IS NULL OR t.cuadrilla_id = sqlc.narg(cuadrilla_id))
  AND (sqlc.narg(tecnico_id)::bigint IS NULL OR t.asignado_a = sqlc.narg(tecnico_id))
  AND (sqlc.narg(categoria_id)::bigint IS NULL OR t.categoria_id = sqlc.narg(categoria_id))
  AND (cardinality(@prioridades::text[]) = 0 OR t.prioridad = ANY(@prioridades::text[]))
GROUP BY GROUPING SETS ((), (t.prioridad), (t.categoria_id, c.nombre))
ORDER BY 1, 2, 3, 4;

-- name: ReporteTecnicos :many
-- Órdenes finalizadas (cerradas o fallidas) en [inicio, fin) por técnico. Tiempo en sitio:
-- desde la llegada hasta el cierre, menos las pausas tomadas en sitio; tiempo pausado: el
-- acumulado de la orden (pausada_seg).
SELECT o.tecnico_id::bigint AS tecnico_id, coalesce(u.nombre, '')::text AS nombre,
       count(*)::bigint AS finalizadas,
       count(*) FILTER (WHERE o.estado = 'cerrada' AND o.resultado = 'exitosa')::bigint AS exitosas,
       count(*) FILTER (WHERE o.estado = 'cerrada' AND o.resultado = 'parcial')::bigint AS parciales,
       count(*) FILTER (WHERE o.estado = 'fallida')::bigint AS fallidas,
       count(*) FILTER (WHERE o.en_sitio_en IS NOT NULL AND o.finalizada_en >= o.en_sitio_en)::bigint AS con_sitio,
       coalesce(sum(greatest(extract(epoch FROM o.finalizada_en - o.en_sitio_en) - coalesce(p.pausado_seg, 0), 0))
                FILTER (WHERE o.en_sitio_en IS NOT NULL AND o.finalizada_en >= o.en_sitio_en), 0)::float8 AS sitio_seg,
       coalesce(sum(o.pausada_seg), 0)::bigint AS pausado_seg,
       count(*) FILTER (WHERE o.pausada_seg > 0)::bigint AS con_pausa
FROM campo.ordenes o
LEFT JOIN nucleo.usuarios u ON u.id = o.tecnico_id
LEFT JOIN LATERAL (
    SELECT sum(extract(epoch FROM pa.fin - pa.inicio)) AS pausado_seg
    FROM campo.orden_pausas pa
    WHERE pa.orden_id = o.id AND pa.desde_estado = 'en_sitio' AND pa.fin IS NOT NULL
      AND pa.inicio >= o.en_sitio_en AND pa.fin <= o.finalizada_en
) p ON true
WHERE o.finalizada_en IS NOT NULL AND o.finalizada_en >= @inicio AND o.finalizada_en < @fin
  AND o.estado IN ('cerrada', 'fallida') AND o.tecnico_id IS NOT NULL
  AND (@todas_empresas::boolean
       OR (sqlc.narg(empresa_id)::bigint IS NULL AND o.empresa_id IS NULL)
       OR o.empresa_id = sqlc.narg(empresa_id))
  AND (@alcance_global::boolean OR o.zona_id = ANY(@zonas::bigint[]) OR o.cuadrilla_id = ANY(@cuadrillas::bigint[]))
  AND (sqlc.narg(zona_id)::bigint IS NULL OR o.zona_id = sqlc.narg(zona_id))
  AND (sqlc.narg(cuadrilla_id)::bigint IS NULL OR o.cuadrilla_id = sqlc.narg(cuadrilla_id))
  AND (sqlc.narg(tecnico_id)::bigint IS NULL OR o.tecnico_id = sqlc.narg(tecnico_id))
GROUP BY o.tecnico_id, u.nombre
ORDER BY finalizadas DESC, o.tecnico_id;

-- name: ReportePrimeraVisita :many
-- Órdenes de instalación o visita cerradas con éxito en [inicio, fin), clasificadas:
--   - sin_servicio: sin servicio de Argos (no se puede evaluar);
--   - con_retorno: el mismo servicio tuvo otra orden (no cancelada) o abrió un ticket (no
--     fusionado) en los días siguientes al cierre;
--   - en_observacion: todavía no pasan los días (no se cuenta como resuelta ni como retorno);
--   - resuelta: lo demás.
-- Total, por tipo y por técnico.
WITH par AS (
    SELECT make_interval(0, 0, 0, @dias::int) AS ventana, @ahora::timestamptz AS ahora
), base AS (
    SELECT o.id, o.empresa_id, o.tecnico_id, tp.codigo AS tipo, o.servicio_id, o.finalizada_en,
           o.finalizada_en + par.ventana AS limite, o.finalizada_en + par.ventana > par.ahora AS abierta
    FROM campo.ordenes o
    CROSS JOIN par
    JOIN campo.tipos_orden tp ON tp.id = o.tipo_id
    WHERE o.finalizada_en IS NOT NULL AND o.finalizada_en >= @inicio AND o.finalizada_en < @fin
      AND o.estado = 'cerrada' AND o.resultado = 'exitosa' AND tp.codigo IN ('instalacion', 'visita')
      AND (@todas_empresas::boolean
           OR (sqlc.narg(empresa_id)::bigint IS NULL AND o.empresa_id IS NULL)
           OR o.empresa_id = sqlc.narg(empresa_id))
      AND (@alcance_global::boolean OR o.zona_id = ANY(@zonas::bigint[]) OR o.cuadrilla_id = ANY(@cuadrillas::bigint[]))
      AND (sqlc.narg(zona_id)::bigint IS NULL OR o.zona_id = sqlc.narg(zona_id))
      AND (sqlc.narg(cuadrilla_id)::bigint IS NULL OR o.cuadrilla_id = sqlc.narg(cuadrilla_id))
      AND (sqlc.narg(tecnico_id)::bigint IS NULL OR o.tecnico_id = sqlc.narg(tecnico_id))
), clasificadas AS (
    SELECT b.tipo, b.tecnico_id,
           CASE
               WHEN b.servicio_id IS NULL THEN 'sin_servicio'
               WHEN EXISTS (SELECT 1 FROM campo.ordenes o2
                            WHERE o2.servicio_id = b.servicio_id AND o2.id <> b.id AND o2.estado <> 'cancelada'
                              AND o2.empresa_id IS NOT DISTINCT FROM b.empresa_id
                              AND o2.created_at > b.finalizada_en AND o2.created_at <= b.limite)
                 OR EXISTS (SELECT 1 FROM soporte.tickets t2
                            WHERE t2.servicio_id = b.servicio_id AND t2.fusionado_en_id IS NULL
                              AND t2.empresa_id IS NOT DISTINCT FROM b.empresa_id
                              AND t2.created_at > b.finalizada_en AND t2.created_at <= b.limite)
                   THEN 'con_retorno'
               WHEN b.abierta THEN 'en_observacion'
               ELSE 'resuelta'
           END AS clase
    FROM base b
)
SELECT GROUPING(cl.tipo)::int AS por_tipo_g, GROUPING(cl.tecnico_id)::int AS por_tecnico_g,
       coalesce(cl.tipo, '')::text AS tipo, coalesce(cl.tecnico_id, 0)::bigint AS tecnico_id,
       coalesce(u.nombre, '')::text AS tecnico,
       count(*)::bigint AS cerradas,
       count(*) FILTER (WHERE cl.clase = 'resuelta')::bigint AS resueltas,
       count(*) FILTER (WHERE cl.clase = 'con_retorno')::bigint AS con_retorno,
       count(*) FILTER (WHERE cl.clase = 'en_observacion')::bigint AS en_observacion,
       count(*) FILTER (WHERE cl.clase = 'sin_servicio')::bigint AS sin_servicio
FROM clasificadas cl
LEFT JOIN nucleo.usuarios u ON u.id = cl.tecnico_id
GROUP BY GROUPING SETS ((), (cl.tipo), (cl.tecnico_id, u.nombre))
ORDER BY 1, 2, 3, 4;

-- name: ResumenReincidencias :one
-- Tickets resueltos en [inicio, fin) y cuántos reincidieron: el mismo servicio (o, sin
-- servicio, el mismo cliente) abrió otro ticket (no fusionado, de la misma empresa) dentro de
-- los días siguientes a la resolución. Sin servicio ni cliente no se puede evaluar.
WITH par AS (SELECT make_interval(0, 0, 0, @dias::int) AS ventana)
SELECT count(*)::bigint AS resueltos,
       count(*) FILTER (WHERE t.servicio_id IS NOT NULL OR t.cliente_id IS NOT NULL)::bigint AS evaluables,
       count(*) FILTER (WHERE EXISTS (
           SELECT 1 FROM soporte.tickets t2
           WHERE t2.id <> t.id AND t2.fusionado_en_id IS NULL
             AND t2.empresa_id IS NOT DISTINCT FROM t.empresa_id
             AND ((t.servicio_id IS NOT NULL AND t2.servicio_id = t.servicio_id)
                  OR (t.servicio_id IS NULL AND t.cliente_id IS NOT NULL AND t2.cliente_id = t.cliente_id))
             AND t2.created_at > t.resuelto_en
             AND t2.created_at <= t.resuelto_en + par.ventana))::bigint AS reincidentes
FROM soporte.tickets t
CROSS JOIN par
WHERE t.resuelto_en IS NOT NULL AND t.fusionado_en_id IS NULL
  AND t.resuelto_en >= @inicio AND t.resuelto_en < @fin
  AND (@todas_empresas::boolean
       OR (sqlc.narg(empresa_id)::bigint IS NULL AND t.empresa_id IS NULL)
       OR t.empresa_id = sqlc.narg(empresa_id))
  AND (@alcance_global::boolean OR t.zona_id = ANY(@zonas::bigint[]) OR t.cuadrilla_id = ANY(@cuadrillas::bigint[]))
  AND (sqlc.narg(zona_id)::bigint IS NULL OR t.zona_id = sqlc.narg(zona_id))
  AND (sqlc.narg(cuadrilla_id)::bigint IS NULL OR t.cuadrilla_id = sqlc.narg(cuadrilla_id))
  AND (sqlc.narg(tecnico_id)::bigint IS NULL OR t.asignado_a = sqlc.narg(tecnico_id))
  AND (sqlc.narg(categoria_id)::bigint IS NULL OR t.categoria_id = sqlc.narg(categoria_id))
  AND (cardinality(@prioridades::text[]) = 0 OR t.prioridad = ANY(@prioridades::text[]));

-- name: ListarReincidencias :many
-- Los reincidentes (ver ResumenReincidencias), más recientes primero (cursor: resuelto_en, id),
-- con el primer ticket que volvió a abrir y cuántos abrió en la ventana.
WITH par AS (SELECT make_interval(0, 0, 0, @dias::int) AS ventana)
SELECT t.id, t.numero, t.asunto, t.prioridad, t.categoria_id, coalesce(c.nombre, '')::text AS categoria,
       t.cliente_id, t.servicio_id, t.resuelto_en::timestamptz AS resuelto_en,
       s.id AS siguiente_id, s.numero AS siguiente_numero, s.created_at AS siguiente_creado_en,
       s.nuevos::bigint AS nuevos
FROM soporte.tickets t
CROSS JOIN par
LEFT JOIN soporte.categorias c ON c.id = t.categoria_id
JOIN LATERAL (
    SELECT t2.id, t2.numero, t2.created_at, count(*) OVER () AS nuevos
    FROM soporte.tickets t2
    WHERE t2.id <> t.id AND t2.fusionado_en_id IS NULL
      AND t2.empresa_id IS NOT DISTINCT FROM t.empresa_id
      AND ((t.servicio_id IS NOT NULL AND t2.servicio_id = t.servicio_id)
           OR (t.servicio_id IS NULL AND t.cliente_id IS NOT NULL AND t2.cliente_id = t.cliente_id))
      AND t2.created_at > t.resuelto_en
      AND t2.created_at <= t.resuelto_en + par.ventana
    ORDER BY t2.created_at, t2.id
    LIMIT 1
) s ON true
WHERE t.resuelto_en IS NOT NULL AND t.fusionado_en_id IS NULL
  AND t.resuelto_en >= @inicio AND t.resuelto_en < @fin
  AND (sqlc.narg(antes_en)::timestamptz IS NULL OR (t.resuelto_en, t.id) < (sqlc.narg(antes_en)::timestamptz, sqlc.narg(antes_id)::bigint))
  AND (@todas_empresas::boolean
       OR (sqlc.narg(empresa_id)::bigint IS NULL AND t.empresa_id IS NULL)
       OR t.empresa_id = sqlc.narg(empresa_id))
  AND (@alcance_global::boolean OR t.zona_id = ANY(@zonas::bigint[]) OR t.cuadrilla_id = ANY(@cuadrillas::bigint[]))
  AND (sqlc.narg(zona_id)::bigint IS NULL OR t.zona_id = sqlc.narg(zona_id))
  AND (sqlc.narg(cuadrilla_id)::bigint IS NULL OR t.cuadrilla_id = sqlc.narg(cuadrilla_id))
  AND (sqlc.narg(tecnico_id)::bigint IS NULL OR t.asignado_a = sqlc.narg(tecnico_id))
  AND (sqlc.narg(categoria_id)::bigint IS NULL OR t.categoria_id = sqlc.narg(categoria_id))
  AND (cardinality(@prioridades::text[]) = 0 OR t.prioridad = ANY(@prioridades::text[]))
ORDER BY t.resuelto_en DESC, t.id DESC
LIMIT @limite;

-- name: ReporteSatisfaccion :many
-- Encuestas creadas (enviadas) en [inicio, fin): respondidas, pendientes, vencidas sin
-- responder, promedio y distribución de la calificación. Total y por entidad (ticket, orden).
-- zona, cuadrilla, usuario y categoría son los copiados al crear la encuesta.
SELECT GROUPING(e.entidad)::int AS por_entidad_g, coalesce(e.entidad, '')::text AS entidad,
       count(*)::bigint AS enviadas,
       count(*) FILTER (WHERE e.estado = 'respondida')::bigint AS respondidas,
       count(*) FILTER (WHERE e.estado = 'pendiente' AND e.expira_en > @ahora)::bigint AS pendientes,
       count(*) FILTER (WHERE e.estado = 'pendiente' AND e.expira_en <= @ahora)::bigint AS vencidas,
       coalesce(avg(e.calificacion), 0)::float8 AS promedio,
       count(*) FILTER (WHERE e.calificacion = 1)::bigint AS c1,
       count(*) FILTER (WHERE e.calificacion = 2)::bigint AS c2,
       count(*) FILTER (WHERE e.calificacion = 3)::bigint AS c3,
       count(*) FILTER (WHERE e.calificacion = 4)::bigint AS c4,
       count(*) FILTER (WHERE e.calificacion = 5)::bigint AS c5
FROM soporte.encuestas e
WHERE e.created_at >= @inicio AND e.created_at < @fin
  AND (@todas_empresas::boolean
       OR (sqlc.narg(empresa_id)::bigint IS NULL AND e.empresa_id IS NULL)
       OR e.empresa_id = sqlc.narg(empresa_id))
  AND (@alcance_global::boolean OR e.zona_id = ANY(@zonas::bigint[]) OR e.cuadrilla_id = ANY(@cuadrillas::bigint[]))
  AND (sqlc.narg(zona_id)::bigint IS NULL OR e.zona_id = sqlc.narg(zona_id))
  AND (sqlc.narg(cuadrilla_id)::bigint IS NULL OR e.cuadrilla_id = sqlc.narg(cuadrilla_id))
  AND (sqlc.narg(tecnico_id)::bigint IS NULL OR e.usuario_id = sqlc.narg(tecnico_id))
  AND (sqlc.narg(categoria_id)::bigint IS NULL OR e.categoria_id = sqlc.narg(categoria_id))
  AND (sqlc.narg(entidad)::text IS NULL OR e.entidad = sqlc.narg(entidad))
GROUP BY GROUPING SETS ((), (e.entidad))
ORDER BY 1, 2;

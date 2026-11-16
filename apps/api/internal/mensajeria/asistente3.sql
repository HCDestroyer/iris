-- Asistente de IA, fase 3a (contratos/conversaciones.md §17): modo autónomo, perfiles, categorías,
-- clasificación y métricas de efecto.

-- name: FijarAutonomoAsistente :one
UPDATE mensajeria.asistente_ajustes
SET modo_autonomo = @modo_autonomo, espera_min = @espera_min, max_turnos = @max_turnos, perfiles = @perfiles, por_cola = @por_cola,
    activado_en = coalesce(activado_en, sqlc.narg('activado_en')), updated_at = now()
WHERE id = @id
RETURNING *;

-- Categorías ----------------------------------------------------------------------------------------

-- name: CategoriasEmpresa :many
SELECT * FROM mensajeria.asistente_categorias
WHERE coalesce(empresa_id, 0) = @empresa::bigint AND (activa OR @incluir_inactivas::boolean)
ORDER BY es_otro, orden, id;

-- name: ContarCategorias :one
SELECT count(*)::bigint AS total, count(*) FILTER (WHERE activa)::bigint AS activas, coalesce(max(orden), 0)::integer AS orden
FROM mensajeria.asistente_categorias WHERE coalesce(empresa_id, 0) = @empresa::bigint;

-- name: CategoriaPorID :one
SELECT * FROM mensajeria.asistente_categorias WHERE id = @id AND coalesce(empresa_id, 0) = @empresa::bigint;

-- name: CategoriaParaActualizar :one
SELECT * FROM mensajeria.asistente_categorias WHERE id = @id AND coalesce(empresa_id, 0) = @empresa::bigint FOR UPDATE;

-- name: InsertarCategoria :one
INSERT INTO mensajeria.asistente_categorias (empresa_id, nombre, descripcion, tono, cola_id, sla_primera_respuesta_min, modo, condicion, manual_ids, es_otro, orden)
VALUES (sqlc.narg('empresa_id'), @nombre, @descripcion, @tono, sqlc.narg('cola_id'), sqlc.narg('sla_primera_respuesta_min'), @modo, sqlc.narg('condicion'),
    @manual_ids, @es_otro, @orden)
ON CONFLICT DO NOTHING
RETURNING *;

-- name: ActualizarCategoria :one
UPDATE mensajeria.asistente_categorias
SET nombre = @nombre, descripcion = @descripcion, tono = @tono, cola_id = sqlc.narg('cola_id'),
    sla_primera_respuesta_min = sqlc.narg('sla_primera_respuesta_min'), modo = @modo, condicion = sqlc.narg('condicion'),
    manual_ids = @manual_ids, activa = @activa, version = version + 1, updated_at = now()
WHERE id = @id
RETURNING *;

-- name: ManualesActivosPorIDs :many
SELECT id, titulo, resumen, cuerpo, visibilidad FROM mensajeria.manuales
WHERE id = ANY (@ids::bigint[]) AND coalesce(empresa_id, 0) = @empresa::bigint AND activo
ORDER BY id;

-- Clasificación -------------------------------------------------------------------------------------

-- name: FijarCategoriaConversacion :one
UPDATE mensajeria.conversaciones
SET categoria_id = sqlc.narg('categoria_id'), categoria_confianza = sqlc.narg('confianza'), categoria_origen = sqlc.narg('origen'),
    categoria_en = @ahora, clasificado_mensaje_id = coalesce(sqlc.narg('mensaje_id'), clasificado_mensaje_id),
    version = version + @sube_version::integer, updated_at = now()
WHERE id = @id
RETURNING *;

-- name: FijarActividadAsistente :exec
UPDATE mensajeria.conversaciones SET asistente_actividad_tipo = @tipo, asistente_actividad_texto = @texto, asistente_actividad_en = @ahora
WHERE id = @id;

-- name: InsertarClasificacion :exec
INSERT INTO mensajeria.asistente_clasificaciones (id, empresa_id, conversacion_id, creada_en)
VALUES (@id, sqlc.narg('empresa_id'), @conversacion_id, @creada_en);

-- name: ClasificacionesRecientes :one
SELECT count(*)::bigint FROM mensajeria.asistente_clasificaciones WHERE conversacion_id = @conversacion_id AND creada_en >= @desde;

-- name: ClasificacionesRecientesEmpresa :one
SELECT count(*)::bigint FROM mensajeria.asistente_clasificaciones WHERE coalesce(empresa_id, 0) = @empresa::bigint AND creada_en >= @desde;

-- name: MarcarColaAsistente :exec
-- La cola actual la puso el asistente (por la categoría): una vez por periodo abierto.
UPDATE mensajeria.conversaciones SET cola_origen = 'asistente', asistente_transfirio_en = @ahora WHERE id = @id;

-- name: PurgarClasificaciones :exec
DELETE FROM mensajeria.asistente_clasificaciones WHERE creada_en < @antes;

-- Efecto ---------------------------------------------------------------------------------------------

-- name: EfectoAsistente :many
-- Conversaciones de la empresa creadas en el periodo que el asistente clasificó, por categoría:
-- cuántas, cuántas se resolvieron sin una persona y el promedio de la primera respuesta (de una
-- persona o del asistente; no cuentan el bot ni fuera de horario).
SELECT k.id AS categoria_id, k.nombre, k.tono, count(*)::bigint AS total,
    count(*) FILTER (WHERE c.estado = 'resuelta' AND c.asignado_a IS NULL AND NOT EXISTS (
        SELECT 1 FROM mensajeria.conversacion_mensajes p WHERE p.conversacion_id = c.id AND p.direccion = 'saliente' AND p.autor_id IS NOT NULL))::bigint AS sin_persona,
    count(r.seg)::bigint AS con_respuesta, coalesce(sum(r.seg), 0)::double precision AS suma_seg
FROM mensajeria.conversaciones c
JOIN mensajeria.asistente_categorias k ON k.id = c.categoria_id AND coalesce(k.empresa_id, 0) = coalesce(c.empresa_id, 0)
LEFT JOIN LATERAL (
    SELECT extract(epoch FROM (s.creado_en - e.creado_en))::double precision AS seg
    FROM (SELECT min(m.creado_en) AS creado_en FROM mensajeria.conversacion_mensajes m WHERE m.conversacion_id = c.id AND m.direccion = 'entrante') e,
    LATERAL (SELECT min(m.creado_en) AS creado_en FROM mensajeria.conversacion_mensajes m
             WHERE m.conversacion_id = c.id AND m.direccion = 'saliente' AND m.creado_en >= e.creado_en
               AND (m.autor_id IS NOT NULL OR m.automatico = 'asistente')) s
    WHERE e.creado_en IS NOT NULL AND s.creado_en IS NOT NULL
) r ON true
WHERE coalesce(c.empresa_id, 0) = @empresa::bigint AND c.created_at >= @desde AND c.created_at < @hasta
GROUP BY k.id, k.nombre, k.tono, k.es_otro, k.orden
ORDER BY k.es_otro, k.orden, k.id;

-- name: PrimeraRespuestaPromedio :one
-- Promedio de la primera respuesta de todas las conversaciones de la empresa creadas en el periodo.
SELECT count(r.seg)::bigint AS con_respuesta, coalesce(sum(r.seg), 0)::double precision AS suma_seg
FROM mensajeria.conversaciones c
JOIN LATERAL (
    SELECT extract(epoch FROM (s.creado_en - e.creado_en))::double precision AS seg
    FROM (SELECT min(m.creado_en) AS creado_en FROM mensajeria.conversacion_mensajes m WHERE m.conversacion_id = c.id AND m.direccion = 'entrante') e,
    LATERAL (SELECT min(m.creado_en) AS creado_en FROM mensajeria.conversacion_mensajes m
             WHERE m.conversacion_id = c.id AND m.direccion = 'saliente' AND m.creado_en >= e.creado_en
               AND (m.autor_id IS NOT NULL OR m.automatico = 'asistente')) s
    WHERE e.creado_en IS NOT NULL AND s.creado_en IS NOT NULL
) r ON true
WHERE coalesce(c.empresa_id, 0) = @empresa::bigint AND c.created_at >= @desde AND c.created_at < @hasta;

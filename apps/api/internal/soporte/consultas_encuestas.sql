-- Encuestas de satisfacción (encuestas.go).

-- name: CrearEncuesta :one
-- 0 filas (pgx.ErrNoRows) = el ticket o la orden ya tenía su encuesta (una por cada uno).
INSERT INTO soporte.encuestas (
    empresa_id, entidad, ticket_id, orden_id, numero, zona_id, cuadrilla_id, usuario_id, categoria_id,
    token_hash, semilla, canal, expira_en
) VALUES (
    sqlc.narg(empresa_id), @entidad, sqlc.narg(ticket_id), sqlc.narg(orden_id), @numero, sqlc.narg(zona_id),
    sqlc.narg(cuadrilla_id), sqlc.narg(usuario_id), sqlc.narg(categoria_id), @token_hash, sqlc.narg(semilla),
    @canal, @expira_en
)
ON CONFLICT DO NOTHING
RETURNING *;

-- name: EncuestaPorID :one
SELECT * FROM soporte.encuestas WHERE id = @id;

-- name: EncuestaPublica :one
-- Lo que ve el cliente por el enlace: la encuesta y el nombre de su empresa.
SELECT sqlc.embed(e), emp.nombre AS empresa_nombre
FROM soporte.encuestas e
LEFT JOIN nucleo.empresas emp ON emp.id = e.empresa_id
WHERE e.token_hash = @token_hash;

-- name: EncuestaParaResponder :one
SELECT * FROM soporte.encuestas WHERE token_hash = @token_hash FOR UPDATE;

-- name: ResponderEncuesta :one
UPDATE soporte.encuestas SET estado = 'respondida', calificacion = @calificacion, comentario = sqlc.narg(comentario),
    respondida_en = @ahora, version = version + 1, updated_at = now()
WHERE id = @id AND estado = 'pendiente'
RETURNING *;

-- name: ListarEncuestas :many
-- Más recientes primero (cursor: id). Empresa activa y alcance de encuestas.ver (zona o
-- cuadrilla copiadas al crear).
SELECT * FROM soporte.encuestas
WHERE (sqlc.narg(antes_de)::bigint IS NULL OR id < sqlc.narg(antes_de))
  AND (@todas_empresas::boolean
       OR (sqlc.narg(empresa_id)::bigint IS NULL AND empresa_id IS NULL)
       OR empresa_id = sqlc.narg(empresa_id))
  AND (@alcance_global::boolean OR zona_id = ANY(@zonas::bigint[]) OR cuadrilla_id = ANY(@cuadrillas::bigint[]))
  AND (sqlc.narg(desde)::timestamptz IS NULL OR created_at >= sqlc.narg(desde))
  AND (sqlc.narg(hasta)::timestamptz IS NULL OR created_at < sqlc.narg(hasta))
  AND (sqlc.narg(entidad)::text IS NULL OR entidad = sqlc.narg(entidad))
  AND (sqlc.narg(ticket_id)::bigint IS NULL OR ticket_id = sqlc.narg(ticket_id))
  AND (sqlc.narg(orden_id)::bigint IS NULL OR orden_id = sqlc.narg(orden_id))
  AND (cardinality(@calificaciones::int[]) = 0 OR calificacion = ANY(@calificaciones::int[]))
  AND (sqlc.narg(estado)::text IS NULL
       OR (sqlc.narg(estado) = 'respondida' AND estado = 'respondida')
       OR (sqlc.narg(estado) = 'pendiente' AND estado = 'pendiente' AND expira_en > @ahora)
       OR (sqlc.narg(estado) = 'vencida' AND estado = 'pendiente' AND expira_en <= @ahora))
ORDER BY id DESC
LIMIT @limite;

-- name: BuzonParaEncuesta :one
-- Un buzón activo con SMTP de la empresa del ticket: el preferido (del último correo del
-- contacto) si cumple, si no el más antiguo.
SELECT * FROM soporte.buzones
WHERE activo AND smtp_host IS NOT NULL AND empresa_id IS NOT DISTINCT FROM sqlc.narg(empresa_id)
ORDER BY (id = sqlc.narg(preferido)::bigint) DESC NULLS LAST, id
LIMIT 1;

-- name: UltimoCorreoEntranteDeTicket :one
-- El último correo del contacto que entró al ticket (para seguir el hilo), si hay.
SELECT * FROM soporte.correos_entrantes
WHERE ticket_id = @ticket_id AND resultado IN ('ticket_creado', 'respuesta') AND lower(remitente) = lower(@contacto::text)
ORDER BY id DESC LIMIT 1;

-- name: CrearCorreoEncuesta :one
-- 0 filas = la encuesta ya tenía su correo.
INSERT INTO soporte.correos_salientes (
    buzon_id, empresa_id, ticket_id, encuesta_id, message_id, in_reply_to, destinatario, asunto
) VALUES (
    @buzon_id, sqlc.narg(empresa_id), @ticket_id, @encuesta_id, @message_id, sqlc.narg(in_reply_to), @destinatario, @asunto
)
ON CONFLICT (encuesta_id) DO NOTHING
RETURNING *;

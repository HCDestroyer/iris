-- Adjuntos (internal/nucleo/adjuntos) ------------------------------------------------------

-- name: InsertarAdjunto :one
-- Idempotente por id: si ya existe no inserta (el caso de uso compara y responde el existente).
INSERT INTO nucleo.adjuntos (
    id, empresa_id, entidad, entidad_id, tipo, clave_checklist, mime, tamano, sha256,
    lat, lon, tomado_en, subido_por, almacenamiento, clave
) VALUES (
    @id, sqlc.narg(empresa_id), @entidad, @entidad_id, @tipo, sqlc.narg(clave_checklist), @mime, @tamano, @sha256,
    sqlc.narg(lat), sqlc.narg(lon), sqlc.narg(tomado_en), sqlc.narg(subido_por), @almacenamiento, @clave
)
ON CONFLICT (id) DO NOTHING
RETURNING *;

-- name: AdjuntoPorID :one
SELECT * FROM nucleo.adjuntos WHERE id = @id;

-- name: AdjuntoPorIDParaActualizar :one
SELECT * FROM nucleo.adjuntos WHERE id = @id FOR UPDATE;

-- name: InsertarParte :exec
INSERT INTO nucleo.adjuntos_partes (adjunto_id, desde, datos) VALUES (@adjunto_id, @desde, @datos);

-- name: AvanzarRecibido :one
-- Solo avanza si nadie más escribió desde ese offset (dos PATCH a la vez: el segundo no avanza).
UPDATE nucleo.adjuntos
SET recibido = recibido + @n::bigint, estado = 'subiendo', updated_at = now()
WHERE id = @id AND recibido = @desde AND estado IN ('pendiente', 'subiendo')
RETURNING recibido;

-- name: ParteDesde :one
SELECT datos FROM nucleo.adjuntos_partes WHERE adjunto_id = @adjunto_id AND desde = @desde;

-- name: BorrarPartes :exec
DELETE FROM nucleo.adjuntos_partes WHERE adjunto_id = @adjunto_id;

-- name: CompletarAdjunto :one
UPDATE nucleo.adjuntos
SET estado = 'completo', completado_en = now(), version = version + 1, updated_at = now()
WHERE id = @id AND estado = 'subiendo' AND recibido = tamano
RETURNING *;

-- name: RechazarAdjunto :one
UPDATE nucleo.adjuntos
SET estado = 'rechazado', motivo_rechazo = @motivo, version = version + 1, updated_at = now()
WHERE id = @id AND estado IN ('pendiente', 'subiendo')
RETURNING *;

-- name: AdjuntosDeEntidad :many
SELECT * FROM nucleo.adjuntos
WHERE entidad = @entidad AND entidad_id = @entidad_id AND id > @despues_de
ORDER BY id
LIMIT @limite;

-- name: ContarCompletos :many
SELECT tipo, coalesce(clave_checklist, '')::text AS clave_checklist, count(*)::bigint AS total
FROM nucleo.adjuntos
WHERE entidad = @entidad AND entidad_id = @entidad_id AND estado = 'completo'
GROUP BY tipo, clave_checklist;

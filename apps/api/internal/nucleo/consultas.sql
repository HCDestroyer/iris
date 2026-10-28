-- name: ReservarIdempotencia :execrows
INSERT INTO nucleo.idempotencia (actor, clave, metodo, ruta, hash_peticion, expira_en)
VALUES (@actor, @clave, @metodo, @ruta, @hash_peticion, @expira_en)
ON CONFLICT (actor, clave) DO NOTHING;

-- name: ObtenerIdempotencia :one
SELECT * FROM nucleo.idempotencia WHERE actor = @actor AND clave = @clave;

-- name: BorrarIdempotenciaExpirada :execrows
DELETE FROM nucleo.idempotencia WHERE actor = @actor AND clave = @clave AND expira_en < now();

-- name: CompletarIdempotencia :exec
UPDATE nucleo.idempotencia
SET estado = 'completa', status = @status, content_type = @content_type, respuesta = @respuesta, updated_at = now()
WHERE actor = @actor AND clave = @clave;

-- name: LiberarIdempotencia :exec
DELETE FROM nucleo.idempotencia WHERE actor = @actor AND clave = @clave AND estado = 'en_proceso';

-- name: PurgarIdempotencia :execrows
DELETE FROM nucleo.idempotencia WHERE expira_en < now();

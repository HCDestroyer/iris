-- name: ReservarIdempotencia :execrows
INSERT INTO nucleo.idempotencia (actor, clave, metodo, ruta, hash_peticion, hash_formato, hash_clave_id, expira_en)
VALUES (@actor, @clave, @metodo, @ruta, @hash_peticion, 1, @hash_clave_id, @expira_en)
ON CONFLICT (actor, clave) DO NOTHING;

-- name: ObtenerIdempotencia :one
SELECT * FROM nucleo.idempotencia WHERE actor = @actor AND clave = @clave;

-- name: BorrarIdempotenciaExpirada :execrows
DELETE FROM nucleo.idempotencia WHERE actor = @actor AND clave = @clave AND expira_en < now();

-- name: CompletarIdempotencia :exec
-- estado: 'completa' (con la respuesta) o 'no_repetible' (sin cuerpo: llevaba un secreto).
UPDATE nucleo.idempotencia
SET estado = @estado, status = @status, content_type = @content_type, respuesta = @respuesta, updated_at = now()
WHERE actor = @actor AND clave = @clave;

-- name: ReclamarIdempotencia :execrows
-- Retoma una clave que quedó en_proceso desde antes de @antes (el proceso cayó entre el COMMIT
-- y CompletarIdempotencia). Solo una petición gana: la que mueve updated_at.
-- El hash ya se comparó en Go (formato y clave de la fila): aquí se reescribe con el actual.
UPDATE nucleo.idempotencia
SET updated_at = now(), expira_en = @expira_en,
    hash_peticion = @hash_peticion, hash_formato = 1, hash_clave_id = @hash_clave_id
WHERE actor = @actor AND clave = @clave AND estado = 'en_proceso'
  AND hash_peticion = @hash_previo AND updated_at < @antes::timestamptz;

-- name: LiberarIdempotencia :exec
DELETE FROM nucleo.idempotencia WHERE actor = @actor AND clave = @clave AND estado = 'en_proceso';

-- name: PurgarIdempotencia :execrows
DELETE FROM nucleo.idempotencia WHERE expira_en < now();

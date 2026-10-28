-- name: InsertarEvento :exec
INSERT INTO integracion.outbox (id, evento, empresa_id, payload, ocurrido_en, request_id)
VALUES (@id, @evento, @empresa_id, @payload, @ocurrido_en, @request_id);

-- name: TomarCandadoRelay :one
SELECT pg_try_advisory_xact_lock(@clave::bigint)::boolean AS tomado;

-- name: TomarEventoPendiente :one
-- Un evento por transacción corta: se entrega y se confirma solo.
SELECT * FROM integracion.outbox
WHERE publicado_en IS NULL AND fallido_en IS NULL AND proximo_intento <= now()
ORDER BY id
LIMIT 1
FOR UPDATE SKIP LOCKED;

-- name: MarcarPublicado :exec
UPDATE integracion.outbox
SET publicado_en = now(), intentos = intentos + 1, ultimo_error = NULL, updated_at = now()
WHERE id = @id;

-- name: MarcarFallido :exec
UPDATE integracion.outbox
SET intentos = intentos + 1, ultimo_error = @ultimo_error, proximo_intento = @proximo_intento, updated_at = now()
WHERE id = @id;

-- name: MarcarAgotado :exec
UPDATE integracion.outbox
SET intentos = intentos + 1, ultimo_error = @ultimo_error, fallido_en = now(), updated_at = now()
WHERE id = @id;

-- name: EventosPorRequest :many
SELECT * FROM integracion.outbox WHERE request_id = @request_id ORDER BY id;

-- name: EventoPorID :one
SELECT * FROM integracion.outbox WHERE id = @id;

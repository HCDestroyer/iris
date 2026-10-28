-- Tickets desde Horus ---------------------------------------------------------

-- name: ReclamarEventoHorus :execrows
-- Deduplica el evento interno horus.* por su entrante: 0 filas = ya se procesó (o lo está
-- procesando otra transacción, que espera aquí hasta su COMMIT).
INSERT INTO soporte.horus_eventos (entrante_id, evento, empresa_id, resultado)
VALUES (@entrante_id, @evento, sqlc.narg(empresa_id), 'procesando')
ON CONFLICT DO NOTHING;

-- name: CerrarEventoHorus :exec
UPDATE soporte.horus_eventos
SET resultado = @resultado, ticket_id = sqlc.narg(ticket_id), detalle = sqlc.narg(detalle), updated_at = now()
WHERE entrante_id = @entrante_id;

-- name: EventoHorus :one
SELECT * FROM soporte.horus_eventos WHERE entrante_id = @entrante_id;

-- name: BloquearIncidente :exec
-- Serializa, hasta el COMMIT, a quienes abren o tocan el maestro de un mismo router o servicio
-- de infraestructura (dos eventos distintos de la misma caída no abren dos maestros).
SELECT pg_advisory_xact_lock(hashtextextended(@clave::text, 0));

-- name: UltimoMaestro :one
-- El maestro más reciente del router o servicio de infraestructura en la empresa (cualquier
-- estado), con candado.
SELECT * FROM soporte.tickets
WHERE maestro = @maestro::text
  AND (CASE WHEN @maestro::text = 'router' THEN router_id = sqlc.narg(router_id) ELSE infra_id = sqlc.narg(infra_id) END)
  AND empresa_id IS NOT DISTINCT FROM sqlc.narg(empresa_id)
ORDER BY id DESC
LIMIT 1
FOR UPDATE;

-- name: HijosNoCerrados :many
SELECT * FROM soporte.tickets WHERE ticket_padre_id = @ticket_padre_id::bigint AND estado <> 'cerrado' ORDER BY id;

-- name: MaestrosParaVincular :many
-- Maestros abiertos del router de los servicios del cliente según la instantánea de Argos: el
-- servicio indicado o, sin servicio, todos los del cliente. Sin instantánea, ninguno.
SELECT t.* FROM soporte.tickets t
WHERE t.maestro IS NOT NULL
  AND t.estado IN ('nuevo', 'abierto', 'en_espera')
  AND t.empresa_id IS NOT DISTINCT FROM sqlc.narg(empresa_id)
  AND t.router_id IN (
      SELECT s.router_id FROM integracion.argos_servicios s
      WHERE s.router_id IS NOT NULL
        AND (sqlc.narg(empresa_id)::bigint IS NULL OR s.empresa_id = sqlc.narg(empresa_id))
        AND (CASE WHEN sqlc.narg(servicio_id)::bigint IS NOT NULL THEN s.id = sqlc.narg(servicio_id)
                  ELSE s.cliente_id = sqlc.narg(cliente_id) END)
  )
ORDER BY (t.maestro = 'router') DESC, t.id;

-- name: ReglaDeHallazgo :one
-- La regla de la empresa si existe (activa o no: una inactiva apaga la global); si no, la global.
SELECT * FROM soporte.reglas_hallazgos
WHERE categoria_horus = @categoria_horus AND (empresa_id IS NULL OR empresa_id = sqlc.narg(empresa_id))
ORDER BY empresa_id NULLS LAST
LIMIT 1;

-- name: TicketDeHallazgo :one
SELECT * FROM soporte.tickets
WHERE hallazgo_id = @hallazgo_id
  AND (sqlc.narg(empresa_id)::bigint IS NULL OR empresa_id = sqlc.narg(empresa_id))
ORDER BY id DESC
LIMIT 1
FOR UPDATE;

-- name: CategoriaPorCodigo :one
-- La categoría activa de la empresa con ese código o, si no tiene, la global.
SELECT * FROM soporte.categorias
WHERE codigo = @codigo AND activa AND (empresa_id IS NULL OR empresa_id = sqlc.narg(empresa_id))
ORDER BY empresa_id NULLS LAST
LIMIT 1;

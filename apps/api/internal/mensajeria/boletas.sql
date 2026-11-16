-- Boletas: aprobar y rechazar desde la conversación (contrato con Argos 0.7.0 §7.7, pedido B3).

-- name: BoletaPorID :one
SELECT * FROM mensajeria.boletas WHERE id = @id;

-- name: BoletasDeConversacion :many
SELECT * FROM mensajeria.boletas WHERE conversacion_id = @conversacion_id ORDER BY created_at, id;

-- name: ResolverBoleta :one
-- Deja la boleta aprobada o rechazada (la fila se leyó con FOR UPDATE y estaba registrada).
UPDATE mensajeria.boletas
SET estado = @estado, decidida_en = @decidida_en, decidida_por = sqlc.narg('decidida_por'), pago_id = sqlc.narg('pago_id'),
    motivo_rechazo = sqlc.narg('motivo_rechazo'), causa_rechazo = sqlc.narg('causa_rechazo'),
    verificado_por_nombre = sqlc.narg('verificado_por_nombre'), aviso_cliente = sqlc.narg('aviso_cliente'),
    aviso_mensaje_id = sqlc.narg('aviso_mensaje_id'), resolucion = NULL, resolucion_en = NULL, updated_at = now()
WHERE id = @id
RETURNING *;

-- name: CompletarDecisionBoleta :one
-- La boleta ya estaba resuelta (llegó antes el webhook): se completa quién decidió, sin tocar el resto.
UPDATE mensajeria.boletas
SET decidida_por = COALESCE(decidida_por, sqlc.narg('decidida_por')),
    verificado_por_nombre = COALESCE(verificado_por_nombre, sqlc.narg('verificado_por_nombre')),
    pago_id = COALESCE(pago_id, sqlc.narg('pago_id')), resolucion = NULL, resolucion_en = NULL, updated_at = now()
WHERE id = @id
RETURNING *;

-- name: GuardarResolucionBoleta :execrows
-- El webhook de Argos dijo que el comprobante se aprobó o se rechazó: queda por aplicar (una sola vez).
UPDATE mensajeria.boletas
SET resolucion = @resolucion, resolucion_en = @ahora, updated_at = now()
WHERE comprobante_id = @comprobante_id::bigint AND estado = 'registrada' AND resolucion IS NULL
  AND (empresa_id IS NULL OR sqlc.narg('empresa_id')::bigint IS NULL OR empresa_id = sqlc.narg('empresa_id')::bigint);

-- name: BoletasPorResolver :many
SELECT id FROM mensajeria.boletas WHERE resolucion IS NOT NULL AND resolucion_en <= @hasta ORDER BY resolucion_en LIMIT 50;

-- name: DescartarResolucionBoleta :exec
UPDATE mensajeria.boletas SET resolucion = NULL, resolucion_en = NULL, updated_at = now() WHERE id = @id;

-- Boletas: aprobar y rechazar desde la conversación (contrato con Argos 0.7.0 §7.7, pedido B3).

-- name: BoletaPorID :one
SELECT * FROM mensajeria.boletas WHERE id = @id;

-- name: BoletasDeConversacion :many
SELECT * FROM mensajeria.boletas WHERE conversacion_id = @conversacion_id ORDER BY created_at, id;

-- name: ResolverBoleta :one
-- Deja la boleta aprobada o rechazada como dice Argos (la fila se leyó con FOR UPDATE). Sirve para
-- resolverla (estaba registrada) y para corregirla (Argos dice otra cosa que lo guardado).
UPDATE mensajeria.boletas
SET estado = @estado, decidida_en = @decidida_en, decidida_por = sqlc.narg('decidida_por'), pago_id = sqlc.narg('pago_id'),
    motivo_rechazo = sqlc.narg('motivo_rechazo'), causa_rechazo = sqlc.narg('causa_rechazo'),
    verificado_por_nombre = sqlc.narg('verificado_por_nombre'), aviso_cliente = sqlc.narg('aviso_cliente'),
    aviso_mensaje_id = sqlc.narg('aviso_mensaje_id'), resolucion = NULL, resolucion_en = NULL, resolucion_proximo = NULL,
    resolucion_intentos = 0, resolucion_error = NULL, resolucion_agotada_en = NULL, updated_at = now()
WHERE id = @id
RETURNING *;

-- name: CompletarDecisionBoleta :one
-- La boleta ya estaba resuelta con ese mismo estado: se completa quién decidió, sin tocar el resto.
UPDATE mensajeria.boletas
SET decidida_por = COALESCE(decidida_por, sqlc.narg('decidida_por')),
    verificado_por_nombre = COALESCE(verificado_por_nombre, sqlc.narg('verificado_por_nombre')),
    pago_id = COALESCE(pago_id, sqlc.narg('pago_id')), resolucion = NULL, resolucion_en = NULL, resolucion_proximo = NULL,
    resolucion_intentos = 0, resolucion_error = NULL, resolucion_agotada_en = NULL, updated_at = now()
WHERE id = @id
RETURNING *;

-- name: GuardarResolucionBoleta :execrows
-- El webhook de Argos dijo que el comprobante se aprobó o se rechazó: queda por confirmar con Argos
-- (GET /comprobantes/:id). Solo la boleta de ese comprobante en esa empresa, y sin pisar una
-- confirmación en curso (una agotada sí se reintenta).
UPDATE mensajeria.boletas
SET resolucion = @resolucion, resolucion_en = @ahora, resolucion_proximo = @proximo, resolucion_intentos = 0, resolucion_error = NULL,
    resolucion_agotada_en = NULL, updated_at = now()
WHERE comprobante_id = @comprobante_id::bigint AND empresa_id = @empresa_id::bigint
  AND estado IN ('registrada', 'aprobada', 'rechazada')
  AND (resolucion IS NULL OR resolucion_agotada_en IS NOT NULL);

-- name: BoletasPorResolver :many
SELECT id FROM mensajeria.boletas
WHERE resolucion IS NOT NULL AND resolucion_agotada_en IS NULL AND resolucion_proximo <= @ahora
ORDER BY resolucion_proximo LIMIT 50;

-- name: DescartarResolucionBoleta :exec
UPDATE mensajeria.boletas
SET resolucion = NULL, resolucion_en = NULL, resolucion_proximo = NULL, resolucion_intentos = 0, resolucion_error = NULL,
    resolucion_agotada_en = NULL, updated_at = now()
WHERE id = @id;

-- name: ReintentarResolucionBoleta :one
-- Argos no confirmó: otro intento más tarde o, si se acabaron, agotada.
UPDATE mensajeria.boletas
SET resolucion_intentos = resolucion_intentos + 1, resolucion_error = sqlc.narg('error'), resolucion_proximo = sqlc.narg('proximo'),
    resolucion_agotada_en = sqlc.narg('agotada_en'), updated_at = now()
WHERE id = @id
RETURNING *;

-- name: UsuarioPersona :one
-- Un usuario persona de esta instalación (para atribuir una decisión que informa Argos).
SELECT id FROM nucleo.usuarios WHERE id = @id AND tipo = 'persona';

-- Gestiones de cobro hacia Argos (contrato Iris ↔ ArgosISP 0.13.0 §3.12, B31). sqlc: mensajeriabd.

-- name: InsertarGestionCobro :one
INSERT INTO mensajeria.gestiones_cobro (id, empresa_id, conversacion_id, cliente_id, servicio_id, contacto_id, tipo, resultado,
    ocurrido_en, origen_tipo, origen_id, usuario_id, asistente, idempotency_key, hash_peticion)
VALUES (@id, @empresa_id, @conversacion_id, @cliente_id, sqlc.narg(servicio_id), sqlc.narg(contacto_id), @tipo, @resultado,
    @ocurrido_en, @origen_tipo, @origen_id, sqlc.narg(usuario_id), @asistente, sqlc.narg(idempotency_key), sqlc.narg(hash_peticion))
ON CONFLICT DO NOTHING
RETURNING *;

-- name: FijarPendienteGestionCobro :exec
UPDATE mensajeria.gestiones_cobro SET pendiente_id = @pendiente_id WHERE id = @id;

-- name: GestionCobroPorClave :one
-- La gestión que ya registró ese usuario (0 = el asistente) en la conversación con esa clave.
SELECT * FROM mensajeria.gestiones_cobro
WHERE conversacion_id = @conversacion_id AND coalesce(usuario_id, 0) = @usuario::bigint AND idempotency_key = @idempotency_key;

-- name: GestionesCobroDeConversacion :many
SELECT g.*, p.estado AS pendiente_estado, p.codigo_error AS pendiente_codigo
FROM mensajeria.gestiones_cobro g
LEFT JOIN integracion.pendientes p ON p.id = g.pendiente_id
WHERE g.conversacion_id = @conversacion_id
ORDER BY g.created_at DESC, g.id DESC
LIMIT 100;

-- name: RespaldoPagoEnConversacion :one
-- Hay una promesa o un comprobante real en la conversación (lo pide el asistente para
-- pago_informado y promete_pagar): una promesa que Argos registró y no rechazó, o una boleta que ya es
-- comprobante en Argos y no se rechazó.
SELECT (EXISTS (SELECT 1 FROM mensajeria.promesas p WHERE p.conversacion_id = @conversacion_id AND p.cliente_id = @cliente_id
                  AND p.promesa_argos_id IS NOT NULL AND p.estado IN ('activa', 'pendiente_aprobacion'))
     OR EXISTS (SELECT 1 FROM mensajeria.boletas b WHERE b.conversacion_id = @conversacion_id AND b.cliente_id = @cliente_id
                  AND b.comprobante_id IS NOT NULL AND b.estado IN ('registrada', 'aprobada')))::boolean;

-- name: ServicioDelClienteInstantanea :one
-- El servicio es de ese cliente y de esa empresa según la instantánea de Argos.
SELECT EXISTS (SELECT 1 FROM integracion.argos_servicios s WHERE s.id = @servicio_id AND s.cliente_id = @cliente_id AND s.empresa_id = @empresa)::boolean;

-- name: ContratoArgosActivo :one
-- Los contratos del conector Argos activo (el acordado sale de conector.ContratoAcordado).
SELECT contrato_remoto, contrato_informado FROM integracion.conectores WHERE sistema = 'argosisp' AND estado = 'activo' LIMIT 1;

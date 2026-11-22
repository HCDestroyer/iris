-- Cambios de Wi-Fi pedidos a Argos (contrato argosisp.md 0.9.0 §12.4).

-- name: CrearCambioWifi :one
INSERT INTO integracion.cambios_wifi (id, empresa_id, servicio_id, cliente_id, origen_tipo, origen_id, actor, usuario_id, metodo,
    verificado_en, con_ssid, con_clave, idempotency_key, pendiente_id, estado, verificacion_id)
VALUES (@id, sqlc.narg(empresa_id), @servicio_id, @cliente_id, @origen_tipo, @origen_id, @actor, sqlc.narg(usuario_id), @metodo,
    @verificado_en, @con_ssid, @con_clave, @idempotency_key, sqlc.narg(pendiente_id), 'pendiente', sqlc.narg(verificacion_id))
RETURNING *;

-- name: CambiosWifiDelDia :one
-- Cambios pedidos al servicio desde @desde (el día de Guatemala), sin contar los que Argos rechazó.
SELECT count(*)::int FROM integracion.cambios_wifi
WHERE servicio_id = @servicio_id AND created_at >= @desde AND estado <> 'fallido';

-- name: CambioWifiPorClave :one
SELECT * FROM integracion.cambios_wifi WHERE idempotency_key = @idempotency_key FOR UPDATE;

-- name: CambioWifiPorSolicitud :one
SELECT * FROM integracion.cambios_wifi WHERE solicitud_id = @solicitud_id::bigint FOR UPDATE;

-- name: GuardarCambioWifi :one
UPDATE integracion.cambios_wifi SET estado = @estado, solicitud_id = sqlc.narg(solicitud_id), causa = sqlc.narg(causa),
    detalle = sqlc.narg(detalle), entrante_id = sqlc.narg(entrante_id), updated_at = now()
WHERE id = @id
RETURNING *;

-- name: CambiosWifiDeOrigen :many
SELECT * FROM integracion.cambios_wifi WHERE origen_tipo = @origen_tipo AND origen_id = @origen_id
ORDER BY created_at DESC LIMIT 20;

-- name: VerificacionParaPanel :one
-- Una verificación §7.5 (desafío de Argos que coincidió) del cliente, de menos de 10 min y sin usar en
-- otro cambio ni en un enlace del asistente: vale para un solo cambio (0.9.0 §12.4).
SELECT v.id, v.creada_en FROM mensajeria.asistente_verificaciones v
WHERE v.id = @id AND v.cliente_id = @cliente_id AND v.resultado = 'coincide' AND v.creada_en >= @desde
  AND NOT EXISTS (SELECT 1 FROM integracion.cambios_wifi c WHERE c.verificacion_id = v.id)
  AND NOT EXISTS (SELECT 1 FROM mensajeria.enlaces_wifi e WHERE e.verificacion_id = v.id AND e.id IS DISTINCT FROM sqlc.narg(enlace_id)::uuid);

-- name: ServicioEsDelCliente :one
-- La instantánea de Argos dice que el servicio sigue siendo del cliente.
SELECT EXISTS (SELECT 1 FROM integracion.argos_servicios WHERE id = @servicio_id AND cliente_id = @cliente_id)::boolean;

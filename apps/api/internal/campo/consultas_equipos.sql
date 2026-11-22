-- Activación desde la orden y equipos del cliente (contrato argosisp.md 0.9.0 §12, B12 y B16).

-- name: ActivacionesDeOrden :many
SELECT * FROM campo.activaciones WHERE orden_id = @orden_id ORDER BY intento DESC;

-- name: ActivacionVivaDeOrden :one
-- El intento vivo (pendiente, en_proceso o listo) de la orden; a lo más uno (índice único).
SELECT * FROM campo.activaciones WHERE orden_id = @orden_id AND estado IN ('pendiente', 'en_proceso', 'listo');

-- name: ActivacionVivaDeServicio :one
SELECT * FROM campo.activaciones WHERE servicio_id = @servicio_id AND estado IN ('pendiente', 'en_proceso', 'listo');

-- name: ActivacionPorID :one
SELECT * FROM campo.activaciones WHERE id = @id;

-- name: ActivacionPorClave :one
SELECT * FROM campo.activaciones WHERE idempotency_key = @idempotency_key;

-- name: ActivacionPorAprovisionamiento :one
SELECT * FROM campo.activaciones WHERE aprovisionamiento_id = @aprovisionamiento_id::bigint ORDER BY intento DESC LIMIT 1;

-- name: ActivacionPorIDParaActualizar :one
SELECT * FROM campo.activaciones WHERE id = @id FOR UPDATE;

-- name: CrearActivacion :one
INSERT INTO campo.activaciones (id, orden_id, empresa_id, servicio_id, intento, idempotency_key, origen, pedido_por, estado,
    equipo_tipo, equipo_serie, equipo_mac, equipo_unidad_id, equipo_modelo, planta, wireless, wifi_ssid, con_clave_wifi, pendiente_id)
VALUES (@id, @orden_id, sqlc.narg(empresa_id), @servicio_id, @intento, @idempotency_key, @origen, sqlc.narg(pedido_por), 'pendiente',
    @equipo_tipo, sqlc.narg(equipo_serie), sqlc.narg(equipo_mac), sqlc.narg(equipo_unidad_id), sqlc.narg(equipo_modelo),
    sqlc.narg(planta), sqlc.narg(wireless), sqlc.narg(wifi_ssid), @con_clave_wifi, sqlc.narg(pendiente_id))
RETURNING *;

-- name: FijarPendienteActivacion :exec
UPDATE campo.activaciones SET pendiente_id = @pendiente_id, updated_at = now() WHERE id = @id;

-- name: GuardarActivacion :one
-- Avanza el intento (en la transacción que tomó la orden y la fila con FOR UPDATE).
UPDATE campo.activaciones SET estado = @estado, aprovisionamiento_id = sqlc.narg(aprovisionamiento_id), causa = sqlc.narg(causa),
    reintentable = sqlc.narg(reintentable), detalle = sqlc.narg(detalle), pasos = sqlc.narg(pasos),
    servicio_estado = sqlc.narg(servicio_estado), consultado_en = sqlc.narg(consultado_en), resuelto_en = sqlc.narg(resuelto_en),
    entrante_id = sqlc.narg(entrante_id), version = version + 1, updated_at = now()
WHERE id = @id
RETURNING *;

-- name: ActivacionesPorConsultar :many
-- En proceso sin respuesta de Argos desde hace más de @antes (el webhook no llegó): se consulta el GET.
SELECT * FROM campo.activaciones
WHERE estado = 'en_proceso' AND aprovisionamiento_id IS NOT NULL AND coalesce(consultado_en, updated_at) < @antes
ORDER BY updated_at LIMIT @limite;

-- name: OrdenActivacion :one
SELECT * FROM campo.orden_activacion WHERE orden_id = @orden_id;

-- name: AsegurarOrdenActivacion :one
-- La fila de la orden (la crea si no existe). Se llama con la orden ya tomada (FOR UPDATE).
INSERT INTO campo.orden_activacion (orden_id, empresa_id) VALUES (@orden_id, sqlc.narg(empresa_id))
ON CONFLICT (orden_id) DO UPDATE SET orden_id = EXCLUDED.orden_id
RETURNING *;

-- name: SumarIntentoActivacion :one
UPDATE campo.orden_activacion SET intentos = intentos + 1, version = version + 1, updated_at = now()
WHERE orden_id = @orden_id RETURNING intentos;

-- name: FijarSinActivacion :exec
UPDATE campo.orden_activacion SET sin_activacion_motivo = sqlc.narg(motivo), sin_activacion_por = sqlc.narg(por),
    version = version + 1, updated_at = now()
WHERE orden_id = @orden_id;

-- name: FijarBienvenidaOrden :one
UPDATE campo.orden_activacion SET bienvenida_canales = @canales, bienvenida_en = @en, version = version + 1, updated_at = now()
WHERE orden_id = @orden_id RETURNING *;

-- name: SumarReenvioBienvenida :one
UPDATE campo.orden_activacion SET bienvenida_reenvios = bienvenida_reenvios + 1, version = version + 1, updated_at = now()
WHERE orden_id = @orden_id RETURNING bienvenida_reenvios;

-- name: AjustesActivacion :one
-- Modo de la activación y bienvenida de la orden: la plantilla manda sobre la empresa; sin fila, al_cerrar.
SELECT coalesce(p.activacion_modo, a.activacion_modo, 'al_cerrar')::text AS modo,
       coalesce(p.cierre_exige_bienvenida, a.cierre_exige_bienvenida, false)::boolean AS exige_bienvenida
FROM (SELECT 1) uno
LEFT JOIN campo.ajustes a ON coalesce(a.empresa_id, 0) = coalesce(sqlc.narg(empresa_id)::bigint, 0)
LEFT JOIN campo.plantillas_checklist p ON p.id = sqlc.narg(plantilla_id)::bigint;

-- name: OrdenesDeServicioParaBienvenida :one
-- Órdenes de instalación del servicio con un intento listo o cerradas con éxito (la bienvenida de
-- Argos sin origen se relaciona por servicio).
SELECT o.id FROM campo.ordenes o JOIN campo.tipos_orden t ON t.id = o.tipo_id
WHERE o.servicio_id = @servicio_id AND t.codigo = 'instalacion' AND o.estado <> 'cancelada'
ORDER BY o.id DESC LIMIT 1;

-- Cobro de la instalación (B15) -------------------------------------------------------------------

-- name: CobroVivoDeOrden :one
SELECT * FROM campo.cobros WHERE orden_id = @orden_id AND estado IN ('pendiente', 'facturado', 'pagado', 'por_confirmar');

-- name: CobrosDeOrden :many
SELECT * FROM campo.cobros WHERE orden_id = @orden_id ORDER BY created_at DESC;

-- name: CobroPorClave :one
SELECT * FROM campo.cobros WHERE idempotency_key = @idempotency_key;

-- name: CobroPorFactura :one
SELECT * FROM campo.cobros WHERE factura_id = @factura_id::bigint AND orden_id = @orden_id ORDER BY created_at DESC LIMIT 1;

-- name: CobroParaActualizar :one
SELECT * FROM campo.cobros WHERE id = @id FOR UPDATE;

-- name: CrearCobro :one
INSERT INTO campo.cobros (id, orden_id, empresa_id, servicio_id, cliente_id, concepto_id, precio_unitario, precio_plantilla,
    motivo_precio, descripcion, registrado_por, estado, idempotency_key, pendiente_id, es_instalacion)
VALUES (@id, @orden_id, sqlc.narg(empresa_id), @servicio_id, @cliente_id, @concepto_id, @precio_unitario, sqlc.narg(precio_plantilla),
    sqlc.narg(motivo_precio), sqlc.narg(descripcion), sqlc.narg(registrado_por), 'pendiente', @idempotency_key, sqlc.narg(pendiente_id),
    @es_instalacion)
RETURNING *;

-- name: GuardarCobro :one
UPDATE campo.cobros SET estado = @estado, factura_id = sqlc.narg(factura_id), numero = sqlc.narg(numero), total = sqlc.narg(total),
    moneda = sqlc.narg(moneda), detalle = sqlc.narg(detalle), version = version + 1, updated_at = now()
WHERE id = @id
RETURNING *;

-- name: CobroDePlantilla :one
SELECT cobro_concepto_id, cobro_precio FROM campo.plantillas_checklist WHERE id = @id;

-- Bienvenida (B16) ------------------------------------------------------------------------------

-- name: BienvenidasSinEnviar :many
-- Instalaciones activas (intento listo o cerradas con éxito) de los últimos 30 días cuya bienvenida
-- no salió por ningún canal ni quedó sin enviar porque el cliente no acepta avisos (§12.5).
SELECT o.id, o.numero, o.empresa_id, o.cliente_id, o.servicio_id, o.estado, o.finalizada_en, o.zona_id, o.cuadrilla_id, o.tecnico_id,
       oa.bienvenida_canales, oa.bienvenida_en
FROM campo.ordenes o
JOIN campo.tipos_orden t ON t.id = o.tipo_id
LEFT JOIN campo.orden_activacion oa ON oa.orden_id = o.id
WHERE t.codigo = 'instalacion' AND o.servicio_id IS NOT NULL
  AND (@todas::boolean OR coalesce(o.empresa_id, 0) = coalesce(sqlc.narg(empresa_id)::bigint, 0))
  AND ((o.estado = 'cerrada' AND o.resultado = 'exitosa') OR EXISTS (
        SELECT 1 FROM campo.activaciones a WHERE a.orden_id = o.id AND a.estado = 'listo'))
  AND coalesce(o.finalizada_en, o.updated_at) > @desde
  AND NOT EXISTS (
        SELECT 1 FROM jsonb_array_elements(coalesce(oa.bienvenida_canales, '[]'::jsonb)) c
        WHERE c->>'estado' = 'enviada' OR c->>'motivo' IN ('sin_permiso', 'baja'))
  AND (sqlc.narg(antes)::bigint IS NULL OR o.id < sqlc.narg(antes)::bigint)
ORDER BY o.id DESC
LIMIT @limite;

-- name: ServicioActivoEnArgos :one
-- La instantánea de Argos dice que el servicio ya está activo (0.14.3: «activado fuera de la orden»).
SELECT EXISTS (SELECT 1 FROM integracion.argos_servicios WHERE id = @servicio_id AND estado = 'activo')::boolean;

-- name: OrdenesParaBienvenidaSinOrigen :many
-- Bienvenida sin origen (0.14.3 §12.5): instalaciones del servicio y del titular actual con actividad en
-- los 7 días anteriores a la recepción; solo se relaciona si hay exactamente una.
SELECT o.id FROM campo.ordenes o JOIN campo.tipos_orden t ON t.id = o.tipo_id
WHERE o.servicio_id = @servicio_id AND o.cliente_id = @cliente_id AND t.codigo = 'instalacion' AND o.estado <> 'cancelada'
  AND o.updated_at >= @desde
LIMIT 2;

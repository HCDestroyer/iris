-- Caídas repetidas y falla general (contrato con Argos 0.13.0 §3.11; B29 y B30). sqlc: soportebd.

-- name: ReclamarEventoRed :execrows
INSERT INTO soporte.red_eventos (entrante_id, evento, empresa_id, resultado)
VALUES (@entrante_id, @evento, sqlc.narg(empresa_id), 'en_proceso')
ON CONFLICT (entrante_id) DO NOTHING;

-- name: EventoRed :one
SELECT * FROM soporte.red_eventos WHERE entrante_id = @entrante_id;

-- name: CerrarEventoRed :exec
UPDATE soporte.red_eventos SET resultado = @resultado, ticket_id = sqlc.narg(ticket_id), detalle = sqlc.narg(detalle), updated_at = now()
WHERE entrante_id = @entrante_id;

-- name: AjustesRed :one
SELECT * FROM soporte.red_ajustes WHERE empresa_id = @empresa_id;

-- name: GuardarAjustesRed :one
-- version 0 = de fábrica (sin fila); si no, la fila debe tener esa versión.
INSERT INTO soporte.red_ajustes (empresa_id, caidas_umbral, caidas_ventana_min, falla_umbral, actualizado_por)
SELECT @empresa_id, @caidas_umbral, @caidas_ventana_min, @falla_umbral, sqlc.narg(actualizado_por)
WHERE @version::integer = 0
ON CONFLICT (empresa_id) DO NOTHING
RETURNING *;

-- name: ActualizarAjustesRed :one
UPDATE soporte.red_ajustes SET caidas_umbral = @caidas_umbral, caidas_ventana_min = @caidas_ventana_min, falla_umbral = @falla_umbral,
    actualizado_por = sqlc.narg(actualizado_por), version = version + 1, updated_at = now()
WHERE empresa_id = @empresa_id AND version = @version
RETURNING *;

-- name: IncidenteAbiertoDeServicio :one
-- La falla general abierta de la empresa que afecta al servicio (la más reciente).
SELECT i.* FROM soporte.incidentes_red i
JOIN soporte.incidente_red_servicios s ON s.incidente = i.id
WHERE i.empresa_id = @empresa_id AND i.estado = 'abierta' AND s.servicio_id = @servicio_id
ORDER BY i.id DESC
LIMIT 1;

-- name: TicketCaidasAbierto :one
SELECT * FROM soporte.tickets
WHERE empresa_id IS NOT DISTINCT FROM sqlc.narg(empresa_id) AND servicio_id = @servicio_id AND categoria_id = @categoria_id
  AND estado IN ('nuevo', 'abierto', 'en_espera')
ORDER BY id DESC
LIMIT 1
FOR UPDATE;

-- name: PosibleFallaAbierta :one
SELECT * FROM soporte.tickets
WHERE empresa_id IS NOT DISTINCT FROM sqlc.narg(empresa_id) AND maestro = 'posible_falla' AND estado IN ('nuevo', 'abierto', 'en_espera')
LIMIT 1
FOR UPDATE;

-- name: ContarCaidasRecientes :one
-- Tickets de caídas que abrió Iris en la empresa desde @desde.
SELECT count(*)::integer FROM soporte.tickets
WHERE empresa_id IS NOT DISTINCT FROM sqlc.narg(empresa_id) AND categoria_id = @categoria_id AND origen = 'argos'
  AND maestro IS NULL AND created_at >= @desde;

-- name: CaidasRecientesSinPadre :many
SELECT * FROM soporte.tickets
WHERE empresa_id IS NOT DISTINCT FROM sqlc.narg(empresa_id) AND categoria_id = @categoria_id AND origen = 'argos'
  AND maestro IS NULL AND created_at >= @desde AND ticket_padre_id IS NULL AND estado IN ('nuevo', 'abierto', 'en_espera')
ORDER BY id
FOR UPDATE;

-- name: IncidenteRed :one
SELECT * FROM soporte.incidentes_red WHERE incidente_id = @incidente_id AND empresa_id = @empresa_id FOR UPDATE;

-- name: IncidenteReusable :one
-- Un incidente del mismo alcance que se cerró desde @desde (su ticket general se reusa).
SELECT i.* FROM soporte.incidentes_red i
JOIN soporte.tickets t ON t.id = i.ticket_id AND t.estado <> 'cerrado'
WHERE i.empresa_id = @empresa_id AND i.alcance = @alcance AND i.alcance_id = @alcance_id AND i.estado = 'cerrada' AND i.cerrada_en >= @desde
ORDER BY i.cerrada_en DESC
LIMIT 1;

-- name: CrearIncidenteRed :one
INSERT INTO soporte.incidentes_red (incidente_id, empresa_id, alcance, alcance_id, ticket_id, desde, hasta, reusa_de)
VALUES (@incidente_id, @empresa_id, @alcance, @alcance_id, @ticket_id, @desde, sqlc.narg(hasta), sqlc.narg(reusa_de))
RETURNING *;

-- name: AgregarServiciosIncidente :execrows
INSERT INTO soporte.incidente_red_servicios (incidente, servicio_id)
SELECT @incidente::bigint, unnest(@servicios::bigint[])
ON CONFLICT DO NOTHING;

-- name: CerrarIncidenteRed :one
UPDATE soporte.incidentes_red SET estado = 'cerrada', hasta = sqlc.narg(hasta), cerrada_en = @ahora, updated_at = now()
WHERE id = @id
RETURNING *;

-- name: IncidentesDeTicket :many
SELECT * FROM soporte.incidentes_red WHERE ticket_id = @ticket_id ORDER BY id DESC;

-- name: TicketsParaAbsorber :many
-- Tickets de conectividad abiertos de servicios afectados, creados después de @desde, que no son
-- maestros ni hijos (o son hijos de una posible falla general).
SELECT t.* FROM soporte.tickets t
JOIN soporte.categorias c ON c.id = t.categoria_id AND c.conectividad
WHERE t.empresa_id IS NOT DISTINCT FROM sqlc.narg(empresa_id) AND t.estado IN ('nuevo', 'abierto', 'en_espera')
  AND t.maestro IS NULL AND t.id <> @general_id::bigint AND t.created_at > @desde
  AND t.servicio_id IN (SELECT s.servicio_id FROM soporte.incidente_red_servicios s WHERE s.incidente = @incidente::bigint)
  AND (t.ticket_padre_id IS NULL OR EXISTS (SELECT 1 FROM soporte.tickets p WHERE p.id = t.ticket_padre_id AND p.maestro = 'posible_falla'))
ORDER BY t.id
FOR UPDATE OF t;

-- name: FijarPadreTicket :one
UPDATE soporte.tickets SET ticket_padre_id = @padre_id, version = version + 1, updated_at = now()
WHERE id = @id
RETURNING *;

-- name: HijosAbiertos :many
SELECT * FROM soporte.tickets WHERE ticket_padre_id = @padre_id AND estado IN ('nuevo', 'abierto', 'en_espera') ORDER BY id FOR UPDATE;

-- name: MaestroRouterAbierto :one
SELECT * FROM soporte.tickets
WHERE empresa_id IS NOT DISTINCT FROM sqlc.narg(empresa_id) AND maestro = 'router' AND router_id = @router_id
  AND estado IN ('nuevo', 'abierto', 'en_espera')
LIMIT 1
FOR UPDATE;

-- name: RegistrarAvisoFallaCliente :execrows
INSERT INTO soporte.avisos_falla_clientes (incidente, cliente_id, tipo, ticket_id, enviado_en)
VALUES (@incidente, @cliente_id, @tipo, @ticket_id, @enviado_en)
ON CONFLICT DO NOTHING;

-- name: CierreAvisadoDesde :one
-- El cliente recibió un aviso de cierre de otra falla desde @desde (máximo uno cada 6 h).
SELECT EXISTS (SELECT 1 FROM soporte.avisos_falla_clientes
               WHERE cliente_id = @cliente_id AND tipo = 'cierre' AND enviado_en >= @desde AND incidente <> @incidente::bigint)::boolean;

-- name: RegistrarAvisoHijoSinMensaje :exec
INSERT INTO soporte.avisos_hijos (maestro_id, aviso, hijo_id, mensaje_id) VALUES (@maestro_id, @aviso, @hijo_id, NULL);

-- name: CategoriaDeTicket :one
SELECT * FROM soporte.categorias WHERE id = @id;

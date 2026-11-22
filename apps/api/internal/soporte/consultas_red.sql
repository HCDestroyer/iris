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
INSERT INTO soporte.red_ajustes (empresa_id, caidas_umbral, caidas_ventana_min, falla_umbral, verificacion_horas, actualizado_por)
SELECT @empresa_id, @caidas_umbral, @caidas_ventana_min, @falla_umbral, @verificacion_horas, sqlc.narg(actualizado_por)
WHERE @version::integer = 0
ON CONFLICT (empresa_id) DO NOTHING
RETURNING *;

-- name: ActualizarAjustesRed :one
UPDATE soporte.red_ajustes SET caidas_umbral = @caidas_umbral, caidas_ventana_min = @caidas_ventana_min, falla_umbral = @falla_umbral,
    verificacion_horas = @verificacion_horas,
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

-- name: PosibleFallaVigente :one
-- La posible falla abierta que todavía agrupa caídas: sin padre (no la confirmó una falla general) o
-- creada dentro de su ventana (desde).
SELECT * FROM soporte.tickets
WHERE empresa_id IS NOT DISTINCT FROM sqlc.narg(empresa_id) AND maestro = 'posible_falla' AND estado IN ('nuevo', 'abierto', 'en_espera')
  AND (ticket_padre_id IS NULL OR created_at >= @desde)
LIMIT 1
FOR UPDATE;

-- name: PosiblesFallaHijasDe :many
-- Las posibles fallas que confirmó (y cuelgan de) este ticket general y siguen abiertas.
SELECT * FROM soporte.tickets
WHERE ticket_padre_id = @general_id::bigint AND maestro = 'posible_falla' AND estado IN ('nuevo', 'abierto', 'en_espera')
ORDER BY id
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

-- name: AvisoFallaDesde :one
-- El cliente recibió un aviso de ese tipo de otra falla desde @desde (máximo uno cada 6 h por tipo).
SELECT EXISTS (SELECT 1 FROM soporte.avisos_falla_clientes
               WHERE cliente_id = @cliente_id AND tipo = @tipo AND enviado_en >= @desde AND incidente <> @incidente::bigint
                 AND incidente IS DISTINCT FROM sqlc.narg(excluir)::bigint)::boolean;

-- name: HijosEnEsperaDeIncidente :many
-- Los hijos de un incidente anterior que siguen colgando del ticket general y en espera de verificar
-- (al reusar el general vuelven a abierto y pasan al incidente nuevo).
SELECT t.* FROM soporte.tickets t
JOIN soporte.incidente_red_hijos h ON h.ticket_id = t.id AND h.incidente = @incidente::bigint
WHERE t.ticket_padre_id = @general_id::bigint AND t.estado = 'en_espera' AND t.maestro IS NULL
ORDER BY t.id
FOR UPDATE OF t;

-- name: RegistrarAvisoHijoSinMensaje :exec
INSERT INTO soporte.avisos_hijos (maestro_id, aviso, hijo_id, mensaje_id) VALUES (@maestro_id, @aviso, @hijo_id, NULL);

-- name: CategoriaDeTicket :one
SELECT * FROM soporte.categorias WHERE id = @id;

-- name: RegistrarCierreAnticipado :exec
INSERT INTO soporte.incidentes_red_cierres (incidente_id, empresa_id, hasta) VALUES (@incidente_id, @empresa_id, sqlc.narg(hasta))
ON CONFLICT DO NOTHING;

-- name: HayCierreAnticipado :one
SELECT EXISTS (SELECT 1 FROM soporte.incidentes_red_cierres WHERE incidente_id = @incidente_id AND empresa_id = @empresa_id)::boolean;

-- name: RegistrarHijoIncidente :exec
INSERT INTO soporte.incidente_red_hijos (incidente, ticket_id) VALUES (@incidente, @ticket_id) ON CONFLICT DO NOTHING;

-- name: EsHijoDeIncidente :one
SELECT EXISTS (SELECT 1 FROM soporte.incidente_red_hijos WHERE incidente = @incidente AND ticket_id = @ticket_id)::boolean;

-- name: HijosDeIncidente :many
-- Los hijos que absorbió este incidente y siguen colgando del ticket general y abiertos.
SELECT t.* FROM soporte.tickets t
JOIN soporte.incidente_red_hijos h ON h.ticket_id = t.id AND h.incidente = @incidente::bigint
WHERE t.ticket_padre_id = @general_id::bigint AND t.estado IN ('nuevo', 'abierto', 'en_espera') AND t.maestro IS NULL
ORDER BY t.id
FOR UPDATE OF t;

-- name: HorusArribaDeServicio :one
-- Horus confirmó que el router del servicio (instantánea) está arriba después de @desde.
SELECT EXISTS (
    SELECT 1 FROM integracion.argos_servicios s
    JOIN soporte.horus_estados h ON h.clave IN ('router:' || s.empresa_id || ':' || s.router_id, 'router:0:' || s.router_id)
    WHERE s.id = @servicio_id AND s.router_id IS NOT NULL AND h.estado = 'arriba' AND h.ocurrido_en >= @desde
)::boolean;

-- name: HijosVerificandoDeRouter :many
-- Hijos de fallas generales cerradas que esperan verificar con el cliente, con servicio en el router
-- (ver resolverConfirmadosPorHorus en red_argos.go).
SELECT DISTINCT t.* FROM soporte.tickets t
JOIN soporte.incidente_red_hijos h ON h.ticket_id = t.id
JOIN soporte.incidentes_red i ON i.id = h.incidente AND i.estado = 'cerrada' AND i.ticket_id = t.ticket_padre_id
JOIN integracion.argos_servicios s ON s.id = t.servicio_id AND s.empresa_id = t.empresa_id
WHERE t.empresa_id = coalesce(sqlc.narg(empresa_id)::bigint, t.empresa_id)
  AND i.empresa_id = t.empresa_id AND s.router_id = @router_id
  AND t.id > @despues::bigint AND t.estado = 'en_espera' AND t.maestro IS NULL
  AND t.en_espera_desde >= i.cerrada_en
  AND i.cerrada_en + make_interval(0, 0, 0, 0, coalesce((SELECT a.verificacion_horas FROM soporte.red_ajustes a WHERE a.empresa_id = i.empresa_id), 24)) >= @ahora::timestamptz
  AND i.desde < @ocurrido::timestamptz
ORDER BY t.id
LIMIT @limite;

-- name: IncidenteRedPorID :one
SELECT * FROM soporte.incidentes_red WHERE id = @id;

-- name: IncidenteAbiertoDeServicioCompartido :one
-- Como IncidenteAbiertoDeServicio, con candado compartido (lo usa quien crea un hijo).
SELECT i.* FROM soporte.incidentes_red i
JOIN soporte.incidente_red_servicios s ON s.incidente = i.id
WHERE i.empresa_id = @empresa_id AND i.estado = 'abierta' AND s.servicio_id = @servicio_id
ORDER BY i.id DESC
LIMIT 1
FOR SHARE OF i;

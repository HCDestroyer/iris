-- Calendarios y SLA ------------------------------------------------------------

-- name: CalendarioPorID :one
SELECT * FROM soporte.calendarios WHERE id = @id;

-- name: TramosDeCalendario :many
SELECT * FROM soporte.calendario_tramos WHERE calendario_id = @calendario_id ORDER BY dia_semana, inicio;

-- name: FeriadosDeCalendario :many
SELECT * FROM soporte.feriados WHERE calendario_id = @calendario_id ORDER BY fecha;

-- name: SLAPorID :one
SELECT * FROM soporte.slas WHERE id = @id;

-- name: SLAPorDefecto :one
-- El de la empresa si tiene uno; si no, el global.
SELECT * FROM soporte.slas
WHERE por_defecto AND activo AND (empresa_id IS NULL OR empresa_id = sqlc.narg(empresa_id))
ORDER BY empresa_id NULLS LAST
LIMIT 1;

-- name: ObjetivoSLA :one
SELECT * FROM soporte.sla_objetivos WHERE sla_id = @sla_id AND prioridad = @prioridad;

-- name: ListarSLAs :many
SELECT s.*, c.nombre AS calendario_nombre, c.zona_horaria AS calendario_zona
FROM soporte.slas s
LEFT JOIN soporte.calendarios c ON c.id = s.calendario_id
WHERE s.empresa_id IS NULL OR s.empresa_id = sqlc.narg(empresa_id)
ORDER BY s.id;

-- name: ObjetivosDeSLAs :many
SELECT * FROM soporte.sla_objetivos WHERE sla_id = ANY(@slas::bigint[]) ORDER BY sla_id, resolucion_min;

-- name: CategoriaPorID :one
SELECT * FROM soporte.categorias WHERE id = @id;

-- name: ListarCategorias :many
SELECT * FROM soporte.categorias
WHERE (empresa_id IS NULL OR empresa_id = sqlc.narg(empresa_id))
  AND (NOT @solo_activas::boolean OR activa)
ORDER BY nombre, id;

-- Usuarios (lectura de nucleo para validar la asignación) -----------------------

-- name: UsuarioAsignable :one
SELECT id, nombre, activo FROM nucleo.usuarios WHERE id = @id;

-- Tickets ----------------------------------------------------------------------

-- name: CrearTicket :one
INSERT INTO soporte.tickets (
    empresa_id, cliente_id, servicio_id, zona_id, contacto_nombre, contacto_telefono, contacto_correo,
    categoria_id, prioridad, origen, asunto, ticket_padre_id,
    sla_id, calendario_id, sla_inicio, sla_respuesta_min, sla_resolucion_min,
    riesgo_respuesta, vence_respuesta, riesgo_resolucion, vence_resolucion
) VALUES (
    @empresa_id, @cliente_id, @servicio_id, @zona_id, @contacto_nombre, @contacto_telefono, @contacto_correo,
    @categoria_id, @prioridad, @origen, @asunto, @ticket_padre_id,
    @sla_id, @calendario_id, @sla_inicio, @sla_respuesta_min, @sla_resolucion_min,
    @riesgo_respuesta, @vence_respuesta, @riesgo_resolucion, @vence_resolucion
)
RETURNING *;

-- name: TicketPorID :one
SELECT * FROM soporte.tickets WHERE id = @id;

-- name: TicketPorIDParaActualizar :one
SELECT * FROM soporte.tickets WHERE id = @id FOR UPDATE;

-- name: GuardarTicket :one
-- Escribe el estado completo que calculó el caso de uso (fila leída con FOR UPDATE).
UPDATE soporte.tickets SET
    estado = @estado, prioridad = @prioridad, asignado_a = @asignado_a, cuadrilla_id = @cuadrilla_id,
    nivel_escalamiento = @nivel_escalamiento,
    sla_respuesta_min = @sla_respuesta_min, sla_resolucion_min = @sla_resolucion_min, pausa_seg = @pausa_seg,
    riesgo_respuesta = @riesgo_respuesta, vence_respuesta = @vence_respuesta,
    riesgo_resolucion = @riesgo_resolucion, vence_resolucion = @vence_resolucion,
    primera_respuesta_en = @primera_respuesta_en, en_espera_desde = @en_espera_desde,
    resuelto_en = @resuelto_en, cerrado_en = @cerrado_en, fusionado_en_id = @fusionado_en_id,
    version = version + 1, updated_at = now()
WHERE id = @id AND version = @version
RETURNING *;

-- name: ListarTickets :many
-- Más recientes primero (cursor: id). El alcance limita a zonas, cuadrillas o lo asignado al actor.
SELECT * FROM soporte.tickets
WHERE (sqlc.narg(antes_de)::bigint IS NULL OR id < sqlc.narg(antes_de))
  AND (sqlc.narg(empresa_id)::bigint IS NULL OR empresa_id = sqlc.narg(empresa_id))
  AND (cardinality(@estados::text[]) = 0 OR estado = ANY(@estados::text[]))
  AND (cardinality(@prioridades::text[]) = 0 OR prioridad = ANY(@prioridades::text[]))
  AND (sqlc.narg(categoria_id)::bigint IS NULL OR categoria_id = sqlc.narg(categoria_id))
  AND (sqlc.narg(asignado_a)::bigint IS NULL OR asignado_a = sqlc.narg(asignado_a))
  AND (NOT @sin_asignar::boolean OR asignado_a IS NULL)
  AND (sqlc.narg(cliente_id)::bigint IS NULL OR cliente_id = sqlc.narg(cliente_id))
  AND (sqlc.narg(servicio_id)::bigint IS NULL OR servicio_id = sqlc.narg(servicio_id))
  AND (sqlc.narg(cuadrilla_id)::bigint IS NULL OR cuadrilla_id = sqlc.narg(cuadrilla_id))
  AND (sqlc.narg(ticket_padre_id)::bigint IS NULL OR ticket_padre_id = sqlc.narg(ticket_padre_id))
  AND (sqlc.narg(q)::text IS NULL OR numero ILIKE '%' || sqlc.narg(q) || '%' OR asunto ILIKE '%' || sqlc.narg(q) || '%'
       OR contacto_nombre ILIKE '%' || sqlc.narg(q) || '%')
  AND (@alcance_global::boolean
       OR zona_id = ANY(@zonas::bigint[])
       OR cuadrilla_id = ANY(@cuadrillas::bigint[])
       OR (sqlc.narg(actor_id)::bigint IS NOT NULL AND asignado_a = sqlc.narg(actor_id)))
ORDER BY id DESC
LIMIT @limite;

-- Mensajes e historial ---------------------------------------------------------

-- name: CrearMensaje :one
INSERT INTO soporte.ticket_mensajes (ticket_id, empresa_id, publico, autor_tipo, autor_id, autor_nombre, canal, cuerpo)
VALUES (@ticket_id, @empresa_id, @publico, @autor_tipo, @autor_id, @autor_nombre, @canal, @cuerpo)
RETURNING *;

-- name: ListarMensajes :many
SELECT * FROM soporte.ticket_mensajes
WHERE ticket_id = @ticket_id AND id > @despues_de AND (NOT @solo_publicos::boolean OR publico)
ORDER BY id
LIMIT @limite;

-- name: CrearEventoTicket :exec
INSERT INTO soporte.ticket_eventos (ticket_id, empresa_id, tipo, de, a, motivo, datos, actor_tipo, actor_id, actor_nombre, request_id)
VALUES (@ticket_id, @empresa_id, @tipo, @de, @a, @motivo, @datos, @actor_tipo, @actor_id, @actor_nombre, @request_id);

-- name: ListarEventosTicket :many
SELECT * FROM soporte.ticket_eventos
WHERE ticket_id = @ticket_id AND id > @despues_de
ORDER BY id
LIMIT @limite;

-- Cron de SLA ------------------------------------------------------------------

-- name: AlertasSLAPendientes :many
-- Relojes que cruzaron el 80 % o su vencimiento y todavía no se alertaron. Un reloj que ya
-- venció no emite «en riesgo» (si el cron estuvo detenido, solo sale el vencido).
SELECT t.id AS ticket_id, t.empresa_id, r.sla::text AS sla, r.umbral::text AS umbral, r.vence_en::timestamptz AS vence_en
FROM soporte.tickets t
CROSS JOIN LATERAL (VALUES
    ('respuesta', 'en_riesgo', t.vence_respuesta,
     t.primera_respuesta_en IS NULL AND t.estado IN ('nuevo', 'abierto', 'en_espera')
     AND t.riesgo_respuesta <= @ahora::timestamptz AND t.vence_respuesta > @ahora::timestamptz),
    ('respuesta', 'vencido', t.vence_respuesta,
     t.primera_respuesta_en IS NULL AND t.estado IN ('nuevo', 'abierto', 'en_espera')
     AND t.vence_respuesta <= @ahora::timestamptz),
    ('resolucion', 'en_riesgo', t.vence_resolucion,
     t.estado IN ('nuevo', 'abierto') AND t.riesgo_resolucion <= @ahora::timestamptz AND t.vence_resolucion > @ahora::timestamptz),
    ('resolucion', 'vencido', t.vence_resolucion,
     t.estado IN ('nuevo', 'abierto') AND t.vence_resolucion <= @ahora::timestamptz)
) AS r (sla, umbral, vence_en, aplica)
WHERE (t.estado IN ('nuevo', 'abierto', 'en_espera'))
  AND (t.riesgo_respuesta <= @ahora::timestamptz OR t.riesgo_resolucion <= @ahora::timestamptz)
  AND r.aplica
  AND NOT EXISTS (
      SELECT 1 FROM soporte.ticket_sla_alertas a
      WHERE a.ticket_id = t.id AND a.sla = r.sla AND a.umbral = r.umbral
  )
ORDER BY t.id, r.sla, r.umbral
LIMIT @limite;

-- name: ReclamarAlertaSLA :execrows
-- Marca la alerta como emitida; 0 filas = otra corrida ya la emitió.
INSERT INTO soporte.ticket_sla_alertas (ticket_id, sla, umbral, vence_en)
VALUES (@ticket_id, @sla, @umbral, @vence_en)
ON CONFLICT DO NOTHING;

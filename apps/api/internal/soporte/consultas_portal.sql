-- Portal del cliente (actor cliente: cliente y empresa de Argos de la sesión) ------------

-- name: ListarTicketsCliente :many
-- servicio_id (opcional): solo los de ese servicio (portal de Argos, §11.10: con un contacto, obligatorio).
SELECT sqlc.embed(t), c.nombre AS categoria_nombre, c.codigo AS categoria_codigo,
       coalesce(p.estado IN ('nuevo', 'abierto', 'en_espera'), false)::boolean AS padre_abierto,
       ac.nombre AS contacto_nombre
FROM soporte.tickets t
LEFT JOIN soporte.categorias c ON c.id = t.categoria_id
LEFT JOIN soporte.tickets p ON p.id = t.ticket_padre_id
LEFT JOIN integracion.argos_contactos ac ON ac.contacto_id = t.creado_por_contacto
WHERE t.cliente_id = @cliente_id::bigint AND t.empresa_id = @empresa_id::bigint
  AND (sqlc.narg(antes_de)::bigint IS NULL OR t.id < sqlc.narg(antes_de))
  AND (cardinality(@estados::text[]) = 0 OR t.estado = ANY(@estados::text[]))
  AND (sqlc.narg(servicio_id)::bigint IS NULL OR t.servicio_id = sqlc.narg(servicio_id))
ORDER BY t.id DESC
LIMIT @limite;

-- name: DatosTicketPortal :one
-- Nombre de la categoría, si el incidente maestro (padre) sigue abierto y el contacto que lo abrió.
SELECT c.nombre AS categoria_nombre, c.codigo AS categoria_codigo,
       coalesce(p.estado IN ('nuevo', 'abierto', 'en_espera'), false)::boolean AS padre_abierto,
       ac.nombre AS contacto_nombre
FROM soporte.tickets t
LEFT JOIN soporte.categorias c ON c.id = t.categoria_id
LEFT JOIN soporte.tickets p ON p.id = t.ticket_padre_id
LEFT JOIN integracion.argos_contactos ac ON ac.contacto_id = t.creado_por_contacto
WHERE t.id = @id;

-- name: ListarCategoriasPortal :many
SELECT * FROM soporte.categorias
WHERE visible_portal AND activa AND (empresa_id IS NULL OR empresa_id = @empresa_id::bigint)
ORDER BY nombre, id;

-- name: ClienteDeArgos :one
-- Ficha del cliente en la instantánea de Argos (contacto del ticket del portal).
-- El cliente es de la empresa si alguno de sus servicios lo es (o, en filas anteriores, su
-- empresa_id), o si no tiene servicios: a ese, el acceso al portal solo lo emite quien tiene
-- empresas.todas, así que una sesión del portal suya ya pasó por ese control.
SELECT c.nombre, c.telefono, c.zona_id FROM integracion.argos_clientes c
WHERE c.id = @id AND (@empresa_id::bigint = ANY(c.empresas) OR c.sin_servicios
    OR (cardinality(c.empresas) = 0 AND c.empresa_id = @empresa_id::bigint));

-- name: ServicioDeCliente :one
-- El servicio es del cliente según la instantánea de Argos (con su zona).
SELECT id, zona_id FROM integracion.argos_servicios WHERE id = @id AND cliente_id = @cliente_id AND empresa_id = @empresa_id;

-- name: ListarMensajesPublicos :many
SELECT * FROM soporte.ticket_mensajes
WHERE ticket_id = @ticket_id AND publico AND id > @despues_de
ORDER BY id
LIMIT @limite;

-- Topes del portal (anti spam): conteos en la base por cliente y empresa.

-- name: ConteoTicketsPortal :one
-- Tickets del portal del cliente abiertos (nuevo, abierto, en espera) y creados en la última hora.
SELECT count(*) FILTER (WHERE estado IN ('nuevo', 'abierto', 'en_espera'))::bigint AS abiertos,
       count(*) FILTER (WHERE created_at > now() - interval '1 hour')::bigint AS ultima_hora
FROM soporte.tickets
WHERE cliente_id = @cliente_id::bigint AND empresa_id = @empresa_id::bigint AND origen IN ('portal', 'portal_argos');

-- name: MensajesPortalUltimoMinuto :one
SELECT count(*)::bigint FROM soporte.ticket_mensajes m
JOIN soporte.tickets t ON t.id = m.ticket_id
WHERE t.cliente_id = @cliente_id::bigint AND t.empresa_id = @empresa_id::bigint
  AND m.autor_tipo = 'cliente' AND m.canal IN ('portal', 'portal_argos') AND m.created_at > now() - interval '1 minute';

-- Portal del cliente de Argos (contrato 0.9.0 §11) ---------------------------------------

-- name: MarcarTicketPortalArgos :exec
-- El contacto que lo abrió (§11.10) y si el cliente queda por conciliar con Argos (§11.2).
UPDATE soporte.tickets SET creado_por_contacto = sqlc.narg(contacto_id), cliente_revision = sqlc.narg(cliente_revision)
WHERE id = @id;

-- name: ConteoTicketsContacto :one
-- Topes propios del contacto (§11.10): abiertos por él y creados en la última hora.
SELECT count(*) FILTER (WHERE estado IN ('nuevo', 'abierto', 'en_espera'))::bigint AS abiertos,
       count(*) FILTER (WHERE created_at > now() - interval '1 hour')::bigint AS ultima_hora
FROM soporte.tickets
WHERE creado_por_contacto = @contacto_id::bigint AND empresa_id = @empresa_id::bigint AND origen = 'portal_argos';

-- name: MensajesContactoUltimoMinuto :one
SELECT count(*)::bigint FROM soporte.ticket_mensajes m
WHERE m.contacto_id = @contacto_id::bigint AND m.empresa_id = @empresa_id::bigint
  AND m.autor_tipo = 'cliente' AND m.canal = 'portal_argos' AND m.created_at > now() - interval '1 minute';

-- name: AdjuntoDeMensajePublico :one
-- El adjunto es de un mensaje público del ticket (§11.6); si no, ninguna fila.
SELECT a.* FROM nucleo.adjuntos a
WHERE a.id = @adjunto_id AND a.estado = 'completo'
  AND EXISTS (SELECT 1 FROM soporte.ticket_mensajes m WHERE m.ticket_id = @ticket_id AND m.publico AND a.id = ANY(m.adjuntos));

-- name: AdjuntosPorIDs :many
SELECT id, mime, tamano, nombre FROM nucleo.adjuntos WHERE id = ANY(@ids::uuid[]) AND estado = 'completo';

-- name: TicketsClientePorConciliar :many
-- Tickets del portal de Argos creados sin poder confirmar el cliente con Argos (§11.2).
SELECT id, cliente_id, empresa_id FROM soporte.tickets
WHERE cliente_revision = 'pendiente' AND cliente_id IS NOT NULL
ORDER BY id
LIMIT @limite;

-- name: ResolverClienteRevision :execrows
UPDATE soporte.tickets SET cliente_revision = sqlc.narg(cliente_revision)
WHERE id = @id AND cliente_revision = 'pendiente';

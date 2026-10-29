-- Portal del cliente (actor cliente: cliente y empresa de Argos de la sesión) ------------

-- name: ListarTicketsCliente :many
SELECT sqlc.embed(t), c.nombre AS categoria_nombre,
       coalesce(p.estado IN ('nuevo', 'abierto', 'en_espera'), false)::boolean AS padre_abierto
FROM soporte.tickets t
LEFT JOIN soporte.categorias c ON c.id = t.categoria_id
LEFT JOIN soporte.tickets p ON p.id = t.ticket_padre_id
WHERE t.cliente_id = @cliente_id::bigint AND t.empresa_id = @empresa_id::bigint
  AND (sqlc.narg(antes_de)::bigint IS NULL OR t.id < sqlc.narg(antes_de))
  AND (cardinality(@estados::text[]) = 0 OR t.estado = ANY(@estados::text[]))
ORDER BY t.id DESC
LIMIT @limite;

-- name: DatosTicketPortal :one
-- Nombre de la categoría y si el incidente maestro (padre) sigue abierto.
SELECT c.nombre AS categoria_nombre,
       coalesce(p.estado IN ('nuevo', 'abierto', 'en_espera'), false)::boolean AS padre_abierto
FROM soporte.tickets t
LEFT JOIN soporte.categorias c ON c.id = t.categoria_id
LEFT JOIN soporte.tickets p ON p.id = t.ticket_padre_id
WHERE t.id = @id;

-- name: ListarCategoriasPortal :many
SELECT * FROM soporte.categorias
WHERE visible_portal AND activa AND (empresa_id IS NULL OR empresa_id = @empresa_id::bigint)
ORDER BY nombre, id;

-- name: ClienteDeArgos :one
-- Ficha del cliente en la instantánea de Argos (contacto del ticket del portal).
-- El cliente es de la empresa si alguno de sus servicios lo es, o si no tiene servicios (Argos).
SELECT c.nombre, c.telefono, c.zona_id FROM integracion.argos_clientes c
WHERE c.id = @id AND (@empresa_id::bigint = ANY(c.empresas)
    OR (cardinality(c.empresas) = 0 AND c.empresa_id = @empresa_id::bigint)
    OR (cardinality(c.empresas) = 0 AND c.empresa_id = 0 AND NOT EXISTS (
    SELECT 1 FROM integracion.argos_servicios s WHERE s.cliente_id = c.id)));

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
WHERE cliente_id = @cliente_id::bigint AND empresa_id = @empresa_id::bigint AND origen = 'portal';

-- name: MensajesPortalUltimoMinuto :one
SELECT count(*)::bigint FROM soporte.ticket_mensajes m
JOIN soporte.tickets t ON t.id = m.ticket_id
WHERE t.cliente_id = @cliente_id::bigint AND t.empresa_id = @empresa_id::bigint
  AND m.autor_tipo = 'cliente' AND m.canal = 'portal' AND m.created_at > now() - interval '1 minute';

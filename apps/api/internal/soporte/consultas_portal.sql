-- Portal del cliente (actor cliente: cliente y empresa de Argos de la sesión) ------------

-- name: ListarTicketsCliente :many
SELECT sqlc.embed(t), c.nombre AS categoria_nombre
FROM soporte.tickets t
LEFT JOIN soporte.categorias c ON c.id = t.categoria_id
WHERE t.cliente_id = @cliente_id::bigint AND t.empresa_id = @empresa_id::bigint
  AND (sqlc.narg(antes_de)::bigint IS NULL OR t.id < sqlc.narg(antes_de))
  AND (cardinality(@estados::text[]) = 0 OR t.estado = ANY(@estados::text[]))
ORDER BY t.id DESC
LIMIT @limite;

-- name: NombreCategoria :one
SELECT nombre FROM soporte.categorias WHERE id = @id;

-- name: ListarCategoriasPortal :many
SELECT * FROM soporte.categorias
WHERE visible_portal AND activa AND (empresa_id IS NULL OR empresa_id = @empresa_id::bigint)
ORDER BY nombre, id;

-- name: ClienteDeArgos :one
-- Ficha del cliente en la instantánea de Argos (contacto del ticket del portal).
SELECT nombre, telefono, zona_id FROM integracion.argos_clientes WHERE id = @id AND empresa_id = @empresa_id;

-- name: ServicioDeCliente :one
-- El servicio es del cliente según la instantánea de Argos (con su zona).
SELECT id, zona_id FROM integracion.argos_servicios WHERE id = @id AND cliente_id = @cliente_id AND empresa_id = @empresa_id;

-- name: ListarMensajesPublicos :many
SELECT * FROM soporte.ticket_mensajes
WHERE ticket_id = @ticket_id AND publico AND id > @despues_de
ORDER BY id
LIMIT @limite;

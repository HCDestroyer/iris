-- Instantáneas (§2.2) -------------------------------------------------------------------

-- name: GuardarCliente :exec
INSERT INTO integracion.argos_clientes (
    id, empresa_id, codigo, nombre, telefono, direccion, lat, lon, zona_id, estado, sincronizado_en
) VALUES (
    @id, @empresa_id, @codigo, @nombre, @telefono, @direccion, @lat, @lon, @zona_id, @estado, @sincronizado_en
)
ON CONFLICT (id) DO UPDATE SET
    empresa_id = excluded.empresa_id, codigo = excluded.codigo, nombre = excluded.nombre,
    telefono = excluded.telefono, direccion = excluded.direccion, lat = excluded.lat, lon = excluded.lon,
    zona_id = excluded.zona_id, estado = excluded.estado, sincronizado_en = excluded.sincronizado_en,
    updated_at = now()
WHERE integracion.argos_clientes.sincronizado_en <= excluded.sincronizado_en;

-- name: ClientePorID :one
SELECT * FROM integracion.argos_clientes WHERE id = @id;

-- name: BorrarCliente :exec
DELETE FROM integracion.argos_clientes WHERE id = @id;

-- name: BuscarClientesLocal :many
-- Respaldo de la búsqueda cuando Argos no responde: solo lo que ya está en la instantánea.
SELECT * FROM integracion.argos_clientes
WHERE (nombre ILIKE '%' || @q::text || '%' OR codigo ILIKE '%' || @q::text || '%' OR telefono ILIKE '%' || @q::text || '%')
  AND (@empresa_id::bigint = 0 OR empresa_id = @empresa_id::bigint)
ORDER BY nombre, id
LIMIT @limite;

-- name: GuardarServicio :exec
INSERT INTO integracion.argos_servicios (
    id, empresa_id, cliente_id, codigo, plan_id, plan_nombre, router_id, router_nombre, zona_id, zona_nombre,
    ip, direccion, lat, lon, estado, sincronizado_en
) VALUES (
    @id, @empresa_id, @cliente_id, @codigo, @plan_id, @plan_nombre, @router_id, @router_nombre, @zona_id, @zona_nombre,
    @ip, @direccion, @lat, @lon, @estado, @sincronizado_en
)
ON CONFLICT (id) DO UPDATE SET
    empresa_id = excluded.empresa_id, cliente_id = excluded.cliente_id, codigo = excluded.codigo,
    plan_id = excluded.plan_id, plan_nombre = excluded.plan_nombre, router_id = excluded.router_id,
    router_nombre = excluded.router_nombre, zona_id = excluded.zona_id, zona_nombre = excluded.zona_nombre,
    ip = excluded.ip, direccion = excluded.direccion, lat = excluded.lat, lon = excluded.lon,
    estado = excluded.estado, sincronizado_en = excluded.sincronizado_en, updated_at = now()
WHERE integracion.argos_servicios.sincronizado_en <= excluded.sincronizado_en;

-- name: ServicioPorID :one
SELECT * FROM integracion.argos_servicios WHERE id = @id;

-- name: ServiciosDeCliente :many
SELECT * FROM integracion.argos_servicios WHERE cliente_id = @cliente_id ORDER BY id;

-- name: ServicioPorIP :one
-- La misma IP privada puede existir en dos empresas: con @empresa_id (≠ 0) solo la de esa empresa.
SELECT * FROM integracion.argos_servicios
WHERE ip = @ip AND (@empresa_id::bigint = 0 OR empresa_id = @empresa_id::bigint)
ORDER BY sincronizado_en DESC LIMIT 1;

-- name: BorrarServicio :exec
DELETE FROM integracion.argos_servicios WHERE id = @id;

-- name: BorrarServiciosAusentes :exec
-- Servicios que Argos ya no devuelve para el cliente (y que no se leyeron después).
DELETE FROM integracion.argos_servicios
WHERE cliente_id = @cliente_id AND NOT (id = ANY(@vigentes::bigint[])) AND sincronizado_en < @sincronizado_en;

-- name: GuardarZona :exec
INSERT INTO integracion.argos_zonas (id, empresa_id, nombre, sincronizado_en)
VALUES (@id, @empresa_id, @nombre, @sincronizado_en)
ON CONFLICT (id) DO UPDATE SET
    empresa_id = excluded.empresa_id, nombre = excluded.nombre, sincronizado_en = excluded.sincronizado_en,
    updated_at = now()
WHERE integracion.argos_zonas.sincronizado_en <= excluded.sincronizado_en;

-- name: BorrarZonasAnteriores :execrows
DELETE FROM integracion.argos_zonas WHERE sincronizado_en < @antes;

-- name: GuardarRouter :exec
INSERT INTO integracion.argos_routers (id, empresa_id, nombre, zona_id, lat, lon, sincronizado_en)
VALUES (@id, @empresa_id, @nombre, @zona_id, @lat, @lon, @sincronizado_en)
ON CONFLICT (id) DO UPDATE SET
    empresa_id = excluded.empresa_id, nombre = excluded.nombre, zona_id = excluded.zona_id,
    lat = excluded.lat, lon = excluded.lon, sincronizado_en = excluded.sincronizado_en, updated_at = now()
WHERE integracion.argos_routers.sincronizado_en <= excluded.sincronizado_en;

-- name: BorrarRoutersAnteriores :execrows
DELETE FROM integracion.argos_routers WHERE sincronizado_en < @antes;

-- name: RouterPorID :one
SELECT * FROM integracion.argos_routers WHERE id = @id;

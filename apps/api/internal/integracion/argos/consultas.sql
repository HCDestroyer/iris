-- Instantáneas (§2.2) -------------------------------------------------------------------

-- name: GuardarCliente :exec
INSERT INTO integracion.argos_clientes (
    id, empresa_id, empresas, sin_servicios, codigo, nombre, telefono, direccion, lat, lon, zona_id, estado, sincronizado_en
) VALUES (
    @id, @empresa_id, @empresas::bigint[], @sin_servicios, @codigo, @nombre, @telefono, @direccion, @lat, @lon, @zona_id, @estado, @sincronizado_en
)
ON CONFLICT (id) DO UPDATE SET
    empresa_id = excluded.empresa_id, empresas = excluded.empresas, sin_servicios = excluded.sin_servicios,
    codigo = excluded.codigo, nombre = excluded.nombre,
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
  -- Visible en la empresa si es una de las de sus servicios, o si Argos dijo que no tiene servicios.
  AND (@empresa_id::bigint = 0 OR @empresa_id::bigint = ANY(empresas) OR sin_servicios
       OR (cardinality(empresas) = 0 AND empresa_id = @empresa_id::bigint))
ORDER BY nombre, id
LIMIT @limite;

-- name: GuardarServicio :exec
-- Un servicio que Argos eliminó (lápida en argos_eliminados) no se recrea con una lectura atrasada.
INSERT INTO integracion.argos_servicios (
    id, empresa_id, cliente_id, codigo, plan_id, plan_nombre, router_id, router_nombre, zona_id, zona_nombre,
    ip, direccion, lat, lon, estado, sincronizado_en
)
SELECT @id, @empresa_id, @cliente_id, @codigo, @plan_id, @plan_nombre, @router_id, @router_nombre, @zona_id, @zona_nombre,
    @ip, @direccion, @lat, @lon, @estado, @sincronizado_en
WHERE NOT EXISTS (SELECT 1 FROM integracion.argos_eliminados e WHERE e.tipo = 'servicio' AND e.id = @id)
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
-- Un servicio sin empresa (0) no se atribuye a nadie.
WHERE ip = @ip AND empresa_id <> 0 AND (@empresa_id::bigint = 0 OR empresa_id = @empresa_id::bigint)
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
-- Un router que Argos eliminó (lápida) no vuelve con una carga de catálogos atrasada.
INSERT INTO integracion.argos_routers (id, empresa_id, nombre, zona_id, lat, lon, sincronizado_en)
SELECT @id, @empresa_id, @nombre, @zona_id, @lat, @lon, @sincronizado_en
WHERE NOT EXISTS (SELECT 1 FROM integracion.argos_eliminados e WHERE e.tipo = 'router' AND e.id = @id)
ON CONFLICT (id) DO UPDATE SET
    empresa_id = excluded.empresa_id, nombre = excluded.nombre, zona_id = excluded.zona_id,
    lat = excluded.lat, lon = excluded.lon, sincronizado_en = excluded.sincronizado_en, updated_at = now()
WHERE integracion.argos_routers.sincronizado_en <= excluded.sincronizado_en;

-- name: BorrarRoutersAnteriores :execrows
DELETE FROM integracion.argos_routers WHERE sincronizado_en < @antes;

-- name: RouterPorID :one
SELECT * FROM integracion.argos_routers WHERE id = @id;

-- name: BorrarRouter :exec
DELETE FROM integracion.argos_routers WHERE id = @id;

-- Eliminados en Argos (servicio.eliminado, router.eliminado; contrato 0.3.1) ---------------

-- name: RegistrarEliminado :execrows
-- La lápida se escribe una vez (un webhook repetido o un segundo aviso no la cambia).
INSERT INTO integracion.argos_eliminados (tipo, id, empresa_id, cliente_id, codigo, nombre, entrante_id, eliminado_en)
VALUES (@tipo, @id, @empresa_id, @cliente_id, @codigo, @nombre, @entrante_id, @eliminado_en)
ON CONFLICT (tipo, id) DO NOTHING;

-- name: Eliminado :one
SELECT * FROM integracion.argos_eliminados WHERE tipo = @tipo AND id = @id;

-- name: ServiciosEliminadosDeCliente :many
-- Con @empresa_id (≠ 0), solo los de esa empresa (un servicio sin empresa no se atribuye a nadie).
SELECT * FROM integracion.argos_eliminados
WHERE tipo = 'servicio' AND cliente_id = @cliente_id
  AND (@empresa_id::bigint = 0 OR empresa_id = @empresa_id::bigint)
ORDER BY id;

-- name: ServiciosEliminados :many
-- Servicios eliminados en Argos entre @ids (para marcar órdenes y tickets «eliminado en Argos»).
SELECT * FROM integracion.argos_eliminados
WHERE tipo = 'servicio' AND id = ANY(@ids::bigint[])
  AND (@empresa_id::bigint = 0 OR empresa_id = @empresa_id::bigint)
ORDER BY id;

-- name: PistaEmpresaAlta :one
-- Cliente recién creado desde un prospecto ganado (alta en Argos), todavía sin servicio: la
-- empresa del prospecto es la pista local hasta que exista el servicio.
SELECT empresa_id::bigint AS empresa_id FROM integracion.pendientes
WHERE operacion = 'crear_cliente' AND estado = 'hecho' AND empresa_id IS NOT NULL
  -- La respuesta guardada conserva la envoltura de Argos ({"data":{"id":N}}); el id puede venir
  -- como número o como texto.
  AND coalesce(respuesta->'data'->>'id', respuesta->>'id') = (@cliente_id::bigint)::text
ORDER BY id DESC LIMIT 1;

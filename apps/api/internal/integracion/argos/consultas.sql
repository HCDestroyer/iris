-- Conectores ----------------------------------------------------------------------------

-- name: SiguienteIDConector :one
-- El id se reserva antes de insertar: el cifrado ata el token y el secreto a su fila.
SELECT nextval(pg_get_serial_sequence('integracion.conectores', 'id'))::bigint AS id;

-- name: InsertarConector :one
INSERT INTO integracion.conectores (
    id, sistema, base_url, origen, token_cifrado, secreto_webhook_cifrado, remoto_id, empresas, creado_por
) VALUES (
    @id, @sistema, @base_url, @origen, @token_cifrado, @secreto_webhook_cifrado, @remoto_id, @empresas, @creado_por
)
RETURNING *;

-- name: ConectorPorID :one
SELECT * FROM integracion.conectores WHERE id = @id;

-- name: ConectorPorIDParaActualizar :one
SELECT * FROM integracion.conectores WHERE id = @id FOR UPDATE;

-- name: ConectorActivo :one
SELECT * FROM integracion.conectores WHERE sistema = @sistema AND estado = 'activo';

-- name: ListarConectores :many
SELECT * FROM integracion.conectores
WHERE id > @despues_de
ORDER BY id
LIMIT @limite;

-- name: CambiarEstadoConector :one
UPDATE integracion.conectores
SET estado       = @estado,
    activado_en  = CASE WHEN @estado = 'activo' THEN now() ELSE activado_en END,
    activado_por = CASE WHEN @estado = 'activo' THEN sqlc.narg(actor_id) ELSE activado_por END,
    version      = version + 1,
    updated_at   = now()
WHERE id = @id AND version = @version
RETURNING *;

-- name: TocarWebhookConector :exec
UPDATE integracion.conectores SET ultimo_webhook_en = now() WHERE id = @id;

-- Códigos de conexión -------------------------------------------------------------------

-- name: InsertarCodigoConexion :one
INSERT INTO integracion.codigos_conexion (sistema, codigo_hash, expira_en, creado_por)
VALUES (@sistema, @codigo_hash, @expira_en, @creado_por)
RETURNING *;

-- name: TomarCodigoConexion :one
-- Un solo uso: lo marca usado solo si estaba vigente.
UPDATE integracion.codigos_conexion
SET usado_en = now(), updated_at = now()
WHERE codigo_hash = @codigo_hash AND sistema = @sistema AND usado_en IS NULL AND expira_en > now()
RETURNING *;

-- name: VincularCodigoConexion :exec
UPDATE integracion.codigos_conexion SET conector_id = @conector_id, updated_at = now() WHERE id = @id;

-- Webhooks entrantes --------------------------------------------------------------------

-- name: InsertarEntrante :one
-- Sin fila (pgx.ErrNoRows) si el X-Webhook-Id ya llegó.
INSERT INTO integracion.entrantes (conector_id, webhook_id, evento, empresa_id, payload, ocurrido_en, request_id)
VALUES (@conector_id, @webhook_id, @evento, @empresa_id, @payload, @ocurrido_en, @request_id)
ON CONFLICT (conector_id, webhook_id) DO NOTHING
RETURNING *;

-- name: EntrantePorWebhook :one
SELECT * FROM integracion.entrantes WHERE conector_id = @conector_id AND webhook_id = @webhook_id;

-- name: EntrantePorID :one
SELECT * FROM integracion.entrantes WHERE id = @id;

-- name: EntranteParaProcesar :one
SELECT * FROM integracion.entrantes WHERE id = @id FOR UPDATE;

-- name: MarcarEntranteProcesado :exec
UPDATE integracion.entrantes
SET procesado_en = now(), resultado = @resultado, intentos = intentos + 1, error = NULL, updated_at = now()
WHERE id = @id;

-- name: MarcarEntranteError :exec
UPDATE integracion.entrantes
SET intentos = intentos + 1, error = @error, updated_at = now()
WHERE id = @id;

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
SELECT * FROM integracion.argos_servicios WHERE ip = @ip ORDER BY sincronizado_en DESC LIMIT 1;

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

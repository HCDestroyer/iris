-- Conectores (Argos y Horus) -------------------------------------------------------------

-- name: SiguienteIDConector :one
-- El id se reserva antes de insertar: el cifrado ata el token y el secreto a su fila.
SELECT nextval(pg_get_serial_sequence('integracion.conectores', 'id'))::bigint AS id;

-- name: InsertarConector :one
INSERT INTO integracion.conectores (
    id, sistema, base_url, origen, token_cifrado, secreto_webhook_cifrado, remoto_id, empresas, creado_por,
    token_entregado_id
) VALUES (
    @id, @sistema, @base_url, @origen, @token_cifrado, @secreto_webhook_cifrado, @remoto_id, @empresas, @creado_por,
    sqlc.narg(token_entregado_id)
)
RETURNING *;

-- name: TokensEntregadosDeOtros :many
-- Los iris_pat_ entregados por los demás conectores del sistema (se revocan al guardar uno nuevo).
SELECT token_entregado_id::bigint AS token_id FROM integracion.conectores
WHERE sistema = @sistema AND id <> @excepto AND token_entregado_id IS NOT NULL;

-- name: ExisteConectorConBase :one
-- El conector activo del sistema tiene ese origen (uno apagado o de un intento no cuenta).
SELECT EXISTS (SELECT 1 FROM integracion.conectores WHERE sistema = @sistema AND base_url = @base_url AND estado = 'activo')::boolean AS existe;

-- name: TokensEntregadosDeInactivos :many
-- Los iris_pat_ de los demás conectores del sistema que no están activos (intentos anteriores).
SELECT token_entregado_id::bigint AS token_id FROM integracion.conectores
WHERE sistema = @sistema AND id <> @excepto AND estado <> 'activo' AND token_entregado_id IS NOT NULL;

-- name: ConectorPorID :one
SELECT * FROM integracion.conectores WHERE id = @id;

-- name: ConectorPorIDParaActualizar :one
SELECT * FROM integracion.conectores WHERE id = @id FOR UPDATE;

-- name: ConectorActivo :one
SELECT * FROM integracion.conectores WHERE sistema = @sistema AND estado = 'activo';

-- name: ConectoresActivos :many
SELECT * FROM integracion.conectores WHERE estado = 'activo' ORDER BY sistema;

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

-- name: CambiarURLPublicaConector :one
UPDATE integracion.conectores
SET url_publica = sqlc.narg(url_publica), version = version + 1, updated_at = now()
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
-- Sin fila (pgx.ErrNoRows) si el id del webhook ya llegó.
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
-- nota: por qué se ignoró (payload inválido…); null si se aplicó.
UPDATE integracion.entrantes
SET procesado_en = now(), resultado = @resultado, intentos = intentos + 1, error = sqlc.narg(nota), updated_at = now()
WHERE id = @id;

-- name: MarcarEntranteError :exec
UPDATE integracion.entrantes
SET intentos = intentos + 1, error = @error, updated_at = now()
WHERE id = @id;

-- Empresas ------------------------------------------------------------------

-- name: RegistrarEmpresaConector :exec
-- Empresa que cubre el token del conector (canje). Si ya existe solo se marca sincronizada: el
-- nombre lo pone el administrador (Argos no lo manda en el canje).
INSERT INTO nucleo.empresas (id, nombre, origen, sincronizado_en)
VALUES (@id, @nombre, 'argos', now())
ON CONFLICT (id) DO UPDATE SET sincronizado_en = now(), updated_at = now();

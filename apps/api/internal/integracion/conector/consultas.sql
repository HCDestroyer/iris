-- Conectores (Argos y Horus) -------------------------------------------------------------

-- name: SiguienteIDConector :one
-- El id se reserva antes de insertar: el cifrado ata el token y el secreto a su fila.
SELECT nextval(pg_get_serial_sequence('integracion.conectores', 'id'))::bigint AS id;

-- name: InsertarConector :one
INSERT INTO integracion.conectores (
    id, sistema, base_url, origen, token_cifrado, secreto_webhook_cifrado, remoto_id, empresas, creado_por,
    token_entregado_id, alcance_global, panel_url, version_remota, contrato_remoto, contrato_informado, version_consultada_en
) VALUES (
    @id, @sistema, @base_url, @origen, @token_cifrado, @secreto_webhook_cifrado, @remoto_id, @empresas, @creado_por,
    sqlc.narg(token_entregado_id), sqlc.narg(alcance_global), sqlc.narg(panel_url), sqlc.narg(version_remota),
    sqlc.narg(contrato_remoto), sqlc.narg(contrato_informado),
    CASE WHEN sqlc.narg(version_remota)::text IS NULL AND sqlc.narg(contrato_remoto)::text IS NULL THEN NULL ELSE now() END
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
-- Los iris_pat_ de los demás conectores del sistema que no están activos (intentos anteriores). No
-- el de un conector reemplazado cuya reconexión no se confirmó (0.6.3): el otro sistema lo sigue usando.
SELECT c.token_entregado_id::bigint AS token_id FROM integracion.conectores c
WHERE c.sistema = @sistema AND c.id <> @excepto AND c.estado <> 'activo' AND c.token_entregado_id IS NOT NULL
  AND NOT EXISTS (SELECT 1 FROM integracion.conectores a WHERE a.estado = 'activo' AND a.reemplaza_token_id = c.token_entregado_id);

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

-- name: FijarVersionConector :one
-- Lo que informó el otro sistema (GET /integraciones/iris/estado, 0.5.0) y el contrato que Iris le
-- informó (PUT /integraciones/iris/contrato; null = no cambia). No sube la versión de la fila: no es
-- una edición del administrador. @sin_version: Argos ya no tiene GET …/estado (volvió a 1.5.x): la
-- versión y el contrato que informaba quedan en null (Iris vuelve a bodega_id). @sin_informado: Argos
-- no conoce PUT …/contrato (el contrato de Iris ya no está informado). @version_estado y
-- @version_detalle: el diagnóstico de esta lectura (ok, ruta_no_existe, sin_permiso, no_responde, error).
UPDATE integracion.conectores
SET version_remota = CASE WHEN @sin_version::boolean THEN NULL ELSE coalesce(sqlc.narg(version_remota), version_remota) END,
    contrato_remoto = CASE WHEN @sin_version::boolean THEN NULL ELSE coalesce(sqlc.narg(contrato_remoto), contrato_remoto) END,
    contrato_informado = CASE WHEN @sin_informado::boolean THEN NULL ELSE coalesce(sqlc.narg(contrato_informado), contrato_informado) END,
    version_estado = @version_estado::text, version_detalle = sqlc.narg(version_detalle),
    version_consultada_en = now(), updated_at = now()
WHERE id = @id
RETURNING *;

-- name: ConectoresSinContrato :many
-- Conectores activos del sistema a los que Iris aún no informó este contrato (job de una vez).
SELECT id FROM integracion.conectores
WHERE sistema = @sistema AND estado = 'activo' AND contrato_informado IS DISTINCT FROM @contrato::text
ORDER BY id;

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

-- Autorizaciones «Conectar con un botón» (contrato 0.4.0 §1.4) ----------------------------

-- name: SiguienteIDAutorizacion :one
-- El id se reserva antes de insertar: el cifrado ata el code_verifier a su fila.
SELECT nextval(pg_get_serial_sequence('integracion.autorizaciones', 'id'))::bigint AS id;

-- name: InsertarAutorizacion :one
INSERT INTO integracion.autorizaciones (id, sistema, state_hash, verificador_cifrado, panel_url, usuario_id, sesion_id, expira_en)
VALUES (@id, @sistema, @state_hash, @verificador_cifrado, @panel_url, @usuario_id, @sesion_id, @expira_en)
RETURNING *;

-- name: AutorizacionParaUsar :one
-- Bloqueada: dos vueltas con el mismo state se atienden de una en una (la segunda la ve usada).
SELECT * FROM integracion.autorizaciones WHERE state_hash = @state_hash AND sistema = @sistema FOR UPDATE;

-- name: UsarAutorizacion :exec
-- Un solo uso: el code_verifier se borra al usarla (ya viaja en el canje).
UPDATE integracion.autorizaciones
SET usado_en = now(), verificador_cifrado = NULL, updated_at = now()
WHERE id = @id;

-- name: AnularAutorizacionesDeSesion :many
-- Al iniciar otra en la misma sesión y sistema, las anteriores sin usar quedan gastadas y sin
-- code_verifier (solo vale la última).
UPDATE integracion.autorizaciones
SET usado_en = now(), verificador_cifrado = NULL, updated_at = now()
WHERE sesion_id = @sesion_id AND sistema = @sistema AND usado_en IS NULL
RETURNING id;

-- name: OlvidarVerificadoresVencidos :execrows
-- Purga (job periódico): una autorización abandonada no guarda su code_verifier después de vencer.
UPDATE integracion.autorizaciones
SET verificador_cifrado = NULL, updated_at = now()
WHERE verificador_cifrado IS NOT NULL AND expira_en < now();

-- name: BorrarAutorizacionesViejas :execrows
-- Purga: las autorizaciones vencidas hace más de un día ya no sirven ni para explicar un error.
DELETE FROM integracion.autorizaciones WHERE expira_en < now() - interval '1 day';

-- name: VincularAutorizacion :exec
UPDATE integracion.autorizaciones SET conector_id = @conector_id, updated_at = now() WHERE id = @id;

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

-- name: EmpresaCanjeParaActualizar :one
-- Empresa que informa el canje (bloqueada para completarla en la misma transacción).
SELECT * FROM nucleo.empresas WHERE id = @id FOR UPDATE;

-- name: CrearEmpresaCanje :one
-- Empresa nueva que cubre el token del conector (canje). Sin filas si otra transacción la creó.
INSERT INTO nucleo.empresas (id, nombre, nit, origen, sincronizado_en)
VALUES (@id, @nombre, @nit, 'argos', now())
ON CONFLICT (id) DO NOTHING
RETURNING *;

-- name: CompletarEmpresaCanje :one
-- Completa nombre o NIT de una empresa de origen argos (lo decide el código: solo el marcador
-- «Empresa <id>» y el NIT vacío).
UPDATE nucleo.empresas
SET nombre = @nombre, nit = @nit, version = version + 1, sincronizado_en = now(), updated_at = now()
WHERE id = @id AND origen = 'argos'
RETURNING *;

-- name: MarcarEmpresaSincronizada :exec
UPDATE nucleo.empresas SET sincronizado_en = now(), updated_at = now() WHERE id = @id AND origen = 'argos';

-- name: FijarWebhookSalida :exec
-- Receptor de webhooks del otro sistema que informó el canje (Iris → Argos, contrato 0.6.0 §6.6): URL
-- y secreto de entrada (cifrado, atado a la fila). No sube la versión: viene del canje.
UPDATE integracion.conectores
SET webhook_salida_url = @webhook_salida_url, webhook_salida_secreto_cifrado = @webhook_salida_secreto_cifrado, updated_at = now()
WHERE id = @id;

-- name: FijarReemplazo :exec
-- Reconexión sin corte (0.6.3): el conector recién activado reemplaza a otro, cuyo iris_pat_ sigue
-- vivo hasta que el otro sistema confirme el token nuevo (o 24 h). No sube la versión.
UPDATE integracion.conectores
SET reemplaza_conector_id = @reemplaza_conector_id, reemplaza_token_id = sqlc.narg(reemplaza_token_id), updated_at = now()
WHERE id = @id;

-- name: QuitarReemplazo :exec
UPDATE integracion.conectores SET reemplaza_conector_id = NULL, reemplaza_token_id = NULL, updated_at = now()
WHERE id = @id AND reemplaza_conector_id IS NOT NULL;

-- name: ConfirmarReemplazo :one
-- La primera llamada con el token del conector activo respondió 2xx: el reemplazo queda confirmado.
-- Sin fila si no estaba pendiente (o el conector ya no está activo).
WITH antes AS (
    SELECT x.id, x.reemplaza_conector_id, x.reemplaza_token_id FROM integracion.conectores x
    WHERE x.id = @conector_id AND x.estado = 'activo' AND x.reemplaza_conector_id IS NOT NULL
    FOR UPDATE
)
UPDATE integracion.conectores c
SET reemplaza_conector_id = NULL, reemplaza_token_id = NULL, updated_at = now()
FROM antes
WHERE c.id = antes.id
RETURNING antes.reemplaza_conector_id::bigint AS conector_viejo_id, antes.reemplaza_token_id AS token_id;

-- name: VencerReemplazo :one
-- 24 h sin confirmar: el reemplazo se da por cerrado. Devuelve el token a revocar y si el conector
-- anterior volvió a estar activo (entonces no se revoca). Sin fila si ya se confirmó o se quitó.
WITH antes AS (
    SELECT x.id, x.reemplaza_token_id FROM integracion.conectores x
    WHERE x.id = @conector_id AND x.reemplaza_conector_id = @conector_viejo_id
    FOR UPDATE
)
UPDATE integracion.conectores c
SET reemplaza_conector_id = NULL, reemplaza_token_id = NULL, updated_at = now()
FROM antes
WHERE c.id = antes.id
RETURNING antes.reemplaza_token_id AS token_id,
    (SELECT v.estado = 'activo' FROM integracion.conectores v WHERE v.id = @conector_viejo_id)::boolean AS viejo_activo;

-- name: ConectorQueReemplaza :one
-- El conector activo del sistema que reemplaza a ese y todavía no está confirmado (sus webhooks
-- firmados con el secreto anterior se aceptan en el activo).
SELECT * FROM integracion.conectores WHERE sistema = @sistema AND estado = 'activo' AND reemplaza_conector_id = @id;

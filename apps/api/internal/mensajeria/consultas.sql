-- Mensajería (contrato Iris ↔ ArgosISP 0.6.0 §6). sqlc: mensajeriabd.

-- Canales -------------------------------------------------------------------------------------

-- name: CanalActivoDeEmpresa :one
SELECT * FROM mensajeria.canales
WHERE coalesce(empresa_id, 0) = @empresa_id::bigint AND canal = @canal AND activo;

-- name: CanalPorID :one
SELECT * FROM mensajeria.canales WHERE id = $1;

-- name: CanalPorIDParaActualizar :one
SELECT * FROM mensajeria.canales WHERE id = $1 FOR UPDATE;

-- name: ListarCanales :many
SELECT * FROM mensajeria.canales
WHERE (@todas::boolean OR coalesce(empresa_id, 0) = ANY(@empresas::bigint[]))
ORDER BY id;

-- name: CanalesActivosDeEmpresas :many
SELECT * FROM mensajeria.canales
WHERE activo AND (@todas::boolean OR coalesce(empresa_id, 0) = ANY(@empresas::bigint[]))
ORDER BY id;

-- name: SiguienteIDCanal :one
SELECT nextval(pg_get_serial_sequence('mensajeria.canales', 'id'))::bigint;

-- name: InsertarCanal :one
INSERT INTO mensajeria.canales (id, empresa_id, tipo, canal, nombre, numero, phone_number_id, waba_id,
    credenciales_cifradas, plantillas, creado_por)
OVERRIDING SYSTEM VALUE
VALUES (@id, @empresa_id, @tipo, @canal, @nombre, @numero, @phone_number_id, @waba_id,
    @credenciales_cifradas, @plantillas, @creado_por)
RETURNING *;

-- name: ActualizarCanal :one
UPDATE mensajeria.canales
SET nombre = @nombre, numero = @numero, phone_number_id = @phone_number_id, waba_id = @waba_id,
    credenciales_cifradas = @credenciales_cifradas, plantillas = @plantillas,
    version = version + 1, updated_at = now()
WHERE id = @id
RETURNING *;

-- name: FijarCanalActivo :one
UPDATE mensajeria.canales SET activo = @activo, version = version + 1, updated_at = now()
WHERE id = @id
RETURNING *;

-- name: PausarCanal :exec
UPDATE mensajeria.canales SET pausado_hasta = @pausado_hasta, updated_at = now() WHERE id = @id;

-- Consentimientos -----------------------------------------------------------------------------

-- name: ConsentimientoPorClave :one
SELECT * FROM mensajeria.consentimientos WHERE registrado_por = @registrado_por::bigint AND idempotency_key = @idempotency_key::text;

-- name: ConsentimientoPorHecho :one
SELECT * FROM mensajeria.consentimientos
WHERE empresa_id = @empresa_id AND cliente_id = @cliente_id AND canal = @canal
  AND ocurrido_en = @ocurrido_en AND estado = @estado AND origen = @origen;

-- name: InsertarConsentimiento :one
INSERT INTO mensajeria.consentimientos (empresa_id, cliente_id, canal, estado, origen, ocurrido_en,
    registrado_por, idempotency_key, hash_peticion, entrante_id)
VALUES (@empresa_id, @cliente_id, @canal, @estado, @origen, @ocurrido_en, @registrado_por,
    @idempotency_key, @hash_peticion, @entrante_id)
ON CONFLICT DO NOTHING
RETURNING *;

-- name: BloquearConsentimientos :exec
-- Serializa los registros de un mismo cliente y canal (el vigente se calcula después).
SELECT pg_advisory_xact_lock(hashtextextended('mensajeria.consentimiento:' || @empresa_id::bigint || ':' || @cliente_id::bigint || ':' || @canal::text, 0));

-- name: ConsentimientoVigente :one
-- El de ocurrido_en más reciente; a igual ocurrido_en, la baja manda.
SELECT * FROM mensajeria.consentimientos
WHERE empresa_id = @empresa_id AND cliente_id = @cliente_id AND canal = @canal
ORDER BY ocurrido_en DESC, (estado = 'baja') DESC, id DESC
LIMIT 1;

-- Avisos --------------------------------------------------------------------------------------

-- name: AvisoPorClave :one
SELECT * FROM mensajeria.avisos WHERE usuario_id = @usuario_id::bigint AND idempotency_key = @idempotency_key::text;

-- name: AvisoPorID :one
SELECT * FROM mensajeria.avisos WHERE id = $1;

-- name: AvisoParaActualizar :one
SELECT * FROM mensajeria.avisos WHERE id = $1 FOR UPDATE;

-- name: AvisoPorProveedor :one
SELECT * FROM mensajeria.avisos WHERE canal_id = @canal_id AND proveedor_id = @proveedor_id FOR UPDATE;

-- name: InsertarAviso :one
INSERT INTO mensajeria.avisos (id, empresa_id, cliente_id, servicio_id, plantilla, variables, canal_preferido,
    prioridad, origen, estado, motivo, canal, canal_id, remitente, destino, usuario_id, token_id,
    idempotency_key, hash_peticion, vence_en, programado_en, finalizado_en, created_at)
VALUES (@id, @empresa_id, @cliente_id, @servicio_id, @plantilla, @variables, @canal_preferido,
    @prioridad, @origen, @estado, @motivo, @canal, @canal_id, @remitente, @destino, @usuario_id, @token_id,
    @idempotency_key, @hash_peticion, @vence_en, @programado_en, @finalizado_en, @creado_en)
ON CONFLICT DO NOTHING
RETURNING *;

-- name: BloquearAvisosCliente :exec
-- Serializa la cuenta del límite diario de un cliente.
SELECT pg_advisory_xact_lock(hashtextextended('mensajeria.aviso:' || @empresa_id::bigint || ':' || @cliente_id::bigint, 0));

-- name: ContarAvisosDelDia :one
-- Avisos automáticos del cliente desde el inicio del día (los bloqueados no cuentan).
SELECT count(*)::bigint FROM mensajeria.avisos
WHERE empresa_id = @empresa_id AND cliente_id = @cliente_id AND created_at >= @desde AND estado <> 'bloqueado';

-- name: BloquearRemitente :exec
SELECT pg_advisory_xact_lock(hashtextextended('mensajeria.remitente:' || @remitente::text, 0));

-- name: EnviadosRecientes :many
-- Envíos del remitente en la última ventana (límite por minuto), del más viejo al más nuevo.
SELECT enviado_en::timestamptz FROM mensajeria.avisos
WHERE remitente = @remitente::text AND enviado_en > @desde::timestamptz
ORDER BY enviado_en;

-- name: MarcarEnviado :one
UPDATE mensajeria.avisos
SET proveedor_id = @proveedor_id, enviado_en = @enviado_en, intentos = intentos + 1, updated_at = now()
WHERE id = @id
RETURNING *;

-- name: ReservarEnvio :exec
-- El envío está en curso (cuenta para el límite por minuto del remitente).
UPDATE mensajeria.avisos SET enviado_en = @enviado_en, updated_at = now() WHERE id = @id;

-- name: ContarIntento :exec
-- Un intento que el canal no aceptó: libera la reserva.
UPDATE mensajeria.avisos SET intentos = intentos + 1, enviado_en = NULL, updated_at = now() WHERE id = @id;

-- name: MarcarEntregado :one
UPDATE mensajeria.avisos
SET estado = 'entregado', entregado_en = @entregado_en, leido_en = coalesce(leido_en, @leido_en),
    finalizado_en = now(), updated_at = now()
WHERE id = @id
RETURNING *;

-- name: MarcarLeido :one
UPDATE mensajeria.avisos SET leido_en = @leido_en, updated_at = now() WHERE id = @id RETURNING *;

-- name: MarcarFallido :one
UPDATE mensajeria.avisos
SET estado = 'fallido', motivo = @motivo, detalle = @detalle, canal = coalesce(canal, @canal),
    finalizado_en = now(), updated_at = now()
WHERE id = @id
RETURNING *;

-- name: AvisosSinConfirmar :many
-- En cola desde antes de @antes (sin acuse del canal): se dan por agotados.
SELECT id FROM mensajeria.avisos WHERE estado = 'en_cola' AND created_at < @antes ORDER BY created_at LIMIT 200;

-- name: ClientesAvisadosEn :many
-- Clientes a los que se avisó desde un canal a un número (bajas por chat).
SELECT DISTINCT empresa_id, cliente_id FROM mensajeria.avisos
WHERE canal_id = @canal_id AND destino = @destino
ORDER BY empresa_id, cliente_id;

-- Entrantes (Meta) ---------------------------------------------------------------------------

-- name: InsertarEntrante :one
INSERT INTO mensajeria.entrantes (canal_id, hash_cuerpo, payload)
VALUES (@canal_id, @hash_cuerpo, @payload)
ON CONFLICT (canal_id, hash_cuerpo) DO NOTHING
RETURNING id;

-- name: EntranteParaProcesar :one
SELECT * FROM mensajeria.entrantes WHERE id = $1 FOR UPDATE;

-- name: MarcarEntrante :exec
UPDATE mensajeria.entrantes SET procesado_en = @procesado_en, error = @error, intentos = intentos + 1 WHERE id = @id;

-- name: EntrantesPendientes :many
SELECT id FROM mensajeria.entrantes WHERE procesado_en IS NULL AND created_at < @antes ORDER BY id LIMIT 200;

-- Webhooks hacia Argos -------------------------------------------------------------------------

-- name: InsertarWebhookSalida :exec
INSERT INTO mensajeria.webhooks_salida (id, evento, empresa_id, payload, ocurrido_en)
VALUES (@id, @evento, @empresa_id, @payload, @ocurrido_en)
ON CONFLICT (id) DO NOTHING;

-- name: WebhooksPorEnviar :many
SELECT * FROM mensajeria.webhooks_salida
WHERE estado = 'pendiente' AND proximo_intento <= @ahora
ORDER BY proximo_intento
LIMIT @limite
FOR UPDATE SKIP LOCKED;

-- name: ReservarWebhook :exec
-- Un despacho lo toma: nadie más lo intenta en los próximos 5 min (si el proceso cae, se retoma).
UPDATE mensajeria.webhooks_salida SET proximo_intento = now() + interval '5 minutes', updated_at = now() WHERE id = @id;

-- name: WebhooksVencidos :many
SELECT id FROM mensajeria.webhooks_salida
WHERE estado = 'pendiente' AND proximo_intento <= @ahora
ORDER BY proximo_intento
LIMIT @limite;

-- name: WebhookParaEnviar :one
SELECT * FROM mensajeria.webhooks_salida WHERE id = $1 AND estado = 'pendiente' FOR UPDATE SKIP LOCKED;

-- name: ResultadoWebhook :exec
UPDATE mensajeria.webhooks_salida
SET estado = @estado, intentos = intentos + 1, proximo_intento = @proximo_intento, ultimo_error = @ultimo_error,
    conector_id = @conector_id, entregado_en = @entregado_en, updated_at = now()
WHERE id = @id;

-- name: WebhookPorID :one
SELECT * FROM mensajeria.webhooks_salida WHERE id = $1;

-- Datos de otros módulos (solo lectura) --------------------------------------------------------

-- name: ConectorArgosParaWebhooks :one
-- El conector Argos activo con su receptor (§6.6).
SELECT id, contrato_remoto, webhook_salida_url, webhook_salida_secreto_cifrado
FROM integracion.conectores WHERE sistema = 'argosisp' AND estado = 'activo';

-- Conexión con Meta, WhatsApp por QR (coexistencia) y préstamo de números
-- (contratos/conversaciones.md §15). sqlc: mensajeriabd.

-- Conexión con Meta ------------------------------------------------------------------------------

-- name: ConexionMeta :one
SELECT * FROM mensajeria.conexion_meta WHERE id = 1;

-- name: ConexionMetaParaActualizar :one
SELECT * FROM mensajeria.conexion_meta WHERE id = 1 FOR UPDATE;

-- name: GuardarConexionMeta :one
-- Alta (version 1) o edición (version + 1) de la única fila.
INSERT INTO mensajeria.conexion_meta (id, modo, app_id, config_id, graph_version, servicio_url, secretos_cifrados,
    app_secret_guardado_en, clave_enlace_guardada_en, verify_token_guardado_en, webhook_verificado_en,
    ultima_prueba_en, ultimo_estado_prueba, comprobaciones, actualizado_por)
VALUES (1, @modo, sqlc.narg('app_id'), sqlc.narg('config_id'), sqlc.narg('graph_version'), sqlc.narg('servicio_url'), @secretos_cifrados,
    sqlc.narg('app_secret_guardado_en'), sqlc.narg('clave_enlace_guardada_en'), sqlc.narg('verify_token_guardado_en'),
    sqlc.narg('webhook_verificado_en'), sqlc.narg('ultima_prueba_en'), sqlc.narg('ultimo_estado_prueba'), @comprobaciones,
    sqlc.narg('actualizado_por'))
ON CONFLICT (id) DO UPDATE SET modo = EXCLUDED.modo, app_id = EXCLUDED.app_id, config_id = EXCLUDED.config_id,
    graph_version = EXCLUDED.graph_version, servicio_url = EXCLUDED.servicio_url, secretos_cifrados = EXCLUDED.secretos_cifrados,
    app_secret_guardado_en = EXCLUDED.app_secret_guardado_en, clave_enlace_guardada_en = EXCLUDED.clave_enlace_guardada_en,
    verify_token_guardado_en = EXCLUDED.verify_token_guardado_en, webhook_verificado_en = EXCLUDED.webhook_verificado_en,
    ultima_prueba_en = EXCLUDED.ultima_prueba_en, ultimo_estado_prueba = EXCLUDED.ultimo_estado_prueba,
    comprobaciones = EXCLUDED.comprobaciones, actualizado_por = EXCLUDED.actualizado_por,
    version = mensajeria.conexion_meta.version + 1, updated_at = now()
RETURNING *;

-- name: RegistrarPruebaConexionMeta :one
-- Resultado de probar (no sube version: no es una edición).
UPDATE mensajeria.conexion_meta
SET ultima_prueba_en = @probado_en, ultimo_estado_prueba = @estado, comprobaciones = @comprobaciones
WHERE id = 1
RETURNING *;

-- name: MarcarConexionWebhookVerificado :exec
UPDATE mensajeria.conexion_meta SET webhook_verificado_en = @verificado_en WHERE id = 1;

-- name: MarcarConexionWebhookRecibido :exec
UPDATE mensajeria.conexion_meta SET ultimo_webhook_en = sqlc.arg(recibido_en)::timestamptz
WHERE id = 1 AND (ultimo_webhook_en IS NULL OR ultimo_webhook_en < sqlc.arg(recibido_en)::timestamptz - interval '1 minute');

-- Números por QR -----------------------------------------------------------------------------------

-- name: InsertarRegistroQR :execrows
-- El código de Embedded Signup se canjea una sola vez (0 filas = ya se usó).
INSERT INTO mensajeria.registros_qr (hash_codigo, empresa_id, usuario_id) VALUES (@hash_codigo, sqlc.narg('empresa_id'), sqlc.narg('usuario_id'))
ON CONFLICT DO NOTHING;

-- name: CanalQRDeEmpresa :one
-- El número QR de la empresa con ese phone_number_id (para reconectarlo).
SELECT * FROM mensajeria.canales
WHERE coalesce(empresa_id, 0) = @empresa::bigint AND phone_number_id = @phone_number_id AND tipo_conexion = 'qr'
ORDER BY activo DESC, id DESC
LIMIT 1
FOR UPDATE;

-- name: HayOtroCanalQRActivo :one
SELECT EXISTS (SELECT 1 FROM mensajeria.canales WHERE tipo_conexion = 'qr' AND activo AND desconectado_en IS NULL AND id <> @excepto::bigint)::boolean;

-- name: InsertarCanalQR :one
INSERT INTO mensajeria.canales (id, empresa_id, tipo, canal, nombre, numero, phone_number_id, waba_id,
    credenciales_cifradas, plantillas, creado_por, usos, avisos_empresa, cola_id,
    token_guardado_en, verify_token_guardado_en, tipo_conexion, coexistencia, conectado_en, sincronizacion)
OVERRIDING SYSTEM VALUE
VALUES (@id, sqlc.narg('empresa_id'), 'whatsapp_cloud', 'whatsapp', @nombre, @numero, @phone_number_id, @waba_id,
    @credenciales_cifradas, '{}', sqlc.narg('creado_por'), @usos::text[], @avisos_empresa, sqlc.narg('cola_id'),
    @conectado_en, @conectado_en, 'qr', true, @conectado_en, @sincronizacion)
RETURNING *;

-- name: ReconectarCanalQR :one
-- «Reconectar»: el mismo número con un token nuevo; la sincronización empieza de cero.
UPDATE mensajeria.canales
SET numero = @numero, waba_id = @waba_id, credenciales_cifradas = @credenciales_cifradas, activo = true,
    conectado_en = @conectado_en, desconectado_en = NULL, desconectado_motivo = NULL,
    token_guardado_en = @conectado_en, verify_token_guardado_en = @conectado_en, webhook_verificado_en = NULL,
    ultimo_estado = NULL, ultimo_detalle = NULL, ultima_prueba_en = NULL, sincronizacion = @sincronizacion,
    version = version + 1, updated_at = now()
WHERE id = @id
RETURNING *;

-- name: CanalesQRPorPhoneNumber :many
-- Para repartir el webhook de la app: los números QR con ese phone_number_id (el activo primero).
SELECT * FROM mensajeria.canales WHERE phone_number_id = @phone_number_id AND tipo_conexion = 'qr'
ORDER BY activo DESC, id DESC;

-- name: CanalesQRPorWaba :many
SELECT * FROM mensajeria.canales WHERE waba_id = @waba_id AND waba_id <> '' AND tipo_conexion = 'qr'
ORDER BY activo DESC, id DESC;

-- name: GuardarSincronizacion :one
-- Estado de la sincronización de contactos e historial (no sube version).
UPDATE mensajeria.canales SET sincronizacion = @sincronizacion WHERE id = @id RETURNING *;

-- name: CanalParaSincronizar :one
SELECT * FROM mensajeria.canales WHERE id = @id FOR UPDATE;

-- name: MarcarEcoTelefono :exec
-- Último mensaje mandado desde el teléfono (precisión de 1 minuto).
UPDATE mensajeria.canales SET ultimo_eco_en = sqlc.arg(eco_en)::timestamptz
WHERE id = @id AND (ultimo_eco_en IS NULL OR ultimo_eco_en < sqlc.arg(eco_en)::timestamptz - interval '1 minute');

-- name: DesconectarCanal :one
UPDATE mensajeria.canales SET desconectado_en = @desconectado_en, desconectado_motivo = @motivo, updated_at = now()
WHERE id = @id
RETURNING *;

-- name: QuitarDesconexionCanal :one
UPDATE mensajeria.canales SET desconectado_en = NULL, desconectado_motivo = NULL, updated_at = now()
WHERE id = @id
RETURNING *;

-- Préstamos ------------------------------------------------------------------------------------------

-- name: PrestamosDeCanal :many
SELECT p.*, e.nombre AS empresa_nombre, co.nombre AS cola_nombre,
    (SELECT count(*) FROM mensajeria.conversaciones c
     WHERE c.canal_id = p.canal_id AND c.empresa_id = p.empresa_id AND c.estado <> 'resuelta')::bigint AS conversaciones_abiertas,
    (SELECT count(*) FROM mensajeria.avisos a
     WHERE a.canal_id = p.canal_id AND a.empresa_id = p.empresa_id AND a.estado = 'en_cola' AND a.enviado_en IS NULL)::bigint AS avisos_programados
FROM mensajeria.canal_prestamos p
LEFT JOIN nucleo.empresas e ON e.id = p.empresa_id
LEFT JOIN mensajeria.colas co ON co.id = p.cola_id
WHERE p.canal_id = @canal_id
ORDER BY NOT p.activo, lower(coalesce(e.nombre, '')), p.empresa_id;

-- name: PrestamoParaActualizar :one
SELECT * FROM mensajeria.canal_prestamos WHERE canal_id = @canal_id AND empresa_id = @empresa_id FOR UPDATE;

-- name: PrestamoActivo :one
SELECT * FROM mensajeria.canal_prestamos WHERE canal_id = @canal_id AND empresa_id = @empresa_id AND activo;

-- name: HayPrestamos :one
-- El número está (o estuvo) compartido: la regla de a qué empresa va un mensaje aplica.
SELECT EXISTS (SELECT 1 FROM mensajeria.canal_prestamos WHERE canal_id = @canal_id)::boolean;

-- name: PrestamosActivosDeCanal :many
SELECT * FROM mensajeria.canal_prestamos WHERE canal_id = @canal_id AND activo ORDER BY empresa_id;

-- name: GuardarPrestamo :one
INSERT INTO mensajeria.canal_prestamos (canal_id, empresa_id, usos, cola_id, avisos_empresa, activo, prestado_en, prestado_por)
VALUES (@canal_id, @empresa_id, @usos::text[], sqlc.narg('cola_id'), false, true, @prestado_en, sqlc.narg('prestado_por'))
ON CONFLICT (canal_id, empresa_id) DO UPDATE SET usos = EXCLUDED.usos, cola_id = EXCLUDED.cola_id,
    avisos_empresa = mensajeria.canal_prestamos.avisos_empresa AND mensajeria.canal_prestamos.activo AND 'avisos' = ANY (EXCLUDED.usos),
    prestado_en = CASE WHEN mensajeria.canal_prestamos.activo THEN mensajeria.canal_prestamos.prestado_en ELSE EXCLUDED.prestado_en END,
    prestado_por = CASE WHEN mensajeria.canal_prestamos.activo THEN mensajeria.canal_prestamos.prestado_por ELSE EXCLUDED.prestado_por END,
    activo = true, revocado_en = NULL, revocado_por = NULL, motivo_revocacion = NULL,
    version = mensajeria.canal_prestamos.version + 1, updated_at = now()
RETURNING *;

-- name: RevocarPrestamo :one
UPDATE mensajeria.canal_prestamos
SET activo = false, avisos_empresa = false, revocado_en = @revocado_en, revocado_por = sqlc.narg('revocado_por'),
    motivo_revocacion = @motivo, version = version + 1, updated_at = now()
WHERE canal_id = @canal_id AND empresa_id = @empresa_id
RETURNING *;

-- name: FijarAvisosPrestamo :one
UPDATE mensajeria.canal_prestamos SET avisos_empresa = @avisos_empresa, version = version + 1, updated_at = now()
WHERE canal_id = @canal_id AND empresa_id = @empresa_id
RETURNING *;

-- name: QuitarAvisosPrestados :many
-- La empresa marca otro número de avisos: lo quita de sus préstamos.
UPDATE mensajeria.canal_prestamos SET avisos_empresa = false, version = version + 1, updated_at = now()
WHERE empresa_id = @empresa_id AND avisos_empresa AND canal_id <> @excepto::bigint
RETURNING canal_id;

-- name: CanalAvisosPrestado :one
-- El número prestado por el que salen los avisos de la empresa (con el uso avisos).
SELECT ca.* FROM mensajeria.canal_prestamos p JOIN mensajeria.canales ca ON ca.id = p.canal_id
WHERE p.empresa_id = @empresa_id AND p.activo AND p.avisos_empresa AND 'avisos' = ANY (p.usos)
  AND ca.canal = @canal AND ca.activo AND ca.desconectado_en IS NULL;

-- name: CanalesPrestadosAEmpresa :many
-- Números de otras empresas que esta usa (préstamo activo), con el nombre de la dueña.
SELECT sqlc.embed(ca), sqlc.embed(p), coalesce(e.nombre, '')::text AS dueno_nombre
FROM mensajeria.canal_prestamos p
JOIN mensajeria.canales ca ON ca.id = p.canal_id
LEFT JOIN nucleo.empresas e ON e.id = ca.empresa_id
WHERE p.empresa_id = @empresa_id AND p.activo
ORDER BY NOT ca.activo, lower(ca.nombre), ca.id;

-- name: CompartidosPorCanal :many
SELECT canal_id, count(*)::bigint AS total FROM mensajeria.canal_prestamos
WHERE canal_id = ANY (@canales::bigint[]) AND activo
GROUP BY canal_id;

-- name: NombreEmpresa :one
SELECT nombre FROM nucleo.empresas WHERE id = @id;

-- name: OtrasEmpresas :many
SELECT id, nombre FROM nucleo.empresas WHERE id <> @dueno::bigint ORDER BY lower(nombre), id;

-- name: ColasActivasDeEmpresas :many
SELECT id, empresa_id, nombre FROM mensajeria.colas WHERE empresa_id = ANY (@empresas::bigint[]) AND activa ORDER BY lower(nombre), id;

-- name: ConversacionesNoResueltasDePrestamo :many
SELECT * FROM mensajeria.conversaciones
WHERE canal_id = @canal_id AND empresa_id = @empresa_id AND estado <> 'resuelta'
ORDER BY id
FOR UPDATE;

-- name: AvisosEnColaDePrestamo :many
SELECT * FROM mensajeria.avisos
WHERE canal_id = @canal_id AND empresa_id = @empresa_id AND estado = 'en_cola' AND enviado_en IS NULL
ORDER BY id
FOR UPDATE;

-- A qué empresa va un mensaje (§15.3) ---------------------------------------------------------------

-- name: ConversacionesNoResueltasDeNumero :many
SELECT id, empresa_id, ultima_actividad FROM mensajeria.conversaciones
WHERE canal_id = @canal_id AND numero = @numero AND estado <> 'resuelta'
ORDER BY ultima_actividad DESC, id DESC;

-- name: IniciosRecientes :many
-- Empresas que empezaron una conversación con ese número por ese canal: avisos enviados y
-- plantillas mandadas por una persona (iniciar), desde @desde, la más reciente primero.
SELECT x.empresa_id::bigint AS empresa_id, x.cuando::timestamptz AS cuando FROM (
    SELECT a.empresa_id, coalesce(a.enviado_en, a.created_at) AS cuando
    FROM mensajeria.avisos a
    WHERE a.canal_id = @canal_id AND a.destino = @numero AND a.estado IN ('en_cola', 'entregado') AND a.enviado_en IS NOT NULL
      AND a.enviado_en >= @desde
    UNION ALL
    SELECT c.empresa_id, m.creado_en AS cuando
    FROM mensajeria.conversacion_mensajes m JOIN mensajeria.conversaciones c ON c.id = m.conversacion_id
    WHERE c.canal_id = @canal_id AND c.numero = @numero AND m.direccion = 'saliente' AND m.plantilla IS NOT NULL
      AND m.autor_id IS NOT NULL AND m.creado_en >= @desde AND c.empresa_id IS NOT NULL
) x
ORDER BY x.cuando DESC
LIMIT 20;

-- name: HayContactoIris :one
SELECT EXISTS (SELECT 1 FROM mensajeria.contactos_iris WHERE coalesce(empresa_id, 0) = @empresa::bigint AND telefono = @telefono)::boolean;

-- Pasar una conversación a otra empresa ---------------------------------------------------------------

-- name: ConversacionTieneVinculos :one
SELECT (EXISTS (SELECT 1 FROM mensajeria.conversacion_vinculos v WHERE v.conversacion_id = sqlc.arg(id)::bigint)
     OR EXISTS (SELECT 1 FROM mensajeria.boletas b WHERE b.conversacion_id = sqlc.arg(id)::bigint)
     OR EXISTS (SELECT 1 FROM mensajeria.promesas pr WHERE pr.conversacion_id = sqlc.arg(id)::bigint))::boolean;

-- name: MoverConversacionAEmpresa :one
UPDATE mensajeria.conversaciones
SET empresa_id = @empresa_id, cola_id = sqlc.narg('cola_id'), asignado_a = NULL, asignado_en = NULL,
    cliente_id = NULL, servicio_id = NULL, contacto_iris_id = NULL, prospecto_id = NULL, asociacion = NULL,
    cliente_verificado = false, verificado_por = NULL, sla_avisado = false,
    sla_vence_en = NULL, fuera_horario_hasta = NULL, guardia_hasta = NULL,
    bot_espera_en = NULL, bot_vence_en = NULL, bot_motivo = NULL, bot_atendio_en = NULL, bot_menu = NULL,
    version = version + 1, updated_at = now()
WHERE id = @id
RETURNING *;

-- name: MoverMensajesAEmpresa :exec
UPDATE mensajeria.conversacion_mensajes SET empresa_id = @empresa_id WHERE conversacion_id = @conversacion_id;

-- name: QuitarEtiquetasConversacion :exec
DELETE FROM mensajeria.conversacion_etiquetas WHERE conversacion_id = @conversacion_id;

-- name: QuitarParticipantesConversacion :exec
DELETE FROM mensajeria.conversacion_participantes WHERE conversacion_id = @conversacion_id;

-- name: QuitarLecturasConversacion :exec
DELETE FROM mensajeria.conversacion_lecturas WHERE conversacion_id = @conversacion_id;

-- Historial y contactos del teléfono -----------------------------------------------------------------

-- name: InsertarConversacionHistorial :one
-- Una conversación que solo trae historial: nace resuelta.
INSERT INTO mensajeria.conversaciones (empresa_id, canal_id, numero, perfil_nombre, cola_id, cliente_id, asociacion, cliente_verificado,
    ultima_actividad, estado, resuelta_en)
VALUES (sqlc.narg('empresa_id'), @canal_id, @numero, sqlc.narg('perfil_nombre'), sqlc.narg('cola_id'), sqlc.narg('cliente_id'),
    sqlc.narg('asociacion'), @cliente_verificado, @ultima_actividad, 'resuelta', @resuelta_en)
ON CONFLICT DO NOTHING
RETURNING *;

-- name: FijarUltimaActividad :exec
UPDATE mensajeria.conversaciones SET ultima_actividad = @ultima_actividad WHERE id = @id;

-- name: RenombrarContactoIris :one
UPDATE mensajeria.contactos_iris SET nombre = @nombre, version = version + 1, updated_at = now() WHERE id = @id RETURNING *;

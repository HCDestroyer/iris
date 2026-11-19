-- Rotación de IRIS_CLAVE_CIFRADO (`iris clave rotar`): lee con candado cada columna cifrada y
-- la vuelve a escribir cifrada con la clave nueva. No sube `version` ni `updated_at`: el valor
-- descifrado no cambia (lo que cambia es la clave), y la acción queda en auditoria.acciones.

-- name: TOTPCifrados :many
SELECT id, totp_secreto FROM nucleo.usuarios
WHERE totp_secreto IS NOT NULL
ORDER BY id
FOR UPDATE;

-- name: RecifrarTOTP :exec
UPDATE nucleo.usuarios SET totp_secreto = @totp_secreto WHERE id = @id;

-- name: ConectoresCifrados :many
SELECT id, token_cifrado, secreto_webhook_cifrado FROM integracion.conectores
ORDER BY id
FOR UPDATE;

-- name: RecifrarConector :exec
UPDATE integracion.conectores
SET token_cifrado = @token_cifrado, secreto_webhook_cifrado = @secreto_webhook_cifrado
WHERE id = @id;

-- name: BuzonesCifrados :many
SELECT id, imap_contrasena, smtp_contrasena FROM soporte.buzones
WHERE imap_contrasena IS NOT NULL OR smtp_contrasena IS NOT NULL
ORDER BY id
FOR UPDATE;

-- name: RecifrarBuzon :exec
UPDATE soporte.buzones
SET imap_contrasena = @imap_contrasena, smtp_contrasena = @smtp_contrasena
WHERE id = @id;

-- name: AutorizacionesCifradas :many
-- code_verifier de las autorizaciones «Conectar con un botón» sin usar (al usarse queda NULL).
SELECT id, verificador_cifrado FROM integracion.autorizaciones
WHERE verificador_cifrado IS NOT NULL
ORDER BY id
FOR UPDATE;

-- name: RecifrarAutorizacion :exec
UPDATE integracion.autorizaciones SET verificador_cifrado = @verificador_cifrado WHERE id = @id;

-- name: WebhooksSalidaCifrados :many
-- Secreto del receptor de webhooks de Argos (Iris → Argos, contrato 0.6.0 §6.6).
SELECT id, webhook_salida_secreto_cifrado FROM integracion.conectores
WHERE webhook_salida_secreto_cifrado IS NOT NULL
ORDER BY id
FOR UPDATE;

-- name: RecifrarWebhookSalida :exec
UPDATE integracion.conectores SET webhook_salida_secreto_cifrado = @webhook_salida_secreto_cifrado WHERE id = @id;

-- name: CanalesMensajeriaCifrados :many
-- Credenciales de los canales de mensajería (WhatsApp Cloud API).
SELECT id, credenciales_cifradas FROM mensajeria.canales
ORDER BY id
FOR UPDATE;

-- name: RecifrarCanalMensajeria :exec
UPDATE mensajeria.canales SET credenciales_cifradas = @credenciales_cifradas WHERE id = @id;

-- name: ConexionMetaCifrada :many
-- Secretos de la conexión con Meta de la instalación (una fila).
SELECT id, secretos_cifrados FROM mensajeria.conexion_meta
ORDER BY id
FOR UPDATE;

-- name: RecifrarConexionMeta :exec
UPDATE mensajeria.conexion_meta SET secretos_cifrados = @secretos_cifrados WHERE id = 1;

-- name: AsistentesCifrados :many
-- Clave del proveedor de IA del asistente de cada empresa.
SELECT id, clave_cifrada FROM mensajeria.asistente_ajustes
WHERE clave_cifrada IS NOT NULL
ORDER BY id
FOR UPDATE;

-- name: RecifrarAsistente :exec
UPDATE mensajeria.asistente_ajustes SET clave_cifrada = @clave_cifrada WHERE id = @id;

-- name: MotoresIACifrados :many
-- Credenciales del canal con el servidor de IA (token de la firma, clave del certificado de cliente
-- y, mientras dura, el código de enlace).
SELECT id, token, token_siguiente, cert_cliente_clave, cert_cliente_clave_siguiente, enlace_codigo FROM mensajeria.voz_motores
WHERE token IS NOT NULL OR token_siguiente IS NOT NULL OR cert_cliente_clave IS NOT NULL OR cert_cliente_clave_siguiente IS NOT NULL
   OR enlace_codigo IS NOT NULL
ORDER BY id
FOR UPDATE;

-- name: RecifrarMotorIA :exec
UPDATE mensajeria.voz_motores SET token = @token, token_siguiente = @token_siguiente, cert_cliente_clave = @cert_cliente_clave,
    cert_cliente_clave_siguiente = @cert_cliente_clave_siguiente, enlace_codigo = @enlace_codigo
WHERE id = @id;

-- name: CorreosSistemaCifrados :many
-- Contraseña del SMTP del correo del sistema (global y por empresa).
SELECT id, contrasena_cifrada FROM notificaciones.correo_ajustes
WHERE contrasena_cifrada IS NOT NULL
ORDER BY id
FOR UPDATE;

-- name: RecifrarCorreoSistema :exec
UPDATE notificaciones.correo_ajustes SET contrasena_cifrada = @contrasena_cifrada WHERE id = @id;

-- name: CentralitasCifradas :many
-- Contraseña del usuario AMI de cada centralita Issabel.
SELECT id, secreto_cifrado FROM mensajeria.centralitas WHERE secreto_cifrado IS NOT NULL ORDER BY id;

-- name: RecifrarCentralita :exec
UPDATE mensajeria.centralitas SET secreto_cifrado = @secreto_cifrado WHERE id = @id;

-- name: ExtensionesSIPCifradas :many
-- Credencial SIP del navegador de cada extensión (E4c; vacía en E4a).
SELECT id, sip_secreto_cifrado FROM mensajeria.centralita_extensiones WHERE sip_secreto_cifrado IS NOT NULL ORDER BY id;

-- name: RecifrarExtensionSIP :exec
UPDATE mensajeria.centralita_extensiones SET sip_secreto_cifrado = @sip_secreto_cifrado WHERE id = @id;

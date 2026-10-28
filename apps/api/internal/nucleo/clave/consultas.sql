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

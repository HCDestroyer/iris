-- Entrada por correo (IMAP) y salida por SMTP. Ver correo.go.

-- Buzones ----------------------------------------------------------------------

-- name: CrearBuzon :one
-- Las contraseñas se fijan después (FijarContrasenasBuzon): van cifradas y atadas al id.
INSERT INTO soporte.buzones (
    empresa_id, nombre, correo, imap_host, imap_puerto, imap_tls, imap_usuario, imap_carpeta,
    imap_carpeta_procesados, smtp_host, smtp_puerto, smtp_tls, smtp_usuario, categoria_id, prioridad
) VALUES (
    @empresa_id, @nombre, @correo, @imap_host, @imap_puerto, @imap_tls, @imap_usuario, @imap_carpeta,
    @imap_carpeta_procesados, @smtp_host, @smtp_puerto, @smtp_tls, @smtp_usuario, @categoria_id, @prioridad
)
RETURNING *;

-- name: FijarContrasenasBuzon :one
UPDATE soporte.buzones SET imap_contrasena = @imap_contrasena, smtp_contrasena = @smtp_contrasena
WHERE id = @id
RETURNING *;

-- name: BuzonPorID :one
SELECT * FROM soporte.buzones WHERE id = @id;

-- name: BuzonPorIDParaActualizar :one
SELECT * FROM soporte.buzones WHERE id = @id FOR UPDATE;

-- name: GuardarBuzon :one
-- Edición (fila leída con FOR UPDATE). Con @reiniciar_cursor (cambió el servidor, el usuario o
-- la carpeta) la próxima lectura vuelve a ser «primera»: solo los no leídos.
UPDATE soporte.buzones SET
    nombre = @nombre, correo = @correo, imap_host = @imap_host, imap_puerto = @imap_puerto,
    imap_tls = @imap_tls, imap_usuario = @imap_usuario, imap_contrasena = @imap_contrasena,
    imap_carpeta = @imap_carpeta, imap_carpeta_procesados = @imap_carpeta_procesados,
    smtp_host = @smtp_host, smtp_puerto = @smtp_puerto, smtp_tls = @smtp_tls, smtp_usuario = @smtp_usuario,
    smtp_contrasena = @smtp_contrasena, categoria_id = @categoria_id, prioridad = @prioridad,
    uid_validity = CASE WHEN @reiniciar_cursor::boolean THEN NULL ELSE uid_validity END,
    ultimo_uid = CASE WHEN @reiniciar_cursor::boolean THEN 0 ELSE ultimo_uid END,
    version = version + 1, updated_at = now()
WHERE id = @id AND version = @version
RETURNING *;

-- name: CambiarActivoBuzon :one
UPDATE soporte.buzones SET activo = @activo, version = version + 1, updated_at = now()
WHERE id = @id AND version = @version
RETURNING *;

-- name: ListarBuzones :many
SELECT * FROM soporte.buzones
WHERE id > @despues_de
  AND ((sqlc.narg(empresa_id)::bigint IS NULL AND empresa_id IS NULL) OR empresa_id = sqlc.narg(empresa_id))
ORDER BY id
LIMIT @limite;

-- name: BuzonesActivos :many
SELECT id FROM soporte.buzones WHERE activo ORDER BY id;

-- name: AvanzarCursorBuzon :exec
-- Tras guardar un correo: el UID ya no se vuelve a pedir (reanudación tras una caída).
UPDATE soporte.buzones SET ultimo_uid = greatest(ultimo_uid, @uid)
WHERE id = @id AND uid_validity = @uid_validity;

-- name: CerrarLecturaBuzon :exec
UPDATE soporte.buzones SET
    uid_validity = @uid_validity,
    ultimo_uid = CASE WHEN uid_validity IS DISTINCT FROM @uid_validity THEN @ultimo_uid
                      ELSE greatest(ultimo_uid, @ultimo_uid) END,
    ultima_lectura_en = @ahora, ultimo_error = NULL
WHERE id = @id;

-- name: ErrorLecturaBuzon :exec
UPDATE soporte.buzones SET ultimo_error = @ultimo_error, ultima_lectura_en = @ahora WHERE id = @id;

-- Correos entrantes ------------------------------------------------------------

-- name: RegistrarCorreoEntrante :one
-- 0 filas (pgx.ErrNoRows) = el Message-ID ya estaba en el buzón (duplicado).
INSERT INTO soporte.correos_entrantes (
    buzon_id, empresa_id, message_id, uid_validity, uid, remitente, remitente_nombre, asunto, in_reply_to,
    referencias, cuerpo, adjuntos, automatico, truncado, fecha
) VALUES (
    @buzon_id, @empresa_id, @message_id, @uid_validity, @uid, @remitente, @remitente_nombre, @asunto,
    sqlc.narg(in_reply_to), @referencias, @cuerpo, @adjuntos, sqlc.narg(automatico), @truncado, sqlc.narg(fecha)
)
ON CONFLICT (buzon_id, message_id) DO NOTHING
RETURNING id;

-- name: CorreosPendientes :many
SELECT id FROM soporte.correos_entrantes WHERE buzon_id = @buzon_id AND estado = 'pendiente' ORDER BY id LIMIT @limite;

-- name: HayCorreosPendientes :one
SELECT EXISTS (SELECT 1 FROM soporte.correos_entrantes WHERE buzon_id = @buzon_id AND estado = 'pendiente');

-- name: CorreoEntranteParaProcesar :one
SELECT * FROM soporte.correos_entrantes WHERE id = @id FOR UPDATE;

-- name: CorreoEntrantePorID :one
SELECT * FROM soporte.correos_entrantes WHERE id = @id;

-- name: CerrarCorreoEntrante :exec
UPDATE soporte.correos_entrantes SET
    estado = @estado, resultado = sqlc.narg(resultado), motivo = sqlc.narg(motivo),
    ticket_id = sqlc.narg(ticket_id), mensaje_id = sqlc.narg(mensaje_id), procesado_en = @ahora, updated_at = now()
WHERE id = @id;

-- name: TicketPorNumero :one
SELECT * FROM soporte.tickets WHERE numero = @numero;

-- name: TicketDeHilo :one
-- El ticket de un hilo de correo: In-Reply-To/References que apuntan a una respuesta que
-- enviamos o a un correo del mismo buzón ya ligado a un ticket. Primero lo enviado.
SELECT x.ticket_id::bigint AS ticket_id FROM (
    SELECT s.ticket_id, s.id, 1 AS orden FROM soporte.correos_salientes s
    WHERE s.buzon_id = @buzon_id AND s.message_id = ANY(@ids::text[])
    UNION ALL
    SELECT e.ticket_id, e.id, 2 AS orden FROM soporte.correos_entrantes e
    WHERE e.buzon_id = @buzon_id AND e.ticket_id IS NOT NULL AND e.message_id = ANY(@ids::text[])
) x
ORDER BY x.orden, x.id DESC
LIMIT 1;

-- name: CrearMensajeCliente :one
INSERT INTO soporte.ticket_mensajes (ticket_id, empresa_id, publico, autor_tipo, autor_nombre, canal, cuerpo)
VALUES (@ticket_id, @empresa_id, true, 'cliente', @autor_nombre, @canal, @cuerpo)
RETURNING *;

-- Correos salientes ------------------------------------------------------------

-- name: MensajePorID :one
SELECT * FROM soporte.ticket_mensajes WHERE id = @id;

-- name: UltimoCorreoDeTicket :one
-- El último correo del cliente ligado al ticket: buzón, destinatario e hilo de la respuesta.
SELECT * FROM soporte.correos_entrantes WHERE ticket_id = @ticket_id ORDER BY id DESC LIMIT 1;

-- name: CrearCorreoSaliente :one
-- 0 filas (pgx.ErrNoRows) = ese mensaje ya se encoló (el evento llegó dos veces).
INSERT INTO soporte.correos_salientes (
    buzon_id, empresa_id, ticket_id, mensaje_id, message_id, in_reply_to, destinatario, asunto
) VALUES (
    @buzon_id, @empresa_id, @ticket_id, @mensaje_id, @message_id, sqlc.narg(in_reply_to), @destinatario, @asunto
)
ON CONFLICT (mensaje_id) DO NOTHING
RETURNING *;

-- name: CorreoSalientePorID :one
SELECT * FROM soporte.correos_salientes WHERE id = @id;

-- name: CorreoSalienteParaActualizar :one
SELECT * FROM soporte.correos_salientes WHERE id = @id FOR UPDATE;

-- name: MarcarCorreoEnviado :exec
UPDATE soporte.correos_salientes SET estado = 'enviado', enviado_en = @ahora, intentos = intentos + 1,
    ultimo_error = NULL, updated_at = now()
WHERE id = @id;

-- name: FalloCorreoSaliente :exec
UPDATE soporte.correos_salientes SET intentos = intentos + 1, ultimo_error = @ultimo_error, updated_at = now()
WHERE id = @id;

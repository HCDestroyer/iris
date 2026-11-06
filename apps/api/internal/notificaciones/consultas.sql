-- Dispositivos -----------------------------------------------------------------------------------

-- name: QuitarTokenDeOtros :many
-- El token pasó a otro usuario (el teléfono cambió de manos): se quita a los demás.
DELETE FROM notificaciones.dispositivos
WHERE token_hash = @token_hash AND usuario_id <> @usuario_id
RETURNING *;

-- name: GuardarDispositivo :one
-- Idempotente por token y empresa: la primera vez inserta (nuevo), después renueva la sesión y la
-- versión de la app.
INSERT INTO notificaciones.dispositivos (usuario_id, empresa_id, sesion_id, token_api_id, plataforma, token, token_hash, token_fin, app_version)
VALUES (@usuario_id, sqlc.narg('empresa_id'), sqlc.narg('sesion_id'), sqlc.narg('token_api_id'), @plataforma, @token, @token_hash, @token_fin, @app_version)
ON CONFLICT (token_hash, (coalesce(empresa_id, 0))) DO UPDATE
SET sesion_id = EXCLUDED.sesion_id, token_api_id = EXCLUDED.token_api_id,
    plataforma = EXCLUDED.plataforma, app_version = EXCLUDED.app_version, updated_at = now()
-- Nunca traspasa el dispositivo de otro usuario (eso lo hace QuitarTokenDeOtros, con su evento).
WHERE notificaciones.dispositivos.usuario_id = EXCLUDED.usuario_id
RETURNING *, (xmax = 0)::boolean AS nuevo;

-- name: BloquearToken :exec
-- Serializa los registros del mismo token (quitar al otro usuario y guardar van juntos).
SELECT pg_advisory_xact_lock(@clave::bigint);

-- name: SobrantesDeUsuario :many
-- Los dispositivos de más allá del tope (los más viejos por última renovación).
SELECT * FROM notificaciones.dispositivos
WHERE usuario_id = @usuario_id AND coalesce(empresa_id, 0) = coalesce(sqlc.narg('empresa')::bigint, 0)
ORDER BY updated_at DESC, id DESC
OFFSET @tope;

-- name: ListarDispositivos :many
SELECT * FROM notificaciones.dispositivos
WHERE usuario_id = @usuario_id AND coalesce(empresa_id, 0) = coalesce(sqlc.narg('empresa')::bigint, 0)
  AND id > @despues
ORDER BY id
LIMIT @limite;

-- name: DispositivoDeUsuario :one
SELECT * FROM notificaciones.dispositivos
WHERE id = @id AND usuario_id = @usuario_id AND coalesce(empresa_id, 0) = coalesce(sqlc.narg('empresa')::bigint, 0)
FOR UPDATE;

-- name: DispositivoPorID :one
SELECT * FROM notificaciones.dispositivos WHERE id = @id;

-- name: BorrarDispositivo :one
DELETE FROM notificaciones.dispositivos WHERE id = @id RETURNING *;

-- name: BorrarDispositivosDeSesion :many
DELETE FROM notificaciones.dispositivos WHERE sesion_id = @sesion_id RETURNING *;

-- name: DispositivosDeUsuarios :many
-- Los dispositivos de estos usuarios en la empresa del evento (null = sin conector).
SELECT * FROM notificaciones.dispositivos
WHERE usuario_id = ANY (@usuarios::bigint[]) AND coalesce(empresa_id, 0) = coalesce(sqlc.narg('empresa')::bigint, 0)
ORDER BY id;

-- name: MarcarUltimoEnvio :exec
UPDATE notificaciones.dispositivos SET ultimo_envio_en = @ahora WHERE id = @id;

-- name: DispositivosSinCredencial :many
-- Para la purga: la sesión o el token ya no valen, o el usuario está inactivo.
SELECT d.* FROM notificaciones.dispositivos d
JOIN nucleo.usuarios u ON u.id = d.usuario_id
LEFT JOIN nucleo.sesiones s ON s.id = d.sesion_id
LEFT JOIN nucleo.tokens_api t ON t.id = d.token_api_id
WHERE NOT u.activo
   OR (d.sesion_id IS NOT NULL AND (s.revocada_en IS NOT NULL OR s.expira_en <= now()))
   OR (d.token_api_id IS NOT NULL AND (t.revocado_en IS NOT NULL OR (t.expira_en IS NOT NULL AND t.expira_en <= now())))
ORDER BY d.id
LIMIT @limite;

-- Preferencias -----------------------------------------------------------------------------------

-- name: Preferencias :one
SELECT * FROM notificaciones.preferencias WHERE usuario_id = @usuario_id;

-- name: PreferenciasParaActualizar :one
SELECT * FROM notificaciones.preferencias WHERE usuario_id = @usuario_id FOR UPDATE;

-- name: InsertarPreferencias :one
INSERT INTO notificaciones.preferencias (usuario_id, chat, mencion, orden, bandeja, contenido, no_molestar,
    no_molestar_desde, no_molestar_hasta, urgente_igual, zona_horaria)
VALUES (@usuario_id, @chat, @mencion, @orden, @bandeja, @contenido, @no_molestar,
    @no_molestar_desde, @no_molestar_hasta, @urgente_igual, @zona_horaria)
ON CONFLICT (usuario_id) DO NOTHING
RETURNING *;

-- name: ActualizarPreferencias :one
UPDATE notificaciones.preferencias
SET chat = @chat, mencion = @mencion, orden = @orden, bandeja = @bandeja, contenido = @contenido,
    no_molestar = @no_molestar, no_molestar_desde = @no_molestar_desde, no_molestar_hasta = @no_molestar_hasta,
    urgente_igual = @urgente_igual, zona_horaria = @zona_horaria, version = version + 1, updated_at = now()
WHERE usuario_id = @usuario_id AND version = @version
RETURNING *;

-- Envíos -----------------------------------------------------------------------------------------

-- name: InsertarEnvio :one
-- Un envío por (evento, dispositivo) y, con clave, por (clave, dispositivo): un evento repetido no
-- inserta nada (sin filas).
INSERT INTO notificaciones.envios (evento_id, evento, dispositivo_id, usuario_id, empresa_id, tipo, clave, datos)
VALUES (@evento_id, @evento, @dispositivo_id, @usuario_id, sqlc.narg('empresa_id'), @tipo, sqlc.narg('clave'), @datos)
ON CONFLICT DO NOTHING
RETURNING id;

-- name: FusionarEnvio :exec
-- Otro evento de la misma acción (misma clave): suma sus banderas si aún no salió.
UPDATE notificaciones.envios
SET datos = datos || @datos, tipo = CASE WHEN @tipo::text = 'mencion' THEN 'mencion' ELSE tipo END, updated_at = now()
WHERE clave = @clave AND dispositivo_id = @dispositivo_id AND estado = 'pendiente';

-- name: EnvioParaEnviar :one
SELECT * FROM notificaciones.envios WHERE id = @id FOR UPDATE;

-- name: CerrarEnvio :exec
UPDATE notificaciones.envios
SET estado = @estado, motivo = sqlc.narg('motivo'), fcm_mensaje = sqlc.narg('fcm_mensaje'),
    enviado_en = sqlc.narg('enviado_en'), intentos = intentos + @intentos, updated_at = now()
WHERE id = @id AND estado = 'pendiente';

-- name: ContarIntento :exec
UPDATE notificaciones.envios SET intentos = intentos + 1, updated_at = now() WHERE id = @id;

-- name: PurgarEnvios :execrows
DELETE FROM notificaciones.envios WHERE created_at < @antes;

-- Contenido (al enviar) --------------------------------------------------------------------------

-- name: MensajeChatParaPush :one
SELECT m.id, m.conversacion_id, m.empresa_id, m.seq, m.autor_id, m.texto, m.adjunto_id, m.lat, m.eliminado_en, m.enviado_en,
       c.tipo AS conversacion_tipo, c.cuadrilla_id, c.orden_id,
       u.nombre AS autor_nombre,
       cu.nombre AS cuadrilla_nombre,
       o.numero AS orden_numero
FROM mensajeria.chat_mensajes m
JOIN mensajeria.chat_conversaciones c ON c.id = m.conversacion_id
JOIN nucleo.usuarios u ON u.id = m.autor_id
LEFT JOIN agenda.cuadrillas cu ON cu.id = c.cuadrilla_id
LEFT JOIN campo.ordenes o ON o.id = c.orden_id
WHERE m.id = @id;

-- name: NotaConversacionParaPush :one
SELECT m.id, m.conversacion_id, m.empresa_id, m.direccion, m.texto, m.autor_id, m.creado_en,
       coalesce(u.nombre, '')::text AS autor_nombre,
       coalesce(ac.nombre, ci.nombre, c.perfil_nombre, '')::text AS contacto_nombre
FROM mensajeria.conversacion_mensajes m
JOIN mensajeria.conversaciones c ON c.id = m.conversacion_id
LEFT JOIN nucleo.usuarios u ON u.id = m.autor_id
LEFT JOIN integracion.argos_clientes ac ON ac.id = c.cliente_id
LEFT JOIN mensajeria.contactos_iris ci ON ci.id = c.contacto_iris_id
WHERE m.id = @id;

-- name: ConversacionParaPush :one
SELECT c.id, c.empresa_id, c.estado, c.asignado_a, c.cola_id,
       q.nombre AS cola_nombre,
       coalesce(ac.nombre, ci.nombre, c.perfil_nombre, '')::text AS contacto_nombre
FROM mensajeria.conversaciones c
LEFT JOIN mensajeria.colas q ON q.id = c.cola_id
LEFT JOIN integracion.argos_clientes ac ON ac.id = c.cliente_id
LEFT JOIN mensajeria.contactos_iris ci ON ci.id = c.contacto_iris_id
WHERE c.id = @id;

-- name: OrdenParaPush :one
SELECT o.id, o.numero, o.empresa_id, o.estado, o.tecnico_id, o.programada_inicio, o.programada_fin,
       t.codigo AS tipo, t.nombre AS tipo_nombre,
       z.nombre AS zona_nombre,
       coalesce(ac.nombre, o.contacto_nombre, '')::text AS cliente_nombre,
       ci.ventana_inicio, ci.ventana_fin
FROM campo.ordenes o
JOIN campo.tipos_orden t ON t.id = o.tipo_id
LEFT JOIN agenda.zonas z ON z.id = o.zona_id
LEFT JOIN integracion.argos_clientes ac ON ac.id = o.cliente_id
LEFT JOIN agenda.citas ci ON ci.orden_id = o.id
WHERE o.id = @id;

-- name: HayEmpresas :one
SELECT EXISTS (SELECT 1 FROM nucleo.empresas)::boolean;

-- Chat interno del equipo (contratos/chat.md). Los miembros derivados (técnicos de la cuadrilla,
-- técnico de la orden, alcance de chat.supervisar) se leen de agenda, campo y núcleo; no se copian.

-- name: ChatVisible :one
SELECT EXISTS (
    SELECT 1 FROM mensajeria.chat_visibles(@usuario::bigint, sqlc.narg('empresa')::bigint, @todas::boolean, @sup_global::boolean,
        @sup_zonas::bigint[], @sup_cuadrillas::bigint[], @ord_global::boolean, @ord_zonas::bigint[], @ord_cuadrillas::bigint[],
        @conversacion_id::bigint)
)::boolean AS visible;

-- name: ConversacionesVisibles :many
-- Página de «mis conversaciones», de la más reciente a la más vieja (cursor: actividad e id).
SELECT c.id, c.ultima_actividad FROM mensajeria.chat_conversaciones c
WHERE c.id IN (SELECT v FROM mensajeria.chat_visibles(@usuario::bigint, sqlc.narg('empresa')::bigint, @todas::boolean, @sup_global::boolean,
        @sup_zonas::bigint[], @sup_cuadrillas::bigint[], @ord_global::boolean, @ord_zonas::bigint[], @ord_cuadrillas::bigint[], NULL::bigint) v)
  AND (sqlc.narg('tipo')::text IS NULL OR c.tipo = sqlc.narg('tipo')::text)
  AND (NOT @con_cursor::boolean OR (c.ultima_actividad, c.id) < (@cursor_actividad::timestamptz, @cursor_id::bigint))
ORDER BY c.ultima_actividad DESC, c.id DESC
LIMIT @limite;

-- name: ResumenConversaciones :many
-- Lo que ve un usuario de cada conversación: título (cuadrilla, orden o el otro de la directa),
-- vista previa del último mensaje, su lectura y sus no leídos (de otros, sin los eliminados).
SELECT sqlc.embed(c),
    cu.nombre AS cuadrilla_nombre,
    o.numero AS orden_numero,
    o.estado AS orden_estado,
    COALESCE((SELECT mm.usuario_id FROM mensajeria.chat_miembros mm
     WHERE c.tipo = 'directa' AND mm.conversacion_id = c.id AND mm.usuario_id <> @usuario::bigint LIMIT 1), 0)::bigint AS otro_id,
    m.autor_id AS ultimo_autor_id,
    m.texto AS ultimo_texto,
    m.adjunto_id AS ultimo_adjunto_id,
    m.lat AS ultimo_lat,
    m.enviado_en AS ultimo_enviado_en,
    m.eliminado_en AS ultimo_eliminado_en,
    l.hasta_mensaje_id AS leido_hasta_mensaje_id,
    (SELECT count(*) FROM mensajeria.chat_mensajes x
     WHERE x.conversacion_id = c.id AND x.seq > COALESCE(l.hasta_seq, 0) AND x.autor_id <> @usuario::bigint AND x.eliminado_en IS NULL)::bigint AS no_leidos,
    EXISTS (SELECT 1 FROM mensajeria.chat_miembros e WHERE e.conversacion_id = c.id AND e.usuario_id = @usuario::bigint)::boolean AS miembro_explicito
FROM mensajeria.chat_conversaciones c
LEFT JOIN agenda.cuadrillas cu ON cu.id = c.cuadrilla_id
LEFT JOIN campo.ordenes o ON o.id = c.orden_id
LEFT JOIN mensajeria.chat_mensajes m ON m.id = c.ultimo_mensaje_id
LEFT JOIN mensajeria.chat_lecturas l ON l.conversacion_id = c.id AND l.usuario_id = @usuario::bigint
WHERE c.id = ANY (@ids::bigint[]);

-- name: NombresUsuarios :many
SELECT id, nombre FROM nucleo.usuarios WHERE id = ANY (@ids::bigint[]);

-- name: UsuarioChat :one
SELECT id, nombre, tipo, activo FROM nucleo.usuarios WHERE id = @id;

-- name: ConversacionPorID :one
SELECT * FROM mensajeria.chat_conversaciones WHERE id = @id;

-- name: ConversacionParaActualizar :one
SELECT * FROM mensajeria.chat_conversaciones WHERE id = @id FOR UPDATE;

-- name: ConversacionDeCuadrilla :one
SELECT * FROM mensajeria.chat_conversaciones
WHERE tipo = 'cuadrilla' AND cuadrilla_id = @cuadrilla_id AND empresa_id IS NOT DISTINCT FROM sqlc.narg('empresa')::bigint;

-- name: ConversacionDeOrden :one
SELECT * FROM mensajeria.chat_conversaciones WHERE tipo = 'orden' AND orden_id = @orden_id;

-- name: ConversacionDirecta :one
SELECT * FROM mensajeria.chat_conversaciones
WHERE tipo = 'directa' AND clave_directa = @clave AND empresa_id IS NOT DISTINCT FROM sqlc.narg('empresa')::bigint;

-- name: InsertarConversacion :one
-- Sin fila: otra petición la creó a la vez (índices únicos); se lee la existente.
INSERT INTO mensajeria.chat_conversaciones (empresa_id, tipo, cuadrilla_id, orden_id, clave_directa, creada_por, ultima_actividad, created_at, updated_at)
VALUES (sqlc.narg('empresa_id'), @tipo, sqlc.narg('cuadrilla_id'), sqlc.narg('orden_id'), sqlc.narg('clave_directa'), sqlc.narg('creada_por'),
        @ahora, @ahora, @ahora)
ON CONFLICT DO NOTHING
RETURNING *;

-- name: InsertarMiembro :execrows
INSERT INTO mensajeria.chat_miembros (conversacion_id, usuario_id, origen, created_at, updated_at)
VALUES (@conversacion_id, @usuario_id, @origen, @ahora, @ahora)
ON CONFLICT DO NOTHING;

-- name: CandadoChat :exec
SELECT pg_advisory_xact_lock(hashtextextended(@clave::text, 0));

-- name: MensajePorID :one
SELECT * FROM mensajeria.chat_mensajes WHERE id = @id;

-- name: MensajePorClave :one
SELECT * FROM mensajeria.chat_mensajes WHERE autor_id = @autor_id AND idempotency_key = @idempotency_key;

-- name: MensajeParaActualizar :one
SELECT * FROM mensajeria.chat_mensajes WHERE id = @id FOR UPDATE;

-- name: EnviosRecientes :one
-- Envíos del autor desde @desde (límite por minuto) y el más viejo de ellos (para Retry-After).
SELECT count(*)::bigint AS total, COALESCE(min(enviado_en), @desde)::timestamptz AS primero
FROM mensajeria.chat_mensajes WHERE autor_id = @autor_id AND enviado_en > @desde;

-- name: AvanzarConversacion :one
UPDATE mensajeria.chat_conversaciones
SET ultimo_seq = ultimo_seq + 1, ultimo_mensaje_id = @mensaje_id::uuid, ultima_actividad = @ahora, updated_at = @ahora
WHERE id = @id
RETURNING ultimo_seq;

-- name: InsertarMensaje :one
INSERT INTO mensajeria.chat_mensajes (id, conversacion_id, empresa_id, seq, autor_id, texto, adjunto_id, lat, lon, creado_en, enviado_en,
    hash_contenido, idempotency_key, created_at, updated_at)
VALUES (@id, @conversacion_id, sqlc.narg('empresa_id'), @seq, @autor_id, sqlc.narg('texto'), sqlc.narg('adjunto_id'), sqlc.narg('lat'), sqlc.narg('lon'),
    sqlc.narg('creado_en'), @enviado_en, @hash_contenido, sqlc.narg('idempotency_key'), @enviado_en, @enviado_en)
RETURNING *;

-- name: EliminarMensaje :one
UPDATE mensajeria.chat_mensajes
SET texto = NULL, adjunto_id = NULL, lat = NULL, lon = NULL, hash_contenido = NULL, eliminado_en = @ahora, updated_at = @ahora
WHERE id = @id
RETURNING *;

-- name: ListarMensajes :many
-- Página de mensajes por seq: siguientes (seq > @desde, ascendente) o anteriores (seq < @desde,
-- descendente; el caso de uso la invierte).
SELECT sqlc.embed(m), u.nombre AS autor_nombre, a.estado AS adjunto_estado
FROM mensajeria.chat_mensajes m
JOIN nucleo.usuarios u ON u.id = m.autor_id
LEFT JOIN nucleo.adjuntos a ON a.id = m.adjunto_id
WHERE m.conversacion_id = @conversacion_id
  AND ((@siguientes::boolean AND m.seq > @desde::bigint) OR (NOT @siguientes::boolean AND m.seq < @desde::bigint))
ORDER BY CASE WHEN @siguientes::boolean THEN m.seq END ASC, CASE WHEN NOT @siguientes::boolean THEN m.seq END DESC
LIMIT @limite;

-- name: MensajesPorIDs :many
SELECT sqlc.embed(m), u.nombre AS autor_nombre, a.estado AS adjunto_estado
FROM mensajeria.chat_mensajes m
JOIN nucleo.usuarios u ON u.id = m.autor_id
LEFT JOIN nucleo.adjuntos a ON a.id = m.adjunto_id
WHERE m.id = ANY (@ids::uuid[]);

-- name: AvanzarLectura :one
-- Solo avanza: sin fila si ya había leído hasta ahí o más allá.
INSERT INTO mensajeria.chat_lecturas (conversacion_id, usuario_id, hasta_seq, hasta_mensaje_id, leido_en, created_at, updated_at)
VALUES (@conversacion_id, @usuario_id, @hasta_seq, @hasta_mensaje_id, @ahora, @ahora, @ahora)
ON CONFLICT (conversacion_id, usuario_id) DO UPDATE
SET hasta_seq = excluded.hasta_seq, hasta_mensaje_id = excluded.hasta_mensaje_id, leido_en = excluded.leido_en, updated_at = excluded.updated_at
WHERE mensajeria.chat_lecturas.hasta_seq < excluded.hasta_seq
RETURNING *;

-- name: LecturaDe :one
SELECT * FROM mensajeria.chat_lecturas WHERE conversacion_id = @conversacion_id AND usuario_id = @usuario_id;

-- name: LecturasDeConversacion :many
SELECT l.usuario_id, u.nombre, l.hasta_mensaje_id, l.hasta_seq, l.leido_en
FROM mensajeria.chat_lecturas l JOIN nucleo.usuarios u ON u.id = l.usuario_id
WHERE l.conversacion_id = @conversacion_id
ORDER BY l.usuario_id;

-- name: NoLeidos :many
SELECT c.id AS conversacion_id,
    (SELECT count(*) FROM mensajeria.chat_mensajes x
     WHERE x.conversacion_id = c.id AND x.seq > COALESCE(l.hasta_seq, 0) AND x.autor_id <> @usuario::bigint AND x.eliminado_en IS NULL)::bigint AS no_leidos
FROM mensajeria.chat_conversaciones c
LEFT JOIN mensajeria.chat_lecturas l ON l.conversacion_id = c.id AND l.usuario_id = @usuario::bigint
WHERE c.id = ANY (@ids::bigint[]);

-- name: InsertarEventoChat :one
-- Va después de mensajeria.chat_eventos_candado (CandadoChat con «chat:eventos:<empresa>»).
INSERT INTO mensajeria.chat_eventos (empresa_id, conversacion_id, tipo, mensaje_id, usuario_id, ocurrido_en)
VALUES (sqlc.narg('empresa_id'), @conversacion_id, @tipo, sqlc.narg('mensaje_id'), sqlc.narg('usuario_id'), @ocurrido_en)
RETURNING id;

-- name: NotificarChat :exec
SELECT pg_notify('iris_chat', @carga::text);

-- name: EventosChat :many
-- Feed de un usuario: los eventos después de @desde de las conversaciones que ve en la empresa.
SELECT e.* FROM mensajeria.chat_eventos e
WHERE e.id > @desde::bigint
  AND (@todas::boolean OR e.empresa_id IS NOT DISTINCT FROM sqlc.narg('empresa')::bigint)
  AND e.conversacion_id IN (SELECT v FROM mensajeria.chat_visibles(@usuario::bigint, sqlc.narg('empresa')::bigint, @todas::boolean,
        @sup_global::boolean, @sup_zonas::bigint[], @sup_cuadrillas::bigint[], @ord_global::boolean, @ord_zonas::bigint[], @ord_cuadrillas::bigint[], NULL::bigint) v)
ORDER BY e.id
LIMIT @limite;

-- name: EstadoFeedChat :one
-- Cabeza del feed (último id, de todas las empresas), la de la empresa y hasta dónde se purgó. Un
-- cursor solo puede saltar hasta la cabeza de SU empresa: los ids de otra empresa se intercalan y un
-- evento de esta que aún no confirma tiene un id menor que la cabeza global.
SELECT COALESCE((SELECT max(id) FROM mensajeria.chat_eventos), 0)::bigint AS cabeza,
    COALESCE((SELECT max(id) FROM mensajeria.chat_eventos WHERE empresa_id IS NOT DISTINCT FROM sqlc.narg('empresa')::bigint), 0)::bigint AS cabeza_empresa,
    (SELECT eventos_purgados_hasta FROM mensajeria.chat_estado WHERE id = 1)::bigint AS purgados_hasta;

-- name: PurgarEventosChat :one
WITH borrados AS (
    DELETE FROM mensajeria.chat_eventos WHERE ocurrido_en < @antes RETURNING id
)
UPDATE mensajeria.chat_estado
SET eventos_purgados_hasta = GREATEST(eventos_purgados_hasta, COALESCE((SELECT max(id) FROM borrados), 0))
WHERE id = 1
RETURNING eventos_purgados_hasta;

-- name: DestinatariosChat :many
-- A quién avisar (push) de un mensaje: miembros explícitos, técnicos de la cuadrilla o de la orden y
-- quien tiene chat.supervisar con alcance que la cubre (no en directas), activos, que operan en la
-- empresa y que no son el autor.
WITH candidatos AS (
    SELECT mm.usuario_id FROM mensajeria.chat_miembros mm WHERE mm.conversacion_id = @conversacion_id AND mm.origen = 'directa'
    UNION
    SELECT mm.usuario_id FROM mensajeria.chat_miembros mm
    WHERE mm.conversacion_id = @conversacion_id AND mm.origen = 'unido'
      AND EXISTS (SELECT 1 FROM nucleo.usuarios_roles ur
                  JOIN nucleo.roles r ON r.id = ur.rol_id AND r.activo
                  JOIN nucleo.roles_permisos rp ON rp.rol_id = ur.rol_id AND rp.permiso = 'ordenes.ver'
                  WHERE ur.usuario_id = mm.usuario_id
                    AND ((ur.zona_id IS NULL AND ur.cuadrilla_id IS NULL)
                         OR ur.cuadrilla_id = sqlc.narg('alcance_cuadrilla')::bigint
                         OR ur.zona_id = ANY (@alcance_zonas::bigint[])))
    UNION
    SELECT t.usuario_id FROM agenda.tecnicos t WHERE t.activo AND @tipo::text = 'cuadrilla' AND t.cuadrilla_id = sqlc.narg('cuadrilla_id')::bigint
    UNION
    SELECT o.tecnico_id FROM campo.ordenes o WHERE o.id = sqlc.narg('orden_id')::bigint AND o.tecnico_id IS NOT NULL
    UNION
    SELECT ur.usuario_id FROM nucleo.usuarios_roles ur
    JOIN nucleo.roles r ON r.id = ur.rol_id AND r.activo
    JOIN nucleo.roles_permisos rp ON rp.rol_id = ur.rol_id AND rp.permiso = 'chat.supervisar'
    WHERE @tipo::text <> 'directa'
      AND ((ur.zona_id IS NULL AND ur.cuadrilla_id IS NULL)
           OR ur.cuadrilla_id = sqlc.narg('alcance_cuadrilla')::bigint
           OR ur.zona_id = ANY (@alcance_zonas::bigint[]))
)
SELECT DISTINCT u.id AS usuario_id FROM candidatos x
JOIN nucleo.usuarios u ON u.id = x.usuario_id AND u.activo
WHERE u.id <> @autor_id
  AND (sqlc.narg('empresa')::bigint IS NULL
       OR EXISTS (SELECT 1 FROM nucleo.usuarios_empresas ue WHERE ue.usuario_id = u.id AND ue.empresa_id = sqlc.narg('empresa')::bigint)
       OR EXISTS (SELECT 1 FROM nucleo.usuarios_roles ur2
                  JOIN nucleo.roles r2 ON r2.id = ur2.rol_id AND r2.activo
                  JOIN nucleo.roles_permisos rp2 ON rp2.rol_id = ur2.rol_id AND rp2.permiso = 'empresas.todas'
                  WHERE ur2.usuario_id = u.id AND ur2.zona_id IS NULL AND ur2.cuadrilla_id IS NULL))
ORDER BY u.id;

-- name: MensajeVivoConAdjunto :one
-- La foto se ve solo si un mensaje sin eliminar de esa conversación la muestra.
SELECT EXISTS (
    SELECT 1 FROM mensajeria.chat_mensajes
    WHERE adjunto_id = @adjunto_id AND conversacion_id = @conversacion_id AND eliminado_en IS NULL
)::boolean AS vivo;

-- name: FotosChatSinMensaje :many
-- Fotos del chat que ningún mensaje muestra y que nadie toca desde @antes (subidas y nunca
-- enviadas): se retiran.
SELECT a.id FROM nucleo.adjuntos a
WHERE a.entidad = 'chat_conversacion' AND a.estado <> 'rechazado' AND a.updated_at < @antes
  AND NOT EXISTS (SELECT 1 FROM mensajeria.chat_mensajes m WHERE m.adjunto_id = a.id)
ORDER BY a.updated_at
LIMIT @limite;

-- Voz E1 (contratos/voz.md): llamadas entrantes de WhatsApp contestadas por personas en el panel.

-- name: InsertarLlamada :one
-- La unicidad de origen, canal e id externo hace de un connect repetido un no-op.
INSERT INTO mensajeria.llamadas (id, empresa_id, origen, canal_id, conversacion_id, id_externo, direccion, numero, cliente_id, servicio_id,
    cola_id, estado, sono_en, suena_hasta, sdp_oferta, grabada)
VALUES (sqlc.arg('llamada_id'), sqlc.narg('empresa_id'), 'whatsapp', sqlc.arg('canal_id'), sqlc.arg('conversacion_id'), sqlc.arg('externo'), 'entrante', sqlc.arg('numero'),
    sqlc.narg('cliente_id'), sqlc.narg('servicio'), sqlc.narg('cola_id'), 'sonando', sqlc.arg('sono_en'), sqlc.arg('suena_hasta'), sqlc.narg('sdp_oferta'), sqlc.arg('grabada'))
ON CONFLICT (origen, canal_id, id_externo) DO NOTHING
RETURNING *;

-- name: LlamadaPorID :one
SELECT * FROM mensajeria.llamadas WHERE id = @id;

-- name: LlamadaParaActualizar :one
SELECT * FROM mensajeria.llamadas WHERE id = @id FOR UPDATE;

-- name: LlamadaPorExterno :one
SELECT * FROM mensajeria.llamadas WHERE origen = 'whatsapp' AND canal_id = @canal_id AND id_externo = @id_externo FOR UPDATE;

-- name: LlamadasPorIDs :many
SELECT * FROM mensajeria.llamadas WHERE id = ANY (@ids::uuid[]);

-- name: FijarMensajeDeLlamada :exec
UPDATE mensajeria.llamadas SET mensaje_id = @mensaje_id WHERE id = @id;

-- name: FijarLlamadaDeMensaje :exec
UPDATE mensajeria.conversacion_mensajes SET llamada_id = @llamada_id WHERE id = @id;

-- name: ReclamarLlamada :one
-- Reserva atómica (§3.3): gana quien actualiza la fila, y solo un candidato vivo de esa llamada.
UPDATE mensajeria.llamadas l
SET estado = 'conectando', reclamada_por = @usuario_id, reclamada_hasta = @hasta, updated_at = now()
WHERE l.id = @id AND coalesce(l.empresa_id, 0) = @empresa::bigint AND l.estado = 'sonando'
  AND EXISTS (SELECT 1 FROM mensajeria.llamada_candidatos c WHERE c.llamada_id = l.id AND c.usuario_id = @usuario_id AND c.retirado_en IS NULL)
RETURNING *;

-- name: EsCandidato :one
SELECT EXISTS (SELECT 1 FROM mensajeria.llamada_candidatos c JOIN mensajeria.llamadas l ON l.id = c.llamada_id
    WHERE c.llamada_id = @id AND c.usuario_id = @usuario_id AND c.retirado_en IS NULL AND coalesce(l.empresa_id, 0) = @empresa::bigint)::boolean;

-- name: GuardarLlamada :one
-- Escribe el estado que calculó el caso de uso (la fila se leyó con FOR UPDATE).
UPDATE mensajeria.llamadas
SET estado = @estado, atendida_por = sqlc.narg('atendida_por'), usuario_id = sqlc.narg('usuario_id'),
    reclamada_por = sqlc.narg('reclamada_por'), reclamada_hasta = sqlc.narg('reclamada_hasta'), motivo_fin = sqlc.narg('motivo_fin'),
    contestada_en = sqlc.narg('contestada_en'), terminada_en = sqlc.narg('terminada_en'), duracion_seg = sqlc.narg('duracion_seg'),
    nota = sqlc.narg('nota'), resultado = sqlc.narg('resultado'), sdp_oferta = sqlc.narg('sdp_oferta'),
    accion_meta = sqlc.narg('accion_meta'), accion_meta_estado = sqlc.narg('accion_meta_estado'), accion_meta_en = sqlc.narg('accion_meta_en'),
    pre_aceptada_en = sqlc.narg('pre_aceptada_en'), aviso_perdida = @aviso_perdida, updated_at = now()
WHERE id = @id
RETURNING *;

-- name: InsertarCandidato :exec
INSERT INTO mensajeria.llamada_candidatos (llamada_id, empresa_id, usuario_id, desde)
VALUES (@llamada_id, sqlc.narg('empresa_id'), @usuario_id, @desde)
ON CONFLICT (llamada_id, usuario_id) DO NOTHING;

-- name: CandidatosVivos :many
SELECT usuario_id FROM mensajeria.llamada_candidatos WHERE llamada_id = @llamada_id AND retirado_en IS NULL ORDER BY usuario_id;

-- name: RetirarCandidato :execrows
UPDATE mensajeria.llamada_candidatos SET retirado_en = @ahora, motivo_retiro = @motivo
WHERE llamada_id = @llamada_id AND usuario_id = @usuario_id AND retirado_en IS NULL;

-- name: RetirarCandidatos :exec
UPDATE mensajeria.llamada_candidatos SET retirado_en = @ahora, motivo_retiro = 'fin' WHERE llamada_id = @llamada_id AND retirado_en IS NULL;

-- name: ContarSuelta :one
-- Una reserva soltada o vencida: a la segunda el usuario deja de recibir esta llamada.
UPDATE mensajeria.llamada_candidatos SET sueltas = sueltas + 1,
    retirado_en = CASE WHEN sueltas + 1 >= 2 THEN @ahora::timestamptz ELSE retirado_en END,
    motivo_retiro = CASE WHEN sueltas + 1 >= 2 THEN 'solto' ELSE motivo_retiro END
WHERE llamada_id = @llamada_id AND usuario_id = @usuario_id
RETURNING sueltas;

-- name: ReceptoresParaLlamada :many
-- Quién puede recibir el timbre: disponible, con «Recibir llamadas» encendido y un flujo SSE vivo,
-- y que atienda la cola (sin cola, cualquiera de la empresa).
SELECT DISTINCT d.usuario_id FROM mensajeria.disponibilidad d
JOIN nucleo.usuarios u ON u.id = d.usuario_id AND u.activo
JOIN mensajeria.llamada_receptores r ON r.usuario_id = d.usuario_id AND coalesce(r.empresa_id, 0) = @empresa::bigint
    AND r.recibe AND r.visto_en > @vivo_desde
WHERE coalesce(d.empresa_id, 0) = @empresa::bigint AND d.estado = 'disponible'
  AND (sqlc.narg('cola_id')::bigint IS NULL
       OR EXISTS (SELECT 1 FROM mensajeria.cola_miembros m WHERE m.cola_id = sqlc.narg('cola_id')::bigint AND m.usuario_id = d.usuario_id)
       OR EXISTS (SELECT 1 FROM mensajeria.cola_supervisores s WHERE s.cola_id = sqlc.narg('cola_id')::bigint AND s.usuario_id = d.usuario_id))
  -- Quien ya tiene una llamada viva no recibe otra.
  AND NOT EXISTS (SELECT 1 FROM mensajeria.llamadas l WHERE l.estado IN ('conectando', 'en_curso')
       AND (l.usuario_id = d.usuario_id OR l.reclamada_por = d.usuario_id))
ORDER BY d.usuario_id;

-- name: FijarReceptor :one
INSERT INTO mensajeria.llamada_receptores (empresa_id, usuario_id, navegador, recibe, visto_en)
VALUES (sqlc.narg('empresa_id'), @usuario_id, @navegador, @recibe, @ahora)
ON CONFLICT ((coalesce(empresa_id, 0)), usuario_id, navegador)
DO UPDATE SET recibe = EXCLUDED.recibe, visto_en = EXCLUDED.visto_en, updated_at = now()
RETURNING *;

-- name: ReceptorDe :one
SELECT * FROM mensajeria.llamada_receptores
WHERE coalesce(empresa_id, 0) = @empresa::bigint AND usuario_id = @usuario_id AND navegador = @navegador;

-- name: ReceptorVivo :one
SELECT EXISTS (SELECT 1 FROM mensajeria.llamada_receptores
    WHERE coalesce(empresa_id, 0) = @empresa::bigint AND usuario_id = @usuario_id AND recibe AND visto_en > @vivo_desde)::boolean;

-- name: TocarReceptores :exec
-- Latido del flujo SSE del usuario.
UPDATE mensajeria.llamada_receptores SET visto_en = @ahora
WHERE coalesce(empresa_id, 0) = @empresa::bigint AND usuario_id = @usuario_id AND recibe;

-- name: LlamadaActivaDeUsuario :one
SELECT * FROM mensajeria.llamadas
WHERE coalesce(empresa_id, 0) = @empresa::bigint AND estado IN ('conectando', 'en_curso')
  AND (usuario_id = @usuario_id OR reclamada_por = @usuario_id)
ORDER BY sono_en DESC LIMIT 1;

-- name: LlamadasRecientesDeNumero :one
-- Ráfaga: llamadas de ese número a ese canal desde un instante.
SELECT count(*)::bigint FROM mensajeria.llamadas WHERE canal_id = @canal_id AND numero = @numero AND sono_en > @desde;

-- name: AvisoPerdidaReciente :one
SELECT EXISTS (SELECT 1 FROM mensajeria.llamadas
    WHERE conversacion_id = @conversacion_id AND aviso_perdida AND sono_en > @desde)::boolean;

-- name: LlamadasVivas :many
-- Para el mantenimiento (cada 5 s): todo lo que no terminó.
SELECT * FROM mensajeria.llamadas WHERE estado IN ('sonando', 'marcando', 'conectando', 'en_curso') ORDER BY sono_en LIMIT 200;

-- name: LlamadasConAccionPendiente :many
-- Intenciones con Meta que quedaron a medias (el proceso murió entre la intención y el resultado).
SELECT * FROM mensajeria.llamadas WHERE accion_meta_estado = 'pendiente' AND accion_meta_en < @antes ORDER BY accion_meta_en LIMIT 100;

-- name: ListarLlamadas :many
SELECT l.* FROM mensajeria.llamadas l
JOIN mensajeria.conversaciones c ON c.id = l.conversacion_id
WHERE coalesce(l.empresa_id, 0) = @empresa::bigint AND coalesce(c.empresa_id, 0) = @empresa::bigint
  AND mensajeria.conv_visible(@usuario::bigint, @supervisa::boolean, c.id, c.cola_id, c.asignado_a)
  AND (sqlc.narg('estado')::text IS NULL OR l.estado = sqlc.narg('estado')::text)
  AND (sqlc.narg('direccion')::text IS NULL OR l.direccion = sqlc.narg('direccion')::text)
  AND (sqlc.narg('conversacion_id')::bigint IS NULL OR l.conversacion_id = sqlc.narg('conversacion_id')::bigint)
  AND (sqlc.narg('cola_id')::bigint IS NULL OR l.cola_id = sqlc.narg('cola_id')::bigint)
  AND (sqlc.narg('desde')::timestamptz IS NULL OR l.sono_en >= sqlc.narg('desde')::timestamptz)
  AND (sqlc.narg('hasta')::timestamptz IS NULL OR l.sono_en < sqlc.narg('hasta')::timestamptz)
  AND (sqlc.narg('antes_de')::uuid IS NULL OR l.id < sqlc.narg('antes_de')::uuid)
ORDER BY l.id DESC
LIMIT @limite;

-- name: InsertarTramo :exec
INSERT INTO mensajeria.llamada_tramos (llamada_id, empresa_id, orden, tipo, usuario_id, inicio)
SELECT l.id, l.empresa_id, (SELECT coalesce(max(t.orden), 0) + 1 FROM mensajeria.llamada_tramos t WHERE t.llamada_id = l.id), 'persona',
    sqlc.arg('usuario_id')::bigint, sqlc.arg('comienzo')::timestamptz
FROM mensajeria.llamadas l WHERE l.id = sqlc.arg('llamada_id');

-- name: CerrarTramos :exec
UPDATE mensajeria.llamada_tramos t SET fin = sqlc.arg('cierre')::timestamptz,
    duracion_seg = GREATEST(0, floor(extract(epoch FROM (sqlc.arg('cierre')::timestamptz - t.inicio))))::int, motivo_fin = sqlc.arg('motivo')
WHERE t.llamada_id = sqlc.arg('llamada_id') AND t.fin IS NULL;

-- name: TramosDeLlamadas :many
SELECT t.*, coalesce(u.nombre, '')::text AS usuario_nombre FROM mensajeria.llamada_tramos t
LEFT JOIN nucleo.usuarios u ON u.id = t.usuario_id
WHERE t.llamada_id = ANY (@ids::uuid[]) ORDER BY t.llamada_id, t.orden;

-- name: VozAjustes :one
SELECT * FROM mensajeria.voz_ajustes WHERE coalesce(empresa_id, 0) = @empresa::bigint;

-- name: VozAjustesParaActualizar :one
SELECT * FROM mensajeria.voz_ajustes WHERE coalesce(empresa_id, 0) = @empresa::bigint FOR UPDATE;

-- name: InsertarVozAjustes :one
INSERT INTO mensajeria.voz_ajustes (empresa_id, grabar, duracion_max_persona_min, segundos_asignado, retencion_grabaciones_dias)
VALUES (sqlc.narg('empresa_id'), @grabar, @duracion_max_persona_min, @segundos_asignado, @retencion_grabaciones_dias)
ON CONFLICT DO NOTHING
RETURNING *;

-- name: ActualizarVozAjustes :one
UPDATE mensajeria.voz_ajustes SET grabar = @grabar, duracion_max_persona_min = @duracion_max_persona_min,
    segundos_asignado = @segundos_asignado, retencion_grabaciones_dias = @retencion_grabaciones_dias, version = version + 1, updated_at = now()
WHERE id = @id
RETURNING *;

-- name: VozAjustesCanal :one
SELECT * FROM mensajeria.voz_ajustes_canal WHERE canal_id = @canal_id;

-- name: VozAjustesCanalParaActualizar :one
SELECT * FROM mensajeria.voz_ajustes_canal WHERE canal_id = @canal_id FOR UPDATE;

-- name: GuardarVozAjustesCanal :one
INSERT INTO mensajeria.voz_ajustes_canal (canal_id, empresa_id, llamadas_activas, requisitos, cola_id, horario_id, segundos_sonando,
    fuera_de_horario, icono_visible, devolucion, horas_meta, mensaje_perdida, mensaje_perdida_texto, sincronizado_en)
VALUES (@canal_id, sqlc.narg('empresa_id'), @llamadas_activas, sqlc.narg('requisitos'), sqlc.narg('cola_id'), sqlc.narg('horario_id'), @segundos_sonando,
    @fuera_de_horario, @icono_visible, @devolucion, sqlc.narg('horas_meta'), @mensaje_perdida, @mensaje_perdida_texto, sqlc.narg('sincronizado_en'))
ON CONFLICT (canal_id) DO UPDATE SET llamadas_activas = EXCLUDED.llamadas_activas, requisitos = EXCLUDED.requisitos, cola_id = EXCLUDED.cola_id,
    horario_id = EXCLUDED.horario_id, segundos_sonando = EXCLUDED.segundos_sonando, fuera_de_horario = EXCLUDED.fuera_de_horario,
    icono_visible = EXCLUDED.icono_visible, devolucion = EXCLUDED.devolucion, horas_meta = EXCLUDED.horas_meta,
    mensaje_perdida = EXCLUDED.mensaje_perdida, mensaje_perdida_texto = EXCLUDED.mensaje_perdida_texto,
    sincronizado_en = EXCLUDED.sincronizado_en, version = mensajeria.voz_ajustes_canal.version + 1, updated_at = now()
RETURNING *;

-- name: InsertarDevolucion :one
INSERT INTO mensajeria.llamada_devoluciones (empresa_id, conversacion_id, llamada_id, pedida_por, estado, vence_en)
VALUES (sqlc.narg('empresa_id'), @conversacion_id, @llamada_id, 'sistema', 'pendiente', @vence_en)
ON CONFLICT (conversacion_id) WHERE estado = 'pendiente' DO NOTHING
RETURNING *;

-- name: DevolucionParaActualizar :one
SELECT * FROM mensajeria.llamada_devoluciones WHERE id = @id AND coalesce(empresa_id, 0) = @empresa::bigint FOR UPDATE;

-- name: CerrarDevolucion :one
UPDATE mensajeria.llamada_devoluciones SET estado = @estado, atendida_por = sqlc.narg('atendida_por'), motivo = sqlc.narg('motivo'), updated_at = now()
WHERE id = @id AND estado = 'pendiente'
RETURNING *;

-- name: DevolucionesVencidas :many
SELECT * FROM mensajeria.llamada_devoluciones WHERE estado = 'pendiente' AND vence_en <= @ahora ORDER BY id LIMIT 200 FOR UPDATE SKIP LOCKED;

-- name: ListarDevoluciones :many
SELECT d.* FROM mensajeria.llamada_devoluciones d
JOIN mensajeria.conversaciones c ON c.id = d.conversacion_id
WHERE coalesce(d.empresa_id, 0) = @empresa::bigint AND d.estado = @estado
  AND mensajeria.conv_visible(@usuario::bigint, @supervisa::boolean, c.id, c.cola_id, c.asignado_a)
  AND (sqlc.narg('antes_de')::bigint IS NULL OR d.id < sqlc.narg('antes_de')::bigint)
ORDER BY d.id DESC
LIMIT @limite;

-- name: InsertarGrabacion :one
INSERT INTO mensajeria.llamada_grabaciones (llamada_id, empresa_id, origen, estado, url_descarga, vence_en)
VALUES (@llamada_id, sqlc.narg('empresa_id'), 'meta', 'pendiente', @url_descarga, @vence_en)
ON CONFLICT (llamada_id) DO NOTHING
RETURNING *;

-- name: GrabacionDeLlamada :one
SELECT * FROM mensajeria.llamada_grabaciones WHERE llamada_id = @llamada_id;

-- name: GrabacionParaActualizar :one
SELECT * FROM mensajeria.llamada_grabaciones WHERE id = @id FOR UPDATE;

-- name: GrabacionesDeLlamadas :many
SELECT llamada_id, estado FROM mensajeria.llamada_grabaciones WHERE llamada_id = ANY (@ids::uuid[]);

-- name: GuardarGrabacion :one
UPDATE mensajeria.llamada_grabaciones SET estado = @estado, adjunto_id = sqlc.narg('adjunto_id'), duracion_seg = sqlc.narg('duracion_seg'),
    sha256 = sqlc.narg('sha256'), url_descarga = NULL, borrada_en = sqlc.narg('borrada_en'), borrada_por = sqlc.narg('borrada_por'),
    motivo_borrado = sqlc.narg('motivo_borrado'), updated_at = now()
WHERE id = @id
RETURNING *;

-- name: GrabacionesVencidas :many
SELECT * FROM mensajeria.llamada_grabaciones WHERE estado = 'disponible' AND vence_en <= @ahora ORDER BY id LIMIT 100 FOR UPDATE SKIP LOCKED;

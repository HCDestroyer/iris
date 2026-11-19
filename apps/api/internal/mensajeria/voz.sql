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
SET estado = 'conectando', reclamada_por = @usuario_id, reclamada_hasta = @hasta, reclamada_navegador = @navegador::text, updated_at = now()
WHERE l.id = @id AND coalesce(l.empresa_id, 0) = @empresa::bigint AND l.estado = 'sonando'
  -- Todavía suena, y no hay una acción con Meta a medias (un pre_accept o accept de la reserva anterior).
  AND l.suena_hasta > @ahora::timestamptz AND l.accion_meta_estado IS DISTINCT FROM 'pendiente'
  AND EXISTS (SELECT 1 FROM mensajeria.llamada_candidatos c WHERE c.llamada_id = l.id AND c.usuario_id = @usuario_id AND c.retirado_en IS NULL)
  -- Quien ya tiene una llamada viva no reclama otra.
  AND NOT EXISTS (SELECT 1 FROM mensajeria.llamadas o WHERE o.id <> l.id AND o.estado IN ('conectando', 'en_curso', 'marcando')
      AND (o.usuario_id = @usuario_id OR o.reclamada_por = @usuario_id))
RETURNING *;

-- name: EsCandidato :one
SELECT EXISTS (SELECT 1 FROM mensajeria.llamada_candidatos c JOIN mensajeria.llamadas l ON l.id = c.llamada_id
    WHERE c.llamada_id = @id AND c.usuario_id = @usuario_id AND c.retirado_en IS NULL AND coalesce(l.empresa_id, 0) = @empresa::bigint)::boolean;

-- name: FueCandidato :one
-- Se le ofreció la llamada alguna vez (aunque ya esté retirado).
SELECT EXISTS (SELECT 1 FROM mensajeria.llamada_candidatos c JOIN mensajeria.llamadas l ON l.id = c.llamada_id
    WHERE c.llamada_id = @id AND c.usuario_id = @usuario_id AND coalesce(l.empresa_id, 0) = @empresa::bigint)::boolean;

-- name: GuardarLlamada :one
-- Escribe el estado que calculó el caso de uso (la fila se leyó con FOR UPDATE).
UPDATE mensajeria.llamadas
SET estado = @estado, atendida_por = sqlc.narg('atendida_por'), usuario_id = sqlc.narg('usuario_id'),
    reclamada_por = sqlc.narg('reclamada_por'), reclamada_hasta = sqlc.narg('reclamada_hasta'), reclamada_navegador = sqlc.narg('reclamada_navegador'), motivo_fin = sqlc.narg('motivo_fin'),
    contestada_en = sqlc.narg('contestada_en'), terminada_en = sqlc.narg('terminada_en'), duracion_seg = sqlc.narg('duracion_seg'),
    nota = sqlc.narg('nota'), resultado = sqlc.narg('resultado'), sdp_oferta = sqlc.narg('sdp_oferta'),
    accion_meta = sqlc.narg('accion_meta'), accion_meta_estado = sqlc.narg('accion_meta_estado'), accion_meta_en = sqlc.narg('accion_meta_en'),
    accion_meta_por = sqlc.narg('accion_meta_por'), accion_meta_no_existe = @accion_meta_no_existe,
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
  AND NOT EXISTS (SELECT 1 FROM mensajeria.llamadas l WHERE l.estado IN ('conectando', 'en_curso', 'marcando')
       AND (l.usuario_id = d.usuario_id OR l.reclamada_por = d.usuario_id))
ORDER BY d.usuario_id;

-- name: FijarReceptor :one
INSERT INTO mensajeria.llamada_receptores (empresa_id, usuario_id, navegador, recibe, visto_en)
VALUES (sqlc.narg('empresa_id'), @usuario_id, @navegador, @recibe, @ahora)
ON CONFLICT ((coalesce(empresa_id, 0)), usuario_id, navegador)
DO UPDATE SET recibe = EXCLUDED.recibe, visto_en = EXCLUDED.visto_en, apagado_motivo = NULL, updated_at = now()
RETURNING *;

-- name: ReceptorDe :one
SELECT * FROM mensajeria.llamada_receptores
WHERE coalesce(empresa_id, 0) = @empresa::bigint AND usuario_id = @usuario_id AND navegador = @navegador;

-- name: ReceptorVivo :one
SELECT EXISTS (SELECT 1 FROM mensajeria.llamada_receptores
    WHERE coalesce(empresa_id, 0) = @empresa::bigint AND usuario_id = @usuario_id AND recibe AND visto_en > @vivo_desde)::boolean;

-- name: TocarReceptores :exec
-- Latido del flujo SSE del usuario.
-- El latido de «Recibir llamadas» (entrantes), por navegador: si el flujo dice de qué navegador es,
-- toca solo esa fila; sin navegador (paneles viejos), las que reciben llamadas. Una saliente no se
-- sostiene con esto: su latido va por pestaña (TocarPestanaDeLlamada).
UPDATE mensajeria.llamada_receptores r SET visto_en = @ahora
WHERE coalesce(r.empresa_id, 0) = @empresa::bigint AND r.usuario_id = @usuario_id
  AND ((sqlc.narg('navegador')::text IS NULL AND r.recibe) OR r.navegador = sqlc.narg('navegador')::text);

-- name: TocarPestanaDeLlamada :exec
-- El latido de una saliente: solo el flujo de eventos de la pestaña desde la que se llamó. Otra
-- pestaña del mismo navegador (mismo id de navegador) u otro equipo no la sostienen.
UPDATE mensajeria.llamadas SET pestana_vista_en = @ahora
WHERE usuario_id = @usuario_id AND pestana = @pestana::text AND direccion = 'saliente' AND estado IN ('marcando', 'en_curso');

-- name: LlamadaActivaDeUsuario :one
SELECT * FROM mensajeria.llamadas
WHERE coalesce(empresa_id, 0) = @empresa::bigint AND estado IN ('conectando', 'en_curso', 'marcando')
  AND (usuario_id = @usuario_id OR reclamada_por = @usuario_id)
ORDER BY sono_en DESC LIMIT 1;

-- name: LlamadasRecientesDeNumero :one
-- Ráfaga (ventana deslizante): llamadas ENTRANTES de ese número a ese canal que sonaron desde un
-- instante. Las que ya rechazó la ráfaga no la alimentan (si no, cada reintento la renovaría y el
-- número no saldría nunca) y las salientes hacia ese número no cuentan.
SELECT count(*)::bigint FROM mensajeria.llamadas
WHERE canal_id = @canal_id AND numero = @numero AND sono_en > @desde AND direccion = 'entrante'
  AND NOT (estado = 'rechazada' AND motivo_fin IS NOT DISTINCT FROM 'limite');

-- name: AvisoPerdidaReciente :one
SELECT EXISTS (SELECT 1 FROM mensajeria.llamadas
    WHERE conversacion_id = @conversacion_id AND aviso_perdida AND sono_en > @desde)::boolean;

-- name: LlamadasVivas :many
-- Para el mantenimiento (cada 5 s): todo lo que no terminó.
SELECT * FROM mensajeria.llamadas WHERE estado IN ('sonando', 'marcando', 'conectando', 'en_curso') ORDER BY sono_en LIMIT 200;

-- name: LlamadasConAccionPendiente :many
-- Intenciones pre_accept o accept que quedaron a medias (el proceso murió entre la intención y el
-- resultado): las revisa el mantenimiento, que no las repite a ciegas.
SELECT * FROM mensajeria.llamadas WHERE accion_meta_estado = 'pendiente' AND accion_meta_en < @antes
  AND estado IN ('sonando', 'marcando', 'conectando', 'en_curso') ORDER BY accion_meta_en LIMIT 100;

-- name: TomarAccionesMeta :many
-- El job de escrituras a Meta toma las reject y terminate pendientes de llamadas ya cerradas cuyo
-- intento ya toca (SKIP LOCKED: dos réplicas no toman la misma) y las deja apartadas lote × plazo
-- (aparte), para que otro ciclo no las retome mientras este las atiende.
UPDATE mensajeria.llamadas SET accion_meta_en = @aparte::timestamptz
WHERE id IN (SELECT l.id FROM mensajeria.llamadas l
    WHERE l.accion_meta_estado = 'pendiente' AND l.accion_meta IN ('reject', 'terminate') AND l.accion_meta_en <= @antes
      AND l.estado IN ('terminada', 'perdida', 'rechazada', 'fuera_de_horario', 'fallida')
    ORDER BY l.accion_meta_en LIMIT @lote FOR UPDATE SKIP LOCKED)
RETURNING id, terminada_en;

-- name: AgotarAccionMeta :exec
UPDATE mensajeria.llamadas SET accion_meta_estado = 'fallida' WHERE id = @id AND accion_meta_estado = 'pendiente';

-- name: TomarCanalesMetaPendiente :many
-- Lo mismo para los ajustes de número pendientes de aplicar en WhatsApp.
UPDATE mensajeria.voz_ajustes_canal SET meta_proximo_en = @aparte::timestamptz
WHERE canal_id IN (SELECT a.canal_id FROM mensajeria.voz_ajustes_canal a WHERE a.meta_estado = 'pendiente' AND a.meta_proximo_en <= @ahora
    ORDER BY a.meta_proximo_en LIMIT @lote FOR UPDATE SKIP LOCKED)
RETURNING canal_id;

-- name: EntrantesDeLlamada :many
-- Los webhooks guardados que traen esa llamada (para tapar su SDP al terminar).
SELECT id, payload FROM mensajeria.entrantes
WHERE canal_id = @canal_id AND created_at >= @desde AND payload::text LIKE '%' || @marca::text || '%'
ORDER BY id LIMIT 50;

-- name: ApagarReceptoresDeMas :exec
-- Tope de navegadores por usuario en «Recibir llamadas»: se apagan (con su motivo, para que ese
-- navegador sepa por qué) los que llevan 30 días sin verse y, de los demás, los más viejos que pasen
-- del tope.
UPDATE mensajeria.llamada_receptores r
SET recibe = false, updated_at = now(),
    apagado_motivo = CASE WHEN r.visto_en < sqlc.arg('viejo')::timestamptz THEN 'inactivo' ELSE 'tope' END
WHERE coalesce(r.empresa_id, 0) = @empresa::bigint AND r.usuario_id = @usuario_id AND r.navegador <> @navegador AND r.recibe
  AND (r.visto_en < sqlc.arg('viejo')::timestamptz OR r.id IN (SELECT x.id FROM mensajeria.llamada_receptores x
        WHERE coalesce(x.empresa_id, 0) = @empresa::bigint AND x.usuario_id = @usuario_id AND x.navegador <> @navegador AND x.recibe
        ORDER BY x.visto_en DESC, x.id DESC OFFSET @conservar));

-- name: BorrarReceptoresSobrantes :exec
-- De los apagados solo se guardan los más recientes: la tabla no crece sin límite.
DELETE FROM mensajeria.llamada_receptores r
WHERE coalesce(r.empresa_id, 0) = @empresa::bigint AND r.usuario_id = @usuario_id AND r.navegador <> @navegador AND NOT r.recibe
  AND r.id IN (SELECT x.id FROM mensajeria.llamada_receptores x
        WHERE coalesce(x.empresa_id, 0) = @empresa::bigint AND x.usuario_id = @usuario_id AND x.navegador <> @navegador AND NOT x.recibe
        ORDER BY x.visto_en DESC, x.id DESC OFFSET @conservar);

-- name: ServicioPrincipal :one
-- El servicio que se muestra con la llamada: el de la conversación; si no, uno activo del cliente.
SELECT s.id, s.codigo, s.plan_nombre, s.estado FROM integracion.argos_servicios s
WHERE s.cliente_id = @cliente_id AND s.empresa_id = @empresa::bigint
ORDER BY (s.id = coalesce(sqlc.narg('servicio_id')::bigint, 0)) DESC, (s.estado = 'activo') DESC, s.id
LIMIT 1;

-- name: CanalRecibioLlamadas :one
-- ¿Ha llegado alguna vez un webhook de llamada a este número? (prueba de que el campo calls está suscrito)
SELECT EXISTS (SELECT 1 FROM mensajeria.llamadas WHERE canal_id = @canal_id)::boolean;

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
INSERT INTO mensajeria.voz_ajustes (empresa_id, grabar, duracion_max_persona_min, segundos_asignado, retencion_grabaciones_dias,
    max_salientes_por_dia, tope_minutos_salientes_mes, max_colgadas_marcando_dia)
VALUES (sqlc.narg('empresa_id'), @grabar, @duracion_max_persona_min, @segundos_asignado, @retencion_grabaciones_dias,
    @max_salientes_por_dia, @tope_minutos_salientes_mes, @max_colgadas_marcando_dia)
ON CONFLICT DO NOTHING
RETURNING *;

-- name: ActualizarVozAjustes :one
UPDATE mensajeria.voz_ajustes SET grabar = @grabar, duracion_max_persona_min = @duracion_max_persona_min,
    segundos_asignado = @segundos_asignado, retencion_grabaciones_dias = @retencion_grabaciones_dias,
    max_salientes_por_dia = @max_salientes_por_dia, tope_minutos_salientes_mes = @tope_minutos_salientes_mes,
    max_colgadas_marcando_dia = @max_colgadas_marcando_dia, version = version + 1, updated_at = now()
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

-- name: FijarMetaAjustesCanal :one
-- El estado de la escritura en WhatsApp de los ajustes del número (pendiente con reintentos).
UPDATE mensajeria.voz_ajustes_canal SET meta_estado = @meta_estado, meta_intentos = @meta_intentos, meta_proximo_en = sqlc.narg('meta_proximo_en'),
    meta_detalle = sqlc.narg('meta_detalle'), horas_meta = sqlc.narg('horas_meta'),
    sincronizado_en = coalesce(sqlc.narg('sincronizado_en'), sincronizado_en), updated_at = now()
WHERE canal_id = @canal_id
RETURNING *;

-- name: MarcarBuzonDeVoz :exec
UPDATE mensajeria.conversacion_mensajes SET buzon_de_voz = true WHERE id = @id;

-- name: LlamadaAlBuzonReciente :one
-- ¿Hubo una llamada de esta conversación que se dejó al buzón de voz (fuera de horario) hace poco?
SELECT EXISTS (SELECT 1 FROM mensajeria.llamadas l JOIN mensajeria.voz_ajustes_canal a ON a.canal_id = l.canal_id
    WHERE l.conversacion_id = @conversacion_id AND l.estado = 'fuera_de_horario' AND a.fuera_de_horario = 'buzon'
      AND l.sono_en > @desde AND l.sono_en <= @hasta)::boolean;

-- Salientes y permiso de llamada (E1b, §3.7) ---------------------------------------------------------

-- name: InsertarLlamadaSaliente :one
-- Nace marcando, con la intención connect pendiente y un id externo provisional hasta que Meta da el suyo.
-- La oferta SDP no se guarda: va de la petición a Meta en memoria.
INSERT INTO mensajeria.llamadas (id, empresa_id, origen, canal_id, conversacion_id, id_externo, direccion, numero, cliente_id, servicio_id,
    cola_id, estado, usuario_id, navegador, pestana, pestana_vista_en, devolucion_id, sono_en, suena_hasta, accion_meta, accion_meta_estado, accion_meta_en)
VALUES (sqlc.arg('llamada_id'), sqlc.narg('empresa_id'), 'whatsapp', sqlc.arg('canal_id'), sqlc.arg('conversacion_id'), sqlc.arg('externo'), 'saliente', sqlc.arg('numero'),
    sqlc.narg('cliente_id'), sqlc.narg('servicio'), sqlc.narg('cola_id'), 'marcando', sqlc.arg('usuario_id'), sqlc.arg('navegador'), sqlc.narg('pestana'), sqlc.narg('pestana_vista_en'), sqlc.narg('devolucion_id'),
    sqlc.arg('sono_en'), sqlc.arg('suena_hasta'), 'connect', 'pendiente', sqlc.arg('sono_en'))
RETURNING *;

-- name: FijarExternoLlamada :one
-- Meta aceptó el connect: la llamada toma el call_id de esa respuesta (es el que manda).
UPDATE mensajeria.llamadas SET id_externo = @externo, accion_meta_estado = 'hecha', sdp_oferta = NULL, updated_at = now()
WHERE id = @id AND estado = 'marcando' AND accion_meta = 'connect' AND accion_meta_estado = 'pendiente'
RETURNING *;

-- name: SalienteSinCallID :one
-- La saliente a ese número que todavía espera la respuesta del connect (un webhook pudo adelantarse).
SELECT * FROM mensajeria.llamadas
WHERE canal_id = @canal_id AND numero = @numero AND direccion = 'saliente' AND estado = 'marcando' AND id_externo LIKE 'pendiente:%'
ORDER BY sono_en DESC LIMIT 1 FOR UPDATE;

-- name: FijarWebhookAdelantado :exec
-- Lo que trajo ese webhook queda apartado: solo vale si la respuesta del connect da el mismo call_id.
UPDATE mensajeria.llamadas SET externo_webhook = @externo, sdp_respuesta = sqlc.narg('sdp'), updated_at = now() WHERE id = @id;

-- name: QuitarWebhookAdelantado :exec
UPDATE mensajeria.llamadas SET externo_webhook = NULL, sdp_respuesta = CASE WHEN @conservar::boolean THEN sdp_respuesta END, updated_at = now() WHERE id = @id;

-- name: RegistrarHuerfana :exec
INSERT INTO mensajeria.llamadas_huerfanas (canal_id, call_id, proximo_en) VALUES (@canal_id, @call_id, @proximo_en)
ON CONFLICT (canal_id, call_id) DO NOTHING;

-- name: RegistrarAjena :one
-- Un call_id saliente que no es de Iris: se anota para diagnóstico y no se termina.
INSERT INTO mensajeria.llamadas_huerfanas (canal_id, call_id, estado, proximo_en) VALUES (@canal_id, @call_id, 'ajena', @ahora)
ON CONFLICT (canal_id, call_id) DO NOTHING
RETURNING id;

-- name: SalienteRecienteANumero :one
-- ¿Iris mandó un connect a ese número desde ese canal hace poco? (cualquier estado: pendiente, cerrada o fallida)
SELECT EXISTS (SELECT 1 FROM mensajeria.llamadas WHERE canal_id = @canal_id AND numero = @numero AND direccion = 'saliente' AND sono_en >= @desde)::boolean;

-- name: TomarHuerfanas :many
-- El job de escrituras a Meta toma las que toca terminar y las aparta lote × plazo (SKIP LOCKED).
UPDATE mensajeria.llamadas_huerfanas SET intentos = intentos + 1, proximo_en = @aparte::timestamptz, updated_at = now()
WHERE id IN (SELECT h.id FROM mensajeria.llamadas_huerfanas h WHERE h.estado = 'pendiente' AND h.proximo_en <= @ahora
    ORDER BY h.proximo_en LIMIT @lote FOR UPDATE SKIP LOCKED)
RETURNING id, canal_id, call_id, intentos;

-- name: CerrarHuerfana :exec
UPDATE mensajeria.llamadas_huerfanas SET estado = @estado, updated_at = now() WHERE id = @id;

-- name: ColgadasMarcandoDesde :one
-- Salientes a ese contacto que terminaron mientras marcaban sin que el cliente contestara: las que
-- colgó el agente y las que cortó el servidor porque la pestaña que llamó murió (timbrar y colgar, o
-- timbrar y cerrar la pestaña).
SELECT count(*)::bigint FROM mensajeria.llamadas
WHERE coalesce(empresa_id, 0) = @empresa::bigint AND numero = @numero AND direccion = 'saliente' AND estado = 'terminada'
  AND motivo_fin IN ('agente_colgo', 'error_medio') AND contestada_en IS NULL AND sono_en >= @desde;

-- name: SolicitudesDeNumero :many
-- Las solicitudes de permiso a ese contacto desde cualquier número de la empresa.
SELECT solicitudes FROM mensajeria.llamada_permisos WHERE coalesce(empresa_id, 0) = @empresa::bigint AND numero = @numero;

-- name: FijarRespuestaSDP :exec
UPDATE mensajeria.llamadas SET sdp_respuesta = sqlc.narg('sdp'), updated_at = now() WHERE id = @id;

-- name: SalientesDesde :one
-- Salientes de la empresa desde un instante (límite por día).
SELECT count(*)::bigint FROM mensajeria.llamadas
WHERE coalesce(empresa_id, 0) = @empresa::bigint AND direccion = 'saliente' AND sono_en >= @desde AND estado <> 'fallida';

-- name: ConectadasDelCanalDesde :one
-- Salientes conectadas de un número desde un instante (Meta: 100 por 24 h).
SELECT count(*)::bigint FROM mensajeria.llamadas
WHERE canal_id = @canal_id AND direccion = 'saliente' AND contestada_en IS NOT NULL AND sono_en >= @desde;

-- name: SegundosSalientes :one
-- Segundos de salientes contestadas de la empresa en un periodo (tope de minutos del mes). Las que
-- siguen en curso cuentan lo que llevan.
SELECT (coalesce(sum(duracion_seg) FILTER (WHERE estado <> 'en_curso'), 0)
    + coalesce(sum(GREATEST(0, floor(extract(epoch FROM (sqlc.arg('ahora')::timestamptz - contestada_en))))) FILTER (WHERE estado = 'en_curso'), 0))::bigint
FROM mensajeria.llamadas
WHERE coalesce(empresa_id, 0) = @empresa::bigint AND direccion = 'saliente' AND contestada_en IS NOT NULL
  AND sono_en >= @desde AND sono_en < @hasta AND id <> @menos;

-- name: SalienteVivaDeUsuario :one
-- En cualquier empresa: una persona hace una sola saliente a la vez (es lo que impone el índice único).
SELECT EXISTS (SELECT 1 FROM mensajeria.llamadas WHERE usuario_id = @usuario_id AND direccion = 'saliente' AND estado IN ('marcando', 'en_curso'))::boolean;

-- name: LlamadaVivaDeNumero :one
SELECT EXISTS (SELECT 1 FROM mensajeria.llamadas WHERE canal_id = @canal_id AND numero = @numero
    AND estado IN ('sonando', 'marcando', 'conectando', 'en_curso'))::boolean;

-- name: PermisoLlamada :one
SELECT * FROM mensajeria.llamada_permisos WHERE coalesce(empresa_id, 0) = @empresa::bigint AND canal_id = @canal_id AND numero = @numero;

-- name: PermisoLlamadaParaActualizar :one
SELECT * FROM mensajeria.llamada_permisos WHERE coalesce(empresa_id, 0) = @empresa::bigint AND canal_id = @canal_id AND numero = @numero FOR UPDATE;

-- name: CrearPermisoLlamada :exec
INSERT INTO mensajeria.llamada_permisos (empresa_id, canal_id, numero) VALUES (sqlc.narg('empresa_id'), @canal_id, @numero)
ON CONFLICT DO NOTHING;

-- name: GuardarPermisoLlamada :one
UPDATE mensajeria.llamada_permisos SET estado = @estado, vence_en = sqlc.narg('vence_en'), solicitudes = @solicitudes, sin_contestar = @sin_contestar,
    ultima_solicitud_mensaje_id = sqlc.narg('ultima_solicitud_mensaje_id'), sincronizado_en = sqlc.narg('sincronizado_en'),
    solicitud_sin_confirmar = @solicitud_sin_confirmar, updated_at = now()
WHERE id = @id
RETURNING *;

-- name: TomarSolicitudesSinConfirmar :many
-- Solicitudes de permiso cuyo envío quedó sin respuesta de Meta: el job de escrituras las resuelve
-- preguntando a Meta, con espera creciente entre intentos (1, 2, 5, 15 y después 60 minutos; SKIP
-- LOCKED). No toca sincronizado_en: un intento no es una lectura buena.
UPDATE mensajeria.llamada_permisos p SET solicitud_intentos = p.solicitud_intentos + 1,
    solicitud_proximo_en = sqlc.arg('ahora')::timestamptz
        + (CASE p.solicitud_intentos WHEN 0 THEN 1 WHEN 1 THEN 2 WHEN 2 THEN 5 WHEN 3 THEN 15 ELSE 60 END) * interval '1 minute'
WHERE p.id IN (SELECT x.id FROM mensajeria.llamada_permisos x
    WHERE x.solicitud_sin_confirmar AND (x.solicitud_proximo_en IS NULL OR x.solicitud_proximo_en <= sqlc.arg('ahora')::timestamptz)
    ORDER BY x.solicitud_proximo_en NULLS FIRST LIMIT @lote FOR UPDATE SKIP LOCKED)
RETURNING p.*;

-- name: MarcarSolicitudSinConfirmar :exec
UPDATE mensajeria.llamada_permisos SET solicitud_sin_confirmar = true, solicitud_intentos = 0, solicitud_proximo_en = NULL, updated_at = now() WHERE id = @id;

-- name: PermisoLlamadaPorID :one
SELECT * FROM mensajeria.llamada_permisos WHERE id = @id FOR UPDATE;

-- name: PermisosDeNumero :many
-- Las filas de permiso de ese número en ese canal (la respuesta del cliente vale para todas).
SELECT * FROM mensajeria.llamada_permisos WHERE canal_id = @canal_id AND numero = @numero ORDER BY id FOR UPDATE;

-- name: FijarPlantillaPermiso :exec
UPDATE mensajeria.voz_ajustes_canal SET plantilla_permiso = sqlc.narg('plantilla'), plantilla_permiso_idioma = @idioma, updated_at = now() WHERE canal_id = @canal_id;

-- name: MarcarDevolucionHecha :one
UPDATE mensajeria.llamada_devoluciones SET estado = 'hecha', atendida_por = @usuario_id, llamada_devuelta_id = @llamada_id, updated_at = now()
WHERE id = @id AND estado = 'pendiente'
RETURNING *;

-- name: DevolucionPendiente :one
SELECT * FROM mensajeria.llamada_devoluciones WHERE id = @id AND conversacion_id = @conversacion_id AND estado = 'pendiente';

-- name: RegistrarRechazadaApagada :exec
-- Una entrante rechazada porque el número tiene las llamadas apagadas en Iris. Acotado a 500 por
-- día y número: una ráfaga no llena la tabla.
INSERT INTO mensajeria.llamadas_rechazadas_apagadas (canal_id, ocurrido_en)
SELECT @canal_id::bigint, @ahora::timestamptz
WHERE (SELECT count(*) FROM mensajeria.llamadas_rechazadas_apagadas r WHERE r.canal_id = @canal_id::bigint AND r.ocurrido_en >= @hoy::timestamptz) < 500;

-- name: BorrarRechazadasApagadasViejas :exec
DELETE FROM mensajeria.llamadas_rechazadas_apagadas r WHERE r.canal_id = @canal_id AND r.ocurrido_en < @antes::timestamptz;

-- name: RechazadasApagadas :one
-- La última de los últimos 7 días y cuántas van hoy.
SELECT coalesce(max(r.ocurrido_en), 'epoch'::timestamptz)::timestamptz AS ultima_en,
    (count(*) FILTER (WHERE r.ocurrido_en >= @hoy::timestamptz))::integer AS hoy, count(*)::integer AS total
FROM mensajeria.llamadas_rechazadas_apagadas r WHERE r.canal_id = @canal_id AND r.ocurrido_en >= @desde::timestamptz;

-- Bandeja de conversaciones con clientes (contratos/conversaciones.md). sqlc: mensajeriabd.

-- Canales (varios números) --------------------------------------------------------------------

-- name: CanalAvisosDeEmpresa :one
-- El número por el que salen los avisos de la empresa.
SELECT * FROM mensajeria.canales
WHERE coalesce(empresa_id, 0) = @empresa_id::bigint AND canal = @canal AND activo AND avisos_empresa;

-- name: HayCanalAvisos :one
SELECT EXISTS (SELECT 1 FROM mensajeria.canales WHERE coalesce(empresa_id, 0) = @empresa_id::bigint AND activo AND avisos_empresa AND id <> @excepto::bigint)::boolean;

-- name: QuitarAvisosEmpresa :many
UPDATE mensajeria.canales SET avisos_empresa = false, version = version + 1, updated_at = now()
WHERE coalesce(empresa_id, 0) = @empresa_id::bigint AND avisos_empresa AND id <> @excepto::bigint
RETURNING id;

-- name: ConfigurarCanal :one
UPDATE mensajeria.canales
SET usos = @usos::text[], avisos_empresa = @avisos_empresa, cola_id = sqlc.narg('cola_id'), version = version + 1, updated_at = now()
WHERE id = @id
RETURNING *;

-- Colas ---------------------------------------------------------------------------------------

-- name: ListarColas :many
SELECT * FROM mensajeria.colas WHERE coalesce(empresa_id, 0) = @empresa::bigint ORDER BY lower(nombre), id;

-- name: ColaPorID :one
SELECT * FROM mensajeria.colas WHERE id = @id AND coalesce(empresa_id, 0) = @empresa::bigint;

-- name: ColaParaActualizar :one
SELECT * FROM mensajeria.colas WHERE id = @id AND coalesce(empresa_id, 0) = @empresa::bigint FOR UPDATE;

-- name: InsertarCola :one
INSERT INTO mensajeria.colas (empresa_id, nombre, descripcion, sla_respuesta_min, sla_por_vencer_min)
VALUES (sqlc.narg('empresa_id'), @nombre, @descripcion, sqlc.narg('sla_respuesta_min'), sqlc.narg('sla_por_vencer_min'))
RETURNING *;

-- name: ActualizarCola :one
UPDATE mensajeria.colas
SET nombre = @nombre, descripcion = @descripcion, sla_respuesta_min = sqlc.narg('sla_respuesta_min'),
    sla_por_vencer_min = sqlc.narg('sla_por_vencer_min'), version = version + 1, updated_at = now()
WHERE id = @id
RETURNING *;

-- name: FijarColaActiva :one
UPDATE mensajeria.colas SET activa = @activa, version = version + 1, updated_at = now() WHERE id = @id RETURNING *;

-- name: TocarCola :one
UPDATE mensajeria.colas SET version = version + 1, updated_at = now() WHERE id = @id RETURNING *;

-- name: MiembrosDeColas :many
SELECT m.cola_id, u.id, u.nombre FROM mensajeria.cola_miembros m JOIN nucleo.usuarios u ON u.id = m.usuario_id
WHERE m.cola_id = ANY (@colas::bigint[])
ORDER BY m.cola_id, lower(u.nombre), u.id;

-- name: BorrarMiembrosCola :exec
DELETE FROM mensajeria.cola_miembros WHERE cola_id = @cola_id;

-- name: InsertarMiembroCola :exec
INSERT INTO mensajeria.cola_miembros (cola_id, usuario_id) VALUES (@cola_id, @usuario_id) ON CONFLICT DO NOTHING;

-- name: EsMiembroCola :one
SELECT EXISTS (SELECT 1 FROM mensajeria.cola_miembros WHERE cola_id = @cola_id AND usuario_id = @usuario_id)::boolean;

-- Etiquetas -----------------------------------------------------------------------------------

-- name: ListarEtiquetas :many
SELECT * FROM mensajeria.etiquetas WHERE coalesce(empresa_id, 0) = @empresa::bigint ORDER BY lower(nombre), id;

-- name: EtiquetaParaActualizar :one
SELECT * FROM mensajeria.etiquetas WHERE id = @id AND coalesce(empresa_id, 0) = @empresa::bigint FOR UPDATE;

-- name: InsertarEtiqueta :one
INSERT INTO mensajeria.etiquetas (empresa_id, nombre, color) VALUES (sqlc.narg('empresa_id'), @nombre, @color) RETURNING *;

-- name: ActualizarEtiqueta :one
UPDATE mensajeria.etiquetas SET nombre = @nombre, color = @color, version = version + 1, updated_at = now()
WHERE id = @id RETURNING *;

-- name: FijarEtiquetaActiva :one
UPDATE mensajeria.etiquetas SET activa = @activa, version = version + 1, updated_at = now() WHERE id = @id RETURNING *;

-- name: EtiquetasActivasPorIDs :many
SELECT * FROM mensajeria.etiquetas
WHERE id = ANY (@ids::bigint[]) AND coalesce(empresa_id, 0) = @empresa::bigint AND activa;

-- name: EtiquetasDeConversaciones :many
SELECT ce.conversacion_id, e.id, e.nombre, e.color
FROM mensajeria.conversacion_etiquetas ce JOIN mensajeria.etiquetas e ON e.id = ce.etiqueta_id
WHERE ce.conversacion_id = ANY (@ids::bigint[])
ORDER BY ce.conversacion_id, lower(e.nombre);

-- name: BorrarEtiquetasConversacion :exec
DELETE FROM mensajeria.conversacion_etiquetas WHERE conversacion_id = @conversacion_id;

-- name: InsertarEtiquetaConversacion :exec
INSERT INTO mensajeria.conversacion_etiquetas (conversacion_id, etiqueta_id) VALUES (@conversacion_id, @etiqueta_id) ON CONFLICT DO NOTHING;

-- Respuestas rápidas --------------------------------------------------------------------------

-- name: ListarRespuestasRapidas :many
SELECT * FROM mensajeria.respuestas_rapidas
WHERE coalesce(empresa_id, 0) = @empresa::bigint
  AND (@todas::boolean OR activa)
  AND (sqlc.narg('cola_id')::bigint IS NULL OR cola_id IS NULL OR cola_id = sqlc.narg('cola_id')::bigint)
  AND (@q::text = '' OR atajo ILIKE '%' || @q::text || '%' OR titulo ILIKE '%' || @q::text || '%')
ORDER BY atajo;

-- name: RespuestaRapidaParaActualizar :one
SELECT * FROM mensajeria.respuestas_rapidas WHERE id = @id AND coalesce(empresa_id, 0) = @empresa::bigint FOR UPDATE;

-- name: RespuestaRapidaPorID :one
SELECT * FROM mensajeria.respuestas_rapidas WHERE id = @id AND coalesce(empresa_id, 0) = @empresa::bigint;

-- name: InsertarRespuestaRapida :one
INSERT INTO mensajeria.respuestas_rapidas (empresa_id, cola_id, atajo, titulo, texto)
VALUES (sqlc.narg('empresa_id'), sqlc.narg('cola_id'), @atajo, @titulo, @texto) RETURNING *;

-- name: ActualizarRespuestaRapida :one
UPDATE mensajeria.respuestas_rapidas
SET cola_id = sqlc.narg('cola_id'), atajo = @atajo, titulo = @titulo, texto = @texto, version = version + 1, updated_at = now()
WHERE id = @id RETURNING *;

-- name: FijarRespuestaRapidaActiva :one
UPDATE mensajeria.respuestas_rapidas SET activa = @activa, version = version + 1, updated_at = now() WHERE id = @id RETURNING *;

-- Contactos de Iris ---------------------------------------------------------------------------

-- name: InsertarContactoIris :one
INSERT INTO mensajeria.contactos_iris (empresa_id, nombre, tipo, telefono, nota, creado_por)
VALUES (sqlc.narg('empresa_id'), @nombre, @tipo, @telefono, @nota, sqlc.narg('creado_por')) RETURNING *;

-- name: ContactoIrisParaActualizar :one
SELECT * FROM mensajeria.contactos_iris WHERE id = @id AND coalesce(empresa_id, 0) = @empresa::bigint FOR UPDATE;

-- name: ActualizarContactoIris :one
UPDATE mensajeria.contactos_iris SET nombre = @nombre, tipo = @tipo, nota = @nota, version = version + 1, updated_at = now()
WHERE id = @id RETURNING *;

-- name: UnirContactoIris :one
UPDATE mensajeria.contactos_iris SET cliente_id = @cliente_id, unido_en = @unido_en, version = version + 1, updated_at = now()
WHERE id = @id RETURNING *;

-- name: BuscarContactosIris :many
SELECT * FROM mensajeria.contactos_iris
WHERE coalesce(empresa_id, 0) = @empresa::bigint
  AND (@q::text = '' OR nombre ILIKE '%' || @q::text || '%' OR telefono LIKE '%' || @q::text || '%')
  AND (NOT @con_cursor::boolean OR id < @cursor_id::bigint)
ORDER BY id DESC
LIMIT @limite;

-- name: ContactosIrisPorTelefono :many
SELECT * FROM mensajeria.contactos_iris
WHERE coalesce(empresa_id, 0) = @empresa::bigint AND telefono = @telefono
ORDER BY id DESC LIMIT 5;

-- name: ContactosIrisPorIDs :many
SELECT * FROM mensajeria.contactos_iris WHERE id = ANY (@ids::bigint[]);

-- name: PasarConversacionesACliente :many
UPDATE mensajeria.conversaciones
SET cliente_id = @cliente_id, servicio_id = NULL, asociacion = 'manual', cliente_verificado = false, verificado_por = NULL,
    version = version + 1, updated_at = now()
WHERE contacto_iris_id = @contacto_iris_id
RETURNING id;

-- Conversaciones ------------------------------------------------------------------------------

-- name: ConversacionPorContacto :one
SELECT * FROM mensajeria.conversaciones
WHERE coalesce(empresa_id, 0) = @empresa::bigint AND canal_id = @canal_id AND numero = @numero
FOR UPDATE;

-- name: InsertarConversacionCliente :one
INSERT INTO mensajeria.conversaciones (empresa_id, canal_id, numero, perfil_nombre, cola_id, cliente_id, asociacion, cliente_verificado, ultima_actividad)
VALUES (sqlc.narg('empresa_id'), @canal_id, @numero, sqlc.narg('perfil_nombre'), sqlc.narg('cola_id'), sqlc.narg('cliente_id'),
    sqlc.narg('asociacion'), @cliente_verificado, @ultima_actividad)
ON CONFLICT DO NOTHING
RETURNING *;

-- name: ConversacionClientePorID :one
SELECT * FROM mensajeria.conversaciones WHERE id = @id;

-- name: ConversacionClienteParaActualizar :one
SELECT * FROM mensajeria.conversaciones WHERE id = @id FOR UPDATE;

-- name: ConversacionVisible :one
SELECT EXISTS (
    SELECT 1 FROM mensajeria.conversaciones c
    WHERE c.id = @id AND coalesce(c.empresa_id, 0) = @empresa::bigint
      AND mensajeria.conv_visible(@usuario::bigint, @supervisa::boolean, c.id, c.cola_id, c.asignado_a)
)::boolean;

-- name: GuardarConversacion :one
-- Escribe el estado de la conversación que calculó el caso de uso (la fila se leyó con FOR UPDATE).
UPDATE mensajeria.conversaciones
SET perfil_nombre = sqlc.narg('perfil_nombre'), cola_id = sqlc.narg('cola_id'), asignado_a = sqlc.narg('asignado_a'),
    asignado_en = sqlc.narg('asignado_en'), estado = @estado, cliente_id = sqlc.narg('cliente_id'), servicio_id = sqlc.narg('servicio_id'),
    contacto_iris_id = sqlc.narg('contacto_iris_id'), prospecto_id = sqlc.narg('prospecto_id'), asociacion = sqlc.narg('asociacion'),
    cliente_verificado = @cliente_verificado, verificado_por = sqlc.narg('verificado_por'),
    ultimo_entrante_en = sqlc.narg('ultimo_entrante_en'), espera_desde = sqlc.narg('espera_desde'), sla_vence_en = sqlc.narg('sla_vence_en'),
    sla_avisado = @sla_avisado, resuelta_en = sqlc.narg('resuelta_en'), reabierta_en = sqlc.narg('reabierta_en'),
    ultima_actividad = @ultima_actividad, version = version + @sube_version::integer, updated_at = now()
WHERE id = @id
RETURNING *;

-- name: AvanzarSeqConversacion :one
UPDATE mensajeria.conversaciones
SET ultimo_seq = ultimo_seq + 1, ultimo_mensaje_id = @mensaje_id, ultima_actividad = GREATEST(ultima_actividad, @ahora::timestamptz), updated_at = now()
WHERE id = @id
RETURNING ultimo_seq;

-- name: ListarConversacionesCliente :many
-- Bandeja: las que ve el usuario en la empresa con los filtros, de la más reciente a la más vieja.
SELECT c.id, c.ultima_actividad FROM mensajeria.conversaciones c
LEFT JOIN integracion.argos_clientes ac ON ac.id = c.cliente_id
LEFT JOIN mensajeria.contactos_iris ci ON ci.id = c.contacto_iris_id
WHERE coalesce(c.empresa_id, 0) = @empresa::bigint
  AND mensajeria.conv_visible(@usuario::bigint, @supervisa::boolean, c.id, c.cola_id, c.asignado_a)
  AND (@vista::text = 'todas' OR (@vista::text = 'mias' AND c.asignado_a = @usuario::bigint) OR (@vista::text = 'sin_asignar' AND c.asignado_a IS NULL))
  AND (CASE @estado::text WHEN 'activas' THEN c.estado <> 'resuelta' WHEN 'todas' THEN true ELSE c.estado = @estado::text END)
  AND (sqlc.narg('canal_id')::bigint IS NULL OR c.canal_id = sqlc.narg('canal_id')::bigint)
  AND (sqlc.narg('cola_id')::bigint IS NULL OR c.cola_id = sqlc.narg('cola_id')::bigint)
  AND (sqlc.narg('etiqueta_id')::bigint IS NULL OR EXISTS (
        SELECT 1 FROM mensajeria.conversacion_etiquetas ce WHERE ce.conversacion_id = c.id AND ce.etiqueta_id = sqlc.narg('etiqueta_id')::bigint))
  AND (CASE @sla::text
        WHEN 'vencido' THEN c.sla_vence_en IS NOT NULL AND c.sla_vence_en <= @ahora::timestamptz
        WHEN 'por_vencer' THEN c.sla_vence_en IS NOT NULL AND c.sla_vence_en > @ahora::timestamptz
            AND c.sla_vence_en - make_interval(mins => coalesce((SELECT co.sla_por_vencer_min FROM mensajeria.colas co WHERE co.id = c.cola_id),
                GREATEST(1, coalesce((SELECT co.sla_respuesta_min FROM mensajeria.colas co WHERE co.id = c.cola_id), 0) / 4))) <= @ahora::timestamptz
        ELSE true END)
  AND (@q::text = ''
       OR coalesce(ac.nombre, '') ILIKE '%' || @q::text || '%'
       OR coalesce(ci.nombre, '') ILIKE '%' || @q::text || '%'
       OR coalesce(c.perfil_nombre, '') ILIKE '%' || @q::text || '%'
       OR (@q_digitos::text <> '' AND c.numero LIKE '%' || @q_digitos::text || '%')
       OR EXISTS (SELECT 1 FROM mensajeria.conversacion_vinculos v WHERE v.conversacion_id = c.id AND lower(v.referencia) = lower(@q::text)))
  AND (NOT @con_cursor::boolean OR (c.ultima_actividad, c.id) < (@cursor_actividad::timestamptz, @cursor_id::bigint))
ORDER BY c.ultima_actividad DESC, c.id DESC
LIMIT @limite;

-- name: ResumenConversacionesCliente :many
SELECT c.*,
    ca.nombre AS canal_nombre, ca.numero AS canal_numero, ca.usos AS canal_usos,
    co.nombre AS cola_nombre, co.sla_respuesta_min, co.sla_por_vencer_min,
    u.nombre AS asignado_nombre,
    ac.nombre AS cliente_nombre, ci.nombre AS contacto_iris_nombre,
    um.direccion AS ultimo_direccion, um.tipo AS ultimo_tipo, um.texto AS ultimo_texto, um.creado_en AS ultimo_creado_en,
    um.evento AS ultimo_evento,
    (SELECT count(*) FROM mensajeria.conversacion_mensajes m
     WHERE m.conversacion_id = c.id AND m.direccion = 'entrante'
       AND m.seq > coalesce((SELECT l.hasta_seq FROM mensajeria.conversacion_lecturas l WHERE l.conversacion_id = c.id AND l.usuario_id = @usuario::bigint), 0)
    )::bigint AS no_leidos
FROM mensajeria.conversaciones c
JOIN mensajeria.canales ca ON ca.id = c.canal_id
LEFT JOIN mensajeria.colas co ON co.id = c.cola_id
LEFT JOIN nucleo.usuarios u ON u.id = c.asignado_a
LEFT JOIN integracion.argos_clientes ac ON ac.id = c.cliente_id
LEFT JOIN mensajeria.contactos_iris ci ON ci.id = c.contacto_iris_id
LEFT JOIN mensajeria.conversacion_mensajes um ON um.id = c.ultimo_mensaje_id
WHERE c.id = ANY (@ids::bigint[]);

-- name: ContadoresConversaciones :one
SELECT
    count(*) FILTER (WHERE c.asignado_a = @usuario::bigint)::bigint AS mias,
    count(*) FILTER (WHERE c.asignado_a IS NULL)::bigint AS sin_asignar,
    count(*)::bigint AS todas,
    count(*) FILTER (WHERE c.estado = 'abierta')::bigint AS abiertas,
    count(*) FILTER (WHERE c.estado = 'esperando_cliente')::bigint AS esperando,
    count(*) FILTER (WHERE c.sla_vence_en IS NOT NULL AND c.sla_vence_en <= @ahora::timestamptz)::bigint AS vencidas,
    count(*) FILTER (WHERE c.sla_vence_en IS NOT NULL AND c.sla_vence_en > @ahora::timestamptz
        AND c.sla_vence_en - make_interval(mins => coalesce(co.sla_por_vencer_min, GREATEST(1, coalesce(co.sla_respuesta_min, 0) / 4))) <= @ahora::timestamptz)::bigint AS por_vencer,
    coalesce(sum((SELECT count(*) FROM mensajeria.conversacion_mensajes m
        WHERE m.conversacion_id = c.id AND m.direccion = 'entrante'
          AND m.seq > coalesce((SELECT l.hasta_seq FROM mensajeria.conversacion_lecturas l WHERE l.conversacion_id = c.id AND l.usuario_id = @usuario::bigint), 0)))
        FILTER (WHERE c.asignado_a = @usuario::bigint), 0)::bigint AS no_leidos_mias
FROM mensajeria.conversaciones c
LEFT JOIN mensajeria.colas co ON co.id = c.cola_id
WHERE coalesce(c.empresa_id, 0) = @empresa::bigint AND c.estado <> 'resuelta'
  AND mensajeria.conv_visible(@usuario::bigint, @supervisa::boolean, c.id, c.cola_id, c.asignado_a);

-- name: ContadoresPorGrupo :many
-- Activas que ve el usuario por número, cola y etiqueta (para los filtros).
SELECT 'canal'::text AS grupo, c.canal_id AS id, count(*)::bigint AS total
FROM mensajeria.conversaciones c
WHERE coalesce(c.empresa_id, 0) = @empresa::bigint AND c.estado <> 'resuelta'
  AND mensajeria.conv_visible(@usuario::bigint, @supervisa::boolean, c.id, c.cola_id, c.asignado_a)
GROUP BY c.canal_id
UNION ALL
SELECT 'cola'::text, c.cola_id, count(*)::bigint
FROM mensajeria.conversaciones c
WHERE coalesce(c.empresa_id, 0) = @empresa::bigint AND c.estado <> 'resuelta' AND c.cola_id IS NOT NULL
  AND mensajeria.conv_visible(@usuario::bigint, @supervisa::boolean, c.id, c.cola_id, c.asignado_a)
GROUP BY c.cola_id
UNION ALL
SELECT 'etiqueta'::text, ce.etiqueta_id, count(*)::bigint
FROM mensajeria.conversaciones c JOIN mensajeria.conversacion_etiquetas ce ON ce.conversacion_id = c.id
WHERE coalesce(c.empresa_id, 0) = @empresa::bigint AND c.estado <> 'resuelta'
  AND mensajeria.conv_visible(@usuario::bigint, @supervisa::boolean, c.id, c.cola_id, c.asignado_a)
GROUP BY ce.etiqueta_id;

-- name: SLAVencidosSinAvisar :many
SELECT id FROM mensajeria.conversaciones
WHERE sla_vence_en IS NOT NULL AND NOT sla_avisado AND sla_vence_en <= @ahora::timestamptz AND estado = 'abierta'
ORDER BY sla_vence_en LIMIT 200;

-- name: ParticipantesDeConversacion :many
SELECT p.usuario_id, u.nombre, p.origen FROM mensajeria.conversacion_participantes p JOIN nucleo.usuarios u ON u.id = p.usuario_id
WHERE p.conversacion_id = @conversacion_id ORDER BY p.created_at;

-- name: InsertarParticipante :execrows
INSERT INTO mensajeria.conversacion_participantes (conversacion_id, usuario_id, origen) VALUES (@conversacion_id, @usuario_id, @origen)
ON CONFLICT DO NOTHING;

-- name: VinculosDeConversacion :many
SELECT * FROM mensajeria.conversacion_vinculos WHERE conversacion_id = @conversacion_id ORDER BY created_at;

-- name: InsertarVinculo :exec
INSERT INTO mensajeria.conversacion_vinculos (conversacion_id, tipo, entidad_id, referencia, creado_por)
VALUES (@conversacion_id, @tipo, @entidad_id, @referencia, sqlc.narg('creado_por'))
ON CONFLICT DO NOTHING;

-- Agentes -------------------------------------------------------------------------------------

-- name: AgentesConversaciones :many
-- Usuarios persona activos que operan en la empresa con conversaciones.ver (y si supervisan).
SELECT u.id, u.nombre,
    EXISTS (SELECT 1 FROM nucleo.usuarios_roles ur JOIN nucleo.roles r ON r.id = ur.rol_id AND r.activo
            JOIN nucleo.roles_permisos rp ON rp.rol_id = ur.rol_id
            WHERE ur.usuario_id = u.id AND rp.permiso = 'conversaciones.supervisar')::boolean AS supervisa
FROM nucleo.usuarios u
WHERE u.activo AND u.tipo = 'persona'
  AND EXISTS (SELECT 1 FROM nucleo.usuarios_roles ur JOIN nucleo.roles r ON r.id = ur.rol_id AND r.activo
              JOIN nucleo.roles_permisos rp ON rp.rol_id = ur.rol_id
              WHERE ur.usuario_id = u.id AND rp.permiso = 'conversaciones.ver')
  AND (@empresa::bigint = 0
       OR EXISTS (SELECT 1 FROM nucleo.usuarios_empresas ue WHERE ue.usuario_id = u.id AND ue.empresa_id = @empresa::bigint)
       OR EXISTS (SELECT 1 FROM nucleo.usuarios_roles ur JOIN nucleo.roles r ON r.id = ur.rol_id AND r.activo
                  JOIN nucleo.roles_permisos rp ON rp.rol_id = ur.rol_id
                  WHERE ur.usuario_id = u.id AND rp.permiso = 'empresas.todas' AND ur.zona_id IS NULL AND ur.cuadrilla_id IS NULL))
ORDER BY lower(u.nombre), u.id;

-- name: ColasDeUsuarios :many
SELECT m.usuario_id, c.id, c.nombre FROM mensajeria.cola_miembros m JOIN mensajeria.colas c ON c.id = m.cola_id
WHERE m.usuario_id = ANY (@usuarios::bigint[]) AND coalesce(c.empresa_id, 0) = @empresa::bigint
ORDER BY m.usuario_id, lower(c.nombre);

-- name: CanalesDeEmpresa :many
SELECT * FROM mensajeria.canales WHERE coalesce(empresa_id, 0) = @empresa::bigint ORDER BY NOT activo, lower(nombre), id;

-- Mensajes ------------------------------------------------------------------------------------

-- name: InsertarMensajeConversacion :one
INSERT INTO mensajeria.conversacion_mensajes (id, conversacion_id, empresa_id, seq, direccion, tipo, texto, adjunto_id, medio_id,
    lat, lon, plantilla, variables, autor_id, menciones, evento, respuesta_rapida_id, estado_entrega, proveedor_id, hash_contenido,
    idempotency_key, creado_en, automatico)
VALUES (@id, @conversacion_id, sqlc.narg('empresa_id'), @seq, @direccion, @tipo, sqlc.narg('texto'), sqlc.narg('adjunto_id'),
    sqlc.narg('medio_id'), sqlc.narg('lat'), sqlc.narg('lon'), sqlc.narg('plantilla'), sqlc.narg('variables'), sqlc.narg('autor_id'),
    @menciones::bigint[], sqlc.narg('evento'), sqlc.narg('respuesta_rapida_id'), sqlc.narg('estado_entrega'), sqlc.narg('proveedor_id'),
    sqlc.narg('hash_contenido'), sqlc.narg('idempotency_key'), @creado_en, sqlc.narg('automatico'))
RETURNING *;

-- name: MensajeConversacionPorID :one
SELECT * FROM mensajeria.conversacion_mensajes WHERE id = @id;

-- name: MensajeConversacionParaActualizar :one
SELECT * FROM mensajeria.conversacion_mensajes WHERE id = @id FOR UPDATE;

-- name: MensajeConversacionPorClave :one
SELECT * FROM mensajeria.conversacion_mensajes WHERE autor_id = @autor_id AND idempotency_key = @idempotency_key;

-- name: MensajeEntrantePorProveedor :one
SELECT * FROM mensajeria.conversacion_mensajes WHERE conversacion_id = @conversacion_id AND proveedor_id = @proveedor_id;

-- name: MensajeSalientePorProveedor :one
SELECT m.* FROM mensajeria.conversacion_mensajes m JOIN mensajeria.conversaciones c ON c.id = m.conversacion_id
WHERE c.canal_id = @canal_id AND m.proveedor_id = @proveedor_id AND m.direccion = 'saliente'
FOR UPDATE OF m;

-- name: ListarMensajesConversacion :many
SELECT * FROM mensajeria.conversacion_mensajes
WHERE conversacion_id = @conversacion_id
  AND (CASE WHEN @adelante::boolean THEN seq > @desde::bigint ELSE seq < @desde::bigint END)
ORDER BY CASE WHEN @adelante::boolean THEN seq END ASC, CASE WHEN NOT @adelante::boolean THEN seq END DESC
LIMIT @limite;

-- name: MensajesConversacionPorIDs :many
SELECT * FROM mensajeria.conversacion_mensajes WHERE id = ANY (@ids::uuid[]);

-- name: EstadosAdjuntos :many
SELECT id, estado, mime, tamano FROM nucleo.adjuntos WHERE id = ANY (@ids::uuid[]);

-- name: MarcarEnvioMensaje :one
UPDATE mensajeria.conversacion_mensajes
SET estado_entrega = @estado_entrega::text, proveedor_id = coalesce(sqlc.narg('proveedor_id'), proveedor_id),
    enviado_en = coalesce(sqlc.narg('enviado_en'), enviado_en), entregado_en = coalesce(sqlc.narg('entregado_en'), entregado_en),
    leido_en = coalesce(sqlc.narg('leido_en'), leido_en), motivo_falla = sqlc.narg('motivo_falla'), detalle_falla = sqlc.narg('detalle_falla'),
    intentos = intentos + @intento::integer, updated_at = now()
WHERE id = @id
RETURNING *;

-- name: FijarAdjuntoMensaje :one
UPDATE mensajeria.conversacion_mensajes SET adjunto_id = @adjunto_id, updated_at = now() WHERE id = @id RETURNING *;

-- name: MarcarEnvioMensajeEntrante :one
-- El medio de un mensaje del cliente no se pudo descargar: queda sin archivo y con el motivo.
UPDATE mensajeria.conversacion_mensajes
SET medio_id = NULL, motivo_falla = @motivo_falla, detalle_falla = @detalle_falla, intentos = intentos + 1, updated_at = now()
WHERE id = @id
RETURNING *;

-- name: ContarIntentoMedio :exec
UPDATE mensajeria.conversacion_mensajes SET intentos = intentos + 1, medio_id = sqlc.narg('medio_id'), updated_at = now() WHERE id = @id;

-- name: MediosPorDescargar :many
SELECT id FROM mensajeria.conversacion_mensajes
WHERE medio_id IS NOT NULL AND adjunto_id IS NULL AND created_at < @antes ORDER BY created_at LIMIT 100;

-- name: AvanzarLecturaConversacion :exec
INSERT INTO mensajeria.conversacion_lecturas (conversacion_id, usuario_id, hasta_seq, leido_en)
VALUES (@conversacion_id, @usuario_id, @hasta_seq, @leido_en)
ON CONFLICT (conversacion_id, usuario_id) DO UPDATE
SET hasta_seq = GREATEST(mensajeria.conversacion_lecturas.hasta_seq, EXCLUDED.hasta_seq),
    leido_en = CASE WHEN EXCLUDED.hasta_seq > mensajeria.conversacion_lecturas.hasta_seq THEN EXCLUDED.leido_en ELSE mensajeria.conversacion_lecturas.leido_en END;

-- name: AvisoDeProveedorCanal :one
-- El aviso al que el cliente respondió (context.id de Meta).
SELECT * FROM mensajeria.avisos WHERE canal_id = @canal_id AND proveedor_id = @proveedor_id;

-- name: UltimoAvisoA :one
-- El último aviso enviado desde el número a ese teléfono desde @desde (respuesta a un aviso).
SELECT * FROM mensajeria.avisos
WHERE canal_id = @canal_id AND destino = @destino AND enviado_en IS NOT NULL AND enviado_en >= @desde::timestamptz
ORDER BY enviado_en DESC LIMIT 1;

-- Instantánea de Argos (solo lectura) -------------------------------------------------------------

-- name: ClientesPorTelefonoInstantanea :many
-- Clientes de la instantánea con ese número (solo dígitos; 502 delante de 8 dígitos) en la empresa.
WITH t AS (SELECT @digitos::text AS d)
SELECT DISTINCT ac.id, ac.nombre, ac.codigo,
    (CASE WHEN length(regexp_replace(ac.telefono, '\D', '', 'g')) = 8 THEN '502' ELSE '' END || regexp_replace(ac.telefono, '\D', '', 'g')) = t.d AS por_telefono,
    (CASE WHEN length(regexp_replace(ac.movil, '\D', '', 'g')) = 8 THEN '502' ELSE '' END || regexp_replace(ac.movil, '\D', '', 'g')) = t.d AS por_movil,
    EXISTS (SELECT 1 FROM integracion.argos_servicios s WHERE s.cliente_id = ac.id
            AND (CASE WHEN length(regexp_replace(s.contacto_telefono, '\D', '', 'g')) = 8 THEN '502' ELSE '' END || regexp_replace(s.contacto_telefono, '\D', '', 'g')) = t.d) AS por_servicio
FROM integracion.argos_clientes ac, t
WHERE (@empresa::bigint = 0 OR ac.empresa_id = @empresa::bigint OR @empresa::bigint = ANY (ac.empresas))
  AND ((CASE WHEN length(regexp_replace(ac.telefono, '\D', '', 'g')) = 8 THEN '502' ELSE '' END || regexp_replace(ac.telefono, '\D', '', 'g')) = t.d
    OR (CASE WHEN length(regexp_replace(ac.movil, '\D', '', 'g')) = 8 THEN '502' ELSE '' END || regexp_replace(ac.movil, '\D', '', 'g')) = t.d
    OR EXISTS (SELECT 1 FROM integracion.argos_servicios s WHERE s.cliente_id = ac.id
            AND (CASE WHEN length(regexp_replace(s.contacto_telefono, '\D', '', 'g')) = 8 THEN '502' ELSE '' END || regexp_replace(s.contacto_telefono, '\D', '', 'g')) = t.d))
ORDER BY ac.id
LIMIT 10;

-- name: ClienteInstantanea :one
SELECT * FROM integracion.argos_clientes WHERE id = @id;

-- name: ServiciosInstantanea :many
SELECT * FROM integracion.argos_servicios WHERE cliente_id = @cliente_id ORDER BY id;

-- Boletas y promesas ---------------------------------------------------------------------------

-- name: InsertarBoleta :one
INSERT INTO mensajeria.boletas (id, empresa_id, conversacion_id, cliente_id, servicio_id, adjunto_id, metodo, cuenta_bancaria_id,
    banco_origen_id, referencia, monto, fecha_deposito, nota, registrada_por, proximo_intento)
VALUES (@id, sqlc.narg('empresa_id'), @conversacion_id, @cliente_id, sqlc.narg('servicio_id'), @adjunto_id, @metodo, @cuenta_bancaria_id,
    sqlc.narg('banco_origen_id'), @referencia, @monto, @fecha_deposito, sqlc.narg('nota'), sqlc.narg('registrada_por'), @proximo_intento)
RETURNING *;

-- name: BoletaParaEnviar :one
SELECT * FROM mensajeria.boletas WHERE id = @id FOR UPDATE;

-- name: ResultadoBoleta :one
UPDATE mensajeria.boletas
SET estado = @estado, comprobante_id = sqlc.narg('comprobante_id'), detalle = sqlc.narg('detalle'), intentos = intentos + 1,
    proximo_intento = @proximo_intento, updated_at = now()
WHERE id = @id
RETURNING *;

-- name: BoletasVencidas :many
SELECT id FROM mensajeria.boletas WHERE estado = 'por_enviar' AND proximo_intento <= @ahora ORDER BY proximo_intento LIMIT 50;

-- name: PromesaPorClave :one
SELECT * FROM mensajeria.promesas WHERE solicitada_por = @solicitada_por AND idempotency_key = @idempotency_key;

-- name: InsertarPromesa :one
INSERT INTO mensajeria.promesas (id, empresa_id, conversacion_id, cliente_id, servicio_id, vence_el, estado, promesa_argos_id, monto,
    regla, motivo, idempotency_key, hash_peticion, solicitada_por)
VALUES (@id, sqlc.narg('empresa_id'), @conversacion_id, @cliente_id, @servicio_id, @vence_el, @estado, sqlc.narg('promesa_argos_id'),
    sqlc.narg('monto'), sqlc.narg('regla'), sqlc.narg('motivo'), sqlc.narg('idempotency_key'), sqlc.narg('hash_peticion'), sqlc.narg('solicitada_por'))
ON CONFLICT DO NOTHING
RETURNING *;

-- Feed ------------------------------------------------------------------------------------------

-- name: InsertarEventoConv :one
INSERT INTO mensajeria.conv_eventos (empresa_id, conversacion_id, tipo, mensaje_id, usuario_id, ocurrido_en)
VALUES (sqlc.narg('empresa_id'), @conversacion_id, @tipo, sqlc.narg('mensaje_id'), sqlc.narg('usuario_id'), @ocurrido_en)
RETURNING id;

-- name: NotificarConv :exec
SELECT pg_notify('iris_conversaciones', @carga::text);

-- name: EventosConv :many
SELECT e.* FROM mensajeria.conv_eventos e
JOIN mensajeria.conversaciones c ON c.id = e.conversacion_id
WHERE e.id > @desde::bigint
  AND coalesce(e.empresa_id, 0) = @empresa::bigint
  AND (e.usuario_id IS NULL OR e.usuario_id = @usuario::bigint)
  AND mensajeria.conv_visible(@usuario::bigint, @supervisa::boolean, c.id, c.cola_id, c.asignado_a)
ORDER BY e.id
LIMIT @limite;

-- name: EstadoFeedConv :one
SELECT COALESCE((SELECT max(id) FROM mensajeria.conv_eventos), 0)::bigint AS cabeza,
    COALESCE((SELECT max(id) FROM mensajeria.conv_eventos WHERE coalesce(empresa_id, 0) = @empresa::bigint), 0)::bigint AS cabeza_empresa,
    (SELECT conv_purgados_hasta FROM mensajeria.chat_estado WHERE id = 1)::bigint AS purgados_hasta;

-- name: PurgarEventosConv :one
WITH borrados AS (
    DELETE FROM mensajeria.conv_eventos WHERE ocurrido_en < @antes RETURNING id
)
UPDATE mensajeria.chat_estado
SET conv_purgados_hasta = GREATEST(conv_purgados_hasta, COALESCE((SELECT max(id) FROM borrados), 0))
WHERE id = 1
RETURNING conv_purgados_hasta;

-- name: ConsentimientoNumeroVigente :one
SELECT * FROM mensajeria.consentimientos_numero
WHERE coalesce(empresa_id, 0) = @empresa::bigint AND telefono = @telefono
ORDER BY ocurrido_en DESC, (estado = 'baja') DESC, id DESC
LIMIT 1;

-- name: BajaDelClientePendiente :one
-- La baja más reciente que pidió el propio cliente y que ningún acepta posterior anuló (aunque
-- después haya otras bajas de agentes).
SELECT b.id FROM mensajeria.consentimientos_numero b
WHERE coalesce(b.empresa_id, 0) = @empresa::bigint AND b.telefono = @telefono AND b.estado = 'baja' AND b.origen = 'cliente'
  AND NOT EXISTS (
    SELECT 1 FROM mensajeria.consentimientos_numero a
    WHERE coalesce(a.empresa_id, 0) = @empresa::bigint AND a.telefono = @telefono AND a.estado = 'acepta'
      AND (a.ocurrido_en, a.id) > (b.ocurrido_en, b.id))
ORDER BY b.ocurrido_en DESC, b.id DESC
LIMIT 1;

-- name: InsertarConsentimientoNumero :one
INSERT INTO mensajeria.consentimientos_numero (empresa_id, telefono, estado, origen, ocurrido_en, registrado_por, motivo)
VALUES (sqlc.narg('empresa_id'), @telefono, @estado, @origen, @ocurrido_en, sqlc.narg('registrado_por'), sqlc.narg('motivo'))
RETURNING *;

-- name: CanalActivoPorPhoneNumber :one
SELECT * FROM mensajeria.canales WHERE phone_number_id = @phone_number_id AND activo;

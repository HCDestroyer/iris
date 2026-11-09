-- Asistente de IA que sugiere respuestas (contratos/conversaciones.md §16).

-- Ajustes --------------------------------------------------------------------------------------

-- name: AsistenteAjustes :one
SELECT * FROM mensajeria.asistente_ajustes WHERE coalesce(empresa_id, 0) = @empresa::bigint;

-- name: AsistenteAjustesParaActualizar :one
SELECT * FROM mensajeria.asistente_ajustes WHERE coalesce(empresa_id, 0) = @empresa::bigint FOR UPDATE;

-- name: InsertarAsistenteAjustes :one
INSERT INTO mensajeria.asistente_ajustes (empresa_id, activo, proveedor, url, clave_cifrada, clave_guardada_en, modelo, modo,
    fuente_manuales, fuente_argos, fuente_horus, fuente_historial, tope_tokens_mes, precio_entrada_millon, precio_salida_millon)
VALUES (sqlc.narg('empresa_id'), @activo, @proveedor, @url, NULL, NULL, @modelo, @modo,
    @fuente_manuales, @fuente_argos, @fuente_horus, @fuente_historial, @tope_tokens_mes,
    sqlc.narg('precio_entrada_millon')::numeric, sqlc.narg('precio_salida_millon')::numeric)
ON CONFLICT DO NOTHING
RETURNING *;

-- name: ActualizarAsistenteAjustes :one
UPDATE mensajeria.asistente_ajustes
SET activo = @activo, proveedor = @proveedor, url = @url, modelo = @modelo, modo = @modo,
    fuente_manuales = @fuente_manuales, fuente_argos = @fuente_argos, fuente_horus = @fuente_horus, fuente_historial = @fuente_historial,
    tope_tokens_mes = @tope_tokens_mes, precio_entrada_millon = sqlc.narg('precio_entrada_millon')::numeric,
    precio_salida_millon = sqlc.narg('precio_salida_millon')::numeric, version = version + 1, updated_at = now()
WHERE id = @id
RETURNING *;

-- name: FijarClaveAsistente :one
UPDATE mensajeria.asistente_ajustes SET clave_cifrada = sqlc.narg('clave_cifrada'), clave_guardada_en = sqlc.narg('clave_guardada_en'), updated_at = now()
WHERE id = @id
RETURNING *;

-- Uso -------------------------------------------------------------------------------------------

-- name: UsoAsistente :one
SELECT * FROM mensajeria.asistente_uso WHERE coalesce(empresa_id, 0) = @empresa::bigint AND mes = @mes;

-- name: SumarUsoAsistente :one
-- Suma el uso de una generación (en la transacción que guarda el resultado).
INSERT INTO mensajeria.asistente_uso AS u (empresa_id, mes, tokens_entrada, tokens_salida, sugerencias, resumenes)
VALUES (sqlc.narg('empresa_id'), @mes, @tokens_entrada, @tokens_salida, @sugerencias, @resumenes)
ON CONFLICT (coalesce(empresa_id, 0), mes) DO UPDATE
SET tokens_entrada = u.tokens_entrada + EXCLUDED.tokens_entrada, tokens_salida = u.tokens_salida + EXCLUDED.tokens_salida,
    sugerencias = u.sugerencias + EXCLUDED.sugerencias, resumenes = u.resumenes + EXCLUDED.resumenes, updated_at = now()
RETURNING *;

-- name: MarcarAvisosTope :exec
UPDATE mensajeria.asistente_uso SET avisado_80 = @avisado_80, avisado_100 = @avisado_100, updated_at = now()
WHERE coalesce(empresa_id, 0) = @empresa::bigint AND mes = @mes;

-- name: MetricasSugerencias :one
SELECT count(*)::bigint AS total,
    count(*) FILTER (WHERE usada_en IS NOT NULL)::bigint AS usadas,
    count(*) FILTER (WHERE calificacion = 'util')::bigint AS utiles,
    count(*) FILTER (WHERE calificacion = 'no_util')::bigint AS no_utiles
FROM mensajeria.sugerencias
WHERE coalesce(empresa_id, 0) = @empresa::bigint AND created_at >= @desde AND created_at < @hasta;

-- Sugerencias -----------------------------------------------------------------------------------

-- name: InsertarSugerencia :one
INSERT INTO mensajeria.sugerencias (id, empresa_id, conversacion_id, para_mensaje_id, estado, origen, pedida_por, created_at, updated_at)
VALUES (@id, sqlc.narg('empresa_id'), @conversacion_id, sqlc.narg('para_mensaje_id'), 'generando', @origen, sqlc.narg('pedida_por'), @creada_en, @creada_en)
RETURNING *;

-- name: SugerenciaPorID :one
SELECT * FROM mensajeria.sugerencias WHERE id = @id;

-- name: SugerenciaParaActualizar :one
SELECT * FROM mensajeria.sugerencias WHERE id = @id FOR UPDATE;

-- name: SugerenciaVigente :one
-- Solo la de la empresa actual de la conversación (un préstamo pudo pasarla a otra, §15.3).
SELECT s.* FROM mensajeria.sugerencias s
JOIN mensajeria.conversaciones c ON c.id = s.conversacion_id
WHERE s.conversacion_id = @conversacion_id AND s.estado IN ('generando', 'lista', 'error')
  AND coalesce(s.empresa_id, 0) = coalesce(c.empresa_id, 0);

-- name: SugerenciasPorIDs :many
SELECT s.* FROM mensajeria.sugerencias s
JOIN mensajeria.conversaciones c ON c.id = s.conversacion_id
WHERE s.id = ANY (@ids::uuid[]) AND coalesce(s.empresa_id, 0) = coalesce(c.empresa_id, 0);

-- name: CerrarSugerenciasVigentes :many
-- Deja descartada o vencida la vigente de la conversación (antes de pedir otra o al llegar un
-- mensaje del cliente).
UPDATE mensajeria.sugerencias SET estado = @estado, updated_at = now()
WHERE conversacion_id = @conversacion_id AND estado IN ('generando', 'lista', 'error')
RETURNING *;

-- name: FijarEstadoSugerencia :one
UPDATE mensajeria.sugerencias SET estado = @estado, updated_at = now() WHERE id = @id RETURNING *;

-- name: GuardarResultadoSugerencia :one
UPDATE mensajeria.sugerencias
SET estado = @estado, texto = sqlc.narg('texto'), fuentes = @fuentes, accion = sqlc.narg('accion'),
    error_codigo = sqlc.narg('error_codigo'), error_detalle = sqlc.narg('error_detalle'),
    proveedor = sqlc.narg('proveedor'), modelo = sqlc.narg('modelo'),
    tokens_entrada = @tokens_entrada, tokens_salida = @tokens_salida, generada_en = @generada_en, updated_at = now()
WHERE id = @id
RETURNING *;

-- name: CalificarSugerencia :one
UPDATE mensajeria.sugerencias SET calificacion = @calificacion, updated_at = now() WHERE id = @id RETURNING *;

-- name: UsarSugerencia :one
UPDATE mensajeria.sugerencias
SET estado = 'usada', usada_por = coalesce(usada_por, @usada_por), usada_en = coalesce(usada_en, @usada_en),
    enviada_editada = sqlc.narg('enviada_editada'), mensaje_enviado_id = sqlc.narg('mensaje_enviado_id'), updated_at = now()
WHERE id = @id
RETURNING *;

-- Contexto para el modelo (solo lectura) ----------------------------------------------------------

-- name: UltimoEntranteConv :one
-- El último mensaje del cliente (para quién es la sugerencia).
SELECT id, texto, creado_en FROM mensajeria.conversacion_mensajes
WHERE conversacion_id = @conversacion_id AND direccion = 'entrante'
ORDER BY seq DESC LIMIT 1;

-- name: HistorialAsistente :many
-- Los últimos mensajes del cliente y de agentes: nunca notas internas ni mensajes de sistema.
SELECT id, direccion, tipo, texto, autor_id, automatico, creado_en FROM mensajeria.conversacion_mensajes
WHERE conversacion_id = @conversacion_id AND direccion IN ('entrante', 'saliente')
ORDER BY seq DESC LIMIT @limite;

-- name: ServiciosClienteAsistente :many
-- La instantánea de Argos del cliente asociado, solo los servicios de la empresa de la conversación.
SELECT c.nombre AS cliente_nombre, s.id, s.codigo, s.plan_nombre, s.estado, s.zona_nombre, s.router_id
FROM integracion.argos_clientes c
JOIN integracion.argos_servicios s ON s.cliente_id = c.id
WHERE c.id = @cliente_id AND s.empresa_id = @empresa::bigint
ORDER BY (s.id = sqlc.narg('servicio_id')::bigint) DESC NULLS LAST, s.id
LIMIT 5;

-- name: FallasHorusCliente :many
-- Fallas abiertas (router caído según Horus) en los servicios del cliente de la empresa.
SELECT DISTINCT h.clave, s.zona_nombre, s.router_nombre
FROM integracion.argos_servicios s
JOIN soporte.horus_estados h ON h.clave IN ('router:' || s.empresa_id || ':' || s.router_id, 'router:0:' || s.router_id)
WHERE s.cliente_id = @cliente_id AND s.empresa_id = @empresa::bigint AND s.router_id IS NOT NULL AND h.estado = 'caido'
ORDER BY h.clave
LIMIT 3;

-- name: EtiquetasActivasEmpresa :many
SELECT id, nombre FROM mensajeria.etiquetas WHERE coalesce(empresa_id, 0) = @empresa::bigint AND activa ORDER BY nombre LIMIT 100;

-- name: ManualesAsistente :many
-- Los 3 mejores manuales activos de la empresa para el texto del cliente (búsqueda de §11, con OR
-- entre palabras para que una frase larga encuentre algo).
SELECT m.id, m.titulo, m.resumen, m.cuerpo, m.visibilidad,
    ts_rank(m.busqueda, to_tsquery('spanish', @consulta::text))::real AS rango
FROM mensajeria.manuales m
WHERE coalesce(m.empresa_id, 0) = @empresa::bigint AND m.activo AND m.busqueda @@ to_tsquery('spanish', @consulta::text)
ORDER BY rango DESC, m.id DESC
LIMIT 3;

-- Reservas de tokens ------------------------------------------------------------------------------

-- name: AsegurarUsoAsistente :exec
INSERT INTO mensajeria.asistente_uso (empresa_id, mes) VALUES (sqlc.narg('empresa_id'), @mes)
ON CONFLICT (coalesce(empresa_id, 0), mes) DO NOTHING;

-- name: ReservarTokens :one
-- Atómica: solo si lo usado más lo reservado más esta reserva cabe en el tope.
UPDATE mensajeria.asistente_uso
SET tokens_reservados = tokens_reservados + @tokens::bigint, updated_at = now()
WHERE coalesce(empresa_id, 0) = @empresa::bigint AND mes = @mes
  AND tokens_entrada + tokens_salida + tokens_reservados + @tokens::bigint <= @tope::bigint
RETURNING tokens_reservados;

-- name: InsertarReserva :exec
INSERT INTO mensajeria.asistente_reservas (id, empresa_id, mes, tokens, creada_en)
VALUES (@id, sqlc.narg('empresa_id'), @mes, @tokens, @creada_en);

-- name: LiberarReserva :one
DELETE FROM mensajeria.asistente_reservas WHERE id = @id RETURNING *;

-- name: DescontarReservados :exec
UPDATE mensajeria.asistente_uso SET tokens_reservados = GREATEST(0, tokens_reservados - @tokens::bigint), updated_at = now()
WHERE coalesce(empresa_id, 0) = @empresa::bigint AND mes = @mes;

-- name: ReservasHuerfanas :many
SELECT id FROM mensajeria.asistente_reservas WHERE creada_en < @antes ORDER BY creada_en LIMIT 200;

-- Límites --------------------------------------------------------------------------------------------

-- name: InsertarPedidoAsistente :exec
INSERT INTO mensajeria.asistente_pedidos (usuario_id, empresa_id, conversacion_id, tipo, creado_en)
VALUES (@usuario_id, sqlc.narg('empresa_id'), @conversacion_id, @tipo, @creado_en);

-- name: PedidosRecientesUsuario :one
SELECT count(*)::bigint AS total, coalesce(min(creado_en), @desde)::timestamptz AS primero
FROM mensajeria.asistente_pedidos WHERE usuario_id = @usuario_id AND creado_en > @desde;

-- name: SugerenciasRecientesConv :one
SELECT count(*)::bigint AS total, coalesce(min(created_at), @desde)::timestamptz AS primera,
    count(*) FILTER (WHERE origen = 'automatico')::bigint AS automaticas
FROM mensajeria.sugerencias WHERE conversacion_id = @conversacion_id AND created_at > @desde;

-- name: PurgarPedidosAsistente :exec
DELETE FROM mensajeria.asistente_pedidos WHERE creado_en < @antes;

-- name: SugerenciasAtascadas :many
SELECT id FROM mensajeria.sugerencias WHERE estado = 'generando' AND created_at < @antes ORDER BY created_at LIMIT 200;

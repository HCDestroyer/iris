-- Asistente de IA, fase 3b (contratos/conversaciones.md §17.8): el asistente atiende solo. Turnos
-- (idempotencia por conversación y mensaje), acciones ejecutadas y el estado en la conversación.

-- name: TurnoPorMensaje :one
SELECT * FROM mensajeria.asistente_turnos WHERE conversacion_id = @conversacion_id AND mensaje_id = @mensaje_id FOR UPDATE;

-- name: TurnoPorID :one
SELECT * FROM mensajeria.asistente_turnos WHERE id = @id;

-- name: TurnoParaActualizar :one
SELECT * FROM mensajeria.asistente_turnos WHERE id = @id FOR UPDATE;

-- name: InsertarTurno :one
INSERT INTO mensajeria.asistente_turnos (id, empresa_id, conversacion_id, mensaje_id, numero, estado, tomado_en, creado_en)
VALUES (@id, sqlc.narg('empresa_id'), @conversacion_id, @mensaje_id, @numero, 'en_curso', @ahora, @ahora)
ON CONFLICT (conversacion_id, mensaje_id) DO NOTHING
RETURNING *;

-- name: TomarTurno :exec
UPDATE mensajeria.asistente_turnos SET tomado_en = @ahora WHERE id = @id;

-- name: TerminarTurno :one
UPDATE mensajeria.asistente_turnos
SET estado = @estado, motivo = sqlc.narg('motivo'), herramientas = @herramientas, tokens_entrada = @tokens_entrada, tokens_salida = @tokens_salida,
    terminado_en = @ahora
WHERE id = @id AND estado = 'en_curso'
RETURNING *;

-- name: TurnosRecientesConv :one
SELECT count(*)::bigint FROM mensajeria.asistente_turnos WHERE conversacion_id = @conversacion_id AND creado_en >= @desde;

-- name: TurnosRecientesEmpresa :one
SELECT count(*)::bigint FROM mensajeria.asistente_turnos WHERE coalesce(empresa_id, 0) = @empresa::bigint AND creado_en >= @desde;

-- name: TurnosAtascados :many
-- Turnos que quedaron en curso sin un job que los corra (el job murió): se vuelven a encolar.
SELECT DISTINCT conversacion_id FROM mensajeria.asistente_turnos WHERE estado = 'en_curso' AND tomado_en < @antes ORDER BY conversacion_id LIMIT 50;

-- name: PurgarTurnosAsistente :exec
DELETE FROM mensajeria.asistente_turnos WHERE creado_en < @antes AND estado <> 'en_curso';

-- name: InsertarAccionAsistente :one
INSERT INTO mensajeria.asistente_acciones (turno_id, empresa_id, conversacion_id, paso, accion, resultado, entidad, entidad_id, creada_en)
VALUES (@turno_id, sqlc.narg('empresa_id'), @conversacion_id, @paso, @accion, @resultado, sqlc.narg('entidad'), sqlc.narg('entidad_id'), @creada_en)
ON CONFLICT (turno_id, paso) DO NOTHING
RETURNING id;

-- name: ContarAccionesTurno :one
SELECT count(*)::bigint FROM mensajeria.asistente_acciones WHERE turno_id = @turno_id;

-- name: AccionesAsistenteConv :many
-- Lo que hizo el asistente en la conversación desde que empezó a atenderla (para la nota de «pasar
-- a una persona»): la empresa va en el filtro, nunca cruza un préstamo.
SELECT a.accion, a.resultado, a.entidad, a.entidad_id, a.creada_en
FROM mensajeria.asistente_acciones a
WHERE a.conversacion_id = @conversacion_id AND coalesce(a.empresa_id, 0) = @empresa::bigint AND a.creada_en >= @desde AND a.resultado = 'ok'
ORDER BY a.id
LIMIT 30;

-- name: FijarAsistenteConv :one
UPDATE mensajeria.conversaciones
SET asistente_estado = sqlc.narg('estado'), asistente_turnos = @turnos, asistente_presento_en = sqlc.narg('presento_en'), updated_at = now()
WHERE id = @id
RETURNING *;

-- name: DetenerAsistente :exec
-- Una persona escribió: el asistente queda detenido hasta que la conversación se resuelva.
UPDATE mensajeria.conversaciones SET asistente_estado = 'detenido', updated_at = now()
WHERE id = @id AND asistente_estado IN ('atendiendo', 'espera_persona');

-- name: PersonaEscribioDesde :one
-- Todo saliente que no sea automático cuenta como «una persona escribió»: un agente desde Iris o
-- desde el teléfono del negocio (eco de coexistencia, sin autor).
SELECT EXISTS (
    SELECT 1 FROM mensajeria.conversacion_mensajes m
    WHERE m.conversacion_id = @conversacion_id AND m.direccion = 'saliente' AND m.automatico IS NULL AND m.creado_en >= @desde
)::boolean;

-- name: UltimoAdjuntoEntrante :one
-- La última imagen o documento que mandó el cliente en este periodo de la conversación y que
-- todavía no es de ninguna boleta (cada adjunto vale para una sola).
SELECT m.id, m.adjunto_id, m.tipo, m.creado_en FROM mensajeria.conversacion_mensajes m
WHERE m.conversacion_id = @conversacion_id AND m.direccion = 'entrante' AND m.adjunto_id IS NOT NULL AND m.tipo IN ('imagen', 'documento')
  AND m.creado_en >= @desde
  AND NOT EXISTS (SELECT 1 FROM mensajeria.boletas b WHERE b.adjunto_id = m.adjunto_id)
  AND m.seq > coalesce((SELECT max(m2.seq) FROM mensajeria.conversacion_mensajes m2 JOIN mensajeria.boletas b ON b.adjunto_id = m2.adjunto_id
                        WHERE m2.conversacion_id = m.conversacion_id), 0)
ORDER BY m.seq DESC LIMIT 1;

-- name: BoletasRecientesConv :one
SELECT count(*)::bigint FROM mensajeria.boletas WHERE conversacion_id = @conversacion_id AND created_at >= @desde;

-- name: UltimaBoletaConv :one
-- La última boleta de la conversación para ese cliente (la reconexión temporal se liga a su comprobante).
SELECT * FROM mensajeria.boletas
WHERE conversacion_id = @conversacion_id AND cliente_id = @cliente_id AND coalesce(empresa_id, 0) = @empresa::bigint
ORDER BY created_at DESC LIMIT 1;

-- name: BoletaPorReferenciaConv :one
SELECT * FROM mensajeria.boletas
WHERE conversacion_id = @conversacion_id AND referencia = @referencia AND coalesce(empresa_id, 0) = @empresa::bigint
ORDER BY created_at DESC LIMIT 1;

-- name: PromesaPorIDConv :one
SELECT * FROM mensajeria.promesas WHERE id = @id AND conversacion_id = @conversacion_id;

-- name: ServicioDeClienteEmpresa :one
-- El servicio es del cliente y de la empresa (argumentos de herramientas: nunca el de otro).
SELECT EXISTS (
    SELECT 1 FROM integracion.argos_servicios s
    WHERE s.id = @servicio_id AND s.cliente_id = @cliente_id AND s.empresa_id = @empresa::bigint
)::boolean;

-- Revisión de seguridad de la #163 ---------------------------------------------------------------------

-- name: FijarIdentidadVerificada :exec
UPDATE mensajeria.conversaciones SET identidad_verificada_en = @ahora, updated_at = now() WHERE id = @id;

-- name: InsertarVerificacion :exec
INSERT INTO mensajeria.asistente_verificaciones (empresa_id, conversacion_id, cliente_id, resultado, creada_en)
VALUES (sqlc.narg('empresa_id'), @conversacion_id, @cliente_id, @resultado, @creada_en);

-- name: VerificacionesRecientes :one
-- Intentos del desafío que cuentan para el máximo (los que Argos evaluó).
SELECT count(*)::bigint FROM mensajeria.asistente_verificaciones
WHERE conversacion_id = @conversacion_id AND creada_en >= @desde AND resultado IN ('coincide', 'no_coincide');

-- name: UltimosEntrantesTexto :many
SELECT id, texto, proveedor_id FROM mensajeria.conversacion_mensajes
WHERE conversacion_id = @conversacion_id AND direccion = 'entrante' AND texto IS NOT NULL AND creado_en >= @desde
ORDER BY seq DESC LIMIT 3;

-- name: EnmascararTextoMensaje :exec
-- El dato de identidad que escribió el cliente no se guarda (contrato con Argos §7.5): queda ***.
UPDATE mensajeria.conversacion_mensajes SET texto = @texto WHERE id = @id AND direccion = 'entrante';

-- name: EnmascararEntranteMeta :exec
-- El payload crudo de Meta del mensaje que traía un documento de identidad: el dato queda ***
-- (el payload ya se procesó; hash_cuerpo, que deduplica, no cambia).
UPDATE mensajeria.entrantes e
SET payload = replace(e.payload::text, @valor::text, '***')::jsonb
WHERE e.canal_id = @canal_id AND e.created_at >= now() - interval '3 days'
  AND strpos(e.payload::text, @proveedor_id::text) > 0 AND strpos(e.payload::text, @valor::text) > 0
  AND pg_input_is_valid(replace(e.payload::text, @valor::text, '***'), 'jsonb');

-- name: LiberarArriendoTurno :exec
-- El cierre del turno falló: el arriendo se suelta para que el reintento lo tome enseguida.
UPDATE mensajeria.asistente_turnos SET tomado_en = 'epoch' WHERE id = @id AND estado = 'en_curso';

-- name: BotsDiferidosViejos :many
-- Conversaciones cuyo paso del bot y de fuera de horario sigue esperando a la clasificación.
SELECT c.id FROM mensajeria.conversaciones c
JOIN mensajeria.conversacion_mensajes m ON m.id = c.bot_diferido_mensaje_id
WHERE c.bot_diferido_mensaje_id IS NOT NULL AND m.creado_en < @antes
ORDER BY m.creado_en LIMIT 200;

-- name: ResolverReconexion :one
UPDATE mensajeria.reconexiones
SET estado = @estado, promesa_argos_id = sqlc.narg('promesa_argos_id'), hasta = sqlc.narg('hasta'), detalle = sqlc.narg('detalle'), resuelta_en = now()
WHERE id = @id AND estado = 'pendiente'
RETURNING *;

-- name: ContarIntentoReconexion :exec
UPDATE mensajeria.reconexiones SET intentos = intentos + 1 WHERE id = @id;

-- name: ReconexionesPendientes :many
SELECT * FROM mensajeria.reconexiones WHERE estado = 'pendiente' AND created_at < @antes ORDER BY created_at LIMIT 100;

-- name: ReconexionParaActualizar :one
SELECT * FROM mensajeria.reconexiones WHERE id = @id FOR UPDATE;

-- name: MarcarMensajeConSaldo :exec
UPDATE mensajeria.conversacion_mensajes SET con_saldo = true WHERE id = @id;

-- name: FijarBotDiferido :exec
UPDATE mensajeria.conversaciones SET bot_diferido_mensaje_id = sqlc.narg('mensaje_id') WHERE id = @id;

-- name: InsertarReconexion :one
INSERT INTO mensajeria.reconexiones (id, empresa_id, conversacion_id, boleta_id, comprobante_id, cliente_id, servicio_id, motivo, estado,
    promesa_argos_id, hasta, detalle, solicitada_por, por_asistente)
VALUES (@id, sqlc.narg('empresa_id'), @conversacion_id, @boleta_id, @comprobante_id, @cliente_id, sqlc.narg('servicio_id'), @motivo, @estado,
    sqlc.narg('promesa_argos_id'), sqlc.narg('hasta'), sqlc.narg('detalle'), sqlc.narg('solicitada_por'), @por_asistente)
ON CONFLICT (comprobante_id) DO NOTHING
RETURNING *;

-- name: ReconexionPorComprobante :one
SELECT * FROM mensajeria.reconexiones WHERE comprobante_id = @comprobante_id;

-- name: SumarUsoAutonomo :exec
UPDATE mensajeria.asistente_uso SET tokens_autonomo = tokens_autonomo + @tokens::bigint, updated_at = now()
WHERE coalesce(empresa_id, 0) = @empresa::bigint AND mes = @mes;

-- name: TurnosDelPeriodo :one
-- Turnos del asistente en este periodo de la conversación: también los descartados cuentan para maxTurnos.
SELECT count(*)::bigint FROM mensajeria.asistente_turnos
WHERE conversacion_id = @conversacion_id AND creado_en >= @desde AND estado IN ('respondio', 'descartado');

-- name: DescartarTurnosEnCurso :exec
UPDATE mensajeria.asistente_turnos SET estado = 'descartado', motivo = 'persona_presente', terminado_en = @ahora
WHERE conversacion_id = @conversacion_id AND estado = 'en_curso';

-- name: TicketsAsistenteRecientes :one
SELECT count(*)::bigint FROM mensajeria.asistente_acciones
WHERE conversacion_id = @conversacion_id AND accion = 'crear_ticket' AND resultado = 'ok' AND creada_en >= @desde;

-- name: FijarPresupuestoAutonomo :exec
UPDATE mensajeria.asistente_ajustes SET presupuesto_autonomo_pct = @pct, updated_at = now() WHERE id = @id;

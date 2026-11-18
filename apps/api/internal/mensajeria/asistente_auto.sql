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

-- name: FijarMotivoPaso :exec
-- Por qué el asistente pasó la conversación a una persona (lista cerrada). El trigger lo borra cuando
-- el estado deja de ser espera_persona.
UPDATE mensajeria.conversaciones SET asistente_motivo_paso = @motivo, updated_at = now() WHERE id = @id AND asistente_estado = 'espera_persona';

-- name: DetenerAsistente :exec
-- Una persona escribió: el asistente queda detenido hasta que la conversación se resuelva.
UPDATE mensajeria.conversaciones SET asistente_estado = 'detenido', updated_at = now()
WHERE id = @id AND asistente_estado IN ('atendiendo', 'espera_persona');

-- name: PausarAsistenteConv :exec
-- §17.11: la pausa queda en la conversación; si el asistente la estaba atendiendo (o esperaba a una
-- persona), queda detenido: el turno en curso ya no envía nada.
UPDATE mensajeria.conversaciones
SET asistente_pausado_en = @ahora, asistente_pausado_por = @usuario, asistente_pausa_motivo = @motivo,
    asistente_estado = CASE WHEN asistente_estado IN ('atendiendo', 'espera_persona') THEN 'detenido' ELSE asistente_estado END,
    updated_at = now()
WHERE id = @id;

-- name: ReanudarAsistenteConv :exec
-- §17.11: quita la pausa y le devuelve la conversación al asistente: deja de estar detenido (conserva
-- sus turnos y su presentación) y lo que una persona escribió antes ya no lo frena.
UPDATE mensajeria.conversaciones
SET asistente_pausado_en = NULL, asistente_pausado_por = NULL, asistente_pausa_motivo = NULL, asistente_reanudado_en = @ahora,
    asistente_estado = CASE WHEN asistente_estado IN ('detenido', 'espera_persona') THEN NULL ELSE asistente_estado END,
    updated_at = now()
WHERE id = @id;

-- name: ReabrirTurnoDescartado :execrows
-- Al reanudar: el turno del último mensaje del cliente que se descartó porque había una persona
-- vuelve a estar pendiente (con el arriendo vencido), para que el asistente conteste ese mensaje.
-- Como mucho una vez por turno (reaperturas), solo si es de la empresa que hoy tiene la conversación
-- y de este periodo: un turno de otra empresa (número prestado) o de antes de resolverse no revive.
UPDATE mensajeria.asistente_turnos SET estado = 'en_curso', motivo = NULL, terminado_en = NULL, tomado_en = 'epoch', reaperturas = reaperturas + 1
WHERE conversacion_id = @conversacion_id AND mensaje_id = @mensaje_id AND estado = 'descartado' AND motivo IN ('pausado', 'persona_presente')
  AND reaperturas = 0 AND coalesce(empresa_id, 0) = @empresa::bigint AND creado_en >= @desde;

-- name: InsertarTurnoPendiente :execrows
-- Al reanudar: el mensaje del cliente que todavía no tuvo turno queda con uno pendiente (arriendo
-- suelto) que ya cuenta como reabierto: pasa por los límites y no se reabre otra vez.
INSERT INTO mensajeria.asistente_turnos (id, empresa_id, conversacion_id, mensaje_id, numero, estado, tomado_en, creado_en, reaperturas)
VALUES (@id, sqlc.narg('empresa_id'), @conversacion_id, @mensaje_id, @numero, 'en_curso', 'epoch', @ahora, 1)
ON CONFLICT (conversacion_id, mensaje_id) DO NOTHING;

-- name: UltimoMensajeAtencion :one
-- Lo último que se dijeron el cliente y quien lo atiende: un mensaje del cliente, de una persona o del
-- asistente (el saludo del bot y la respuesta de fuera de horario no son una respuesta).
SELECT id, direccion, creado_en FROM mensajeria.conversacion_mensajes
WHERE conversacion_id = @conversacion_id
  AND (direccion = 'entrante' OR (direccion = 'saliente' AND (automatico IS NULL OR automatico = 'asistente')))
ORDER BY seq DESC LIMIT 1;

-- name: PersonaRespondioDespues :one
-- ¿Una persona ya le contestó al cliente después de ese mensaje suyo?
SELECT EXISTS (
    SELECT 1 FROM mensajeria.conversacion_mensajes m
    WHERE m.conversacion_id = @conversacion_id AND m.direccion = 'saliente' AND m.automatico IS NULL
      AND m.seq > (SELECT e.seq FROM mensajeria.conversacion_mensajes e WHERE e.id = @mensaje_id)
)::boolean;

-- name: BotRespondioDesde :one
-- ¿El bot le mandó algo al cliente en esta espera? (saludo o menú)
SELECT EXISTS (
    SELECT 1 FROM mensajeria.conversacion_mensajes m
    WHERE m.conversacion_id = @conversacion_id AND m.direccion = 'saliente' AND m.automatico = 'bot' AND m.creado_en >= @desde
)::boolean;

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
UPDATE mensajeria.conversaciones SET identidad_verificada_en = @ahora, identidad_vence_en = @vence, updated_at = now() WHERE id = @id;

-- name: RechazosVerificacionConv :one
-- Veces que verificar_identidad se rechazó en la conversación (dato inválido o que no coincide).
SELECT count(*)::bigint FROM mensajeria.asistente_acciones
WHERE conversacion_id = @conversacion_id AND accion = 'verificar_identidad' AND resultado = 'rechazada' AND creada_en >= @desde;

-- name: FijarVerificacionFuerteMin :exec
UPDATE mensajeria.asistente_ajustes SET verificacion_fuerte_min = @minutos, updated_at = now() WHERE id = @id;

-- name: ReservarTokensAutonomo :one
-- Atómica, como ReservarTokens, y además contra el presupuesto del modo autónomo: lo gastado más lo
-- reservado por turnos autónomos más esta reserva cabe en el porcentaje del tope.
UPDATE mensajeria.asistente_uso
SET tokens_reservados = tokens_reservados + @tokens::bigint, tokens_autonomo_reservados = tokens_autonomo_reservados + @tokens::bigint, updated_at = now()
WHERE coalesce(empresa_id, 0) = @empresa::bigint AND mes = @mes
  AND tokens_entrada + tokens_salida + tokens_reservados + @tokens::bigint <= @tope::bigint
  AND tokens_autonomo + tokens_autonomo_reservados + @tokens::bigint <= @presupuesto::bigint
RETURNING tokens_reservados;

-- name: EntrantesDeMensaje :many
-- Los payloads crudos de Meta que traen ese mensaje (por su wamid).
SELECT e.id, e.payload FROM mensajeria.entrantes e
WHERE e.canal_id = @canal_id AND e.created_at >= now() - interval '3 days' AND strpos(e.payload::text, @proveedor_id::text) > 0
FOR UPDATE;

-- name: FijarPayloadEntrante :exec
UPDATE mensajeria.entrantes SET payload = @payload WHERE id = @id;

-- name: BloquearVerificacionesCliente :exec
-- Serializa el conteo y la reserva de un intento por cliente y empresa (dos conversaciones a la vez).
SELECT pg_advisory_xact_lock(hashtextextended('mensajeria.verificaciones:' || @empresa::bigint || ':' || @cliente_id::bigint, 0));

-- name: ReservarVerificacion :one
-- El intento se reserva antes de llamar a Argos: cuenta desde ya para el máximo del cliente.
INSERT INTO mensajeria.asistente_verificaciones (empresa_id, conversacion_id, cliente_id, resultado, creada_en, numero_verificado)
VALUES (sqlc.narg('empresa_id'), @conversacion_id, @cliente_id, 'en_curso', @creada_en, @numero_verificado)
RETURNING id;

-- name: CerrarVerificacion :execrows
-- El resultado del intento reservado (para el cliente con el que se hizo).
UPDATE mensajeria.asistente_verificaciones SET resultado = @resultado
WHERE id = @id AND cliente_id = @cliente_id AND resultado = 'en_curso';

-- name: VerificacionesRecientes :one
-- Intentos del desafío del cliente en la empresa que cuentan para sus máximos (los que Argos evaluó
-- y los reservados), separados: desde números verificados como suyos (comparten el tope del cliente),
-- desde números sin verificar (tope aparte), los de esta conversación sin verificar y los sin
-- verificar de la última media hora.
SELECT count(*) FILTER (WHERE numero_verificado)::bigint AS verificados,
       count(*) FILTER (WHERE NOT numero_verificado)::bigint AS sin_verificar,
       count(*) FILTER (WHERE NOT numero_verificado AND conversacion_id = @conversacion_id)::bigint AS de_la_conversacion,
       count(*) FILTER (WHERE NOT numero_verificado AND creada_en >= @reciente::timestamptz)::bigint AS sin_verificar_recientes
FROM mensajeria.asistente_verificaciones
WHERE coalesce(empresa_id, 0) = @empresa::bigint AND cliente_id = @cliente_id AND creada_en >= @desde
  AND resultado IN ('coincide', 'no_coincide', 'en_curso');

-- name: UltimosEntrantesTexto :many
SELECT id, texto, proveedor_id FROM mensajeria.conversacion_mensajes
WHERE conversacion_id = @conversacion_id AND direccion = 'entrante' AND texto IS NOT NULL AND creado_en >= @desde
ORDER BY seq DESC LIMIT 3;

-- name: EnmascararTextoMensaje :exec
-- El dato de identidad que escribió el cliente no se guarda (contrato con Argos §7.5): queda ***.
UPDATE mensajeria.conversacion_mensajes SET texto = @texto WHERE id = @id AND direccion = 'entrante';

-- name: DescartarTurnosSueltos :exec
-- §17.11: un turno que quedó pendiente al reanudar (arriendo suelto) y al que al final no le toca al
-- asistente (categoría en «sugiere», tope, el bot…) se cierra: si no, el mantenimiento lo reencolaría
-- sin fin.
UPDATE mensajeria.asistente_turnos SET estado = 'descartado', motivo = 'no_aplica', terminado_en = @ahora
WHERE conversacion_id = @conversacion_id AND estado = 'en_curso' AND tomado_en < @antes;

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
-- motivo: persona_presente (la tomó o escribió una persona, o se resolvió) o pausado (§17.11).
UPDATE mensajeria.asistente_turnos SET estado = 'descartado', motivo = @motivo, terminado_en = @ahora
WHERE conversacion_id = @conversacion_id AND estado = 'en_curso';

-- name: TicketsAsistenteRecientes :one
SELECT count(*)::bigint FROM mensajeria.asistente_acciones
WHERE conversacion_id = @conversacion_id AND accion = 'crear_ticket' AND resultado = 'ok' AND creada_en >= @desde;

-- name: FijarPresupuestoAutonomo :exec
UPDATE mensajeria.asistente_ajustes SET presupuesto_autonomo_pct = @pct, updated_at = now() WHERE id = @id;

-- Visitas del asistente (§17.10) ---------------------------------------------------------------------

-- name: ServiciosParaVisita :many
-- Los servicios del cliente en la empresa, con su estado y su zona (instantánea de Argos).
SELECT s.id, s.estado, s.zona_id FROM integracion.argos_servicios s
WHERE s.cliente_id = @cliente_id AND s.empresa_id = @empresa::bigint
ORDER BY s.id LIMIT 20;

-- name: BorrarOfertasVisita :exec
DELETE FROM mensajeria.asistente_ofertas_visita WHERE conversacion_id = @conversacion_id;

-- name: InsertarOfertaVisita :exec
INSERT INTO mensajeria.asistente_ofertas_visita (empresa_id, conversacion_id, cliente_id, servicio_id, zona_id, tipo, clave, inicio, fin, motivo,
    ofrecida_en, vence_en)
VALUES (sqlc.narg('empresa_id'), @conversacion_id, @cliente_id, @servicio_id, @zona_id, @tipo, @clave, @inicio, @fin, @motivo, @ofrecida_en, @vence_en);

-- name: OfertaVisita :one
-- Una ventana ofrecida a esta conversación y todavía vigente.
SELECT * FROM mensajeria.asistente_ofertas_visita
WHERE conversacion_id = @conversacion_id AND clave = @clave AND vence_en > @ahora;

-- name: PurgarOfertasVisita :exec
DELETE FROM mensajeria.asistente_ofertas_visita WHERE vence_en < @antes;

-- name: AccionesAsistenteRecientes :one
-- Veces que el asistente hizo esa acción (con éxito) en la conversación: los topes por día.
SELECT count(*)::bigint FROM mensajeria.asistente_acciones
WHERE conversacion_id = @conversacion_id AND accion = @accion AND resultado = 'ok' AND creada_en >= @desde;

-- name: AccionesAsistenteEmpresa :one
SELECT count(*)::bigint FROM mensajeria.asistente_acciones
WHERE coalesce(empresa_id, 0) = @empresa::bigint AND accion = @accion AND resultado = 'ok' AND creada_en >= @desde;

-- name: TicketParaVisita :one
-- El último ticket creado desde la conversación en su periodo abierto que sirve para una visita: del
-- mismo cliente, del mismo servicio, de la empresa y todavía abierto (ni resuelto ni cerrado).
SELECT tk.id FROM mensajeria.conversacion_vinculos v
JOIN mensajeria.conversaciones c ON c.id = v.conversacion_id
JOIN soporte.tickets tk ON tk.id = CASE WHEN v.entidad_id ~ '^[0-9]{1,18}$' THEN v.entidad_id::bigint END
WHERE v.conversacion_id = @conversacion_id AND v.tipo = 'ticket' AND v.created_at >= coalesce(c.reabierta_en, 'epoch')
  AND tk.cliente_id = @cliente_id AND tk.servicio_id = @servicio_id AND coalesce(tk.empresa_id, 0) = @empresa::bigint
  AND tk.estado IN ('nuevo', 'abierto', 'en_espera')
ORDER BY v.created_at DESC LIMIT 1;

-- name: FijarVisitasAsistente :exec
UPDATE mensajeria.asistente_ajustes SET visitas = @visitas, updated_at = now() WHERE id = @id;

-- name: AccionYaAnotada :one
-- El asistente ya anotó esa acción (con éxito) sobre esa entidad en la conversación: un reintento no
-- la anota ni la cuenta otra vez.
SELECT EXISTS (SELECT 1 FROM mensajeria.asistente_acciones
    WHERE conversacion_id = @conversacion_id AND accion = @accion AND resultado = 'ok' AND entidad_id = @entidad_id::text)::boolean;


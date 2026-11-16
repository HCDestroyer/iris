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
SELECT EXISTS (
    SELECT 1 FROM mensajeria.conversacion_mensajes m
    WHERE m.conversacion_id = @conversacion_id AND m.direccion = 'saliente' AND m.autor_id IS NOT NULL AND m.creado_en >= @desde
)::boolean;

-- name: UltimoAdjuntoEntrante :one
-- La última imagen o documento que mandó el cliente (la boleta).
SELECT id, adjunto_id, tipo, creado_en FROM mensajeria.conversacion_mensajes
WHERE conversacion_id = @conversacion_id AND direccion = 'entrante' AND adjunto_id IS NOT NULL AND tipo IN ('imagen', 'documento')
ORDER BY seq DESC LIMIT 1;

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

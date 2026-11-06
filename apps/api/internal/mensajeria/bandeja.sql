-- Bandeja 1.1 (contratos/conversaciones.md, seccion 14): supervisores por cola, disponibilidad, bot e
-- iniciar conversacion. sqlc: mensajeriabd. Comentarios sin acentos (sqlc reescribe por bytes).

-- Supervisores por cola ------------------------------------------------------------------------

-- name: SupervisoresDeColas :many
SELECT s.cola_id, u.id, u.nombre FROM mensajeria.cola_supervisores s JOIN nucleo.usuarios u ON u.id = s.usuario_id
WHERE s.cola_id = ANY (@colas::bigint[]) AND s.tipo = 'persona'
ORDER BY s.cola_id, lower(u.nombre), u.id;

-- name: BorrarSupervisoresCola :exec
DELETE FROM mensajeria.cola_supervisores WHERE cola_id = @cola_id AND tipo = 'persona';

-- name: InsertarSupervisorCola :exec
INSERT INTO mensajeria.cola_supervisores (cola_id, tipo, usuario_id) VALUES (@cola_id, 'persona', @usuario_id) ON CONFLICT DO NOTHING;

-- name: EsSupervisorCola :one
SELECT EXISTS (SELECT 1 FROM mensajeria.cola_supervisores WHERE cola_id = @cola_id AND usuario_id = @usuario_id)::boolean;

-- name: ColasSupervisadasDeUsuarios :many
SELECT s.usuario_id, c.id, c.nombre FROM mensajeria.cola_supervisores s JOIN mensajeria.colas c ON c.id = s.cola_id
WHERE s.usuario_id = ANY (@usuarios::bigint[]) AND coalesce(c.empresa_id, 0) = @empresa::bigint
ORDER BY s.usuario_id, lower(c.nombre);

-- name: FijarBotCola :one
UPDATE mensajeria.colas SET bot_sin_confirmacion_min = sqlc.narg('sin_confirmacion'), bot_sin_disponibles_min = sqlc.narg('sin_disponibles')
WHERE id = @id RETURNING *;

-- name: AtiendeCola :one
-- El usuario es miembro o supervisor de la cola.
SELECT (EXISTS (SELECT 1 FROM mensajeria.cola_miembros WHERE cola_id = @cola_id::bigint AND usuario_id = @usuario_id::bigint)
    OR EXISTS (SELECT 1 FROM mensajeria.cola_supervisores WHERE cola_id = @cola_id::bigint AND usuario_id = @usuario_id::bigint))::boolean;

-- Bot del numero ----------------------------------------------------------------------------------

-- name: ConfigurarBotCanal :one
UPDATE mensajeria.canales SET bot = @bot WHERE id = @id RETURNING *;

-- name: GuardarBotConversacion :exec
UPDATE mensajeria.conversaciones
SET bot_espera_en = sqlc.narg('espera_en'), bot_vence_en = sqlc.narg('vence_en'), bot_motivo = sqlc.narg('motivo'),
    bot_atendio_en = sqlc.narg('atendio_en'), bot_menu = sqlc.narg('menu'), updated_at = now()
WHERE id = @id;

-- name: BotVencidos :many
-- Temporizadores vencidos hasta ahora (el caso vuelve a revisar con la fila bloqueada).
SELECT id FROM mensajeria.conversaciones WHERE bot_vence_en IS NOT NULL AND bot_vence_en <= @ahora::timestamptz
ORDER BY bot_vence_en LIMIT 200;

-- Disponibilidad --------------------------------------------------------------------------------

-- name: DisponibilidadDe :one
SELECT * FROM mensajeria.disponibilidad WHERE usuario_id = @usuario_id AND coalesce(empresa_id, 0) = @empresa::bigint;

-- name: DisponibilidadParaActualizar :one
SELECT * FROM mensajeria.disponibilidad WHERE usuario_id = @usuario_id AND coalesce(empresa_id, 0) = @empresa::bigint FOR UPDATE;

-- name: DisponibilidadPorID :one
SELECT * FROM mensajeria.disponibilidad WHERE id = @id FOR UPDATE;

-- name: InsertarDisponibilidad :one
INSERT INTO mensajeria.disponibilidad (usuario_id, empresa_id, estado, origen, desde, ultima_actividad, sesion_id, horario_hasta)
VALUES (@usuario_id, sqlc.narg('empresa_id'), @estado, @origen, @desde, @ultima_actividad, sqlc.narg('sesion_id'), sqlc.narg('horario_hasta'))
ON CONFLICT DO NOTHING
RETURNING *;

-- name: FijarDisponibilidad :one
UPDATE mensajeria.disponibilidad
SET estado = @estado, origen = @origen, desde = @desde, ultima_actividad = @ultima_actividad, sesion_id = sqlc.narg('sesion_id'),
    horario_hasta = sqlc.narg('horario_hasta'), version = version + 1, updated_at = now()
WHERE id = @id
RETURNING *;

-- name: LatidoDisponibilidad :one
-- Actividad: sin subir la version ni cambiar el estado.
UPDATE mensajeria.disponibilidad
SET ultima_actividad = GREATEST(ultima_actividad, @ahora::timestamptz), sesion_id = coalesce(sqlc.narg('sesion_id'), sesion_id),
    horario_hasta = CASE WHEN @con_horario::boolean THEN sqlc.narg('horario_hasta') ELSE horario_hasta END
WHERE id = @id
RETURNING *;

-- name: TocarActividad :exec
UPDATE mensajeria.disponibilidad SET ultima_actividad = GREATEST(ultima_actividad, @ahora::timestamptz)
WHERE usuario_id = @usuario_id AND coalesce(empresa_id, 0) = @empresa::bigint;

-- name: DisponibilidadDeUsuarios :many
SELECT usuario_id, estado, origen, desde FROM mensajeria.disponibilidad
WHERE coalesce(empresa_id, 0) = @empresa::bigint AND usuario_id = ANY (@usuarios::bigint[]);

-- name: DisponiblesDeCola :many
-- Disponibles que atienden la cola (miembros o supervisores); sin cola, los de la empresa.
SELECT u.id, u.nombre FROM mensajeria.disponibilidad d JOIN nucleo.usuarios u ON u.id = d.usuario_id AND u.activo
WHERE coalesce(d.empresa_id, 0) = @empresa::bigint AND d.estado = 'disponible'
  AND (sqlc.narg('cola_id')::bigint IS NULL
       OR EXISTS (SELECT 1 FROM mensajeria.cola_miembros m WHERE m.cola_id = sqlc.narg('cola_id')::bigint AND m.usuario_id = d.usuario_id)
       OR EXISTS (SELECT 1 FROM mensajeria.cola_supervisores s WHERE s.cola_id = sqlc.narg('cola_id')::bigint AND s.usuario_id = d.usuario_id))
ORDER BY lower(u.nombre), u.id;

-- name: DisponiblesPorVencer :many
-- Disponibles que deben pasar a no_disponible: su sesion se cerro o vencio, termino su horario o
-- pasaron los minutos de inactividad de la empresa.
WITH r AS (SELECT @ahora::timestamptz AS ahora)
SELECT d.id,
    (CASE WHEN s.id IS NULL OR s.revocada_en IS NOT NULL OR s.expira_en <= r.ahora OR NOT u.activo THEN 'cierre_sesion'
          WHEN d.horario_hasta IS NOT NULL AND d.horario_hasta <= r.ahora THEN 'horario'
          ELSE 'inactividad' END)::text AS motivo
FROM r, mensajeria.disponibilidad d
JOIN nucleo.usuarios u ON u.id = d.usuario_id
LEFT JOIN nucleo.sesiones s ON s.id = d.sesion_id
LEFT JOIN mensajeria.ajustes_bandeja a ON coalesce(a.empresa_id, 0) = coalesce(d.empresa_id, 0)
WHERE d.estado = 'disponible'
  AND (s.id IS NULL OR s.revocada_en IS NOT NULL OR s.expira_en <= r.ahora OR NOT u.activo
       OR (d.horario_hasta IS NOT NULL AND d.horario_hasta <= r.ahora)
       OR d.ultima_actividad <= r.ahora - make_interval(mins => coalesce(a.minutos_inactividad, 15)))
ORDER BY d.id
LIMIT 500;

-- name: HorariosDeAgente :many
-- Horarios de las colas activas que atiende (el de la cola o, sin el, el de sus numeros).
SELECT h.* FROM mensajeria.horarios h
WHERE h.activo AND h.id IN (
    SELECT co.horario_id FROM mensajeria.colas co
    WHERE co.activa AND co.horario_id IS NOT NULL AND coalesce(co.empresa_id, 0) = @empresa::bigint
      AND (EXISTS (SELECT 1 FROM mensajeria.cola_miembros m WHERE m.cola_id = co.id AND m.usuario_id = @usuario_id::bigint)
           OR EXISTS (SELECT 1 FROM mensajeria.cola_supervisores s WHERE s.cola_id = co.id AND s.usuario_id = @usuario_id::bigint))
    UNION
    SELECT ca.horario_id FROM mensajeria.canales ca JOIN mensajeria.colas co ON co.id = ca.cola_id
    WHERE ca.activo AND co.activa AND co.horario_id IS NULL AND ca.horario_id IS NOT NULL AND coalesce(co.empresa_id, 0) = @empresa::bigint
      AND (EXISTS (SELECT 1 FROM mensajeria.cola_miembros m WHERE m.cola_id = co.id AND m.usuario_id = @usuario_id::bigint)
           OR EXISTS (SELECT 1 FROM mensajeria.cola_supervisores s WHERE s.cola_id = co.id AND s.usuario_id = @usuario_id::bigint)))
ORDER BY h.id;

-- name: AgenteSiempreEnHorario :one
-- Atiende alguna cola activa sin horario propio ni de sus numeros (o no atiende ninguna).
SELECT (NOT EXISTS (
    SELECT 1 FROM mensajeria.colas co
    WHERE co.activa AND coalesce(co.empresa_id, 0) = @empresa::bigint
      AND (EXISTS (SELECT 1 FROM mensajeria.cola_miembros m WHERE m.cola_id = co.id AND m.usuario_id = @usuario_id::bigint)
           OR EXISTS (SELECT 1 FROM mensajeria.cola_supervisores s WHERE s.cola_id = co.id AND s.usuario_id = @usuario_id::bigint)))
    OR EXISTS (
    SELECT 1 FROM mensajeria.colas co
    WHERE co.activa AND co.horario_id IS NULL AND coalesce(co.empresa_id, 0) = @empresa::bigint
      AND (EXISTS (SELECT 1 FROM mensajeria.cola_miembros m WHERE m.cola_id = co.id AND m.usuario_id = @usuario_id::bigint)
           OR EXISTS (SELECT 1 FROM mensajeria.cola_supervisores s WHERE s.cola_id = co.id AND s.usuario_id = @usuario_id::bigint))
      AND NOT EXISTS (SELECT 1 FROM mensajeria.canales ca JOIN mensajeria.horarios h ON h.id = ca.horario_id AND h.activo
                      WHERE ca.cola_id = co.id AND ca.activo)))::boolean;

-- Ajustes de la bandeja ----------------------------------------------------------------------------

-- name: AjustesBandeja :one
SELECT * FROM mensajeria.ajustes_bandeja WHERE coalesce(empresa_id, 0) = @empresa::bigint;

-- name: AjustesBandejaParaActualizar :one
SELECT * FROM mensajeria.ajustes_bandeja WHERE coalesce(empresa_id, 0) = @empresa::bigint FOR UPDATE;

-- name: InsertarAjustesBandeja :one
INSERT INTO mensajeria.ajustes_bandeja (empresa_id, minutos_inactividad) VALUES (sqlc.narg('empresa_id'), @minutos_inactividad)
ON CONFLICT DO NOTHING
RETURNING *;

-- name: ActualizarAjustesBandeja :one
UPDATE mensajeria.ajustes_bandeja SET minutos_inactividad = @minutos_inactividad, version = version + 1, updated_at = now()
WHERE id = @id RETURNING *;

-- Agentes e iniciar ------------------------------------------------------------------------------

-- name: AbiertasDeUsuarios :many
-- Conversaciones activas asignadas a cada usuario en la empresa.
SELECT asignado_a::bigint AS usuario_id, count(*)::bigint AS total FROM mensajeria.conversaciones
WHERE coalesce(empresa_id, 0) = @empresa::bigint AND estado <> 'resuelta' AND asignado_a = ANY (@usuarios::bigint[])
GROUP BY asignado_a;

-- name: BuscarClientesInstantanea :many
-- Clientes de la empresa en la instantanea de Argos por nombre, codigo o digitos del telefono.
SELECT ac.id, ac.nombre, ac.codigo, ac.telefono, ac.movil FROM integracion.argos_clientes ac
WHERE (@empresa::bigint = 0 OR ac.empresa_id = @empresa::bigint OR @empresa::bigint = ANY (ac.empresas))
  AND (ac.nombre ILIKE '%' || @q::text || '%' OR ac.codigo ILIKE @q::text || '%'
       OR (@digitos::text <> '' AND (regexp_replace(ac.movil, '\D', '', 'g') LIKE '%' || @digitos::text || '%'
                                     OR regexp_replace(ac.telefono, '\D', '', 'g') LIKE '%' || @digitos::text || '%')))
ORDER BY lower(ac.nombre), ac.id
LIMIT @limite;

-- name: ClienteInstantaneaEnEmpresa :one
SELECT ac.id, ac.nombre, ac.codigo, ac.telefono, ac.movil FROM integracion.argos_clientes ac
WHERE ac.id = @id AND (@empresa::bigint = 0 OR ac.empresa_id = @empresa::bigint OR @empresa::bigint = ANY (ac.empresas));

-- name: BuscarContactosIrisDestino :many
SELECT * FROM mensajeria.contactos_iris
WHERE coalesce(empresa_id, 0) = @empresa::bigint
  AND (nombre ILIKE '%' || @q::text || '%' OR (@digitos::text <> '' AND telefono LIKE '%' || @digitos::text || '%'))
ORDER BY lower(nombre), id
LIMIT @limite;

-- name: ConversacionDeContacto :one
SELECT * FROM mensajeria.conversaciones
WHERE coalesce(empresa_id, 0) = @empresa::bigint AND canal_id = @canal_id AND numero = @numero;

-- name: ContactoIrisDeEmpresa :one
SELECT * FROM mensajeria.contactos_iris WHERE id = @id AND coalesce(empresa_id, 0) = @empresa::bigint;

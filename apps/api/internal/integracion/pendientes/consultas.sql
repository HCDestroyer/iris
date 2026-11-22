-- Cola de pendientes hacia Argos (internal/integracion/pendientes) ---------------------------

-- name: InsertarPendiente :one
-- Idempotente por la Idempotency-Key del contrato: un evento repetido no duplica la acción.
INSERT INTO integracion.pendientes (
    empresa_id, sistema, operacion, idempotency_key, entidad, entidad_id, metodo, ruta, payload,
    depende_de, datos_incompletos, estado, ultimo_error, proximo_intento, evento_id, plantilla, por_resolver, verificar,
    tecnico_usuario_id, actor, transito_id
) VALUES (
    sqlc.narg(empresa_id), @sistema, @operacion, @idempotency_key, @entidad, @entidad_id, @metodo, @ruta, @payload,
    @depende_de, @datos_incompletos, @estado, sqlc.narg(ultimo_error), @proximo_intento, sqlc.narg(evento_id),
    sqlc.narg(plantilla), @por_resolver, coalesce(sqlc.narg(verificar)::text[], '{}'), sqlc.narg(tecnico_usuario_id),
    sqlc.narg(actor), sqlc.narg(transito_id)
)
ON CONFLICT (idempotency_key) DO NOTHING
RETURNING *;

-- name: PendientePorClave :one
SELECT * FROM integracion.pendientes WHERE idempotency_key = @idempotency_key;

-- name: PendientePorID :one
SELECT * FROM integracion.pendientes WHERE id = @id;

-- name: PendientePorIDParaActualizar :one
SELECT * FROM integracion.pendientes WHERE id = @id FOR UPDATE;

-- name: TomarVencido :one
-- El siguiente vencido hasta @ahora cuyas dependencias ya están hechas (o uno en_curso cuyo
-- worker cayó: tomado antes de @reclamar_antes). SKIP LOCKED: varios workers no toman el mismo.
SELECT p.* FROM integracion.pendientes p
WHERE ((p.estado = 'pendiente' AND p.proximo_intento <= @ahora)
    OR (p.estado = 'en_curso' AND p.tomado_en <= @reclamar_antes::timestamptz))
  AND NOT p.por_resolver
  AND NOT EXISTS (
      SELECT 1 FROM integracion.pendientes d WHERE d.id = ANY (p.depende_de) AND d.estado NOT IN ('hecho', 'resuelto_manual'))
ORDER BY p.proximo_intento, p.id
LIMIT 1
FOR UPDATE OF p SKIP LOCKED;

-- name: MarcarEnCurso :exec
UPDATE integracion.pendientes SET estado = 'en_curso', tomado_en = @ahora::timestamptz, updated_at = now() WHERE id = @id;

-- name: ResolverPendiente :one
-- Resultado de un intento (solo si sigue en_curso: un reintento manual a la vez no se pisa).
UPDATE integracion.pendientes
SET estado          = @estado,
    intentos        = intentos + 1,
    proximo_intento = @proximo_intento,
    ultimo_error    = sqlc.narg(ultimo_error),
    ultimo_status   = sqlc.narg(ultimo_status),
    hecho_en        = sqlc.narg(hecho_en),
    respuesta       = coalesce(respuesta, sqlc.narg(respuesta)::jsonb),
    codigo_error    = sqlc.narg(codigo_error),
    tomado_en       = NULL,
    version         = version + 1,
    updated_at      = now()
WHERE id = @id AND estado = 'en_curso'
RETURNING *;

-- name: ReintentarPendiente :one
UPDATE integracion.pendientes
SET estado = 'pendiente', intentos = 0, proximo_intento = @ahora, tomado_en = NULL,
    version = version + 1, updated_at = now()
WHERE id = @id AND version = @version
RETURNING *;

-- name: ListarPendientes :many
-- Más recientes primero. @todas = sin filtro de empresa (actor sistema); si no, empresa_id
-- igual a la activa (null = modo sin conector).
SELECT * FROM integracion.pendientes
WHERE (@despues_de::bigint = 0 OR id < @despues_de)
  AND (@todas::boolean OR empresa_id IS NOT DISTINCT FROM sqlc.narg(empresa_id)::bigint)
  AND (sqlc.narg(entidad)::text IS NULL OR entidad = sqlc.narg(entidad))
  AND (sqlc.narg(entidad_id)::text IS NULL OR entidad_id = sqlc.narg(entidad_id))
  AND (sqlc.narg(estado)::text IS NULL OR estado = sqlc.narg(estado))
  AND (sqlc.narg(estado_distinto_de)::text IS NULL OR estado <> sqlc.narg(estado_distinto_de))
  AND (NOT @solo_conflicto::boolean OR conflicto IS NOT NULL)
ORDER BY id DESC
LIMIT @limite;

-- name: PendientesDeEntidad :many
SELECT * FROM integracion.pendientes WHERE entidad = @entidad AND entidad_id = @entidad_id ORDER BY id;

-- name: CorregirPendiente :one
-- Cierre repetido con los datos que faltaban: el pendiente incompleto vuelve a la cola.
UPDATE integracion.pendientes
SET payload = @payload, ruta = @ruta, depende_de = @depende_de, datos_incompletos = false,
    estado = 'pendiente', intentos = 0, ultimo_error = NULL, ultimo_status = NULL,
    proximo_intento = @ahora, version = version + 1, updated_at = now()
WHERE id = @id AND estado = 'fallido_negocio' AND datos_incompletos
RETURNING *;

-- name: CorregirTecnico :one
-- Reintento a mano de un pendiente rechazado por el técnico (tecnico_no_encontrado…, contrato 0.5.0
-- §3.3): el cuerpo lleva el vínculo actual del técnico. Argos no guardó la clave (un 4xx no la
-- guarda), así que otro cuerpo con la misma Idempotency-Key es seguro.
UPDATE integracion.pendientes
SET payload = @payload, estado = 'pendiente', intentos = 0, ultimo_error = NULL, ultimo_status = NULL, codigo_error = NULL,
    proximo_intento = @ahora, tomado_en = NULL, version = version + 1, updated_at = now()
WHERE id = @id AND version = @version AND estado = 'fallido_negocio'
RETURNING *;

-- name: VinculoArgosDeTecnico :one
-- El vínculo con Argos de la ficha de agenda del usuario (id_externo; vacío = sin vincular).
SELECT coalesce(argos_id_externo, '')::text AS id_externo FROM agenda.tecnicos WHERE usuario_id = @usuario_id;

-- name: MarcarConflicto :one
UPDATE integracion.pendientes
SET conflicto = @conflicto, conflicto_payload = @conflicto_payload, conflicto_en = @ahora::timestamptz,
    version = version + 1, updated_at = now()
WHERE id = @id
RETURNING *;

-- name: BloquearDependientes :many
-- La dependencia @id quedó fallido_negocio o agotado: sus dependientes en espera se bloquean.
UPDATE integracion.pendientes
SET estado = 'bloqueado', ultimo_error = @motivo, version = version + 1, updated_at = now()
WHERE estado = 'pendiente' AND @id::bigint = ANY (depende_de)
RETURNING *;

-- name: DesbloquearDependientes :many
-- La dependencia @id quedó hecha (o resuelta a mano): vuelven a la cola los bloqueados cuyas
-- dependencias están todas hechas o resueltas a mano.
UPDATE integracion.pendientes p
SET estado = 'pendiente', proximo_intento = @ahora, ultimo_error = NULL, version = p.version + 1, updated_at = now()
WHERE p.estado = 'bloqueado' AND @id::bigint = ANY (p.depende_de)
  AND NOT EXISTS (
      SELECT 1 FROM integracion.pendientes d WHERE d.id = ANY (p.depende_de) AND d.estado NOT IN ('hecho', 'resuelto_manual'))
RETURNING p.*;

-- name: DependientesPorResolver :many
-- Dependientes de @id cuyo cuerpo todavía tiene marcadores, con candado.
SELECT * FROM integracion.pendientes
WHERE por_resolver AND @id::bigint = ANY (depende_de) AND estado IN ('pendiente', 'bloqueado')
ORDER BY id
FOR UPDATE;

-- name: PendientesPorIDCompartido :many
-- Las dependencias de un pendiente con candado compartido: si otra transacción está dejando una
-- en «hecho», se espera a que confirme (y se ve su estado y su respuesta); y mientras esta
-- transacción no confirme, la otra no puede marcarla (y luego ve al dependiente nuevo).
SELECT * FROM integracion.pendientes WHERE id = ANY (@ids::bigint[]) ORDER BY id FOR SHARE;

-- name: PorResolverListos :many
-- Red de seguridad del job: dependientes con marcadores cuyas dependencias ya están todas
-- hechas (no se debería ver ninguno: se resuelven al quedar hecha la última).
SELECT p.* FROM integracion.pendientes p
WHERE p.por_resolver AND p.estado IN ('pendiente', 'bloqueado')
  AND NOT EXISTS (
      SELECT 1 FROM integracion.pendientes d WHERE d.id = ANY (p.depende_de) AND d.estado <> 'hecho')
ORDER BY p.id
LIMIT @limite
FOR UPDATE OF p SKIP LOCKED;

-- name: FijarCuerpoResuelto :one
-- Una sola vez: el cuerpo final reemplaza a la plantilla (después no cambia).
UPDATE integracion.pendientes
SET payload = @payload, por_resolver = false, proximo_intento = @ahora::timestamptz,
    version = version + 1, updated_at = now()
WHERE id = @id AND por_resolver
RETURNING *;

-- name: FallarResolucion :one
-- La respuesta de la dependencia no trae el dato que pide el marcador: fallido de negocio.
UPDATE integracion.pendientes
SET estado = 'fallido_negocio', datos_incompletos = true, ultimo_error = @motivo,
    version = version + 1, updated_at = now()
WHERE id = @id AND por_resolver
RETURNING *;

-- name: CompletarEsperando :one
-- El pendiente que esperaba algo de Iris (ultimo_error = @motivo, p. ej. esperando_firma) recibe su
-- cuerpo final y vuelve a la cola. Una sola vez: después ya no está bloqueado con ese motivo. Solo
-- si nadie lo tocó desde que se leyó (@version): un armado viejo no pisa una recepción rehecha.
UPDATE integracion.pendientes
SET payload = @payload, estado = 'pendiente', ultimo_error = NULL, proximo_intento = @ahora, version = version + 1, updated_at = now()
WHERE id = @id AND estado = 'bloqueado' AND ultimo_error = @motivo::text AND version = @version
RETURNING *;

-- name: FallarEsperando :one
-- Lo que esperaba no sirve (firma inválida…): queda fallido_negocio con el código, sin llamar a Argos.
UPDATE integracion.pendientes
SET estado = 'fallido_negocio', ultimo_error = @detalle::text, codigo_error = @codigo::text, version = version + 1, updated_at = now()
WHERE id = @id AND estado = 'bloqueado' AND ultimo_error = @motivo::text AND version = @version
RETURNING *;

-- name: PendientesEsperando :many
-- Los bloqueados esperando algo de Iris (barrido del job).
SELECT * FROM integracion.pendientes
WHERE estado = 'bloqueado' AND ultimo_error = @motivo::text AND operacion = @operacion::text
ORDER BY id
LIMIT @limite;

-- name: PendientesDeTecnico :many
-- Los de una operación de un técnico (recepciones: la app de campo ve su estado), recientes primero.
SELECT * FROM integracion.pendientes
WHERE tecnico_usuario_id = @usuario_id AND operacion = @operacion::text AND updated_at >= @desde
ORDER BY id DESC
LIMIT @limite;

-- name: RehacerEsperando :one
-- Un pendiente rechazado (fallido_negocio: Argos no guardó la clave, o nunca salió) vuelve a esperar
-- con un cuerpo nuevo (p. ej. otra recepción del técnico con otra firma). @estados: desde cuáles
-- (fallido_negocio; o, si la toma el destinatario real, también pendiente, bloqueado o agotado).
-- @tecnico_usuario_id (o null = el mismo) pasa a ser el dueño.
UPDATE integracion.pendientes
SET payload = @payload, estado = 'bloqueado', ultimo_error = @motivo::text, codigo_error = NULL, ultimo_status = NULL,
    intentos = 0, tomado_en = NULL, tecnico_usuario_id = coalesce(sqlc.narg(tecnico_usuario_id), tecnico_usuario_id),
    version = version + 1, updated_at = now()
WHERE id = @id AND estado = ANY (@estados::text[])
RETURNING *;

-- name: BodegaDeOrden :one
-- La bodega de la orden de trabajo (para volver de tecnico a bodega_id si Argos ya no lo entiende).
SELECT bodega_id FROM campo.ordenes WHERE id = @id::bigint;

-- name: ResolverManual :one
-- Un rechazado por Argos (fallido_negocio, también incompleto) queda resuelto a mano: estado final,
-- nunca vuelve a salir hacia Argos.
UPDATE integracion.pendientes
SET estado = 'resuelto_manual', resuelto_motivo = @motivo::text, resuelto_por = sqlc.narg(resuelto_por), resuelto_en = @ahora::timestamptz,
    resuelto_como = 'manual', resuelto_cliente_id = sqlc.narg(resuelto_cliente_id), tomado_en = NULL, version = version + 1, updated_at = now()
WHERE id = @id AND version = @version AND estado = 'fallido_negocio'
RETURNING *;

-- name: DependientesAbiertos :many
-- Los dependientes de @id que aún esperan (pendiente o bloqueado), bloqueados para decidir qué hacer
-- con ellos al resolver @id a mano.
SELECT * FROM integracion.pendientes
WHERE @id::bigint = ANY (depende_de) AND estado IN ('pendiente', 'bloqueado')
ORDER BY id
FOR UPDATE;

-- name: FallarPorResolucionManual :one
-- La dependencia se resolvió a mano y este cuerpo esperaba su respuesta (marcadores): nunca la
-- tendrá. Queda incompleto (fallido_negocio + datos_incompletos) para resolverlo también a mano o
-- corregirlo con los datos reales; sin marcadores pendientes (por_resolver = false).
UPDATE integracion.pendientes
SET estado = 'fallido_negocio', datos_incompletos = true, por_resolver = false, ultimo_error = @motivo::text,
    codigo_error = 'dependencia_resuelta_a_mano', tomado_en = NULL, version = version + 1, updated_at = now()
WHERE id = @id AND estado IN ('pendiente', 'bloqueado')
RETURNING *;

-- name: ResolverManualDependiente :one
-- El cargo que esperaba al consumo resuelto a mano también se da por resuelto (resolver_tambien).
UPDATE integracion.pendientes
SET estado = 'resuelto_manual', resuelto_motivo = @motivo::text, resuelto_por = sqlc.narg(resuelto_por), resuelto_en = @ahora::timestamptz,
    tomado_en = NULL, version = version + 1, updated_at = now()
WHERE id = @id AND estado IN ('pendiente', 'bloqueado')
RETURNING *;

-- --- Depósito de tránsito (contrato 0.10.0 §3.4.8) --------------------------------------------

-- name: GuardarTransito :exec
INSERT INTO integracion.datos_transito (id, empresa_id, entidad, orden_id, cifrado, llave_cifrada, tomado_en)
VALUES (@id, sqlc.narg(empresa_id), @entidad, @orden_id, @cifrado, @llave_cifrada, @tomado_en);

-- name: LeerTransito :one
SELECT * FROM integracion.datos_transito WHERE id = @id;

-- name: BorrarTransito :exec
-- Se destruye la llave de datos con el contenido.
UPDATE integracion.datos_transito SET cifrado = NULL, llave_cifrada = NULL, borrado_en = @ahora, motivo_borrado = @motivo
WHERE id = @id AND cifrado IS NOT NULL;

-- name: PurgarTransito :execrows
-- A los 7 días de tomado se borra lo que no se entregó (rechazado, agotado o en espera).
UPDATE integracion.datos_transito SET cifrado = NULL, llave_cifrada = NULL, borrado_en = @ahora, motivo_borrado = 'vencido'
WHERE cifrado IS NOT NULL AND tomado_en < @antes;

-- name: PasosTitularDeOrden :many
-- Los pendientes del paso «Confirmar titular y plan» de la orden (A41, A39, A43, A40), en orden:
-- la activación y el cobro de la instalación dependen de todos (§3.4, «Orden»). Los reemplazados
-- (resuelto_manual) cuentan como hechos.
SELECT * FROM integracion.pendientes
WHERE entidad = 'orden' AND entidad_id = @orden_id::text
    AND operacion IN ('registrar_identidad', 'cambiar_titular', 'completar_datos', 'cambiar_plan')
ORDER BY id;

-- name: TransitoVigente :one
-- El depósito sigue con sus datos (no se entregó, no se borró ni venció).
SELECT (cifrado IS NOT NULL)::boolean AS vigente FROM integracion.datos_transito WHERE id = @id;

-- name: RedirigirDependientes :many
-- Un paso del titular rechazado se reemplaza por otra toma: lo que esperaba al viejo (los pasos
-- siguientes, la activación, el cobro) pasa a esperar al nuevo, en la misma posición (los
-- marcadores apuntan al nuevo).
UPDATE integracion.pendientes
SET depende_de = array_replace(depende_de, @viejo::bigint, @nuevo::bigint), version = version + 1, updated_at = now()
WHERE @viejo::bigint = ANY (depende_de) AND estado IN ('pendiente', 'bloqueado')
RETURNING *;

-- name: ReemplazarPendiente :one
-- El paso que Argos rechazó (fallido_negocio; lo agotado se reintenta con su clave) queda
-- resuelto_manual: lo reemplaza otra toma o lo valida un supervisor (@como). Nunca vuelve a salir.
UPDATE integracion.pendientes
SET estado = 'resuelto_manual', resuelto_motivo = @motivo::text, resuelto_por = sqlc.narg(resuelto_por),
    resuelto_como = @como::text, resuelto_en = @ahora::timestamptz, tomado_en = NULL, version = version + 1, updated_at = now()
WHERE id = @id AND estado = 'fallido_negocio'
RETURNING *;

-- name: DetenerPasosTitular :many
-- La orden se canceló o falló: sus pasos del titular que no salieron (ni están saliendo) quedan
-- resuelto_manual con el motivo. Lo en_curso termina; lo hecho no se deshace.
UPDATE integracion.pendientes
SET estado = 'resuelto_manual', resuelto_motivo = @motivo::text, resuelto_como = @motivo::text, resuelto_por = sqlc.narg(resuelto_por),
    resuelto_en = @ahora::timestamptz, tomado_en = NULL, version = version + 1, updated_at = now()
WHERE entidad = 'orden' AND entidad_id = @orden_id::text
    AND operacion IN ('registrar_identidad', 'cambiar_titular', 'completar_datos', 'cambiar_plan')
    AND estado IN ('pendiente', 'bloqueado', 'fallido_negocio', 'agotado')
RETURNING *;

-- name: DetenerEnCurso :one
-- Un paso del titular en_curso de una orden ya cancelada o fallida que no salió bien: detenido.
UPDATE integracion.pendientes
SET estado = 'resuelto_manual', resuelto_motivo = @motivo::text, resuelto_como = @motivo::text,
    resuelto_en = @ahora::timestamptz, tomado_en = NULL, intentos = intentos + 1, version = version + 1, updated_at = now()
WHERE id = @id AND estado = 'en_curso'
RETURNING *;

-- name: FallarPorTitularAMano :one
-- El cambio de titular se resolvió a mano sin decir el cliente final: el consumo y el cobro no
-- salen solos (incompletos, para resolverlos también a mano o confirmar el cliente).
UPDATE integracion.pendientes
SET estado = 'fallido_negocio', datos_incompletos = true, por_resolver = false, ultimo_error = @motivo::text,
    codigo_error = 'titular_resuelto_a_mano', tomado_en = NULL, version = version + 1, updated_at = now()
WHERE id = @id AND estado IN ('pendiente', 'bloqueado')
RETURNING *;

-- name: FijarClienteCargo :one
-- El cambio de titular se resolvió a mano con el cliente final: el cobro va a su nombre.
UPDATE integracion.pendientes
SET payload = @payload, por_resolver = false, proximo_intento = @ahora::timestamptz, version = version + 1, updated_at = now()
WHERE id = @id AND estado IN ('pendiente', 'bloqueado')
RETURNING *;

-- name: MarcarTitularEnRevision :execrows
-- Argos respondió titular_no_coincide: la orden queda en revisión de un supervisor (persistente).
UPDATE campo.orden_titular
SET en_revision = true, revision_desde = coalesce(revision_desde, @ahora::timestamptz), version = version + 1, updated_at = now()
WHERE orden_id = @orden_id::bigint AND NOT en_revision;

-- name: EstadoDeOrden :one
SELECT estado FROM campo.ordenes WHERE id = @id::bigint;

-- name: DescartarClavesAgotadas :execrows
-- La clave Wi-Fi de un pendiente agotado (Argos no respondió) se descarta a las 24 h (0.9.0 §12.1).
UPDATE integracion.datos_transito SET cifrado = NULL, llave_cifrada = NULL, borrado_en = @ahora, motivo_borrado = 'vencido'
WHERE cifrado IS NOT NULL AND id IN (
    SELECT transito_id FROM integracion.pendientes
    WHERE operacion IN ('aprovisionar', 'cambiar_wifi') AND estado = 'agotado' AND transito_id IS NOT NULL AND updated_at < @antes);

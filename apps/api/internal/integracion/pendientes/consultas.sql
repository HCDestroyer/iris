-- Cola de pendientes hacia Argos (internal/integracion/pendientes) ---------------------------

-- name: InsertarPendiente :one
-- Idempotente por la Idempotency-Key del contrato: un evento repetido no duplica la acción.
INSERT INTO integracion.pendientes (
    empresa_id, sistema, operacion, idempotency_key, entidad, entidad_id, metodo, ruta, payload,
    depende_de, datos_incompletos, estado, ultimo_error, proximo_intento, evento_id
) VALUES (
    sqlc.narg(empresa_id), @sistema, @operacion, @idempotency_key, @entidad, @entidad_id, @metodo, @ruta, @payload,
    @depende_de, @datos_incompletos, @estado, sqlc.narg(ultimo_error), @proximo_intento, sqlc.narg(evento_id)
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
  AND NOT EXISTS (
      SELECT 1 FROM integracion.pendientes d WHERE d.id = ANY (p.depende_de) AND d.estado <> 'hecho')
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
-- La dependencia @id quedó hecha: vuelven a la cola los bloqueados cuyas dependencias están todas hechas.
UPDATE integracion.pendientes p
SET estado = 'pendiente', proximo_intento = @ahora, ultimo_error = NULL, version = p.version + 1, updated_at = now()
WHERE p.estado = 'bloqueado' AND @id::bigint = ANY (p.depende_de)
  AND NOT EXISTS (
      SELECT 1 FROM integracion.pendientes d WHERE d.id = ANY (p.depende_de) AND d.estado <> 'hecho')
RETURNING p.*;

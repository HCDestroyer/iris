-- name: InsertarAccion :one
INSERT INTO auditoria.acciones (
    actor_tipo, actor_id, actor_nombre, token_id, canal, accion,
    entidad, entidad_id, empresa_id, entrada, resultado, error, request_id
) VALUES (
    @actor_tipo, @actor_id, @actor_nombre, @token_id, @canal, @accion,
    @entidad, @entidad_id, @empresa_id, @entrada, @resultado, @error, @request_id
)
RETURNING id;

-- name: InsertarCambio :exec
INSERT INTO auditoria.cambios (accion_id, entidad, entidad_id, campo, antes, despues)
VALUES (@accion_id, @entidad, @entidad_id, @campo, @antes, @despues);

-- name: AccionesPorRequest :many
SELECT * FROM auditoria.acciones WHERE request_id = @request_id ORDER BY id;

-- name: CambiosDeAccion :many
SELECT * FROM auditoria.cambios WHERE accion_id = @accion_id ORDER BY campo, id;

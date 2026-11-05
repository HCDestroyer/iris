-- +goose Up
-- Chat: revisión de seguridad de la fase 1 (PR #110).
--
-- 1. La lápida no guarda nada del contenido: hash_contenido queda en null al eliminar.
-- 2. La Idempotency-Key de POST …/mensajes la aplica el caso de uso (no se guarda la respuesta con
--    el texto en nucleo.idempotencia): la clave queda en el mensaje.
-- 3. El despacho «unido» al hilo de una orden solo lo sigue viendo mientras su ordenes.ver (con
--    alcance) cubra la orden: chat_visibles recibe ese alcance.
ALTER TABLE mensajeria.chat_mensajes ALTER COLUMN hash_contenido DROP NOT NULL;
ALTER TABLE mensajeria.chat_mensajes ADD CONSTRAINT chat_mensajes_hash_lapida_ck
    CHECK ((eliminado_en IS NULL) = (hash_contenido IS NOT NULL));
ALTER TABLE mensajeria.chat_mensajes ADD COLUMN idempotency_key text
    CHECK (idempotency_key IS NULL OR length(idempotency_key) BETWEEN 1 AND 255);
CREATE UNIQUE INDEX chat_mensajes_clave_uk ON mensajeria.chat_mensajes (autor_id, idempotency_key) WHERE idempotency_key IS NOT NULL;

DROP FUNCTION mensajeria.chat_visibles(bigint, bigint, boolean, boolean, bigint[], bigint[], bigint);

-- Conversaciones que ve un usuario en la empresa (p_todas: cualquier empresa, solo el sistema):
--   - directa: es uno de sus dos miembros;
--   - orden con miembro «unido» (despacho): mientras su ordenes.ver cubra la orden (global, su zona
--     o su cuadrilla: p_ord_*);
--   - cuadrilla: es técnico activo de ella, o chat.supervisar la cubre (global, la cuadrilla, o una
--     zona que la cuadrilla atiende; una cuadrilla sin zonas no la cubre el alcance por zona);
--   - orden: es el técnico asignado, o chat.supervisar cubre la orden (global, su zona o cuadrilla).
-- Las directas nunca se ven por alcance. p_solo limita a una conversación. Lee agenda y campo (I2)
-- sin copiar nada: ver .claude/integration/i1.md («Chat»).
-- +goose StatementBegin
CREATE FUNCTION mensajeria.chat_visibles(p_usuario bigint, p_empresa bigint, p_todas boolean, p_sup_global boolean,
    p_sup_zonas bigint[], p_sup_cuadrillas bigint[], p_ord_global boolean, p_ord_zonas bigint[], p_ord_cuadrillas bigint[],
    p_solo bigint)
RETURNS SETOF bigint LANGUAGE sql STABLE AS $$
    SELECT c.id FROM mensajeria.chat_conversaciones c
    WHERE (p_todas OR c.empresa_id IS NOT DISTINCT FROM p_empresa)
      AND (p_solo IS NULL OR c.id = p_solo)
      AND (
        (c.tipo = 'directa' AND EXISTS (
            SELECT 1 FROM mensajeria.chat_miembros m WHERE m.conversacion_id = c.id AND m.usuario_id = p_usuario))
        OR (c.tipo = 'cuadrilla' AND (
            p_sup_global
            OR c.cuadrilla_id = ANY (p_sup_cuadrillas)
            OR EXISTS (SELECT 1 FROM agenda.tecnicos t WHERE t.usuario_id = p_usuario AND t.activo AND t.cuadrilla_id = c.cuadrilla_id)
            OR EXISTS (SELECT 1 FROM agenda.cuadrilla_zonas cz WHERE cz.cuadrilla_id = c.cuadrilla_id AND cz.zona_id = ANY (p_sup_zonas))))
        OR (c.tipo = 'orden' AND EXISTS (
            SELECT 1 FROM campo.ordenes o WHERE o.id = c.orden_id AND (
                o.tecnico_id = p_usuario OR p_sup_global
                OR o.zona_id = ANY (p_sup_zonas) OR o.cuadrilla_id = ANY (p_sup_cuadrillas)
                OR (EXISTS (SELECT 1 FROM mensajeria.chat_miembros m
                            WHERE m.conversacion_id = c.id AND m.usuario_id = p_usuario AND m.origen = 'unido')
                    AND (p_ord_global OR o.zona_id = ANY (p_ord_zonas) OR o.cuadrilla_id = ANY (p_ord_cuadrillas))))))
      )
$$;
-- +goose StatementEnd

-- +goose Down
DROP FUNCTION mensajeria.chat_visibles(bigint, bigint, boolean, boolean, bigint[], bigint[], boolean, bigint[], bigint[], bigint);
-- +goose StatementBegin
CREATE FUNCTION mensajeria.chat_visibles(p_usuario bigint, p_empresa bigint, p_todas boolean, p_sup_global boolean,
    p_sup_zonas bigint[], p_sup_cuadrillas bigint[], p_solo bigint)
RETURNS SETOF bigint LANGUAGE sql STABLE AS $$
    SELECT c.id FROM mensajeria.chat_conversaciones c
    WHERE (p_todas OR c.empresa_id IS NOT DISTINCT FROM p_empresa)
      AND (p_solo IS NULL OR c.id = p_solo)
      AND (
        EXISTS (SELECT 1 FROM mensajeria.chat_miembros m WHERE m.conversacion_id = c.id AND m.usuario_id = p_usuario)
        OR (c.tipo = 'cuadrilla' AND (
            p_sup_global
            OR c.cuadrilla_id = ANY (p_sup_cuadrillas)
            OR EXISTS (SELECT 1 FROM agenda.tecnicos t WHERE t.usuario_id = p_usuario AND t.activo AND t.cuadrilla_id = c.cuadrilla_id)
            OR EXISTS (SELECT 1 FROM agenda.cuadrilla_zonas cz WHERE cz.cuadrilla_id = c.cuadrilla_id AND cz.zona_id = ANY (p_sup_zonas))))
        OR (c.tipo = 'orden' AND EXISTS (
            SELECT 1 FROM campo.ordenes o WHERE o.id = c.orden_id AND (
                o.tecnico_id = p_usuario OR p_sup_global
                OR o.zona_id = ANY (p_sup_zonas) OR o.cuadrilla_id = ANY (p_sup_cuadrillas))))
      )
$$;
-- +goose StatementEnd
DROP INDEX mensajeria.chat_mensajes_clave_uk;
ALTER TABLE mensajeria.chat_mensajes DROP COLUMN idempotency_key;
ALTER TABLE mensajeria.chat_mensajes DROP CONSTRAINT chat_mensajes_hash_lapida_ck;
UPDATE mensajeria.chat_mensajes SET hash_contenido = '\x00' WHERE hash_contenido IS NULL;
ALTER TABLE mensajeria.chat_mensajes ALTER COLUMN hash_contenido SET NOT NULL;

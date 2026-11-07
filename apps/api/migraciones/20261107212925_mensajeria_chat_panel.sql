-- +goose Up
-- Chat del equipo para el panel (contratos/chat.md §2.2 y §3.2):
--
-- 1. Menciones: a quién menciona cada mensaje (los ids validados al enviar). Se borran al eliminar
--    el mensaje (la lápida no guarda nada del contenido). Con seq para contar las no leídas.
-- 2. Miembros «agregado»: la supervisión (chat.supervisar con alcance sobre la orden) agrega a
--    alguien al hilo de una orden. Lo ve sin depender de su ordenes.ver (chat_visibles).
CREATE TABLE mensajeria.chat_menciones (
    mensaje_id      uuid        NOT NULL REFERENCES mensajeria.chat_mensajes (id) ON DELETE CASCADE,
    usuario_id      bigint      NOT NULL REFERENCES nucleo.usuarios (id),
    conversacion_id bigint      NOT NULL REFERENCES mensajeria.chat_conversaciones (id) ON DELETE CASCADE,
    empresa_id      bigint,
    seq             bigint      NOT NULL CHECK (seq > 0),
    created_at      timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (mensaje_id, usuario_id)
);
-- Menciones sin leer de un usuario por conversación (seq > su lectura).
CREATE INDEX chat_menciones_usuario_idx ON mensajeria.chat_menciones (usuario_id, conversacion_id, seq);

ALTER TABLE mensajeria.chat_miembros DROP CONSTRAINT chat_miembros_origen_check;
ALTER TABLE mensajeria.chat_miembros ADD CONSTRAINT chat_miembros_origen_check CHECK (origen IN ('directa', 'unido', 'agregado'));
ALTER TABLE mensajeria.chat_miembros ADD COLUMN agregado_por bigint REFERENCES nucleo.usuarios (id);
ALTER TABLE mensajeria.chat_miembros ADD CONSTRAINT chat_miembros_agregado_por_ck CHECK ((origen = 'agregado') = (agregado_por IS NOT NULL));

DROP FUNCTION mensajeria.chat_visibles(bigint, bigint, boolean, boolean, bigint[], bigint[], boolean, bigint[], bigint[], bigint);

-- Igual que en 20261105181408 y, además, el miembro «agregado» de un hilo de orden lo ve.
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
        OR (c.tipo = 'orden' AND (
            EXISTS (SELECT 1 FROM mensajeria.chat_miembros m
                    WHERE m.conversacion_id = c.id AND m.usuario_id = p_usuario AND m.origen = 'agregado')
            OR EXISTS (
            SELECT 1 FROM campo.ordenes o WHERE o.id = c.orden_id AND (
                o.tecnico_id = p_usuario OR p_sup_global
                OR o.zona_id = ANY (p_sup_zonas) OR o.cuadrilla_id = ANY (p_sup_cuadrillas)
                OR (EXISTS (SELECT 1 FROM mensajeria.chat_miembros m
                            WHERE m.conversacion_id = c.id AND m.usuario_id = p_usuario AND m.origen = 'unido')
                    AND (p_ord_global OR o.zona_id = ANY (p_ord_zonas) OR o.cuadrilla_id = ANY (p_ord_cuadrillas)))))))
      )
$$;
-- +goose StatementEnd

-- +goose Down
DROP FUNCTION mensajeria.chat_visibles(bigint, bigint, boolean, boolean, bigint[], bigint[], boolean, bigint[], bigint[], bigint);
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
DELETE FROM mensajeria.chat_miembros WHERE origen = 'agregado';
ALTER TABLE mensajeria.chat_miembros DROP CONSTRAINT chat_miembros_agregado_por_ck;
ALTER TABLE mensajeria.chat_miembros DROP COLUMN agregado_por;
ALTER TABLE mensajeria.chat_miembros DROP CONSTRAINT chat_miembros_origen_check;
ALTER TABLE mensajeria.chat_miembros ADD CONSTRAINT chat_miembros_origen_check CHECK (origen IN ('directa', 'unido'));
DROP TABLE mensajeria.chat_menciones;

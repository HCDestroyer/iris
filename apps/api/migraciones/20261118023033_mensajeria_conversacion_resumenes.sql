-- +goose Up
-- El resumen del asistente queda guardado en la conversación (contratos/conversaciones.md §16.4):
-- quién lo pidió, cuándo, con qué modelo y hasta qué mensaje cubre. Se ve sin volver a gastar
-- tokens, se regenera a pedido (el anterior deja de ser el vigente) y está «desactualizado» cuando
-- llegaron mensajes después de hasta_seq. El texto vive solo aquí: nunca en eventos ni auditoría.
CREATE TABLE mensajeria.conversacion_resumenes (
    id               uuid PRIMARY KEY,
    empresa_id       bigint,
    conversacion_id  bigint      NOT NULL REFERENCES mensajeria.conversaciones (id),
    texto            text        NOT NULL CHECK (char_length(texto) BETWEEN 1 AND 3000),
    pedido_por       bigint REFERENCES nucleo.usuarios (id),
    proveedor        text        NOT NULL,
    modelo           text        NOT NULL,
    hasta_mensaje_id uuid        NOT NULL,
    hasta_seq        bigint      NOT NULL,
    tokens_entrada   bigint      NOT NULL DEFAULT 0,
    tokens_salida    bigint      NOT NULL DEFAULT 0,
    vigente          boolean     NOT NULL DEFAULT true,
    generado_en      timestamptz NOT NULL,
    created_at       timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX conversacion_resumenes_vigente_uk ON mensajeria.conversacion_resumenes (conversacion_id) WHERE vigente;
CREATE INDEX conversacion_resumenes_conversacion_idx ON mensajeria.conversacion_resumenes (conversacion_id, generado_en DESC);

-- +goose Down
DROP TABLE mensajeria.conversacion_resumenes;

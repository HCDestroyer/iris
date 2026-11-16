-- +goose Up
-- El asistente de IA agenda una visita técnica (contratos/conversaciones.md §17.10).

-- Ajustes de visitas por empresa (tipos permitidos, bloques, anticipación, límites…). NULL = los de fábrica.
ALTER TABLE mensajeria.asistente_ajustes ADD COLUMN visitas jsonb;

-- Ventanas que el asistente le ofreció al cliente: agendar_visita solo acepta una de estas, de su
-- conversación y vigente (el cliente contesta en otro turno).
CREATE TABLE mensajeria.asistente_ofertas_visita (
    id              bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    empresa_id      bigint,
    conversacion_id bigint      NOT NULL REFERENCES mensajeria.conversaciones (id),
    cliente_id      bigint      NOT NULL,
    servicio_id     bigint      NOT NULL,
    zona_id         bigint      NOT NULL,
    tipo            text        NOT NULL,
    clave           text        NOT NULL CHECK (clave ~ '^V[1-9]$'),
    inicio          timestamptz NOT NULL,
    fin             timestamptz NOT NULL,
    motivo          text        NOT NULL DEFAULT '',
    ofrecida_en     timestamptz NOT NULL,
    vence_en        timestamptz NOT NULL,
    UNIQUE (conversacion_id, clave)
);

-- +goose Down
DROP TABLE mensajeria.asistente_ofertas_visita;
ALTER TABLE mensajeria.asistente_ajustes DROP COLUMN visitas;

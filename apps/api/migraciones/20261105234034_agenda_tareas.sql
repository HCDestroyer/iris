-- +goose Up
-- Tareas propias del técnico (I2): algo suyo que no es una orden (recoger material, revisar una
-- caja…), con hora y duración, opcionalmente ligado a una orden o a un cliente de Argos. Se crean
-- desde la app de campo sin señal (id = uuid v7 del teléfono, el de la operación del lote) o desde
-- la API. Nunca se borran: completar y anular son acciones explícitas (borrado lógico).
CREATE TABLE agenda.tareas (
    id             uuid        PRIMARY KEY,
    empresa_id     bigint,
    tecnico_id     bigint      NOT NULL REFERENCES nucleo.usuarios (id),
    creada_por     bigint      REFERENCES nucleo.usuarios (id),
    que            text        NOT NULL CHECK (char_length(que) BETWEEN 1 AND 200),
    inicio         timestamptz NOT NULL,
    duracion_min   integer     NOT NULL CHECK (duracion_min BETWEEN 5 AND 480),
    fin            timestamptz NOT NULL,
    orden_id       bigint      REFERENCES campo.ordenes (id),
    cliente_id     bigint,
    origen         text        NOT NULL DEFAULT 'panel' CHECK (origen IN ('panel', 'campo', 'api')),
    estado         text        NOT NULL DEFAULT 'pendiente' CHECK (estado IN ('pendiente', 'completada', 'anulada')),
    completada_en  timestamptz,
    completada_por bigint      REFERENCES nucleo.usuarios (id),
    anulada_en     timestamptz,
    anulada_por    bigint      REFERENCES nucleo.usuarios (id),
    motivo         text        CHECK (motivo IS NULL OR char_length(motivo) <= 500),
    version        integer     NOT NULL DEFAULT 1,
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now(),
    CHECK (fin = inicio + make_interval(mins => duracion_min)),
    CHECK ((estado = 'completada') = (completada_en IS NOT NULL)),
    CHECK ((estado = 'anulada') = (anulada_en IS NOT NULL))
);
CREATE INDEX tareas_tecnico_idx ON agenda.tareas (tecnico_id, inicio) WHERE estado <> 'anulada';
CREATE INDEX tareas_empresa_idx ON agenda.tareas (empresa_id, inicio, id);

-- +goose Down
DROP TABLE agenda.tareas;

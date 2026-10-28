-- +goose Up
-- Cola de pendientes hacia Argos (contratos/argosisp.md §0 y §3; CLAUDE.md §4): cada escritura
-- de Iris en Argos queda aquí hasta su 2xx, con su Idempotency-Key del contrato
-- (iris:<operacion>:<id>). Reintentos a 1 min, 5 min, 30 min, 2 h y 12 h; después «agotado» y
-- alerta. Un 4xx de negocio no se reintenta («fallido_negocio», con el detail de Argos).
CREATE TABLE integracion.pendientes (
    id              bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    empresa_id      bigint,
    sistema         text        NOT NULL DEFAULT 'argosisp' CHECK (sistema IN ('argosisp', 'horus')),
    operacion       text        NOT NULL CHECK (operacion ~ '^[a-z][a-z_]{0,39}$'),
    idempotency_key text        NOT NULL UNIQUE,
    entidad         text        NOT NULL,                          -- 'orden', 'prospecto'
    entidad_id      text        NOT NULL,
    metodo          text        NOT NULL CHECK (metodo IN ('POST', 'PUT', 'PATCH', 'DELETE')),
    ruta            text        NOT NULL,                          -- bajo /api/v1 del otro sistema
    payload         jsonb       NOT NULL DEFAULT '{}'::jsonb,      -- cuerpo de la llamada
    depende_de      bigint[]    NOT NULL DEFAULT '{}',             -- espera a que estos estén «hecho»
    -- La orden no traía un dato obligatorio (servicio_id, unidad_id…): nace fallido_negocio y no
    -- se puede reintentar (habría que corregir la orden y volver a cerrarla).
    datos_incompletos boolean   NOT NULL DEFAULT false,
    estado          text        NOT NULL DEFAULT 'pendiente'
                                CHECK (estado IN ('pendiente', 'en_curso', 'hecho', 'fallido_negocio', 'agotado')),
    intentos        integer     NOT NULL DEFAULT 0,
    proximo_intento timestamptz NOT NULL DEFAULT now(),
    tomado_en       timestamptz,                                   -- en_curso desde (se reclama si el worker cae)
    ultimo_error    text,
    ultimo_status   integer,
    hecho_en        timestamptz,
    evento_id       uuid,                                          -- evento del outbox que lo originó
    version         integer     NOT NULL DEFAULT 1,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX pendientes_vencidos_idx ON integracion.pendientes (proximo_intento) WHERE estado IN ('pendiente', 'en_curso');
CREATE INDEX pendientes_entidad_idx ON integracion.pendientes (entidad, entidad_id);
CREATE INDEX pendientes_empresa_idx ON integracion.pendientes (empresa_id, id);

-- +goose Down
DROP TABLE integracion.pendientes;

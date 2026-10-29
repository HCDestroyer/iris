-- +goose Up
-- Encuesta de satisfacción (Fase 5, I2): una por ticket (al pasar a resuelto) y una por orden
-- (instalación o visita cerrada con éxito). El cliente entra por un enlace con un token de un
-- solo uso que vence a los 7 días. En la base solo queda su SHA-256 (token_hash) y una semilla
-- aleatoria: el token es HMAC(clave derivada de IRIS_CLAVE_CIFRADO, semilla), así soporte puede
-- volver a ver el enlace y el worker armar el correo sin que el token esté en la base. Sin la
-- clave (semilla null) el token es aleatorio y no se puede volver a mostrar.
CREATE TABLE soporte.encuestas (
    id            bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    empresa_id    bigint,
    entidad       text        NOT NULL CHECK (entidad IN ('ticket', 'orden')),
    ticket_id     bigint      REFERENCES soporte.tickets (id),
    orden_id      bigint      REFERENCES campo.ordenes (id),
    numero        text        NOT NULL,                 -- del ticket o la orden: lo que ve el cliente
    -- Copia al crear (alcance y filtros de los reportes): zona, cuadrilla, usuario (técnico de la
    -- orden o responsable del ticket) y categoría del ticket.
    zona_id       bigint,
    cuadrilla_id  bigint,
    usuario_id    bigint,
    categoria_id  bigint,
    token_hash    bytea       NOT NULL UNIQUE,
    semilla       bytea,
    canal         text        NOT NULL CHECK (canal IN ('correo', 'enlace')),
    expira_en     timestamptz NOT NULL,
    estado        text        NOT NULL DEFAULT 'pendiente' CHECK (estado IN ('pendiente', 'respondida')),
    calificacion  smallint    CHECK (calificacion BETWEEN 1 AND 5),
    comentario    text        CHECK (char_length(comentario) <= 1000),
    respondida_en timestamptz,
    version       integer     NOT NULL DEFAULT 1,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),
    CHECK ((entidad = 'ticket') = (ticket_id IS NOT NULL)),
    CHECK ((entidad = 'orden') = (orden_id IS NOT NULL)),
    CHECK ((estado = 'respondida') = (respondida_en IS NOT NULL)),
    CHECK ((estado = 'respondida') = (calificacion IS NOT NULL)),
    CHECK (estado = 'respondida' OR comentario IS NULL)
);
CREATE UNIQUE INDEX encuestas_ticket_uq ON soporte.encuestas (ticket_id) WHERE ticket_id IS NOT NULL;
CREATE UNIQUE INDEX encuestas_orden_uq ON soporte.encuestas (orden_id) WHERE orden_id IS NOT NULL;
CREATE INDEX encuestas_creada_idx ON soporte.encuestas (created_at, id);

-- El correo de la encuesta sale por el worker de correo existente: un saliente es la respuesta
-- de un mensaje del ticket o la encuesta (uno de los dos).
ALTER TABLE soporte.correos_salientes
    ALTER COLUMN mensaje_id DROP NOT NULL,
    ADD COLUMN encuesta_id bigint UNIQUE REFERENCES soporte.encuestas (id),
    ADD CONSTRAINT correos_salientes_origen_ck CHECK (num_nonnulls(mensaje_id, encuesta_id) = 1);

-- +goose Down
DELETE FROM soporte.correos_salientes WHERE encuesta_id IS NOT NULL;
ALTER TABLE soporte.correos_salientes
    DROP CONSTRAINT correos_salientes_origen_ck,
    DROP COLUMN encuesta_id,
    ALTER COLUMN mensaje_id SET NOT NULL;
DROP TABLE soporte.encuestas;

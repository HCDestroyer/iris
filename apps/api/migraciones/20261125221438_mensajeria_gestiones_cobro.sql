-- +goose Up
-- Gestiones de cobro registradas en Iris y enviadas a Argos (contrato Iris ↔ ArgosISP 0.13.0 §3.12,
-- pedido B31). Solo informativas: nunca cambian estados, cortes ni promesas en Argos. Sin texto libre,
-- montos ni teléfonos.
CREATE TABLE mensajeria.gestiones_cobro (
    -- gestion_id: UUID v7 que genera Iris; la clave hacia Argos es iris:gestion:<id>.
    id               uuid        PRIMARY KEY,
    empresa_id       bigint      NOT NULL,
    conversacion_id  bigint      NOT NULL REFERENCES mensajeria.conversaciones (id),
    cliente_id       bigint      NOT NULL CHECK (cliente_id > 0),
    servicio_id      bigint      CHECK (servicio_id IS NULL OR servicio_id > 0),
    -- El contacto de Argos que habló (X-Iris-Contacto); null = el titular o sin decir.
    contacto_id      bigint      CHECK (contacto_id IS NULL OR contacto_id > 0),
    tipo             text        NOT NULL CHECK (tipo IN ('llamada', 'mensaje', 'visita', 'promesa')),
    resultado        text        NOT NULL CHECK (resultado IN ('contesto', 'no_contesto', 'numero_equivocado', 'promete_pagar',
                                                               'rechaza', 'pago_informado', 'visita_sin_contacto')),
    ocurrido_en      timestamptz NOT NULL,
    origen_tipo      text        NOT NULL CHECK (origen_tipo IN ('conversacion', 'orden', 'llamada')),
    origen_id        text        NOT NULL CHECK (origen_id ~ '^[A-Za-z0-9._:-]{1,128}$'),
    -- Quién la registró: una persona (usuario_id) o el asistente de IA (asistente = true).
    usuario_id       bigint      REFERENCES nucleo.usuarios (id),
    asistente        boolean     NOT NULL DEFAULT false,
    -- Idempotency-Key de quien la pidió en Iris (por usuario o, el asistente, por conversación).
    idempotency_key  text,
    hash_peticion    bytea,
    pendiente_id     bigint      REFERENCES integracion.pendientes (id),
    created_at       timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT gestiones_cobro_actor_ck CHECK ((usuario_id IS NULL) = asistente)
);
CREATE INDEX gestiones_cobro_conversacion_idx ON mensajeria.gestiones_cobro (conversacion_id, created_at DESC);
CREATE UNIQUE INDEX gestiones_cobro_clave_uq ON mensajeria.gestiones_cobro (conversacion_id, coalesce(usuario_id, 0), idempotency_key)
    WHERE idempotency_key IS NOT NULL;

-- Pendientes hacia Argos: el asistente de IA también actúa (X-Iris-Actor: asistente_ia, §7.1) y quien
-- habló puede ser un contacto del cliente (X-Iris-Contacto: <contacto_id>, §7.1 y §3.12).
ALTER TABLE integracion.pendientes DROP CONSTRAINT pendientes_actor_check;
ALTER TABLE integracion.pendientes ADD CONSTRAINT pendientes_actor_check
    CHECK (actor IS NULL OR actor ~ '^(asistente_ia|usuario:[A-Za-z0-9_-]{1,64})$');
ALTER TABLE integracion.pendientes
    ADD COLUMN contacto text CHECK (contacto IS NULL OR contacto ~ '^(titular|[1-9][0-9]{0,17})$');

-- +goose Down
DROP TABLE mensajeria.gestiones_cobro;
ALTER TABLE integracion.pendientes DROP COLUMN contacto;
UPDATE integracion.pendientes SET actor = NULL WHERE actor = 'asistente_ia';
ALTER TABLE integracion.pendientes DROP CONSTRAINT pendientes_actor_check;
ALTER TABLE integracion.pendientes ADD CONSTRAINT pendientes_actor_check
    CHECK (actor IS NULL OR actor ~ '^usuario:[A-Za-z0-9_-]{1,64}$');

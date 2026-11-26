-- +goose Up
-- Contactos en la atención (contrato argosisp.md §3.5 y §3.8, pedidos B22 y B25).

-- X-Iris-Contacto con que se registró la boleta (§7.1, 0.10.1): 'titular' o el contacto_id de
-- Argos. Se guarda al registrar para que los reintentos (y la reconexión ligada a la boleta) manden
-- la misma cabecera (Argos la incluye en la huella de idempotencia). null: anterior (titular).
ALTER TABLE mensajeria.boletas ADD COLUMN contacto_argos text
    CHECK (contacto_argos IS NULL OR contacto_argos = 'titular' OR contacto_argos ~ '^[1-9][0-9]{0,17}$');

-- Elegir entre varios clientes con el mismo número (§3.5, ajustes g, ac y ah de §10.5): cada intento
-- de referencia (el código del servicio) queda aquí, sin la referencia. 3 por conversación y 5 por
-- número y empresa al día; acertar asocia la conversación, pero no la verifica.
CREATE TABLE mensajeria.elecciones_cliente (
    id              bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    empresa_id      bigint,
    conversacion_id bigint      NOT NULL REFERENCES mensajeria.conversaciones (id),
    numero          text        NOT NULL,
    acierto         boolean     NOT NULL,
    cliente_id      bigint,
    -- usuario_id: la persona que escribió la referencia; null = el asistente de IA.
    usuario_id      bigint REFERENCES nucleo.usuarios (id),
    creado_en       timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX elecciones_cliente_conv_idx ON mensajeria.elecciones_cliente (conversacion_id);
CREATE INDEX elecciones_cliente_numero_idx ON mensajeria.elecciones_cliente (coalesce(empresa_id, 0), numero, creado_en);

-- Números sin cliente que una persona (o el asistente) ya buscó en los contactos de Argos
-- (GET /contactos/buscar): uno por empresa y número, para no volver a buscarlo antes de 24 h (Argos
-- limita buscar a 20 por minuto por actor). Solo los dígitos, sin quién ni qué respondió.
CREATE TABLE mensajeria.contactos_buscados (
    empresa    bigint      NOT NULL DEFAULT 0,
    digitos    text        NOT NULL CHECK (digitos ~ '^[0-9]{8,15}$'),
    buscado_en timestamptz NOT NULL,
    PRIMARY KEY (empresa, digitos)
);

-- El contacto de Argos con que se asoció la conversación (§3.5): su rol solo sale de ese contacto. Si
-- Argos lo quita (o deja de devolverlo), la conversación pierde la verificación del número.
ALTER TABLE mensajeria.conversaciones ADD COLUMN contacto_argos_id bigint CHECK (contacto_argos_id IS NULL OR contacto_argos_id > 0);
CREATE INDEX conversaciones_contacto_argos_idx ON mensajeria.conversaciones (contacto_argos_id) WHERE contacto_argos_id IS NOT NULL;

-- +goose Down
ALTER TABLE mensajeria.conversaciones DROP COLUMN contacto_argos_id;
DROP TABLE mensajeria.contactos_buscados;
DROP TABLE mensajeria.elecciones_cliente;
ALTER TABLE mensajeria.boletas DROP COLUMN contacto_argos;

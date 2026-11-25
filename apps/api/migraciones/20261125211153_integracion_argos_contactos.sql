-- +goose Up
-- Caché de contactos de los clientes de Argos (contrato argosisp.md 0.10.0 §3.5, A42; 0.10.1 §3.8,
-- A46; pedidos B22 y B25). Argos es el dueño: Iris solo guarda lo que leyó de
-- GET /clientes/:id/contactos (o de GET /contactos/buscar), con la hora de lectura, y la refresca
-- con el webhook cliente.contactos_cambiados. Nunca se edita a mano: lo que cambia un contacto se
-- pide a Argos.
CREATE TABLE integracion.argos_contactos (
    contacto_id        bigint      PRIMARY KEY CHECK (contacto_id > 0),
    cliente_id         bigint      NOT NULL CHECK (cliente_id > 0),
    empresa_id         bigint,
    nombre             text        NOT NULL DEFAULT '' CHECK (char_length(nombre) <= 200),
    relacion           text        NOT NULL CHECK (relacion IN ('titular', 'familiar', 'inquilino', 'empleado', 'otro')),
    es_titular         boolean     NOT NULL DEFAULT false,
    -- servicios: los ids de servicio del cliente a los que aplica; null = todos (§3.5).
    servicios          bigint[],
    consultar          boolean     NOT NULL DEFAULT true,
    reportar           boolean     NOT NULL DEFAULT true,
    pagar              boolean     NOT NULL DEFAULT true,
    avisos             boolean     NOT NULL DEFAULT true,
    -- persona_cliente_id: otro cliente de la misma empresa a cargo de este servicio (§3.8, A46).
    persona_cliente_id bigint CHECK (persona_cliente_id IS NULL OR persona_cliente_id > 0),
    version            integer     NOT NULL DEFAULT 1 CHECK (version > 0),
    sincronizado_en    timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX argos_contactos_cliente_idx ON integracion.argos_contactos (cliente_id);
CREATE INDEX argos_contactos_persona_idx ON integracion.argos_contactos (persona_cliente_id) WHERE persona_cliente_id IS NOT NULL;

-- Teléfonos de cada contacto (E.164). digitos: solo los dígitos, para buscar el número de una
-- conversación con la misma regla que la instantánea de clientes.
CREATE TABLE integracion.argos_contacto_telefonos (
    contacto_id bigint  NOT NULL REFERENCES integracion.argos_contactos (contacto_id) ON DELETE CASCADE,
    numero      text    NOT NULL CHECK (numero ~ '^\+[1-9][0-9]{7,14}$'),
    digitos     text    NOT NULL,
    whatsapp    boolean NOT NULL DEFAULT false,
    principal   boolean NOT NULL DEFAULT false,
    PRIMARY KEY (contacto_id, numero)
);
CREATE INDEX argos_contacto_telefonos_digitos_idx ON integracion.argos_contacto_telefonos (digitos);

-- Clientes cuyos contactos Iris leyó completos (y cuándo). Sin fila: Iris no los conoce y los lee
-- de Argos cuando una persona los necesita.
CREATE TABLE integracion.argos_contactos_leidos (
    cliente_id      bigint      PRIMARY KEY CHECK (cliente_id > 0),
    sincronizado_en timestamptz NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE integracion.argos_contactos_leidos;
DROP TABLE integracion.argos_contacto_telefonos;
DROP TABLE integracion.argos_contactos;

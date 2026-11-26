-- +goose Up
-- Contactos de los clientes de Argos en la atención (contrato argosisp.md 0.10.0 §3.5, A42; 0.10.1
-- §3.8, A46; pedidos B22 y B25). La tabla integracion.argos_contactos la crea el portal (#283,
-- 20261126044435): aquí se agregan solo las columnas que usa la atención. Argos es el dueño: Iris solo
-- guarda lo que leyó de GET /clientes/:id/contactos y lo refresca con cliente.contactos_cambiados. Un
-- contacto quitado se marca (quitado), no se borra. Los permisos van en false por defecto: cada
-- escritura los pone explícitos con lo que dice Argos.
ALTER TABLE integracion.argos_contactos
    ADD COLUMN relacion text NOT NULL DEFAULT 'otro' CHECK (relacion IN ('titular', 'familiar', 'inquilino', 'empleado', 'otro')),
    ADD COLUMN es_titular boolean NOT NULL DEFAULT false,
    ADD COLUMN pagar boolean NOT NULL DEFAULT false,
    ADD COLUMN avisos boolean NOT NULL DEFAULT false,
    -- persona_cliente_id: otro cliente de la misma empresa a cargo de este servicio (§3.8, A46).
    ADD COLUMN persona_cliente_id bigint CHECK (persona_cliente_id IS NULL OR persona_cliente_id > 0);
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

-- Clientes cuyos contactos Iris leyó completos (y cuándo), también los que no tienen ninguno. Sin
-- fila: Iris no los conoce y los lee de Argos cuando una persona los necesita.
CREATE TABLE integracion.argos_contactos_leidos (
    cliente_id      bigint      PRIMARY KEY CHECK (cliente_id > 0),
    sincronizado_en timestamptz NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE integracion.argos_contactos_leidos;
DROP TABLE integracion.argos_contacto_telefonos;
DROP INDEX integracion.argos_contactos_persona_idx;
ALTER TABLE integracion.argos_contactos
    DROP COLUMN persona_cliente_id,
    DROP COLUMN avisos,
    DROP COLUMN pagar,
    DROP COLUMN es_titular,
    DROP COLUMN relacion;

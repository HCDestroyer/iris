-- +goose Up
-- Contrato con Argos 0.8.0 §7.8 (B5): las cuentas bancarias de la empresa son de Argos. Iris guarda
-- solo una instantánea de lectura con los campos que usa; nunca la edita. Se llena con la primera
-- carga y la relectura periódica (GET /cuentas-bancarias, activas e inactivas), con el webhook
-- cuenta_bancaria.cambiada (GET /cuentas-bancarias/:id) y con la relectura de la cuenta antes de
-- registrar una boleta. Nunca cuenta_id, cuenta_codigo, version ni fechas de Argos (contables).
-- titular y tipo: null con un Argos anterior a 0.8.0 o en una cuenta vieja que nadie editó; una
-- cuenta sin titular no se le ofrece al cliente (§10.2, punto 14).
CREATE TABLE integracion.argos_cuentas_bancarias (
    id              bigint PRIMARY KEY,
    empresa_id      bigint      NOT NULL,
    banco_id        bigint,
    banco_nombre    text        NOT NULL DEFAULT '',
    numero          text        NOT NULL,
    alias           text        NOT NULL DEFAULT '',
    titular         text,
    tipo            text CHECK (tipo IN ('monetaria', 'ahorro')),
    moneda          text        NOT NULL DEFAULT '',
    activa          boolean     NOT NULL,
    sincronizado_en timestamptz NOT NULL,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX argos_cuentas_bancarias_empresa_idx ON integracion.argos_cuentas_bancarias (empresa_id);

-- Cuándo se leyó por última vez la lista completa de cuentas de una empresa (empresa_id = 0: la
-- lista sin filtro de la primera carga o del periódico, que trae todas las del alcance). Con ella se
-- sabe si la instantánea tiene más de 24 h aunque la empresa no tenga ninguna cuenta.
CREATE TABLE integracion.argos_cuentas_lecturas (
    empresa_id bigint PRIMARY KEY,
    leido_en   timestamptz NOT NULL
);

-- +goose Down
DROP TABLE integracion.argos_cuentas_lecturas;
DROP TABLE integracion.argos_cuentas_bancarias;

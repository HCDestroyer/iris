-- +goose Up
-- Multiempresa igual que Argos (CLAUDE.md, modelo-datos §nucleo): instantánea de las empresas de
-- Argos y membresía usuario ↔ empresa. La empresa activa de cada petición sale de aquí
-- (cabecera X-Iris-Empresa; ver internal/nucleo/identidad/empresa_activa.go).

-- id = id de la empresa en Argos (no es identity). La llena el conector (empresas del canje) o
-- un administrador (POST /empresas). Sin filas, Iris trabaja «sin conector» (empresa_id null).
CREATE TABLE nucleo.empresas (
    id              bigint PRIMARY KEY CHECK (id > 0),
    nombre          text        NOT NULL CHECK (nombre <> ''),
    nit             text        NOT NULL DEFAULT '',
    origen          text        NOT NULL DEFAULT 'manual' CHECK (origen IN ('argos', 'manual')),
    sincronizado_en timestamptz,
    version         integer     NOT NULL DEFAULT 1,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);

-- Empresas en las que un usuario (persona o integración, y sus tokens) puede operar. Quien tiene
-- el permiso empresas.todas (admin) opera en todas sin fila aquí.
CREATE TABLE nucleo.usuarios_empresas (
    usuario_id   bigint      NOT NULL REFERENCES nucleo.usuarios (id),
    empresa_id   bigint      NOT NULL REFERENCES nucleo.empresas (id),
    asignado_por bigint      REFERENCES nucleo.usuarios (id),
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (usuario_id, empresa_id)
);
CREATE INDEX usuarios_empresas_empresa_idx ON nucleo.usuarios_empresas (empresa_id);

-- +goose Down
DROP TABLE nucleo.usuarios_empresas;
DROP TABLE nucleo.empresas;

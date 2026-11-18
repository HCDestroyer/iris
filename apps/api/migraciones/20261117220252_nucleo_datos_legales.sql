-- +goose Up
-- Datos legales del titular (contratos/legal.md): quién opera la instalación, para las páginas
-- públicas de privacidad, términos y eliminación de datos que Meta pide. Una fila por ámbito: hoy
-- solo la de la instalación (empresa_id null), que es la que sirven las páginas públicas; la
-- columna empresa_id queda para cuando Iris resuelva la empresa por dominio. Todo lo que hay aquí
-- es público por definición: nunca secretos.
CREATE TABLE nucleo.datos_legales (
    id                bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    empresa_id        bigint,
    razon_social      text        NOT NULL DEFAULT '' CHECK (char_length(razon_social) <= 200),
    nombre_comercial  text        NOT NULL DEFAULT '' CHECK (char_length(nombre_comercial) <= 120),
    correo_privacidad text        NOT NULL DEFAULT '' CHECK (char_length(correo_privacidad) <= 254),
    telefono          text        NOT NULL DEFAULT '' CHECK (char_length(telefono) <= 40),
    direccion         text        NOT NULL DEFAULT '' CHECK (char_length(direccion) <= 300),
    pais              text        NOT NULL DEFAULT '' CHECK (char_length(pais) <= 80),
    version           integer     NOT NULL DEFAULT 1,
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX datos_legales_ambito_uq ON nucleo.datos_legales ((coalesce(empresa_id, 0)));

-- +goose Down
DROP TABLE nucleo.datos_legales;

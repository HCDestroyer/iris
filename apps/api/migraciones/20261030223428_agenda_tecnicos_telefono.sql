-- +goose Up
-- Teléfono de contacto de la ficha del técnico (para despacho). Texto libre hasta 30 caracteres
-- (dígitos, espacios, +, -, paréntesis y puntos; lo valida el caso de uso). Vacío = sin teléfono.
ALTER TABLE agenda.tecnicos
    ADD COLUMN telefono text NOT NULL DEFAULT '' CHECK (length(telefono) <= 30);

-- +goose Down
ALTER TABLE agenda.tecnicos DROP COLUMN telefono;

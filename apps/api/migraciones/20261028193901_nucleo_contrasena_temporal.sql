-- +goose Up
-- Contraseña temporal: el reinicio de MFA por un administrador reemplaza la contraseña por una
-- temporal y obliga a cambiarla al entrar; hasta entonces la sesión solo puede cambiarla
-- (POST /api/v1/yo/contrasena), ver /yo o cerrar sesión.
ALTER TABLE nucleo.usuarios ADD COLUMN debe_cambiar_contrasena boolean NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE nucleo.usuarios DROP COLUMN debe_cambiar_contrasena;

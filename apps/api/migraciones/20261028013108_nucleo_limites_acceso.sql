-- +goose Up
-- Límites contra fuerza bruta (identidad).
-- Fallos de inicio de sesión por correo (normalizado), exista o no el usuario: así el bloqueo
-- no revela qué correos existen. Se borra la fila al entrar bien.
CREATE TABLE nucleo.intentos_login (
    correo          text        PRIMARY KEY,
    fallos          integer     NOT NULL DEFAULT 0,
    bloqueado_hasta timestamptz,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX intentos_login_updated_idx ON nucleo.intentos_login (updated_at);

-- Códigos MFA fallidos de una sesión pendiente; al llegar al máximo, la sesión se revoca.
ALTER TABLE nucleo.sesiones ADD COLUMN mfa_fallos integer NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE nucleo.sesiones DROP COLUMN mfa_fallos;
DROP TABLE nucleo.intentos_login;

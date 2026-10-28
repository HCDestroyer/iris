-- +goose Up
-- Conector Horus (contratos/horus.md): reutiliza integracion.conectores, codigos_conexion y
-- entrantes del conector Argos (sistema = 'horus' ya estaba permitido). Se agrega la URL
-- pública de cada conector, la que el panel usa para «Ver en Argos» / «Ver en Horus»: null =
-- la URL base del conector (la de su API). Un administrador la cambia si la interfaz del otro
-- sistema vive en otro origen.
ALTER TABLE integracion.conectores ADD COLUMN url_publica text;

-- +goose Down
ALTER TABLE integracion.conectores DROP COLUMN url_publica;

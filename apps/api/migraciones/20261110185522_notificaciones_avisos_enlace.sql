-- +goose Up
-- El enlace de un aviso del panel es una ruta interna (revisión de seguridad de la #155, B3): solo
-- caracteres de ruta y nunca «//host» (un enlace relativo al protocolo saldría del panel).
ALTER TABLE notificaciones.avisos DROP CONSTRAINT avisos_enlace_check;
ALTER TABLE notificaciones.avisos ADD CONSTRAINT avisos_enlace_check
    CHECK (enlace IS NULL OR (enlace ~ '^/[A-Za-z0-9_\-/?=&#.]*$' AND enlace NOT LIKE '//%' AND char_length(enlace) <= 300));

-- +goose Down
ALTER TABLE notificaciones.avisos DROP CONSTRAINT avisos_enlace_check;
ALTER TABLE notificaciones.avisos ADD CONSTRAINT avisos_enlace_check
    CHECK (enlace IS NULL OR (enlace LIKE '/%' AND char_length(enlace) <= 300));

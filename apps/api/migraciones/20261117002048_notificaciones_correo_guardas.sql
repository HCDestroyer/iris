-- +goose Up
-- Correo del sistema (contratos/correo-sistema.md §4), tras la revisión de seguridad:
-- sin cifrado solo en el ajuste global, y los rechazos de host cuentan para un tope por usuario
-- (un host que resuelve a una red interna no sirve de oráculo de DNS).
ALTER TABLE notificaciones.correo_ajustes
    ADD CONSTRAINT correo_ajustes_ninguna_solo_global CHECK (seguridad <> 'ninguna' OR empresa_id IS NULL);

ALTER TABLE notificaciones.correo_pruebas DROP CONSTRAINT correo_pruebas_estado_check;
ALTER TABLE notificaciones.correo_pruebas
    ADD CONSTRAINT correo_pruebas_estado_check CHECK (estado IN ('en_curso', 'ok', 'error', 'rechazado'));

-- +goose Down
DELETE FROM notificaciones.correo_pruebas WHERE estado = 'rechazado';
ALTER TABLE notificaciones.correo_pruebas DROP CONSTRAINT correo_pruebas_estado_check;
ALTER TABLE notificaciones.correo_pruebas
    ADD CONSTRAINT correo_pruebas_estado_check CHECK (estado IN ('en_curso', 'ok', 'error'));
ALTER TABLE notificaciones.correo_ajustes DROP CONSTRAINT correo_ajustes_ninguna_solo_global;

-- +goose Up
-- Revisión de la PR de campo (I2):
--  - orden_equipos.validado: el equipo retirado coincide con el comodato del servicio en Argos
--    (false: sin conector o Argos no respondió; I1 lo trata como dato no verificado).
--  - firmas.invalidada_en: la firma de un intento anterior (fallida reprogramada o reabierta) ya
--    no cuenta para el cierre; capturarla de nuevo la reemplaza.
ALTER TABLE campo.orden_equipos ADD COLUMN validado boolean NOT NULL DEFAULT false;
ALTER TABLE campo.firmas ADD COLUMN invalidada_en timestamptz;

-- +goose Down
ALTER TABLE campo.firmas DROP COLUMN invalidada_en;
ALTER TABLE campo.orden_equipos DROP COLUMN validado;

-- +goose Up
-- Respuesta del primer 2xx de Argos (máx. 64 KiB, claves sensibles como "***") y cuerpos de
-- dependientes con marcadores ({"$iris_ref": {"dependencia": i, "campo": "id"}}) que se
-- resuelven una sola vez con la respuesta de su dependencia, cuando todas quedan hechas. Desde
-- ahí payload tiene el cuerpo final y no cambia (Argos exige los mismos bytes al reintentar).
ALTER TABLE integracion.pendientes
    ADD COLUMN respuesta    jsonb,
    ADD COLUMN plantilla    jsonb,                          -- el cuerpo con marcadores, tal como se encoló
    ADD COLUMN por_resolver boolean NOT NULL DEFAULT false; -- payload todavía es la plantilla

-- +goose Down
ALTER TABLE integracion.pendientes DROP COLUMN por_resolver, DROP COLUMN plantilla, DROP COLUMN respuesta;

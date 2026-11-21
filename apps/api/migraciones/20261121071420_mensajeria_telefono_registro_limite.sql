-- +goose Up
-- E4c, revisión de seguridad de #252 (fail2ban): «Probar registro» no repite una contraseña que la
-- Issabel ya rechazó (hasta que se escriba otra o pasen 30 min) y tiene tope por minuto, por extensión
-- y por centralita. `sip_rechazado_en`: la última vez que la Issabel rechazó la contraseña guardada
-- (en «Probar registro» o en una conexión del puente); se borra cuando una prueba pasa.
ALTER TABLE mensajeria.centralita_extensiones
    ADD COLUMN sip_rechazado_en timestamptz,
    ADD COLUMN registro_pruebas integer NOT NULL DEFAULT 0,
    ADD COLUMN registro_pruebas_desde timestamptz;

ALTER TABLE mensajeria.centralitas
    ADD COLUMN registro_pruebas integer NOT NULL DEFAULT 0,
    ADD COLUMN registro_pruebas_desde timestamptz;

-- +goose Down
ALTER TABLE mensajeria.centralitas
    DROP COLUMN registro_pruebas_desde,
    DROP COLUMN registro_pruebas;

ALTER TABLE mensajeria.centralita_extensiones
    DROP COLUMN registro_pruebas_desde,
    DROP COLUMN registro_pruebas,
    DROP COLUMN sip_rechazado_en;

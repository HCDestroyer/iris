-- +goose Up
-- E4c, re-verificación de seguridad de #252 (M1, fail2ban): las contraseñas que la Issabel rechaza
-- se cuentan por centralita en una ventana de 10 min, sumando «Probar registro» y el puente. Pasado el
-- tope no se prueba ni se abre el teléfono hasta que la ventana pase (429 `demasiados_rechazos`).
ALTER TABLE mensajeria.centralitas
    ADD COLUMN rechazos integer NOT NULL DEFAULT 0,
    ADD COLUMN rechazos_desde timestamptz;

-- +goose Down
ALTER TABLE mensajeria.centralitas
    DROP COLUMN rechazos_desde,
    DROP COLUMN rechazos;

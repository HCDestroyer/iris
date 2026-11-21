-- +goose Up
-- E4c, #252 B5 (fail2ban): un solo «Probar registro» a la vez por centralita. El REGISTER corre fuera
-- de la transacción, así que el turno se reserva en la fila (`registro_en_curso_hasta`) y se suelta al
-- guardar el resultado; si el proceso muere, vence solo.
ALTER TABLE mensajeria.centralitas
    ADD COLUMN registro_en_curso_hasta timestamptz;

-- +goose Down
ALTER TABLE mensajeria.centralitas
    DROP COLUMN registro_en_curso_hasta;

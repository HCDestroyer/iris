-- +goose Up
-- E4c, #252 B8 (fail2ban): con rechazos recientes en la centralita, /telefono/abrir reserva el turno
-- de REGISTER (`centralitas.registro_en_curso_hasta`) y el ticket guarda cuál (`turno_hasta`): la
-- conexión del puente con ese ticket ya lo tiene; otra tiene que reservarlo.
ALTER TABLE mensajeria.telefono_tickets
    ADD COLUMN turno_hasta timestamptz;

-- +goose Down
ALTER TABLE mensajeria.telefono_tickets
    DROP COLUMN turno_hasta;

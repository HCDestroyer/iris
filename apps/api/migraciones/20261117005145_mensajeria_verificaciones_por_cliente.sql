-- +goose Up
-- El tope del desafío de identidad pasa a contarse por cliente (y empresa), sumando todas sus
-- conversaciones y canales (contratos/conversaciones.md §17.8.2). Cada intento se reserva
-- («en_curso») antes de llamar a Argos, para que dos conversaciones a la vez no se pasen del tope.
ALTER TABLE mensajeria.asistente_verificaciones DROP CONSTRAINT asistente_verificaciones_resultado_check;
ALTER TABLE mensajeria.asistente_verificaciones
    ADD CONSTRAINT asistente_verificaciones_resultado_check
    CHECK (resultado IN ('en_curso', 'coincide', 'no_coincide', 'no_disponible', 'error'));
CREATE INDEX asistente_verificaciones_cliente_idx
    ON mensajeria.asistente_verificaciones ((coalesce(empresa_id, 0)), cliente_id, creada_en);

-- +goose Down
DROP INDEX mensajeria.asistente_verificaciones_cliente_idx;
UPDATE mensajeria.asistente_verificaciones SET resultado = 'error' WHERE resultado = 'en_curso';
ALTER TABLE mensajeria.asistente_verificaciones DROP CONSTRAINT asistente_verificaciones_resultado_check;
ALTER TABLE mensajeria.asistente_verificaciones
    ADD CONSTRAINT asistente_verificaciones_resultado_check
    CHECK (resultado IN ('coincide', 'no_coincide', 'no_disponible', 'error'));

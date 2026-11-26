-- +goose Up
-- Verificación de cambios e intentos de servidor a servidor (contrato con ArgosISP 0.14.0 §3.10).
--
-- enviado_a_conector: el conector que respondió el 2xx de la acción (Argos verifica solo lo que
-- Iris le mandó a él).
ALTER TABLE integracion.pendientes ADD COLUMN enviado_a_conector bigint REFERENCES integracion.conectores (id);

-- verificaciones_intento: cada consulta de GET …/intentos-recuperacion/verificacion, para el tope
-- (60 por minuto por conector y 20 por clave al día). Se borran las de más de dos días.
CREATE TABLE integracion.verificaciones_intento (
    id            bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    conector_id   bigint      NOT NULL REFERENCES integracion.conectores (id),
    clave         text        NOT NULL CHECK (length(clave) <= 300),
    consultada_en timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX verificaciones_intento_conector_idx ON integracion.verificaciones_intento (conector_id, consultada_en);
CREATE INDEX verificaciones_intento_clave_idx ON integracion.verificaciones_intento (clave, consultada_en);

-- +goose Down
DROP TABLE IF EXISTS integracion.verificaciones_intento;
ALTER TABLE integracion.pendientes DROP COLUMN IF EXISTS enviado_a_conector;

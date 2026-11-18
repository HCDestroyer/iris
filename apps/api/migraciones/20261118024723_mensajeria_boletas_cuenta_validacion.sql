-- +goose Up
-- Si Argos validó la cuenta bancaria de la boleta al registrarla. NULL = boleta anterior a esta
-- columna. Una boleta «sin validar» (Argos no respondió o el conector estaba apagado) se revalida en
-- el despacho, antes del POST /comprobantes.
ALTER TABLE mensajeria.boletas
    ADD COLUMN cuenta_validacion text
        CHECK (cuenta_validacion IS NULL OR cuenta_validacion IN ('validada', 'sin_validar_argos_no_disponible', 'sin_validar_conector_apagado'));

-- +goose Down
ALTER TABLE mensajeria.boletas DROP COLUMN cuenta_validacion;

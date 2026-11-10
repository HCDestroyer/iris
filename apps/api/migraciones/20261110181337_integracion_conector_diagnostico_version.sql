-- +goose Up
-- Diagnóstico de la lectura de versión del conector (contrato Iris ↔ ArgosISP, «Versión y
-- compatibilidad»): el último resultado de integraciones.informar_contrato. version_estado:
-- ok | ruta_no_existe | sin_permiso | no_responde | error (null = nunca se consultó).
-- version_detalle: texto en español para el administrador (la URL base usada y el status HTTP;
-- nunca el token ni la respuesta cruda). version_consultada_en ya existía (0.5.0).
ALTER TABLE integracion.conectores
    ADD COLUMN version_estado  text CHECK (version_estado IN ('ok', 'ruta_no_existe', 'sin_permiso', 'no_responde', 'error')),
    ADD COLUMN version_detalle text CHECK (char_length(version_detalle) <= 500);

-- +goose Down
ALTER TABLE integracion.conectores DROP COLUMN version_estado, DROP COLUMN version_detalle;

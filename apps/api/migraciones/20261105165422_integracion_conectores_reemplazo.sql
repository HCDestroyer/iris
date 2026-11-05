-- +goose Up
-- Reconexión sin corte (contrato Iris ↔ ArgosISP 0.6.3 §1.3): al activar un conector que reemplaza al
-- activo, Argos sigue usando lo del anterior (su iris_pat_ y el secreto de sus webhooks) hasta la
-- primera llamada autenticada con el atk_ nuevo. El conector nuevo guarda a cuál reemplaza y el token
-- que Iris le entregó a ese anterior: se revoca cuando una llamada con el atk_ nuevo responde 2xx (o
-- a las 24 h, job) y entonces las dos columnas vuelven a null.
ALTER TABLE integracion.conectores ADD COLUMN reemplaza_conector_id bigint REFERENCES integracion.conectores (id);
ALTER TABLE integracion.conectores ADD COLUMN reemplaza_token_id bigint REFERENCES nucleo.tokens_api (id);

-- +goose Down
ALTER TABLE integracion.conectores DROP COLUMN reemplaza_token_id;
ALTER TABLE integracion.conectores DROP COLUMN reemplaza_conector_id;

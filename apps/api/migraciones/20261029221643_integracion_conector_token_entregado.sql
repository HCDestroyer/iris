-- +goose Up
-- El iris_pat_ que Iris entregó al otro sistema al canjear su enlace (contrato §1.2): se confirma
-- (sin vencimiento) al activar el conector y se revoca al desactivarlo o al reemplazarlo por otro
-- conector del mismo sistema.
ALTER TABLE integracion.conectores ADD COLUMN token_entregado_id bigint REFERENCES nucleo.tokens_api (id);

-- +goose Down
ALTER TABLE integracion.conectores DROP COLUMN token_entregado_id;

-- +goose Up
-- Eliminar tokens de API (tokens.eliminar): borrado lógico de un token revocado o vencido. La fila
-- se queda (auditoria.*, accesos, conectores y el portal la siguen referenciando), pero su hash se
-- borra para que no quede material de credencial, y deja de aparecer en la lista.
-- Invariante: el hash es null si y solo si el token está eliminado.
ALTER TABLE nucleo.tokens_api ALTER COLUMN hash DROP NOT NULL;
ALTER TABLE nucleo.tokens_api ADD COLUMN eliminado_en  timestamptz;
ALTER TABLE nucleo.tokens_api ADD COLUMN eliminado_por bigint REFERENCES nucleo.usuarios (id);
ALTER TABLE nucleo.tokens_api ADD CONSTRAINT tokens_api_hash_si_no_eliminado
    CHECK ((hash IS NULL) = (eliminado_en IS NOT NULL));

-- +goose Down
-- Los eliminados no tienen hash: reciben uno inservible (nadie conoce su valor) para volver a NOT NULL.
ALTER TABLE nucleo.tokens_api DROP CONSTRAINT tokens_api_hash_si_no_eliminado;
UPDATE nucleo.tokens_api SET hash = sha256(convert_to('eliminado:' || id || ':' || random()::text, 'UTF8')) WHERE hash IS NULL;
ALTER TABLE nucleo.tokens_api DROP COLUMN eliminado_por;
ALTER TABLE nucleo.tokens_api DROP COLUMN eliminado_en;
ALTER TABLE nucleo.tokens_api ALTER COLUMN hash SET NOT NULL;

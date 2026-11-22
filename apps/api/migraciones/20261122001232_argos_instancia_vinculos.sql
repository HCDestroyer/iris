-- +goose Up
-- Instancia de Argos (contrato con ArgosISP 0.14.0 §6.6): dos conectores son de la misma instancia si
-- su base_url normalizada coincide (host en minúsculas, sin punto final, sin el puerto por defecto y
-- sin la barra final) y, cuando los dos informaron remoto_id, también ese. Lo usan la reconexión (los
-- vínculos de los técnicos y los webhooks tecnico.argos_* sin entregar pasan al conector nuevo) y la
-- programación de un webhook cuyo conector de origen ya fue reemplazado.

-- +goose StatementBegin
CREATE FUNCTION integracion.url_instancia(u text) RETURNS text
LANGUAGE sql IMMUTABLE AS $$
    SELECT regexp_replace(
        regexp_replace(
            regexp_replace(lower(u), '/+$', ''),
            '^(https?://[^/]+):(443|80)(/|$)', '\1\3'),
        '^(https?://[^/:]+)\.(:|/|$)', '\1\2')
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION integracion.misma_instancia_argos(a bigint, b bigint) RETURNS boolean
LANGUAGE sql STABLE AS $$
    SELECT coalesce((
        SELECT x.sistema = 'argosisp' AND y.sistema = 'argosisp'
           AND integracion.url_instancia(x.base_url) = integracion.url_instancia(y.base_url)
           AND (x.remoto_id IS NULL OR y.remoto_id IS NULL OR x.remoto_id = y.remoto_id)
        FROM integracion.conectores x, integracion.conectores y WHERE x.id = a AND y.id = b), false)
$$;
-- +goose StatementEnd

-- +goose Down
DROP FUNCTION IF EXISTS integracion.misma_instancia_argos(bigint, bigint);
DROP FUNCTION IF EXISTS integracion.url_instancia(text);

-- Versión del contrato acordada con el otro sistema (contrato con Argos §4.3–§4.6): la menor entre la
-- que el otro dice hablar (contrato_remoto) y la que Iris le informó y aceptó (contrato_informado).
-- Es la misma regla que conector.ContratoAcordado en Go (una prueba compara las dos): remoto null →
-- null; informado null o alguna que no es una versión → remoto; empate → remoto.

-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION integracion.partes_version(v text) RETURNS numeric[]
LANGUAGE sql IMMUTABLE AS $$
    SELECT CASE WHEN p ~ '^[0-9]+(\.[0-9]+){0,2}$' THEN
        string_to_array(p || repeat('.0', 2 - (length(p) - length(replace(p, '.', '')))), '.')::numeric[]
    END
    FROM (SELECT split_part(split_part(regexp_replace(btrim(v), '^v', ''), '-', 1), '+', 1) AS p) s
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION integracion.contrato_acordado(remoto text, informado text) RETURNS text
LANGUAGE sql IMMUTABLE AS $$
    SELECT CASE WHEN r IS NULL OR i IS NULL OR r <= i THEN remoto ELSE informado END
    FROM (SELECT integracion.partes_version(remoto) AS r, integracion.partes_version(informado) AS i) v
$$;
-- +goose StatementEnd

-- +goose Down
DROP FUNCTION IF EXISTS integracion.contrato_acordado(text, text);
DROP FUNCTION IF EXISTS integracion.partes_version(text);

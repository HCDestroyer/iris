-- integracion.partes_version con la misma regla que conector.partesVersion en Go (revisión de
-- seguridad de #299): cada parte de 1 a 9 dígitos (una más larga no es una versión: falla cerrado en
-- los dos lados, en vez de compararse como numeric sin tope) y el mismo recorte a los lados (espacio,
-- tabulador, salto de línea, retorno, tabulador vertical y salto de página), no solo espacios.

-- +goose Up
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION integracion.partes_version(v text) RETURNS numeric[]
LANGUAGE sql IMMUTABLE AS $$
    SELECT CASE WHEN p ~ '^[0-9]{1,9}(\.[0-9]{1,9}){0,2}$' THEN
        string_to_array(p || repeat('.0', 2 - (length(p) - length(replace(p, '.', '')))), '.')::numeric[]
    END
    FROM (SELECT split_part(split_part(regexp_replace(btrim(v, E' \t\n\r\v\f'), '^v', ''), '-', 1), '+', 1) AS p) s
$$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION integracion.partes_version(v text) RETURNS numeric[]
LANGUAGE sql IMMUTABLE AS $$
    SELECT CASE WHEN p ~ '^[0-9]+(\.[0-9]+){0,2}$' THEN
        string_to_array(p || repeat('.0', 2 - (length(p) - length(replace(p, '.', '')))), '.')::numeric[]
    END
    FROM (SELECT split_part(split_part(regexp_replace(btrim(v), '^v', ''), '-', 1), '+', 1) AS p) s
$$;
-- +goose StatementEnd

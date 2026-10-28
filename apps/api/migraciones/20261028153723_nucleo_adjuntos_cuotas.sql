-- +goose Up
-- Cuotas de subidas en curso (por usuario y por entidad) y purga de abandonadas: índices sobre
-- los adjuntos no terminados. «Actividad» = updated_at (alta o última parte recibida).
CREATE INDEX adjuntos_en_curso_usuario_idx ON nucleo.adjuntos (subido_por)
    WHERE estado IN ('pendiente', 'subiendo');
CREATE INDEX adjuntos_en_curso_actividad_idx ON nucleo.adjuntos (updated_at)
    WHERE estado IN ('pendiente', 'subiendo');

-- +goose Down
DROP INDEX nucleo.adjuntos_en_curso_actividad_idx;
DROP INDEX nucleo.adjuntos_en_curso_usuario_idx;

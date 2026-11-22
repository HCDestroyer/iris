-- +goose Up
-- Modo empresarial del paso «Confirmar titular y plan» (contrato argosisp.md 0.10.0 §3.4.2): el
-- titular es la empresa, con su NIT, y la identidad es la del representante, con DPI. La orden
-- guarda el tipo (nit) y el nombre (razón social); nunca el número.
ALTER TABLE campo.orden_titular DROP CONSTRAINT orden_titular_documento_tipo_check;
ALTER TABLE campo.orden_titular ADD CONSTRAINT orden_titular_documento_tipo_check
    CHECK (documento_tipo IN ('dpi', 'pasaporte', 'nit'));

-- +goose Down
UPDATE campo.orden_titular SET documento_tipo = 'dpi' WHERE documento_tipo = 'nit';
ALTER TABLE campo.orden_titular DROP CONSTRAINT orden_titular_documento_tipo_check;
ALTER TABLE campo.orden_titular ADD CONSTRAINT orden_titular_documento_tipo_check
    CHECK (documento_tipo IN ('dpi', 'pasaporte'));

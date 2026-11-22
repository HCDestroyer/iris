-- +goose Up
-- Titular empresa del paso «Confirmar titular y plan» (contrato argosisp.md 0.14.1 §3.4.4): en una
-- venta empresarial el titular es la empresa (razón social y NIT en los campos fiscales) y la
-- identidad es la de su representante (DPI o pasaporte, decisión del PM). La orden guarda el tipo
-- de titular, el tipo del documento de la identidad y el nombre; nunca un número.
ALTER TABLE campo.orden_titular ADD COLUMN tipo_titular text CHECK (tipo_titular IN ('persona', 'empresa'));

-- +goose Down
ALTER TABLE campo.orden_titular DROP COLUMN tipo_titular;

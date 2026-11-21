-- +goose Up
-- Nombre del servicio (contrato argosisp.md 0.10.0 §3.7, A45 / B24): opcional, editable en Argos,
-- hasta 60 caracteres («Casa», «Negocio»). Es texto del cliente: se muestra escapado y nunca va a
-- los logs ni al proveedor de IA. El código (S-000412) ya se guardaba en codigo.
ALTER TABLE integracion.argos_servicios ADD COLUMN nombre text NOT NULL DEFAULT '' CHECK (length(nombre) <= 60);

-- +goose Down
ALTER TABLE integracion.argos_servicios DROP COLUMN nombre;

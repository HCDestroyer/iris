-- +goose Up
-- Solicitudes del portal de Argos (contrato 0.9.0 §11.7 y §10.4 punto 10; revisión H3).

-- Evidencia de que el cliente aceptó la cotización de una solicitud del portal (el cliente no la ve en
-- su portal): por qué canal, cuándo y quién la registró en Iris.
ALTER TABLE comercial.cotizaciones
    ADD COLUMN aceptacion_canal text CHECK (aceptacion_canal IN ('telefono', 'whatsapp', 'presencial', 'correo')),
    ADD COLUMN aceptacion_cliente_en timestamptz,
    ADD COLUMN aceptacion_registrada_por bigint REFERENCES nucleo.usuarios (id),
    ADD CONSTRAINT cotizaciones_aceptacion_evidencia_check CHECK ((aceptacion_canal IS NULL) = (aceptacion_cliente_en IS NULL));

-- Ajuste por empresa (§10.4 punto 10): exigir contrato firmado para ganar un cambio de plan. De fábrica
-- apagado: basta la cotización aceptada.
CREATE TABLE comercial.ajustes_solicitudes (
    empresa_id                 bigint      PRIMARY KEY,
    cambio_plan_exige_contrato boolean     NOT NULL DEFAULT false,
    version                    integer     NOT NULL DEFAULT 1,
    created_at                 timestamptz NOT NULL DEFAULT now(),
    updated_at                 timestamptz NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE comercial.ajustes_solicitudes;
ALTER TABLE comercial.cotizaciones DROP CONSTRAINT cotizaciones_aceptacion_evidencia_check,
    DROP COLUMN aceptacion_registrada_por, DROP COLUMN aceptacion_cliente_en, DROP COLUMN aceptacion_canal;

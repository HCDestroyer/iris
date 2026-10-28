-- +goose Up
-- Soporte (I2), revisión de la PR de Horus y portal:
--
-- - horus_estados: último estado y último ocurrido_en conocidos por clave (router o servicio de
--   infraestructura + empresa, o hallazgo + empresa). Un evento de Horus con ocurrido_en igual o
--   anterior al guardado es «obsoleto» y no hace nada (Horus reintenta y puede desordenar).
-- - avisos_hijos: qué hijos de un incidente maestro ya recibieron cada aviso (resuelto o
--   reabierto). El job soporte.avisar_hijos procesa en tandas y es idempotente por esta tabla.
-- - ticket_eventos.tipo 'aviso_incidente': historial del hijo avisado (por ahora no se lista por
--   la API: el enum del contrato se amplía cuando el panel lo muestre).
CREATE TABLE soporte.horus_estados (
    clave       text        PRIMARY KEY,             -- router:<empresa|0>:<id>, infra:…, hallazgo:…
    empresa_id  bigint,
    estado      text        NOT NULL CHECK (estado IN ('caido', 'arriba', 'abierto', 'resuelto')),
    ocurrido_en timestamptz NOT NULL,
    entrante_id bigint      NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE soporte.avisos_hijos (
    maestro_id bigint      NOT NULL REFERENCES soporte.tickets (id),
    aviso      text        NOT NULL,                  -- resuelto:<version> | reabierto:<version>
    hijo_id    bigint      NOT NULL REFERENCES soporte.tickets (id),
    mensaje_id bigint      NOT NULL REFERENCES soporte.ticket_mensajes (id),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (maestro_id, aviso, hijo_id)
);

ALTER TABLE soporte.ticket_eventos DROP CONSTRAINT ticket_eventos_tipo_check;
ALTER TABLE soporte.ticket_eventos ADD CONSTRAINT ticket_eventos_tipo_check
    CHECK (tipo IN ('creado', 'estado', 'asignado', 'escalado', 'fusionado', 'sla_en_riesgo', 'sla_vencido', 'aviso_incidente'));

-- +goose Down
DELETE FROM soporte.ticket_eventos WHERE tipo = 'aviso_incidente';
ALTER TABLE soporte.ticket_eventos DROP CONSTRAINT ticket_eventos_tipo_check;
ALTER TABLE soporte.ticket_eventos ADD CONSTRAINT ticket_eventos_tipo_check
    CHECK (tipo IN ('creado', 'estado', 'asignado', 'escalado', 'fusionado', 'sla_en_riesgo', 'sla_vencido'));
DROP TABLE soporte.avisos_hijos;
DROP TABLE soporte.horus_estados;

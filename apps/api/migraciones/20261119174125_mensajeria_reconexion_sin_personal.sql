-- +goose Up
-- Reconexión temporal con motivo sin_personal (contrato con Argos 0.8.0 §7.3, bloque B4; §10.2 puntos
-- 1, 2 y 12): en horario hábil, sin nadie de caja disponible. Apagada de fábrica, con un interruptor
-- por empresa aparte del de la reconexión fuera de horario; lo enciende un admin.

-- 1. El interruptor por empresa (fila ausente = apagado).
CREATE TABLE mensajeria.ajustes_reconexion (
    id             bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    empresa_id     bigint,
    sin_personal   boolean     NOT NULL DEFAULT false,
    version        integer     NOT NULL DEFAULT 1,
    actualizado_por bigint     REFERENCES nucleo.usuarios (id),
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX ajustes_reconexion_uk ON mensajeria.ajustes_reconexion (coalesce(empresa_id, 0));

-- 2. La reconexión acepta el motivo nuevo y guarda con cuántas personas de caja se decidió (para
--    revisar el criterio después: nunca quiénes). Lo que Argos cuenta después (webhooks promesa.*)
--    queda en argos_*: el motivo con el que Argos la guardó y cómo terminó.
ALTER TABLE mensajeria.reconexiones DROP CONSTRAINT reconexiones_motivo_check;
ALTER TABLE mensajeria.reconexiones ADD CONSTRAINT reconexiones_motivo_check CHECK (motivo IN ('fuera_de_horario', 'sin_personal'));
ALTER TABLE mensajeria.reconexiones
    ADD COLUMN caja_total        integer,
    ADD COLUMN caja_disponibles  integer,
    ADD COLUMN argos_motivo      text CHECK (argos_motivo IN ('fuera_de_horario', 'sin_personal')),
    ADD COLUMN argos_estado      text CHECK (argos_estado IN ('activa', 'cumplida', 'incumplida', 'cancelada')),
    ADD COLUMN argos_fin_motivo  text CHECK (argos_fin_motivo IN ('comprobante_rechazado', 'vencida_sin_revisar', 'otro')),
    ADD COLUMN argos_entrante_id bigint,
    ADD COLUMN argos_actualizada_en timestamptz;

-- +goose Down
ALTER TABLE mensajeria.reconexiones
    DROP COLUMN caja_total, DROP COLUMN caja_disponibles, DROP COLUMN argos_motivo, DROP COLUMN argos_estado,
    DROP COLUMN argos_fin_motivo, DROP COLUMN argos_entrante_id, DROP COLUMN argos_actualizada_en;
DELETE FROM mensajeria.reconexiones WHERE motivo = 'sin_personal';
ALTER TABLE mensajeria.reconexiones DROP CONSTRAINT reconexiones_motivo_check;
ALTER TABLE mensajeria.reconexiones ADD CONSTRAINT reconexiones_motivo_check CHECK (motivo IN ('fuera_de_horario'));
DROP TABLE mensajeria.ajustes_reconexion;

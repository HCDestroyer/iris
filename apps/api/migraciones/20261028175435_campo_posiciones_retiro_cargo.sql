-- +goose Up
-- Campo (I2, fase 3):
--  - ordenes.origen: de dónde vino la orden (panel, api, argos —orden de retiro automática desde
--    argos.servicio_retirado— o sistema). Las existentes quedan como panel.
--  - Cargo único al cerrar (visita cobrada): el tipo de orden o la plantilla dicen si se cobra y
--    con qué producto de Argos y/o monto (> 0). La plantilla manda si define cobrar (true o
--    false); si no, el tipo. Se configura por SQL (como el resto del catálogo).
--  - posiciones: ubicación en vivo de los técnicos, particionada por mes (UTC) y sin auditoría
--    por fila (CLAUDE.md §9); posiciones_ultimas (la última por técnico y empresa, para
--    despacho) y posiciones_limite (lotes por minuto por técnico).
ALTER TABLE campo.ordenes ADD COLUMN origen text NOT NULL DEFAULT 'panel'
    CHECK (origen IN ('panel', 'api', 'argos', 'sistema'));
CREATE INDEX ordenes_servicio_abiertas_idx ON campo.ordenes (servicio_id, tipo_id)
    WHERE servicio_id IS NOT NULL AND estado NOT IN ('cerrada', 'cancelada');

ALTER TABLE campo.tipos_orden
    ADD COLUMN cobrar            boolean       NOT NULL DEFAULT false,
    ADD COLUMN cargo_producto_id bigint,
    ADD COLUMN cargo_monto       numeric(14,2) CHECK (cargo_monto IS NULL OR cargo_monto > 0),
    ADD COLUMN cargo_descripcion text          NOT NULL DEFAULT '',
    ADD CONSTRAINT tipos_orden_cargo_ck CHECK (NOT cobrar OR cargo_producto_id IS NOT NULL OR cargo_monto IS NOT NULL);
ALTER TABLE campo.plantillas_checklist
    ADD COLUMN cobrar            boolean,
    ADD COLUMN cargo_producto_id bigint,
    ADD COLUMN cargo_monto       numeric(14,2) CHECK (cargo_monto IS NULL OR cargo_monto > 0),
    ADD COLUMN cargo_descripcion text          NOT NULL DEFAULT '';

-- tecnico_id es nucleo.usuarios.id (sin FK: tabla de alto volumen que se purga por partición).
CREATE TABLE campo.posiciones (
    tecnico_id     bigint           NOT NULL,
    empresa_id     bigint,
    lat            double precision NOT NULL CHECK (lat BETWEEN -90 AND 90),
    lon            double precision NOT NULL CHECK (lon BETWEEN -180 AND 180),
    precision_m    double precision CHECK (precision_m IS NULL OR precision_m >= 0),
    velocidad      double precision,
    rumbo          double precision,
    dispositivo_id text,
    registrada_en  timestamptz      NOT NULL,
    created_at     timestamptz      NOT NULL DEFAULT now(),
    updated_at     timestamptz      NOT NULL DEFAULT now()
) PARTITION BY RANGE (registrada_en);
-- Un punto por técnico y hora: reenviar un lote no duplica.
CREATE UNIQUE INDEX posiciones_uq ON campo.posiciones (tecnico_id, registrada_en);

-- +goose StatementBegin
-- Crea (si falta) la partición del mes UTC de p_fecha: campo.posiciones_YYYYMM.
CREATE FUNCTION campo.asegurar_particion_posiciones(p_fecha timestamptz) RETURNS text
LANGUAGE plpgsql AS $$
DECLARE
    mes    timestamp   := date_trunc('month', p_fecha AT TIME ZONE 'UTC');
    nombre text        := 'posiciones_' || to_char(mes, 'YYYYMM');
    desde  timestamptz := mes AT TIME ZONE 'UTC';
    hasta  timestamptz := (mes + interval '1 month') AT TIME ZONE 'UTC';
BEGIN
    IF to_regclass('campo.' || nombre) IS NOT NULL THEN
        RETURN nombre;
    END IF;
    PERFORM pg_advisory_xact_lock(hashtextextended('campo.posiciones.particion', 0));
    IF to_regclass('campo.' || nombre) IS NOT NULL THEN
        RETURN nombre;
    END IF;
    EXECUTE format('CREATE TABLE campo.%I PARTITION OF campo.posiciones FOR VALUES FROM (%L) TO (%L)',
                   nombre, desde, hasta);
    RETURN nombre;
END
$$;
-- +goose StatementEnd

-- +goose StatementBegin
-- Borra las particiones cuyo mes terminó antes de p_limite (retención). Devuelve cuántas.
CREATE FUNCTION campo.purgar_posiciones(p_limite timestamptz) RETURNS integer
LANGUAGE plpgsql AS $$
DECLARE
    r RECORD;
    n integer := 0;
BEGIN
    FOR r IN
        SELECT c.relname
        FROM pg_inherits i
        JOIN pg_class c ON c.oid = i.inhrelid
        WHERE i.inhparent = 'campo.posiciones'::regclass AND c.relname ~ '^posiciones_[0-9]{6}$'
        ORDER BY c.relname
    LOOP
        IF ((to_date(substr(r.relname, 12), 'YYYYMM')::timestamp + interval '1 month') AT TIME ZONE 'UTC') <= p_limite THEN
            EXECUTE format('DROP TABLE campo.%I', r.relname);
            n := n + 1;
        END IF;
    END LOOP;
    RETURN n;
END
$$;
-- +goose StatementEnd

SELECT campo.asegurar_particion_posiciones(now());
SELECT campo.asegurar_particion_posiciones(now() + interval '1 month');

CREATE TABLE campo.posiciones_ultimas (
    tecnico_id     bigint           NOT NULL REFERENCES nucleo.usuarios (id),
    empresa_id     bigint,
    lat            double precision NOT NULL,
    lon            double precision NOT NULL,
    precision_m    double precision,
    velocidad      double precision,
    rumbo          double precision,
    dispositivo_id text,
    registrada_en  timestamptz      NOT NULL,
    recibida_en    timestamptz      NOT NULL,
    created_at     timestamptz      NOT NULL DEFAULT now(),
    updated_at     timestamptz      NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX posiciones_ultimas_uq ON campo.posiciones_ultimas (tecnico_id, empresa_id) NULLS NOT DISTINCT;

CREATE TABLE campo.posiciones_limite (
    tecnico_id     bigint      PRIMARY KEY REFERENCES nucleo.usuarios (id),
    ventana_inicio timestamptz NOT NULL,
    lotes          integer     NOT NULL,
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE campo.posiciones_limite;
DROP TABLE campo.posiciones_ultimas;
DROP TABLE campo.posiciones;
DROP FUNCTION campo.purgar_posiciones(timestamptz);
DROP FUNCTION campo.asegurar_particion_posiciones(timestamptz);
ALTER TABLE campo.plantillas_checklist
    DROP COLUMN cargo_descripcion, DROP COLUMN cargo_monto, DROP COLUMN cargo_producto_id, DROP COLUMN cobrar;
ALTER TABLE campo.tipos_orden
    DROP CONSTRAINT tipos_orden_cargo_ck,
    DROP COLUMN cargo_descripcion, DROP COLUMN cargo_monto, DROP COLUMN cargo_producto_id, DROP COLUMN cobrar;
DROP INDEX campo.ordenes_servicio_abiertas_idx;
ALTER TABLE campo.ordenes DROP COLUMN origen;

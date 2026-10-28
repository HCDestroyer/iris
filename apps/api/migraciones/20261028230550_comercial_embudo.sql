-- +goose Up
-- Seguimiento comercial (Fase 4, I2): prospecto → factibilidad → cotización → contrato → alta en
-- Argos (contratos/argosisp.md §3: POST /clientes y POST /servicios en pendiente_instalacion) y
-- orden de instalación. Todo lo operativo lleva empresa_id (null = modo sin conector) y se
-- adopta con la empresa (nucleo.RegistrarTablaConEmpresa en internal/comercial).
CREATE SCHEMA comercial;

-- Numeración legible: PR-000001, COT-000001, CTR-000001 (crece sin truncar).
CREATE SEQUENCE comercial.prospectos_numero_seq;
CREATE SEQUENCE comercial.cotizaciones_numero_seq;
CREATE SEQUENCE comercial.contratos_numero_seq;

-- +goose StatementBegin
CREATE FUNCTION comercial.siguiente_numero(prefijo text, secuencia regclass) RETURNS text
LANGUAGE sql VOLATILE AS $$
    SELECT prefijo || '-' || lpad(s.n::text, greatest(6, length(s.n::text)), '0')
    FROM (SELECT nextval(secuencia) AS n) s
$$;
-- +goose StatementEnd

CREATE TABLE comercial.prospectos (
    id                  bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    numero              text             NOT NULL DEFAULT comercial.siguiente_numero('PR', 'comercial.prospectos_numero_seq'),
    empresa_id          bigint,
    origen              text             NOT NULL CHECK (origen IN ('panel', 'web', 'referido', 'argos', 'otro')),
    origen_detalle      text,                                  -- quién lo refirió, campaña…
    nombre              text             NOT NULL,
    documento           text,                                  -- NIT o DPI
    telefono            text,
    correo              text,
    direccion           text             NOT NULL,
    lat                 double precision,
    lon                 double precision,
    zona_id             bigint           REFERENCES agenda.zonas (id),
    plan_interes_id     bigint,                                -- plan de Argos
    plan_interes_nombre text,
    vendedor_id         bigint           REFERENCES nucleo.usuarios (id),
    estado              text             NOT NULL DEFAULT 'nuevo'
                                         CHECK (estado IN ('nuevo', 'contactado', 'factibilidad', 'cotizado',
                                                           'contrato', 'ganado', 'perdido')),
    notas               text             NOT NULL DEFAULT '',
    motivo_perdida      text,
    perdido_en          timestamptz,
    ganado_en           timestamptz,
    obra_confirmada     boolean          NOT NULL DEFAULT false,
    -- Alta: local (sin conector: la orden con los datos capturados) o argos (pendientes de §3).
    alta_modo           text             CHECK (alta_modo IN ('local', 'argos')),
    alta_estado         text             CHECK (alta_estado IN ('esperando_cliente', 'esperando_servicio', 'completa')),
    cliente_argos_id    bigint,
    servicio_argos_id   bigint,
    orden_id            bigint           REFERENCES campo.ordenes (id),
    creado_por          bigint           REFERENCES nucleo.usuarios (id),
    idempotencia        text,
    version             integer          NOT NULL DEFAULT 1,
    created_at          timestamptz      NOT NULL DEFAULT now(),
    updated_at          timestamptz      NOT NULL DEFAULT now(),
    CHECK ((lat IS NULL) = (lon IS NULL)),
    CHECK (lat IS NULL OR (lat BETWEEN -90 AND 90 AND lon BETWEEN -180 AND 180)),
    CHECK ((estado = 'perdido') = (motivo_perdida IS NOT NULL)),
    CHECK ((estado = 'ganado') = (ganado_en IS NOT NULL)),
    CHECK ((estado = 'ganado') = (alta_modo IS NOT NULL)),
    CHECK (alta_modo IS DISTINCT FROM 'local' OR (cliente_argos_id IS NULL AND servicio_argos_id IS NULL))
);
CREATE UNIQUE INDEX prospectos_numero_uq ON comercial.prospectos (numero);
CREATE UNIQUE INDEX prospectos_idempotencia_uq ON comercial.prospectos (idempotencia) WHERE idempotencia IS NOT NULL;
CREATE INDEX prospectos_empresa_estado_idx ON comercial.prospectos (empresa_id, estado, id);
CREATE INDEX prospectos_vendedor_idx ON comercial.prospectos (vendedor_id, id) WHERE vendedor_id IS NOT NULL;
CREATE INDEX prospectos_zona_idx ON comercial.prospectos (zona_id, id) WHERE zona_id IS NOT NULL;
CREATE INDEX prospectos_alta_idx ON comercial.prospectos (id) WHERE alta_estado IN ('esperando_cliente', 'esperando_servicio');

-- Historial del prospecto (estado, asignación, alta en Argos).
CREATE TABLE comercial.prospecto_eventos (
    id           bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    prospecto_id bigint      NOT NULL REFERENCES comercial.prospectos (id),
    tipo         text        NOT NULL,
    de           text,
    a            text,
    motivo       text,
    datos        jsonb       NOT NULL DEFAULT '{}'::jsonb,
    actor_tipo   text        NOT NULL,
    actor_id     bigint,
    created_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX prospecto_eventos_idx ON comercial.prospecto_eventos (prospecto_id, id);

-- Actividades del vendedor: llamada, visita, nota (y mensaje), con la próxima acción.
CREATE TABLE comercial.actividades (
    id             bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    empresa_id     bigint,
    prospecto_id   bigint      NOT NULL REFERENCES comercial.prospectos (id),
    tipo           text        NOT NULL CHECK (tipo IN ('llamada', 'visita', 'nota', 'mensaje')),
    descripcion    text        NOT NULL,
    resultado      text,
    ocurrida_en    timestamptz NOT NULL,
    proxima_accion text,
    proxima_fecha  timestamptz,
    autor_id       bigint      REFERENCES nucleo.usuarios (id),
    idempotencia   text,
    created_at     timestamptz NOT NULL DEFAULT now(),
    CHECK (proxima_fecha IS NULL OR proxima_accion IS NOT NULL)
);
CREATE INDEX actividades_prospecto_idx ON comercial.actividades (prospecto_id, id);
CREATE INDEX actividades_proxima_idx ON comercial.actividades (empresa_id, proxima_fecha) WHERE proxima_fecha IS NOT NULL;
CREATE UNIQUE INDEX actividades_idempotencia_uq ON comercial.actividades (idempotencia) WHERE idempotencia IS NOT NULL;

-- Umbrales de la factibilidad por distancia (por empresa; empresa_id null = por defecto).
CREATE TABLE comercial.umbrales_factibilidad (
    id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    empresa_id  bigint,
    viable_m    integer     NOT NULL CHECK (viable_m > 0),
    obra_m      integer     NOT NULL,
    version     integer     NOT NULL DEFAULT 1,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    CHECK (obra_m >= viable_m AND obra_m <= 50000)
);
CREATE UNIQUE INDEX umbrales_empresa_uq ON comercial.umbrales_factibilidad (coalesce(empresa_id, 0));
INSERT INTO comercial.umbrales_factibilidad (empresa_id, viable_m, obra_m) VALUES (NULL, 300, 1000);

-- Evaluaciones de factibilidad (historial; la vigente es la última).
CREATE TABLE comercial.factibilidades (
    id            bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    empresa_id    bigint,
    prospecto_id  bigint           NOT NULL REFERENCES comercial.prospectos (id),
    resultado     text             NOT NULL CHECK (resultado IN ('viable', 'no_viable', 'requiere_obra', 'indeterminado')),
    motor         text             NOT NULL,                -- distancia, manual, ozmap (fase 6)
    motivo        text,                                     -- por qué es indeterminado (sin_conector…)
    lat           double precision,
    lon           double precision,
    router_id     bigint,
    router_nombre text,
    caja          text,
    distancia_m   double precision,
    viable_m      integer,
    obra_m        integer,
    notas         text             NOT NULL DEFAULT '',
    evaluada_por  bigint           REFERENCES nucleo.usuarios (id),
    created_at    timestamptz      NOT NULL DEFAULT now(),
    CHECK ((resultado = 'indeterminado') = (motivo IS NOT NULL))
);
CREATE INDEX factibilidades_prospecto_idx ON comercial.factibilidades (prospecto_id, id);

-- Cotizaciones: una versión nueva reemplaza la anterior sin borrarla. Montos numeric(14,2).
CREATE TABLE comercial.cotizaciones (
    id              bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    numero          text          NOT NULL DEFAULT comercial.siguiente_numero('COT', 'comercial.cotizaciones_numero_seq'),
    empresa_id      bigint,
    prospecto_id    bigint        NOT NULL REFERENCES comercial.prospectos (id),
    revision        integer       NOT NULL,
    estado          text          NOT NULL DEFAULT 'vigente' CHECK (estado IN ('vigente', 'aceptada', 'reemplazada')),
    moneda          text          NOT NULL DEFAULT 'GTQ',
    total_mensual   numeric(14,2) NOT NULL,
    total_unico     numeric(14,2) NOT NULL,
    vigente_hasta   timestamptz   NOT NULL,
    plan_id         bigint,
    notas           text          NOT NULL DEFAULT '',
    cliente         jsonb         NOT NULL DEFAULT '{}'::jsonb, -- datos del prospecto al cotizar (el PDF no cambia si se editan)
    pdf_adjunto_id  uuid,
    reemplazada_por bigint        REFERENCES comercial.cotizaciones (id),
    aceptada_en     timestamptz,
    aceptada_por    bigint        REFERENCES nucleo.usuarios (id),
    creado_por      bigint        REFERENCES nucleo.usuarios (id),
    idempotencia    text,
    version         integer       NOT NULL DEFAULT 1,
    created_at      timestamptz   NOT NULL DEFAULT now(),
    updated_at      timestamptz   NOT NULL DEFAULT now(),
    UNIQUE (prospecto_id, revision),
    CHECK (total_mensual >= 0 AND total_unico >= 0),
    CHECK ((estado = 'aceptada') = (aceptada_en IS NOT NULL))
);
CREATE UNIQUE INDEX cotizaciones_numero_uq ON comercial.cotizaciones (numero);
CREATE UNIQUE INDEX cotizaciones_idempotencia_uq ON comercial.cotizaciones (idempotencia) WHERE idempotencia IS NOT NULL;
-- A lo sumo una cotización vigente o aceptada por prospecto.
CREATE UNIQUE INDEX cotizaciones_viva_uq ON comercial.cotizaciones (prospecto_id) WHERE estado IN ('vigente', 'aceptada');

CREATE TABLE comercial.cotizacion_lineas (
    id              bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    cotizacion_id   bigint        NOT NULL REFERENCES comercial.cotizaciones (id),
    posicion        integer       NOT NULL,
    tipo            text          NOT NULL CHECK (tipo IN ('plan', 'instalacion', 'equipo', 'descuento', 'otro')),
    plan_id         bigint,
    producto_id     bigint,
    descripcion     text          NOT NULL,
    cantidad        numeric(10,2) NOT NULL CHECK (cantidad > 0),
    precio_unitario numeric(14,2) NOT NULL CHECK (precio_unitario >= 0),
    recurrente      boolean       NOT NULL,
    total           numeric(14,2) NOT NULL,                -- negativo en los descuentos
    UNIQUE (cotizacion_id, posicion),
    CHECK ((tipo = 'descuento' AND total <= 0) OR (tipo <> 'descuento' AND total >= 0)),
    CHECK (tipo <> 'plan' OR recurrente)
);

-- Contratos: plantilla con los datos del prospecto y la cotización aceptada; firma del cliente
-- capturada como imagen (SVG) y PDF firmado guardado como adjunto con su SHA-256.
CREATE TABLE comercial.contratos (
    id                 bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    numero             text        NOT NULL DEFAULT comercial.siguiente_numero('CTR', 'comercial.contratos_numero_seq'),
    empresa_id         bigint,
    prospecto_id       bigint      NOT NULL REFERENCES comercial.prospectos (id),
    cotizacion_id      bigint      NOT NULL REFERENCES comercial.cotizaciones (id),
    plantilla          text        NOT NULL,
    estado             text        NOT NULL DEFAULT 'borrador' CHECK (estado IN ('borrador', 'firmado', 'anulado')),
    contenido          jsonb       NOT NULL,              -- datos con que se armó (vuelve a generar el mismo PDF)
    pdf_adjunto_id     uuid,
    firmante_nombre    text,
    firmante_documento text,
    firma_svg          text,
    firma_lat          double precision,
    firma_lon          double precision,
    firmado_en         timestamptz,
    firmado_por        bigint      REFERENCES nucleo.usuarios (id),
    pdf_firmado_id     uuid,
    pdf_firmado_sha256 text,
    motivo_anulacion   text,
    version            integer     NOT NULL DEFAULT 1,
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now(),
    CHECK (estado <> 'firmado' OR (firmado_en IS NOT NULL AND pdf_firmado_id IS NOT NULL AND pdf_firmado_sha256 IS NOT NULL)),
    CHECK ((estado = 'anulado') = (motivo_anulacion IS NOT NULL))
);
CREATE UNIQUE INDEX contratos_numero_uq ON comercial.contratos (numero);
-- A lo sumo un contrato vivo (borrador o firmado) por prospecto.
CREATE UNIQUE INDEX contratos_vivo_uq ON comercial.contratos (prospecto_id) WHERE estado IN ('borrador', 'firmado');

-- La orden de instalación nace del prospecto ganado. NOT VALID: no revisa filas viejas (la
-- entrada prospectoId de ordenes.crear existía sin tabla); las nuevas sí.
ALTER TABLE campo.ordenes
    ADD CONSTRAINT ordenes_prospecto_fk FOREIGN KEY (prospecto_id) REFERENCES comercial.prospectos (id) NOT VALID;
CREATE INDEX ordenes_prospecto_idx ON campo.ordenes (prospecto_id) WHERE prospecto_id IS NOT NULL;

-- +goose Down
DROP INDEX campo.ordenes_prospecto_idx;
ALTER TABLE campo.ordenes DROP CONSTRAINT ordenes_prospecto_fk;
DROP SCHEMA comercial CASCADE;

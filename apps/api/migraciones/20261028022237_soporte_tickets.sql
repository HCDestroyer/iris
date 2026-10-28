-- +goose Up
-- Soporte (I2): tickets con SLA por categoría y prioridad, calendario laboral y feriados.
-- Los datos de Argos (cliente, servicio, zona) se guardan solo como ids; sin conector, los
-- datos de contacto capturados a mano (CLAUDE.md §4).
CREATE SCHEMA IF NOT EXISTS soporte;

-- Calendario laboral: tramos por día de la semana y feriados. Un SLA sin calendario es 24/7.
CREATE TABLE soporte.calendarios (
    id           bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    empresa_id   bigint,                                     -- null = de todas las empresas
    codigo       text        NOT NULL,
    nombre       text        NOT NULL,
    zona_horaria text        NOT NULL DEFAULT 'America/Guatemala',
    version      integer     NOT NULL DEFAULT 1,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX calendarios_codigo_uq ON soporte.calendarios (coalesce(empresa_id, 0), codigo);

-- Horario laboral (hora local de la zona del calendario). dia_semana ISO: 1 = lunes … 7 = domingo.
CREATE TABLE soporte.calendario_tramos (
    id            bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    calendario_id bigint      NOT NULL REFERENCES soporte.calendarios (id) ON DELETE CASCADE,
    dia_semana    smallint    NOT NULL CHECK (dia_semana BETWEEN 1 AND 7),
    inicio        time        NOT NULL,
    fin           time        NOT NULL,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),
    CHECK (inicio < fin)
);
CREATE INDEX calendario_tramos_calendario_idx ON soporte.calendario_tramos (calendario_id, dia_semana, inicio);

-- Feriados: todo el día, o desde una hora (24 y 31 de diciembre, medio día).
CREATE TABLE soporte.feriados (
    id            bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    calendario_id bigint      NOT NULL REFERENCES soporte.calendarios (id) ON DELETE CASCADE,
    fecha         date        NOT NULL,
    nombre        text        NOT NULL,
    desde         time,                                      -- null = todo el día
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),
    UNIQUE (calendario_id, fecha)
);

CREATE TABLE soporte.slas (
    id            bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    empresa_id    bigint,
    codigo        text        NOT NULL,
    nombre        text        NOT NULL,
    calendario_id bigint      REFERENCES soporte.calendarios (id), -- null = 24/7
    por_defecto   boolean     NOT NULL DEFAULT false,              -- para categorías sin SLA
    activo        boolean     NOT NULL DEFAULT true,
    version       integer     NOT NULL DEFAULT 1,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX slas_codigo_uq ON soporte.slas (coalesce(empresa_id, 0), codigo);
CREATE UNIQUE INDEX slas_por_defecto_uq ON soporte.slas (coalesce(empresa_id, 0)) WHERE por_defecto;

-- Objetivos del SLA por prioridad, en minutos (laborales si el SLA tiene calendario).
CREATE TABLE soporte.sla_objetivos (
    sla_id         bigint      NOT NULL REFERENCES soporte.slas (id) ON DELETE CASCADE,
    prioridad      text        NOT NULL CHECK (prioridad IN ('baja', 'media', 'alta', 'critica')),
    respuesta_min  integer     NOT NULL CHECK (respuesta_min > 0),
    resolucion_min integer     NOT NULL CHECK (resolucion_min > 0),
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (sla_id, prioridad),
    CHECK (respuesta_min <= resolucion_min)
);

CREATE TABLE soporte.categorias (
    id             bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    empresa_id     bigint,
    codigo         text        NOT NULL,
    nombre         text        NOT NULL,
    sla_id         bigint      REFERENCES soporte.slas (id),     -- null = el SLA por defecto
    requiere_orden boolean     NOT NULL DEFAULT false,
    activa         boolean     NOT NULL DEFAULT true,
    version        integer     NOT NULL DEFAULT 1,
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX categorias_codigo_uq ON soporte.categorias (coalesce(empresa_id, 0), codigo);

-- Numeración legible: TK-000001 (crece sin truncar después de 999999).
CREATE SEQUENCE soporte.tickets_numero_seq;

-- +goose StatementBegin
CREATE FUNCTION soporte.siguiente_numero_ticket() RETURNS text
LANGUAGE sql VOLATILE AS $$
    SELECT 'TK-' || lpad(s.n::text, greatest(6, length(s.n::text)), '0')
    FROM (SELECT nextval('soporte.tickets_numero_seq') AS n) s
$$;
-- +goose StatementEnd

CREATE TABLE soporte.tickets (
    id                   bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    numero               text        NOT NULL DEFAULT soporte.siguiente_numero_ticket(),
    empresa_id           bigint,
    -- Argos (ids; la instantánea vive en integracion.argos_*)
    cliente_id           bigint,
    servicio_id          bigint,
    zona_id              bigint,
    -- contacto capturado a mano (sin conector o cliente no identificado)
    contacto_nombre      text,
    contacto_telefono    text,
    contacto_correo      text,
    categoria_id         bigint      REFERENCES soporte.categorias (id),
    prioridad            text        NOT NULL CHECK (prioridad IN ('baja', 'media', 'alta', 'critica')),
    estado               text        NOT NULL DEFAULT 'nuevo'
                                     CHECK (estado IN ('nuevo', 'abierto', 'en_espera', 'resuelto', 'cerrado')),
    origen               text        NOT NULL
                                     CHECK (origen IN ('panel', 'portal', 'correo', 'telegram', 'horus', 'argos', 'api')),
    asunto               text        NOT NULL,
    asignado_a           bigint      REFERENCES nucleo.usuarios (id),
    cuadrilla_id         bigint,                             -- agenda.cuadrillas (fase 3)
    nivel_escalamiento   integer     NOT NULL DEFAULT 0,
    -- SLA: objetivos copiados al crear (cambiar el SLA no mueve tickets abiertos); escalar los
    -- recalcula. pausa_seg acumula el tiempo laboral en espera o resuelto (no cuenta para la
    -- resolución). vence_*/riesgo_* (80 %) se recalculan en cada acción; null = sin SLA o en pausa.
    sla_id               bigint      REFERENCES soporte.slas (id),
    calendario_id        bigint      REFERENCES soporte.calendarios (id),
    sla_inicio           timestamptz NOT NULL,
    sla_respuesta_min    integer,
    sla_resolucion_min   integer,
    pausa_seg            bigint      NOT NULL DEFAULT 0 CHECK (pausa_seg >= 0),
    riesgo_respuesta     timestamptz,
    vence_respuesta      timestamptz,
    riesgo_resolucion    timestamptz,
    vence_resolucion     timestamptz,
    primera_respuesta_en timestamptz,
    en_espera_desde      timestamptz,
    resuelto_en          timestamptz,
    cerrado_en           timestamptz,
    -- relaciones
    ticket_padre_id      bigint      REFERENCES soporte.tickets (id), -- incidente maestro
    fusionado_en_id      bigint      REFERENCES soporte.tickets (id), -- ticket que lo absorbió
    version              integer     NOT NULL DEFAULT 1,
    created_at           timestamptz NOT NULL DEFAULT now(),
    updated_at           timestamptz NOT NULL DEFAULT now(),
    CHECK ((estado = 'en_espera') = (en_espera_desde IS NOT NULL)),
    CHECK (estado IN ('resuelto', 'cerrado') OR resuelto_en IS NULL),
    CHECK ((estado = 'cerrado') = (cerrado_en IS NOT NULL)),
    CHECK (fusionado_en_id IS NULL OR fusionado_en_id <> id),
    CHECK (ticket_padre_id IS NULL OR ticket_padre_id <> id)
);
CREATE UNIQUE INDEX tickets_numero_uq ON soporte.tickets (numero);
CREATE INDEX tickets_empresa_estado_idx ON soporte.tickets (empresa_id, estado, id);
CREATE INDEX tickets_cliente_idx ON soporte.tickets (cliente_id, id) WHERE cliente_id IS NOT NULL;
CREATE INDEX tickets_asignado_idx ON soporte.tickets (asignado_a, id) WHERE asignado_a IS NOT NULL;
CREATE INDEX tickets_zona_idx ON soporte.tickets (zona_id, id) WHERE zona_id IS NOT NULL;
CREATE INDEX tickets_cuadrilla_idx ON soporte.tickets (cuadrilla_id, id) WHERE cuadrilla_id IS NOT NULL;
CREATE INDEX tickets_padre_idx ON soporte.tickets (ticket_padre_id) WHERE ticket_padre_id IS NOT NULL;
-- Para el cron de SLA: solo los relojes que siguen corriendo.
CREATE INDEX tickets_sla_respuesta_idx ON soporte.tickets (riesgo_respuesta)
    WHERE primera_respuesta_en IS NULL AND estado IN ('nuevo', 'abierto', 'en_espera');
CREATE INDEX tickets_sla_resolucion_idx ON soporte.tickets (riesgo_resolucion)
    WHERE estado IN ('nuevo', 'abierto');

-- Conversación: mensajes públicos (los ve el cliente) y notas internas.
CREATE TABLE soporte.ticket_mensajes (
    id           bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    ticket_id    bigint      NOT NULL REFERENCES soporte.tickets (id),
    empresa_id   bigint,
    publico      boolean     NOT NULL,
    autor_tipo   text        NOT NULL CHECK (autor_tipo IN ('usuario', 'cliente', 'sistema')),
    autor_id     bigint      REFERENCES nucleo.usuarios (id),
    autor_nombre text        NOT NULL DEFAULT '',
    canal        text        NOT NULL,
    cuerpo       text        NOT NULL CHECK (length(cuerpo) > 0),
    adjuntos     uuid[]      NOT NULL DEFAULT '{}',          -- ids de adjuntos (subida: I1, tarea 5)
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX ticket_mensajes_ticket_idx ON soporte.ticket_mensajes (ticket_id, id);

-- Historial: estado, asignación, escalamiento, fusión y SLA. Solo inserción.
CREATE TABLE soporte.ticket_eventos (
    id           bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    ticket_id    bigint      NOT NULL REFERENCES soporte.tickets (id),
    empresa_id   bigint,
    tipo         text        NOT NULL CHECK (tipo IN ('creado', 'estado', 'asignado', 'escalado', 'fusionado',
                                                      'sla_en_riesgo', 'sla_vencido')),
    de           text,
    a            text,
    motivo       text,
    datos        jsonb       NOT NULL DEFAULT '{}'::jsonb,
    actor_tipo   text        NOT NULL,
    actor_id     bigint,
    actor_nombre text        NOT NULL DEFAULT '',
    request_id   text        NOT NULL DEFAULT '',
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX ticket_eventos_ticket_idx ON soporte.ticket_eventos (ticket_id, id);

-- Alertas de SLA ya emitidas: una por ticket, reloj y umbral (el cron es idempotente).
CREATE TABLE soporte.ticket_sla_alertas (
    ticket_id  bigint      NOT NULL REFERENCES soporte.tickets (id),
    sla        text        NOT NULL CHECK (sla IN ('respuesta', 'resolucion')),
    umbral     text        NOT NULL CHECK (umbral IN ('en_riesgo', 'vencido')),
    vence_en   timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (ticket_id, sla, umbral)
);

-- +goose Down
DROP TABLE soporte.ticket_sla_alertas;
DROP TABLE soporte.ticket_eventos;
DROP TABLE soporte.ticket_mensajes;
DROP TABLE soporte.tickets;
DROP FUNCTION soporte.siguiente_numero_ticket();
DROP SEQUENCE soporte.tickets_numero_seq;
DROP TABLE soporte.categorias;
DROP TABLE soporte.sla_objetivos;
DROP TABLE soporte.slas;
DROP TABLE soporte.feriados;
DROP TABLE soporte.calendario_tramos;
DROP TABLE soporte.calendarios;
DROP SCHEMA soporte;

-- +goose Up
-- Campo (I2): órdenes de trabajo con su máquina de estados, checklist por plantilla, materiales,
-- equipos, firma y adjuntos (referencias), y la sincronización de la app de campo
-- (contratos/sync.md): dispositivos, operaciones del lote (idempotencia) y el feed de cambios
-- por técnico. Los datos de Argos (cliente, servicio, zona, producto, unidad, bodega) se guardan
-- solo como ids (CLAUDE.md §4).
CREATE SCHEMA IF NOT EXISTS campo;

-- Plantillas de checklist. tipo_codigo dice para qué tipo de orden sirve (una instalación puede
-- ser de fibra o inalámbrica); el tipo apunta a su plantilla por defecto.
CREATE TABLE campo.plantillas_checklist (
    id             bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    codigo         text        NOT NULL UNIQUE,
    nombre         text        NOT NULL,
    tipo_codigo    text        NOT NULL
                               CHECK (tipo_codigo IN ('instalacion', 'visita', 'retiro', 'cambio_equipo', 'mantenimiento')),
    requiere_firma boolean     NOT NULL DEFAULT false,
    activa         boolean     NOT NULL DEFAULT true,
    version        integer     NOT NULL DEFAULT 1,
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now()
);

-- Ítems de la plantilla. foto: el valor es el uuid de un adjunto; medicion: número con unidad.
CREATE TABLE campo.plantilla_items (
    id           bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    plantilla_id bigint      NOT NULL REFERENCES campo.plantillas_checklist (id) ON DELETE CASCADE,
    posicion     integer     NOT NULL,
    codigo       text        NOT NULL,
    etiqueta     text        NOT NULL,
    tipo         text        NOT NULL CHECK (tipo IN ('si_no', 'texto', 'numero', 'foto', 'medicion')),
    obligatorio  boolean     NOT NULL DEFAULT false,
    unidad       text        NOT NULL DEFAULT '',
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    UNIQUE (plantilla_id, codigo)
);
CREATE INDEX plantilla_items_plantilla_idx ON campo.plantilla_items (plantilla_id, posicion);

-- Tipos de orden. exige_instalar / exige_retirar: reglas de cierre de equipos.
CREATE TABLE campo.tipos_orden (
    id             bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    codigo         text        NOT NULL UNIQUE
                               CHECK (codigo IN ('instalacion', 'visita', 'retiro', 'cambio_equipo', 'mantenimiento')),
    nombre         text        NOT NULL,
    duracion_min   integer     NOT NULL CHECK (duracion_min > 0),
    plantilla_id   bigint      REFERENCES campo.plantillas_checklist (id),
    exige_instalar boolean     NOT NULL DEFAULT false,
    exige_retirar  boolean     NOT NULL DEFAULT false,
    activo         boolean     NOT NULL DEFAULT true,
    version        integer     NOT NULL DEFAULT 1,
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now()
);

-- Numeración legible: OT-000001 (crece sin truncar después de 999999).
CREATE SEQUENCE campo.ordenes_numero_seq;

-- +goose StatementBegin
CREATE FUNCTION campo.siguiente_numero_orden() RETURNS text
LANGUAGE sql VOLATILE AS $$
    SELECT 'OT-' || lpad(s.n::text, greatest(6, length(s.n::text)), '0')
    FROM (SELECT nextval('campo.ordenes_numero_seq') AS n) s
$$;
-- +goose StatementEnd

CREATE TABLE campo.ordenes (
    id                bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    numero            text             NOT NULL DEFAULT campo.siguiente_numero_orden(),
    empresa_id        bigint,
    tipo_id           bigint           NOT NULL REFERENCES campo.tipos_orden (id),
    plantilla_id      bigint           REFERENCES campo.plantillas_checklist (id),
    ticket_id         bigint           REFERENCES soporte.tickets (id),
    prospecto_id      bigint,                                  -- comercial.prospectos (fase 4)
    -- Argos (ids; la instantánea vive en integracion.argos_*)
    cliente_id        bigint,
    servicio_id       bigint,
    zona_id           bigint,
    cuadrilla_id      bigint,                                  -- agenda.cuadrillas (fase 3)
    bodega_id         bigint,                                  -- bodega del técnico en Argos
    direccion         text             NOT NULL,
    lat               double precision,
    lon               double precision,
    contacto_nombre   text,
    contacto_telefono text,
    descripcion       text             NOT NULL DEFAULT '',
    tecnico_id        bigint           REFERENCES nucleo.usuarios (id),
    asignada_en       timestamptz,
    estado            text             NOT NULL DEFAULT 'pendiente'
                                       CHECK (estado IN ('pendiente', 'programada', 'en_camino', 'en_sitio',
                                                         'cerrada', 'fallida', 'cancelada')),
    programada_inicio timestamptz,
    programada_fin    timestamptz,
    en_camino_en      timestamptz,
    en_sitio_en       timestamptz,
    finalizada_en     timestamptz,
    resultado         text             CHECK (resultado IN ('exitosa', 'parcial', 'fallida', 'cancelada')),
    motivo_falla      text,
    motivo            text,
    creado_por        bigint           REFERENCES nucleo.usuarios (id),
    idempotencia      text,
    version           integer          NOT NULL DEFAULT 1,
    created_at        timestamptz      NOT NULL DEFAULT now(),
    updated_at        timestamptz      NOT NULL DEFAULT now(),
    CHECK ((lat IS NULL) = (lon IS NULL)),
    CHECK (lat IS NULL OR (lat BETWEEN -90 AND 90 AND lon BETWEEN -180 AND 180)),
    CHECK (estado NOT IN ('programada', 'en_camino', 'en_sitio') OR tecnico_id IS NOT NULL),
    CHECK (estado <> 'programada' OR programada_inicio IS NOT NULL),
    CHECK (programada_fin IS NULL OR programada_fin > programada_inicio),
    CHECK ((estado IN ('cerrada', 'fallida', 'cancelada')) = (finalizada_en IS NOT NULL)),
    CHECK ((estado IN ('cerrada', 'fallida', 'cancelada')) = (resultado IS NOT NULL)),
    CHECK (estado <> 'fallida' OR motivo_falla IS NOT NULL)
);
CREATE UNIQUE INDEX ordenes_numero_uq ON campo.ordenes (numero);
CREATE UNIQUE INDEX ordenes_idempotencia_uq ON campo.ordenes (idempotencia) WHERE idempotencia IS NOT NULL;
CREATE INDEX ordenes_empresa_estado_idx ON campo.ordenes (empresa_id, estado, id);
CREATE INDEX ordenes_tecnico_idx ON campo.ordenes (tecnico_id, estado) WHERE tecnico_id IS NOT NULL;
CREATE INDEX ordenes_ticket_idx ON campo.ordenes (ticket_id) WHERE ticket_id IS NOT NULL;
CREATE INDEX ordenes_cliente_idx ON campo.ordenes (cliente_id, id) WHERE cliente_id IS NOT NULL;
CREATE INDEX ordenes_zona_idx ON campo.ordenes (zona_id, id) WHERE zona_id IS NOT NULL;
CREATE INDEX ordenes_cuadrilla_idx ON campo.ordenes (cuadrilla_id, id) WHERE cuadrilla_id IS NOT NULL;

-- Historial: creación, estado, asignación y programación. Solo inserción. ocurrido_en es la
-- hora del hecho (la del teléfono corregida, en el lote); created_at, la de registro.
CREATE TABLE campo.orden_eventos (
    id           bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    orden_id     bigint      NOT NULL REFERENCES campo.ordenes (id),
    tipo         text        NOT NULL CHECK (tipo IN ('creada', 'estado', 'asignada', 'programada')),
    de           text,
    a            text,
    motivo       text,
    datos        jsonb       NOT NULL DEFAULT '{}'::jsonb,
    actor_tipo   text        NOT NULL,
    actor_id     bigint,
    actor_nombre text        NOT NULL DEFAULT '',
    canal        text        NOT NULL DEFAULT '',
    request_id   text        NOT NULL DEFAULT '',
    ocurrido_en  timestamptz NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX orden_eventos_orden_idx ON campo.orden_eventos (orden_id, id);

-- Checklist: la marca vigente por ítem (gana la más reciente por ocurrido_en) y el historial
-- de todas las marcas, también las que perdieron (contratos/sync.md §3).
CREATE TABLE campo.orden_checklist (
    orden_id     bigint      NOT NULL REFERENCES campo.ordenes (id),
    item_id      bigint      NOT NULL REFERENCES campo.plantilla_items (id),
    valor        jsonb       NOT NULL,
    marcado_por  bigint      REFERENCES nucleo.usuarios (id),
    canal        text        NOT NULL DEFAULT '',
    ocurrido_en  timestamptz NOT NULL,
    operacion_id uuid,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (orden_id, item_id)
);

CREATE TABLE campo.orden_checklist_historial (
    id           bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    orden_id     bigint      NOT NULL REFERENCES campo.ordenes (id),
    item_id      bigint      NOT NULL REFERENCES campo.plantilla_items (id),
    valor        jsonb       NOT NULL,
    marcado_por  bigint      REFERENCES nucleo.usuarios (id),
    canal        text        NOT NULL DEFAULT '',
    ocurrido_en  timestamptz NOT NULL,
    operacion_id uuid,
    vigente      boolean     NOT NULL,                     -- ganó al registrarse
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX orden_checklist_historial_idx ON campo.orden_checklist_historial (orden_id, item_id, id);

-- Notas, materiales y equipos usan como id el de la operación del lote que los creó (uuid v7 del
-- teléfono): el cliente los referencia sin esperar al servidor. Desde el panel, uno v7 nuevo.
CREATE TABLE campo.orden_notas (
    id           uuid        PRIMARY KEY,
    orden_id     bigint      NOT NULL REFERENCES campo.ordenes (id),
    texto        text        NOT NULL CHECK (length(texto) > 0),
    autor_id     bigint      REFERENCES nucleo.usuarios (id),
    autor_nombre text        NOT NULL DEFAULT '',
    canal        text        NOT NULL DEFAULT '',
    ocurrido_en  timestamptz NOT NULL,
    operacion_id uuid,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX orden_notas_orden_idx ON campo.orden_notas (orden_id, ocurrido_en);

-- Materiales consumidos (producto de Argos, desde la bodega del técnico). excede_existencia:
-- se usó más de lo que Argos decía que había (se acepta en Iris; el consumo en Argos queda
-- pendiente con error, contratos/sync.md §3).
CREATE TABLE campo.orden_materiales (
    id                uuid        PRIMARY KEY,
    orden_id          bigint        NOT NULL REFERENCES campo.ordenes (id),
    producto_id       bigint        NOT NULL,
    descripcion       text          NOT NULL DEFAULT '',
    cantidad          numeric(12,3) NOT NULL CHECK (cantidad > 0),
    bodega_id         bigint,
    excede_existencia boolean       NOT NULL DEFAULT false,
    registrado_por    bigint        REFERENCES nucleo.usuarios (id),
    canal             text          NOT NULL DEFAULT '',
    ocurrido_en       timestamptz   NOT NULL,
    operacion_id      uuid,
    created_at        timestamptz   NOT NULL DEFAULT now(),
    updated_at        timestamptz   NOT NULL DEFAULT now()
);
CREATE INDEX orden_materiales_orden_idx ON campo.orden_materiales (orden_id);

-- Equipos instalados (comodato) o retirados (con su condición), por unidad de Argos o serie.
CREATE TABLE campo.orden_equipos (
    id             uuid        PRIMARY KEY,
    orden_id       bigint      NOT NULL REFERENCES campo.ordenes (id),
    accion         text        NOT NULL CHECK (accion IN ('instalar', 'retirar')),
    unidad_id      bigint,
    serie          text,
    mac            text,
    modelo         text        NOT NULL DEFAULT '',
    condicion      text        CHECK (condicion IN ('buena', 'danada', 'perdido')),
    registrado_por bigint      REFERENCES nucleo.usuarios (id),
    canal          text        NOT NULL DEFAULT '',
    ocurrido_en    timestamptz NOT NULL,
    operacion_id   uuid,
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now(),
    CHECK (unidad_id IS NOT NULL OR serie IS NOT NULL),
    CHECK ((accion = 'retirar') = (condicion IS NOT NULL))
);
CREATE INDEX orden_equipos_orden_idx ON campo.orden_equipos (orden_id);
CREATE UNIQUE INDEX orden_equipos_serie_uq ON campo.orden_equipos (orden_id, accion, serie) WHERE serie IS NOT NULL;
CREATE UNIQUE INDEX orden_equipos_unidad_uq ON campo.orden_equipos (orden_id, accion, unidad_id) WHERE unidad_id IS NOT NULL;

-- Firma del cliente (una por orden; capturarla de nuevo la reemplaza). El SVG es dato sensible
-- (nunca en la auditoría); el PNG viaja como adjunto (adjunto_id).
CREATE TABLE campo.firmas (
    id            bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    orden_id      bigint      NOT NULL UNIQUE REFERENCES campo.ordenes (id),
    nombre        text        NOT NULL,
    documento     text        NOT NULL DEFAULT '',
    svg           text        NOT NULL,
    adjunto_id    uuid,
    capturada_por bigint      REFERENCES nucleo.usuarios (id),
    canal         text        NOT NULL DEFAULT '',
    ocurrido_en   timestamptz NOT NULL,
    operacion_id  uuid,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now()
);

-- Adjuntos registrados en la orden (adjunto.registrar). El id es el uuid del adjunto que crea
-- I1 (POST /adjuntos, tabla de adjuntos con su estado de subida): sin FK hasta que exista.
CREATE TABLE campo.orden_adjuntos (
    id             uuid        PRIMARY KEY,
    orden_id       bigint      NOT NULL REFERENCES campo.ordenes (id),
    tipo           text        NOT NULL CHECK (tipo IN ('foto', 'firma', 'documento')),
    item_id        bigint      REFERENCES campo.plantilla_items (id),
    descripcion    text        NOT NULL DEFAULT '',
    registrado_por bigint      REFERENCES nucleo.usuarios (id),
    canal          text        NOT NULL DEFAULT '',
    ocurrido_en    timestamptz NOT NULL,
    operacion_id   uuid,
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX orden_adjuntos_orden_idx ON campo.orden_adjuntos (orden_id);

-- Teléfonos de la app de campo. desfase_ms = hora del servidor − hora del teléfono, medido en
-- el último lote.
CREATE TABLE campo.dispositivos (
    id              text        PRIMARY KEY CHECK (length(id) BETWEEN 1 AND 100),
    usuario_id      bigint      NOT NULL REFERENCES nucleo.usuarios (id),
    plataforma      text        NOT NULL DEFAULT '',
    ultimo_contacto timestamptz NOT NULL,
    desfase_ms      bigint      NOT NULL DEFAULT 0,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX dispositivos_usuario_idx ON campo.dispositivos (usuario_id);

-- Operaciones del lote (POST /sync/lote): idempotencia por id (uuid v7 del teléfono). Una
-- aplicada responde «duplicada» con su resultado; una rechazada se vuelve a evaluar.
CREATE TABLE campo.operaciones_sync (
    id                   uuid        PRIMARY KEY,
    dispositivo_id       text        NOT NULL REFERENCES campo.dispositivos (id),
    usuario_id           bigint      NOT NULL REFERENCES nucleo.usuarios (id),
    orden_id             bigint,
    tipo                 text        NOT NULL,
    estado               text        NOT NULL CHECK (estado IN ('aplicada', 'rechazada')),
    resultado            jsonb       NOT NULL,
    version_base         integer,
    ocurrido_en_telefono timestamptz,
    ocurrido_en          timestamptz,
    created_at           timestamptz NOT NULL DEFAULT now(),
    updated_at           timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX operaciones_sync_orden_idx ON campo.operaciones_sync (orden_id) WHERE orden_id IS NOT NULL;

-- Feed de cambios por técnico (GET /campo/cambios?cursor=): una fila por cambio de una orden
-- que el técnico tiene (o tenía: reasignada). Se escribe con un candado por técnico
-- (pg_advisory_xact_lock), así los ids de un técnico quedan en orden de COMMIT.
CREATE TABLE campo.orden_cambios (
    id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    tecnico_id bigint      NOT NULL REFERENCES nucleo.usuarios (id),
    orden_id   bigint      NOT NULL REFERENCES campo.ordenes (id),
    tipo       text        NOT NULL CHECK (tipo IN ('nueva', 'actualizada', 'reprogramada', 'reasignada', 'cancelada')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX orden_cambios_tecnico_idx ON campo.orden_cambios (tecnico_id, id);

-- Semilla: plantillas y tipos razonables para un ISP (editables por SQL; se identifican por
-- codigo).
INSERT INTO campo.plantillas_checklist (codigo, nombre, tipo_codigo, requiere_firma) VALUES
    ('instalacion_fibra', 'Instalación de fibra (FTTH)', 'instalacion', true),
    ('instalacion_inalambrica', 'Instalación inalámbrica', 'instalacion', true),
    ('visita_tecnica', 'Visita técnica', 'visita', true),
    ('retiro', 'Retiro de servicio y equipos', 'retiro', true),
    ('cambio_equipo', 'Cambio de equipo', 'cambio_equipo', true),
    ('mantenimiento', 'Mantenimiento de red', 'mantenimiento', false);

INSERT INTO campo.plantilla_items (plantilla_id, posicion, codigo, etiqueta, tipo, obligatorio, unidad)
SELECT p.id, i.posicion, i.codigo, i.etiqueta, i.tipo, i.obligatorio, i.unidad
FROM campo.plantillas_checklist p
JOIN (VALUES
    ('instalacion_fibra', 1, 'acometida_tendida', 'Acometida de fibra tendida y fijada', 'si_no', true, ''),
    ('instalacion_fibra', 2, 'potencia_rx', 'Potencia óptica en la ONU', 'medicion', true, 'dBm'),
    ('instalacion_fibra', 3, 'onu_registrada', 'ONU registrada y en línea', 'si_no', true, ''),
    ('instalacion_fibra', 4, 'velocidad_bajada', 'Prueba de velocidad de bajada', 'medicion', true, 'Mbps'),
    ('instalacion_fibra', 5, 'velocidad_subida', 'Prueba de velocidad de subida', 'medicion', false, 'Mbps'),
    ('instalacion_fibra', 6, 'wifi_configurado', 'Wi-Fi configurado con el cliente', 'si_no', true, ''),
    ('instalacion_fibra', 7, 'foto_onu', 'Foto de la ONU instalada', 'foto', true, ''),
    ('instalacion_fibra', 8, 'foto_roseta', 'Foto de la roseta óptica', 'foto', false, ''),
    ('instalacion_fibra', 9, 'observaciones', 'Observaciones', 'texto', false, ''),
    ('instalacion_inalambrica', 1, 'antena_instalada', 'Antena instalada y asegurada', 'si_no', true, ''),
    ('instalacion_inalambrica', 2, 'senal', 'Nivel de señal', 'medicion', true, 'dBm'),
    ('instalacion_inalambrica', 3, 'ccq', 'Calidad del enlace (CCQ)', 'medicion', false, '%'),
    ('instalacion_inalambrica', 4, 'velocidad_bajada', 'Prueba de velocidad de bajada', 'medicion', true, 'Mbps'),
    ('instalacion_inalambrica', 5, 'cableado_protegido', 'Cableado protegido y aterrizado', 'si_no', true, ''),
    ('instalacion_inalambrica', 6, 'foto_antena', 'Foto de la antena', 'foto', true, ''),
    ('instalacion_inalambrica', 7, 'foto_router', 'Foto del router', 'foto', false, ''),
    ('instalacion_inalambrica', 8, 'observaciones', 'Observaciones', 'texto', false, ''),
    ('visita_tecnica', 1, 'diagnostico', 'Diagnóstico', 'texto', true, ''),
    ('visita_tecnica', 2, 'potencia_rx', 'Potencia o señal medida', 'medicion', false, 'dBm'),
    ('visita_tecnica', 3, 'velocidad_bajada', 'Prueba de velocidad de bajada', 'medicion', false, 'Mbps'),
    ('visita_tecnica', 4, 'problema_resuelto', 'Problema resuelto', 'si_no', true, ''),
    ('visita_tecnica', 5, 'foto_evidencia', 'Foto de evidencia', 'foto', false, ''),
    ('visita_tecnica', 6, 'observaciones', 'Observaciones', 'texto', false, ''),
    ('retiro', 1, 'equipos_recuperados', 'Equipos del cliente recuperados', 'si_no', true, ''),
    ('retiro', 2, 'acometida_retirada', 'Acometida retirada', 'si_no', false, ''),
    ('retiro', 3, 'foto_equipos', 'Foto de los equipos retirados', 'foto', true, ''),
    ('retiro', 4, 'observaciones', 'Observaciones', 'texto', false, ''),
    ('cambio_equipo', 1, 'equipo_anterior_retirado', 'Equipo anterior retirado', 'si_no', true, ''),
    ('cambio_equipo', 2, 'equipo_nuevo_operando', 'Equipo nuevo en línea', 'si_no', true, ''),
    ('cambio_equipo', 3, 'potencia_rx', 'Potencia o señal medida', 'medicion', false, 'dBm'),
    ('cambio_equipo', 4, 'foto_equipo_nuevo', 'Foto del equipo nuevo', 'foto', true, ''),
    ('cambio_equipo', 5, 'observaciones', 'Observaciones', 'texto', false, ''),
    ('mantenimiento', 1, 'trabajo_realizado', 'Trabajo realizado', 'texto', true, ''),
    ('mantenimiento', 2, 'minutos_corte', 'Minutos de corte provocado', 'numero', false, 'min'),
    ('mantenimiento', 3, 'foto_evidencia', 'Foto de evidencia', 'foto', false, ''),
    ('mantenimiento', 4, 'observaciones', 'Observaciones', 'texto', false, '')
) AS i (plantilla, posicion, codigo, etiqueta, tipo, obligatorio, unidad) ON i.plantilla = p.codigo;

INSERT INTO campo.tipos_orden (codigo, nombre, duracion_min, plantilla_id, exige_instalar, exige_retirar)
SELECT t.codigo, t.nombre, t.duracion_min, p.id, t.exige_instalar, t.exige_retirar
FROM (VALUES
    ('instalacion', 'Instalación', 120, 'instalacion_fibra', true, false),
    ('visita', 'Visita técnica', 60, 'visita_tecnica', false, false),
    ('retiro', 'Retiro', 45, 'retiro', false, true),
    ('cambio_equipo', 'Cambio de equipo', 60, 'cambio_equipo', true, true),
    ('mantenimiento', 'Mantenimiento', 90, 'mantenimiento', false, false)
) AS t (codigo, nombre, duracion_min, plantilla, exige_instalar, exige_retirar)
JOIN campo.plantillas_checklist p ON p.codigo = t.plantilla;

-- +goose Down
DROP TABLE campo.orden_cambios;
DROP TABLE campo.operaciones_sync;
DROP TABLE campo.dispositivos;
DROP TABLE campo.orden_adjuntos;
DROP TABLE campo.firmas;
DROP TABLE campo.orden_equipos;
DROP TABLE campo.orden_materiales;
DROP TABLE campo.orden_notas;
DROP TABLE campo.orden_checklist_historial;
DROP TABLE campo.orden_checklist;
DROP TABLE campo.orden_eventos;
DROP TABLE campo.ordenes;
DROP FUNCTION campo.siguiente_numero_orden();
DROP SEQUENCE campo.ordenes_numero_seq;
DROP TABLE campo.tipos_orden;
DROP TABLE campo.plantilla_items;
DROP TABLE campo.plantillas_checklist;
DROP SCHEMA campo;

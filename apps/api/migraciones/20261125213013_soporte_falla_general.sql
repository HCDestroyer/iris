-- +goose Up
-- Caídas repetidas y falla general (contrato Iris ↔ ArgosISP 0.13.0 §3.11; pedidos B29 y B30).
--
-- - red_ajustes: umbral de caídas por empresa (de fábrica 3 en 60 min) y de «posible falla general»
--   (más de 20 tickets de caídas en 10 min). Sin fila = los de fábrica.
-- - red_eventos: un registro por evento interno argos.caidas_repetidas / argos.falla_general_* procesado
--   (deduplicado por entrante_id): qué hizo soporte con él.
-- - incidentes_red: un ticket general por incidente de Argos y empresa (idempotente). Un incidente del
--   mismo alcance que reabre antes de 30 min reusa el ticket (reusa_de).
-- - incidente_red_servicios: los servicios afectados (ya validados por el conector).
-- - avisos_falla_clientes: un aviso de apertura y uno de cierre por cliente e incidente (y como máximo
--   un aviso de cierre cada 6 h por cliente).
-- - tickets.maestro: 'falla_general' (ticket general de un incidente de Argos) y 'posible_falla' (los
--   tickets de caídas agrupados cuando pasan del umbral). categorias.conectividad: las categorías que
--   absorbe una falla general.
ALTER TABLE soporte.tickets DROP CONSTRAINT tickets_maestro_check;
ALTER TABLE soporte.tickets ADD CONSTRAINT tickets_maestro_check
    CHECK (maestro IN ('router', 'infra', 'falla_general', 'posible_falla'));
ALTER TABLE soporte.tickets DROP CONSTRAINT tickets_maestro_datos_check;
ALTER TABLE soporte.tickets ADD CONSTRAINT tickets_maestro_datos_check CHECK (
    maestro IS NULL OR maestro IN ('falla_general', 'posible_falla') OR (maestro = 'router' AND router_id IS NOT NULL)
    OR (maestro = 'infra' AND infra_id IS NOT NULL));
CREATE UNIQUE INDEX tickets_posible_falla_uq ON soporte.tickets (coalesce(empresa_id, 0))
    WHERE maestro = 'posible_falla' AND estado IN ('nuevo', 'abierto', 'en_espera');
CREATE INDEX tickets_caidas_servicio_idx ON soporte.tickets (servicio_id, categoria_id)
    WHERE servicio_id IS NOT NULL AND estado IN ('nuevo', 'abierto', 'en_espera');

ALTER TABLE soporte.categorias ADD COLUMN conectividad boolean NOT NULL DEFAULT false;
INSERT INTO soporte.categorias (codigo, nombre, sla_id, requiere_orden, conectividad)
SELECT 'caidas_repetidas', 'Caídas repetidas', NULL, false, true
WHERE NOT EXISTS (SELECT 1 FROM soporte.categorias WHERE empresa_id IS NULL AND codigo = 'caidas_repetidas');
UPDATE soporte.categorias SET conectividad = true WHERE codigo IN ('sin_servicio', 'intermitencia', 'lentitud', 'caidas_repetidas');

CREATE TABLE soporte.red_ajustes (
    empresa_id         bigint      PRIMARY KEY,
    caidas_umbral      integer     NOT NULL DEFAULT 3 CHECK (caidas_umbral BETWEEN 1 AND 100),
    caidas_ventana_min integer     NOT NULL DEFAULT 60 CHECK (caidas_ventana_min BETWEEN 5 AND 1440),
    falla_umbral       integer     NOT NULL DEFAULT 20 CHECK (falla_umbral BETWEEN 2 AND 1000),
    -- Horas que un hijo espera la respuesta del cliente tras el cierre de la falla antes de volver a abierto.
    verificacion_horas integer     NOT NULL DEFAULT 24 CHECK (verificacion_horas BETWEEN 1 AND 168),
    version            integer     NOT NULL DEFAULT 1,
    actualizado_por    bigint      REFERENCES nucleo.usuarios (id),
    updated_at         timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE soporte.red_eventos (
    entrante_id bigint      PRIMARY KEY,
    evento      text        NOT NULL,
    empresa_id  bigint,
    resultado   text        NOT NULL,
    ticket_id   bigint      REFERENCES soporte.tickets (id),
    detalle     text,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE soporte.incidentes_red (
    id           bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    incidente_id text        NOT NULL CHECK (incidente_id ~ '^[A-Za-z0-9._:-]{1,100}$'),
    empresa_id   bigint      NOT NULL,
    alcance      text        NOT NULL CHECK (alcance IN ('router', 'olt', 'pon', 'zona')),
    alcance_id   text        NOT NULL CHECK (char_length(alcance_id) BETWEEN 1 AND 100),
    ticket_id    bigint      NOT NULL REFERENCES soporte.tickets (id),
    estado       text        NOT NULL DEFAULT 'abierta' CHECK (estado IN ('abierta', 'cerrada')),
    desde        timestamptz NOT NULL,
    hasta        timestamptz,
    cerrada_en   timestamptz,
    reusa_de     bigint      REFERENCES soporte.incidentes_red (id),
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    UNIQUE (incidente_id, empresa_id),
    CONSTRAINT incidentes_red_cerrada_check CHECK ((estado = 'cerrada') = (cerrada_en IS NOT NULL))
);
CREATE INDEX incidentes_red_alcance_idx ON soporte.incidentes_red (empresa_id, alcance, alcance_id, cerrada_en DESC);
CREATE INDEX incidentes_red_ticket_idx ON soporte.incidentes_red (ticket_id);

CREATE TABLE soporte.incidente_red_servicios (
    incidente   bigint NOT NULL REFERENCES soporte.incidentes_red (id),
    servicio_id bigint NOT NULL CHECK (servicio_id > 0),
    PRIMARY KEY (incidente, servicio_id)
);
CREATE INDEX incidente_red_servicios_servicio_idx ON soporte.incidente_red_servicios (servicio_id);

CREATE TABLE soporte.avisos_falla_clientes (
    incidente  bigint      NOT NULL REFERENCES soporte.incidentes_red (id),
    cliente_id bigint      NOT NULL,
    tipo       text        NOT NULL CHECK (tipo IN ('apertura', 'cierre')),
    ticket_id  bigint      NOT NULL REFERENCES soporte.tickets (id),
    enviado_en timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (incidente, cliente_id, tipo)
);
CREATE INDEX avisos_falla_clientes_cliente_idx ON soporte.avisos_falla_clientes (cliente_id, tipo, enviado_en DESC);

-- Un cierre que llegó antes que su apertura (Argos reintenta y puede desordenar): la apertura atrasada
-- ya no abre nada.
CREATE TABLE soporte.incidentes_red_cierres (
    incidente_id text        NOT NULL,
    empresa_id   bigint      NOT NULL,
    hasta        timestamptz,
    cerrado_en   timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (incidente_id, empresa_id)
);

-- Qué incidente absorbió cada hijo: el cierre y los avisos tocan solo esos.
CREATE TABLE soporte.incidente_red_hijos (
    incidente    bigint      NOT NULL REFERENCES soporte.incidentes_red (id),
    ticket_id    bigint      NOT NULL REFERENCES soporte.tickets (id),
    vinculado_en timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (incidente, ticket_id)
);
CREATE INDEX incidente_red_hijos_ticket_idx ON soporte.incidente_red_hijos (ticket_id);

-- Servicios de una falla general que el conector todavía tiene que leer en Argos (los que Iris no conoce
-- y los de la lectura paginada), en un job aparte y con su propio presupuesto.
CREATE TABLE integracion.argos_incidente_lecturas (
    entrante_id  bigint      PRIMARY KEY,
    conector_id  bigint      NOT NULL,
    incidente_id text        NOT NULL,
    empresa_id   bigint      NOT NULL,
    paginar      boolean     NOT NULL DEFAULT false,
    cursor       text,
    terminada_en timestamptz,
    created_at   timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE integracion.argos_incidente_servicios (
    entrante_id bigint  NOT NULL REFERENCES integracion.argos_incidente_lecturas (entrante_id),
    servicio_id bigint  NOT NULL CHECK (servicio_id > 0),
    estado      text    NOT NULL DEFAULT 'pendiente' CHECK (estado IN ('pendiente', 'valido', 'descartado', 'no_verificado')),
    intentos    integer NOT NULL DEFAULT 0,
    PRIMARY KEY (entrante_id, servicio_id)
);
CREATE INDEX argos_incidente_servicios_pendientes_idx ON integracion.argos_incidente_servicios (entrante_id) WHERE estado = 'pendiente';

-- Un hijo que no recibe el aviso (sin cliente, o el cliente ya lo recibió) queda registrado sin mensaje.
ALTER TABLE soporte.avisos_hijos ALTER COLUMN mensaje_id DROP NOT NULL;

-- Aviso a supervisión de una posible falla general (campana del panel).
ALTER TABLE notificaciones.avisos DROP CONSTRAINT avisos_tipo_check;
ALTER TABLE notificaciones.avisos ADD CONSTRAINT avisos_tipo_check
    CHECK (tipo IN ('asistente_tope', 'servidor_ia_caido', 'servidor_ia_incompatible', 'servidor_ia_umbral', 'argos_certificado',
                    'venta_revisada', 'paso_titular', 'contrato_sin_firma', 'posible_falla_general', 'verificacion_vencida'));

-- +goose Down
DELETE FROM notificaciones.avisos WHERE tipo IN ('posible_falla_general', 'verificacion_vencida');
ALTER TABLE notificaciones.avisos DROP CONSTRAINT avisos_tipo_check;
ALTER TABLE notificaciones.avisos ADD CONSTRAINT avisos_tipo_check
    CHECK (tipo IN ('asistente_tope', 'servidor_ia_caido', 'servidor_ia_incompatible', 'servidor_ia_umbral', 'argos_certificado',
                    'venta_revisada', 'paso_titular', 'contrato_sin_firma'));
DELETE FROM soporte.avisos_hijos WHERE mensaje_id IS NULL;
ALTER TABLE soporte.avisos_hijos ALTER COLUMN mensaje_id SET NOT NULL;
DROP TABLE integracion.argos_incidente_servicios;
DROP TABLE integracion.argos_incidente_lecturas;
DROP TABLE soporte.incidente_red_hijos;
DROP TABLE soporte.incidentes_red_cierres;
DROP TABLE soporte.avisos_falla_clientes;
DROP TABLE soporte.incidente_red_servicios;
DROP TABLE soporte.incidentes_red;
DROP TABLE soporte.red_eventos;
DROP TABLE soporte.red_ajustes;
ALTER TABLE soporte.categorias DROP COLUMN conectividad;
DROP INDEX soporte.tickets_caidas_servicio_idx;
DROP INDEX soporte.tickets_posible_falla_uq;
UPDATE soporte.tickets SET maestro = NULL WHERE maestro IN ('falla_general', 'posible_falla');
ALTER TABLE soporte.tickets DROP CONSTRAINT tickets_maestro_datos_check;
ALTER TABLE soporte.tickets ADD CONSTRAINT tickets_maestro_datos_check CHECK (
    maestro IS NULL OR (maestro = 'router' AND router_id IS NOT NULL) OR (maestro = 'infra' AND infra_id IS NOT NULL));
ALTER TABLE soporte.tickets DROP CONSTRAINT tickets_maestro_check;
ALTER TABLE soporte.tickets ADD CONSTRAINT tickets_maestro_check CHECK (maestro IN ('router', 'infra'));

-- +goose Up
-- Equipos del cliente y activación desde la orden (contrato argosisp.md 0.9.0 §12, pedido B12).
-- El técnico (también sin señal: orden.activar_servicio) o un agente desde el panel piden a Argos
-- que aprovisione y active el servicio (POST /servicios/:id/aprovisionar). El resultado llega por
-- los webhooks instalacion.aprovisionada / instalacion.fallida, o por la consulta a los 10 min.
--
-- Nunca se guarda la clave Wi-Fi aquí: viaja cifrada en integracion.datos_transito y se borra al
-- 2xx de Argos (o al rechazo, o a los 7 días). Iris nunca guarda credenciales de SmartOLT ni del ACS.

-- Modo de la activación (por empresa; la plantilla puede cambiarlo) y si el cierre exige la
-- bienvenida enviada (B16; solo con el modo exigir_lista).
ALTER TABLE campo.ajustes
    ADD COLUMN activacion_modo         text    NOT NULL DEFAULT 'al_cerrar'
        CHECK (activacion_modo IN ('al_cerrar', 'exigir_lista', 'pendiente_agente')),
    ADD COLUMN cierre_exige_bienvenida boolean NOT NULL DEFAULT false;

ALTER TABLE campo.plantillas_checklist
    ADD COLUMN activacion_modo         text
        CHECK (activacion_modo IN ('al_cerrar', 'exigir_lista', 'pendiente_agente')),
    ADD COLUMN cierre_exige_bienvenida boolean;

-- Cada intento de activación (iris:aprovisionar:<orden_id>:<intento>). Un fallido no es un rechazo:
-- «Reintentar» es otro intento (otra clave). Candados contra activar dos veces:
--   - un solo intento vivo (pendiente, en_proceso o listo) por orden y por servicio (índices únicos);
--   - la orden se toma con FOR UPDATE antes de crear el intento (la misma fila que toma el cierre);
--   - la clave de idempotencia es única (y Argos deduplica por ella, §3.1).
CREATE TABLE campo.activaciones (
    id                   uuid        PRIMARY KEY,
    orden_id             bigint      NOT NULL REFERENCES campo.ordenes (id),
    empresa_id           bigint,
    servicio_id          bigint      NOT NULL,
    intento              integer     NOT NULL CHECK (intento > 0),
    idempotency_key      text        NOT NULL UNIQUE,
    origen               text        NOT NULL CHECK (origen IN ('campo', 'panel')),
    pedido_por           bigint      REFERENCES nucleo.usuarios (id),
    estado               text        NOT NULL CHECK (estado IN ('pendiente', 'en_proceso', 'listo', 'fallido')),
    equipo_tipo          text        NOT NULL CHECK (equipo_tipo IN ('ont', 'cpe_wireless', 'router_tr069')),
    equipo_serie         text,
    equipo_mac           text,
    equipo_unidad_id     bigint,
    equipo_modelo        text,
    planta               jsonb,
    wireless             jsonb,
    wifi_ssid            text,
    con_clave_wifi       boolean     NOT NULL DEFAULT false,
    pendiente_id         bigint,
    aprovisionamiento_id bigint,
    causa                text,
    reintentable         boolean,
    detalle              text,
    pasos                jsonb,
    servicio_estado      text,
    consultado_en        timestamptz,
    resuelto_en          timestamptz,
    entrante_id          bigint,
    version              integer     NOT NULL DEFAULT 1,
    created_at           timestamptz NOT NULL DEFAULT now(),
    updated_at           timestamptz NOT NULL DEFAULT now(),
    UNIQUE (orden_id, intento)
);
CREATE UNIQUE INDEX activaciones_viva_orden_uq ON campo.activaciones (orden_id) WHERE estado IN ('pendiente', 'en_proceso', 'listo');
CREATE UNIQUE INDEX activaciones_viva_servicio_uq ON campo.activaciones (servicio_id) WHERE estado IN ('pendiente', 'en_proceso', 'listo');
CREATE INDEX activaciones_aprovisionamiento_idx ON campo.activaciones (aprovisionamiento_id) WHERE aprovisionamiento_id IS NOT NULL;
CREATE INDEX activaciones_en_proceso_idx ON campo.activaciones (updated_at) WHERE estado = 'en_proceso';

-- Lo de la orden que no es un intento: los contadores (intentos, reenvíos de la bienvenida), el
-- cierre sin activación (un supervisor con motivo, o «pendiente por agente») y la bienvenida (B16).
CREATE TABLE campo.orden_activacion (
    orden_id              bigint      PRIMARY KEY REFERENCES campo.ordenes (id),
    empresa_id            bigint,
    intentos              integer     NOT NULL DEFAULT 0,
    sin_activacion_motivo text,
    sin_activacion_por    bigint      REFERENCES nucleo.usuarios (id),
    bienvenida_canales    jsonb,
    bienvenida_en         timestamptz,
    bienvenida_reenvios   integer     NOT NULL DEFAULT 0,
    version               integer     NOT NULL DEFAULT 1,
    created_at            timestamptz NOT NULL DEFAULT now(),
    updated_at            timestamptz NOT NULL DEFAULT now()
);

-- Historial de la orden: los intentos de activación.
ALTER TABLE campo.orden_eventos
    DROP CONSTRAINT orden_eventos_tipo_check,
    ADD CONSTRAINT orden_eventos_tipo_check CHECK (tipo IN ('creada', 'estado', 'asignada', 'programada', 'formulario', 'activacion'));

-- El depósito de tránsito sirve también a la clave Wi-Fi pedida desde un ticket o una conversación
-- (§12.4): entidad dice de qué es orden_id (que guarda el id de esa entidad).
ALTER TABLE integracion.datos_transito
    ADD COLUMN entidad text NOT NULL DEFAULT 'orden' CHECK (entidad IN ('orden', 'ticket', 'conversacion'));
ALTER TABLE integracion.datos_transito DROP CONSTRAINT IF EXISTS datos_transito_motivo_borrado_check;
ALTER TABLE integracion.datos_transito ADD CONSTRAINT datos_transito_motivo_borrado_check
    CHECK (motivo_borrado IN ('entregado', 'vencido', 'resuelto_manual', 'cancelado', 'rechazado'));

-- Lo que Argos puede aprovisionar, por empresa y tecnología (0.9.0 §4.3, capacidades.aprovisionamiento):
-- [{ empresa_id, ftth, wireless, tr069 }]. null = no lo informó (Iris activa al cerrar).
ALTER TABLE integracion.conectores ADD COLUMN capacidades_aprovisionamiento jsonb;

-- Cambios de Wi-Fi pedidos a Argos (§12.4, POST /servicios/:id/wifi): desde la orden, el ticket o la
-- conversación (enlace de un solo uso del asistente, B14). Sin la clave (va en el depósito de
-- tránsito) ni el SSID.
CREATE TABLE integracion.cambios_wifi (
    id              uuid        PRIMARY KEY,
    empresa_id      bigint,
    servicio_id     bigint      NOT NULL,
    cliente_id      bigint      NOT NULL,
    origen_tipo     text        NOT NULL CHECK (origen_tipo IN ('iris_orden', 'iris_ticket', 'iris_conversacion')),
    origen_id       text        NOT NULL,
    actor           text        NOT NULL,
    usuario_id      bigint      REFERENCES nucleo.usuarios (id),
    metodo          text        NOT NULL,
    verificado_en   timestamptz NOT NULL,
    con_ssid        boolean     NOT NULL,
    con_clave       boolean     NOT NULL,
    idempotency_key text        NOT NULL UNIQUE,
    pendiente_id    bigint,
    solicitud_id    bigint,
    estado          text        NOT NULL CHECK (estado IN ('pendiente', 'en_proceso', 'aplicado', 'fallido')),
    causa           text,
    detalle         text,
    entrante_id     bigint,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX cambios_wifi_servicio_idx ON integracion.cambios_wifi (servicio_id, created_at);
CREATE INDEX cambios_wifi_solicitud_idx ON integracion.cambios_wifi (solicitud_id) WHERE solicitud_id IS NOT NULL;

-- +goose Down
DROP TABLE integracion.cambios_wifi;
ALTER TABLE integracion.conectores DROP COLUMN capacidades_aprovisionamiento;
UPDATE integracion.pendientes SET transito_id = NULL
WHERE transito_id IN (SELECT id FROM integracion.datos_transito WHERE entidad <> 'orden');
DELETE FROM integracion.datos_transito WHERE entidad <> 'orden';
UPDATE integracion.datos_transito SET motivo_borrado = 'vencido' WHERE motivo_borrado = 'rechazado';
ALTER TABLE integracion.datos_transito DROP CONSTRAINT datos_transito_motivo_borrado_check;
ALTER TABLE integracion.datos_transito ADD CONSTRAINT datos_transito_motivo_borrado_check
    CHECK (motivo_borrado IN ('entregado', 'vencido', 'resuelto_manual', 'cancelado'));
ALTER TABLE integracion.datos_transito DROP COLUMN entidad;
DROP TABLE campo.orden_activacion;
DROP TABLE campo.activaciones;
DELETE FROM campo.orden_eventos WHERE tipo = 'activacion';
ALTER TABLE campo.orden_eventos
    DROP CONSTRAINT orden_eventos_tipo_check,
    ADD CONSTRAINT orden_eventos_tipo_check CHECK (tipo IN ('creada', 'estado', 'asignada', 'programada', 'formulario'));
ALTER TABLE campo.plantillas_checklist DROP COLUMN cierre_exige_bienvenida, DROP COLUMN activacion_modo;
ALTER TABLE campo.ajustes DROP COLUMN cierre_exige_bienvenida, DROP COLUMN activacion_modo;

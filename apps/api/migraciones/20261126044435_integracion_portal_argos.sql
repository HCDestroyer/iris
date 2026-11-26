-- +goose Up
-- Portal del cliente de Argos (contrato argosisp.md 0.9.0 §11, bloque P; pedidos B8–B11, B13 y B28
-- de 0.12.1 §11.10). Argos llama a Iris servidor a servidor con el token_portal, en nombre de un
-- cliente que su portal ya identificó.

-- token_portal (§1.2): otro iris_pat_ del usuario de integración, recortado a portal_argos.*, que
-- vive mientras viva su token padre (el iris_pat_ que se entregó en el canje). Así sigue el mismo
-- ciclo que el del conector (vence, se confirma, se revoca y se elimina con él) y se rota con cada
-- canje sin otra máquina de estados.
ALTER TABLE nucleo.tokens_api
    ADD COLUMN token_padre_id bigint REFERENCES nucleo.tokens_api (id);
CREATE INDEX tokens_api_padre_idx ON nucleo.tokens_api (token_padre_id) WHERE token_padre_id IS NOT NULL;

-- Tickets: origen portal_argos (B13); el contacto que lo abrió (§11.10, B28); y si el cliente no se
-- pudo confirmar con Argos al crearlo (§11.2: Argos no respondía; se concilia al volver).
ALTER TABLE soporte.tickets DROP CONSTRAINT tickets_origen_check;
ALTER TABLE soporte.tickets ADD CONSTRAINT tickets_origen_check
    CHECK (origen IN ('panel', 'portal', 'portal_argos', 'correo', 'telegram', 'horus', 'argos', 'api'));
ALTER TABLE soporte.tickets
    -- contacto_id de Argos (core.contactos_cliente) que lo abrió desde el portal de Argos, o null.
    ADD COLUMN creado_por_contacto bigint,
    -- null = validado o no aplica; pendiente = Argos no respondió al crearlo; no_coincide = al
    -- conciliar, el cliente no era de la empresa (queda para revisión de una persona).
    ADD COLUMN cliente_revision text CHECK (cliente_revision IN ('pendiente', 'no_coincide'));
CREATE INDEX tickets_cliente_revision_idx ON soporte.tickets (id) WHERE cliente_revision = 'pendiente';
CREATE INDEX tickets_contacto_idx ON soporte.tickets (creado_por_contacto) WHERE creado_por_contacto IS NOT NULL;

-- Adjuntos: nombre del archivo, saneado (descarga del portal de Argos, §11.6).
ALTER TABLE nucleo.adjuntos ADD COLUMN nombre text CHECK (nombre IS NULL OR length(nombre) BETWEEN 1 AND 120);

-- Mensajes: el contacto que lo escribió por el portal de Argos (topes propios, §11.10).
ALTER TABLE soporte.ticket_mensajes ADD COLUMN contacto_id bigint;

-- Solicitudes de servicio (§11.7, B10): un prospecto con origen portal_argos, del cliente de
-- Argos conocido (cliente_existente_id), con su tipo y lo que ve el cliente (estado_cliente).
ALTER TABLE comercial.prospectos DROP CONSTRAINT prospectos_origen_check;
ALTER TABLE comercial.prospectos ADD CONSTRAINT prospectos_origen_check
    CHECK (origen IN ('panel', 'web', 'referido', 'argos', 'argos_panel', 'portal_argos', 'otro'));
ALTER TABLE comercial.prospectos
    ADD COLUMN solicitud_tipo text CHECK (solicitud_tipo IN ('alta', 'cambio_plan', 'traslado', 'otro')),
    ADD COLUMN solicitud_servicio_id bigint,
    ADD COLUMN solicitud_comentario text CHECK (solicitud_comentario IS NULL OR length(solicitud_comentario) <= 2000),
    ADD COLUMN estado_cliente text CHECK (estado_cliente IN ('recibida', 'en_revision', 'cotizada', 'en_proceso', 'completada', 'no_procede')),
    ADD CONSTRAINT prospectos_solicitud_check CHECK (
        (solicitud_tipo IS NULL) = (estado_cliente IS NULL)
        AND (solicitud_tipo IS NULL OR (origen = 'portal_argos' AND cliente_existente_id IS NOT NULL))
        AND (solicitud_tipo IS DISTINCT FROM 'alta' OR solicitud_servicio_id IS NULL)
        AND (solicitud_tipo NOT IN ('cambio_plan', 'traslado') OR solicitud_servicio_id IS NOT NULL));
-- Una solicitud abierta por cliente, tipo y servicio (alta: por tipo) en la empresa: 409 solicitud_abierta.
CREATE UNIQUE INDEX prospectos_solicitud_abierta_uq ON comercial.prospectos
    (empresa_id, cliente_existente_id, solicitud_tipo, coalesce(solicitud_servicio_id, 0))
    WHERE solicitud_tipo IS NOT NULL AND estado_cliente IN ('recibida', 'en_revision', 'cotizada', 'en_proceso');
CREATE INDEX prospectos_solicitudes_cliente_idx ON comercial.prospectos (empresa_id, cliente_existente_id, id DESC)
    WHERE solicitud_tipo IS NOT NULL;

-- Caché de contactos de Argos (§3.5 y §11.10, B28): la llena GET /clientes/:id/contactos al recibir
-- cliente.contactos_cambiados o una X-Iris-Contacto-Version mayor. Sin números de teléfono: el
-- portal solo necesita el nombre, los servicios y los permisos.
CREATE TABLE integracion.argos_contactos (
    contacto_id bigint      PRIMARY KEY,
    cliente_id  bigint      NOT NULL,
    empresa_id  bigint,
    nombre      text        NOT NULL DEFAULT '',
    -- null = todos los servicios del cliente.
    servicios   bigint[],
    consultar   boolean     NOT NULL DEFAULT false,
    reportar    boolean     NOT NULL DEFAULT false,
    version     integer     NOT NULL CHECK (version > 0),
    -- Quitado en Argos (accion: quitado, o ya no viene en la lista): deja de ver sus tickets.
    quitado     boolean     NOT NULL DEFAULT false,
    leido_en    timestamptz NOT NULL DEFAULT now(),
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX argos_contactos_cliente_idx ON integracion.argos_contactos (cliente_id);

-- Tope del conector (§11.2): 600 peticiones por minuto. Un contador por conector y minuto.
CREATE TABLE integracion.portal_argos_minutos (
    conector_id bigint      NOT NULL,
    minuto      timestamptz NOT NULL,
    peticiones  integer     NOT NULL,
    PRIMARY KEY (conector_id, minuto)
);

-- Webhooks a Argos (§6.6, 0.9.0): tickets y solicitudes de servicio (se conservan los de cuadrilla,
-- 20261125220001).
ALTER TABLE mensajeria.webhooks_salida DROP CONSTRAINT webhooks_salida_evento_check;
ALTER TABLE mensajeria.webhooks_salida ADD CONSTRAINT webhooks_salida_evento_check
    CHECK (evento IN ('aviso.entregado', 'aviso.fallido', 'contacto.baja', 'prospecto.estado_cambiado',
                      'cuadrilla.creada', 'cuadrilla.actualizada',
                      'ticket.estado_cambiado', 'ticket.respondido', 'solicitud_servicio.estado_cambiado'));

-- +goose Down
DELETE FROM mensajeria.webhooks_salida
WHERE evento IN ('ticket.estado_cambiado', 'ticket.respondido', 'solicitud_servicio.estado_cambiado');
ALTER TABLE mensajeria.webhooks_salida DROP CONSTRAINT webhooks_salida_evento_check;
ALTER TABLE mensajeria.webhooks_salida ADD CONSTRAINT webhooks_salida_evento_check
    CHECK (evento IN ('aviso.entregado', 'aviso.fallido', 'contacto.baja', 'prospecto.estado_cambiado',
                      'cuadrilla.creada', 'cuadrilla.actualizada'));
DROP TABLE integracion.portal_argos_minutos;
DROP TABLE integracion.argos_contactos;
DROP INDEX comercial.prospectos_solicitudes_cliente_idx;
DROP INDEX comercial.prospectos_solicitud_abierta_uq;
ALTER TABLE comercial.prospectos DROP CONSTRAINT prospectos_solicitud_check;
ALTER TABLE comercial.prospectos DROP COLUMN estado_cliente, DROP COLUMN solicitud_comentario,
    DROP COLUMN solicitud_servicio_id, DROP COLUMN solicitud_tipo;
UPDATE comercial.prospectos SET origen = 'argos' WHERE origen = 'portal_argos';
ALTER TABLE comercial.prospectos DROP CONSTRAINT prospectos_origen_check;
ALTER TABLE comercial.prospectos ADD CONSTRAINT prospectos_origen_check
    CHECK (origen IN ('panel', 'web', 'referido', 'argos', 'argos_panel', 'otro'));
ALTER TABLE soporte.ticket_mensajes DROP COLUMN contacto_id;
ALTER TABLE nucleo.adjuntos DROP COLUMN nombre;
DROP INDEX soporte.tickets_contacto_idx;
DROP INDEX soporte.tickets_cliente_revision_idx;
ALTER TABLE soporte.tickets DROP COLUMN cliente_revision, DROP COLUMN creado_por_contacto;
UPDATE soporte.tickets SET origen = 'portal' WHERE origen = 'portal_argos';
ALTER TABLE soporte.tickets DROP CONSTRAINT tickets_origen_check;
ALTER TABLE soporte.tickets ADD CONSTRAINT tickets_origen_check
    CHECK (origen IN ('panel', 'portal', 'correo', 'telegram', 'horus', 'argos', 'api'));
DROP INDEX nucleo.tokens_api_padre_idx;
ALTER TABLE nucleo.tokens_api DROP COLUMN token_padre_id;

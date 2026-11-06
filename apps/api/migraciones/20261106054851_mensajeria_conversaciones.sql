-- +goose Up
-- Bandeja de conversaciones con clientes (fase 1, sin IA; contratos/conversaciones.md):
-- varios números por empresa, colas, conversaciones por (empresa, número, contacto), mensajes
-- entrantes y salientes, notas internas con menciones, etiquetas, respuestas rápidas, contactos de
-- Iris, boletas y promesas pedidas a Argos y el feed de tiempo real.

-- Colas (alcance de los agentes y SLA de respuesta).
CREATE TABLE mensajeria.colas (
    id                  bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    empresa_id          bigint,
    nombre              text        NOT NULL CHECK (char_length(nombre) BETWEEN 1 AND 80),
    descripcion         text        NOT NULL DEFAULT '' CHECK (char_length(descripcion) <= 300),
    -- Minutos para contestar un mensaje del cliente (null = sin SLA) y cuánto antes de vencer
    -- pasa a «por vencer» (null = 25 % del SLA).
    sla_respuesta_min   integer CHECK (sla_respuesta_min IS NULL OR sla_respuesta_min BETWEEN 1 AND 10080),
    sla_por_vencer_min  integer CHECK (sla_por_vencer_min IS NULL OR sla_por_vencer_min >= 1),
    activa              boolean     NOT NULL DEFAULT true,
    version             integer     NOT NULL DEFAULT 1,
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX colas_nombre_uk ON mensajeria.colas (coalesce(empresa_id, 0), lower(nombre));

CREATE TABLE mensajeria.cola_miembros (
    cola_id    bigint      NOT NULL REFERENCES mensajeria.colas (id) ON DELETE CASCADE,
    usuario_id bigint      NOT NULL REFERENCES nucleo.usuarios (id),
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (cola_id, usuario_id)
);
CREATE INDEX cola_miembros_usuario_idx ON mensajeria.cola_miembros (usuario_id);

-- Varios números por empresa: usos, cola y el número de avisos de la empresa (uno activo).
DROP INDEX mensajeria.canales_activo_uk;
ALTER TABLE mensajeria.canales
    ADD COLUMN usos text[] NOT NULL DEFAULT '{avisos,soporte}'
        CHECK (cardinality(usos) BETWEEN 1 AND 4 AND usos <@ ARRAY['avisos', 'soporte', 'cobros', 'ventas']),
    ADD COLUMN avisos_empresa boolean NOT NULL DEFAULT false,
    ADD COLUMN cola_id bigint REFERENCES mensajeria.colas (id),
    ADD CONSTRAINT canales_avisos_uso CHECK (NOT avisos_empresa OR 'avisos' = ANY (usos));
-- El número que ya existía queda como el de avisos (y soporte): los avisos no cambian.
UPDATE mensajeria.canales SET avisos_empresa = true WHERE activo;
CREATE UNIQUE INDEX canales_avisos_uk ON mensajeria.canales (coalesce(empresa_id, 0)) WHERE activo AND avisos_empresa;

-- Etiquetas por empresa.
CREATE TABLE mensajeria.etiquetas (
    id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    empresa_id bigint,
    nombre     text        NOT NULL CHECK (char_length(nombre) BETWEEN 1 AND 40),
    color      text        NOT NULL DEFAULT 'neutro' CHECK (color IN ('neutro', 'marca', 'info', 'exito', 'alerta', 'peligro')),
    activa     boolean     NOT NULL DEFAULT true,
    version    integer     NOT NULL DEFAULT 1,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX etiquetas_nombre_uk ON mensajeria.etiquetas (coalesce(empresa_id, 0), lower(nombre));

-- Respuestas rápidas por empresa (y opcionalmente por cola).
CREATE TABLE mensajeria.respuestas_rapidas (
    id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    empresa_id bigint,
    cola_id    bigint REFERENCES mensajeria.colas (id),
    atajo      text        NOT NULL CHECK (atajo ~ '^/[a-z0-9_-]{1,30}$'),
    titulo     text        NOT NULL CHECK (char_length(titulo) BETWEEN 1 AND 80),
    texto      text        NOT NULL CHECK (char_length(texto) BETWEEN 1 AND 4000),
    activa     boolean     NOT NULL DEFAULT true,
    version    integer     NOT NULL DEFAULT 1,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX respuestas_rapidas_atajo_uk ON mensajeria.respuestas_rapidas (coalesce(empresa_id, 0), atajo);

-- Permisos de contacto por número (contactos de Iris que no son clientes de Argos). Solo inserción:
-- el vigente es el de ocurrido_en más reciente (a igual ocurrido_en, la baja manda).
CREATE TABLE mensajeria.consentimientos_numero (
    id             bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    empresa_id     bigint,
    telefono       text        NOT NULL CHECK (telefono ~ '^\+[1-9][0-9]{6,14}$'),
    estado         text        NOT NULL CHECK (estado IN ('acepta', 'baja')),
    origen         text        NOT NULL CHECK (origen IN ('cliente', 'agente')),
    ocurrido_en    timestamptz NOT NULL,
    registrado_por bigint REFERENCES nucleo.usuarios (id),
    created_at     timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX consentimientos_numero_idx ON mensajeria.consentimientos_numero (coalesce(empresa_id, 0), telefono, ocurrido_en DESC);

-- Contactos propios de Iris (proveedores, personal, quien aún no es cliente). No es una copia de
-- Argos: cliente_id solo se llena al unirlo a la ficha de Argos.
CREATE TABLE mensajeria.contactos_iris (
    id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    empresa_id bigint,
    nombre     text        NOT NULL CHECK (char_length(nombre) BETWEEN 1 AND 120),
    tipo       text        NOT NULL CHECK (tipo IN ('proveedor', 'personal', 'prospecto', 'otro')),
    telefono   text        NOT NULL CHECK (telefono ~ '^\+[1-9][0-9]{7,14}$'),
    nota       text        NOT NULL DEFAULT '' CHECK (char_length(nota) <= 500),
    cliente_id bigint CHECK (cliente_id IS NULL OR cliente_id > 0),
    unido_en   timestamptz,
    creado_por bigint REFERENCES nucleo.usuarios (id),
    version    integer     NOT NULL DEFAULT 1,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX contactos_iris_telefono_idx ON mensajeria.contactos_iris (coalesce(empresa_id, 0), telefono);

-- Conversaciones: una por (empresa, número, contacto). Nunca se cierran: se resuelven y se reabren.
CREATE TABLE mensajeria.conversaciones (
    id                 bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    empresa_id         bigint,
    canal_id           bigint      NOT NULL REFERENCES mensajeria.canales (id),
    -- Teléfono del contacto en E.164 (wa_id de Meta con «+»).
    numero             text        NOT NULL CHECK (numero ~ '^\+[1-9][0-9]{6,14}$'),
    perfil_nombre      text CHECK (perfil_nombre IS NULL OR char_length(perfil_nombre) <= 120),
    cola_id            bigint REFERENCES mensajeria.colas (id),
    asignado_a         bigint REFERENCES nucleo.usuarios (id),
    asignado_en        timestamptz,
    estado             text        NOT NULL DEFAULT 'abierta' CHECK (estado IN ('abierta', 'esperando_cliente', 'resuelta')),
    -- Contacto: cliente de Argos (solo el id), contacto de Iris o prospecto de comercial.
    cliente_id         bigint CHECK (cliente_id IS NULL OR cliente_id > 0),
    servicio_id        bigint CHECK (servicio_id IS NULL OR servicio_id > 0),
    contacto_iris_id   bigint REFERENCES mensajeria.contactos_iris (id),
    prospecto_id       bigint REFERENCES comercial.prospectos (id),
    asociacion         text CHECK (asociacion IS NULL OR asociacion IN ('automatica', 'manual')),
    -- El número coincide con un teléfono del cliente en la instantánea (o lo confirmó un
    -- supervisor). Sin esto no hay plantillas fuera de ventana, boletas ni promesas a su nombre.
    cliente_verificado boolean     NOT NULL DEFAULT false,
    verificado_por     bigint REFERENCES nucleo.usuarios (id),
    -- Ventana de 24 h: último mensaje del cliente.
    ultimo_entrante_en timestamptz,
    -- SLA: primer mensaje del cliente sin contestar (null = nada pendiente) y su vencimiento.
    espera_desde       timestamptz,
    sla_vence_en       timestamptz,
    sla_avisado        boolean     NOT NULL DEFAULT false,
    resuelta_en        timestamptz,
    reabierta_en       timestamptz,
    ultimo_seq         bigint      NOT NULL DEFAULT 0,
    ultimo_mensaje_id  uuid,
    ultima_actividad   timestamptz NOT NULL,
    version            integer     NOT NULL DEFAULT 1,
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now(),
    CHECK (servicio_id IS NULL OR cliente_id IS NOT NULL)
);
CREATE UNIQUE INDEX conversaciones_contacto_uk ON mensajeria.conversaciones (coalesce(empresa_id, 0), canal_id, numero);
CREATE INDEX conversaciones_actividad_idx ON mensajeria.conversaciones (empresa_id, ultima_actividad DESC, id DESC);
CREATE INDEX conversaciones_asignado_idx ON mensajeria.conversaciones (asignado_a) WHERE asignado_a IS NOT NULL;
CREATE INDEX conversaciones_cliente_idx ON mensajeria.conversaciones (cliente_id) WHERE cliente_id IS NOT NULL;
CREATE INDEX conversaciones_sla_idx ON mensajeria.conversaciones (sla_vence_en) WHERE sla_vence_en IS NOT NULL AND NOT sla_avisado;

CREATE TABLE mensajeria.conversacion_etiquetas (
    conversacion_id bigint      NOT NULL REFERENCES mensajeria.conversaciones (id) ON DELETE CASCADE,
    etiqueta_id     bigint      NOT NULL REFERENCES mensajeria.etiquetas (id),
    created_at      timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (conversacion_id, etiqueta_id)
);
CREATE INDEX conversacion_etiquetas_etiqueta_idx ON mensajeria.conversacion_etiquetas (etiqueta_id);

-- Participantes: quienes la ven sin ser de su cola (mencionados; guardia en la PR B).
CREATE TABLE mensajeria.conversacion_participantes (
    conversacion_id bigint      NOT NULL REFERENCES mensajeria.conversaciones (id) ON DELETE CASCADE,
    usuario_id      bigint      NOT NULL REFERENCES nucleo.usuarios (id),
    origen          text        NOT NULL CHECK (origen IN ('mencion', 'guardia')),
    created_at      timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (conversacion_id, usuario_id)
);
CREATE INDEX conversacion_participantes_usuario_idx ON mensajeria.conversacion_participantes (usuario_id);

-- Mensajes: entrantes (cliente), salientes (agente o sistema), notas internas y eventos de sistema.
-- El contenido nunca va a auditoría (solo id y hash), igual que el chat interno.
CREATE TABLE mensajeria.conversacion_mensajes (
    id              uuid PRIMARY KEY,
    conversacion_id bigint      NOT NULL REFERENCES mensajeria.conversaciones (id),
    empresa_id      bigint,
    seq             bigint      NOT NULL CHECK (seq > 0),
    direccion       text        NOT NULL CHECK (direccion IN ('entrante', 'saliente', 'nota', 'sistema')),
    tipo            text        NOT NULL CHECK (tipo IN ('texto', 'imagen', 'documento', 'audio', 'video', 'ubicacion', 'plantilla', 'contacto', 'sistema')),
    texto           text CHECK (texto IS NULL OR char_length(texto) <= 4096),
    adjunto_id      uuid REFERENCES nucleo.adjuntos (id),
    -- Medio entrante aún por descargar de Meta (media id).
    medio_id        text,
    lat             double precision CHECK (lat IS NULL OR lat BETWEEN -90 AND 90),
    lon             double precision CHECK (lon IS NULL OR lon BETWEEN -180 AND 180),
    plantilla       text,
    variables       jsonb,
    autor_id        bigint REFERENCES nucleo.usuarios (id),
    menciones       bigint[]    NOT NULL DEFAULT '{}',
    -- Mensaje de sistema: { tipo, referencia, entidad_id, actor_id, … }.
    evento          jsonb,
    respuesta_rapida_id bigint REFERENCES mensajeria.respuestas_rapidas (id),
    -- Saliente: estado de entrega en WhatsApp.
    estado_entrega  text CHECK (estado_entrega IS NULL OR estado_entrega IN ('en_cola', 'enviado', 'entregado', 'leido', 'fallido')),
    motivo_falla    text,
    detalle_falla   text CHECK (detalle_falla IS NULL OR char_length(detalle_falla) <= 300),
    proveedor_id    text,
    intentos        integer     NOT NULL DEFAULT 0,
    hash_contenido  bytea,
    idempotency_key text CHECK (idempotency_key IS NULL OR length(idempotency_key) BETWEEN 1 AND 255),
    creado_en       timestamptz NOT NULL,
    enviado_en      timestamptz,
    entregado_en    timestamptz,
    leido_en        timestamptz,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    CHECK ((lat IS NULL) = (lon IS NULL)),
    CHECK ((direccion = 'saliente') = (estado_entrega IS NOT NULL)),
    CHECK ((direccion = 'sistema') = (evento IS NOT NULL))
);
CREATE UNIQUE INDEX conversacion_mensajes_seq_uk ON mensajeria.conversacion_mensajes (conversacion_id, seq);
CREATE UNIQUE INDEX conversacion_mensajes_proveedor_uk ON mensajeria.conversacion_mensajes (conversacion_id, proveedor_id) WHERE proveedor_id IS NOT NULL;
CREATE INDEX conversacion_mensajes_wamid_idx ON mensajeria.conversacion_mensajes (proveedor_id) WHERE proveedor_id IS NOT NULL;
CREATE UNIQUE INDEX conversacion_mensajes_clave_uk ON mensajeria.conversacion_mensajes (autor_id, idempotency_key) WHERE idempotency_key IS NOT NULL;
CREATE INDEX conversacion_mensajes_medio_idx ON mensajeria.conversacion_mensajes (created_at) WHERE medio_id IS NOT NULL AND adjunto_id IS NULL;

-- Hasta dónde leyó cada usuario (no leídos = mensajes del cliente después de su puntero).
CREATE TABLE mensajeria.conversacion_lecturas (
    conversacion_id bigint      NOT NULL REFERENCES mensajeria.conversaciones (id) ON DELETE CASCADE,
    usuario_id      bigint      NOT NULL REFERENCES nucleo.usuarios (id),
    hasta_seq       bigint      NOT NULL CHECK (hasta_seq > 0),
    leido_en        timestamptz NOT NULL,
    PRIMARY KEY (conversacion_id, usuario_id)
);

-- Lo que se creó desde la conversación (ticket, orden, prospecto, boleta, promesa): para mostrarlo
-- y para buscar por su referencia (TK-004812).
CREATE TABLE mensajeria.conversacion_vinculos (
    conversacion_id bigint      NOT NULL REFERENCES mensajeria.conversaciones (id) ON DELETE CASCADE,
    tipo            text        NOT NULL CHECK (tipo IN ('ticket', 'orden', 'prospecto', 'boleta', 'promesa')),
    entidad_id      text        NOT NULL,
    referencia      text        NOT NULL,
    creado_por      bigint REFERENCES nucleo.usuarios (id),
    created_at      timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (conversacion_id, tipo, entidad_id)
);
CREATE INDEX conversacion_vinculos_referencia_idx ON mensajeria.conversacion_vinculos (lower(referencia));

-- Boletas del cliente que Iris manda a Argos (POST /comprobantes, contrato §7.2) en un job.
CREATE TABLE mensajeria.boletas (
    id                 uuid PRIMARY KEY,
    empresa_id         bigint,
    conversacion_id    bigint      NOT NULL REFERENCES mensajeria.conversaciones (id),
    cliente_id         bigint      NOT NULL CHECK (cliente_id > 0),
    servicio_id        bigint,
    adjunto_id         uuid        NOT NULL REFERENCES nucleo.adjuntos (id),
    metodo             text        NOT NULL CHECK (metodo IN ('deposito', 'transferencia', 'ach', 'remesa')),
    cuenta_bancaria_id bigint      NOT NULL CHECK (cuenta_bancaria_id > 0),
    banco_origen_id    bigint,
    referencia         text        NOT NULL CHECK (char_length(referencia) BETWEEN 1 AND 60),
    monto              numeric(14, 2) NOT NULL CHECK (monto > 0),
    fecha_deposito     date        NOT NULL,
    nota               text CHECK (nota IS NULL OR char_length(nota) <= 500),
    estado             text        NOT NULL DEFAULT 'por_enviar' CHECK (estado IN ('por_enviar', 'registrada', 'fallida', 'aprobada', 'rechazada')),
    comprobante_id     bigint,
    detalle            text CHECK (detalle IS NULL OR char_length(detalle) <= 300),
    intentos           integer     NOT NULL DEFAULT 0,
    proximo_intento    timestamptz NOT NULL DEFAULT now(),
    registrada_por     bigint REFERENCES nucleo.usuarios (id),
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX boletas_por_enviar_idx ON mensajeria.boletas (proximo_intento) WHERE estado = 'por_enviar';

-- Promesas de pago pedidas a Argos (POST /promesas, contrato §7.4).
CREATE TABLE mensajeria.promesas (
    id              uuid PRIMARY KEY,
    empresa_id      bigint,
    conversacion_id bigint      NOT NULL REFERENCES mensajeria.conversaciones (id),
    cliente_id      bigint      NOT NULL CHECK (cliente_id > 0),
    servicio_id     bigint      NOT NULL CHECK (servicio_id > 0),
    vence_el        date        NOT NULL,
    estado          text        NOT NULL CHECK (estado IN ('activa', 'rechazada', 'pendiente_aprobacion')),
    promesa_argos_id bigint,
    monto           text,
    regla           text,
    motivo          text CHECK (motivo IS NULL OR char_length(motivo) <= 300),
    idempotency_key text CHECK (idempotency_key IS NULL OR length(idempotency_key) BETWEEN 1 AND 255),
    hash_peticion   bytea,
    solicitada_por  bigint REFERENCES nucleo.usuarios (id),
    created_at      timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX promesas_clave_uk ON mensajeria.promesas (solicitada_por, idempotency_key) WHERE idempotency_key IS NOT NULL;

-- Feed de tiempo real de la bandeja (mismo patrón que chat_eventos): NOTIFY iris_conversaciones
-- '<empresa o 0>' y candado de la empresa hasta el COMMIT.
CREATE TABLE mensajeria.conv_eventos (
    id              bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    empresa_id      bigint,
    conversacion_id bigint      NOT NULL REFERENCES mensajeria.conversaciones (id),
    tipo            text        NOT NULL CHECK (tipo IN ('mensaje_nuevo', 'mensaje_actualizado', 'conversacion_actualizada', 'mencion', 'guardia')),
    mensaje_id      uuid,
    -- Solo para él (mencion, guardia).
    usuario_id      bigint,
    ocurrido_en     timestamptz NOT NULL,
    created_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX conv_eventos_empresa_idx ON mensajeria.conv_eventos (empresa_id, id);
CREATE INDEX conv_eventos_ocurrido_idx ON mensajeria.conv_eventos (ocurrido_en);

ALTER TABLE mensajeria.chat_estado ADD COLUMN conv_purgados_hasta bigint NOT NULL DEFAULT 0;

-- Conversaciones que ve un usuario en la empresa: supervisión (todas), asignadas a él, de sus
-- colas, sin cola, o en las que participa (mencionado o de guardia).
-- +goose StatementBegin
CREATE FUNCTION mensajeria.conv_visible(p_usuario bigint, p_supervisa boolean, p_conv bigint, p_cola bigint, p_asignado bigint)
RETURNS boolean LANGUAGE sql STABLE AS $$
    SELECT p_supervisa
        OR p_asignado = p_usuario
        OR p_cola IS NULL
        OR EXISTS (SELECT 1 FROM mensajeria.cola_miembros m WHERE m.cola_id = p_cola AND m.usuario_id = p_usuario)
        OR EXISTS (SELECT 1 FROM mensajeria.conversacion_participantes p WHERE p.conversacion_id = p_conv AND p.usuario_id = p_usuario)
$$;
-- +goose StatementEnd

-- +goose Down
DROP FUNCTION mensajeria.conv_visible(bigint, boolean, bigint, bigint, bigint);
ALTER TABLE mensajeria.chat_estado DROP COLUMN conv_purgados_hasta;
DROP TABLE mensajeria.conv_eventos;
DROP TABLE mensajeria.promesas;
DROP TABLE mensajeria.boletas;
DROP TABLE mensajeria.conversacion_vinculos;
DROP TABLE mensajeria.conversacion_lecturas;
DROP TABLE mensajeria.conversacion_mensajes;
DROP TABLE mensajeria.conversacion_participantes;
DROP TABLE mensajeria.conversacion_etiquetas;
DROP TABLE mensajeria.conversaciones;
DROP TABLE mensajeria.contactos_iris;
DROP TABLE mensajeria.consentimientos_numero;
DROP TABLE mensajeria.respuestas_rapidas;
DROP TABLE mensajeria.etiquetas;
DROP INDEX mensajeria.canales_avisos_uk;
ALTER TABLE mensajeria.canales DROP CONSTRAINT canales_avisos_uso, DROP COLUMN cola_id, DROP COLUMN avisos_empresa, DROP COLUMN usos;
DROP TABLE mensajeria.cola_miembros;
DROP TABLE mensajeria.colas;
CREATE UNIQUE INDEX canales_activo_uk ON mensajeria.canales (coalesce(empresa_id, 0), canal) WHERE activo;

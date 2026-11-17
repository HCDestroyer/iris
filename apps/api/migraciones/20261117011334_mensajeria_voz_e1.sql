-- +goose Up
-- Voz, etapa E1 (contratos/voz.md): personas contestan en el panel las llamadas entrantes de
-- WhatsApp. Todo en el schema mensajeria: una llamada es un hecho de una conversación.

CREATE TABLE mensajeria.llamadas (
    id                uuid PRIMARY KEY,
    empresa_id        bigint,
    origen            text        NOT NULL CHECK (origen IN ('whatsapp', 'centralita')),
    canal_id          bigint REFERENCES mensajeria.canales (id),
    conversacion_id   bigint REFERENCES mensajeria.conversaciones (id),
    mensaje_id        uuid,
    id_externo        text        NOT NULL CHECK (char_length(id_externo) BETWEEN 1 AND 200),
    direccion         text        NOT NULL CHECK (direccion IN ('entrante', 'saliente')),
    numero            text        NOT NULL,
    cliente_id        bigint,
    servicio_id       bigint,
    cola_id           bigint,
    estado            text        NOT NULL CHECK (estado IN ('sonando', 'marcando', 'conectando', 'en_curso', 'terminada', 'perdida',
                                                             'rechazada', 'fuera_de_horario', 'fallida')),
    atendida_por      text CHECK (atendida_por IN ('persona', 'asistente')),
    usuario_id        bigint REFERENCES nucleo.usuarios (id),
    reclamada_por     bigint REFERENCES nucleo.usuarios (id),
    reclamada_hasta   timestamptz,
    motivo_fin        text CHECK (motivo_fin IN ('cliente_colgo', 'agente_colgo', 'asistente_termino', 'nadie_contesto', 'rechazada_agente',
        'fuera_de_horario', 'sin_capacidad', 'motor_no_disponible', 'duracion_maxima', 'silencio', 'mal_uso', 'tope', 'limite', 'error_medio',
        'error_meta', 'sin_permiso', 'transferencia_fallida')),
    sono_en           timestamptz NOT NULL,
    -- Hasta cuándo suena (sono_en + segundosSonando del número al llegar).
    suena_hasta       timestamptz NOT NULL,
    contestada_en     timestamptz,
    terminada_en      timestamptz,
    duracion_seg      integer CHECK (duracion_seg IS NULL OR duracion_seg >= 0),
    nota              text CHECK (nota IS NULL OR char_length(nota) <= 2000),
    resultado         text CHECK (resultado IN ('resuelto', 'ticket_creado', 'devolver', 'equivocado', 'otro')),
    grabada           boolean     NOT NULL DEFAULT false,
    -- Solo mientras suena o se conecta; se borra al terminar. Nunca va a auditoría ni eventos.
    sdp_oferta        text,
    -- Dos fases con la Graph API (§3.3): la intención se anota antes de llamar a Meta.
    accion_meta        text CHECK (accion_meta IN ('pre_accept', 'accept', 'reject', 'terminate', 'connect')),
    accion_meta_estado text CHECK (accion_meta_estado IN ('pendiente', 'hecha', 'fallida')),
    accion_meta_en     timestamptz,
    -- pre_accept aceptado por Meta: desde aquí corren los 5 s para confirmar el medio.
    pre_aceptada_en   timestamptz,
    -- Ya salió el mensaje «No pudimos contestarte» por esta llamada.
    aviso_perdida     boolean     NOT NULL DEFAULT false,
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now(),
    CHECK (origen <> 'whatsapp' OR canal_id IS NOT NULL)
);
-- Deduplicación: un call_id de Meta por número.
CREATE UNIQUE INDEX llamadas_externo_uq ON mensajeria.llamadas (origen, canal_id, id_externo);
CREATE INDEX llamadas_conversacion_idx ON mensajeria.llamadas (conversacion_id, sono_en DESC);
CREATE INDEX llamadas_empresa_idx ON mensajeria.llamadas ((coalesce(empresa_id, 0)), sono_en DESC, id);
CREATE INDEX llamadas_vivas_idx ON mensajeria.llamadas (estado) WHERE estado IN ('sonando', 'marcando', 'conectando', 'en_curso');
CREATE INDEX llamadas_numero_idx ON mensajeria.llamadas (canal_id, numero, sono_en);

CREATE TABLE mensajeria.llamada_tramos (
    id           bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    llamada_id   uuid        NOT NULL REFERENCES mensajeria.llamadas (id),
    empresa_id   bigint,
    orden        integer     NOT NULL,
    tipo         text        NOT NULL CHECK (tipo IN ('asistente', 'persona')),
    usuario_id   bigint REFERENCES nucleo.usuarios (id),
    motor_id     bigint,
    inicio       timestamptz NOT NULL,
    fin          timestamptz,
    duracion_seg integer,
    motivo_fin   text CHECK (motivo_fin IN ('fin_llamada', 'paso_a_persona', 'tomada', 'transferida', 'error')),
    turnos       integer,
    puente       boolean     NOT NULL DEFAULT false,
    UNIQUE (llamada_id, orden)
);

-- A quién se le ofreció la llamada: solo un candidato vivo puede reclamarla (§3.3).
CREATE TABLE mensajeria.llamada_candidatos (
    llamada_id    uuid        NOT NULL REFERENCES mensajeria.llamadas (id),
    empresa_id    bigint,
    usuario_id    bigint      NOT NULL REFERENCES nucleo.usuarios (id),
    desde         timestamptz NOT NULL,
    retirado_en   timestamptz,
    motivo_retiro text CHECK (motivo_retiro IN ('rechazo', 'solto', 'fin', 'no_disponible')),
    -- Reservas soltadas o vencidas: a la segunda deja de recibir esta llamada.
    sueltas       integer     NOT NULL DEFAULT 0,
    PRIMARY KEY (llamada_id, usuario_id)
);

-- «Recibir llamadas»: un interruptor por usuario y navegador (§3.4). visto_en lo renueva el latido
-- del flujo SSE del usuario: sin un flujo vivo no es candidato.
CREATE TABLE mensajeria.llamada_receptores (
    id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    empresa_id bigint,
    usuario_id bigint      NOT NULL REFERENCES nucleo.usuarios (id),
    navegador  text        NOT NULL CHECK (navegador ~ '^[A-Za-z0-9_-]{8,64}$'),
    recibe     boolean     NOT NULL DEFAULT true,
    visto_en   timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX llamada_receptores_uq ON mensajeria.llamada_receptores ((coalesce(empresa_id, 0)), usuario_id, navegador);

-- Ajustes de voz de la empresa. E1 guarda los que usa E1; E2 amplía la tabla.
CREATE TABLE mensajeria.voz_ajustes (
    id                       bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    empresa_id               bigint,
    grabar                   text        NOT NULL DEFAULT 'nunca' CHECK (grabar IN ('nunca', 'todas', 'asistente')),
    duracion_max_persona_min integer     NOT NULL DEFAULT 60 CHECK (duracion_max_persona_min BETWEEN 10 AND 120),
    segundos_asignado        integer     NOT NULL DEFAULT 10 CHECK (segundos_asignado BETWEEN 0 AND 20),
    retencion_grabaciones_dias integer   NOT NULL DEFAULT 30 CHECK (retencion_grabaciones_dias BETWEEN 1 AND 365),
    version                  integer     NOT NULL DEFAULT 1,
    created_at               timestamptz NOT NULL DEFAULT now(),
    updated_at               timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX voz_ajustes_empresa_uq ON mensajeria.voz_ajustes ((coalesce(empresa_id, 0)));

-- Ajustes de llamadas de cada número (solo números por API).
CREATE TABLE mensajeria.voz_ajustes_canal (
    canal_id         bigint PRIMARY KEY REFERENCES mensajeria.canales (id),
    empresa_id       bigint,
    llamadas_activas boolean     NOT NULL DEFAULT false,
    requisitos       jsonb,
    cola_id          bigint,
    horario_id       bigint,
    segundos_sonando integer     NOT NULL DEFAULT 30 CHECK (segundos_sonando BETWEEN 15 AND 45),
    fuera_de_horario text        NOT NULL DEFAULT 'rechazar_con_mensaje' CHECK (fuera_de_horario IN ('rechazar_con_mensaje', 'buzon', 'asistente')),
    icono_visible    boolean     NOT NULL DEFAULT true,
    devolucion       boolean     NOT NULL DEFAULT false,
    horas_meta       jsonb,
    -- Mensaje automático al cliente por una llamada perdida (una vez por hora y conversación).
    mensaje_perdida  boolean     NOT NULL DEFAULT true,
    mensaje_perdida_texto text   NOT NULL DEFAULT 'No pudimos contestar tu llamada. Escríbenos por aquí y te atendemos.'
                                 CHECK (char_length(mensaje_perdida_texto) BETWEEN 1 AND 500),
    sincronizado_en  timestamptz,
    version          integer     NOT NULL DEFAULT 1,
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE mensajeria.llamada_grabaciones (
    id             bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    llamada_id     uuid        NOT NULL REFERENCES mensajeria.llamadas (id),
    empresa_id     bigint,
    origen         text        NOT NULL CHECK (origen IN ('meta', 'centralita')),
    adjunto_id     uuid,
    duracion_seg   integer,
    sha256         text,
    estado         text        NOT NULL CHECK (estado IN ('pendiente', 'disponible', 'fallida', 'borrada')),
    -- De dónde se baja (solo hosts de la lista cerrada); se borra al terminar la descarga.
    url_descarga   text,
    vence_en       timestamptz,
    borrada_en     timestamptz,
    borrada_por    bigint,
    motivo_borrado text CHECK (motivo_borrado IS NULL OR char_length(motivo_borrado) <= 300),
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now(),
    UNIQUE (llamada_id)
);

-- «Por devolver»: una pendiente por conversación.
CREATE TABLE mensajeria.llamada_devoluciones (
    id                  bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    empresa_id          bigint,
    conversacion_id     bigint      NOT NULL REFERENCES mensajeria.conversaciones (id),
    llamada_id          uuid        NOT NULL REFERENCES mensajeria.llamadas (id),
    pedida_por          text        NOT NULL CHECK (pedida_por IN ('sistema', 'asistente', 'cliente')),
    estado              text        NOT NULL CHECK (estado IN ('pendiente', 'hecha', 'descartada', 'vencida')),
    atendida_por        bigint,
    llamada_devuelta_id uuid,
    motivo              text CHECK (motivo IS NULL OR char_length(motivo) <= 300),
    vence_en            timestamptz NOT NULL,
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX llamada_devoluciones_pendiente_uq ON mensajeria.llamada_devoluciones (conversacion_id) WHERE estado = 'pendiente';

-- La llamada en el hilo y en el feed.
ALTER TABLE mensajeria.conversacion_mensajes ADD COLUMN llamada_id uuid;
ALTER TABLE mensajeria.conversacion_mensajes DROP CONSTRAINT conversacion_mensajes_tipo_check;
ALTER TABLE mensajeria.conversacion_mensajes ADD CONSTRAINT conversacion_mensajes_tipo_check
    CHECK (tipo IN ('texto', 'imagen', 'documento', 'audio', 'video', 'ubicacion', 'plantilla', 'contacto', 'sistema', 'llamada'));
ALTER TABLE mensajeria.conv_eventos ADD COLUMN llamada_id uuid;
ALTER TABLE mensajeria.conv_eventos DROP CONSTRAINT conv_eventos_tipo_check;
ALTER TABLE mensajeria.conv_eventos ADD CONSTRAINT conv_eventos_tipo_check
    CHECK (tipo IN ('mensaje_nuevo', 'mensaje_actualizado', 'conversacion_actualizada', 'mencion', 'guardia', 'aviso_sin_atender', 'sugerencia',
                    'llamada_entrante', 'llamada_actualizada', 'llamada_terminada', 'devolucion_actualizada'));

-- +goose Down
DELETE FROM mensajeria.conv_eventos WHERE tipo IN ('llamada_entrante', 'llamada_actualizada', 'llamada_terminada', 'devolucion_actualizada');
ALTER TABLE mensajeria.conv_eventos DROP CONSTRAINT conv_eventos_tipo_check;
ALTER TABLE mensajeria.conv_eventos ADD CONSTRAINT conv_eventos_tipo_check
    CHECK (tipo IN ('mensaje_nuevo', 'mensaje_actualizado', 'conversacion_actualizada', 'mencion', 'guardia', 'aviso_sin_atender', 'sugerencia'));
ALTER TABLE mensajeria.conv_eventos DROP COLUMN llamada_id;
DELETE FROM mensajeria.conversacion_mensajes WHERE tipo = 'llamada';
ALTER TABLE mensajeria.conversacion_mensajes DROP CONSTRAINT conversacion_mensajes_tipo_check;
ALTER TABLE mensajeria.conversacion_mensajes ADD CONSTRAINT conversacion_mensajes_tipo_check
    CHECK (tipo IN ('texto', 'imagen', 'documento', 'audio', 'video', 'ubicacion', 'plantilla', 'contacto', 'sistema'));
ALTER TABLE mensajeria.conversacion_mensajes DROP COLUMN llamada_id;
DROP TABLE mensajeria.llamada_devoluciones;
DROP TABLE mensajeria.llamada_grabaciones;
DROP TABLE mensajeria.voz_ajustes_canal;
DROP TABLE mensajeria.voz_ajustes;
DROP TABLE mensajeria.llamada_receptores;
DROP TABLE mensajeria.llamada_candidatos;
DROP TABLE mensajeria.llamada_tramos;
DROP TABLE mensajeria.llamadas;

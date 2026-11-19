-- +goose Up
-- Voz, etapa E4a (contratos/voz.md §2.10 y §3.12): centralita Issabel de solo lectura. Iris escucha
-- el AMI (eventos de llamadas y CDR) y, cuando suena la extensión de un usuario, le muestra quién
-- llama. Las llamadas de la centralita van en su propia tabla (no en mensajeria.llamadas): así ningún
-- proceso de las llamadas de WhatsApp (mantenimiento, Meta, salientes) puede tocarlas.

CREATE TABLE mensajeria.centralitas (
    id                  bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    empresa_id          bigint,
    nombre              text        NOT NULL CHECK (char_length(nombre) BETWEEN 1 AND 80),
    tipo                text        NOT NULL DEFAULT 'issabel' CHECK (tipo IN ('issabel')),
    host                text        NOT NULL CHECK (char_length(host) BETWEEN 1 AND 253),
    puerto              integer     NOT NULL DEFAULT 5039 CHECK (puerto BETWEEN 1 AND 65535),
    usuario             text        NOT NULL CHECK (usuario ~ '^[A-Za-z0-9_.-]{1,64}$'),
    -- AES-256-GCM con IRIS_CLAVE_CIFRADO (datos asociados mensajeria.centralita:<id>:secreto).
    secreto_cifrado     bytea,
    secreto_guardado_en timestamptz,
    -- SHA-256 del certificado del AMI (hex en minúsculas), fijada al probar y confirmada por un admin.
    huella_tls          text CHECK (huella_tls IS NULL OR huella_tls ~ '^[0-9a-f]{64}$'),
    -- La huella distinta que vio la conexión (la detiene hasta que un admin la confirme).
    huella_vista        text CHECK (huella_vista IS NULL OR huella_vista ~ '^[0-9a-f]{64}$'),
    -- E4a: solo lectura (E4b suma lectura_y_llamar).
    modo                text        NOT NULL DEFAULT 'lectura' CHECK (modo IN ('lectura')),
    estado              text        NOT NULL DEFAULT 'desactivada' CHECK (estado IN ('conectada', 'desconectada', 'error', 'desactivada')),
    -- Con estado error: por qué (sin detalle técnico).
    error_codigo        text CHECK (error_codigo IN ('huella_cambiada', 'permisos_de_mas', 'credenciales', 'sin_conexion')),
    -- La última prueba que pasó con los datos actuales (activar la exige).
    probada_en          timestamptz,
    conectada_en        timestamptz,
    desconectada_desde  timestamptz,
    ultimo_evento_en    timestamptz,
    activa              boolean     NOT NULL DEFAULT false,
    -- Qué proceso de mensajería tiene la conexión AMI y hasta cuándo (arriendo que renueva cada
    -- latido): con varias réplicas, solo una conecta cada centralita.
    motor_dueno         text,
    motor_hasta         timestamptz,
    version             integer     NOT NULL DEFAULT 1,
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now(),
    CHECK (NOT activa OR (secreto_cifrado IS NOT NULL AND huella_tls IS NOT NULL))
);
-- Una centralita por empresa en E4a.
CREATE UNIQUE INDEX centralitas_empresa_uq ON mensajeria.centralitas ((coalesce(empresa_id, 0)));

CREATE TABLE mensajeria.centralita_extensiones (
    id            bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    empresa_id    bigint,
    centralita_id bigint      NOT NULL REFERENCES mensajeria.centralitas (id),
    extension     text        NOT NULL CHECK (extension ~ '^[0-9]{2,8}$'),
    usuario_id    bigint      NOT NULL REFERENCES nucleo.usuarios (id),
    -- E4c (previsto, sin usar en E4a): escritorio = teléfono SIP de la oficina; navegador = extensión
    -- PJSIP webrtc que contesta en el panel. Su credencial SIP se guarda cifrada (datos asociados
    -- mensajeria.centralita_extension:<id>:sip), solo en el servidor; al navegador se le entrega por
    -- sesión y con vida corta.
    tipo          text        NOT NULL DEFAULT 'escritorio' CHECK (tipo IN ('escritorio', 'navegador')),
    sip_usuario   text CHECK (sip_usuario IS NULL OR sip_usuario ~ '^[A-Za-z0-9_.-]{1,64}$'),
    sip_secreto_cifrado bytea,
    activa        boolean     NOT NULL DEFAULT true,
    quitada_en    timestamptz,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX centralita_extensiones_ext_uq ON mensajeria.centralita_extensiones (centralita_id, extension) WHERE activa;
CREATE UNIQUE INDEX centralita_extensiones_usuario_uq ON mensajeria.centralita_extensiones (centralita_id, usuario_id) WHERE activa;
CREATE INDEX centralita_extensiones_usuario_idx ON mensajeria.centralita_extensiones (usuario_id) WHERE activa;

-- Las llamadas de la centralita van en mensajeria.llamadas (origen centralita): la misma tabla, el
-- mismo historial y los mismos eventos que las de WhatsApp (una sola fuente de verdad; E4c les
-- sumará el audio en el navegador). Sin canal ni conversación. Todo lo de WhatsApp (mantenimiento,
-- Meta, salientes) filtra origen = 'whatsapp'.
ALTER TABLE mensajeria.llamadas ADD COLUMN centralita_id bigint REFERENCES mensajeria.centralitas (id);
-- Extensión que contestó (entrante) o marcó (saliente).
ALTER TABLE mensajeria.llamadas ADD COLUMN extension text CHECK (extension IS NULL OR extension ~ '^[0-9]{2,8}$');
-- Ticket al que la persona agregó la llamada (a elección; nunca automático).
ALTER TABLE mensajeria.llamadas ADD COLUMN ticket_id bigint;
-- Cómo quedó el cliente: automatica (una sola coincidencia del número) o manual (lo eligió la persona).
ALTER TABLE mensajeria.llamadas ADD COLUMN asociacion text CHECK (asociacion IN ('automatica', 'manual'));
-- Clientes de la instantánea con ese número al llegar (0, 1 o varios).
ALTER TABLE mensajeria.llamadas ADD COLUMN coincidencias integer NOT NULL DEFAULT 0 CHECK (coincidencias >= 0);
ALTER TABLE mensajeria.llamadas ADD COLUMN anotada_por bigint REFERENCES nucleo.usuarios (id);
ALTER TABLE mensajeria.llamadas ADD COLUMN version integer NOT NULL DEFAULT 1;
ALTER TABLE mensajeria.llamadas ADD CONSTRAINT llamadas_centralita_ck
    CHECK (origen <> 'centralita' OR (centralita_id IS NOT NULL AND canal_id IS NULL AND conversacion_id IS NULL)) NOT VALID;
-- Deduplicación por Linkedid de Asterisk (llamadas_externo_uq no sirve: canal_id es null).
CREATE UNIQUE INDEX llamadas_centralita_externo_uq ON mensajeria.llamadas (centralita_id, id_externo) WHERE origen = 'centralita';
CREATE INDEX llamadas_cliente_idx ON mensajeria.llamadas ((coalesce(empresa_id, 0)), cliente_id, sono_en DESC) WHERE cliente_id IS NOT NULL;
CREATE INDEX llamadas_ticket_idx ON mensajeria.llamadas (ticket_id, sono_en DESC) WHERE ticket_id IS NOT NULL;
-- Una saliente viva por persona es regla de WhatsApp (una sola llamada con audio en el panel): no
-- cuenta lo que marca su teléfono de escritorio.
DROP INDEX mensajeria.llamadas_saliente_viva_uq;
CREATE UNIQUE INDEX llamadas_saliente_viva_uq ON mensajeria.llamadas (usuario_id)
    WHERE direccion = 'saliente' AND estado IN ('marcando', 'en_curso') AND origen = 'whatsapp';

-- A quién le sonó una llamada de la centralita (una fila por persona; en un grupo de timbrado, varias).
CREATE TABLE mensajeria.centralita_receptores (
    llamada_id uuid        NOT NULL REFERENCES mensajeria.llamadas (id),
    usuario_id bigint      NOT NULL REFERENCES nucleo.usuarios (id),
    empresa_id bigint,
    extension  text        NOT NULL CHECK (extension ~ '^[0-9]{2,8}$'),
    sono_en    timestamptz NOT NULL,
    PRIMARY KEY (llamada_id, usuario_id)
);
CREATE INDEX centralita_receptores_usuario_idx ON mensajeria.centralita_receptores (usuario_id, sono_en DESC);

-- La ventana llega por el feed de la bandeja como evento de llamada dirigido a una persona, sin
-- conversación (llamada_entrante, llamada_actualizada, llamada_terminada con llamada_id).
ALTER TABLE mensajeria.conv_eventos ALTER COLUMN conversacion_id DROP NOT NULL;
ALTER TABLE mensajeria.conv_eventos ADD CONSTRAINT conv_eventos_sin_conversacion_ck CHECK (
    conversacion_id IS NOT NULL
    OR (usuario_id IS NOT NULL AND llamada_id IS NOT NULL AND tipo IN ('llamada_entrante', 'llamada_actualizada', 'llamada_terminada')));

-- +goose Down
DELETE FROM mensajeria.conv_eventos WHERE conversacion_id IS NULL;
ALTER TABLE mensajeria.conv_eventos DROP CONSTRAINT conv_eventos_sin_conversacion_ck;
ALTER TABLE mensajeria.conv_eventos ALTER COLUMN conversacion_id SET NOT NULL;
DROP TABLE mensajeria.centralita_receptores;
DROP INDEX mensajeria.llamadas_saliente_viva_uq;
DELETE FROM mensajeria.llamadas WHERE origen = 'centralita';
CREATE UNIQUE INDEX llamadas_saliente_viva_uq ON mensajeria.llamadas (usuario_id) WHERE direccion = 'saliente' AND estado IN ('marcando', 'en_curso');
DROP INDEX mensajeria.llamadas_ticket_idx;
DROP INDEX mensajeria.llamadas_cliente_idx;
DROP INDEX mensajeria.llamadas_centralita_externo_uq;
ALTER TABLE mensajeria.llamadas DROP CONSTRAINT llamadas_centralita_ck;
ALTER TABLE mensajeria.llamadas DROP COLUMN version;
ALTER TABLE mensajeria.llamadas DROP COLUMN anotada_por;
ALTER TABLE mensajeria.llamadas DROP COLUMN coincidencias;
ALTER TABLE mensajeria.llamadas DROP COLUMN asociacion;
ALTER TABLE mensajeria.llamadas DROP COLUMN ticket_id;
ALTER TABLE mensajeria.llamadas DROP COLUMN extension;
ALTER TABLE mensajeria.llamadas DROP COLUMN centralita_id;
DROP TABLE mensajeria.centralita_extensiones;
DROP TABLE mensajeria.centralitas;

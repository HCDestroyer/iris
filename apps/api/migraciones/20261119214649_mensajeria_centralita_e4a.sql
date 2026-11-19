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
    activa        boolean     NOT NULL DEFAULT true,
    quitada_en    timestamptz,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX centralita_extensiones_ext_uq ON mensajeria.centralita_extensiones (centralita_id, extension) WHERE activa;
CREATE UNIQUE INDEX centralita_extensiones_usuario_uq ON mensajeria.centralita_extensiones (centralita_id, usuario_id) WHERE activa;
CREATE INDEX centralita_extensiones_usuario_idx ON mensajeria.centralita_extensiones (usuario_id) WHERE activa;

CREATE TABLE mensajeria.centralita_llamadas (
    id               uuid PRIMARY KEY,
    empresa_id       bigint,
    centralita_id    bigint      NOT NULL REFERENCES mensajeria.centralitas (id),
    -- Linkedid de Asterisk: una llamada aunque suene en varias extensiones.
    id_externo       text        NOT NULL CHECK (char_length(id_externo) BETWEEN 1 AND 150),
    direccion        text        NOT NULL CHECK (direccion IN ('entrante', 'saliente')),
    -- E.164 si se pudo normalizar; vacío = número oculto o con formato que no se reconoce.
    numero           text        NOT NULL CHECK (numero = '' OR numero ~ '^\+[0-9]{8,15}$'),
    cliente_id       bigint,
    servicio_id      bigint,
    asociacion       text CHECK (asociacion IN ('automatica', 'manual')),
    -- Clientes de la instantánea con ese número al llegar (0, 1 o varios).
    coincidencias    integer     NOT NULL DEFAULT 0 CHECK (coincidencias >= 0),
    estado           text        NOT NULL CHECK (estado IN ('sonando', 'en_curso', 'terminada', 'perdida')),
    -- Quién contestó (entrante) o quién marcó (saliente) y desde qué extensión.
    usuario_id       bigint REFERENCES nucleo.usuarios (id),
    extension        text CHECK (extension IS NULL OR extension ~ '^[0-9]{2,8}$'),
    sono_en          timestamptz NOT NULL,
    contestada_en    timestamptz,
    terminada_en     timestamptz,
    duracion_seg     integer CHECK (duracion_seg IS NULL OR duracion_seg >= 0),
    nota             text CHECK (nota IS NULL OR char_length(nota) <= 2000),
    resultado        text CHECK (resultado IN ('resuelto', 'devolver', 'equivocado', 'otro')),
    anotada_por      bigint REFERENCES nucleo.usuarios (id),
    ticket_id        bigint,
    version          integer     NOT NULL DEFAULT 1,
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now(),
    UNIQUE (centralita_id, id_externo)
);
CREATE INDEX centralita_llamadas_cliente_idx ON mensajeria.centralita_llamadas ((coalesce(empresa_id, 0)), cliente_id, sono_en DESC) WHERE cliente_id IS NOT NULL;
CREATE INDEX centralita_llamadas_ticket_idx ON mensajeria.centralita_llamadas (ticket_id, sono_en DESC) WHERE ticket_id IS NOT NULL;
CREATE INDEX centralita_llamadas_vivas_idx ON mensajeria.centralita_llamadas (centralita_id, sono_en) WHERE estado IN ('sonando', 'en_curso');

-- A quién le sonó (una fila por usuario; en un grupo de timbrado, varias).
CREATE TABLE mensajeria.centralita_receptores (
    llamada_id uuid        NOT NULL REFERENCES mensajeria.centralita_llamadas (id),
    usuario_id bigint      NOT NULL REFERENCES nucleo.usuarios (id),
    empresa_id bigint,
    extension  text        NOT NULL CHECK (extension ~ '^[0-9]{2,8}$'),
    sono_en    timestamptz NOT NULL,
    PRIMARY KEY (llamada_id, usuario_id)
);
CREATE INDEX centralita_receptores_usuario_idx ON mensajeria.centralita_receptores (usuario_id, sono_en DESC);

-- La ventana llega por el feed de la bandeja como evento dirigido a una persona, sin conversación.
ALTER TABLE mensajeria.conv_eventos ALTER COLUMN conversacion_id DROP NOT NULL;
ALTER TABLE mensajeria.conv_eventos ADD COLUMN centralita_llamada_id uuid;
ALTER TABLE mensajeria.conv_eventos DROP CONSTRAINT conv_eventos_tipo_check;
ALTER TABLE mensajeria.conv_eventos ADD CONSTRAINT conv_eventos_tipo_check
    CHECK (tipo IN ('mensaje_nuevo', 'mensaje_actualizado', 'conversacion_actualizada', 'mencion', 'guardia', 'aviso_sin_atender', 'sugerencia',
                    'llamada_entrante', 'llamada_actualizada', 'llamada_terminada', 'devolucion_actualizada', 'llamada_respuesta', 'centralita_llamada'));
ALTER TABLE mensajeria.conv_eventos ADD CONSTRAINT conv_eventos_centralita_ck CHECK (
    (tipo = 'centralita_llamada' AND conversacion_id IS NULL AND usuario_id IS NOT NULL AND centralita_llamada_id IS NOT NULL)
    OR (tipo <> 'centralita_llamada' AND conversacion_id IS NOT NULL));

-- +goose Down
DELETE FROM mensajeria.conv_eventos WHERE tipo = 'centralita_llamada';
ALTER TABLE mensajeria.conv_eventos DROP CONSTRAINT conv_eventos_centralita_ck;
ALTER TABLE mensajeria.conv_eventos DROP CONSTRAINT conv_eventos_tipo_check;
ALTER TABLE mensajeria.conv_eventos ADD CONSTRAINT conv_eventos_tipo_check
    CHECK (tipo IN ('mensaje_nuevo', 'mensaje_actualizado', 'conversacion_actualizada', 'mencion', 'guardia', 'aviso_sin_atender', 'sugerencia',
                    'llamada_entrante', 'llamada_actualizada', 'llamada_terminada', 'devolucion_actualizada', 'llamada_respuesta'));
ALTER TABLE mensajeria.conv_eventos DROP COLUMN centralita_llamada_id;
ALTER TABLE mensajeria.conv_eventos ALTER COLUMN conversacion_id SET NOT NULL;
DROP TABLE mensajeria.centralita_receptores;
DROP TABLE mensajeria.centralita_llamadas;
DROP TABLE mensajeria.centralita_extensiones;
DROP TABLE mensajeria.centralitas;

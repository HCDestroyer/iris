-- +goose Up
-- Avisos del panel (la campana de la barra superior) y correos del sistema (IRIS_SMTP_*). El primer
-- uso: el tope de uso del asistente de IA al 80 % y al 100 % (contratos/conversaciones.md §16.1).
-- clave une los avisos de un mismo hecho: uno por usuario y clave (un evento repetido no avisa dos
-- veces). Solo referencias y el texto que se muestra; nunca datos de clientes.
CREATE TABLE notificaciones.avisos (
    id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    usuario_id bigint      NOT NULL REFERENCES nucleo.usuarios (id),
    empresa_id bigint,
    tipo       text        NOT NULL CHECK (tipo IN ('asistente_tope')),
    clave      text        NOT NULL CHECK (char_length(clave) BETWEEN 1 AND 200),
    nivel      text        NOT NULL CHECK (nivel IN ('info', 'alerta', 'peligro')),
    titulo     text        NOT NULL CHECK (char_length(titulo) BETWEEN 1 AND 300),
    enlace     text        CHECK (enlace IS NULL OR (enlace LIKE '/%' AND char_length(enlace) <= 300)),
    datos      jsonb       NOT NULL DEFAULT '{}',
    evento_id  uuid,
    leido_en   timestamptz,
    creado_en  timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX avisos_usuario_clave_uq ON notificaciones.avisos (usuario_id, clave);
CREATE INDEX avisos_usuario_idx ON notificaciones.avisos (usuario_id, (coalesce(empresa_id, 0)), id DESC);
CREATE INDEX avisos_no_leidos_idx ON notificaciones.avisos (usuario_id, (coalesce(empresa_id, 0))) WHERE leido_en IS NULL;

-- Correos del sistema (uno por usuario y clave, como los avisos). Sin correo saliente configurado
-- (IRIS_SMTP_HOST e IRIS_SMTP_REMITENTE) la fila queda «omitido» con su motivo: no es un error.
-- El texto se arma al enviar desde tipo y datos.
CREATE TABLE notificaciones.correos (
    id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    usuario_id bigint      NOT NULL REFERENCES nucleo.usuarios (id),
    empresa_id bigint,
    aviso_id   bigint REFERENCES notificaciones.avisos (id) ON DELETE SET NULL,
    tipo       text        NOT NULL CHECK (tipo IN ('asistente_tope')),
    clave      text        NOT NULL CHECK (char_length(clave) BETWEEN 1 AND 200),
    para       text        NOT NULL,
    asunto     text        NOT NULL CHECK (char_length(asunto) BETWEEN 1 AND 300),
    datos      jsonb       NOT NULL DEFAULT '{}',
    estado     text        NOT NULL CHECK (estado IN ('pendiente', 'enviado', 'omitido', 'fallido')),
    motivo     text        CHECK (motivo IS NULL OR char_length(motivo) <= 300),
    intentos   integer     NOT NULL DEFAULT 0,
    enviado_en timestamptz,
    creado_en  timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX correos_usuario_clave_uq ON notificaciones.correos (usuario_id, clave);
CREATE INDEX correos_creado_idx ON notificaciones.correos (creado_en);

-- +goose Down
DROP TABLE notificaciones.correos;
DROP TABLE notificaciones.avisos;

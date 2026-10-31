-- +goose Up
-- Historial de accesos, actividad de la app de campo y roles propios (pedido 10 del PM).

-- 1. Historial de accesos: una fila por intento de entrar (panel, app de campo, autologin desde
--    Argos) y por uso de un token de API (como mucho una por token y hora). usuario_id null = un
--    correo que no existe: el correo NO se guarda (no se puede enumerar quién existe ni guardar
--    por error una contraseña escrita en el campo del correo).
--    Retención: IRIS_ACCESOS_RETENCION_DIAS (180 por defecto); la purga la hace el worker cada hora
--    (job nucleo.accesos_purga). No es auditoria.*: la auditoría de cada acción sigue intacta.
CREATE TABLE nucleo.accesos (
    id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    usuario_id  bigint      REFERENCES nucleo.usuarios (id),
    canal       text        NOT NULL CHECK (canal IN ('panel', 'campo_pwa', 'campo_nativa', 'sso', 'token')),
    resultado   text        NOT NULL CHECK (resultado IN ('exito', 'contrasena_erronea', 'mfa_fallido', 'bloqueado', 'inactivo')),
    ip          text        NOT NULL DEFAULT '',
    user_agent  text        NOT NULL DEFAULT '',
    dispositivo text        NOT NULL DEFAULT '',          -- resumen legible del user-agent o de X-Iris-App
    app         text,                                     -- X-Iris-App tal cual (campo/<versión> (<plataforma>))
    sesion_id   bigint      REFERENCES nucleo.sesiones (id),
    token_id    bigint      REFERENCES nucleo.tokens_api (id),
    request_id  text        NOT NULL DEFAULT '',
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX accesos_usuario_idx ON nucleo.accesos (usuario_id, id);
CREATE INDEX accesos_created_idx ON nucleo.accesos (created_at);
CREATE INDEX accesos_token_idx ON nucleo.accesos (token_id, created_at) WHERE token_id IS NOT NULL;

-- Canal y app de cada sesión (para la lista de sesiones activas).
ALTER TABLE nucleo.sesiones
    ADD COLUMN canal text NOT NULL DEFAULT 'panel' CHECK (canal IN ('panel', 'campo_pwa', 'campo_nativa', 'sso')),
    ADD COLUMN app   text;
CREATE INDEX sesiones_activas_idx ON nucleo.sesiones (usuario_id, expira_en) WHERE revocada_en IS NULL;

-- 2. Actividad de la app de campo: la última conexión de cada usuario por app y plataforma, con la
--    versión que manda la cabecera X-Iris-App y las operaciones sin enviar que informa la app
--    (X-Iris-Pendientes). Una fila por (usuario, app, plataforma): no crece con el uso.
CREATE TABLE nucleo.conexiones_app (
    usuario_id      bigint      NOT NULL REFERENCES nucleo.usuarios (id),
    app             text        NOT NULL CHECK (length(app) BETWEEN 1 AND 40),
    plataforma      text        NOT NULL CHECK (length(plataforma) BETWEEN 1 AND 40),
    version         text        NOT NULL CHECK (length(version) BETWEEN 1 AND 40),
    pendientes      integer     CHECK (pendientes IS NULL OR pendientes >= 0),
    pendientes_en   timestamptz,
    ip              text        NOT NULL DEFAULT '',
    user_agent      text        NOT NULL DEFAULT '',
    ultima_conexion timestamptz NOT NULL,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (usuario_id, app, plataforma)
);
CREATE INDEX operaciones_sync_rechazadas_idx ON campo.operaciones_sync (usuario_id, created_at) WHERE estado = 'rechazada';

-- 3. Roles propios: se desactivan (nunca se borran); un rol inactivo no da permisos ni se asigna.
ALTER TABLE nucleo.roles ADD COLUMN activo boolean NOT NULL DEFAULT true;

-- +goose Down
ALTER TABLE nucleo.roles DROP COLUMN activo;
DROP INDEX campo.operaciones_sync_rechazadas_idx;
DROP TABLE nucleo.conexiones_app;
DROP INDEX nucleo.sesiones_activas_idx;
ALTER TABLE nucleo.sesiones DROP COLUMN app, DROP COLUMN canal;
DROP TABLE nucleo.accesos;

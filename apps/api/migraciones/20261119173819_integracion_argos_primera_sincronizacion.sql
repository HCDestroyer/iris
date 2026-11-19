-- +goose Up
-- Contrato con Argos 0.8.0 §1.5 y §2.5 (B7): primera sincronización automática al activar un
-- conector de Argos, con progreso por recurso, reanudable desde el último cursor, y el informe del
-- avance a Argos (PUT /integraciones/iris/sincronizacion). Es estado de Iris: Argos solo guarda el
-- último informe.
CREATE TABLE integracion.argos_sincronizaciones (
    id                uuid PRIMARY KEY, -- UUID v7 generado por Iris (sincronizacion_id del contrato)
    conector_id       bigint      NOT NULL REFERENCES integracion.conectores (id) ON DELETE CASCADE,
    estado            text        NOT NULL DEFAULT 'en_curso' CHECK (estado IN ('en_curso', 'completa', 'con_errores')),
    -- Lo que se decidió al empezar según el contrato que informó Argos (§4.1): con < 0.8.0 no se
    -- manda excluir_retirados ni se leen cuentas ni bancos, y el avance no se informa.
    contrato_remoto   text,
    excluir_retirados boolean     NOT NULL DEFAULT false,
    informar          boolean     NOT NULL DEFAULT false,
    iniciada_en       timestamptz NOT NULL DEFAULT now(),
    actualizada_en    timestamptz NOT NULL DEFAULT now(),
    terminada_en      timestamptz,
    -- Último informe a Argos: pendiente, informado, descartado (409 sincronizacion_vieja),
    -- no_soportado (Argos < 0.8.0 o sin la ruta) o error (se reintenta).
    informe_estado    text        NOT NULL DEFAULT 'pendiente'
        CHECK (informe_estado IN ('pendiente', 'informado', 'descartado', 'no_soportado', 'error')),
    informado_en      timestamptz,
    -- actualizada_en del último informe que Argos aceptó (para no mandar dos veces el mismo).
    informado_hasta   timestamptz,
    informe_detalle   text,
    iniciada_por      bigint      REFERENCES nucleo.usuarios (id),
    origen            text        NOT NULL DEFAULT 'activacion' CHECK (origen IN ('activacion', 'manual'))
);
-- Una sola sincronización en curso por conector (el job es único por conector).
CREATE UNIQUE INDEX argos_sincronizaciones_en_curso_uq ON integracion.argos_sincronizaciones (conector_id) WHERE estado = 'en_curso';
CREATE INDEX argos_sincronizaciones_conector_idx ON integracion.argos_sincronizaciones (conector_id, id DESC);

CREATE TABLE integracion.argos_sincronizacion_etapas (
    sincronizacion_id uuid        NOT NULL REFERENCES integracion.argos_sincronizaciones (id) ON DELETE CASCADE,
    recurso           text        NOT NULL CHECK (recurso IN ('empresas', 'zonas', 'planes', 'routers', 'cuentas_bancarias', 'bancos', 'clientes', 'servicios')),
    orden             smallint    NOT NULL,
    estado            text        NOT NULL DEFAULT 'pendiente' CHECK (estado IN ('pendiente', 'en_curso', 'completa', 'con_errores')),
    procesados        integer     NOT NULL DEFAULT 0,
    -- Filas que no se guardaron (lápida de servicio.eliminado o cliente que no está en la instantánea).
    omitidos          integer     NOT NULL DEFAULT 0,
    total             integer,
    -- Último cursor guardado: se reanuda desde aquí (nunca desde el principio).
    cursor            text,
    iniciada_en       timestamptz,
    terminada_en      timestamptz,
    -- Hasta 300 caracteres, sin datos de clientes (§2.5).
    error             text CHECK (char_length(error) <= 300),
    PRIMARY KEY (sincronizacion_id, recurso)
);

-- Certificado TLS de la API de Argos (§1.5, «registrar y avisar»): nunca se fija ni se compara
-- para bloquear. Una fila por certificado distinto visto en cada conector; cambiado = apareció
-- otro en el mismo conector (sin reconexión) con una cadena válida.
CREATE TABLE integracion.argos_certificados (
    id               bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    conector_id      bigint      NOT NULL REFERENCES integracion.conectores (id) ON DELETE CASCADE,
    huella_sha256    text        NOT NULL CHECK (huella_sha256 ~ '^[0-9a-f]{64}$'),
    emisor           text        NOT NULL,
    sujeto           text        NOT NULL,
    host             text        NOT NULL,
    vence_en         timestamptz,
    visto_en         timestamptz NOT NULL DEFAULT now(),
    ultimo_visto_en  timestamptz NOT NULL DEFAULT now(),
    anterior_id      bigint      REFERENCES integracion.argos_certificados (id),
    cambiado         boolean     NOT NULL DEFAULT false,
    UNIQUE (conector_id, huella_sha256)
);
CREATE INDEX argos_certificados_conector_idx ON integracion.argos_certificados (conector_id, ultimo_visto_en DESC);

-- Permiso de la acción integraciones.sincronizar (reanudar o volver a lanzar la primera carga).
-- Los roles base los recibe `iris migrar` (nucleo.RolesBase: solo admin).
INSERT INTO nucleo.permisos (codigo, descripcion) VALUES
    ('integraciones.sincronizar', 'Reanudar o volver a lanzar la primera sincronización con Argos')
ON CONFLICT (codigo) DO NOTHING;

-- Vía de recuperación (contrato 0.8.0 §1.5, S5): el token de la integración de un conector creado a
-- mano (recuperacion: vence a 30 días por defecto, 90 como máximo) o por consola (cli) queda marcado y se
-- revoca al confirmarse un canje nuevo del mismo sistema.
ALTER TABLE nucleo.tokens_api ADD COLUMN via text CHECK (via IN ('recuperacion', 'cli'));
CREATE INDEX tokens_api_via_idx ON nucleo.tokens_api (usuario_id) WHERE via IS NOT NULL AND revocado_en IS NULL;

-- Instalaciones ya conectadas (no se pueden cortar al actualizar): los tokens VIGENTES de los
-- usuarios de integración de un conector que no son el entregado en un canje (ni el de una reconexión
-- sin confirmar) se crearon a mano o por consola: quedan como recuperación, SIN vencimiento nuevo.
-- Siguen valiendo hasta que se confirme un canje nuevo del mismo sistema. (Lo prueba
-- TestMigracionMarcaRecuperacion con el bloque entre las marcas.)
-- b7:marcar_recuperacion:inicio
UPDATE nucleo.tokens_api t SET via = 'recuperacion'
WHERE t.via IS NULL AND t.revocado_en IS NULL AND (t.expira_en IS NULL OR t.expira_en > now())
    AND EXISTS (SELECT 1 FROM nucleo.usuarios_roles ur JOIN nucleo.roles r ON r.id = ur.rol_id
        WHERE ur.usuario_id = t.usuario_id AND r.codigo IN ('integracion_argos', 'integracion_horus'))
    AND NOT EXISTS (SELECT 1 FROM integracion.conectores c WHERE c.token_entregado_id = t.id OR c.reemplaza_token_id = t.id);
-- b7:marcar_recuperacion:fin

-- Aviso de campana cuando cambia el certificado (notificaciones, tipo argos_certificado).
ALTER TABLE notificaciones.avisos DROP CONSTRAINT avisos_tipo_check;
ALTER TABLE notificaciones.avisos ADD CONSTRAINT avisos_tipo_check
    CHECK (tipo IN ('asistente_tope', 'servidor_ia_caido', 'servidor_ia_incompatible', 'servidor_ia_umbral', 'argos_certificado'));

-- +goose Down
DROP INDEX nucleo.tokens_api_via_idx;
ALTER TABLE nucleo.tokens_api DROP COLUMN via;
DELETE FROM notificaciones.avisos WHERE tipo = 'argos_certificado';
ALTER TABLE notificaciones.avisos DROP CONSTRAINT avisos_tipo_check;
ALTER TABLE notificaciones.avisos ADD CONSTRAINT avisos_tipo_check
    CHECK (tipo IN ('asistente_tope', 'servidor_ia_caido', 'servidor_ia_incompatible', 'servidor_ia_umbral'));
DELETE FROM nucleo.roles_permisos WHERE permiso = 'integraciones.sincronizar';
DELETE FROM nucleo.permisos WHERE codigo = 'integraciones.sincronizar';
DROP TABLE integracion.argos_certificados;
DROP TABLE integracion.argos_sincronizacion_etapas;
DROP TABLE integracion.argos_sincronizaciones;

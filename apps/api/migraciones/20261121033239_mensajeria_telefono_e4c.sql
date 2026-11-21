-- +goose Up
-- Voz, etapa E4c (contratos/voz.md §3.12, «E4c: el teléfono dentro de Iris»): el navegador y la app
-- hablan SIP sobre WebRTC con la Issabel a través del puente de Iris. La contraseña SIP de cada
-- extensión vive solo en el servidor (cifrada); Iris responde los desafíos. Todas las llamadas siguen en
-- mensajeria.llamadas (origen centralita las de la extensión, celular las directas del técnico).

-- El teléfono de la empresa (centralita.configurar).
ALTER TABLE mensajeria.centralitas ADD COLUMN telefono_activo boolean NOT NULL DEFAULT false;
ALTER TABLE mensajeria.centralitas ADD COLUMN wss_puerto integer NOT NULL DEFAULT 8089 CHECK (wss_puerto BETWEEN 1 AND 65535);
ALTER TABLE mensajeria.centralitas ADD COLUMN wss_ruta text NOT NULL DEFAULT '/ws' CHECK (wss_ruta ~ '^/[A-Za-z0-9/_.-]{0,63}$');
-- SHA-256 del certificado del WSS (puede no ser el del AMI), fijada al probar.
ALTER TABLE mensajeria.centralitas ADD COLUMN wss_huella text CHECK (wss_huella IS NULL OR wss_huella ~ '^[0-9a-f]{64}$');
-- Dominio SIP de From/To (null = host).
ALTER TABLE mensajeria.centralitas ADD COLUMN sip_dominio text CHECK (sip_dominio IS NULL OR char_length(sip_dominio) BETWEEN 1 AND 253);
-- Lo que se antepone a los 8 dígitos de Guatemala para salir por la troncal.
ALTER TABLE mensajeria.centralitas ADD COLUMN prefijo_salida text NOT NULL DEFAULT '' CHECK (prefijo_salida ~ '^[0-9]{0,6}$');
ALTER TABLE mensajeria.centralitas ADD COLUMN llamadas_por_usuario_dia integer NOT NULL DEFAULT 60 CHECK (llamadas_por_usuario_dia BETWEEN 1 AND 300);
ALTER TABLE mensajeria.centralitas ADD COLUMN llamadas_simultaneas integer NOT NULL DEFAULT 10 CHECK (llamadas_simultaneas BETWEEN 1 AND 100);
-- La última prueba del WSS que pasó con los datos actuales (encender el teléfono la exige).
ALTER TABLE mensajeria.centralitas ADD COLUMN telefono_probado_en timestamptz;
-- Cuándo el administrador confirmó las restricciones de la Issabel (runbook §9.3: contexto propio que
-- solo sale a Guatemala, sin transferencias por DTMF, sin desvíos). Encender el teléfono la exige.
ALTER TABLE mensajeria.centralitas ADD COLUMN restricciones_confirmadas_en timestamptz;
ALTER TABLE mensajeria.centralitas ADD CONSTRAINT centralitas_telefono_ck
    CHECK (NOT telefono_activo OR (wss_huella IS NOT NULL AND telefono_probado_en IS NOT NULL AND restricciones_confirmadas_en IS NOT NULL));

-- La credencial SIP de la extensión del navegador (centralita.asignar_extension): solo escritura.
ALTER TABLE mensajeria.centralita_extensiones ADD COLUMN sip_guardado_en timestamptz;
ALTER TABLE mensajeria.centralita_extensiones ADD COLUMN sip_probado_en timestamptz;
ALTER TABLE mensajeria.centralita_extensiones ADD CONSTRAINT centralita_extensiones_navegador_ck
    CHECK (tipo <> 'navegador' OR NOT activa OR (sip_usuario IS NOT NULL AND sip_secreto_cifrado IS NOT NULL));

-- Grupos de timbrado y colas de Issabel a los que se puede transferir.
CREATE TABLE mensajeria.centralita_grupos (
    id            bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    empresa_id    bigint,
    centralita_id bigint      NOT NULL REFERENCES mensajeria.centralitas (id),
    nombre        text        NOT NULL CHECK (char_length(nombre) BETWEEN 1 AND 60),
    numero        text        NOT NULL CHECK (numero ~ '^[0-9]{2,8}$'),
    activo        boolean     NOT NULL DEFAULT true,
    version       integer     NOT NULL DEFAULT 1,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now()
);
-- Los números internos que marca el teléfono son de 2 a 6 cifras (8 o más cifras saldrían a la calle).
ALTER TABLE mensajeria.centralita_grupos ADD CONSTRAINT centralita_grupos_numero_corto_ck CHECK (numero ~ '^[0-9]{2,6}$');
CREATE UNIQUE INDEX centralita_grupos_numero_uq ON mensajeria.centralita_grupos (centralita_id, numero) WHERE activo;

-- Tickets de un solo uso para abrir el WebSocket del teléfono (30 s), atados a la sesión.
CREATE TABLE mensajeria.telefono_tickets (
    hash         bytea PRIMARY KEY,
    usuario_id   bigint      NOT NULL REFERENCES nucleo.usuarios (id),
    empresa_id   bigint,
    extension_id bigint      NOT NULL REFERENCES mensajeria.centralita_extensiones (id),
    dispositivo  text        NOT NULL CHECK (dispositivo IN ('panel', 'app')),
    sesion_id    bigint      NOT NULL,
    expira_en    timestamptz NOT NULL,
    usado_en     timestamptz,
    created_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX telefono_tickets_expira_idx ON mensajeria.telefono_tickets (expira_en);

-- La conexión viva del teléfono de cada persona (una sola: la nueva reemplaza a la vieja).
CREATE TABLE mensajeria.telefono_conexiones (
    id           uuid PRIMARY KEY,
    usuario_id   bigint      NOT NULL REFERENCES nucleo.usuarios (id),
    empresa_id   bigint,
    extension_id bigint      NOT NULL REFERENCES mensajeria.centralita_extensiones (id),
    dispositivo  text        NOT NULL CHECK (dispositivo IN ('panel', 'app')),
    replica      text        NOT NULL,
    abierta_en   timestamptz NOT NULL,
    latido_en    timestamptz NOT NULL
);
CREATE UNIQUE INDEX telefono_conexiones_usuario_uq ON mensajeria.telefono_conexiones (usuario_id);

-- Las llamadas: la orden desde la que se llamó, las del puente y las directas del celular.
ALTER TABLE mensajeria.llamadas ADD COLUMN orden_id bigint;
ALTER TABLE mensajeria.llamadas ADD COLUMN puente boolean NOT NULL DEFAULT false;
-- Cuándo el puente dejó pasar el INVITE de esta marcación (un solo uso).
ALTER TABLE mensajeria.llamadas ADD COLUMN invite_en timestamptz;
ALTER TABLE mensajeria.llamadas DROP CONSTRAINT llamadas_origen_check;
ALTER TABLE mensajeria.llamadas ADD CONSTRAINT llamadas_origen_check CHECK (origen IN ('whatsapp', 'centralita', 'celular'));
ALTER TABLE mensajeria.llamadas DROP CONSTRAINT llamadas_motivo_fin_check;
ALTER TABLE mensajeria.llamadas ADD CONSTRAINT llamadas_motivo_fin_check CHECK (motivo_fin IN ('cliente_colgo', 'agente_colgo', 'asistente_termino',
    'nadie_contesto', 'rechazada_agente', 'fuera_de_horario', 'sin_capacidad', 'motor_no_disponible', 'duracion_maxima', 'silencio', 'mal_uso', 'tope',
    'limite', 'error_medio', 'error_meta', 'sin_permiso', 'transferencia_fallida', 'tope_alcanzado', 'agente_salio', 'directa'));
ALTER TABLE mensajeria.llamadas ADD CONSTRAINT llamadas_celular_ck
    CHECK (origen <> 'celular' OR (orden_id IS NOT NULL AND canal_id IS NULL AND centralita_id IS NULL AND conversacion_id IS NULL));
CREATE INDEX llamadas_orden_idx ON mensajeria.llamadas (orden_id, sono_en DESC) WHERE orden_id IS NOT NULL;
-- Límite por persona y día y simultáneas por empresa (solo las del puente).
CREATE INDEX llamadas_puente_usuario_idx ON mensajeria.llamadas (usuario_id, sono_en) WHERE puente;
CREATE INDEX llamadas_puente_vivas_idx ON mensajeria.llamadas ((coalesce(empresa_id, 0))) WHERE puente AND estado IN ('marcando', 'en_curso');

-- +goose Down
DROP INDEX mensajeria.llamadas_puente_vivas_idx;
DROP INDEX mensajeria.llamadas_puente_usuario_idx;
DROP INDEX mensajeria.llamadas_orden_idx;
DELETE FROM mensajeria.llamadas WHERE origen = 'celular';
ALTER TABLE mensajeria.llamadas DROP CONSTRAINT llamadas_celular_ck;
UPDATE mensajeria.llamadas SET motivo_fin = NULL WHERE motivo_fin = 'directa';
ALTER TABLE mensajeria.llamadas DROP CONSTRAINT llamadas_motivo_fin_check;
ALTER TABLE mensajeria.llamadas ADD CONSTRAINT llamadas_motivo_fin_check CHECK (motivo_fin IN ('cliente_colgo', 'agente_colgo', 'asistente_termino',
    'nadie_contesto', 'rechazada_agente', 'fuera_de_horario', 'sin_capacidad', 'motor_no_disponible', 'duracion_maxima', 'silencio', 'mal_uso', 'tope',
    'limite', 'error_medio', 'error_meta', 'sin_permiso', 'transferencia_fallida', 'tope_alcanzado', 'agente_salio'));
ALTER TABLE mensajeria.llamadas DROP CONSTRAINT llamadas_origen_check;
ALTER TABLE mensajeria.llamadas ADD CONSTRAINT llamadas_origen_check CHECK (origen IN ('whatsapp', 'centralita'));
ALTER TABLE mensajeria.llamadas DROP COLUMN invite_en;
ALTER TABLE mensajeria.llamadas DROP COLUMN puente;
ALTER TABLE mensajeria.llamadas DROP COLUMN orden_id;
DROP TABLE mensajeria.telefono_conexiones;
DROP TABLE mensajeria.telefono_tickets;
DROP TABLE mensajeria.centralita_grupos;
ALTER TABLE mensajeria.centralita_extensiones DROP CONSTRAINT centralita_extensiones_navegador_ck;
ALTER TABLE mensajeria.centralita_extensiones DROP COLUMN sip_probado_en;
ALTER TABLE mensajeria.centralita_extensiones DROP COLUMN sip_guardado_en;
ALTER TABLE mensajeria.centralitas DROP CONSTRAINT centralitas_telefono_ck;
ALTER TABLE mensajeria.centralitas DROP COLUMN restricciones_confirmadas_en;
ALTER TABLE mensajeria.centralitas DROP COLUMN telefono_probado_en;
ALTER TABLE mensajeria.centralitas DROP COLUMN llamadas_simultaneas;
ALTER TABLE mensajeria.centralitas DROP COLUMN llamadas_por_usuario_dia;
ALTER TABLE mensajeria.centralitas DROP COLUMN prefijo_salida;
ALTER TABLE mensajeria.centralitas DROP COLUMN sip_dominio;
ALTER TABLE mensajeria.centralitas DROP COLUMN wss_huella;
ALTER TABLE mensajeria.centralitas DROP COLUMN wss_ruta;
ALTER TABLE mensajeria.centralitas DROP COLUMN wss_puerto;
ALTER TABLE mensajeria.centralitas DROP COLUMN telefono_activo;

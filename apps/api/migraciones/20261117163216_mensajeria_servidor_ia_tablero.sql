-- +goose Up
-- Tablero del servidor de IA (contratos/voz.md §4.12): muestras de /metricas, agregados por hora y
-- episodios de aviso. Nunca texto de prompts ni de respuestas: solo lecturas ya validadas por Iris.

-- 1. Una muestra por latido (cada 10 s). ok = false: el servidor no respondió o su lectura no pasó la
-- validación. Los escalares sirven para las series y los umbrales; datos es la muestra completa
-- (GPU por tarjeta, CPU, memoria, disco y modelos) de la última lectura buena.
CREATE TABLE mensajeria.servidor_ia_muestras (
    motor_id      bigint      NOT NULL REFERENCES mensajeria.voz_motores (id) ON DELETE CASCADE,
    tomada_en     timestamptz NOT NULL,
    ok            boolean     NOT NULL,
    uso_pct       real,
    vram_pct      real,
    temperatura_c real,
    disco_pct     real,
    en_curso      integer,
    datos         jsonb,
    PRIMARY KEY (motor_id, tomada_en)
);

-- 2. Agregados por hora (lo que queda cuando se borran las muestras en detalle).
CREATE TABLE mensajeria.servidor_ia_muestras_hora (
    motor_id      bigint      NOT NULL REFERENCES mensajeria.voz_motores (id) ON DELETE CASCADE,
    hora          timestamptz NOT NULL,
    muestras      integer     NOT NULL,
    fallidas      integer     NOT NULL,
    uso_pct       real,
    vram_pct      real,
    temperatura_c real,
    PRIMARY KEY (motor_id, hora)
);

-- 3. Episodios de aviso: uno abierto por tipo como mucho; se cierra solo al volver a la normalidad.
CREATE TABLE mensajeria.servidor_ia_avisos (
    id       bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    motor_id bigint      NOT NULL REFERENCES mensajeria.voz_motores (id) ON DELETE CASCADE,
    tipo     text        NOT NULL CHECK (tipo IN ('caido', 'incompatible', 'vram', 'disco', 'temperatura', 'saturacion')),
    desde    timestamptz NOT NULL,
    hasta    timestamptz,
    valor    real,
    umbral   real
);
CREATE UNIQUE INDEX servidor_ia_avisos_abierto_uk ON mensajeria.servidor_ia_avisos (motor_id, tipo) WHERE hasta IS NULL;
CREATE INDEX servidor_ia_avisos_rango_idx ON mensajeria.servidor_ia_avisos (motor_id, desde);

-- 4. Rotación del certificado de cliente: el «siguiente» se guarda (su clave, cifrada) junto con el
-- token siguiente ANTES de entregarlo al servidor; si la respuesta se pierde, Iris reintenta con lo
-- guardado en vez de quedarse sin canal.
ALTER TABLE mensajeria.voz_motores ADD COLUMN cert_cliente_siguiente text;
ALTER TABLE mensajeria.voz_motores ADD COLUMN cert_cliente_clave_siguiente bytea;
ALTER TABLE mensajeria.voz_motores ADD COLUMN cert_cliente_vence_siguiente timestamptz;
-- Una rotación a la vez: quien la empieza la aparta unos segundos (lo que tarda en entregarla).
ALTER TABLE mensajeria.voz_motores ADD COLUMN rotacion_desde timestamptz;

-- 5. Avisos de campana y correo del servidor de IA (notificaciones): los tres tipos nuevos.
ALTER TABLE notificaciones.avisos DROP CONSTRAINT avisos_tipo_check;
ALTER TABLE notificaciones.avisos ADD CONSTRAINT avisos_tipo_check
    CHECK (tipo IN ('asistente_tope', 'servidor_ia_caido', 'servidor_ia_incompatible', 'servidor_ia_umbral'));
ALTER TABLE notificaciones.correos DROP CONSTRAINT correos_tipo_check;
ALTER TABLE notificaciones.correos ADD CONSTRAINT correos_tipo_check
    CHECK (tipo IN ('asistente_tope', 'servidor_ia_caido', 'servidor_ia_incompatible', 'servidor_ia_umbral'));

-- +goose Down
DELETE FROM notificaciones.correos WHERE tipo <> 'asistente_tope';
DELETE FROM notificaciones.avisos WHERE tipo <> 'asistente_tope';
ALTER TABLE notificaciones.correos DROP CONSTRAINT correos_tipo_check;
ALTER TABLE notificaciones.correos ADD CONSTRAINT correos_tipo_check CHECK (tipo = 'asistente_tope');
ALTER TABLE notificaciones.avisos DROP CONSTRAINT avisos_tipo_check;
ALTER TABLE notificaciones.avisos ADD CONSTRAINT avisos_tipo_check CHECK (tipo = 'asistente_tope');
ALTER TABLE mensajeria.voz_motores DROP COLUMN rotacion_desde;
ALTER TABLE mensajeria.voz_motores DROP COLUMN cert_cliente_vence_siguiente;
ALTER TABLE mensajeria.voz_motores DROP COLUMN cert_cliente_clave_siguiente;
ALTER TABLE mensajeria.voz_motores DROP COLUMN cert_cliente_siguiente;
DROP TABLE mensajeria.servidor_ia_avisos;
DROP TABLE mensajeria.servidor_ia_muestras_hora;
DROP TABLE mensajeria.servidor_ia_muestras;

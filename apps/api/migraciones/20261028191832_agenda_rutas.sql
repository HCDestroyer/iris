-- +goose Up
-- Rutas del día (I2, fase 3): propuesta por técnico y día con OSRM (o línea recta si OSRM no
-- responde), publicación y marca de «desactualizada» cuando una orden cambia de técnico o de
-- hora. Una ruta nunca se borra: al publicar otra, la anterior queda «reemplazada».
--
-- tecnico_id es el usuario (como en agenda.citas y campo.ordenes). fecha es el día local
-- (America/Guatemala). empresa_id es la empresa activa de quien la propone (null sin conector):
-- una ruta solo lleva órdenes de su empresa.
CREATE TABLE agenda.rutas (
    id                    bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    empresa_id            bigint,
    tecnico_id            bigint           NOT NULL REFERENCES nucleo.usuarios (id),
    fecha                 date             NOT NULL,
    estado                text             NOT NULL CHECK (estado IN ('borrador', 'publicada', 'reemplazada')),
    modo                  text             NOT NULL CHECK (modo IN ('optimizada', 'manual')),
    -- Motor de tiempos y distancias: osrm, o haversine (línea recta) si OSRM no está configurado,
    -- no respondió o los datos cambiaron entre la lectura de OSRM y la transacción.
    motor                 text             NOT NULL CHECK (motor IN ('osrm', 'haversine')),
    motivo_motor          text             CHECK (motivo_motor IS NULL OR motivo_motor IN ('sin_osrm', 'osrm_no_disponible', 'datos_cambiaron')),
    -- Punto y hora de salida de la parte que se ordena.
    origen_salida         text             NOT NULL CHECK (origen_salida IN ('base', 'posicion', 'ultima_parada', 'primera_parada')),
    salida_lat            double precision,
    salida_lon            double precision,
    sale_en               timestamptz      NOT NULL,
    termina_en            timestamptz,
    fin_turno             timestamptz,
    distancia_m           integer          NOT NULL DEFAULT 0 CHECK (distancia_m >= 0),
    viaje_seg             integer          NOT NULL DEFAULT 0 CHECK (viaje_seg >= 0),
    espera_seg            integer          NOT NULL DEFAULT 0 CHECK (espera_seg >= 0),
    geometria             jsonb            CHECK (geometria IS NULL OR jsonb_typeof(geometria) = 'object'),
    -- Una orden de la ruta cambió de técnico o de hora (o entró una nueva ese día): hay que
    -- volver a proponerla. La publicada sigue vigente para el técnico hasta que se publique otra.
    desactualizada        boolean          NOT NULL DEFAULT false,
    desactualizada_en     timestamptz,
    motivo_desactualizada text,
    propuesta_por         bigint           REFERENCES nucleo.usuarios (id),
    propuesta_en          timestamptz      NOT NULL DEFAULT now(),
    publicada_por         bigint           REFERENCES nucleo.usuarios (id),
    publicada_en          timestamptz,
    reemplazada_en        timestamptz,
    reemplazada_por_id    bigint           REFERENCES agenda.rutas (id),
    version               integer          NOT NULL DEFAULT 1,
    created_at            timestamptz      NOT NULL DEFAULT now(),
    updated_at            timestamptz      NOT NULL DEFAULT now(),
    CHECK ((salida_lat IS NULL) = (salida_lon IS NULL)),
    CHECK ((estado = 'borrador') = (publicada_en IS NULL)),
    CHECK ((estado = 'reemplazada') = (reemplazada_en IS NOT NULL))
);
-- A lo sumo un borrador y una publicada por técnico, día y empresa.
CREATE UNIQUE INDEX rutas_borrador_uq ON agenda.rutas (empresa_id, tecnico_id, fecha) NULLS NOT DISTINCT
    WHERE estado = 'borrador';
CREATE UNIQUE INDEX rutas_publicada_uq ON agenda.rutas (empresa_id, tecnico_id, fecha) NULLS NOT DISTINCT
    WHERE estado = 'publicada';
CREATE INDEX rutas_tecnico_fecha_idx ON agenda.rutas (tecnico_id, fecha);
CREATE INDEX rutas_empresa_fecha_idx ON agenda.rutas (empresa_id, fecha);

-- Paradas en el orden de visita, con la llegada estimada. fija: la orden ya estaba en camino, en
-- sitio o terminada al proponer (va primero, en el orden real). Coordenadas, ventana y duración
-- copiadas al proponer (la comparación con la real no cambia si la orden se edita después).
CREATE TABLE agenda.ruta_paradas (
    ruta_id          bigint           NOT NULL REFERENCES agenda.rutas (id),
    posicion         smallint         NOT NULL CHECK (posicion >= 1),
    orden_id         bigint           NOT NULL REFERENCES campo.ordenes (id),
    lat              double precision,
    lon              double precision,
    duracion_min     integer          NOT NULL CHECK (duracion_min > 0),
    ventana_inicio   timestamptz,
    ventana_fin      timestamptz,
    cita_inicio      timestamptz,
    fija             boolean          NOT NULL DEFAULT false,
    llegada_estimada timestamptz      NOT NULL,
    salida_estimada  timestamptz      NOT NULL,
    viaje_seg        integer          NOT NULL DEFAULT 0 CHECK (viaje_seg >= 0),
    distancia_m      integer          NOT NULL DEFAULT 0 CHECK (distancia_m >= 0),
    espera_seg       integer          NOT NULL DEFAULT 0 CHECK (espera_seg >= 0),
    retraso_seg      integer          NOT NULL DEFAULT 0 CHECK (retraso_seg >= 0),
    created_at       timestamptz      NOT NULL DEFAULT now(),
    updated_at       timestamptz      NOT NULL DEFAULT now(),
    PRIMARY KEY (ruta_id, posicion),
    UNIQUE (ruta_id, orden_id),
    CHECK ((lat IS NULL) = (lon IS NULL)),
    CHECK ((ventana_inicio IS NULL) = (ventana_fin IS NULL))
);
CREATE INDEX ruta_paradas_orden_idx ON agenda.ruta_paradas (orden_id);

-- +goose Down
DROP TABLE agenda.ruta_paradas;
DROP TABLE agenda.rutas;

-- +goose Up
-- Horarios y feriados (entidad genérica: número, cola y, en la fase 2, asistente), fuera de horario
-- (respuesta automática una vez por ventana cerrada y guardia) y biblioteca de manuales con búsqueda
-- de texto completo en español (contratos/conversaciones.md §10–§11).

-- Horario: franjas por día de la semana (1 = lunes … 7 = domingo) en hora local de su zona y
-- feriados que cierran el día o lo reemplazan. perfil (por franja) es el punto de extensión de la
-- fase 2: el conjunto de permisos de un asistente en esa franja; en la fase 1 nada lo usa.
CREATE TABLE mensajeria.horarios (
    id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    empresa_id bigint,
    nombre     text        NOT NULL CHECK (char_length(nombre) BETWEEN 1 AND 80),
    zona       text        NOT NULL DEFAULT 'America/Guatemala' CHECK (char_length(zona) BETWEEN 1 AND 60),
    -- [{ dias: [1..7], inicio: "HH:MM", fin: "HH:MM", perfil: text | null }]
    franjas    jsonb       NOT NULL DEFAULT '[]' CHECK (jsonb_typeof(franjas) = 'array'),
    -- [{ fecha: "AAAA-MM-DD", nombre, franjas: [{ inicio, fin, perfil }] }]
    feriados   jsonb       NOT NULL DEFAULT '[]' CHECK (jsonb_typeof(feriados) = 'array'),
    activo     boolean     NOT NULL DEFAULT true,
    version    integer     NOT NULL DEFAULT 1,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX horarios_nombre_uk ON mensajeria.horarios (coalesce(empresa_id, 0), lower(nombre));

ALTER TABLE mensajeria.colas ADD COLUMN horario_id bigint REFERENCES mensajeria.horarios (id);
ALTER TABLE mensajeria.canales
    ADD COLUMN horario_id bigint REFERENCES mensajeria.horarios (id),
    -- { responder, mensaje, guardia: { activa, usuario_id, etiqueta_ids, falla_masiva }, asistente: { activo } }
    ADD COLUMN fuera_horario jsonb NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(fuera_horario) = 'object');

-- Una vez por ventana cerrada: hasta cuándo ya se contestó fuera de horario y ya se avisó a la guardia.
ALTER TABLE mensajeria.conversaciones
    ADD COLUMN fuera_horario_hasta timestamptz,
    ADD COLUMN guardia_hasta timestamptz;

-- Biblioteca de manuales por empresa. busqueda la arma el caso de uso (título A, resumen y
-- etiquetas B, cuerpo C, en español).
CREATE TABLE mensajeria.manuales (
    id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    empresa_id  bigint,
    titulo      text        NOT NULL CHECK (char_length(titulo) BETWEEN 1 AND 160),
    resumen     text        NOT NULL DEFAULT '' CHECK (char_length(resumen) <= 400),
    cuerpo      text        NOT NULL CHECK (char_length(cuerpo) BETWEEN 1 AND 50000),
    etiquetas   text[]      NOT NULL DEFAULT '{}' CHECK (cardinality(etiquetas) <= 20),
    equipo      text CHECK (equipo IS NULL OR char_length(equipo) BETWEEN 1 AND 80),
    visibilidad text        NOT NULL CHECK (visibilidad IN ('interna', 'cliente')),
    activo      boolean     NOT NULL DEFAULT true,
    busqueda    tsvector    NOT NULL,
    creado_por  bigint REFERENCES nucleo.usuarios (id),
    version     integer     NOT NULL DEFAULT 1,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX manuales_busqueda_idx ON mensajeria.manuales USING gin (busqueda);
CREATE INDEX manuales_etiquetas_idx ON mensajeria.manuales USING gin (etiquetas);
CREATE INDEX manuales_empresa_idx ON mensajeria.manuales (empresa_id, id DESC);

-- Enlaces públicos de un manual «para el cliente»: se guarda solo el hash del token.
CREATE TABLE mensajeria.manual_enlaces (
    hash_token      bytea PRIMARY KEY,
    manual_id       bigint      NOT NULL REFERENCES mensajeria.manuales (id),
    empresa_id      bigint,
    conversacion_id bigint REFERENCES mensajeria.conversaciones (id),
    vence_en        timestamptz NOT NULL,
    created_at      timestamptz NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE mensajeria.manual_enlaces;
DROP TABLE mensajeria.manuales;
ALTER TABLE mensajeria.conversaciones DROP COLUMN guardia_hasta, DROP COLUMN fuera_horario_hasta;
ALTER TABLE mensajeria.canales DROP COLUMN fuera_horario, DROP COLUMN horario_id;
ALTER TABLE mensajeria.colas DROP COLUMN horario_id;
DROP TABLE mensajeria.horarios;

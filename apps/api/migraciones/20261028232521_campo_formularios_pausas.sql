-- +goose Up
-- Campo (I2): plantillas de formularios administrables con versiones, varios formularios por
-- orden y pausa de la tarea.
--
--  - plantillas_checklist: empresa_id (null = común a todas las empresas), descripcion,
--    tipo_codigo opcional (null = sirve para cualquier tipo de orden), codigo único por empresa y
--    version_actual_id. nombre y requiere_firma repiten los de la versión actual.
--  - plantilla_versiones: cada versión de una plantilla (nombre, firma e ítems). Editar una
--    plantilla con uso (algún formulario de una orden apunta a su versión actual) crea otra; sin
--    uso, la versión actual se reescribe. Una versión usada nunca cambia.
--  - plantilla_items: de una versión (version_id), con el tipo nuevo `seleccion` y sus opciones.
--  - orden_formularios: los formularios de una orden (plantilla y versión fijas, origen tipo,
--    despacho o tecnico, quién y cuándo). El id es uuid: el que agrega el técnico sin señal usa el
--    id de la operación del lote. Quitar deja la fila con quitado_en (historial).
--  - orden_checklist y su historial quedan ligados al formulario (formulario_id).
--  - Pausa: estado `pausada` (desde en_camino o en_sitio), ordenes.pausada_desde/pausada_en y el
--    total pausado (pausada_seg, solo pausas terminadas); orden_pausas con cada pausa.
--
-- Migración de lo existente: cada plantilla queda con su versión 1 (sus ítems) y cada orden con
-- plantilla_id recibe ese formulario con origen `tipo`; sus marcas se ligan a él.

-- Plantillas ------------------------------------------------------------------------------
ALTER TABLE campo.plantillas_checklist
    ADD COLUMN empresa_id  bigint,
    ADD COLUMN descripcion text   NOT NULL DEFAULT '',
    ADD COLUMN creado_por  bigint REFERENCES nucleo.usuarios (id),
    ALTER COLUMN tipo_codigo DROP NOT NULL,
    DROP CONSTRAINT plantillas_checklist_codigo_key;
CREATE UNIQUE INDEX plantillas_checklist_codigo_uq ON campo.plantillas_checklist (empresa_id, codigo) NULLS NOT DISTINCT;
CREATE INDEX plantillas_checklist_empresa_idx ON campo.plantillas_checklist (empresa_id, activa);

CREATE TABLE campo.plantilla_versiones (
    id             bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    plantilla_id   bigint      NOT NULL REFERENCES campo.plantillas_checklist (id),
    numero         integer     NOT NULL CHECK (numero > 0),
    nombre         text        NOT NULL,
    requiere_firma boolean     NOT NULL DEFAULT false,
    creada_por     bigint      REFERENCES nucleo.usuarios (id),
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now(),
    UNIQUE (plantilla_id, numero)
);

INSERT INTO campo.plantilla_versiones (plantilla_id, numero, nombre, requiere_firma, created_at)
SELECT id, 1, nombre, requiere_firma, created_at FROM campo.plantillas_checklist;

ALTER TABLE campo.plantillas_checklist ADD COLUMN version_actual_id bigint;
UPDATE campo.plantillas_checklist p SET version_actual_id = v.id
FROM campo.plantilla_versiones v WHERE v.plantilla_id = p.id AND v.numero = 1;
-- Diferida: una plantilla nueva y su versión 1 se crean en la misma transacción.
ALTER TABLE campo.plantillas_checklist
    ALTER COLUMN version_actual_id SET NOT NULL,
    ADD CONSTRAINT plantillas_checklist_version_actual_fk FOREIGN KEY (version_actual_id)
        REFERENCES campo.plantilla_versiones (id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE campo.plantilla_items
    ADD COLUMN version_id bigint REFERENCES campo.plantilla_versiones (id),
    ADD COLUMN opciones   jsonb  NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(opciones) = 'array'),
    DROP CONSTRAINT plantilla_items_tipo_check,
    ADD CONSTRAINT plantilla_items_tipo_check CHECK (tipo IN ('si_no', 'texto', 'numero', 'foto', 'medicion', 'seleccion')),
    ADD CONSTRAINT plantilla_items_opciones_ck CHECK ((tipo = 'seleccion') = (jsonb_array_length(opciones) > 0)),
    DROP CONSTRAINT plantilla_items_plantilla_id_codigo_key;
UPDATE campo.plantilla_items i SET version_id = p.version_actual_id
FROM campo.plantillas_checklist p WHERE p.id = i.plantilla_id;
ALTER TABLE campo.plantilla_items ALTER COLUMN version_id SET NOT NULL;
CREATE UNIQUE INDEX plantilla_items_version_codigo_uq ON campo.plantilla_items (version_id, codigo);
CREATE INDEX plantilla_items_version_idx ON campo.plantilla_items (version_id, posicion);

-- Formularios de la orden -------------------------------------------------------------------
CREATE TABLE campo.orden_formularios (
    id           uuid        PRIMARY KEY,
    orden_id     bigint      NOT NULL REFERENCES campo.ordenes (id),
    plantilla_id bigint      NOT NULL REFERENCES campo.plantillas_checklist (id),
    version_id   bigint      NOT NULL REFERENCES campo.plantilla_versiones (id),
    origen       text        NOT NULL CHECK (origen IN ('tipo', 'despacho', 'tecnico')),
    agregado_por bigint      REFERENCES nucleo.usuarios (id),
    agregado_en  timestamptz NOT NULL,
    canal        text        NOT NULL DEFAULT '',
    operacion_id uuid,
    quitado_en   timestamptz,
    quitado_por  bigint      REFERENCES nucleo.usuarios (id),
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now()
);
-- Una plantilla una sola vez por orden (entre los formularios vigentes): así un ítem (de una
-- versión) identifica su formulario.
CREATE UNIQUE INDEX orden_formularios_vigente_uq ON campo.orden_formularios (orden_id, plantilla_id) WHERE quitado_en IS NULL;
CREATE INDEX orden_formularios_orden_idx ON campo.orden_formularios (orden_id, agregado_en);
CREATE INDEX orden_formularios_version_idx ON campo.orden_formularios (version_id);

INSERT INTO campo.orden_formularios (id, orden_id, plantilla_id, version_id, origen, agregado_por, agregado_en)
SELECT gen_random_uuid(), o.id, o.plantilla_id, p.version_actual_id, 'tipo', o.creado_por, o.created_at
FROM campo.ordenes o
JOIN campo.plantillas_checklist p ON p.id = o.plantilla_id;

-- Marcas de ítems de otra plantilla que la de la orden (no debería haberlas): su formulario.
INSERT INTO campo.orden_formularios (id, orden_id, plantilla_id, version_id, origen, agregado_en)
SELECT gen_random_uuid(), x.orden_id, x.plantilla_id, p.version_actual_id, 'tipo', x.primero
FROM (
    SELECT h.orden_id, i.plantilla_id, min(h.ocurrido_en) AS primero
    FROM campo.orden_checklist_historial h
    JOIN campo.plantilla_items i ON i.id = h.item_id
    WHERE NOT EXISTS (SELECT 1 FROM campo.orden_formularios f WHERE f.orden_id = h.orden_id AND f.plantilla_id = i.plantilla_id)
    GROUP BY h.orden_id, i.plantilla_id
) x
JOIN campo.plantillas_checklist p ON p.id = x.plantilla_id;

ALTER TABLE campo.orden_checklist ADD COLUMN formulario_id uuid REFERENCES campo.orden_formularios (id);
UPDATE campo.orden_checklist c SET formulario_id = f.id
FROM campo.plantilla_items i, campo.orden_formularios f
WHERE i.id = c.item_id AND f.orden_id = c.orden_id AND f.plantilla_id = i.plantilla_id;
ALTER TABLE campo.orden_checklist
    ALTER COLUMN formulario_id SET NOT NULL,
    DROP CONSTRAINT orden_checklist_pkey,
    ADD PRIMARY KEY (formulario_id, item_id);
CREATE INDEX orden_checklist_orden_idx ON campo.orden_checklist (orden_id);

ALTER TABLE campo.orden_checklist_historial ADD COLUMN formulario_id uuid REFERENCES campo.orden_formularios (id);
UPDATE campo.orden_checklist_historial h SET formulario_id = f.id
FROM campo.plantilla_items i, campo.orden_formularios f
WHERE i.id = h.item_id AND f.orden_id = h.orden_id AND f.plantilla_id = i.plantilla_id;
ALTER TABLE campo.orden_checklist_historial ALTER COLUMN formulario_id SET NOT NULL;
CREATE INDEX orden_checklist_historial_formulario_idx ON campo.orden_checklist_historial (formulario_id);

-- Pausa ---------------------------------------------------------------------------------------
ALTER TABLE campo.ordenes
    ADD COLUMN pausada_desde text CHECK (pausada_desde IN ('en_camino', 'en_sitio')),
    ADD COLUMN pausada_en    timestamptz,
    ADD COLUMN pausada_seg   bigint NOT NULL DEFAULT 0 CHECK (pausada_seg >= 0),
    DROP CONSTRAINT ordenes_estado_check,
    ADD CONSTRAINT ordenes_estado_check CHECK (estado IN ('pendiente', 'programada', 'en_camino', 'en_sitio', 'pausada',
                                                         'cerrada', 'fallida', 'cancelada')),
    DROP CONSTRAINT ordenes_check2,
    ADD CONSTRAINT ordenes_tecnico_ck CHECK (estado NOT IN ('programada', 'en_camino', 'en_sitio', 'pausada') OR tecnico_id IS NOT NULL),
    ADD CONSTRAINT ordenes_pausa_ck CHECK ((estado = 'pausada') = (pausada_desde IS NOT NULL)
                                          AND (estado = 'pausada') = (pausada_en IS NOT NULL));

CREATE TABLE campo.orden_pausas (
    id               bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    orden_id         bigint      NOT NULL REFERENCES campo.ordenes (id),
    motivo           text        NOT NULL CHECK (motivo IN ('esperando_material', 'cliente_ausente', 'clima', 'acceso', 'otro')),
    nota             text,
    desde_estado     text        NOT NULL CHECK (desde_estado IN ('en_camino', 'en_sitio')),
    inicio           timestamptz NOT NULL,
    fin              timestamptz,
    cierre           text        CHECK (cierre IN ('reanudada', 'cancelada', 'reprogramada')),
    pausada_por      bigint      REFERENCES nucleo.usuarios (id),
    cerrada_por      bigint      REFERENCES nucleo.usuarios (id),
    canal            text        NOT NULL DEFAULT '',
    canal_cierre     text,
    operacion_id     uuid,
    operacion_cierre uuid,
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now(),
    CHECK (fin IS NULL OR fin >= inicio),
    CHECK ((fin IS NULL) = (cierre IS NULL)),
    CHECK (motivo <> 'otro' OR nota IS NOT NULL)
);
CREATE UNIQUE INDEX orden_pausas_abierta_uq ON campo.orden_pausas (orden_id) WHERE fin IS NULL;
CREATE INDEX orden_pausas_orden_idx ON campo.orden_pausas (orden_id, inicio);

ALTER TABLE campo.orden_eventos
    DROP CONSTRAINT orden_eventos_tipo_check,
    ADD CONSTRAINT orden_eventos_tipo_check CHECK (tipo IN ('creada', 'estado', 'asignada', 'programada', 'formulario'));

-- +goose Down
-- Las órdenes pausadas vuelven al estado de antes de la pausa; los formularios agregados (que no
-- son el del tipo), sus marcas y las versiones anteriores a la actual se pierden. Si dos
-- plantillas de distintas empresas comparten codigo, falla: hay que renombrar una antes.
DELETE FROM campo.orden_eventos WHERE tipo = 'formulario';
ALTER TABLE campo.orden_eventos
    DROP CONSTRAINT orden_eventos_tipo_check,
    ADD CONSTRAINT orden_eventos_tipo_check CHECK (tipo IN ('creada', 'estado', 'asignada', 'programada'));
UPDATE campo.ordenes SET estado = pausada_desde, pausada_desde = NULL, pausada_en = NULL WHERE estado = 'pausada';
DROP TABLE campo.orden_pausas;
ALTER TABLE campo.ordenes
    DROP CONSTRAINT ordenes_pausa_ck,
    DROP CONSTRAINT ordenes_tecnico_ck,
    ADD CONSTRAINT ordenes_check2 CHECK (estado NOT IN ('programada', 'en_camino', 'en_sitio') OR tecnico_id IS NOT NULL),
    DROP CONSTRAINT ordenes_estado_check,
    ADD CONSTRAINT ordenes_estado_check CHECK (estado IN ('pendiente', 'programada', 'en_camino', 'en_sitio',
                                                         'cerrada', 'fallida', 'cancelada')),
    DROP COLUMN pausada_seg,
    DROP COLUMN pausada_en,
    DROP COLUMN pausada_desde;

DELETE FROM campo.orden_checklist c USING campo.orden_formularios f
WHERE f.id = c.formulario_id AND (f.origen <> 'tipo' OR f.quitado_en IS NOT NULL);
DELETE FROM campo.orden_checklist_historial c USING campo.orden_formularios f
WHERE f.id = c.formulario_id AND (f.origen <> 'tipo' OR f.quitado_en IS NOT NULL);
ALTER TABLE campo.orden_checklist_historial DROP COLUMN formulario_id;
DROP INDEX campo.orden_checklist_orden_idx;
ALTER TABLE campo.orden_checklist DROP CONSTRAINT orden_checklist_pkey;
ALTER TABLE campo.orden_checklist DROP COLUMN formulario_id, ADD PRIMARY KEY (orden_id, item_id);
DROP TABLE campo.orden_formularios;

-- Solo quedan los ítems de la versión actual de cada plantilla.
UPDATE campo.orden_adjuntos a SET item_id = NULL FROM campo.plantilla_items i, campo.plantillas_checklist p
WHERE a.item_id = i.id AND p.id = i.plantilla_id AND i.version_id <> p.version_actual_id;
DELETE FROM campo.orden_checklist c USING campo.plantilla_items i, campo.plantillas_checklist p
WHERE c.item_id = i.id AND p.id = i.plantilla_id AND i.version_id <> p.version_actual_id;
DELETE FROM campo.orden_checklist_historial c USING campo.plantilla_items i, campo.plantillas_checklist p
WHERE c.item_id = i.id AND p.id = i.plantilla_id AND i.version_id <> p.version_actual_id;
DELETE FROM campo.plantilla_items i USING campo.plantillas_checklist p
WHERE p.id = i.plantilla_id AND i.version_id <> p.version_actual_id;
UPDATE campo.plantilla_items SET tipo = 'texto', opciones = '[]'::jsonb WHERE tipo = 'seleccion';
DROP INDEX campo.plantilla_items_version_idx;
DROP INDEX campo.plantilla_items_version_codigo_uq;
ALTER TABLE campo.plantilla_items
    DROP CONSTRAINT plantilla_items_opciones_ck,
    DROP CONSTRAINT plantilla_items_tipo_check,
    ADD CONSTRAINT plantilla_items_tipo_check CHECK (tipo IN ('si_no', 'texto', 'numero', 'foto', 'medicion')),
    ADD CONSTRAINT plantilla_items_plantilla_id_codigo_key UNIQUE (plantilla_id, codigo),
    DROP COLUMN opciones,
    DROP COLUMN version_id;
ALTER TABLE campo.plantillas_checklist DROP COLUMN version_actual_id;
DROP TABLE campo.plantilla_versiones;
UPDATE campo.plantillas_checklist SET tipo_codigo = 'visita' WHERE tipo_codigo IS NULL;
DROP INDEX campo.plantillas_checklist_empresa_idx;
DROP INDEX campo.plantillas_checklist_codigo_uq;
ALTER TABLE campo.plantillas_checklist
    ADD CONSTRAINT plantillas_checklist_codigo_key UNIQUE (codigo),
    ALTER COLUMN tipo_codigo SET NOT NULL,
    DROP COLUMN creado_por,
    DROP COLUMN descripcion,
    DROP COLUMN empresa_id;

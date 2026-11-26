-- +goose Up
-- Corte manual de respaldo del cable (contrato argosisp.md 0.15.0 §13.2, A54 / B34). Cuando SmartOLT
-- no puede cortar o reconectar el CATV, Argos pide a Iris que alguien lo haga en campo o en la OLT:
-- Iris abre una orden nueva de tipo corte_catv o reconexion_catv y le devuelve el estado.

-- Tipos de orden nuevos.
ALTER TABLE campo.tipos_orden
    DROP CONSTRAINT tipos_orden_codigo_check,
    ADD CONSTRAINT tipos_orden_codigo_check
        CHECK (codigo IN ('instalacion', 'visita', 'retiro', 'cambio_equipo', 'mantenimiento', 'corte_catv', 'reconexion_catv'));
ALTER TABLE campo.plantillas_checklist
    DROP CONSTRAINT plantillas_checklist_tipo_codigo_check,
    ADD CONSTRAINT plantillas_checklist_tipo_codigo_check
        CHECK (tipo_codigo IN ('instalacion', 'visita', 'retiro', 'cambio_equipo', 'mantenimiento', 'corte_catv', 'reconexion_catv'));

-- Prioridad de la orden (B5: la reconexión siempre va con prioridad alta). Las demás, normal.
ALTER TABLE campo.ordenes ADD COLUMN prioridad text NOT NULL DEFAULT 'normal' CHECK (prioridad IN ('normal', 'alta'));

-- Checklist de cierre (comunes, versión 1). «resultado» decide lo que Iris informa a Argos: Hecho →
-- resuelta; Sin acceso, Equipo no encontrado u Otro motivo → no_realizada con ese motivo_codigo.
INSERT INTO campo.plantillas_checklist (codigo, nombre, tipo_codigo, version_actual_id) VALUES
    ('corte_catv', 'Corte de cable (TV)', 'corte_catv', 0),
    ('reconexion_catv', 'Reconexión de cable (TV)', 'reconexion_catv', 0);
INSERT INTO campo.plantilla_versiones (plantilla_id, numero, nombre)
SELECT id, 1, nombre FROM campo.plantillas_checklist WHERE empresa_id IS NULL AND codigo IN ('corte_catv', 'reconexion_catv');
UPDATE campo.plantillas_checklist p SET version_actual_id = v.id
FROM campo.plantilla_versiones v
WHERE v.plantilla_id = p.id AND v.numero = 1 AND p.empresa_id IS NULL AND p.codigo IN ('corte_catv', 'reconexion_catv');
INSERT INTO campo.plantilla_items (plantilla_id, version_id, posicion, codigo, etiqueta, tipo, obligatorio, unidad, opciones)
SELECT p.id, p.version_actual_id, i.posicion, i.codigo,
    CASE WHEN p.codigo = 'corte_catv' THEN i.etiqueta_corte ELSE i.etiqueta_reconexion END,
    i.tipo, i.obligatorio, '', i.opciones::jsonb
FROM campo.plantillas_checklist p
JOIN (VALUES
    (1, 'resultado', '¿Se cortó el cable (TV)?', '¿Se reconectó el cable (TV)?', 'seleccion', true,
        '["Hecho", "Sin acceso", "Equipo no encontrado", "Otro motivo"]'),
    (2, 'donde', 'Dónde se hizo (puerto CATV de la ONU, OLT o caja)', 'Dónde se hizo (puerto CATV de la ONU, OLT o caja)', 'texto', false, '[]'),
    (3, 'foto_evidencia', 'Foto de evidencia', 'Foto de evidencia', 'foto', false, '[]'),
    (4, 'observaciones', 'Observaciones', 'Observaciones', 'texto', false, '[]')
) AS i (posicion, codigo, etiqueta_corte, etiqueta_reconexion, tipo, obligatorio, opciones) ON true
WHERE p.empresa_id IS NULL AND p.codigo IN ('corte_catv', 'reconexion_catv');
INSERT INTO campo.tipos_orden (codigo, nombre, duracion_min, plantilla_id)
SELECT t.codigo, t.nombre, 30, p.id
FROM (VALUES ('corte_catv', 'Corte de cable (TV)'), ('reconexion_catv', 'Reconexión de cable (TV)')) AS t (codigo, nombre)
JOIN campo.plantillas_checklist p ON p.codigo = t.codigo AND p.empresa_id IS NULL;

-- Solicitudes de corte manual (una por solicitud_id de Argos, su clave de idempotencia). Dos
-- contadores independientes (B3): version_argos, la mayor recibida en servicio.corte_manual_*, y
-- version_iris, la de corte_manual.estado_cambiado. Solo ids: sin nombre, teléfono ni dirección.
--   estado: abierta (orden creada), en_curso (asignada), resuelta, no_realizada y cancelada
--   (terminales: no retroceden), descartada (B9: empresa fuera del alcance o servicio desconocido;
--   no se abre nada) y cancelada_antes (el cancelado llegó antes que el solicitado, B4).
CREATE TABLE campo.cortes_manuales (
    solicitud_id      bigint      PRIMARY KEY CHECK (solicitud_id > 0),
    conector_id       bigint      NOT NULL REFERENCES integracion.conectores (id),
    empresa_id        bigint      NOT NULL,
    servicio_id       bigint      NOT NULL,
    cliente_id        bigint,
    accion            text        NOT NULL CHECK (accion IN ('corte', 'reconexion')),
    causa             text        NOT NULL CHECK (causa ~ '^[a-z][a-z0-9_]{0,39}$'),
    componente        text        NOT NULL DEFAULT 'cable' CHECK (componente ~ '^[a-z][a-z0-9_]{0,19}$'),
    solicitada_en     timestamptz,
    version_argos     integer     NOT NULL CHECK (version_argos > 0),
    version_iris      integer     NOT NULL DEFAULT 0 CHECK (version_iris >= 0),
    estado            text        NOT NULL
                                  CHECK (estado IN ('abierta', 'en_curso', 'resuelta', 'no_realizada', 'cancelada', 'descartada', 'cancelada_antes')),
    descarte          text        CHECK (descarte ~ '^[a-z][a-z0-9_]{0,39}$'),
    motivo_codigo     text        CHECK (motivo_codigo IN ('sin_acceso', 'equipo_no_encontrado', 'otro')),
    -- cancelacion_pedida: Argos canceló con el técnico ya en camino o en sitio; el cierre decide
    -- (Hecho → resuelta y Argos pide la reconexión; si no, cancelada).
    cancelacion_pedida boolean    NOT NULL DEFAULT false,
    orden_id          bigint      UNIQUE REFERENCES campo.ordenes (id),
    entrante_id       bigint,
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now(),
    CHECK ((estado = 'no_realizada') = (motivo_codigo IS NOT NULL)),
    CHECK ((estado = 'descartada') = (descarte IS NOT NULL))
);
CREATE INDEX cortes_manuales_servicio_idx ON campo.cortes_manuales (servicio_id, accion);

-- +goose Down
DROP TABLE campo.cortes_manuales;
DELETE FROM campo.tipos_orden WHERE codigo IN ('corte_catv', 'reconexion_catv');
DELETE FROM campo.plantilla_items WHERE plantilla_id IN
    (SELECT id FROM campo.plantillas_checklist WHERE empresa_id IS NULL AND codigo IN ('corte_catv', 'reconexion_catv'));
UPDATE campo.plantillas_checklist SET version_actual_id = 0 WHERE empresa_id IS NULL AND codigo IN ('corte_catv', 'reconexion_catv');
DELETE FROM campo.plantilla_versiones WHERE plantilla_id IN
    (SELECT id FROM campo.plantillas_checklist WHERE empresa_id IS NULL AND codigo IN ('corte_catv', 'reconexion_catv'));
DELETE FROM campo.plantillas_checklist WHERE empresa_id IS NULL AND codigo IN ('corte_catv', 'reconexion_catv');
ALTER TABLE campo.ordenes DROP COLUMN prioridad;
ALTER TABLE campo.plantillas_checklist
    DROP CONSTRAINT plantillas_checklist_tipo_codigo_check,
    ADD CONSTRAINT plantillas_checklist_tipo_codigo_check
        CHECK (tipo_codigo IN ('instalacion', 'visita', 'retiro', 'cambio_equipo', 'mantenimiento'));
ALTER TABLE campo.tipos_orden
    DROP CONSTRAINT tipos_orden_codigo_check,
    ADD CONSTRAINT tipos_orden_codigo_check
        CHECK (codigo IN ('instalacion', 'visita', 'retiro', 'cambio_equipo', 'mantenimiento'));

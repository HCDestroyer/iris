-- +goose Up
-- Semilla de soporte: calendario laboral de Guatemala con sus feriados 2026–2027, SLA
-- estándar (laboral, por defecto) y de red (24/7) y categorías base. Todo global
-- (empresa_id null) y editable después; las filas se identifican por codigo.

INSERT INTO soporte.calendarios (codigo, nombre, zona_horaria)
VALUES ('gt_laboral', 'Guatemala · horario laboral', 'America/Guatemala');

-- Lunes a viernes de 08:00 a 17:00 y sábado de 08:00 a 12:00 (hora de Guatemala).
INSERT INTO soporte.calendario_tramos (calendario_id, dia_semana, inicio, fin)
SELECT c.id, d.dia, d.inicio, d.fin
FROM soporte.calendarios c
CROSS JOIN (VALUES (1, time '08:00', time '17:00'), (2, time '08:00', time '17:00'),
                   (3, time '08:00', time '17:00'), (4, time '08:00', time '17:00'),
                   (5, time '08:00', time '17:00'), (6, time '08:00', time '12:00')) AS d (dia, inicio, fin)
WHERE c.codigo = 'gt_laboral' AND c.empresa_id IS NULL;

-- Feriados de Guatemala (Código de Trabajo, art. 127). El 24 y el 31 de diciembre son
-- feriado desde las 12:00. Los traslados que disponga un decreto y la feria de cada
-- localidad se ajustan en esta tabla.
INSERT INTO soporte.feriados (calendario_id, fecha, nombre, desde)
SELECT c.id, f.fecha, f.nombre, f.desde
FROM soporte.calendarios c
CROSS JOIN (VALUES
    (date '2026-01-01', 'Año Nuevo', NULL::time),
    (date '2026-04-02', 'Jueves Santo', NULL),
    (date '2026-04-03', 'Viernes Santo', NULL),
    (date '2026-04-04', 'Sábado Santo', NULL),
    (date '2026-05-01', 'Día del Trabajo', NULL),
    (date '2026-06-30', 'Día del Ejército', NULL),
    (date '2026-09-15', 'Día de la Independencia', NULL),
    (date '2026-10-20', 'Día de la Revolución', NULL),
    (date '2026-11-01', 'Día de Todos los Santos', NULL),
    (date '2026-12-24', 'Nochebuena (medio día)', time '12:00'),
    (date '2026-12-25', 'Navidad', NULL),
    (date '2026-12-31', 'Fin de año (medio día)', time '12:00'),
    (date '2027-01-01', 'Año Nuevo', NULL),
    (date '2027-03-25', 'Jueves Santo', NULL),
    (date '2027-03-26', 'Viernes Santo', NULL),
    (date '2027-03-27', 'Sábado Santo', NULL),
    (date '2027-05-01', 'Día del Trabajo', NULL),
    (date '2027-06-30', 'Día del Ejército', NULL),
    (date '2027-09-15', 'Día de la Independencia', NULL),
    (date '2027-10-20', 'Día de la Revolución', NULL),
    (date '2027-11-01', 'Día de Todos los Santos', NULL),
    (date '2027-12-24', 'Nochebuena (medio día)', time '12:00'),
    (date '2027-12-25', 'Navidad', NULL),
    (date '2027-12-31', 'Fin de año (medio día)', time '12:00')
) AS f (fecha, nombre, desde)
WHERE c.codigo = 'gt_laboral' AND c.empresa_id IS NULL;

INSERT INTO soporte.slas (codigo, nombre, calendario_id, por_defecto)
SELECT 'estandar', 'Estándar (horario laboral)', c.id, true
FROM soporte.calendarios c WHERE c.codigo = 'gt_laboral' AND c.empresa_id IS NULL;

INSERT INTO soporte.slas (codigo, nombre, calendario_id, por_defecto)
VALUES ('red_24_7', 'Red y sin servicio (24/7)', NULL, false);

-- Minutos de respuesta y de resolución por prioridad.
INSERT INTO soporte.sla_objetivos (sla_id, prioridad, respuesta_min, resolucion_min)
SELECT s.id, o.prioridad, o.respuesta, o.resolucion
FROM soporte.slas s
JOIN (VALUES
    ('estandar', 'critica', 30, 240),
    ('estandar', 'alta', 60, 480),
    ('estandar', 'media', 240, 1440),
    ('estandar', 'baja', 480, 2880),
    ('red_24_7', 'critica', 15, 240),
    ('red_24_7', 'alta', 30, 480),
    ('red_24_7', 'media', 60, 1440),
    ('red_24_7', 'baja', 120, 2880)
) AS o (sla, prioridad, respuesta, resolucion) ON o.sla = s.codigo
WHERE s.empresa_id IS NULL;

INSERT INTO soporte.categorias (codigo, nombre, sla_id, requiere_orden)
SELECT c.codigo, c.nombre, s.id, c.requiere_orden
FROM (VALUES
    ('sin_servicio', 'Sin servicio', 'red_24_7', false),
    ('intermitencia', 'Intermitencia', NULL, false),
    ('lentitud', 'Lentitud', NULL, false),
    ('equipo', 'Equipo dañado o cambio de equipo', NULL, true),
    ('traslado', 'Traslado de servicio', NULL, true),
    ('facturacion', 'Facturación y pagos', NULL, false),
    ('cambio_plan', 'Cambio de plan', NULL, false),
    ('incidente_red', 'Incidente de red', 'red_24_7', false),
    ('otro', 'Otro', NULL, false)
) AS c (codigo, nombre, sla, requiere_orden)
LEFT JOIN soporte.slas s ON s.codigo = c.sla AND s.empresa_id IS NULL;

-- +goose Down
DELETE FROM soporte.categorias WHERE empresa_id IS NULL
    AND codigo IN ('sin_servicio', 'intermitencia', 'lentitud', 'equipo', 'traslado', 'facturacion',
                   'cambio_plan', 'incidente_red', 'otro');
DELETE FROM soporte.slas WHERE empresa_id IS NULL AND codigo IN ('estandar', 'red_24_7');
DELETE FROM soporte.calendarios WHERE empresa_id IS NULL AND codigo = 'gt_laboral';

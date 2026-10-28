-- +goose Up
-- Llaves foráneas de zona_id y cuadrilla_id en tickets y órdenes (antes sin catálogo).
--
-- Antes de agregarlas se completa el catálogo con lo que ya hay en la base:
--  1. Zonas espejo de integracion.argos_zonas (origen argos, mismo id que en Argos).
--  2. Valores huérfanos (un zona_id o cuadrilla_id en soporte.tickets, campo.ordenes o en el
--     alcance de nucleo.usuarios_roles que no está en el catálogo) NO se borran ni se ponen en
--     null (el alcance de los usuarios depende de ellos): se crea una zona «Zona <id>» (origen
--     migrada) o una cuadrilla «Cuadrilla <id> (migrada)» con el mismo id. Su empresa es la
--     única empresa de los tickets y órdenes que la usan; si son de varias (o ninguna), null.
--     Quedan activas para no romper nada; administración las renombra o desactiva. Si después
--     Argos trae una zona con ese id, el espejo la toma (origen argos).
INSERT INTO agenda.zonas (id, empresa_id, origen, nombre, sincronizado_en)
SELECT id, empresa_id, 'argos', coalesce(nullif(left(btrim(nombre), 120), ''), 'Zona ' || id), sincronizado_en
FROM integracion.argos_zonas
ON CONFLICT (id) DO NOTHING;

INSERT INTO agenda.zonas (id, empresa_id, origen, nombre)
SELECT u.id,
       (CASE WHEN cardinality(array_agg(DISTINCT u.empresa_id) FILTER (WHERE NOT u.rol)) = 1
             THEN (array_agg(DISTINCT u.empresa_id) FILTER (WHERE NOT u.rol))[1] END),
       'migrada', 'Zona ' || u.id
FROM (
    SELECT zona_id AS id, empresa_id, false AS rol FROM soporte.tickets WHERE zona_id IS NOT NULL
    UNION ALL
    SELECT zona_id, empresa_id, false FROM campo.ordenes WHERE zona_id IS NOT NULL
    UNION ALL
    SELECT zona_id, NULL, true FROM nucleo.usuarios_roles WHERE zona_id IS NOT NULL
) u
WHERE NOT EXISTS (SELECT 1 FROM agenda.zonas z WHERE z.id = u.id)
GROUP BY u.id;

INSERT INTO agenda.cuadrillas (id, empresa_id, nombre)
SELECT u.id,
       (CASE WHEN cardinality(array_agg(DISTINCT u.empresa_id) FILTER (WHERE NOT u.rol)) = 1
             THEN (array_agg(DISTINCT u.empresa_id) FILTER (WHERE NOT u.rol))[1] END),
       'Cuadrilla ' || u.id || ' (migrada)'
FROM (
    SELECT cuadrilla_id AS id, empresa_id, false AS rol FROM soporte.tickets WHERE cuadrilla_id IS NOT NULL
    UNION ALL
    SELECT cuadrilla_id, empresa_id, false FROM campo.ordenes WHERE cuadrilla_id IS NOT NULL
    UNION ALL
    SELECT cuadrilla_id, NULL, true FROM nucleo.usuarios_roles WHERE cuadrilla_id IS NOT NULL
) u
WHERE NOT EXISTS (SELECT 1 FROM agenda.cuadrillas c WHERE c.id = u.id)
GROUP BY u.id;

-- La identidad sigue después del mayor id conservado.
SELECT setval(pg_get_serial_sequence('agenda.cuadrillas', 'id'),
              coalesce((SELECT max(id) FROM agenda.cuadrillas), 1),
              (SELECT count(*) > 0 FROM agenda.cuadrillas));

ALTER TABLE soporte.tickets
    ADD CONSTRAINT tickets_zona_fk FOREIGN KEY (zona_id) REFERENCES agenda.zonas (id),
    ADD CONSTRAINT tickets_cuadrilla_fk FOREIGN KEY (cuadrilla_id) REFERENCES agenda.cuadrillas (id);
ALTER TABLE campo.ordenes
    ADD CONSTRAINT ordenes_zona_fk FOREIGN KEY (zona_id) REFERENCES agenda.zonas (id),
    ADD CONSTRAINT ordenes_cuadrilla_fk FOREIGN KEY (cuadrilla_id) REFERENCES agenda.cuadrillas (id);

-- +goose Down
ALTER TABLE campo.ordenes DROP CONSTRAINT ordenes_cuadrilla_fk, DROP CONSTRAINT ordenes_zona_fk;
ALTER TABLE soporte.tickets DROP CONSTRAINT tickets_cuadrilla_fk, DROP CONSTRAINT tickets_zona_fk;

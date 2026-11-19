-- +goose Up
-- Contrato con Argos 0.8.0 §2.4: más datos del servicio en la instantánea, con lista blanca.
-- precio_mensual y moneda: los calcula Argos (≥ 1.18.0, costo_override ?? precio del plan, con IVA);
-- null = Argos no lo informó (anterior a 1.18.0): Iris no muestra precio ni lo calcula.
-- prefijo_enlace_v6 / prefijo_delegado_v6: CIDR con máscara; '' = sin asignación.
-- planta: el objeto de A9 tal cual (seis claves string o null); null = sin datos.
-- Nunca costo_override, mac, ipv6_duid, ipv6_modo, local_address, servidor_pppoe_id,
-- excepciones, suspension_manual_motivo ni externo (fuera de la lista blanca).
ALTER TABLE integracion.argos_servicios
    ADD COLUMN precio_mensual      numeric(12, 2),
    ADD COLUMN moneda              text,
    ADD COLUMN prefijo_enlace_v6   text NOT NULL DEFAULT '',
    ADD COLUMN prefijo_delegado_v6 text NOT NULL DEFAULT '',
    ADD COLUMN planta              jsonb,
    ADD CONSTRAINT argos_servicios_precio_moneda_ck CHECK ((precio_mensual IS NULL) = (moneda IS NULL));

-- catalogo.cambiado de un plan con precio: se releen los servicios de ese plan.
CREATE INDEX argos_servicios_plan_idx ON integracion.argos_servicios (plan_id) WHERE plan_id IS NOT NULL;

-- Permisos servicios.ver_red y servicios.ver_ubicacion (§2.4). Los roles base los recibe `iris
-- migrar` (nucleo.RolesBase). Los roles PROPIOS que hoy ven el servicio con clientes.ver los
-- reciben aquí, una sola vez, para que nadie pierda en silencio lo que veía (decisión del líder,
-- 19 nov 2026); el admin se los quita si quiere. La descripción definitiva la pone la semilla.
INSERT INTO nucleo.permisos (codigo, descripcion) VALUES
    ('servicios.ver_red', 'Ver los datos de red de los servicios de Argos'),
    ('servicios.ver_ubicacion', 'Ver la dirección y la ubicación de los servicios de Argos')
ON CONFLICT (codigo) DO NOTHING;

WITH propios AS (
    SELECT DISTINCT rp.rol_id FROM nucleo.roles_permisos rp
    JOIN nucleo.roles r ON r.id = rp.rol_id
    WHERE rp.permiso = 'clientes.ver' AND NOT r.sistema
), dados AS (
    INSERT INTO nucleo.roles_permisos (rol_id, permiso)
    SELECT p.rol_id, x.permiso FROM propios p
    CROSS JOIN (VALUES ('servicios.ver_red'), ('servicios.ver_ubicacion')) AS x (permiso)
    ON CONFLICT DO NOTHING
    RETURNING rol_id, permiso
), versiones AS (
    UPDATE nucleo.roles SET version = version + 1, updated_at = now()
    WHERE id IN (SELECT rol_id FROM dados)
    RETURNING id
)
INSERT INTO auditoria.acciones (actor_tipo, actor_nombre, canal, accion, entidad, entidad_id, entrada, resultado, request_id)
SELECT 'sistema', 'migracion', 'consola', 'roles.permisos_agregados_por_migracion', 'rol', d.rol_id::text,
    jsonb_build_object('permisos', jsonb_agg(d.permiso ORDER BY d.permiso), 'motivo', 'contrato_argos_080_servicios'),
    'ok', 'migracion-20261119024424'
FROM dados d
GROUP BY d.rol_id;

-- +goose Down
DELETE FROM nucleo.roles_permisos WHERE permiso IN ('servicios.ver_red', 'servicios.ver_ubicacion');
DROP INDEX integracion.argos_servicios_plan_idx;
ALTER TABLE integracion.argos_servicios
    DROP CONSTRAINT argos_servicios_precio_moneda_ck,
    DROP COLUMN planta,
    DROP COLUMN prefijo_delegado_v6,
    DROP COLUMN prefijo_enlace_v6,
    DROP COLUMN moneda,
    DROP COLUMN precio_mensual;

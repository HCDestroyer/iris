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

-- +goose Down
DROP INDEX integracion.argos_servicios_plan_idx;
ALTER TABLE integracion.argos_servicios
    DROP CONSTRAINT argos_servicios_precio_moneda_ck,
    DROP COLUMN planta,
    DROP COLUMN prefijo_delegado_v6,
    DROP COLUMN prefijo_enlace_v6,
    DROP COLUMN moneda,
    DROP COLUMN precio_mensual;

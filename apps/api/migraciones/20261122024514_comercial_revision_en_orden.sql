-- +goose Up
-- Venta revisada en la instalación (contrato argosisp.md 0.10.0 §3.4.8, ajuste e de §10.5; ALTA 1
-- de la revisión de #280). Cuando Argos acepta un cambio de plan o de titular, la cotización nueva
-- queda vigente (no aceptada) y el contrato regenerado queda en borrador ligado a la orden; el
-- contrato firmado sigue vivo hasta que el cliente firma el regenerado en la orden. Al firmar,
-- el firmado anterior se anula y su cotización queda reemplazada.
--
-- Contratos: a lo sumo uno firmado y uno en borrador por prospecto; el borrador ligado a una orden
-- (contenido.ordenId) puede convivir con el firmado, uno sin orden no.
DROP INDEX comercial.contratos_vivo_uq;
CREATE UNIQUE INDEX contratos_vivo_uq ON comercial.contratos (prospecto_id)
    WHERE estado IN ('borrador', 'firmado') AND (contenido->>'ordenId') IS NULL;
CREATE UNIQUE INDEX contratos_firmado_uq ON comercial.contratos (prospecto_id) WHERE estado = 'firmado';
CREATE UNIQUE INDEX contratos_borrador_orden_uq ON comercial.contratos (prospecto_id)
    WHERE estado = 'borrador' AND (contenido->>'ordenId') IS NOT NULL;

-- Cotizaciones: a lo sumo una aceptada por prospecto y una revisión de la instalación por firmar
-- (idempotencia iris:venta:*). Las demás, como antes: una sola viva.
DROP INDEX comercial.cotizaciones_viva_uq;
CREATE UNIQUE INDEX cotizaciones_viva_uq ON comercial.cotizaciones (prospecto_id)
    WHERE estado IN ('vigente', 'aceptada') AND coalesce(idempotencia, '') NOT LIKE 'iris:venta:%';
CREATE UNIQUE INDEX cotizaciones_aceptada_uq ON comercial.cotizaciones (prospecto_id) WHERE estado = 'aceptada';
CREATE UNIQUE INDEX cotizaciones_revision_orden_uq ON comercial.cotizaciones (prospecto_id)
    WHERE estado = 'vigente' AND idempotencia LIKE 'iris:venta:%';

-- +goose Down
DROP INDEX comercial.cotizaciones_revision_orden_uq;
DROP INDEX comercial.cotizaciones_aceptada_uq;
DROP INDEX comercial.cotizaciones_viva_uq;
-- Una revisión sin firmar no cabe en el índice de antes: queda reemplazada.
UPDATE comercial.cotizaciones c SET estado = 'reemplazada', updated_at = now()
WHERE c.estado = 'vigente' AND c.idempotencia LIKE 'iris:venta:%'
  AND EXISTS (SELECT 1 FROM comercial.cotizaciones a WHERE a.prospecto_id = c.prospecto_id AND a.estado = 'aceptada');
CREATE UNIQUE INDEX cotizaciones_viva_uq ON comercial.cotizaciones (prospecto_id) WHERE estado IN ('vigente', 'aceptada');
DROP INDEX comercial.contratos_borrador_orden_uq;
DROP INDEX comercial.contratos_firmado_uq;
DROP INDEX comercial.contratos_vivo_uq;
UPDATE comercial.contratos c SET estado = 'anulado', motivo_anulacion = 'Migración revertida: el contrato firmado sigue vivo',
    version = version + 1, updated_at = now()
WHERE c.estado = 'borrador' AND (c.contenido->>'ordenId') IS NOT NULL
  AND EXISTS (SELECT 1 FROM comercial.contratos f WHERE f.prospecto_id = c.prospecto_id AND f.estado = 'firmado');
CREATE UNIQUE INDEX contratos_vivo_uq ON comercial.contratos (prospecto_id) WHERE estado IN ('borrador', 'firmado');

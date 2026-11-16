-- +goose Up
-- Órdenes que crea el asistente de IA de mensajería al agendar una visita (contratos/conversaciones.md
-- §17.10): origen propio, para que despacho las distinga y para el evento orden.creada.
ALTER TABLE campo.ordenes DROP CONSTRAINT ordenes_origen_check;
ALTER TABLE campo.ordenes ADD CONSTRAINT ordenes_origen_check
    CHECK (origen IN ('panel', 'api', 'argos', 'sistema', 'asistente')) NOT VALID;
ALTER TABLE campo.ordenes VALIDATE CONSTRAINT ordenes_origen_check;

-- +goose Down
UPDATE campo.ordenes SET origen = 'sistema' WHERE origen = 'asistente';
ALTER TABLE campo.ordenes DROP CONSTRAINT ordenes_origen_check;
ALTER TABLE campo.ordenes ADD CONSTRAINT ordenes_origen_check
    CHECK (origen IN ('panel', 'api', 'argos', 'sistema'));

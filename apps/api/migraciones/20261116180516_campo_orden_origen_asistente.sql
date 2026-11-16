-- +goose NO TRANSACTION
-- +goose Up
-- Órdenes que crea el asistente de IA de mensajería al agendar una visita (contratos/conversaciones.md
-- §17.10): origen propio, para que despacho las distinga y para el evento orden.creada.
--
-- Sin transacción a propósito: la restricción nueva entra NOT VALID (no escanea campo.ordenes), se
-- valida en su propia sentencia (VALIDATE no bloquea lecturas ni escrituras) y solo entonces se quita
-- la anterior. La tabla nunca queda sin restricción; si algo falla a medias, repetir la migración a
-- mano es seguro (cada paso es idempotente).
ALTER TABLE campo.ordenes DROP CONSTRAINT IF EXISTS ordenes_origen_asistente_check;
ALTER TABLE campo.ordenes ADD CONSTRAINT ordenes_origen_asistente_check
    CHECK (origen IN ('panel', 'api', 'argos', 'sistema', 'asistente')) NOT VALID;
ALTER TABLE campo.ordenes VALIDATE CONSTRAINT ordenes_origen_asistente_check;
ALTER TABLE campo.ordenes DROP CONSTRAINT IF EXISTS ordenes_origen_check;

-- +goose Down
-- Las órdenes con origen «asistente» NO se tocan (no se re-etiquetan ni se borran). La restricción
-- anterior vuelve SIN VALIDAR: las filas viejas con «asistente» se quedan como están y ninguna fila
-- nueva puede usar ese valor. Para validarla después hay que decidir a mano qué origen llevan.
ALTER TABLE campo.ordenes DROP CONSTRAINT IF EXISTS ordenes_origen_check;
ALTER TABLE campo.ordenes ADD CONSTRAINT ordenes_origen_check
    CHECK (origen IN ('panel', 'api', 'argos', 'sistema')) NOT VALID;
ALTER TABLE campo.ordenes DROP CONSTRAINT IF EXISTS ordenes_origen_asistente_check;

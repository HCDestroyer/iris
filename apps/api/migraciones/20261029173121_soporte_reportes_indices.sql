-- +goose Up
-- Índices de los reportes (internal/reportes, I2). Todos parciales: solo las filas que un
-- reporte puede leer.
--
-- Tickets resueltos en un período (SLA cumplido, tiempo de resolución, reincidencias). Los
-- fusionados no cuentan.
CREATE INDEX tickets_resueltos_idx ON soporte.tickets (resuelto_en)
    WHERE resuelto_en IS NOT NULL AND fusionado_en_id IS NULL;
-- Reincidencias y primera visita: «¿el mismo servicio (o cliente) abrió otro ticket en los N días
-- siguientes?».
CREATE INDEX tickets_servicio_creado_idx ON soporte.tickets (servicio_id, created_at)
    WHERE servicio_id IS NOT NULL;
CREATE INDEX tickets_cliente_creado_idx ON soporte.tickets (cliente_id, created_at)
    WHERE cliente_id IS NOT NULL;
-- Órdenes finalizadas en un período (por técnico, primera visita).
CREATE INDEX ordenes_finalizadas_idx ON campo.ordenes (finalizada_en)
    WHERE finalizada_en IS NOT NULL;
-- Primera visita: «¿el mismo servicio tuvo otra orden en los N días siguientes?».
CREATE INDEX ordenes_servicio_creada_idx ON campo.ordenes (servicio_id, created_at)
    WHERE servicio_id IS NOT NULL;

-- +goose Down
DROP INDEX campo.ordenes_servicio_creada_idx;
DROP INDEX campo.ordenes_finalizadas_idx;
DROP INDEX soporte.tickets_cliente_creado_idx;
DROP INDEX soporte.tickets_servicio_creado_idx;
DROP INDEX soporte.tickets_resueltos_idx;

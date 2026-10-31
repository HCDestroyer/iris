-- +goose Up
-- Resolver a mano un pendiente rechazado por Argos (fallido_negocio, también con datos incompletos):
-- p. ej. el supervisor ya corrigió el consumo en Argos. Queda en el estado final «resuelto_manual»
-- (nunca vuelve a salir hacia Argos), con el motivo, quién y cuándo, y sus dependientes bloqueados
-- (el cargo que esperaba al consumo) vuelven a la cola como si estuviera hecho.
ALTER TABLE integracion.pendientes DROP CONSTRAINT pendientes_estado_check;
ALTER TABLE integracion.pendientes ADD CONSTRAINT pendientes_estado_check
    CHECK (estado IN ('pendiente', 'en_curso', 'hecho', 'fallido_negocio', 'agotado', 'bloqueado', 'resuelto_manual'));
ALTER TABLE integracion.pendientes
    ADD COLUMN resuelto_motivo text CHECK (resuelto_motivo IS NULL OR length(resuelto_motivo) BETWEEN 5 AND 1000),
    ADD COLUMN resuelto_por    bigint REFERENCES nucleo.usuarios (id),
    ADD COLUMN resuelto_en     timestamptz,
    ADD CONSTRAINT pendientes_resuelto_completo CHECK ((estado = 'resuelto_manual') = (resuelto_en IS NOT NULL));

-- +goose Down
UPDATE integracion.pendientes SET estado = 'fallido_negocio', resuelto_en = NULL WHERE estado = 'resuelto_manual';
ALTER TABLE integracion.pendientes
    DROP CONSTRAINT pendientes_resuelto_completo,
    DROP COLUMN resuelto_motivo, DROP COLUMN resuelto_por, DROP COLUMN resuelto_en;
ALTER TABLE integracion.pendientes DROP CONSTRAINT pendientes_estado_check;
ALTER TABLE integracion.pendientes ADD CONSTRAINT pendientes_estado_check
    CHECK (estado IN ('pendiente', 'en_curso', 'hecho', 'fallido_negocio', 'agotado', 'bloqueado'));

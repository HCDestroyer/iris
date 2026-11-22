-- +goose Up
-- Jefe de cuadrilla (contrato Iris ↔ ArgosISP 0.11.0 §3.9, pedido B26). El jefe recibe y firma los
-- traslados de la bodega de la cuadrilla. Debe ser miembro (ficha activa en la cuadrilla, usuario
-- activo) con vínculo I1 con Argos; si deja de serlo, la cuadrilla queda sin jefe. Lo asigna
-- POST /cuadrillas/{id}/asignar-jefe (cuadrillas.asignar_jefe).
--
-- version_argos: la versión que viaja a Argos en cada cuadrilla.* (entera y creciente; Argos solo
-- aplica una mayor que la última). Sube con cada evento, también cuando cambia un miembro (su
-- ficha, su vínculo I1 o su usuario). Es aparte de `version`, la de la edición en el panel: mover a
-- un técnico de cuadrilla no debe dar 409 a quien está editando el nombre de la cuadrilla.
ALTER TABLE agenda.cuadrillas
    ADD COLUMN jefe_usuario_id bigint REFERENCES nucleo.usuarios (id),
    ADD COLUMN version_argos integer NOT NULL DEFAULT 1;

CREATE INDEX cuadrillas_jefe_idx ON agenda.cuadrillas (jefe_usuario_id) WHERE jefe_usuario_id IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS agenda.cuadrillas_jefe_idx;
ALTER TABLE agenda.cuadrillas DROP COLUMN IF EXISTS jefe_usuario_id, DROP COLUMN IF EXISTS version_argos;

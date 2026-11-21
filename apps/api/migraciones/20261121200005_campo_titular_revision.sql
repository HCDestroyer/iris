-- +goose Up
-- Ajustes de la revisión de seguridad del paso «Confirmar titular y plan» (contrato argosisp.md
-- 0.10.0 §3.4.8, PR #273).

-- Revisión de un supervisor: Argos respondió titular_no_coincide (A39 o A41). La marca es
-- persistente: mientras está puesta no se toma otra identidad ni otro titular; solo la levanta un
-- supervisor (ordenes.validar_identidad) con motivo.
ALTER TABLE campo.orden_titular
    ADD COLUMN en_revision          boolean     NOT NULL DEFAULT false,
    ADD COLUMN revision_desde       timestamptz,
    ADD COLUMN revision_levantada_por bigint  REFERENCES nucleo.usuarios (id),
    ADD COLUMN revision_motivo      text,
    ADD COLUMN revision_levantada_en timestamptz;

-- Cómo quedó resuelto un pendiente (resuelto_manual): a mano en Argos, reemplazado por otra toma, la
-- orden se canceló o falló, o validado desde el panel. Un cambio de titular resuelto a mano deja el
-- cliente final por confirmar (resuelto_cliente_id, si quien lo resolvió lo indicó).
ALTER TABLE integracion.pendientes
    ADD COLUMN resuelto_como text CHECK (resuelto_como IN ('manual', 'reemplazado', 'orden_cancelada', 'orden_fallida', 'validado_panel')),
    ADD COLUMN resuelto_cliente_id bigint CHECK (resuelto_cliente_id > 0);

-- Llave de datos propia de cada depósito (sobre de llaves): el contenido va cifrado con ella y ella
-- con IRIS_CLAVE_CIFRADO. Al borrar el depósito se destruye la llave: lo cifrado que quede en el WAL
-- o en un respaldo ya no se puede abrir. La rotación de la clave solo vuelve a cifrar la llave.
ALTER TABLE integracion.datos_transito
    ADD COLUMN llave_cifrada bytea,
    ADD CONSTRAINT datos_transito_llave_check CHECK ((llave_cifrada IS NULL) = (cifrado IS NULL)) NOT VALID;

-- +goose Down
ALTER TABLE integracion.datos_transito DROP CONSTRAINT datos_transito_llave_check, DROP COLUMN llave_cifrada;
ALTER TABLE integracion.pendientes DROP COLUMN resuelto_cliente_id, DROP COLUMN resuelto_como;
ALTER TABLE campo.orden_titular DROP COLUMN revision_levantada_en, DROP COLUMN revision_motivo,
    DROP COLUMN revision_levantada_por, DROP COLUMN revision_desde, DROP COLUMN en_revision;

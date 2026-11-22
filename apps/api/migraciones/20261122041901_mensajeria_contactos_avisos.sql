-- +goose Up
-- Permiso de contacto por número (contrato argosisp.md §6.4: 0.10.0, A42; clave de 0.12.1) y avisos de
-- cobro a los contactos del cliente con «avisos» (§3.5, B22).

-- telefono: el número (E.164) de un contacto del cliente; null = el permiso del cliente (el de
-- siempre). El vigente se calcula por número.
ALTER TABLE mensajeria.consentimientos ADD COLUMN telefono text CHECK (telefono IS NULL OR telefono ~ '^\+[1-9][0-9]{7,14}$');
DROP INDEX mensajeria.consentimientos_hecho_uk;
CREATE UNIQUE INDEX consentimientos_hecho_uk ON mensajeria.consentimientos (empresa_id, cliente_id, canal, coalesce(telefono, ''), ocurrido_en, estado, origen);
DROP INDEX mensajeria.consentimientos_vigente_idx;
CREATE INDEX consentimientos_vigente_idx ON mensajeria.consentimientos (empresa_id, cliente_id, canal, coalesce(telefono, ''), ocurrido_en DESC);

-- Un aviso de cobro que sale también a un contacto: copia del aviso del cliente (aviso_padre_id) con
-- el número del contacto (destino) que Iris tomó de su caché, nunca del cuerpo del aviso. No tiene
-- Idempotency-Key ni webhooks hacia Argos (origen iris).
ALTER TABLE mensajeria.avisos ADD COLUMN aviso_padre_id uuid REFERENCES mensajeria.avisos (id);
ALTER TABLE mensajeria.avisos ADD COLUMN contacto_argos_id bigint CHECK (contacto_argos_id IS NULL OR contacto_argos_id > 0);
CREATE UNIQUE INDEX avisos_contacto_uk ON mensajeria.avisos (aviso_padre_id, contacto_argos_id) WHERE aviso_padre_id IS NOT NULL;

-- +goose Down
DROP INDEX mensajeria.avisos_contacto_uk;
ALTER TABLE mensajeria.avisos DROP COLUMN contacto_argos_id;
ALTER TABLE mensajeria.avisos DROP COLUMN aviso_padre_id;
DROP INDEX mensajeria.consentimientos_vigente_idx;
CREATE INDEX consentimientos_vigente_idx ON mensajeria.consentimientos (empresa_id, cliente_id, canal, ocurrido_en DESC);
DROP INDEX mensajeria.consentimientos_hecho_uk;
DELETE FROM mensajeria.consentimientos WHERE telefono IS NOT NULL;
CREATE UNIQUE INDEX consentimientos_hecho_uk ON mensajeria.consentimientos (empresa_id, cliente_id, canal, ocurrido_en, estado, origen);
ALTER TABLE mensajeria.consentimientos DROP COLUMN telefono;

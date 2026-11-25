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
-- copias_motivo: por qué un aviso de cobro no salió a los contactos (p. ej. sin_plantilla_contacto: la
-- plantilla de Meta lleva datos del titular). Historial, no un fallo.
ALTER TABLE mensajeria.avisos ADD COLUMN copias_motivo text;
-- NOT VALID: no recorre la tabla de avisos (las filas viejas no tienen el campo).
ALTER TABLE mensajeria.avisos ADD CONSTRAINT avisos_copias_motivo_ck
    CHECK (copias_motivo IS NULL OR copias_motivo IN ('sin_plantilla_contacto')) NOT VALID;
CREATE UNIQUE INDEX avisos_contacto_uk ON mensajeria.avisos (aviso_padre_id, contacto_argos_id) WHERE aviso_padre_id IS NOT NULL;

-- La verificación fuerte es de quien la pasó (§3.5, B22): si cambia el contacto con que se asoció la
-- conversación o se pierde la verificación del número, se borra (la del titular no pasa a un contacto
-- ni al revés).
-- +goose StatementBegin
CREATE FUNCTION mensajeria.conv_contacto_verificacion() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.contacto_argos_id IS DISTINCT FROM OLD.contacto_argos_id
        OR (OLD.cliente_verificado AND NOT NEW.cliente_verificado) THEN
        NEW.identidad_verificada_en := NULL;
        NEW.identidad_vence_en := NULL;
    END IF;
    RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER conv_contacto_verificacion BEFORE UPDATE OF contacto_argos_id, cliente_verificado ON mensajeria.conversaciones
    FOR EACH ROW EXECUTE FUNCTION mensajeria.conv_contacto_verificacion();

-- +goose Down
DROP TRIGGER conv_contacto_verificacion ON mensajeria.conversaciones;
DROP FUNCTION mensajeria.conv_contacto_verificacion();
DROP INDEX mensajeria.avisos_contacto_uk;
ALTER TABLE mensajeria.avisos DROP CONSTRAINT avisos_copias_motivo_ck;
ALTER TABLE mensajeria.avisos DROP COLUMN copias_motivo;
ALTER TABLE mensajeria.avisos DROP COLUMN contacto_argos_id;
ALTER TABLE mensajeria.avisos DROP COLUMN aviso_padre_id;
DROP INDEX mensajeria.consentimientos_vigente_idx;
CREATE INDEX consentimientos_vigente_idx ON mensajeria.consentimientos (empresa_id, cliente_id, canal, ocurrido_en DESC);
DROP INDEX mensajeria.consentimientos_hecho_uk;
DELETE FROM mensajeria.consentimientos WHERE telefono IS NOT NULL;
CREATE UNIQUE INDEX consentimientos_hecho_uk ON mensajeria.consentimientos (empresa_id, cliente_id, canal, ocurrido_en, estado, origen);
ALTER TABLE mensajeria.consentimientos DROP COLUMN telefono;

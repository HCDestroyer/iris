-- +goose Up
-- Vínculo del técnico con su usuario de Argos (contrato Iris ↔ ArgosISP 0.5.0 §3.3): id_externo de
-- GET /inventario/tecnicos, que va como `tecnico` en consumos, comodatos, devoluciones y
-- recepciones. Lo enlaza un administrador (agenda.vincular_argos) y no depende del vínculo de SSO
-- (nucleo.sso_vinculos se borra al reiniciar el MFA o cambiar el correo). Columna aditiva de I1
-- sobre la ficha de agenda (I2), coordinada en .claude/integration/i1.md.
ALTER TABLE agenda.tecnicos
    ADD COLUMN argos_id_externo    text CHECK (argos_id_externo IS NULL OR length(argos_id_externo) BETWEEN 1 AND 100),
    ADD COLUMN argos_correo        text CHECK (argos_correo IS NULL OR length(argos_correo) <= 320),
    ADD COLUMN argos_nombre        text CHECK (argos_nombre IS NULL OR length(argos_nombre) <= 200),
    ADD COLUMN argos_vinculado_en  timestamptz,
    ADD COLUMN argos_vinculado_por bigint REFERENCES nucleo.usuarios (id),
    ADD CONSTRAINT tecnicos_argos_completo CHECK ((argos_id_externo IS NULL) = (argos_vinculado_en IS NULL));
-- Un usuario de Argos es de un solo técnico de Iris.
CREATE UNIQUE INDEX tecnicos_argos_uq ON agenda.tecnicos (argos_id_externo) WHERE argos_id_externo IS NOT NULL;

-- +goose Down
DROP INDEX agenda.tecnicos_argos_uq;
ALTER TABLE agenda.tecnicos
    DROP CONSTRAINT tecnicos_argos_completo,
    DROP COLUMN argos_id_externo, DROP COLUMN argos_correo, DROP COLUMN argos_nombre,
    DROP COLUMN argos_vinculado_en, DROP COLUMN argos_vinculado_por;

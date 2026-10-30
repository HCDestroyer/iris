-- +goose Up
-- Sesiones abiertas por el autologin desde Argos (origen sso): vida más corta y re-autenticación
-- con contraseña (reautenticada_en) para las acciones sensibles.
ALTER TABLE nucleo.sesiones
    ADD COLUMN origen text NOT NULL DEFAULT 'password' CHECK (origen IN ('password', 'sso')),
    ADD COLUMN reautenticada_en timestamptz;
CREATE INDEX sso_tickets_conector_creado_idx ON nucleo.sso_tickets (conector_id, created_at);

-- +goose Down
DROP INDEX nucleo.sso_tickets_conector_creado_idx;
ALTER TABLE nucleo.sesiones DROP COLUMN reautenticada_en, DROP COLUMN origen;

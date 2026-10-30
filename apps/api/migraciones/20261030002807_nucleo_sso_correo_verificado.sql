-- +goose Up
-- Argos dice si el correo de su usuario está verificado (contrato 0.3.1): la autovinculación lo
-- exige además de que coincida con el de la cuenta de Iris.
ALTER TABLE nucleo.sso_tickets ADD COLUMN correo_verificado boolean NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE nucleo.sso_tickets DROP COLUMN correo_verificado;

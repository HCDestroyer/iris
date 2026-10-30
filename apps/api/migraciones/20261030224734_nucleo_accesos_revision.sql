-- +goose Up
-- Revisión de seguridad del pedido 10.
-- 1. Los fallos repetidos (mismo usuario o, sin usuario, misma IP, y mismo resultado) dentro de
--    la ventana suman a la fila existente en vez de crear otra: el historial no se inunda.
ALTER TABLE nucleo.accesos ADD COLUMN intentos integer NOT NULL DEFAULT 1 CHECK (intentos >= 1);

-- 2. Conexiones de la app: lista cerrada (app campo; plataforma pwa, android o ios). Como mucho
--    tres filas por usuario.
DELETE FROM nucleo.conexiones_app WHERE app <> 'campo' OR plataforma NOT IN ('pwa', 'android', 'ios');
ALTER TABLE nucleo.conexiones_app
    ADD CONSTRAINT conexiones_app_lista_cerrada CHECK (app = 'campo' AND plataforma IN ('pwa', 'android', 'ios'));

-- +goose Down
ALTER TABLE nucleo.conexiones_app DROP CONSTRAINT conexiones_app_lista_cerrada;
ALTER TABLE nucleo.accesos DROP COLUMN intentos;

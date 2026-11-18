-- +goose Up
-- Con qué rechazó Meta un mensaje: el status HTTP y el código numérico (y subcódigo) de su error.
-- Solo números: el texto libre de Meta sigue en detalle_falla, que no sale por la API.
ALTER TABLE mensajeria.conversacion_mensajes ADD COLUMN falla_status integer;
ALTER TABLE mensajeria.conversacion_mensajes ADD COLUMN falla_codigo integer;
ALTER TABLE mensajeria.conversacion_mensajes ADD COLUMN falla_subcodigo integer;

-- Llamadas que llegaron a un número con las llamadas apagadas en Iris y se rechazaron sin registro.
-- Solo cuándo: sin número, sin call_id, sin nada del cliente. Es lo que deja ver en la pantalla de
-- llamadas del número que en Meta están encendidas y en Iris no.
CREATE TABLE mensajeria.llamadas_rechazadas_apagadas (
    id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    canal_id    bigint      NOT NULL REFERENCES mensajeria.canales (id) ON DELETE CASCADE,
    ocurrido_en timestamptz NOT NULL
);
CREATE INDEX llamadas_rechazadas_apagadas_canal_idx ON mensajeria.llamadas_rechazadas_apagadas (canal_id, ocurrido_en DESC);

-- +goose Down
DROP TABLE mensajeria.llamadas_rechazadas_apagadas;
ALTER TABLE mensajeria.conversacion_mensajes DROP COLUMN falla_subcodigo;
ALTER TABLE mensajeria.conversacion_mensajes DROP COLUMN falla_codigo;
ALTER TABLE mensajeria.conversacion_mensajes DROP COLUMN falla_status;

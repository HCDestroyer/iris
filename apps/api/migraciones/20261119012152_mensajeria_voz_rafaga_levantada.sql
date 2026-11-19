-- +goose Up
-- Ráfaga levantada (contratos/voz.md §3.5): desde este instante las entrantes anteriores de ese número
-- a ese canal dejan de contar para la ráfaga. La levanta una persona a mano («Dejar que vuelva a
-- llamar») o una saliente de la empresa a ese número. Una fila por canal y número: la última.
CREATE TABLE mensajeria.voz_rafagas_levantadas (
    canal_id      bigint      NOT NULL REFERENCES mensajeria.canales (id) ON DELETE CASCADE,
    numero        text        NOT NULL,
    empresa_id    bigint,
    levantada_en  timestamptz NOT NULL,
    levantada_por bigint,
    origen        text        NOT NULL CHECK (origen IN ('manual', 'llamada')),
    PRIMARY KEY (canal_id, numero)
);

-- Hasta cuándo seguía la ráfaga cuando se rechazó esta llamada (solo motivo_fin = 'limite'): lo que
-- el hilo dice con «vuelve a sonar a las HH:MM».
ALTER TABLE mensajeria.llamadas ADD COLUMN rafaga_hasta timestamptz;
-- Reintentos que la ráfaga rechazó después de esta llamada, en la misma ventana: se cuentan aquí en
-- vez de dejar una fila, un mensaje y un evento por intento.
ALTER TABLE mensajeria.llamadas ADD COLUMN rafaga_intentos integer NOT NULL DEFAULT 0 CHECK (rafaga_intentos >= 0);

-- +goose Down
ALTER TABLE mensajeria.llamadas DROP COLUMN rafaga_intentos;
ALTER TABLE mensajeria.llamadas DROP COLUMN rafaga_hasta;
DROP TABLE mensajeria.voz_rafagas_levantadas;

-- +goose Up
-- Ráfaga levantada (contratos/voz.md §3.5): desde este instante las entrantes anteriores de ese número
-- a ese canal dejan de contar para la ráfaga. La levanta una persona a mano («Dejar que vuelva a
-- llamar») o una saliente de la empresa a ese número. Una fila por canal y número: la última. El
-- mantenimiento borra las que tienen más de 10 minutos (ya no cambian nada y guardan el número).
CREATE TABLE mensajeria.voz_rafagas_levantadas (
    canal_id      bigint      NOT NULL REFERENCES mensajeria.canales (id) ON DELETE CASCADE,
    numero        text        NOT NULL,
    empresa_id    bigint,
    levantada_en  timestamptz NOT NULL,
    levantada_por bigint,
    origen        text        NOT NULL CHECK (origen IN ('manual', 'llamada')),
    PRIMARY KEY (canal_id, numero)
);
CREATE INDEX voz_rafagas_levantadas_en_idx ON mensajeria.voz_rafagas_levantadas (levantada_en);

-- En la llamada que la ráfaga rechazó (motivo_fin = 'limite'), su tarjeta del hilo:
--   rafaga_hasta: hasta cuándo seguía la ráfaga («vuelve a sonar a las HH:MM»);
--   rafaga_intentos: reintentos de la misma ventana, contados aquí en vez de dejar una fila, un
--     mensaje y un evento por intento;
--   rafaga_ultimo_call_id: el último reintento contado (un connect repetido de Meta no cuenta dos veces);
--   rafaga_avisada_en: la última vez que se refrescó la tarjeta en el feed (como mucho cada 30 s).
ALTER TABLE mensajeria.llamadas
    ADD COLUMN rafaga_hasta timestamptz,
    ADD COLUMN rafaga_intentos integer NOT NULL DEFAULT 0,
    ADD COLUMN rafaga_ultimo_call_id text,
    ADD COLUMN rafaga_avisada_en timestamptz;
-- Sin validar (NOT VALID): el ALTER no recorre la tabla con su candado ACCESS EXCLUSIVE. Vale desde ya
-- para cada fila que se escriba; la valida …_voz_rafaga_levantada_validar, fuera de una transacción.
ALTER TABLE mensajeria.llamadas ADD CONSTRAINT llamadas_rafaga_intentos_ck CHECK (rafaga_intentos >= 0) NOT VALID;

-- +goose Down
ALTER TABLE mensajeria.llamadas DROP CONSTRAINT llamadas_rafaga_intentos_ck;
ALTER TABLE mensajeria.llamadas
    DROP COLUMN rafaga_avisada_en,
    DROP COLUMN rafaga_ultimo_call_id,
    DROP COLUMN rafaga_intentos,
    DROP COLUMN rafaga_hasta;
DROP TABLE mensajeria.voz_rafagas_levantadas;

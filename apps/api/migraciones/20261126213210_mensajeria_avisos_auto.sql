-- +goose Up
-- Recordatorios que entrega Iris (contrato argosisp.md 0.15.0 §6.2 y §13.3, bloque C): canal_preferido
-- admite «auto» (Iris elige un solo canal de los que declara para la empresa) con la lista opcional
-- canales_permitidos, y el aviso guarda suscripcion_id y recordatorio, solo informativos.
ALTER TABLE mensajeria.avisos DROP CONSTRAINT avisos_canal_preferido_check;
ALTER TABLE mensajeria.avisos ADD CONSTRAINT avisos_canal_preferido_check
    CHECK (canal_preferido IN ('whatsapp', 'telegram', 'correo', 'auto'));
-- null = sin lista (todos los canales declarados); nunca vacía.
ALTER TABLE mensajeria.avisos ADD COLUMN canales_permitidos text[]
    CHECK (canales_permitidos IS NULL OR cardinality(canales_permitidos) BETWEEN 1 AND 5);
ALTER TABLE mensajeria.avisos ADD COLUMN suscripcion_id bigint CHECK (suscripcion_id IS NULL OR suscripcion_id > 0);
-- { disparador, orden } tal como lo mandó Argos.
ALTER TABLE mensajeria.avisos ADD COLUMN recordatorio jsonb;

-- +goose Down
ALTER TABLE mensajeria.avisos DROP COLUMN recordatorio;
ALTER TABLE mensajeria.avisos DROP COLUMN suscripcion_id;
ALTER TABLE mensajeria.avisos DROP COLUMN canales_permitidos;
-- Un aviso auto queda con el canal por el que salió (o whatsapp, el único que Iris entregaba).
UPDATE mensajeria.avisos SET canal_preferido = coalesce(canal, 'whatsapp') WHERE canal_preferido = 'auto';
ALTER TABLE mensajeria.avisos DROP CONSTRAINT avisos_canal_preferido_check;
ALTER TABLE mensajeria.avisos ADD CONSTRAINT avisos_canal_preferido_check
    CHECK (canal_preferido IN ('whatsapp', 'telegram', 'correo'));

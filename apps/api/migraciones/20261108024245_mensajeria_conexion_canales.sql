-- +goose Up
-- Conexión de los números de WhatsApp (contratos/conversaciones.md §1.1):
--
-- 1. Última prueba con la Graph API (POST /canales/{id}/probar): cuándo, resultado y detalle corto
--    (nunca credenciales).
-- 2. Webhook: última verificación correcta de Meta (GET con hub.challenge) y último webhook firmado
--    recibido (con precisión de 1 minuto, para no escribir la fila en cada mensaje).
-- 3. Cuándo se guardó cada credencial (el panel muestra «•••• guardado el …»), nunca el valor.
--
-- Ninguna de estas columnas sube `version`: no son ediciones del administrador.
ALTER TABLE mensajeria.canales
    ADD COLUMN ultima_prueba_en         timestamptz,
    ADD COLUMN ultimo_estado            text CHECK (ultimo_estado IN ('conectado', 'token_invalido', 'numero_no_coincide', 'sin_permiso', 'error_meta')),
    ADD COLUMN ultimo_detalle           text CHECK (length(ultimo_detalle) <= 300),
    ADD COLUMN webhook_verificado_en    timestamptz,
    ADD COLUMN ultimo_mensaje_en        timestamptz,
    ADD COLUMN token_guardado_en        timestamptz,
    ADD COLUMN app_secret_guardado_en   timestamptz,
    ADD COLUMN verify_token_guardado_en timestamptz;

-- Los números que ya existían: credenciales guardadas al crearlos (o en su última edición), y el
-- webhook se da por verificado si ya llegó alguno firmado (Meta solo los manda tras verificar).
UPDATE mensajeria.canales c
SET token_guardado_en = c.updated_at, app_secret_guardado_en = c.updated_at, verify_token_guardado_en = c.updated_at,
    ultimo_mensaje_en = e.ultimo, webhook_verificado_en = e.primero
FROM (SELECT ca.id, min(en.created_at) AS primero, max(en.created_at) AS ultimo
      FROM mensajeria.canales ca LEFT JOIN mensajeria.entrantes en ON en.canal_id = ca.id
      GROUP BY ca.id) e
WHERE e.id = c.id;

-- +goose Down
ALTER TABLE mensajeria.canales
    DROP COLUMN verify_token_guardado_en,
    DROP COLUMN app_secret_guardado_en,
    DROP COLUMN token_guardado_en,
    DROP COLUMN ultimo_mensaje_en,
    DROP COLUMN webhook_verificado_en,
    DROP COLUMN ultimo_detalle,
    DROP COLUMN ultimo_estado,
    DROP COLUMN ultima_prueba_en;

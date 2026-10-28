-- +goose Up
-- Soporte (I2): verificación del remitente de los correos y límites de lectura MIME (revisión
-- de seguridad de la PR #8).
--
-- - buzones.authserv_id: el authserv-id de la cabecera Authentication-Results que pone el
--   servidor de correo de confianza (RFC 8601). Null = no se confía en ninguna: ningún
--   remitente queda verificado.
-- - correos_entrantes.verificado / autenticacion: si el From quedó autenticado (dmarc=pass, o
--   dkim=pass con dominio alineado) según esa cabecera, y el resumen de lo que se usó.
-- - correos_entrantes.limite: por qué el correo se guardó truncado (tamano, mime_anidado,
--   partes, profundidad, bytes, tiempo). truncado sigue siendo el indicador.
-- - correos_entrantes.resultado nota_interna: el correo llegó a un hilo de ticket pero el
--   remitente no es el contacto o no está verificado: queda como nota interna.
-- - tickets.contacto_verificado: false si el ticket nació de un correo cuyo From no se pudo
--   verificar (el panel lo muestra). Los tickets capturados a mano quedan en true.
ALTER TABLE soporte.buzones ADD COLUMN authserv_id text;

ALTER TABLE soporte.correos_entrantes
    ADD COLUMN verificado    boolean NOT NULL DEFAULT false,
    ADD COLUMN autenticacion text,
    ADD COLUMN limite        text CHECK (limite IN ('tamano', 'mime_anidado', 'partes', 'profundidad', 'bytes', 'tiempo'));
ALTER TABLE soporte.correos_entrantes DROP CONSTRAINT correos_entrantes_resultado_check;
ALTER TABLE soporte.correos_entrantes ADD CONSTRAINT correos_entrantes_resultado_check
    CHECK (resultado IN ('ticket_creado', 'respuesta', 'nota_interna'));

ALTER TABLE soporte.tickets ADD COLUMN contacto_verificado boolean NOT NULL DEFAULT true;

-- +goose Down
ALTER TABLE soporte.tickets DROP COLUMN contacto_verificado;
ALTER TABLE soporte.correos_entrantes DROP CONSTRAINT correos_entrantes_resultado_check;
UPDATE soporte.correos_entrantes SET resultado = 'respuesta' WHERE resultado = 'nota_interna';
ALTER TABLE soporte.correos_entrantes ADD CONSTRAINT correos_entrantes_resultado_check
    CHECK (resultado IN ('ticket_creado', 'respuesta'));
ALTER TABLE soporte.correos_entrantes DROP COLUMN limite, DROP COLUMN autenticacion, DROP COLUMN verificado;
ALTER TABLE soporte.buzones DROP COLUMN authserv_id;

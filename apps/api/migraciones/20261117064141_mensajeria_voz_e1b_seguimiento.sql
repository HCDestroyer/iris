-- +goose Up
-- Voz E1b, seguimiento: una solicitud de permiso de llamada cuyo envío quedó sin respuesta de Meta
-- (pudo llegar al cliente) no se descuenta a ciegas: queda «sin confirmar» hasta resolverla con
-- call_permissions o con la respuesta del cliente.
ALTER TABLE mensajeria.llamada_permisos ADD COLUMN solicitud_sin_confirmar boolean NOT NULL DEFAULT false;
-- Cuántas veces se le preguntó a Meta por ella y cuándo toca la siguiente (espera creciente). No se
-- usa sincronizado_en: esa marca es la de una lectura buena de call_permissions.
ALTER TABLE mensajeria.llamada_permisos ADD COLUMN solicitud_intentos integer NOT NULL DEFAULT 0;
ALTER TABLE mensajeria.llamada_permisos ADD COLUMN solicitud_proximo_en timestamptz;

-- Una entrante que el agente dejó mientras la contestaba (cerró la pestaña antes de haber audio): para
-- el cliente es una llamada perdida, con su propio motivo.
ALTER TABLE mensajeria.llamadas DROP CONSTRAINT llamadas_motivo_fin_check;
ALTER TABLE mensajeria.llamadas ADD CONSTRAINT llamadas_motivo_fin_check CHECK (motivo_fin IN ('cliente_colgo', 'agente_colgo', 'asistente_termino',
    'nadie_contesto', 'rechazada_agente', 'fuera_de_horario', 'sin_capacidad', 'motor_no_disponible', 'duracion_maxima', 'silencio', 'mal_uso', 'tope',
    'limite', 'error_medio', 'error_meta', 'sin_permiso', 'transferencia_fallida', 'tope_alcanzado', 'agente_salio'));

-- +goose Down
UPDATE mensajeria.llamadas SET motivo_fin = 'nadie_contesto' WHERE motivo_fin = 'agente_salio';
ALTER TABLE mensajeria.llamadas DROP CONSTRAINT llamadas_motivo_fin_check;
ALTER TABLE mensajeria.llamadas ADD CONSTRAINT llamadas_motivo_fin_check CHECK (motivo_fin IN ('cliente_colgo', 'agente_colgo', 'asistente_termino',
    'nadie_contesto', 'rechazada_agente', 'fuera_de_horario', 'sin_capacidad', 'motor_no_disponible', 'duracion_maxima', 'silencio', 'mal_uso', 'tope',
    'limite', 'error_medio', 'error_meta', 'sin_permiso', 'transferencia_fallida', 'tope_alcanzado'));
ALTER TABLE mensajeria.llamada_permisos DROP COLUMN solicitud_proximo_en;
ALTER TABLE mensajeria.llamada_permisos DROP COLUMN solicitud_intentos;
ALTER TABLE mensajeria.llamada_permisos DROP COLUMN solicitud_sin_confirmar;

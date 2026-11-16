-- +goose Up
-- Asistente de IA, fase 3a: correcciones de la revisión de seguridad de la #159.

-- 1. Quién puso la conversación en su cola actual: «inicial» (la del número al crearla), «asistente»
-- (por su categoría) u «otro» (una persona, el menú del bot, un préstamo). El asistente solo mueve una
-- conversación que está en su cola inicial o donde él mismo la dejó, y una vez por periodo abierto.
-- Las que ya existían quedan como «otro»: no se sabe quién las movió.
ALTER TABLE mensajeria.conversaciones
    ADD COLUMN cola_origen text NOT NULL DEFAULT 'inicial' CHECK (cola_origen IN ('inicial', 'asistente', 'otro')),
    ADD COLUMN asistente_transfirio_en timestamptz;
UPDATE mensajeria.conversaciones SET cola_origen = 'otro';

-- +goose StatementBegin
CREATE FUNCTION mensajeria.conv_cola_origen() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    -- Cualquier cambio de cola que no diga quién fue (transferir, menú del bot, préstamo) es «otro».
    IF NEW.cola_origen = OLD.cola_origen THEN
        NEW.cola_origen := 'otro';
    END IF;
    RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER conv_cola_origen BEFORE UPDATE OF cola_id ON mensajeria.conversaciones
    FOR EACH ROW WHEN (OLD.cola_id IS DISTINCT FROM NEW.cola_id) EXECUTE FUNCTION mensajeria.conv_cola_origen();

-- 2. Una conversación solo lleva una categoría de su propia empresa (también al pasar de empresa
-- con un número prestado).
-- +goose StatementBegin
CREATE FUNCTION mensajeria.conv_categoria_empresa() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.categoria_id IS NOT NULL AND NOT EXISTS (
        SELECT 1 FROM mensajeria.asistente_categorias k
        WHERE k.id = NEW.categoria_id AND coalesce(k.empresa_id, 0) = coalesce(NEW.empresa_id, 0)) THEN
        RAISE EXCEPTION 'la categoría % no es de la empresa de la conversación %', NEW.categoria_id, NEW.id
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER conv_categoria_empresa BEFORE INSERT OR UPDATE OF categoria_id, empresa_id ON mensajeria.conversaciones
    FOR EACH ROW EXECUTE FUNCTION mensajeria.conv_categoria_empresa();

-- 3. Las categorías iniciales de las empresas que ya tenían ajustes del asistente (GET ya no siembra).
INSERT INTO mensajeria.asistente_categorias (empresa_id, nombre, descripcion, tono, modo, condicion, es_otro, orden)
SELECT a.empresa_id, v.nombre, v.descripcion, v.tono, v.modo, v.condicion, v.es_otro, v.orden
FROM mensajeria.asistente_ajustes a
CROSS JOIN (VALUES
    ('Planes y precios', 'Pregunta por planes, velocidades, precios, promociones o cobertura', 'info', 'responde_solo', NULL, false, 1),
    ('Sin internet', 'No tiene servicio, está lento, se corta o el equipo tiene una luz roja', 'error', 'responde_solo', 'si_hay_falla', false, 2),
    ('Pagos y saldo', 'Saldo, facturas, formas de pago, boletas, promesas de pago o reconexión', 'ok', 'responde_solo', 'solo_verificados', false, 3),
    ('Cambio de plan', 'Quiere subir, bajar o cambiar su plan actual', 'info', 'sugiere', NULL, false, 4),
    ('Mudanza o traslado', 'Se muda o quiere trasladar el servicio a otra dirección', 'aviso', 'sugiere', NULL, false, 5),
    ('Otro', 'Cualquier otro tema', 'neutro', 'sugiere', NULL, true, 6)
) AS v (nombre, descripcion, tono, modo, condicion, es_otro, orden)
WHERE NOT EXISTS (SELECT 1 FROM mensajeria.asistente_categorias k WHERE coalesce(k.empresa_id, 0) = coalesce(a.empresa_id, 0));

-- 4. Límite de clasificaciones aparte por empresa y hora.
CREATE INDEX asistente_clasificaciones_empresa_idx ON mensajeria.asistente_clasificaciones (coalesce(empresa_id, 0), creada_en);

-- +goose Down
DROP INDEX mensajeria.asistente_clasificaciones_empresa_idx;
DROP TRIGGER conv_categoria_empresa ON mensajeria.conversaciones;
DROP FUNCTION mensajeria.conv_categoria_empresa();
DROP TRIGGER conv_cola_origen ON mensajeria.conversaciones;
DROP FUNCTION mensajeria.conv_cola_origen();
ALTER TABLE mensajeria.conversaciones DROP COLUMN cola_origen, DROP COLUMN asistente_transfirio_en;

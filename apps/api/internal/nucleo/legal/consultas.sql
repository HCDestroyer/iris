-- Datos legales del titular (contratos/legal.md). Hoy solo el ámbito de la instalación
-- (empresa_id null).

-- name: DatosLegalesGlobales :one
SELECT * FROM nucleo.datos_legales WHERE empresa_id IS NULL;

-- name: DatosLegalesGlobalesParaActualizar :one
SELECT * FROM nucleo.datos_legales WHERE empresa_id IS NULL FOR UPDATE;

-- name: InsertarDatosLegalesGlobales :one
INSERT INTO nucleo.datos_legales (razon_social, nombre_comercial, correo_privacidad, telefono, direccion, pais, updated_at)
VALUES (@razon_social, @nombre_comercial, @correo_privacidad, @telefono, @direccion, @pais, @ahora)
ON CONFLICT DO NOTHING
RETURNING *;

-- name: ActualizarDatosLegales :one
UPDATE nucleo.datos_legales
SET razon_social = @razon_social, nombre_comercial = @nombre_comercial, correo_privacidad = @correo_privacidad,
    telefono = @telefono, direccion = @direccion, pais = @pais, version = version + 1, updated_at = @ahora
WHERE id = @id
RETURNING *;

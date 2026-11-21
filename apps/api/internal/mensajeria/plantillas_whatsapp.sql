-- Plantillas de WhatsApp por cuenta (contratos/plantillas-whatsapp.md). sqlc: mensajeriabd.

-- name: BloquearCuentaPlantillas :exec
SELECT pg_advisory_xact_lock(hashtextextended('mensajeria.plantillas_whatsapp:' || @waba_id::text, 0));

-- name: InsertarPlantillaWA :one
INSERT INTO mensajeria.plantillas_whatsapp (empresa_id, waba_id, meta_id, nombre, idioma, etiqueta, categoria, estado, motivo, rechazo, calidad,
    formato, componentes, componentes_meta, codigo, parametros, usos, origen, sugerida, enviada_en, sincronizada_en, creada_por)
VALUES (sqlc.narg('empresa_id'), @waba_id, sqlc.narg('meta_id'), @nombre, @idioma, @etiqueta, @categoria, @estado, sqlc.narg('motivo'), sqlc.narg('rechazo'),
    @calidad, @formato, @componentes, sqlc.narg('componentes_meta'), sqlc.narg('codigo'), @parametros, @usos, @origen, sqlc.narg('sugerida'),
    sqlc.narg('enviada_en'), sqlc.narg('sincronizada_en'), sqlc.narg('creada_por'))
RETURNING *;

-- name: GuardarPlantillaWA :one
UPDATE mensajeria.plantillas_whatsapp
SET meta_id = sqlc.narg('meta_id'), nombre = @nombre, idioma = @idioma, etiqueta = @etiqueta, categoria = @categoria,
    categoria_anterior = sqlc.narg('categoria_anterior'), categoria_cambiada_en = sqlc.narg('categoria_cambiada_en'),
    categoria_proxima = sqlc.narg('categoria_proxima'), categoria_proxima_en = sqlc.narg('categoria_proxima_en'),
    estado = @estado, estado_en = @estado_en, motivo = sqlc.narg('motivo'), rechazo = sqlc.narg('rechazo'), calidad = @calidad,
    pausada_hasta = sqlc.narg('pausada_hasta'), formato = @formato, componentes = @componentes, componentes_meta = sqlc.narg('componentes_meta'),
    codigo = sqlc.narg('codigo'), parametros = @parametros, usos = @usos, ediciones = @ediciones, enviada_en = sqlc.narg('enviada_en'),
    borrada_en = sqlc.narg('borrada_en'), borrada_por = sqlc.narg('borrada_por'), nombre_libre_en = sqlc.narg('nombre_libre_en'),
    sincronizada_en = sqlc.narg('sincronizada_en'), version = version + 1, updated_at = now()
WHERE id = @id
RETURNING *;

-- name: PlantillaWAPorID :one
SELECT * FROM mensajeria.plantillas_whatsapp WHERE id = @id AND coalesce(empresa_id, 0) = @empresa::bigint;

-- name: PlantillaWAParaActualizar :one
SELECT * FROM mensajeria.plantillas_whatsapp WHERE id = @id AND coalesce(empresa_id, 0) = @empresa::bigint FOR UPDATE;

-- name: PlantillasWADeCuenta :many
SELECT * FROM mensajeria.plantillas_whatsapp WHERE waba_id = @waba_id ORDER BY id FOR UPDATE;

-- name: PlantillaWAPorMeta :one
SELECT * FROM mensajeria.plantillas_whatsapp
WHERE waba_id = @waba_id AND (meta_id = @meta_id OR (meta_id IS NULL AND estado <> 'BORRADOR' AND nombre = @nombre AND idioma = @idioma))
  AND estado <> 'DELETED'
ORDER BY meta_id NULLS LAST, id
LIMIT 1 FOR UPDATE;

-- name: PlantillaWAPorNombre :one
SELECT * FROM mensajeria.plantillas_whatsapp
WHERE waba_id = @waba_id AND nombre = @nombre AND idioma = @idioma AND estado <> 'DELETED'
FOR UPDATE;

-- name: ListarPlantillasWA :many
SELECT * FROM mensajeria.plantillas_whatsapp p
WHERE coalesce(p.empresa_id, 0) = @empresa::bigint
  AND (@waba_id::text = '' OR p.waba_id = @waba_id::text)
  AND (@categoria::text = '' OR p.categoria = @categoria::text)
  AND (@idioma::text = '' OR p.idioma = @idioma::text)
  AND (@buscar::text = '' OR p.nombre ILIKE '%' || @buscar::text || '%' OR p.etiqueta ILIKE '%' || @buscar::text || '%')
  AND (@grupo::text = ''
       OR (@grupo::text = 'activas' AND p.estado IN ('APPROVED', 'FLAGGED'))
       OR (@grupo::text = 'revision' AND p.estado IN ('PENDING', 'IN_APPEAL', 'BORRADOR'))
       OR (@grupo::text = 'atender' AND (p.estado IN ('REJECTED', 'PAUSED', 'DISABLED', 'LIMIT_EXCEEDED', 'LOCKED')
            OR (p.estado = 'DELETED' AND p.borrada_por = 'meta' AND (p.codigo IS NOT NULL OR cardinality(p.usos) > 0))
            OR p.categoria_anterior IS NOT NULL OR p.categoria_proxima IS NOT NULL)))
  AND (@incluir_borradas::boolean OR p.estado <> 'DELETED' OR (p.borrada_por = 'meta' AND (p.codigo IS NOT NULL OR cardinality(p.usos) > 0)))
  AND p.id > @despues::bigint
ORDER BY p.id
LIMIT @limite;

-- name: ContarPlantillasWA :one
SELECT
    count(*) FILTER (WHERE estado <> 'DELETED' OR (borrada_por = 'meta' AND (codigo IS NOT NULL OR cardinality(usos) > 0)))::bigint AS todas,
    count(*) FILTER (WHERE estado IN ('APPROVED', 'FLAGGED'))::bigint AS activas,
    count(*) FILTER (WHERE estado IN ('PENDING', 'IN_APPEAL', 'BORRADOR'))::bigint AS revision,
    count(*) FILTER (WHERE estado IN ('REJECTED', 'PAUSED', 'DISABLED', 'LIMIT_EXCEEDED', 'LOCKED')
        OR (estado = 'DELETED' AND borrada_por = 'meta' AND (codigo IS NOT NULL OR cardinality(usos) > 0))
        OR categoria_anterior IS NOT NULL OR categoria_proxima IS NOT NULL)::bigint AS atender
FROM mensajeria.plantillas_whatsapp
WHERE coalesce(empresa_id, 0) = @empresa::bigint AND (@waba_id::text = '' OR waba_id = @waba_id::text);

-- name: PlantillasWAUsables :many
-- Las que pueden salir: con código y aprobadas en Meta (sin pausa vigente).
SELECT * FROM mensajeria.plantillas_whatsapp
WHERE waba_id = @waba_id AND codigo IS NOT NULL AND estado IN ('APPROVED', 'FLAGGED')
  AND (pausada_hasta IS NULL OR pausada_hasta <= @ahora)
ORDER BY codigo;

-- name: PlantillasWACodigoDe :many
SELECT * FROM mensajeria.plantillas_whatsapp
WHERE waba_id = @waba_id AND codigo = @codigo AND id <> @excepto
ORDER BY id FOR UPDATE;

-- name: NombreWABloqueado :one
SELECT nombre_libre_en FROM mensajeria.plantillas_whatsapp
WHERE waba_id = @waba_id AND nombre = @nombre AND estado = 'DELETED' AND nombre_libre_en > @ahora
ORDER BY nombre_libre_en DESC LIMIT 1;

-- name: NombreWAEnUso :one
SELECT EXISTS (SELECT 1 FROM mensajeria.plantillas_whatsapp
    WHERE waba_id = @waba_id AND nombre = @nombre AND idioma = @idioma AND estado <> 'DELETED' AND id <> @excepto)::boolean;

-- name: NombresWAParecidos :many
SELECT nombre FROM mensajeria.plantillas_whatsapp
WHERE waba_id = @waba_id AND (nombre = @nombre OR nombre LIKE @nombre || '\_%') AND (estado <> 'DELETED' OR nombre_libre_en > @ahora);

-- name: InsertarHistorialPlantillaWA :exec
INSERT INTO mensajeria.plantillas_whatsapp_historial (plantilla_id, tipo, de, a, detalle, origen, actor_id, ocurrido_en)
VALUES (@plantilla_id, @tipo, sqlc.narg('de'), sqlc.narg('a'), @detalle, @origen, sqlc.narg('actor_id'), @ocurrido_en);

-- name: HistorialPlantillaWA :many
SELECT h.*, u.nombre AS actor_nombre
FROM mensajeria.plantillas_whatsapp_historial h
LEFT JOIN nucleo.usuarios u ON u.id = h.actor_id
WHERE h.plantilla_id = @plantilla_id
ORDER BY h.id DESC
LIMIT 100;

-- name: CuentasWADeEmpresa :many
-- Las cuentas (WABA) de los números de la empresa, con sus números activos.
SELECT c.waba_id, min(c.empresa_id)::bigint AS empresa_id,
       array_agg(c.numero ORDER BY c.avisos_empresa DESC, c.id)::text[] AS numeros,
       array_agg(c.nombre ORDER BY c.avisos_empresa DESC, c.id)::text[] AS nombres,
       k.sincronizada_en, k.resumen, k.error, k.error_en
FROM mensajeria.canales c
LEFT JOIN mensajeria.plantillas_whatsapp_cuentas k ON k.waba_id = c.waba_id
WHERE coalesce(c.empresa_id, 0) = @empresa::bigint AND c.waba_id <> '' AND c.activo AND c.desconectado_en IS NULL
GROUP BY c.waba_id, k.sincronizada_en, k.resumen, k.error, k.error_en
ORDER BY c.waba_id;

-- name: CanalesDeCuentaWA :many
-- Los números activos de la cuenta (el de avisos primero): el token para hablar con Meta.
SELECT * FROM mensajeria.canales
WHERE waba_id = @waba_id AND activo AND desconectado_en IS NULL
ORDER BY avisos_empresa DESC, (tipo_conexion = 'qr'), id;

-- name: CuentaWA :one
SELECT * FROM mensajeria.plantillas_whatsapp_cuentas WHERE waba_id = @waba_id;

-- name: GuardarCuentaWA :exec
INSERT INTO mensajeria.plantillas_whatsapp_cuentas (waba_id, empresa_id, sincronizada_en, resumen, error, error_en)
VALUES (@waba_id, sqlc.narg('empresa_id'), sqlc.narg('sincronizada_en'), sqlc.narg('resumen'), sqlc.narg('error'), sqlc.narg('error_en'))
ON CONFLICT (waba_id) DO UPDATE
SET empresa_id = EXCLUDED.empresa_id,
    sincronizada_en = coalesce(EXCLUDED.sincronizada_en, mensajeria.plantillas_whatsapp_cuentas.sincronizada_en),
    resumen = coalesce(EXCLUDED.resumen, mensajeria.plantillas_whatsapp_cuentas.resumen),
    error = EXCLUDED.error, error_en = EXCLUDED.error_en, updated_at = now();

-- name: CuentasWAParaNoche :many
-- Cuentas con un número activo que no se sincronizan desde @antes.
SELECT DISTINCT c.waba_id FROM mensajeria.canales c
LEFT JOIN mensajeria.plantillas_whatsapp_cuentas k ON k.waba_id = c.waba_id
WHERE c.waba_id <> '' AND c.activo AND c.desconectado_en IS NULL
  AND (k.sincronizada_en IS NULL OR k.sincronizada_en < @antes)
  AND (k.error_en IS NULL OR k.error_en < @antes)
ORDER BY c.waba_id
LIMIT 200;

-- name: AjustesPlantillasWA :one
SELECT * FROM mensajeria.plantillas_whatsapp_ajustes WHERE empresa_id = @empresa_id;

-- Datos de Argos para las variables (instantánea; nunca se pide a Argos en el envío).

-- name: DatosClienteParaPlantilla :one
SELECT c.nombre, c.codigo, coalesce((SELECT s.plan_nombre FROM integracion.argos_servicios s
        WHERE s.id = sqlc.narg('servicio_id')::bigint AND s.cliente_id = c.id), '')::text AS plan
FROM integracion.argos_clientes c
WHERE c.id = @cliente_id AND c.empresa_id = @empresa_id;

-- name: NombreEmpresaParaPlantilla :one
SELECT coalesce((SELECT nombre FROM nucleo.empresas WHERE id = @empresa_id::bigint), '')::text;

-- name: PrestatariasDeCuentaWA :many
-- Las empresas a las que se prestan números de la cuenta (usan sus plantillas, §15.3).
SELECT DISTINCT e.id, e.nombre, p.usos
FROM mensajeria.canal_prestamos p
JOIN mensajeria.canales c ON c.id = p.canal_id
JOIN nucleo.empresas e ON e.id = p.empresa_id
WHERE c.waba_id = @waba_id AND p.activo
ORDER BY e.nombre;

-- name: GuardarAjustesPlantillasWA :one
INSERT INTO mensajeria.plantillas_whatsapp_ajustes (empresa_id, marketing_permitido)
VALUES (@empresa_id, @marketing_permitido)
ON CONFLICT (empresa_id) DO UPDATE
SET marketing_permitido = EXCLUDED.marketing_permitido, version = mensajeria.plantillas_whatsapp_ajustes.version + 1, updated_at = now()
RETURNING *;

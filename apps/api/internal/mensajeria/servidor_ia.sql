-- Servidor de IA («Iris Voz», contratos/voz.md §4). Uno por instalación.

-- name: MotorIA :one
SELECT * FROM mensajeria.voz_motores ORDER BY id LIMIT 1;

-- name: MotorIAPorID :one
SELECT * FROM mensajeria.voz_motores WHERE id = $1;

-- name: MotorIAParaActualizar :one
SELECT * FROM mensajeria.voz_motores WHERE id = $1 FOR UPDATE;

-- name: MotorIAUnicoParaActualizar :one
SELECT * FROM mensajeria.voz_motores ORDER BY id LIMIT 1 FOR UPDATE;

-- name: InsertarMotorIA :one
INSERT INTO mensajeria.voz_motores (nombre, url) VALUES ($1, $2) RETURNING *;

-- name: PrepararEnlaceMotorIA :one
-- Un enlace nuevo invalida todo lo anterior: token, huellas y certificado de cliente.
UPDATE mensajeria.voz_motores SET nombre = @nombre, url = @url, estado = 'sin_enlazar', detalle = NULL,
    enlace_codigo = @enlace_codigo, enlace_vence_en = @enlace_vence_en, enlace_usado_en = NULL,
    token = NULL, token_siguiente = NULL, token_rotado_en = NULL, rotacion_desde = NULL, siguiente_rechazos = 0, rotacion_fallida_en = NULL,
    cert_cliente_siguiente = NULL, cert_cliente_clave_siguiente = NULL, cert_cliente_vence_siguiente = NULL,
    cert_cliente = @cert_cliente, cert_cliente_clave = @cert_cliente_clave, cert_cliente_vence_en = @cert_cliente_vence_en,
    huella_tls = NULL, huella_tls_siguiente = NULL, ip_observada = NULL, restringe_ip = false,
    enlazado_en = NULL, activado_en = NULL, compatible = false, motivo_incompatible = NULL, catalogo = '{}', max_simultaneas = 0,
    version_motor = NULL, version_protocolo = NULL, iris_min = NULL, iris_max = NULL,
    fallos_seguidos = 0, ultimo_latido_en = NULL, ultima_prueba = NULL, version = version + 1, updated_at = now()
WHERE id = @id RETURNING *;

-- name: BorrarCodigosEnlaceVencidos :execrows
-- El código de enlace no se queda guardado: se borra al vencer.
UPDATE mensajeria.voz_motores SET enlace_codigo = NULL, enlace_vence_en = NULL, updated_at = now()
WHERE enlace_codigo IS NOT NULL AND enlace_vence_en <= @ahora;

-- name: GuardarEnlaceMotorIA :one
-- El intercambio con código se completó: se borra el código y queda pendiente de activar.
UPDATE mensajeria.voz_motores SET enlace_codigo = NULL, enlace_vence_en = NULL, enlace_usado_en = now(), enlazado_en = now(),
    token = @token, huella_tls = @huella_tls, version_motor = @version_motor, version_protocolo = @version_protocolo,
    iris_min = @iris_min, iris_max = @iris_max, compatible = @compatible, motivo_incompatible = @motivo_incompatible,
    version = version + 1, updated_at = now()
-- Solo si el código sigue ahí y nadie reenlazó entretanto (si no, se mezclaría un token viejo con
-- un certificado nuevo): sin filas = el enlace ya no vale.
WHERE id = @id AND enlace_codigo IS NOT NULL AND version = @version RETURNING *;

-- name: FijarSiguienteMotorIA :one
-- Guarda lo que se va a entregar al servidor (token y, si se renueva, certificado de cliente) ANTES
-- de entregarlo. Compara la versión y aparta la rotación (rotacion_desde): dos a la vez no se pisan
-- ni se cruzan en el servidor (sin filas = otra ganó o sigue en vuelo). Un apartado viejo (una
-- rotación que murió a medias) no bloquea: @vencida es «ahora menos el plazo de una rotación».
UPDATE mensajeria.voz_motores SET token_siguiente = @token_siguiente, rotacion_desde = @ahora::timestamptz, siguiente_rechazos = 0,
    cert_cliente_siguiente = sqlc.narg(cert_cliente_siguiente), cert_cliente_clave_siguiente = sqlc.narg(cert_cliente_clave_siguiente),
    cert_cliente_vence_siguiente = sqlc.narg(cert_cliente_vence_siguiente), version = version + 1, updated_at = now()
WHERE id = @id AND version = @version AND (rotacion_desde IS NULL OR rotacion_desde <= @vencida::timestamptz) RETURNING version;

-- name: PromoverTokenMotorIA :one
-- El servidor ya tiene lo «siguiente»: pasa a vigente (token y, si lo había, certificado de cliente).
UPDATE mensajeria.voz_motores SET token = token_siguiente, token_siguiente = NULL, token_rotado_en = now(),
    cert_cliente = coalesce(cert_cliente_siguiente, cert_cliente),
    cert_cliente_clave = coalesce(cert_cliente_clave_siguiente, cert_cliente_clave),
    cert_cliente_vence_en = coalesce(cert_cliente_vence_siguiente, cert_cliente_vence_en),
    cert_cliente_siguiente = NULL, cert_cliente_clave_siguiente = NULL, cert_cliente_vence_siguiente = NULL, rotacion_desde = NULL,
    siguiente_rechazos = 0, rotacion_fallida_en = NULL, version = version + 1, updated_at = now()
WHERE id = @id AND token_siguiente IS NOT NULL RETURNING *;

-- name: DescartarSiguienteMotorIA :execrows
-- Lo «siguiente» de una entrega que nunca llegó al servidor (lo vigente contesta, el servidor rechaza
-- lo siguiente de forma explícita varias veces seguidas, y el apartado ya venció): no se queda
-- guardado hasta la próxima rotación.
UPDATE mensajeria.voz_motores SET token_siguiente = NULL, cert_cliente_siguiente = NULL, cert_cliente_clave_siguiente = NULL,
    cert_cliente_vence_siguiente = NULL, rotacion_desde = NULL, siguiente_rechazos = 0, version = version + 1, updated_at = now()
WHERE id = @id AND token_siguiente IS NOT NULL AND (rotacion_desde IS NULL OR rotacion_desde <= @vencida::timestamptz);

-- name: AnotarRechazoSiguienteMotorIA :one
-- Un sondeo de lo «siguiente»: si el servidor lo rechazó de forma explícita suma uno; cualquier otra
-- cosa (tiempo agotado, corte de red) vuelve la cuenta a cero. Devuelve cuántos seguidos lleva.
UPDATE mensajeria.voz_motores SET siguiente_rechazos = CASE WHEN @explicito::boolean THEN siguiente_rechazos + 1 ELSE 0 END
WHERE id = @id AND token_siguiente IS NOT NULL RETURNING siguiente_rechazos;

-- name: MarcarRotacionFallidaMotorIA :exec
UPDATE mensajeria.voz_motores SET rotacion_fallida_en = @ahora::timestamptz WHERE id = @id;

-- name: ActivarMotorIA :one
UPDATE mensajeria.voz_motores SET activado_en = now(), estado = @estado, detalle = NULL, fallos_seguidos = 0,
    version = version + 1, updated_at = now()
WHERE id = @id RETURNING *;

-- name: DesactivarMotorIA :one
UPDATE mensajeria.voz_motores SET estado = 'desactivado', detalle = sqlc.narg(detalle), version = version + 1, updated_at = now()
WHERE id = @id RETURNING *;

-- name: GuardarLatidoMotorIA :one
UPDATE mensajeria.voz_motores SET estado = @estado, detalle = sqlc.narg(detalle), fallos_seguidos = @fallos_seguidos,
    ultimo_latido_en = coalesce(sqlc.narg(ultimo_latido_en), ultimo_latido_en),
    version_motor = coalesce(sqlc.narg(version_motor), version_motor),
    version_protocolo = coalesce(sqlc.narg(version_protocolo), version_protocolo),
    iris_min = coalesce(sqlc.narg(iris_min), iris_min), iris_max = coalesce(sqlc.narg(iris_max), iris_max),
    compatible = @compatible, motivo_incompatible = sqlc.narg(motivo_incompatible),
    catalogo = coalesce(sqlc.narg(catalogo), catalogo), max_simultaneas = @max_simultaneas,
    ip_observada = coalesce(sqlc.narg(ip_observada), ip_observada), restringe_ip = @restringe_ip,
    huella_tls_siguiente = sqlc.narg(huella_tls_siguiente), updated_at = now()
WHERE id = @id RETURNING *;

-- name: PromoverHuellaMotorIA :exec
-- El servidor ya usa el certificado que anunció por el canal vigente.
UPDATE mensajeria.voz_motores SET huella_tls = huella_tls_siguiente, huella_tls_siguiente = NULL, version = version + 1, updated_at = now()
WHERE id = @id AND huella_tls_siguiente = @huella;

-- name: GuardarPruebaMotorIA :one
UPDATE mensajeria.voz_motores SET ultima_prueba = @ultima_prueba, updated_at = now() WHERE id = @id RETURNING *;

-- name: ActualizarEmpresasMotorIA :one
UPDATE mensajeria.voz_motores SET empresas_permitidas = @empresas_permitidas, simultaneas_por_empresa = @simultaneas_por_empresa,
    version = version + 1, updated_at = now()
WHERE id = @id RETURNING *;

-- name: ReservarPlazaIA :one
-- Toma una plaza en la misma sentencia en que se decide llamar. Quien la llama ya tiene el candado
-- de la fila del servidor (MotorIAUnicoParaActualizar, en la misma transacción), que pone en fila a
-- las reservas. Hay plaza si: queda sitio en el servidor, la empresa no pasó su
-- máximo y, si la empresa ya tiene alguna en curso, después de tomarla quedan libres al menos tantas
-- como empresas activas sin ninguna en curso (una plaza garantizada por empresa activa).
WITH m AS (
    SELECT vm.id, vm.max_simultaneas, vm.simultaneas_por_empresa, vm.empresas_permitidas FROM mensajeria.voz_motores vm WHERE vm.id = @motor_id FOR UPDATE
), vivas AS (
    SELECT p.empresa_id FROM mensajeria.servidor_ia_plazas p JOIN m ON m.id = p.motor_id WHERE p.vence_en > @ahora
), cuenta AS (
    SELECT (SELECT count(*) FROM vivas) AS total, (SELECT count(*) FROM vivas WHERE empresa_id = @empresa_id) AS propias
), esperan AS (
    SELECT count(*) AS n FROM mensajeria.asistente_ajustes a, m
    WHERE a.proveedor = 'servidor_ia' AND a.activo AND coalesce(a.empresa_id, 0) <> @empresa_id
      AND (m.empresas_permitidas IS NULL OR coalesce(a.empresa_id, 0) = ANY (m.empresas_permitidas))
      AND NOT EXISTS (SELECT 1 FROM vivas v WHERE v.empresa_id = coalesce(a.empresa_id, 0))
)
INSERT INTO mensajeria.servidor_ia_plazas (id, motor_id, empresa_id, vence_en)
SELECT @plaza_id, m.id, @empresa_id, @vence_en FROM m, cuenta, esperan
WHERE cuenta.total < m.max_simultaneas AND cuenta.propias < m.simultaneas_por_empresa
  AND (cuenta.propias = 0 OR m.max_simultaneas - cuenta.total - 1 >= esperan.n)
RETURNING mensajeria.servidor_ia_plazas.id;

-- name: SoltarPlazaIA :exec
DELETE FROM mensajeria.servidor_ia_plazas WHERE id = $1;

-- name: BorrarPlazasVencidasIA :execrows
DELETE FROM mensajeria.servidor_ia_plazas WHERE vence_en <= @ahora;

-- name: PlazasEnCursoIA :one
SELECT count(*) FROM mensajeria.servidor_ia_plazas WHERE motor_id = @motor_id AND vence_en > @ahora;

-- name: AsegurarUsoIA :exec
INSERT INTO mensajeria.servidor_ia_uso (motor_id, hora, empresa_id, modelo, latencia_cubetas)
VALUES (@motor_id, @hora, @empresa_id, @modelo, array_fill(0::bigint, ARRAY[12]))
ON CONFLICT DO NOTHING;

-- name: SumarUsoIA :exec
UPDATE mensajeria.servidor_ia_uso SET peticiones = peticiones + @peticiones, tokens_entrada = tokens_entrada + @tokens_entrada,
    tokens_salida = tokens_salida + @tokens_salida, latencia_ms_suma = latencia_ms_suma + @latencia_ms,
    latencia_cubetas = (SELECT array_agg(c.v + CASE WHEN c.i = @cubeta::int THEN 1 ELSE 0 END ORDER BY c.i)
                        FROM unnest(latencia_cubetas) WITH ORDINALITY AS c (v, i)),
    rechazos = rechazos + @rechazos,
    errores = CASE WHEN @motivo::text = '' THEN errores
                   ELSE jsonb_set(errores, ARRAY[@motivo::text], to_jsonb(coalesce((errores ->> @motivo::text)::bigint, 0) + 1)) END
WHERE motor_id = @motor_id AND hora = @hora AND empresa_id = @empresa_id AND modelo = @modelo;

-- name: UsoIADeEmpresa :many
SELECT * FROM mensajeria.servidor_ia_uso WHERE motor_id = @motor_id AND empresa_id = @empresa_id AND hora >= @desde ORDER BY hora, modelo;

-- name: EmpresasConServidorIA :many
-- Las empresas cuyo asistente usa el servidor de IA (para avisar y para la plaza garantizada).
SELECT coalesce(empresa_id, 0)::bigint AS empresa_id, activo FROM mensajeria.asistente_ajustes WHERE proveedor = 'servidor_ia' ORDER BY 1;

-- name: ActualizarModelosMotorIA :one
UPDATE mensajeria.voz_motores SET modelos_permitidos = @modelos_permitidos, version = version + 1, updated_at = now()
WHERE id = @id RETURNING *;

-- name: ActualizarUmbralesMotorIA :one
UPDATE mensajeria.voz_motores SET vram_pct = @vram_pct, vram_minutos = @vram_minutos, disco_pct = @disco_pct,
    temperatura_c = @temperatura_c, temperatura_minutos = @temperatura_minutos, saturacion_rechazos = @saturacion_rechazos,
    saturacion_minutos = @saturacion_minutos, retencion_detalle_dias = @retencion_detalle_dias,
    retencion_hora_dias = @retencion_hora_dias, umbrales_version = umbrales_version + 1, updated_at = now()
WHERE id = @id RETURNING *;

-- name: UsoIAEnRango :many
-- Uso por empresa en el rango (todas, o solo una con @empresa_id > 0).
SELECT u.empresa_id, coalesce(e.nombre, '')::text AS nombre, sum(u.peticiones)::bigint AS peticiones,
    sum(u.tokens_entrada)::bigint AS tokens_entrada, sum(u.tokens_salida)::bigint AS tokens_salida,
    sum(u.latencia_ms_suma)::bigint AS latencia_ms_suma, sum(u.rechazos)::bigint AS rechazos,
    coalesce(sum((SELECT sum(v.valor::bigint) FROM jsonb_each_text(u.errores) AS v (clave, valor))), 0)::bigint AS errores
FROM mensajeria.servidor_ia_uso u LEFT JOIN nucleo.empresas e ON e.id = u.empresa_id
WHERE u.motor_id = @motor_id AND u.hora >= @desde AND (@empresa_id::bigint < 0 OR u.empresa_id = @empresa_id)
GROUP BY u.empresa_id, e.nombre ORDER BY peticiones DESC, u.empresa_id;

-- name: ErroresIAEnRango :many
SELECT v.clave::text AS motivo, sum(v.valor::bigint)::bigint AS cuenta, max(u.hora)::timestamptz AS ultimo_en
FROM mensajeria.servidor_ia_uso u, jsonb_each_text(u.errores) AS v (clave, valor)
WHERE u.motor_id = @motor_id AND u.hora >= @desde AND (@empresa_id::bigint < 0 OR u.empresa_id = @empresa_id)
GROUP BY v.clave ORDER BY cuenta DESC;

-- name: EmpresasActivasIA :many
-- Empresas cuyo asistente está encendido con el servidor de IA (plaza garantizada, tablero).
SELECT coalesce(a.empresa_id, 0)::bigint AS empresa_id, coalesce(e.nombre, '')::text AS nombre
FROM mensajeria.asistente_ajustes a LEFT JOIN nucleo.empresas e ON e.id = a.empresa_id
WHERE a.proveedor = 'servidor_ia' AND a.activo ORDER BY 1;

-- Tablero: muestras, agregados y episodios de aviso (contratos/voz.md §4.12) ----------------------

-- name: InsertarMuestraIA :exec
INSERT INTO mensajeria.servidor_ia_muestras (motor_id, tomada_en, ok, uso_pct, vram_pct, temperatura_c, disco_pct, en_curso, datos, estado)
VALUES (@motor_id, @tomada_en, @ok, sqlc.narg(uso_pct), sqlc.narg(vram_pct), sqlc.narg(temperatura_c), sqlc.narg(disco_pct), sqlc.narg(en_curso), sqlc.narg(datos),
    sqlc.narg(estado))
ON CONFLICT DO NOTHING;

-- name: QuitarDatosAnterioresIA :exec
-- La muestra completa (datos) solo se conserva en la última lectura buena: las anteriores se quedan
-- con sus escalares (un servidor hostil no llena la base con JSON cada 10 s).
UPDATE mensajeria.servidor_ia_muestras SET datos = NULL WHERE motor_id = @motor_id AND datos IS NOT NULL AND tomada_en < @tomada_en;

-- name: UltimosEstadosIA :one
-- De las últimas @n muestras, cuántas hay y cuántas quedaron en ese estado.
SELECT count(*)::bigint AS total, (count(*) FILTER (WHERE u.estado = @estado::text))::bigint AS en_estado
FROM (SELECT mu.estado FROM mensajeria.servidor_ia_muestras mu WHERE mu.motor_id = @motor_id ORDER BY mu.tomada_en DESC LIMIT @n) u;

-- name: UltimaMuestraIA :one
-- La última, buena o fallida.
SELECT * FROM mensajeria.servidor_ia_muestras WHERE motor_id = @motor_id ORDER BY tomada_en DESC LIMIT 1;

-- name: UltimaMuestraBuenaIA :one
SELECT * FROM mensajeria.servidor_ia_muestras WHERE motor_id = @motor_id AND ok AND datos IS NOT NULL ORDER BY tomada_en DESC LIMIT 1;

-- name: SerieMuestrasIA :many
-- Media por paso (segundos) de las muestras buenas desde @desde.
SELECT (floor(extract(epoch FROM tomada_en) / @paso::int) * @paso::int)::bigint AS t,
    coalesce(avg(uso_pct), 0)::float8 AS uso_pct, coalesce(avg(vram_pct), 0)::float8 AS vram_pct, count(uso_pct)::bigint AS con_uso, count(vram_pct)::bigint AS con_vram
FROM mensajeria.servidor_ia_muestras
WHERE motor_id = @motor_id AND tomada_en >= @desde AND ok
GROUP BY 1 ORDER BY 1;

-- name: SerieMuestrasHoraIA :many
SELECT hora, uso_pct, vram_pct FROM mensajeria.servidor_ia_muestras_hora WHERE motor_id = @motor_id AND hora >= @desde ORDER BY hora;

-- name: VentanaMuestrasIA :one
-- Lo mínimo de la ventana (un umbral «durante N minutos» exige que toda la ventana lo supere).
SELECT count(*)::bigint AS total, count(vram_pct)::bigint AS con_vram, coalesce(min(vram_pct), 0)::float8 AS vram_min,
    coalesce(max(vram_pct), 0)::float8 AS vram_max,
    count(temperatura_c)::bigint AS con_temperatura, coalesce(min(temperatura_c), 0)::float8 AS temperatura_min,
    coalesce(max(temperatura_c), 0)::float8 AS temperatura_max,
    count(disco_pct)::bigint AS con_disco, coalesce(min(disco_pct), 0)::float8 AS disco_min, coalesce(max(disco_pct), 0)::float8 AS disco_max
FROM mensajeria.servidor_ia_muestras WHERE motor_id = @motor_id AND tomada_en > @desde AND ok;

-- name: AgregarMuestrasHoraIA :exec
INSERT INTO mensajeria.servidor_ia_muestras_hora (motor_id, hora, muestras, fallidas, uso_pct, vram_pct, temperatura_c)
SELECT motor_id, date_trunc('hour', tomada_en), count(*)::integer, (count(*) FILTER (WHERE NOT ok))::integer,
    avg(uso_pct)::real, avg(vram_pct)::real, max(temperatura_c)::real
FROM mensajeria.servidor_ia_muestras
WHERE tomada_en >= @desde AND tomada_en < @hasta
GROUP BY 1, 2
ON CONFLICT (motor_id, hora) DO UPDATE SET muestras = excluded.muestras, fallidas = excluded.fallidas, uso_pct = excluded.uso_pct,
    vram_pct = excluded.vram_pct, temperatura_c = excluded.temperatura_c;

-- name: BorrarMuestrasIA :execrows
DELETE FROM mensajeria.servidor_ia_muestras m USING mensajeria.voz_motores v
WHERE v.id = m.motor_id AND m.tomada_en < sqlc.arg(ahora)::timestamptz - make_interval(0, 0, 0, v.retencion_detalle_dias);

-- name: BorrarMuestrasHoraIA :execrows
DELETE FROM mensajeria.servidor_ia_muestras_hora m USING mensajeria.voz_motores v
WHERE v.id = m.motor_id AND m.hora < sqlc.arg(ahora)::timestamptz - make_interval(0, 0, 0, v.retencion_hora_dias);

-- name: BorrarUsoIA :execrows
DELETE FROM mensajeria.servidor_ia_uso u USING mensajeria.voz_motores v
WHERE v.id = u.motor_id AND u.hora < sqlc.arg(ahora)::timestamptz - make_interval(0, 0, 0, v.retencion_hora_dias);

-- name: BorrarAvisosIA :execrows
DELETE FROM mensajeria.servidor_ia_avisos a USING mensajeria.voz_motores v
WHERE v.id = a.motor_id AND a.hasta IS NOT NULL AND a.hasta < sqlc.arg(ahora)::timestamptz - make_interval(0, 0, 0, v.retencion_hora_dias);

-- name: UsoIAFilas :many
-- Las filas de uso (por minuto, empresa y modelo) del rango, para armar las series.
SELECT hora, peticiones, latencia_ms_suma, latencia_cubetas FROM mensajeria.servidor_ia_uso
WHERE motor_id = @motor_id AND hora >= @desde AND (@empresa_id::bigint < 0 OR empresa_id = @empresa_id)
ORDER BY hora;

-- name: RechazosIADesde :one
SELECT coalesce(sum(rechazos), 0)::bigint FROM mensajeria.servidor_ia_uso WHERE motor_id = @motor_id AND hora >= @desde;

-- name: AbrirAvisoIA :one
-- Abre el episodio si no hay uno abierto de ese tipo (sin filas = ya estaba abierto).
INSERT INTO mensajeria.servidor_ia_avisos (motor_id, tipo, desde, valor, umbral)
VALUES (@motor_id, @tipo, @desde, sqlc.narg(valor), sqlc.narg(umbral))
ON CONFLICT (motor_id, tipo) WHERE hasta IS NULL DO NOTHING
RETURNING id;

-- name: CerrarAvisoIA :one
UPDATE mensajeria.servidor_ia_avisos SET hasta = @hasta WHERE motor_id = @motor_id AND tipo = @tipo AND hasta IS NULL RETURNING id;

-- name: AvisosIAEnRango :many
SELECT * FROM mensajeria.servidor_ia_avisos WHERE motor_id = @motor_id AND (hasta IS NULL OR hasta >= @desde) ORDER BY desde DESC LIMIT 200;

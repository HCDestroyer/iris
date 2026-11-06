-- Horarios, fuera de horario y manuales (contratos/conversaciones.md §10–§11). sqlc: mensajeriabd.

-- Horarios ------------------------------------------------------------------------------------

-- name: ListarHorarios :many
SELECT * FROM mensajeria.horarios WHERE coalesce(empresa_id, 0) = @empresa::bigint ORDER BY lower(nombre), id;

-- name: HorarioPorID :one
SELECT * FROM mensajeria.horarios WHERE id = @id AND coalesce(empresa_id, 0) = @empresa::bigint;

-- name: HorarioParaActualizar :one
SELECT * FROM mensajeria.horarios WHERE id = @id AND coalesce(empresa_id, 0) = @empresa::bigint FOR UPDATE;

-- name: InsertarHorario :one
INSERT INTO mensajeria.horarios (empresa_id, nombre, zona, franjas, feriados)
VALUES (sqlc.narg('empresa_id'), @nombre, @zona, @franjas, @feriados)
RETURNING *;

-- name: ActualizarHorario :one
UPDATE mensajeria.horarios
SET nombre = @nombre, zona = @zona, franjas = @franjas, feriados = @feriados, activo = @activo, version = version + 1, updated_at = now()
WHERE id = @id
RETURNING *;

-- name: HorarioPorIDSinEmpresa :one
SELECT * FROM mensajeria.horarios WHERE id = @id;

-- name: FijarHorarioCola :one
UPDATE mensajeria.colas SET horario_id = sqlc.narg('horario_id'), version = version + 1, updated_at = now() WHERE id = @id RETURNING *;

-- name: ConfigurarHorarioCanal :one
UPDATE mensajeria.canales
SET horario_id = sqlc.narg('horario_id'), fuera_horario = @fuera_horario, version = version + 1, updated_at = now()
WHERE id = @id
RETURNING *;

-- name: FijarFueraHorario :exec
UPDATE mensajeria.conversaciones SET fuera_horario_hasta = @hasta, updated_at = now() WHERE id = @id;

-- name: FijarGuardia :exec
UPDATE mensajeria.conversaciones SET guardia_hasta = @hasta, updated_at = now() WHERE id = @id;

-- name: EtiquetasDeConversacion :many
SELECT etiqueta_id FROM mensajeria.conversacion_etiquetas WHERE conversacion_id = @conversacion_id;

-- name: RouterCaidoDelCliente :one
-- Falla masiva: un router de un servicio del cliente en la empresa está caído según Horus (lo
-- guarda soporte en horus_estados; solo lectura).
SELECT EXISTS (
    SELECT 1 FROM integracion.argos_servicios s
    JOIN soporte.horus_estados h ON h.clave IN ('router:' || s.empresa_id || ':' || s.router_id, 'router:0:' || s.router_id)
    WHERE s.cliente_id = @cliente_id AND s.router_id IS NOT NULL AND h.estado = 'caido'
      AND (sqlc.narg('servicio_id')::bigint IS NULL OR s.id = sqlc.narg('servicio_id')::bigint)
)::boolean;

-- Manuales ------------------------------------------------------------------------------------

-- name: InsertarManual :one
INSERT INTO mensajeria.manuales (empresa_id, titulo, resumen, cuerpo, etiquetas, equipo, visibilidad, creado_por, busqueda)
VALUES (sqlc.narg('empresa_id'), @titulo, @resumen, @cuerpo, @etiquetas::text[], sqlc.narg('equipo'), @visibilidad, sqlc.narg('creado_por'),
    setweight(to_tsvector('spanish', @titulo::text), 'A')
    || setweight(to_tsvector('spanish', @resumen::text || ' ' || array_to_string(@etiquetas::text[], ' ') || ' ' || coalesce(sqlc.narg('equipo')::text, '')), 'B')
    || setweight(to_tsvector('spanish', @cuerpo::text), 'C'))
RETURNING *;

-- name: ActualizarManual :one
UPDATE mensajeria.manuales
SET titulo = @titulo, resumen = @resumen, cuerpo = @cuerpo, etiquetas = @etiquetas::text[], equipo = sqlc.narg('equipo'),
    visibilidad = @visibilidad, version = version + 1, updated_at = now(),
    busqueda = setweight(to_tsvector('spanish', @titulo::text), 'A')
        || setweight(to_tsvector('spanish', @resumen::text || ' ' || array_to_string(@etiquetas::text[], ' ') || ' ' || coalesce(sqlc.narg('equipo')::text, '')), 'B')
        || setweight(to_tsvector('spanish', @cuerpo::text), 'C')
WHERE id = @id
RETURNING *;

-- name: FijarManualActivo :one
UPDATE mensajeria.manuales SET activo = @activo, version = version + 1, updated_at = now() WHERE id = @id RETURNING *;

-- name: ManualPorID :one
SELECT * FROM mensajeria.manuales WHERE id = @id AND coalesce(empresa_id, 0) = @empresa::bigint;

-- name: ManualParaActualizar :one
SELECT * FROM mensajeria.manuales WHERE id = @id AND coalesce(empresa_id, 0) = @empresa::bigint FOR UPDATE;

-- name: BuscarManuales :many
-- Texto completo en español (websearch), por relevancia; sin q, los más recientes.
SELECT m.*, (CASE WHEN @q::text = '' THEN 0 ELSE ts_rank(m.busqueda, websearch_to_tsquery('spanish', @q::text)) END)::real AS rango
FROM mensajeria.manuales m
WHERE coalesce(m.empresa_id, 0) = @empresa::bigint
  AND (@incluir_inactivos::boolean OR m.activo)
  AND (@q::text = '' OR m.busqueda @@ websearch_to_tsquery('spanish', @q::text))
  AND (@etiqueta::text = '' OR @etiqueta::text = ANY (m.etiquetas))
  AND (@visibilidad::text = '' OR m.visibilidad = @visibilidad::text)
ORDER BY rango DESC, m.id DESC
LIMIT @limite;

-- name: ManualesPorEtiquetas :many
SELECT * FROM mensajeria.manuales
WHERE coalesce(empresa_id, 0) = @empresa::bigint AND activo AND etiquetas && @etiquetas::text[]
ORDER BY id DESC LIMIT 5;

-- name: ManualesPorEquipos :many
SELECT * FROM mensajeria.manuales
WHERE coalesce(empresa_id, 0) = @empresa::bigint AND activo AND equipo IS NOT NULL AND lower(equipo) = ANY (@equipos::text[])
ORDER BY id DESC LIMIT 5;

-- name: ManualesPorConsulta :many
SELECT * FROM mensajeria.manuales
WHERE coalesce(empresa_id, 0) = @empresa::bigint AND activo AND busqueda @@ to_tsquery('spanish', @consulta::text)
ORDER BY ts_rank(busqueda, to_tsquery('spanish', @consulta::text)) DESC, id DESC LIMIT 5;

-- name: TicketsAbiertosDelCliente :many
-- Asuntos de los tickets abiertos del cliente (soporte; solo lectura).
SELECT id, numero, asunto FROM soporte.tickets
WHERE cliente_id = @cliente_id AND (sqlc.narg('empresa')::bigint IS NULL OR empresa_id = sqlc.narg('empresa')::bigint)
  AND estado IN ('nuevo', 'abierto', 'en_espera')
ORDER BY id DESC LIMIT 5;

-- name: InsertarEnlaceManual :exec
INSERT INTO mensajeria.manual_enlaces (hash_token, manual_id, empresa_id, conversacion_id, vence_en)
VALUES (@hash_token, @manual_id, sqlc.narg('empresa_id'), sqlc.narg('conversacion_id'), @vence_en);

-- name: ManualPorEnlace :one
SELECT m.*, e.vence_en FROM mensajeria.manual_enlaces e JOIN mensajeria.manuales m ON m.id = e.manual_id
WHERE e.hash_token = @hash_token;

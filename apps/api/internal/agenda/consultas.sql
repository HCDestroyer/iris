-- Consultas de agenda (sqlc → internal/agenda/bd).

-- Usuarios, empresas y alcances (lectura de nucleo) ----------------------------------

-- name: UsuarioParaTecnico :one
SELECT id, nombre, correo, tipo, activo FROM nucleo.usuarios WHERE id = @id;

-- name: UsuariosPorIDs :many
SELECT id, nombre FROM nucleo.usuarios WHERE id = ANY(@ids::bigint[]);

-- name: AlcancesDePermisoDeUsuario :many
-- Alcances con que el usuario tiene el permiso por sus roles (zona y cuadrilla null = global).
SELECT ur.zona_id, ur.cuadrilla_id
FROM nucleo.usuarios_roles ur
JOIN nucleo.roles_permisos rp ON rp.rol_id = ur.rol_id
WHERE ur.usuario_id = @usuario_id AND rp.permiso = ANY(@permisos::text[]);

-- Zonas ---------------------------------------------------------------------------

-- name: ZonaPorID :one
SELECT * FROM agenda.zonas WHERE id = @id;

-- name: ZonaPorIDParaActualizar :one
SELECT * FROM agenda.zonas WHERE id = @id FOR UPDATE;

-- name: ZonasPorIDs :many
SELECT * FROM agenda.zonas WHERE id = ANY(@ids::bigint[]) ORDER BY id;

-- name: ListarZonas :many
-- Las de la empresa activa (o las sin empresa: catálogo de todas). todas_empresas: actor sistema.
SELECT * FROM agenda.zonas
WHERE (@todas_empresas::boolean OR empresa_id IS NULL OR empresa_id = sqlc.narg(empresa_id))
  AND (@incluir_inactivas::boolean OR activa)
ORDER BY nombre, id;

-- name: CrearZonaLocal :one
INSERT INTO agenda.zonas (id, empresa_id, origen, nombre, poligono)
VALUES (nextval('agenda.zonas_locales_seq'), @empresa_id, 'local', @nombre, @poligono)
RETURNING *;

-- name: GuardarZona :one
UPDATE agenda.zonas SET nombre = @nombre, poligono = @poligono, activa = @activa,
    version = version + 1, updated_at = now()
WHERE id = @id AND version = @version
RETURNING *;

-- name: ZonaArgos :one
SELECT * FROM integracion.argos_zonas WHERE id = @id;

-- name: AsegurarZonaArgos :exec
-- Espejo de una zona de Argos que llegó por un dato (instantánea de un servicio o cliente)
-- antes que el refresco del catálogo. Si ya existe, no la toca.
INSERT INTO agenda.zonas (id, empresa_id, origen, nombre, sincronizado_en)
VALUES (@id, @empresa_id, 'argos', @nombre, sqlc.narg(sincronizado_en))
ON CONFLICT (id) DO NOTHING;

-- name: EspejarZonasArgos :execrows
-- Copia el catálogo de Argos (integracion.argos_zonas) a agenda.zonas: nuevas, renombradas o de
-- otra empresa. No toca el polígono ni «activa» (lo administra Iris).
INSERT INTO agenda.zonas (id, empresa_id, origen, nombre, sincronizado_en)
SELECT a.id, a.empresa_id, 'argos', coalesce(nullif(left(btrim(a.nombre), 120), ''), 'Zona ' || a.id), a.sincronizado_en
FROM integracion.argos_zonas a
ON CONFLICT (id) DO UPDATE SET
    empresa_id = EXCLUDED.empresa_id, origen = 'argos', nombre = EXCLUDED.nombre,
    sincronizado_en = EXCLUDED.sincronizado_en, version = agenda.zonas.version + 1, updated_at = now()
WHERE (agenda.zonas.empresa_id, agenda.zonas.origen, agenda.zonas.nombre) IS DISTINCT FROM
      (EXCLUDED.empresa_id, 'argos', EXCLUDED.nombre);

-- name: DesactivarZonasSinArgos :execrows
-- Zonas de Argos que ya no están en su catálogo (solo si Iris tiene el catálogo de esa empresa).
UPDATE agenda.zonas z SET activa = false, version = version + 1, updated_at = now()
WHERE z.origen = 'argos' AND z.activa
  AND NOT EXISTS (SELECT 1 FROM integracion.argos_zonas a WHERE a.id = z.id)
  AND EXISTS (SELECT 1 FROM integracion.argos_zonas a WHERE a.empresa_id = z.empresa_id);

-- Cuadrillas ----------------------------------------------------------------------

-- name: CuadrillaPorID :one
SELECT * FROM agenda.cuadrillas WHERE id = @id;

-- name: CuadrillaPorIDParaActualizar :one
SELECT * FROM agenda.cuadrillas WHERE id = @id FOR UPDATE;

-- name: ListarCuadrillas :many
SELECT * FROM agenda.cuadrillas
WHERE (@todas_empresas::boolean OR empresa_id IS NULL OR empresa_id = sqlc.narg(empresa_id))
  AND (@incluir_inactivas::boolean OR activa)
ORDER BY nombre, id;

-- name: CrearCuadrilla :one
INSERT INTO agenda.cuadrillas (empresa_id, nombre) VALUES (@empresa_id, @nombre) RETURNING *;

-- name: GuardarCuadrilla :one
UPDATE agenda.cuadrillas SET nombre = @nombre, activa = @activa, version = version + 1, updated_at = now()
WHERE id = @id AND version = @version
RETURNING *;

-- name: ZonasDeCuadrillas :many
SELECT * FROM agenda.cuadrilla_zonas WHERE cuadrilla_id = ANY(@cuadrillas::bigint[]) ORDER BY cuadrilla_id, zona_id;

-- name: QuitarZonasDeCuadrilla :exec
DELETE FROM agenda.cuadrilla_zonas WHERE cuadrilla_id = @cuadrilla_id;

-- name: AgregarZonasACuadrilla :exec
INSERT INTO agenda.cuadrilla_zonas (cuadrilla_id, zona_id)
SELECT @cuadrilla_id, unnest(@zonas::bigint[]);

-- Técnicos ------------------------------------------------------------------------

-- name: TecnicoPorID :one
SELECT * FROM agenda.tecnicos WHERE id = @id;

-- name: TecnicoPorIDParaActualizar :one
SELECT * FROM agenda.tecnicos WHERE id = @id FOR UPDATE;

-- name: TecnicoPorUsuario :one
SELECT * FROM agenda.tecnicos WHERE usuario_id = @usuario_id;

-- name: TecnicosPorUsuarios :many
SELECT * FROM agenda.tecnicos WHERE usuario_id = ANY(@usuarios::bigint[]);

-- name: CrearTecnico :one
INSERT INTO agenda.tecnicos (usuario_id, cuadrilla_id, bodega_id, habilidades, base_lat, base_lon)
VALUES (@usuario_id, @cuadrilla_id, @bodega_id, @habilidades, @base_lat, @base_lon)
RETURNING *;

-- name: GuardarTecnico :one
UPDATE agenda.tecnicos SET cuadrilla_id = @cuadrilla_id, bodega_id = @bodega_id, habilidades = @habilidades,
    base_lat = @base_lat, base_lon = @base_lon, activo = @activo, version = version + 1, updated_at = now()
WHERE id = @id AND version = @version
RETURNING *;

-- name: ListarTecnicos :many
-- Fichas visibles: el usuario opera en la empresa (asignada o empresas.todas global; sin
-- conector, todos) y el alcance del actor cubre su cuadrilla (o sus zonas).
SELECT t.*, u.nombre, u.correo, u.activo AS usuario_activo
FROM agenda.tecnicos t
JOIN nucleo.usuarios u ON u.id = t.usuario_id
WHERE (sqlc.narg(despues_de)::bigint IS NULL OR t.id > sqlc.narg(despues_de))
  AND (@todas_empresas::boolean
       OR sqlc.narg(empresa_id)::bigint IS NULL
       OR EXISTS (SELECT 1 FROM nucleo.usuarios_empresas ue WHERE ue.usuario_id = t.usuario_id AND ue.empresa_id = sqlc.narg(empresa_id))
       OR EXISTS (SELECT 1 FROM nucleo.usuarios_roles ur JOIN nucleo.roles_permisos rp ON rp.rol_id = ur.rol_id
                  WHERE ur.usuario_id = t.usuario_id AND rp.permiso = 'empresas.todas'
                    AND ur.zona_id IS NULL AND ur.cuadrilla_id IS NULL))
  AND (@alcance_global::boolean
       OR t.cuadrilla_id = ANY(@cuadrillas::bigint[])
       OR (t.cuadrilla_id IS NOT NULL AND cardinality(@zonas::bigint[]) > 0
           AND (NOT EXISTS (SELECT 1 FROM agenda.cuadrilla_zonas cz WHERE cz.cuadrilla_id = t.cuadrilla_id)
                OR EXISTS (SELECT 1 FROM agenda.cuadrilla_zonas cz WHERE cz.cuadrilla_id = t.cuadrilla_id
                           AND cz.zona_id = ANY(@zonas::bigint[])))))
  AND (sqlc.narg(cuadrilla_id)::bigint IS NULL OR t.cuadrilla_id = sqlc.narg(cuadrilla_id))
  AND (sqlc.narg(usuario_id)::bigint IS NULL OR t.usuario_id = sqlc.narg(usuario_id))
  AND (sqlc.narg(activo)::boolean IS NULL OR (t.activo AND u.activo) = sqlc.narg(activo))
  AND (sqlc.narg(habilidad)::text IS NULL OR sqlc.narg(habilidad)::text = ANY(t.habilidades))
  AND (sqlc.narg(zona_id)::bigint IS NULL
       OR t.cuadrilla_id IS NULL
       OR NOT EXISTS (SELECT 1 FROM agenda.cuadrilla_zonas cz WHERE cz.cuadrilla_id = t.cuadrilla_id)
       OR EXISTS (SELECT 1 FROM agenda.cuadrilla_zonas cz WHERE cz.cuadrilla_id = t.cuadrilla_id AND cz.zona_id = sqlc.narg(zona_id)))
  AND (sqlc.narg(q)::text IS NULL OR u.nombre ILIKE '%' || sqlc.narg(q) || '%' OR u.correo ILIKE '%' || sqlc.narg(q) || '%')
ORDER BY t.id
LIMIT @limite;

-- name: TurnosDeTecnicos :many
SELECT * FROM agenda.turnos WHERE tecnico_id = ANY(@tecnicos::bigint[]) ORDER BY tecnico_id, dia, inicio;

-- name: QuitarTurnos :exec
DELETE FROM agenda.turnos WHERE tecnico_id = @tecnico_id;

-- name: CrearTurno :exec
INSERT INTO agenda.turnos (tecnico_id, dia, inicio, fin) VALUES (@tecnico_id, @dia, @inicio, @fin);

-- name: TocarTecnico :one
-- Sube la versión de la ficha (turnos y capacidad son parte de ella).
UPDATE agenda.tecnicos SET version = version + 1, updated_at = now()
WHERE id = @id AND version = @version
RETURNING *;

-- name: TocarCuadrilla :one
UPDATE agenda.cuadrillas SET version = version + 1, updated_at = now()
WHERE id = @id AND version = @version
RETURNING *;

-- Capacidad -----------------------------------------------------------------------

-- name: CapacidadesDeTecnico :many
SELECT * FROM agenda.capacidades WHERE tecnico_id = @tecnico_id ORDER BY tipo_id NULLS FIRST;

-- name: CapacidadesDeCuadrilla :many
SELECT * FROM agenda.capacidades WHERE cuadrilla_id = @cuadrilla_id ORDER BY tipo_id NULLS FIRST;

-- name: CapacidadesDeTecnicos :many
SELECT * FROM agenda.capacidades WHERE tecnico_id = ANY(@tecnicos::bigint[]) OR cuadrilla_id = ANY(@cuadrillas::bigint[]);

-- name: QuitarCapacidadesTecnico :exec
DELETE FROM agenda.capacidades WHERE tecnico_id = @tecnico_id;

-- name: QuitarCapacidadesCuadrilla :exec
DELETE FROM agenda.capacidades WHERE cuadrilla_id = @cuadrilla_id;

-- name: CrearCapacidad :exec
INSERT INTO agenda.capacidades (tecnico_id, cuadrilla_id, tipo_id, cupos, minutos)
VALUES (@tecnico_id, @cuadrilla_id, @tipo_id, @cupos, @minutos);

-- name: TiposOrden :many
SELECT id, codigo, nombre, duracion_min FROM campo.tipos_orden ORDER BY id;

-- Ausencias -----------------------------------------------------------------------

-- name: CrearAusencia :one
INSERT INTO agenda.ausencias (tecnico_id, inicio, fin, motivo, detalle, creada_por)
VALUES (@tecnico_id, @inicio, @fin, @motivo, @detalle, @creada_por)
RETURNING *;

-- name: AusenciaPorIDParaActualizar :one
SELECT * FROM agenda.ausencias WHERE id = @id FOR UPDATE;

-- name: AnularAusencia :one
UPDATE agenda.ausencias SET anulada_en = @anulada_en, anulada_por = @anulada_por, version = version + 1, updated_at = now()
WHERE id = @id AND version = @version
RETURNING *;

-- name: AusenciasEnRango :many
-- Ausencias vigentes (no anuladas) de los técnicos que tocan [desde, hasta).
SELECT * FROM agenda.ausencias
WHERE tecnico_id = ANY(@tecnicos::bigint[]) AND anulada_en IS NULL AND inicio < @hasta AND fin > @desde
ORDER BY tecnico_id, inicio;

-- name: ListarAusencias :many
SELECT * FROM agenda.ausencias
WHERE tecnico_id = @tecnico_id AND fin > @desde AND inicio < @hasta
  AND (@incluir_anuladas::boolean OR anulada_en IS NULL)
ORDER BY inicio, id;

-- Citas ---------------------------------------------------------------------------

-- name: CandadoAgendaTecnico :exec
-- Serializa las reservas de un técnico (solape y capacidad se verifican con él tomado).
SELECT pg_advisory_xact_lock(hashtextextended(concat('agenda.tecnico:', @usuario_id::bigint), 0));

-- name: CandadoAgendaCuadrilla :exec
SELECT pg_advisory_xact_lock(hashtextextended(concat('agenda.cuadrilla:', @cuadrilla_id::bigint), 0));

-- name: CitaDeOrden :one
SELECT * FROM agenda.citas WHERE orden_id = @orden_id;

-- name: CitasDeOrdenes :many
SELECT * FROM agenda.citas WHERE orden_id = ANY(@ordenes::bigint[]);

-- name: GuardarCita :one
INSERT INTO agenda.citas (orden_id, empresa_id, tecnico_id, tipo_id, inicio, fin, ventana_inicio, ventana_fin)
VALUES (@orden_id, @empresa_id, @tecnico_id, @tipo_id, @inicio, @fin, @ventana_inicio, @ventana_fin)
ON CONFLICT (orden_id) DO UPDATE SET
    empresa_id = EXCLUDED.empresa_id, tecnico_id = EXCLUDED.tecnico_id, tipo_id = EXCLUDED.tipo_id,
    inicio = EXCLUDED.inicio, fin = EXCLUDED.fin, ventana_inicio = EXCLUDED.ventana_inicio,
    ventana_fin = EXCLUDED.ventana_fin, version = agenda.citas.version + 1, updated_at = now()
RETURNING *;

-- name: QuitarCita :execrows
DELETE FROM agenda.citas WHERE orden_id = @orden_id;

-- name: Solapes :many
-- Citas del técnico que ocupan su agenda (orden programada, en camino o en sitio) y se cruzan
-- con [inicio, fin).
SELECT c.orden_id, o.numero, o.empresa_id, c.inicio, c.fin
FROM agenda.citas c
JOIN campo.ordenes o ON o.id = c.orden_id
WHERE c.tecnico_id = @tecnico_id AND c.orden_id <> @orden_id
  AND o.estado IN ('programada', 'en_camino', 'en_sitio')
  AND c.inicio < @fin AND c.fin > @inicio
ORDER BY c.inicio;

-- name: OcupacionTecnicos :many
-- Citas de los técnicos (usuarios) que empiezan en [desde, hasta) y cuentan para la agenda:
-- las vivas ocupan tiempo; las cerradas solo cuentan para la capacidad del día.
SELECT c.orden_id, c.tecnico_id, c.tipo_id, c.inicio, c.fin, o.estado, o.numero, o.empresa_id
FROM agenda.citas c
JOIN campo.ordenes o ON o.id = c.orden_id
WHERE c.tecnico_id = ANY(@tecnicos::bigint[]) AND c.orden_id <> @excluir_orden
  AND o.estado IN ('programada', 'en_camino', 'en_sitio', 'cerrada')
  AND c.inicio < @hasta AND c.fin > @desde
ORDER BY c.tecnico_id, c.inicio;

-- name: UsuariosDeCuadrilla :many
SELECT usuario_id FROM agenda.tecnicos WHERE cuadrilla_id = @cuadrilla_id;

-- name: AgendaCitas :many
-- Calendario de despacho: citas que tocan [desde, hasta) con su orden.
SELECT c.*, o.numero, o.estado, o.zona_id, o.cuadrilla_id AS orden_cuadrilla_id, o.direccion, o.lat, o.lon,
       o.cliente_id, o.servicio_id, o.version AS orden_version, tp.codigo AS tipo_codigo, t.cuadrilla_id AS tecnico_cuadrilla_id
FROM agenda.citas c
JOIN campo.ordenes o ON o.id = c.orden_id
JOIN campo.tipos_orden tp ON tp.id = c.tipo_id
LEFT JOIN agenda.tecnicos t ON t.usuario_id = c.tecnico_id
WHERE c.inicio < @hasta AND c.fin > @desde
  AND (@todas_empresas::boolean
       OR (sqlc.narg(empresa_id)::bigint IS NULL AND c.empresa_id IS NULL)
       OR c.empresa_id = sqlc.narg(empresa_id))
  AND (sqlc.narg(tecnico_id)::bigint IS NULL OR c.tecnico_id = sqlc.narg(tecnico_id))
  AND (sqlc.narg(cuadrilla_id)::bigint IS NULL OR t.cuadrilla_id = sqlc.narg(cuadrilla_id) OR o.cuadrilla_id = sqlc.narg(cuadrilla_id))
  AND (sqlc.narg(zona_id)::bigint IS NULL OR o.zona_id = sqlc.narg(zona_id))
  AND (@incluir_canceladas::boolean OR o.estado NOT IN ('cancelada', 'pendiente'))
  AND (@alcance_global::boolean
       OR o.zona_id = ANY(@zonas::bigint[])
       OR o.cuadrilla_id = ANY(@cuadrillas::bigint[])
       OR t.cuadrilla_id = ANY(@cuadrillas::bigint[]))
ORDER BY c.inicio, c.id
LIMIT 2000;

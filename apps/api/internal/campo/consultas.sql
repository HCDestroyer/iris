-- Consultas de campo (sqlc → internal/campo/bd).

-- Catálogo: tipos y plantillas ---------------------------------------------------

-- name: TipoPorCodigo :one
SELECT * FROM campo.tipos_orden WHERE codigo = @codigo;

-- name: TipoPorID :one
SELECT * FROM campo.tipos_orden WHERE id = @id;

-- name: ListarTipos :many
SELECT * FROM campo.tipos_orden ORDER BY id;

-- name: PlantillaPorID :one
SELECT * FROM campo.plantillas_checklist WHERE id = @id;

-- name: PlantillaPorCodigo :one
SELECT * FROM campo.plantillas_checklist WHERE codigo = @codigo;

-- name: ListarPlantillas :many
SELECT * FROM campo.plantillas_checklist WHERE activa ORDER BY id;

-- name: ItemsDePlantillas :many
SELECT * FROM campo.plantilla_items WHERE plantilla_id = ANY(@plantillas::bigint[]) ORDER BY plantilla_id, posicion, id;

-- name: ItemPorCodigo :one
SELECT * FROM campo.plantilla_items WHERE plantilla_id = @plantilla_id AND codigo = @codigo;

-- name: ItemPorID :one
SELECT * FROM campo.plantilla_items WHERE id = @id;

-- Usuarios y empresas (lectura de nucleo) -----------------------------------------

-- name: UsuarioAsignable :one
SELECT id, nombre, tipo, activo FROM nucleo.usuarios WHERE id = @id;

-- name: AlcancesDePermisoDeUsuario :many
-- Alcances con que el usuario tiene el permiso por sus roles (zona y cuadrilla null = global).
SELECT ur.zona_id, ur.cuadrilla_id
FROM nucleo.usuarios_roles ur
JOIN nucleo.roles_permisos rp ON rp.rol_id = ur.rol_id
WHERE ur.usuario_id = @usuario_id AND rp.permiso = @permiso;

-- name: HayEmpresas :one
SELECT EXISTS (SELECT 1 FROM nucleo.empresas);

-- name: EmpresaRegistrada :one
SELECT EXISTS (SELECT 1 FROM nucleo.empresas WHERE id = @id);

-- Instantáneas de Argos (solo lectura) --------------------------------------------

-- name: ClienteArgos :one
SELECT * FROM integracion.argos_clientes WHERE id = @id;

-- name: ServicioArgos :one
SELECT * FROM integracion.argos_servicios WHERE id = @id;

-- name: ClientesArgos :many
SELECT * FROM integracion.argos_clientes WHERE id = ANY(@ids::bigint[]);

-- name: ServiciosArgos :many
SELECT * FROM integracion.argos_servicios WHERE id = ANY(@ids::bigint[]);

-- Órdenes -------------------------------------------------------------------------

-- name: CrearOrden :one
INSERT INTO campo.ordenes (
    empresa_id, tipo_id, plantilla_id, ticket_id, prospecto_id, cliente_id, servicio_id, zona_id,
    cuadrilla_id, bodega_id, direccion, lat, lon, contacto_nombre, contacto_telefono, descripcion,
    creado_por, idempotencia
) VALUES (
    @empresa_id, @tipo_id, @plantilla_id, @ticket_id, @prospecto_id, @cliente_id, @servicio_id, @zona_id,
    @cuadrilla_id, @bodega_id, @direccion, @lat, @lon, @contacto_nombre, @contacto_telefono, @descripcion,
    @creado_por, sqlc.narg(idempotencia)
)
RETURNING *;

-- name: OrdenPorID :one
SELECT * FROM campo.ordenes WHERE id = @id;

-- name: OrdenPorIDParaActualizar :one
SELECT * FROM campo.ordenes WHERE id = @id FOR UPDATE;

-- name: OrdenPorIdempotencia :one
SELECT * FROM campo.ordenes WHERE idempotencia = @idempotencia;

-- name: GuardarOrden :one
-- Escribe el estado completo que calculó el caso de uso (fila leída con FOR UPDATE). Sube la
-- versión: con una versión vieja no actualiza nada (409).
UPDATE campo.ordenes SET
    tecnico_id = @tecnico_id, cuadrilla_id = @cuadrilla_id, bodega_id = @bodega_id, asignada_en = @asignada_en,
    estado = @estado, programada_inicio = @programada_inicio, programada_fin = @programada_fin,
    en_camino_en = @en_camino_en, en_sitio_en = @en_sitio_en, finalizada_en = @finalizada_en,
    resultado = @resultado, motivo_falla = @motivo_falla, motivo = @motivo,
    version = version + 1, updated_at = now()
WHERE id = @id AND version = @version
RETURNING *;

-- name: ListarOrdenes :many
-- Más recientes primero (cursor: id). El alcance limita a zonas, cuadrillas o lo asignado al
-- actor; solo_propias (ordenes.ver_propias sin ordenes.ver) deja solo las del actor.
SELECT * FROM campo.ordenes
WHERE (sqlc.narg(antes_de)::bigint IS NULL OR id < sqlc.narg(antes_de))
  AND (@todas_empresas::boolean
       OR (sqlc.narg(empresa_id)::bigint IS NULL AND empresa_id IS NULL)
       OR empresa_id = sqlc.narg(empresa_id))
  AND (cardinality(@estados::text[]) = 0 OR estado = ANY(@estados::text[]))
  AND (cardinality(@tipos::bigint[]) = 0 OR tipo_id = ANY(@tipos::bigint[]))
  AND (sqlc.narg(tecnico_id)::bigint IS NULL OR tecnico_id = sqlc.narg(tecnico_id))
  AND (NOT @sin_tecnico::boolean OR tecnico_id IS NULL)
  AND (sqlc.narg(ticket_id)::bigint IS NULL OR ticket_id = sqlc.narg(ticket_id))
  AND (sqlc.narg(cliente_id)::bigint IS NULL OR cliente_id = sqlc.narg(cliente_id))
  AND (sqlc.narg(servicio_id)::bigint IS NULL OR servicio_id = sqlc.narg(servicio_id))
  AND (sqlc.narg(zona_id)::bigint IS NULL OR zona_id = sqlc.narg(zona_id))
  AND (sqlc.narg(cuadrilla_id)::bigint IS NULL OR cuadrilla_id = sqlc.narg(cuadrilla_id))
  AND (sqlc.narg(desde)::timestamptz IS NULL OR programada_inicio >= sqlc.narg(desde))
  AND (sqlc.narg(hasta)::timestamptz IS NULL OR programada_inicio < sqlc.narg(hasta))
  AND (sqlc.narg(q)::text IS NULL OR numero ILIKE '%' || sqlc.narg(q) || '%' OR direccion ILIKE '%' || sqlc.narg(q) || '%'
       OR contacto_nombre ILIKE '%' || sqlc.narg(q) || '%')
  AND (CASE WHEN @solo_propias::boolean THEN tecnico_id = sqlc.narg(actor_id)
       ELSE (@alcance_global::boolean
             OR zona_id = ANY(@zonas::bigint[])
             OR cuadrilla_id = ANY(@cuadrillas::bigint[])
             OR (sqlc.narg(actor_id)::bigint IS NOT NULL AND tecnico_id = sqlc.narg(actor_id))) END)
ORDER BY id DESC
LIMIT @limite;

-- name: OrdenesDelPaquete :many
-- Órdenes vivas del técnico para el día (hora de Guatemala): las programadas hasta ese día
-- (también las atrasadas), las que ya están en camino o en sitio y las pendientes asignadas.
SELECT * FROM campo.ordenes
WHERE tecnico_id = @tecnico_id
  AND (@todas_empresas::boolean
       OR (sqlc.narg(empresa_id)::bigint IS NULL AND empresa_id IS NULL)
       OR empresa_id = sqlc.narg(empresa_id))
  AND estado IN ('pendiente', 'programada', 'en_camino', 'en_sitio')
  AND (programada_inicio IS NULL OR programada_inicio < @hasta)
ORDER BY programada_inicio NULLS LAST, id
LIMIT 500;

-- name: OrdenesPorIDs :many
SELECT * FROM campo.ordenes WHERE id = ANY(@ids::bigint[]) ORDER BY id;

-- Historial -----------------------------------------------------------------------

-- name: CrearEventoOrden :exec
INSERT INTO campo.orden_eventos (orden_id, tipo, de, a, motivo, datos, actor_tipo, actor_id, actor_nombre, canal, request_id, ocurrido_en)
VALUES (@orden_id, @tipo, @de, @a, @motivo, @datos, @actor_tipo, @actor_id, @actor_nombre, @canal, @request_id, @ocurrido_en);

-- name: ListarEventosOrden :many
SELECT * FROM campo.orden_eventos WHERE orden_id = @orden_id AND id > @despues_de ORDER BY id LIMIT @limite;

-- Checklist -----------------------------------------------------------------------

-- name: MarcaChecklist :one
SELECT * FROM campo.orden_checklist WHERE orden_id = @orden_id AND item_id = @item_id;

-- name: GuardarMarcaChecklist :exec
INSERT INTO campo.orden_checklist (orden_id, item_id, valor, marcado_por, canal, ocurrido_en, operacion_id)
VALUES (@orden_id, @item_id, @valor, @marcado_por, @canal, @ocurrido_en, @operacion_id)
ON CONFLICT (orden_id, item_id) DO UPDATE SET
    valor = EXCLUDED.valor, marcado_por = EXCLUDED.marcado_por, canal = EXCLUDED.canal,
    ocurrido_en = EXCLUDED.ocurrido_en, operacion_id = EXCLUDED.operacion_id, updated_at = now();

-- name: CrearHistorialChecklist :exec
INSERT INTO campo.orden_checklist_historial (orden_id, item_id, valor, marcado_por, canal, ocurrido_en, operacion_id, vigente)
VALUES (@orden_id, @item_id, @valor, @marcado_por, @canal, @ocurrido_en, @operacion_id, @vigente);

-- name: ChecklistDeOrdenes :many
SELECT * FROM campo.orden_checklist WHERE orden_id = ANY(@ordenes::bigint[]) ORDER BY orden_id, item_id;

-- name: HistorialChecklist :many
SELECT * FROM campo.orden_checklist_historial WHERE orden_id = @orden_id ORDER BY id;

-- Notas, materiales, equipos, firma y adjuntos ----------------------------------------

-- name: CrearNota :one
INSERT INTO campo.orden_notas (id, orden_id, texto, autor_id, autor_nombre, canal, ocurrido_en, operacion_id)
VALUES (@id, @orden_id, @texto, @autor_id, @autor_nombre, @canal, @ocurrido_en, @operacion_id)
RETURNING *;

-- name: NotasDeOrdenes :many
SELECT * FROM campo.orden_notas WHERE orden_id = ANY(@ordenes::bigint[]) ORDER BY orden_id, ocurrido_en, created_at;

-- name: CrearMaterial :one
INSERT INTO campo.orden_materiales (id, orden_id, producto_id, descripcion, cantidad, bodega_id, excede_existencia,
                                    registrado_por, canal, ocurrido_en, operacion_id)
VALUES (@id, @orden_id, @producto_id, @descripcion, @cantidad, @bodega_id, @excede_existencia,
        @registrado_por, @canal, @ocurrido_en, @operacion_id)
RETURNING *;

-- name: CantidadUsada :one
-- Lo ya usado de un producto en la orden (para comparar con la existencia de la bodega).
SELECT coalesce(sum(cantidad), 0)::numeric(14,3) FROM campo.orden_materiales
WHERE orden_id = @orden_id AND producto_id = @producto_id;

-- name: MaterialesDeOrdenes :many
SELECT * FROM campo.orden_materiales WHERE orden_id = ANY(@ordenes::bigint[]) ORDER BY orden_id, ocurrido_en, created_at;

-- name: CrearEquipo :one
INSERT INTO campo.orden_equipos (id, orden_id, accion, unidad_id, serie, mac, modelo, condicion, validado, registrado_por, canal, ocurrido_en, operacion_id)
VALUES (@id, @orden_id, @accion, @unidad_id, @serie, @mac, @modelo, @condicion, @validado, @registrado_por, @canal, @ocurrido_en, @operacion_id)
RETURNING *;

-- name: EquiposDeOrdenes :many
SELECT * FROM campo.orden_equipos WHERE orden_id = ANY(@ordenes::bigint[]) ORDER BY orden_id, ocurrido_en, created_at;

-- name: GuardarFirma :one
INSERT INTO campo.firmas (orden_id, nombre, documento, svg, adjunto_id, capturada_por, canal, ocurrido_en, operacion_id)
VALUES (@orden_id, @nombre, @documento, @svg, @adjunto_id, @capturada_por, @canal, @ocurrido_en, @operacion_id)
ON CONFLICT (orden_id) DO UPDATE SET
    nombre = EXCLUDED.nombre, documento = EXCLUDED.documento, svg = EXCLUDED.svg, adjunto_id = EXCLUDED.adjunto_id,
    capturada_por = EXCLUDED.capturada_por, canal = EXCLUDED.canal, ocurrido_en = EXCLUDED.ocurrido_en,
    operacion_id = EXCLUDED.operacion_id, invalidada_en = NULL, updated_at = now()
RETURNING *;

-- name: FirmasDeOrdenes :many
-- Solo las vigentes (una firma de un intento anterior no cuenta).
SELECT * FROM campo.firmas WHERE orden_id = ANY(@ordenes::bigint[]) AND invalidada_en IS NULL;

-- name: InvalidarFirma :execrows
UPDATE campo.firmas SET invalidada_en = @invalidada_en, updated_at = now()
WHERE orden_id = @orden_id AND invalidada_en IS NULL;

-- name: NotaPorID :one
SELECT * FROM campo.orden_notas WHERE id = @id;

-- name: MaterialPorID :one
SELECT * FROM campo.orden_materiales WHERE id = @id;

-- name: EquipoPorID :one
SELECT * FROM campo.orden_equipos WHERE id = @id;

-- name: AdjuntoRegistrado :one
SELECT * FROM campo.orden_adjuntos WHERE id = @id;

-- name: RegistrarAdjunto :one
INSERT INTO campo.orden_adjuntos (id, orden_id, tipo, item_id, descripcion, registrado_por, canal, ocurrido_en, operacion_id)
VALUES (@id, @orden_id, @tipo, @item_id, @descripcion, @registrado_por, @canal, @ocurrido_en, @operacion_id)
RETURNING *;

-- name: AdjuntosDeOrdenes :many
SELECT * FROM campo.orden_adjuntos WHERE orden_id = ANY(@ordenes::bigint[]) ORDER BY orden_id, created_at, id;

-- Sincronización: dispositivos, operaciones y feed de cambios ------------------------

-- name: GuardarDispositivo :one
-- Un dispositivo es de un usuario: el de otro no se toma (sin fila = dispositivo ajeno).
INSERT INTO campo.dispositivos (id, usuario_id, plataforma, ultimo_contacto, desfase_ms)
VALUES (@id, @usuario_id, @plataforma, @ultimo_contacto, @desfase_ms)
ON CONFLICT (id) DO UPDATE SET
    plataforma = EXCLUDED.plataforma, ultimo_contacto = EXCLUDED.ultimo_contacto,
    desfase_ms = EXCLUDED.desfase_ms, updated_at = now()
WHERE campo.dispositivos.usuario_id = EXCLUDED.usuario_id
RETURNING *;

-- name: OperacionPorID :one
SELECT * FROM campo.operaciones_sync WHERE id = @id;

-- name: OperacionesPorIDs :many
SELECT * FROM campo.operaciones_sync WHERE id = ANY(@ids::uuid[]);

-- name: ReclamarOperacion :execrows
-- Reclama la operación al empezar a aplicarla (en la transacción de la acción): un lote
-- concurrente con el mismo id espera el COMMIT y luego no reclama nada (0 filas = duplicada).
-- Una rechazada del mismo usuario se vuelve a evaluar.
INSERT INTO campo.operaciones_sync (id, dispositivo_id, usuario_id, orden_id, tipo, estado, resultado,
                                    version_base, ocurrido_en_telefono, ocurrido_en)
VALUES (@id, @dispositivo_id, @usuario_id, @orden_id, @tipo, 'aplicada', '{}'::jsonb,
        @version_base, @ocurrido_en_telefono, @ocurrido_en)
ON CONFLICT (id) DO UPDATE SET
    dispositivo_id = EXCLUDED.dispositivo_id, orden_id = EXCLUDED.orden_id, tipo = EXCLUDED.tipo,
    estado = 'aplicada', resultado = '{}'::jsonb, version_base = EXCLUDED.version_base,
    ocurrido_en_telefono = EXCLUDED.ocurrido_en_telefono, ocurrido_en = EXCLUDED.ocurrido_en, updated_at = now()
WHERE campo.operaciones_sync.estado = 'rechazada' AND campo.operaciones_sync.usuario_id = EXCLUDED.usuario_id;

-- name: FijarResultadoOperacion :exec
UPDATE campo.operaciones_sync SET resultado = @resultado, updated_at = now() WHERE id = @id;

-- name: GuardarRechazo :exec
-- Guarda una rechazada (para rastrear); nunca pisa una aplicada ni la de otro usuario.
INSERT INTO campo.operaciones_sync (id, dispositivo_id, usuario_id, orden_id, tipo, estado, resultado,
                                    version_base, ocurrido_en_telefono, ocurrido_en)
VALUES (@id, @dispositivo_id, @usuario_id, @orden_id, @tipo, 'rechazada', @resultado,
        @version_base, @ocurrido_en_telefono, @ocurrido_en)
ON CONFLICT (id) DO UPDATE SET
    dispositivo_id = EXCLUDED.dispositivo_id, orden_id = EXCLUDED.orden_id, tipo = EXCLUDED.tipo,
    resultado = EXCLUDED.resultado, version_base = EXCLUDED.version_base,
    ocurrido_en_telefono = EXCLUDED.ocurrido_en_telefono, ocurrido_en = EXCLUDED.ocurrido_en, updated_at = now()
WHERE campo.operaciones_sync.estado = 'rechazada' AND campo.operaciones_sync.usuario_id = EXCLUDED.usuario_id;

-- name: CandadoCambiosTecnico :exec
-- Serializa el feed de un técnico: sus filas toman id en orden de COMMIT.
SELECT pg_advisory_xact_lock(hashtextextended(concat('campo.cambios:', @tecnico_id::bigint), 0));

-- name: CrearCambio :exec
INSERT INTO campo.orden_cambios (tecnico_id, orden_id, tipo) VALUES (@tecnico_id, @orden_id, @tipo);

-- name: UltimoCambio :one
SELECT coalesce(max(id), 0)::bigint FROM campo.orden_cambios WHERE tecnico_id = @tecnico_id;

-- name: CambiosDesde :many
SELECT * FROM campo.orden_cambios WHERE tecnico_id = @tecnico_id AND id > @despues_de ORDER BY id LIMIT @limite;

-- name: TuvoOrden :one
-- La orden estuvo asignada al técnico (para distinguir «reasignada» de «no existe»).
SELECT EXISTS (SELECT 1 FROM campo.orden_cambios WHERE tecnico_id = @tecnico_id AND orden_id = @orden_id);

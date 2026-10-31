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

-- name: PlantillasPorIDs :many
SELECT * FROM campo.plantillas_checklist WHERE id = ANY(@ids::bigint[]);

-- name: PlantillaPorIDParaActualizar :one
SELECT * FROM campo.plantillas_checklist WHERE id = @id FOR UPDATE;

-- name: PlantillaPorIDCompartida :one
-- Al agregar un formulario: la edición (FOR UPDATE) espera, así no reescribe una versión que
-- se acaba de empezar a usar.
SELECT * FROM campo.plantillas_checklist WHERE id = @id FOR SHARE;

-- name: EsPlantillaDeTipo :one
SELECT EXISTS (SELECT 1 FROM campo.tipos_orden WHERE plantilla_id = @plantilla_id::bigint AND activo);

-- name: PlantillasVisibles :many
-- Plantillas que ve la empresa: las comunes (empresa null) y las de la empresa (null = sin
-- conector: solo las comunes; todas = el sistema).
SELECT * FROM campo.plantillas_checklist
WHERE (@todas_empresas::boolean OR empresa_id IS NULL OR empresa_id = sqlc.narg(empresa_id))
  AND (NOT @solo_activas::boolean OR activa)
  AND (sqlc.narg(tipo)::text IS NULL OR tipo_codigo IS NULL OR tipo_codigo = sqlc.narg(tipo))
  AND (sqlc.narg(q)::text IS NULL OR nombre ILIKE '%' || sqlc.narg(q) || '%' OR codigo ILIKE '%' || sqlc.narg(q) || '%')
ORDER BY nombre, id
LIMIT 500;

-- name: PaginaPlantillas :many
-- GET /plantillas: mismas reglas que PlantillasVisibles, por nombre e id (cursor: el último).
SELECT * FROM campo.plantillas_checklist
WHERE (@todas_empresas::boolean OR empresa_id IS NULL OR empresa_id = sqlc.narg(empresa_id))
  AND (NOT @solo_activas::boolean OR activa)
  AND (sqlc.narg(tipo)::text IS NULL OR tipo_codigo IS NULL OR tipo_codigo = sqlc.narg(tipo))
  AND (sqlc.narg(q)::text IS NULL OR nombre ILIKE '%' || sqlc.narg(q) || '%' OR codigo ILIKE '%' || sqlc.narg(q) || '%')
  AND (sqlc.narg(despues_id)::bigint IS NULL OR (nombre, id) > (sqlc.narg(despues_nombre)::text, sqlc.narg(despues_id)::bigint))
ORDER BY nombre, id
LIMIT @limite;

-- name: CrearPlantilla :one
-- Nace con una versión provisional (la 1 se crea enseguida y se fija con FijarVersionActual).
INSERT INTO campo.plantillas_checklist (codigo, nombre, descripcion, tipo_codigo, requiere_firma, empresa_id, creado_por,
                                        version_actual_id, activa, alguna_vez_activa)
VALUES (@codigo, @nombre, @descripcion, sqlc.narg(tipo_codigo), @requiere_firma, sqlc.narg(empresa_id), sqlc.narg(creado_por),
        0, @activa, @activa)
RETURNING *;

-- name: FijarVersionActual :one
UPDATE campo.plantillas_checklist SET version_actual_id = @version_id, nombre = @nombre, requiere_firma = @requiere_firma,
    updated_at = now()
WHERE id = @id
RETURNING *;

-- name: GuardarPlantilla :one
-- Datos de la plantilla (no versionados) y la versión actual. Con una versión vieja no actualiza
-- nada (409).
UPDATE campo.plantillas_checklist SET nombre = @nombre, descripcion = @descripcion, tipo_codigo = sqlc.narg(tipo_codigo),
    requiere_firma = @requiere_firma, activa = @activa, version_actual_id = @version_actual_id,
    alguna_vez_activa = alguna_vez_activa OR @activa,
    version = version + 1, updated_at = now()
WHERE id = @id AND version = @version
RETURNING *;

-- name: CrearVersion :one
INSERT INTO campo.plantilla_versiones (plantilla_id, numero, nombre, requiere_firma, creada_por, created_at)
VALUES (@plantilla_id,
        (SELECT coalesce(max(numero), 0) + 1 FROM campo.plantilla_versiones WHERE plantilla_id = @plantilla_id),
        @nombre, @requiere_firma, sqlc.narg(creada_por), @creada_en)
RETURNING *;

-- name: ReescribirVersion :one
-- Una versión sin uso se reescribe (sus ítems se reemplazan aparte).
UPDATE campo.plantilla_versiones SET nombre = @nombre, requiere_firma = @requiere_firma, updated_at = now()
WHERE id = @id
RETURNING *;

-- name: VersionPorID :one
SELECT * FROM campo.plantilla_versiones WHERE id = @id;

-- name: VersionPorNumero :one
SELECT * FROM campo.plantilla_versiones WHERE plantilla_id = @plantilla_id AND numero = @numero;

-- name: VersionVigenteEn :one
-- La versión de la plantilla vigente en ese momento: la última creada hasta entonces.
SELECT * FROM campo.plantilla_versiones
WHERE plantilla_id = @plantilla_id AND created_at <= @momento
ORDER BY numero DESC
LIMIT 1;

-- name: VersionesPorIDs :many
SELECT * FROM campo.plantilla_versiones WHERE id = ANY(@ids::bigint[]);

-- name: VersionesDePlantilla :many
-- Con la cantidad de formularios (de órdenes) que la usan.
SELECT v.*, (SELECT count(*) FROM campo.orden_formularios f WHERE f.version_id = v.id)::bigint AS formularios
FROM campo.plantilla_versiones v
WHERE v.plantilla_id = @plantilla_id
ORDER BY v.numero DESC;

-- name: VersionEnUso :one
-- Algún formulario de una orden (vigente o quitado) usa la versión: ya no se reescribe.
SELECT EXISTS (SELECT 1 FROM campo.orden_formularios WHERE version_id = @version_id);

-- name: BorrarItemsDeVersion :exec
DELETE FROM campo.plantilla_items WHERE version_id = @version_id;

-- name: CrearItems :exec
INSERT INTO campo.plantilla_items (plantilla_id, version_id, posicion, codigo, etiqueta, tipo, obligatorio, unidad, opciones)
SELECT @plantilla_id, @version_id, i.posicion, i.codigo, i.etiqueta, i.tipo, i.obligatorio, i.unidad, i.opciones
FROM jsonb_to_recordset(@items::jsonb)
     AS i (posicion integer, codigo text, etiqueta text, tipo text, obligatorio boolean, unidad text, opciones jsonb);

-- name: ItemsDeVersiones :many
SELECT * FROM campo.plantilla_items WHERE version_id = ANY(@versiones::bigint[]) ORDER BY version_id, posicion, id;

-- name: ItemPorID :one
SELECT * FROM campo.plantilla_items WHERE id = @id;

-- Usuarios y empresas (lectura de nucleo) -----------------------------------------

-- name: UsuarioAsignable :one
SELECT id, nombre, tipo, activo FROM nucleo.usuarios WHERE id = @id;

-- name: AlcancesDePermisoDeUsuario :many
-- Alcances con que el usuario tiene el permiso por sus roles (zona y cuadrilla null = global).
SELECT ur.zona_id, ur.cuadrilla_id
FROM nucleo.usuarios_roles ur
JOIN nucleo.roles ra ON ra.id = ur.rol_id AND ra.activo JOIN nucleo.roles_permisos rp ON rp.rol_id = ur.rol_id
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
    creado_por, idempotencia, origen, referencia
) VALUES (
    @empresa_id, @tipo_id, @plantilla_id, @ticket_id, @prospecto_id, @cliente_id, @servicio_id, @zona_id,
    @cuadrilla_id, @bodega_id, @direccion, @lat, @lon, @contacto_nombre, @contacto_telefono, @descripcion,
    @creado_por, sqlc.narg(idempotencia), @origen, sqlc.narg(referencia)
)
RETURNING *;

-- name: OrdenPorID :one
SELECT * FROM campo.ordenes WHERE id = @id;

-- name: OrdenPorIDParaActualizar :one
SELECT * FROM campo.ordenes WHERE id = @id FOR UPDATE;

-- name: OrdenPorIdempotencia :one
SELECT * FROM campo.ordenes WHERE idempotencia = @idempotencia;

-- name: OrdenesAbiertasDeTicket :many
-- Las otras órdenes del ticket que siguen abiertas (ni cerradas ni canceladas), al cerrar una.
SELECT numero FROM campo.ordenes
WHERE ticket_id = @ticket_id AND id <> @excluir AND estado NOT IN ('cerrada', 'cancelada')
ORDER BY id;

-- name: GuardarOrden :one
-- Escribe el estado completo que calculó el caso de uso (fila leída con FOR UPDATE). Sube la
-- versión: con una versión vieja no actualiza nada (409).
UPDATE campo.ordenes SET
    tecnico_id = @tecnico_id, cuadrilla_id = @cuadrilla_id, bodega_id = @bodega_id, asignada_en = @asignada_en,
    estado = @estado, programada_inicio = @programada_inicio, programada_fin = @programada_fin,
    en_camino_en = @en_camino_en, en_sitio_en = @en_sitio_en, finalizada_en = @finalizada_en,
    resultado = @resultado, motivo_falla = @motivo_falla, motivo = @motivo,
    pausada_desde = @pausada_desde, pausada_en = @pausada_en, pausada_seg = @pausada_seg,
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
-- (también las atrasadas), las que ya están en camino, en sitio o pausadas y las pendientes
-- asignadas.
SELECT * FROM campo.ordenes
WHERE tecnico_id = @tecnico_id
  AND (@todas_empresas::boolean
       OR (sqlc.narg(empresa_id)::bigint IS NULL AND empresa_id IS NULL)
       OR empresa_id = sqlc.narg(empresa_id))
  AND estado IN ('pendiente', 'programada', 'en_camino', 'en_sitio', 'pausada')
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
SELECT * FROM campo.orden_checklist WHERE formulario_id = @formulario_id AND item_id = @item_id;

-- name: GuardarMarcaChecklist :exec
INSERT INTO campo.orden_checklist (formulario_id, orden_id, item_id, valor, marcado_por, canal, ocurrido_en, operacion_id)
VALUES (@formulario_id, @orden_id, @item_id, @valor, @marcado_por, @canal, @ocurrido_en, @operacion_id)
ON CONFLICT (formulario_id, item_id) DO UPDATE SET
    valor = EXCLUDED.valor, marcado_por = EXCLUDED.marcado_por, canal = EXCLUDED.canal,
    ocurrido_en = EXCLUDED.ocurrido_en, operacion_id = EXCLUDED.operacion_id, updated_at = now();

-- name: CrearHistorialChecklist :exec
INSERT INTO campo.orden_checklist_historial (formulario_id, orden_id, item_id, valor, marcado_por, canal, ocurrido_en, operacion_id, vigente)
VALUES (@formulario_id, @orden_id, @item_id, @valor, @marcado_por, @canal, @ocurrido_en, @operacion_id, @vigente);

-- name: ChecklistDeOrdenes :many
SELECT * FROM campo.orden_checklist WHERE orden_id = ANY(@ordenes::bigint[]) ORDER BY orden_id, item_id;

-- name: HistorialChecklist :many
SELECT * FROM campo.orden_checklist_historial WHERE orden_id = @orden_id ORDER BY id;

-- Formularios de la orden ---------------------------------------------------------------

-- name: CrearFormulario :one
INSERT INTO campo.orden_formularios (id, orden_id, plantilla_id, version_id, origen, agregado_por, agregado_en, canal, operacion_id)
VALUES (@id, @orden_id, @plantilla_id, @version_id, @origen, sqlc.narg(agregado_por), @agregado_en, @canal, sqlc.narg(operacion_id))
RETURNING *;

-- name: FormularioPorID :one
SELECT * FROM campo.orden_formularios WHERE id = @id;

-- name: FormulariosDeOrdenes :many
-- Los vigentes (no quitados), en el orden en que se agregaron.
SELECT * FROM campo.orden_formularios
WHERE orden_id = ANY(@ordenes::bigint[]) AND quitado_en IS NULL
ORDER BY orden_id, agregado_en, created_at, id;

-- name: FormularioVigenteDePlantilla :one
SELECT * FROM campo.orden_formularios
WHERE orden_id = @orden_id AND plantilla_id = @plantilla_id AND quitado_en IS NULL;

-- name: FormularioDeVersion :one
-- El formulario de la orden con esa versión (vigente primero; si no, el último quitado).
SELECT * FROM campo.orden_formularios
WHERE orden_id = @orden_id AND version_id = @version_id
ORDER BY (quitado_en IS NULL) DESC, quitado_en DESC
LIMIT 1;

-- name: QuitarFormulario :one
UPDATE campo.orden_formularios SET quitado_en = @quitado_en, quitado_por = sqlc.narg(quitado_por), updated_at = now()
WHERE id = @id AND quitado_en IS NULL
RETURNING *;

-- name: FormulariosConRespuestas :many
-- Los que tienen alguna marca (aunque no sea la vigente) o un adjunto registrado en uno de sus
-- ítems: ya no se quitan.
SELECT f.id FROM campo.orden_formularios f
WHERE f.id = ANY(@ids::uuid[])
  AND (EXISTS (SELECT 1 FROM campo.orden_checklist_historial h WHERE h.formulario_id = f.id)
       OR EXISTS (SELECT 1 FROM campo.orden_adjuntos a
                  JOIN campo.plantilla_items i ON i.id = a.item_id
                  WHERE a.orden_id = f.orden_id AND i.version_id = f.version_id));

-- Pausas ----------------------------------------------------------------------------------

-- name: CrearPausa :one
INSERT INTO campo.orden_pausas (orden_id, motivo, nota, desde_estado, inicio, pausada_por, canal, operacion_id)
VALUES (@orden_id, @motivo, sqlc.narg(nota), @desde_estado, @inicio, sqlc.narg(pausada_por), @canal, sqlc.narg(operacion_id))
RETURNING *;

-- name: PausaAbierta :one
SELECT * FROM campo.orden_pausas WHERE orden_id = @orden_id AND fin IS NULL;

-- name: CerrarPausa :one
UPDATE campo.orden_pausas SET fin = @fin, cierre = @cierre, cerrada_por = sqlc.narg(cerrada_por),
    canal_cierre = @canal_cierre, operacion_cierre = sqlc.narg(operacion_cierre), updated_at = now()
WHERE id = @id AND fin IS NULL
RETURNING *;

-- name: FinUltimaPausa :one
-- Fin de la última pausa terminada de la orden (piso de una pausa nueva).
SELECT coalesce(max(fin), 'epoch'::timestamptz)::timestamptz FROM campo.orden_pausas WHERE orden_id = @orden_id AND fin IS NOT NULL;

-- name: PausadoDesde :one
-- Segundos de las pausas terminadas que empezaron desde ese momento (la visita en curso).
SELECT coalesce(sum(extract(epoch FROM fin - inicio)), 0)::bigint FROM campo.orden_pausas
WHERE orden_id = @orden_id AND fin IS NOT NULL AND inicio >= @desde;

-- name: PausasDeOrdenes :many
SELECT * FROM campo.orden_pausas WHERE orden_id = ANY(@ordenes::bigint[]) ORDER BY orden_id, inicio, id;

-- Notas, materiales, equipos, firma y adjuntos ----------------------------------------

-- name: CrearNota :one
INSERT INTO campo.orden_notas (id, orden_id, texto, autor_id, autor_nombre, canal, ocurrido_en, operacion_id)
VALUES (@id, @orden_id, @texto, @autor_id, @autor_nombre, @canal, @ocurrido_en, @operacion_id)
RETURNING *;

-- name: NotasDeOrdenes :many
SELECT * FROM campo.orden_notas WHERE orden_id = ANY(@ordenes::bigint[]) ORDER BY orden_id, ocurrido_en, created_at;

-- name: CrearMaterial :one
INSERT INTO campo.orden_materiales (id, orden_id, producto_id, descripcion, cantidad, bodega_id, excede_existencia,
                                    registrado_por, canal, ocurrido_en, operacion_id, modalidad)
VALUES (@id, @orden_id, @producto_id, @descripcion, @cantidad, @bodega_id, @excede_existencia,
        @registrado_por, @canal, @ocurrido_en, @operacion_id, sqlc.narg(modalidad))
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

-- Agenda (lectura de agenda.*) ------------------------------------------------------

-- name: BodegaDeTecnico :one
-- La bodega de Argos de la ficha de agenda del técnico (usuario).
SELECT bodega_id FROM agenda.tecnicos WHERE usuario_id = @usuario_id AND activo;

-- name: VinculoArgosDeTecnico :one
-- El vínculo con Argos de la ficha del técnico (contrato 0.5.0 §3.3): va como tecnico en orden.cerrada.
SELECT coalesce(argos_id_externo, '')::text AS id_externo FROM agenda.tecnicos WHERE usuario_id = @usuario_id;

-- name: CitasDeOrdenes :many
SELECT * FROM agenda.citas WHERE orden_id = ANY(@ordenes::bigint[]);

-- Retiro automático (argos.servicio_retirado) ----------------------------------------

-- name: CandadoRetiroServicio :exec
SELECT pg_advisory_xact_lock(hashtextextended(concat('campo.retiro:', @servicio_id::bigint), 0));

-- name: RetiroAbiertoDeServicio :one
-- Una orden de retiro del servicio que no terminó (una fallida también cuenta: se reprograma).
SELECT o.id FROM campo.ordenes o
JOIN campo.tipos_orden tp ON tp.id = o.tipo_id
WHERE o.servicio_id = @servicio_id AND tp.codigo = 'retiro'
  AND o.estado NOT IN ('cerrada', 'cancelada')
  AND (sqlc.narg(empresa_id)::bigint IS NULL AND o.empresa_id IS NULL OR o.empresa_id = sqlc.narg(empresa_id))
ORDER BY o.id
LIMIT 1;

-- Posiciones en vivo ------------------------------------------------------------------

-- name: AsegurarParticionPosiciones :exec
SELECT campo.asegurar_particion_posiciones(@fecha::timestamptz);

-- name: ContarLotePosiciones :one
-- Ventana fija de un minuto por técnico: devuelve los lotes de la ventana contando este.
INSERT INTO campo.posiciones_limite AS l (tecnico_id, ventana_inicio, lotes)
VALUES (@tecnico_id, @ahora, 1)
ON CONFLICT (tecnico_id) DO UPDATE SET
    lotes = (CASE WHEN l.ventana_inicio <= EXCLUDED.ventana_inicio - interval '1 minute' THEN 1 ELSE l.lotes + 1 END),
    ventana_inicio = (CASE WHEN l.ventana_inicio <= EXCLUDED.ventana_inicio - interval '1 minute'
                           THEN EXCLUDED.ventana_inicio ELSE l.ventana_inicio END),
    updated_at = now()
RETURNING lotes, ventana_inicio;

-- name: InsertarPosiciones :execrows
-- Un punto por técnico y hora (reenviar un lote no duplica).
INSERT INTO campo.posiciones (tecnico_id, empresa_id, lat, lon, precision_m, velocidad, rumbo, dispositivo_id, registrada_en)
SELECT @tecnico_id, sqlc.narg(empresa_id)::bigint, p.lat, p.lon, p.precision_m, p.velocidad, p.rumbo, sqlc.narg(dispositivo_id)::text, p.registrada_en
FROM jsonb_to_recordset(@puntos::jsonb)
     AS p (lat double precision, lon double precision, precision_m double precision, velocidad double precision,
           rumbo double precision, registrada_en timestamptz)
ON CONFLICT (tecnico_id, registrada_en) DO NOTHING;

-- name: GuardarUltimaPosicion :exec
-- La más reciente por técnico y empresa (solo si es más nueva que la guardada).
INSERT INTO campo.posiciones_ultimas (tecnico_id, empresa_id, lat, lon, precision_m, velocidad, rumbo, dispositivo_id, registrada_en, recibida_en)
VALUES (@tecnico_id, sqlc.narg(empresa_id), @lat, @lon, sqlc.narg(precision_m), sqlc.narg(velocidad), sqlc.narg(rumbo), sqlc.narg(dispositivo_id), @registrada_en, @recibida_en)
ON CONFLICT (tecnico_id, empresa_id) DO UPDATE SET
    lat = EXCLUDED.lat, lon = EXCLUDED.lon, precision_m = EXCLUDED.precision_m, velocidad = EXCLUDED.velocidad,
    rumbo = EXCLUDED.rumbo, dispositivo_id = EXCLUDED.dispositivo_id, registrada_en = EXCLUDED.registrada_en,
    recibida_en = EXCLUDED.recibida_en, updated_at = now()
WHERE campo.posiciones_ultimas.registrada_en < EXCLUDED.registrada_en;

-- name: UltimasPosiciones :many
-- Última posición de cada técnico en la empresa, según el alcance del actor (cuadrilla del
-- técnico o las zonas que atiende su cuadrilla, de la empresa activa; una cuadrilla sin zonas no
-- la ve quien tiene alcance por zona).
SELECT p.*, u.nombre, t.id AS ficha_id, t.cuadrilla_id
FROM campo.posiciones_ultimas p
JOIN nucleo.usuarios u ON u.id = p.tecnico_id
LEFT JOIN agenda.tecnicos t ON t.usuario_id = p.tecnico_id
WHERE (@todas_empresas::boolean
       OR (sqlc.narg(empresa_id)::bigint IS NULL AND p.empresa_id IS NULL)
       OR p.empresa_id = sqlc.narg(empresa_id))
  AND p.registrada_en >= @desde
  AND (sqlc.narg(tecnico_id)::bigint IS NULL OR p.tecnico_id = sqlc.narg(tecnico_id))
  AND (sqlc.narg(cuadrilla_id)::bigint IS NULL OR t.cuadrilla_id = sqlc.narg(cuadrilla_id))
  AND (sqlc.narg(zona_id)::bigint IS NULL
       OR (t.cuadrilla_id IS NOT NULL
           AND (NOT EXISTS (SELECT 1 FROM agenda.cuadrilla_zonas cz WHERE cz.cuadrilla_id = t.cuadrilla_id)
                OR EXISTS (SELECT 1 FROM agenda.cuadrilla_zonas cz WHERE cz.cuadrilla_id = t.cuadrilla_id AND cz.zona_id = sqlc.narg(zona_id)))))
  AND (@alcance_global::boolean
       OR EXISTS (SELECT 1 FROM agenda.cuadrillas c
                  WHERE c.id = t.cuadrilla_id
                    AND (c.empresa_id = sqlc.narg(empresa_id) OR (c.empresa_id IS NULL AND @cuadrilla_comun::boolean))
                    AND (c.id = ANY(@cuadrillas::bigint[])
                         OR EXISTS (SELECT 1 FROM agenda.cuadrilla_zonas cz
                                    WHERE cz.cuadrilla_id = c.id AND cz.zona_id = ANY(@zonas::bigint[])))))
ORDER BY u.nombre, p.tecnico_id
LIMIT 1000;

-- name: PurgarPosiciones :one
SELECT campo.purgar_posiciones(@limite::timestamptz)::integer;

-- name: PurgarUltimasPosiciones :execrows
DELETE FROM campo.posiciones_ultimas WHERE registrada_en < @limite;

-- name: CandadoPosicionesTecnico :exec
-- Serializa los lotes de un técnico (el intervalo mínimo se mide contra lo ya guardado).
SELECT pg_advisory_xact_lock(hashtextextended(concat('campo.posiciones:', @tecnico_id::bigint), 0));

-- name: PosicionesCercanas :many
-- Horas de las posiciones guardadas del técnico en [desde, hasta], en orden.
SELECT registrada_en FROM campo.posiciones
WHERE tecnico_id = @tecnico_id AND registrada_en BETWEEN @desde AND @hasta
ORDER BY registrada_en;

-- Datos de Argos cambiados (servicio.datos_cambiados / cliente.actualizado, contrato 0.5.0) -------

-- name: MarcarDatosArgos :many
-- Órdenes abiertas del servicio (o del cliente, sin servicio en la orden): conservan lo que copiaron
-- y quedan marcadas «dato de Argos cambió» con los campos acumulados. No sube la versión.
UPDATE campo.ordenes o
SET datos_argos_cambiados_en = @cambiado_en,
    datos_argos_campos = ARRAY(SELECT DISTINCT c FROM unnest(o.datos_argos_campos || @campos::text[]) AS c ORDER BY c),
    updated_at = now()
WHERE o.estado NOT IN ('cerrada', 'fallida', 'cancelada')
  AND ((sqlc.narg(servicio_id)::bigint IS NOT NULL AND o.servicio_id = sqlc.narg(servicio_id)::bigint)
    OR (sqlc.narg(cliente_id)::bigint IS NOT NULL AND o.cliente_id = sqlc.narg(cliente_id)::bigint
        AND (@solo_sin_servicio::boolean = false OR o.servicio_id IS NULL)))
RETURNING o.id, o.numero, o.empresa_id;

-- name: AplicarDatosArgosOrden :one
-- Despacho aplica a la orden los datos actuales de la instantánea de Argos (dirección, coordenadas,
-- referencia y contacto, ya calculados por el caso de uso) y quita la marca «dato de Argos cambió».
-- Sube la versión: con una versión vieja no actualiza nada (409).
UPDATE campo.ordenes SET
    direccion = @direccion, lat = sqlc.narg(lat), lon = sqlc.narg(lon), referencia = sqlc.narg(referencia),
    contacto_nombre = sqlc.narg(contacto_nombre), contacto_telefono = sqlc.narg(contacto_telefono),
    datos_argos_cambiados_en = NULL, datos_argos_campos = '{}', version = version + 1, updated_at = now()
WHERE id = @id AND version = @version
RETURNING *;

-- name: DescartarDatosArgosOrden :one
-- Despacho revisó el aviso y la orden se queda con lo suyo: solo se quita la marca.
UPDATE campo.ordenes SET
    datos_argos_cambiados_en = NULL, datos_argos_campos = '{}', version = version + 1, updated_at = now()
WHERE id = @id AND version = @version
RETURNING *;

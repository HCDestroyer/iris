-- Seguimiento comercial (internal/comercial) ---------------------------------------------------

-- Prospectos ----------------------------------------------------------------------------------

-- name: CrearProspecto :one
INSERT INTO comercial.prospectos (
    empresa_id, origen, origen_detalle, nombre, documento, telefono, correo, direccion, lat, lon,
    zona_id, plan_interes_id, plan_interes_nombre, vendedor_id, notas, creado_por, idempotencia
) VALUES (
    sqlc.narg(empresa_id), @origen, sqlc.narg(origen_detalle), @nombre, sqlc.narg(documento), sqlc.narg(telefono),
    sqlc.narg(correo), @direccion, sqlc.narg(lat), sqlc.narg(lon), sqlc.narg(zona_id), sqlc.narg(plan_interes_id),
    sqlc.narg(plan_interes_nombre), sqlc.narg(vendedor_id), @notas, sqlc.narg(creado_por), sqlc.narg(idempotencia)
)
RETURNING *;

-- name: ProspectoPorIdempotencia :one
SELECT * FROM comercial.prospectos WHERE idempotencia = @idempotencia;

-- name: ProspectoPorID :one
SELECT * FROM comercial.prospectos WHERE id = @id;

-- name: ProspectoPorIDParaActualizar :one
SELECT * FROM comercial.prospectos WHERE id = @id FOR UPDATE;

-- name: GuardarProspecto :one
-- Guarda los campos que cambian (control de versión: 0 filas = otro lo cambió).
UPDATE comercial.prospectos
SET origen_detalle      = sqlc.narg(origen_detalle),
    nombre              = @nombre,
    documento           = sqlc.narg(documento),
    telefono            = sqlc.narg(telefono),
    correo              = sqlc.narg(correo),
    direccion           = @direccion,
    lat                 = sqlc.narg(lat),
    lon                 = sqlc.narg(lon),
    zona_id             = sqlc.narg(zona_id),
    plan_interes_id     = sqlc.narg(plan_interes_id),
    plan_interes_nombre = sqlc.narg(plan_interes_nombre),
    vendedor_id         = sqlc.narg(vendedor_id),
    estado              = @estado,
    notas               = @notas,
    motivo_perdida      = sqlc.narg(motivo_perdida),
    perdido_en          = sqlc.narg(perdido_en),
    ganado_en           = sqlc.narg(ganado_en),
    obra_confirmada     = @obra_confirmada,
    alta_modo           = sqlc.narg(alta_modo),
    alta_estado         = sqlc.narg(alta_estado),
    cliente_argos_id    = sqlc.narg(cliente_argos_id),
    servicio_argos_id   = sqlc.narg(servicio_argos_id),
    orden_id            = sqlc.narg(orden_id),
    version             = version + 1,
    updated_at          = now()
WHERE id = @id AND version = @version
RETURNING *;

-- name: ListarProspectos :many
-- Más recientes primero. @todas = sin filtro de empresa (actor sistema); si no, la empresa
-- activa (null = modo sin conector). Alcance (falla cerrado): global, la zona del prospecto
-- entre las del actor o el prospecto es del actor (vendedor).
SELECT * FROM comercial.prospectos p
WHERE (@despues_de::bigint = 0 OR p.id < @despues_de)
  AND (@todas::boolean OR p.empresa_id IS NOT DISTINCT FROM sqlc.narg(empresa_id)::bigint)
  AND (@global::boolean OR p.zona_id = ANY (@zonas::bigint[]) OR p.vendedor_id = sqlc.narg(propio)::bigint)
  AND (cardinality(@estados::text[]) = 0 OR p.estado = ANY (@estados::text[]))
  AND (sqlc.narg(origen)::text IS NULL OR p.origen = sqlc.narg(origen))
  AND (sqlc.narg(vendedor_id)::bigint IS NULL OR p.vendedor_id = sqlc.narg(vendedor_id))
  AND (NOT @sin_vendedor::boolean OR p.vendedor_id IS NULL)
  AND (sqlc.narg(zona_id)::bigint IS NULL OR p.zona_id = sqlc.narg(zona_id))
  AND (NOT @con_coordenadas::boolean OR p.lat IS NOT NULL)
  AND (sqlc.narg(q)::text IS NULL
       OR p.numero ILIKE '%' || sqlc.narg(q) || '%'
       OR p.nombre ILIKE '%' || sqlc.narg(q) || '%'
       OR p.telefono ILIKE '%' || sqlc.narg(q) || '%'
       OR p.correo ILIKE '%' || sqlc.narg(q) || '%'
       OR p.direccion ILIKE '%' || sqlc.narg(q) || '%')
ORDER BY p.id DESC
LIMIT @limite;

-- name: InsertarEventoProspecto :exec
INSERT INTO comercial.prospecto_eventos (prospecto_id, tipo, de, a, motivo, datos, actor_tipo, actor_id)
VALUES (@prospecto_id, @tipo, sqlc.narg(de), sqlc.narg(a), sqlc.narg(motivo), @datos, @actor_tipo, sqlc.narg(actor_id));

-- name: EventosDeProspecto :many
SELECT * FROM comercial.prospecto_eventos WHERE prospecto_id = @prospecto_id ORDER BY id;

-- name: ProspectosConAltaPendiente :many
-- Ganados con el alta en Argos sin terminar (barrido del worker).
SELECT id FROM comercial.prospectos
WHERE alta_estado IN ('esperando_cliente', 'esperando_servicio') AND id > @despues_de
ORDER BY id
LIMIT @limite;

-- Actividades ---------------------------------------------------------------------------------

-- name: CrearActividad :one
INSERT INTO comercial.actividades (
    empresa_id, prospecto_id, tipo, descripcion, resultado, ocurrida_en, proxima_accion, proxima_fecha, autor_id, idempotencia
) VALUES (
    sqlc.narg(empresa_id), @prospecto_id, @tipo, @descripcion, sqlc.narg(resultado), @ocurrida_en,
    sqlc.narg(proxima_accion), sqlc.narg(proxima_fecha), sqlc.narg(autor_id), sqlc.narg(idempotencia)
)
RETURNING *;

-- name: ActividadPorIdempotencia :one
SELECT * FROM comercial.actividades WHERE idempotencia = @idempotencia;

-- name: ListarActividades :many
SELECT * FROM comercial.actividades
WHERE prospecto_id = @prospecto_id AND id > @despues_de
ORDER BY id
LIMIT @limite;

-- Factibilidad --------------------------------------------------------------------------------

-- name: UmbralesDeEmpresa :one
-- Los de la empresa o, si no tiene, los por defecto (empresa_id null).
SELECT * FROM comercial.umbrales_factibilidad
WHERE empresa_id IS NOT DISTINCT FROM sqlc.narg(empresa_id)::bigint OR empresa_id IS NULL
ORDER BY empresa_id NULLS LAST
LIMIT 1;

-- name: UmbralesPropiosParaActualizar :one
SELECT * FROM comercial.umbrales_factibilidad
WHERE empresa_id IS NOT DISTINCT FROM sqlc.narg(empresa_id)::bigint
FOR UPDATE;

-- name: CrearUmbrales :one
INSERT INTO comercial.umbrales_factibilidad (empresa_id, viable_m, obra_m)
VALUES (sqlc.narg(empresa_id), @viable_m, @obra_m)
RETURNING *;

-- name: GuardarUmbrales :one
UPDATE comercial.umbrales_factibilidad
SET viable_m = @viable_m, obra_m = @obra_m, version = version + 1, updated_at = now()
WHERE id = @id AND version = @version
RETURNING *;

-- name: RoutersConCoordenadas :many
-- Routers de la instantánea de Argos de la empresa con coordenadas (factibilidad por distancia).
SELECT id, nombre, zona_id, lat::double precision AS lat, lon::double precision AS lon, sincronizado_en
FROM integracion.argos_routers
WHERE empresa_id = @empresa_id AND lat IS NOT NULL AND lon IS NOT NULL
ORDER BY id;

-- name: ContarRoutersDeEmpresa :one
SELECT count(*) FROM integracion.argos_routers WHERE empresa_id = @empresa_id;

-- name: EstadoConectorArgos :one
-- 'activo', 'apagado' o '' (nunca se conectó: modo sin conector).
SELECT coalesce(
    (SELECT estado FROM integracion.conectores WHERE sistema = 'argosisp' ORDER BY (estado = 'activo') DESC, id DESC LIMIT 1),
    '')::text AS estado;

-- name: CrearFactibilidad :one
INSERT INTO comercial.factibilidades (
    empresa_id, prospecto_id, resultado, motor, motivo, lat, lon, router_id, router_nombre, caja,
    distancia_m, viable_m, obra_m, notas, evaluada_por
) VALUES (
    sqlc.narg(empresa_id), @prospecto_id, @resultado, @motor, sqlc.narg(motivo), sqlc.narg(lat), sqlc.narg(lon),
    sqlc.narg(router_id), sqlc.narg(router_nombre), sqlc.narg(caja), sqlc.narg(distancia_m), sqlc.narg(viable_m),
    sqlc.narg(obra_m), @notas, sqlc.narg(evaluada_por)
)
RETURNING *;

-- name: UltimaFactibilidad :one
SELECT * FROM comercial.factibilidades WHERE prospecto_id = @prospecto_id ORDER BY id DESC LIMIT 1;

-- name: FactibilidadesDeProspecto :many
SELECT * FROM comercial.factibilidades WHERE prospecto_id = @prospecto_id ORDER BY id DESC;

-- Cotizaciones --------------------------------------------------------------------------------

-- name: SiguienteRevision :one
SELECT (coalesce(max(revision), 0) + 1)::integer AS revision FROM comercial.cotizaciones WHERE prospecto_id = @prospecto_id;

-- name: CotizacionVivaParaActualizar :one
SELECT * FROM comercial.cotizaciones WHERE prospecto_id = @prospecto_id AND estado IN ('vigente', 'aceptada') FOR UPDATE;

-- name: CotizacionViva :one
SELECT * FROM comercial.cotizaciones WHERE prospecto_id = @prospecto_id AND estado IN ('vigente', 'aceptada');

-- name: CotizacionPorIdempotencia :one
SELECT * FROM comercial.cotizaciones WHERE idempotencia = @idempotencia;

-- name: CrearCotizacion :one
INSERT INTO comercial.cotizaciones (
    empresa_id, prospecto_id, revision, moneda, total_mensual, total_unico, vigente_hasta, plan_id, notas,
    cliente, pdf_adjunto_id, creado_por, idempotencia
) VALUES (
    sqlc.narg(empresa_id), @prospecto_id, @revision, @moneda, @total_mensual, @total_unico, @vigente_hasta,
    sqlc.narg(plan_id), @notas, @cliente, sqlc.narg(pdf_adjunto_id), sqlc.narg(creado_por), sqlc.narg(idempotencia)
)
RETURNING *;

-- name: CrearLineaCotizacion :one
INSERT INTO comercial.cotizacion_lineas (
    cotizacion_id, posicion, tipo, plan_id, producto_id, descripcion, cantidad, precio_unitario, recurrente, total
) VALUES (
    @cotizacion_id, @posicion, @tipo, sqlc.narg(plan_id), sqlc.narg(producto_id), @descripcion, @cantidad,
    @precio_unitario, @recurrente, @total
)
RETURNING *;

-- name: LineasDeCotizacion :many
SELECT * FROM comercial.cotizacion_lineas WHERE cotizacion_id = @cotizacion_id ORDER BY posicion;

-- name: CotizacionPorID :one
SELECT * FROM comercial.cotizaciones WHERE id = @id;

-- name: CotizacionPorIDParaActualizar :one
SELECT * FROM comercial.cotizaciones WHERE id = @id FOR UPDATE;

-- name: CotizacionesDeProspecto :many
SELECT * FROM comercial.cotizaciones WHERE prospecto_id = @prospecto_id ORDER BY revision DESC;

-- name: MarcarReemplazada :exec
UPDATE comercial.cotizaciones
SET estado = 'reemplazada', aceptada_en = NULL, version = version + 1, updated_at = now()
WHERE id = @id;

-- name: EnlazarReemplazo :exec
UPDATE comercial.cotizaciones SET reemplazada_por = @reemplazada_por WHERE id = @id;

-- name: AceptarCotizacion :one
UPDATE comercial.cotizaciones
SET estado = 'aceptada', aceptada_en = @ahora, aceptada_por = sqlc.narg(aceptada_por), version = version + 1, updated_at = now()
WHERE id = @id AND version = @version AND estado = 'vigente'
RETURNING *;

-- name: EstadoAdjunto :one
SELECT estado, sha256 FROM nucleo.adjuntos WHERE id = @id;

-- Contratos -----------------------------------------------------------------------------------

-- name: CrearContrato :one
INSERT INTO comercial.contratos (empresa_id, prospecto_id, cotizacion_id, plantilla, contenido, pdf_adjunto_id)
VALUES (sqlc.narg(empresa_id), @prospecto_id, @cotizacion_id, @plantilla, @contenido, sqlc.narg(pdf_adjunto_id))
RETURNING *;

-- name: ContratoPorID :one
SELECT * FROM comercial.contratos WHERE id = @id;

-- name: ContratoPorIDParaActualizar :one
SELECT * FROM comercial.contratos WHERE id = @id FOR UPDATE;

-- name: ContratoVivoDeProspecto :one
SELECT * FROM comercial.contratos WHERE prospecto_id = @prospecto_id AND estado IN ('borrador', 'firmado');

-- name: FirmarContrato :one
UPDATE comercial.contratos
SET estado = 'firmado', firmante_nombre = @firmante_nombre, firmante_documento = sqlc.narg(firmante_documento),
    firma_svg = @firma_svg, firma_lat = sqlc.narg(firma_lat), firma_lon = sqlc.narg(firma_lon),
    firmado_en = @firmado_en, firmado_por = sqlc.narg(firmado_por), pdf_firmado_id = @pdf_firmado_id,
    pdf_firmado_sha256 = @pdf_firmado_sha256, version = version + 1, updated_at = now()
WHERE id = @id AND version = @version AND estado = 'borrador'
RETURNING *;

-- name: AnularContrato :one
UPDATE comercial.contratos
SET estado = 'anulado', motivo_anulacion = @motivo, version = version + 1, updated_at = now()
WHERE id = @id
RETURNING *;

-- Lecturas de otros módulos (por SQL; comercial no importa sus paquetes de BD) ------------------

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

-- name: ResumenOrden :one
SELECT o.id, o.numero, o.estado FROM campo.ordenes o WHERE o.id = @id;

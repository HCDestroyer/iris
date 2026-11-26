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
    router_id           = sqlc.narg(router_id),
    alta_error          = sqlc.narg(alta_error),
    cliente_validado    = sqlc.narg(cliente_validado),
    ganado_por          = sqlc.narg(ganado_por),
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
WHERE alta_estado IN ('esperando_cliente', 'esperando_servicio', 'esperando_activacion') AND alta_error IS NULL AND id > @despues_de
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
-- empresa_id NULL = sin empresa sugerida en Argos: lo usan todas las empresas (multiempresa de Argos).
SELECT id, nombre, zona_id, lat::double precision AS lat, lon::double precision AS lon, sincronizado_en
FROM integracion.argos_routers
WHERE (empresa_id = @empresa_id::bigint OR empresa_id IS NULL) AND lat IS NOT NULL AND lon IS NOT NULL
ORDER BY id;

-- name: RouterDeEmpresa :one
-- El router está en la instantánea de Argos de la empresa (al elegirlo al ganar).
SELECT id, nombre FROM integracion.argos_routers WHERE id = @id AND (empresa_id = @empresa_id::bigint OR empresa_id IS NULL);

-- name: ContarRoutersDeEmpresa :one
SELECT count(*) FROM integracion.argos_routers WHERE empresa_id = @empresa_id::bigint OR empresa_id IS NULL;

-- name: EstadoConectorArgos :one
-- 'activo', 'apagado' o '' (nunca se conectó, o se eliminaron todos: modo sin conector).
SELECT coalesce(
    (SELECT estado FROM integracion.conectores WHERE sistema = 'argosisp' AND eliminado_en IS NULL ORDER BY (estado = 'activo') DESC, id DESC LIMIT 1),
    '')::text AS estado;

-- name: CrearFactibilidad :one
INSERT INTO comercial.factibilidades (
    empresa_id, prospecto_id, resultado, motor, motivo, lat, lon, router_id, router_nombre, caja,
    distancia_m, viable_m, obra_m, notas, evaluada_por, anula_id, motivo_anulacion
) VALUES (
    sqlc.narg(empresa_id), @prospecto_id, @resultado, @motor, sqlc.narg(motivo), sqlc.narg(lat), sqlc.narg(lon),
    sqlc.narg(router_id), sqlc.narg(router_nombre), sqlc.narg(caja), sqlc.narg(distancia_m), sqlc.narg(viable_m),
    sqlc.narg(obra_m), @notas, sqlc.narg(evaluada_por), sqlc.narg(anula_id), sqlc.narg(motivo_anulacion)
)
RETURNING *;

-- name: UltimaFactibilidad :one
SELECT * FROM comercial.factibilidades WHERE prospecto_id = @prospecto_id ORDER BY id DESC LIMIT 1;

-- name: UltimaAutomaticaNoViable :one
-- La última evaluación automática (no manual) no_viable sobre esas coordenadas.
SELECT * FROM comercial.factibilidades
WHERE prospecto_id = @prospecto_id AND motor <> 'manual' AND resultado = 'no_viable'
  AND lat IS NOT DISTINCT FROM sqlc.narg(lat)::double precision AND lon IS NOT DISTINCT FROM sqlc.narg(lon)::double precision
ORDER BY id DESC LIMIT 1;

-- name: FactibilidadesDeProspecto :many
SELECT * FROM comercial.factibilidades WHERE prospecto_id = @prospecto_id ORDER BY id DESC;

-- Cotizaciones --------------------------------------------------------------------------------

-- name: SiguienteRevision :one
SELECT (coalesce(max(revision), 0) + 1)::integer AS revision FROM comercial.cotizaciones WHERE prospecto_id = @prospecto_id;

-- name: CotizacionVivaParaActualizar :one
-- La vigente o aceptada. Con una revisión de la instalación por firmar (B20), la aceptada.
SELECT * FROM comercial.cotizaciones WHERE prospecto_id = @prospecto_id AND estado IN ('vigente', 'aceptada')
ORDER BY (estado = 'aceptada') DESC, id DESC LIMIT 1 FOR UPDATE;

-- name: CotizacionViva :one
-- La vigente o aceptada. Con una revisión de la instalación por firmar (B20), la aceptada.
SELECT * FROM comercial.cotizaciones WHERE prospecto_id = @prospecto_id AND estado IN ('vigente', 'aceptada')
ORDER BY (estado = 'aceptada') DESC, id DESC LIMIT 1;

-- name: RevisionPorFirmar :one
-- B20: la revisión de la instalación (cambio de plan o de titular hecho en Argos) que espera la
-- firma del contrato regenerado en la orden.
SELECT * FROM comercial.cotizaciones
WHERE prospecto_id = @prospecto_id AND estado = 'vigente' AND idempotencia LIKE 'iris:venta:%';

-- name: CotizacionAceptadaDe :one
SELECT * FROM comercial.cotizaciones WHERE prospecto_id = @prospecto_id AND estado = 'aceptada';

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
-- El borrador o firmado. Con un contrato regenerado en la instalación por firmar (B20), el firmado.
SELECT * FROM comercial.contratos WHERE prospecto_id = @prospecto_id AND estado IN ('borrador', 'firmado')
ORDER BY (estado = 'firmado') DESC, id DESC LIMIT 1;

-- name: ContratoPorFirmarEnOrden :one
-- B20: el contrato regenerado en la instalación, en borrador, que se firma en la orden.
SELECT * FROM comercial.contratos
WHERE prospecto_id = @prospecto_id AND estado = 'borrador' AND (contenido->>'ordenId') IS NOT NULL;

-- name: ContratoFirmadoDe :one
SELECT * FROM comercial.contratos WHERE prospecto_id = @prospecto_id AND estado = 'firmado';

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
JOIN nucleo.roles ra ON ra.id = ur.rol_id AND ra.activo JOIN nucleo.roles_permisos rp ON rp.rol_id = ur.rol_id
WHERE ur.usuario_id = @usuario_id AND rp.permiso = @permiso;

-- name: HayEmpresas :one
SELECT EXISTS (SELECT 1 FROM nucleo.empresas);

-- name: EmpresaRegistrada :one
SELECT EXISTS (SELECT 1 FROM nucleo.empresas WHERE id = @id);

-- name: ResumenOrden :one
SELECT o.id, o.numero, o.estado FROM campo.ordenes o WHERE o.id = @id;

-- --- Prospectos registrados desde Argos (contrato 0.10.0 §3.6, A44) -------------------------

-- name: CrearProspectoArgos :one
INSERT INTO comercial.prospectos (
    empresa_id, origen, origen_detalle, nombre, telefono, direccion, lat, lon, zona_id, plan_interes_id,
    vendedor_id, notas, modo, registrado_por_argos, cliente_existente_id, cliente_validado, idempotencia
) VALUES (
    @empresa_id, 'argos_panel', @origen_detalle, @nombre, @telefono, @direccion, sqlc.narg(lat), sqlc.narg(lon),
    sqlc.narg(zona_id), sqlc.narg(plan_interes_id), sqlc.narg(vendedor_id), @notas, @modo, @registrado_por_argos,
    sqlc.narg(cliente_existente_id), sqlc.narg(cliente_validado), sqlc.narg(idempotencia)
)
RETURNING *;

-- name: CrearTelefonoProspecto :one
INSERT INTO comercial.prospecto_telefonos (prospecto_id, empresa_id, posicion, numero, whatsapp, principal, nombre, relacion)
VALUES (@prospecto_id, sqlc.narg(empresa_id), @posicion, @numero, @whatsapp, @principal, sqlc.narg(nombre), sqlc.narg(relacion))
RETURNING *;

-- name: TelefonosDeProspecto :many
SELECT * FROM comercial.prospecto_telefonos WHERE prospecto_id = @prospecto_id ORDER BY posicion, id;

-- name: ProspectoAbiertoConNumeros :one
-- El prospecto abierto (ni ganado ni perdido) más antiguo de la empresa con cualquiera de los
-- números: en su tabla de teléfonos (E.164) o, para los de antes, en prospectos.telefono (formato
-- de Argos). FOR UPDATE: el pedido se suma como actividad.
SELECT p.* FROM comercial.prospectos p
WHERE p.empresa_id = @empresa_id AND p.estado NOT IN ('ganado', 'perdido')
    AND (EXISTS (SELECT 1 FROM comercial.prospecto_telefonos t WHERE t.prospecto_id = p.id AND t.numero = ANY(@numeros::text[]))
         OR p.telefono = ANY(@telefonos::text[]))
ORDER BY p.id
LIMIT 1
FOR UPDATE OF p;

-- name: NumerosRegistradosArgos :one
-- Números aceptados en el último minuto por el actor de Argos y por la empresa (§3.6: 30 y 300).
SELECT
    coalesce(sum(numeros) FILTER (WHERE actor = @actor), 0)::bigint AS por_actor,
    coalesce(sum(numeros) FILTER (WHERE empresa_id = @empresa_id), 0)::bigint AS por_empresa
FROM comercial.registros_argos
WHERE created_at > @desde AND (actor = @actor OR empresa_id = @empresa_id);

-- name: AnotarRegistroArgos :exec
INSERT INTO comercial.registros_argos (actor, empresa_id, numeros, created_at) VALUES (@actor, @empresa_id, @numeros, @ahora);

-- name: PurgarRegistrosArgos :exec
DELETE FROM comercial.registros_argos WHERE created_at < @antes;

-- name: VendedoresDeZona :many
-- Candidatos a vendedor de un prospecto de la zona: personas activas que ven prospectos con un
-- alcance de esa zona (no global: supervisión no recibe asignaciones automáticas) y pueden
-- trabajarlos (prospectos.contactar), de menos prospectos abiertos a más.
SELECT u.id
FROM nucleo.usuarios u
WHERE u.activo AND u.tipo = 'persona'
    AND EXISTS (SELECT 1 FROM nucleo.usuarios_roles ur JOIN nucleo.roles r ON r.id = ur.rol_id AND r.activo
        JOIN nucleo.roles_permisos rp ON rp.rol_id = ur.rol_id
        WHERE ur.usuario_id = u.id AND rp.permiso = 'prospectos.contactar' AND ur.zona_id = @zona_id)
ORDER BY (SELECT count(*) FROM comercial.prospectos p WHERE p.vendedor_id = u.id AND p.estado NOT IN ('ganado', 'perdido')), u.id
LIMIT 20;

-- name: ClienteEnEmpresaInstantanea :one
-- ¿La instantánea de Argos dice que el cliente es de la empresa (por su ficha o por un servicio)?
-- existe = Iris lo tiene en la instantánea.
SELECT
    EXISTS (SELECT 1 FROM integracion.argos_clientes c WHERE c.id = @cliente_id) AS existe,
    (EXISTS (SELECT 1 FROM integracion.argos_clientes c WHERE c.id = @cliente_id AND c.empresa_id = @empresa_id)
     OR EXISTS (SELECT 1 FROM integracion.argos_servicios s WHERE s.cliente_id = @cliente_id AND s.empresa_id = @empresa_id)) AS de_la_empresa;

-- name: BloquearRegistrosArgos :exec
-- Un registro desde Argos a la vez por empresa: el aviso de duplicado y los límites no se saltan
-- con dos peticiones simultáneas.
SELECT pg_advisory_xact_lock(hashtextextended('comercial.registro_argos:' || @empresa_id::bigint, 0));

-- name: ContratoArgosAcordado :one
-- Los contratos del conector Argos (el acordado sale de conector.ContratoAcordado: la menor entre la
-- que informó Argos y la que Iris le informó; null si no la informó o no hay).
SELECT contrato_remoto, contrato_informado FROM integracion.conectores
WHERE sistema = 'argosisp' AND eliminado_en IS NULL
ORDER BY (estado = 'activo') DESC, id DESC
LIMIT 1;

-- Solicitudes de servicio del portal de Argos (contrato 0.9.0 §11.7) -------------------------

-- name: CrearSolicitudPortal :one
INSERT INTO comercial.prospectos (
    empresa_id, origen, nombre, telefono, correo, direccion, lat, lon, zona_id, plan_interes_id, vendedor_id, notas,
    cliente_existente_id, cliente_validado, idempotencia, solicitud_tipo, solicitud_servicio_id, solicitud_comentario, estado_cliente,
    solicitud_plan_id, solicitud_plan_no_validado
) VALUES (
    @empresa_id, 'portal_argos', @nombre, sqlc.narg(telefono), sqlc.narg(correo), @direccion, sqlc.narg(lat), sqlc.narg(lon),
    sqlc.narg(zona_id), sqlc.narg(plan_interes_id), sqlc.narg(vendedor_id), @notas, @cliente_id, true, sqlc.narg(idempotencia),
    @solicitud_tipo, sqlc.narg(solicitud_servicio_id), sqlc.narg(solicitud_comentario), 'recibida',
    sqlc.narg(solicitud_plan_id), @solicitud_plan_no_validado
)
RETURNING *;

-- name: SolicitudAbierta :one
-- La solicitud abierta del cliente del mismo tipo y servicio (alta: del mismo tipo).
SELECT id FROM comercial.prospectos
WHERE empresa_id = @empresa_id::bigint AND cliente_existente_id = @cliente_id::bigint AND solicitud_tipo = @solicitud_tipo::text
  AND coalesce(solicitud_servicio_id, 0) = @servicio_id::bigint
  AND estado_cliente IN ('recibida', 'en_revision', 'cotizada', 'en_proceso')
ORDER BY id DESC
LIMIT 1;

-- name: SolicitudesUltimoDia :one
SELECT count(*)::bigint FROM comercial.prospectos
WHERE empresa_id = @empresa_id::bigint AND cliente_existente_id = @cliente_id::bigint AND solicitud_tipo IS NOT NULL
  AND created_at > now() - interval '1 day';

-- name: PrimeraSolicitudUltimoDia :one
-- Desde cuándo cuenta el tope diario (Retry-After).
SELECT min(created_at)::timestamptz FROM comercial.prospectos
WHERE empresa_id = @empresa_id::bigint AND cliente_existente_id = @cliente_id::bigint AND solicitud_tipo IS NOT NULL
  AND created_at > now() - interval '1 day';

-- name: SolicitudPorIdempotencia :one
SELECT * FROM comercial.prospectos WHERE idempotencia = @idempotencia AND solicitud_tipo IS NOT NULL;

-- name: ListarSolicitudesCliente :many
SELECT * FROM comercial.prospectos
WHERE empresa_id = @empresa_id::bigint AND cliente_existente_id = @cliente_id::bigint AND solicitud_tipo IS NOT NULL
  AND (sqlc.narg(antes_de)::bigint IS NULL OR id < sqlc.narg(antes_de))
  AND (cardinality(@estados::text[]) = 0 OR estado_cliente = ANY(@estados::text[]))
ORDER BY id DESC
LIMIT @limite;

-- name: FijarEstadoCliente :exec
UPDATE comercial.prospectos SET estado_cliente = @estado_cliente WHERE id = @id;

-- name: SolicitudDeOrden :one
SELECT * FROM comercial.prospectos WHERE orden_id = @orden_id AND solicitud_tipo IS NOT NULL;

-- name: FichaClienteSolicitud :one
-- Nombre, teléfono y correo de la ficha de Argos (instantánea) del cliente de la solicitud.
SELECT nombre, telefono, movil, correo, direccion, lat, lon, zona_id FROM integracion.argos_clientes WHERE id = @id;

-- name: ServicioDeClienteSolicitud :one
SELECT id, direccion, lat, lon, zona_id FROM integracion.argos_servicios
WHERE id = @id AND cliente_id = @cliente_id AND empresa_id = @empresa_id;

-- name: FijarTareaArgos :one
-- La tarea «Abrir en Argos» de una solicitud ganada (§11.7): pendiente al ganar, hecha al marcarla.
UPDATE comercial.prospectos
SET tarea_argos = @tarea_argos, tarea_argos_hecha_en = sqlc.narg(hecha_en), tarea_argos_hecha_por = sqlc.narg(hecha_por),
    version = version + 1, updated_at = now()
WHERE id = @id
RETURNING *;

-- name: RegistrarEvidenciaAceptacion :exec
UPDATE comercial.cotizaciones
SET aceptacion_canal = @canal, aceptacion_cliente_en = @cliente_en, aceptacion_registrada_por = sqlc.narg(registrada_por)
WHERE id = @id;

-- name: AjustesSolicitudes :one
SELECT * FROM comercial.ajustes_solicitudes WHERE empresa_id = @empresa_id;

-- name: GuardarAjustesSolicitudes :one
-- Control de versión: con version (la leída) que no coincide, ninguna fila. Sin fila previa, version 0.
INSERT INTO comercial.ajustes_solicitudes (empresa_id, cambio_plan_exige_contrato)
VALUES (@empresa_id, @cambio_plan_exige_contrato)
ON CONFLICT (empresa_id) DO UPDATE SET cambio_plan_exige_contrato = excluded.cambio_plan_exige_contrato,
    version = comercial.ajustes_solicitudes.version + 1, updated_at = now()
WHERE comercial.ajustes_solicitudes.version = @version::integer
RETURNING *;

-- name: ProspectosEsperandoActivacion :many
-- B17: los prospectos «ganado, alta pendiente» de una orden de instalación (la activación llegó a
-- Argos).
SELECT id FROM comercial.prospectos WHERE orden_id = @orden_id AND alta_estado = 'esperando_activacion' ORDER BY id;

-- name: ProspectosDeServicioArgos :many
-- B17: los prospectos ligados a un servicio de Argos (servicio.titular_cambiado los pasa al cliente
-- nuevo).
SELECT id FROM comercial.prospectos WHERE servicio_argos_id = @servicio_id AND alta_modo = 'argos' ORDER BY id;

-- name: TitularDeServicioArgos :one
-- B17 (MEDIA 2 de #280): el titular del servicio en la instantánea de Argos, con candado
-- compartido: el prospecto solo pasa si sigue siendo el del evento.
SELECT cliente_id FROM integracion.argos_servicios WHERE id = @id FOR SHARE;

-- name: ProspectoGanadoDeOrden :one
-- B20: el prospecto ganado de la orden de instalación (si la venta fue en Iris).
SELECT id FROM comercial.prospectos WHERE orden_id = @orden_id AND estado = 'ganado' ORDER BY id LIMIT 1;

-- name: NombreTitularDeOrden :one
-- B20: el nombre del titular confirmado en la orden (campo.orden_titular; nunca el documento).
SELECT nombre FROM campo.orden_titular WHERE orden_id = @orden_id;

-- name: NumeroDeOrden :one
SELECT numero FROM campo.ordenes WHERE id = @id;

-- name: OrdenParaFirmarContrato :one
-- B20: la orden donde se firma el contrato regenerado (el técnico asignado, en sitio).
SELECT id, numero, estado, tecnico_id, prospecto_id, empresa_id FROM campo.ordenes WHERE id = @id;

-- name: OrdenEstadoCompartido :one
-- B20 (verificación de #280): el estado de la orden de instalación con candado compartido. Lo toman
-- antes que el prospecto los casos de sistema que dependen de él (revisar la venta, anular o
-- regenerar el contrato por firmar), así no se cruzan con cancelar o reabrir la orden.
SELECT id, numero, estado FROM campo.ordenes WHERE id = @id FOR SHARE;

-- name: RenovarVigencia :one
-- B20: la revisión que espera la firma en una orden reabierta vuelve a tener vigencia, con un PDF
-- nuevo (el anterior decía la vigencia vencida).
UPDATE comercial.cotizaciones SET vigente_hasta = @vigente_hasta, pdf_adjunto_id = sqlc.narg(pdf_adjunto_id),
    version = version + 1, updated_at = now()
WHERE id = @id RETURNING *;

-- name: AnularBorrador :one
-- B20: anula el contrato solo si sigue en borrador (sin fila si ya se firmó o se anuló).
UPDATE comercial.contratos
SET estado = 'anulado', motivo_anulacion = @motivo, version = version + 1, updated_at = now()
WHERE id = @id AND estado = 'borrador'
RETURNING *;

-- name: AvisoSinFirmaPrevio :one
-- B20: ¿ya salió orden.contrato_sin_firma con ese motivo para esa clave de pendiente o esa
-- cotización? (los reintentos del relay no repiten el aviso ni la auditoría).
-- Solo de la misma empresa. La outbox no se purga: la deduplicación depende de eso.
SELECT EXISTS (
    SELECT 1 FROM integracion.outbox
    WHERE evento = 'orden.contrato_sin_firma' AND payload->>'motivo' = @motivo::text
      AND coalesce((payload->>'empresa_id')::bigint, 0) = coalesce(sqlc.narg('empresa')::bigint, 0)
      AND ((@clave::text <> '' AND payload->>'clave' = @clave::text)
           OR (@cotizacion_id::bigint > 0 AND (payload->>'cotizacion_id')::bigint = @cotizacion_id::bigint))
)::boolean;

-- name: NombreClienteArgos :one
-- B20: el nombre del cliente de Argos en la instantánea (el titular final de un cambio de titular
-- resuelto a mano con resuelto_cliente_id).
-- Solo un cliente de la empresa de la venta (o de cualquiera, sin empresa).
SELECT nombre FROM integracion.argos_clientes
WHERE id = @id AND (sqlc.narg('empresa')::bigint IS NULL OR empresa_id = sqlc.narg('empresa')::bigint
                    OR sqlc.narg('empresa')::bigint = ANY (empresas));

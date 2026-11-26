-- Instantáneas (§2.2) -------------------------------------------------------------------

-- name: GuardarCliente :exec
INSERT INTO integracion.argos_clientes (
    id, empresa_id, empresas, sin_servicios, codigo, nombre, telefono, direccion, lat, lon, zona_id, estado, sincronizado_en,
    tipo, tipo_documento_fiscal, nit_cui, movil, correo, zona_nombre, version_datos, referencia
) VALUES (
    @id, @empresa_id, @empresas::bigint[], @sin_servicios, @codigo, @nombre, @telefono, @direccion, @lat, @lon, @zona_id, @estado, @sincronizado_en,
    @tipo, @tipo_documento_fiscal, @nit_cui, @movil, @correo, @zona_nombre, @version_datos, @referencia
)
ON CONFLICT (id) DO UPDATE SET
    empresa_id = excluded.empresa_id, empresas = excluded.empresas, sin_servicios = excluded.sin_servicios,
    codigo = excluded.codigo, nombre = excluded.nombre,
    telefono = excluded.telefono, direccion = excluded.direccion, lat = excluded.lat, lon = excluded.lon,
    zona_id = excluded.zona_id, estado = excluded.estado, sincronizado_en = excluded.sincronizado_en,
    tipo = excluded.tipo, tipo_documento_fiscal = excluded.tipo_documento_fiscal, nit_cui = excluded.nit_cui,
    movil = excluded.movil, correo = excluded.correo, zona_nombre = excluded.zona_nombre,
    version_datos = excluded.version_datos, referencia = excluded.referencia, fallo_en = NULL, updated_at = now()
WHERE integracion.argos_clientes.sincronizado_en <= excluded.sincronizado_en;

-- name: ClientePorID :one
SELECT * FROM integracion.argos_clientes WHERE id = @id;

-- name: BorrarCliente :exec
DELETE FROM integracion.argos_clientes WHERE id = @id;

-- name: BuscarClientesLocal :many
-- Respaldo de la búsqueda cuando Argos no responde: solo lo que ya está en la instantánea.
SELECT * FROM integracion.argos_clientes
WHERE (nombre ILIKE '%' || @q::text || '%' OR codigo ILIKE '%' || @q::text || '%' OR telefono ILIKE '%' || @q::text || '%'
       OR movil ILIKE '%' || @q::text || '%' OR nit_cui ILIKE '%' || @q::text || '%')
  -- Visible en la empresa si es una de las de sus servicios, o si Argos dijo que no tiene servicios.
  AND (@empresa_id::bigint = 0 OR @empresa_id::bigint = ANY(empresas) OR sin_servicios
       OR (cardinality(empresas) = 0 AND empresa_id = @empresa_id::bigint))
ORDER BY nombre, id
LIMIT @limite;

-- name: GuardarServicio :exec
-- Un servicio con lápida no se guarda desde una lista: solo un GET por id con 200 lo resucita
-- (ResucitarEliminado borra antes la lápida). Llamar con BloquearArgos tomado.
INSERT INTO integracion.argos_servicios (
    id, empresa_id, cliente_id, codigo, plan_id, plan_nombre, router_id, router_nombre, zona_id, zona_nombre,
    ip, direccion, lat, lon, estado, sincronizado_en, user_ppp, tipo_conexion, referencia, contacto_nombre, contacto_telefono,
    precio_mensual, moneda, prefijo_enlace_v6, prefijo_delegado_v6, planta, nombre
)
SELECT @id, @empresa_id, @cliente_id, @codigo, @plan_id, @plan_nombre, @router_id, @router_nombre, @zona_id, @zona_nombre,
    @ip, @direccion, @lat, @lon, @estado, @sincronizado_en, @user_ppp, @tipo_conexion, @referencia, @contacto_nombre, @contacto_telefono,
    CAST(sqlc.narg('precio_mensual')::text AS numeric), sqlc.narg('moneda')::text, @prefijo_enlace_v6, @prefijo_delegado_v6, sqlc.narg('planta')::jsonb,
    @nombre
WHERE NOT EXISTS (SELECT 1 FROM integracion.argos_eliminados e WHERE e.tipo = 'servicio' AND e.id = @id)
ON CONFLICT (id) DO UPDATE SET
    empresa_id = excluded.empresa_id, cliente_id = excluded.cliente_id, codigo = excluded.codigo, nombre = excluded.nombre,
    plan_id = excluded.plan_id, plan_nombre = excluded.plan_nombre, router_id = excluded.router_id,
    router_nombre = excluded.router_nombre, zona_id = excluded.zona_id, zona_nombre = excluded.zona_nombre,
    ip = excluded.ip, direccion = excluded.direccion, lat = excluded.lat, lon = excluded.lon,
    estado = excluded.estado, sincronizado_en = excluded.sincronizado_en, user_ppp = excluded.user_ppp,
    tipo_conexion = excluded.tipo_conexion, referencia = excluded.referencia, contacto_nombre = excluded.contacto_nombre,
    contacto_telefono = excluded.contacto_telefono, precio_mensual = excluded.precio_mensual, moneda = excluded.moneda,
    prefijo_enlace_v6 = excluded.prefijo_enlace_v6, prefijo_delegado_v6 = excluded.prefijo_delegado_v6,
    planta = excluded.planta, updated_at = now()
WHERE integracion.argos_servicios.sincronizado_en <= excluded.sincronizado_en;

-- name: ServicioPorID :one
SELECT * FROM integracion.argos_servicios WHERE id = @id;

-- name: ServiciosPorIDs :many
SELECT * FROM integracion.argos_servicios WHERE id = ANY(@ids::bigint[]);

-- name: ServiciosDeCliente :many
SELECT * FROM integracion.argos_servicios WHERE cliente_id = @cliente_id ORDER BY id;

-- name: ServicioPorIP :one
-- La misma IP privada puede existir en dos empresas: con @empresa_id (≠ 0) solo la de esa empresa.
SELECT * FROM integracion.argos_servicios
-- Un servicio sin empresa (0) no se atribuye a nadie.
WHERE ip = @ip AND empresa_id <> 0 AND estado <> 'retirado' AND (@empresa_id::bigint = 0 OR empresa_id = @empresa_id::bigint)
ORDER BY sincronizado_en DESC LIMIT 1;

-- name: BorrarServicio :exec
DELETE FROM integracion.argos_servicios WHERE id = @id;

-- name: BorrarServiciosAusentes :exec
-- Servicios que Argos ya no devuelve para el cliente (y que no se leyeron después).
DELETE FROM integracion.argos_servicios
WHERE cliente_id = @cliente_id AND NOT (id = ANY(@vigentes::bigint[])) AND sincronizado_en < @sincronizado_en;

-- name: GuardarZona :exec
INSERT INTO integracion.argos_zonas (id, empresa_id, nombre, sincronizado_en)
VALUES (@id, @empresa_id, @nombre, @sincronizado_en)
ON CONFLICT (id) DO UPDATE SET
    empresa_id = excluded.empresa_id, nombre = excluded.nombre, sincronizado_en = excluded.sincronizado_en,
    updated_at = now()
WHERE integracion.argos_zonas.sincronizado_en <= excluded.sincronizado_en;

-- name: BorrarZonasAnteriores :execrows
DELETE FROM integracion.argos_zonas WHERE sincronizado_en < @antes;

-- name: GuardarRouter :exec
-- Como GuardarServicio: un router con lápida no vuelve desde la carga de catálogos (solo con un
-- GET por id con 200). Llamar con BloquearArgos tomado.
INSERT INTO integracion.argos_routers (id, empresa_id, nombre, zona_id, lat, lon, sincronizado_en)
SELECT @id, @empresa_id, @nombre, @zona_id, @lat, @lon, @sincronizado_en
WHERE NOT EXISTS (SELECT 1 FROM integracion.argos_eliminados e WHERE e.tipo = 'router' AND e.id = @id)
ON CONFLICT (id) DO UPDATE SET
    empresa_id = excluded.empresa_id, nombre = excluded.nombre, zona_id = excluded.zona_id,
    lat = excluded.lat, lon = excluded.lon, sincronizado_en = excluded.sincronizado_en, updated_at = now()
WHERE integracion.argos_routers.sincronizado_en <= excluded.sincronizado_en;

-- name: BorrarRoutersAnteriores :execrows
DELETE FROM integracion.argos_routers WHERE sincronizado_en < @antes;

-- name: RouterPorID :one
SELECT * FROM integracion.argos_routers WHERE id = @id;

-- name: BorrarRouter :exec
DELETE FROM integracion.argos_routers WHERE id = @id;

-- Eliminados en Argos (servicio.eliminado, router.eliminado; contrato 0.3.1) ---------------

-- name: BloquearArgos :exec
-- Candado de transacción por (tipo, id): serializa la eliminación con el guardado de la
-- instantánea del mismo servicio o router (sin él, una lectura en curso la recreaba).
SELECT pg_advisory_xact_lock(hashtextextended(@tipo::text || ':' || (@id::bigint)::text, 0));

-- name: RegistrarEliminado :one
-- Escribe la lápida o, si ya había una, la pone al día: confirmado_en queda en la confirmación
-- más reciente (así un GET 200 anterior a esta no resucita) y una copia de la instantánea
-- reemplaza a una armada con el payload. insertado = true solo la primera vez (el evento interno
-- sale una sola vez).
INSERT INTO integracion.argos_eliminados AS e (
    tipo, id, empresa_id, cliente_id, codigo, nombre, de_instantanea, conector_id, entrante_id, eliminado_en, confirmado_en
) VALUES (
    @tipo, @id, @empresa_id, @cliente_id, @codigo, @nombre, @de_instantanea, @conector_id, @entrante_id, @eliminado_en, @confirmado_en
)
ON CONFLICT (tipo, id) DO UPDATE SET
    confirmado_en = GREATEST(e.confirmado_en, excluded.confirmado_en),
    entrante_id = excluded.entrante_id,
    conector_id = excluded.conector_id,
    empresa_id = CASE WHEN excluded.de_instantanea THEN excluded.empresa_id ELSE e.empresa_id END,
    cliente_id = CASE WHEN excluded.de_instantanea THEN excluded.cliente_id ELSE e.cliente_id END,
    codigo = CASE WHEN excluded.de_instantanea THEN excluded.codigo ELSE e.codigo END,
    nombre = CASE WHEN excluded.de_instantanea THEN excluded.nombre ELSE e.nombre END,
    de_instantanea = e.de_instantanea OR excluded.de_instantanea
RETURNING (xmax = 0) AS insertado;

-- name: ResucitarEliminado :one
-- Un GET por id pedido después de confirmar la eliminación (@leido_en: clock_timestamp() de
-- Postgres antes de la llamada) respondió 200: la lápida estaba mal (o Argos lo restauró) y se
-- borra.
DELETE FROM integracion.argos_eliminados
WHERE tipo = @tipo AND id = @id AND confirmado_en < @leido_en
RETURNING *;

-- name: Eliminado :one
SELECT * FROM integracion.argos_eliminados WHERE tipo = @tipo AND id = @id;

-- name: EmpresasDeEliminadosDeCliente :many
-- Empresas de los servicios eliminados del cliente: pista para no darlo por «sin servicios»
-- (visible en todas las empresas) cuando su único servicio se eliminó.
SELECT DISTINCT empresa_id::bigint AS empresa_id FROM integracion.argos_eliminados
WHERE tipo = 'servicio' AND cliente_id = @cliente_id AND empresa_id IS NOT NULL AND empresa_id > 0
  -- Solo lápidas copiadas de la instantánea: empresa y cliente de un payload no son pista.
  AND de_instantanea
ORDER BY 1;

-- name: EliminadosEntre :many
-- Ids con lápida entre los que trae una lista de Argos (se confirman con un GET por id).
SELECT id FROM integracion.argos_eliminados WHERE tipo = @tipo AND id = ANY(@ids::bigint[]) ORDER BY id;

-- name: RelojBD :one
-- El reloj único para comparar lecturas de Argos con lápidas (el de Postgres, no el del proceso).
SELECT clock_timestamp()::timestamptz AS ahora;

-- name: ServiciosEliminadosDeCliente :many
-- Con @empresa_id (≠ 0), solo los de esa empresa (un servicio sin empresa no se atribuye a nadie).
SELECT * FROM integracion.argos_eliminados
WHERE tipo = 'servicio' AND cliente_id = @cliente_id
  AND (@empresa_id::bigint = 0 OR empresa_id = @empresa_id::bigint)
ORDER BY id;

-- name: ServiciosEliminados :many
-- Servicios eliminados en Argos entre @ids de la empresa (para marcar órdenes y tickets).
SELECT * FROM integracion.argos_eliminados
WHERE tipo = 'servicio' AND id = ANY(@ids::bigint[]) AND empresa_id = @empresa_id::bigint
ORDER BY id;

-- name: ServiciosEliminadosTodasLasEmpresas :many
-- Uso interno (sistema): sin filtro de empresa.
SELECT * FROM integracion.argos_eliminados
WHERE tipo = 'servicio' AND id = ANY(@ids::bigint[])
ORDER BY id;

-- name: PistaEmpresaAlta :one
-- Cliente recién creado desde un prospecto ganado (alta en Argos), todavía sin servicio: la
-- empresa del prospecto es la pista local hasta que exista el servicio.
SELECT empresa_id::bigint AS empresa_id FROM integracion.pendientes
WHERE operacion = 'crear_cliente' AND estado = 'hecho' AND empresa_id IS NOT NULL
  -- La respuesta guardada conserva la envoltura de Argos ({"data":{"id":N}}); el id puede venir
  -- como número o como texto.
  AND coalesce(respuesta->'data'->>'id', respuesta->>'id') = (@cliente_id::bigint)::text
ORDER BY id DESC LIMIT 1;

-- Nombres de catálogo -------------------------------------------------------------------------

-- name: NombreZona :one
SELECT nombre FROM integracion.argos_zonas WHERE id = @id;

-- Copias de tickets y órdenes (datos coherentes) ------------------------------------------------

-- name: GuardarCopia :exec
-- Solo inserción: la copia es la de cuando se creó el ticket o la orden.
INSERT INTO integracion.argos_copias (entidad, entidad_id, empresa_id, cliente_id, servicio_id, datos, sincronizado_en)
VALUES (@entidad, @entidad_id, @empresa_id, @cliente_id, @servicio_id, @datos, @sincronizado_en)
ON CONFLICT (entidad, entidad_id) DO NOTHING;

-- name: CopiaDe :one
SELECT * FROM integracion.argos_copias WHERE entidad = @entidad AND entidad_id = @entidad_id;

-- name: AnonimizarCopiasDeCliente :exec
-- Argos eliminó al cliente: sus copias quedan solo con los ids (sin nombre, dirección, teléfono
-- ni IP).
UPDATE integracion.argos_copias
SET datos = jsonb_build_object('cliente', jsonb_build_object('id', cliente_id), 'anonimizada', true),
    anonimizada_en = now()
WHERE cliente_id = @cliente_id AND anonimizada_en IS NULL;

-- name: MarcarFalloCliente :exec
-- Argos no respondió al leer el cliente: durante EsperaTrasFallo no se vuelve a pedir.
UPDATE integracion.argos_clientes SET fallo_en = @fallo_en WHERE id = @id;

-- name: ClienteConfirmadoDesde :one
-- ¿Hubo noticia del cliente desde @desde? Un alta propia hecha en Argos (pendiente crear_cliente
-- con su id en la respuesta) o un webhook cliente.creado / cliente.actualizado procesado. Con
-- esto la caché negativa en memoria (un 404 reciente) se descarta en cualquier proceso, aunque el
-- webhook o el pendiente los haya procesado el worker.
SELECT EXISTS (
    SELECT 1 FROM integracion.pendientes
    WHERE operacion = 'crear_cliente' AND estado = 'hecho' AND hecho_en >= @desde
      AND coalesce(respuesta->'data'->>'id', respuesta->>'id') = (@cliente_id::bigint)::text
) OR EXISTS (
    SELECT 1 FROM integracion.entrantes
    WHERE evento IN ('cliente.creado', 'cliente.actualizado') AND procesado_en >= @desde
      AND coalesce(payload->>'cliente_id', payload->'cliente'->>'id', payload->>'id') = (@cliente_id::bigint)::text
) AS confirmado;

-- Catálogos (catalogo.cambiado, contrato 0.5.0) ---------------------------------------------

-- name: CambiarZonaArgos :execrows
-- Nombre y activo de una zona de la instantánea (null = no cambia).
UPDATE integracion.argos_zonas SET nombre = coalesce(sqlc.narg(nombre)::text, nombre), activo = coalesce(sqlc.narg(activo)::boolean, activo),
    updated_at = now()
WHERE id = @id;

-- name: CambiarZonaCopiada :execrows
-- El nombre de la zona copiado en los servicios y clientes de la instantánea.
WITH s AS (
    UPDATE integracion.argos_servicios SET zona_nombre = @nombre::text, updated_at = now()
    WHERE zona_id = @zona_id::bigint AND zona_nombre <> @nombre::text RETURNING 1
), c AS (
    UPDATE integracion.argos_clientes SET zona_nombre = @nombre::text, updated_at = now()
    WHERE zona_id = @zona_id::bigint AND zona_nombre <> @nombre::text RETURNING 1
)
SELECT (SELECT count(*) FROM s) + (SELECT count(*) FROM c);

-- name: ZonaAgendaArgos :one
-- La zona espejo de agenda (origen argos, mismo id), bloqueada para cambiarla.
SELECT id, empresa_id, nombre, activa FROM agenda.zonas WHERE id = @id AND origen = 'argos' FOR UPDATE;

-- name: CambiarZonaAgenda :one
-- La zona espejo de agenda: nombre y activa como en Argos (sin fila = nada cambió).
UPDATE agenda.zonas
SET nombre = coalesce(nullif(left(btrim(coalesce(sqlc.narg(nombre)::text, '')), 120), ''), nombre),
    activa = coalesce(sqlc.narg(activo)::boolean, activa), version = version + 1, updated_at = now()
WHERE id = @id AND origen = 'argos'
  AND (nombre, activa) IS DISTINCT FROM (coalesce(nullif(left(btrim(coalesce(sqlc.narg(nombre)::text, '')), 120), ''), nombre),
                                         coalesce(sqlc.narg(activo)::boolean, activa))
RETURNING id, empresa_id, nombre, activa;

-- name: CambiarRouterArgos :execrows
UPDATE integracion.argos_routers SET nombre = coalesce(sqlc.narg(nombre)::text, nombre), activo = coalesce(sqlc.narg(activo)::boolean, activo),
    lat = CASE WHEN @con_coordenadas::boolean THEN sqlc.narg(lat)::double precision ELSE lat END,
    lon = CASE WHEN @con_coordenadas::boolean THEN sqlc.narg(lon)::double precision ELSE lon END, updated_at = now()
WHERE id = @id;

-- name: CambiarRouterCopiado :execrows
UPDATE integracion.argos_servicios SET router_nombre = @nombre::text, updated_at = now()
WHERE router_id = @router_id::bigint AND router_nombre <> @nombre::text;

-- name: CambiarPlanArgos :execrows
-- Argos no guarda planes en la instantánea: solo el nombre copiado en los servicios.
UPDATE integracion.argos_servicios SET plan_nombre = @nombre, updated_at = now()
WHERE plan_id = @id AND plan_nombre IS DISTINCT FROM @nombre;

-- Órdenes de traslado (inventario.traslado_*, contrato 0.5.0) ------------------------------

-- name: GuardarTraslado :one
-- Un estado final (recibido, cancelado) no se toca más (ni vuelve a enviado): solo lo actualiza el mismo
-- estado repetido. Sin fila = nada cambió (un webhook atrasado o de otro estado final).
INSERT INTO integracion.argos_traslados (
    id, empresa_id, bodega_destino_id, tecnico_id_externo, tecnico_correo, estado, lineas, enviado_en,
    recibido_en, recibido_por, metodo_recepcion, origen_recepcion, cancelado_en, motivo, entrante_id, cuadrilla_id
) VALUES (
    @id, sqlc.narg(empresa_id), sqlc.narg(bodega_destino_id), sqlc.narg(tecnico_id_externo), sqlc.narg(tecnico_correo), @estado, @lineas,
    sqlc.narg(enviado_en), sqlc.narg(recibido_en), sqlc.narg(recibido_por), sqlc.narg(metodo_recepcion), sqlc.narg(origen_recepcion),
    sqlc.narg(cancelado_en), sqlc.narg(motivo), sqlc.narg(entrante_id),
    -- La cuadrilla (0.11.0 §3.9): la del webhook o, si vino de GET /inventario/traslados/:id (que solo
    -- trae la bodega), la que Iris conoce de esa bodega.
    coalesce(sqlc.narg(cuadrilla_id)::bigint,
             (SELECT bc.cuadrilla_id FROM integracion.argos_bodegas_cuadrilla bc WHERE bc.bodega_id = sqlc.narg(bodega_destino_id)::bigint))
)
ON CONFLICT (id) DO UPDATE SET
    empresa_id = coalesce(excluded.empresa_id, integracion.argos_traslados.empresa_id),
    cuadrilla_id = coalesce(excluded.cuadrilla_id, integracion.argos_traslados.cuadrilla_id),
    bodega_destino_id = coalesce(excluded.bodega_destino_id, integracion.argos_traslados.bodega_destino_id),
    tecnico_id_externo = coalesce(excluded.tecnico_id_externo, integracion.argos_traslados.tecnico_id_externo),
    tecnico_correo = coalesce(excluded.tecnico_correo, integracion.argos_traslados.tecnico_correo),
    estado = excluded.estado,
    lineas = CASE WHEN jsonb_array_length(excluded.lineas) > 0 THEN excluded.lineas ELSE integracion.argos_traslados.lineas END,
    enviado_en = coalesce(excluded.enviado_en, integracion.argos_traslados.enviado_en),
    recibido_en = coalesce(excluded.recibido_en, integracion.argos_traslados.recibido_en),
    recibido_por = coalesce(excluded.recibido_por, integracion.argos_traslados.recibido_por),
    metodo_recepcion = coalesce(excluded.metodo_recepcion, integracion.argos_traslados.metodo_recepcion),
    origen_recepcion = coalesce(excluded.origen_recepcion, integracion.argos_traslados.origen_recepcion),
    cancelado_en = coalesce(excluded.cancelado_en, integracion.argos_traslados.cancelado_en),
    motivo = coalesce(excluded.motivo, integracion.argos_traslados.motivo),
    entrante_id = excluded.entrante_id, updated_at = now()
WHERE integracion.argos_traslados.estado = 'enviado' OR integracion.argos_traslados.estado = excluded.estado
RETURNING *;

-- name: GuardarBodegaCuadrilla :exec
-- Una bodega `cuadrilla` de Argos (mi-bodega o un webhook de traslado, 0.11.0 §3.9).
INSERT INTO integracion.argos_bodegas_cuadrilla (bodega_id, empresa_id, cuadrilla_id, nombre, visto_en)
VALUES (@bodega_id, @empresa_id, @cuadrilla_id, sqlc.narg(nombre), @visto_en)
ON CONFLICT (bodega_id) DO UPDATE SET
    empresa_id = excluded.empresa_id, cuadrilla_id = excluded.cuadrilla_id,
    nombre = coalesce(excluded.nombre, integracion.argos_bodegas_cuadrilla.nombre),
    visto_en = excluded.visto_en, updated_at = now();

-- name: GuardarBodegaTecnico :exec
-- Una bodega personal del técnico (mi-bodega, M-268-1).
INSERT INTO integracion.argos_bodegas_tecnico (bodega_id, empresa_id, usuario_id, visto_en)
VALUES (@bodega_id, @empresa_id, @usuario_id, @visto_en)
ON CONFLICT (bodega_id) DO UPDATE SET
    empresa_id = excluded.empresa_id, usuario_id = excluded.usuario_id, visto_en = excluded.visto_en, updated_at = now();

-- name: CuadrillaDelUsuario :one
-- La cuadrilla actual del técnico (ficha activa) y su jefe.
SELECT t.cuadrilla_id::bigint AS cuadrilla_id, c.jefe_usuario_id
FROM agenda.tecnicos t JOIN agenda.cuadrillas c ON c.id = t.cuadrilla_id
WHERE t.usuario_id = @usuario_id AND t.activo AND c.activa;

-- name: TrasladoPorID :one
SELECT * FROM integracion.argos_traslados WHERE id = @id;

-- name: TrasladosDeTecnico :many
-- Los del técnico (por su vínculo: id_externo o, si Argos solo mandó el correo, el correo) y los de
-- su cuadrilla actual (0.11.0 §3.9; los recibe el jefe) en la empresa (0 = todas), más recientes
-- primero.
SELECT * FROM integracion.argos_traslados
WHERE ((tecnico_id_externo = @id_externo OR (tecnico_id_externo IS NULL AND @correo::text <> '' AND lower(tecnico_correo) = lower(@correo::text)))
       OR (sqlc.narg(cuadrilla_id)::bigint IS NOT NULL AND cuadrilla_id = sqlc.narg(cuadrilla_id)::bigint))
  AND (@empresa_id::bigint = 0 OR empresa_id = @empresa_id::bigint)
  AND (sqlc.narg(estado)::text IS NULL OR estado = sqlc.narg(estado)::text)
  AND (@antes_de::bigint = 0 OR id < @antes_de::bigint)
ORDER BY id DESC
LIMIT @limite;

-- name: UsuarioDeVinculoArgos :one
-- El técnico de Iris vinculado a un usuario de Argos (id_externo; si no, por el correo del vínculo).
SELECT usuario_id FROM agenda.tecnicos
WHERE argos_id_externo = @id_externo::text OR (@id_externo::text = '' AND @correo::text <> '' AND lower(argos_correo) = lower(@correo::text))
ORDER BY (argos_id_externo = @id_externo::text) DESC
LIMIT 1;

-- name: TecnicosVinculadosArgos :many
-- Fichas de agenda ya vinculadas a esos usuarios de Argos (selector del vínculo). Solo las de
-- usuarios que operan en la empresa (@todas: quien opera en todas las ve todas).
SELECT t.id, t.usuario_id, t.argos_id_externo::text AS id_externo FROM agenda.tecnicos t
WHERE t.argos_id_externo = ANY(@ids::text[])
  AND (@todas::boolean OR EXISTS (
      SELECT 1 FROM nucleo.usuarios_empresas ue WHERE ue.usuario_id = t.usuario_id AND ue.empresa_id = @empresa_id::bigint));

-- name: VinculoDeUsuario :one
-- El vínculo con Argos de la ficha de agenda (activa) del usuario: id_externo y correo de Argos.
SELECT coalesce(argos_id_externo, '')::text AS id_externo, coalesce(argos_correo, '')::text AS correo
FROM agenda.tecnicos WHERE usuario_id = @usuario_id AND activo;

-- name: IDsExternosVinculados :many
-- Usuarios de Argos ya vinculados a una ficha de agenda (en cualquier empresa: el vínculo es único).
-- Solo los ids, para el selector «sin vincular».
SELECT argos_id_externo::text AS id_externo FROM agenda.tecnicos WHERE argos_id_externo IS NOT NULL;

-- name: ServiciosDeOrdenesVisibles :many
-- «Sus órdenes» para servicios.ver_red / ver_ubicacion (contrato 0.8.0 §2.4; visibilidad.go): de la
-- empresa activa (0 = sin empresas registradas), abiertas o terminadas (cerrada, fallida) hace
-- menos de 24 h, nunca canceladas; asignadas al usuario (solo con ordenes.ver_propias) o en una zona o
-- cuadrilla de su alcance de ordenes.ver.
SELECT DISTINCT servicio_id::bigint AS servicio_id FROM campo.ordenes
WHERE servicio_id IS NOT NULL
    AND (@empresa_id::bigint = 0 OR empresa_id = @empresa_id::bigint)
    AND (estado IN ('pendiente', 'programada', 'en_camino', 'en_sitio', 'pausada')
        OR (estado IN ('cerrada', 'fallida') AND finalizada_en > now() - interval '24 hours'))
    AND ((@propias::boolean AND tecnico_id = @usuario_id::bigint) OR zona_id = ANY(@zonas::bigint[]) OR cuadrilla_id = ANY(@cuadrillas::bigint[]));

-- name: ServiciosDePlan :many
-- catalogo.cambiado de un plan con precio (§2.4): los servicios de la instantánea con ese plan.
SELECT id, cliente_id FROM integracion.argos_servicios WHERE plan_id = @plan_id::bigint ORDER BY id;

-- name: CambiarPrecioServicio :execrows
-- Precio releído de Argos por catalogo.cambiado de un plan (§2.4). Solo si el servicio sigue en ese
-- plan; null/null = Argos ya no lo informa.
UPDATE integracion.argos_servicios
SET precio_mensual = CAST(sqlc.narg('precio_mensual')::text AS numeric), moneda = sqlc.narg('moneda')::text, updated_at = now()
WHERE id = @id AND plan_id = @plan_id::bigint
    AND (precio_mensual IS DISTINCT FROM CAST(sqlc.narg('precio_mensual')::text AS numeric) OR moneda IS DISTINCT FROM sqlc.narg('moneda')::text);

-- name: AvisoPrecioPlanDesde :one
-- ¿Llegó un catalogo.cambiado de este plan con precio después de @desde? (el job de precios vuelve a
-- leer: un aviso que llegó mientras corría no encoló otro job, por único).
SELECT EXISTS (
    SELECT 1 FROM integracion.entrantes
    WHERE conector_id = @conector_id::bigint AND evento = 'catalogo.cambiado' AND recibido_en > @desde::timestamptz
        AND payload->>'catalogo' = 'plan' AND (payload->>'id') = (@plan_id::bigint)::text
        AND payload->'campos' ? 'precio'
)::boolean;

-- Cuentas bancarias (contrato 0.8.0 §7.8) ---------------------------------------------------------

-- name: CuentaBancariaParaActualizar :one
-- La fila de la instantánea con su candado (para comparar antes de guardar y emitir el cambio).
SELECT * FROM integracion.argos_cuentas_bancarias WHERE id = @id FOR UPDATE;

-- name: CuentaBancariaInstantanea :one
SELECT * FROM integracion.argos_cuentas_bancarias WHERE id = @id;

-- name: CuentasBancariasDeEmpresa :many
-- Orden de Argos: banco, alias, id.
SELECT * FROM integracion.argos_cuentas_bancarias WHERE empresa_id = @empresa_id
ORDER BY banco_nombre, alias, id;

-- name: CuentasBancariasTodas :many
SELECT * FROM integracion.argos_cuentas_bancarias ORDER BY empresa_id, banco_nombre, alias, id;

-- name: GuardarCuentaBancaria :execrows
-- Gana la lectura más reciente de Argos (una relectura vieja no pisa a una nueva).
INSERT INTO integracion.argos_cuentas_bancarias (id, empresa_id, banco_id, banco_nombre, numero, alias, titular, tipo, moneda, activa, sincronizado_en)
VALUES (@id, @empresa_id, @banco_id, @banco_nombre, @numero, @alias, @titular, @tipo, @moneda, @activa, @sincronizado_en)
ON CONFLICT (id) DO UPDATE SET
    empresa_id = excluded.empresa_id, banco_id = excluded.banco_id, banco_nombre = excluded.banco_nombre, numero = excluded.numero,
    alias = excluded.alias, titular = excluded.titular, tipo = excluded.tipo, moneda = excluded.moneda, activa = excluded.activa,
    sincronizado_en = excluded.sincronizado_en, updated_at = now()
WHERE integracion.argos_cuentas_bancarias.sincronizado_en <= excluded.sincronizado_en;

-- name: BorrarCuentaBancaria :execrows
-- Una cuenta que Argos ya no le muestra a Iris (fuera del alcance del token). Argos nunca borra cuentas.
DELETE FROM integracion.argos_cuentas_bancarias WHERE id = @id AND sincronizado_en <= @leido_en;

-- name: CuentasBancariasNoVistas :many
-- Las de la instantánea que una lista completa (leída desde @desde) no trajo.
SELECT * FROM integracion.argos_cuentas_bancarias WHERE sincronizado_en < @desde ORDER BY id;

-- name: MarcarLecturaCuentas :exec
INSERT INTO integracion.argos_cuentas_lecturas (empresa_id, leido_en) VALUES (@empresa_id, @leido_en)
ON CONFLICT (empresa_id) DO UPDATE SET leido_en = greatest(integracion.argos_cuentas_lecturas.leido_en, excluded.leido_en);

-- name: LecturaCuentasDe :one
-- La última lista completa que cubre a la empresa (la suya o la de todas, empresa 0).
-- Sin filas: nunca se leyó.
SELECT leido_en FROM integracion.argos_cuentas_lecturas WHERE empresa_id IN (0, @empresa_id::bigint)
ORDER BY leido_en DESC LIMIT 1;

-- Primera sincronización (contrato 0.8.0 §2.5, B7) ----------------------------------------------

-- name: CrearSincronizacion :one
INSERT INTO integracion.argos_sincronizaciones (id, conector_id, contrato_remoto, excluir_retirados, informar, iniciada_en, actualizada_en, iniciada_por, origen)
VALUES (@id, @conector_id, sqlc.narg('contrato_remoto')::text, @excluir_retirados, @informar, @ahora, @ahora, sqlc.narg('iniciada_por')::bigint, @origen)
RETURNING *;

-- name: CrearEtapa :exec
INSERT INTO integracion.argos_sincronizacion_etapas (sincronizacion_id, recurso, orden) VALUES (@sincronizacion_id, @recurso, @orden);

-- name: SincronizacionEnCurso :one
SELECT * FROM integracion.argos_sincronizaciones WHERE conector_id = @conector_id AND estado = 'en_curso';

-- name: UltimaSincronizacion :one
SELECT * FROM integracion.argos_sincronizaciones WHERE conector_id = @conector_id ORDER BY id DESC LIMIT 1;

-- name: SincronizacionPorID :one
SELECT * FROM integracion.argos_sincronizaciones WHERE id = @id;

-- name: SincronizacionParaActualizar :one
SELECT * FROM integracion.argos_sincronizaciones WHERE id = @id FOR UPDATE;

-- name: EtapasDeSincronizacion :many
SELECT * FROM integracion.argos_sincronizacion_etapas WHERE sincronizacion_id = @sincronizacion_id ORDER BY orden;

-- name: ActualizarEtapa :one
UPDATE integracion.argos_sincronizacion_etapas
SET estado = @estado, procesados = @procesados, omitidos = @omitidos, total = sqlc.narg('total')::integer, cursor = sqlc.narg('cursor')::text,
    iniciada_en = sqlc.narg('iniciada_en')::timestamptz, terminada_en = sqlc.narg('terminada_en')::timestamptz, error = sqlc.narg('error')::text
WHERE sincronizacion_id = @sincronizacion_id AND recurso = @recurso
RETURNING *;

-- name: ActualizarSincronizacion :one
-- actualizada_en siempre avanza (el informe a Argos compara por este instante).
UPDATE integracion.argos_sincronizaciones
SET estado = @estado, actualizada_en = GREATEST(@actualizada_en::timestamptz, actualizada_en + interval '1 millisecond'),
    terminada_en = sqlc.narg('terminada_en')::timestamptz
WHERE id = @id
RETURNING *;

-- name: FijarInformeSincronizacion :one
-- informado_hasta solo avanza (un informe atrasado no lo retrocede).
UPDATE integracion.argos_sincronizaciones
SET informe_estado = @informe_estado, informado_en = coalesce(sqlc.narg('informado_en')::timestamptz, informado_en),
    informado_hasta = GREATEST(informado_hasta, sqlc.narg('informado_hasta')::timestamptz), informe_detalle = sqlc.narg('informe_detalle')::text
WHERE id = @id
RETURNING *;

-- name: GuardarClienteLista :exec
-- Cliente leído de GET /clientes (primera sincronización): la lista no trae sus servicios, así que
-- una fila que ya existe conserva sus empresas y sin_servicios (los recalcula la etapa de servicios).
-- Una fila nueva nace sin empresas: nadie la ve en una empresa hasta que llegan sus servicios.
INSERT INTO integracion.argos_clientes (
    id, empresa_id, empresas, sin_servicios, codigo, nombre, telefono, direccion, lat, lon, zona_id, estado, sincronizado_en,
    tipo, tipo_documento_fiscal, nit_cui, movil, correo, zona_nombre, version_datos, referencia
) VALUES (
    @id, 0, '{}'::bigint[], false, @codigo, @nombre, @telefono, @direccion, @lat, @lon, @zona_id, @estado, @sincronizado_en,
    @tipo, @tipo_documento_fiscal, @nit_cui, @movil, @correo, @zona_nombre, @version_datos, @referencia
)
ON CONFLICT (id) DO UPDATE SET
    codigo = excluded.codigo, nombre = excluded.nombre,
    telefono = excluded.telefono, direccion = excluded.direccion, lat = excluded.lat, lon = excluded.lon,
    zona_id = excluded.zona_id, estado = excluded.estado, sincronizado_en = excluded.sincronizado_en,
    tipo = excluded.tipo, tipo_documento_fiscal = excluded.tipo_documento_fiscal, nit_cui = excluded.nit_cui,
    movil = excluded.movil, correo = excluded.correo,
    zona_nombre = CASE WHEN excluded.zona_nombre <> '' THEN excluded.zona_nombre ELSE integracion.argos_clientes.zona_nombre END,
    version_datos = excluded.version_datos, referencia = excluded.referencia, fallo_en = NULL, updated_at = now()
WHERE integracion.argos_clientes.sincronizado_en <= excluded.sincronizado_en;

-- name: ClientesEnInstantanea :many
SELECT id, zona_id FROM integracion.argos_clientes WHERE id = ANY(@ids::bigint[]);

-- name: RecalcularEmpresasClientes :exec
-- Después de guardar una página de servicios: las empresas de cada cliente son las de sus servicios
-- en la instantánea (las mayores que 0). Un cliente sin servicios en la instantánea no cambia.
UPDATE integracion.argos_clientes c
SET empresas = s.empresas, empresa_id = s.empresas[1], sin_servicios = false, updated_at = now()
FROM (
    SELECT cliente_id, array_agg(DISTINCT empresa_id ORDER BY empresa_id)::bigint[] AS empresas
    FROM integracion.argos_servicios
    WHERE cliente_id = ANY(@ids::bigint[]) AND empresa_id > 0
    GROUP BY cliente_id
) s
WHERE c.id = s.cliente_id AND c.empresas IS DISTINCT FROM s.empresas;

-- name: ResumenInstantaneas :many
-- Pantalla de estado (§2.5 d): cuántos hay y el último sincronizado_en de cada instantánea.
SELECT 'clientes'::text AS recurso, count(*)::bigint AS cuantos, max(sincronizado_en) AS ultimo FROM integracion.argos_clientes
UNION ALL SELECT 'servicios'::text, count(*)::bigint, max(sincronizado_en) FROM integracion.argos_servicios
UNION ALL SELECT 'zonas'::text, count(*)::bigint, max(sincronizado_en) FROM integracion.argos_zonas
UNION ALL SELECT 'routers'::text, count(*)::bigint, max(sincronizado_en) FROM integracion.argos_routers;

-- name: ResumenEntrantesFallidos :one
-- Webhooks de Argos de este conector que no se pudieron aplicar (con error al procesarlos).
SELECT count(*)::bigint AS cuantos, max(recibido_en) AS ultimo
FROM integracion.entrantes WHERE conector_id = @conector_id AND error IS NOT NULL AND procesado_en IS NULL;

-- name: ResumenPendientesArgos :many
-- Acciones hacia Argos que no están hechas (integracion.pendientes), por estado.
SELECT estado, count(*)::bigint AS cuantos FROM integracion.pendientes WHERE estado <> 'hecho' GROUP BY estado ORDER BY estado;

-- Certificado TLS de la API de Argos (§1.5, «registrar y avisar») ------------------------------

-- name: UltimoCertificado :one
SELECT * FROM integracion.argos_certificados WHERE conector_id = @conector_id ORDER BY ultimo_visto_en DESC, id DESC LIMIT 1;

-- name: CertificadoPorHuella :one
SELECT * FROM integracion.argos_certificados WHERE conector_id = @conector_id AND huella_sha256 = @huella_sha256;

-- name: VolverAVerCertificado :exec
UPDATE integracion.argos_certificados SET ultimo_visto_en = @visto_en WHERE id = @id AND ultimo_visto_en < @visto_en;

-- name: RegistrarCertificado :one
INSERT INTO integracion.argos_certificados (conector_id, huella_sha256, emisor, sujeto, host, vence_en, visto_en, ultimo_visto_en, anterior_id, cambiado)
VALUES (@conector_id, @huella_sha256, @emisor, @sujeto, @host, sqlc.narg('vence_en')::timestamptz, @visto_en, @visto_en, sqlc.narg('anterior_id')::bigint, @cambiado)
RETURNING *;

-- name: CertificadoPorID :one
SELECT * FROM integracion.argos_certificados WHERE id = @id;

-- name: ReabrirSincronizacion :one
-- Reanudar a mano una sincronización con_errores: vuelve a en_curso con el mismo id (Argos recibe un
-- informe más nuevo por actualizada_en).
UPDATE integracion.argos_sincronizaciones
SET estado = 'en_curso', terminada_en = NULL, actualizada_en = GREATEST(@ahora::timestamptz, actualizada_en + interval '1 millisecond'),
    informe_estado = CASE WHEN informar THEN 'pendiente' ELSE informe_estado END,
    origen = 'manual', iniciada_por = sqlc.narg('iniciada_por')::bigint
WHERE id = @id AND estado = 'con_errores'
RETURNING *;

-- name: ReabrirEtapas :exec
-- Las etapas con errores vuelven a pendiente; las listas por cursor siguen desde su último cursor.
UPDATE integracion.argos_sincronizacion_etapas
SET estado = 'pendiente', error = NULL, terminada_en = NULL
WHERE sincronizacion_id = @sincronizacion_id AND estado = 'con_errores';

-- name: EmpresasPorIDs :many
SELECT id, nombre, nit FROM nucleo.empresas WHERE id = ANY(@ids::bigint[]) ORDER BY id;

-- name: ActivacionDeConector :one
-- Quién activó el conector por última vez (de la auditoría; el conector no lo guarda).
SELECT a.actor_id, a.actor_nombre, a.created_at FROM auditoria.acciones a
WHERE a.accion = 'integraciones.activar' AND a.entidad = 'conector' AND a.entidad_id = (@conector_id::bigint)::text AND a.resultado = 'ok'
ORDER BY a.id DESC LIMIT 1;

-- name: CertificadoVuelveAVerse :one
-- Un certificado ya visto vuelve a aparecer después de otro (A → B → A): cuenta como cambio.
UPDATE integracion.argos_certificados
SET visto_en = @visto_en, ultimo_visto_en = @visto_en, cambiado = true, anterior_id = @anterior_id
WHERE id = @id
RETURNING *;

-- Portal del cliente de Argos (0.9.0 §11) -------------------------------------------------

-- name: ConectorDeTokenPortal :one
-- El conector Argos del token_portal: el token es hijo del iris_pat_ que se entregó en el canje de
-- ese conector (o del que una reconexión sin confirmar todavía reemplaza).
SELECT c.id, c.estado, c.contrato_remoto
FROM nucleo.tokens_api t
JOIN integracion.conectores c ON c.sistema = 'argosisp' AND c.eliminado_en IS NULL
     AND (t.token_padre_id = c.token_entregado_id OR t.token_padre_id = c.reemplaza_token_id)
WHERE t.id = @token_id AND t.token_padre_id IS NOT NULL
ORDER BY (c.estado = 'activo') DESC, c.id DESC
LIMIT 1;

-- name: ContarPeticionPortal :one
-- Tope del conector (§11.2): una petición más en el minuto en curso.
INSERT INTO integracion.portal_argos_minutos (conector_id, minuto, peticiones)
VALUES (@conector_id, date_trunc('minute', now()), 1)
ON CONFLICT (conector_id, minuto) DO UPDATE SET peticiones = integracion.portal_argos_minutos.peticiones + 1
RETURNING peticiones;

-- name: PurgarPeticionesPortal :exec
DELETE FROM integracion.portal_argos_minutos WHERE minuto < now() - interval '10 minutes';

-- name: ClienteEnInstantanea :one
SELECT id, empresa_id, empresas, sin_servicios FROM integracion.argos_clientes WHERE id = @id;

-- name: ContactoEnCache :one
SELECT * FROM integracion.argos_contactos WHERE contacto_id = @contacto_id;

-- name: GuardarContacto :execrows
-- La única escritura de un contacto en la caché (portal §11.10 y atención §3.5): todas las columnas,
-- sin bajar nunca la versión (una lectura vieja que llega tarde no pisa una más nueva). @leido_en es la
-- hora (de la base) en que empezó la lectura en Argos: con la misma versión, una lectura que empezó
-- antes del último cambio de la fila (p. ej. un quitado por webhook, que no sube la versión) no la pisa
-- ni la revive. Devuelve 0 filas si no se escribió (entonces tampoco se tocan sus teléfonos).
INSERT INTO integracion.argos_contactos (contacto_id, cliente_id, empresa_id, nombre, relacion, es_titular, servicios,
    consultar, reportar, pagar, avisos, persona_cliente_id, version, quitado, leido_en)
VALUES (@contacto_id, @cliente_id, sqlc.narg('empresa_id'), @nombre, @relacion, @es_titular, sqlc.narg('servicios')::bigint[],
    @consultar, @reportar, @pagar, @avisos, sqlc.narg('persona_cliente_id'), @version, false, @leido_en)
ON CONFLICT (contacto_id) DO UPDATE SET cliente_id = excluded.cliente_id, empresa_id = excluded.empresa_id,
    nombre = excluded.nombre, relacion = excluded.relacion, es_titular = excluded.es_titular, servicios = excluded.servicios,
    consultar = excluded.consultar, reportar = excluded.reportar, pagar = excluded.pagar, avisos = excluded.avisos,
    persona_cliente_id = excluded.persona_cliente_id, version = excluded.version, quitado = false,
    leido_en = greatest(integracion.argos_contactos.leido_en, excluded.leido_en), updated_at = now()
WHERE excluded.version > integracion.argos_contactos.version
    OR (excluded.version = integracion.argos_contactos.version AND excluded.leido_en >= integracion.argos_contactos.leido_en);

-- name: QuitarContactosAusentes :execrows
-- Los contactos del cliente que Argos ya no devuelve: quitados (no se borran; dejan de ver sus tickets
-- y pierden su rol en la atención). Solo los que no se escribieron después de que empezó esta lectura
-- (@leido_en): una lectura vieja no quita un contacto que guardó una más nueva.
UPDATE integracion.argos_contactos SET quitado = true, leido_en = now(), updated_at = now()
WHERE cliente_id = @cliente_id AND NOT quitado AND NOT (contacto_id = ANY(@vigentes::bigint[]))
    AND leido_en <= @leido_en::timestamptz;

-- name: QuitarContacto :execrows
UPDATE integracion.argos_contactos SET quitado = true, leido_en = now(), updated_at = now()
WHERE contacto_id = @contacto_id AND cliente_id = @cliente_id AND NOT quitado;

-- name: HayContactosDeCliente :one
SELECT EXISTS (SELECT 1 FROM integracion.argos_contactos WHERE cliente_id = @cliente_id)::boolean;

-- name: TokenPortalVigente :one
-- El iris_pat_ entregado ya tiene un token_portal vigente (hijo sin revocar ni eliminar).
SELECT EXISTS (SELECT 1 FROM nucleo.tokens_api WHERE token_padre_id = @padre_id
    AND revocado_en IS NULL AND eliminado_en IS NULL)::boolean;

-- name: ConectorDeTokenPanel :one
-- El conector Argos al que pertenece el iris_pat_ del panel (el entregado en su canje o el que una
-- reconexión sin confirmar todavía reemplaza).
SELECT c.id, c.estado FROM integracion.conectores c
WHERE c.sistema = 'argosisp' AND c.eliminado_en IS NULL AND (c.token_entregado_id = @token_id OR c.reemplaza_token_id = @token_id)
ORDER BY (c.estado = 'activo') DESC, c.id DESC
LIMIT 1;

-- Contactos del cliente en la atención (contrato 0.10.0 §3.5, A42; 0.10.1 §3.8, A46) --------------
-- La tabla y su escritura son las del portal (GuardarContacto, QuitarContacto*): aquí, los teléfonos y
-- cuándo se leyó completo cada cliente.

-- name: BorrarTelefonosContactoArgos :exec
DELETE FROM integracion.argos_contacto_telefonos WHERE contacto_id = @contacto_id;

-- name: GuardarTelefonoContactoArgos :exec
INSERT INTO integracion.argos_contacto_telefonos (contacto_id, numero, digitos, whatsapp, principal)
VALUES (@contacto_id, @numero, @digitos, @whatsapp, @principal)
ON CONFLICT (contacto_id, numero) DO UPDATE SET digitos = EXCLUDED.digitos, whatsapp = EXCLUDED.whatsapp, principal = EXCLUDED.principal;

-- name: OlvidarContactosLeidos :exec
-- Invalida la caché del cliente: se vuelve a leer de Argos cuando una persona la necesite.
DELETE FROM integracion.argos_contactos_leidos WHERE cliente_id = @cliente_id;

-- name: MarcarContactosLeidos :exec
-- @sincronizado_en es la hora en que empezó la lectura (no la de guardar) y nunca retrocede.
INSERT INTO integracion.argos_contactos_leidos (cliente_id, sincronizado_en) VALUES (@cliente_id, @sincronizado_en)
ON CONFLICT (cliente_id) DO UPDATE SET sincronizado_en = greatest(integracion.argos_contactos_leidos.sincronizado_en, EXCLUDED.sincronizado_en);

-- name: InicioLecturaContactos :one
-- La hora de la base al empezar una lectura de contactos en Argos (la misma reloj que now()).
SELECT clock_timestamp()::timestamptz;

-- name: ContactosLeidosEn :one
SELECT sincronizado_en FROM integracion.argos_contactos_leidos WHERE cliente_id = @cliente_id;

-- name: HuellaContactosCliente :one
-- Huella de los contactos vigentes del cliente (ids, si son titulares y sus números): si cambia al
-- releer, la mensajería revisa las conversaciones asociadas por esos contactos.
SELECT coalesce(md5(string_agg(k.contacto_id::text || ':' || k.es_titular::text || ':' || coalesce(t.digitos, ''), ','
    ORDER BY k.contacto_id, t.digitos)), '')::text
FROM integracion.argos_contactos k LEFT JOIN integracion.argos_contacto_telefonos t ON t.contacto_id = k.contacto_id
WHERE k.cliente_id = @cliente_id AND NOT k.quitado;


-- Falla general (0.13.0 §3.11): servicios por leer en Argos en un job aparte -------------------------

-- name: CrearLecturaIncidente :exec
INSERT INTO integracion.argos_incidente_lecturas (entrante_id, conector_id, incidente_id, empresa_id, paginar)
VALUES (@entrante_id, @conector_id, @incidente_id, @empresa_id, @paginar)
ON CONFLICT (entrante_id) DO NOTHING;

-- name: AgregarServiciosLectura :exec
INSERT INTO integracion.argos_incidente_servicios (entrante_id, servicio_id)
SELECT @entrante_id::bigint, unnest(@servicios::bigint[])
ON CONFLICT DO NOTHING;

-- name: LecturaIncidente :one
SELECT * FROM integracion.argos_incidente_lecturas WHERE entrante_id = @entrante_id;

-- name: AvanzarLecturaIncidente :exec
UPDATE integracion.argos_incidente_lecturas SET cursor = sqlc.narg(cursor), paginar = @paginar WHERE entrante_id = @entrante_id;

-- name: TerminarLecturaIncidente :exec
UPDATE integracion.argos_incidente_lecturas SET terminada_en = now(), paginar = false WHERE entrante_id = @entrante_id AND terminada_en IS NULL;

-- name: TerminarLecturasDeIncidente :exec
-- La falla se cerro: lo que quede por leer de ese incidente ya no se lee.
UPDATE integracion.argos_incidente_lecturas SET terminada_en = now(), paginar = false
WHERE incidente_id = @incidente_id AND empresa_id = @empresa_id AND terminada_en IS NULL;

-- name: ContarPaginaLectura :one
UPDATE integracion.argos_incidente_lecturas SET paginas = paginas + 1 WHERE entrante_id = @entrante_id RETURNING paginas;

-- name: PendientesLecturaIncidente :many
SELECT servicio_id, intentos FROM integracion.argos_incidente_servicios
WHERE entrante_id = @entrante_id AND estado = 'pendiente'
ORDER BY servicio_id
LIMIT @limite;

-- name: ContarServiciosLectura :one
SELECT count(*)::integer FROM integracion.argos_incidente_servicios WHERE entrante_id = @entrante_id;

-- name: MarcarServicioLectura :exec
UPDATE integracion.argos_incidente_servicios SET estado = @estado, intentos = @intentos
WHERE entrante_id = @entrante_id AND servicio_id = @servicio_id;

-- name: MarcarTitularCambiado :execrows
-- servicio.titular_cambiado aplicado (MEDIA 2 de #280): la hora de Argos (ocurrido_en, solo con
-- ocurrido_origen = argos y dentro de la tolerancia) del último
-- cambio de titular; nunca retrocede. Llamar con BloquearArgos del servicio tomado.
UPDATE integracion.argos_servicios SET titular_cambiado_en = @en
WHERE id = @id AND (titular_cambiado_en IS NULL OR titular_cambiado_en <= @en);

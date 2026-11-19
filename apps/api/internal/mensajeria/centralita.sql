-- Centralita Issabel, etapa E4a (contratos/voz.md §2.10 y §3.12).

-- Conexión ---------------------------------------------------------------------------------------

-- name: CentralitaDeEmpresa :one
SELECT * FROM mensajeria.centralitas WHERE coalesce(empresa_id, 0) = @empresa::bigint;

-- name: CentralitaPorID :one
SELECT * FROM mensajeria.centralitas WHERE id = @id AND coalesce(empresa_id, 0) = @empresa::bigint;

-- name: CentralitaParaActualizar :one
SELECT * FROM mensajeria.centralitas WHERE id = @id AND coalesce(empresa_id, 0) = @empresa::bigint FOR UPDATE;

-- name: CentralitaDelMotor :one
-- La fila que usa la conexión (sin empresa: el motor es del proceso).
SELECT * FROM mensajeria.centralitas WHERE id = @id;

-- name: CentralitaDelMotorParaActualizar :one
SELECT * FROM mensajeria.centralitas WHERE id = @id FOR UPDATE;

-- name: CentralitasActivas :many
SELECT id, version FROM mensajeria.centralitas WHERE activa ORDER BY id;

-- name: InsertarCentralita :one
INSERT INTO mensajeria.centralitas (empresa_id, nombre, host, puerto, usuario)
VALUES (sqlc.narg('empresa_id'), @nombre, @host, @puerto, @usuario)
RETURNING *;

-- name: GuardarCentralita :one
-- Escribe lo que calculó el caso de uso (la fila se leyó con FOR UPDATE).
UPDATE mensajeria.centralitas SET nombre = @nombre, host = @host, puerto = @puerto, usuario = @usuario,
    secreto_cifrado = @secreto_cifrado, secreto_guardado_en = @secreto_guardado_en, huella_tls = @huella_tls,
    huella_vista = @huella_vista, estado = @estado, error_codigo = @error_codigo, probada_en = @probada_en,
    conectada_en = @conectada_en, desconectada_desde = @desconectada_desde, activa = @activa,
    version = version + 1, updated_at = now()
WHERE id = @id
RETURNING *;

-- name: TocarCentralita :exec
-- Señal de vida del AMI: como mucho una escritura por minuto.
UPDATE mensajeria.centralitas SET ultimo_evento_en = @ahora
WHERE id = @id AND (ultimo_evento_en IS NULL OR ultimo_evento_en < @hace_minuto::timestamptz);

-- name: NotificarCentralitas :exec
SELECT pg_notify('iris_centralitas', @carga::text);

-- Extensiones ------------------------------------------------------------------------------------

-- name: ExtensionesDeCentralita :many
SELECT x.*, u.nombre AS usuario_nombre FROM mensajeria.centralita_extensiones x
JOIN nucleo.usuarios u ON u.id = x.usuario_id
WHERE x.centralita_id = @centralita_id AND x.activa
ORDER BY x.extension;

-- name: ExtensionPorID :one
SELECT * FROM mensajeria.centralita_extensiones WHERE id = @id AND centralita_id = @centralita_id FOR UPDATE;

-- name: ExtensionActivaPorNumero :one
SELECT * FROM mensajeria.centralita_extensiones WHERE centralita_id = @centralita_id AND extension = @extension AND activa;

-- name: ExtensionActivaDeUsuario :one
SELECT * FROM mensajeria.centralita_extensiones WHERE centralita_id = @centralita_id AND usuario_id = @usuario_id AND activa;

-- name: InsertarExtension :one
INSERT INTO mensajeria.centralita_extensiones (empresa_id, centralita_id, extension, usuario_id)
VALUES (sqlc.narg('empresa_id'), @centralita_id, @extension, @usuario_id)
RETURNING *;

-- name: QuitarExtension :one
UPDATE mensajeria.centralita_extensiones SET activa = false, quitada_en = @ahora::timestamptz, updated_at = now()
WHERE id = @id AND activa
RETURNING *;

-- name: ExtensionesDelMotor :many
-- Las extensiones que el motor escucha: las demás se descartan sin mirar.
SELECT extension, usuario_id FROM mensajeria.centralita_extensiones WHERE centralita_id = @centralita_id AND activa;

-- name: TieneExtensionActiva :one
-- El usuario tiene una extensión activa en la centralita activa de la empresa (feed de la ventana).
SELECT EXISTS (
    SELECT 1 FROM mensajeria.centralita_extensiones x
    JOIN mensajeria.centralitas c ON c.id = x.centralita_id
    WHERE x.usuario_id = @usuario::bigint AND x.activa AND c.activa AND coalesce(c.empresa_id, 0) = @empresa::bigint
)::boolean AS tiene;

-- name: UsuarioDeEmpresaActivo :one
-- El usuario existe, está activo y pertenece a la empresa (o la empresa es 0: sin multiempresa).
SELECT u.id, u.nombre FROM nucleo.usuarios u
WHERE u.id = @usuario::bigint AND u.activo AND u.tipo = 'persona'
  AND (@empresa::bigint = 0
       OR EXISTS (SELECT 1 FROM nucleo.usuarios_empresas ue WHERE ue.usuario_id = u.id AND ue.empresa_id = @empresa::bigint)
       OR EXISTS (SELECT 1 FROM nucleo.usuarios_roles ur JOIN nucleo.roles r ON r.id = ur.rol_id AND r.activo
                  JOIN nucleo.roles_permisos rp ON rp.rol_id = ur.rol_id
                  WHERE ur.usuario_id = u.id AND rp.permiso = 'empresas.todas' AND ur.zona_id IS NULL AND ur.cuadrilla_id IS NULL));

-- Llamadas (mensajeria.llamadas con origen centralita) --------------------------------------------

-- name: InsertarLlamadaCentralita :one
INSERT INTO mensajeria.llamadas (id, empresa_id, origen, centralita_id, id_externo, direccion, numero, cliente_id, servicio_id,
    asociacion, coincidencias, estado, usuario_id, extension, atendida_por, sono_en, suena_hasta)
VALUES (@id, sqlc.narg('empresa_id'), 'centralita', @centralita_id::bigint, @id_externo, @direccion, @numero, sqlc.narg('cliente_id'), sqlc.narg('servicio_id'),
    sqlc.narg('asociacion'), @coincidencias, @estado, sqlc.narg('usuario_id'), sqlc.narg('extension'), sqlc.narg('atendida_por'), @sono_en, @suena_hasta)
ON CONFLICT (centralita_id, id_externo) WHERE origen = 'centralita' DO NOTHING
RETURNING *;

-- name: LlamadaCentralitaPorExterno :one
SELECT * FROM mensajeria.llamadas WHERE origen = 'centralita' AND centralita_id = @centralita_id::bigint AND id_externo = @id_externo FOR UPDATE;

-- name: LlamadaCentralitaExiste :one
SELECT EXISTS (SELECT 1 FROM mensajeria.llamadas WHERE origen = 'centralita' AND centralita_id = @centralita_id::bigint AND id_externo = @id_externo)::boolean AS existe;

-- name: LlamadaCentralitaParaActualizar :one
SELECT * FROM mensajeria.llamadas WHERE id = @id AND origen = 'centralita' AND coalesce(empresa_id, 0) = @empresa::bigint FOR UPDATE;

-- name: GuardarLlamadaCentralita :one
UPDATE mensajeria.llamadas SET estado = @estado, usuario_id = @usuario_id, extension = @extension, atendida_por = @atendida_por,
    contestada_en = @contestada_en, terminada_en = @terminada_en, duracion_seg = @duracion_seg, motivo_fin = @motivo_fin,
    cliente_id = @cliente_id, servicio_id = @servicio_id, asociacion = @asociacion,
    nota = @nota, resultado = @resultado, anotada_por = @anotada_por, ticket_id = @ticket_id,
    version = version + 1, updated_at = now()
WHERE id = @id AND origen = 'centralita'
RETURNING *;

-- name: InsertarReceptorCentralita :one
INSERT INTO mensajeria.centralita_receptores (llamada_id, usuario_id, empresa_id, extension, sono_en)
VALUES (@llamada_id, @usuario_id, sqlc.narg('empresa_id'), @extension, @sono_en)
ON CONFLICT (llamada_id, usuario_id) DO NOTHING
RETURNING usuario_id;

-- name: ReceptoresCentralita :many
SELECT r.usuario_id, r.extension, u.nombre FROM mensajeria.centralita_receptores r
JOIN nucleo.usuarios u ON u.id = r.usuario_id
WHERE r.llamada_id = @llamada_id ORDER BY r.extension;

-- name: ReceptoresDeLlamadas :many
SELECT r.llamada_id, r.usuario_id, r.extension, u.nombre FROM mensajeria.centralita_receptores r
JOIN nucleo.usuarios u ON u.id = r.usuario_id
WHERE r.llamada_id = ANY (@ids::uuid[]) ORDER BY r.llamada_id, r.extension;

-- name: EsReceptorCentralita :one
SELECT EXISTS (SELECT 1 FROM mensajeria.centralita_receptores WHERE llamada_id = @llamada_id AND usuario_id = @usuario_id)::boolean AS es;

-- name: LlamadasCentralitaPorIDs :many
SELECT * FROM mensajeria.llamadas WHERE id = ANY (@ids::uuid[]) AND origen = 'centralita';

-- name: ListarLlamadasCliente :many
-- Historial de llamadas (WhatsApp y centralita) con cursor (sono_en, id) descendente: por cliente,
-- por ticket o las de la centralita que le sonaron a un usuario o que marcó.
SELECT l.* FROM mensajeria.llamadas l
WHERE coalesce(l.empresa_id, 0) = @empresa::bigint
  AND (sqlc.narg('cliente_id')::bigint IS NULL OR l.cliente_id = sqlc.narg('cliente_id')::bigint)
  AND (sqlc.narg('ticket_id')::bigint IS NULL OR l.ticket_id = sqlc.narg('ticket_id')::bigint)
  AND (sqlc.narg('receptor')::bigint IS NULL OR (l.origen = 'centralita' AND (l.usuario_id = sqlc.narg('receptor')::bigint
       OR EXISTS (SELECT 1 FROM mensajeria.centralita_receptores r WHERE r.llamada_id = l.id AND r.usuario_id = sqlc.narg('receptor')::bigint))))
  -- Las de WhatsApp solo de las conversaciones que ve (con conversaciones.ver).
  AND (l.origen = 'centralita' OR (@conversaciones::boolean AND EXISTS (
       SELECT 1 FROM mensajeria.conversaciones c WHERE c.id = l.conversacion_id AND coalesce(c.empresa_id, 0) = @empresa::bigint
         AND mensajeria.conv_visible(@usuario::bigint, @supervisa::boolean, c.id, c.cola_id, c.asignado_a))))
  AND (sqlc.narg('desde')::timestamptz IS NULL OR l.sono_en >= sqlc.narg('desde')::timestamptz)
  AND (sqlc.narg('cursor_en')::timestamptz IS NULL OR (l.sono_en, l.id) < (sqlc.narg('cursor_en')::timestamptz, sqlc.narg('cursor_id')::uuid))
ORDER BY l.sono_en DESC, l.id DESC
LIMIT @limite;

-- name: LlamadasCentralitaVivas :many
-- Las que siguen sonando, marcando o en curso de una centralita.
SELECT * FROM mensajeria.llamadas
WHERE origen = 'centralita' AND centralita_id = @centralita_id::bigint AND estado IN ('sonando', 'marcando', 'en_curso') AND sono_en < @antes::timestamptz
ORDER BY sono_en
LIMIT 200
FOR UPDATE SKIP LOCKED;

-- name: InsertarEventoCentralita :one
-- La ventana de la llamada, dirigida a una persona (sin conversación).
INSERT INTO mensajeria.conv_eventos (empresa_id, conversacion_id, tipo, usuario_id, ocurrido_en, llamada_id)
VALUES (sqlc.narg('empresa_id'), NULL, @tipo, @usuario_id::bigint, @ocurrido_en, @llamada_id::uuid)
RETURNING id;

-- name: FijarEstadoCentralita :one
-- Lo que cambia la conexión (no sube version: la versión es de lo que configura una persona).
UPDATE mensajeria.centralitas SET estado = @estado, error_codigo = sqlc.narg('error_codigo'), huella_vista = sqlc.narg('huella_vista'),
    conectada_en = sqlc.narg('conectada_en'), desconectada_desde = sqlc.narg('desconectada_desde'), updated_at = now()
WHERE id = @id AND version = @version AND activa
RETURNING *;

-- name: TomarMotorCentralita :one
-- El arriendo de la conexión: lo toma quien no tiene dueño, quien ya lo tenía o si venció.
UPDATE mensajeria.centralitas SET motor_dueno = @dueno, motor_hasta = now() + make_interval(secs => @segundos::int)
WHERE id = @id AND activa AND (motor_dueno IS NULL OR motor_dueno = @dueno OR motor_hasta < now())
RETURNING id;

-- name: SoltarMotorCentralita :exec
UPDATE mensajeria.centralitas SET motor_dueno = NULL, motor_hasta = NULL WHERE id = @id AND motor_dueno = @dueno;

-- name: MiExtension :one
-- La extensión activa del usuario en la centralita activa de la empresa (para montar la ventana).
SELECT x.id, x.extension, c.id AS centralita_id, c.estado FROM mensajeria.centralita_extensiones x
JOIN mensajeria.centralitas c ON c.id = x.centralita_id
WHERE x.usuario_id = @usuario::bigint AND x.activa AND c.activa AND coalesce(c.empresa_id, 0) = @empresa::bigint
LIMIT 1;

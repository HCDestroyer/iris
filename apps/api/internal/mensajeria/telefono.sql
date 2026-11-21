-- El teléfono de Iris, etapa E4c (contratos/voz.md §3.12, «E4c: el teléfono dentro de Iris»).

-- Configuración ------------------------------------------------------------------------------------

-- name: GuardarTelefonoCentralita :one
UPDATE mensajeria.centralitas SET telefono_activo = @telefono_activo, wss_puerto = @wss_puerto, wss_ruta = @wss_ruta, wss_huella = sqlc.narg('wss_huella'),
    sip_dominio = sqlc.narg('sip_dominio'), prefijo_salida = @prefijo_salida, llamadas_por_usuario_dia = @llamadas_por_usuario_dia,
    llamadas_simultaneas = @llamadas_simultaneas, telefono_probado_en = sqlc.narg('telefono_probado_en'),
    restricciones_confirmadas_en = sqlc.narg('restricciones_confirmadas_en'), version = version + 1, updated_at = now()
WHERE id = @id
RETURNING *;

-- name: GuardarTelefonoExtension :one
UPDATE mensajeria.centralita_extensiones SET tipo = @tipo, sip_usuario = sqlc.narg('sip_usuario'), sip_secreto_cifrado = sqlc.narg('sip_secreto_cifrado'),
    sip_guardado_en = sqlc.narg('sip_guardado_en'), sip_probado_en = sqlc.narg('sip_probado_en'), updated_at = now()
WHERE id = @id AND activa
RETURNING *;

-- name: MarcarRegistroProbado :exec
UPDATE mensajeria.centralita_extensiones SET sip_probado_en = sqlc.narg('sip_probado_en'), updated_at = now() WHERE id = @id AND activa;

-- name: BorrarCredencialExtension :exec
-- Al quitar la extensión, la credencial se borra.
UPDATE mensajeria.centralita_extensiones SET sip_secreto_cifrado = NULL, sip_guardado_en = NULL, sip_probado_en = NULL, tipo = 'escritorio'
WHERE id = @id;

-- name: TelefonoDeUsuario :one
-- La extensión activa del usuario en la centralita de la empresa, con lo que el teléfono necesita.
SELECT x.id, x.extension, x.tipo, x.sip_usuario, x.sip_secreto_cifrado, x.sip_guardado_en, x.sip_probado_en, x.empresa_id,
    c.id AS centralita_id, c.telefono_activo, c.host, c.wss_puerto, c.wss_ruta, c.wss_huella, coalesce(c.sip_dominio, c.host)::text AS dominio,
    c.prefijo_salida, c.llamadas_por_usuario_dia, c.llamadas_simultaneas
FROM mensajeria.centralita_extensiones x
JOIN mensajeria.centralitas c ON c.id = x.centralita_id
WHERE x.usuario_id = @usuario::bigint AND x.activa AND coalesce(c.empresa_id, 0) = @empresa::bigint
LIMIT 1;

-- Grupos -------------------------------------------------------------------------------------------

-- name: GruposDeCentralita :many
SELECT * FROM mensajeria.centralita_grupos WHERE centralita_id = @centralita_id ORDER BY activo DESC, lower(nombre), id;

-- name: InsertarGrupoCentralita :one
INSERT INTO mensajeria.centralita_grupos (empresa_id, centralita_id, nombre, numero)
VALUES (sqlc.narg('empresa_id'), @centralita_id, @nombre, @numero)
RETURNING *;

-- name: GrupoCentralitaPorID :one
SELECT * FROM mensajeria.centralita_grupos WHERE id = @id AND centralita_id = @centralita_id FOR UPDATE;

-- name: GuardarGrupoCentralita :one
UPDATE mensajeria.centralita_grupos SET nombre = @nombre, numero = @numero, activo = @activo, version = version + 1, updated_at = now()
WHERE id = @id
RETURNING *;

-- name: GrupoActivoPorNumero :one
SELECT * FROM mensajeria.centralita_grupos WHERE centralita_id = @centralita_id AND numero = @numero AND activo;

-- Tickets y conexiones -------------------------------------------------------------------------------

-- name: InsertarTicketTelefono :exec
INSERT INTO mensajeria.telefono_tickets (hash, usuario_id, empresa_id, extension_id, dispositivo, sesion_id, expira_en)
VALUES (@hash, @usuario_id, sqlc.narg('empresa_id'), @extension_id, @dispositivo, @sesion_id, @expira_en);

-- name: UsarTicketTelefono :one
-- Un solo uso y sin vencer.
UPDATE mensajeria.telefono_tickets SET usado_en = @ahora::timestamptz
WHERE hash = @hash AND usado_en IS NULL AND expira_en > @ahora::timestamptz
RETURNING *;

-- name: TicketsTelefonoRecientes :one
-- Tickets que la persona pidió en el último minuto (vencen a los 30 s).
SELECT count(*)::int AS n FROM mensajeria.telefono_tickets WHERE usuario_id = @usuario_id::bigint AND expira_en > @desde::timestamptz;

-- name: TicketsTelefonoRecientesEmpresa :one
-- Tickets de toda la empresa en el último minuto (tope por empresa; va después de CandadoChat «telefono:abrir:e<empresa>»).
SELECT count(*)::int AS n FROM mensajeria.telefono_tickets WHERE coalesce(empresa_id, 0) = @empresa::bigint AND expira_en > @desde::timestamptz;

-- name: ConexionTelefonoVigente :one
SELECT EXISTS (SELECT 1 FROM mensajeria.telefono_conexiones WHERE id = @id)::boolean AS vigente;

-- name: BorrarTicketsTelefono :exec
DELETE FROM mensajeria.telefono_tickets WHERE expira_en < @antes::timestamptz;

-- name: TomarConexionTelefono :exec
-- La nueva conexión de la persona reemplaza a la vieja (que se cierra en su siguiente latido).
INSERT INTO mensajeria.telefono_conexiones (id, usuario_id, empresa_id, extension_id, dispositivo, replica, abierta_en, latido_en)
VALUES (@id, @usuario_id, sqlc.narg('empresa_id'), @extension_id, @dispositivo, @replica, @ahora::timestamptz, @ahora::timestamptz)
ON CONFLICT (usuario_id) DO UPDATE SET id = excluded.id, empresa_id = excluded.empresa_id, extension_id = excluded.extension_id,
    dispositivo = excluded.dispositivo, replica = excluded.replica, abierta_en = excluded.abierta_en, latido_en = excluded.latido_en;

-- name: LatidoConexionTelefono :one
-- La conexión sigue siendo la de la persona (si no, la reemplazaron).
UPDATE mensajeria.telefono_conexiones SET latido_en = @ahora::timestamptz WHERE id = @id RETURNING id;

-- name: SoltarConexionTelefono :exec
DELETE FROM mensajeria.telefono_conexiones WHERE id = @id;

-- Llamadas del puente y directas ---------------------------------------------------------------------

-- name: InsertarLlamadaPuente :one
INSERT INTO mensajeria.llamadas (id, empresa_id, origen, centralita_id, id_externo, direccion, numero, cliente_id, servicio_id, asociacion, coincidencias,
    estado, usuario_id, extension, sono_en, suena_hasta, ticket_id, orden_id, puente)
VALUES (@id, sqlc.narg('empresa_id'), 'centralita', @centralita_id::bigint, @id_externo, 'saliente', @numero, sqlc.narg('cliente_id'),
    sqlc.narg('servicio_id'), sqlc.narg('asociacion'), @coincidencias, 'marcando', @usuario_id::bigint, @extension::text, @sono_en, @suena_hasta,
    sqlc.narg('ticket_id'), sqlc.narg('orden_id'), true)
RETURNING *;

-- name: InsertarLlamadaDirecta :one
INSERT INTO mensajeria.llamadas (id, empresa_id, origen, id_externo, direccion, numero, cliente_id, servicio_id, coincidencias, estado, usuario_id,
    atendida_por, motivo_fin, sono_en, suena_hasta, terminada_en, orden_id)
VALUES (@id, sqlc.narg('empresa_id'), 'celular', @id_externo, 'saliente', @numero, sqlc.narg('cliente_id'), sqlc.narg('servicio_id'),
    0, 'terminada', @usuario_id::bigint, 'persona', 'directa', @ahora::timestamptz, @ahora::timestamptz, @ahora::timestamptz, @orden_id::bigint)
RETURNING *;

-- name: ContarLlamadasPuenteDia :one
SELECT count(*)::int AS n FROM mensajeria.llamadas WHERE puente AND usuario_id = @usuario_id::bigint AND sono_en >= @desde::timestamptz;

-- name: ContarLlamadasPuenteVivas :one
-- Simultáneas de la empresa: las que el puente dejó pasar o que aún esperan su INVITE (60 s).
SELECT count(*)::int AS n FROM mensajeria.llamadas
WHERE puente AND coalesce(empresa_id, 0) = @empresa::bigint AND estado IN ('marcando', 'en_curso')
  AND (estado = 'en_curso' OR invite_en IS NOT NULL OR sono_en > @hace_minuto::timestamptz);

-- name: MarcacionesVencidas :many
-- Marcaciones de la persona que nunca mandaron su INVITE.
SELECT * FROM mensajeria.llamadas
WHERE puente AND usuario_id = @usuario_id::bigint AND estado = 'marcando' AND invite_en IS NULL AND sono_en < @antes::timestamptz
ORDER BY sono_en
LIMIT 50
FOR UPDATE SKIP LOCKED;

-- name: LlamadaPuenteParaInvitar :one
SELECT * FROM mensajeria.llamadas
WHERE id = @id AND puente AND usuario_id = @usuario_id::bigint AND estado = 'marcando' AND invite_en IS NULL AND sono_en > @desde::timestamptz
FOR UPDATE;

-- name: MarcarInviteLlamada :exec
UPDATE mensajeria.llamadas SET invite_en = @ahora::timestamptz, updated_at = now() WHERE id = @id;

-- name: LlamadaPuenteParaActualizar :one
SELECT * FROM mensajeria.llamadas WHERE id = @id AND puente FOR UPDATE;

-- name: CandidatasAdoptarPuente :many
-- Salientes del puente de esa extensión aún sin Linkedid (el AMI adopta la que marcó el mismo número).
SELECT * FROM mensajeria.llamadas
WHERE puente AND origen = 'centralita' AND centralita_id = @centralita_id::bigint AND extension = @extension::text
  AND id_externo LIKE 'iris:%' AND estado IN ('marcando', 'en_curso') AND sono_en > @desde::timestamptz
ORDER BY sono_en DESC
LIMIT 5
FOR UPDATE;

-- name: AdoptarLlamadaPuente :one
UPDATE mensajeria.llamadas SET id_externo = @linkedid, updated_at = now() WHERE id = @id RETURNING *;

-- name: LlamadaEntranteDeUsuario :one
-- La llamada que el AMI creó para un INVITE que llega al teléfono de la persona (X-Iris-Llamada).
SELECT l.id FROM mensajeria.llamadas l
JOIN mensajeria.centralita_receptores r ON r.llamada_id = l.id AND r.usuario_id = @usuario_id::bigint
WHERE l.origen = 'centralita' AND l.direccion = 'entrante' AND l.estado = 'sonando' AND l.sono_en > @desde::timestamptz
  AND right(regexp_replace(l.numero, '\D', '', 'g'), 8) = right(@digitos::text, 8)
ORDER BY l.sono_en DESC
LIMIT 1;

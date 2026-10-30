-- Usuarios -----------------------------------------------------------------

-- name: CrearUsuario :one
INSERT INTO nucleo.usuarios (nombre, correo, tipo, contrasena_hash)
VALUES (@nombre, @correo, @tipo, @contrasena_hash)
RETURNING *;

-- name: UsuarioPorID :one
SELECT * FROM nucleo.usuarios WHERE id = @id;

-- name: UsuarioPorIDParaActualizar :one
SELECT * FROM nucleo.usuarios WHERE id = @id FOR UPDATE;

-- name: UsuarioPorCorreo :one
SELECT * FROM nucleo.usuarios WHERE correo = @correo;

-- name: ListarUsuarios :many
-- Sin @todas (empresas.todas global o sin empresas registradas), solo el propio usuario y los
-- que comparten alguna empresa con él (@empresas).
SELECT * FROM nucleo.usuarios u
WHERE u.id > @despues_de
  AND (@todas::boolean OR u.id = @yo
       OR EXISTS (SELECT 1 FROM nucleo.usuarios_empresas ue
                  WHERE ue.usuario_id = u.id AND ue.empresa_id = ANY(@empresas::bigint[])))
  AND (sqlc.narg(q)::text IS NULL OR nombre ILIKE '%' || sqlc.narg(q) || '%' OR correo ILIKE '%' || sqlc.narg(q) || '%')
  AND (sqlc.narg(activo)::boolean IS NULL OR activo = sqlc.narg(activo))
ORDER BY id
LIMIT @limite;

-- name: ActualizarUsuario :one
UPDATE nucleo.usuarios
SET nombre = @nombre, correo = @correo, contrasena_hash = @contrasena_hash,
    version = version + 1, updated_at = now()
WHERE id = @id AND version = @version
RETURNING *;

-- name: DesactivarUsuario :one
UPDATE nucleo.usuarios
SET activo = false, version = version + 1, updated_at = now()
WHERE id = @id AND version = @version
RETURNING *;

-- name: GuardarSecretoTOTP :exec
UPDATE nucleo.usuarios
SET totp_secreto = @totp_secreto, totp_activo = false, updated_at = now()
WHERE id = @id;

-- name: ActivarTOTP :one
UPDATE nucleo.usuarios
SET totp_activo = true, totp_ultimo_paso = @paso, version = version + 1, updated_at = now()
WHERE id = @id
RETURNING *;

-- name: ReiniciarMFA :one
-- Reinicio por un administrador: borra el secreto (configurado o activo) y el paso usado, y
-- reemplaza la contraseña por una temporal que hay que cambiar al entrar.
UPDATE nucleo.usuarios
SET totp_secreto = NULL, totp_activo = false, totp_ultimo_paso = 0,
    contrasena_hash = @contrasena_hash, debe_cambiar_contrasena = true,
    version = version + 1, updated_at = now()
WHERE id = @id AND version = @version
RETURNING *;

-- name: CambiarContrasenaPropia :one
-- El propio usuario cambia su contraseña (y deja de tener una temporal).
UPDATE nucleo.usuarios
SET contrasena_hash = @contrasena_hash, debe_cambiar_contrasena = false,
    version = version + 1, updated_at = now()
WHERE id = @id
RETURNING *;

-- name: RevocarOtrasSesionesDeUsuario :execrows
UPDATE nucleo.sesiones SET revocada_en = now(), updated_at = now()
WHERE usuario_id = @usuario_id AND id <> @sesion_id AND revocada_en IS NULL;

-- name: RegistrarPasoTOTP :execrows
UPDATE nucleo.usuarios SET totp_ultimo_paso = @paso, updated_at = now()
WHERE id = @id AND totp_ultimo_paso < @paso;

-- Roles y permisos ------------------------------------------------------------

-- name: RolesDeUsuarios :many
SELECT ur.usuario_id, ur.zona_id, ur.cuadrilla_id, r.codigo, r.nombre
FROM nucleo.usuarios_roles ur
JOIN nucleo.roles r ON r.id = ur.rol_id
WHERE ur.usuario_id = ANY(@usuarios::bigint[])
ORDER BY ur.usuario_id, r.codigo, ur.zona_id NULLS FIRST, ur.cuadrilla_id NULLS FIRST;

-- name: QuitarRolesDeUsuario :exec
DELETE FROM nucleo.usuarios_roles WHERE usuario_id = @usuario_id;

-- name: AsignarRol :exec
INSERT INTO nucleo.usuarios_roles (usuario_id, rol_id, zona_id, cuadrilla_id)
VALUES (@usuario_id, @rol_id, @zona_id, @cuadrilla_id);

-- name: RolesPorCodigos :many
SELECT * FROM nucleo.roles WHERE codigo = ANY(@codigos::text[]) ORDER BY codigo;

-- name: ListarRoles :many
SELECT * FROM nucleo.roles WHERE id > @despues_de ORDER BY id LIMIT @limite;

-- name: PermisosDeRoles :many
SELECT rol_id, permiso FROM nucleo.roles_permisos
WHERE rol_id = ANY(@roles::bigint[])
ORDER BY rol_id, permiso;

-- name: PermisosDeUsuario :many
SELECT rp.permiso, ur.zona_id, ur.cuadrilla_id
FROM nucleo.usuarios_roles ur
JOIN nucleo.roles_permisos rp ON rp.rol_id = ur.rol_id
WHERE ur.usuario_id = @usuario_id
ORDER BY rp.permiso;

-- name: ListarPermisos :many
SELECT * FROM nucleo.permisos ORDER BY codigo;

-- Semilla del catálogo (iris migrar) -----------------------------------------

-- name: CandadoSemilla :exec
SELECT pg_advisory_xact_lock(@clave::bigint);

-- name: GuardarPermiso :exec
INSERT INTO nucleo.permisos (codigo, descripcion) VALUES (@codigo, @descripcion)
ON CONFLICT (codigo) DO UPDATE SET descripcion = excluded.descripcion, updated_at = now()
WHERE nucleo.permisos.descripcion <> excluded.descripcion;

-- name: QuitarPermisosFueraDeCatalogo :execrows
DELETE FROM nucleo.permisos WHERE NOT (codigo = ANY(@catalogo::text[]));

-- name: GuardarRolSistema :one
INSERT INTO nucleo.roles (codigo, nombre, descripcion, sistema)
VALUES (@codigo, @nombre, @descripcion, true)
ON CONFLICT (codigo) DO UPDATE
SET nombre = excluded.nombre, descripcion = excluded.descripcion, sistema = true, updated_at = now()
RETURNING id;

-- name: QuitarPermisosDeRol :exec
DELETE FROM nucleo.roles_permisos WHERE rol_id = @rol_id AND NOT (permiso = ANY(@permisos::text[]));

-- name: AgregarPermisosARol :exec
INSERT INTO nucleo.roles_permisos (rol_id, permiso)
SELECT @rol_id, p FROM unnest(@permisos::text[]) AS p
ON CONFLICT DO NOTHING;

-- Sesiones -------------------------------------------------------------------

-- name: CrearSesion :one
INSERT INTO nucleo.sesiones (usuario_id, token_hash, csrf_hash, mfa_pendiente, ip, user_agent, expira_en, origen)
VALUES (@usuario_id, @token_hash, @csrf_hash, @mfa_pendiente, @ip, @user_agent, @expira_en, @origen)
RETURNING *;

-- name: SesionVigentePorHash :one
-- reautenticada: confirmó la contraseña hace menos de @ventana_reautenticacion_seg (reloj de la base).
SELECT sqlc.embed(s), sqlc.embed(u),
       (s.reautenticada_en IS NOT NULL AND s.reautenticada_en > now() - make_interval(secs => @ventana_reautenticacion_seg::integer))::boolean AS reautenticada
FROM nucleo.sesiones s
JOIN nucleo.usuarios u ON u.id = s.usuario_id
WHERE s.token_hash = @token_hash AND s.revocada_en IS NULL AND s.expira_en > now() AND u.activo;

-- name: TocarSesion :exec
UPDATE nucleo.sesiones SET ultimo_uso = now()
WHERE id = @id AND ultimo_uso < now() - interval '1 minute';

-- name: CompletarMFASesion :execrows
UPDATE nucleo.sesiones SET mfa_pendiente = false, updated_at = now()
WHERE id = @id AND mfa_pendiente AND revocada_en IS NULL;

-- name: RevocarSesion :execrows
UPDATE nucleo.sesiones SET revocada_en = now(), updated_at = now()
WHERE id = @id AND revocada_en IS NULL;

-- name: RevocarSesionesDeUsuario :execrows
UPDATE nucleo.sesiones SET revocada_en = now(), updated_at = now()
WHERE usuario_id = @usuario_id AND revocada_en IS NULL;

-- Tokens de API --------------------------------------------------------------

-- name: CrearToken :one
INSERT INTO nucleo.tokens_api (usuario_id, nombre, prefijo, hash, permisos, expira_en, creado_por)
VALUES (@usuario_id, @nombre, @prefijo, @hash, @permisos, @expira_en, @creado_por)
RETURNING *;

-- name: TokenVigentePorHash :one
SELECT sqlc.embed(t), sqlc.embed(u)
FROM nucleo.tokens_api t
JOIN nucleo.usuarios u ON u.id = t.usuario_id
WHERE t.hash = @hash AND t.revocado_en IS NULL AND (t.expira_en IS NULL OR t.expira_en > now()) AND u.activo;

-- name: TocarToken :exec
UPDATE nucleo.tokens_api SET ultimo_uso = now()
WHERE id = @id AND (ultimo_uso IS NULL OR ultimo_uso < now() - interval '1 minute');

-- name: ListarTokens :many
-- Sin @todas, solo los tokens propios y los de usuarios que comparten alguna empresa (@empresas).
SELECT * FROM nucleo.tokens_api t
WHERE t.id > @despues_de AND (sqlc.narg(usuario_id)::bigint IS NULL OR t.usuario_id = sqlc.narg(usuario_id))
  AND (@todas::boolean OR t.usuario_id = @yo
       OR EXISTS (SELECT 1 FROM nucleo.usuarios_empresas ue
                  WHERE ue.usuario_id = t.usuario_id AND ue.empresa_id = ANY(@empresas::bigint[])))
ORDER BY id
LIMIT @limite;

-- name: TokenPorID :one
SELECT * FROM nucleo.tokens_api WHERE id = @id;

-- name: TokenPorIDParaActualizar :one
SELECT * FROM nucleo.tokens_api WHERE id = @id FOR UPDATE;

-- name: RevocarTokensDeUsuario :many
UPDATE nucleo.tokens_api SET revocado_en = now(), updated_at = now()
WHERE usuario_id = @usuario_id AND revocado_en IS NULL
RETURNING id;

-- name: RevocarToken :one
UPDATE nucleo.tokens_api SET revocado_en = now(), updated_at = now()
WHERE id = @id
RETURNING *;

-- Límites de intentos -------------------------------------------------------

-- name: BloqueoLogin :one
SELECT bloqueado_hasta::timestamptz FROM nucleo.intentos_login
WHERE correo = @correo AND bloqueado_hasta > now();

-- name: RegistrarFalloLogin :one
-- Cuenta un fallo; si el último fue antes de @desde, la cuenta empieza de nuevo.
INSERT INTO nucleo.intentos_login (correo, fallos) VALUES (@correo, 1)
ON CONFLICT (correo) DO UPDATE
SET fallos = CASE WHEN nucleo.intentos_login.updated_at < @desde::timestamptz THEN 1
                  ELSE nucleo.intentos_login.fallos + 1 END,
    updated_at = now()
RETURNING fallos;

-- name: FallosVigentes :one
-- Fallos contados desde @desde (0 si no hay o si la cuenta ya venció).
SELECT coalesce((SELECT fallos FROM nucleo.intentos_login
                 WHERE correo = @correo AND updated_at >= @desde::timestamptz), 0)::integer AS fallos;

-- name: BloquearLogin :exec
UPDATE nucleo.intentos_login SET fallos = 0, bloqueado_hasta = @hasta, updated_at = now()
WHERE correo = @correo;

-- name: LimpiarIntentosLogin :exec
DELETE FROM nucleo.intentos_login WHERE correo = @correo;

-- name: PurgarIntentosLogin :execrows
-- Purga (job nucleo.intentos_login_purga): borra hasta @lote filas cuyo último fallo tiene más
-- de @antiguedad_seg segundos y sin bloqueo vigente. Esas filas ya no cuentan (FallosVigentes las
-- ignora y RegistrarFalloLogin empieza de 1), así que borrarlas no cambia ningún límite. El corte
-- usa now() de la base, el mismo reloj que escribe updated_at: un worker con el reloj adelantado
-- no puede borrar fallos que todavía cuentan. SKIP LOCKED: no espera a un login que esté contando
-- un fallo en esa fila.
DELETE FROM nucleo.intentos_login WHERE correo IN (
    SELECT i.correo FROM nucleo.intentos_login i
    WHERE i.updated_at < now() - make_interval(secs => @antiguedad_seg::bigint)
      AND (i.bloqueado_hasta IS NULL OR i.bloqueado_hasta <= now())
    ORDER BY i.updated_at
    LIMIT @lote::integer
    FOR UPDATE SKIP LOCKED);

-- name: FallosMFASesion :one
SELECT mfa_fallos FROM nucleo.sesiones WHERE id = @id;

-- name: RegistrarFalloMFA :one
UPDATE nucleo.sesiones SET mfa_fallos = mfa_fallos + 1, updated_at = now()
WHERE id = @id AND mfa_pendiente AND revocada_en IS NULL
RETURNING mfa_fallos;

-- Empresas y empresa activa ---------------------------------------------------

-- name: EmpresasAccesibles :many
-- Ids de las empresas en que el usuario puede operar: todas con @todas (empresas.todas), si no
-- las de su membresía.
SELECT e.id FROM nucleo.empresas e
WHERE @todas::boolean
   OR EXISTS (SELECT 1 FROM nucleo.usuarios_empresas ue WHERE ue.empresa_id = e.id AND ue.usuario_id = @usuario_id)
ORDER BY e.id;

-- name: HayEmpresas :one
SELECT EXISTS (SELECT 1 FROM nucleo.empresas);

-- name: EmpresasPorIDs :many
SELECT * FROM nucleo.empresas WHERE id = ANY(@ids::bigint[]) ORDER BY nombre, id;

-- name: EmpresaPorID :one
SELECT * FROM nucleo.empresas WHERE id = @id;

-- name: EmpresaPorIDParaActualizar :one
SELECT * FROM nucleo.empresas WHERE id = @id FOR UPDATE;

-- name: ListarEmpresas :many
-- Sin @todas, solo las de @empresas (las del actor).
SELECT * FROM nucleo.empresas
WHERE id > @despues_de AND (@todas::boolean OR id = ANY(@empresas::bigint[]))
ORDER BY id LIMIT @limite;

-- name: CrearEmpresa :one
INSERT INTO nucleo.empresas (id, nombre, nit, origen) VALUES (@id, @nombre, @nit, 'manual')
RETURNING *;

-- name: EditarEmpresa :one
UPDATE nucleo.empresas
SET nombre = @nombre, nit = @nit, version = version + 1, updated_at = now()
WHERE id = @id AND version = @version
RETURNING *;

-- name: EmpresasDeUsuarios :many
SELECT ue.usuario_id, ue.empresa_id
FROM nucleo.usuarios_empresas ue
WHERE ue.usuario_id = ANY(@usuarios::bigint[])
ORDER BY ue.usuario_id, ue.empresa_id;

-- name: AsignarEmpresa :execrows
INSERT INTO nucleo.usuarios_empresas (usuario_id, empresa_id, asignado_por)
VALUES (@usuario_id, @empresa_id, @asignado_por)
ON CONFLICT (usuario_id, empresa_id) DO NOTHING;

-- name: QuitarEmpresa :execrows
DELETE FROM nucleo.usuarios_empresas WHERE usuario_id = @usuario_id AND empresa_id = @empresa_id;

-- name: TieneEmpresasTodasGlobal :one
-- El usuario tiene empresas.todas por un rol sin límite de zona ni cuadrilla.
SELECT EXISTS (
    SELECT 1 FROM nucleo.usuarios_roles ur
    JOIN nucleo.roles_permisos rp ON rp.rol_id = ur.rol_id
    WHERE ur.usuario_id = @usuario_id AND rp.permiso = 'empresas.todas'
      AND ur.zona_id IS NULL AND ur.cuadrilla_id IS NULL
);

-- name: UsuarioEnEmpresa :one
-- El usuario opera en la empresa: la tiene asignada o tiene empresas.todas sin límite (y la
-- empresa está registrada).
SELECT COALESCE(
    EXISTS (SELECT 1 FROM nucleo.usuarios_empresas ue WHERE ue.usuario_id = @usuario_id AND ue.empresa_id = @empresa_id)
    OR (EXISTS (SELECT 1 FROM nucleo.empresas e WHERE e.id = @empresa_id)
        AND EXISTS (SELECT 1 FROM nucleo.usuarios_roles ur
                    JOIN nucleo.roles_permisos rp ON rp.rol_id = ur.rol_id
                    WHERE ur.usuario_id = @usuario_id AND rp.permiso = 'empresas.todas'
                      AND ur.zona_id IS NULL AND ur.cuadrilla_id IS NULL)),
    false)::boolean AS en_empresa;

-- Portal de soporte (actor cliente) -----------------------------------------------

-- name: CrearAccesoPortal :one
INSERT INTO nucleo.portal_accesos (ticket_hash, cliente_id, empresa_id, nombre, emitido_por, token_id, expira_en)
VALUES (@ticket_hash, @cliente_id, @empresa_id, @nombre, @emitido_por, @token_id, @expira_en)
RETURNING *;

-- name: TomarAccesoPortal :one
-- Un solo uso: lo marca usado solo si estaba vigente.
UPDATE nucleo.portal_accesos
SET usado_en = now(), updated_at = now()
WHERE ticket_hash = @ticket_hash AND usado_en IS NULL AND expira_en > now()
RETURNING *;

-- name: CrearSesionPortal :one
INSERT INTO nucleo.portal_sesiones (acceso_id, cliente_id, empresa_id, nombre, token_hash, csrf_hash, ip, user_agent, expira_en)
VALUES (@acceso_id, @cliente_id, @empresa_id, @nombre, @token_hash, @csrf_hash, @ip, @user_agent, @expira_en)
RETURNING *;

-- name: SesionPortalVigentePorHash :one
SELECT * FROM nucleo.portal_sesiones
WHERE token_hash = @token_hash AND revocada_en IS NULL AND expira_en > now();

-- name: SesionPortalPorID :one
SELECT * FROM nucleo.portal_sesiones WHERE id = @id;

-- name: TocarSesionPortal :exec
UPDATE nucleo.portal_sesiones SET ultimo_uso = now()
WHERE id = @id AND ultimo_uso < now() - interval '1 minute';

-- name: RevocarSesionPortal :execrows
UPDATE nucleo.portal_sesiones SET revocada_en = now(), updated_at = now()
WHERE id = @id AND revocada_en IS NULL;

-- name: EmpresasDeClienteArgos :one
-- Empresas del cliente según la instantánea de Argos (las de sus servicios) y si Argos dijo que no
-- tiene servicios. Una fila sin empresas pero con empresa_id (anterior) cuenta como [empresa_id].
-- Sin fila si Iris nunca lo leyó.
SELECT CASE WHEN cardinality(c.empresas) = 0 AND c.empresa_id <> 0 THEN ARRAY[c.empresa_id] ELSE c.empresas END::bigint[] AS empresas,
       c.sin_servicios
FROM integracion.argos_clientes c WHERE c.id = @id;

-- name: PurgarSesionesPortal :execrows
-- Purga (job nucleo.portal_purga): sesiones del portal vencidas o revocadas antes de @hasta, por
-- lotes. Ya no autentican (SesionPortalVigentePorHash exige vigente y sin revocar).
DELETE FROM nucleo.portal_sesiones WHERE id IN (
    SELECT s.id FROM nucleo.portal_sesiones s
    WHERE s.expira_en < @hasta::timestamptz OR s.revocada_en < @hasta::timestamptz
    ORDER BY s.id
    LIMIT @lote::integer
    FOR UPDATE SKIP LOCKED);

-- name: PurgarAccesosPortal :execrows
-- Tickets de acceso vencidos antes de @hasta que ya no tienen sesiones (la llave foránea lo
-- exige). Un ticket así ya no se puede canjear: borrarlo no cambia nada (el canje busca el hash
-- vigente y sin usar).
DELETE FROM nucleo.portal_accesos WHERE id IN (
    SELECT a.id FROM nucleo.portal_accesos a
    WHERE a.expira_en < @hasta::timestamptz
      AND NOT EXISTS (SELECT 1 FROM nucleo.portal_sesiones s WHERE s.acceso_id = a.id)
    ORDER BY a.id
    LIMIT @lote::integer
    FOR UPDATE SKIP LOCKED);

-- name: ConfirmarTokenEntregado :execrows
-- El iris_pat_ de un canje queda sin vencimiento al activar el conector (si sigue vigente).
UPDATE nucleo.tokens_api SET expira_en = NULL
WHERE id = @id AND revocado_en IS NULL AND (expira_en IS NULL OR expira_en > now());

-- Autologin desde Argos (SSO, sso.go) -----------------------------------------------------

-- name: ConectorActivoDeSistema :one
-- El conector activo del sistema y el iris_pat_ que Iris le entregó en el canje.
SELECT id, token_entregado_id FROM integracion.conectores WHERE sistema = @sistema AND estado = 'activo';

-- name: ConectorSigueActivo :one
SELECT (estado = 'activo')::boolean AS activo FROM integracion.conectores WHERE id = @id;

-- name: CrearTicketSSO :one
INSERT INTO nucleo.sso_tickets (conector_id, ticket_hash, usuario_externo, correo, correo_verificado, nombre, destino_tipo, destino_id, emitido_token_id, expira_en)
VALUES (@conector_id, @ticket_hash, @usuario_externo, @correo, @correo_verificado, @nombre, @destino_tipo, @destino_id, @emitido_token_id,
        now() + make_interval(secs => @vigencia_seg::integer))
RETURNING *;

-- name: TomarTicketSSO :one
-- Un solo uso: lo marca canjeado solo si estaba vigente.
UPDATE nucleo.sso_tickets SET canjeado_en = now(), updated_at = now()
WHERE ticket_hash = @ticket_hash AND canjeado_en IS NULL AND expira_en > now()
RETURNING *;

-- name: VinculoSSO :one
SELECT v.id, v.usuario_id, u.activo, u.tipo
FROM nucleo.sso_vinculos v JOIN nucleo.usuarios u ON u.id = v.usuario_id
WHERE v.conector_id = @conector_id AND v.usuario_externo = @usuario_externo;

-- name: PrepararVinculacionSSO :one
UPDATE nucleo.sso_tickets SET vinculacion_hash = @vinculacion_hash,
    vinculacion_expira_en = now() + make_interval(secs => @vigencia_seg::integer), updated_at = now()
WHERE id = @id
RETURNING vinculacion_expira_en::timestamptz;

-- name: VerTicketSSO :one
SELECT id, conector_id, usuario_externo, nombre, destino_tipo, destino_id FROM nucleo.sso_tickets
WHERE ticket_hash = @ticket_hash AND canjeado_en IS NULL AND expira_en > now();

-- name: BloquearEmisionSSO :exec
-- Serializa la emisión por conector (el tope se cuenta después, sin carreras).
SELECT pg_advisory_xact_lock(1937011567, @conector_id::integer);

-- name: TicketsSSORecientes :one
-- Tickets emitidos por el conector en la ventana, id_externo distintos y edad del más viejo.
SELECT count(id)::integer AS emitidos,
       count(DISTINCT usuario_externo)::integer AS externos,
       coalesce(floor(extract(epoch FROM (now() - min(created_at)))), 0)::integer AS edad_seg
FROM nucleo.sso_tickets
WHERE conector_id = @conector_id AND created_at > now() - make_interval(secs => @ventana_seg::integer);

-- name: MarcarSesionReautenticada :execrows
UPDATE nucleo.sesiones SET reautenticada_en = now(), updated_at = now()
WHERE id = @id AND revocada_en IS NULL AND NOT mfa_pendiente;

-- name: BorrarVinculosSSODeUsuario :many
DELETE FROM nucleo.sso_vinculos WHERE usuario_id = @usuario_id RETURNING *;

-- name: SistemaDeConector :one
SELECT sistema FROM integracion.conectores WHERE id = @id;

-- name: TomarVinculacionSSO :one
-- La vinculación pendiente se usa una sola vez y dentro de su plazo.
UPDATE nucleo.sso_tickets SET vinculado_en = now(), updated_at = now()
WHERE vinculacion_hash = @vinculacion_hash AND vinculado_en IS NULL AND vinculacion_expira_en > now()
RETURNING *;

-- name: SesionCreadaEn :one
SELECT created_at FROM nucleo.sesiones WHERE id = @id;

-- name: VinculoSSODeUsuarioEnConector :one
SELECT id, usuario_externo FROM nucleo.sso_vinculos WHERE conector_id = @conector_id AND usuario_id = @usuario_id;

-- name: CrearVinculoSSO :one
INSERT INTO nucleo.sso_vinculos (conector_id, usuario_externo, usuario_id, ticket_id)
VALUES (@conector_id, @usuario_externo, @usuario_id, @ticket_id)
RETURNING *;

-- name: VinculosSSODeUsuario :many
SELECT v.id, v.conector_id, c.sistema, v.usuario_externo, v.created_at
FROM nucleo.sso_vinculos v JOIN integracion.conectores c ON c.id = v.conector_id
WHERE v.usuario_id = @usuario_id ORDER BY v.id;

-- name: VinculoSSOParaBorrar :one
SELECT * FROM nucleo.sso_vinculos WHERE id = @id AND usuario_id = @usuario_id FOR UPDATE;

-- name: BorrarVinculoSSO :exec
DELETE FROM nucleo.sso_vinculos WHERE id = @id;

-- name: PurgarTicketsSSO :execrows
-- Purga (job nucleo.portal_purga): tickets de autologin vencidos antes de @hasta (y su
-- vinculación pendiente, si la hubo), por lotes.
DELETE FROM nucleo.sso_tickets WHERE id IN (
    SELECT t.id FROM nucleo.sso_tickets t
    WHERE t.expira_en < @hasta::timestamptz
      AND (t.vinculacion_expira_en IS NULL OR t.vinculacion_expira_en < @hasta::timestamptz)
    ORDER BY t.id
    LIMIT @lote::integer
    FOR UPDATE SKIP LOCKED);

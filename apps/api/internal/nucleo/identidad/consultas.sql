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
INSERT INTO nucleo.sesiones (usuario_id, token_hash, csrf_hash, mfa_pendiente, ip, user_agent, expira_en)
VALUES (@usuario_id, @token_hash, @csrf_hash, @mfa_pendiente, @ip, @user_agent, @expira_en)
RETURNING *;

-- name: SesionVigentePorHash :one
SELECT sqlc.embed(s), sqlc.embed(u)
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

-- name: BloquearLogin :exec
UPDATE nucleo.intentos_login SET fallos = 0, bloqueado_hasta = @hasta, updated_at = now()
WHERE correo = @correo;

-- name: LimpiarIntentosLogin :exec
DELETE FROM nucleo.intentos_login WHERE correo = @correo;

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

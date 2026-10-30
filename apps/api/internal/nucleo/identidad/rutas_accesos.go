package identidad

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
)

// Rutas del historial de accesos, las sesiones activas, la actividad de la app de campo y los
// roles propios. Handlers delgados, como rutas.go.

type entradaAccesosUsuario struct {
	ID int64 `path:"id" minimum:"1"`
	EntradaListarAccesos
}

type entradaCerrarSesion struct {
	nucleo.ConIdempotencia
	ID       int64 `path:"id" minimum:"1"`
	SesionID int64 `path:"sesionId" minimum:"1"`
	Body     EntradaCerrarSesionUsuario
}

const descReautenticacion = " Si el rol incluye un permiso sensible (`sensible` en `GET /api/v1/permisos`), exige haber confirmado la contraseña en los últimos 15 min (`POST /api/v1/sesion/reautenticar`): si no, 403 `reautenticacion_requerida`; desde un token, 403 `requiere_sesion`."

func (m *Modulo) registrarAccesosYRoles(api huma.API) {
	// Historial de accesos ----------------------------------------------------------------
	accesos := operacion("listarAccesos", http.MethodGet, "/api/v1/accesos", "Historial de accesos", nucleo.PermAccesosVer, http.StatusOK, http.StatusNotFound)
	accesos.Description += " Del más reciente al más antiguo: inicios de sesión (panel, app de campo PWA o nativa, autologin desde Argos) y uso de tokens (una fila por token y hora), con IP, dispositivo y resultado (`exito`, `contrasena_erronea`, `mfa_fallido`, `bloqueado`, `inactivo`). Un intento con un correo que no existe aparece sin usuario (el correo no se guarda). " + descPorEmpresa + " Los intentos sin usuario solo los ve quien opera en todas las empresas. Retención: `IRIS_ACCESOS_RETENCION_DIAS` (180 por defecto)."
	huma.Register(api, accesos, func(ctx context.Context, in *EntradaListarAccesos) (*cuerpo[nucleo.Pagina[Acceso]], error) {
		return responder(m.ListarAccesos.Ejecutar(ctx, *in))
	})

	accesosUsuario := operacion("listarAccesosUsuario", http.MethodGet, "/api/v1/usuarios/{id}/accesos", "Historial de accesos de un usuario", nucleo.PermAccesosVer, http.StatusOK, http.StatusNotFound)
	accesosUsuario.Description += " Los mismos filtros que `GET /api/v1/accesos`. Un usuario fuera de tu alcance de empresas responde 404."
	huma.Register(api, accesosUsuario, func(ctx context.Context, in *entradaAccesosUsuario) (*cuerpo[nucleo.Pagina[Acceso]], error) {
		e := in.EntradaListarAccesos
		e.UsuarioID = in.ID
		return responder(m.ListarAccesos.Ejecutar(ctx, e))
	})

	sesiones := operacion("listarSesionesUsuario", http.MethodGet, "/api/v1/usuarios/{id}/sesiones", "Sesiones activas de un usuario", nucleo.PermAccesosVer, http.StatusOK, http.StatusNotFound)
	sesiones.Description += " Las abiertas y vigentes (no las pendientes de MFA), de la más usada a la menos. La lista viene completa (`nextCursor` null)."
	huma.Register(api, sesiones, func(ctx context.Context, in *EntradaSesionesUsuario) (*cuerpo[nucleo.Pagina[SesionActiva]], error) {
		return responder(m.ListarSesionesUsuario.Ejecutar(ctx, *in))
	})

	cerrar := operacion("cerrarSesionUsuario", http.MethodPost, "/api/v1/usuarios/{id}/sesiones/{sesionId}/cerrar", "Cerrar una sesión de un usuario", nucleo.PermUsuariosCerrarSesiones, http.StatusOK, http.StatusNotFound)
	cerrar.Description += " Quien la usaba debe volver a entrar. Exige tener sin límite de alcance los permisos del usuario (salvo el propio). 404 si la sesión no es suya, ya se cerró o venció. Emite `usuario.sesiones_cerradas`."
	huma.Register(api, cerrar, func(ctx context.Context, in *entradaCerrarSesion) (*cuerpo[SesionesCerradas], error) {
		e := in.Body
		e.UsuarioID, e.SesionID = in.ID, in.SesionID
		return responder(m.CerrarSesionUsuario.Ejecutar(ctx, e))
	})

	cerrarTodas := operacion("cerrarSesionesUsuario", http.MethodPost, "/api/v1/usuarios/{id}/cerrar-sesiones", "Cerrar todas las sesiones de un usuario", nucleo.PermUsuariosCerrarSesiones, http.StatusOK, http.StatusNotFound)
	cerrarTodas.Description += " Las propias: todas menos la que hace la petición. Exige tener sin límite de alcance los permisos del usuario (salvo el propio). Sin sesiones abiertas responde `cerradas: 0` sin evento. Emite `usuario.sesiones_cerradas`."
	huma.Register(api, cerrarTodas, func(ctx context.Context, in *conID[EntradaCerrarSesionesUsuario]) (*cuerpo[SesionesCerradas], error) {
		e := in.Body
		e.UsuarioID = in.ID
		return responder(m.CerrarSesionesUsuario.Ejecutar(ctx, e))
	})

	// Actividad de la app de campo ------------------------------------------------------
	actividad := operacion("actividadCampo", http.MethodGet, "/api/v1/actividad-campo", "Actividad de la app de campo por técnico", nucleo.PermCampoVerActividad, http.StatusOK)
	actividad.Description += " Técnicos activos de la empresa activa (según el alcance por zona o cuadrilla) con su última conexión, último `POST /sync/lote`, operaciones rechazadas, pendientes que informa la app (`X-Iris-Pendientes`), versión y plataforma (`X-Iris-App: campo/<versión> (<plataforma>)`) y última posición. `estado`: `sin_conexion` sin conexión en `sinConexionMin`; `atrasado` con pendientes y sin sincronizar en `atrasoMin`; si no, `al_dia`. La lista viene completa (`nextCursor` null)."
	huma.Register(api, actividad, func(ctx context.Context, in *EntradaActividadCampo) (*cuerpo[ActividadCampo], error) {
		return responder(m.ActividadCampo.Ejecutar(ctx, *in))
	})

	// Roles propios ---------------------------------------------------------------------
	huma.Register(api, operacion("verRol", http.MethodGet, "/api/v1/roles/{id}", "Ver un rol con sus permisos", nucleo.PermRolesVer, http.StatusOK, http.StatusNotFound),
		func(ctx context.Context, in *EntradaVerRol) (*cuerpo[Rol], error) {
			return responder(m.VerRol.Ejecutar(ctx, *in))
		})

	crear := operacion("crearRol", http.MethodPost, "/api/v1/roles", "Crear un rol propio", nucleo.PermRolesCrear, http.StatusCreated, http.StatusConflict)
	crear.Description += " Con al menos un permiso; solo los que tienes sin límite de alcance (403). 409 `rol_existe` si el código ya está en uso. Emite `rol.creado`." + descReautenticacion
	huma.Register(api, crear, func(ctx context.Context, in *conClave[EntradaCrearRol]) (*cuerpo[Rol], error) {
		return responder(m.CrearRol.Ejecutar(ctx, in.Body))
	})

	editar := operacion("editarRol", http.MethodPost, "/api/v1/roles/{id}/editar", "Editar un rol propio", nucleo.PermRolesEditar, http.StatusOK, http.StatusNotFound, http.StatusConflict)
	editar.Description += " Nombre, descripción y permisos (reemplaza la lista). Un rol base responde 422 `rol_base`. Exige tener sin límite de alcance sus permisos de antes y de después; con usuarios asignados, operar en todas las empresas. Emite `rol.actualizado`. La re-autenticación se pide si agrega un permiso sensible." + descReautenticacion
	huma.Register(api, editar, func(ctx context.Context, in *conID[EntradaEditarRol]) (*cuerpo[Rol], error) {
		e := in.Body
		e.ID = in.ID
		return responder(m.EditarRol.Ejecutar(ctx, e))
	})

	desactivar := operacion("desactivarRol", http.MethodPost, "/api/v1/roles/{id}/desactivar", "Desactivar un rol propio", nucleo.PermRolesDesactivar, http.StatusOK, http.StatusNotFound, http.StatusConflict)
	desactivar.Description += " Deja de dar permisos a quienes lo tienen (las asignaciones se conservan) y no se puede asignar. Los roles no se borran; un rol base responde 422 `rol_base`. Emite `rol.desactivado`."
	huma.Register(api, desactivar, func(ctx context.Context, in *conID[EntradaCambiarActivoRol]) (*cuerpo[Rol], error) {
		e := in.Body
		e.ID = in.ID
		return responder(m.DesactivarRol.Ejecutar(ctx, e))
	})

	activar := operacion("activarRol", http.MethodPost, "/api/v1/roles/{id}/activar", "Volver a activar un rol propio", nucleo.PermRolesActivar, http.StatusOK, http.StatusNotFound, http.StatusConflict)
	activar.Description += " Devuelve sus permisos a quienes lo tienen asignado. Emite `rol.activado`." + descReautenticacion
	huma.Register(api, activar, func(ctx context.Context, in *conID[EntradaCambiarActivoRol]) (*cuerpo[Rol], error) {
		e := in.Body
		e.ID = in.ID
		return responder(m.ActivarRol.Ejecutar(ctx, e))
	})

	asignar := operacion("asignarRolUsuario", http.MethodPost, "/api/v1/usuarios/{id}/asignar-rol", "Asignar un rol a un usuario", nucleo.PermUsuariosAsignarRol, http.StatusOK, http.StatusNotFound, http.StatusConflict)
	asignar.Description += " Con alcance opcional por zona (`zonaId`) o por cuadrilla (`cuadrillaId`), no ambos. Solo roles activos (422 `rol_inactivo`) cuyos permisos tienes sin límite de alcance (403). 422 `rol_ya_asignado`. Responde el usuario con sus roles. Emite `usuario.rol_asignado`. " + descAdminPorEmpresa + descReautenticacion
	huma.Register(api, asignar, func(ctx context.Context, in *conID[AsignacionRol]) (*cuerpo[Usuario], error) {
		return responder(m.AsignarRolUsuario.Ejecutar(ctx, EntradaRolUsuario{UsuarioID: in.ID, AsignacionRol: in.Body}))
	})

	quitar := operacion("quitarRolUsuario", http.MethodPost, "/api/v1/usuarios/{id}/quitar-rol", "Quitar un rol a un usuario", nucleo.PermUsuariosQuitarRol, http.StatusOK, http.StatusNotFound, http.StatusConflict)
	quitar.Description += " La asignación exacta (rol y alcance). Exige tener sin límite de alcance los permisos del usuario (salvo el propio). 422 `rol_no_asignado`. Emite `usuario.rol_quitado`. " + descAdminPorEmpresa
	huma.Register(api, quitar, func(ctx context.Context, in *conID[AsignacionRol]) (*cuerpo[Usuario], error) {
		return responder(m.QuitarRolUsuario.Ejecutar(ctx, EntradaRolUsuario{UsuarioID: in.ID, AsignacionRol: in.Body}))
	})
}

package identidad

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
	identidadbd "github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad/bd"
)

// Roles propios y su asignación (CLAUDE.md §8). Reglas:
//   - Los roles base (sistema) los mantiene la semilla: no se editan, no se desactivan y nunca se
//     borra ningún rol (un rol propio se desactiva: deja de dar permisos y de poder asignarse).
//   - Nadie da más de lo que tiene: crear, editar, activar o desactivar un rol exige tener sin
//     límite de alcance cada uno de sus permisos (los de antes y los de después).
//   - Un rol asignado a usuarios se edita, activa o desactiva solo con alcance de todas las
//     empresas (cambia los permisos de usuarios de cualquier empresa).
//   - Dar un permiso sensible (nucleo.PermisosSensibles) exige haber confirmado la contraseña en
//     los últimos 15 min (POST /api/v1/sesion/reautenticar) desde una sesión del panel; un token
//     no puede darlo (403 requiere_sesion).

// Rol base o propio: código en minúsculas, sin espacios.
var reCodigoRol = regexp.MustCompile(`^[a-z][a-z0-9_]{1,39}$`)

// exigirReautenticacionSensibles: si permisos incluye alguno sensible, el actor debe haber
// confirmado su contraseña hace poco. El sistema (consola, semilla) y una llamada interna sin
// sesión no pasan por HTTP y no se piden.
func exigirReautenticacionSensibles(ctx context.Context, permisos []string, que string) error {
	var sensibles []string
	for _, p := range permisos {
		if nucleo.EsSensible(nucleo.Permiso(p)) || p == string(nucleo.PermEmpresasTodas) {
			sensibles = append(sensibles, p)
		}
	}
	if len(sensibles) == 0 {
		return nil
	}
	slices.Sort(sensibles)
	sensibles = slices.Compact(sensibles)
	a, s := contexto.ActorDe(ctx), contexto.SesionDe(ctx)
	switch {
	case a.Tipo == contexto.ActorSistema:
		return nil
	case a.Tipo != contexto.ActorUsuario:
		return nucleo.NuevoProblema(http.StatusForbidden, "requiere_sesion", "Requiere una sesión del panel",
			fmt.Sprintf("%s da permisos sensibles (%s): solo desde una sesión del panel, confirmando la contraseña", que, strings.Join(sensibles, ", ")))
	case s.ID == 0 || s.Reautenticada:
		return nil
	}
	return nucleo.NuevoProblema(http.StatusForbidden, "reautenticacion_requerida", "Re-autenticación requerida",
		fmt.Sprintf("%s da permisos sensibles (%s): confirma tu contraseña (POST /api/v1/sesion/reautenticar) y vuelve a intentarlo", que, strings.Join(sensibles, ", ")))
}

// exigirTenerPermisos: quien actúa tiene sin límite de alcance cada permiso.
func exigirTenerPermisos(ctx context.Context, permisos []string, que string) error {
	quien := contexto.PermisosDe(ctx)
	for _, p := range permisos {
		if !quien.TieneGlobal(p) {
			return nucleo.Prohibido(fmt.Sprintf("No puedes %s: incluye el permiso %s, que no tienes sin límite de alcance", que, p))
		}
	}
	return nil
}

// exigirRolPropio: un rol base no se edita ni se desactiva.
func exigirRolPropio(r identidadbd.NucleoRole) error {
	if r.Sistema {
		return nucleo.ReglaNegocio("rol_base", "El rol "+r.Codigo+" es un rol base: lo mantiene el sistema y no se edita ni se desactiva")
	}
	return nil
}

// exigirAlcanceSiAsignado: cambiar un rol con usuarios exige alcance de todas las empresas.
func exigirAlcanceSiAsignado(ctx context.Context, q *identidadbd.Queries, rolID int64, que string) error {
	usos, err := q.UsuariosPorRol(ctx, []int64{rolID})
	if err != nil || len(usos) == 0 || usos[0].Usuarios == 0 {
		return err
	}
	a, err := alcanceDelActor(ctx, q)
	if err != nil || a.todas {
		return err
	}
	return nucleo.Prohibido(fmt.Sprintf("No puedes %s: lo tienen %d usuarios y solo quien opera en todas las empresas cambia un rol asignado", que, usos[0].Usuarios))
}

func permisosDeRol(ctx context.Context, q *identidadbd.Queries, rolID int64) ([]string, error) {
	filas, err := q.PermisosDeRoles(ctx, []int64{rolID})
	if err != nil {
		return nil, err
	}
	salida := make([]string, 0, len(filas))
	for _, f := range filas {
		salida = append(salida, f.Permiso)
	}
	return salida, nil
}

// aRol arma el rol con sus permisos y cuántos usuarios lo tienen.
func aRol(r identidadbd.NucleoRole, permisos []string, usuarios int32) Rol {
	ps := make([]nucleo.Permiso, 0, len(permisos))
	for _, p := range permisos {
		ps = append(ps, nucleo.Permiso(p))
	}
	return Rol{ID: r.ID, Codigo: r.Codigo, Nombre: r.Nombre, Descripcion: r.Descripcion, Sistema: r.Sistema,
		Activo: r.Activo, Permisos: ps, Usuarios: usuarios, Version: r.Version}
}

func rolCompleto(ctx context.Context, q *identidadbd.Queries, r identidadbd.NucleoRole) (Rol, error) {
	permisos, err := permisosDeRol(ctx, q, r.ID)
	if err != nil {
		return Rol{}, err
	}
	usos, err := q.UsuariosPorRol(ctx, []int64{r.ID})
	if err != nil {
		return Rol{}, err
	}
	var n int32
	if len(usos) > 0 {
		n = usos[0].Usuarios
	}
	return aRol(r, permisos, n), nil
}

// validarPermisos: del catálogo, sin repetir, al menos uno. Devuelve los códigos ordenados.
func validarPermisos(campo string, permisos []nucleo.Permiso) ([]string, error) {
	if len(permisos) == 0 {
		return nil, nucleo.Validacion(nucleo.ErrorCampo{Field: campo, Message: "Elige al menos un permiso", Rule: "requerido"})
	}
	salida := make([]string, 0, len(permisos))
	var errores []nucleo.ErrorCampo
	for i, p := range permisos {
		if !nucleo.EsPermiso(string(p)) {
			errores = append(errores, nucleo.ErrorCampo{Field: fmt.Sprintf("%s[%d]", campo, i), Message: "El permiso " + string(p) + " no existe", Rule: "existe"})
			continue
		}
		salida = append(salida, string(p))
	}
	if len(errores) > 0 {
		return nil, nucleo.Validacion(errores...)
	}
	slices.Sort(salida)
	return slices.Compact(salida), nil
}

func diferencia(a, b []string) []string {
	salida := []string{}
	for _, x := range a {
		if !slices.Contains(b, x) {
			salida = append(salida, x)
		}
	}
	return salida
}

type rolAuditable struct {
	Nombre      string   `json:"nombre"`
	Descripcion string   `json:"descripcion"`
	Activo      bool     `json:"activo"`
	Permisos    []string `json:"permisos"`
}

// Ver ------------------------------------------------------------------------------------

// EntradaVerRol es el id del rol.
type EntradaVerRol struct {
	ID int64 `path:"id" minimum:"1"`
}

func nuevoVerRol() *auditoria.Caso[EntradaVerRol, Rol] {
	return auditoria.Nuevo(auditoria.Caso[EntradaVerRol, Rol]{
		Accion: "roles.ver", Permiso: nucleo.PermRolesVer, SinEmpresa: true, AuditarExito: soloLecturas[Rol],
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaVerRol) (Rol, error) {
			q := identidadbd.New(t)
			r, err := q.RolPorID(ctx, in.ID)
			if err != nil {
				return Rol{}, err
			}
			return rolCompleto(ctx, q, r)
		},
	})
}

// Crear ----------------------------------------------------------------------------------

// EntradaCrearRol es un rol propio nuevo.
type EntradaCrearRol struct {
	Codigo      string           `json:"codigo" minLength:"2" maxLength:"40" pattern:"^[a-z][a-z0-9_]{1,39}$" doc:"Identificador en minúsculas (p. ej. despacho_norte); no cambia"`
	Nombre      string           `json:"nombre" minLength:"1" maxLength:"80"`
	Descripcion string           `json:"descripcion,omitempty" maxLength:"300"`
	Permisos    []nucleo.Permiso `json:"permisos" minItems:"1" maxItems:"300"`
}

func nuevoCrearRol() *auditoria.Caso[EntradaCrearRol, Rol] {
	return auditoria.Nuevo(auditoria.Caso[EntradaCrearRol, Rol]{
		Accion: "roles.crear", Permiso: nucleo.PermRolesCrear, SinEmpresa: true,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaCrearRol) (Rol, error) {
			q := identidadbd.New(t)
			codigo := strings.TrimSpace(in.Codigo)
			nombre := strings.TrimSpace(in.Nombre)
			var errores []nucleo.ErrorCampo
			if !reCodigoRol.MatchString(codigo) {
				errores = append(errores, nucleo.ErrorCampo{Field: "codigo", Message: "Solo minúsculas, números y guion bajo; empieza con letra", Rule: "formato"})
			}
			if nombre == "" {
				errores = append(errores, nucleo.ErrorCampo{Field: "nombre", Message: "Escribe un nombre", Rule: "requerido"})
			}
			if len(errores) > 0 {
				return Rol{}, nucleo.Validacion(errores...)
			}
			permisos, err := validarPermisos("permisos", in.Permisos)
			if err != nil {
				return Rol{}, err
			}
			if err := exigirTenerPermisos(ctx, permisos, "crear este rol"); err != nil {
				return Rol{}, err
			}
			if err := exigirReautenticacionSensibles(ctx, permisos, "Crear este rol"); err != nil {
				return Rol{}, err
			}
			r, err := q.CrearRol(ctx, identidadbd.CrearRolParams{Codigo: codigo, Nombre: nombre, Descripcion: strings.TrimSpace(in.Descripcion)})
			if errors.Is(err, pgx.ErrNoRows) {
				return Rol{}, nucleo.Conflicto("rol_existe", "Ya existe un rol con el código "+codigo)
			}
			if err != nil {
				return Rol{}, err
			}
			t.FijarEntidad("rol", r.ID)
			if err := q.AgregarPermisosARol(ctx, identidadbd.AgregarPermisosARolParams{RolID: r.ID, Permisos: permisos}); err != nil {
				return Rol{}, err
			}
			t.RegistrarCambios("rol", r.ID, nil, rolAuditable{Nombre: r.Nombre, Descripcion: r.Descripcion, Activo: r.Activo, Permisos: permisos})
			if err := t.Emitir("rol.creado", map[string]any{"rol_id": r.ID, "codigo": r.Codigo, "permisos": permisos}); err != nil {
				return Rol{}, err
			}
			return aRol(r, permisos, 0), nil
		},
	})
}

// Editar ---------------------------------------------------------------------------------

// EntradaEditarRol: los campos ausentes no cambian; permisos, si viene, reemplaza todos.
type EntradaEditarRol struct {
	ID          int64             `json:"-"`
	Version     int32             `json:"version" minimum:"1" doc:"Versión leída; si cambió, 409"`
	Nombre      *string           `json:"nombre,omitempty" minLength:"1" maxLength:"80"`
	Descripcion *string           `json:"descripcion,omitempty" maxLength:"300"`
	Permisos    *[]nucleo.Permiso `json:"permisos,omitempty" minItems:"1" maxItems:"300"`
}

func nuevoEditarRol() *auditoria.Caso[EntradaEditarRol, Rol] {
	return auditoria.Nuevo(auditoria.Caso[EntradaEditarRol, Rol]{
		Accion: "roles.editar", Permiso: nucleo.PermRolesEditar, SinEmpresa: true,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaEditarRol) (Rol, error) {
			q := identidadbd.New(t)
			t.FijarEntidad("rol", in.ID)
			antes, err := q.RolPorIDParaActualizar(ctx, in.ID)
			if err != nil {
				return Rol{}, err
			}
			if err := exigirRolPropio(antes); err != nil {
				return Rol{}, err
			}
			if err := nucleo.ExigirVersion(in.Version, antes.Version, "rol", in.ID); err != nil {
				return Rol{}, err
			}
			previos, err := permisosDeRol(ctx, q, in.ID)
			if err != nil {
				return Rol{}, err
			}
			nuevos := previos
			if in.Permisos != nil {
				if nuevos, err = validarPermisos("permisos", *in.Permisos); err != nil {
					return Rol{}, err
				}
			}
			if err := exigirTenerPermisos(ctx, append(slices.Clone(previos), nuevos...), "editar este rol"); err != nil {
				return Rol{}, err
			}
			if err := exigirAlcanceSiAsignado(ctx, q, in.ID, "editar este rol"); err != nil {
				return Rol{}, err
			}
			agregados, quitados := diferencia(nuevos, previos), diferencia(previos, nuevos)
			if err := exigirReautenticacionSensibles(ctx, agregados, "Editar este rol"); err != nil {
				return Rol{}, err
			}
			nombre, descripcion := antes.Nombre, antes.Descripcion
			if in.Nombre != nil {
				if nombre = strings.TrimSpace(*in.Nombre); nombre == "" {
					return Rol{}, nucleo.Validacion(nucleo.ErrorCampo{Field: "nombre", Message: "Escribe un nombre", Rule: "requerido"})
				}
			}
			if in.Descripcion != nil {
				descripcion = strings.TrimSpace(*in.Descripcion)
			}
			despues, err := q.ActualizarRol(ctx, identidadbd.ActualizarRolParams{ID: in.ID, Version: in.Version, Nombre: nombre, Descripcion: descripcion})
			if err != nil {
				return Rol{}, nucleo.VerificarVersion(err, "rol", in.ID)
			}
			if len(quitados) > 0 {
				if err := q.QuitarPermisosDeRol(ctx, identidadbd.QuitarPermisosDeRolParams{RolID: in.ID, Permisos: nuevos}); err != nil {
					return Rol{}, err
				}
			}
			if len(agregados) > 0 {
				if err := q.AgregarPermisosARol(ctx, identidadbd.AgregarPermisosARolParams{RolID: in.ID, Permisos: agregados}); err != nil {
					return Rol{}, err
				}
			}
			campos := t.RegistrarCambios("rol", in.ID,
				rolAuditable{Nombre: antes.Nombre, Descripcion: antes.Descripcion, Activo: antes.Activo, Permisos: previos},
				rolAuditable{Nombre: despues.Nombre, Descripcion: despues.Descripcion, Activo: despues.Activo, Permisos: nuevos})
			if len(campos) > 0 {
				if err := t.Emitir("rol.actualizado", map[string]any{"rol_id": in.ID, "campos": campos,
					"permisos_agregados": agregados, "permisos_quitados": quitados}); err != nil {
					return Rol{}, err
				}
			}
			return rolCompleto(ctx, q, despues)
		},
	})
}

// Activar y desactivar -------------------------------------------------------------------

// EntradaCambiarActivoRol: versión leída y motivo.
type EntradaCambiarActivoRol struct {
	ID      int64  `json:"-"`
	Version int32  `json:"version" minimum:"1"`
	Motivo  string `json:"motivo,omitempty" maxLength:"200"`
}

func nuevoCambiarActivoRol(activar bool) *auditoria.Caso[EntradaCambiarActivoRol, Rol] {
	accion, permiso, evento, que := "roles.desactivar", nucleo.PermRolesDesactivar, "rol.desactivado", "desactivar este rol"
	if activar {
		accion, permiso, evento, que = "roles.activar", nucleo.PermRolesActivar, "rol.activado", "activar este rol"
	}
	return auditoria.Nuevo(auditoria.Caso[EntradaCambiarActivoRol, Rol]{
		Accion: accion, Permiso: permiso, SinEmpresa: true,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaCambiarActivoRol) (Rol, error) {
			q := identidadbd.New(t)
			t.FijarEntidad("rol", in.ID)
			antes, err := q.RolPorIDParaActualizar(ctx, in.ID)
			if err != nil {
				return Rol{}, err
			}
			if err := exigirRolPropio(antes); err != nil {
				return Rol{}, err
			}
			if err := nucleo.ExigirVersion(in.Version, antes.Version, "rol", in.ID); err != nil {
				return Rol{}, err
			}
			if antes.Activo == activar {
				if activar {
					return Rol{}, nucleo.ReglaNegocio("rol_activo", "El rol ya está activo")
				}
				return Rol{}, nucleo.ReglaNegocio("rol_inactivo", "El rol ya está desactivado")
			}
			permisos, err := permisosDeRol(ctx, q, in.ID)
			if err != nil {
				return Rol{}, err
			}
			if err := exigirTenerPermisos(ctx, permisos, que); err != nil {
				return Rol{}, err
			}
			if err := exigirAlcanceSiAsignado(ctx, q, in.ID, que); err != nil {
				return Rol{}, err
			}
			rol, err := rolCompleto(ctx, q, antes)
			if err != nil {
				return Rol{}, err
			}
			if activar && rol.Usuarios > 0 {
				// Reactivar devuelve sus permisos a quienes lo tienen asignado.
				if err := exigirReautenticacionSensibles(ctx, permisos, "Activar este rol"); err != nil {
					return Rol{}, err
				}
			}
			despues, err := q.CambiarActivoRol(ctx, identidadbd.CambiarActivoRolParams{ID: in.ID, Version: in.Version, Activo: activar})
			if err != nil {
				return Rol{}, nucleo.VerificarVersion(err, "rol", in.ID)
			}
			t.RegistrarCambios("rol", in.ID, map[string]any{"activo": antes.Activo}, map[string]any{"activo": despues.Activo})
			var motivo *string
			if m := strings.TrimSpace(in.Motivo); m != "" {
				motivo = &m
			}
			if err := t.Emitir(evento, map[string]any{"rol_id": in.ID, "codigo": despues.Codigo, "usuarios": rol.Usuarios, "motivo": motivo}); err != nil {
				return Rol{}, err
			}
			rol.Activo, rol.Version = despues.Activo, despues.Version
			return rol, nil
		},
	})
}

// Asignar y quitar a un usuario ----------------------------------------------------------

// EntradaRolUsuario es una asignación (rol y alcance opcional) a un usuario.
type EntradaRolUsuario struct {
	UsuarioID int64 `json:"-"`
	AsignacionRol
}

func nuevoAsignarRolUsuario() *auditoria.Caso[EntradaRolUsuario, Usuario] {
	return auditoria.Nuevo(auditoria.Caso[EntradaRolUsuario, Usuario]{
		Accion: "usuarios.asignar_rol", Permiso: nucleo.PermUsuariosAsignarRol, SinEmpresa: true,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaRolUsuario) (Usuario, error) {
			q := identidadbd.New(t)
			t.FijarEntidad("usuario", in.UsuarioID)
			u, err := q.UsuarioPorIDParaActualizar(ctx, in.UsuarioID)
			if err != nil {
				return Usuario{}, err
			}
			if err := exigirVisible(ctx, q, in.UsuarioID, "asignar roles a este usuario"); err != nil {
				return Usuario{}, err
			}
			resueltos, err := resolverRoles(ctx, q, contexto.PermisosDe(ctx), "rol", []AsignacionRol{in.AsignacionRol})
			if err != nil {
				return Usuario{}, err
			}
			r, err := q.RolPorID(ctx, resueltos[0].id)
			if err != nil {
				return Usuario{}, err
			}
			if !r.Activo {
				return Usuario{}, nucleo.ReglaNegocio("rol_inactivo", "El rol "+r.Codigo+" está desactivado: actívalo antes de asignarlo")
			}
			ya, err := q.ExisteAsignacionRol(ctx, identidadbd.ExisteAsignacionRolParams{UsuarioID: in.UsuarioID, RolID: r.ID, ZonaID: in.ZonaID, CuadrillaID: in.CuadrillaID})
			if err != nil {
				return Usuario{}, err
			}
			if ya {
				return Usuario{}, nucleo.ReglaNegocio("rol_ya_asignado", "El usuario ya tiene ese rol con ese alcance")
			}
			permisos, err := permisosDeRol(ctx, q, r.ID)
			if err != nil {
				return Usuario{}, err
			}
			if err := exigirReautenticacionSensibles(ctx, permisos, "Asignar el rol "+r.Codigo); err != nil {
				return Usuario{}, err
			}
			previos, err := rolesDe(ctx, q, in.UsuarioID)
			if err != nil {
				return Usuario{}, err
			}
			if err := asignarRoles(ctx, q, in.UsuarioID, resueltos); err != nil {
				return Usuario{}, err
			}
			return cerrarCambioRol(ctx, t, q, u, previos[in.UsuarioID], "usuario.rol_asignado", in.AsignacionRol)
		},
	})
}

func nuevoQuitarRolUsuario() *auditoria.Caso[EntradaRolUsuario, Usuario] {
	return auditoria.Nuevo(auditoria.Caso[EntradaRolUsuario, Usuario]{
		Accion: "usuarios.quitar_rol", Permiso: nucleo.PermUsuariosQuitarRol, SinEmpresa: true,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaRolUsuario) (Usuario, error) {
			q := identidadbd.New(t)
			t.FijarEntidad("usuario", in.UsuarioID)
			u, err := q.UsuarioPorIDParaActualizar(ctx, in.UsuarioID)
			if err != nil {
				return Usuario{}, err
			}
			if err := exigirVisible(ctx, q, in.UsuarioID, "quitar roles a este usuario"); err != nil {
				return Usuario{}, err
			}
			// Quitar roles exige superar al usuario (como en usuarios.editar), salvo a uno mismo.
			if a := contexto.ActorDe(ctx); a.Tipo != contexto.ActorUsuario || a.ID != in.UsuarioID {
				if err := exigirSuperior(ctx, q, contexto.PermisosDe(ctx), in.UsuarioID, "quitar roles a este usuario"); err != nil {
					return Usuario{}, err
				}
			}
			roles, err := q.RolesPorCodigos(ctx, []string{in.Rol})
			if err != nil {
				return Usuario{}, err
			}
			if len(roles) == 0 {
				return Usuario{}, nucleo.Validacion(nucleo.ErrorCampo{Field: "rol", Message: "El rol " + in.Rol + " no existe", Rule: "existe"})
			}
			previos, err := rolesDe(ctx, q, in.UsuarioID)
			if err != nil {
				return Usuario{}, err
			}
			n, err := q.QuitarAsignacionRol(ctx, identidadbd.QuitarAsignacionRolParams{UsuarioID: in.UsuarioID, RolID: roles[0].ID, ZonaID: in.ZonaID, CuadrillaID: in.CuadrillaID})
			if err != nil {
				return Usuario{}, err
			}
			if n == 0 {
				return Usuario{}, nucleo.ReglaNegocio("rol_no_asignado", "El usuario no tiene ese rol con ese alcance")
			}
			return cerrarCambioRol(ctx, t, q, u, previos[in.UsuarioID], "usuario.rol_quitado", in.AsignacionRol)
		},
	})
}

// cerrarCambioRol sube la versión del usuario, registra el diff de roles y emite el evento.
func cerrarCambioRol(ctx context.Context, t *auditoria.Tx, q *identidadbd.Queries, u identidadbd.NucleoUsuario,
	previos []AsignacionRol, evento string, a AsignacionRol) (Usuario, error) {
	despues, err := q.SubirVersionUsuario(ctx, identidadbd.SubirVersionUsuarioParams{ID: u.ID, Version: u.Version})
	if err != nil {
		return Usuario{}, nucleo.VerificarVersion(err, "usuario", u.ID)
	}
	roles, err := rolesDe(ctx, q, u.ID)
	if err != nil {
		return Usuario{}, err
	}
	t.RegistrarCambios("usuario", u.ID, auditable(u, previos), auditable(despues, roles[u.ID]))
	if err := t.Emitir(evento, map[string]any{"usuario_id": u.ID, "rol": a.Rol, "zona_id": a.ZonaID, "cuadrilla_id": a.CuadrillaID}); err != nil {
		return Usuario{}, err
	}
	empresas, err := empresasDe(ctx, q, u.ID)
	if err != nil {
		return Usuario{}, err
	}
	return aUsuario(despues, roles[u.ID], empresas[u.ID]), nil
}

// permisosDeAsignaciones son los permisos de los roles de las asignaciones (para la
// re-autenticación de usuarios.crear y usuarios.editar).
func permisosDeAsignaciones(ctx context.Context, q *identidadbd.Queries, rs []rolResuelto) ([]string, error) {
	if len(rs) == 0 {
		return nil, nil
	}
	ids := make([]int64, 0, len(rs))
	for _, r := range rs {
		ids = append(ids, r.id)
	}
	filas, err := q.PermisosDeRoles(ctx, ids)
	if err != nil {
		return nil, err
	}
	salida := make([]string, 0, len(filas))
	for _, f := range filas {
		salida = append(salida, f.Permiso)
	}
	return salida, nil
}

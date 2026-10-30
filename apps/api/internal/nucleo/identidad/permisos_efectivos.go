package identidad

import (
	"context"
	"fmt"
	"slices"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
	identidadbd "github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad/bd"
)

// cargarPermisos arma los permisos efectivos de un usuario a partir de sus roles. Con limite
// (permisos de un token), solo quedan los que están en ambos.
func cargarPermisos(ctx context.Context, q *identidadbd.Queries, usuarioID int64, limite []string) (contexto.Permisos, error) {
	filas, err := q.PermisosDeUsuario(ctx, usuarioID)
	if err != nil {
		return contexto.Permisos{}, err
	}
	mapa := map[string][]contexto.Alcance{}
	for _, f := range filas {
		if limite != nil && !slices.Contains(limite, f.Permiso) {
			continue
		}
		a := contexto.Alcance{}
		if f.ZonaID != nil {
			a.ZonaID = *f.ZonaID
		}
		if f.CuadrillaID != nil {
			a.CuadrillaID = *f.CuadrillaID
		}
		if !slices.Contains(mapa[f.Permiso], a) {
			mapa[f.Permiso] = append(mapa[f.Permiso], a)
		}
	}
	return contexto.NuevosPermisos(mapa), nil
}

// exigirSuperior aplica a un usuario existente la regla de resolverRoles: para tomar control
// de su cuenta (contraseña, correo, tokens), quitarle roles o desactivarlo, quien actúa debe
// tener sin límite de alcance cada permiso actual del objetivo. Así un admin de zona no toma
// la cuenta de un admin global. Además, por empresa (exigirEmpresasDe): sin empresas.todas
// global, solo usuarios cuyas empresas son todas del actor.
func exigirSuperior(ctx context.Context, q *identidadbd.Queries, quien contexto.Permisos, objetivo int64, que string) error {
	if err := exigirEmpresasDe(ctx, q, objetivo, que); err != nil {
		return err
	}
	actuales, err := cargarPermisos(ctx, q, objetivo, nil)
	if err != nil {
		return err
	}
	for _, p := range actuales.Lista() {
		if !quien.TieneGlobal(p) {
			return nucleo.Prohibido(fmt.Sprintf("No puedes %s: el usuario tiene el permiso %s, que no tienes sin límite de alcance", que, p))
		}
	}
	return nil
}

type rolResuelto struct {
	AsignacionRol
	id int64
}

// resolverRoles valida las asignaciones (rol existente, sin duplicados, alcance de zona o de
// cuadrilla pero no ambos) y que quien asigna tenga, sin límite de alcance, cada permiso de
// cada rol: nadie da más de lo que tiene.
//
// Un rol desactivado no se asigna (422), salvo que la asignación ya estuviera en previas (editar
// un usuario conserva lo que tiene).
func resolverRoles(ctx context.Context, q *identidadbd.Queries, quien contexto.Permisos, campo string, roles []AsignacionRol, previas ...AsignacionRol) ([]rolResuelto, error) {
	if len(roles) == 0 {
		return nil, nil
	}
	codigos := make([]string, 0, len(roles))
	var errores []nucleo.ErrorCampo
	vistas := map[string]bool{}
	for i, r := range roles {
		if r.ZonaID != nil && r.CuadrillaID != nil {
			errores = append(errores, nucleo.ErrorCampo{Field: fmt.Sprintf("%s[%d]", campo, i),
				Message: "El alcance es por zona o por cuadrilla, no ambos", Rule: "alcance"})
		}
		if vistas[r.clave()] {
			errores = append(errores, nucleo.ErrorCampo{Field: fmt.Sprintf("%s[%d]", campo, i),
				Message: "Asignación repetida", Rule: "unico"})
		}
		vistas[r.clave()] = true
		codigos = append(codigos, r.Rol)
	}
	filas, err := q.RolesPorCodigos(ctx, codigos)
	if err != nil {
		return nil, err
	}
	porCodigo := map[string]identidadbd.NucleoRole{}
	ids := make([]int64, 0, len(filas))
	for _, f := range filas {
		porCodigo[f.Codigo] = f
		ids = append(ids, f.ID)
	}
	tenia := map[string]bool{}
	for _, p := range previas {
		tenia[p.clave()] = true
	}
	for i, r := range roles {
		f, ok := porCodigo[r.Rol]
		switch {
		case !ok:
			errores = append(errores, nucleo.ErrorCampo{Field: fmt.Sprintf("%s[%d].rol", campo, i),
				Message: "El rol " + r.Rol + " no existe", Rule: "existe"})
		case !f.Activo && !tenia[r.clave()]:
			errores = append(errores, nucleo.ErrorCampo{Field: fmt.Sprintf("%s[%d].rol", campo, i),
				Message: "El rol " + r.Rol + " está desactivado: actívalo antes de asignarlo", Rule: "activo"})
		}
	}
	if len(errores) > 0 {
		return nil, nucleo.Validacion(errores...)
	}

	permisos, err := q.PermisosDeRoles(ctx, ids)
	if err != nil {
		return nil, err
	}
	for _, p := range permisos {
		if !quien.TieneGlobal(p.Permiso) {
			var codigo string
			for c, f := range porCodigo {
				if f.ID == p.RolID {
					codigo = c
				}
			}
			return nil, nucleo.Prohibido(fmt.Sprintf("No puedes asignar el rol %s: incluye el permiso %s, que no tienes sin límite de alcance", codigo, p.Permiso))
		}
	}

	salida := make([]rolResuelto, 0, len(roles))
	for _, r := range roles {
		salida = append(salida, rolResuelto{AsignacionRol: r, id: porCodigo[r.Rol].ID})
	}
	return salida, nil
}

func asignarRoles(ctx context.Context, q *identidadbd.Queries, usuarioID int64, roles []rolResuelto) error {
	for _, r := range roles {
		if err := q.AsignarRol(ctx, identidadbd.AsignarRolParams{
			UsuarioID: usuarioID, RolID: r.id, ZonaID: r.ZonaID, CuadrillaID: r.CuadrillaID,
		}); err != nil {
			return err
		}
	}
	return nil
}

func asignaciones(rs []rolResuelto) []AsignacionRol {
	salida := make([]AsignacionRol, 0, len(rs))
	for _, r := range rs {
		salida = append(salida, r.AsignacionRol)
	}
	return salida
}

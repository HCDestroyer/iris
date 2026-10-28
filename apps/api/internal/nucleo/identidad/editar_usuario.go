package identidad

import (
	"context"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
	identidadbd "github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad/bd"
)

// EntradaEditarUsuario es la entrada de usuarios.editar. Los campos ausentes no cambian; roles,
// si viene, reemplaza todas las asignaciones.
type EntradaEditarUsuario struct {
	ID         int64            `json:"-"`
	Version    int32            `json:"version" minimum:"1" doc:"Versión leída; si cambió, 409"`
	Nombre     *string          `json:"nombre,omitempty" minLength:"1" maxLength:"120"`
	Correo     *string          `json:"correo,omitempty" format:"email" maxLength:"254"`
	Contrasena *string          `json:"contrasena,omitempty" maxLength:"200" doc:"Nueva contraseña; cierra las sesiones abiertas del usuario"`
	Roles      *[]AsignacionRol `json:"roles,omitempty" maxItems:"50"`
}

func nuevoEditarUsuario() *auditoria.Caso[EntradaEditarUsuario, Usuario] {
	return auditoria.Nuevo(auditoria.Caso[EntradaEditarUsuario, Usuario]{
		Accion:     "usuarios.editar",
		Permiso:    nucleo.PermUsuariosEditar,
		SinEmpresa: true, // administración: no es operativa
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaEditarUsuario) (Usuario, error) {
			q := identidadbd.New(t)
			t.FijarEntidad("usuario", in.ID)
			antes, err := q.UsuarioPorIDParaActualizar(ctx, in.ID)
			if err != nil {
				return Usuario{}, err // sin filas → 404
			}
			if err := nucleo.ExigirVersion(in.Version, antes.Version, "usuario", in.ID); err != nil {
				return Usuario{}, err
			}
			rolesPrevios, err := rolesDe(ctx, q, in.ID)
			if err != nil {
				return Usuario{}, err
			}

			nombre, correo := antes.Nombre, antes.Correo
			if in.Nombre != nil {
				nombre = *in.Nombre
			}
			if in.Correo != nil {
				correo = *in.Correo
			}
			nombre, correo, err = normalizarNombreCorreo(nombre, correo)
			if err != nil {
				return Usuario{}, err
			}
			if correo != antes.Correo {
				if otro, err := q.UsuarioPorCorreo(ctx, correo); err == nil && otro.ID != in.ID {
					return Usuario{}, nucleo.Conflicto("correo_en_uso", "Ya existe un usuario con el correo "+correo)
				}
			}
			// Tomar la cuenta (contraseña, correo) o quitar roles exige superar al objetivo; con
			// sesión propia no (un token, aunque sea del mismo usuario, sí: no puede escalar a
			// la sesión completa cambiando la contraseña).
			quitaRoles := in.Roles != nil && quitaAsignaciones(rolesPrevios[in.ID], *in.Roles)
			if in.Contrasena != nil || correo != antes.Correo || quitaRoles {
				actor := contexto.ActorDe(ctx)
				if actor.Tipo != contexto.ActorUsuario || actor.ID != in.ID {
					if err := exigirSuperior(ctx, q, contexto.PermisosDe(ctx), in.ID, "cambiar la contraseña, el correo o quitar roles a este usuario"); err != nil {
						return Usuario{}, err
					}
				}
			}
			hash := antes.ContrasenaHash
			if in.Contrasena != nil {
				if antes.Tipo == "integracion" {
					return Usuario{}, nucleo.Validacion(nucleo.ErrorCampo{Field: "contrasena", Message: "Las integraciones entran solo con token", Rule: "prohibido"})
				}
				if hash, err = hashValidado(*in.Contrasena); err != nil {
					return Usuario{}, err
				}
			}

			roles := rolesPrevios[in.ID]
			if in.Roles != nil {
				resueltos, err := resolverRoles(ctx, q, contexto.PermisosDe(ctx), "roles", *in.Roles)
				if err != nil {
					return Usuario{}, err
				}
				if err := q.QuitarRolesDeUsuario(ctx, in.ID); err != nil {
					return Usuario{}, err
				}
				if err := asignarRoles(ctx, q, in.ID, resueltos); err != nil {
					return Usuario{}, err
				}
				roles = asignaciones(resueltos)
			}

			despues, err := q.ActualizarUsuario(ctx, identidadbd.ActualizarUsuarioParams{
				ID: in.ID, Version: in.Version, Nombre: nombre, Correo: correo, ContrasenaHash: hash,
			})
			if err != nil {
				return Usuario{}, nucleo.VerificarVersion(err, "usuario", in.ID)
			}
			if in.Contrasena != nil {
				if _, err := q.RevocarSesionesDeUsuario(ctx, in.ID); err != nil {
					return Usuario{}, err
				}
			}

			campos := t.RegistrarCambios("usuario", in.ID, auditable(antes, rolesPrevios[in.ID]), auditable(despues, roles))
			if len(campos) > 0 {
				if err := t.Emitir("usuario.actualizado", map[string]any{"usuario_id": in.ID, "campos": campos}); err != nil {
					return Usuario{}, err
				}
			}
			empresas, err := empresasDe(ctx, q, in.ID)
			if err != nil {
				return Usuario{}, err
			}
			return aUsuario(despues, roles, empresas[in.ID]), nil
		},
	})
}

// quitaAsignaciones indica si alguna asignación previa no está en las nuevas.
func quitaAsignaciones(previas, nuevas []AsignacionRol) bool {
	quedan := map[string]bool{}
	for _, r := range nuevas {
		quedan[r.clave()] = true
	}
	for _, r := range previas {
		if !quedan[r.clave()] {
			return true
		}
	}
	return false
}

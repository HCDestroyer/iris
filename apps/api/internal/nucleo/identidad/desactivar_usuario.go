package identidad

import (
	"context"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
	identidadbd "github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad/bd"
)

// EntradaDesactivarUsuario es la entrada de usuarios.desactivar. El usuario no se borra: deja
// de poder entrar y sus sesiones se cierran (sus tokens dejan de valer porque está inactivo).
type EntradaDesactivarUsuario struct {
	ID      int64  `json:"-"`
	Version int32  `json:"version" minimum:"1" doc:"Versión leída; si cambió, 409"`
	Motivo  string `json:"motivo" minLength:"1" maxLength:"500"`
}

func nuevoDesactivarUsuario() *auditoria.Caso[EntradaDesactivarUsuario, Usuario] {
	return auditoria.Nuevo(auditoria.Caso[EntradaDesactivarUsuario, Usuario]{
		Accion:     "usuarios.desactivar",
		Permiso:    nucleo.PermUsuariosDesactivar,
		SinEmpresa: true, // administración: no es operativa
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaDesactivarUsuario) (Usuario, error) {
			q := identidadbd.New(t)
			t.FijarEntidad("usuario", in.ID)
			if actor := contexto.ActorDe(ctx); actor.EsUsuario() && actor.ID == in.ID {
				return Usuario{}, nucleo.ReglaNegocio("autodesactivacion", "No puedes desactivar tu propio usuario")
			}
			antes, err := q.UsuarioPorIDParaActualizar(ctx, in.ID)
			if err != nil {
				return Usuario{}, err
			}
			// Fuera del alcance de empresas: 404, como si no existiera (ni su versión se revela).
			if err := exigirVisible(ctx, q, in.ID, "desactivar a este usuario"); err != nil {
				return Usuario{}, err
			}
			if err := nucleo.ExigirVersion(in.Version, antes.Version, "usuario", in.ID); err != nil {
				return Usuario{}, err
			}
			if !antes.Activo {
				return Usuario{}, nucleo.ReglaNegocio("usuario_inactivo", "El usuario ya está desactivado")
			}
			if err := exigirSuperior(ctx, q, contexto.PermisosDe(ctx), in.ID, "desactivar a este usuario"); err != nil {
				return Usuario{}, err
			}
			despues, err := q.DesactivarUsuario(ctx, identidadbd.DesactivarUsuarioParams{ID: in.ID, Version: in.Version})
			if err != nil {
				return Usuario{}, nucleo.VerificarVersion(err, "usuario", in.ID)
			}
			if _, err := q.RevocarSesionesDeUsuario(ctx, in.ID); err != nil {
				return Usuario{}, err
			}
			if err := borrarVinculosSSO(ctx, t, q, in.ID, "usuario_desactivado"); err != nil {
				return Usuario{}, err
			}
			roles, err := rolesDe(ctx, q, in.ID)
			if err != nil {
				return Usuario{}, err
			}
			t.RegistrarCambios("usuario", in.ID, auditable(antes, roles[in.ID]), auditable(despues, roles[in.ID]))
			if err := t.Emitir("usuario.desactivado", map[string]any{"usuario_id": in.ID, "motivo": in.Motivo}); err != nil {
				return Usuario{}, err
			}
			empresas, err := empresasDe(ctx, q, in.ID)
			if err != nil {
				return Usuario{}, err
			}
			return aUsuario(despues, roles[in.ID], empresas[in.ID]), nil
		},
	})
}

package identidad

import (
	"context"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
	identidadbd "github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad/bd"
)

// EntradaCambiarContrasena es la entrada de POST /api/v1/yo/contrasena: el usuario con sesión
// cambia su propia contraseña. Es lo único que puede hacer (además de ver /yo y cerrar sesión)
// mientras tenga una contraseña temporal.
type EntradaCambiarContrasena struct {
	Actual string `json:"actual" maxLength:"200" auditoria:"sensible" doc:"Contraseña actual (la temporal, si la hay)"`
	Nueva  string `json:"nueva" maxLength:"200" auditoria:"sensible" doc:"Nueva contraseña: mínimo 12 caracteres y distinta de la actual"`
}

func nuevoCambiarContrasena() *auditoria.Caso[EntradaCambiarContrasena, Usuario] {
	return auditoria.Nuevo(auditoria.Caso[EntradaCambiarContrasena, Usuario]{
		Accion: "yo.cambiar_contrasena",
		Acceso: auditoria.Autenticado,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaCambiarContrasena) (Usuario, error) {
			actor := contexto.ActorDe(ctx)
			sesion := contexto.SesionDe(ctx)
			if actor.Tipo != contexto.ActorUsuario || sesion.ID == 0 {
				return Usuario{}, nucleo.Prohibido("La contraseña se cambia desde una sesión del panel")
			}
			q := identidadbd.New(t)
			t.FijarEntidad("usuario", actor.ID)
			antes, err := q.UsuarioPorIDParaActualizar(ctx, actor.ID)
			if err != nil {
				return Usuario{}, err
			}
			if antes.ContrasenaHash == nil {
				return Usuario{}, nucleo.ReglaNegocio("sin_contrasena", "El usuario no entra con contraseña")
			}
			bien, err := VerificarContrasena(in.Actual, *antes.ContrasenaHash)
			if err != nil {
				return Usuario{}, err
			}
			if !bien {
				return Usuario{}, nucleo.Validacion(nucleo.ErrorCampo{Field: "actual", Message: "La contraseña actual no es correcta", Rule: "incorrecta"})
			}
			if in.Nueva == in.Actual {
				return Usuario{}, nucleo.Validacion(nucleo.ErrorCampo{Field: "nueva", Message: "Debe ser distinta de la actual", Rule: "distinta"})
			}
			hash, err := hashValidado(in.Nueva)
			if err != nil {
				return Usuario{}, nucleo.Validacion(nucleo.ErrorCampo{Field: "nueva", Message: "Mínimo 12 caracteres", Rule: "minLength"})
			}
			despues, err := q.CambiarContrasenaPropia(ctx, identidadbd.CambiarContrasenaPropiaParams{ID: actor.ID, ContrasenaHash: hash})
			if err != nil {
				return Usuario{}, err
			}
			// Las demás sesiones del usuario se cierran; la actual sigue.
			if _, err := q.RevocarOtrasSesionesDeUsuario(ctx, identidadbd.RevocarOtrasSesionesDeUsuarioParams{UsuarioID: actor.ID, SesionID: sesion.ID}); err != nil {
				return Usuario{}, err
			}
			roles, err := rolesDe(ctx, q, actor.ID)
			if err != nil {
				return Usuario{}, err
			}
			campos := t.RegistrarCambios("usuario", actor.ID, auditable(antes, roles[actor.ID]), auditable(despues, roles[actor.ID]))
			if err := t.Emitir("usuario.actualizado", map[string]any{"usuario_id": actor.ID, "campos": campos}); err != nil {
				return Usuario{}, err
			}
			empresas, err := empresasDe(ctx, q, actor.ID)
			if err != nil {
				return Usuario{}, err
			}
			return aUsuario(despues, roles[actor.ID], empresas[actor.ID]), nil
		},
	})
}

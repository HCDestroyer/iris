package identidad

import (
	"context"
	"unicode/utf8"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
	identidadbd "github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad/bd"
)

// EntradaReiniciarMFA es la entrada de usuarios.reiniciar_mfa: un administrador borra el TOTP
// de otro usuario (perdió el teléfono, o tras rotar IRIS_CLAVE_CIFRADO sin la anterior), cierra
// sus sesiones y le pone una contraseña temporal que le entrega fuera de banda. La contraseña
// vieja deja de servir: quien la robó no puede entrar y configurar su propio MFA en la cuenta.
type EntradaReiniciarMFA struct {
	ID                 int64  `json:"-"`
	Version            int32  `json:"version" minimum:"1" doc:"Versión leída; si cambió, 409"`
	Motivo             string `json:"motivo" minLength:"1" maxLength:"500" doc:"Por qué se reinicia (queda en la auditoría y en el evento)"`
	ContrasenaTemporal string `json:"contrasenaTemporal" maxLength:"200" auditoria:"sensible" doc:"Contraseña temporal (mínimo 12 caracteres, distinta de la actual) que el administrador entrega al usuario fuera de banda. Reemplaza la actual; al entrar con ella, el usuario debe cambiarla antes de cualquier otra cosa. Nunca se audita ni va en eventos"`
	RevocarTokens      *bool  `json:"revocarTokens,omitempty" doc:"Revoca también sus tokens iris_pat_ (true por omisión; emite token.revocado por cada uno)"`
}

func nuevoReiniciarMFA() *auditoria.Caso[EntradaReiniciarMFA, Usuario] {
	return auditoria.Nuevo(auditoria.Caso[EntradaReiniciarMFA, Usuario]{
		Accion:     "usuarios.reiniciar_mfa",
		Permiso:    nucleo.PermUsuariosReiniciarMFA,
		SinEmpresa: true, // administración: no es operativa
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaReiniciarMFA) (Usuario, error) {
			q := identidadbd.New(t)
			t.FijarEntidad("usuario", in.ID)
			// El propio MFA no se reinicia por esta vía: quien lo pide no podría probar que es él
			// (con sesión o token propios se saltaría su segundo factor). Lo hace otro admin.
			if actor := contexto.ActorDe(ctx); actor.EsUsuario() && actor.ID == in.ID {
				return Usuario{}, nucleo.ReglaNegocio("reinicio_mfa_propio", "No puedes reiniciar tu propio MFA; pídeselo a otro administrador")
			}
			antes, err := q.UsuarioPorIDParaActualizar(ctx, in.ID)
			if err != nil {
				return Usuario{}, err
			}
			// Fuera del alcance de empresas: 404, como si no existiera (ni su versión se revela).
			if err := exigirVisible(ctx, q, in.ID, "reiniciar el MFA de este usuario"); err != nil {
				return Usuario{}, err
			}
			if err := nucleo.ExigirVersion(in.Version, antes.Version, "usuario", in.ID); err != nil {
				return Usuario{}, err
			}
			// Mismo alcance que editar o desactivar: además, sus permisos sin límite.
			if err := exigirSuperior(ctx, q, contexto.PermisosDe(ctx), in.ID, "reiniciar el MFA de este usuario"); err != nil {
				return Usuario{}, err
			}
			if antes.TotpSecreto == nil && !antes.TotpActivo {
				return Usuario{}, nucleo.ReglaNegocio("mfa_no_configurado", "El usuario no tiene MFA configurado")
			}
			hash, err := hashTemporal(in.ContrasenaTemporal, antes.ContrasenaHash)
			if err != nil {
				return Usuario{}, err
			}
			despues, err := q.ReiniciarMFA(ctx, identidadbd.ReiniciarMFAParams{ID: in.ID, Version: in.Version, ContrasenaHash: &hash})
			if err != nil {
				return Usuario{}, nucleo.VerificarVersion(err, "usuario", in.ID)
			}
			cerradas, err := q.RevocarSesionesDeUsuario(ctx, in.ID)
			if err != nil {
				return Usuario{}, err
			}
			revocados := []int64{}
			if in.RevocarTokens == nil || *in.RevocarTokens {
				if revocados, err = q.RevocarTokensDeUsuario(ctx, in.ID); err != nil {
					return Usuario{}, err
				}
				for _, id := range revocados {
					if err := t.Emitir("token.revocado", map[string]any{"token_id": id, "usuario_id": in.ID, "motivo": "mfa_reiniciado"}); err != nil {
						return Usuario{}, err
					}
				}
			}
			roles, err := rolesDe(ctx, q, in.ID)
			if err != nil {
				return Usuario{}, err
			}
			t.RegistrarCambios("usuario", in.ID, auditable(antes, roles[in.ID]), auditable(despues, roles[in.ID]))
			t.Anotar(map[string]any{"mfa_activo_antes": antes.TotpActivo, "sesiones_cerradas": cerradas, "tokens_revocados": revocados})
			if err := t.Emitir("usuario.mfa_reiniciado", map[string]any{
				"usuario_id": in.ID, "motivo": in.Motivo, "sesiones_cerradas": cerradas, "tokens_revocados": len(revocados),
			}); err != nil {
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

// hashTemporal valida la contraseña temporal con la política de contraseñas y que no sea la
// actual (si lo fuera, quien la robó seguiría entrando), y devuelve su hash.
func hashTemporal(temporal string, actual *string) (string, error) {
	if utf8.RuneCountInString(temporal) < LongitudMinimaContrasena {
		return "", nucleo.Validacion(nucleo.ErrorCampo{Field: "contrasenaTemporal", Message: "Mínimo 12 caracteres", Rule: "minLength"})
	}
	if actual != nil {
		igual, err := VerificarContrasena(temporal, *actual)
		if err != nil {
			return "", err
		}
		if igual {
			return "", nucleo.Validacion(nucleo.ErrorCampo{Field: "contrasenaTemporal", Message: "Debe ser distinta de la contraseña actual", Rule: "distinta"})
		}
	}
	return HashContrasena(temporal)
}

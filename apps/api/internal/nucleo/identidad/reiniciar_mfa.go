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

// MFAReiniciado es la respuesta de usuarios.reiniciar_mfa: el usuario actualizado (sus campos van
// en la raíz, como antes, para no romper a quien ya leía un Usuario) más los mismos contadores que
// lleva el evento usuario.mfa_reiniciado.
type MFAReiniciado struct {
	Usuario
	SesionesCerradas int64 `json:"sesionesCerradas" minimum:"0" doc:"Sesiones del usuario que se cerraron (completas y pendientes de MFA)"`
	TokensRevocados  int   `json:"tokensRevocados" minimum:"0" doc:"Tokens iris_pat_ revocados (0 con revocarTokens: false)"`
}

func nuevoReiniciarMFA() *auditoria.Caso[EntradaReiniciarMFA, MFAReiniciado] {
	return auditoria.Nuevo(auditoria.Caso[EntradaReiniciarMFA, MFAReiniciado]{
		Accion:     "usuarios.reiniciar_mfa",
		Permiso:    nucleo.PermUsuariosReiniciarMFA,
		SinEmpresa: true, // administración: no es operativa
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaReiniciarMFA) (MFAReiniciado, error) {
			q := identidadbd.New(t)
			t.FijarEntidad("usuario", in.ID)
			// El propio MFA no se reinicia por esta vía: quien lo pide no podría probar que es él
			// (con sesión o token propios se saltaría su segundo factor). Lo hace otro admin.
			if actor := contexto.ActorDe(ctx); actor.EsUsuario() && actor.ID == in.ID {
				return MFAReiniciado{}, nucleo.ReglaNegocio("reinicio_mfa_propio", "No puedes reiniciar tu propio MFA; pídeselo a otro administrador")
			}
			antes, err := q.UsuarioPorIDParaActualizar(ctx, in.ID)
			if err != nil {
				return MFAReiniciado{}, err
			}
			// Fuera del alcance de empresas: 404, como si no existiera (ni su versión se revela).
			if err := exigirVisible(ctx, q, in.ID, "reiniciar el MFA de este usuario"); err != nil {
				return MFAReiniciado{}, err
			}
			if err := nucleo.ExigirVersion(in.Version, antes.Version, "usuario", in.ID); err != nil {
				return MFAReiniciado{}, err
			}
			// Mismo alcance que editar o desactivar: además, sus permisos sin límite.
			if err := exigirSuperior(ctx, q, contexto.PermisosDe(ctx), in.ID, "reiniciar el MFA de este usuario"); err != nil {
				return MFAReiniciado{}, err
			}
			if antes.TotpSecreto == nil && !antes.TotpActivo {
				return MFAReiniciado{}, nucleo.ReglaNegocio("mfa_no_configurado", "El usuario no tiene MFA configurado")
			}
			hash, err := hashTemporal(in.ContrasenaTemporal, antes.ContrasenaHash)
			if err != nil {
				return MFAReiniciado{}, err
			}
			despues, err := q.ReiniciarMFA(ctx, identidadbd.ReiniciarMFAParams{ID: in.ID, Version: in.Version, ContrasenaHash: &hash})
			if err != nil {
				return MFAReiniciado{}, nucleo.VerificarVersion(err, "usuario", in.ID)
			}
			cerradas, err := q.RevocarSesionesDeUsuario(ctx, in.ID)
			if err != nil {
				return MFAReiniciado{}, err
			}
			revocados := []int64{}
			if in.RevocarTokens == nil || *in.RevocarTokens {
				if revocados, err = q.RevocarTokensDeUsuario(ctx, in.ID); err != nil {
					return MFAReiniciado{}, err
				}
				for _, id := range revocados {
					if err := t.Emitir("token.revocado", map[string]any{"token_id": id, "usuario_id": in.ID, "motivo": "mfa_reiniciado"}); err != nil {
						return MFAReiniciado{}, err
					}
				}
			}
			roles, err := rolesDe(ctx, q, in.ID)
			if err != nil {
				return MFAReiniciado{}, err
			}
			t.RegistrarCambios("usuario", in.ID, auditable(antes, roles[in.ID]), auditable(despues, roles[in.ID]))
			t.Anotar(map[string]any{"mfa_activo_antes": antes.TotpActivo, "sesiones_cerradas": cerradas, "tokens_revocados": revocados})
			if err := t.Emitir("usuario.mfa_reiniciado", map[string]any{
				"usuario_id": in.ID, "motivo": in.Motivo, "sesiones_cerradas": cerradas, "tokens_revocados": len(revocados),
			}); err != nil {
				return MFAReiniciado{}, err
			}
			empresas, err := empresasDe(ctx, q, in.ID)
			if err != nil {
				return MFAReiniciado{}, err
			}
			return MFAReiniciado{
				Usuario:          aUsuario(despues, roles[in.ID], empresas[in.ID]),
				SesionesCerradas: cerradas,
				TokensRevocados:  len(revocados),
			}, nil
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

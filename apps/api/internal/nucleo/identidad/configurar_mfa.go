package identidad

import (
	"context"

	"github.com/pquerna/otp/totp"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
	identidadbd "github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad/bd"
)

// MFAConfigurado lleva el secreto para la app autenticadora. Solo se ve esta vez; el MFA queda
// activo después de POST /api/v1/yo/mfa/activar con un código válido.
type MFAConfigurado struct {
	Secreto string `json:"secreto" doc:"Secreto TOTP en base32"`
	URL     string `json:"url" doc:"URL otpauth:// para el código QR"`
}

// nuevoConfigurarMFA genera (o regenera, si aún no está activo) el secreto TOTP del usuario
// con sesión. Lo hace el propio usuario desde el panel; un token no puede.
func nuevoConfigurarMFA(cfg Config) *auditoria.Caso[struct{}, MFAConfigurado] {
	return auditoria.Nuevo(auditoria.Caso[struct{}, MFAConfigurado]{
		Accion: "mfa.configurar",
		Acceso: auditoria.Autenticado,
		Logica: func(ctx context.Context, t *auditoria.Tx, _ struct{}) (MFAConfigurado, error) {
			actor := contexto.ActorDe(ctx)
			if actor.Tipo != contexto.ActorUsuario {
				return MFAConfigurado{}, nucleo.Prohibido("El MFA se configura desde una sesión del panel")
			}
			if cfg.Cifrador == nil {
				return MFAConfigurado{}, nucleo.ReglaNegocio("mfa_no_disponible", "MFA no está disponible: falta IRIS_CLAVE_CIFRADO")
			}
			q := identidadbd.New(t)
			t.FijarEntidad("usuario", actor.ID)
			u, err := q.UsuarioPorIDParaActualizar(ctx, actor.ID)
			if err != nil {
				return MFAConfigurado{}, err
			}
			if u.TotpActivo {
				return MFAConfigurado{}, nucleo.ReglaNegocio("mfa_activo", "El MFA ya está activo")
			}
			clave, err := totp.Generate(totp.GenerateOpts{Issuer: cfg.EmisorTOTP, AccountName: u.Correo})
			if err != nil {
				return MFAConfigurado{}, err
			}
			cifrado, err := cfg.Cifrador.Cifrar([]byte(clave.Secret()), datosAsociadosTOTP(u.ID))
			if err != nil {
				return MFAConfigurado{}, err
			}
			if err := q.GuardarSecretoTOTP(ctx, identidadbd.GuardarSecretoTOTPParams{ID: u.ID, TotpSecreto: cifrado}); err != nil {
				return MFAConfigurado{}, err
			}
			return MFAConfigurado{Secreto: clave.Secret(), URL: clave.URL()}, nil
		},
	})
}

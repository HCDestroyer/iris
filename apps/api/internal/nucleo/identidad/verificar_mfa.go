package identidad

import (
	"context"
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
	identidadbd "github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad/bd"
)

// EntradaVerificarMFA es la entrada de sesion.verificar_mfa.
type EntradaVerificarMFA struct {
	Codigo    string `json:"codigo" pattern:"^[0-9]{6}$" auditoria:"sensible" doc:"Código TOTP de 6 dígitos"`
	IP        string `json:"-"`
	UserAgent string `json:"-"`
}

// nuevoVerificarMFA completa un inicio de sesión que quedó pendiente de TOTP. Se cambia la
// sesión pendiente por una nueva (otro valor de cookie y de CSRF).
func nuevoVerificarMFA(cfg Config) *auditoria.Caso[EntradaVerificarMFA, SesionIniciada] {
	return auditoria.Nuevo(auditoria.Caso[EntradaVerificarMFA, SesionIniciada]{
		Accion: "sesion.verificar_mfa",
		Acceso: auditoria.Publico,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaVerificarMFA) (SesionIniciada, error) {
			s := contexto.SesionDe(ctx)
			if s.ID == 0 || !s.MFAPendiente {
				return SesionIniciada{}, nucleo.NoAutenticado("No hay un inicio de sesión pendiente de MFA")
			}
			q := identidadbd.New(t)
			t.FijarEntidad("usuario", s.UsuarioID)
			u, err := q.UsuarioPorIDParaActualizar(ctx, s.UsuarioID)
			if err != nil {
				return SesionIniciada{}, err
			}
			secreto, err := secretoTOTP(cfg, u)
			if err != nil {
				return SesionIniciada{}, err
			}
			paso, ok := validarTOTP(secreto, in.Codigo, time.Now())
			if !ok {
				return SesionIniciada{}, codigoInvalido()
			}
			if n, err := q.RegistrarPasoTOTP(ctx, identidadbd.RegistrarPasoTOTPParams{ID: u.ID, Paso: paso}); err != nil {
				return SesionIniciada{}, err
			} else if n == 0 {
				return SesionIniciada{}, codigoInvalido() // código ya usado
			}
			if n, err := q.CompletarMFASesion(ctx, s.ID); err != nil {
				return SesionIniciada{}, err
			} else if n == 0 {
				return SesionIniciada{}, nucleo.NoAutenticado("La sesión pendiente ya no es válida")
			}
			if _, err := q.RevocarSesion(ctx, s.ID); err != nil {
				return SesionIniciada{}, err
			}
			t.IdentificarActor(contexto.Actor{Tipo: contexto.ActorUsuario, ID: u.ID, Nombre: u.Nombre})
			return abrirSesion(ctx, q, cfg, u, false, in.IP, in.UserAgent)
		},
	})
}

package identidad

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"

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
			fallos, err := q.FallosMFASesion(ctx, s.ID)
			if err != nil {
				return SesionIniciada{}, err
			}
			paso, ok := validarTOTP(secreto, in.Codigo, time.Now())
			if ok {
				// Un código ya usado cuenta como fallo.
				n, err := q.RegistrarPasoTOTP(ctx, identidadbd.RegistrarPasoTOTPParams{ID: u.ID, Paso: paso})
				if err != nil {
					return SesionIniciada{}, err
				}
				ok = n > 0
			}
			if !ok {
				return SesionIniciada{}, contarFalloMFA(t, cfg, s.ID, fallos)
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

// contarFalloMFA suma el fallo a la sesión pendiente (en la transacción de auditoría del error)
// y, al llegar a MFAMaxFallos, la revoca: hay que volver a iniciar sesión. Devuelve el error
// que ve el cliente: codigo_invalido o, con el último intento, mfa_bloqueado.
func contarFalloMFA(t *auditoria.Tx, cfg Config, sesionID int64, previos int32) error {
	t.AlFallar(func(ctx context.Context, tx pgx.Tx) error {
		q := identidadbd.New(tx)
		fallos, err := q.RegistrarFalloMFA(ctx, sesionID)
		if errors.Is(err, pgx.ErrNoRows) || (err == nil && fallos < cfg.MFAMaxFallos) {
			return nil
		}
		if err != nil {
			return err
		}
		if _, err := q.RevocarSesion(ctx, sesionID); err != nil {
			return err
		}
		t.Anotar(map[string]any{"sesion_revocada": sesionID, "fallos": fallos})
		slog.WarnContext(ctx, "identidad: sesión pendiente de MFA revocada por fallos", "sesion_id", sesionID, "fallos", fallos)
		return nil
	})
	if previos+1 >= cfg.MFAMaxFallos {
		return nucleo.NuevoProblema(401, "mfa_bloqueado", "Demasiados códigos inválidos",
			"La sesión pendiente se cerró tras varios códigos inválidos; vuelve a iniciar sesión")
	}
	return codigoInvalido()
}

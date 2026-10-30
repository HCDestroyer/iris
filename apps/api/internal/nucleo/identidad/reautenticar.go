package identidad

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
	identidadbd "github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad/bd"
)

// VentanaReautenticacion: tras confirmar la contraseña, una sesión abierta por el autologin
// puede hacer las acciones sensibles (auditoria.RequiereReautenticacion) durante este tiempo.
const VentanaReautenticacion = 15 * time.Minute

// EntradaReautenticar es la contraseña del usuario de la sesión.
type EntradaReautenticar struct {
	Contrasena string `json:"contrasena" minLength:"1" maxLength:"200"`
}

// Reautenticado dice hasta cuándo valen las acciones sensibles.
type Reautenticado struct {
	ValidaHasta time.Time `json:"validaHasta"`
}

// nuevoReautenticar: confirmar la contraseña dentro de la sesión (la exige una sesión sso para
// las acciones sensibles). Cuenta los fallos y bloquea como el login.
func nuevoReautenticar(cfg Config) *auditoria.Caso[EntradaReautenticar, Reautenticado] {
	return auditoria.Nuevo(auditoria.Caso[EntradaReautenticar, Reautenticado]{
		Accion: "sesion.reautenticar", Acceso: auditoria.Autenticado,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaReautenticar) (Reautenticado, error) {
			q := identidadbd.New(t)
			a, s := t.Actor(), contexto.SesionDe(ctx)
			if a.Tipo != contexto.ActorUsuario || s.ID == 0 {
				return Reautenticado{}, nucleo.Prohibido("Solo con la sesión del panel")
			}
			u, err := q.UsuarioPorID(ctx, a.ID)
			if err != nil {
				return Reautenticado{}, err
			}
			t.FijarEntidad("usuario", u.ID)
			hasta, err := q.BloqueoLogin(ctx, u.Correo)
			switch {
			case err == nil:
				return Reautenticado{}, nucleo.DemasiadosIntentos("demasiados_intentos",
					"Demasiados intentos fallidos con este correo; espera antes de volver a intentar", time.Until(hasta))
			case !errors.Is(err, pgx.ErrNoRows):
				return Reautenticado{}, err
			}
			if u.ContrasenaHash == nil {
				return Reautenticado{}, nucleo.CredencialesInvalidas()
			}
			ok, err := VerificarContrasena(in.Contrasena, *u.ContrasenaHash)
			if err != nil {
				return Reautenticado{}, err
			}
			if !ok {
				contarFalloLogin(t, cfg, u.Correo)
				return Reautenticado{}, nucleo.CredencialesInvalidas()
			}
			if err := q.LimpiarIntentosLogin(ctx, u.Correo); err != nil {
				return Reautenticado{}, err
			}
			if n, err := q.MarcarSesionReautenticada(ctx, s.ID); err != nil {
				return Reautenticado{}, err
			} else if n == 0 {
				return Reautenticado{}, nucleo.NoAutenticado("La sesión ya no es válida")
			}
			return Reautenticado{ValidaHasta: time.Now().Add(VentanaReautenticacion)}, nil
		},
	})
}

package identidad

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"

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

// claveIntentosContrasena: los fallos de la contraseña actual se cuentan por usuario en
// nucleo.intentos_login, con el mismo mecanismo y los mismos límites que el login (no choca con
// los correos: no lleva @).
func claveIntentosContrasena(usuarioID int64) string {
	return "usuario:" + strconv.FormatInt(usuarioID, 10)
}

func nuevoCambiarContrasena(cfg Config) *auditoria.Caso[EntradaCambiarContrasena, Usuario] {
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
			// Límite de intentos (una cookie robada no prueba contraseñas sin fin): con el
			// bloqueo vigente, 429 sin verificar (y sin gastar un argon2).
			clave := claveIntentosContrasena(actor.ID)
			if hasta, err := q.BloqueoLogin(ctx, clave); err == nil {
				revocarSesionAlFallar(t, sesion.ID)
				return Usuario{}, demasiadosIntentosContrasena(time.Until(hasta))
			} else if !errors.Is(err, pgx.ErrNoRows) {
				return Usuario{}, err
			}
			bien, err := VerificarContrasena(in.Actual, *antes.ContrasenaHash)
			if err != nil {
				return Usuario{}, err
			}
			if !bien {
				ahora := time.Now()
				previos, err := q.FallosVigentes(ctx, identidadbd.FallosVigentesParams{Correo: clave, Desde: ahora.Add(-cfg.LoginVentana)})
				if err != nil {
					return Usuario{}, err
				}
				t.AlFallar(func(ctx context.Context, tx pgx.Tx) error {
					_, err := identidadbd.New(tx).RegistrarFalloLogin(ctx, identidadbd.RegistrarFalloLoginParams{Correo: clave, Desde: ahora.Add(-cfg.LoginVentana)})
					return err
				})
				if previos+1 >= cfg.LoginMaxFallos {
					// Al llegar al máximo: bloqueo, la sesión se cierra y 429.
					hasta := ahora.Add(cfg.LoginBloqueo)
					t.AlFallar(func(ctx context.Context, tx pgx.Tx) error {
						return identidadbd.New(tx).BloquearLogin(ctx, identidadbd.BloquearLoginParams{Correo: clave, Hasta: &hasta})
					})
					revocarSesionAlFallar(t, sesion.ID)
					t.Anotar(map[string]any{"bloqueado_hasta": hasta.UTC(), "fallos": previos + 1})
					slog.WarnContext(ctx, "identidad: cambio de contraseña bloqueado por fallos", "usuario", actor.ID, "fallos", previos+1, "hasta", hasta)
					return Usuario{}, demasiadosIntentosContrasena(cfg.LoginBloqueo)
				}
				return Usuario{}, nucleo.Validacion(nucleo.ErrorCampo{Field: "actual", Message: "La contraseña actual no es correcta", Rule: "incorrecta"})
			}
			if err := q.LimpiarIntentosLogin(ctx, clave); err != nil {
				return Usuario{}, err
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

// revocarSesionAlFallar cierra la sesión actual en la transacción de la fila de error (la de la
// acción se revierte).
func revocarSesionAlFallar(t *auditoria.Tx, sesionID int64) {
	t.AlFallar(func(ctx context.Context, tx pgx.Tx) error {
		_, err := identidadbd.New(tx).RevocarSesion(ctx, sesionID)
		return err
	})
}

func demasiadosIntentosContrasena(espera time.Duration) *nucleo.Problema {
	return nucleo.DemasiadosIntentos("demasiados_intentos",
		"Demasiados intentos con una contraseña actual incorrecta: la sesión se cerró; espera antes de volver a entrar", espera)
}

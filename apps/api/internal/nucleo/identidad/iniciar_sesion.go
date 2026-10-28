package identidad

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
	identidadbd "github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad/bd"
)

// EntradaIniciarSesion es la entrada de sesion.iniciar.
type EntradaIniciarSesion struct {
	Correo     string `json:"correo" format:"email" maxLength:"254"`
	Contrasena string `json:"contrasena" minLength:"1" maxLength:"200"`
	IP         string `json:"-"`
	UserAgent  string `json:"-"`
}

// SesionIniciada es el resultado de iniciar sesión o completar el MFA. El valor de la cookie
// nunca sale en el cuerpo.
type SesionIniciada struct {
	Token        string    `json:"-"`
	CSRFToken    string    `json:"csrfToken" doc:"Valor para la cabecera X-CSRF-Token (también va en la cookie iris_csrf)"`
	MFARequerido bool      `json:"mfaRequerido" doc:"true: falta POST /api/v1/sesion/mfa con el código TOTP"`
	ExpiraEn     time.Time `json:"expiraEn"`
	Usuario      *Usuario  `json:"usuario,omitempty" doc:"El usuario, cuando la sesión quedó completa"`
}

func nuevoIniciarSesion(cfg Config) *auditoria.Caso[EntradaIniciarSesion, SesionIniciada] {
	return auditoria.Nuevo(auditoria.Caso[EntradaIniciarSesion, SesionIniciada]{
		Accion: "sesion.iniciar",
		Acceso: auditoria.Publico,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaIniciarSesion) (SesionIniciada, error) {
			q := identidadbd.New(t)
			t.Anotar(map[string]any{"ip": in.IP, "user_agent": in.UserAgent})
			correo := strings.ToLower(strings.TrimSpace(in.Correo))
			// Bloqueo por fuerza bruta: por correo, exista o no, para no revelar cuáles existen.
			hasta, err := q.BloqueoLogin(ctx, correo)
			switch {
			case err == nil:
				return SesionIniciada{}, nucleo.DemasiadosIntentos("demasiados_intentos",
					"Demasiados intentos fallidos con este correo; espera antes de volver a intentar", time.Until(hasta))
			case !errors.Is(err, pgx.ErrNoRows):
				return SesionIniciada{}, err
			}
			u, err := q.UsuarioPorCorreo(ctx, correo)
			if err != nil || !u.Activo || u.ContrasenaHash == nil {
				_, _ = VerificarContrasena(in.Contrasena, hashFicticio) // mismo tiempo de respuesta
				if err == nil {
					t.FijarEntidad("usuario", u.ID)
				}
				contarFalloLogin(t, cfg, correo)
				return SesionIniciada{}, nucleo.CredencialesInvalidas()
			}
			t.FijarEntidad("usuario", u.ID)
			ok, err := VerificarContrasena(in.Contrasena, *u.ContrasenaHash)
			if err != nil {
				return SesionIniciada{}, err
			}
			if !ok {
				contarFalloLogin(t, cfg, correo)
				return SesionIniciada{}, nucleo.CredencialesInvalidas()
			}
			if err := q.LimpiarIntentosLogin(ctx, correo); err != nil {
				return SesionIniciada{}, err
			}
			t.IdentificarActor(contexto.Actor{Tipo: contexto.ActorUsuario, ID: u.ID, Nombre: u.Nombre})
			return abrirSesion(ctx, q, cfg, u, u.TotpActivo, in.IP, in.UserAgent)
		},
	})
}

// contarFalloLogin suma el fallo al correo (en la transacción de auditoría del error, que sí
// se confirma) y, al llegar a LoginMaxFallos, lo bloquea LoginBloqueo; el bloqueo queda en la
// fila de auditoría (entrada.auditoria.bloqueado_hasta) y en el log.
func contarFalloLogin(t *auditoria.Tx, cfg Config, correo string) {
	t.AlFallar(func(ctx context.Context, tx pgx.Tx) error {
		q := identidadbd.New(tx)
		ahora := time.Now()
		fallos, err := q.RegistrarFalloLogin(ctx, identidadbd.RegistrarFalloLoginParams{Correo: correo, Desde: ahora.Add(-cfg.LoginVentana)})
		if err != nil || fallos < cfg.LoginMaxFallos {
			return err
		}
		hasta := ahora.Add(cfg.LoginBloqueo)
		if err := q.BloquearLogin(ctx, identidadbd.BloquearLoginParams{Correo: correo, Hasta: &hasta}); err != nil {
			return err
		}
		t.Anotar(map[string]any{"bloqueado_hasta": hasta.UTC(), "fallos": fallos})
		slog.WarnContext(ctx, "identidad: inicio de sesión bloqueado por fallos", "correo", correo, "fallos", fallos, "hasta", hasta)
		return nil
	})
}

// abrirSesion crea la fila de sesión y devuelve los valores de las cookies.
func abrirSesion(ctx context.Context, q *identidadbd.Queries, cfg Config, u identidadbd.NucleoUsuario, mfaPendiente bool, ip, agente string) (SesionIniciada, error) {
	token, err := NuevoSecreto()
	if err != nil {
		return SesionIniciada{}, err
	}
	csrf, err := NuevoSecreto()
	if err != nil {
		return SesionIniciada{}, err
	}
	duracion := cfg.DuracionSesion
	if mfaPendiente {
		duracion = 10 * time.Minute // tiempo para escribir el código
	}
	s, err := q.CrearSesion(ctx, identidadbd.CrearSesionParams{
		UsuarioID: u.ID, TokenHash: Hash256(token), CsrfHash: Hash256(csrf), MfaPendiente: mfaPendiente,
		Ip: recortar(ip, 64), UserAgent: recortar(agente, 400), ExpiraEn: time.Now().Add(duracion),
	})
	if err != nil {
		return SesionIniciada{}, err
	}
	salida := SesionIniciada{Token: token, CSRFToken: csrf, MFARequerido: mfaPendiente, ExpiraEn: s.ExpiraEn}
	if !mfaPendiente {
		roles, err := rolesDe(ctx, q, u.ID)
		if err != nil {
			return SesionIniciada{}, err
		}
		usuario := aUsuario(u, roles[u.ID])
		salida.Usuario = &usuario
	}
	return salida, nil
}

func recortar(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

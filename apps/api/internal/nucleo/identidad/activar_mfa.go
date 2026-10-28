package identidad

import (
	"context"
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
	identidadbd "github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad/bd"
)

// EntradaActivarMFA confirma el secreto con un código de la app.
type EntradaActivarMFA struct {
	Codigo string `json:"codigo" pattern:"^[0-9]{6}$" auditoria:"sensible" doc:"Código TOTP de 6 dígitos"`
}

func nuevoActivarMFA(cfg Config) *auditoria.Caso[EntradaActivarMFA, Usuario] {
	return auditoria.Nuevo(auditoria.Caso[EntradaActivarMFA, Usuario]{
		Accion: "mfa.activar",
		Acceso: auditoria.Autenticado,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaActivarMFA) (Usuario, error) {
			actor := contexto.ActorDe(ctx)
			if actor.Tipo != contexto.ActorUsuario {
				return Usuario{}, nucleo.Prohibido("El MFA se activa desde una sesión del panel")
			}
			q := identidadbd.New(t)
			t.FijarEntidad("usuario", actor.ID)
			antes, err := q.UsuarioPorIDParaActualizar(ctx, actor.ID)
			if err != nil {
				return Usuario{}, err
			}
			if antes.TotpActivo {
				return Usuario{}, nucleo.ReglaNegocio("mfa_activo", "El MFA ya está activo")
			}
			secreto, err := secretoTOTP(cfg, antes)
			if err != nil {
				return Usuario{}, err
			}
			paso, ok := validarTOTP(secreto, in.Codigo, time.Now())
			if !ok {
				return Usuario{}, nucleo.ReglaNegocio("codigo_invalido", "El código no coincide; revisa la hora del teléfono")
			}
			despues, err := q.ActivarTOTP(ctx, identidadbd.ActivarTOTPParams{ID: actor.ID, Paso: paso})
			if err != nil {
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
			return aUsuario(despues, roles[actor.ID]), nil
		},
	})
}

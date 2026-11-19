package identidad

import (
	"context"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	identidadbd "github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad/bd"
)

// EntradaRevocarToken es la entrada de tokens.revocar.
type EntradaRevocarToken struct {
	ID     int64  `json:"-"`
	Motivo string `json:"motivo,omitempty" maxLength:"500"`
	// interno: lo pide el conector (RevocarTokenIntegracion), que ya apagó o reemplazó al
	// conector; no sale de la entrada HTTP.
	interno bool
}

func nuevoRevocarToken() *auditoria.Caso[EntradaRevocarToken, TokenAPI] {
	return auditoria.Nuevo(auditoria.Caso[EntradaRevocarToken, TokenAPI]{
		Accion:     "tokens.revocar",
		Permiso:    nucleo.PermTokensRevocar,
		SinEmpresa: true, // administración: no es operativa
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaRevocarToken) (TokenAPI, error) {
			q := identidadbd.New(t)
			t.FijarEntidad("token", in.ID)
			antes, err := q.TokenPorIDParaActualizar(ctx, in.ID)
			if err != nil {
				return TokenAPI{}, err
			}
			// Revocar tokens de otro usuario exige operar en todas sus empresas; fuera de ese
			// alcance, 404 como un token inexistente (no se revela su dueño ni sus empresas).
			if err := exigirVisible(ctx, q, antes.UsuarioID, "revocar tokens de este usuario"); err != nil {
				return TokenAPI{}, err
			}
			if antes.EliminadoEn != nil {
				return TokenAPI{}, nucleo.NoEncontrado("") // un token eliminado ya no existe para la API
			}
			// Revocar el iris_pat_ de un conector activo cortaría la integración: se cambia desde
			// Integraciones (desactivar o reconectar), no aquí.
			if !in.interno {
				usado, err := q.TokenUsadoPorConectorActivo(ctx, in.ID)
				if err != nil {
					return TokenAPI{}, err
				}
				if usado {
					return TokenAPI{}, nucleo.Conflicto("token_en_uso_por_conector",
						"Lo usa la integración con Argos u Horus: revocarlo la cortaría. Cámbialo desde Integraciones (desactivar o reconectar el conector)")
				}
			}
			if antes.RevocadoEn != nil {
				return TokenAPI{}, nucleo.ReglaNegocio("token_revocado", "El token ya está revocado")
			}
			despues, err := q.RevocarToken(ctx, in.ID)
			if err != nil {
				return TokenAPI{}, err
			}
			t.RegistrarCambios("token", in.ID, aTokenAPI(antes), aTokenAPI(despues))
			if err := t.Emitir("token.revocado", map[string]any{"token_id": in.ID, "usuario_id": antes.UsuarioID, "motivo": in.Motivo}); err != nil {
				return TokenAPI{}, err
			}
			return aTokenAPI(despues), nil
		},
	})
}

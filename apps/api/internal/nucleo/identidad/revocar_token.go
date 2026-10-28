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

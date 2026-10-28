package identidad

import (
	"context"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	identidadbd "github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad/bd"
)

// EntradaListarTokens filtra por dueño.
type EntradaListarTokens struct {
	nucleo.ParamsPagina
	UsuarioID int64 `query:"usuario_id" minimum:"0" doc:"Solo los tokens de este usuario"`
}

func nuevoListarTokens() *auditoria.Caso[EntradaListarTokens, nucleo.Pagina[TokenAPI]] {
	return auditoria.Nuevo(auditoria.Caso[EntradaListarTokens, nucleo.Pagina[TokenAPI]]{
		Accion:       "tokens.ver",
		Permiso:      nucleo.PermTokensVer,
		AuditarExito: soloLecturas[nucleo.Pagina[TokenAPI]],
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaListarTokens) (nucleo.Pagina[TokenAPI], error) {
			var despues int64
			if _, err := nucleo.DecodificarCursor(in.Cursor, &despues); err != nil {
				return nucleo.Pagina[TokenAPI]{}, err
			}
			p := identidadbd.ListarTokensParams{DespuesDe: despues, Limite: int32(in.Limite() + 1)} //nolint:gosec // ≤ 201
			if in.UsuarioID != 0 {
				p.UsuarioID = &in.UsuarioID
			}
			filas, err := identidadbd.New(t).ListarTokens(ctx, p)
			if err != nil {
				return nucleo.Pagina[TokenAPI]{}, err
			}
			return nucleo.Paginar(filas, in.Limite(), aTokenAPI,
				func(f identidadbd.NucleoTokensApi) []any { return []any{f.ID} }), nil
		},
	})
}

package identidad

import (
	"context"
	"strings"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
	identidadbd "github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad/bd"
)

// EntradaListarUsuarios: búsqueda por nombre o correo y filtro por activo, paginada por id.
type EntradaListarUsuarios struct {
	nucleo.ParamsPagina
	Q      string `query:"q" maxLength:"120" doc:"Busca en nombre y correo"`
	Activo string `query:"activo" enum:"true,false" doc:"Filtra por activo"`
}

func nuevoListarUsuarios() *auditoria.Caso[EntradaListarUsuarios, nucleo.Pagina[Usuario]] {
	return auditoria.Nuevo(auditoria.Caso[EntradaListarUsuarios, nucleo.Pagina[Usuario]]{
		Accion:       "usuarios.ver",
		Permiso:      nucleo.PermUsuariosVer,
		SinEmpresa:   true, // administración: no es operativa
		AuditarExito: soloLecturas[nucleo.Pagina[Usuario]],
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaListarUsuarios) (nucleo.Pagina[Usuario], error) {
			var despues int64
			if _, err := nucleo.DecodificarCursor(in.Cursor, &despues); err != nil {
				return nucleo.Pagina[Usuario]{}, err
			}
			p := identidadbd.ListarUsuariosParams{DespuesDe: despues, Limite: int32(in.Limite() + 1)} //nolint:gosec // ≤ 201
			if q := strings.TrimSpace(in.Q); q != "" {
				p.Q = &q
			}
			if in.Activo != "" {
				activo := in.Activo == "true"
				p.Activo = &activo
			}
			q := identidadbd.New(t)
			// Sin empresas.todas global, solo usuarios que comparten alguna empresa (y uno mismo).
			a, err := alcanceDelActor(ctx, q)
			if err != nil {
				return nucleo.Pagina[Usuario]{}, err
			}
			p.Todas, p.Empresas, p.Yo = a.todas, lista(a.propias), contexto.ActorDe(ctx).ID
			filas, err := q.ListarUsuarios(ctx, p)
			if err != nil {
				return nucleo.Pagina[Usuario]{}, err
			}
			ids := make([]int64, 0, len(filas))
			for _, f := range filas {
				ids = append(ids, f.ID)
			}
			roles, err := rolesDe(ctx, q, ids...)
			if err != nil {
				return nucleo.Pagina[Usuario]{}, err
			}
			empresas, err := empresasDe(ctx, q, ids...)
			if err != nil {
				return nucleo.Pagina[Usuario]{}, err
			}
			return nucleo.Paginar(filas, in.Limite(),
				func(u identidadbd.NucleoUsuario) Usuario { return aUsuario(u, roles[u.ID], empresas[u.ID]) },
				func(u identidadbd.NucleoUsuario) []any { return []any{u.ID} }), nil
		},
	})
}

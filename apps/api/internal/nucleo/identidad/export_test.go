package identidad

import (
	"context"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
	identidadbd "github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad/bd"
)

// Accesos a lo interno solo para las pruebas del paquete identidad_test.

func ResolverRolesPrueba(ctx context.Context, q *identidadbd.Queries, quien contexto.Permisos, roles []AsignacionRol) error {
	_, err := resolverRoles(ctx, q, quien, "roles", roles)
	return err
}

func ExigirSuperiorPrueba(ctx context.Context, q *identidadbd.Queries, quien contexto.Permisos, objetivo int64) error {
	return exigirSuperior(ctx, q, quien, objetivo, "probar")
}

func PuedeEmitirPrueba(ctx context.Context, q *identidadbd.Queries, actor contexto.Actor, dueno identidadbd.NucleoUsuario, limite []string) error {
	return puedeEmitir(ctx, q, actor, dueno, limite)
}

func ConTokenPadrePrueba(ctx context.Context, id int64) context.Context {
	return conTokenPadre(ctx, id)
}

func ConTokenDeCanjePrueba(ctx context.Context) context.Context { return conTokenDeCanje(ctx) }

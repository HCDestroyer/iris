package soporte

import (
	"context"
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	soportebd "github.com/HCDestroyer/iris/apps/api/internal/soporte/bd"
)

// ResolverConfirmadosCompartido corre la confirmación de Horus de un router compartido (empresa nula),
// como lo haría el caso de Horus, en su propia transacción auditada (solo pruebas: hoy Horus descarta un
// router sin empresa cuando Iris tiene empresas).
func (m *Modulo) ResolverConfirmadosCompartido(ctx context.Context, router int64, ocurrido time.Time) (int, error) {
	caso := auditoria.Nuevo(auditoria.Caso[int64, int]{
		Accion: "tickets.horus_incidente", Acceso: auditoria.Autenticado,
		Logica: func(ctx context.Context, t *auditoria.Tx, r int64) (int, error) {
			t.FijarEmpresa(0)
			return m.resolverConfirmadosPorHorus(ctx, t, soportebd.New(t), nil, r, ocurrido)
		},
	})
	return caso.Ejecutar(ctx, router)
}

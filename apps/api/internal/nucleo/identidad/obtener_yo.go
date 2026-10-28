package identidad

import (
	"context"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
	identidadbd "github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad/bd"
)

// Yo es el usuario autenticado con sus permisos efectivos (los del token si entró con uno) y
// el alcance de cada rol.
type Yo struct {
	Usuario  Usuario          `json:"usuario"`
	Actor    string           `json:"actor" enum:"usuario,token,integracion" doc:"Cómo entró: sesión del panel, token o integración"`
	TokenID  *int64           `json:"tokenId,omitempty" doc:"Token con que entró, si aplica"`
	Permisos []nucleo.Permiso `json:"permisos" doc:"Permisos efectivos; el alcance de cada uno sale de los roles"`
}

func nuevoObtenerYo() *auditoria.Caso[struct{}, Yo] {
	return auditoria.Nuevo(auditoria.Caso[struct{}, Yo]{
		Accion:       "yo.ver",
		Acceso:       auditoria.Autenticado,
		AuditarExito: soloLecturas[Yo],
		Logica: func(ctx context.Context, t *auditoria.Tx, _ struct{}) (Yo, error) {
			actor := contexto.ActorDe(ctx)
			id, err := usuarioDelActor(actor)
			if err != nil {
				return Yo{}, err
			}
			q := identidadbd.New(t)
			u, err := q.UsuarioPorID(ctx, id)
			if err != nil {
				return Yo{}, err
			}
			roles, err := rolesDe(ctx, q, id)
			if err != nil {
				return Yo{}, err
			}
			lista := contexto.PermisosDe(ctx).Lista()
			permisos := make([]nucleo.Permiso, 0, len(lista))
			for _, p := range lista {
				permisos = append(permisos, nucleo.Permiso(p))
			}
			yo := Yo{Usuario: aUsuario(u, roles[id]), Actor: string(actor.Tipo), Permisos: permisos}
			if actor.TokenID != 0 {
				yo.TokenID = &actor.TokenID
			}
			return yo, nil
		},
	})
}

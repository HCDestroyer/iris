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
	// Empresa activa (cabecera X-Iris-Empresa): el panel arma el selector con empresas.
	Empresas         []EmpresaResumen `json:"empresas" doc:"Empresas en que puede operar (todas las registradas si tiene empresas.todas)"`
	EmpresaActiva    *EmpresaResumen  `json:"empresaActiva,omitempty" doc:"Empresa activa de esta petición (X-Iris-Empresa o la única disponible); ausente si no hay"`
	ModoEmpresa      string           `json:"modoEmpresa" enum:"activa,por_elegir,sin_empresa,sin_conector" doc:"activa: hay empresa activa; por_elegir: tiene varias y falta X-Iris-Empresa (lo operativo responde 422 empresa_requerida); sin_empresa: no tiene ninguna asignada (lo operativo responde 403 sin_empresa); sin_conector: no hay empresas registradas y se trabaja con empresa_id null"`
	TodasLasEmpresas bool             `json:"todasLasEmpresas" doc:"Tiene empresas.todas (rol global)"`
	// HayDatosSinEmpresa: para quien puede adoptarlos (admin global).
	HayDatosSinEmpresa bool `json:"hayDatosSinEmpresa" doc:"Solo para admin global (empresas.adoptar): hay empresas registradas y quedan datos operativos con empresa_id null, que nadie ve; se asignan con POST /empresas/{id}/adoptar-sin-empresa. false para los demás"`
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
			asignadas, err := empresasDe(ctx, q, id)
			if err != nil {
				return Yo{}, err
			}
			yo := Yo{Usuario: aUsuario(u, roles[id], asignadas[id]), Actor: string(actor.Tipo), Permisos: permisos}
			e := contexto.EmpresasDe(ctx)
			if e.Estado == contexto.EmpresaNoResuelta {
				// Sin middleware (consola, pruebas): se resuelve aquí sin cabecera.
				if e, err = ResolverEmpresa(ctx, q, ""); err != nil {
					return Yo{}, err
				}
			}
			if yo.Empresas, err = empresasResumen(ctx, q, e.Disponibles); err != nil {
				return Yo{}, err
			}
			yo.ModoEmpresa, yo.TodasLasEmpresas = string(e.Estado), e.Todas
			for _, r := range yo.Empresas {
				if r.ID == e.Activa {
					yo.EmpresaActiva = &r
				}
			}
			if actor.TokenID != 0 {
				yo.TokenID = &actor.TokenID
			}
			if todasGlobal(ctx) && contexto.PermisosDe(ctx).Tiene(string(nucleo.PermEmpresasAdoptar)) {
				if yo.HayDatosSinEmpresa, err = hayDatosSinEmpresa(ctx, t); err != nil {
					return Yo{}, err
				}
			}
			return yo, nil
		},
	})
}

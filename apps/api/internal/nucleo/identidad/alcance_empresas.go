package identidad

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
	identidadbd "github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad/bd"
)

// Alcance por empresa de la administración de usuarios: «nadie da más de lo que tiene» también
// en empresas. Quien no tiene empresas.todas sin límite de alcance (admin la tiene: por ahora
// admin es siempre global) solo administra usuarios cuyas empresas están todas entre las suyas.
// Aplica a cualquier actor sin empresas.todas global: tokens limitados, roles personalizados.

// alcanceEmpresas son las empresas sobre las que el actor administra.
type alcanceEmpresas struct {
	// todas: empresas.todas global, actor sistema o modo «sin conector» (sin empresas
	// registradas no hay separación por empresa).
	todas   bool
	propias []int64
}

// todasGlobal indica si el actor del contexto tiene empresas.todas sin límite de alcance.
func todasGlobal(ctx context.Context) bool {
	return contexto.PermisosDe(ctx).TieneGlobal(string(nucleo.PermEmpresasTodas))
}

// alcanceDelActor calcula las empresas del actor (las asignadas a su usuario; un token usa las
// de su dueño).
func alcanceDelActor(ctx context.Context, q *identidadbd.Queries) (alcanceEmpresas, error) {
	if todasGlobal(ctx) {
		return alcanceEmpresas{todas: true}, nil
	}
	hay, err := q.HayEmpresas(ctx)
	if err != nil {
		return alcanceEmpresas{}, err
	}
	if !hay {
		return alcanceEmpresas{todas: true}, nil
	}
	actor := contexto.ActorDe(ctx)
	if !actor.EsUsuario() {
		return alcanceEmpresas{propias: []int64{}}, nil
	}
	mapa, err := empresasDe(ctx, q, actor.ID)
	if err != nil {
		return alcanceEmpresas{}, err
	}
	return alcanceEmpresas{propias: lista(mapa[actor.ID])}, nil
}

// exigirEmpresasDe exige que el actor pueda administrar al usuario objetivo por empresa: sin
// empresas.todas global, cada empresa del objetivo debe ser del actor; un objetivo sin
// empresas, o con empresas.todas, solo lo administra quien tiene empresas.todas global. El
// propio usuario (sesión o token suyo) no se restringe por esta regla (son sus empresas).
func exigirEmpresasDe(ctx context.Context, q *identidadbd.Queries, objetivo int64, que string) error {
	a, err := alcanceDelActor(ctx, q)
	if err != nil || a.todas {
		return err
	}
	if actor := contexto.ActorDe(ctx); actor.EsUsuario() && actor.ID == objetivo {
		return nil
	}
	global, err := q.TieneEmpresasTodasGlobal(ctx, objetivo)
	if err != nil {
		return err
	}
	if global {
		return nucleo.Prohibido(fmt.Sprintf("No puedes %s: opera en todas las empresas y tú no", que))
	}
	mapa, err := empresasDe(ctx, q, objetivo)
	if err != nil {
		return err
	}
	if len(mapa[objetivo]) == 0 {
		return nucleo.Prohibido(fmt.Sprintf("No puedes %s: no tiene empresas asignadas; solo quien opera en todas las empresas puede hacerlo", que))
	}
	for _, e := range mapa[objetivo] {
		if !slices.Contains(a.propias, e) {
			return nucleo.Prohibido(fmt.Sprintf("No puedes %s: opera en la empresa %d, en que tú no operas", que, e))
		}
	}
	return nil
}

// UsuarioEnEmpresa indica si el usuario puede operar en la empresa: la tiene asignada o tiene
// empresas.todas sin límite de alcance (rol global). Con empresaID 0 (modo «sin conector»: el
// dato tiene empresa_id null) es true solo si no hay empresas registradas; con empresas
// registradas un dato sin empresa no es de nadie (ver POST /empresas/{id}/adoptar-sin-empresa).
// Para I2: comprobar que el usuario al que se asigna un ticket u orden opera en la empresa de
// esa entidad (si no, 422).
func UsuarioEnEmpresa(ctx context.Context, bd identidadbd.DBTX, usuarioID, empresaID int64) (bool, error) {
	q := identidadbd.New(bd)
	if empresaID == 0 {
		hay, err := q.HayEmpresas(ctx)
		return !hay, err
	}
	return q.UsuarioEnEmpresa(ctx, identidadbd.UsuarioEnEmpresaParams{UsuarioID: usuarioID, EmpresaID: empresaID})
}

// exigirVisible es exigirEmpresasDe para acciones sobre un usuario por id: si el objetivo está
// fuera del alcance de empresas del actor responde 404, igual que si no existiera, para no
// revelar que existe ni su versión. Va antes de ExigirVersion.
func exigirVisible(ctx context.Context, q *identidadbd.Queries, objetivo int64, que string) error {
	err := exigirEmpresasDe(ctx, q, objetivo, que)
	var p *nucleo.Problema
	if errors.As(err, &p) && p.Status == http.StatusForbidden {
		return nucleo.NoEncontrado("")
	}
	return err
}

// Package contexto lleva en context.Context quién ejecuta (Actor), por qué canal y con qué
// request id, además de los permisos efectivos del actor. Lo llenan el middleware HTTP, los
// workers y la consola; lo leen los casos de uso (internal/auditoria) y la auditoría.
package contexto

import (
	"context"
	"slices"

	"github.com/google/uuid"
)

// TipoActor distingue a quien ejecuta una acción.
type TipoActor string

const (
	// ActorUsuario es una persona con sesión del panel o de la app de campo.
	ActorUsuario TipoActor = "usuario"
	// ActorToken es un token de API (iris_pat_) de un usuario persona.
	ActorToken TipoActor = "token"
	// ActorIntegracion es un token de un usuario de tipo integración (Argos, Horus).
	ActorIntegracion TipoActor = "integracion"
	// ActorSistema son los workers, crons y la consola: tiene todos los permisos.
	ActorSistema TipoActor = "sistema"
	// ActorAnonimo es una petición sin credenciales (p. ej. el inicio de sesión).
	ActorAnonimo TipoActor = "anonimo"
)

// Actor es quien ejecuta. ID es nucleo.usuarios.id (también para tokens e integraciones);
// 0 para sistema y anónimo. TokenID es el id en nucleo.tokens_api cuando entró por token.
type Actor struct {
	Tipo    TipoActor
	ID      int64
	Nombre  string
	TokenID int64
}

// EsUsuario indica si ID apunta a nucleo.usuarios.
func (a Actor) EsUsuario() bool {
	return a.ID != 0 && (a.Tipo == ActorUsuario || a.Tipo == ActorToken || a.Tipo == ActorIntegracion)
}

// Canal es por dónde entró la acción.
type Canal string

const (
	CanalPanel       Canal = "panel"
	CanalCampo       Canal = "campo"
	CanalPortal      Canal = "portal"
	CanalAPI         Canal = "api"
	CanalIntegracion Canal = "integracion"
	CanalWebhook     Canal = "webhook"
	CanalWorker      Canal = "worker"
	CanalCron        Canal = "cron"
	CanalConsola     Canal = "consola"
)

// Alcance limita un permiso a una zona o a una cuadrilla. El valor cero es alcance global.
type Alcance struct {
	ZonaID      int64
	CuadrillaID int64
}

// Global indica que el alcance no está limitado.
func (a Alcance) Global() bool { return a.ZonaID == 0 && a.CuadrillaID == 0 }

// Permisos son los permisos efectivos del actor con su alcance. nil = sin permisos.
type Permisos struct {
	todos bool
	mapa  map[string][]Alcance
}

// TodosLosPermisos es el conjunto del actor sistema.
func TodosLosPermisos() Permisos { return Permisos{todos: true} }

// NuevosPermisos arma el conjunto a partir de permiso → alcances (vacío o con un Alcance global
// = sin límite).
func NuevosPermisos(mapa map[string][]Alcance) Permisos {
	return Permisos{mapa: mapa}
}

// Tiene indica si el actor tiene el permiso con cualquier alcance.
func (p Permisos) Tiene(permiso string) bool {
	if p.todos {
		return true
	}
	_, ok := p.mapa[permiso]
	return ok
}

// TieneGlobal indica si el actor tiene el permiso sin límite de zona ni cuadrilla.
func (p Permisos) TieneGlobal(permiso string) bool {
	if p.todos {
		return true
	}
	alcances, ok := p.mapa[permiso]
	if !ok {
		return false
	}
	return slices.ContainsFunc(alcances, Alcance.Global)
}

// Alcances devuelve las zonas y cuadrillas a las que se limita el permiso. global = true
// significa sin límite (y las listas vienen vacías). Para filtrar consultas de I2.
func (p Permisos) Alcances(permiso string) (global bool, zonas, cuadrillas []int64) {
	if p.TieneGlobal(permiso) {
		return true, nil, nil
	}
	for _, a := range p.mapa[permiso] {
		if a.ZonaID != 0 {
			zonas = append(zonas, a.ZonaID)
		}
		if a.CuadrillaID != 0 {
			cuadrillas = append(cuadrillas, a.CuadrillaID)
		}
	}
	return false, zonas, cuadrillas
}

// Lista devuelve los códigos de permiso, ordenados.
func (p Permisos) Lista() []string {
	salida := make([]string, 0, len(p.mapa))
	for codigo := range p.mapa {
		salida = append(salida, codigo)
	}
	slices.Sort(salida)
	return salida
}

// Sesion es la sesión del panel que autenticó la petición (0 = no hubo sesión).
type Sesion struct {
	ID           int64
	UsuarioID    int64
	MFAPendiente bool
}

type clave int

const (
	claveActor clave = iota
	claveCanal
	claveRequestID
	clavePermisos
	claveSesion
	claveEmpresa
)

// ConActor fija el actor y sus permisos.
func ConActor(ctx context.Context, actor Actor, permisos Permisos) context.Context {
	ctx = context.WithValue(ctx, claveActor, actor)
	return context.WithValue(ctx, clavePermisos, permisos)
}

// ActorDe devuelve el actor; sin actor, anónimo.
func ActorDe(ctx context.Context) Actor {
	if a, ok := ctx.Value(claveActor).(Actor); ok {
		return a
	}
	return Actor{Tipo: ActorAnonimo}
}

// PermisosDe devuelve los permisos efectivos del actor.
func PermisosDe(ctx context.Context) Permisos {
	if p, ok := ctx.Value(clavePermisos).(Permisos); ok {
		return p
	}
	return Permisos{}
}

// ConCanal fija el canal.
func ConCanal(ctx context.Context, canal Canal) context.Context {
	return context.WithValue(ctx, claveCanal, canal)
}

// CanalDe devuelve el canal (api por defecto).
func CanalDe(ctx context.Context) Canal {
	if c, ok := ctx.Value(claveCanal).(Canal); ok {
		return c
	}
	return CanalAPI
}

// ConRequestID fija el request id.
func ConRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, claveRequestID, id)
}

// RequestIDDe devuelve el request id ("" si no hay).
func RequestIDDe(ctx context.Context) string {
	id, _ := ctx.Value(claveRequestID).(string)
	return id
}

// ConSesion fija la sesión del panel que autenticó la petición.
func ConSesion(ctx context.Context, s Sesion) context.Context {
	return context.WithValue(ctx, claveSesion, s)
}

// SesionDe devuelve la sesión del panel (ID 0 si la petición no vino con sesión).
func SesionDe(ctx context.Context) Sesion {
	s, _ := ctx.Value(claveSesion).(Sesion)
	return s
}

// ConEmpresa fija la empresa activa (id de Argos). 0 = sin empresa activa.
func ConEmpresa(ctx context.Context, empresaID int64) context.Context {
	return context.WithValue(ctx, claveEmpresa, empresaID)
}

// EmpresaDe devuelve la empresa activa (0 = ninguna).
func EmpresaDe(ctx context.Context) int64 {
	id, _ := ctx.Value(claveEmpresa).(int64)
	return id
}

// NuevoRequestID genera un id (uuid v7).
func NuevoRequestID() string {
	return uuid.Must(uuid.NewV7()).String()
}

// Sistema prepara el contexto de un worker, cron o comando de consola: actor sistema con todos
// los permisos, el canal dado y un request id nuevo si no había.
func Sistema(ctx context.Context, canal Canal) context.Context {
	ctx = ConActor(ctx, Actor{Tipo: ActorSistema, Nombre: string(canal)}, TodosLosPermisos())
	ctx = ConCanal(ctx, canal)
	if RequestIDDe(ctx) == "" {
		ctx = ConRequestID(ctx, NuevoRequestID())
	}
	return ctx
}

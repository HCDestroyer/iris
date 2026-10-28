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
	// ActorCliente es el cliente final de Argos en el portal de soporte (/soporte): no es un
	// usuario de Iris; entra con una sesión de portal (cookie iris_portal) con su cliente y su
	// empresa de Argos fijos. Solo lo aceptan los casos de uso con Acceso auditoria.Cliente.
	ActorCliente TipoActor = "cliente"
)

// Actor es quien ejecuta. ID es nucleo.usuarios.id (también para tokens e integraciones);
// 0 para sistema, anónimo y cliente. TokenID es el id en nucleo.tokens_api cuando entró por
// token. ClienteID y EmpresaID son los de Argos del actor cliente (0 para los demás).
type Actor struct {
	Tipo      TipoActor
	ID        int64
	Nombre    string
	TokenID   int64
	ClienteID int64
	EmpresaID int64
}

// EsUsuario indica si ID apunta a nucleo.usuarios.
func (a Actor) EsUsuario() bool {
	return a.ID != 0 && (a.Tipo == ActorUsuario || a.Tipo == ActorToken || a.Tipo == ActorIntegracion)
}

// EsCliente indica si es el actor cliente del portal, con su cliente y su empresa.
func (a Actor) EsCliente() bool {
	return a.Tipo == ActorCliente && a.ClienteID > 0 && a.EmpresaID > 0
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

// Cubre indica si p incluye a otro: cada permiso de otro, con cada uno de sus alcances, está en
// p sin límite o con ese mismo alcance. Si no, devuelve el primer permiso que falta.
func (p Permisos) Cubre(otro Permisos) (falta string, ok bool) {
	if p.todos {
		return "", true
	}
	if otro.todos {
		return "*", false
	}
	for _, codigo := range otro.Lista() {
		if p.TieneGlobal(codigo) {
			continue
		}
		for _, a := range otro.mapa[codigo] {
			if a.Global() || !slices.Contains(p.mapa[codigo], a) {
				return codigo, false
			}
		}
	}
	return "", true
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
	claveEmpresas
	claveSesionPortal
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

// ConSesionPortal fija la sesión del portal que autenticó al actor cliente.
func ConSesionPortal(ctx context.Context, id int64) context.Context {
	return context.WithValue(ctx, claveSesionPortal, id)
}

// SesionPortalDe devuelve la sesión del portal (0 si la petición no vino con una).
func SesionPortalDe(ctx context.Context) int64 {
	id, _ := ctx.Value(claveSesionPortal).(int64)
	return id
}

// ConEmpresa fija la empresa activa (id de Argos). 0 = sin empresa activa. En HTTP la fija el
// middleware de empresa (identidad.MiddlewareEmpresa) con ConEmpresas; los workers y las
// pruebas pueden llamarla directo.
func ConEmpresa(ctx context.Context, empresaID int64) context.Context {
	return context.WithValue(ctx, claveEmpresa, empresaID)
}

// EmpresaDe devuelve la empresa activa (0 = ninguna). Ojo: 0 NO significa «sin filtro» para un
// usuario. Con un usuario detrás (Actor.EsUsuario), los casos de uso operativos solo corren con
// EstadoEmpresa Activa (filtrar por este id) o SinConector (solo datos con empresa_id null):
// auditoria.Caso corta los demás estados. Solo el actor sistema (workers, crons, consola) ve 0
// con el significado de «todas».
func EmpresaDe(ctx context.Context) int64 {
	id, _ := ctx.Value(claveEmpresa).(int64)
	return id
}

// EstadoEmpresa dice cómo quedó resuelta la empresa activa de una petición.
type EstadoEmpresa string

const (
	// EmpresaNoResuelta: nadie la resolvió (actor sistema, consola, worker o pruebas que llaman
	// casos de uso directo). Se usa EmpresaDe tal cual.
	EmpresaNoResuelta EstadoEmpresa = ""
	// EmpresaActiva: hay empresa activa (EmpresaDe != 0); todo lo operativo se filtra por ella.
	EmpresaActiva EstadoEmpresa = "activa"
	// EmpresaSinConector: no hay empresas registradas (nucleo.empresas vacía): Iris trabaja sin
	// Argos y lo operativo lleva empresa_id null.
	EmpresaSinConector EstadoEmpresa = "sin_conector"
	// EmpresaPorElegir: el actor puede operar en varias y no mandó X-Iris-Empresa (422
	// empresa_requerida en lo operativo).
	EmpresaPorElegir EstadoEmpresa = "por_elegir"
	// EmpresaSinAsignar: hay empresas pero el actor no tiene ninguna (403 sin_empresa en lo
	// operativo).
	EmpresaSinAsignar EstadoEmpresa = "sin_empresa"
)

// Empresas es el resultado de resolver la empresa activa de una petición.
type Empresas struct {
	Estado EstadoEmpresa
	// Activa es la empresa activa (0 salvo con Estado EmpresaActiva).
	Activa int64
	// Disponibles son las empresas en que el actor puede operar (ordenadas).
	Disponibles []int64
	// Todas: el actor tiene empresas.todas (opera en cualquier empresa registrada).
	Todas bool
}

// ConEmpresas fija el resultado del middleware de empresa y la empresa activa (ConEmpresa).
func ConEmpresas(ctx context.Context, e Empresas) context.Context {
	ctx = context.WithValue(ctx, claveEmpresas, e)
	return ConEmpresa(ctx, e.Activa)
}

// EmpresasDe devuelve cómo se resolvió la empresa activa (Estado EmpresaNoResuelta si nadie lo
// hizo).
func EmpresasDe(ctx context.Context) Empresas {
	e, _ := ctx.Value(claveEmpresas).(Empresas)
	return e
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

// ComoCliente prepara el contexto del actor cliente del portal: el actor (sin permisos de
// usuario), el canal portal y su empresa como empresa activa (fija: no la elige él).
func ComoCliente(ctx context.Context, a Actor) context.Context {
	ctx = ConActor(ctx, a, Permisos{})
	ctx = ConCanal(ctx, CanalPortal)
	return ConEmpresas(ctx, Empresas{Estado: EmpresaActiva, Activa: a.EmpresaID, Disponibles: []int64{a.EmpresaID}})
}

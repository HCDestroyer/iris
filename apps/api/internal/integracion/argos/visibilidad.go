package argos

import (
	"cmp"
	"context"
	"slices"

	argosbd "github.com/HCDestroyer/iris/apps/api/internal/integracion/argos/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
)

// Datos sensibles del servicio (contrato argosisp.md 0.8.0 §2.4; §10.2 puntos 4 y 5).
//
//   - servicios.ver_red: router, IP, prefijos IPv6, user_ppp, tipo de conexión y planta.
//   - servicios.ver_ubicacion: dirección, referencia, coordenadas y contacto en sitio.
//
// El filtro es del SERVIDOR: sin el permiso, el campo no sale por la API (ni vacío). Todo exige
// además clientes.ver. Un servicio muestra el dato si el actor:
//
//  1. tiene el permiso global, o con alcance de la zona del servicio, o el servicio es de una de
//     «sus órdenes» (abajo); y
//  2. si sus órdenes están acotadas (ordenes.ver_propias, u ordenes.ver con alcance de zona o
//     cuadrilla, sin ordenes.ver global): el servicio es de una de sus órdenes.
//
// «Sus órdenes» (mismo criterio que campo/comun.go): de la empresa activa, abiertas o terminadas
// (cerrada, fallida) hace menos de 24 h (nunca canceladas), asignadas a él o en una zona o cuadrilla
// que cubre su ordenes.ver. Una orden reasignada deja de ser suya. El actor sistema lo ve todo.
// El precio, el plan, la zona y el estado los ve cualquiera con clientes.ver.

// alcanceDato es lo que da un permiso servicios.ver_*.
type alcanceDato struct {
	tiene, global bool
	zonas         map[int64]bool
}

func alcanceDe(p contexto.Permisos, permiso nucleo.Permiso) alcanceDato {
	a := alcanceDato{tiene: p.Tiene(string(permiso))}
	if !a.tiene {
		return a
	}
	global, zonas, _ := p.Alcances(string(permiso))
	a.global = global
	a.zonas = map[int64]bool{}
	for _, z := range zonas {
		a.zonas[z] = true
	}
	return a
}

// Visibilidad dice qué datos sensibles del servicio puede ver el actor.
type Visibilidad struct {
	red, ubicacion alcanceDato
	// deOrdenes: los servicios de sus órdenes (ver arriba).
	deOrdenes map[int64]bool
	// acotado: sus órdenes están acotadas (regla 2).
	acotado bool
}

// VisibilidadTotal: lo ve todo (jobs, pruebas internas).
var VisibilidadTotal = Visibilidad{red: alcanceDato{tiene: true, global: true}, ubicacion: alcanceDato{tiene: true, global: true}}

// LeerVisibilidad arma la visibilidad del actor del contexto (una consulta a campo.ordenes si hace
// falta, con bd o, si es nil, con la transacción o la base del contexto).
func LeerVisibilidad(ctx context.Context, bd argosbd.DBTX) (Visibilidad, error) {
	p := contexto.PermisosDe(ctx)
	if !p.Tiene(string(nucleo.PermClientesVer)) {
		return Visibilidad{}, nil
	}
	v := Visibilidad{red: alcanceDe(p, nucleo.PermServiciosVerRed), ubicacion: alcanceDe(p, nucleo.PermServiciosVerUbicacion)}
	if !v.red.tiene && !v.ubicacion.tiene {
		return v, nil
	}
	ordenesGlobal := p.TieneGlobal(string(nucleo.PermOrdenesVer))
	v.acotado = !ordenesGlobal && (p.Tiene(string(nucleo.PermOrdenesVer)) || p.Tiene(string(nucleo.PermOrdenesVerPropias)))
	if ordenesGlobal || (!v.acotado && v.red.global == v.red.tiene && v.ubicacion.global == v.ubicacion.tiene) {
		// Sin regla de órdenes y con permisos globales: no hace falta leer órdenes.
		return v, nil
	}
	v.deOrdenes = map[int64]bool{}
	a := contexto.ActorDe(ctx)
	if !a.EsUsuario() {
		return v, nil
	}
	if bd == nil {
		if tx := nucleo.TxDe(ctx); tx != nil {
			bd = tx
		} else if pool := nucleo.BDDe(ctx); pool != nil {
			bd = pool
		} else {
			return Visibilidad{}, nucleo.ErrSinBD
		}
	}
	_, zonas, cuadrillas := p.Alcances(string(nucleo.PermOrdenesVer))
	if !p.Tiene(string(nucleo.PermOrdenesVer)) {
		zonas, cuadrillas = nil, nil
	}
	ids, err := argosbd.New(bd).ServiciosDeOrdenesVisibles(ctx, argosbd.ServiciosDeOrdenesVisiblesParams{
		UsuarioID: a.ID, EmpresaID: contexto.EmpresaDe(ctx), Zonas: nuncaNilIDs(zonas), Cuadrillas: nuncaNilIDs(cuadrillas),
		// Las asignadas a él cuentan solo si ve sus órdenes (ordenes.ver_propias).
		Propias: p.Tiene(string(nucleo.PermOrdenesVerPropias)),
	})
	if err != nil {
		return Visibilidad{}, err
	}
	for _, id := range ids {
		v.deOrdenes[id] = true
	}
	return v, nil
}

func nuncaNilIDs(ids []int64) []int64 {
	if ids == nil {
		return []int64{}
	}
	return ids
}

func (v Visibilidad) puede(a alcanceDato, servicioID int64, zonaID *int64) bool {
	if !a.tiene {
		return false
	}
	deOrden := v.deOrdenes[servicioID]
	if v.acotado && !deOrden {
		return false
	}
	return a.global || (zonaID != nil && a.zonas[*zonaID]) || deOrden
}

// Red indica si el actor ve los datos de red del servicio (zonaID: la zona del servicio, nil si no
// se sabe).
func (v Visibilidad) Red(servicioID int64, zonaID *int64) bool {
	return v.puede(v.red, servicioID, zonaID)
}

// Ubicacion indica si el actor ve la dirección y la ubicación del servicio.
func (v Visibilidad) Ubicacion(servicioID int64, zonaID *int64) bool {
	return v.puede(v.ubicacion, servicioID, zonaID)
}

// PuedeRed: el actor tiene servicios.ver_red con algún alcance (para cortar antes una lectura).
func (v Visibilidad) PuedeRed() bool { return v.red.tiene }

// Recortar quita del servicio lo que el actor no puede ver.
func (v Visibilidad) Recortar(s *Servicio) {
	if !v.Red(s.ID, s.ZonaID) {
		s.RouterID, s.RouterNombre, s.IP, s.PrefijoEnlaceV6, s.PrefijoDelegadoV6 = nil, "", "", "", ""
		s.UserPPP, s.TipoConexion, s.Planta = "", "", nil
	}
	if !v.Ubicacion(s.ID, s.ZonaID) {
		s.Direccion, s.Lat, s.Lon, s.Referencia = "", nil, nil, ""
		s.ContactoNombre, s.ContactoTelefono, s.ContactoTelefonos = "", "", nil
	}
}

// RecortarTodos aplica Recortar a cada servicio.
func (v Visibilidad) RecortarTodos(s []Servicio) {
	for i := range s {
		v.Recortar(&s[i])
	}
}

// Anotacion es lo que queda en la auditoría de la lectura (§2.4: lectura de datos personales):
// los ids de los servicios cuyos datos de red o de ubicación salieron, nunca los valores.
func (v Visibilidad) Anotacion(servicios []Servicio) map[string]any {
	red, ubic := []int64{}, []int64{}
	for _, s := range servicios {
		if v.Red(s.ID, s.ZonaID) {
			red = append(red, s.ID)
		}
		if v.Ubicacion(s.ID, s.ZonaID) {
			ubic = append(ubic, s.ID)
		}
	}
	return map[string]any{"servicios_ver_red": red, "servicios_ver_ubicacion": ubic}
}

// RecortarCopia quita de la copia del servicio de un ticket u orden lo que el actor no puede ver.
// La dirección general de la copia (CopiaArgos.Direccion) la decide quien la expone: la orden
// conserva su propia dirección con los permisos de la orden (§2.4).
func (v Visibilidad) RecortarCopia(c *CopiaServicio, zonaID *int64) {
	if c == nil {
		return
	}
	if !v.Red(c.ID, zonaID) {
		c.IP, c.RouterNombre = "", ""
	}
	if !v.Ubicacion(c.ID, zonaID) {
		c.Direccion, c.Lat, c.Lon = "", nil, nil
	}
}

// Numerar ordena los servicios por id y les pone su número (1, 2, 3…): «Servicio 2» en el panel
// y en el asistente (§2.4, etiqueta corta). Se numera la lista que se muestra (la de la empresa).
func Numerar(s []Servicio) {
	slices.SortFunc(s, func(a, b Servicio) int { return cmp.Compare(a.ID, b.ID) })
	for i := range s {
		s[i].Numero = i + 1
	}
}

// ServiciosVisiblesDeCliente: los servicios de la instantánea del cliente en la empresa (0 = todas),
// numerados y recortados según lo que el actor puede ver. Para fichas de otros módulos
// (conversaciones, tickets, órdenes).
func ServiciosVisiblesDeCliente(ctx context.Context, bd argosbd.DBTX, clienteID, empresa int64) ([]Servicio, error) {
	filas, err := argosbd.New(bd).ServiciosDeCliente(ctx, clienteID)
	if err != nil {
		return nil, err
	}
	return ServiciosVisibles(ctx, bd, filas, empresa)
}

// ServiciosVisibles: como ServiciosVisiblesDeCliente, con las filas ya leídas (otro módulo las
// convierte: argosbd.IntegracionArgosServicio(fila), mismo esquema). bd puede ser nil.
func ServiciosVisibles(ctx context.Context, bd argosbd.DBTX, filas []argosbd.IntegracionArgosServicio, empresa int64) ([]Servicio, error) {
	filas = slices.DeleteFunc(filas, func(f argosbd.IntegracionArgosServicio) bool { return empresa != 0 && f.EmpresaID != empresa })
	salida := aServicios(filas)
	vis, err := LeerVisibilidad(ctx, bd)
	if err != nil {
		return nil, err
	}
	vis.RecortarTodos(salida)
	return salida, nil
}

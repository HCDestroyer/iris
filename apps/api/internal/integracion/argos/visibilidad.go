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
// El filtro es del SERVIDOR: sin el permiso, el campo no sale por la API (ni vacío). Quien solo
// ve sus órdenes (ordenes.ver_propias sin ordenes.ver, el técnico) recibe esos datos solo de los
// servicios de las órdenes asignadas a él. El actor sistema tiene todos los permisos.
// El precio, el plan, la zona y el estado los ve cualquiera con clientes.ver.

// Visibilidad dice qué datos sensibles del servicio puede ver el actor.
type Visibilidad struct {
	red, ubicacion bool
	// propias != nil: solo los servicios de sus órdenes asignadas.
	propias map[int64]bool
}

// VisibilidadTotal: lo ve todo (jobs, pruebas internas).
var VisibilidadTotal = Visibilidad{red: true, ubicacion: true}

// LeerVisibilidad arma la visibilidad del actor del contexto. Con ordenes.ver_propias sin
// ordenes.ver lee los servicios de sus órdenes asignadas (una consulta, con bd o, si es nil, con
// la transacción o la base del contexto).
func LeerVisibilidad(ctx context.Context, bd argosbd.DBTX) (Visibilidad, error) {
	p := contexto.PermisosDe(ctx)
	v := Visibilidad{red: p.Tiene(string(nucleo.PermServiciosVerRed)), ubicacion: p.Tiene(string(nucleo.PermServiciosVerUbicacion))}
	if !v.red && !v.ubicacion {
		return v, nil
	}
	if p.Tiene(string(nucleo.PermOrdenesVer)) || !p.Tiene(string(nucleo.PermOrdenesVerPropias)) {
		return v, nil
	}
	a := contexto.ActorDe(ctx)
	v.propias = map[int64]bool{}
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
	ids, err := argosbd.New(bd).ServiciosDeOrdenesAsignadas(ctx, a.ID)
	if err != nil {
		return Visibilidad{}, err
	}
	for _, id := range ids {
		v.propias[id] = true
	}
	return v, nil
}

// Red indica si el actor ve los datos de red del servicio.
func (v Visibilidad) Red(servicioID int64) bool {
	return v.red && (v.propias == nil || v.propias[servicioID])
}

// Ubicacion indica si el actor ve la dirección y la ubicación del servicio.
func (v Visibilidad) Ubicacion(servicioID int64) bool {
	return v.ubicacion && (v.propias == nil || v.propias[servicioID])
}

// Recortar quita del servicio lo que el actor no puede ver.
func (v Visibilidad) Recortar(s *Servicio) {
	if !v.Red(s.ID) {
		s.RouterID, s.RouterNombre, s.IP, s.PrefijoEnlaceV6, s.PrefijoDelegadoV6 = nil, "", "", "", ""
		s.UserPPP, s.TipoConexion, s.Planta = "", "", nil
	}
	if !v.Ubicacion(s.ID) {
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
		if v.Red(s.ID) {
			red = append(red, s.ID)
		}
		if v.Ubicacion(s.ID) {
			ubic = append(ubic, s.ID)
		}
	}
	return map[string]any{"servicios_ver_red": red, "servicios_ver_ubicacion": ubic}
}

// RecortarCopia quita de la copia del servicio de un ticket u orden lo que el actor no puede ver.
// La dirección general de la copia (CopiaArgos.Direccion) la decide quien la expone: la orden
// conserva su propia dirección con los permisos de la orden (§2.4).
func (v Visibilidad) RecortarCopia(c *CopiaServicio) {
	if c == nil {
		return
	}
	if !v.Red(c.ID) {
		c.IP, c.RouterNombre = "", ""
	}
	if !v.Ubicacion(c.ID) {
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

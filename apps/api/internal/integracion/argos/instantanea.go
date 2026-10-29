package argos

import (
	"context"
	"time"

	argosbd "github.com/HCDestroyer/iris/apps/api/internal/integracion/argos/bd"
)

// Instantáneas de lectura (§2.2): solo lo que Iris muestra, con empresa_id y sincronizado_en.
// sincronizado_en es la hora en que se pidió a Argos (antes de la llamada): una lectura más
// vieja que llegue tarde no pisa una más nueva (WHERE sincronizado_en <= excluded…).

// fichaArgos es lo que se leyó de Argos para refrescar la instantánea de un cliente.
type fichaArgos struct {
	cliente   ClienteArgos
	servicios []ServicioArgos
	leidoEn   time.Time
}

// leerFichaArgos lee cliente y servicios (fuera de toda transacción).
func leerFichaArgos(ctx context.Context, c *Cliente, clienteID int64) (fichaArgos, error) {
	f := fichaArgos{leidoEn: time.Now().UTC()}
	var err error
	if f.cliente, err = c.ObtenerCliente(ctx, clienteID); err != nil {
		return f, err
	}
	if f.servicios, err = c.ServiciosDeCliente(ctx, clienteID); err != nil {
		return f, err
	}
	return f, nil
}

// guardarFicha escribe la instantánea del cliente y sus servicios y borra los servicios que
// Argos ya no devuelve.
func guardarFicha(ctx context.Context, q *argosbd.Queries, f fichaArgos) error {
	c := f.cliente
	lat, lon := c.LatLon()
	// El cliente de Argos no tiene empresa: las suyas son las de sus servicios. empresa_id queda
	// como la primera (0 sin servicios), por compatibilidad.
	empresas := EmpresasDelCliente(c, f.servicios)
	primera := int64(0)
	if len(empresas) > 0 {
		primera = empresas[0]
	}
	if err := q.GuardarCliente(ctx, argosbd.GuardarClienteParams{
		ID: c.ID, EmpresaID: primera, Empresas: empresas, Codigo: c.Codigo, Nombre: c.Nombre, Telefono: c.Telefono,
		Direccion: c.Direccion, Lat: lat, Lon: lon, ZonaID: c.IDZona(), Estado: c.Estado, SincronizadoEn: f.leidoEn,
	}); err != nil {
		return err
	}
	vigentes := make([]int64, 0, len(f.servicios))
	for _, s := range f.servicios {
		if s.ClienteID == 0 {
			s.ClienteID = c.ID
		}
		if s.ClienteID != c.ID {
			continue
		}
		if s.EmpresaID == 0 {
			s.EmpresaID = primera
		}
		slat, slon := s.LatLon()
		if err := q.GuardarServicio(ctx, argosbd.GuardarServicioParams{
			ID: s.ID, EmpresaID: s.EmpresaID, ClienteID: s.ClienteID, Codigo: s.Codigo,
			PlanID: idDe(s.PlanID, s.Plan), PlanNombre: nombreDe(s.PlanNombre, s.Plan),
			RouterID: idDe(s.RouterID, s.Router), RouterNombre: nombreDe(s.RouterNombre, s.Router),
			ZonaID: idDe(s.ZonaID, s.Zona), ZonaNombre: nombreDe(s.ZonaNombre, s.Zona),
			Ip: s.IP, Direccion: s.Direccion, Lat: slat, Lon: slon, Estado: s.Estado, SincronizadoEn: f.leidoEn,
		}); err != nil {
			return err
		}
		vigentes = append(vigentes, s.ID)
	}
	return q.BorrarServiciosAusentes(ctx, argosbd.BorrarServiciosAusentesParams{
		ClienteID: c.ID, Vigentes: vigentes, SincronizadoEn: f.leidoEn,
	})
}

func aClienteResumen(f argosbd.IntegracionArgosCliente) ClienteResumen {
	empresas := empresasDeFila(f)
	if empresas == nil {
		empresas = []int64{}
	}
	return ClienteResumen{
		ID: f.ID, EmpresaID: f.EmpresaID, Empresas: empresas, Codigo: f.Codigo, Nombre: f.Nombre, Telefono: f.Telefono,
		Direccion: f.Direccion, Lat: f.Lat, Lon: f.Lon, ZonaID: f.ZonaID, Estado: f.Estado,
	}
}

func clienteArgosAResumen(c ClienteArgos) ClienteResumen {
	lat, lon := c.LatLon()
	return ClienteResumen{
		ID: c.ID, EmpresaID: c.EmpresaID, Empresas: EmpresasDelCliente(c, nil), Codigo: c.Codigo, Nombre: c.Nombre, Telefono: c.Telefono,
		Direccion: c.Direccion, Lat: lat, Lon: lon, ZonaID: c.IDZona(), Estado: c.Estado,
	}
}

func aServicio(f argosbd.IntegracionArgosServicio) Servicio {
	return Servicio{
		ID: f.ID, EmpresaID: f.EmpresaID, ClienteID: f.ClienteID, Codigo: f.Codigo, PlanID: f.PlanID,
		PlanNombre: f.PlanNombre, RouterID: f.RouterID, RouterNombre: f.RouterNombre, ZonaID: f.ZonaID,
		ZonaNombre: f.ZonaNombre, IP: f.Ip, Direccion: f.Direccion, Lat: f.Lat, Lon: f.Lon, Estado: f.Estado,
	}
}

func aServicios(filas []argosbd.IntegracionArgosServicio) []Servicio {
	salida := make([]Servicio, 0, len(filas))
	for _, f := range filas {
		salida = append(salida, aServicio(f))
	}
	return salida
}

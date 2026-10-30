package argos

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	argosbd "github.com/HCDestroyer/iris/apps/api/internal/integracion/argos/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos/formato"
)

// Instantáneas de lectura (§2.2): solo lo que Iris muestra, con empresa_id y sincronizado_en.
// sincronizado_en es la hora en que se pidió a Argos (antes de la llamada): una lectura más
// vieja que llegue tarde no pisa una más nueva (WHERE sincronizado_en <= excluded…).

// fichaArgos es lo que se leyó de Argos para refrescar la instantánea de un cliente.
type fichaArgos struct {
	cliente   ClienteArgos
	servicios []ServicioArgos
	// completa: servicios es la lista de todas las empresas (empresas=todas); si no, solo lo del
	// alcance del token y una lista vacía no dice nada.
	completa bool
	// leidoEn: hora de Postgres (clock_timestamp()) antes de pedir a Argos.
	leidoEn time.Time
	// vivos: servicios de la lista con lápida que un GET por id confirmó con 200 (hora de Postgres
	// de antes del GET); solo esos resucitan.
	vivos map[int64]time.Time
}

// idsServicios: los ids de los servicios del cliente que se guardarían (para tomar sus candados
// en orden).
func (f fichaArgos) idsServicios() []int64 {
	ids := make([]int64, 0, len(f.servicios))
	for _, s := range f.servicios {
		if s.ClienteID == 0 || s.ClienteID == f.cliente.ID {
			ids = append(ids, s.ID)
		}
	}
	return ids
}

// leerFichaArgos lee cliente y servicios (fuera de toda transacción). bd da el reloj (Postgres) y
// las lápidas: los servicios de la lista que tienen una se confirman con GET /servicios/:id.
func leerFichaArgos(ctx context.Context, bd argosbd.DBTX, c *Cliente, clienteID int64) (fichaArgos, error) {
	var f fichaArgos
	var err error
	if f.leidoEn, err = argosbd.New(bd).RelojBD(ctx); err != nil {
		return f, err
	}
	if f.cliente, err = c.ObtenerCliente(ctx, clienteID); err != nil {
		return f, err
	}
	if f.servicios, f.completa, err = c.ServiciosDeClienteCompletos(ctx, clienteID); err != nil {
		return f, err
	}
	f.vivos, err = confirmarVivos(ctx, bd, eliminadoServicio, f.idsServicios(), func(ctx context.Context, id int64) error {
		_, err := c.obtenerServicioUnaVez(ctx, id)
		return err
	})
	return f, err
}

// empresasYPista: las empresas del cliente (las de sus servicios) y si no tiene servicios de forma
// concluyente (la lista de todas las empresas vino vacía). Un cliente recién creado por un alta
// desde Iris, todavía sin servicio, tiene como pista la empresa del prospecto: solo se ve en ella.
//
// Un cliente cuyos servicios Argos eliminó (lápidas) tampoco queda «sin servicios»: las empresas
// de esos servicios son la pista (si no, al eliminarse su único servicio lo verían todas).
func empresasYPista(ctx context.Context, q *argosbd.Queries, c ClienteArgos, servicios []ServicioArgos, completa bool) ([]int64, bool, error) {
	empresas := EmpresasDelCliente(c, servicios)
	if !completa || len(servicios) > 0 {
		return empresas, false, nil
	}
	pista, err := q.PistaEmpresaAlta(ctx, c.ID)
	switch {
	case err == nil:
		return []int64{pista}, false, nil
	case !errors.Is(err, pgx.ErrNoRows):
		return nil, false, err
	}
	if len(empresas) == 0 {
		de, err := q.EmpresasDeEliminadosDeCliente(ctx, &c.ID)
		if err != nil {
			return nil, false, err
		}
		if len(de) > 0 {
			return de, false, nil
		}
	}
	return empresas, len(empresas) == 0, nil
}

// guardarFicha escribe la instantánea del cliente y sus servicios y borra los servicios que
// Argos ya no devuelve.
func guardarFicha(ctx context.Context, t *auditoria.Tx, q *argosbd.Queries, f fichaArgos) error {
	// Candados de todos los servicios en orden de id, antes de tocar ninguna fila (ni la del
	// cliente): sin interbloqueos con una eliminación del mismo cliente.
	if err := bloquear(ctx, q, eliminadoServicio, f.idsServicios()); err != nil {
		return err
	}
	c := f.cliente
	lat, lon := c.LatLon()
	// El cliente de Argos no tiene empresa: las suyas son las de sus servicios. empresa_id queda
	// como la primera (0 sin servicios), por compatibilidad.
	empresas, sinServicios, err := empresasYPista(ctx, q, c, f.servicios, f.completa)
	if err != nil {
		return err
	}
	primera := int64(0)
	if len(empresas) > 0 {
		primera = empresas[0]
	}
	zonaNombre, err := nombreZonaCliente(ctx, q, c, f.servicios)
	if err != nil {
		return err
	}
	if err := q.GuardarCliente(ctx, argosbd.GuardarClienteParams{
		ID: c.ID, EmpresaID: primera, Empresas: empresas, SinServicios: sinServicios, Codigo: c.Codigo, Nombre: c.Nombre, Telefono: c.Telefono,
		Direccion: c.Direccion, Lat: lat, Lon: lon, ZonaID: c.IDZona(), Estado: c.Estado, SincronizadoEn: f.leidoEn,
		Tipo: c.Tipo, TipoDocumentoFiscal: c.TipoDocumentoFiscal, NitCui: c.NitCui, Movil: c.Movil, Correo: c.CorreoCliente(),
		ZonaNombre: zonaNombre, VersionDatos: VersionDatos,
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
		// Un servicio sin empresa no hereda la del cliente: queda en 0 y no se atribuye a nadie.
		slat, slon := s.LatLon()
		// En Argos la zona del servicio es la de su cliente (zona_nombre sale de core.clientes):
		// sin zona_id propio, el servicio toma la del cliente.
		zona := idDe(s.ZonaID, s.Zona)
		if zona == nil {
			zona = c.IDZona()
		}
		if err := guardarServicio(ctx, t, q, argosbd.GuardarServicioParams{
			ID: s.ID, EmpresaID: s.EmpresaID, ClienteID: s.ClienteID, Codigo: s.Codigo,
			PlanID: idDe(s.PlanID, s.Plan), PlanNombre: nombreDe(s.PlanNombre, s.Plan),
			RouterID: idDe(s.RouterID, s.Router), RouterNombre: nombreDe(s.RouterNombre, s.Router),
			ZonaID: zona, ZonaNombre: nombreDe(s.ZonaNombre, s.Zona),
			Ip: s.IPVigente(), Direccion: s.Direccion, Lat: slat, Lon: slon, Estado: s.Estado, SincronizadoEn: f.leidoEn,
			UserPpp: s.UserPPP, TipoConexion: s.TipoConexion,
		}, vivoEn(f.vivos, s.ID)); err != nil {
			return err
		}
		vigentes = append(vigentes, s.ID)
	}
	return q.BorrarServiciosAusentes(ctx, argosbd.BorrarServiciosAusentesParams{
		ClienteID: c.ID, Vigentes: vigentes, SincronizadoEn: f.leidoEn,
	})
}

func vivoEn(vivos map[int64]time.Time, id int64) *time.Time {
	if t, ok := vivos[id]; ok {
		return &t
	}
	return nil
}

// VersionDatos: qué guarda hoy la instantánea de un cliente (columna version_datos). Una fila
// con una versión menor se relee de Argos aunque tenga menos de 24 h.
const VersionDatos = 2

// nombreZonaCliente: Argos manda el nombre de la zona del cliente en cada servicio (zona_nombre);
// sin servicios, sale del catálogo de zonas de la instantánea. Vacío si no se sabe.
func nombreZonaCliente(ctx context.Context, q *argosbd.Queries, c ClienteArgos, servicios []ServicioArgos) (string, error) {
	if c.Zona != nil && c.Zona.Nombre != "" {
		return c.Zona.Nombre, nil
	}
	for _, s := range servicios {
		if n := nombreDe(s.ZonaNombre, s.Zona); n != "" && (s.ClienteID == 0 || s.ClienteID == c.ID) {
			return n, nil
		}
	}
	if id := c.IDZona(); id != nil {
		n, err := q.NombreZona(ctx, *id)
		if errors.Is(err, pgx.ErrNoRows) {
			return "", nil
		}
		return n, err
	}
	return "", nil
}

// gravedadEstado: el estado más grave de los servicios es el que resume al cliente (el mismo
// criterio que Argos en GET /clientes/:id/dependientes).
var gravedadEstado = []string{"suspendido", "limitado", "activo", "pendiente_instalacion", "retirado"}

// ResumenEstado resume el estado de un cliente por el de sus servicios: Argos 1.4.0 no tiene
// estado de cliente. Vacío sin servicios.
func ResumenEstado(estados []string) string {
	for _, e := range gravedadEstado {
		if slices.Contains(estados, e) {
			return e
		}
	}
	if len(estados) > 0 {
		return estados[0]
	}
	return ""
}

func telefonosDe(telefono, movil string) []formato.Telefono {
	return append(formato.Telefonos("telefono", telefono), formato.Telefonos("movil", movil)...)
}

func aClienteResumen(f argosbd.IntegracionArgosCliente) ClienteResumen {
	empresas := empresasDeFila(f)
	if empresas == nil {
		empresas = []int64{}
	}
	sinc := f.SincronizadoEn
	return ClienteResumen{
		ID: f.ID, EmpresaID: f.EmpresaID, Empresas: empresas, Codigo: f.Codigo, Nombre: f.Nombre, Telefono: f.Telefono,
		Direccion: f.Direccion, Lat: f.Lat, Lon: f.Lon, ZonaID: f.ZonaID, Estado: f.Estado,
		Tipo: f.Tipo, TipoDocumentoFiscal: f.TipoDocumentoFiscal, NitCui: f.NitCui,
		NitCuiMostrar: formato.Documento(f.TipoDocumentoFiscal, f.NitCui), Movil: f.Movil, Correo: f.Correo,
		Telefonos: telefonosDe(f.Telefono, f.Movil), ZonaNombre: f.ZonaNombre, SincronizadoEn: &sinc,
	}
}

func clienteArgosAResumen(c ClienteArgos) ClienteResumen {
	lat, lon := c.LatLon()
	zona := ""
	if c.Zona != nil {
		zona = c.Zona.Nombre
	}
	return ClienteResumen{
		ID: c.ID, EmpresaID: c.EmpresaID, Empresas: EmpresasDelCliente(c, nil), Codigo: c.Codigo, Nombre: c.Nombre, Telefono: c.Telefono,
		Direccion: c.Direccion, Lat: lat, Lon: lon, ZonaID: c.IDZona(), Estado: c.Estado,
		Tipo: c.Tipo, TipoDocumentoFiscal: c.TipoDocumentoFiscal, NitCui: c.NitCui,
		NitCuiMostrar: formato.Documento(c.TipoDocumentoFiscal, c.NitCui), Movil: c.Movil, Correo: c.CorreoCliente(),
		Telefonos: telefonosDe(c.Telefono, c.Movil), ZonaNombre: zona,
	}
}

func aServicio(f argosbd.IntegracionArgosServicio) Servicio {
	return Servicio{
		ID: f.ID, EmpresaID: f.EmpresaID, ClienteID: f.ClienteID, Codigo: f.Codigo, PlanID: f.PlanID,
		PlanNombre: f.PlanNombre, RouterID: f.RouterID, RouterNombre: f.RouterNombre, ZonaID: f.ZonaID,
		ZonaNombre: f.ZonaNombre, IP: f.Ip, Direccion: f.Direccion, Lat: f.Lat, Lon: f.Lon, Estado: f.Estado,
		UserPPP: f.UserPpp, TipoConexion: f.TipoConexion, SincronizadoEn: f.SincronizadoEn,
	}
}

func aServicios(filas []argosbd.IntegracionArgosServicio) []Servicio {
	salida := make([]Servicio, 0, len(filas))
	for _, f := range filas {
		salida = append(salida, aServicio(f))
	}
	return salida
}

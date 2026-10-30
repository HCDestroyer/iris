package argos

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	argosbd "github.com/HCDestroyer/iris/apps/api/internal/integracion/argos/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos/formato"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
)

// Copias de los datos de Argos en tickets y órdenes (datos coherentes, pedido 5 del PM).
//
// Al crear un ticket o una orden con cliente de Argos, el caso de uso que lo crea llama a
// CopiarDatos en su misma transacción: se guarda lo operativo que la instantánea tiene del cliente
// y del servicio (CopiaArgos). La copia no se actualiza: es lo que se sabía al crear; si Argos
// elimina al cliente, se anonimiza. El detalle del ticket y de la orden la devuelve (CopiaDe) para
// que el panel muestre la dirección y el teléfono con los que se creó, aunque la ficha no se pueda
// leer o el servicio se haya eliminado en Argos.
//
// Multiempresa: la copia es de la empresa de la entidad. No se copia un cliente que la instantánea
// pone en otra empresa, y al leer se descarta si la empresa ya no coincide.

// Entidades con copia.
const (
	CopiaTicket = "ticket"
	CopiaOrden  = "orden"
)

// errNoDisponible es la única respuesta cuando el cliente o el servicio no sirven para la empresa:
// no existe en Argos, es de otra empresa o el servicio no es de ese cliente o de esa empresa. Es la
// misma en todos los casos, para no revelar qué clientes o servicios existen en otras empresas.
func errNoDisponible() error {
	return nucleo.ReglaNegocio("empresa_no_coincide", "El cliente o el servicio no existe en Argos o no es de tu empresa")
}

// EsOtraEmpresa indica si err es el 422 empresa_no_coincide de ValidarClienteEnEmpresa.
func EsOtraEmpresa(err error) bool {
	var p *nucleo.Problema
	return errors.As(err, &p) && p.Codigo() == "empresa_no_coincide"
}

// ValidarClienteEnEmpresa comprueba, contra la instantánea (si Iris la tiene), primero que el
// cliente sea de la empresa (alguno de sus servicios lo es, o no tiene servicios: como en Argos) y
// después que el servicio sea de ese cliente y de esa empresa. Sin instantánea no se puede validar
// y se acepta (AsegurarInstantanea ya la trajo si hacía falta); entonces tampoco hay copia.
// empresa nil o 0 (sin conector): solo se valida que el servicio sea del cliente. Cualquier
// incumplimiento da el mismo 422 empresa_no_coincide (errNoDisponible).
func ValidarClienteEnEmpresa(ctx context.Context, db argosbd.DBTX, clienteID int64, servicioID, empresa *int64) error {
	q := argosbd.New(db)
	e := valor(empresa)
	c, err := q.ClientePorID(ctx, clienteID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return err
	case e != 0 && !enEmpresa(empresasDeFila(c), c.SinServicios, e):
		return errNoDisponible()
	}
	if servicioID == nil {
		return nil
	}
	s, err := q.ServicioPorID(ctx, *servicioID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil
	case err != nil:
		return err
	case s.ClienteID != clienteID, e != 0 && s.EmpresaID != e:
		return errNoDisponible()
	}
	return nil
}

func valor(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}

// DatosParaCopia arma la copia desde la instantánea (sin guardarla). nil si Iris no tiene la
// instantánea del cliente o si el cliente no es de la empresa (≠ 0). El servicio cuenta solo si
// es de ese cliente y de esa empresa; el resumen del estado usa solo los servicios de la empresa.
func DatosParaCopia(ctx context.Context, db argosbd.DBTX, clienteID int64, servicioID, empresa *int64) (*CopiaArgos, error) {
	q := argosbd.New(db)
	c, err := q.ClientePorID(ctx, clienteID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	e := valor(empresa)
	if e != 0 && !enEmpresa(empresasDeFila(c), c.SinServicios, e) {
		return nil, nil
	}
	filas, err := q.ServiciosDeCliente(ctx, clienteID)
	if err != nil {
		return nil, err
	}
	copia := CopiaArgos{
		Cliente: CopiaCliente{
			ID: c.ID, Codigo: c.Codigo, Nombre: c.Nombre, Direccion: c.Direccion, Lat: c.Lat, Lon: c.Lon,
			ZonaNombre: c.ZonaNombre, Estado: c.Estado,
		},
		Completa: c.VersionDatos >= VersionDatos, SincronizadoEn: c.SincronizadoEn,
	}
	estados := []string{}
	for _, s := range filas {
		if e != 0 && s.EmpresaID != e {
			continue
		}
		estados = append(estados, s.Estado)
		if servicioID != nil && s.ID == *servicioID {
			copia.Servicio = &CopiaServicio{
				ID: s.ID, PlanNombre: s.PlanNombre, Estado: s.Estado, Direccion: s.Direccion, Lat: s.Lat, Lon: s.Lon,
				IP: s.Ip, RouterNombre: s.RouterNombre, ZonaNombre: s.ZonaNombre,
			}
		}
	}
	if copia.Cliente.Estado == "" {
		copia.Cliente.Estado = ResumenEstado(estados)
	}
	copia.Direccion = c.Direccion
	if copia.Servicio != nil && copia.Servicio.Direccion != "" {
		copia.Direccion = copia.Servicio.Direccion
	}
	if p := formato.Principal(telefonosDe(c.Telefono, c.Movil)); p != nil {
		tel := *p
		copia.Telefono = &tel
	}
	return &copia, nil
}

// CopiarDatos guarda la copia del ticket u orden recién creado (en la transacción del caso de uso
// que lo crea), con la empresa de la entidad. nil si no hay nada que copiar (sin instantánea del
// cliente, o el cliente es de otra empresa). Si ya había una copia para la entidad (un
// reintento), no se cambia.
func CopiarDatos(ctx context.Context, db argosbd.DBTX, entidad string, entidadID, clienteID int64, servicioID, empresa *int64, ahora time.Time) (*CopiaArgos, error) {
	copia, err := DatosParaCopia(ctx, db, clienteID, servicioID, empresa)
	if err != nil || copia == nil {
		return nil, err
	}
	copia.CopiadoEn = ahora.UTC()
	datos, err := json.Marshal(copia)
	if err != nil {
		return nil, err
	}
	var srv *int64
	if copia.Servicio != nil {
		srv = &copia.Servicio.ID
	}
	err = argosbd.New(db).GuardarCopia(ctx, argosbd.GuardarCopiaParams{
		Entidad: entidad, EntidadID: entidadID, EmpresaID: empresa, ClienteID: clienteID, ServicioID: srv, Datos: datos,
		SincronizadoEn: copia.SincronizadoEn,
	})
	if err != nil {
		return nil, err
	}
	return CopiaDe(ctx, db, entidad, entidadID, empresa)
}

// CopiaDe lee la copia de un ticket u orden de la empresa dada (la de la entidad); nil si no
// tiene (se creó sin cliente de Argos, sin instantánea o antes de las copias), si se guardó con
// otra empresa o si la instantánea actual pone al cliente en otra empresa.
func CopiaDe(ctx context.Context, db argosbd.DBTX, entidad string, entidadID int64, empresa *int64) (*CopiaArgos, error) {
	q := argosbd.New(db)
	f, err := q.CopiaDe(ctx, argosbd.CopiaDeParams{Entidad: entidad, EntidadID: entidadID})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	e := valor(empresa)
	if valor(f.EmpresaID) != e {
		return nil, nil
	}
	if e != 0 {
		c, err := q.ClientePorID(ctx, f.ClienteID)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
		case err != nil:
			return nil, err
		case !enEmpresa(empresasDeFila(c), c.SinServicios, e):
			return nil, nil
		}
	}
	var copia CopiaArgos
	if err := json.Unmarshal(f.Datos, &copia); err != nil {
		return nil, err
	}
	copia.CopiadoEn, copia.SincronizadoEn = f.CopiadoEn, f.SincronizadoEn
	copia.Anonimizada = f.AnonimizadaEn != nil
	return &copia, nil
}

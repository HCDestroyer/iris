package argos

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	argosbd "github.com/HCDestroyer/iris/apps/api/internal/integracion/argos/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos/formato"
)

// Copias de los datos de Argos en tickets y órdenes (datos coherentes, pedido 5 del PM).
//
// Al crear un ticket o una orden con cliente de Argos, el caso de uso que lo crea llama a
// CopiarDatos en su misma transacción: se guarda lo que la instantánea tiene del cliente y del
// servicio, en la forma de la ficha (ClienteResumen y Servicio). La copia no se actualiza: es lo
// que se sabía al crear. El detalle del ticket y de la orden la devuelve (CopiaDe) para que el
// panel muestre siempre la dirección y el teléfono con los que se creó, aunque la ficha no se pueda
// leer o el servicio se haya eliminado en Argos.

// Entidades con copia.
const (
	CopiaTicket = "ticket"
	CopiaOrden  = "orden"
)

// DatosParaCopia arma la copia desde la instantánea (sin guardarla). nil si Iris no tiene la
// instantánea del cliente. El servicio cuenta solo si es de ese cliente; con empresa (≠ nil), el
// resumen del estado del cliente usa solo los servicios de esa empresa (como la ficha).
func DatosParaCopia(ctx context.Context, db argosbd.DBTX, clienteID int64, servicioID, empresa *int64) (*CopiaArgos, error) {
	q := argosbd.New(db)
	c, err := q.ClientePorID(ctx, clienteID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	filas, err := q.ServiciosDeCliente(ctx, clienteID)
	if err != nil {
		return nil, err
	}
	copia := CopiaArgos{Cliente: aClienteResumen(c), SincronizadoEn: c.SincronizadoEn}
	estados := []string{}
	for _, s := range filas {
		if empresa != nil && *empresa != 0 && s.EmpresaID != *empresa {
			continue
		}
		estados = append(estados, s.Estado)
		if servicioID != nil && s.ID == *servicioID {
			srv := aServicio(s)
			copia.Servicio = &srv
		}
	}
	if copia.Cliente.Estado == "" {
		copia.Cliente.Estado = ResumenEstado(estados)
	}
	copia.Direccion = c.Direccion
	if copia.Servicio != nil && copia.Servicio.Direccion != "" {
		copia.Direccion = copia.Servicio.Direccion
	}
	if p := formato.Principal(copia.Cliente.Telefonos); p != nil {
		tel := *p
		copia.Telefono = &tel
	}
	return &copia, nil
}

// CopiarDatos guarda la copia del ticket u orden recién creado (en la transacción del caso de uso
// que lo crea). Devuelve la copia, o nil si Iris no tiene la instantánea del cliente (sin conector,
// o un cliente que nunca se abrió): entonces no hay nada que copiar. Si ya había una copia para la
// entidad (un reintento), no se cambia.
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
		Entidad: entidad, EntidadID: entidadID, ClienteID: clienteID, ServicioID: srv, Datos: datos, SincronizadoEn: copia.SincronizadoEn,
	})
	if err != nil {
		return nil, err
	}
	return CopiaDe(ctx, db, entidad, entidadID)
}

// CopiaDe lee la copia de un ticket u orden; nil si no tiene (se creó sin cliente de Argos, sin
// instantánea o antes de que existieran las copias).
func CopiaDe(ctx context.Context, db argosbd.DBTX, entidad string, entidadID int64) (*CopiaArgos, error) {
	f, err := argosbd.New(db).CopiaDe(ctx, argosbd.CopiaDeParams{Entidad: entidad, EntidadID: entidadID})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var copia CopiaArgos
	if err := json.Unmarshal(f.Datos, &copia); err != nil {
		return nil, err
	}
	copia.CopiadoEn, copia.SincronizadoEn = f.CopiadoEn, f.SincronizadoEn
	if copia.Cliente.Telefonos == nil {
		copia.Cliente.Telefonos = []formato.Telefono{}
	}
	if copia.Cliente.Empresas == nil {
		copia.Cliente.Empresas = []int64{}
	}
	return &copia, nil
}

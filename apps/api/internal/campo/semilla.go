package campo

import (
	"context"
	"fmt"
	"time"
)

// OpcionesSemilla de la semilla de desarrollo.
type OpcionesSemilla struct {
	// TecnicoID: usuario con el rol tecnico (y la empresa, si hay empresas registradas).
	TecnicoID int64
	// EmpresaID: empresa de las órdenes (obligatoria si hay empresas registradas).
	EmpresaID *int64
	// Ordenes: cuántas órdenes del día (3 por defecto: instalación, visita y retiro).
	Ordenes int
}

// SemillaDesarrollo crea órdenes del día programadas para un técnico, para probar la app de
// campo sin Argos (iris campo semilla). Corre por los casos de uso con el actor del contexto
// (consola: sistema), así que todo queda auditado y con sus eventos. La ruta del día no existe
// todavía (agenda, fase 3): el paquete la manda ausente.
func (m *Modulo) SemillaDesarrollo(ctx context.Context, op OpcionesSemilla) ([]Orden, error) {
	if op.Ordenes <= 0 {
		op.Ordenes = 3
	}
	tipos := []string{"instalacion", "visita", "retiro", "cambio_equipo", "mantenimiento"}
	direcciones := []string{"Zona 10, 5a avenida 12-34, Ciudad de Guatemala", "Mixco, colonia El Naranjo, 3a calle 8-10",
		"Villa Nueva, residenciales Catalina, casa 22", "Zona 7, Kaminal Juyú 2, 14 avenida 3-50", "Antigua Guatemala, calle del Arco 9"}
	loc := m.cfg.ZonaHoraria
	hoy := m.ahora().In(loc)
	salida := []Orden{}
	for i := range op.Ordenes {
		o, err := m.CrearOrden.Ejecutar(ctx, EntradaCrearOrden{
			Tipo: tipos[i%len(tipos)], EmpresaID: op.EmpresaID, Direccion: ptr(direcciones[i%len(direcciones)]),
			Lat: ptr(14.60 + float64(i)/100), Lon: ptr(-90.52 - float64(i)/100),
			Contacto:    &EntradaContactoOrden{Nombre: ptr(fmt.Sprintf("Cliente de prueba %d", i+1)), Telefono: ptr("5555-0000")},
			Descripcion: "Orden de desarrollo (iris campo semilla)",
		})
		if err != nil {
			return salida, err
		}
		inicio := time.Date(hoy.Year(), hoy.Month(), hoy.Day(), 8+2*i%10, 0, 0, 0, loc)
		o, err = m.ProgramarOrden.Ejecutar(ctx, EntradaProgramarOrden{ID: o.ID, Version: o.Version, TecnicoID: &op.TecnicoID, Inicio: inicio})
		if err != nil {
			return salida, err
		}
		salida = append(salida, o)
	}
	return salida, nil
}

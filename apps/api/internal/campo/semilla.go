package campo

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/agenda"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
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
// (consola: sistema), así que todo queda auditado y con sus eventos. Si el técnico no tiene
// ficha de agenda, se la da con turnos de lunes a domingo de 07:00 a 19:00; cada orden toma el
// primer hueco libre del día desde las 08:00.
func (m *Modulo) SemillaDesarrollo(ctx context.Context, op OpcionesSemilla) ([]Orden, error) {
	if op.Ordenes <= 0 {
		op.Ordenes = 3
	}
	if err := m.fichaDeSemilla(ctx, op.TecnicoID); err != nil {
		return nil, err
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
		var programada Orden
		for mitad := 16; mitad < 36; mitad++ { // 08:00 … 17:30, cada media hora
			inicio := time.Date(hoy.Year(), hoy.Month(), hoy.Day(), mitad/2, 30*(mitad%2), 0, 0, loc)
			programada, err = m.ProgramarOrden.Ejecutar(ctx, EntradaProgramarOrden{ID: o.ID, Version: o.Version, TecnicoID: &op.TecnicoID, Inicio: inicio})
			var p *nucleo.Problema
			if err == nil || !errors.As(err, &p) || (p.Codigo() != "solape" && p.Codigo() != "fuera_de_turno") {
				break
			}
		}
		if err != nil {
			return salida, err
		}
		salida = append(salida, programada)
	}
	return salida, nil
}

// fichaDeSemilla da ficha de agenda (con turnos) al técnico si no la tiene.
func (m *Modulo) fichaDeSemilla(ctx context.Context, usuario int64) error {
	ag := m.cfg.Agenda
	pag, err := ag.ListarTecnicos.Ejecutar(ctx, agenda.EntradaListarTecnicos{UsuarioID: usuario})
	if err != nil {
		return err
	}
	if len(pag.Data) > 0 {
		return nil
	}
	f, err := ag.CrearTecnico.Ejecutar(ctx, agenda.EntradaCrearTecnico{UsuarioID: usuario, Habilidades: []string{"fibra", "inalambrico"}})
	if err != nil {
		return err
	}
	turnos := make([]agenda.Turno, 0, 7)
	for dia := int16(1); dia <= 7; dia++ {
		turnos = append(turnos, agenda.Turno{Dia: dia, Inicio: "07:00", Fin: "19:00"})
	}
	_, err = ag.FijarTurnos.Ejecutar(ctx, agenda.EntradaFijarTurnos{ID: f.ID, Version: f.Version, Turnos: turnos})
	return err
}

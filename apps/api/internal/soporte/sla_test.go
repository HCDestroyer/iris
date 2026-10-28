package soporte_test

import (
	"testing"
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/soporte"
)

var gt = func() *time.Location {
	l, err := time.LoadLocation("America/Guatemala")
	if err != nil {
		panic(err)
	}
	return l
}()

// hora en Guatemala.
func hora(anio int, mes time.Month, dia, h, m int) time.Time {
	return time.Date(anio, mes, dia, h, m, 0, 0, gt)
}

func desde(h int) *time.Duration {
	d := time.Duration(h) * time.Hour
	return &d
}

// calendarioGT es el horario de la semilla: L-V 08–17, sábado 08–12, con algunos feriados.
func calendarioGT(t *testing.T) *soporte.Calendario {
	t.Helper()
	var tramos []soporte.Tramo
	for d := 1; d <= 5; d++ {
		tramos = append(tramos, soporte.Tramo{Dia: d, Inicio: 8 * time.Hour, Fin: 17 * time.Hour})
	}
	tramos = append(tramos, soporte.Tramo{Dia: 6, Inicio: 8 * time.Hour, Fin: 12 * time.Hour})
	c, err := soporte.NuevoCalendario("America/Guatemala", tramos, []soporte.Feriado{
		{Anio: 2026, Mes: time.April, Dia: 2}, {Anio: 2026, Mes: time.April, Dia: 3}, {Anio: 2026, Mes: time.April, Dia: 4},
		{Anio: 2026, Mes: time.December, Dia: 24, Desde: desde(12)},
		{Anio: 2026, Mes: time.December, Dia: 25},
	})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestCalendarioSumar(t *testing.T) {
	c := calendarioGT(t)
	casos := []struct {
		nombre string
		desde  time.Time
		min    int
		espera time.Time
	}{
		{"dentro del horario", hora(2026, 10, 28, 10, 0), 240, hora(2026, 10, 28, 14, 0)},
		{"pasa al día siguiente", hora(2026, 10, 28, 16, 0), 120, hora(2026, 10, 29, 9, 0)},
		{"termina justo al cierre", hora(2026, 10, 28, 16, 0), 60, hora(2026, 10, 28, 17, 0)},
		{"viernes tarde → sábado", hora(2026, 10, 30, 16, 0), 240, hora(2026, 10, 31, 11, 0)},
		{"sábado → lunes (domingo no cuenta)", hora(2026, 10, 31, 11, 0), 120, hora(2026, 11, 2, 9, 0)},
		{"fuera de horario arranca al abrir", hora(2026, 11, 8, 20, 0), 30, hora(2026, 11, 9, 8, 30)},
		{"antes de abrir", hora(2026, 11, 9, 6, 0), 60, hora(2026, 11, 9, 9, 0)},
		{"Semana Santa (jueves a sábado feriados)", hora(2026, 4, 1, 16, 0), 120, hora(2026, 4, 6, 9, 0)},
		{"24 de diciembre medio día y Navidad", hora(2026, 12, 24, 10, 0), 180, hora(2026, 12, 26, 9, 0)},
	}
	for _, c2 := range casos {
		t.Run(c2.nombre, func(t *testing.T) {
			got := c.Sumar(c2.desde, time.Duration(c2.min)*time.Minute)
			if !got.Equal(c2.espera) || got.Location() != time.UTC {
				t.Fatalf("Sumar(%s, %d min) = %s, esperaba %s (UTC)", c2.desde, c2.min, got.In(gt), c2.espera)
			}
		})
	}
}

func TestCalendarioEntre(t *testing.T) {
	c := calendarioGT(t)
	if d := c.Entre(hora(2026, 10, 28, 16, 0), hora(2026, 10, 29, 9, 0)); d != 2*time.Hour {
		t.Fatalf("miércoles 16:00 → jueves 09:00: %s", d)
	}
	// Viernes 16:00 → lunes 10:00: 1 h + sábado 4 h + lunes 2 h.
	if d := c.Entre(hora(2026, 10, 30, 16, 0), hora(2026, 11, 2, 10, 0)); d != 7*time.Hour {
		t.Fatalf("fin de semana: %s", d)
	}
	if d := c.Entre(hora(2026, 12, 24, 11, 0), hora(2026, 12, 25, 16, 0)); d != time.Hour {
		t.Fatalf("feriados: %s", d)
	}
	if d := c.Entre(hora(2026, 10, 29, 9, 0), hora(2026, 10, 28, 9, 0)); d != 0 {
		t.Fatalf("intervalo invertido: %s", d)
	}
}

func TestCalendarioContinuo(t *testing.T) {
	var c *soporte.Calendario // 24/7
	t0 := hora(2026, 12, 25, 23, 0)
	if got := c.Sumar(t0, 90*time.Minute); !got.Equal(t0.Add(90 * time.Minute)) {
		t.Fatalf("24/7: %s", got)
	}
	if d := c.Entre(t0, t0.Add(5*time.Hour)); d != 5*time.Hour {
		t.Fatalf("24/7 entre: %s", d)
	}
}

func TestCalendarioInvalido(t *testing.T) {
	if _, err := soporte.NuevoCalendario("America/Guatemala", nil, nil); err == nil {
		t.Fatal("un calendario sin tramos no es válido")
	}
	if _, err := soporte.NuevoCalendario("Marte/Olympus", []soporte.Tramo{{Dia: 1, Inicio: 0, Fin: time.Hour}}, nil); err == nil {
		t.Fatal("zona inexistente")
	}
	if _, err := soporte.NuevoCalendario("UTC", []soporte.Tramo{{Dia: 1, Inicio: 2 * time.Hour, Fin: time.Hour}}, nil); err == nil {
		t.Fatal("tramo invertido")
	}
}

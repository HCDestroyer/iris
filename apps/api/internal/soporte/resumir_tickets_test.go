package soporte_test

import (
	"context"
	"testing"
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
	"github.com/HCDestroyer/iris/apps/api/internal/soporte"
)

// fijar escribe a mano los campos de SLA y fechas de un ticket de prueba (created_at lo pone la
// base con now(); el resumen se calcula contra el reloj del módulo).
func fijar(t *testing.T, id int64, sql string, args ...any) {
	t.Helper()
	if _, err := pruebas.Pool(t).Exec(context.Background(), "UPDATE soporte.tickets SET "+sql+" WHERE id = $1", append([]any{id}, args...)...); err != nil {
		t.Fatal(err)
	}
}

func TestResumenTickets(t *testing.T) {
	// Jueves 5 de noviembre de 2026, 12:00 en Guatemala (18:00 UTC).
	t0 := time.Date(2026, 11, 5, 18, 0, 0, 0, time.UTC)
	m, _ := nuevoModulo(t0)
	yo := agente(t)
	empresa := 950000 + time.Now().UnixNano()%10000
	nuevo := func(asunto string) int64 {
		id := crear(t, m, como(t, yo), soporte.EntradaCrearTicket{Asunto: asunto}).ID
		fijar(t, id, "empresa_id = $2", empresa)
		return id
	}

	// A: mío, vencido hace 1 h, creado anteayer.
	a := nuevo("Vencido")
	fijar(t, a, "asignado_a = $2, estado = 'abierto', created_at = $3, riesgo_resolucion = $4, vence_resolucion = $5",
		yo, t0.Add(-48*time.Hour), t0.Add(-3*time.Hour), t0.Add(-time.Hour))
	// B: sin asignar, en riesgo y vence hoy (14:00 en Guatemala), zona 3, creado hoy.
	b := nuevo("En riesgo")
	fijar(t, b, "zona_id = 3, created_at = $2, riesgo_resolucion = $3, vence_resolucion = $4",
		t0.Add(-time.Hour), t0.Add(-10*time.Minute), t0.Add(2*time.Hour))
	// C: en espera con el plazo pasado: está en pausa, no cuenta como vencido ni urgente.
	c := nuevo("En espera")
	fijar(t, c, "estado = 'en_espera', en_espera_desde = $2, created_at = $2, riesgo_resolucion = $3, vence_resolucion = $4",
		t0.Add(-30*time.Minute), t0.Add(-2*time.Hour), t0.Add(-time.Hour))
	// D: resuelto ayer dentro del SLA (no está vivo).
	d := nuevo("Resuelto ayer")
	fijar(t, d, "estado = 'resuelto', created_at = $2, resuelto_en = $3, vence_resolucion = $4",
		t0.Add(-72*time.Hour), t0.Add(-24*time.Hour), t0.Add(-23*time.Hour))
	// E: resuelto hace 10 días: fuera de la serie.
	e := nuevo("Viejo")
	fijar(t, e, "estado = 'resuelto', created_at = $2, resuelto_en = $2, vence_resolucion = $3",
		t0.Add(-240*time.Hour), t0.Add(-200*time.Hour))
	// Otro ticket sin empresa (de otra prueba o del modo sin conector): no se cuenta.
	crear(t, m, como(t, yo), soporte.EntradaCrearTicket{Asunto: "Sin empresa"})

	r, err := m.ResumirTickets.Ejecutar(comoEnEmpresa(t, yo, empresa), soporte.EntradaResumirTickets{})
	if err != nil {
		t.Fatal(err)
	}
	if r.Activos != 3 || r.Mios != 1 || r.SinAsignar != 2 || r.EnEspera != 1 || r.SLAEnRiesgo != 1 || r.SLAVencidos != 1 || r.SLAVenceHoy != 1 {
		t.Fatalf("contadores: %+v", r)
	}
	if len(r.Urgentes) != 2 || r.Urgentes[0].ID != a || r.Urgentes[1].ID != b || r.Urgentes[0].SLAResolucion.Estado != "vencido" {
		t.Fatalf("urgentes (el que vence antes primero, sin el en espera): %+v", r.Urgentes)
	}
	if len(r.Dias) != soporte.DiasResumen || r.Dias[0].Dia != "2026-10-30" || r.Dias[6].Dia != "2026-11-05" {
		t.Fatalf("días: %+v", r.Dias)
	}
	hoy, ayer, anteayer := r.Dias[6], r.Dias[5], r.Dias[4]
	if hoy.Creados != 2 || hoy.Vencidos != 1 || hoy.Resueltos != 0 {
		t.Fatalf("hoy: %+v", hoy)
	}
	if ayer.Resueltos != 1 || ayer.ResueltosConSLA != 1 || ayer.ResueltosEnSLA != 1 || ayer.Creados != 0 {
		t.Fatalf("ayer: %+v", ayer)
	}
	if anteayer.Creados != 1 {
		t.Fatalf("anteayer: %+v", anteayer)
	}
	if !r.CalculadoEn.Equal(t0) {
		t.Fatalf("calculadoEn: %v", r.CalculadoEn)
	}

	// Alcance por zona: otro agente con tickets.ver solo en la zona 3 cuenta solo B.
	otro := agente(t)
	limitado := comoEnEmpresa(t, otro, empresa)
	limitado = contexto.ConActor(limitado, contexto.ActorDe(limitado), contexto.NuevosPermisos(map[string][]contexto.Alcance{
		"tickets.ver": {{ZonaID: 3}},
	}))
	r, err = m.ResumirTickets.Ejecutar(limitado, soporte.EntradaResumirTickets{})
	if err != nil {
		t.Fatal(err)
	}
	if r.Activos != 1 || r.Mios != 0 || r.SinAsignar != 1 || r.SLAVencidos != 0 || len(r.Urgentes) != 1 || r.Urgentes[0].ID != b || r.Dias[4].Creados != 0 {
		t.Fatalf("alcance por zona: %+v", r)
	}

	// Sin tickets.ver → 403.
	_, err = m.ResumirTickets.Ejecutar(como(t, yo, nucleo.PermTicketsCrear), soporte.EntradaResumirTickets{})
	problema(t, err, 403, "prohibido")
}

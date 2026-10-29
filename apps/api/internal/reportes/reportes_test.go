package reportes_test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
	"github.com/HCDestroyer/iris/apps/api/internal/reportes"
)

func TestMain(m *testing.M) { os.Exit(pruebas.Correr(m)) }

// Las pruebas siembran filas directamente (los reportes solo leen) con horas exactas.

var ahora = time.Date(2026, 10, 29, 18, 0, 0, 0, time.UTC)

var guate = func() *time.Location {
	l, err := time.LoadLocation("America/Guatemala")
	if err != nil {
		panic(err)
	}
	return l
}()

// gt arma una hora de Guatemala.
func gt(dia, hora string) time.Time {
	t, err := time.ParseInLocation("2006-01-02 15:04", dia+" "+hora, guate)
	if err != nil {
		panic(err)
	}
	return t.UTC()
}

func modulo() *reportes.Modulo {
	return reportes.Nuevo(reportes.Config{Reloj: func() time.Time { return ahora }})
}

var ident = identidad.Nuevo(identidad.Config{})

func usuario(t *testing.T, nombre string) int64 {
	t.Helper()
	u, err := ident.CrearUsuario.Ejecutar(pruebas.ContextoSistema(pruebas.Pool(t)), identidad.EntradaCrearUsuario{
		Nombre: nombre, Correo: pruebas.Correo("reportes"), Contrasena: "contrasena-larga-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	return u.ID
}

// limpiar deja las tablas que leen los reportes vacías y el catálogo de zonas y cuadrillas.
func limpiar(t *testing.T) {
	t.Helper()
	if _, err := pruebas.Pool(t).Exec(context.Background(), `
		TRUNCATE campo.ordenes, soporte.tickets CASCADE;
		INSERT INTO agenda.zonas (id, origen, nombre) SELECT n, 'local', 'Zona ' || n FROM generate_series(1, 5) n
			ON CONFLICT (id) DO NOTHING;
		INSERT INTO agenda.cuadrillas (id, nombre) SELECT n, 'Cuadrilla ' || n FROM generate_series(1, 5) n
			ON CONFLICT (id) DO NOTHING`); err != nil {
		t.Fatal(err)
	}
}

func como(t *testing.T, permisos ...nucleo.Permiso) context.Context {
	if len(permisos) == 0 {
		permisos = []nucleo.Permiso{nucleo.PermReportesVer}
	}
	return pruebas.ContextoUsuario(pruebas.Pool(t), 1, permisos...)
}

func enEmpresa(t *testing.T, empresa int64) context.Context {
	return pruebas.ContextoUsuarioEnEmpresa(pruebas.Pool(t), 1, empresa, nucleo.PermReportesVer)
}

func conAlcance(t *testing.T, alcances ...contexto.Alcance) context.Context {
	ctx := como(t)
	return contexto.ConActor(ctx, contexto.ActorDe(ctx), contexto.NuevosPermisos(map[string][]contexto.Alcance{
		string(nucleo.PermReportesVer): alcances,
	}))
}

// tk es un ticket sembrado.
type tk struct {
	empresa, zona, cuadrilla, asignado, categoria, servicio, cliente *int64
	prioridad                                                        string
	creado                                                           time.Time
	respuesta, venceRespuesta, resuelto, venceResolucion             *time.Time
	fusionadoEn                                                      *int64
}

func ticket(t *testing.T, d tk) int64 {
	t.Helper()
	if d.prioridad == "" {
		d.prioridad = "media"
	}
	estado := "abierto"
	var cerrado *time.Time
	if d.resuelto != nil {
		estado = "resuelto"
	}
	if d.fusionadoEn != nil {
		estado, cerrado = "cerrado", d.resuelto
		if cerrado == nil {
			cerrado = &d.creado
		}
	}
	var id int64
	if err := pruebas.Pool(t).QueryRow(context.Background(), `
		INSERT INTO soporte.tickets (empresa_id, zona_id, cuadrilla_id, asignado_a, categoria_id, servicio_id, cliente_id,
			prioridad, estado, origen, asunto, sla_inicio, primera_respuesta_en, vence_respuesta, resuelto_en,
			vence_resolucion, fusionado_en_id, cerrado_en, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, 'panel', 'Prueba', $10, $11, $12, $13, $14, $15, $16, $10)
		RETURNING id`,
		d.empresa, d.zona, d.cuadrilla, d.asignado, d.categoria, d.servicio, d.cliente, d.prioridad, estado, d.creado,
		d.respuesta, d.venceRespuesta, d.resuelto, d.venceResolucion, d.fusionadoEn, cerrado).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

// od es una orden sembrada.
type od struct {
	empresa, zona, cuadrilla, tecnico, servicio *int64
	tipo, estado, resultado                     string
	creada, enSitio, finalizada                 *time.Time
	pausadaSeg                                  int64
}

func orden(t *testing.T, d od) int64 {
	t.Helper()
	if d.tipo == "" {
		d.tipo = "visita"
	}
	if d.creada == nil {
		c := ahora.AddDate(0, -1, 0)
		if d.finalizada != nil {
			c = d.finalizada.Add(-48 * time.Hour)
		}
		d.creada = &c
	}
	var motivoFalla *string
	if d.estado == "fallida" {
		m := "cliente ausente"
		motivoFalla, d.resultado = &m, "fallida"
	}
	var resultado *string
	if d.resultado != "" {
		resultado = &d.resultado
	}
	var id int64
	if err := pruebas.Pool(t).QueryRow(context.Background(), `
		INSERT INTO campo.ordenes (empresa_id, tipo_id, servicio_id, zona_id, cuadrilla_id, direccion, tecnico_id, estado,
			en_camino_en, en_sitio_en, finalizada_en, resultado, motivo_falla, pausada_seg, created_at)
		VALUES ($1, (SELECT id FROM campo.tipos_orden WHERE codigo = $2), $3, $4, $5, 'Calle 1', $6, $7, $8, $8, $9, $10, $11, $12, $13)
		RETURNING id`,
		d.empresa, d.tipo, d.servicio, d.zona, d.cuadrilla, d.tecnico, d.estado, d.enSitio, d.finalizada, resultado,
		motivoFalla, d.pausadaSeg, *d.creada).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func pausa(t *testing.T, ordenID int64, desde string, inicio, fin time.Time) {
	t.Helper()
	if _, err := pruebas.Pool(t).Exec(context.Background(), `
		INSERT INTO campo.orden_pausas (orden_id, motivo, desde_estado, inicio, fin, cierre)
		VALUES ($1, 'clima', $2, $3, $4, 'reanudada')`, ordenID, desde, inicio, fin); err != nil {
		t.Fatal(err)
	}
}

func categoria(t *testing.T, codigo string) *int64 {
	t.Helper()
	var id int64
	if err := pruebas.Pool(t).QueryRow(context.Background(),
		"SELECT id FROM soporte.categorias WHERE codigo = $1 AND empresa_id IS NULL", codigo).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return &id
}

func p[T any](v T) *T { return &v }

func en(t time.Time, d time.Duration) *time.Time { v := t.Add(d); return &v }

func problema(t *testing.T, err error, status int, codigo string) {
	t.Helper()
	var pr *nucleo.Problema
	if !errors.As(err, &pr) || pr.Status != status || pr.Codigo() != codigo {
		t.Fatalf("esperaba %d %s, llegó %v", status, codigo, err)
	}
}

func igual[T comparable](t *testing.T, que string, llego, esperaba T) {
	t.Helper()
	if llego != esperaba {
		t.Fatalf("%s: llegó %v, esperaba %v", que, llego, esperaba)
	}
}

func pct(t *testing.T, que string, llego *float64, esperaba float64) {
	t.Helper()
	if llego == nil || *llego != esperaba {
		t.Fatalf("%s: llegó %v, esperaba %v", que, llego, esperaba)
	}
}

func octubre() reportes.FiltrosPeriodo {
	return reportes.FiltrosPeriodo{Desde: "2026-10-01", Hasta: "2026-10-31"}
}

func TestPeriodoYRango(t *testing.T) {
	m := modulo()
	ctx := como(t)
	_, err := m.SLA.Ejecutar(ctx, reportes.FiltrosTickets{FiltrosPeriodo: reportes.FiltrosPeriodo{Desde: "2026-01-01", Hasta: "2027-01-02"}})
	problema(t, err, 422, "rango_excedido")
	// 366 días exactos (incluidos los dos extremos) sí.
	if _, err := m.SLA.Ejecutar(ctx, reportes.FiltrosTickets{FiltrosPeriodo: reportes.FiltrosPeriodo{Desde: "2026-01-01", Hasta: "2027-01-01"}}); err != nil {
		t.Fatal(err)
	}
	_, err = m.Tecnicos.Ejecutar(ctx, reportes.FiltrosOrdenes{FiltrosPeriodo: reportes.FiltrosPeriodo{Desde: "2026-10-31", Hasta: "2026-10-01"}})
	problema(t, err, 422, "validacion")
	_, err = m.Resolucion.Ejecutar(ctx, reportes.FiltrosTickets{FiltrosPeriodo: reportes.FiltrosPeriodo{Desde: "2026-13-01", Hasta: "2026-10-01"}})
	problema(t, err, 422, "validacion")
	_, err = m.PrimeraVisita.Ejecutar(ctx, reportes.EntradaPrimeraVisita{FiltrosPeriodo: octubre(), Dias: 5})
	problema(t, err, 422, "validacion")
	// Sin el permiso, 403.
	_, err = m.SLA.Ejecutar(como(t, nucleo.PermTicketsVer), reportes.FiltrosTickets{FiltrosPeriodo: octubre()})
	problema(t, err, 403, "prohibido")

	r, err := m.SLA.Ejecutar(ctx, reportes.FiltrosTickets{FiltrosPeriodo: octubre()})
	if err != nil {
		t.Fatal(err)
	}
	// Octubre en Guatemala (UTC-6): del 1 a las 06:00 UTC al 1 de noviembre a las 06:00 UTC.
	igual(t, "inicio", r.Periodo.Inicio, time.Date(2026, 10, 1, 6, 0, 0, 0, time.UTC))
	igual(t, "fin", r.Periodo.Fin, time.Date(2026, 11, 1, 6, 0, 0, 0, time.UTC))
}

func TestReporteSLAyResolucion(t *testing.T) {
	limpiar(t)
	m := modulo()
	sinServicio, lentitud := categoria(t, "sin_servicio"), categoria(t, "lentitud")
	base := gt("2026-10-10", "08:00")
	// A: alta, todo en tiempo (resuelto en 60 min).
	ticket(t, tk{prioridad: "alta", categoria: sinServicio, creado: base, respuesta: en(base, 10*time.Minute),
		venceRespuesta: en(base, 30*time.Minute), resuelto: en(base, time.Hour), venceResolucion: en(base, 4*time.Hour)})
	// B: alta, respuesta tarde, resolución en tiempo (120 min).
	ticket(t, tk{prioridad: "alta", categoria: lentitud, creado: base, respuesta: en(base, time.Hour),
		venceRespuesta: en(base, 30*time.Minute), resuelto: en(base, 2*time.Hour), venceResolucion: en(base, 4*time.Hour)})
	// C: media, sin SLA (240 min), sin categoría.
	ticket(t, tk{prioridad: "media", creado: base, resuelto: en(base, 4*time.Hour)})
	// D: resolución vencida (respondido con la solución) (600 min).
	ticket(t, tk{prioridad: "media", categoria: lentitud, creado: base, respuesta: en(base, 10*time.Hour),
		venceRespuesta: en(base, 12*time.Hour), resuelto: en(base, 10*time.Hour), venceResolucion: en(base, 8*time.Hour)})
	// Fuera: fusionado, abierto, resuelto el 30 de septiembre a las 23:30 de Guatemala.
	destino := ticket(t, tk{creado: base})
	ticket(t, tk{creado: base, resuelto: en(base, time.Hour), fusionadoEn: &destino})
	ticket(t, tk{creado: gt("2026-09-30", "20:00"), resuelto: p(gt("2026-09-30", "23:30"))})
	// Dentro por la frontera: 1 de octubre a las 00:00 de Guatemala (E, 240 min, baja).
	ticket(t, tk{prioridad: "baja", creado: gt("2026-09-30", "20:00"), resuelto: p(gt("2026-10-01", "00:00"))})

	r, err := m.SLA.Ejecutar(como(t), reportes.FiltrosTickets{FiltrosPeriodo: octubre()})
	if err != nil {
		t.Fatal(err)
	}
	igual(t, "resueltos", r.Total.Resueltos, 5)
	igual(t, "con SLA de respuesta", r.Total.ConSLARespuesta, 3)
	igual(t, "respuesta en tiempo", r.Total.RespuestaEnTiempo, 2)
	pct(t, "% respuesta", r.Total.PorcentajeRespuesta, 66.67)
	igual(t, "con SLA de resolución", r.Total.ConSLAResolucion, 3)
	pct(t, "% resolución", r.Total.PorcentajeResolucion, 66.67)
	porPrioridad := map[string]reportes.FilaSLA{}
	for _, f := range r.PorPrioridad {
		porPrioridad[*f.Prioridad] = f
	}
	igual(t, "alta", porPrioridad["alta"].Resueltos, 2)
	pct(t, "alta % respuesta", porPrioridad["alta"].PorcentajeRespuesta, 50)
	pct(t, "alta % resolución", porPrioridad["alta"].PorcentajeResolucion, 100)
	if porPrioridad["baja"].PorcentajeRespuesta != nil {
		t.Fatal("sin SLA el porcentaje debe ser null, no 0 ni 100")
	}
	igual(t, "categorías", len(r.PorCategoria), 3) // sin_servicio, lentitud y sin categoría
	for _, f := range r.PorCategoria {
		if f.CategoriaID == nil {
			igual(t, "sin categoría", *f.Categoria, "Sin categoría")
			igual(t, "sin categoría resueltos", f.Resueltos, 2)
		}
	}

	// Filtros: categoría y prioridades.
	r, err = m.SLA.Ejecutar(como(t), reportes.FiltrosTickets{FiltrosPeriodo: octubre(), CategoriaID: *lentitud})
	if err != nil {
		t.Fatal(err)
	}
	igual(t, "lentitud", r.Total.Resueltos, 2)
	r, err = m.SLA.Ejecutar(como(t), reportes.FiltrosTickets{FiltrosPeriodo: octubre(), Prioridad: []string{"alta", "baja"}})
	if err != nil {
		t.Fatal(err)
	}
	igual(t, "alta y baja", r.Total.Resueltos, 3)

	res, err := m.Resolucion.Ejecutar(como(t), reportes.FiltrosTickets{FiltrosPeriodo: octubre()})
	if err != nil {
		t.Fatal(err)
	}
	igual(t, "resueltos", res.Total.Resueltos, 5)
	// 60, 120, 240, 600, 240 → promedio 252, mediana 240.
	pct(t, "promedio", res.Total.PromedioMin, 252)
	pct(t, "mediana", res.Total.MedianaMin, 240)

	// Sin datos: ceros y null, nunca un porcentaje inventado.
	vacio, err := m.Resolucion.Ejecutar(como(t), reportes.FiltrosTickets{FiltrosPeriodo: reportes.FiltrosPeriodo{Desde: "2025-01-01", Hasta: "2025-01-31"}})
	if err != nil {
		t.Fatal(err)
	}
	if vacio.Total.Resueltos != 0 || vacio.Total.PromedioMin != nil || len(vacio.PorPrioridad) != 0 {
		t.Fatalf("vacío: %+v", vacio)
	}
}

func TestAlcanceYEmpresa(t *testing.T) {
	limpiar(t)
	m := modulo()
	base := gt("2026-10-10", "08:00")
	for _, d := range []tk{
		{zona: p(int64(1)), creado: base, resuelto: en(base, time.Hour)},
		{zona: p(int64(2)), creado: base, resuelto: en(base, time.Hour)},
		{cuadrilla: p(int64(3)), creado: base, resuelto: en(base, time.Hour)},
		{creado: base, resuelto: en(base, time.Hour)}, // sin zona ni cuadrilla
	} {
		ticket(t, d)
	}
	cuenta := func(ctx context.Context, f reportes.FiltrosTickets) int64 {
		t.Helper()
		f.Desde, f.Hasta = "2026-10-01", "2026-10-31"
		r, err := m.SLA.Ejecutar(ctx, f)
		if err != nil {
			t.Fatal(err)
		}
		return r.Total.Resueltos
	}
	igual(t, "global", cuenta(como(t), reportes.FiltrosTickets{}), 4)
	igual(t, "zona 1", cuenta(conAlcance(t, contexto.Alcance{ZonaID: 1}), reportes.FiltrosTickets{}), 1)
	igual(t, "zona 1 y cuadrilla 3", cuenta(conAlcance(t, contexto.Alcance{ZonaID: 1}, contexto.Alcance{CuadrillaID: 3}), reportes.FiltrosTickets{}), 2)
	// Pedir otra zona con alcance limitado no amplía nada.
	igual(t, "zona 2 fuera", cuenta(conAlcance(t, contexto.Alcance{ZonaID: 1}), reportes.FiltrosTickets{FiltrosPeriodo: reportes.FiltrosPeriodo{ZonaID: 2}}), 0)
	igual(t, "filtro zona 2", cuenta(como(t), reportes.FiltrosTickets{FiltrosPeriodo: reportes.FiltrosPeriodo{ZonaID: 2}}), 1)

	// Empresa: con empresa activa solo la suya; sin conector solo las de empresa null.
	ticket(t, tk{empresa: p(int64(7)), creado: base, resuelto: en(base, time.Hour)})
	ticket(t, tk{empresa: p(int64(7)), creado: base, resuelto: en(base, time.Hour)})
	ticket(t, tk{empresa: p(int64(8)), creado: base, resuelto: en(base, time.Hour)})
	igual(t, "empresa 7", cuenta(enEmpresa(t, 7), reportes.FiltrosTickets{}), 2)
	igual(t, "empresa 8", cuenta(enEmpresa(t, 8), reportes.FiltrosTickets{}), 1)
	igual(t, "sin conector", cuenta(como(t), reportes.FiltrosTickets{}), 4)
	igual(t, "sistema", cuenta(pruebas.ContextoSistema(pruebas.Pool(t)), reportes.FiltrosTickets{}), 7)

	// Órdenes: el mismo alcance.
	fin := gt("2026-10-12", "10:00")
	tec := usuario(t, "Técnico")
	orden(t, od{zona: p(int64(1)), tecnico: &tec, estado: "cerrada", resultado: "exitosa", finalizada: &fin})
	orden(t, od{zona: p(int64(2)), tecnico: &tec, estado: "cerrada", resultado: "exitosa", finalizada: &fin})
	orden(t, od{empresa: p(int64(7)), zona: p(int64(1)), tecnico: &tec, estado: "cerrada", resultado: "exitosa", finalizada: &fin})
	r, err := m.Tecnicos.Ejecutar(conAlcance(t, contexto.Alcance{ZonaID: 1}), reportes.FiltrosOrdenes{FiltrosPeriodo: octubre()})
	if err != nil {
		t.Fatal(err)
	}
	igual(t, "órdenes zona 1 sin conector", r.Total.Finalizadas, 1)
	r, err = m.Tecnicos.Ejecutar(enEmpresa(t, 7), reportes.FiltrosOrdenes{FiltrosPeriodo: octubre()})
	if err != nil {
		t.Fatal(err)
	}
	igual(t, "órdenes empresa 7", r.Total.Finalizadas, 1)
}

func TestReporteTecnicos(t *testing.T) {
	limpiar(t)
	m := modulo()
	ana, beto := usuario(t, "Ana"), usuario(t, "Beto")
	llegada := gt("2026-10-05", "09:00")
	// Ana: 90 min en sitio con 30 min de pausa en sitio → 60; 10 min de pausa en camino (no se
	// resta del sitio, sí cuenta como tiempo pausado).
	o1 := orden(t, od{tecnico: &ana, estado: "cerrada", resultado: "exitosa", enSitio: &llegada,
		finalizada: en(llegada, 90*time.Minute), pausadaSeg: 40 * 60})
	pausa(t, o1, "en_sitio", llegada.Add(20*time.Minute), llegada.Add(50*time.Minute))
	pausa(t, o1, "en_camino", llegada.Add(-30*time.Minute), llegada.Add(-20*time.Minute))
	// Ana: parcial, 30 min en sitio.
	orden(t, od{tecnico: &ana, estado: "cerrada", resultado: "parcial", enSitio: &llegada, finalizada: en(llegada, 30*time.Minute)})
	// Ana: fallida sin llegar (no cuenta en la duración en sitio).
	orden(t, od{tecnico: &ana, estado: "fallida", finalizada: en(llegada, time.Hour)})
	// Beto: exitosa, 45 min.
	orden(t, od{tecnico: &beto, estado: "cerrada", resultado: "exitosa", enSitio: &llegada, finalizada: en(llegada, 45*time.Minute)})
	// Fuera: cancelada, abierta y la de noviembre.
	orden(t, od{tecnico: &beto, estado: "cancelada", resultado: "cancelada", finalizada: en(llegada, time.Hour)})
	orden(t, od{tecnico: &beto, estado: "en_sitio", enSitio: &llegada})
	orden(t, od{tecnico: &beto, estado: "cerrada", resultado: "exitosa", finalizada: p(gt("2026-11-01", "00:30"))})

	r, err := m.Tecnicos.Ejecutar(como(t), reportes.FiltrosOrdenes{FiltrosPeriodo: octubre()})
	if err != nil {
		t.Fatal(err)
	}
	igual(t, "técnicos", len(r.Data), 2)
	a := r.Data[0]
	igual(t, "primero Ana", a.TecnicoID, ana)
	igual(t, "nombre", a.Nombre, "Ana")
	igual(t, "finalizadas", a.Finalizadas, 3)
	igual(t, "exitosas", a.Exitosas, 1)
	igual(t, "parciales", a.Parciales, 1)
	igual(t, "fallidas", a.Fallidas, 1)
	pct(t, "% éxito", a.PorcentajeExito, 33.33)
	igual(t, "con sitio", a.ConTiempoEnSitio, 2)
	pct(t, "sitio medio", a.DuracionMediaSitioMin, 45) // (60 + 30) / 2
	igual(t, "pausado", a.TiempoPausadoMin, 40)
	igual(t, "con pausa", a.ConPausa, 1)
	igual(t, "total", r.Total.Finalizadas, 4)
	pct(t, "sitio medio total", r.Total.DuracionMediaSitioMin, 45) // (60 + 30 + 45) / 3

	r, err = m.Tecnicos.Ejecutar(como(t), reportes.FiltrosOrdenes{FiltrosPeriodo: reportes.FiltrosPeriodo{Desde: "2026-10-01", Hasta: "2026-10-31", TecnicoID: beto}})
	if err != nil {
		t.Fatal(err)
	}
	igual(t, "solo Beto", len(r.Data), 1)
}

func TestReportePrimeraVisita(t *testing.T) {
	limpiar(t)
	m := modulo()
	ana := usuario(t, "Ana")
	cierre := gt("2026-09-10", "10:00")
	// 1: resuelta (nada en 30 días; otra orden del servicio a los 40 días no cuenta).
	orden(t, od{tecnico: &ana, servicio: p(int64(101)), tipo: "instalacion", estado: "cerrada", resultado: "exitosa", finalizada: &cierre})
	orden(t, od{servicio: p(int64(101)), estado: "pendiente", creada: en(cierre, 40*24*time.Hour)})
	// 2: retorno por otra orden a los 5 días.
	orden(t, od{tecnico: &ana, servicio: p(int64(102)), estado: "cerrada", resultado: "exitosa", finalizada: &cierre})
	orden(t, od{servicio: p(int64(102)), estado: "pendiente", creada: en(cierre, 5*24*time.Hour)})
	// 3: retorno por un ticket a los 10 días (un ticket fusionado no cuenta: el 4 queda resuelto).
	orden(t, od{tecnico: &ana, servicio: p(int64(103)), estado: "cerrada", resultado: "exitosa", finalizada: &cierre})
	ticket(t, tk{servicio: p(int64(103)), creado: *en(cierre, 10*24*time.Hour)})
	orden(t, od{tecnico: &ana, servicio: p(int64(104)), estado: "cerrada", resultado: "exitosa", finalizada: &cierre})
	destino := ticket(t, tk{creado: cierre})
	ticket(t, tk{servicio: p(int64(104)), creado: *en(cierre, 24*time.Hour), fusionadoEn: &destino})
	// 5: cancelada como retorno no cuenta → resuelta.
	orden(t, od{tecnico: &ana, servicio: p(int64(105)), estado: "cerrada", resultado: "exitosa", finalizada: &cierre})
	orden(t, od{servicio: p(int64(105)), estado: "cancelada", resultado: "cancelada", creada: en(cierre, 24*time.Hour), finalizada: en(cierre, 25*time.Hour)})
	// 6: sin servicio. 7: cerrada hace 5 días (en observación). 8: retiro (no cuenta). 9: parcial (no cuenta).
	orden(t, od{tecnico: &ana, estado: "cerrada", resultado: "exitosa", finalizada: &cierre})
	orden(t, od{tecnico: &ana, servicio: p(int64(107)), estado: "cerrada", resultado: "exitosa", finalizada: en(ahora, -5*24*time.Hour)})
	orden(t, od{tecnico: &ana, servicio: p(int64(108)), tipo: "retiro", estado: "cerrada", resultado: "exitosa", finalizada: &cierre})
	orden(t, od{tecnico: &ana, servicio: p(int64(109)), estado: "cerrada", resultado: "parcial", finalizada: &cierre})

	r, err := m.PrimeraVisita.Ejecutar(como(t), reportes.EntradaPrimeraVisita{FiltrosPeriodo: reportes.FiltrosPeriodo{Desde: "2026-09-01", Hasta: "2026-10-31"}})
	if err != nil {
		t.Fatal(err)
	}
	igual(t, "días", r.Dias, 30)
	igual(t, "cerradas", r.Total.Cerradas, 7)
	igual(t, "resueltas", r.Total.Resueltas, 3)
	igual(t, "con retorno", r.Total.ConRetorno, 2)
	igual(t, "evaluadas", r.Total.Evaluadas, 5)
	pct(t, "%", r.Total.Porcentaje, 60)
	igual(t, "en observación", r.Total.EnObservacion, 1)
	igual(t, "sin servicio", r.Total.SinServicio, 1)
	igual(t, "por tipo", len(r.PorTipo), 2)
	igual(t, "por técnico", len(r.PorTecnico), 1)
	igual(t, "técnico", *r.PorTecnico[0].TecnicoID, ana)

	// Con 45 días, la orden del servicio 101 a los 40 días es un retorno.
	r, err = m.PrimeraVisita.Ejecutar(como(t), reportes.EntradaPrimeraVisita{FiltrosPeriodo: reportes.FiltrosPeriodo{Desde: "2026-09-01", Hasta: "2026-09-30"}, Dias: 45})
	if err != nil {
		t.Fatal(err)
	}
	igual(t, "con retorno a 45 días", r.Total.ConRetorno, 3)
}

func TestReincidencias(t *testing.T) {
	limpiar(t)
	m := modulo()
	res := gt("2026-10-05", "10:00")
	// 1: servicio 201 vuelve a los 3 días (y otra vez a los 20): reincidente con 2 nuevos.
	a := ticket(t, tk{servicio: p(int64(201)), cliente: p(int64(1)), creado: res.Add(-time.Hour), resuelto: &res})
	ticket(t, tk{servicio: p(int64(201)), cliente: p(int64(1)), creado: *en(res, 72*time.Hour)})
	ticket(t, tk{servicio: p(int64(201)), cliente: p(int64(1)), creado: *en(res, 20*24*time.Hour)})
	// 2: otro servicio del mismo cliente: no es reincidencia del servicio 202.
	ticket(t, tk{servicio: p(int64(202)), cliente: p(int64(1)), creado: res.Add(-time.Hour), resuelto: en(res, time.Hour)})
	// 3: sin servicio, el cliente 2 vuelve a los 2 días.
	b := ticket(t, tk{cliente: p(int64(2)), creado: res.Add(-time.Hour), resuelto: en(res, 2*time.Hour)})
	ticket(t, tk{cliente: p(int64(2)), servicio: p(int64(299)), creado: *en(res, 50*time.Hour)})
	// 4: sin servicio ni cliente: no evaluable.
	ticket(t, tk{creado: res.Add(-time.Hour), resuelto: en(res, 3*time.Hour)})
	// 5: vuelve a los 40 días (fuera de 30).
	ticket(t, tk{servicio: p(int64(205)), creado: res.Add(-time.Hour), resuelto: en(res, 4*time.Hour)})
	ticket(t, tk{servicio: p(int64(205)), creado: *en(res, 40*24*time.Hour)})

	in := reportes.EntradaReincidencias{FiltrosTickets: reportes.FiltrosTickets{FiltrosPeriodo: octubre()}}
	in.Limit = 1
	r, err := m.Reincidencias.Ejecutar(como(t), in)
	if err != nil {
		t.Fatal(err)
	}
	// Resueltos en octubre: 1, 2, 3, 4, 5 y el 6 (vuelta del servicio 201 abierta, no resuelta) no.
	igual(t, "resueltos", r.Resueltos, 5)
	igual(t, "evaluables", r.Evaluables, 4)
	igual(t, "reincidentes", r.Reincidentes, 2)
	pct(t, "%", r.Porcentaje, 50)
	igual(t, "página 1", len(r.Data), 1)
	igual(t, "más reciente primero", r.Data[0].TicketID, b)
	igual(t, "horas", r.Data[0].HorasHastaVolver, 48)
	if r.NextCursor == nil {
		t.Fatal("falta el cursor")
	}
	in.Cursor = *r.NextCursor
	r, err = m.Reincidencias.Ejecutar(como(t), in)
	if err != nil {
		t.Fatal(err)
	}
	igual(t, "página 2", len(r.Data), 1)
	igual(t, "ticket", r.Data[0].TicketID, a)
	igual(t, "nuevos en la ventana", r.Data[0].NuevosEnVentana, 2)
	igual(t, "horas", r.Data[0].HorasHastaVolver, 72)
	if r.NextCursor != nil {
		t.Fatal("no debe haber más páginas")
	}
	in.Cursor = "no-es-un-cursor"
	_, err = m.Reincidencias.Ejecutar(como(t), in)
	problema(t, err, 422, "validacion")

	in = reportes.EntradaReincidencias{FiltrosTickets: reportes.FiltrosTickets{FiltrosPeriodo: octubre()}, Dias: 45}
	r, err = m.Reincidencias.Ejecutar(como(t), in)
	if err != nil {
		t.Fatal(err)
	}
	igual(t, "con 45 días", r.Reincidentes, 3)
}

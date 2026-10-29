package reportes_test

import (
	"context"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
	"github.com/HCDestroyer/iris/apps/api/internal/reportes"
)

// Carga (Fase 5): 10 000 tickets abiertos, un año de historia (60 000 tickets resueltos y 20 000
// órdenes finalizadas). Cada reporte del año completo debe responder en poco tiempo y los de un
// mes deben usar los índices de la migración soporte_reportes_indices (EXPLAIN de la consulta
// real que genera sqlc).

// limiteCarga: holgado para CI (en una máquina de desarrollo cada reporte tarda decenas de ms).
const limiteCarga = 3 * time.Second

func sembrarCarga(t *testing.T) {
	t.Helper()
	limpiar(t)
	pool := pruebas.Pool(t)
	ctx := context.Background()
	inicio := time.Now()
	// $1 = ahora. Zonas 1–5, cuadrillas 1–5, 5 000 servicios, 3 000 clientes, prioridades y
	// categorías repartidas; los resueltos, en el año anterior a ahora.
	if _, err := pool.Exec(ctx, `
		INSERT INTO soporte.tickets (empresa_id, zona_id, cuadrilla_id, categoria_id, servicio_id, cliente_id, prioridad,
			estado, origen, asunto, sla_inicio, created_at)
		SELECT NULL, 1 + n % 5, 1 + n % 5, (SELECT id FROM soporte.categorias WHERE empresa_id IS NULL ORDER BY id LIMIT 1 OFFSET n % 5),
			1 + n % 5000, 1 + n % 3000, (ARRAY['baja','media','alta','critica'])[1 + n % 4], 'abierto', 'panel', 'Abierto ' || n,
			$1::timestamptz - (n % 720) * interval '1 hour', $1::timestamptz - (n % 720) * interval '1 hour'
		FROM generate_series(1, 10000) n`, ahora); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO soporte.tickets (empresa_id, zona_id, cuadrilla_id, categoria_id, servicio_id, cliente_id, prioridad,
			estado, origen, asunto, sla_inicio, created_at, sla_respuesta_min, vence_respuesta, primera_respuesta_en,
			sla_resolucion_min, vence_resolucion, resuelto_en)
		SELECT NULL, 1 + n % 5, 1 + n % 5, (SELECT id FROM soporte.categorias WHERE empresa_id IS NULL ORDER BY id LIMIT 1 OFFSET n % 7),
			CASE WHEN n % 10 = 0 THEN NULL ELSE 1 + n % 5000 END, 1 + n % 3000, (ARRAY['baja','media','alta','critica'])[1 + n % 4],
			'resuelto', 'panel', 'Resuelto ' || n, c, c, 60, c + interval '1 hour', c + (n % 90) * interval '1 minute',
			480, c + interval '8 hours', c + (30 + n % 900) * interval '1 minute'
		FROM generate_series(1, 60000) n, LATERAL (SELECT $1::timestamptz - (n % 8760) * interval '1 hour' - interval '1 day' AS c) x`, ahora); err != nil {
		t.Fatal(err)
	}
	tec := usuario(t, "Técnico de carga")
	if _, err := pool.Exec(ctx, `
		INSERT INTO campo.ordenes (empresa_id, tipo_id, servicio_id, zona_id, cuadrilla_id, direccion, tecnico_id, estado,
			en_camino_en, en_sitio_en, finalizada_en, resultado, motivo_falla, pausada_seg, created_at)
		SELECT NULL, (SELECT id FROM campo.tipos_orden ORDER BY id LIMIT 1 OFFSET n % 3), 1 + n % 5000, 1 + n % 5, 1 + n % 5,
			'Calle ' || n, $2, CASE WHEN n % 9 = 0 THEN 'fallida' ELSE 'cerrada' END, f - interval '2 hours', f - interval '1 hour', f,
			CASE WHEN n % 9 = 0 THEN 'fallida' WHEN n % 7 = 0 THEN 'parcial' ELSE 'exitosa' END,
			CASE WHEN n % 9 = 0 THEN 'cliente ausente' END, (n % 4) * 300, f - interval '3 days'
		FROM generate_series(1, 20000) n, LATERAL (SELECT $1::timestamptz - (n % 8760) * interval '1 hour' AS f) x`, ahora, tec); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "ANALYZE soporte.tickets; ANALYZE campo.ordenes; ANALYZE campo.orden_pausas"); err != nil {
		t.Fatal(err)
	}
	t.Logf("siembra: %v", time.Since(inicio))
}

func TestCargaReportes(t *testing.T) {
	sembrarCarga(t)
	m := modulo()
	anio := reportes.FiltrosPeriodo{Desde: ahora.AddDate(-1, 0, 1).Format(time.DateOnly), Hasta: ahora.Format(time.DateOnly)}
	medir := func(nombre string, fn func() error) {
		t.Helper()
		inicio := time.Now()
		if err := fn(); err != nil {
			t.Fatalf("%s: %v", nombre, err)
		}
		d := time.Since(inicio)
		t.Logf("%s: %v", nombre, d)
		if d > limiteCarga {
			t.Errorf("%s tardó %v (límite %v)", nombre, d, limiteCarga)
		}
	}
	for _, ctx := range []struct {
		nombre string
		ctx    context.Context
	}{
		{"global", como(t)},
		{"zona 2", conAlcance(t, contexto.Alcance{ZonaID: 2})},
	} {
		c := ctx.ctx
		medir(ctx.nombre+" sla", func() error {
			r, err := m.SLA.Ejecutar(c, reportes.FiltrosTickets{FiltrosPeriodo: anio})
			if err == nil && r.Total.Resueltos == 0 {
				t.Error("sla sin datos")
			}
			return err
		})
		medir(ctx.nombre+" resolución", func() error {
			_, err := m.Resolucion.Ejecutar(c, reportes.FiltrosTickets{FiltrosPeriodo: anio})
			return err
		})
		medir(ctx.nombre+" técnicos", func() error {
			r, err := m.Tecnicos.Ejecutar(c, reportes.FiltrosOrdenes{FiltrosPeriodo: anio})
			if err == nil && r.Total.Finalizadas == 0 {
				t.Error("técnicos sin datos")
			}
			return err
		})
		medir(ctx.nombre+" primera visita", func() error {
			_, err := m.PrimeraVisita.Ejecutar(c, reportes.EntradaPrimeraVisita{FiltrosPeriodo: anio})
			return err
		})
		medir(ctx.nombre+" reincidencias", func() error {
			r, err := m.Reincidencias.Ejecutar(c, reportes.EntradaReincidencias{FiltrosTickets: reportes.FiltrosTickets{FiltrosPeriodo: anio}})
			if err == nil && r.Reincidentes == 0 {
				t.Error("reincidencias sin datos")
			}
			return err
		})
	}

	// EXPLAIN de las consultas reales (las que genera sqlc) para un mes: deben usar los índices.
	fuente, err := os.ReadFile("bd/consultas.sql.go")
	if err != nil {
		t.Fatal(err)
	}
	consulta := func(nombre string) string {
		t.Helper()
		m := regexp.MustCompile("(?s)const " + nombre + " = `-- name: [^\\n]*\\n(.*?)`").FindSubmatch(fuente)
		if m == nil {
			t.Fatalf("no encuentro la consulta %s", nombre)
		}
		return string(m[1])
	}
	mesInicio, mesFin := ahora.AddDate(0, -1, 0), ahora
	sinZonas, sinPrioridades := []int64{}, []string{}
	plan := func(sql string, args ...any) string {
		t.Helper()
		filas, err := pruebas.Pool(t).Query(context.Background(), "EXPLAIN "+sql, args...)
		if err != nil {
			t.Fatal(err)
		}
		defer filas.Close()
		var b strings.Builder
		for filas.Next() {
			var linea string
			if err := filas.Scan(&linea); err != nil {
				t.Fatal(err)
			}
			b.WriteString(linea + "\n")
		}
		return b.String()
	}
	debeUsar := func(nombre, texto string, indices ...string) {
		t.Helper()
		for _, i := range indices {
			if !strings.Contains(texto, i) {
				t.Errorf("%s no usa %s:\n%s", nombre, i, texto)
			}
		}
	}
	debeUsar("ReporteSLA", plan(consulta("reporteSLA"), &mesInicio, &mesFin, false, nil, true, sinZonas, sinZonas,
		nil, nil, nil, nil, sinPrioridades), "tickets_resueltos_idx")
	debeUsar("ReporteTecnicos", plan(consulta("reporteTecnicos"), &mesInicio, &mesFin, false, nil, true, sinZonas, sinZonas,
		nil, nil, nil), "ordenes_finalizadas_idx")
	debeUsar("ListarReincidencias", plan(consulta("listarReincidencias"), true, sinZonas, sinZonas, &mesInicio, &mesFin, nil, nil, false, nil,
		nil, nil, nil, nil, sinPrioridades, 51, 30), "tickets_resueltos_idx", "tickets_servicio_creado_idx")
	debeUsar("ReportePrimeraVisita", plan(consulta("reportePrimeraVisita"), 30, ahora, &mesInicio, &mesFin, false, nil, true,
		sinZonas, sinZonas, nil, nil, nil), "ordenes_finalizadas_idx", "ordenes_servicio_creada_idx", "tickets_servicio_creado_idx")
}

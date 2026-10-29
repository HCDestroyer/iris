// Package carga_test mide las dos consultas de soporte que señaló la prueba de carga de I5
// (.claude/integration/i5.md): el cron de SLA (AlertasSLAPendientes) y la búsqueda q de la
// bandeja (ListarTickets). Va en su propio paquete porque siembra decenas de miles de tickets en
// su base (cada paquete de pruebas recrea la suya) sin afectar a las demás pruebas de soporte.
package carga_test

import (
	"context"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
	"github.com/HCDestroyer/iris/apps/api/internal/soporte"
)

func TestMain(m *testing.M) { os.Exit(pruebas.Correr(m)) }

var ahora = time.Date(2026, 10, 29, 18, 0, 0, 0, time.UTC)

// consultaAnteriorSLA es AlertasSLAPendientes antes de esta revisión: la referencia para comprobar
// que la nueva devuelve lo mismo.
const consultaAnteriorSLA = `
SELECT t.id AS ticket_id, t.empresa_id, r.sla::text AS sla, r.umbral::text AS umbral, r.vence_en::timestamptz AS vence_en
FROM soporte.tickets t
CROSS JOIN LATERAL (VALUES
    ('respuesta', 'en_riesgo', t.vence_respuesta,
     t.primera_respuesta_en IS NULL AND t.estado IN ('nuevo', 'abierto', 'en_espera')
     AND t.riesgo_respuesta <= $1::timestamptz AND t.vence_respuesta > $1::timestamptz),
    ('respuesta', 'vencido', t.vence_respuesta,
     t.primera_respuesta_en IS NULL AND t.estado IN ('nuevo', 'abierto', 'en_espera')
     AND t.vence_respuesta <= $1::timestamptz),
    ('resolucion', 'en_riesgo', t.vence_resolucion,
     t.estado IN ('nuevo', 'abierto') AND t.riesgo_resolucion <= $1::timestamptz AND t.vence_resolucion > $1::timestamptz),
    ('resolucion', 'vencido', t.vence_resolucion,
     t.estado IN ('nuevo', 'abierto') AND t.vence_resolucion <= $1::timestamptz)
) AS r (sla, umbral, vence_en, aplica)
WHERE (t.estado IN ('nuevo', 'abierto', 'en_espera'))
  AND (t.riesgo_respuesta <= $1::timestamptz OR t.riesgo_resolucion <= $1::timestamptz)
  AND r.aplica
  AND NOT EXISTS (
      SELECT 1 FROM soporte.ticket_sla_alertas a
      WHERE a.ticket_id = t.id AND a.sla = r.sla AND a.umbral = r.umbral AND a.vence_en = r.vence_en
  )
ORDER BY t.id, r.sla, r.umbral
LIMIT $2`

// consulta devuelve el SQL que generó sqlc para una consulta (la que corre en producción).
func consulta(t *testing.T, nombre string) string {
	t.Helper()
	fuente, err := os.ReadFile("../bd/consultas.sql.go")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile("(?s)const " + nombre + " = `-- name: [^\\n]*\\n(.*?)`").FindSubmatch(fuente)
	if m == nil {
		t.Fatalf("no encuentro la consulta %s", nombre)
	}
	return string(m[1])
}

// sembrar: 60 000 tickets de un año (56 000 cerrados con sus alertas ya emitidas), 2 000 vivos con
// un reloj en riesgo o vencido (la mitad ya alertados), 1 000 en espera y 1 000 vivos sin riesgo.
func sembrar(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	pool := pruebas.Pool(t)
	inicio := time.Now()
	for _, sql := range []string{
		// Cerrados: relojes en el pasado y sus alertas emitidas.
		`INSERT INTO soporte.tickets (prioridad, estado, origen, asunto, contacto_nombre, sla_inicio, created_at,
			sla_respuesta_min, riesgo_respuesta, vence_respuesta, primera_respuesta_en,
			sla_resolucion_min, riesgo_resolucion, vence_resolucion, resuelto_en, cerrado_en)
		 SELECT 'media', 'cerrado', 'panel', 'Lentitud en el nodo ' || n, 'Cliente ' || n, c, c,
			60, c + interval '48 minutes', c + interval '1 hour', c + interval '30 minutes',
			480, c + interval '384 minutes', c + interval '8 hours', c + interval '7 hours', c + interval '9 hours'
		 FROM generate_series(1, 56000) n, LATERAL (SELECT $1::timestamptz - (n % 8760) * interval '1 hour' - interval '2 days' AS c) x`,
		`INSERT INTO soporte.ticket_sla_alertas (ticket_id, sla, umbral, vence_en)
		 SELECT id, 'resolucion', u, vence_resolucion FROM soporte.tickets, unnest(ARRAY['en_riesgo', 'vencido']) u
		 WHERE estado = 'cerrado'`,
		// Vivos en riesgo o vencidos (respuesta y resolución).
		`INSERT INTO soporte.tickets (prioridad, estado, origen, asunto, contacto_nombre, sla_inicio, created_at,
			sla_respuesta_min, riesgo_respuesta, vence_respuesta, sla_resolucion_min, riesgo_resolucion, vence_resolucion)
		 SELECT 'alta', CASE WHEN n % 2 = 0 THEN 'nuevo' ELSE 'abierto' END, 'panel', 'Sin internet ' || n, 'Cliente vivo ' || n,
			c, c, 60, c + interval '48 minutes', c + interval '1 hour', 480, c + interval '384 minutes', c + interval '8 hours'
		 FROM generate_series(1, 2000) n, LATERAL (SELECT $1::timestamptz - (n % 600) * interval '1 minute' AS c) x`,
		// En espera (respuesta sin dar; resolución pausada) y vivos sin riesgo todavía.
		`INSERT INTO soporte.tickets (prioridad, estado, origen, asunto, sla_inicio, created_at, en_espera_desde,
			sla_respuesta_min, riesgo_respuesta, vence_respuesta, sla_resolucion_min)
		 SELECT 'baja', 'en_espera', 'panel', 'En espera ' || n, c, c, c + interval '5 minutes',
			60, c + interval '48 minutes', c + interval '1 hour', 480
		 FROM generate_series(1, 1000) n, LATERAL (SELECT $1::timestamptz - (n % 300) * interval '1 minute' AS c) x`,
		`INSERT INTO soporte.tickets (prioridad, estado, origen, asunto, sla_inicio, created_at,
			sla_respuesta_min, riesgo_respuesta, vence_respuesta, sla_resolucion_min, riesgo_resolucion, vence_resolucion)
		 SELECT 'media', 'nuevo', 'panel', 'Reciente ' || n, $1::timestamptz, $1::timestamptz,
			60, $1::timestamptz + interval '48 minutes', $1::timestamptz + interval '1 hour', 480,
			$1::timestamptz + interval '384 minutes', $1::timestamptz + interval '8 hours'
		 FROM generate_series(1, 1000) n`,
		// La mitad de los vivos ya tiene sus alertas de respuesta.
		`INSERT INTO soporte.ticket_sla_alertas (ticket_id, sla, umbral, vence_en)
		 SELECT id, 'respuesta', u, vence_respuesta FROM soporte.tickets, unnest(ARRAY['en_riesgo', 'vencido']) u
		 WHERE estado IN ('nuevo', 'abierto') AND id % 2 = 0 AND vence_respuesta <= $1::timestamptz`,
	} {
		args := []any{}
		if strings.Contains(sql, "$1") {
			args = append(args, ahora)
		}
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(ctx, "ANALYZE soporte.tickets; ANALYZE soporte.ticket_sla_alertas"); err != nil {
		t.Fatal(err)
	}
	t.Logf("siembra: %v", time.Since(inicio))
}

type alerta struct {
	Ticket  int64
	Empresa *int64
	SLA     string
	Umbral  string
	VenceEn time.Time
}

func filas(t *testing.T, sql string, args ...any) ([]alerta, time.Duration) {
	t.Helper()
	mejor := time.Duration(1 << 62)
	var salida []alerta
	for range 5 { // la mejor de 5 (cache caliente)
		inicio := time.Now()
		rows, err := pruebas.Pool(t).Query(context.Background(), sql, args...)
		if err != nil {
			t.Fatal(err)
		}
		salida, err = pgx.CollectRows(rows, func(f pgx.CollectableRow) (alerta, error) {
			var a alerta
			err := f.Scan(&a.Ticket, &a.Empresa, &a.SLA, &a.Umbral, &a.VenceEn)
			return a, err
		})
		if err != nil {
			t.Fatal(err)
		}
		mejor = min(mejor, time.Since(inicio))
	}
	return salida, mejor
}

func plan(t *testing.T, sql string, args ...any) string {
	t.Helper()
	rows, err := pruebas.Pool(t).Query(context.Background(), "EXPLAIN "+sql, args...)
	if err != nil {
		t.Fatal(err)
	}
	lineas, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	return strings.Join(lineas, "\n")
}

func TestCargaSoporte(t *testing.T) {
	sembrar(t)

	// 1. Cron de SLA: mismo resultado que la consulta anterior, con los índices parciales.
	nueva := consulta(t, "alertasSLAPendientes")
	antes, tAntes := filas(t, consultaAnteriorSLA, ahora, 100000)
	despues, tDespues := filas(t, nueva, ahora, 100000)
	t.Logf("AlertasSLAPendientes (60 000 tickets, %d pendientes): antes %v, después %v", len(despues), tAntes, tDespues)
	if len(antes) == 0 || !slices.EqualFunc(antes, despues, func(a, b alerta) bool {
		return a.Ticket == b.Ticket && a.SLA == b.SLA && a.Umbral == b.Umbral && a.VenceEn.Equal(b.VenceEn)
	}) {
		t.Fatalf("la consulta nueva no devuelve lo mismo: %d antes, %d después", len(antes), len(despues))
	}
	p := plan(t, nueva, ahora, 200)
	for _, indice := range []string{"tickets_sla_respuesta_idx", "tickets_sla_resolucion_idx", "ticket_sla_alertas_pkey"} {
		if !strings.Contains(p, indice) {
			t.Errorf("el cron no usa %s:\n%s", indice, p)
		}
	}
	for _, recorrido := range []string{"Seq Scan on tickets", "tickets_pkey", "Seq Scan on ticket_sla_alertas", "Anti Join"} {
		if strings.Contains(p, recorrido) {
			t.Errorf("el cron hace %s (recorre la historia):\n%s", recorrido, p)
		}
	}
	t.Logf("plan del cron:\n%s", p)

	// La corrida completa emite todo en lotes de 200 (una transacción y una fila de auditoría por
	// lote con alertas) y una segunda corrida no emite nada.
	m := soporte.Nuevo(soporte.Config{Reloj: func() time.Time { return ahora }})
	sis := contexto.Sistema(pruebas.ContextoSistema(pruebas.Pool(t)), contexto.CanalCron)
	inicio := time.Now()
	res, err := m.RevisarSLA.Ejecutar(sis, soporte.EntradaRevisarSLA{})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("corrida del cron: %d alertas en %v", res.Emitidas(), time.Since(inicio))
	if res.Emitidas() != len(despues) {
		t.Fatalf("emitidas %d, pendientes %d", res.Emitidas(), len(despues))
	}
	var lotes int
	if err := pruebas.Pool(t).QueryRow(context.Background(),
		"SELECT count(*) FROM auditoria.acciones WHERE accion = 'soporte.revisar_sla' AND resultado = 'ok'").Scan(&lotes); err != nil {
		t.Fatal(err)
	}
	if esperados := (len(despues) + soporte.LoteSLAPorDefecto - 1) / soporte.LoteSLAPorDefecto; lotes != esperados {
		t.Fatalf("lotes: %d, esperaba %d", lotes, esperados)
	}
	if res, err := m.RevisarSLA.Ejecutar(sis, soporte.EntradaRevisarSLA{}); err != nil || res.Emitidas() != 0 {
		t.Fatalf("segunda corrida: %+v %v", res, err)
	}
	_, tVacio := filas(t, nueva, ahora, 200)
	_, tVacioAntes := filas(t, consultaAnteriorSLA, ahora, 200)
	t.Logf("AlertasSLAPendientes sin nada pendiente (un año de alertas en la historia): antes %v, después %v", tVacioAntes, tVacio)

	// 2. Búsqueda q: trigramas (GIN) y el mismo resultado que sin índices.
	listar := consulta(t, "listarTickets")
	args := func(q string) []any {
		return []any{nil, true, nil, []string{}, []string{}, nil, nil, false, nil, nil, nil, nil, q, true, []int64{}, []int64{}, nil, 51}
	}
	ids := func(sinIndices bool, q string) ([]int64, time.Duration) {
		t.Helper()
		ctx := context.Background()
		tx, err := pruebas.Pool(t).Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		if sinIndices {
			if _, err := tx.Exec(ctx, "SET LOCAL enable_bitmapscan = off"); err != nil {
				t.Fatal(err)
			}
		}
		mejor := time.Duration(1 << 62)
		var salida []int64
		for range 5 {
			inicio := time.Now()
			rows, err := tx.Query(ctx, "SELECT id FROM ("+listar+") lista", args(q)...)
			if err != nil {
				t.Fatal(err)
			}
			salida, err = pgx.CollectRows(rows, pgx.RowTo[int64])
			if err != nil {
				t.Fatal(err)
			}
			mejor = min(mejor, time.Since(inicio))
		}
		return salida, mejor
	}
	for _, q := range []string{"nodo 4242", "Cliente vivo 199", "TK-0007", "no-existe-zz", "nodo"} {
		sin, tSin := ids(true, q)
		con, tCon := ids(false, q)
		t.Logf("ListarTickets q=%q: sin trigramas %v, con trigramas %v (%d filas)", q, tSin, tCon, len(con))
		if !slices.Equal(sin, con) || (q != "no-existe-zz" && len(con) == 0) {
			t.Fatalf("q=%q cambia el resultado: %v / %v", q, sin, con)
		}
	}
	p = plan(t, listar, args("no-existe-zz")...)
	if !strings.Contains(p, "trgm_idx") {
		t.Errorf("la búsqueda no usa los índices de trigramas:\n%s", p)
	}
	t.Logf("plan de la búsqueda:\n%s", p)
}

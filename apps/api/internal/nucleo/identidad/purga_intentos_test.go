package identidad_test

import (
	"context"
	"testing"
	"time"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
	"github.com/HCDestroyer/iris/apps/api/migraciones"
)

// intento inserta una fila de nucleo.intentos_login con su último fallo hace `hace` y, si
// bloqueo no es vacío, bloqueada hasta now() + bloqueo.
func intento(t *testing.T, correo, hace, bloqueo string) {
	t.Helper()
	if _, err := pruebas.Pool(t).Exec(context.Background(), `INSERT INTO nucleo.intentos_login (correo, fallos, bloqueado_hasta, updated_at)
		VALUES ($1, 3, CASE WHEN $3 = '' THEN NULL ELSE now() + $3::interval END, now() - $2::interval)`, correo, hace, bloqueo); err != nil {
		t.Fatal(err)
	}
}

func quedan(t *testing.T, prefijo string) map[string]bool {
	t.Helper()
	filas, err := pruebas.Pool(t).Query(context.Background(), `SELECT correo FROM nucleo.intentos_login WHERE correo LIKE $1 || '%'`, prefijo)
	if err != nil {
		t.Fatal(err)
	}
	defer filas.Close()
	res := map[string]bool{}
	for filas.Next() {
		var c string
		if err := filas.Scan(&c); err != nil {
			t.Fatal(err)
		}
		res[c] = true
	}
	return res
}

// Borra por lotes lo que ya no cuenta (último fallo fuera de la ventana y sin bloqueo vigente) y
// deja lo que sí: fallos recientes y bloqueos vigentes, aunque el último fallo sea viejo.
func TestPurgarIntentosLogin(t *testing.T) {
	ctx := context.Background()
	pool := pruebas.Pool(t)
	p := "purga-" + pruebas.Unico() + "-"
	intento(t, p+"reciente", "5 minutes", "")
	intento(t, p+"bloqueado", "40 minutes", "10 minutes")
	intento(t, p+"usuario:1", "10 minutes", "")
	intento(t, p+"bloqueo-vencido", "40 minutes", "-5 minutes")
	// 250 viejas con lotes de 100: tres sentencias.
	if _, err := pool.Exec(ctx, `INSERT INTO nucleo.intentos_login (correo, fallos, updated_at)
		SELECT $1 || 'viejo-' || g, 1, now() - interval '31 days' FROM generate_series(1, 250) g`, p); err != nil {
		t.Fatal(err)
	}

	antiguedad := identidad.AntiguedadPurgaIntentos(0, 0)
	if antiguedad != 15*time.Minute {
		t.Fatalf("antigüedad por defecto: %v", antiguedad)
	}
	n, err := identidad.PurgarIntentosLogin(ctx, pool, time.Now(), antiguedad, 100)
	if err != nil {
		t.Fatal(err)
	}
	// Otras pruebas en paralelo pueden dejar filas, pero nunca viejas: al menos las 251 nuestras.
	if n < 251 {
		t.Fatalf("purgadas %d, se esperaban al menos 251", n)
	}
	q := quedan(t, p)
	if len(q) != 3 || !q[p+"reciente"] || !q[p+"bloqueado"] || !q[p+"usuario:1"] {
		t.Fatalf("quedan %v", q)
	}

	// Con una ventana más larga (configurada), los fallos de hace 10 min siguen contando.
	intento(t, p+"viejo-otra-vez", "2 hours", "")
	if _, err := identidad.PurgarIntentosLogin(ctx, pool, time.Now(), identidad.AntiguedadPurgaIntentos(time.Hour, 0), 0); err != nil {
		t.Fatal(err)
	}
	if q := quedan(t, p); len(q) != 3 || q[p+"viejo-otra-vez"] {
		t.Fatalf("con ventana de 1 h quedan %v", q)
	}
}

// El job de River (como lo corre cmd/worker) purga y es único por período.
func TestJobPurgaIntentosLogin(t *testing.T) {
	pool := pruebas.Pool(t)
	p := "job-" + pruebas.Unico() + "-"
	intento(t, p+"viejo", "1 hour", "")
	intento(t, p+"reciente", "1 minute", "")

	if opts := (identidad.ArgsPurgaIntentosLogin{}).InsertOpts(); opts.UniqueOpts.ByPeriod <= 0 {
		t.Fatalf("el job debe ser único por período: %+v", opts.UniqueOpts)
	}

	workers := river.NewWorkers()
	identidad.RegistrarPurgaIntentos(workers, pool, 0, 0)
	cliente, err := river.NewClient(riverpgxv5.New(pool), &river.Config{
		Schema: migraciones.EsquemaRiver, Workers: workers,
		Queues: map[string]river.QueueConfig{river.QueueDefault: {MaxWorkers: 1}},
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancelar := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancelar()
	eventos, desuscribir := cliente.Subscribe(river.EventKindJobCompleted, river.EventKindJobFailed)
	defer desuscribir()
	if err := cliente.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cliente.Stop(context.Background()) }()
	// Sin la unicidad por período (otra corrida de la prueba en la misma media hora la chocaría).
	if _, err := cliente.Insert(ctx, identidad.ArgsPurgaIntentosLogin{}, &river.InsertOpts{UniqueOpts: river.UniqueOpts{}}); err != nil {
		t.Fatal(err)
	}
	for terminado := false; !terminado; {
		select {
		case ev := <-eventos:
			if ev.Job.Kind != (identidad.ArgsPurgaIntentosLogin{}).Kind() {
				continue
			}
			if ev.Kind != river.EventKindJobCompleted {
				t.Fatalf("el job falló: %v", ev.Job.Errors)
			}
			terminado = true
		case <-ctx.Done():
			t.Fatal("el job no terminó")
		}
	}
	if q := quedan(t, p); len(q) != 1 || !q[p+"reciente"] {
		t.Fatalf("quedan %v", q)
	}
}

// Portal: borra sesiones vencidas o revocadas hace más de la retención y luego los tickets
// vencidos sin sesiones; conserva lo reciente y el ticket de una sesión que sigue guardada.
func TestPurgarPortal(t *testing.T) {
	ctx := context.Background()
	pool := pruebas.Pool(t)
	if _, err := pool.Exec(ctx, `INSERT INTO nucleo.empresas (id, nombre) VALUES (1, 'Empresa 1') ON CONFLICT (id) DO NOTHING`); err != nil {
		t.Fatal(err)
	}
	cliente := time.Now().UnixNano() % 1_000_000_000
	acceso := func(nombre, vencio string) (id int64) {
		t.Helper()
		if err := pool.QueryRow(ctx, `INSERT INTO nucleo.portal_accesos (ticket_hash, cliente_id, empresa_id, nombre, expira_en, created_at)
			VALUES (decode(md5(random()::text), 'hex'), $1, 1, $2, now() - $3::interval, now() - $3::interval) RETURNING id`, cliente, nombre, vencio).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	sesion := func(accesoID int64, nombre, vencio, revocada string) {
		t.Helper()
		if _, err := pool.Exec(ctx, `INSERT INTO nucleo.portal_sesiones (acceso_id, cliente_id, empresa_id, nombre, token_hash, csrf_hash, expira_en, revocada_en)
			VALUES ($1, $2, 1, $3, decode(md5(random()::text), 'hex'), decode(md5(random()::text), 'hex'), now() - $4::interval,
			        CASE WHEN $5 = '' THEN NULL ELSE now() - $5::interval END)`, accesoID, cliente, nombre, vencio, revocada); err != nil {
			t.Fatal(err)
		}
	}
	// Vieja (vencida hace 40 días) con su ticket: se van las dos.
	sesion(acceso("a-viejo", "40 days"), "s-vieja", "40 days", "")
	// Revocada hace 35 días aunque «vence» en el futuro: se va; su ticket viejo, también.
	sesion(acceso("a-revocada", "35 days"), "s-revocada", "-1 hour", "35 days")
	// Ticket viejo con una sesión reciente (vencida hace 1 día): quedan los dos.
	sesion(acceso("a-con-reciente", "31 days"), "s-reciente", "1 day", "")
	// Ticket viejo sin sesión (nunca se canjeó): se va. Ticket reciente sin sesión: queda.
	acceso("a-sin-canjear", "31 days")
	acceso("a-reciente", "10 minutes")

	res, err := identidad.PurgarPortal(ctx, pool, time.Now(), identidad.RetencionPortal, 1)
	if err != nil {
		t.Fatal(err)
	}
	if res.Sesiones < 2 || res.Accesos < 3 {
		t.Fatalf("purgados %+v", res)
	}
	nombres := func(tabla string) map[string]bool {
		filas, err := pool.Query(ctx, `SELECT nombre FROM nucleo.`+tabla+` WHERE cliente_id = $1`, cliente)
		if err != nil {
			t.Fatal(err)
		}
		defer filas.Close()
		m := map[string]bool{}
		for filas.Next() {
			var n string
			_ = filas.Scan(&n)
			m[n] = true
		}
		return m
	}
	if s := nombres("portal_sesiones"); len(s) != 1 || !s["s-reciente"] {
		t.Fatalf("sesiones que quedan: %v", s)
	}
	if a := nombres("portal_accesos"); len(a) != 2 || !a["a-con-reciente"] || !a["a-reciente"] {
		t.Fatalf("accesos que quedan: %v", a)
	}
}

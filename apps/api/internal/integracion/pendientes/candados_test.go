package pendientes_test

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/integracion/pendientes"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
)

// Orden de candados (B3, B-b): la red de seguridad y el resultado de un paso del titular toman la orden
// antes que el pendiente. T1 tiene la orden; Procesar queda esperando su candado y, mientras espera,
// T1 puede tomar el pendiente con NOWAIT. Con el orden invertido, Procesar ya tendría el pendiente y el
// NOWAIT fallaría.

// ordenReal inserta una fila en campo.ordenes (para que el candado de la orden exista).
func (e *entorno) ordenReal() int64 {
	e.t.Helper()
	var id int64
	if err := e.pool.QueryRow(context.Background(), `INSERT INTO campo.ordenes (tipo_id, direccion)
		SELECT id, 'Zona 1' FROM campo.tipos_orden WHERE codigo = 'visita' RETURNING id`).Scan(&id); err != nil {
		e.t.Fatal(err)
	}
	return id
}

// esperarCandadoDeOrden espera a que otra conexión quede esperando el candado de campo.ordenes.
func (e *entorno) esperarCandadoDeOrden(hecho <-chan error) {
	e.t.Helper()
	limite := time.Now().Add(10 * time.Second)
	for time.Now().Before(limite) {
		select {
		case err := <-hecho:
			e.t.Fatalf("Procesar terminó sin esperar el candado de la orden: %v", err)
		default:
		}
		var n int
		if err := e.pool.QueryRow(context.Background(), `SELECT count(*) FROM pg_stat_activity
			WHERE datname = current_database() AND wait_event_type = 'Lock' AND query ILIKE '%campo.ordenes%'`).Scan(&n); err != nil {
			e.t.Fatal(err)
		}
		if n > 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	e.t.Fatal("Procesar no llegó a esperar el candado de la orden")
}

// probarOrdenDeCandados: T1 toma la orden (modo FOR UPDATE o FOR SHARE), Procesar corre en una
// goroutine y espera; T1 toma el pendiente con NOWAIT y suelta todo.
func (e *entorno) probarOrdenDeCandados(orden, pendiente int64, modo string) {
	e.t.Helper()
	ctx := context.Background()
	t1, err := e.pool.Begin(ctx)
	if err != nil {
		e.t.Fatal(err)
	}
	defer func() { _ = t1.Rollback(ctx) }()
	if _, err := t1.Exec(ctx, "SELECT id FROM campo.ordenes WHERE id = $1 "+modo, orden); err != nil {
		e.t.Fatal(err)
	}
	hecho := make(chan error, 1)
	go func() {
		_, err := e.m.Procesar(e.ctx())
		hecho <- err
	}()
	e.esperarCandadoDeOrden(hecho)
	var x int64
	errNowait := t1.QueryRow(ctx, "SELECT id FROM integracion.pendientes WHERE id = $1 FOR UPDATE NOWAIT", pendiente).Scan(&x)
	if err := t1.Rollback(ctx); err != nil && err != pgx.ErrTxClosed {
		e.t.Fatal(err)
	}
	select {
	case err := <-hecho:
		if err != nil {
			e.t.Fatalf("Procesar: %v", err)
		}
	case <-time.After(15 * time.Second):
		e.t.Fatal("Procesar no terminó al soltar la orden")
	}
	if errNowait != nil {
		e.t.Fatalf("mientras Procesar espera la orden, el pendiente debe estar libre (orden → pendiente): %v", errNowait)
	}
}

// B3: resolverAtascado toma la orden y luego el cargo.
func TestRedDeSeguridadTomaLaOrdenAntesQueElCargo(t *testing.T) {
	e := nuevoEntorno(t)
	ctx := context.Background()
	orden := e.ordenReal()
	id := strconv.FormatInt(orden, 10)
	var dep, cargo int64
	if err := e.pool.QueryRow(ctx, `INSERT INTO integracion.pendientes (empresa_id, sistema, operacion, idempotency_key, entidad, entidad_id, metodo,
		ruta, payload, depende_de, datos_incompletos, estado, proximo_intento, respuesta, hecho_en)
		VALUES (1, 'argosisp', 'cambiar_titular', $1, 'orden', $2, 'POST', '/servicios/20/cambiar-titular', '{}', '{}', false, 'hecho', now(),
		'{"titular":{"cliente_id":777}}', now()) RETURNING id`, "iris:titular:"+id+":1", id).Scan(&dep); err != nil {
		t.Fatal(err)
	}
	marcador, _ := json.Marshal(pendientes.Ref(0, "titular.cliente_id"))
	plantilla := `{"servicio_id":20,"cliente_id":` + string(marcador) + `,"lineas":[{"concepto_id":9,"precio_unitario":"100.00"}],"origen_tipo":"iris_orden","origen_id":"` + id + `"}`
	if err := e.pool.QueryRow(ctx, `INSERT INTO integracion.pendientes (empresa_id, sistema, operacion, idempotency_key, entidad, entidad_id, metodo,
		ruta, payload, depende_de, datos_incompletos, estado, proximo_intento, plantilla, por_resolver)
		VALUES (1, 'argosisp', 'cargo', $1, 'orden', $2, 'POST', '/facturas/cargo-unico', $3, ARRAY[$4::bigint], false, 'pendiente', $5, $3, true)
		RETURNING id`, "iris:cargo:"+id, id, plantilla, dep, pendientes.NuncaVence).Scan(&cargo); err != nil {
		t.Fatal(err)
	}
	e.probarOrdenDeCandados(orden, cargo, "FOR UPDATE")
	// T1 soltó el cargo justo después del NOWAIT: si Procesar lo saltó (SKIP LOCKED), lo resuelve la pasada siguiente.
	e.procesar()
	if n := e.contar(`SELECT count(*) FROM integracion.pendientes WHERE id = $1 AND NOT por_resolver AND payload->>'cliente_id' = '777'`, cargo); n != 1 {
		t.Fatal("el cargo se resuelve con el titular")
	}
}

// B-b: el resultado de un paso del titular toma la orden FOR UPDATE (espera aunque T1 la tenga solo
// FOR SHARE) y antes que el paso.
func TestPasoDelTitularTomaLaOrdenForUpdateAntesQueElPaso(t *testing.T) {
	e := nuevoEntorno(t)
	orden := e.ordenReal()
	id := strconv.FormatInt(orden, 10)
	e.argos.mu.Lock()
	e.argos.cuerpos["/servicios/20/cambiar-titular"] = `{"titular":{"cliente_id":777}}`
	e.argos.mu.Unlock()
	var paso int64
	if err := e.pool.QueryRow(context.Background(), `INSERT INTO integracion.pendientes (empresa_id, sistema, operacion, idempotency_key, entidad, entidad_id, metodo,
		ruta, payload, depende_de, datos_incompletos, estado, proximo_intento)
		VALUES (1, 'argosisp', 'cambiar_titular', $1, 'orden', $2, 'POST', '/servicios/20/cambiar-titular', '{}', '{}', false, 'pendiente', $3)
		RETURNING id`, "iris:titular:"+id+":1", id, e.reloj).Scan(&paso); err != nil {
		t.Fatal(err)
	}
	e.probarOrdenDeCandados(orden, paso, "FOR SHARE")
	if n := e.contar(`SELECT count(*) FROM integracion.pendientes WHERE id = $1 AND estado = 'hecho'`, paso); n != 1 {
		t.Fatal("el paso queda hecho")
	}
}

// Bajo de #286: reintentar un paso del titular sin una orden válida (entidad_id que no es un número)
// falla cerrado (403), igual que resolverlo a mano, aunque quien lo pide tenga todos los permisos.
func TestPasoDelTitularSinOrdenValidaFallaCerrado(t *testing.T) {
	e := nuevoEntorno(t)
	var paso int64
	var version int32
	if err := e.pool.QueryRow(context.Background(), `INSERT INTO integracion.pendientes (empresa_id, sistema, operacion, idempotency_key, entidad, entidad_id, metodo,
		ruta, payload, depende_de, datos_incompletos, estado, proximo_intento, ultimo_error)
		VALUES (1, 'argosisp', 'cambiar_titular', $1, 'orden', 'no-es-una-orden', 'POST', '/servicios/20/cambiar-titular', '{}', '{}', false,
		'fallido_negocio', now(), 'HTTP 422') RETURNING id, version`, "iris:titular:sin-orden:"+nuevaOrden()).Scan(&paso, &version); err != nil {
		t.Fatal(err)
	}
	sup := pruebas.ContextoUsuarioEnEmpresa(e.pool, e.tecnico("u-sin-orden"), 1, nucleo.PermIntegracionesReintentar,
		nucleo.PermIntegracionesResolverPendiente, nucleo.PermOrdenesValidarIdentidad)
	prohibido := func(err error) {
		t.Helper()
		var p *nucleo.Problema
		if !errors.As(err, &p) || p.Status != 403 {
			t.Fatalf("esperaba 403, llegó %v", err)
		}
	}
	_, err := e.m.Reintentar.Ejecutar(sup, pendientes.EntradaReintentar{ID: paso, Version: version})
	prohibido(err)
	_, err = e.m.ResolverManual.Ejecutar(sup, pendientes.EntradaResolverManual{ID: paso, Version: version, Motivo: "Lo corregí en Argos a mano"})
	prohibido(err)
	if n := e.contar("SELECT count(*) FROM integracion.pendientes WHERE id = $1 AND estado = 'fallido_negocio'", paso); n != 1 {
		t.Fatal("el paso no cambia")
	}
}

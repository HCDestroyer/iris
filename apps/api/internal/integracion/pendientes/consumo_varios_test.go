package pendientes_test

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/integracion/pendientes"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
)

// B-2 de la revisión de seguridad de #286: con Argos ≥ 0.14.0 hay un consumo por actor y bodega, así
// que dos técnicos pueden consumir el mismo consumible_cobro. El cargo de ese producto depende de los
// dos consumos y la confirmación los mira todos.

// cerrarDosActores cierra una orden con el producto 5 (consumible_cobro) usado por dos técnicos y el
// cargo de ese producto; devuelve la orden y las claves de los dos consumos.
func (e *entorno) cerrarDosActores(prefijo string) (string, string, string) {
	e.t.Helper()
	e.argos16()
	e.contrato("0.14.0")
	a, b := e.tecnico(prefijo+"-a"), e.tecnico(prefijo+"-b")
	orden := nuevaOrden()
	linea := func(quien int64) map[string]any {
		return map[string]any{"producto_id": 5, "cantidad": 1, "bodega_id": 3, "modalidad": "consumible_cobro",
			"desde_app": true, "usuario_id": quien, "fecha": "2026-11-21T16:00:00Z", "recibido_en_iris": "2026-11-21T16:40:00Z"}
	}
	e.cerrar(map[string]any{
		"orden_id": orden, "tipo": "instalacion", "resultado": "exitosa", "cliente_id": 10, "servicio_id": 20,
		"tecnico_id": a, "bodega_id": 3, "tecnico": map[string]any{"id_externo": prefijo + "-a"},
		"materiales": []any{linea(a), linea(b)},
		"cargo":      map[string]any{"producto_id": 5, "monto": "50.00", "descripcion": "Cable"},
	})
	clave := func(u int64) string {
		return "iris:consumo:" + orden + ":" + strconv.FormatInt(u, 10) + ":3"
	}
	ps := e.pendientes(orden)
	ca, cb := ps[clave(a)], ps[clave(b)]
	if ca.ID == 0 || cb.ID == 0 {
		e.t.Fatalf("dos consumos (uno por actor): %v", ps)
	}
	var depende []int64
	if err := e.pool.QueryRow(context.Background(), "SELECT depende_de FROM integracion.pendientes WHERE idempotency_key = $1",
		"iris:cargo:"+orden).Scan(&depende); err != nil {
		e.t.Fatal(err)
	}
	if len(depende) != 2 || !((depende[0] == ca.ID && depende[1] == cb.ID) || (depende[0] == cb.ID && depende[1] == ca.ID)) {
		e.t.Fatalf("el cargo depende de los dos consumos: %v (%d, %d)", depende, ca.ID, cb.ID)
	}
	return orden, clave(a), clave(b)
}

// Camino automático: el primer consumo confirma el cobro y el segundo todavía no sale (503). El cargo
// sigue esperando (sin resolverse ni fallar) hasta que el segundo también lo confirma.
func TestCargoEsperaATodosLosConsumos(t *testing.T) {
	e := nuevoEntorno(t)
	e.argos.mu.Lock()
	e.argos.cuerpos["/inventario/consumos"] = `{"id": 900, "movimientos": [{"id": 900}], "cargos": [{"id": 31, "producto_id": 5, "monto": "25.00"}]}`
	e.argos.mu.Unlock()
	e.argos.responder("/inventario/consumos", 201, 503, 201)
	orden, primero, segundo := e.cerrarDosActores("u-dos")
	e.procesar()
	ps := e.pendientes(orden)
	if ps[primero].Estado != "hecho" || ps[segundo].Estado != "pendiente" {
		t.Fatalf("un consumo hecho y otro por reintentar: %+v / %+v", ps[primero], ps[segundo])
	}
	if c := ps["iris:cargo:"+orden]; c.Estado != "bloqueado" {
		t.Fatalf("el cargo espera al segundo consumo: %+v", c)
	}
	e.reloj = e.reloj.Add(time.Hour)
	e.procesar()
	ps = e.pendientes(orden)
	if ps[segundo].Estado != "hecho" || ps["iris:cargo:"+orden].Estado != "resuelto_manual" {
		t.Fatalf("con los dos consumos confirmados, el cargo queda resuelto: %+v / %+v", ps[segundo], ps["iris:cargo:"+orden])
	}
	for _, l := range e.argos.copia() {
		if l.Clave == "iris:cargo:"+orden {
			t.Fatal("el cargo de un consumible_cobro salió hacia Argos")
		}
	}
}

// Camino de liberar: un consumo ya cobró el producto y el otro lo rechazó Argos. Resolver a mano el
// rechazado con cargo = liberar cobraría dos veces: 422 cargo_en_otro_consumo, y nada sale.
func TestLiberarCargoConOtroConsumoQueLoCobro(t *testing.T) {
	e := nuevoEntorno(t)
	e.argos.mu.Lock()
	e.argos.cuerpos["/inventario/consumos"] = `{"id": 900, "movimientos": [{"id": 900}], "cargos": [{"id": 31, "producto_id": 5, "monto": "25.00"}]}`
	e.argos.mu.Unlock()
	e.argos.responder("/inventario/consumos", 201, 422)
	orden, primero, segundo := e.cerrarDosActores("u-lib")
	e.procesar()
	ps := e.pendientes(orden)
	if ps[primero].Estado != "hecho" || ps[segundo].Estado != "fallido_negocio" {
		t.Fatalf("uno hecho y otro rechazado: %+v / %+v", ps[primero], ps[segundo])
	}
	sup := pruebas.ContextoUsuarioEnEmpresa(e.pool, e.tecnico(""), 1, nucleo.PermIntegracionesResolverPendiente)
	_, err := e.m.ResolverManual.Ejecutar(sup, pendientes.EntradaResolverManual{ID: ps[segundo].ID, Version: ps[segundo].Version,
		Motivo: "Consumo rehecho en Argos", Cargo: pendientes.CargoLiberar})
	esperarCodigo(t, err, 422, "cargo_en_otro_consumo")
	e.procesar()
	for _, l := range e.argos.copia() {
		if l.Clave == "iris:cargo:"+orden {
			t.Fatal("el cargo no sale")
		}
	}
	// resolver_tambien sí: el cargo queda resuelto sin salir.
	deberP(t)(e.m.ResolverManual.Ejecutar(sup, pendientes.EntradaResolverManual{ID: ps[segundo].ID, Version: ps[segundo].Version,
		Motivo: "Consumo rehecho en Argos, ya cobrado", Cargo: pendientes.CargoResolverTambien}))
	if c := e.pendientes(orden)["iris:cargo:"+orden]; c.Estado != "resuelto_manual" {
		t.Fatalf("cargo resuelto también: %+v", c)
	}
}

func deberP(t *testing.T) func(pendientes.Pendiente, error) {
	return func(_ pendientes.Pendiente, err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
}

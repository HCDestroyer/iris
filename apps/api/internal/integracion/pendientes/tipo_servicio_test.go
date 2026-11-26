package pendientes_test

import (
	"strings"
	"testing"
	"time"
)

// Contrato argosisp.md 0.15.0 §13.1 (A3, B33): 422 tipo_sin_router es de negocio. Se muestra el
// error (con su explicación) y no se reintenta en bucle; el supervisor lo ve en la cola.
func TestTipoSinRouterNoSeReintenta(t *testing.T) {
	e := nuevoEntorno(t)
	orden := nuevaOrden()
	e.argos.mu.Lock()
	e.argos.tipos["/servicios/20/activar-instalacion"] = "urn:argosisp:problema:tipo_sin_router"
	e.argos.mu.Unlock()
	e.argos.responder("/servicios/20/activar-instalacion", 422)
	e.cerrar(map[string]any{"orden_id": orden, "tipo": "instalacion", "resultado": "exitosa", "empresa_id": 1,
		"cliente_id": 10, "servicio_id": "20", "tecnico_id": 7})
	e.procesar()
	f := e.pendientes(orden)["iris:instalacion:"+orden]
	if f.Estado != "fallido_negocio" || f.Intentos != 1 || !strings.Contains(f.Error, "tipo_sin_router") || !strings.Contains(f.Error, "cable") {
		t.Fatalf("tipo_sin_router: %+v", f)
	}
	if n := e.eventos("integracion.pendiente_fallido", orden); n != 1 {
		t.Fatalf("aviso al supervisor: %d", n)
	}
	llamadas := len(e.argos.copia())
	for range 3 {
		e.reloj = e.reloj.Add(24 * time.Hour)
		if n := e.procesar(); n != 0 {
			t.Fatal("un 422 tipo_sin_router no se reintenta solo")
		}
	}
	if len(e.argos.copia()) != llamadas {
		t.Fatal("no debía volver a llamar a Argos")
	}
}

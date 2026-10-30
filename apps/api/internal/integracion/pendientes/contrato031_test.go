package pendientes_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/integracion/pendientes"
)

// Contrato 0.3.1 §3.1: activar-instalacion va sin version; un 409 servicio_no_pendiente es de
// negocio (no se reintenta): la orden queda «cerrada con pendiente en Argos» y se avisa al
// supervisor (integracion.pendiente_fallido con el detail de Argos). Los comodatos de la misma
// orden no dependen de la activación y siguen su curso.
func TestActivarInstalacionServicioNoPendiente(t *testing.T) {
	e := nuevoEntorno(t)
	orden := nuevaOrden()
	ruta := "/servicios/20/activar-instalacion"
	e.argos.responder(ruta, 409)
	e.argos.tipos[ruta] = "urn:argosisp:problema:servicio_no_pendiente"
	e.argos.detalle = "El servicio está activo (no pendiente_instalacion)"
	e.cerrar(map[string]any{"orden_id": orden, "tipo": "instalacion", "resultado": "exitosa", "servicio_id": 20,
		"equipos": []map[string]any{{"accion": "instalar", "serie": "ONT-31"}}})
	if n := e.procesar(); n != 2 {
		t.Fatalf("procesó %d", n)
	}
	clave := "iris:instalacion:" + orden
	f := e.pendientes(orden)[clave]
	if f.Estado != pendientes.EstadoFallidoNegocio || f.Intentos != 1 ||
		!strings.Contains(f.Error, "HTTP 409") || !strings.Contains(f.Error, "servicio_no_pendiente") || !strings.Contains(f.Error, "activo") {
		t.Fatalf("activar con 409 servicio_no_pendiente: %+v", f)
	}
	if c := e.pendientes(orden)["iris:comodato:"+orden+":ONT-31"]; c.Estado != pendientes.EstadoHecho {
		t.Fatalf("el comodato no depende de la activación: %+v", c)
	}
	// Aviso al supervisor, una vez.
	if n := e.eventos(pendientes.EventoFallido, orden); n != 1 {
		t.Fatalf("pendiente_fallido: %d", n)
	}
	var detalle string
	if err := e.pool.QueryRow(context.Background(), `SELECT payload->>'detalle' FROM integracion.outbox
		WHERE evento = $1 AND payload->>'idempotency_key' = $2`, pendientes.EventoFallido, clave).Scan(&detalle); err != nil ||
		!strings.Contains(detalle, "servicio_no_pendiente") {
		t.Fatalf("detalle del aviso: %q %v", detalle, err)
	}
	ps, err := pendientes.DeEntidad(context.Background(), e.pool, "orden", orden)
	if err != nil || !pendientes.ConPendienteEnArgos(ps) {
		t.Fatalf("la orden queda «cerrada con pendiente en Argos»: %+v %v", ps, err)
	}
	// No se reintenta: ni en la siguiente corrida ni días después.
	e.reloj = e.reloj.Add(48 * time.Hour)
	if n := e.procesar(); n != 0 {
		t.Fatalf("reintentó un 409 de negocio (%d)", n)
	}
	activaciones := 0
	for _, l := range e.argos.copia() {
		if l.Ruta != ruta {
			continue
		}
		activaciones++
		// Sin version (§3.1): Argos se protege con la máquina de estados y la Idempotency-Key.
		if _, ok := l.Cuerpo["version"]; ok || l.Clave != clave || l.Cuerpo["origen_tipo"] != "iris_orden" || l.Cuerpo["origen_id"] != orden {
			t.Fatalf("cuerpo de activar-instalacion: %s (clave %s)", l.Crudo, l.Clave)
		}
	}
	if activaciones != 1 {
		t.Fatalf("activaciones: %d", activaciones)
	}
}

// En cambio el 409 idempotencia_en_curso (Argos aún procesa la misma clave) se reintenta con la
// espera del contrato y, cuando Argos termina, queda hecho.
func TestActivarInstalacionEnCursoSeReintenta(t *testing.T) {
	e := nuevoEntorno(t)
	orden := nuevaOrden()
	ruta := "/servicios/20/activar-instalacion"
	e.argos.responder(ruta, 409, 200)
	e.argos.tipos[ruta] = "urn:argosisp:problema:idempotencia_en_curso"
	e.cerrar(map[string]any{"orden_id": orden, "tipo": "instalacion", "resultado": "exitosa", "servicio_id": 20})
	e.procesar()
	clave := "iris:instalacion:" + orden
	if f := e.pendientes(orden)[clave]; f.Estado != pendientes.EstadoPendiente || f.Intentos != 1 || !f.Proximo.Equal(e.reloj.Add(time.Minute)) {
		t.Fatalf("idempotencia_en_curso se reintenta: %+v", f)
	}
	e.reloj = e.reloj.Add(time.Minute)
	e.procesar()
	if f := e.pendientes(orden)[clave]; f.Estado != pendientes.EstadoHecho || f.Intentos != 2 {
		t.Fatalf("tras el reintento: %+v", f)
	}
	if n := e.eventos(pendientes.EventoFallido, orden); n != 0 {
		t.Fatalf("sin aviso: %d", n)
	}
}

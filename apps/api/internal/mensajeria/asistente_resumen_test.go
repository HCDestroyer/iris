package mensajeria_test

import (
	"net/http"
	"testing"
	"time"
)

// D1 (reporte del PM): el resumen del asistente queda guardado en la conversación
// (contratos/conversaciones.md §16.4). Antes se calculaba al vuelo y se perdía.
func TestResumenQuedaGuardado(t *testing.T) {
	x := nuevaBandejaIA(t)
	x.activar(nil)
	num := numeroCliente()
	x.texto(num, "wamid.r1", "Hola, no tengo internet desde ayer")
	conv := cid(x.conv(num))
	ruta := "/conversaciones/" + conv + "/resumen"
	// Sin resumen todavía.
	x.exigir(x.api(x.agente, http.MethodGet, ruta, nil), http.StatusNotFound, "type", "urn:iris:problema:sin_resumen")
	if c := x.conv(num); c["resumen"] != nil {
		t.Fatalf("sin resumen: %v", c["resumen"])
	}
	// Se genera y queda guardado: quién, cuándo, modelo y hasta qué mensaje cubre.
	const secreto = "El cliente reporta que no tiene internet desde ayer."
	x.ia.decir("- " + secreto + "\n- Falta revisar el equipo.")
	r := x.api(x.agente, http.MethodPost, ruta, nil)
	x.exigir(r, http.StatusOK, "modelo", "llama3.1:8b", "proveedor", "openai_compatible", "desactualizado", "false", "mensajesNuevos", "0")
	por, _ := r.cuerpo["pedidoPor"].(map[string]any)
	ms := x.mensajes(x.supervisor, conv)
	var ultimo any
	for _, m := range ms {
		if m["direccion"] == "entrante" {
			ultimo = m["id"]
		}
	}
	if r.cuerpo["id"] == nil || por == nil || int64(por["id"].(float64)) != x.agente.id || r.cuerpo["hastaMensajeId"] != ultimo || r.cuerpo["generadoEn"] == nil {
		t.Fatalf("resumen guardado: %v", r.cuerpo)
	}
	// Se ve sin volver a llamar al modelo (otra persona, más tarde).
	antes := x.ia.pedidos()
	*x.reloj = x.reloj.Add(time.Hour)
	g := x.api(x.supervisor, http.MethodGet, ruta, nil)
	x.exigir(g, http.StatusOK, "id", r.cuerpo["id"].(string), "texto", r.cuerpo["texto"].(string), "desactualizado", "false")
	if x.ia.pedidos() != antes {
		t.Fatal("ver el resumen guardado llamó al modelo")
	}
	d, _ := x.conv(num)["resumen"].(map[string]any)
	if d == nil || d["id"] != r.cuerpo["id"] || d["desactualizado"] != false || d["texto"] != nil {
		t.Fatalf("el detalle dice que hay resumen (sin el texto): %v", d)
	}
	// Una nota interna no lo desactualiza; un mensaje del cliente o una respuesta, sí.
	x.exigir(x.api(x.agente, http.MethodPost, "/conversaciones/"+conv+"/notas", map[string]any{"id": nuevoID(), "texto": "Reviso el nodo."}), http.StatusCreated)
	x.exigir(x.api(x.agente, http.MethodGet, ruta, nil), http.StatusOK, "desactualizado", "false")
	x.texto(num, "wamid.r2", "Ya reinicié el router y sigue igual")
	x.exigir(x.api(x.agente, http.MethodPost, "/conversaciones/"+conv+"/mensajes", map[string]any{"id": nuevoID(), "texto": "Ya lo revisamos."}), http.StatusCreated)
	x.exigir(x.api(x.agente, http.MethodGet, ruta, nil), http.StatusOK, "desactualizado", "true", "mensajesNuevos", "2", "id", r.cuerpo["id"].(string))
	if d, _ = x.conv(num)["resumen"].(map[string]any); d == nil || d["desactualizado"] != true {
		t.Fatalf("el detalle lo marca desactualizado: %v", d)
	}
	// Si regenerarlo falla, el anterior se conserva.
	*x.reloj = x.reloj.Add(7 * time.Second)
	x.ia.mu.Lock()
	x.ia.status = http.StatusInternalServerError
	x.ia.mu.Unlock()
	x.exigir(x.api(x.supervisor, http.MethodPost, ruta, nil), http.StatusBadGateway)
	x.ia.mu.Lock()
	x.ia.status = http.StatusOK
	x.ia.mu.Unlock()
	x.exigir(x.api(x.agente, http.MethodGet, ruta, nil), http.StatusOK, "id", r.cuerpo["id"].(string))
	// Regenerar a pedido: reemplaza al anterior y vuelve a estar al día.
	*x.reloj = x.reloj.Add(7 * time.Second)
	x.ia.decir("- Sigue sin internet después de reiniciar.")
	r2 := x.api(x.supervisor, http.MethodPost, ruta, nil)
	x.exigir(r2, http.StatusOK, "desactualizado", "false", "texto", "- Sigue sin internet después de reiniciar.")
	por, _ = r2.cuerpo["pedidoPor"].(map[string]any)
	if r2.cuerpo["id"] == r.cuerpo["id"] || por == nil || int64(por["id"].(float64)) != x.supervisor.id {
		t.Fatalf("regenerado: %v", r2.cuerpo)
	}
	x.exigir(x.api(x.agente, http.MethodGet, ruta, nil), http.StatusOK, "id", r2.cuerpo["id"].(string))
	if n := x.contar(`SELECT count(*) FILTER (WHERE vigente) * 10 + count(*) FROM mensajeria.conversacion_resumenes WHERE conversacion_id = $1`, conv); n != 12 {
		t.Fatalf("un vigente de dos guardados: %d", n)
	}
	// Retención: el vigente y, como mucho, los 5 anteriores.
	for i := range 7 {
		*x.reloj = x.reloj.Add(7 * time.Second)
		x.ia.decir("- Resumen de relleno.")
		x.exigir(x.api(x.supervisor, http.MethodPost, ruta, nil), http.StatusOK)
		_ = i
	}
	if n := x.contar(`SELECT count(*) FILTER (WHERE vigente) * 100 + count(*) FROM mensajeria.conversacion_resumenes WHERE conversacion_id = $1`, conv); n != 106 {
		t.Fatalf("retención de resúmenes (1 vigente de 6): %d", n)
	}
	// El texto vive solo en la tabla de resúmenes: nunca en eventos, auditoría ni idempotencia.
	for _, donde := range []string{
		`SELECT count(*) FROM integracion.outbox WHERE payload::text LIKE '%' || $1 || '%'`,
		`SELECT count(*) FROM auditoria.acciones WHERE entrada::text || coalesce(error::text, '') LIKE '%' || $1 || '%'`,
		`SELECT count(*) FROM nucleo.idempotencia WHERE respuesta::text LIKE '%' || $1 || '%'`,
	} {
		if n := x.contar(donde, "no tiene internet desde ayer."); n != 0 {
			t.Fatalf("el texto del resumen salió de su tabla: %s", donde)
		}
	}
	if n := x.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'conversacion.resumen_guardado' AND payload->>'conversacion_id' = $1
		AND payload->>'modelo' = 'llama3.1:8b' AND payload ? 'resumen_id' AND payload ? 'hasta_mensaje_id' AND NOT payload ? 'texto'`, conv); n != 9 {
		t.Fatalf("conversacion.resumen_guardado: %d", n)
	}
	if n := x.contar(`SELECT count(*) FROM auditoria.acciones WHERE accion = 'conversaciones.resumen' AND entidad = 'conversacion' AND entidad_id = $1`, conv); n < 2 {
		t.Fatalf("auditoría del resumen: %d", n)
	}
	// Permiso y aislamiento: sin el permiso, 403; otra empresa o quien no ve la conversación, 404.
	ajeno := x.persona([]int64{2}, rolBase("supervisor"))
	x.exigir(x.api(x.lectura, http.MethodGet, ruta, nil), http.StatusForbidden)
	x.exigir(x.api(ajeno, http.MethodGet, ruta, nil), http.StatusNotFound)
	x.exigir(x.api(x.otro, http.MethodGet, ruta, nil), http.StatusNotFound)
}

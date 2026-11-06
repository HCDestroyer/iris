package mensajeria_test

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
)

// Revisión de seguridad de la bandeja 1.1 (PR #133): iniciar sobre una conversación de otra cola,
// asociación, destinatarios entre empresas, BAJA con el menú activo, auditoría del bot y de la
// disponibilidad, y barridos en paralelo.

func dispID(b *bandeja, p persona) int64 {
	b.t.Helper()
	var id int64
	if err := b.bd.QueryRow(b.ctx, `SELECT id FROM mensajeria.disponibilidad WHERE usuario_id = $1 AND empresa_id = 1`, p.id).Scan(&id); err != nil {
		b.t.Fatal(err)
	}
	return id
}

// rolPropio crea un rol con solo esos permisos.
func rolPropio(b *bandeja, permisos ...string) identidad.AsignacionRol {
	b.t.Helper()
	codigo := "prueba_" + strings.ToLower(pruebas.Unico())
	var id int64
	if err := b.bd.QueryRow(b.ctx, `INSERT INTO nucleo.roles (codigo, nombre) VALUES ($1, $1) RETURNING id`, codigo).Scan(&id); err != nil {
		b.t.Fatal(err)
	}
	for _, p := range permisos {
		if _, err := b.bd.Exec(b.ctx, `INSERT INTO nucleo.roles_permisos (rol_id, permiso) VALUES ($1, $2)`, id, p); err != nil {
			b.t.Fatal(err)
		}
	}
	return identidad.AsignacionRol{Rol: codigo}
}

func TestConvIniciarConversacionDeOtraCola(t *testing.T) {
	b := nuevaBandeja(t, nil)
	ventas := b.otraCola()
	b.botActivo(map[string]any{"activo": true, "atiende": "bot", "minutosSinConfirmacion": 30, "minutosSinDisponibles": 3, "saludo": "Hola",
		"menu": []any{map[string]any{"texto": "Ventas", "colaId": ventas, "etiquetaIds": []any{}}}})
	movil := fmt.Sprintf("5%07d", siguienteCliente.Add(1)%10_000_000)
	cliente := b.cliente(1, movil)
	if _, err := b.bd.Exec(b.ctx, `INSERT INTO integracion.argos_clientes (id, empresa_id, nombre, movil, codigo, sincronizado_en, empresas)
		VALUES ($1, 1, 'Cliente M1', $2, 'C-M1', now(), '{1}')`, cliente, movil); err != nil {
		t.Fatal(err)
	}
	b.exigir(b.consentir(cliente, "acepta", b.reloj.Add(-time.Hour)), http.StatusCreated)
	num := "502" + movil
	// Nadie disponible: saludo; a los 4 min, menú; el cliente elige Ventas.
	b.texto(num, "wamid.o1", "Hola")
	*b.reloj = b.reloj.Add(4 * time.Minute)
	b.disparar()
	b.texto(num, "wamid.o2", "1")
	c := b.conv(num)
	if c["cola"].(map[string]any)["id"].(float64) != float64(ventas) {
		t.Fatalf("no pasó a Ventas: %v", c["cola"])
	}
	pl := map[string]any{"codigo": "bienvenida", "variables": map[string]string{"plan": "Fibra"}}
	cuerpo := func() map[string]any {
		return map[string]any{"id": nuevoID(), "canalId": b.canal.ID, "clienteId": cliente, "plantilla": pl}
	}
	// El agente es de la cola del número (Soporte), no de Ventas: 409 conversacion_de_otra_cola.
	b.exigir(b.api(b.agente, http.MethodPost, "/conversaciones/iniciar", cuerpo()), http.StatusConflict, "type", "urn:iris:problema:conversacion_de_otra_cola")
	if n := b.contar(`SELECT count(*) FROM mensajeria.conversaciones WHERE id = $1 AND asignado_a IS NOT NULL`, cid(c)); n != 0 {
		t.Fatal("la tomó")
	}
	// Destinatario: el mismo motivo y sin el id de una conversación que no ve.
	d := b.api(b.agente, http.MethodGet, fmt.Sprintf("/conversaciones/destinatario?canalId=%d&clienteId=%d", b.canal.ID, cliente), nil)
	b.exigir(d, http.StatusOK, "motivo", "conversacion_de_otra_cola", "conversacionId", "<nil>")
	// Resuelta en otra cola: tampoco la reabre.
	if _, err := b.bd.Exec(b.ctx, `UPDATE mensajeria.conversaciones SET estado = 'resuelta' WHERE id = $1`, cid(c)); err != nil {
		t.Fatal(err)
	}
	b.exigir(b.api(b.agente, http.MethodPost, "/conversaciones/iniciar", cuerpo()), http.StatusConflict, "type", "urn:iris:problema:conversacion_de_otra_cola")
	// El supervisor global sí.
	b.exigir(b.api(b.supervisor, http.MethodPost, "/conversaciones/iniciar", cuerpo()), http.StatusCreated, "nueva", "false")
	// Las respuestas del bot quedaron auditadas como actor sistema: saludo, menú (temporizador) y elegido.
	if n := b.contar(`SELECT count(*) FROM auditoria.acciones WHERE actor_tipo = 'sistema' AND entidad = 'conversacion' AND entidad_id = $1
		AND resultado = 'ok' AND accion IN ('mensajeria.bot_respondio', 'mensajeria.bot_conversacion')`, cid(c)); n != 3 {
		t.Fatalf("filas de auditoría del bot: %d", n)
	}
}

func TestConvIniciarNoReescribeAsociacion(t *testing.T) {
	b := nuevaBandeja(t, nil)
	movil := fmt.Sprintf("5%07d", siguienteCliente.Add(1)%10_000_000)
	cliente := b.cliente(1, movil)
	otro := b.cliente(1, "5512-0999")
	b.exigir(b.consentir(cliente, "acepta", b.reloj.Add(-time.Hour)), http.StatusCreated)
	num := "502" + movil
	b.texto(num, "wamid.r1", "Hola")
	c := b.conv(num)
	// La conversación del número está asociada a otro cliente.
	if _, err := b.bd.Exec(b.ctx, `UPDATE mensajeria.conversaciones SET cliente_id = $2, asociacion = 'manual' WHERE id = $1`, cid(c), otro); err != nil {
		t.Fatal(err)
	}
	pl := map[string]any{"codigo": "bienvenida", "variables": map[string]string{"plan": "Fibra"}}
	// Sin conversaciones.asociar no la cambia.
	sin := b.persona([]int64{1}, rolPropio(b, "conversaciones.ver", "conversaciones.iniciar"))
	b.miembro(sin)
	b.exigir(b.api(sin, http.MethodPost, "/conversaciones/iniciar", map[string]any{"id": nuevoID(), "canalId": b.canal.ID, "clienteId": cliente, "plantilla": pl}),
		http.StatusForbidden)
	if k := b.conv(num)["contacto"].(map[string]any); k["clienteId"].(float64) != float64(otro) {
		t.Fatalf("cambió la asociación: %v", k)
	}
	// El agente (soporte, con asociar) sí.
	b.exigir(b.api(b.agente, http.MethodPost, "/conversaciones/iniciar", map[string]any{"id": nuevoID(), "canalId": b.canal.ID, "clienteId": cliente, "plantilla": pl}),
		http.StatusCreated)
	if k := b.conv(num)["contacto"].(map[string]any); k["clienteId"].(float64) != float64(cliente) {
		t.Fatalf("asociación: %v", k)
	}
}

func TestConvDestinatariosEntreEmpresas(t *testing.T) {
	b := nuevaBandeja(t, nil)
	nombre := "Ximena " + pruebas.Unico()
	id := siguienteCliente.Add(1)
	if _, err := b.bd.Exec(b.ctx, `INSERT INTO integracion.argos_clientes (id, empresa_id, nombre, movil, codigo, sincronizado_en, empresas)
		VALUES ($1, 1, $2, '5512-8001', 'C-X', now(), '{1}')`, id, nombre); err != nil {
		t.Fatal(err)
	}
	ajeno := b.persona([]int64{2}, rolBase("supervisor"))
	r := b.api(ajeno, http.MethodGet, "/conversaciones/destinatarios?q="+strings.ReplaceAll(nombre, " ", "%20"), nil)
	b.exigir(r, http.StatusOK)
	if d := r.cuerpo["data"].([]any); len(d) != 0 {
		t.Fatalf("ve clientes de otra empresa: %v", d)
	}
	// El número de la empresa 1 desde la empresa 2: el mismo 422 que uno que no existe.
	otro := b.api(ajeno, http.MethodGet, fmt.Sprintf("/conversaciones/destinatarios?q=Xim&canalId=%d", b.canal.ID), nil)
	nada := b.api(ajeno, http.MethodGet, "/conversaciones/destinatarios?q=Xim&canalId=999999999", nil)
	b.exigir(otro, http.StatusUnprocessableEntity)
	b.exigir(nada, http.StatusUnprocessableEntity)
	if fmt.Sprint(otro.cuerpo) != fmt.Sprint(nada.cuerpo) {
		t.Fatalf("respuestas distintas: %v %v", otro.cuerpo, nada.cuerpo)
	}
	b.exigir(b.api(ajeno, http.MethodGet, fmt.Sprintf("/conversaciones/destinatario?clienteId=%d", id), nil), http.StatusNotFound)
	b.exigir(b.api(ajeno, http.MethodPost, "/conversaciones/iniciar", map[string]any{"id": nuevoID(), "canalId": b.canal.ID, "clienteId": id,
		"plantilla": map[string]any{"codigo": "bienvenida", "variables": map[string]string{"plan": "x"}}}), http.StatusUnprocessableEntity,
		"type", "urn:iris:problema:numero_no_permitido")
}

func TestBotNoContestaBajaConMenu(t *testing.T) {
	b := nuevaBandeja(t, nil)
	b.botActivo(map[string]any{"activo": true, "atiende": "bot", "minutosSinConfirmacion": 30, "minutosSinDisponibles": 3, "saludo": "Hola",
		"menu": []any{map[string]any{"texto": "Soporte", "colaId": b.cola, "etiquetaIds": []any{}}}})
	num := numeroCliente()
	b.texto(num, "wamid.m1", "Hola")
	*b.reloj = b.reloj.Add(4 * time.Minute)
	b.disparar()
	c := b.conv(num)
	antes := len(automaticos(b.mensajes(b.supervisor, cid(c)), "bot"))
	if n := b.contar(`SELECT count(*) FROM mensajeria.conversaciones WHERE id = $1 AND bot_menu = 'esperando'`, cid(c)); n != 1 {
		t.Fatal("el menú no quedó esperando")
	}
	// Con el menú esperando, el cliente escribe BAJA: ni menú repetido ni nada más.
	b.texto(num, "wamid.m2", "BAJA")
	b.texto(num, "wamid.m3", "1")
	if n := len(automaticos(b.mensajes(b.supervisor, cid(c)), "bot")); n != antes {
		t.Fatalf("el bot contestó después de BAJA: %d → %d", antes, n)
	}
	if n := b.contar(`SELECT count(*) FROM mensajeria.conversaciones WHERE id = $1 AND bot_menu IS NOT NULL`, cid(c)); n != 0 {
		t.Fatal("el menú sigue esperando después de BAJA")
	}
}

func TestDisponibilidadDosBarridosALaVez(t *testing.T) {
	b := nuevaBandeja(t, nil)
	b.disponible(b.agente, "disponible")
	*b.reloj = b.reloj.Add(20 * time.Minute)
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			b.m.VencerDisponibilidad(b.ctx, b.bd)
		}()
	}
	wg.Wait()
	if n := b.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'agente.disponibilidad_cambiada' AND payload->>'usuario_id' = $1
		AND payload->>'origen' = 'inactividad'`, fmt.Sprint(b.agente.id)); n != 1 {
		t.Fatalf("transiciones con barridos en paralelo: %d", n)
	}
	if n := b.contar(`SELECT count(*) FROM auditoria.acciones WHERE accion = 'mensajeria.vencer_disponibilidad' AND resultado = 'ok' AND actor_tipo = 'sistema'
		AND entidad_id = $1`, fmt.Sprint(dispID(b, b.agente))); n != 1 {
		t.Fatalf("filas de auditoría: %d", n)
	}
}

package soporte_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
	"github.com/HCDestroyer/iris/apps/api/internal/soporte"
)

// Portal del cliente por HTTP con el middleware real: sesión de portal (actor cliente),
// aislamiento entre clientes y empresas, sin notas internas, sin acceso al panel, y las
// acciones del cliente (crear, responder, confirmar la solución).

// entrarPortal emite un acceso al portal (como la consola, actor sistema) y lo canjea con un
// navegador nuevo.
func entrarPortal(t *testing.T, base string, cliente, empresa int64) *clienteHTTP {
	t.Helper()
	a, err := ident.EmitirAccesoPortal.Ejecutar(pruebas.ContextoSistema(pruebas.Pool(t)),
		identidad.EntradaEmitirAccesoPortal{ClienteID: cliente, EmpresaID: empresa, Nombre: fmt.Sprintf("Cliente %d", cliente)})
	if err != nil {
		t.Fatal(err)
	}
	_, ticket, _ := strings.Cut(a.URL, "#")
	jar, _ := cookiejar.New(nil)
	c := &clienteHTTP{t: t, base: base, http: &http.Client{Jar: jar}}
	r := c.pedir(http.MethodPost, "/api/v1/portal/sesion", map[string]any{"ticket": ticket})
	if r.status != http.StatusOK {
		t.Fatalf("entrar al portal: %d %s", r.status, r.crudo)
	}
	c.csrf, _ = r.cuerpo["csrfToken"].(string)
	return c
}

func idsData(t *testing.T, r respuesta) []int64 {
	t.Helper()
	if r.status != http.StatusOK {
		t.Fatalf("lista: %d %s", r.status, r.crudo)
	}
	var ids []int64
	for _, x := range r.cuerpo["data"].([]any) {
		ids = append(ids, int64(x.(map[string]any)["id"].(float64)))
	}
	return ids
}

// limpiarPortal borra los accesos y sesiones del portal antes de que registrarEmpresas borre
// las empresas (t.Cleanup corre en orden inverso).
func limpiarPortal(t *testing.T) {
	t.Cleanup(func() {
		for _, sql := range []string{"DELETE FROM nucleo.portal_sesiones", "DELETE FROM nucleo.portal_accesos"} {
			if _, err := pruebas.Pool(t).Exec(context.Background(), sql); err != nil {
				t.Error(err)
			}
		}
	})
}

func TestHTTPPortalDelCliente(t *testing.T) {
	registrarEmpresas(t, 71, 72)
	limpiarPortal(t)
	instantanea(t, 71, 7101, 710101, 7100)
	instantanea(t, 71, 7102, 710201, 7100)
	base := nuevoServidor(t)
	ana := entrarPortal(t, base, 7101, 71)
	vecino := entrarPortal(t, base, 7102, 71)      // otro cliente, misma empresa
	otraEmpresa := entrarPortal(t, base, 7199, 72) // el mismo id en otra empresa: TestPortalCasosSoloParaElCliente
	agente := entrar(t, base, []int64{71}, "soporte")

	// Categorías públicas: sin incidente_red.
	r := ana.pedir(http.MethodGet, "/api/v1/portal/categorias", nil)
	var lentitud, incidente float64
	for _, x := range r.cuerpo["data"].([]any) {
		c := x.(map[string]any)
		switch c["codigo"] {
		case "lentitud":
			lentitud = c["id"].(float64)
		case "incidente_red":
			incidente = c["id"].(float64)
		}
	}
	if r.status != 200 || lentitud == 0 || incidente != 0 {
		t.Fatalf("categorías del portal: %d %s", r.status, r.crudo)
	}

	// Crear: cliente y empresa de la sesión, contacto de la ficha, idempotente por clave.
	nuevo := map[string]any{"asunto": "Internet lento", "descripcion": "Desde ayer va lento", "categoriaId": lentitud, "servicioId": 710101}
	r = ana.pedir(http.MethodPost, "/api/v1/portal/tickets", nuevo, "Idempotency-Key", "portal-crear-1")
	if r.status != http.StatusCreated || r.cuerpo["origen"] != "portal" || r.cuerpo["estado"] != "nuevo" || r.cuerpo["categoria"] != "Lentitud" {
		t.Fatalf("crear: %d %s", r.status, r.crudo)
	}
	id := int64(r.cuerpo["id"].(float64))
	if rr := ana.pedir(http.MethodPost, "/api/v1/portal/tickets", nuevo, "Idempotency-Key", "portal-crear-1"); rr.status != http.StatusCreated ||
		int64(rr.cuerpo["id"].(float64)) != id {
		t.Fatalf("repetido: %d %s", rr.status, rr.crudo)
	}
	// La misma clave de otro cliente no choca (las claves son por cliente).
	if rr := vecino.pedir(http.MethodPost, "/api/v1/portal/tickets", map[string]any{"asunto": "Sin señal", "descripcion": "Nada"},
		"Idempotency-Key", "portal-crear-1"); rr.status != http.StatusCreated || int64(rr.cuerpo["id"].(float64)) == id {
		t.Fatalf("clave de otro cliente: %d %s", rr.status, rr.crudo)
	}
	ruta := fmt.Sprintf("/api/v1/portal/tickets/%d", id)
	panel := fmt.Sprintf("/api/v1/tickets/%d", id)
	v := agente.pedir(http.MethodGet, panel, nil)
	contacto, _ := v.cuerpo["contacto"].(map[string]any)
	if v.status != 200 || v.cuerpo["clienteId"] != float64(7101) || v.cuerpo["empresaId"] != float64(71) ||
		v.cuerpo["servicioId"] != float64(710101) || v.cuerpo["zonaId"] != float64(718) || contacto["nombre"] != "Cliente 7101" {
		t.Fatalf("el ticket en el panel: %d %s", v.status, v.crudo)
	}
	ana.pedir(http.MethodPost, "/api/v1/portal/tickets", map[string]any{"asunto": "Caída", "descripcion": "x", "categoriaId": incidenteRed(t)}).
		problema(t, 422, "validacion")
	ana.pedir(http.MethodPost, "/api/v1/portal/tickets", map[string]any{"asunto": "Ajeno", "descripcion": "x", "servicioId": 710201}).
		problema(t, 422, "validacion")
	// El cuerpo no puede fijar cliente ni empresa (campos desconocidos: 422).
	if rr := ana.pedir(http.MethodPost, "/api/v1/portal/tickets", map[string]any{"asunto": "Colado", "descripcion": "x", "clienteId": 7102}); rr.status != 422 {
		t.Fatalf("clienteId en el cuerpo: %d %s", rr.status, rr.crudo)
	}

	// El agente responde y deja una nota interna: el cliente ve la respuesta, nunca la nota.
	if rr := agente.pedir(http.MethodPost, panel+"/responder", map[string]any{"cuerpo": "Vamos a revisar tu módem"}); rr.status != 200 {
		t.Fatalf("responder: %d %s", rr.status, rr.crudo)
	}
	agente.pedir(http.MethodPost, panel+"/nota-interna", map[string]any{"cuerpo": "SECRETO: cliente moroso"})
	m := ana.pedir(http.MethodGet, ruta+"/mensajes", nil)
	if m.status != 200 || strings.Contains(string(m.crudo), "SECRETO") || len(m.cuerpo["data"].([]any)) != 2 {
		t.Fatalf("mensajes del cliente: %d %s", m.status, m.crudo)
	}
	primero := m.cuerpo["data"].([]any)[0].(map[string]any)
	segundo := m.cuerpo["data"].([]any)[1].(map[string]any)
	if primero["autorTipo"] != "cliente" || segundo["autorTipo"] != "agente" || segundo["autorId"] != nil {
		t.Fatalf("autores: %v / %v", primero, segundo)
	}
	if d := ana.pedir(http.MethodGet, ruta, nil); d.status != 200 || d.cuerpo["asignadoA"] != nil || d.cuerpo["slaRespuesta"] != nil ||
		d.cuerpo["estado"] != "abierto" {
		t.Fatalf("detalle del cliente: %d %s", d.status, d.crudo)
	}

	// Aislamiento: otro cliente de la misma empresa y uno de otra empresa no lo ven ni lo tocan (el mismo 404 que si no existiera).
	for _, c := range []*clienteHTTP{vecino, otraEmpresa} {
		c.pedir(http.MethodGet, ruta, nil).problema(t, 404, "no_encontrado")
		c.pedir(http.MethodGet, ruta+"/mensajes", nil).problema(t, 404, "no_encontrado")
		c.pedir(http.MethodPost, ruta+"/responder", map[string]any{"cuerpo": "hola"}).problema(t, 404, "no_encontrado")
		c.pedir(http.MethodPost, ruta+"/confirmar-solucion", map[string]any{"version": 1}).problema(t, 404, "no_encontrado")
		if ids := idsData(t, c.pedir(http.MethodGet, "/api/v1/portal/tickets?limit=200", nil)); slices.Contains(ids, id) {
			t.Fatalf("la lista de otro trae el ticket: %v", ids)
		}
	}
	if ids := idsData(t, ana.pedir(http.MethodGet, "/api/v1/portal/tickets?limit=200", nil)); !slices.Contains(ids, id) {
		t.Fatalf("lista de Ana: %v", ids)
	}
	// Un ticket de su cliente creado en el panel sin cliente, o de la empresa, no aparece.
	sinCliente := agente.pedir(http.MethodPost, "/api/v1/tickets", map[string]any{"asunto": "Interno del NOC"})
	ana.pedir(http.MethodGet, fmt.Sprintf("/api/v1/portal/tickets/%d", int64(sinCliente.cuerpo["id"].(float64))), nil).problema(t, 404, "no_encontrado")

	// El portal no llega al panel (la cookie del portal no autentica fuera de /portal) y la sesión
	// del panel no sirve en el portal.
	ana.pedir(http.MethodGet, panel, nil).problema(t, 401, "no_autenticado")
	ana.pedir(http.MethodGet, "/api/v1/tickets", nil).problema(t, 401, "no_autenticado")
	ana.pedir(http.MethodPost, panel+"/resolver", map[string]any{"version": 1, "solucion": "Hecho"}).problema(t, 401, "no_autenticado")
	agente.pedir(http.MethodGet, "/api/v1/portal/tickets", nil).problema(t, 401, "no_autenticado")

	// Responder en espera: reanuda.
	v = agente.pedir(http.MethodGet, panel, nil)
	agente.pedir(http.MethodPost, panel+"/poner-en-espera", map[string]any{"version": v.cuerpo["version"], "motivo": "Esperando al cliente"})
	r = ana.pedir(http.MethodPost, ruta+"/responder", map[string]any{"cuerpo": "Ya reinicié el módem"}, "Idempotency-Key", "resp-1")
	if r.status != 200 || r.cuerpo["ticket"].(map[string]any)["estado"] != "abierto" || r.cuerpo["mensaje"].(map[string]any)["autorTipo"] != "cliente" {
		t.Fatalf("responder en espera: %d %s", r.status, r.crudo)
	}
	if es := eventosDe(t, id); contar(es, "ticket.mensaje_cliente") != 1 {
		t.Fatalf("eventos: %v", nombres(es))
	}
	// Versión vieja: 409.
	ana.pedir(http.MethodPost, ruta+"/responder", map[string]any{"cuerpo": "Otra vez", "version": 1}).problema(t, 409, "conflicto_version")

	// Confirmar: solo resuelto; con versión vieja, 409; luego cerrado y ya no se responde.
	tk := ana.pedir(http.MethodGet, ruta, nil)
	ana.pedir(http.MethodPost, ruta+"/confirmar-solucion", map[string]any{"version": tk.cuerpo["version"]}).problema(t, 422, "transicion_invalida")
	v = agente.pedir(http.MethodGet, panel, nil)
	agente.pedir(http.MethodPost, panel+"/resolver", map[string]any{"version": v.cuerpo["version"], "solucion": "Cambio de módem"})
	tk = ana.pedir(http.MethodGet, ruta, nil)
	if tk.cuerpo["estado"] != "resuelto" || tk.cuerpo["puedeConfirmar"] != true {
		t.Fatalf("resuelto: %s", tk.crudo)
	}
	ana.pedir(http.MethodPost, ruta+"/confirmar-solucion", map[string]any{"version": 1}).problema(t, 409, "conflicto_version")
	r = ana.pedir(http.MethodPost, ruta+"/confirmar-solucion", map[string]any{"version": tk.cuerpo["version"]})
	if r.status != 200 || r.cuerpo["estado"] != "cerrado" {
		t.Fatalf("confirmar: %d %s", r.status, r.crudo)
	}
	ana.pedir(http.MethodPost, ruta+"/responder", map[string]any{"cuerpo": "Gracias"}).problema(t, 422, "transicion_invalida")

	// Responder a un resuelto lo reabre (regla del correo).
	r = ana.pedir(http.MethodPost, "/api/v1/portal/tickets", map[string]any{"asunto": "Otra falla", "descripcion": "Se corta"})
	id2 := int64(r.cuerpo["id"].(float64))
	v = agente.pedir(http.MethodGet, fmt.Sprintf("/api/v1/tickets/%d", id2), nil)
	agente.pedir(http.MethodPost, fmt.Sprintf("/api/v1/tickets/%d/resolver", id2), map[string]any{"version": v.cuerpo["version"], "solucion": "Ajuste de señal"})
	r = ana.pedir(http.MethodPost, fmt.Sprintf("/api/v1/portal/tickets/%d/responder", id2), map[string]any{"cuerpo": "Sigue fallando"})
	if r.status != 200 || r.cuerpo["ticket"].(map[string]any)["estado"] != "abierto" {
		t.Fatalf("reabrir por el portal: %d %s", r.status, r.crudo)
	}

	// Filtro por estado.
	if ids := idsData(t, ana.pedir(http.MethodGet, "/api/v1/portal/tickets?estado=cerrado", nil)); !slices.Contains(ids, id) || slices.Contains(ids, id2) {
		t.Fatalf("filtro por estado: %v", ids)
	}
}

func incidenteRed(t *testing.T) int64 { return *categoria(t, "incidente_red") }

func TestPortalCasosSoloParaElCliente(t *testing.T) {
	registrarEmpresas(t, 73, 74)
	m, _ := nuevoModulo(time.Now())
	cli := pruebas.ContextoCliente(pruebas.Pool(t), 7301, 73)

	// El cliente no usa casos del panel (ConPermiso): 403.
	_, err := m.ListarTickets.Ejecutar(cli, soporte.EntradaListarTickets{})
	problema(t, err, 403, "prohibido")
	_, err = m.CrearTicket.Ejecutar(cli, soporte.EntradaCrearTicket{Asunto: "Hola"})
	problema(t, err, 403, "prohibido")
	// Y una persona no usa los del portal.
	_, err = m.ListarTicketsPortal.Ejecutar(comoEnEmpresa(t, agente(t), 73), soporte.EntradaListarTicketsPortal{})
	problema(t, err, 403, "prohibido")

	// Crear por el caso: sin instantánea, contacto = nombre de la sesión; auditado como cliente.
	tk, err := m.CrearTicketPortal.Ejecutar(cli, soporte.EntradaCrearTicketPortal{Asunto: "Sin señal", Descripcion: "Nada funciona"})
	if err != nil || tk.Origen != "portal" || tk.AfectadoPorIncidente {
		t.Fatalf("crear por el portal: %+v %v", tk, err)
	}
	var actor string
	var actorID, empresa int64
	if err := pruebas.Pool(t).QueryRow(context.Background(),
		"SELECT actor_tipo, actor_id, empresa_id FROM auditoria.acciones WHERE accion = 'portal.crear_ticket' AND entidad_id = $1", fmt.Sprint(tk.ID)).
		Scan(&actor, &actorID, &empresa); err != nil || actor != "cliente" || actorID != 7301 || empresa != 73 {
		t.Fatalf("auditoría: %s %d %d %v", actor, actorID, empresa, err)
	}
	// El mismo id de cliente con otra empresa (otra sesión) no lo ve.
	otra := pruebas.ContextoCliente(pruebas.Pool(t), 7301, 74)
	_, err = m.VerTicketPortal.Ejecutar(otra, soporte.EntradaVerTicketPortal{ID: tk.ID})
	problema(t, err, 404, "no_encontrado")
	if l, err := m.ListarTicketsPortal.Ejecutar(otra, soporte.EntradaListarTicketsPortal{}); err != nil || len(l.Data) != 0 {
		t.Fatalf("lista de la otra empresa: %+v %v", l, err)
	}
	if l, err := m.ListarTicketsPortal.Ejecutar(cli, soporte.EntradaListarTicketsPortal{}); err != nil || len(l.Data) != 1 || l.Data[0].ID != tk.ID {
		t.Fatalf("lista propia: %+v %v", l, err)
	}
}

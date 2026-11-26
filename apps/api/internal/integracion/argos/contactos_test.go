package argos_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
)

// Contactos del cliente (contrato 0.10.0 §3.5, A42; 0.10.1 §3.8, A46; pedidos B22 y B25), contra un
// Argos HTTP falso: la caché de Iris se llena con GET /clientes/:id/contactos y se refresca con el
// webhook cliente.contactos_cambiados, que Iris relee sola con X-Iris-Actor «sistema» (0.14.2).

type contactosArgos struct {
	mu sync.Mutex
	// lista: lo que responde GET /clientes/:id/contactos; buscar: GET /contactos/buscar.
	lista  []map[string]any
	buscar []map[string]any
	// pedidos: «ruta actor=… conversacion=… contacto=…» de cada petición.
	pedidos []string
	// rechazarSistema: responde 400 a X-Iris-Actor «sistema» (un Argos anterior a 0.14.2).
	rechazarSistema bool
	// noExiste: GET /clientes/:id/contactos responde 404.
	noExiste bool
	// durante: se ejecuta una vez en medio de la próxima lectura, antes de responder (carreras).
	durante func()
}

func nuevosContactosArgos(e *entorno) *contactosArgos {
	c := &contactosArgos{}
	e.argos.con(func() {
		e.argos.rutaContactos = func(w http.ResponseWriter, r *http.Request) {
			c.mu.Lock()
			durante := c.durante
			c.durante = nil
			c.mu.Unlock()
			if durante != nil {
				durante()
			}
			c.mu.Lock()
			defer c.mu.Unlock()
			actor := r.Header.Get("X-Iris-Actor")
			c.pedidos = append(c.pedidos, r.URL.Path+" actor="+actor+" conversacion="+r.Header.Get("X-Iris-Conversacion")+" contacto="+r.Header.Get("X-Iris-Contacto"))
			if actor == "" || (actor == "sistema" && (c.rechazarSistema || r.URL.Path == "/api/v1/contactos/buscar")) {
				problema(w, http.StatusBadRequest, "urn:argosisp:problema:cabecera_iris_requerida", "X-Iris-Actor")
				return
			}
			if c.noExiste {
				problema(w, http.StatusNotFound, "urn:argosisp:problema:no_encontrado", "Cliente")
				return
			}
			if r.URL.Path == "/api/v1/contactos/buscar" {
				escribir(w, map[string]any{"data": c.buscar})
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(c.lista)
		}
	})
	return c
}

func (c *contactosArgos) ultimo() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.pedidos) == 0 {
		return ""
	}
	return c.pedidos[len(c.pedidos)-1]
}

func (c *contactosArgos) cuantos() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.pedidos)
}

func (e *entorno) conContrato(id int64, contrato string) {
	e.t.Helper()
	if _, err := e.pool.Exec(e.t.Context(), `UPDATE integracion.conectores SET contrato_remoto = $2 WHERE id = $1`, id, contrato); err != nil {
		e.t.Fatal(err)
	}
}

func TestContactosDelCliente(t *testing.T) {
	e := nuevoEntorno(t)
	_, conID, secreto := e.conectado()
	ca := nuevosContactosArgos(e)
	ctx := nucleo.ConBD(pruebas.ContextoSistema(e.pool), e.pool)
	cliente := int64(1201)
	e.sembrarCliente(cliente, "Ana Pérez")

	// Con un Argos anterior a 0.10.0 no hay contactos: ni buscar ni leer.
	e.conContrato(conID, "0.9.0")
	if l, soporta, err := e.mod.BuscarContactos(ctx, "usuario:7", "+50255123344"); err != nil || soporta || l != nil {
		t.Fatalf("Argos 0.9.0: %v %v %v", l, soporta, err)
	}
	if leido, err := e.mod.RefrescarContactos(ctx, argos.EntradaRefrescarContactos{ClienteID: cliente, Actor: "usuario:7"}); err != nil || leido {
		t.Fatalf("Argos 0.9.0 no lee contactos: %v %v", leido, err)
	}
	if ca.cuantos() != 0 {
		t.Fatal("con Argos 0.9.0 no se llama a las rutas de contactos")
	}

	e.conContrato(conID, "0.14.2")
	persona := int64(1300)
	ca.mu.Lock()
	ca.lista = []map[string]any{
		{"contacto_id": 77, "cliente_id": cliente, "empresa_id": 1, "nombre": "Luis Pérez", "relacion": "familiar", "es_titular": false,
			"servicios": []int64{4410}, "permisos": map[string]any{"consultar": true, "reportar": true, "pagar": false, "avisos": false},
			"telefonos":          []map[string]any{{"numero": "+50255123344", "whatsapp": true, "principal": true}, {"numero": "no-es-e164", "whatsapp": false, "principal": false}},
			"persona_cliente_id": persona, "version": 3},
		{"contacto_id": 78, "cliente_id": cliente, "nombre": "Ana Pérez", "relacion": "titular", "es_titular": true, "servicios": nil,
			"telefonos": []map[string]any{{"numero": "+50244556677", "whatsapp": true, "principal": true}}, "version": 1},
		// De otro cliente o con una relación desconocida: no se guardan.
		{"contacto_id": 79, "cliente_id": 999, "nombre": "Otro", "relacion": "familiar", "version": 1},
		{"contacto_id": 80, "cliente_id": cliente, "nombre": "Raro", "relacion": "jefe", "version": 1},
		// Sin versión: tampoco.
		{"contacto_id": 81, "cliente_id": cliente, "nombre": "Sin versión", "relacion": "familiar"},
	}
	ca.buscar = []map[string]any{{"cliente_id": cliente, "empresa_id": 1, "contacto_id": 77, "nombre": "Luis Pérez", "relacion": "familiar",
		"es_titular": false, "adicional": false, "servicios": []int64{4410}, "permisos": map[string]any{"consultar": true}, "a_cargo": true}}
	ca.mu.Unlock()

	// buscar exige una persona o el asistente: nunca «sistema».
	if _, _, err := e.mod.BuscarContactos(ctx, "", "+50255123344"); !errors.Is(err, argos.ErrCabecerasIris) {
		t.Fatalf("buscar sin actor: %v", err)
	}
	l, soporta, err := e.mod.BuscarContactos(ctx, "usuario:7", "+50255123344")
	if err != nil || !soporta || len(l) != 1 || !l[0].ACargo || l[0].ContactoID == nil || *l[0].ContactoID != 77 {
		t.Fatalf("buscar: %+v %v %v", l, soporta, err)
	}
	if p := ca.ultimo(); p != "/api/v1/contactos/buscar actor=usuario:7 conversacion= contacto=" {
		t.Fatalf("buscar va con el actor y sin conversación ni contacto: %s", p)
	}

	leido, err := e.mod.RefrescarContactos(ctx, argos.EntradaRefrescarContactos{ClienteID: cliente, Actor: "usuario:7"})
	if err != nil || !leido {
		t.Fatalf("refrescar: %v %v", leido, err)
	}
	if p := ca.ultimo(); p != "/api/v1/clientes/1201/contactos actor=usuario:7 conversacion= contacto=" {
		t.Fatalf("leer: %s", p)
	}
	if n := e.contar(`SELECT count(*) FROM integracion.argos_contactos WHERE cliente_id = 1201`); n != 2 {
		t.Fatalf("solo los contactos válidos de ese cliente: %d", n)
	}
	if n := e.contar(`SELECT count(*) FROM integracion.argos_contactos WHERE contacto_id = 77 AND NOT pagar AND consultar AND servicios = '{4410}'
		AND persona_cliente_id = 1300 AND version = 3 AND NOT es_titular`); n != 1 {
		t.Fatal("permisos, servicios y persona a cargo del contacto 77")
	}
	if n := e.contar(`SELECT count(*) FROM integracion.argos_contacto_telefonos WHERE contacto_id = 77`); n != 1 {
		t.Fatalf("solo números E.164: %d", n)
	}
	if n := e.contar(`SELECT count(*) FROM integracion.argos_contactos WHERE contacto_id = 78 AND es_titular AND servicios IS NULL
		AND NOT pagar AND NOT consultar AND NOT reportar AND NOT avisos AND NOT quitado`); n != 1 {
		t.Fatal("sin «permisos» de Argos, ninguno (lo seguro)")
	}
	if n := e.contar(`SELECT count(*) FROM auditoria.acciones WHERE accion = 'clientes.sincronizar_contactos' AND entidad_id = '1201'`); n != 1 {
		t.Fatalf("la lectura queda auditada: %d", n)
	}
	// Antes de 24 h no se vuelve a leer.
	antes := ca.cuantos()
	if leido, err := e.mod.RefrescarContactos(ctx, argos.EntradaRefrescarContactos{ClienteID: cliente, Actor: "usuario:7"}); err != nil || !leido || ca.cuantos() != antes {
		t.Fatalf("caché vigente: %v %v %d", leido, err, ca.cuantos()-antes)
	}

	// Webhook editado: Iris relee con X-Iris-Actor «sistema» (0.14.2), sin conversación ni contacto.
	ca.mu.Lock()
	ca.lista = ca.lista[:1]
	ca.lista[0]["permisos"] = map[string]any{"consultar": true, "reportar": true, "pagar": true, "avisos": true}
	ca.mu.Unlock()
	r := e.procesarWebhook(conID, secreto, argos.EventoContactosCambiados, map[string]any{"cliente_id": cliente, "empresa_id": 1, "contacto_id": 77,
		"accion": "editado", "campos": []string{"permisos"}, "persona_cliente_id": persona})
	if r.Resultado != "aplicado" {
		t.Fatalf("webhook: %+v", r)
	}
	if p := ca.ultimo(); p != "/api/v1/clientes/1201/contactos actor=sistema conversacion= contacto=" {
		t.Fatalf("relectura del sistema: %s", p)
	}
	if n := e.contar(`SELECT count(*) FROM integracion.argos_contactos WHERE cliente_id = 1201 AND NOT quitado`); n != 1 {
		t.Fatalf("un solo contacto vigente: %d", n)
	}
	if n := e.contar(`SELECT count(*) FROM integracion.argos_contactos WHERE contacto_id = 78 AND quitado AND leido_en > created_at`); n != 1 {
		t.Fatal("el contacto que Argos ya no devuelve queda quitado (no se borra), con leido_en")
	}
	if n := e.contar(`SELECT count(*) FROM integracion.argos_contactos WHERE contacto_id = 77 AND pagar`); n != 1 {
		t.Fatal("el permiso nuevo")
	}
	if n := e.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'argos.contactos_cambiados' AND (payload->>'cliente_id')::bigint = 1201
		AND (payload->>'contacto_id')::bigint = 77 AND payload->>'accion' = 'editado' AND (payload->>'persona_cliente_id')::bigint = 1300
		AND payload::text NOT LIKE '%5512%' AND payload::text NOT LIKE '%Luis%'`); n != 1 {
		t.Fatalf("argos.contactos_cambiados sin números ni nombres: %d", n)
	}

	// Una lectura vieja que llega tarde (versión 2 < 3) no baja la versión ni pisa los permisos.
	ca.mu.Lock()
	ca.lista[0]["version"] = 2
	ca.lista[0]["permisos"] = map[string]any{"consultar": false}
	ca.mu.Unlock()
	if _, err := e.mod.RefrescarContactos(ctx, argos.EntradaRefrescarContactos{ClienteID: cliente, Actor: "usuario:7", Forzar: true}); err != nil {
		t.Fatal(err)
	}
	if n := e.contar(`SELECT count(*) FROM integracion.argos_contactos WHERE contacto_id = 77 AND version = 3 AND pagar AND consultar AND NOT quitado`); n != 1 {
		t.Fatal("la versión nunca baja")
	}
	if n := e.contar(`SELECT count(*) FROM integracion.argos_contacto_telefonos WHERE contacto_id = 77`); n != 1 {
		t.Fatal("los teléfonos de una versión vieja tampoco se escriben")
	}

	// Un Argos anterior a 0.14.2 no acepta «sistema»: el webhook falla (se reintenta) y la caché
	// queda como estaba.
	ca.mu.Lock()
	ca.rechazarSistema = true
	ca.lista[0]["version"] = 3
	ca.mu.Unlock()
	ent := e.entranteDe(e.enviar(conID, secreto, "wh-contactos-"+pruebas.Unico(), argos.EventoContactosCambiados,
		map[string]any{"cliente_id": cliente, "empresa_id": 1, "contacto_id": 77, "accion": "editado"}, time.Now()))
	if _, err := e.mod.ProcesarEntrante.Ejecutar(pruebas.ContextoSistema(e.pool), ent); err == nil {
		t.Fatal("sin relectura, el webhook se reintenta")
	}
	if n := e.contar(`SELECT count(*) FROM integracion.argos_contactos WHERE cliente_id = 1201 AND NOT quitado`); n != 1 {
		t.Fatalf("la caché no cambia: %d", n)
	}

	// quitado: queda quitado al momento, sin leer a Argos; la versión no baja.
	ca.mu.Lock()
	ca.rechazarSistema = false
	ca.mu.Unlock()
	antes = ca.cuantos()
	r = e.procesarWebhook(conID, secreto, argos.EventoContactosCambiados, map[string]any{"cliente_id": cliente, "empresa_id": 1, "contacto_id": 77, "accion": "quitado"})
	if r.Resultado != "aplicado" || ca.cuantos() != antes || e.contar(`SELECT count(*) FROM integracion.argos_contactos WHERE contacto_id = 77 AND quitado AND version = 3`) != 1 {
		t.Fatalf("quitado: %+v %d", r, ca.cuantos()-antes)
	}
	// Otra vez quitado: no cambia nada.
	r = e.procesarWebhook(conID, secreto, argos.EventoContactosCambiados, map[string]any{"cliente_id": cliente, "empresa_id": 1, "contacto_id": 77, "accion": "quitado"})
	if r.Resultado == "aplicado" {
		t.Fatalf("quitado dos veces: %+v", r)
	}
	// Vuelve en Argos con una versión nueva: deja de estar quitado.
	ca.mu.Lock()
	ca.lista[0]["version"] = 4
	ca.mu.Unlock()
	if _, err := e.mod.RefrescarContactos(ctx, argos.EntradaRefrescarContactos{ClienteID: cliente, Actor: "usuario:7", Forzar: true}); err != nil {
		t.Fatal(err)
	}
	if n := e.contar(`SELECT count(*) FROM integracion.argos_contactos WHERE contacto_id = 77 AND NOT quitado AND version = 4`); n != 1 {
		t.Fatal("vuelve a estar vigente")
	}

	// Argos ya no tiene al cliente (404): todos sus contactos quedan quitados, ninguno se borra.
	ca.mu.Lock()
	ca.noExiste = true
	ca.mu.Unlock()
	if _, err := e.mod.RefrescarContactos(ctx, argos.EntradaRefrescarContactos{ClienteID: cliente, Actor: "usuario:7", Forzar: true}); err != nil {
		t.Fatal(err)
	}
	if e.contar(`SELECT count(*) FROM integracion.argos_contactos WHERE cliente_id = 1201 AND NOT quitado`) != 0 ||
		e.contar(`SELECT count(*) FROM integracion.argos_contactos WHERE cliente_id = 1201`) != 2 ||
		e.contar(`SELECT count(*) FROM integracion.argos_contactos_leidos WHERE cliente_id = 1201`) != 0 {
		t.Fatal("404: todos quitados y la lectura olvidada")
	}
	ca.mu.Lock()
	ca.noExiste = false
	ca.mu.Unlock()

	// Un cliente que Iris no conoce se ignora; sin cliente_id, payload inválido.
	r = e.procesarWebhook(conID, secreto, argos.EventoContactosCambiados, map[string]any{"cliente_id": 55555, "empresa_id": 1, "accion": "creado"})
	if r.Resultado != "ignorado" || e.omitidoDe(r.EntranteID) != "cliente_no_en_instantanea" {
		t.Fatalf("cliente desconocido: %+v %s", r, e.omitidoDe(r.EntranteID))
	}
	r = e.procesarWebhook(conID, secreto, argos.EventoContactosCambiados, map[string]any{"empresa_id": 1, "accion": "creado"})
	if r.Resultado != "ignorado" || e.omitidoDe(r.EntranteID) != "payload_invalido" {
		t.Fatalf("sin cliente: %+v", r)
	}
}

// X-Iris-Contacto (§7.1, 0.10.1): con Argos ≥ 0.10.1 va en toda ruta de §7 con X-Iris-Conversacion,
// «titular» por defecto o el contacto_id; con uno anterior no va. Mal formada, Iris no la manda.
func TestCabeceraContactoEnLaAtencion(t *testing.T) {
	e := nuevoEntorno(t)
	_, conID, _ := e.conectado()
	ctx := nucleo.ConBD(pruebas.ContextoSistema(e.pool), e.pool)
	e.sembrarCliente(1201, "Ana Pérez")
	a := argos.AtencionIris{Actor: "usuario:7", Conversacion: "iris:conversacion:9", EmpresaID: 1}
	leer := func(contacto string) (string, error) {
		a.Contacto = contacto
		_, err := e.mod.EstadoCuentaServicio(ctx, a, 1201, nil)
		e.argos.mu.Lock()
		defer e.argos.mu.Unlock()
		return e.argos.ultimoContacto, err
	}
	e.conContrato(conID, "0.10.0")
	if c, err := leer(""); err != nil || c != "" {
		t.Fatalf("Argos 0.10.0 no recibe X-Iris-Contacto: %q %v", c, err)
	}
	e.conContrato(conID, "0.10.1")
	if c, err := leer(""); err != nil || c != "titular" {
		t.Fatalf("titular por defecto: %q %v", c, err)
	}
	if c, err := leer("77"); err != nil || c != "77" {
		t.Fatalf("el contacto: %q %v", c, err)
	}
	if _, err := leer("0077"); !errors.Is(err, argos.ErrCabecerasIris) {
		t.Fatalf("mal formada: %v", err)
	}
}

// Carrera de la revisión de seguridad: una lectura de contactos que empezó antes de un quitado por
// webhook (que no sube la versión) y se guarda después, con la misma versión, no revive al contacto
// ni le repone permisos, y la caché no queda marcada como leída después del quitado. Con una versión
// mayor de Argos, sí vuelve.
func TestContactosCarreraQuitadoNoRevive(t *testing.T) {
	e := nuevoEntorno(t)
	_, conID, secreto := e.conectado()
	ca := nuevosContactosArgos(e)
	ctx := nucleo.ConBD(pruebas.ContextoSistema(e.pool), e.pool)
	cliente := int64(1250)
	e.sembrarCliente(cliente, "Ana Pérez")
	e.conContrato(conID, "0.14.2")
	contacto := func(version int, pagar bool) map[string]any {
		return map[string]any{"contacto_id": 90, "cliente_id": cliente, "empresa_id": 1, "nombre": "Luis Pérez", "relacion": "familiar",
			"permisos":  map[string]any{"consultar": true, "reportar": true, "pagar": pagar, "avisos": pagar},
			"telefonos": []map[string]any{{"numero": "+50255120090", "whatsapp": true, "principal": true}}, "version": version}
	}
	refrescar := func() {
		t.Helper()
		if _, err := e.mod.RefrescarContactos(ctx, argos.EntradaRefrescarContactos{ClienteID: cliente, Actor: "usuario:7", Forzar: true}); err != nil {
			t.Fatal(err)
		}
	}
	ca.mu.Lock()
	ca.lista = []map[string]any{contacto(2, false)}
	ca.mu.Unlock()
	refrescar()

	// La lectura empieza; mientras Argos responde, llega el quitado (misma versión 2); la respuesta vieja
	// (con pagar y avisos) se guarda después.
	ca.mu.Lock()
	ca.lista = []map[string]any{contacto(2, true)}
	ca.durante = func() {
		r := e.procesarWebhook(conID, secreto, argos.EventoContactosCambiados, map[string]any{"cliente_id": cliente, "empresa_id": 1, "contacto_id": 90, "accion": "quitado"})
		if r.Resultado != "aplicado" {
			t.Errorf("quitado: %+v", r)
		}
	}
	ca.mu.Unlock()
	refrescar()
	if n := e.contar(`SELECT count(*) FROM integracion.argos_contactos WHERE contacto_id = 90 AND quitado AND version = 2 AND NOT pagar AND NOT avisos`); n != 1 {
		t.Fatal("la lectura vieja no revive al contacto quitado ni le repone pagar y avisos")
	}
	if n := e.contar(`SELECT count(*) FROM integracion.argos_contactos_leidos l JOIN integracion.argos_contactos k ON k.cliente_id = l.cliente_id
		WHERE l.cliente_id = 1250 AND k.contacto_id = 90 AND l.sincronizado_en < k.leido_en`); n != 1 {
		t.Fatal("la caché queda leída a la hora en que empezó la lectura, antes del quitado")
	}

	// Argos lo vuelve a agregar (versión 3) y la lectura empieza después del quitado: vuelve.
	ca.mu.Lock()
	ca.lista = []map[string]any{contacto(3, false)}
	ca.mu.Unlock()
	refrescar()
	if n := e.contar(`SELECT count(*) FROM integracion.argos_contactos WHERE contacto_id = 90 AND NOT quitado AND version = 3`); n != 1 {
		t.Fatal("con una versión mayor, leída después del quitado, vuelve")
	}

	// Argos lo edita (v4, con pagar y avisos) y después lo quita. Una lectura que empezó entre la
	// edición y el quitado trae v4; el quitado lo deja en v3. La lectura vieja, aunque traiga una versión
	// mayor, no lo revive ni le da pagar y avisos.
	ca.mu.Lock()
	ca.lista = []map[string]any{contacto(4, true)}
	ca.durante = func() {
		r := e.procesarWebhook(conID, secreto, argos.EventoContactosCambiados, map[string]any{"cliente_id": cliente, "empresa_id": 1, "contacto_id": 90, "accion": "quitado"})
		if r.Resultado != "aplicado" {
			t.Errorf("quitado: %+v", r)
		}
	}
	ca.mu.Unlock()
	refrescar()
	if n := e.contar(`SELECT count(*) FROM integracion.argos_contactos WHERE contacto_id = 90 AND quitado AND version = 3 AND NOT pagar AND NOT avisos`); n != 1 {
		t.Fatal("una lectura v4 iniciada antes del quitado no revive al contacto")
	}

	// Una lectura que empieza después del quitado y trae v4: vuelve, con sus permisos.
	refrescar()
	if n := e.contar(`SELECT count(*) FROM integracion.argos_contactos WHERE contacto_id = 90 AND NOT quitado AND version = 4 AND pagar AND avisos`); n != 1 {
		t.Fatal("una lectura posterior al quitado con v4 lo devuelve")
	}
}

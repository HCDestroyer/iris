package mensajeria_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
)

// Contactos del cliente en la atención (contrato argosisp.md §3.5, B22) y servicios a cargo de otro
// cliente (§3.8, B25), contra Postgres real y por HTTP. La caché de contactos (integracion.argos_contactos)
// la llena integracion/argos; aquí se siembra directo o la llena el Argos falso al buscar.

// contactosArgosFalsos es lo que «Argos» responde en GET /contactos/buscar y GET /clientes/:id/contactos.
type contactosArgosFalsos struct {
	mu sync.Mutex
	// buscar: las filas de buscar; por cliente: los contactos (con sus números) que se guardan al leer.
	buscar     []argos.ContactoBuscado
	porCliente map[int64][]contactoSembrado
	// actores: X-Iris-Actor de cada búsqueda y lectura; busquedas: cuántas veces se buscó.
	actores   []string
	busquedas int
}

// BuscarContactos: GET /contactos/buscar (mensajeria.ArgosContactos).
func (f *argosFalso) BuscarContactos(_ context.Context, actor, _ string) ([]argos.ContactoBuscado, bool, error) {
	f.mu.Lock()
	k := f.contactos
	f.mu.Unlock()
	if k == nil {
		return nil, false, nil
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	k.actores = append(k.actores, actor)
	k.busquedas++
	return k.buscar, true, nil
}

// RefrescarContactos: como argos.Modulo, deja en la caché los contactos que «Argos» tiene del cliente.
func (f *argosFalso) RefrescarContactos(ctx context.Context, in argos.EntradaRefrescarContactos) (bool, error) {
	f.mu.Lock()
	k := f.contactos
	f.mu.Unlock()
	if k == nil {
		return false, nil
	}
	k.mu.Lock()
	k.actores = append(k.actores, in.Actor)
	l := k.porCliente[in.ClienteID]
	k.mu.Unlock()
	bd := nucleo.BDDe(ctx)
	for _, c := range l {
		if err := sembrarContactoEn(ctx, bd, c); err != nil {
			return false, err
		}
	}
	return true, nil
}

// contactoSembrado es un contacto de la caché.
type contactoSembrado struct {
	id, cliente int64
	relacion    string
	servicios   []int64
	consultar   bool
	reportar    bool
	pagar       bool
	persona     *int64
	numero      string
}

func sembrarContactoEn(ctx context.Context, bd nucleo.BD, c contactoSembrado) error {
	if _, err := bd.Exec(ctx, `INSERT INTO integracion.argos_contactos (contacto_id, cliente_id, empresa_id, nombre, relacion, es_titular, servicios,
		consultar, reportar, pagar, avisos, persona_cliente_id) VALUES ($1, $2, 1, 'Luis Pérez', $3, $3 = 'titular', $4, $5, $6, $7, true, $8)
		ON CONFLICT (contacto_id) DO NOTHING`, c.id, c.cliente, c.relacion, c.servicios, c.consultar, c.reportar, c.pagar, c.persona); err != nil {
		return err
	}
	_, err := bd.Exec(ctx, `INSERT INTO integracion.argos_contacto_telefonos (contacto_id, numero, digitos, whatsapp, principal)
		VALUES ($1, $2, $3, true, true) ON CONFLICT DO NOTHING`, c.id, "+"+c.numero, c.numero)
	return err
}

func (b *bandeja) sembrarContacto(c contactoSembrado) {
	b.t.Helper()
	if err := sembrarContactoEn(b.ctx, b.bd, c); err != nil {
		b.t.Fatal(err)
	}
}

// clienteArgos siembra un cliente de la instantánea (empresa 1) con su móvil (vacío = sin móvil) y
// sus servicios con código.
func (b *bandeja) clienteArgos(nombre, movil string, codigos ...string) (int64, []int64) {
	b.t.Helper()
	id := siguienteCliente.Add(1)
	m := ""
	if movil != "" {
		m = "+" + movil
	}
	if _, err := b.bd.Exec(b.ctx, `INSERT INTO integracion.argos_clientes (id, empresa_id, nombre, movil, codigo, sincronizado_en, empresas)
		VALUES ($1, 1, $2, $3, $4, now(), '{1}')`, id, nombre, m, fmt.Sprintf("C-%d", id)); err != nil {
		b.t.Fatal(err)
	}
	servicios := []int64{}
	for i, cod := range codigos {
		s := id*10 + int64(i)
		if _, err := b.bd.Exec(b.ctx, `INSERT INTO integracion.argos_servicios (id, empresa_id, cliente_id, codigo, nombre, plan_nombre, estado, zona_nombre, sincronizado_en)
			VALUES ($1, 1, $2, $3, 'Casa', 'Plan 50', 'activo', 'Centro', now())`, s, id, cod); err != nil {
			b.t.Fatal(err)
		}
		servicios = append(servicios, s)
	}
	return id, servicios
}

func codigoUnico() string { return fmt.Sprintf("S-%06d", siguienteCliente.Add(1)%1_000_000) }

// Un contacto que escribe desde su número: la conversación se asocia sola al cliente con el rol del
// contacto; ve solo sus servicios y sus permisos; un servicio que no es suyo o un permiso apagado
// responden como Argos (403 servicio_fuera_del_contacto, 403 permiso_de_contacto). Con permiso, la
// acción sale con X-Iris-Contacto = su contacto_id.
func TestContactoDelClienteEnLaAtencion(t *testing.T) {
	b := nuevaBandeja(t, nil)
	num := numeroCliente()
	c1, c2 := codigoUnico(), codigoUnico()
	cliente, ss := b.clienteArgos("Ana Pérez", "", c1, c2)
	contacto := siguienteCliente.Add(1)
	b.sembrarContacto(contactoSembrado{id: contacto, cliente: cliente, relacion: "familiar", servicios: []int64{ss[0]}, consultar: true, reportar: true, pagar: false, numero: num})

	b.texto(num, "wamid.k1", "Hola, soy Luis")
	c := b.conv(num)
	k := c["contacto"].(map[string]any)
	if k["clienteId"].(float64) != float64(cliente) || k["asociacion"] != "automatica" || k["verificado"] != true {
		t.Fatalf("asociada al cliente del contacto: %v", k)
	}
	rol := c["rolContacto"].(map[string]any)
	if rol["rol"] != "contacto" || rol["contactoId"].(float64) != float64(contacto) || rol["relacion"] != "familiar" {
		t.Fatalf("rol del contacto: %v", rol)
	}
	if sv := rol["servicios"].([]any); len(sv) != 1 || sv[0].(map[string]any)["codigo"] != c1 {
		t.Fatalf("solo sus servicios: %v", sv)
	}
	if p := rol["permisos"].(map[string]any); p["pagar"] != false || p["consultar"] != true {
		t.Fatalf("permisos: %v", p)
	}
	if !strings.Contains(fmt.Sprint(rol["reservado"]), "titular") {
		t.Fatalf("lo reservado al titular: %v", rol["reservado"])
	}
	ruta := "/conversaciones/" + cid(c)
	b.exigirProblema(b.api(b.agente, http.MethodPost, ruta+"/promesas/simular", map[string]any{"servicioId": ss[0]}), http.StatusForbidden, "permiso_de_contacto")
	b.exigirProblema(b.api(b.agente, http.MethodPost, ruta+"/promesas/simular", map[string]any{"servicioId": ss[1]}), http.StatusForbidden, "servicio_fuera_del_contacto")
	if len(b.argos.simulaciones) != 0 {
		t.Fatal("sin permiso del contacto no se llama a Argos")
	}
	// El titular le enciende «pagar»: la simulación sale con su contacto.
	if _, err := b.bd.Exec(b.ctx, `UPDATE integracion.argos_contactos SET pagar = true WHERE contacto_id = $1`, contacto); err != nil {
		t.Fatal(err)
	}
	b.exigir(b.api(b.agente, http.MethodPost, ruta+"/promesas/simular", map[string]any{"servicioId": ss[0]}), http.StatusOK)
	if a := b.argos.simulaciones[len(b.argos.simulaciones)-1]; a.Contacto != fmt.Sprint(contacto) {
		t.Fatalf("X-Iris-Contacto del contacto: %+v", a)
	}

	// Un número adicional del titular (está en el contacto titular, no es su teléfono ni su móvil):
	// contacto no verificado, no asocia solo.
	numA := numeroCliente()
	b.sembrarContacto(contactoSembrado{id: siguienteCliente.Add(1), cliente: cliente, relacion: "titular", consultar: true, reportar: true, pagar: true, numero: numA})
	b.texto(numA, "wamid.k3", "Hola")
	if k := b.conv(numA)["contacto"].(map[string]any); k["clienteId"] != nil {
		t.Fatalf("un número adicional no asocia solo: %v", k)
	}

	// El titular que escribe desde su móvil: rol titular y X-Iris-Contacto «titular».
	numT := numeroCliente()
	titular, st := b.clienteArgos("Marta Díaz", numT, codigoUnico())
	b.texto(numT, "wamid.k2", "Hola")
	ct := b.conv(numT)
	if ct["contacto"].(map[string]any)["clienteId"].(float64) != float64(titular) || ct["rolContacto"].(map[string]any)["rol"] != "titular" {
		t.Fatalf("titular: %v %v", ct["contacto"], ct["rolContacto"])
	}
	b.exigir(b.api(b.agente, http.MethodPost, "/conversaciones/"+cid(ct)+"/promesas/simular", map[string]any{"servicioId": st[0]}), http.StatusOK)
	if a := b.argos.simulaciones[len(b.argos.simulaciones)-1]; a.Contacto != argos.ContactoTitular {
		t.Fatalf("X-Iris-Contacto del titular: %+v", a)
	}
}

// Un número que está en varios clientes: la conversación no se asocia y el panel no ve datos de
// ninguno; se compara el número de servicio que da el cliente, solo contra los que comparten el
// número. 3 fallos por conversación; acertar asocia (identifica, no verifica más que el número).
func TestNumeroEnVariosClientes(t *testing.T) {
	b := nuevaBandeja(t, nil)
	num := numeroCliente()
	cod := codigoUnico()
	uno, _ := b.clienteArgos("Pedro Secreto", num, cod)
	dos, _ := b.clienteArgos("Rosa Escondida", num, codigoUnico())
	// Un servicio de otro cliente que no comparte el número: su código nunca coincide.
	_, _ = b.clienteArgos("Ajeno", "", "S-999001")

	b.texto(num, "wamid.v1", "Hola")
	c := b.conv(num)
	if c["contacto"].(map[string]any)["clienteId"] != nil {
		t.Fatalf("con varios clientes no se asocia: %v", c["contacto"])
	}
	rol := c["rolContacto"].(map[string]any)
	if rol["varios"] != true || rol["elegir"].(map[string]any)["conversacion"].(float64) != 3 {
		t.Fatalf("varios: %v", rol)
	}
	crudo, _ := json.Marshal(c)
	for _, nombre := range []string{"Pedro Secreto", "Rosa Escondida", cod} {
		if strings.Contains(string(crudo), nombre) {
			t.Fatalf("la conversación muestra %q de un cliente", nombre)
		}
	}
	ruta := "/conversaciones/" + cid(c) + "/elegir-cliente"
	r := b.api(b.agente, http.MethodPost, ruta, map[string]any{"referencia": "S-999001"})
	b.exigir(r, http.StatusOK)
	if r.cuerpo["coincide"] != false || r.cuerpo["intentos"].(map[string]any)["conversacion"].(float64) != 2 {
		t.Fatalf("un código de otro cliente no coincide: %v", r.cuerpo)
	}
	// Se escribe a su manera: «s 0…» es el mismo código.
	r = b.api(b.agente, http.MethodPost, ruta, map[string]any{"referencia": strings.ToLower(strings.Replace(cod, "-", " ", 1))})
	b.exigir(r, http.StatusOK)
	if r.cuerpo["coincide"] != true || r.cuerpo["conversacion"].(map[string]any)["contacto"].(map[string]any)["clienteId"].(float64) != float64(uno) {
		t.Fatalf("el código del cliente: %v", r.cuerpo)
	}
	if n := b.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'conversacion.cliente_asociado' AND payload->>'conversacion_id' = $1
		AND payload->>'origen' = 'referencia' AND (payload->>'cliente_id')::bigint = $2`, cid(c), uno); n != 1 {
		t.Fatalf("conversacion.cliente_asociado: %d", n)
	}
	// La referencia no queda en la auditoría.
	if n := b.contar(`SELECT count(*) FROM auditoria.acciones WHERE accion = 'conversaciones.elegir_cliente' AND entrada::text LIKE '%' || $1 || '%'`,
		strings.TrimPrefix(cod, "S-")); n != 0 {
		t.Fatal("la referencia quedó en la auditoría")
	}
	b.exigirProblema(b.api(b.agente, http.MethodPost, ruta, map[string]any{"referencia": cod}), http.StatusUnprocessableEntity, "ya_asociada")
	_ = dos

	// Otra conversación del mismo número (otro canal no hace falta: se desasocia y se agotan los intentos).
	num2 := numeroCliente()
	_, _ = b.clienteArgos("Uno", num2, codigoUnico())
	_, _ = b.clienteArgos("Dos", num2, codigoUnico())
	b.texto(num2, "wamid.v2", "Hola")
	c2 := b.conv(num2)
	ruta2 := "/conversaciones/" + cid(c2) + "/elegir-cliente"
	for range 3 {
		b.exigir(b.api(b.agente, http.MethodPost, ruta2, map[string]any{"referencia": "S-000000"}), http.StatusOK)
	}
	b.exigirProblema(b.api(b.agente, http.MethodPost, ruta2, map[string]any{"referencia": "S-000000"}), http.StatusTooManyRequests, "demasiados_intentos")
}

// José está a cargo del servicio de su mamá (§3.8): la conversación queda con José (su propio
// cliente), el servicio de la mamá aparece «a su cargo» solo con código y nombre, y las acciones sobre
// él salen con X-Iris-Contacto = el contacto de José en el cliente de la mamá y sus permisos.
func TestServicioACargoDeOtroCliente(t *testing.T) {
	b := nuevaBandeja(t, nil)
	num := numeroCliente()
	jose, sj := b.clienteArgos("José", num, codigoUnico())
	codMama := codigoUnico()
	mama, sm := b.clienteArgos("Mamá de José", "", codMama)
	contacto := siguienteCliente.Add(1)
	b.sembrarContacto(contactoSembrado{id: contacto, cliente: mama, relacion: "familiar", consultar: false, reportar: true, pagar: true, persona: &jose, numero: num})

	b.texto(num, "wamid.j1", "Hola")
	c := b.conv(num)
	if c["contacto"].(map[string]any)["clienteId"].(float64) != float64(jose) {
		t.Fatalf("la conversación sigue con José: %v", c["contacto"])
	}
	rol := c["rolContacto"].(map[string]any)
	ac := rol["aCargo"].([]any)
	if rol["rol"] != "titular" || len(ac) != 1 {
		t.Fatalf("a su cargo: %v", rol)
	}
	a0 := ac[0].(map[string]any)
	if a0["contactoId"].(float64) != float64(contacto) || a0["permisos"].(map[string]any)["consultar"] != false {
		t.Fatalf("el contacto a cargo: %v", a0)
	}
	if sv := a0["servicios"].([]any); len(sv) != 1 || sv[0].(map[string]any)["codigo"] != codMama {
		t.Fatalf("servicios a cargo: %v", sv)
	}
	if crudo, _ := json.Marshal(rol); strings.Contains(string(crudo), "Mamá de José") {
		t.Fatal("nunca el nombre del otro cliente")
	}
	ruta := "/conversaciones/" + cid(c)
	b.exigir(b.api(b.agente, http.MethodPost, ruta+"/promesas/simular", map[string]any{"servicioId": sm[0]}), http.StatusOK)
	if a := b.argos.simulaciones[len(b.argos.simulaciones)-1]; a.Contacto != fmt.Sprint(contacto) {
		t.Fatalf("servicio a cargo con X-Iris-Contacto del contacto: %+v", a)
	}
	b.exigir(b.api(b.agente, http.MethodPost, ruta+"/promesas/simular", map[string]any{"servicioId": sj[0]}), http.StatusOK)
	if a := b.argos.simulaciones[len(b.argos.simulaciones)-1]; a.Contacto != argos.ContactoTitular {
		t.Fatalf("su propio servicio con «titular»: %+v", a)
	}
	// Sin «pagar» en el contacto, nada sobre el servicio de la mamá.
	if _, err := b.bd.Exec(b.ctx, `UPDATE integracion.argos_contactos SET pagar = false WHERE contacto_id = $1`, contacto); err != nil {
		t.Fatal(err)
	}
	b.exigirProblema(b.api(b.agente, http.MethodPost, ruta+"/promesas/simular", map[string]any{"servicioId": sm[0]}), http.StatusForbidden, "permiso_de_contacto")
}

// Al abrir una conversación sin cliente, Iris busca el número en los contactos de Argos con
// X-Iris-Actor de quien atiende (nunca «sistema»), una vez al día por número; si con eso el número es
// de una sola persona, la asocia sola.
func TestAbrirConversacionBuscaContactosEnArgos(t *testing.T) {
	b := nuevaBandeja(t, nil)
	num := numeroCliente()
	cliente, ss := b.clienteArgos("Ana Pérez", "", codigoUnico())
	contacto := siguienteCliente.Add(1)
	b.argos.contactos = &contactosArgosFalsos{
		buscar:     []argos.ContactoBuscado{{ClienteID: cliente, ContactoID: &contacto, Relacion: "inquilino"}},
		porCliente: map[int64][]contactoSembrado{cliente: {{id: contacto, cliente: cliente, relacion: "inquilino", servicios: ss, consultar: true, reportar: true, pagar: true, numero: num}}},
	}
	b.texto(num, "wamid.b1", "Hola")
	var id int64
	if err := b.bd.QueryRow(b.ctx, `SELECT id FROM mensajeria.conversaciones WHERE canal_id = $1 AND numero = $2 AND cliente_id IS NULL`, b.canal.ID, "+"+num).Scan(&id); err != nil {
		t.Fatalf("al llegar el mensaje no se asocia (la caché está vacía): %v", err)
	}
	r := b.api(b.agente, http.MethodGet, fmt.Sprintf("/conversaciones/%d", id), nil)
	b.exigir(r, http.StatusOK)
	if k := r.cuerpo["contacto"].(map[string]any); k["clienteId"] == nil || k["clienteId"].(float64) != float64(cliente) || k["asociacion"] != "automatica" {
		t.Fatalf("asociada tras buscar en Argos: %v", k)
	}
	if rol := r.cuerpo["rolContacto"].(map[string]any); rol["rol"] != "contacto" || rol["relacion"] != "inquilino" {
		t.Fatalf("rol: %v", rol)
	}
	k := b.argos.contactos
	if k.busquedas != 1 || k.actores[0] != fmt.Sprintf("usuario:%d", b.agente.id) {
		t.Fatalf("buscar con el actor de quien atiende: %d %v", k.busquedas, k.actores)
	}
	if n := b.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'conversacion.cliente_asociado' AND (payload->>'conversacion_id')::bigint = $1
		AND payload->>'origen' = 'contacto'`, id); n != 1 {
		t.Fatalf("conversacion.cliente_asociado: %d", n)
	}
	// Otro número sin nadie: se busca una vez y no se vuelve a buscar en 24 h.
	otro := numeroCliente()
	k.mu.Lock()
	k.buscar = nil
	k.mu.Unlock()
	b.texto(otro, "wamid.b2", "Hola")
	co := b.conv(otro)
	b.conv(otro)
	if k.busquedas != 2 {
		t.Fatalf("una sola búsqueda por número al día: %d", k.busquedas)
	}
	if co["contacto"].(map[string]any)["clienteId"] != nil {
		t.Fatalf("sin coincidencias no se asocia: %v", co["contacto"])
	}
}

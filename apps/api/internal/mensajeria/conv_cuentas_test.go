package mensajeria_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/conector"
)

// Cuentas que Argos no debería mandarle a la empresa 1 (o que no reciben depósitos): de otra
// empresa (8), sin empresa_id (9) e inactiva (10). Iris no las ofrece ni las acepta.
const cuentasQueNoSon = `,
	{"id":8,"empresa_id":2,"banco_id":1,"banco_nombre":"Banco Industrial","numero":"111-000008-8","alias":"De otra empresa","moneda":"GTQ","activa":true},
	{"id":9,"banco_id":1,"banco_nombre":"Banco Industrial","numero":"111-000009-9","alias":"Sin empresa","moneda":"GTQ","activa":true},
	{"id":10,"empresa_id":1,"banco_id":1,"banco_nombre":"Banco Industrial","numero":"111-000010-0","alias":"Inactiva","moneda":"GTQ","activa":false}`

// El cliente lee la forma real de Argos 1.17.0 (alias, banco_nombre, activa, empresa_id) y arma el
// nombre: el alias o, sin alias, el banco y los últimos 4 dígitos del número.
func TestCuentaBancariaArgosForma(t *testing.T) {
	var cs []argos.CuentaBancariaArgos
	if err := json.Unmarshal([]byte(`[
		{"id":3,"empresa_id":1,"banco_id":1,"banco_nombre":"Banco Industrial","numero":"000-123456-7","alias":"Monetaria","moneda":"GTQ","activa":true},
		{"id":5,"empresa_id":1,"banco_id":2,"banco_nombre":"Banrural","numero":"3-445-00987-6","alias":"  ","moneda":"GTQ","activa":true},
		{"id":6,"empresa_id":1,"banco_id":2,"banco_nombre":"Banrural","numero":"","alias":null,"moneda":"GTQ","activa":true},
		{"id":7,"nombre":"No existe en Argos","banco":"Tampoco","numero":"12","moneda":"GTQ"}]`), &cs); err != nil {
		t.Fatal(err)
	}
	if c := cs[0]; c.Nombre != "Monetaria" || c.Banco != "Banco Industrial" || !c.Activa || c.EmpresaID == nil || *c.EmpresaID != 1 || !c.RecibeDe(1) || c.RecibeDe(2) || c.RecibeDe(0) {
		t.Fatalf("con alias: %+v", c)
	}
	if c := cs[1]; c.Nombre != "Banrural …9876" || c.Banco != "Banrural" {
		t.Fatalf("sin alias: %+v", c)
	}
	if c := cs[2]; c.Nombre != "Banrural" {
		t.Fatalf("sin alias ni número: %+v", c)
	}
	// «nombre» y «banco» no son campos de Argos: no se leen. Sin empresa_id ni activa, no recibe.
	if c := cs[3]; c.Nombre != "…12" || c.Banco != "" || c.RecibeDe(1) || c.RecibeDe(0) {
		t.Fatalf("campos que Argos no manda: %+v", c)
	}
}

// La lista del panel sale con nombre y banco; una cuenta de otra empresa, sin empresa_id o inactiva
// no se ofrece ni se acepta al registrar la boleta (una persona).
func TestConvCuentasBancariasDeLaEmpresa(t *testing.T) {
	b := nuevaBandeja(t, nil)
	b.argos.cuentasExtra = cuentasQueNoSon
	num := numeroCliente()
	b.webhook(b.mensajeCliente(num, "wamid.cu1", `"type":"image","image":{"id":"boleta1","mime_type":"image/jpeg"}`))
	c := b.conv(num)
	ms := b.mensajes(b.supervisor, cid(c))
	if err := b.m.DescargarMedio(b.ctx, uuid.MustParse(ms[len(ms)-1]["id"].(string)), false); err != nil {
		t.Fatal(err)
	}
	ms = b.mensajes(b.supervisor, cid(c))
	adj := ms[len(ms)-1]["adjunto"].(map[string]any)["id"]

	cu := b.api(b.agente, http.MethodGet, "/conversaciones/cuentas-bancarias", nil)
	b.exigir(cu, http.StatusOK)
	d := cu.cuerpo["data"].([]any)
	if len(d) != 2 {
		t.Fatalf("solo las activas en GTQ de la empresa: %v", cu.cuerpo)
	}
	if k := d[0].(map[string]any); fmt.Sprint(k["id"]) != "3" || k["nombre"] != "Monetaria" || k["banco"] != "Banco Industrial" || k["numero"] != "000-123456-7" || k["moneda"] != "GTQ" || len(k) != 5 {
		t.Fatalf("cuenta con alias: %v", k)
	}
	if k := d[1].(map[string]any); fmt.Sprint(k["id"]) != "5" || k["nombre"] != "Banrural …9876" || k["banco"] != "Banrural" {
		t.Fatalf("cuenta sin alias: %v", k)
	}

	cliente := b.cliente(1, "5512-0102")
	if _, err := b.bd.Exec(b.ctx, `INSERT INTO integracion.argos_clientes (id, empresa_id, nombre, sincronizado_en, empresas) VALUES ($1, 1, 'María López', now(), '{1}')
		ON CONFLICT DO NOTHING`, cliente); err != nil {
		t.Fatal(err)
	}
	if _, err := b.bd.Exec(b.ctx, `INSERT INTO integracion.argos_servicios (id, empresa_id, cliente_id, sincronizado_en) VALUES ($1, 1, $2, now())`, cliente*10, cliente); err != nil {
		t.Fatal(err)
	}
	ruta := "/conversaciones/" + cid(c)
	b.exigir(b.api(b.agente, http.MethodPost, ruta+"/asociar-cliente", map[string]any{"version": ver(b.conv(num)), "clienteId": cliente, "servicioId": cliente * 10}), http.StatusOK)
	b.exigir(b.api(b.supervisor, http.MethodPost, ruta+"/confirmar-cliente", map[string]any{"version": ver(b.conv(num))}), http.StatusOK)

	boleta := func(cuenta int) map[string]any {
		return map[string]any{"adjuntoId": adj, "metodo": "deposito", "cuentaBancariaId": cuenta, "referencia": fmt.Sprintf("CU-%d", cuenta), "monto": "250.00", "fechaDeposito": "2026-11-04"}
	}
	boletas := func() int {
		return b.contar(`SELECT count(*) FROM mensajeria.boletas WHERE conversacion_id = $1::bigint`, cid(c))
	}
	// 8: de otra empresa; 9: sin empresa_id; 10: inactiva; 4: en dólares; 77: no existe.
	for _, cuenta := range []int{8, 9, 10, 4, 77} {
		r := b.api(b.agente, http.MethodPost, ruta+"/boletas", boleta(cuenta))
		b.exigir(r, http.StatusUnprocessableEntity, "type", "urn:iris:problema:validacion")
		if e := fmt.Sprint(r.cuerpo["errors"]); !strings.Contains(e, "cuentaBancariaId") || !strings.Contains(e, "cuenta_de_la_empresa") {
			t.Fatalf("cuenta %d: %v", cuenta, r.cuerpo)
		}
	}
	if boletas() != 0 {
		t.Fatal("una cuenta que no es de la empresa no deja boleta")
	}
	// Sin poder leer las cuentas en Argos no se registra (falla cerrado).
	b.argos.mu.Lock()
	b.argos.errCuentas = &conector.ErrorRemoto{Status: 0}
	b.argos.mu.Unlock()
	if r := b.api(b.agente, http.MethodPost, ruta+"/boletas", boleta(3)); r.status/100 == 2 || boletas() != 0 {
		t.Fatalf("sin Argos no se valida la cuenta: %d %v", r.status, r.cuerpo)
	}
	b.argos.mu.Lock()
	b.argos.errCuentas = nil
	b.argos.mu.Unlock()
	// La cuenta sin alias de la empresa, sí.
	b.exigir(b.api(b.agente, http.MethodPost, ruta+"/boletas", boleta(5)), http.StatusCreated, "estado", "por_enviar")
	if boletas() != 1 {
		t.Fatal("la boleta con una cuenta de la empresa se registra")
	}
	// Se manda a Argos aquí mismo: no queda por enviar para el despacho de otra prueba.
	if err := b.m.DespacharBoletas(b.ctx, b.bd); err != nil {
		t.Fatal(err)
	}
	if n := b.contar(`SELECT count(*) FROM mensajeria.boletas WHERE conversacion_id = $1::bigint AND estado = 'registrada' AND cuenta_bancaria_id = 5`, cid(c)); n != 1 {
		t.Fatalf("boleta registrada en Argos con la cuenta 5: %d", n)
	}
}

// El asistente: cuentas_para_boleta ofrece las cuentas con su nombre y solo las de la empresa;
// registrar_boleta no acepta una cuenta de otra empresa, sin empresa_id o inactiva.
func TestAutonomoCuentasDeLaEmpresa(t *testing.T) {
	x := nuevaBandejaIA(t)
	x.argos.cuentasExtra = cuentasQueNoSon
	x.autonomo("siempre_primero", map[string]any{"maxTurnos": 20})
	conv, _ := x.conClienteArgos()
	num := x.numeroDe(conv)
	*x.reloj = x.reloj.Add(21 * time.Second)
	x.ia.decir(`{"categoria": "Pagos y saldo", "confianza": 0.95}`)
	x.guion(dice("Por favor mándame la foto de tu boleta."))
	x.correr(conv)
	x.webhook(x.mensajeCliente(num, "wamid.cuia1", `"type":"image","image":{"id":"boleta1","mime_type":"image/jpeg"}`))
	ms := x.mensajes(x.supervisor, conv)
	if err := x.m.DescargarMedio(x.ctx, uuid.MustParse(ms[len(ms)-1]["id"].(string)), false); err != nil {
		t.Fatal(err)
	}
	*x.reloj = x.reloj.Add(21 * time.Second)
	registrar := func(cuenta int) llamadaFalsa {
		return llamadaFalsa{"registrar_boleta", fmt.Sprintf(`{"metodo": "deposito", "cuenta_bancaria_id": %d, "banco_origen_id": 0, "referencia": "CU-%d", "monto": "250.00", "fecha_deposito": "2026-11-04"}`, cuenta, cuenta)}
	}
	x.guion(llama("cuentas_para_boleta", `{}`),
		pasoFalso{llamadas: []llamadaFalsa{registrar(8), registrar(9), registrar(10)}},
		dice("Dime a cuál de nuestras cuentas depositaste."))
	x.correr(conv)
	if a := strings.Join(x.acciones(conv)[1:], ","); a != "cuentas_para_boleta:ok,recibir_boleta:rechazada,recibir_boleta:rechazada,recibir_boleta:rechazada,responder:ok" {
		t.Fatalf("cuentas ajenas: %s", a)
	}
	if n := x.contar(`SELECT count(*) FROM mensajeria.boletas WHERE conversacion_id = $1::bigint`, conv); n != 0 {
		t.Fatalf("una cuenta que no es de la empresa no deja boleta: %d", n)
	}
	// Lo que leyó el modelo: las dos cuentas de la empresa con su nombre, y ninguna de las otras.
	cuerpos := strings.Join(x.ia.cuerposAutonomos(), "\n")
	for _, s := range []string{"cuenta id 3: Monetaria", "cuenta id 5: Banrural …9876"} {
		if !strings.Contains(cuerpos, s) {
			t.Fatalf("falta %q en lo que leyó el asistente:\n%s", s, cuerpos)
		}
	}
	for _, s := range []string{"cuenta id 4", "cuenta id 8", "cuenta id 9", "cuenta id 10", "De otra empresa", "Sin empresa", "Inactiva"} {
		if strings.Contains(cuerpos, s) {
			t.Fatalf("el asistente no debía ver %q", s)
		}
	}
}

package mensajeria_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/HCDestroyer/iris/apps/api/internal/integracion/conector"
	"github.com/HCDestroyer/iris/apps/api/internal/mensajeria"
)

// La cuenta se desactivó en Argos después del depósito (0.8.0 §7.8, §10.2 punto 13): POST
// /comprobantes responde 422 con rule «inactiva». La boleta queda fallida sin reintento, con un
// motivo claro, y la conversación que atendía el asistente pasa a una persona.
func TestAutonomoBoletaCuentaInactiva(t *testing.T) {
	x := nuevaBandejaIA(t)
	x.autonomo("siempre_primero", map[string]any{"maxTurnos": 20})
	conv, _ := x.conClienteArgos()
	num := x.numeroDe(conv)
	*x.reloj = x.reloj.Add(21 * time.Second)
	x.ia.decir(`{"categoria": "Pagos y saldo", "confianza": 0.95}`)
	x.guion(dice("Por favor mándame la foto de tu boleta."))
	x.correr(conv)
	x.webhook(x.mensajeCliente(num, "wamid.ina1", `"type":"image","image":{"id":"boleta1","mime_type":"image/jpeg"}`))
	ms := x.mensajes(x.supervisor, conv)
	if err := x.m.DescargarMedio(x.ctx, uuid.MustParse(ms[len(ms)-1]["id"].(string)), false); err != nil {
		t.Fatal(err)
	}
	*x.reloj = x.reloj.Add(21 * time.Second)
	x.guion(llama("cuentas_para_boleta", `{}`),
		llama("registrar_boleta", `{"metodo": "deposito", "cuenta_bancaria_id": 3, "banco_origen_id": 0, "referencia": "INA-1", "monto": "250.00", "fecha_deposito": "2026-11-04"}`),
		dice("Listo, caja la revisa."))
	x.correr(conv)
	if n := x.contar(`SELECT count(*) FROM mensajeria.boletas WHERE conversacion_id = $1::bigint AND por_asistente`, conv); n != 1 {
		t.Fatalf("el asistente registra la boleta: %d (%v)", n, x.acciones(conv))
	}
	x.argos.mu.Lock()
	x.argos.errComprob = &conector.ErrorRemoto{Status: 422, Campos: []string{"cuenta_bancaria_id"}, Reglas: []string{"inactiva"}}
	x.argos.mu.Unlock()
	if _, err := x.bd.Exec(x.ctx, `UPDATE mensajeria.boletas SET proximo_intento = '2000-01-01' WHERE conversacion_id = $1::bigint`, conv); err != nil {
		t.Fatal(err)
	}
	if err := x.m.DespacharBoletas(x.ctx, x.bd); err != nil {
		t.Fatal(err)
	}
	var estado, detalle string
	if err := x.bd.QueryRow(x.ctx, `SELECT estado, coalesce(detalle, '') FROM mensajeria.boletas WHERE conversacion_id = $1::bigint`, conv).Scan(&estado, &detalle); err != nil {
		t.Fatal(err)
	}
	if estado != "fallida" || detalle != mensajeria.MotivoCuentaInactiva {
		t.Fatalf("boleta con cuenta desactivada: %s %q", estado, detalle)
	}
	if e := estadoAsistente(x.conv(num)); !strings.HasPrefix(e, "espera_persona") {
		t.Fatalf("la conversación pasa a una persona: %s", e)
	}
	if n := x.contar(`SELECT count(*) FROM mensajeria.conversacion_mensajes WHERE conversacion_id = $1::bigint AND texto = $2`, conv, mensajeria.NotaCuentaInactiva); n != 1 {
		t.Fatalf("la nota interna para la persona: %d", n)
	}
	if n := x.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'asistente.paso_a_persona' AND payload->>'conversacion_id' = $1 AND payload->>'motivo' = 'herramienta_fallo'`, conv); n != 1 {
		t.Fatalf("asistente.paso_a_persona: %d", n)
	}
}

// Cuenta con titular y tipo (Argos 0.8.0): la única que se le puede dar al cliente.
const cuentaConTitular = `,
	{"id":12,"empresa_id":1,"banco_id":1,"banco_nombre":"Banco Industrial","numero":"000-777777-1","alias":"Cobros","titular":"Internet del Norte, S.A.","tipo":"monetaria","moneda":"GTQ","activa":true}`

// «Enviar cuentas» (contrato con Argos 0.8.0 §7.8, §10.3 fila 9): el texto fijo lleva banco, tipo,
// número, titular y moneda, solo de las cuentas activas en quetzales de la empresa y con titular. El
// servidor lo arma al enviar (la persona no lo escribe); sin cuentas para dar, 422.
func TestEnviarCuentas(t *testing.T) {
	b := nuevaBandeja(t, nil)
	b.argos.cuentasExtra = cuentasQueNoSon + cuentaConTitular
	num := numeroCliente()
	b.webhook(b.mensajeCliente(num, "wamid.ec1", `"type":"text","text":{"body":"¿A qué cuenta deposito?"}`))
	c := b.conv(num)
	ruta := "/conversaciones/" + cid(c)

	tx := b.api(b.agente, http.MethodGet, ruta+"/texto-cuentas", nil)
	b.exigir(tx, http.StatusOK)
	texto, _ := tx.cuerpo["texto"].(string)
	for _, s := range []string{mensajeria.EncabezadoCuentas, "Banco Industrial · cuenta monetaria", "No. 000-777777-1", "A nombre de: Internet del Norte, S.A.", "Moneda: quetzales (GTQ)", mensajeria.CierreCuentas} {
		if !strings.Contains(texto, s) {
			t.Fatalf("falta %q en el texto:\n%s", s, texto)
		}
	}
	// Sin titular (3 y 5), en dólares (4), de otra empresa (8), sin empresa (9) o inactiva (10): no van.
	for _, s := range []string{"000-123456-7", "3-445-00987-6", "000-999999-1", "111-000008-8", "111-000009-9", "111-000010-0"} {
		if strings.Contains(texto, s) {
			t.Fatalf("%s no debía ir en el texto", s)
		}
	}
	if fmt.Sprint(tx.cuerpo["cuentas"]) != "1" || fmt.Sprint(tx.cuerpo["sinTitular"]) != "2" {
		t.Fatalf("cuentas y sin titular: %v", tx.cuerpo)
	}

	// Con texto propio: 422 (el texto de las cuentas va solo).
	b.exigir(b.api(b.agente, http.MethodPost, ruta+"/mensajes", map[string]any{"id": nuevoID(), "cuentas": true, "texto": "Nuestras cuentas: 123"}), http.StatusUnprocessableEntity)
	// El servidor arma el texto.
	id := nuevoID()
	r := b.api(b.agente, http.MethodPost, ruta+"/mensajes", map[string]any{"id": id, "cuentas": true})
	b.exigir(r, http.StatusCreated)
	if r.cuerpo["texto"] != texto {
		t.Fatalf("el mensaje lleva el texto fijo: %v", r.cuerpo["texto"])
	}
	// Reenvío del mismo id: idempotente.
	b.exigir(b.api(b.agente, http.MethodPost, ruta+"/mensajes", map[string]any{"id": id, "cuentas": true}), http.StatusOK)
	if n := b.contar(`SELECT count(*) FROM mensajeria.conversacion_mensajes WHERE conversacion_id = $1::bigint AND texto LIKE '%000-777777-1%'`, cid(c)); n != 1 {
		t.Fatalf("un solo mensaje con las cuentas: %d", n)
	}
	// Ni el número ni el titular van a la auditoría ni a los eventos.
	if n := b.contar(`SELECT count(*) FROM auditoria.acciones WHERE entrada::text LIKE '%000-777777%' OR entrada::text LIKE '%Internet del Norte%'`); n != 0 {
		t.Fatal("el número de cuenta no va en la auditoría")
	}
	if n := b.contar(`SELECT count(*) FROM integracion.outbox WHERE payload::text LIKE '%000-777777%'`); n != 0 {
		t.Fatal("el número de cuenta no va en los eventos")
	}
	// El modelo nunca ve el texto de las cuentas en el historial.
	if mensajeria.TaparTextoCuentas(texto) != mensajeria.TextoCuentasTapado || mensajeria.TaparTextoCuentas("hola") != "hola" {
		t.Fatal("el historial del modelo tapa el texto de las cuentas")
	}

	// Sin cuentas con titular: no hay texto y enviar responde 422 sin_cuentas_para_enviar.
	b.argos.mu.Lock()
	b.argos.cuentasExtra = cuentasQueNoSon
	b.argos.mu.Unlock()
	tx = b.api(b.agente, http.MethodGet, ruta+"/texto-cuentas", nil)
	b.exigir(tx, http.StatusOK)
	if tx.cuerpo["texto"] != nil || fmt.Sprint(tx.cuerpo["sinTitular"]) != "2" {
		t.Fatalf("sin cuentas con titular: %v", tx.cuerpo)
	}
	b.exigir(b.api(b.agente, http.MethodPost, ruta+"/mensajes", map[string]any{"id": nuevoID(), "cuentas": true}), http.StatusUnprocessableEntity, "type", "urn:iris:problema:sin_cuentas_para_enviar")
	// Una conversación que no ve: 404.
	b.exigir(b.api(b.agente, http.MethodGet, "/conversaciones/999999999/texto-cuentas", nil), http.StatusNotFound)
}

// El asistente da las cuentas (§7.8, decisión del PM §10.2 punto 6) con la marca {{cuentas}}: solo a
// un número registrado, solo las que tienen titular, y el modelo nunca recibe el número ni el
// titular (ni en la herramienta ni después en el historial). A un número desconocido no se las da.
func TestAutonomoDaCuentas(t *testing.T) {
	x := nuevaBandejaIA(t)
	x.argos.cuentasExtra = cuentasQueNoSon + cuentaConTitular
	x.autonomo("siempre_primero", map[string]any{"maxTurnos": 20})
	conv, _ := x.conClienteArgos()
	num := x.numeroDe(conv)
	*x.reloj = x.reloj.Add(21 * time.Second)
	x.ia.decir(`{"categoria": "Pagos y saldo", "confianza": 0.95}`)
	x.guion(llama("cuentas_para_boleta", `{}`), dice("Con gusto, aquí están: {{cuentas}}"))
	x.correr(conv)
	ms := x.delAsistente(conv)
	if len(ms) == 0 {
		t.Fatal("el asistente debía responder")
	}
	final := textoDe(ms[len(ms)-1])
	if !strings.Contains(final, "No. 000-777777-1") || !strings.Contains(final, "A nombre de: Internet del Norte, S.A.") || strings.Contains(final, "{{") ||
		strings.Contains(final, "000-123456-7") {
		t.Fatalf("el mensaje con las cuentas:\n%s", final)
	}
	// Otro turno: el historial que ve el modelo trae el texto tapado.
	x.turno(num, "wamid.dc2", "Gracias", "Pagos y saldo", dice("Con gusto."))
	cuerpos := strings.Join(x.ia.cuerposAutonomos(), "\n")
	if strings.Contains(cuerpos, "000-777777") || strings.Contains(cuerpos, "Internet del Norte") || !strings.Contains(cuerpos, mensajeria.TextoCuentasTapado) {
		t.Fatalf("el modelo no debía ver números ni titulares:\n%s", cuerpos)
	}

	// Número desconocido: la herramienta no prepara la marca y un mensaje con ella no sale.
	otro := numeroCliente()
	c2 := x.llega(otro, "wamid.dc3", "¿A qué cuenta deposito?", "Pagos y saldo")
	x.guion(llama("cuentas_para_boleta", `{}`), dice("Aquí están: {{cuentas}}"))
	x.correr(c2)
	for _, m := range x.mensajes(x.supervisor, c2) {
		if strings.Contains(textoDe(m), "000-777777") {
			t.Fatal("a un número desconocido no se le dan las cuentas")
		}
	}
	if !strings.Contains(strings.Join(x.ia.cuerposAutonomos(), "\n"), "su número no está registrado") {
		t.Fatal("la herramienta le dice al modelo que no las dé")
	}
}

// Textos con números de cuenta escritos a mano (§10.3, filas 12 a 14): solo se avisa, nunca se
// bloquea guardar y nunca se devuelve el número.
func TestRevisionTextosConCuentas(t *testing.T) {
	if m := mensajeria.MotivoNumeroCuenta("Deposite a la cuenta monetaria 000-123456-7 del BI", nil); m != "parece_cuenta" {
		t.Fatalf("número junto a «cuenta»: %q", m)
	}
	if m := mensajeria.MotivoNumeroCuenta("Llámenos al 2245 0000 o escriba al 5512 0102", nil); m != "" {
		t.Fatalf("un teléfono sin «cuenta» no avisa: %q", m)
	}
	if m := mensajeria.MotivoNumeroCuenta("Transfiera a 3445009876", []string{"3-445-00987-6"}); m != "cuenta_de_argos" {
		t.Fatalf("una cuenta de Argos: %q", m)
	}
	b := nuevaBandeja(t, nil)
	if _, err := b.bd.Exec(b.ctx, `INSERT INTO mensajeria.respuestas_rapidas (empresa_id, atajo, titulo, texto) VALUES (1, '/cuentasb5', 'Cuentas', 'Nuestra cuenta monetaria BI 000-123456-7')`); err != nil {
		t.Fatal(err)
	}
	r := b.api(b.supervisor, http.MethodGet, "/revision-cuentas", nil)
	b.exigir(r, http.StatusOK)
	if !strings.Contains(fmt.Sprint(r.cuerpo["data"]), "/cuentasb5") || strings.Contains(fmt.Sprint(r.cuerpo), "000-123456") {
		t.Fatalf("revisión: %v", r.cuerpo)
	}
	// Quien no configura los números no la ve.
	b.exigir(b.api(b.agente, http.MethodGet, "/revision-cuentas", nil), http.StatusForbidden)
}

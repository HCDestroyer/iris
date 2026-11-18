package mensajeria_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/conector"
	"github.com/HCDestroyer/iris/apps/api/internal/mensajeria"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
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
	marca := func(ref string) string {
		var v, col string
		if err := b.bd.QueryRow(b.ctx, `SELECT coalesce(k.despues #>> '{}', ''), coalesce(bo.cuenta_validacion, '') FROM mensajeria.boletas bo
			JOIN auditoria.cambios k ON k.entidad = 'boleta' AND k.entidad_id = bo.id::text AND k.campo = 'cuenta_validacion' AND (k.antes IS NULL OR k.antes = 'null')
			WHERE bo.conversacion_id = $1::bigint AND bo.referencia = $2`, cid(c), ref).Scan(&v, &col); err != nil {
			t.Fatalf("marca de la boleta %s: %v", ref, err)
		}
		if v != col {
			t.Fatalf("la marca de %s en la fila (%q) y en la auditoría (%q) no coinciden", ref, col, v)
		}
		return v
	}
	fila := func(ref string) (estado, detalle, validacion string) {
		if err := b.bd.QueryRow(b.ctx, `SELECT estado, coalesce(detalle, ''), coalesce(cuenta_validacion, '') FROM mensajeria.boletas
			WHERE conversacion_id = $1::bigint AND referencia = $2`, cid(c), ref).Scan(&estado, &detalle, &validacion); err != nil {
			t.Fatalf("boleta %s: %v", ref, err)
		}
		return
	}
	falla := func(err error) {
		b.argos.mu.Lock()
		b.argos.errCuentas = err
		b.argos.mu.Unlock()
	}
	enviados := func() int64 {
		b.argos.mu.Lock()
		defer b.argos.mu.Unlock()
		return b.argos.comprobante
	}
	// Argos responde un error que no es una caída (403): no se registra.
	falla(&conector.ErrorRemoto{Status: http.StatusForbidden})
	if r := b.api(b.agente, http.MethodPost, ruta+"/boletas", boleta(3)); r.status/100 == 2 || boletas() != 0 {
		t.Fatalf("con un rechazo de Argos al leer las cuentas no se registra: %d %v", r.status, r.cuerpo)
	}
	falla(nil)
	// M1. Argos responde 200 pero cambió un tipo ("activa":"true"): respuesta ilegible. No es una
	// caída: 502 y ninguna boleta (si no, todas pasarían sin validar con solo un aviso en el log).
	b.argos.mu.Lock()
	b.argos.cuentasExtra = `,{"id":11,"empresa_id":1,"banco_id":1,"banco_nombre":"Banco Industrial","numero":"1","alias":"Tipo cambiado","moneda":"GTQ","activa":"true"}`
	b.argos.mu.Unlock()
	b.exigir(b.api(b.agente, http.MethodPost, ruta+"/boletas", boleta(3)), http.StatusBadGateway, "type", "urn:iris:problema:argos_respuesta_ilegible")
	b.exigir(b.api(b.agente, http.MethodGet, "/conversaciones/cuentas-bancarias", nil), http.StatusBadGateway, "type", "urn:iris:problema:argos_respuesta_ilegible")
	if boletas() != 0 {
		t.Fatal("con una respuesta ilegible de Argos no se registra la boleta")
	}
	b.argos.mu.Lock()
	b.argos.cuentasExtra = cuentasQueNoSon
	b.argos.mu.Unlock()
	// Argos caído (regla 4): la boleta se registra igual y sigue su camino de reintentos; queda
	// marcada (fila y auditoría) como sin validar. Aquí no se puede descartar ni la cuenta ajena.
	falla(&conector.ErrorRemoto{Status: 0})
	b.exigir(b.api(b.agente, http.MethodPost, ruta+"/boletas", boleta(3)), http.StatusCreated, "estado", "por_enviar")
	falla(&conector.ErrorRemoto{Status: http.StatusBadGateway})
	b.exigir(b.api(b.agente, http.MethodPost, ruta+"/boletas", boleta(8)), http.StatusCreated, "estado", "por_enviar")
	// S1. Con Argos en 429 y una cuenta inactiva de la misma empresa: también se registra sin validar.
	falla(&conector.ErrorRemoto{Status: http.StatusTooManyRequests})
	b.exigir(b.api(b.agente, http.MethodPost, ruta+"/boletas", boleta(10)), http.StatusCreated, "estado", "por_enviar")
	for _, ref := range []string{"CU-3", "CU-8", "CU-10"} {
		if m := marca(ref); m != "sin_validar_argos_no_disponible" {
			t.Fatalf("con Argos caído %s se registra sin validar: %q", ref, m)
		}
	}
	// Conector con Argos apagado: Iris sigue trabajando; la boleta queda por enviar con su marca.
	falla(conector.ConectorApagado("Argos"))
	b.exigir(b.api(b.agente, http.MethodPost, ruta+"/boletas", boleta(4)), http.StatusCreated, "estado", "por_enviar")
	if boletas() != 4 || marca("CU-4") != "sin_validar_conector_apagado" {
		t.Fatalf("con el conector apagado la boleta se registra sin validar: %d %q", boletas(), marca("CU-4"))
	}
	// El despacho con Argos todavía sin responder: no manda ningún comprobante y sigue el reintento.
	falla(&conector.ErrorRemoto{Status: http.StatusTooManyRequests})
	antes := enviados()
	if err := b.m.DespacharBoletas(b.ctx, b.bd); err != nil {
		t.Fatal(err)
	}
	if e, _, v := fila("CU-10"); e != "por_enviar" || v != "sin_validar_argos_no_disponible" || enviados() != antes {
		t.Fatalf("con Argos sin responder la boleta sigue por enviar y sin POST: %s %s, comprobantes %d → %d", e, v, antes, enviados())
	}
	falla(nil)
	// Con Argos arriba, la cuenta sin alias de la empresa se valida y se registra.
	b.exigir(b.api(b.agente, http.MethodPost, ruta+"/boletas", boleta(5)), http.StatusCreated, "estado", "por_enviar")
	if boletas() != 5 || marca("CU-5") != "validada" {
		t.Fatalf("la boleta con una cuenta de la empresa se registra validada: %d %q", boletas(), marca("CU-5"))
	}
	// La lista por conversación: las cuentas de la empresa de esa conversación; una que no existe, 404.
	lc := b.api(b.agente, http.MethodGet, "/conversaciones/cuentas-bancarias?conversacionId="+cid(c), nil)
	b.exigir(lc, http.StatusOK)
	if d := lc.cuerpo["data"].([]any); len(d) != 2 || d[1].(map[string]any)["nombre"] != "Banrural …9876" {
		t.Fatalf("cuentas de la conversación: %v", lc.cuerpo)
	}
	b.exigir(b.api(b.agente, http.MethodGet, "/conversaciones/cuentas-bancarias?conversacionId=999999999", nil), http.StatusNotFound)
	vencer := func() {
		if _, err := b.bd.Exec(b.ctx, `UPDATE mensajeria.boletas SET proximo_intento = '2000-01-01' WHERE conversacion_id = $1::bigint AND estado = 'por_enviar'`, cid(c)); err != nil {
			t.Fatal(err)
		}
	}
	// El despacho con una respuesta ilegible de Argos: detalle propio (no «Argos no respondió»), sin POST.
	b.argos.mu.Lock()
	b.argos.cuentasExtra = `,{"id":11,"empresa_id":"1","activa":"true"}`
	b.argos.mu.Unlock()
	vencer()
	antes = enviados()
	if err := b.m.DespacharBoletas(b.ctx, b.bd); err != nil {
		t.Fatal(err)
	}
	if e, d, _ := fila("CU-10"); e != "por_enviar" || d != mensajeria.DetalleArgosIlegible || enviados() != antes+1 {
		// Solo la CU-5, ya validada, no necesita leer las cuentas y se manda.
		t.Fatalf("respuesta ilegible en el despacho: %s %q, comprobantes %d", e, d, enviados()-antes)
	}
	b.argos.mu.Lock()
	b.argos.cuentasExtra = cuentasQueNoSon
	b.argos.mu.Unlock()
	// Boletas anteriores a la marca (cuenta_validacion nula) todavía por enviar: se revalidan igual.
	if _, err := b.bd.Exec(b.ctx, `UPDATE mensajeria.boletas SET cuenta_validacion = NULL WHERE conversacion_id = $1::bigint AND referencia IN ('CU-8', 'CU-3')`, cid(c)); err != nil {
		t.Fatal(err)
	}
	// S1. El despacho con Argos arriba revalida las que quedaron sin validar antes del POST
	// /comprobantes: la ajena (8), la inactiva (10) y la que está en dólares (4) quedan fallidas con
	// su motivo y sin POST; la buena (3) se valida y se manda, igual que la ya validada (5).
	vencer()
	// Dos corridas solapadas: mientras «Argos» atiende el POST de una boleta, otro despacho de la
	// misma boleta no la toma (está reservada): un solo POST por boleta.
	var solapadas []error
	b.argos.mu.Lock()
	b.argos.alComprobante = func() {
		filas, err := b.bd.Query(b.ctx, `SELECT id FROM mensajeria.boletas WHERE conversacion_id = $1::bigint`, cid(c))
		if err != nil {
			solapadas = append(solapadas, err)
			return
		}
		var ids []uuid.UUID
		for filas.Next() {
			var id uuid.UUID
			if err := filas.Scan(&id); err != nil {
				solapadas = append(solapadas, err)
			}
			ids = append(ids, id)
		}
		filas.Close()
		b.argos.mu.Lock()
		b.argos.alComprobante = nil
		b.argos.mu.Unlock()
		for _, id := range ids {
			if err := b.m.EnviarBoleta(b.ctx, id); err != nil {
				solapadas = append(solapadas, err)
			}
		}
	}
	b.argos.mu.Unlock()
	antes = enviados()
	if err := b.m.DespacharBoletas(b.ctx, b.bd); err != nil {
		t.Fatal(err)
	}
	for _, ref := range []string{"CU-8", "CU-10", "CU-4"} {
		if e, d, _ := fila(ref); e != "fallida" || d != mensajeria.MotivoCuentaBoleta {
			t.Fatalf("%s debía quedar fallida por la cuenta: %s %q", ref, e, d)
		}
	}
	for _, ref := range []string{"CU-3", "CU-5"} {
		if e, _, v := fila(ref); e != "registrada" || v != "validada" {
			t.Fatalf("%s debía mandarse a Argos ya validada: %s %s", ref, e, v)
		}
	}
	if len(solapadas) != 0 {
		t.Fatalf("el despacho solapado: %v", solapadas)
	}
	// CU-5 ya salió en el despacho anterior; aquí solo CU-3, una sola vez aunque hubo dos corridas.
	if enviados() != antes+1 {
		t.Fatalf("solo la boleta con cuenta válida hace POST /comprobantes, y una sola vez: %d", enviados()-antes)
	}
	if n := b.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'boleta.fallida' AND payload->>'conversacion_id' = $1`, cid(c)); n != 3 {
		t.Fatalf("boleta.fallida: %d", n)
	}
}

// S2 y M1, la decisión sola: sin empresa (0) nunca se registra una boleta con cuenta bancaria, ni con
// Argos caído ni con el conector apagado; una respuesta ilegible no es una caída.
func TestDecidirCuentaBoleta(t *testing.T) {
	var cs []argos.CuentaBancariaArgos
	if err := json.Unmarshal([]byte(`[{"id":3,"empresa_id":1,"banco_nombre":"Banco Industrial","numero":"1","alias":"Monetaria","moneda":"GTQ","activa":true}]`), &cs); err != nil {
		t.Fatal(err)
	}
	es422 := func(err error) bool {
		var p *nucleo.Problema
		return errors.As(err, &p) && p.Status == http.StatusUnprocessableEntity
	}
	caido, limite, mal := &conector.ErrorRemoto{Status: 0}, &conector.ErrorRemoto{Status: http.StatusTooManyRequests}, &conector.ErrorRemoto{Status: http.StatusInternalServerError}
	ilegible := &conector.ErrorRemoto{Status: http.StatusBadGateway, Titulo: "Respuesta ilegible", Ilegible: true}
	apagado := conector.ConectorApagado("Argos")
	// Empresa 0: siempre 422, en todas las ramas.
	for nombre, err := range map[string]error{"arriba": nil, "caido": caido, "429": limite, "500": mal, "apagado": apagado, "ilegible": ilegible} {
		if m, e := mensajeria.DecidirCuenta(0, cs, err, 3); m != "" || !es422(e) {
			t.Fatalf("sin empresa (%s) debía ser 422: %q %v", nombre, m, e)
		}
	}
	// Con empresa: arriba valida; red, 429 y 5xx de Argos y conector apagado registran sin validar.
	if m, e := mensajeria.DecidirCuenta(1, cs, nil, 3); m != mensajeria.CuentaValidada || e != nil {
		t.Fatalf("cuenta de la empresa: %q %v", m, e)
	}
	if m, e := mensajeria.DecidirCuenta(2, cs, nil, 3); m != "" || !es422(e) {
		t.Fatalf("cuenta de otra empresa: %q %v", m, e)
	}
	for _, err := range []error{caido, limite, mal} {
		if m, e := mensajeria.DecidirCuenta(1, nil, err, 3); m != mensajeria.CuentaSinValidar || e != nil {
			t.Fatalf("Argos sin responder (%v): %q %v", err, m, e)
		}
	}
	if m, e := mensajeria.DecidirCuenta(1, nil, apagado, 3); m != mensajeria.CuentaSinConector || e != nil {
		t.Fatalf("conector apagado: %q %v", m, e)
	}
	// Ilegible: 502 y no se registra.
	var p *nucleo.Problema
	if m, e := mensajeria.DecidirCuenta(1, nil, ilegible, 3); m != "" || !errors.As(e, &p) || p.Status != http.StatusBadGateway || p.Codigo() != "argos_respuesta_ilegible" {
		t.Fatalf("respuesta ilegible: %q %v", m, e)
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
	// Argos caído: el asistente falla cerrado (no registra ni con una cuenta buena de la empresa).
	x.argos.mu.Lock()
	x.argos.errCuentas = &conector.ErrorRemoto{Status: 0}
	x.argos.mu.Unlock()
	x.turno(num, "wamid.cuia2", "Fue a la Monetaria", "Pagos y saldo", pasoFalso{llamadas: []llamadaFalsa{registrar(3)}}, dice("Te paso con una persona."))
	if a := strings.Join(x.acciones(conv), ","); !strings.Contains(a, "recibir_boleta:fallo") || strings.Contains(a, "recibir_boleta:ok") {
		t.Fatalf("con Argos caído el asistente no registra: %s", a)
	}
	if n := x.contar(`SELECT count(*) FROM mensajeria.boletas WHERE conversacion_id = $1::bigint`, conv); n != 0 {
		t.Fatalf("el asistente no registra sin poder leer las cuentas: %d", n)
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

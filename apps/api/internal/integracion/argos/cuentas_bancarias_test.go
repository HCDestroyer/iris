package argos_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/conector"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
)

// Cuentas bancarias de Argos como instantánea de lectura (contrato 0.8.0 §7.8, B5), contra un Argos
// HTTP falso que responde la forma real: GET /cuentas-bancarias es un arreglo plano sin paginar (con
// activas e inactivas sin filtro) y GET /cuentas-bancarias/:id responde la cuenta, 404 o 403.

// cuentasFalsas es el catálogo de cuentas del Argos falso.
type cuentasFalsas struct {
	mu      sync.Mutex
	cuentas map[int64]map[string]any
	// fuera: ids de cuentas de otra empresa (403 empresa_no_permitida en GET por id).
	fuera map[int64]bool
	// pedidos: «ruta?consulta actor=… empresa=…» de cada petición.
	pedidos []string
	// status: si ≠ 0, todas las lecturas responden ese status; crudo: un cuerpo 200 forzado.
	status int
	crudo  string
}

func nuevasCuentasFalsas(e *entorno) *cuentasFalsas {
	c := &cuentasFalsas{cuentas: map[int64]map[string]any{}, fuera: map[int64]bool{}}
	e.argos.con(func() {
		e.argos.rutaExtra = func(w http.ResponseWriter, r *http.Request) {
			c.mu.Lock()
			defer c.mu.Unlock()
			c.pedidos = append(c.pedidos, r.URL.Path+"?"+r.URL.RawQuery+" actor="+r.Header.Get("X-Iris-Actor")+" empresa="+r.Header.Get("X-Empresa-Id"))
			if c.status != 0 {
				problema(w, c.status, "urn:argosisp:problema:forzado", "forzado")
				return
			}
			w.Header().Set("Content-Type", "application/json")
			if c.crudo != "" {
				_, _ = w.Write([]byte(c.crudo))
				return
			}
			if id := r.PathValue("id"); id != "" {
				n, _ := strconv.ParseInt(id, 10, 64)
				if c.fuera[n] {
					problema(w, http.StatusForbidden, "urn:argosisp:problema:empresa_no_permitida", "Fuera del alcance")
					return
				}
				k, ok := c.cuentas[n]
				if !ok {
					problema(w, http.StatusNotFound, "urn:argosisp:problema:no_encontrado", "No existe")
					return
				}
				_ = json.NewEncoder(w).Encode(k)
				return
			}
			ids := []int64{}
			for id := range c.cuentas {
				if !c.fuera[id] {
					ids = append(ids, id)
				}
			}
			sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
			lista := []map[string]any{}
			for _, id := range ids {
				lista = append(lista, c.cuentas[id])
			}
			_ = json.NewEncoder(w).Encode(lista)
		}
	})
	return c
}

func (c *cuentasFalsas) poner(id int64, campos map[string]any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	k := map[string]any{"id": id, "empresa_id": 1, "banco_id": 1, "banco_nombre": "Banco Industrial", "numero": "000-123456-" + strconv.FormatInt(id, 10),
		"alias": "Monetaria " + strconv.FormatInt(id, 10), "moneda": "GTQ", "activa": true,
		"cuenta_id": 118, "cuenta_codigo": "1.1.02.03", "version": 4, "created_at": "2026-03-02T15:00:00.000Z", "updated_at": "2026-11-18T21:20:00.000Z"}
	for n, v := range campos {
		if v == nil {
			delete(k, n)
		} else {
			k[n] = v
		}
	}
	c.cuentas[id] = k
}

func (c *cuentasFalsas) ultimos(n int) []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if n > len(c.pedidos) {
		n = len(c.pedidos)
	}
	return append([]string(nil), c.pedidos[len(c.pedidos)-n:]...)
}

func (c *cuentasFalsas) cuantos() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.pedidos)
}

func (c *cuentasFalsas) con(fn func()) {
	c.mu.Lock()
	defer c.mu.Unlock()
	fn()
}

// conectado080 conecta con un Argos 1.18.0 que informa el contrato 0.8.0 (Iris sigue informando
// el suyo, 0.7.1: la constante la sube el líder al juntar B4, B5 y B7).
func (e *entorno) conectado080() (*clienteHTTP, int64, string) {
	e.t.Helper()
	e.argos.con(func() { e.argos.version, e.argos.panelURL, e.argos.contratoMax = "1.18.0", panelArgos, "0.8.0" })
	admin, id, secreto := e.conectado()
	if _, err := e.mod.InformarContrato.Ejecutar(pruebas.ContextoSistema(e.pool), id); err != nil {
		e.t.Fatal(err)
	}
	if c := e.consultarTexto(`SELECT coalesce(contrato_remoto, '') FROM integracion.conectores WHERE id = $1`, id); c != "0.8.0" {
		e.t.Fatalf("el conector debía quedar con el contrato de Argos 0.8.0: %q", c)
	}
	return admin, id, secreto
}

func (e *entorno) envejecerCuentas() {
	e.t.Helper()
	if _, err := e.pool.Exec(e.t.Context(), `UPDATE integracion.argos_cuentas_lecturas SET leido_en = now() - interval '25 hours'`); err != nil {
		e.t.Fatal(err)
	}
}

func (e *entorno) eventosCuenta(cuenta int64) []map[string]any {
	e.t.Helper()
	filas, err := e.pool.Query(e.t.Context(), `SELECT payload FROM integracion.outbox WHERE evento = 'argos.cuenta_bancaria_cambiada'
		AND (payload->>'cuenta_bancaria_id')::bigint = $1 ORDER BY id`, cuenta)
	if err != nil {
		e.t.Fatal(err)
	}
	defer filas.Close()
	var out []map[string]any
	for filas.Next() {
		var p map[string]any
		if err := filas.Scan(&p); err != nil {
			e.t.Fatal(err)
		}
		out = append(out, p)
	}
	return out
}

// Con un Argos anterior a 0.8.0 (contrato 0.7.x) la lista exige las cabeceras de una conversación: el
// job no la pide (omitido) y la instantánea se llena desde la conversación, con X-Iris-Actor y la
// empresa, sin `activa` (para enterarse también de las desactivadas) y sin limit. Mientras la lista
// de la empresa tiene menos de 24 h no se vuelve a pedir. Un 200 ilegible no es una caída; con la
// lista vieja y Argos caído, las cuentas no se ofrecen (ErrCuentasViejas).
func TestCuentasBancariasArgosAnterior080(t *testing.T) {
	e := nuevoEntorno(t)
	e.conectado070()
	cf := nuevasCuentasFalsas(e)
	cf.poner(9101, nil)
	cf.poner(9102, map[string]any{"alias": "", "banco_id": 2, "banco_nombre": "Banrural", "numero": "3-445-00987-6"})
	cf.poner(9103, map[string]any{"activa": false})
	cf.poner(9104, map[string]any{"empresa_id": 2})
	ctx := nucleo.ConBD(pruebas.ContextoSistema(e.pool), e.pool)

	r, err := e.mod.CargarCuentasBancarias(ctx)
	if err != nil || r.Omitido != argos.OmitidoArgosAnterior080 || cf.cuantos() != 0 {
		t.Fatalf("con Argos < 0.8.0 el job no pide la lista: %+v %v (%d pedidos)", r, err, cf.cuantos())
	}

	at := argos.AtencionIris{Actor: "usuario:1", Conversacion: "iris:conversacion:1", EmpresaID: 1}
	ce, err := e.mod.CuentasDeEmpresa(ctx, at)
	if err != nil {
		t.Fatal(err)
	}
	if p := cf.ultimos(1)[0]; p != "/api/v1/cuentas-bancarias? actor=usuario:1 empresa=1" {
		t.Fatalf("la petición: %s", p)
	}
	if len(ce.Cuentas) != 3 || ce.LeidoEn == nil {
		t.Fatalf("las tres de la empresa 1 (activas e inactivas), nunca la de la empresa 2: %+v", ce)
	}
	if k := ce.Cuentas[0]; k.ID != 9101 || k.Nombre != "Monetaria 9101" || k.Titular != nil || k.Tipo != nil || !k.RecibeDe(1) {
		t.Fatalf("cuenta sin titular ni tipo (Argos < 0.8.0): %+v", k)
	}
	if n := e.contar(`SELECT count(*) FROM integracion.argos_cuentas_bancarias WHERE id BETWEEN 9101 AND 9104`); n != 3 {
		t.Fatalf("instantánea: %d filas", n)
	}
	// Los campos contables no entran a la instantánea (la tabla no los tiene) y el número no va al outbox.
	if n := e.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'argos.cuenta_bancaria_cambiada' AND payload::text LIKE '%000-123456%'`); n != 0 {
		t.Fatal("el número de cuenta no va en los eventos")
	}
	if ev := e.eventosCuenta(9101); len(ev) != 1 || ev[0]["creada"] != true || ev[0]["entrante_id"] != nil {
		t.Fatalf("evento de la cuenta nueva: %v", ev)
	}
	// Antes de 24 h: de la instantánea, sin pedir a Argos.
	antes := cf.cuantos()
	if _, err := e.mod.CuentasDeEmpresa(ctx, at); err != nil || cf.cuantos() != antes {
		t.Fatalf("con menos de 24 h no se relee: %v (%d → %d)", err, antes, cf.cuantos())
	}
	// Con más de 24 h: se relee. Un 200 que no se puede leer: ilegible (no caído).
	e.envejecerCuentas()
	cf.con(func() { cf.crudo = `[{"id":9101,"empresa_id":1,"numero":"1","activa":"true"}]` })
	if _, err := e.mod.CuentasDeEmpresa(ctx, at); !conector.EsIlegible(err) {
		t.Fatalf("un 200 ilegible: %v", err)
	}
	// Argos caído con la lista vieja: no se ofrecen (ErrCuentasViejas, que es también una caída).
	cf.con(func() { cf.crudo, cf.status = "", http.StatusServiceUnavailable })
	if _, err := e.mod.CuentasDeEmpresa(ctx, at); !errors.Is(err, argos.ErrCuentasViejas) || !conector.EsCaido(err) {
		t.Fatalf("Argos caído y lista de más de 24 h: %v", err)
	}
	// Sin empresa: ninguna cuenta y sin llamar a Argos.
	antes = cf.cuantos()
	if ce, err := e.mod.CuentasDeEmpresa(ctx, argos.AtencionIris{Actor: "usuario:1", Conversacion: "iris:conversacion:1"}); err != nil || len(ce.Cuentas) != 0 || cf.cuantos() != antes {
		t.Fatalf("sin empresa: %+v %v", ce, err)
	}
}

// Con Argos 0.8.0: la carga completa sin cabeceras trae titular y tipo, activas e inactivas; cada
// cambio emite argos.cuenta_bancaria_cambiada con los nombres de los campos (nunca valores); una
// cuenta que sale del alcance se quita. La relectura antes de una boleta (GET /:id) refresca la
// instantánea; un 403 la quita. El webhook cuenta_bancaria.cambiada relee la cuenta y guarda lo que
// dice Argos, no el payload, y se procesa una sola vez.
func TestCuentasBancariasContrato080(t *testing.T) {
	e := nuevoEntorno(t)
	_, conID, secreto := e.conectado080()
	cf := nuevasCuentasFalsas(e)
	cf.poner(9201, map[string]any{"titular": "Internet del Norte, S.A.", "tipo": "monetaria"})
	cf.poner(9202, map[string]any{"titular": "  ", "tipo": "otro"})
	cf.poner(9203, map[string]any{"titular": "Internet del Norte, S.A.", "tipo": "ahorro", "activa": false})
	ctx := nucleo.ConBD(pruebas.ContextoSistema(e.pool), e.pool)

	r, err := e.mod.CargarCuentasBancarias(ctx)
	if err != nil || r.Omitido != "" || r.Cuentas != 3 || r.Cambiadas != 3 {
		t.Fatalf("carga completa: %+v %v", r, err)
	}
	if p := cf.ultimos(1)[0]; p != "/api/v1/cuentas-bancarias? actor= empresa=" {
		t.Fatalf("la lista va sin cabeceras de Iris ni filtro: %s", p)
	}
	var titular, tipo *string
	if err := e.pool.QueryRow(t.Context(), `SELECT titular, tipo FROM integracion.argos_cuentas_bancarias WHERE id = 9201`).Scan(&titular, &tipo); err != nil ||
		titular == nil || *titular != "Internet del Norte, S.A." || tipo == nil || *tipo != "monetaria" {
		t.Fatalf("titular y tipo de la 9201: %v %v %v", titular, tipo, err)
	}
	if err := e.pool.QueryRow(t.Context(), `SELECT titular, tipo FROM integracion.argos_cuentas_bancarias WHERE id = 9202`).Scan(&titular, &tipo); err != nil || titular != nil || tipo != nil {
		t.Fatalf("titular vacío y tipo desconocido quedan nulos: %v %v %v", titular, tipo, err)
	}
	if n := e.contar(`SELECT count(*) FROM integracion.argos_cuentas_bancarias WHERE id = 9203 AND NOT activa`); n != 1 {
		t.Fatal("la inactiva también está en la instantánea")
	}
	// Repetir sin cambios: sin eventos ni auditoría nueva.
	ev := e.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'argos.cuenta_bancaria_cambiada'`)
	if r, err := e.mod.CargarCuentasBancarias(ctx); err != nil || r.Cambiadas != 0 {
		t.Fatalf("sin cambios: %+v %v", r, err)
	}
	if n := e.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'argos.cuenta_bancaria_cambiada'`); n != ev {
		t.Fatal("una relectura sin cambios no emite eventos")
	}
	// Cambia el titular de la 9202 y la 9203 sale del alcance.
	cf.poner(9202, map[string]any{"titular": "Internet del Norte, S.A."})
	cf.con(func() { cf.fuera[9203] = true })
	if r, err := e.mod.CargarCuentasBancarias(ctx); err != nil || r.Cambiadas != 1 || r.Quitadas != 1 {
		t.Fatalf("cambio y salida del alcance: %+v %v", r, err)
	}
	if evs := e.eventosCuenta(9202); len(evs) != 2 || strings.Join(aTextos(evs[1]["campos"]), ",") != "titular" || evs[1]["creada"] != false {
		t.Fatalf("evento del cambio de titular: %v", evs)
	}
	if n := e.contar(`SELECT count(*) FROM integracion.argos_cuentas_bancarias WHERE id = 9203`); n != 0 {
		t.Fatal("la cuenta fuera del alcance se quita de la instantánea")
	}

	// Relectura antes de registrar una boleta: GET /cuentas-bancarias/9201 (sin cabeceras).
	cf.poner(9201, map[string]any{"titular": "Internet del Norte, S.A.", "tipo": "monetaria", "activa": false})
	k, err := e.mod.ReleerCuentaBancaria(ctx, 9201)
	if err != nil || k.Activa || k.ID != 9201 || k.Titular == nil {
		t.Fatalf("relectura: %+v %v", k, err)
	}
	if p := cf.ultimos(1)[0]; p != "/api/v1/cuentas-bancarias/9201? actor= empresa=" {
		t.Fatalf("la relectura: %s", p)
	}
	if n := e.contar(`SELECT count(*) FROM integracion.argos_cuentas_bancarias WHERE id = 9201 AND NOT activa`); n != 1 {
		t.Fatal("la relectura refresca la instantánea")
	}
	if _, err := e.mod.ReleerCuentaBancaria(ctx, 9299); !conector.EsNoEncontrado(err) {
		t.Fatalf("una cuenta que Argos no tiene: %v", err)
	}
	cf.con(func() { cf.fuera[9202] = true })
	if _, err := e.mod.ReleerCuentaBancaria(ctx, 9202); !argos.EsFueraDeAlcance(err) {
		t.Fatalf("403 empresa_no_permitida: %v", err)
	}
	if n := e.contar(`SELECT count(*) FROM integracion.argos_cuentas_bancarias WHERE id = 9202`); n != 0 {
		t.Fatal("una cuenta fuera del alcance se quita")
	}

	// Webhook: Argos manda solo nombres de campos; Iris relee la cuenta. El payload dice activa: true,
	// pero Argos (la fuente) la tiene inactiva: gana Argos.
	cf.con(func() { delete(cf.fuera, 9202) })
	cf.poner(9204, map[string]any{"titular": "Internet del Norte, S.A.", "tipo": "ahorro"})
	antes := cf.cuantos()
	res := e.procesarWebhook(conID, secreto, argos.EventoCuentaBancariaCambiada, map[string]any{"cuenta_bancaria_id": 9204, "empresa_id": 1,
		"campos": []string{"banco_id", "numero", "alias", "titular", "tipo", "moneda", "activa", "otro"}, "activa": true, "creada": true})
	if res.Resultado != "aplicado" || cf.cuantos() != antes+1 || cf.ultimos(1)[0] != "/api/v1/cuentas-bancarias/9204? actor= empresa=" {
		t.Fatalf("webhook de una cuenta nueva: %+v %v", res, cf.ultimos(1))
	}
	evs := e.eventosCuenta(9204)
	if len(evs) != 1 || evs[0]["creada"] != true || evs[0]["entrante_id"] == nil || strings.Join(aTextos(evs[0]["campos"]), ",") != "banco_id,numero,alias,titular,tipo,moneda,activa" {
		t.Fatalf("evento del webhook (campos del contrato, sin valores): %v", evs)
	}
	cf.poner(9204, map[string]any{"titular": "Internet del Norte, S.A.", "tipo": "ahorro", "activa": false})
	e.procesarWebhook(conID, secreto, argos.EventoCuentaBancariaCambiada, map[string]any{"cuenta_bancaria_id": 9204, "empresa_id": 1, "campos": []string{"activa"}, "activa": true, "creada": false})
	if n := e.contar(`SELECT count(*) FROM integracion.argos_cuentas_bancarias WHERE id = 9204 AND NOT activa`); n != 1 {
		t.Fatal("el webhook guarda lo que dice Argos, no el payload")
	}
	// Una cuenta que Argos no tiene: omitido, sin cambios.
	ent := e.entranteDe(e.enviar(conID, secreto, "wh-b5-"+pruebas.Unico(), argos.EventoCuentaBancariaCambiada, map[string]any{"cuenta_bancaria_id": 9298, "empresa_id": 1, "campos": []string{"alias"}, "activa": true, "creada": false}, time.Now()))
	if r, err := e.mod.ProcesarEntrante.Ejecutar(pruebas.ContextoSistema(e.pool), ent); err != nil || r.Resultado != "ignorado" || e.omitidoDe(ent) != "cuenta_no_encontrada" {
		t.Fatalf("cuenta inexistente: %+v %v %q", r, err, e.omitidoDe(ent))
	}
	// El mismo entrante otra vez: ya procesado (consumidor idempotente).
	if r, err := e.mod.ProcesarEntrante.Ejecutar(pruebas.ContextoSistema(e.pool), ent); err != nil || r.Resultado != "ya_procesado" {
		t.Fatalf("idempotente: %+v %v", r, err)
	}
	// Argos caído al releer: el job se reintenta (error), el entrante queda sin procesar.
	cf.con(func() { cf.status = http.StatusServiceUnavailable })
	ent = e.entranteDe(e.enviar(conID, secreto, "wh-b5-"+pruebas.Unico(), argos.EventoCuentaBancariaCambiada, map[string]any{"cuenta_bancaria_id": 9204, "empresa_id": 1, "campos": []string{"alias"}, "activa": true, "creada": false}, time.Now()))
	if _, err := e.mod.ProcesarEntrante.Ejecutar(pruebas.ContextoSistema(e.pool), ent); err == nil {
		t.Fatal("con Argos caído el webhook se reintenta")
	}
	cf.con(func() { cf.status = 0 })
	if r, err := e.mod.ProcesarEntrante.Ejecutar(pruebas.ContextoSistema(e.pool), ent); err != nil || r.Resultado != "aplicado" {
		t.Fatalf("reintento: %+v %v", r, err)
	}
	// Nada del número de cuenta ni del titular en eventos ni auditoría.
	if n := e.contar(`SELECT count(*) FROM integracion.outbox WHERE payload::text LIKE '%000-123456%' OR payload::text LIKE '%Internet del Norte%'`); n != 0 {
		t.Fatal("el número y el titular no van en los eventos")
	}
	if n := e.contar(`SELECT count(*) FROM auditoria.acciones WHERE entrada::text LIKE '%000-123456%' OR entrada::text LIKE '%Internet del Norte%'`); n != 0 {
		t.Fatal("el número y el titular no van en la auditoría")
	}
}

func aTextos(v any) []string {
	out := []string{}
	if l, ok := v.([]any); ok {
		for _, x := range l {
			if s, ok := x.(string); ok {
				out = append(out, s)
			}
		}
	}
	return out
}

// POST /comprobantes con una cuenta desactivada: 422 validacion con rule «inactiva» (0.8.0); con
// «exists» (Argos anterior) no se distingue.
func TestCuentaInactivaAlRegistrar(t *testing.T) {
	inactiva := &conector.ErrorRemoto{Status: http.StatusUnprocessableEntity, Campos: []string{"cuenta_bancaria_id"}, Reglas: []string{"inactiva"}}
	existe := &conector.ErrorRemoto{Status: http.StatusUnprocessableEntity, Campos: []string{"cuenta_bancaria_id"}, Reglas: []string{"exists"}}
	if !argos.EsCuentaInactiva(inactiva) || argos.EsCuentaInactiva(existe) || argos.EsCuentaInactiva(errors.New("x")) {
		t.Fatal("EsCuentaInactiva")
	}
}

// Una cuenta de una empresa fuera del alcance del conector no entra a la instantánea, ni por la lista
// completa ni por el webhook (y si estaba, se quita).
func TestCuentasBancariasFueraDeAlcance(t *testing.T) {
	e := nuevoEntorno(t)
	e.argos.con(func() { conAlcance(false, map[string]any{"id": 1, "nombre": "Cable Norte", "nit": "1"})(e.argos) })
	_, conID, secreto := e.conectado080()
	cf := nuevasCuentasFalsas(e)
	cf.poner(9301, map[string]any{"titular": "Cable Norte, S.A.", "tipo": "monetaria"})
	cf.poner(9302, map[string]any{"empresa_id": 7, "titular": "Otra, S.A.", "tipo": "ahorro"})
	ctx := nucleo.ConBD(pruebas.ContextoSistema(e.pool), e.pool)
	r, err := e.mod.CargarCuentasBancarias(ctx)
	if err != nil || r.FueraDeAlcance != 1 {
		t.Fatalf("carga completa: %+v %v", r, err)
	}
	if n := e.contar(`SELECT count(*) FROM integracion.argos_cuentas_bancarias WHERE id IN (9301, 9302)`); n != 1 {
		t.Fatalf("solo la de la empresa del alcance: %d", n)
	}
	// Una fila vieja de la empresa 7 (de antes) se quita en la siguiente lista completa.
	if _, err := e.pool.Exec(t.Context(), `INSERT INTO integracion.argos_cuentas_bancarias (id, empresa_id, numero, activa, moneda, sincronizado_en)
		VALUES (9302, 7, '1', true, 'GTQ', now() - interval '1 hour')`); err != nil {
		t.Fatal(err)
	}
	if _, err := e.mod.CargarCuentasBancarias(ctx); err != nil {
		t.Fatal(err)
	}
	if n := e.contar(`SELECT count(*) FROM integracion.argos_cuentas_bancarias WHERE id = 9302`); n != 0 {
		t.Fatal("la cuenta fuera del alcance se quita")
	}
	// Webhook de la cuenta de la empresa 7: omitido, sin guardar.
	ent := e.entranteDe(e.enviar(conID, secreto, "wh-b5-"+pruebas.Unico(), argos.EventoCuentaBancariaCambiada,
		map[string]any{"cuenta_bancaria_id": 9302, "empresa_id": 7, "campos": []string{"alias"}, "activa": true, "creada": true}, time.Now()))
	if _, err := e.mod.ProcesarEntrante.Ejecutar(pruebas.ContextoSistema(e.pool), ent); err != nil || e.omitidoDe(ent) != "cuenta_fuera_de_alcance" {
		t.Fatalf("webhook fuera del alcance: %v %q", err, e.omitidoDe(ent))
	}
	if n := e.contar(`SELECT count(*) FROM integracion.argos_cuentas_bancarias WHERE id = 9302`); n != 0 {
		t.Fatal("el webhook no guarda una cuenta fuera del alcance")
	}
}

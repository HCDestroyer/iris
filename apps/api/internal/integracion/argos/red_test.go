package argos_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
)

// serviciosIncidente: GET /incidentes-red/:id/servicios?empresa_id=&cursor=&limit= (0.13.0 §3.11).
func (a *argosFalso) serviciosIncidente(w http.ResponseWriter, r *http.Request) {
	ids, ok := a.incidentes[r.PathValue("id")+":"+r.URL.Query().Get("empresa_id")]
	if !ok {
		problema(w, 404, "urn:argosisp:problema:no_encontrado", "No existe")
		return
	}
	if c, ok := a.cursorFijo[r.PathValue("id")]; ok {
		escribir(w, map[string]any{"data": ids, "nextCursor": c})
		return
	}
	desde, _ := strconv.Atoi(r.URL.Query().Get("cursor"))
	limite, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limite <= 0 || limite > 1000 {
		limite = 1000
	}
	hasta := min(desde+limite, len(ids))
	out := map[string]any{"data": ids[desde:hasta], "nextCursor": nil}
	if hasta < len(ids) {
		out["nextCursor"] = strconv.Itoa(hasta)
	}

	escribir(w, out)
}

// §3.11 (A49, A50): caídas repetidas y falla general solo por el webhook firmado, con la empresa en
// el alcance y cada servicio validado; salen como eventos internos argos.* sin nombres ni direcciones.
func TestWebhooksDeRed(t *testing.T) {
	e := nuevoEntorno(t)
	_, id, secreto := e.conectado()
	ctx := pruebas.ContextoSistema(e.pool)
	// 901 en la instantánea (empresa 1); 902 de la empresa 1 solo en Argos; 903 de otra empresa en
	// Argos; 904 no existe; 905 fuera del alcance (403).
	if _, err := e.pool.Exec(ctx, `INSERT INTO nucleo.empresas (id, nombre) VALUES (2, 'Empresa 2') ON CONFLICT (id) DO NOTHING`); err != nil {
		t.Fatal(err)
	}
	if _, err := e.pool.Exec(ctx, `INSERT INTO integracion.argos_clientes (id, empresa_id, nombre, sincronizado_en, empresas) VALUES (9001, 1, 'Ana', now(), '{1}')`); err != nil {
		t.Fatal(err)
	}
	if _, err := e.pool.Exec(ctx, `INSERT INTO integracion.argos_servicios (id, empresa_id, cliente_id, codigo, plan_nombre, estado, sincronizado_en)
		VALUES (901, 1, 9001, 'S-901', 'Fibra', 'activo', now())`); err != nil {
		t.Fatal(err)
	}
	e.argos.con(func() {
		e.argos.servicios[9002] = []map[string]any{{"id": int64(902), "empresa_id": int64(1), "cliente_id": int64(9002), "estado": "activo"},
			{"id": int64(903), "empresa_id": int64(2), "cliente_id": int64(9002), "estado": "activo"}}
		e.argos.fueraDeAlcance["/api/v1/servicios/905"] = true
		e.argos.incidentes = map[string][]int64{"inc-7:1": {902, 906}}
	})
	payloadDe := func(evento string) map[string]any {
		var p map[string]any
		if err := json.Unmarshal([]byte(e.consultarTexto(`SELECT payload::text FROM integracion.outbox WHERE evento = $1 ORDER BY created_at DESC LIMIT 1`, evento)), &p); err != nil {
			t.Fatal(err)
		}
		return p
	}
	caidas := map[string]any{"servicio_id": 901, "empresa_id": 1, "cliente_id": 9001, "router_id": 33, "sesion_id": 1, "sesiones": 4,
		"desde": "2026-11-21T15:00:00Z", "hasta": "2026-11-21T15:40:00Z", "umbral": 3, "ventana_minutos": 60, "causas": []string{"lost-carrier"}}
	r := e.procesarWebhook(id, secreto, argos.EventoCaidasRepetidas, caidas)
	if r.Resultado != "aplicado" {
		t.Fatalf("caídas: %+v", r)
	}
	if p := payloadDe(argos.EventoInternoCaidas); p["servicio_id"] != float64(901) || p["sesiones"] != float64(4) || p["entrante_id"] != float64(r.EntranteID) {
		t.Fatalf("argos.caidas_repetidas: %v", p)
	}
	// Servicio de otra empresa (leído en Argos): se ignora.
	caidas["servicio_id"] = 903
	if r := e.procesarWebhook(id, secreto, argos.EventoCaidasRepetidas, caidas); r.Resultado != "ignorado" || e.omitidoDe(r.EntranteID) != "servicio_de_otra_empresa" {
		t.Fatalf("otra empresa: %+v %s", r, e.omitidoDe(r.EntranteID))
	}

	// Falla general: la apertura sale enseguida solo con la instantánea, sin leer Argos.
	falla := map[string]any{"incidente_id": "inc-7", "empresa_id": 1, "alcance": "router", "alcance_id": 33,
		"servicios_afectados": map[string]any{"cantidad": 7, "ids": []int64{901, 903, 904, 905, 901}}, "desde": "2026-11-21T15:00:00Z", "hasta": nil}
	antes := e.argos.llamadasA("/api/v1/servicios/903") + e.argos.llamadasA("/api/v1/incidentes-red/inc-7/servicios")
	r = e.procesarWebhook(id, secreto, argos.EventoFallaGeneralAbierta, falla)
	if r.Resultado != "aplicado" {
		t.Fatalf("falla: %+v", r)
	}
	if e.argos.llamadasA("/api/v1/servicios/903")+e.argos.llamadasA("/api/v1/incidentes-red/inc-7/servicios") != antes {
		t.Fatal("la apertura no lee Argos")
	}
	p := payloadDe(argos.EventoInternoFallaAbierta)
	if fmt.Sprint(p["servicios"]) != "[901]" || p["descartados"] != float64(0) || p["por_verificar"] != float64(3) || p["paginando"] != true ||
		p["alcance_id"] != "33" || p["incidente_id"] != "inc-7" {
		t.Fatalf("argos.falla_general_abierta: %v", p)
	}
	if _, nombre := p["nombre"]; nombre {
		t.Fatal("sin nombres")
	}
	// El job lee la página que falta (un error ahí reintenta solo la lectura paginada) y verifica uno
	// por uno: 902 es de la empresa; 903 de otra; 904 y 906 no existen; 905 fuera del alcance.
	e.argos.con(func() { e.argos.caido = true })
	if _, err := e.mod.VerificarIncidente(ctx, r.EntranteID); err == nil {
		t.Fatal("la página falló: el job se reintenta")
	}
	e.argos.con(func() { e.argos.caido = false })
	if espera, err := e.mod.VerificarIncidente(ctx, r.EntranteID); err != nil || espera != 0 {
		t.Fatalf("verificar: %v %v", espera, err)
	}
	var servicios []int64
	descartados, noVerificados := 0, 0
	filas, err := e.pool.Query(ctx, `SELECT payload FROM integracion.outbox WHERE evento = $1 AND (payload->>'entrante_id')::bigint = $2`,
		argos.EventoInternoFallaServicios, r.EntranteID)
	if err != nil {
		t.Fatal(err)
	}
	for filas.Next() {
		var crudo []byte
		if err := filas.Scan(&crudo); err != nil {
			t.Fatal(err)
		}
		var x struct {
			Servicios     []int64 `json:"servicios"`
			Descartados   int     `json:"descartados"`
			NoVerificados int     `json:"no_verificados"`
		}
		_ = json.Unmarshal(crudo, &x)
		servicios = append(servicios, x.Servicios...)
		descartados += x.Descartados
		noVerificados += x.NoVerificados
	}
	filas.Close()
	slices.Sort(servicios)
	if !slices.Equal(servicios, []int64{902}) || descartados != 4 || noVerificados != 0 {
		t.Fatalf("verificados: %v %d %d", servicios, descartados, noVerificados)
	}
	// Terminó: otra vuelta no lee nada.
	n := e.argos.llamadasA("/api/v1/servicios/902")
	if espera, err := e.mod.VerificarIncidente(ctx, r.EntranteID); err != nil || espera != 0 || e.argos.llamadasA("/api/v1/servicios/902") != n {
		t.Fatalf("terminada: %v %v", espera, err)
	}

	// Un error de Argos con un id: solo ese id cuenta un intento y el job vuelve en un minuto; a los 3,
	// no_verificado (aparte de los descartados).
	f2 := map[string]any{"incidente_id": "inc-8", "empresa_id": 1, "alcance": "zona", "alcance_id": "z-1",
		"servicios_afectados": map[string]any{"cantidad": 2, "ids": []int64{902, 907}}, "desde": "2026-11-21T15:00:00Z"}
	r2 := e.procesarWebhook(id, secreto, argos.EventoFallaGeneralAbierta, f2)
	for i := 0; i < argos.IntentosVerificar; i++ {
		e.argos.con(func() { e.argos.caido = true })
		if espera, err := e.mod.VerificarIncidente(ctx, r2.EntranteID); err != nil || espera != time.Minute {
			t.Fatalf("intento %d: %v %v", i, espera, err)
		}
		e.argos.con(func() { e.argos.caido = false })
	}
	if espera, err := e.mod.VerificarIncidente(ctx, r2.EntranteID); err != nil || espera != 0 {
		t.Fatalf("termina: %v %v", espera, err)
	}
	// 902 (el primero) falló tres veces: no verificado, contado aparte; 907 se verificó después (no existe).
	if n := e.contar(`SELECT coalesce(sum((payload->>'no_verificados')::int), 0)::int FROM integracion.outbox WHERE evento = $1
		AND (payload->>'entrante_id')::bigint = $2`, argos.EventoInternoFallaServicios, r2.EntranteID); n != 1 {
		t.Fatalf("no verificados: %d", n)
	}
	if n := e.contar(`SELECT coalesce(sum((payload->>'descartados')::int), 0)::int FROM integracion.outbox WHERE evento = $1
		AND (payload->>'entrante_id')::bigint = $2`, argos.EventoInternoFallaServicios, r2.EntranteID); n != 1 {
		t.Fatalf("descartados: %d", n)
	}

	falla["hasta"] = "2026-11-21T17:00:00Z"
	if r := e.procesarWebhook(id, secreto, argos.EventoFallaGeneralCerrada, falla); r.Resultado != "aplicado" {
		t.Fatalf("cerrada: %+v", r)
	}
	if p := payloadDe(argos.EventoInternoFallaCerrada); p["incidente_id"] != "inc-7" || p["hasta"] != "2026-11-21T17:00:00Z" {
		t.Fatalf("argos.falla_general_cerrada: %v", p)
	}
	// Empresa fuera del alcance del conector, payload inválido.
	if _, err := e.pool.Exec(ctx, `UPDATE integracion.conectores SET alcance_global = false, empresas = '{1}' WHERE id = $1`, id); err != nil {
		t.Fatal(err)
	}
	falla["empresa_id"] = 2
	if r := e.procesarWebhook(id, secreto, argos.EventoFallaGeneralAbierta, falla); r.Resultado != "ignorado" || e.omitidoDe(r.EntranteID) != "empresa_fuera_de_alcance" {
		t.Fatalf("fuera de alcance: %+v", r)
	}
	falla["empresa_id"], falla["alcance"] = 1, "edificio"
	if r := e.procesarWebhook(id, secreto, argos.EventoFallaGeneralAbierta, falla); r.Resultado != "ignorado" || e.omitidoDe(r.EntranteID) != "payload_invalido" {
		t.Fatalf("alcance inválido: %+v", r)
	}
}

// La lectura de los servicios de una falla se corta si la falla se cerró, si Argos repite el cursor o
// al llegar al máximo de páginas.
func TestVerificarIncidenteSeCorta(t *testing.T) {
	e := nuevoEntorno(t)
	_, id, secreto := e.conectado()
	ctx := pruebas.ContextoSistema(e.pool)
	e.argos.con(func() {
		e.argos.incidentes = map[string][]int64{"inc-c:1": {1}, "inc-r:1": {1}, "inc-m:1": {1}}
		e.argos.cursorFijo = map[string]string{"inc-r": "7"}
	})
	abrir := func(inc string) int64 {
		f := map[string]any{"incidente_id": inc, "empresa_id": 1, "alcance": "zona", "alcance_id": "z-" + inc,
			"servicios_afectados": map[string]any{"cantidad": 5000, "ids": []int64{}}, "desde": "2026-11-21T15:00:00Z"}
		r := e.procesarWebhook(id, secreto, argos.EventoFallaGeneralAbierta, f)
		if r.Resultado != "aplicado" {
			t.Fatalf("%s: %+v", inc, r)
		}
		return r.EntranteID
	}
	llamadas := func(inc string) int { return e.argos.llamadasA("/api/v1/incidentes-red/" + inc + "/servicios") }

	// Cerrada antes de leer: el cierre termina la lectura y el job no llama a Argos.
	c := abrir("inc-c")
	if r := e.procesarWebhook(id, secreto, argos.EventoFallaGeneralCerrada, map[string]any{"incidente_id": "inc-c", "empresa_id": 1, "alcance": "zona",
		"alcance_id": "z-inc-c", "desde": "2026-11-21T15:00:00Z", "hasta": "2026-11-21T16:00:00Z"}); r.Resultado != "aplicado" {
		t.Fatalf("cerrada: %+v", r)
	}
	if espera, err := e.mod.VerificarIncidente(ctx, c); err != nil || espera != 0 || llamadas("inc-c") != 0 {
		t.Fatalf("falla cerrada: %v %v %d", espera, err, llamadas("inc-c"))
	}
	// Argos repite el cursor: una página más y se deja de paginar.
	rr := abrir("inc-r")
	if espera, err := e.mod.VerificarIncidente(ctx, rr); err != nil || espera != 0 || llamadas("inc-r") != 2 {
		t.Fatalf("mismo cursor: %v %v %d", espera, err, llamadas("inc-r"))
	}
	// Con el máximo de páginas ya leído, no se pide otra.
	mx := abrir("inc-m")
	if _, err := e.pool.Exec(ctx, "UPDATE integracion.argos_incidente_lecturas SET paginas = $2 WHERE entrante_id = $1", mx, argos.MaxPaginasIncidente); err != nil {
		t.Fatal(err)
	}
	if espera, err := e.mod.VerificarIncidente(ctx, mx); err != nil || espera != 0 || llamadas("inc-m") != 0 {
		t.Fatalf("máximo de páginas: %v %v %d", espera, err, llamadas("inc-m"))
	}
}

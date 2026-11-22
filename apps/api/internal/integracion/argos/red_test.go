package argos_test

import (
	"encoding/json"
	"net/http"
	"slices"
	"strconv"
	"testing"

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

	// Falla general: 5000 como máximo en el evento y el resto con la lectura paginada.
	falla := map[string]any{"incidente_id": "inc-7", "empresa_id": 1, "alcance": "router", "alcance_id": 33,
		"servicios_afectados": map[string]any{"cantidad": 7, "ids": []int64{901, 903, 904, 905, 901}}, "desde": "2026-11-21T15:00:00Z", "hasta": nil}
	r = e.procesarWebhook(id, secreto, argos.EventoFallaGeneralAbierta, falla)
	if r.Resultado != "aplicado" {
		t.Fatalf("falla: %+v", r)
	}
	p := payloadDe(argos.EventoInternoFallaAbierta)
	var servicios []int64
	for _, s := range p["servicios"].([]any) {
		servicios = append(servicios, int64(s.(float64)))
	}
	slices.Sort(servicios)
	if !slices.Equal(servicios, []int64{901, 902}) || p["descartados"] != float64(4) || p["alcance_id"] != "33" || p["incidente_id"] != "inc-7" {
		t.Fatalf("argos.falla_general_abierta: %v", p)
	}
	if _, nombre := p["nombre"]; nombre {
		t.Fatal("sin nombres")
	}
	if e.argos.llamadasA("/api/v1/incidentes-red/inc-7/servicios") != 1 {
		t.Fatal("el resto se lee de Argos")
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

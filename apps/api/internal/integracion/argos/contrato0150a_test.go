package argos_test

import (
	"net/http"
	"slices"
	"strconv"
	"testing"

	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos"
)

// Contrato argosisp.md 0.15.0 §13.1 (bloque A, B33): tipo de servicio y componentes en la
// instantánea de servicios y de planes, la regla A1 y la marca capacidades.tipo_servicio.

func TestTipoServicioReglaA1(t *testing.T) {
	p := func(s string) *string { return &s }
	router := int64(5)
	casos := []struct {
		nombre      string
		tipo        string
		componentes []string
		router      *int64
		conRouter   bool
		visible     string
	}{
		{"sin datos (Argos < 0.15.0)", "", nil, nil, true, ""},
		{"internet", "internet", nil, &router, true, "internet"},
		{"cable sin router", "cable", []string{"cable"}, nil, false, "cable"},
		{"cable sin componentes", "CABLE ", nil, nil, false, "cable"},
		{"iptv es de datos", "iptv", []string{"iptv"}, &router, true, "iptv"},
		{"mixto internet+cable", "mixto", []string{"internet", "cable"}, &router, true, "mixto"},
		{"mixto solo cable", "mixto", []string{"cable", "basura"}, nil, false, "mixto"},
		{"desconocido con router", "satelital", nil, &router, true, "otro"},
		{"desconocido sin router", "satelital", nil, nil, false, "otro"},
		{"tipo sin forma", "<script>", nil, nil, true, ""},
	}
	for _, c := range casos {
		tipo, comps := argos.NormalizarTipo(c.tipo, c.componentes)
		if got := argos.ConRouter(tipo, comps, c.router, true); got != c.conRouter {
			t.Errorf("%s: ConRouter = %v", c.nombre, got)
		}
		if got := argos.TipoVisible(tipo); got != c.visible {
			t.Errorf("%s: TipoVisible = %q", c.nombre, got)
		}
		for _, x := range comps {
			if !slices.Contains([]string{"internet", "cable", "iptv"}, x) {
				t.Errorf("%s: componente fuera de la lista blanca %q", c.nombre, x)
			}
		}
	}
	// Un plan no tiene router propio: un tipo desconocido cuenta como internet.
	if !argos.ConRouter(p("satelital"), nil, nil, false) || argos.ConRouter(p("cable"), nil, nil, false) {
		t.Fatal("A1 en planes")
	}
	if !argos.IncluyeCable(p("mixto"), []string{"internet", "cable"}) || argos.IncluyeCable(p("internet"), nil) {
		t.Fatal("IncluyeCable")
	}
}

func TestTipoServicioInstantaneaYWebhooks(t *testing.T) {
	e := nuevoEntorno(t)
	e.limpiarSinc()
	e.sembrarSinc("1.30.0", "0.15.0")
	e.argos.con(func() {
		e.argos.planes = []map[string]any{
			{"id": 12, "nombre": "Fibra 50", "tipo_servicio": "internet", "componentes": []string{"internet"}, "bajada_kbps": 50000},
			{"id": 20, "nombre": "TV Básica", "tipo_servicio": "cable", "componentes": []string{"cable"}, "bajada_kbps": nil, "subida_kbps": nil},
			{"id": 21, "nombre": "Combo", "tipo_servicio": "mixto", "componentes": []string{"internet", "cable", "radio"}},
		}
		// Cable con router null (§13.1), mixto, y uno sin tipo (como un Argos anterior).
		e.argos.servicios[701] = []map[string]any{
			{"id": 7010, "cliente_id": 701, "empresa_id": int64(1), "estado": "activo", "plan_id": 20, "plan_nombre": "TV Básica",
				"router_id": nil, "router_nombre": nil, "tipo_servicio": "cable", "componentes": []string{"cable"}},
			{"id": 7011, "cliente_id": 701, "empresa_id": int64(1), "estado": "activo", "plan_id": 21, "plan_nombre": "Combo",
				"router_id": 5, "router_nombre": "Nodo Centro", "tipo_servicio": "mixto", "componentes": []string{"internet", "cable"}},
			{"id": 7012, "cliente_id": 701, "empresa_id": int64(1), "estado": "activo", "plan_id": 12, "plan_nombre": "Fibra 50",
				"router_id": 5, "router_nombre": "Nodo Centro"},
		}
	})
	_, id, secreto := e.conectado()
	// La primera sincronización también carga las cuentas bancarias de la empresa 1: no deben quedar
	// para las pruebas siguientes (una lista de menos de 24 h no se vuelve a pedir).
	t.Cleanup(func() {
		e.limpiarSinc()
		e.exec("DELETE FROM integracion.argos_cuentas_lecturas")
		e.exec("DELETE FROM integracion.argos_cuentas_bancarias WHERE id = 1")
		// TestPrimeraSincronizacion080 cuenta los argos.sincronizacion_* de toda la base.
		e.exec("DELETE FROM integracion.outbox WHERE evento LIKE 'argos.sincronizacion_%' AND payload->>'conector_id' = $1", strconv.FormatInt(id, 10))
	})
	if _, err := e.sincronizar(id); err != nil {
		t.Fatal(err)
	}

	// Primera sincronización: servicios y planes con su tipo, solo con los componentes conocidos.
	if n := e.contar(`SELECT count(*) FROM integracion.argos_servicios WHERE id = 7010 AND tipo_servicio = 'cable'
		AND componentes = ARRAY['cable'] AND router_id IS NULL`); n != 1 {
		t.Fatal("el servicio de cable no quedó en la instantánea con su tipo")
	}
	if n := e.contar(`SELECT count(*) FROM integracion.argos_servicios WHERE id = 7012 AND tipo_servicio IS NULL AND componentes IS NULL`); n != 1 {
		t.Fatal("un servicio sin tipo debía quedar null (se trata como internet)")
	}
	if n := e.contar(`SELECT count(*) FROM integracion.argos_planes WHERE (id = 20 AND tipo_servicio = 'cable')
		OR (id = 21 AND componentes = ARRAY['internet', 'cable'])`); n != 2 {
		t.Fatalf("instantánea de planes: %d de 2", n)
	}

	// La ficha muestra el tipo y marca el de cable sin router.
	ss := serviciosDeFicha(t, e.usuario("soporte").pedir(http.MethodGet, "/api/v1/clientes/701", nil))
	cable, mixto, viejo := ss[7010], ss[7011], ss[7012]
	if cable["tipoServicio"] != "cable" || cable["sinRouter"] != true || cable["routerId"] != nil {
		t.Fatalf("cable en la ficha: %v", cable)
	}
	if mixto["tipoServicio"] != "mixto" || mixto["sinRouter"] != nil || mixto["routerNombre"] != "Nodo Centro" {
		t.Fatalf("mixto en la ficha: %v", mixto)
	}
	if _, ok := viejo["tipoServicio"]; ok || viejo["sinRouter"] != nil {
		t.Fatalf("sin tipo debía verse como hoy: %v", viejo)
	}

	// servicio.config_cambiada con tipo_servicio (A5): se relee y cambia el tipo.
	e.argos.con(func() {
		s := e.argos.servicios[701][2]
		s["tipo_servicio"], s["componentes"], s["router_id"], s["router_nombre"] = "cable", []string{"cable"}, nil, nil
	})
	r := e.procesarWebhook(id, secreto, "servicio.config_cambiada", map[string]any{"servicio_id": 7012, "empresa_id": 1, "cliente_id": 701,
		"campos": []string{"tipo_servicio", "router"}})
	if r.Resultado != "aplicado" {
		t.Fatalf("config_cambiada: %+v", r)
	}
	if n := e.contar(`SELECT count(*) FROM integracion.argos_servicios WHERE id = 7012 AND tipo_servicio = 'cable' AND router_id IS NULL`); n != 1 {
		t.Fatal("servicio.config_cambiada no refrescó el tipo")
	}

	// servicio.creado de cable con router null.
	e.argos.con(func() {
		e.argos.servicios[701] = append(e.argos.servicios[701], map[string]any{"id": 7013, "cliente_id": 701, "empresa_id": int64(1),
			"estado": "pendiente_instalacion", "plan_id": 20, "plan_nombre": "TV Básica", "router_id": nil, "tipo_servicio": "cable", "componentes": []string{"cable"}})
	})
	if r := e.procesarWebhook(id, secreto, "servicio.creado", map[string]any{"servicio_id": 7013, "empresa_id": 1, "cliente_id": 701,
		"tipo_servicio": "cable", "componentes": []string{"cable"}, "router_id": nil}); r.Resultado != "aplicado" {
		t.Fatalf("servicio.creado: %+v", r)
	}
	if n := e.contar(`SELECT count(*) FROM integracion.argos_servicios WHERE id = 7013 AND tipo_servicio = 'cable'`); n != 1 {
		t.Fatal("servicio.creado de cable no quedó en la instantánea")
	}

	// catalogo.cambiado de plan con tipo_servicio: refresca la instantánea del plan.
	e.argos.con(func() {
		e.argos.planes[0]["tipo_servicio"], e.argos.planes[0]["componentes"] = "iptv", []string{"iptv"}
	})
	e.procesarWebhook(id, secreto, "catalogo.cambiado", map[string]any{"catalogo": "plan", "id": 12, "empresa_id": nil,
		"campos": []string{"tipo_servicio", "componentes"}})
	if n := e.contar(`SELECT count(*) FROM integracion.argos_planes WHERE id = 12 AND tipo_servicio = 'iptv' AND componentes = ARRAY['iptv']`); n != 1 {
		t.Fatal("catalogo.cambiado de plan no refrescó el tipo")
	}
	tipo, err := argos.TipoDePlan(t.Context(), e.pool, 20)
	if err != nil || !tipo.Conocido || tipo.ConRouter() {
		t.Fatalf("TipoDePlan(20): %+v %v", tipo, err)
	}
	if desconocido, _ := argos.TipoDePlan(t.Context(), e.pool, 999); desconocido.Conocido || !desconocido.ConRouter() {
		t.Fatal("un plan que Iris no conoce cuenta como internet")
	}
	if con, err := argos.ArgosConTipoServicio(t.Context(), e.pool); err != nil || !con {
		t.Fatalf("Argos 0.15.0: %v %v", con, err)
	}
	e.conContrato(id, "0.14.3")
	if con, _ := argos.ArgosConTipoServicio(t.Context(), e.pool); con {
		t.Fatal("con Argos 0.14.3 el bloque A va apagado")
	}
}

// capacidades.tipo_servicio: true solo con un Iris que habla la 0.15.0 (§4.6).
func TestCapacidadTipoServicio(t *testing.T) {
	for _, c := range []struct {
		contrato string
		tipo     bool
	}{{argos.ContratoIris, argos.SoportaTipoServicio(ptrTexto(argos.ContratoIris))}, {"0.14.3", false}, {"0.15.0", true}} {
		if got := argos.CapacidadesDeIris(c.contrato).TipoServicio; got != c.tipo {
			t.Errorf("contrato %s: capacidades.tipo_servicio = %v", c.contrato, got)
		}
	}
}

func ptrTexto(s string) *string { return &s }

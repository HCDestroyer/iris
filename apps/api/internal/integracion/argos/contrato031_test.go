package argos_test

import (
	"fmt"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
)

// Contrato Iris ↔ ArgosISP 0.3.1 (§1.3, §2.1 y §2.3).

func (e *entorno) procesarWebhook(conectorID int64, secreto, evento string, payload map[string]any) argos.ResultadoEntrante {
	e.t.Helper()
	ent := e.entranteDe(e.enviar(conectorID, secreto, "wh-031-"+pruebas.Unico(), evento, payload, time.Now()))
	r, err := e.mod.ProcesarEntrante.Ejecutar(pruebas.ContextoSistema(e.pool), ent)
	if err != nil {
		e.t.Fatalf("%s: %v", evento, err)
	}
	return r
}

func (e *entorno) omitidoDe(entrante int64) string {
	e.t.Helper()
	return e.consultarTexto(`SELECT coalesce(entrada->'auditoria'->>'omitido', '') FROM auditoria.acciones
		WHERE accion = 'integraciones.procesar_webhook' AND entidad_id = $1 ORDER BY id DESC LIMIT 1`, strconv.FormatInt(entrante, 10))
}

func conAlcance(global bool, empresas ...any) func(*argosFalso) {
	return func(a *argosFalso) { a.alcanceGlobal, a.empresasCanje = &global, empresas }
}

// §1.3: el canje informa alcance_global y empresas[] { id, nombre, nit }. Iris guarda el alcance
// en el conector, registra las empresas con su nombre y NIT y no pisa lo que puso un
// administrador.
func TestCanjeAlcanceGlobalYEmpresas(t *testing.T) {
	e := nuevoEntorno(t)
	if _, err := e.pool.Exec(t.Context(), `UPDATE nucleo.empresas SET nombre = 'Nombre del admin', nit = '' WHERE id = 1`); err != nil {
		t.Fatal(err)
	}
	e.argos.con(func() {
		conAlcance(false, map[string]any{"id": 1, "nombre": "Cable Norte", "nit": "1234567"},
			map[string]any{"id": 2, "nombre": "Cable Sur", "nit": nil})(e.argos)
	})
	admin, id, _ := e.conectado()
	c := admin.pedir(http.MethodGet, "/api/v1/integraciones/conectores", nil)
	if c.status != 200 {
		t.Fatalf("conectores: %s", c.crudo)
	}
	var visto map[string]any
	for _, x := range c.cuerpo["data"].([]any) {
		if m := x.(map[string]any); int64(m["id"].(float64)) == id {
			visto = m
		}
	}
	if visto == nil || visto["alcanceGlobal"] != false || fmt.Sprint(visto["empresas"]) != "[1 2]" {
		t.Fatalf("conector: %v", visto)
	}
	if n := e.contar(`SELECT count(*) FROM nucleo.empresas WHERE id = 1 AND nombre = 'Nombre del admin' AND nit = '1234567'`); n != 1 {
		t.Fatal("empresa 1: el nombre del admin se respeta y el NIT vacío se completa")
	}
	if n := e.contar(`SELECT count(*) FROM nucleo.empresas WHERE id = 2 AND nombre = 'Cable Sur' AND nit = '' AND origen = 'argos'`); n != 1 {
		t.Fatal("empresa 2: registrada con el nombre de Argos y sin NIT (null)")
	}
}

// Sin alcance_global (Argos anterior a 0.3.1) el conector no trae alcanceGlobal y el alcance no
// filtra los webhooks.
func TestCanjeSinAlcanceGlobal(t *testing.T) {
	e := nuevoEntorno(t)
	admin, id, _ := e.conectado()
	c := admin.pedir(http.MethodGet, "/api/v1/integraciones/conectores", nil)
	for _, x := range c.cuerpo["data"].([]any) {
		m := x.(map[string]any)
		if _, ok := m["alcanceGlobal"]; ok && int64(m["id"].(float64)) == id {
			t.Fatalf("alcanceGlobal: %s", c.crudo)
		}
	}
	if n := e.contar(`SELECT count(*) FROM integracion.conectores WHERE estado = 'activo' AND alcance_global IS NULL`); n != 1 {
		t.Fatal("alcance_global debía quedar null")
	}
}

// §2.3 cliente.actualizado con campos[]: refresca teléfono, dirección, coordenadas y zona de la
// instantánea; si solo cambiaron campos que Iris no guarda, no relee el cliente.
func TestClienteActualizadoConCampos(t *testing.T) {
	e := nuevoEntorno(t)
	_, id, secreto := e.conectado()
	e.sembrarCliente(91, "Hilda")
	if f := e.usuario("lectura").pedir(http.MethodGet, "/api/v1/clientes/91", nil); f.status != 200 {
		t.Fatalf("ficha: %s", f.crudo)
	}
	e.argos.con(func() {
		c := e.argos.clientes[91]
		delete(c, "latitud")
		delete(c, "longitud")
		c["telefono"], c["movil"], c["direccion"], c["coordenadas"] = "7777-1234", "5555-9999", "Zona 10", "14.6000,-90.5000"
		c["zona"] = map[string]any{"id": 7, "nombre": "Sur"}
	})
	r := e.procesarWebhook(id, secreto, "cliente.actualizado", map[string]any{"cliente_id": 91, "empresa_id": 1, "empresas": []int{1},
		"campos": []string{"telefono", "movil", "direccion", "coordenadas", "zona_id"}})
	if r.Resultado != "aplicado" {
		t.Fatalf("cliente.actualizado: %+v", r)
	}
	if n := e.contar(`SELECT count(*) FROM integracion.argos_clientes WHERE id = 91 AND telefono = '7777-1234' AND direccion = 'Zona 10'
		AND lat = 14.6 AND lon = -90.5 AND zona_id = 7`); n != 1 {
		t.Fatal("la instantánea no se refrescó")
	}
	// Solo NIT, tipo o móvil: la instantánea no los guarda; no se relee.
	antes := e.argos.llamadasA("/api/v1/clientes/91")
	r = e.procesarWebhook(id, secreto, "cliente.actualizado", map[string]any{"cliente_id": 91, "empresa_id": 1, "campos": []string{"nit_cui", "movil"}})
	if r.Resultado != "ignorado" || e.omitidoDe(r.EntranteID) != "campos_sin_efecto" || e.argos.llamadasA("/api/v1/clientes/91") != antes {
		t.Fatalf("campos sin efecto: %+v (%s)", r, e.omitidoDe(r.EntranteID))
	}
	// Un campo desconocido (o sin campos[]) refresca por si acaso.
	e.argos.con(func() { e.argos.clientes[91]["nombre"] = "Hilda M." })
	if r := e.procesarWebhook(id, secreto, "cliente.actualizado", map[string]any{"cliente_id": 91, "campos": []string{"otro"}}); r.Resultado != "aplicado" {
		t.Fatalf("campo desconocido: %+v", r)
	}
	if e.consultarTexto(`SELECT nombre FROM integracion.argos_clientes WHERE id = 91`) != "Hilda M." {
		t.Fatal("no se refrescó con un campo desconocido")
	}
}

// §2.3 servicio.eliminado: se confirma con el 404 de GET /servicios/:id (sin reintentos), se
// quita de la instantánea y de la lista del cliente, queda la lápida con la última copia (la
// ficha lo lista en serviciosEliminados para órdenes y tickets) y un evento o una lectura
// atrasados no lo recrean. Respeta el alcance del canje.
func TestServicioEliminado(t *testing.T) {
	e := nuevoEntorno(t)
	e.argos.con(func() { conAlcance(false, map[string]any{"id": 1, "nombre": "Cable Norte", "nit": "1"})(e.argos) })
	_, id, secreto := e.conectado()
	lectura := e.usuario("lectura")
	e.sembrarCliente(81, "Gina")
	e.argos.con(func() {
		e.argos.servicios[81] = append(e.argos.servicios[81], map[string]any{"id": int64(811), "empresa_id": 1, "cliente_id": 81, "codigo": "S-2", "estado": "activo"})
	})
	if f := lectura.pedir(http.MethodGet, "/api/v1/clientes/81", nil); f.status != 200 || len(f.cuerpo["servicios"].([]any)) != 2 {
		t.Fatalf("ficha: %s", f.crudo)
	}
	payload := map[string]any{"servicio_id": 810, "empresa_id": 1, "cliente_id": 81, "router_id": 4}

	// Argos aún lo tiene: no se borra nada.
	r := e.procesarWebhook(id, secreto, "servicio.eliminado", payload)
	if r.Resultado != "ignorado" || e.omitidoDe(r.EntranteID) != "no_confirmado" || e.contar(`SELECT count(*) FROM integracion.argos_servicios WHERE id = 810`) != 1 {
		t.Fatalf("no confirmado: %+v", r)
	}

	// Argos lo eliminó: 404 (una sola llamada, sin reintentos).
	e.argos.con(func() { e.argos.quitarServicio(810) })
	antes := e.argos.llamadasA("/api/v1/servicios/810")
	r = e.procesarWebhook(id, secreto, "servicio.eliminado", payload)
	if r.Resultado != "aplicado" {
		t.Fatalf("servicio.eliminado: %+v", r)
	}
	if n := e.argos.llamadasA("/api/v1/servicios/810") - antes; n != 1 {
		t.Fatalf("GET /servicios/810: %d llamadas (el 404 no se reintenta)", n)
	}
	if e.contar(`SELECT count(*) FROM integracion.argos_servicios WHERE id = 810`) != 0 ||
		e.contar(`SELECT count(*) FROM integracion.argos_servicios WHERE id = 811`) != 1 {
		t.Fatal("instantánea del servicio")
	}
	if n := e.contar(`SELECT count(*) FROM integracion.argos_eliminados WHERE tipo = 'servicio' AND id = 810 AND cliente_id = 81
		AND empresa_id = 1 AND codigo = 'S-1' AND nombre = 'Fibra 100' AND entrante_id = $1`, r.EntranteID); n != 1 {
		t.Fatal("lápida con la última copia")
	}
	if n := e.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'argos.servicio_eliminado' AND empresa_id = 1
		AND payload->>'servicio_id' = '810' AND payload->>'cliente_id' = '81' AND payload->>'router_id' = '4'`); n != 1 {
		t.Fatalf("argos.servicio_eliminado: %d", n)
	}
	f := lectura.pedir(http.MethodGet, "/api/v1/clientes/81", nil)
	eliminados, _ := f.cuerpo["serviciosEliminados"].([]any)
	if f.status != 200 || len(f.cuerpo["servicios"].([]any)) != 1 || len(eliminados) != 1 {
		t.Fatalf("ficha con el eliminado: %s", f.crudo)
	}
	if el := eliminados[0].(map[string]any); el["id"] != float64(810) || el["codigo"] != "S-1" || el["planNombre"] != "Fibra 100" || el["eliminadoEn"] == nil {
		t.Fatalf("eliminado: %v", el)
	}
	if lista, err := argos.ServiciosEliminados(t.Context(), e.pool, []int64{810, 811, 999}, 1); err != nil || len(lista) != 1 || lista[0].ID != 810 {
		t.Fatalf("ServiciosEliminados: %+v %v", lista, err)
	}
	if lista, _ := argos.ServiciosEliminados(t.Context(), e.pool, []int64{810}, 2); len(lista) != 0 {
		t.Fatal("ServiciosEliminados filtra por empresa")
	}

	// Un evento atrasado del servicio no lo recrea (ni lee Argos).
	antes = e.argos.llamadasA("/api/v1/clientes/81")
	r = e.procesarWebhook(id, secreto, "asignacion.cambiada", map[string]any{"servicio_id": 810, "empresa_id": 1})
	if r.Resultado != "ignorado" || e.omitidoDe(r.EntranteID) != "servicio_eliminado" || e.argos.llamadasA("/api/v1/clientes/81") != antes {
		t.Fatalf("evento atrasado: %+v", r)
	}
	// Una lectura vieja (el servicio vuelve en la lista de Argos) tampoco: la lápida manda.
	e.argos.con(func() {
		e.argos.servicios[81] = append(e.argos.servicios[81], map[string]any{"id": int64(810), "empresa_id": 1, "cliente_id": 81, "codigo": "S-1"})
	})
	if r := e.procesarWebhook(id, secreto, "cliente.actualizado", map[string]any{"cliente_id": 81}); r.Resultado != "aplicado" {
		t.Fatalf("refresco: %+v", r)
	}
	if e.contar(`SELECT count(*) FROM integracion.argos_servicios WHERE id = 810`) != 0 {
		t.Fatal("una lectura atrasada recreó el servicio eliminado")
	}
	e.argos.con(func() { e.argos.quitarServicio(810) })

	// Un segundo aviso (otro webhook_id) no repite el evento.
	if r := e.procesarWebhook(id, secreto, "servicio.eliminado", payload); r.Resultado != "aplicado" {
		t.Fatalf("segundo aviso: %+v", r)
	}
	if n := e.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'argos.servicio_eliminado' AND payload->>'servicio_id' = '810'`); n != 1 {
		t.Fatalf("el evento interno sale una vez: %d", n)
	}

	// Fuera del alcance del canje (empresa 3): se ignora sin preguntar a Argos.
	e.argos.con(func() { e.argos.quitarServicio(811) })
	antes = e.argos.llamadasA("/api/v1/servicios/811")
	r = e.procesarWebhook(id, secreto, "servicio.eliminado", map[string]any{"servicio_id": 811, "empresa_id": 3, "cliente_id": 81})
	if r.Resultado != "ignorado" || e.omitidoDe(r.EntranteID) != "fuera_de_alcance" || e.argos.llamadasA("/api/v1/servicios/811") != antes ||
		e.contar(`SELECT count(*) FROM integracion.argos_servicios WHERE id = 811`) != 1 {
		t.Fatalf("fuera de alcance: %+v", r)
	}

	// Argos caído al confirmar: error (River reintenta) y nada cambia.
	e.argos.con(func() { e.argos.caido = true })
	ent := e.entranteDe(e.enviar(id, secreto, "wh-031-caido-"+pruebas.Unico(), "servicio.eliminado", map[string]any{"servicio_id": 811, "empresa_id": 1}, time.Now()))
	if _, err := e.mod.ProcesarEntrante.Ejecutar(pruebas.ContextoSistema(e.pool), ent); err == nil {
		t.Fatal("con Argos caído debía fallar")
	}
	e.argos.con(func() { e.argos.caido = false })
	if r, err := e.mod.ProcesarEntrante.Ejecutar(pruebas.ContextoSistema(e.pool), ent); err != nil || r.Resultado != "aplicado" {
		t.Fatalf("tras volver Argos: %+v %v", r, err)
	}
	if e.contar(`SELECT count(*) FROM integracion.argos_servicios WHERE cliente_id = 81`) != 0 {
		t.Fatal("el cliente se quedó sin servicios")
	}
}

// §2.1 coordenadas de routers ("lat,lng" o null) y §2.3 router.eliminado: se quita de la
// instantánea (factibilidad ya no lo ve), queda la lápida y una carga de catálogos atrasada no lo
// recrea. empresa_id null = router compartido (llega a cualquier alcance).
func TestRouterCoordenadasYEliminado(t *testing.T) {
	e := nuevoEntorno(t)
	e.argos.con(func() { conAlcance(false, map[string]any{"id": 1, "nombre": "Cable Norte", "nit": "1"})(e.argos) })
	_, id, secreto := e.conectado()
	routers := []map[string]any{
		{"id": 9, "empresa_sugerida_id": 1, "nombre": "RT-9", "coordenadas": "14.5000,-90.5000"},
		{"id": 10, "empresa_sugerida_id": nil, "nombre": "RT-10", "coordenadas": nil},
		{"id": 11, "empresa_sugerida_id": nil, "nombre": "RT-11", "coordenadas": "14.6,-90.6"},
	}
	e.argos.con(func() { e.argos.routers = append([]map[string]any(nil), routers...) })
	ctx := pruebas.ContextoSistema(e.pool)
	if r, err := e.mod.SincronizarCatalogos.Ejecutar(ctx, struct{}{}); err != nil || r.Routers != 3 {
		t.Fatalf("catálogos: %+v %v", r, err)
	}
	if e.contar(`SELECT count(*) FROM integracion.argos_routers WHERE id = 9 AND lat = 14.5 AND lon = -90.5 AND empresa_id = 1`) != 1 ||
		e.contar(`SELECT count(*) FROM integracion.argos_routers WHERE id = 10 AND lat IS NULL AND lon IS NULL AND empresa_id IS NULL`) != 1 {
		t.Fatal("coordenadas de routers")
	}

	r := e.procesarWebhook(id, secreto, "router.eliminado", map[string]any{"router_id": 9, "empresa_id": 1})
	if r.Resultado != "ignorado" || e.omitidoDe(r.EntranteID) != "no_confirmado" {
		t.Fatalf("no confirmado: %+v", r)
	}
	e.argos.con(func() { e.argos.quitarRouter(9) })
	antes := e.argos.llamadasA("/api/v1/routers/9")
	if r := e.procesarWebhook(id, secreto, "router.eliminado", map[string]any{"router_id": 9, "empresa_id": 1}); r.Resultado != "aplicado" {
		t.Fatalf("router.eliminado: %+v", r)
	}
	if n := e.argos.llamadasA("/api/v1/routers/9") - antes; n != 1 {
		t.Fatalf("GET /routers/9: %d llamadas (el 404 no se reintenta)", n)
	}
	if e.contar(`SELECT count(*) FROM integracion.argos_routers WHERE id = 9`) != 0 ||
		e.contar(`SELECT count(*) FROM integracion.argos_eliminados WHERE tipo = 'router' AND id = 9 AND nombre = 'RT-9' AND empresa_id = 1`) != 1 ||
		e.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'argos.router_eliminado' AND payload->>'router_id' = '9' AND empresa_id = 1`) != 1 {
		t.Fatal("router eliminado")
	}
	// Una carga de catálogos atrasada (Argos aún lo listaba) no lo recrea.
	e.argos.con(func() { e.argos.routers = append([]map[string]any(nil), routers...) })
	if _, err := e.mod.SincronizarCatalogos.Ejecutar(ctx, struct{}{}); err != nil {
		t.Fatal(err)
	}
	if e.contar(`SELECT count(*) FROM integracion.argos_routers WHERE id = 9`) != 0 {
		t.Fatal("la carga de catálogos recreó un router eliminado")
	}
	e.argos.con(func() { e.argos.quitarRouter(9) })

	// Router compartido (empresa_id null): llega a cualquier alcance.
	e.argos.con(func() { e.argos.quitarRouter(10) })
	if r := e.procesarWebhook(id, secreto, "router.eliminado", map[string]any{"router_id": 10, "empresa_id": nil}); r.Resultado != "aplicado" {
		t.Fatalf("router compartido: %+v", r)
	}
	if e.contar(`SELECT count(*) FROM integracion.argos_eliminados WHERE tipo = 'router' AND id = 10 AND empresa_id IS NULL`) != 1 {
		t.Fatal("lápida del router compartido")
	}
	// Con empresa fuera del alcance: se ignora.
	e.argos.con(func() { e.argos.quitarRouter(11) })
	r = e.procesarWebhook(id, secreto, "router.eliminado", map[string]any{"router_id": 11, "empresa_id": 5})
	if r.Resultado != "ignorado" || e.omitidoDe(r.EntranteID) != "fuera_de_alcance" || e.contar(`SELECT count(*) FROM integracion.argos_routers WHERE id = 11`) != 1 {
		t.Fatalf("fuera de alcance: %+v", r)
	}
}

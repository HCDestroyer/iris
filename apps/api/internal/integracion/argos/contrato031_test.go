package argos_test

import (
	"context"
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
// en el conector y registra las empresas con su nombre y NIT, auditado y con su evento; de una
// existente de origen argos solo completa el marcador y el NIT vacío; una manual no se toca.
func TestCanjeAlcanceGlobalYEmpresas(t *testing.T) {
	e := nuevoEntorno(t)
	for _, sql := range []string{
		`UPDATE nucleo.empresas SET nombre = 'Nombre del admin', nit = '', origen = 'argos' WHERE id = 1`,
		`INSERT INTO nucleo.empresas (id, nombre, nit, origen) VALUES (3, 'Empresa 3', '', 'manual')`,
	} {
		if _, err := e.pool.Exec(t.Context(), sql); err != nil {
			t.Fatal(err)
		}
	}
	e.argos.con(func() {
		conAlcance(false, map[string]any{"id": 1, "nombre": "Cable Norte", "nit": "1234567"},
			map[string]any{"id": 2, "nombre": "Cable Sur", "nit": nil},
			map[string]any{"id": 3, "nombre": "Cable Este", "nit": "999"})(e.argos)
	})
	desde := e.consultarTexto(`SELECT clock_timestamp()::text`)
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
	if visto == nil || visto["alcanceGlobal"] != false || fmt.Sprint(visto["empresas"]) != "[1 2 3]" {
		t.Fatalf("conector: %v", visto)
	}
	if n := e.contar(`SELECT count(*) FROM nucleo.empresas WHERE id = 1 AND nombre = 'Nombre del admin' AND nit = '1234567' AND version = 2`); n != 1 {
		t.Fatal("empresa 1: el nombre del admin se respeta y el NIT vacío se completa")
	}
	if n := e.contar(`SELECT count(*) FROM nucleo.empresas WHERE id = 2 AND nombre = 'Cable Sur' AND nit = '' AND origen = 'argos'`); n != 1 {
		t.Fatal("empresa 2: registrada con el nombre de Argos y sin NIT (null)")
	}
	if n := e.contar(`SELECT count(*) FROM nucleo.empresas WHERE id = 3 AND nombre = 'Empresa 3' AND nit = '' AND version = 1`); n != 1 {
		t.Fatal("empresa 3 (manual): no se completa")
	}
	if n := e.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'empresa.actualizada' AND empresa_id = 1
		AND payload->'campos' = '["nit"]'::jsonb AND ocurrido_en > $1::timestamptz`, desde); n != 1 {
		t.Fatalf("empresa.actualizada de la 1: %d", n)
	}
	if n := e.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'empresa.creada' AND empresa_id = 2 AND payload->>'origen' = 'argos' AND ocurrido_en > $1::timestamptz`, desde); n != 1 {
		t.Fatalf("empresa.creada de la 2: %d", n)
	}
	if n := e.contar(`SELECT count(*) FROM integracion.outbox WHERE evento LIKE 'empresa.%' AND empresa_id = 3 AND ocurrido_en > $1::timestamptz`, desde); n != 0 {
		t.Fatalf("la 3 (manual) no emite: %d", n)
	}
	if n := e.contar(`SELECT count(*) FROM auditoria.cambios WHERE entidad = 'empresa' AND entidad_id = '1' AND campo = 'nit'
		AND despues = '"1234567"'::jsonb`); n != 1 {
		t.Fatalf("cambio auditado del NIT: %d", n)
	}
}

// Sin alcance_global (Argos anterior a 0.3.1) el conector no trae alcanceGlobal.
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
// instantánea, y también NIT y móvil (la instantánea los guarda desde los datos coherentes).
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
	if n := e.contar(`SELECT count(*) FROM integracion.argos_clientes WHERE id = 91 AND movil = '5555-9999'`); n != 1 {
		t.Fatal("el móvil no se guardó")
	}
	// Solo NIT o móvil: desde los datos coherentes la instantánea los guarda; se relee.
	antes := e.argos.llamadasA("/api/v1/clientes/91")
	e.argos.con(func() {
		c := e.argos.clientes[91]
		c["nit_cui"], c["tipo_documento_fiscal"], c["movil"] = "12345678", "nit", "4444-1111"
	})
	r = e.procesarWebhook(id, secreto, "cliente.actualizado", map[string]any{"cliente_id": 91, "empresa_id": 1, "campos": []string{"nit_cui", "movil"}})
	if r.Resultado != "aplicado" || e.argos.llamadasA("/api/v1/clientes/91") == antes {
		t.Fatalf("NIT y móvil: %+v", r)
	}
	if n := e.contar(`SELECT count(*) FROM integracion.argos_clientes WHERE id = 91 AND nit_cui = '12345678' AND movil = '4444-1111'`); n != 1 {
		t.Fatal("NIT y móvil no se refrescaron")
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

// §2.3 servicio.eliminado: se confirma con el 404 problem+json de GET /servicios/:id (sin
// reintentos), se quita de la instantánea y de la lista del cliente, queda la lápida con la
// última copia (la ficha lo lista en serviciosEliminados) y un evento atrasado no lo recrea; una
// lectura directa posterior que lo devuelve con 200 lo resucita.
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
	// Sin servicio_id (solo «id»): se descarta sin preguntar a Argos.
	antes := e.argos.llamadasA("/api/v1/servicios/810")
	r = e.procesarWebhook(id, secreto, "servicio.eliminado", map[string]any{"id": 810, "empresa_id": 1})
	if r.Resultado != "ignorado" || e.omitidoDe(r.EntranteID) != "sin_id" || e.argos.llamadasA("/api/v1/servicios/810") != antes {
		t.Fatalf("sin servicio_id: %+v", r)
	}

	e.argos.con(func() { e.argos.quitarServicio(810) })
	// Un 404 que no es de Argos (HTML de un proxy): error, el job se reintenta y no hay lápida.
	e.argos.con(func() { e.argos.proxy404["/api/v1/servicios/810"] = true })
	ent := e.entranteDe(e.enviar(id, secreto, "wh-031-proxy-"+pruebas.Unico(), "servicio.eliminado", payload, time.Now()))
	if _, err := e.mod.ProcesarEntrante.Ejecutar(pruebas.ContextoSistema(e.pool), ent); err == nil {
		t.Fatal("un 404 HTML no confirma la eliminación")
	}
	if e.contar(`SELECT count(*) FROM integracion.argos_eliminados WHERE id = 810`) != 0 || e.contar(`SELECT count(*) FROM integracion.argos_servicios WHERE id = 810`) != 1 {
		t.Fatal("un 404 HTML dejó lápida")
	}
	e.argos.con(func() { delete(e.argos.proxy404, "/api/v1/servicios/810") })

	// El 404 de Argos lo confirma (una sola llamada, sin reintentos); el mismo entrante, al
	// reintentarse, se aplica.
	antes = e.argos.llamadasA("/api/v1/servicios/810")
	r, err := e.mod.ProcesarEntrante.Ejecutar(pruebas.ContextoSistema(e.pool), ent)
	if err != nil || r.Resultado != "aplicado" {
		t.Fatalf("servicio.eliminado: %+v %v", r, err)
	}
	if n := e.argos.llamadasA("/api/v1/servicios/810") - antes; n != 1 {
		t.Fatalf("GET /servicios/810: %d llamadas (el 404 no se reintenta)", n)
	}
	if e.contar(`SELECT count(*) FROM integracion.argos_servicios WHERE id = 810`) != 0 ||
		e.contar(`SELECT count(*) FROM integracion.argos_servicios WHERE id = 811`) != 1 {
		t.Fatal("instantánea del servicio")
	}
	if n := e.contar(`SELECT count(*) FROM integracion.argos_eliminados WHERE tipo = 'servicio' AND id = 810 AND cliente_id = 81
		AND empresa_id = 1 AND codigo = 'S-1' AND nombre = 'Fibra 100' AND entrante_id = $1 AND conector_id = $2`, ent, id); n != 1 {
		t.Fatal("lápida con la última copia y el conector")
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
	if _, err := argos.ServiciosEliminados(t.Context(), e.pool, []int64{810}, 0); err == nil {
		t.Fatal("ServiciosEliminados exige empresa")
	}
	if lista, err := argos.ServiciosEliminadosTodasLasEmpresas(t.Context(), e.pool, []int64{810}); err != nil || len(lista) != 1 {
		t.Fatalf("ServiciosEliminadosTodasLasEmpresas: %+v %v", lista, err)
	}

	// Un evento atrasado del servicio no lo recrea (ni lee Argos).
	antes = e.argos.llamadasA("/api/v1/clientes/81")
	r = e.procesarWebhook(id, secreto, "asignacion.cambiada", map[string]any{"servicio_id": 810, "empresa_id": 1})
	if r.Resultado != "ignorado" || e.omitidoDe(r.EntranteID) != "servicio_eliminado" || e.argos.llamadasA("/api/v1/clientes/81") != antes {
		t.Fatalf("evento atrasado: %+v", r)
	}
	// Un segundo aviso (otro webhook_id) no repite el evento.
	if r := e.procesarWebhook(id, secreto, "servicio.eliminado", payload); r.Resultado != "aplicado" {
		t.Fatalf("segundo aviso: %+v", r)
	}
	if n := e.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'argos.servicio_eliminado' AND payload->>'servicio_id' = '810'`); n != 1 {
		t.Fatalf("el evento interno sale una vez: %d", n)
	}

	// Un 403 empresa_no_permitida (existe fuera del alcance) no confirma: error y se reintenta.
	e.argos.con(func() { e.argos.quitarServicio(811); e.argos.fueraDeAlcance["/api/v1/servicios/811"] = true })
	ent = e.entranteDe(e.enviar(id, secreto, "wh-031-403-"+pruebas.Unico(), "servicio.eliminado", map[string]any{"servicio_id": 811, "empresa_id": 1}, time.Now()))
	if _, err := e.mod.ProcesarEntrante.Ejecutar(pruebas.ContextoSistema(e.pool), ent); err == nil ||
		e.contar(`SELECT count(*) FROM integracion.argos_eliminados WHERE id = 811`) != 0 {
		t.Fatalf("un 403 no confirma la eliminación: %v", err)
	}
	e.argos.con(func() { delete(e.argos.fueraDeAlcance, "/api/v1/servicios/811") })

	// Payload desfasado (otra empresa): no se pierde la eliminación; se aplica con los datos de la
	// instantánea y la discrepancia queda en la auditoría.
	r = e.procesarWebhook(id, secreto, "servicio.eliminado", map[string]any{"servicio_id": 811, "empresa_id": 3, "cliente_id": 81})
	if r.Resultado != "aplicado" || e.consultarTexto(`SELECT coalesce(entrada->'auditoria'->>'discrepancia', '') FROM auditoria.acciones
		WHERE accion = 'integraciones.procesar_webhook' AND entidad_id = $1 ORDER BY id DESC LIMIT 1`, strconv.FormatInt(r.EntranteID, 10)) != "empresa_no_coincide" {
		t.Fatalf("empresa distinta: %+v", r)
	}
	if e.contar(`SELECT count(*) FROM integracion.argos_eliminados WHERE id = 811 AND empresa_id = 1 AND cliente_id = 81 AND de_instantanea`) != 1 ||
		e.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'argos.servicio_eliminado' AND payload->>'servicio_id' = '811' AND empresa_id = 1`) != 1 {
		t.Fatal("la eliminación desfasada debía usar la instantánea")
	}

	// Una lista (ficha) que trae un id con lápida no lo guarda si el GET por id no lo confirma.
	e.argos.con(func() {
		e.argos.servicios[81] = append(e.argos.servicios[81], map[string]any{"id": int64(810), "empresa_id": 1, "cliente_id": 81, "codigo": "S-1"})
		e.argos.sinGet[810] = true
	})
	if r := e.procesarWebhook(id, secreto, "cliente.actualizado", map[string]any{"cliente_id": 81}); r.Resultado != "aplicado" {
		t.Fatalf("lista atrasada: %+v", r)
	}
	if e.contar(`SELECT count(*) FROM integracion.argos_servicios WHERE id = 810`) != 0 || e.contar(`SELECT count(*) FROM integracion.argos_eliminados WHERE id = 810`) != 1 {
		t.Fatal("una lista no debía recrear el servicio sin el GET por id")
	}

	// El GET por id responde 200 después de la confirmación: la lápida se borra (anotado en la
	// auditoría), la instantánea vuelve y sale argos.servicio_restaurado.
	e.argos.con(func() { delete(e.argos.sinGet, 810) })
	r = e.procesarWebhook(id, secreto, "cliente.actualizado", map[string]any{"cliente_id": 81})
	if r.Resultado != "aplicado" || e.contar(`SELECT count(*) FROM integracion.argos_servicios WHERE id = 810`) != 1 ||
		e.contar(`SELECT count(*) FROM integracion.argos_eliminados WHERE id = 810`) != 0 {
		t.Fatalf("resucitar: %+v", r)
	}
	if n := e.contar(`SELECT count(*) FROM auditoria.acciones WHERE accion = 'integraciones.procesar_webhook'
		AND entidad_id = $1 AND entrada->'auditoria' ? 'resucitado_servicio_810'`, strconv.FormatInt(r.EntranteID, 10)); n != 1 {
		t.Fatal("la resurrección queda en la auditoría")
	}
	if n := e.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'argos.servicio_restaurado' AND empresa_id = 1
		AND payload->>'servicio_id' = '810' AND payload->>'cliente_id' = '81'`); n != 1 {
		t.Fatalf("argos.servicio_restaurado: %d", n)
	}
	// En cambio, un GET 200 pedido antes de la confirmación no la borra: lápida confirmada
	// «después» de cualquier lectura de ahora.
	if _, err := e.pool.Exec(t.Context(), `DELETE FROM integracion.argos_servicios WHERE id = 810`); err != nil {
		t.Fatal(err)
	}
	if _, err := e.pool.Exec(t.Context(), `INSERT INTO integracion.argos_eliminados (tipo, id, empresa_id, cliente_id, conector_id, eliminado_en, confirmado_en)
		VALUES ('servicio', 810, 1, 81, $1, now(), now() + interval '1 hour')`, id); err != nil {
		t.Fatal(err)
	}
	if r := e.procesarWebhook(id, secreto, "cliente.actualizado", map[string]any{"cliente_id": 81}); r.Resultado != "aplicado" {
		t.Fatalf("lectura vieja: %+v", r)
	}
	if e.contar(`SELECT count(*) FROM integracion.argos_servicios WHERE id = 810`) != 0 || e.contar(`SELECT count(*) FROM integracion.argos_eliminados WHERE id = 810`) != 1 {
		t.Fatal("un GET anterior a la confirmación recreó el servicio")
	}

	// Argos caído al confirmar: error (River reintenta) y nada cambia.
	e.argos.con(func() {
		e.argos.servicios[81] = append(e.argos.servicios[81], map[string]any{"id": int64(812), "empresa_id": 1, "cliente_id": 81, "codigo": "S-3"})
	})
	if r := e.procesarWebhook(id, secreto, "cliente.actualizado", map[string]any{"cliente_id": 81}); r.Resultado != "aplicado" {
		t.Fatalf("alta de 812: %+v", r)
	}
	e.argos.con(func() { e.argos.quitarServicio(812); e.argos.caido = true })
	ent = e.entranteDe(e.enviar(id, secreto, "wh-031-caido-"+pruebas.Unico(), "servicio.eliminado", map[string]any{"servicio_id": 812, "empresa_id": 1}, time.Now()))
	if _, err := e.mod.ProcesarEntrante.Ejecutar(pruebas.ContextoSistema(e.pool), ent); err == nil {
		t.Fatal("con Argos caído debía fallar")
	}
	e.argos.con(func() { e.argos.caido = false })
	if r, err := e.mod.ProcesarEntrante.Ejecutar(pruebas.ContextoSistema(e.pool), ent); err != nil || r.Resultado != "aplicado" {
		t.Fatalf("tras volver Argos: %+v %v", r, err)
	}
}

// Un cliente cuyo único servicio se elimina no queda «sin servicios» (visible en todas las
// empresas): la empresa de la lápida lo sigue atando a la suya.
func TestClienteSinServiciosTrasEliminarSigueEnSuEmpresa(t *testing.T) {
	e := nuevoEntorno(t)
	_, id, secreto := e.conectado()
	if _, err := e.pool.Exec(t.Context(), `INSERT INTO nucleo.empresas (id, nombre) VALUES (2, 'Empresa 2') ON CONFLICT (id) DO NOTHING`); err != nil {
		t.Fatal(err)
	}
	e.sembrarCliente(85, "Ivo")
	propia, otra := e.usuario("lectura"), e.usuarioEn([]int64{2}, "lectura")
	if f := propia.pedir(http.MethodGet, "/api/v1/clientes/85", nil); f.status != 200 {
		t.Fatalf("ficha: %s", f.crudo)
	}
	// Una lápida sin instantánea (empresa y cliente del payload) no es pista de empresa (N4).
	if r := e.procesarWebhook(id, secreto, "servicio.eliminado", map[string]any{"servicio_id": 859, "empresa_id": 2, "cliente_id": 85}); r.Resultado != "aplicado" {
		t.Fatalf("lápida sin instantánea: %+v", r)
	}
	if e.contar(`SELECT count(*) FROM integracion.argos_eliminados WHERE id = 859 AND NOT de_instantanea AND empresa_id = 2`) != 1 {
		t.Fatal("la lápida sin instantánea debía quedar marcada")
	}
	e.argos.con(func() { e.argos.quitarServicio(850) })
	if r := e.procesarWebhook(id, secreto, "servicio.eliminado", map[string]any{"servicio_id": 850, "empresa_id": 1, "cliente_id": 85}); r.Resultado != "aplicado" {
		t.Fatalf("servicio.eliminado: %+v", r)
	}
	if n := e.contar(`SELECT count(*) FROM integracion.argos_clientes WHERE id = 85 AND NOT sin_servicios AND empresas = '{1}'`); n != 1 {
		t.Fatal("el cliente debía quedar en la empresa 1")
	}
	if f := propia.pedir(http.MethodGet, "/api/v1/clientes/85", nil); f.status != 200 {
		t.Fatalf("su empresa lo ve: %s", f.crudo)
	}
	if f := otra.pedir(http.MethodGet, "/api/v1/clientes/85", nil); f.status != 404 {
		t.Fatalf("otra empresa no lo ve: %d %s", f.status, f.crudo)
	}
}

// La eliminación toma el candado (tipo, id) que también toma el guardado de la instantánea: no
// avanza mientras otra transacción guarda ese servicio.
func TestEliminacionEsperaElCandado(t *testing.T) {
	e := nuevoEntorno(t)
	_, id, secreto := e.conectado()
	e.sembrarCliente(87, "Jade")
	if f := e.usuario("lectura").pedir(http.MethodGet, "/api/v1/clientes/87", nil); f.status != 200 {
		t.Fatalf("ficha: %s", f.crudo)
	}
	e.argos.con(func() { e.argos.quitarServicio(870) })
	tx, err := e.pool.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if _, err := tx.Exec(t.Context(), `SELECT pg_advisory_xact_lock(hashtextextended('servicio:870', 0))`); err != nil {
		t.Fatal(err)
	}
	ent := e.entranteDe(e.enviar(id, secreto, "wh-031-candado-"+pruebas.Unico(), "servicio.eliminado", map[string]any{"servicio_id": 870, "empresa_id": 1}, time.Now()))
	listo := make(chan error, 1)
	go func() {
		_, err := e.mod.ProcesarEntrante.Ejecutar(pruebas.ContextoSistema(e.pool), ent)
		listo <- err
	}()
	select {
	case err := <-listo:
		t.Fatalf("no esperó el candado: %v", err)
	case <-time.After(300 * time.Millisecond):
	}
	if err := tx.Commit(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := <-listo; err != nil {
		t.Fatal(err)
	}
	if e.contar(`SELECT count(*) FROM integracion.argos_eliminados WHERE id = 870`) != 1 {
		t.Fatal("lápida tras soltar el candado")
	}
}

// N3: la eliminación de S (con la ficha de su cliente) y un refresco de esa ficha cuya lista trae
// [S2, S] (réplica atrasada) toman los candados en el mismo orden de id: con los dos esperando el
// de S, al soltarlo terminan ambos sin «deadlock detected».
func TestEliminacionYFichaSinInterbloqueo(t *testing.T) {
	e := nuevoEntorno(t)
	_, id, secreto := e.conectado()
	e.sembrarCliente(88, "Kira")
	e.argos.con(func() {
		// Primero el de id mayor: el orden de Argos no es el de los candados.
		e.argos.servicios[88] = append([]map[string]any{{"id": int64(881), "empresa_id": 1, "cliente_id": 88, "codigo": "S-2"}}, e.argos.servicios[88]...)
	})
	if f := e.usuario("lectura").pedir(http.MethodGet, "/api/v1/clientes/88", nil); f.status != 200 {
		t.Fatalf("ficha: %s", f.crudo)
	}
	// Argos ya eliminó 880 (GET 404) pero su lista todavía lo trae.
	e.argos.con(func() { e.argos.sinGet[880] = true })
	tx, err := e.pool.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if _, err := tx.Exec(t.Context(), `SELECT pg_advisory_xact_lock(hashtextextended('servicio:880', 0))`); err != nil {
		t.Fatal(err)
	}
	ctx := pruebas.ContextoSistema(e.pool)
	eliminar := e.entranteDe(e.enviar(id, secreto, "wh-031-dl-a-"+pruebas.Unico(), "servicio.eliminado", map[string]any{"servicio_id": 880, "empresa_id": 1}, time.Now()))
	refrescar := e.entranteDe(e.enviar(id, secreto, "wh-031-dl-b-"+pruebas.Unico(), "cliente.actualizado", map[string]any{"cliente_id": 88}, time.Now()))
	errs := make(chan error, 2)
	esperando := func(n int) {
		limite := time.Now().Add(10 * time.Second)
		for e.contar(`SELECT count(*) FROM pg_locks WHERE locktype = 'advisory' AND NOT granted`) < n {
			if time.Now().After(limite) {
				t.Fatalf("no llegaron %d a esperar el candado", n)
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	// La eliminación hace cola primero (al soltar el candado, gana ella); luego el refresco.
	for i, ent := range []int64{eliminar, refrescar} {
		go func() {
			_, err := e.mod.ProcesarEntrante.Ejecutar(ctx, ent)
			errs <- err
		}()
		esperando(i + 1)
	}
	if err := tx.Commit(t.Context()); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := <-errs; err != nil {
			t.Fatalf("interbloqueo o error: %v", err)
		}
	}
	if e.contar(`SELECT count(*) FROM integracion.argos_eliminados WHERE id = 880`) != 1 ||
		e.contar(`SELECT count(*) FROM integracion.argos_servicios WHERE id = 880`) != 0 ||
		e.contar(`SELECT count(*) FROM integracion.argos_servicios WHERE id = 881`) != 1 {
		t.Fatal("estado final")
	}
}

// B1: lápida vieja C1, una lectura cuyo GET por id da 200 en T y, antes de que esa lectura
// guarde, una eliminación real confirmada en C2 > T. La lápida queda con C2 (GREATEST) y la
// lectura no resucita el servicio.
func TestLapidaViejaYEliminacionConcurrenteNoResucita(t *testing.T) {
	e := nuevoEntorno(t)
	_, id, secreto := e.conectado()
	e.sembrarCliente(89, "Lía")
	if f := e.usuario("lectura").pedir(http.MethodGet, "/api/v1/clientes/89", nil); f.status != 200 {
		t.Fatalf("ficha: %s", f.crudo)
	}
	for _, sql := range []string{
		`DELETE FROM integracion.argos_servicios WHERE id = 890`,
		`INSERT INTO integracion.argos_eliminados (tipo, id, empresa_id, cliente_id, de_instantanea, conector_id, eliminado_en, confirmado_en)
			VALUES ('servicio', 890, 1, 89, true, ` + strconv.FormatInt(id, 10) + `, now() - interval '1 hour', now() - interval '1 hour')`,
	} {
		if _, err := e.pool.Exec(t.Context(), sql); err != nil {
			t.Fatal(err)
		}
	}
	c1 := e.consultarTexto(`SELECT confirmado_en::text FROM integracion.argos_eliminados WHERE id = 890`)
	ctx := pruebas.ContextoSistema(e.pool)
	// El refresco lee Argos (GET /servicios/890 → 200 en T) y queda detenido antes de su
	// transacción (alguien tiene su entrante).
	refrescar := e.entranteDe(e.enviar(id, secreto, "wh-031-b1-"+pruebas.Unico(), "cliente.actualizado", map[string]any{"cliente_id": 89}, time.Now()))
	tx, err := e.pool.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if _, err := tx.Exec(t.Context(), `SELECT 1 FROM integracion.entrantes WHERE id = $1 FOR UPDATE`, refrescar); err != nil {
		t.Fatal(err)
	}
	listo := make(chan error, 1)
	go func() {
		_, err := e.mod.ProcesarEntrante.Ejecutar(ctx, refrescar)
		listo <- err
	}()
	limite := time.Now().Add(10 * time.Second)
	for e.contar(`SELECT count(*) FROM pg_locks WHERE locktype = 'transactionid' AND NOT granted`) < 1 {
		if time.Now().After(limite) {
			t.Fatal("el refresco no llegó a su transacción")
		}
		time.Sleep(20 * time.Millisecond)
	}
	// Argos elimina el servicio de verdad (C2 > T) y la eliminación se aplica antes.
	e.argos.con(func() { e.argos.sinGet[890] = true })
	if r := e.procesarWebhook(id, secreto, "servicio.eliminado", map[string]any{"servicio_id": 890, "empresa_id": 1, "cliente_id": 89}); r.Resultado != "aplicado" {
		t.Fatalf("eliminación: %+v", r)
	}
	if e.contar(`SELECT count(*) FROM integracion.argos_eliminados WHERE id = 890 AND confirmado_en > $1::timestamptz`, c1) != 1 {
		t.Fatal("la lápida debía quedar con la confirmación nueva")
	}
	if err := tx.Commit(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := <-listo; err != nil {
		t.Fatal(err)
	}
	if e.contar(`SELECT count(*) FROM integracion.argos_servicios WHERE id = 890`) != 0 || e.contar(`SELECT count(*) FROM integracion.argos_eliminados WHERE id = 890`) != 1 ||
		e.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'argos.servicio_restaurado' AND payload->>'servicio_id' = '890'`) != 0 {
		t.Fatal("un GET 200 anterior a la confirmación nueva resucitó el servicio")
	}
}

// B5: un refresco de ficha (webhook de la empresa 1) que resucita un servicio de la empresa 2
// emite argos.servicio_restaurado con la empresa 2, pero la acción auditada y el entrante quedan
// con la empresa 1.
func TestRestauradoNoCambiaLaEmpresaDelCaso(t *testing.T) {
	e := nuevoEntorno(t)
	_, id, secreto := e.conectado()
	e.sembrarCliente(83, "Mara")
	e.argos.con(func() {
		e.argos.servicios[83] = append(e.argos.servicios[83], map[string]any{"id": int64(831), "empresa_id": 2, "cliente_id": 83, "codigo": "S-E2"})
	})
	if f := e.usuario("lectura").pedir(http.MethodGet, "/api/v1/clientes/83", nil); f.status != 200 {
		t.Fatalf("ficha: %s", f.crudo)
	}
	for _, sql := range []string{
		`DELETE FROM integracion.argos_servicios WHERE id = 831`,
		`INSERT INTO integracion.argos_eliminados (tipo, id, empresa_id, cliente_id, de_instantanea, conector_id, eliminado_en, confirmado_en)
			VALUES ('servicio', 831, 2, 83, true, ` + strconv.FormatInt(id, 10) + `, now() - interval '1 hour', now() - interval '1 hour')`,
	} {
		if _, err := e.pool.Exec(t.Context(), sql); err != nil {
			t.Fatal(err)
		}
	}
	r := e.procesarWebhook(id, secreto, "cliente.actualizado", map[string]any{"cliente_id": 83, "empresa_id": 1})
	if r.Resultado != "aplicado" || e.contar(`SELECT count(*) FROM integracion.argos_servicios WHERE id = 831`) != 1 {
		t.Fatalf("resucitar: %+v", r)
	}
	if e.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'argos.servicio_restaurado' AND payload->>'servicio_id' = '831' AND empresa_id = 2`) != 1 {
		t.Fatal("el evento lleva la empresa del servicio")
	}
	ent := strconv.FormatInt(r.EntranteID, 10)
	if e.contar(`SELECT count(*) FROM auditoria.acciones WHERE accion = 'integraciones.procesar_webhook' AND entidad_id = $1 AND empresa_id = 1`, ent) != 1 ||
		e.contar(`SELECT count(*) FROM auditoria.acciones WHERE accion = 'integraciones.procesar_webhook' AND entidad_id = $1 AND empresa_id IS DISTINCT FROM 1`, ent) != 0 {
		t.Fatal("la acción auditada debía quedar con la empresa 1")
	}
	if e.contar(`SELECT count(*) FROM integracion.entrantes WHERE id = $1 AND empresa_id = 1 AND resultado = 'aplicado' AND procesado_en IS NOT NULL`, r.EntranteID) != 1 {
		t.Fatal("el entrante debía quedar marcado con la empresa 1")
	}
}

// §2.1 coordenadas de routers ("lat,lng" o null) y §2.3 router.eliminado: se quita de la
// instantánea (factibilidad ya no lo ve), queda la lápida y una carga empezada antes de la
// confirmación no lo recrea; una posterior que lo trae lo resucita. empresa_id null = router
// compartido; un payload que no coincide con la instantánea se ignora.
func TestRouterCoordenadasYEliminado(t *testing.T) {
	e := nuevoEntorno(t)
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
		e.contar(`SELECT count(*) FROM integracion.argos_eliminados WHERE tipo = 'router' AND id = 9 AND nombre = 'RT-9' AND empresa_id = 1 AND conector_id = $1`, id) != 1 ||
		e.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'argos.router_eliminado' AND payload->>'router_id' = '9' AND empresa_id = 1`) != 1 {
		t.Fatal("router eliminado")
	}
	// Una carga empezada antes de la confirmación (lápida confirmada «después») no lo recrea.
	if _, err := e.pool.Exec(t.Context(), `UPDATE integracion.argos_eliminados SET confirmado_en = now() + interval '1 hour' WHERE tipo = 'router' AND id = 9`); err != nil {
		t.Fatal(err)
	}
	e.argos.con(func() { e.argos.routers = append([]map[string]any(nil), routers...) })
	if _, err := e.mod.SincronizarCatalogos.Ejecutar(ctx, struct{}{}); err != nil {
		t.Fatal(err)
	}
	if e.contar(`SELECT count(*) FROM integracion.argos_routers WHERE id = 9`) != 0 {
		t.Fatal("una carga anterior a la confirmación recreó el router")
	}
	// Una carga posterior que lo trae y cuyo GET por id responde 200 lo resucita.
	if _, err := e.pool.Exec(t.Context(), `UPDATE integracion.argos_eliminados SET confirmado_en = now() - interval '1 minute' WHERE tipo = 'router' AND id = 9`); err != nil {
		t.Fatal(err)
	}
	if _, err := e.mod.SincronizarCatalogos.Ejecutar(ctx, struct{}{}); err != nil {
		t.Fatal(err)
	}
	if e.contar(`SELECT count(*) FROM integracion.argos_routers WHERE id = 9`) != 1 || e.contar(`SELECT count(*) FROM integracion.argos_eliminados WHERE tipo = 'router' AND id = 9`) != 0 ||
		e.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'argos.router_restaurado' AND payload->>'router_id' = '9' AND empresa_id = 1`) != 1 {
		t.Fatal("la carga posterior (GET /routers/9 con 200) debía resucitar el router")
	}

	// Router compartido (empresa_id null): coincide con la instantánea (sin empresa).
	e.argos.con(func() { e.argos.quitarRouter(10) })
	if r := e.procesarWebhook(id, secreto, "router.eliminado", map[string]any{"router_id": 10, "empresa_id": nil}); r.Resultado != "aplicado" {
		t.Fatalf("router compartido: %+v", r)
	}
	if e.contar(`SELECT count(*) FROM integracion.argos_eliminados WHERE tipo = 'router' AND id = 10 AND empresa_id IS NULL`) != 1 {
		t.Fatal("lápida del router compartido")
	}
	// Un 404 HTML tampoco confirma un router, ni un 403 de alcance.
	e.argos.con(func() { e.argos.quitarRouter(11); e.argos.proxy404["/api/v1/routers/11"] = true })
	ent := e.entranteDe(e.enviar(id, secreto, "wh-031-rproxy-"+pruebas.Unico(), "router.eliminado", map[string]any{"router_id": 11, "empresa_id": nil}, time.Now()))
	if _, err := e.mod.ProcesarEntrante.Ejecutar(ctx, ent); err == nil || e.contar(`SELECT count(*) FROM integracion.argos_eliminados WHERE tipo = 'router' AND id = 11`) != 0 {
		t.Fatalf("404 HTML de router: %v", err)
	}
	e.argos.con(func() {
		delete(e.argos.proxy404, "/api/v1/routers/11")
		e.argos.fueraDeAlcance["/api/v1/routers/11"] = true
	})
	if _, err := e.mod.ProcesarEntrante.Ejecutar(ctx, ent); err == nil || e.contar(`SELECT count(*) FROM integracion.argos_eliminados WHERE tipo = 'router' AND id = 11`) != 0 {
		t.Fatalf("403 de router: %v", err)
	}
	e.argos.con(func() { delete(e.argos.fueraDeAlcance, "/api/v1/routers/11") })
	// El payload dice una empresa y la instantánea lo tiene compartido: se aplica con la
	// instantánea (sin empresa) y se anota la discrepancia.
	r = e.procesarWebhook(id, secreto, "router.eliminado", map[string]any{"router_id": 11, "empresa_id": 5})
	if r.Resultado != "aplicado" || e.contar(`SELECT count(*) FROM integracion.argos_routers WHERE id = 11`) != 0 ||
		e.contar(`SELECT count(*) FROM integracion.argos_eliminados WHERE tipo = 'router' AND id = 11 AND empresa_id IS NULL AND de_instantanea`) != 1 ||
		e.contar(`SELECT count(*) FROM auditoria.acciones WHERE accion = 'integraciones.procesar_webhook' AND entidad_id = $1
			AND entrada->'auditoria'->>'discrepancia' = 'empresa_no_coincide'`, strconv.FormatInt(r.EntranteID, 10)) != 1 {
		t.Fatalf("empresa distinta: %+v", r)
	}
}

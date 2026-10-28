package argos_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/riverqueue/river"

	"github.com/HCDestroyer/iris/apps/api/internal/integracion"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
	"github.com/HCDestroyer/iris/apps/api/migraciones"
)

// enviar firma y manda un webhook como lo haría Argos.
func (e *entorno) enviar(conectorID int64, secreto, id, evento string, payload map[string]any, marca time.Time) respuesta {
	e.t.Helper()
	cuerpo, _ := json.Marshal(map[string]any{"id": id, "evento": evento, "ocurrido_en": time.Now().UTC(), "payload": payload})
	ts := strconv.FormatInt(marca.Unix(), 10)
	return e.anonimo().pedir(http.MethodPost, fmt.Sprintf("/api/v1/integraciones/argosisp/webhook/%d", conectorID), cuerpo,
		"X-Webhook-Id", id, "X-Webhook-Timestamp", ts, "X-Webhook-Signature", argos.FirmarWebhook(secreto, ts, cuerpo))
}

func TestWebhookFirmaVentanaYDeduplicacion(t *testing.T) {
	e := nuevoEntorno(t)
	admin, id, secreto := e.conectado()
	ahora := time.Now()
	wid := "wh-" + pruebas.Unico()

	r := e.enviar(id, secreto, wid, "cliente.actualizado", map[string]any{"empresa_id": 1, "cliente_id": 5}, ahora)
	if r.status != 200 || r.cuerpo["duplicado"] != false {
		t.Fatalf("webhook: %d %s", r.status, r.crudo)
	}
	entrante := int64(r.cuerpo["entranteId"].(float64))
	// Encolado en la misma transacción.
	if n := e.contar(`SELECT count(*) FROM river.river_job WHERE kind = 'integracion.argos_procesar_entrante' AND (args->>'entrante_id')::bigint = $1`, entrante); n != 1 {
		t.Fatalf("job encolado: %d", n)
	}
	// Mismo X-Webhook-Id (reintento de Argos): 200 duplicado, una sola fila y un solo job.
	d := e.enviar(id, secreto, wid, "cliente.actualizado", map[string]any{"empresa_id": 1, "cliente_id": 5}, ahora)
	if d.status != 200 || d.cuerpo["duplicado"] != true || int64(d.cuerpo["entranteId"].(float64)) != entrante {
		t.Fatalf("duplicado: %d %s", d.status, d.crudo)
	}
	if n := e.contar(`SELECT count(*) FROM integracion.entrantes WHERE webhook_id = $1`, wid); n != 1 {
		t.Fatalf("entrantes: %d", n)
	}
	if n := e.contar(`SELECT count(*) FROM river.river_job WHERE kind = 'integracion.argos_procesar_entrante' AND (args->>'entrante_id')::bigint = $1`, entrante); n != 1 {
		t.Fatalf("jobs: %d", n)
	}
	if n := e.contar(`SELECT count(*) FROM integracion.entrantes WHERE webhook_id = $1 AND empresa_id = 1 AND evento = 'cliente.actualizado'`, wid); n != 1 {
		t.Fatal("entrante mal guardado")
	}

	// Firma inválida (otro secreto), fuera de ventana, conector inexistente, sin cabeceras.
	e.enviar(id, "otro-secreto", "wh-x1", "cliente.actualizado", map[string]any{}, ahora).problema(t, 401, "firma_invalida")
	e.enviar(id, secreto, "wh-x2", "cliente.actualizado", map[string]any{}, ahora.Add(-10*time.Minute)).problema(t, 401, "firma_vencida")
	e.enviar(id, secreto, "wh-x3", "cliente.actualizado", map[string]any{}, ahora.Add(10*time.Minute)).problema(t, 401, "firma_vencida")
	e.enviar(999999, secreto, "wh-x4", "cliente.actualizado", map[string]any{}, ahora).problema(t, 401, "firma_invalida")
	e.anonimo().pedir(http.MethodPost, fmt.Sprintf("/api/v1/integraciones/argosisp/webhook/%d", id), []byte(`{}`)).problema(t, 401, "firma_invalida")
	// Cuerpo alterado después de firmar.
	cuerpo := []byte(`{"id":"wh-x5","evento":"cliente.actualizado","payload":{}}`)
	ts := strconv.FormatInt(ahora.Unix(), 10)
	firma := argos.FirmarWebhook(secreto, ts, cuerpo)
	alterado := []byte(`{"id":"wh-x5","evento":"cliente.actualizado","payload":{"cliente_id":1}}`)
	e.anonimo().pedir(http.MethodPost, fmt.Sprintf("/api/v1/integraciones/argosisp/webhook/%d", id), alterado,
		"X-Webhook-Id", "wh-x5", "X-Webhook-Timestamp", ts, "X-Webhook-Signature", firma).problema(t, 401, "firma_invalida")
	// Varias firmas (rotación): basta una válida.
	ok := e.anonimo().pedir(http.MethodPost, fmt.Sprintf("/api/v1/integraciones/argosisp/webhook/%d", id), cuerpo,
		"X-Webhook-Id", "wh-x5", "X-Webhook-Timestamp", ts, "X-Webhook-Signature", "v1=00ff,"+firma)
	if ok.status != 200 {
		t.Fatalf("rotación de firma: %d %s", ok.status, ok.crudo)
	}
	if n := e.contar(`SELECT count(*) FROM integracion.entrantes WHERE webhook_id LIKE 'wh-x%' AND webhook_id <> 'wh-x5'`); n != 0 {
		t.Fatal("un webhook rechazado no debía guardarse")
	}
	// El secreto nunca queda en la auditoría.
	e.sinSecretos(secreto)
	if n := e.contar(`SELECT count(*) FROM auditoria.acciones WHERE accion = 'integraciones.webhook' AND resultado = 'error'`); n < 6 {
		t.Fatalf("los rechazos debían auditarse: %d", n)
	}

	// Conector apagado: 503 con Retry-After, para que Argos reintente.
	if r := admin.pedir(http.MethodPost, fmt.Sprintf("/api/v1/integraciones/conectores/%d/desactivar", id), map[string]any{"version": 2}); r.status != 200 {
		t.Fatalf("desactivar: %s", r.crudo)
	}
	a := e.enviar(id, secreto, "wh-apagado", "cliente.actualizado", map[string]any{}, ahora)
	a.problema(t, 503, "conector_apagado")
	if a.h.Get("Retry-After") == "" {
		t.Fatal("falta Retry-After")
	}
}

func (e *entorno) entranteDe(r respuesta) int64 {
	e.t.Helper()
	if r.status != 200 {
		e.t.Fatalf("webhook: %d %s", r.status, r.crudo)
	}
	return int64(r.cuerpo["entranteId"].(float64))
}

func TestProcesarEntranteIdempotente(t *testing.T) {
	e := nuevoEntorno(t)
	_, id, secreto := e.conectado()
	lectura := e.usuario("lectura")
	e.sembrarCliente(61, "Elena")
	if f := lectura.pedir(http.MethodGet, "/api/v1/clientes/61", nil); f.status != 200 {
		t.Fatalf("ficha: %s", f.crudo)
	}
	ctx := pruebas.ContextoSistema(e.pool)
	ahora := time.Now()

	// cliente.actualizado de un cliente en la instantánea: se refresca.
	e.argos.con(func() { e.argos.clientes[61]["nombre"] = "Elena Ruiz" })
	ent := e.entranteDe(e.enviar(id, secreto, "wh-a-"+pruebas.Unico(), "cliente.actualizado", map[string]any{"empresa_id": 1, "cliente_id": 61}, ahora))
	r, err := e.mod.ProcesarEntrante.Ejecutar(ctx, ent)
	if err != nil || r.Resultado != "aplicado" {
		t.Fatalf("procesar: %+v %v", r, err)
	}
	if e.consultarTexto(`SELECT nombre FROM integracion.argos_clientes WHERE id = 61`) != "Elena Ruiz" {
		t.Fatal("no se refrescó la instantánea")
	}
	// Repetir el job no hace nada.
	if r, err := e.mod.ProcesarEntrante.Ejecutar(ctx, ent); err != nil || r.Resultado != "ya_procesado" {
		t.Fatalf("repetir: %+v %v", r, err)
	}

	// servicio.config_cambiada con solo servicio_id: busca el cliente en la instantánea.
	e.argos.con(func() { e.argos.servicios[61][0]["plan"] = map[string]any{"id": 9, "nombre": "Fibra 200"} })
	ent = e.entranteDe(e.enviar(id, secreto, "wh-b-"+pruebas.Unico(), "servicio.config_cambiada", map[string]any{"empresa_id": 1, "servicio_id": 610}, ahora))
	if r, err := e.mod.ProcesarEntrante.Ejecutar(ctx, ent); err != nil || r.Resultado != "aplicado" {
		t.Fatalf("servicio: %+v %v", r, err)
	}
	if e.consultarTexto(`SELECT plan_nombre FROM integracion.argos_servicios WHERE id = 610`) != "Fibra 200" {
		t.Fatal("no se refrescó el servicio")
	}

	// servicio.creado de un cliente que Iris nunca mostró: se ignora (no crece la instantánea).
	ent = e.entranteDe(e.enviar(id, secreto, "wh-c-"+pruebas.Unico(), "servicio.creado", map[string]any{"empresa_id": 1, "servicio_id": 999, "cliente_id": 998}, ahora))
	if r, err := e.mod.ProcesarEntrante.Ejecutar(ctx, ent); err != nil || r.Resultado != "ignorado" {
		t.Fatalf("ignorado: %+v %v", r, err)
	}

	// servicio.estado_cambiado a retirado: refresca y emite argos.servicio_retirado una sola vez.
	e.argos.con(func() { e.argos.servicios[61][0]["estado"] = "retirado" })
	ent = e.entranteDe(e.enviar(id, secreto, "wh-d-"+pruebas.Unico(), "servicio.estado_cambiado",
		map[string]any{"empresa_id": 1, "servicio_id": 610, "cliente_id": 61, "de": "activo", "a": "retirado"}, ahora))
	for range 2 {
		if _, err := e.mod.ProcesarEntrante.Ejecutar(ctx, ent); err != nil {
			t.Fatal(err)
		}
	}
	if n := e.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'argos.servicio_retirado' AND payload->>'servicio_id' = '610' AND empresa_id = 1`); n != 1 {
		t.Fatalf("argos.servicio_retirado: %d", n)
	}
	if e.consultarTexto(`SELECT estado FROM integracion.argos_servicios WHERE id = 610`) != "retirado" {
		t.Fatal("no se refrescó el estado")
	}

	// Argos caído: error (River reintenta), intento contado y sin marcar procesado.
	e.argos.con(func() { e.argos.caido = true })
	ent = e.entranteDe(e.enviar(id, secreto, "wh-e-"+pruebas.Unico(), "cliente.actualizado", map[string]any{"empresa_id": 1, "cliente_id": 61}, ahora))
	if _, err := e.mod.ProcesarEntrante.Ejecutar(ctx, ent); err == nil {
		t.Fatal("debía fallar con Argos caído")
	}
	if n := e.contar(`SELECT count(*) FROM integracion.entrantes WHERE id = $1 AND procesado_en IS NULL AND intentos = 1 AND error IS NOT NULL`, ent); n != 1 {
		t.Fatal("el fallo debía quedar registrado en el entrante")
	}
	e.argos.con(func() { e.argos.caido = false })
	if r, err := e.mod.ProcesarEntrante.Ejecutar(ctx, ent); err != nil || r.Resultado != "aplicado" {
		t.Fatalf("tras volver Argos: %+v %v", r, err)
	}
}

func TestWebhookConRiver(t *testing.T) {
	e := nuevoEntorno(t)
	_, id, secreto := e.conectado()
	lectura := e.usuario("lectura")
	e.sembrarCliente(71, "Fabiola")
	if f := lectura.pedir(http.MethodGet, "/api/v1/clientes/71", nil); f.status != 200 {
		t.Fatalf("ficha: %s", f.crudo)
	}
	relay := &integracion.Relay{BD: e.pool, Bus: integracion.NuevoBus()}
	cliente, err := integracion.NuevoClienteRiver(e.pool, relay, integracion.OpcionesWorker{
		Esquema: migraciones.EsquemaRiver, RelayIntervalo: time.Hour,
		Registrar: func(w *river.Workers) { e.mod.RegistrarWorkers(w, e.pool) },
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancelar := context.WithCancel(context.Background())
	defer cancelar()
	if err := cliente.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() {
		parar, c := context.WithTimeout(context.Background(), 10*time.Second)
		defer c()
		_ = cliente.Stop(parar)
	}()

	e.argos.con(func() { e.argos.clientes[71]["telefono"] = "4444-1111" })
	ent := e.entranteDe(e.enviar(id, secreto, "wh-river-"+pruebas.Unico(), "cliente.actualizado", map[string]any{"empresa_id": 1, "cliente_id": 71}, time.Now()))
	limite := time.Now().Add(20 * time.Second)
	for time.Now().Before(limite) {
		if e.contar(`SELECT count(*) FROM integracion.entrantes WHERE id = $1 AND procesado_en IS NOT NULL`, ent) == 1 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if e.consultarTexto(`SELECT telefono FROM integracion.argos_clientes WHERE id = 71`) != "4444-1111" {
		t.Fatal("River no procesó el webhook")
	}
}

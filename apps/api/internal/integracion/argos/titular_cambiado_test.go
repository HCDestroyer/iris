package argos_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
)

// servicio.titular_cambiado (contrato 0.10.0 §2.3, B17): Iris relee al titular nuevo y, si Argos
// confirma que el servicio ya es suyo, refresca la instantánea y emite argos.titular_cambiado (sin
// documento ni motivo en texto). Un servicio que Iris no tiene se ignora.
func TestTitularCambiado(t *testing.T) {
	e := nuevoEntorno(t)
	e.argos.con(func() { e.argos.version = "1.6.0" })
	_, id, secreto := e.conectado()
	e.sembrarCliente(61, "Ana Pérez") // en Argos, el servicio 610 ya es de 61
	e.sembrarCliente(62, "Otro Cliente")
	e.exec(`INSERT INTO integracion.argos_clientes (id, empresa_id, empresas, nombre, codigo, telefono, direccion, sincronizado_en)
		VALUES (60, 1, '{1}', 'Menor de edad', 'C-60', '5555-0000', 'Zona 1', now()) ON CONFLICT (id) DO NOTHING`)
	e.exec(`INSERT INTO integracion.argos_servicios (id, empresa_id, cliente_id, plan_nombre, estado, sincronizado_en)
		VALUES (610, 1, 60, 'Fibra 100', 'pendiente_instalacion', now()) ON CONFLICT (id) DO NOTHING`)
	t.Cleanup(func() {
		e.exec("DELETE FROM integracion.argos_servicios WHERE id IN (610, 620)")
		e.exec("DELETE FROM integracion.argos_clientes WHERE id IN (60, 61, 62)")
	})
	payload := func(a int64) map[string]any {
		return map[string]any{"servicio_id": 610, "empresa_id": 1, "de_cliente_id": 60, "a_cliente_id": a, "existente": false,
			"posible_duplicado": false, "de_cliente_eliminado": true, "motivo_codigo": "menor_de_edad", "origen_tipo": "iris_orden", "origen_id": "9"}
	}
	eventos := `SELECT count(*) FROM integracion.outbox WHERE evento = 'argos.titular_cambiado'`

	// Argos no confirma (el servicio no está entre los del cliente 62): sin evento.
	e.procesarWebhook(id, secreto, "servicio.titular_cambiado", payload(62))
	if n := e.contar(eventos); n != 0 {
		t.Fatal("evento sin confirmar con Argos")
	}

	r := e.procesarWebhook(id, secreto, "servicio.titular_cambiado", payload(61))
	if r.Resultado != "aplicado" {
		t.Fatalf("titular cambiado: %+v", r)
	}
	if n := e.contar(eventos + ` AND (payload->>'servicio_id')::bigint = 610 AND (payload->>'a_cliente_id')::bigint = 61
		AND (payload->>'de_cliente_id')::bigint = 60 AND (payload->>'de_cliente_eliminado')::boolean AND NOT payload ? 'motivo'`); n != 1 {
		t.Fatal("falta argos.titular_cambiado")
	}
	if n := e.contar(`SELECT count(*) FROM integracion.argos_servicios WHERE id = 610 AND cliente_id = 61`); n != 1 {
		t.Fatal("la instantánea no pasó al titular nuevo")
	}
	// 0.14.1: tipo_titular (persona si Argos no lo manda).
	if n := e.contar(eventos + ` AND payload->>'tipo_titular' = 'persona'`); n != 1 {
		t.Fatal("argos.titular_cambiado sin tipo_titular")
	}

	// Un servicio que Iris no tiene: ignorado.
	otro := payload(61)
	otro["servicio_id"] = 999999
	if r := e.procesarWebhook(id, secreto, "servicio.titular_cambiado", otro); r.Resultado != "ignorado" {
		t.Fatalf("servicio sin instantánea: %+v", r)
	}
}

// MEDIA 2 de #280: dos avisos seguidos (A→B y B→C) que se procesan al revés. El de B→C (más
// reciente) se aplica; el de A→B (atrasado) no hace retroceder la instantánea ni emite nada, aunque
// la ficha de B que se leyó todavía dijera que el servicio era suyo.
func TestTitularCambiadoAtrasadoNoRetrocede(t *testing.T) {
	e := nuevoEntorno(t)
	e.argos.con(func() { e.argos.version = "1.6.0" })
	_, id, secreto := e.conectado()
	e.sembrarCliente(71, "Titular B")
	e.sembrarCliente(72, "Titular C")
	servicio := func(cliente int64) []map[string]any {
		return []map[string]any{{"id": 710, "empresa_id": 1, "cliente_id": cliente, "codigo": "S-710",
			"plan": map[string]any{"id": 8, "nombre": "Fibra 100"}, "router_id": 4, "estado": "pendiente_instalacion"}}
	}
	// Las dos fichas dicen que el servicio es suyo (la de B, leída antes del segundo cambio).
	e.argos.con(func() { e.argos.servicios[71], e.argos.servicios[72] = servicio(71), servicio(72) })
	e.exec(`INSERT INTO integracion.argos_clientes (id, empresa_id, empresas, nombre, codigo, telefono, direccion, sincronizado_en)
		VALUES (70, 1, '{1}', 'Titular A', 'C-70', '5555-0000', 'Zona 1', now()) ON CONFLICT (id) DO NOTHING`)
	e.exec(`INSERT INTO integracion.argos_servicios (id, empresa_id, cliente_id, plan_nombre, estado, sincronizado_en)
		VALUES (710, 1, 70, 'Fibra 100', 'pendiente_instalacion', now()) ON CONFLICT (id) DO NOTHING`)
	t.Cleanup(func() {
		e.exec("DELETE FROM integracion.argos_servicios WHERE id IN (710, 720)")
		e.exec("DELETE FROM integracion.argos_clientes WHERE id IN (70, 71, 72)")
	})
	cambio := func(de, a int64, ocurrido time.Time) string {
		ent := e.entranteDe(e.enviar(id, secreto, "wh-tit-"+pruebas.Unico(), "servicio.titular_cambiado", map[string]any{
			"servicio_id": 710, "empresa_id": 1, "de_cliente_id": de, "a_cliente_id": a, "existente": false, "posible_duplicado": false,
			"de_cliente_eliminado": false, "motivo_codigo": "otro", "origen_tipo": "iris_orden", "origen_id": "9", "tipo_titular": "empresa",
		}, time.Now()))
		e.exec("UPDATE integracion.entrantes SET ocurrido_en = $1 WHERE id = $2", ocurrido, ent)
		r, err := e.mod.ProcesarEntrante.Ejecutar(pruebas.ContextoSistema(e.pool), ent)
		if err != nil {
			t.Fatal(err)
		}
		return r.Resultado
	}
	t0 := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	if r := cambio(71, 72, t0.Add(2*time.Minute)); r != "aplicado" {
		t.Fatalf("B→C: %s", r)
	}
	cambio(70, 71, t0.Add(time.Minute))
	if n := e.contar(`SELECT count(*) FROM integracion.argos_servicios WHERE id = 710 AND cliente_id = 72 AND titular_cambiado_en = $1`, t0.Add(2*time.Minute)); n != 1 {
		t.Fatal("el aviso atrasado hizo retroceder la instantánea")
	}
	if n := e.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'argos.titular_cambiado' AND (payload->>'servicio_id')::bigint = 710`); n != 1 {
		t.Fatalf("eventos: %d (solo el de B→C)", n)
	}
	if n := e.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'argos.titular_cambiado' AND (payload->>'servicio_id')::bigint = 710
		AND (payload->>'a_cliente_id')::bigint = 72 AND payload->>'tipo_titular' = 'empresa'`); n != 1 {
		t.Fatal("B→C sin tipo_titular empresa")
	}
	if n := e.contar(`SELECT count(*) FROM auditoria.acciones WHERE accion = 'integraciones.procesar_webhook' AND entrada->'auditoria'->>'titular' = 'atrasado'`); n != 1 {
		t.Fatal("el aviso atrasado no quedó anotado")
	}
}

// Verificación de #280 (reloj): solo se ordenan avisos por la hora que da Argos. Un aviso sin
// ocurrido_en (hora de recepción de Iris) o con una hora futura (más allá de la recepción más la
// tolerancia) no deja hora en la instantánea ni congela el servicio: manda la ficha releída de
// Argos. El origen de la hora queda en integracion.entrantes.ocurrido_origen.
func TestTitularCambiadoRelojDeIris(t *testing.T) {
	e := nuevoEntorno(t)
	e.argos.con(func() { e.argos.version = "1.6.0" })
	_, id, secreto := e.conectado()
	e.sembrarCliente(81, "Titular B")
	e.sembrarCliente(82, "Titular C")
	servicio := func(cliente int64) []map[string]any {
		return []map[string]any{{"id": 810, "empresa_id": 1, "cliente_id": cliente, "codigo": "S-810",
			"plan": map[string]any{"id": 8, "nombre": "Fibra 100"}, "router_id": 4, "estado": "pendiente_instalacion"}}
	}
	e.exec(`INSERT INTO integracion.argos_clientes (id, empresa_id, empresas, nombre, codigo, telefono, direccion, sincronizado_en)
		VALUES (80, 1, '{1}', 'Titular A', 'C-80', '5555-0000', 'Zona 1', now()) ON CONFLICT (id) DO NOTHING`)
	e.exec(`INSERT INTO integracion.argos_servicios (id, empresa_id, cliente_id, plan_nombre, estado, sincronizado_en)
		VALUES (810, 1, 80, 'Fibra 100', 'pendiente_instalacion', now()) ON CONFLICT (id) DO NOTHING`)
	t.Cleanup(func() {
		e.exec("DELETE FROM integracion.argos_servicios WHERE id IN (810, 820)")
		e.exec("DELETE FROM integracion.argos_clientes WHERE id IN (80, 81, 82)")
	})
	cambio := func(de, a int64, ocurrido string) (string, int64) {
		t.Helper()
		e.argos.con(func() { e.argos.servicios[a] = servicio(a) })
		wid := "wh-reloj-" + pruebas.Unico()
		cuerpo := map[string]any{"id": wid, "evento": "servicio.titular_cambiado", "payload": map[string]any{
			"servicio_id": 810, "empresa_id": 1, "de_cliente_id": de, "a_cliente_id": a, "existente": false, "posible_duplicado": false,
			"de_cliente_eliminado": false, "motivo_codigo": "otro", "origen_tipo": "iris_orden", "origen_id": "9"}}
		if ocurrido != "" {
			cuerpo["ocurrido_en"] = ocurrido
		}
		crudo, _ := json.Marshal(cuerpo)
		ent := e.entranteDe(e.firmado(id, secreto, wid, crudo, time.Now()))
		r, err := e.mod.ProcesarEntrante.Ejecutar(pruebas.ContextoSistema(e.pool), ent)
		if err != nil {
			t.Fatal(err)
		}
		return r.Resultado, ent
	}
	// Sin ocurrido_en: la hora es de Iris (origen iris); se aplica por la ficha y no deja hora.
	r, ent := cambio(80, 81, "")
	if r != "aplicado" || e.contar(`SELECT count(*) FROM integracion.entrantes WHERE id = $1 AND ocurrido_origen = 'iris'`, ent) != 1 {
		t.Fatalf("sin ocurrido_en: %s", r)
	}
	if e.contar(`SELECT count(*) FROM integracion.argos_servicios WHERE id = 810 AND cliente_id = 81 AND titular_cambiado_en IS NULL`) != 1 {
		t.Fatal("la hora de recepción de Iris quedó como hora del cambio")
	}
	// Con una hora futura (reloj de Argos adelantado un día): tampoco deja hora.
	r, ent = cambio(81, 82, time.Now().UTC().Add(24*time.Hour).Format(time.RFC3339))
	if r != "aplicado" || e.contar(`SELECT count(*) FROM integracion.entrantes WHERE id = $1 AND ocurrido_origen = 'argos'`, ent) != 1 {
		t.Fatalf("hora futura: %s", r)
	}
	if e.contar(`SELECT count(*) FROM integracion.argos_servicios WHERE id = 810 AND cliente_id = 82 AND titular_cambiado_en IS NULL`) != 1 {
		t.Fatal("la hora futura congeló el servicio")
	}
	// El siguiente aviso, con la hora real de Argos, se aplica y sí la deja.
	ahora := time.Now().UTC().Truncate(time.Second)
	if r, _ := cambio(82, 81, ahora.Format(time.RFC3339)); r != "aplicado" {
		t.Fatalf("tras la hora futura: %s", r)
	}
	if e.contar(`SELECT count(*) FROM integracion.argos_servicios WHERE id = 810 AND cliente_id = 81 AND titular_cambiado_en = $1`, ahora) != 1 {
		t.Fatal("la hora de Argos no quedó en la instantánea")
	}
	// Uno sin hora después: no se compara con la de Argos; manda la ficha.
	if r, _ := cambio(81, 82, ""); r != "aplicado" {
		t.Fatalf("sin hora tras una de Argos: %s", r)
	}
	if n := e.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'argos.titular_cambiado' AND (payload->>'servicio_id')::bigint = 810`); n != 4 {
		t.Fatalf("eventos: %d (los cuatro avisos confirmados por la ficha)", n)
	}
}

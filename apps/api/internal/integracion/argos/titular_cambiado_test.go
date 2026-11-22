package argos_test

import (
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

package argos_test

import "testing"

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

	// Un servicio que Iris no tiene: ignorado.
	otro := payload(61)
	otro["servicio_id"] = 999999
	if r := e.procesarWebhook(id, secreto, "servicio.titular_cambiado", otro); r.Resultado != "ignorado" {
		t.Fatalf("servicio sin instantánea: %+v", r)
	}
}

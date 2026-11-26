package argos_test

import (
	"context"
	"testing"
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
)

// Contrato 0.12.0 §2.3 (B27): inventario.unidad_estado_cambiado e inventario.equipo_cambiado llenan el
// estado y el historial de cada unidad (sin costos); un webhook más viejo no pisa el estado; uno
// repetido no duplica; fuera del alcance, con payload inválido o con un conector anterior a la
// 0.12.0, se ignoran.
func TestWebhooksEquiposDelCliente(t *testing.T) {
	e := nuevoEntorno(t)
	_, id, secreto := e.conectado()
	ctx := pruebas.ContextoSistema(e.pool)
	t.Cleanup(func() {
		for _, sql := range []string{"DELETE FROM integracion.argos_unidades_historial", "DELETE FROM integracion.argos_unidades"} {
			if _, err := e.pool.Exec(context.Background(), sql); err != nil {
				t.Error(err)
			}
		}
	})
	e.exec(`UPDATE integracion.conectores SET contrato_remoto = '0.12.0', alcance_global = false, empresas = ARRAY[1]::bigint[] WHERE id = $1`, id)
	// enviarEn: el webhook con su ocurrido_en, procesado.
	enviarEn := func(evento string, payload map[string]any, en time.Time) argos.ResultadoEntrante {
		t.Helper()
		ent := e.entranteDe(e.enviar(id, secreto, "wh-eq-"+pruebas.Unico(), evento, payload, time.Now()))
		e.exec(`UPDATE integracion.entrantes SET ocurrido_en = $2 WHERE id = $1`, ent, en)
		r, err := e.mod.ProcesarEntrante.Ejecutar(ctx, ent)
		if err != nil {
			t.Fatalf("%s: %v", evento, err)
		}
		return r
	}
	ahora := time.Now().UTC().Truncate(time.Second)
	estado := func(unidad int64) (string, *int64) {
		t.Helper()
		var est *string
		var servicio *int64
		if err := e.pool.QueryRow(ctx, `SELECT estado, servicio_id FROM integracion.argos_unidades WHERE unidad_id = $1`, unidad).Scan(&est, &servicio); err != nil {
			t.Fatal(err)
		}
		if est == nil {
			return "", servicio
		}
		return *est, servicio
	}

	// Cambio de equipo desde una orden de Iris: la que entra queda en el servicio, la que sale sin él;
	// historial de las dos, con la orden; el evento interno sin costos.
	cambio := map[string]any{"servicio_id": 4410, "cliente_id": 990, "empresa_id": 1, "sale_unidad_id": 7001, "entra_unidad_id": 7002,
		"motivo": "danado", "destino_sale": "revision", "origen": "iris_orden", "origen_id": "555", "costo": "350.00", "proveedor": "X"}
	if r := enviarEn(argos.EventoEquipoCambiado, cambio, ahora); r.Resultado != "aplicado" {
		t.Fatalf("equipo_cambiado: %+v", r)
	}
	if _, s := estado(7002); s == nil || *s != 4410 {
		t.Fatalf("la que entra queda en el servicio: %v", s)
	}
	if _, s := estado(7001); s != nil {
		t.Fatalf("la que sale queda sin servicio: %v", *s)
	}
	if n := e.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = $1 AND (payload->>'entra_unidad_id')::bigint = 7002
		AND payload->>'origen_id' = '555' AND NOT payload ? 'costo' AND NOT payload ? 'proveedor'`, argos.EventoEquipoCambiadoInterno); n != 1 {
		t.Fatalf("argos.equipo_cambiado: %d", n)
	}

	// Estados: la que sale pasa a revisión; uno más viejo llega después y no la pisa (sí queda en el
	// historial); el mismo webhook repetido no duplica.
	revision := map[string]any{"unidad_id": 7001, "empresa_id": 1, "servicio_id": nil, "de": "en_comodato", "a": "revision", "motivo": "cambio_equipo"}
	if r := enviarEn(argos.EventoUnidadEstadoCambiado, revision, ahora.Add(time.Minute)); r.Resultado != "aplicado" {
		t.Fatalf("unidad_estado_cambiado: %+v", r)
	}
	viejo := map[string]any{"unidad_id": 7001, "empresa_id": 1, "servicio_id": 4410, "de": "disponible", "a": "en_comodato", "motivo": "comodato"}
	enviarEn(argos.EventoUnidadEstadoCambiado, viejo, ahora.Add(-time.Hour))
	if est, s := estado(7001); est != "revision" || s != nil {
		t.Fatalf("un aviso más viejo no pisa el estado ni el servicio: %s %v", est, s)
	}
	if n := e.contar(`SELECT count(*) FROM integracion.argos_unidades_historial WHERE unidad_id = 7001`); n != 3 {
		t.Fatalf("historial de la que sale (sale, revisión y el viejo): %d", n)
	}
	// El mismo entrante procesado otra vez no cambia nada.
	ent := e.entranteDe(e.enviar(id, secreto, "wh-eq-rep-"+pruebas.Unico(), argos.EventoUnidadEstadoCambiado,
		map[string]any{"unidad_id": 7002, "empresa_id": 1, "servicio_id": 4410, "de": "disponible", "a": "en_comodato"}, time.Now()))
	for range 2 {
		if _, err := e.mod.ProcesarEntrante.Ejecutar(ctx, ent); err != nil {
			t.Fatal(err)
		}
	}
	if n := e.contar(`SELECT count(*) FROM integracion.argos_unidades_historial WHERE unidad_id = 7002 AND tipo = 'estado'`); n != 1 {
		t.Fatalf("webhook repetido: %d", n)
	}

	// Lectura para la orden: las del servicio y las de la orden, del historial más nuevo al más viejo.
	us, err := argos.EstadosDeUnidades(ctx, e.pool, []int64{1}, 4410, []int64{7001})
	if err != nil {
		t.Fatal(err)
	}
	if len(us) != 2 || us[0].UnidadID != 7001 || *us[0].Estado != "revision" || len(us[0].Historial) != 3 || us[0].Historial[0].Tipo != "estado" ||
		us[0].Historial[1].Tipo != "sale" || us[0].Historial[1].OrdenID == nil || *us[0].Historial[1].OrdenID != "555" ||
		*us[0].Historial[1].DestinoSale != "revision" || us[1].UnidadID != 7002 || *us[1].Estado != "en_comodato" {
		t.Fatalf("estados de la orden: %+v", us)
	}
	if otra, _ := argos.EstadosDeUnidades(ctx, e.pool, []int64{2}, 4410, []int64{7001}); len(otra) != 0 {
		t.Fatalf("otra empresa no ve las unidades: %+v", otra)
	}

	// Ignorados: empresa fuera del alcance, estado desconocido, cambio sin la que entra y un conector
	// anterior a la 0.12.0.
	for _, c := range []struct {
		evento, omitido string
		payload         map[string]any
	}{
		{argos.EventoUnidadEstadoCambiado, "empresa_fuera_de_alcance", map[string]any{"unidad_id": 7003, "empresa_id": 2, "a": "revision"}},
		{argos.EventoUnidadEstadoCambiado, "payload_invalido", map[string]any{"unidad_id": 7003, "empresa_id": 1, "a": "vendida"}},
		{argos.EventoEquipoCambiado, "payload_invalido", map[string]any{"servicio_id": 4410, "empresa_id": 1, "sale_unidad_id": 7001}},
	} {
		if r := enviarEn(c.evento, c.payload, ahora); r.Resultado != "ignorado" || e.omitidoDe(r.EntranteID) != c.omitido {
			t.Fatalf("%s %v: %+v %s", c.evento, c.payload, r, e.omitidoDe(r.EntranteID))
		}
	}
	e.exec(`UPDATE integracion.conectores SET contrato_remoto = '0.11.0' WHERE id = $1`, id)
	if r := enviarEn(argos.EventoUnidadEstadoCambiado, revision, ahora.Add(2*time.Minute)); r.Resultado != "ignorado" || e.omitidoDe(r.EntranteID) != "contrato_anterior" {
		t.Fatalf("contrato anterior: %+v", r)
	}
	if n := e.contar(`SELECT count(*) FROM integracion.argos_unidades WHERE unidad_id = 7003`); n != 0 {
		t.Fatalf("lo ignorado no se guarda: %d", n)
	}
}

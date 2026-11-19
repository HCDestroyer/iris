package argos_test

import (
	"testing"

	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
)

// §2.3 / §7.4 (0.8.0): promesa.* sale como argos.promesa_cambiada, con motivo_reconexion y sin monto.
// Un Argos anterior (sin motivo_reconexion) o un valor desconocido queda null.
func TestWebhookPromesaEmiteEventoInterno(t *testing.T) {
	e := nuevoEntorno(t)
	_, id, secreto := e.conectado()
	promesa := int64(770000) + int64(e.contar(`SELECT count(*)::int FROM integracion.outbox`))
	base := func(extra map[string]any) map[string]any {
		p := map[string]any{"promesa_id": promesa, "empresa_id": 1, "cliente_id": 1201, "servicio_id": 4410, "tipo": "reconexion_temporal",
			"canal": "ia", "comprobante_id": 9901, "vence": "2026-11-19T06:00:00.000Z", "monto": "250.00"}
		for k, v := range extra {
			p[k] = v
		}
		return p
	}
	r := e.procesarWebhook(id, secreto, argos.EventoPromesaCreada, base(map[string]any{"motivo_reconexion": "sin_personal"}))
	if r.Resultado != "aplicado" {
		t.Fatalf("promesa.creada: %+v", r)
	}
	if n := e.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'argos.promesa_cambiada' AND (payload->>'promesa_id')::bigint = $1
		AND payload->>'estado' = 'activa' AND payload->>'motivo_reconexion' = 'sin_personal' AND NOT payload ? 'monto'
		AND payload->>'vence' = '2026-11-19T06:00:00Z' AND (payload->>'comprobante_id')::bigint = 9901 AND empresa_id = 1`, promesa); n != 1 {
		t.Fatalf("argos.promesa_cambiada (creada): %d", n)
	}
	// Argos 1.17.x no manda motivo_reconexion; un valor raro tampoco pasa; el motivo de cancelada es de
	// una lista cerrada.
	r = e.procesarWebhook(id, secreto, argos.EventoPromesaCancelada, base(map[string]any{"motivo": "texto libre de Argos"}))
	if n := e.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'argos.promesa_cambiada' AND (payload->>'entrante_id')::bigint = $1
		AND payload->>'estado' = 'cancelada' AND payload->>'motivo_reconexion' IS NULL AND payload->>'motivo' = 'otro'`, r.EntranteID); r.Resultado != "aplicado" || n != 1 {
		t.Fatalf("promesa.cancelada sin motivo_reconexion: %+v %d", r, n)
	}
	r = e.procesarWebhook(id, secreto, argos.EventoPromesaCumplida, base(map[string]any{"motivo_reconexion": "porque_si"}))
	if n := e.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'argos.promesa_cambiada' AND (payload->>'entrante_id')::bigint = $1
		AND payload->>'motivo_reconexion' IS NULL AND payload->>'motivo' IS NULL`, r.EntranteID); n != 1 {
		t.Fatalf("motivo_reconexion desconocido: %d", n)
	}
	// Una promesa normal: motivo_reconexion siempre null.
	r = e.procesarWebhook(id, secreto, argos.EventoPromesaIncumplida, base(map[string]any{"tipo": "promesa", "comprobante_id": nil, "motivo_reconexion": "sin_personal"}))
	if n := e.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'argos.promesa_cambiada' AND (payload->>'entrante_id')::bigint = $1
		AND payload->>'motivo_reconexion' IS NULL AND payload->>'tipo' = 'promesa'`, r.EntranteID); n != 1 {
		t.Fatalf("promesa normal: %d", n)
	}
	// Una empresa fuera del alcance del conector: descartado.
	if _, err := e.pool.Exec(pruebas.ContextoSistema(e.pool), `UPDATE integracion.conectores SET alcance_global = false, empresas = '{1}' WHERE id = $1`, id); err != nil {
		t.Fatal(err)
	}
	r = e.procesarWebhook(id, secreto, argos.EventoPromesaCreada, base(map[string]any{"empresa_id": 7, "motivo_reconexion": "sin_personal"}))
	if r.Resultado != "ignorado" || e.omitidoDe(r.EntranteID) != "empresa_fuera_de_alcance" {
		t.Fatalf("empresa fuera de alcance: %+v %s", r, e.omitidoDe(r.EntranteID))
	}
	// Sin empresa_id: payload inválido.
	r = e.procesarWebhook(id, secreto, argos.EventoPromesaCreada, base(map[string]any{"empresa_id": nil}))
	if r.Resultado != "ignorado" || e.omitidoDe(r.EntranteID) != "payload_invalido" {
		t.Fatalf("sin empresa: %+v", r)
	}
	// Sin promesa_id: se ignora y queda dicho.
	r = e.procesarWebhook(id, secreto, argos.EventoPromesaCreada, map[string]any{"empresa_id": 1})
	if r.Resultado != "ignorado" || e.omitidoDe(r.EntranteID) != "payload_invalido" {
		t.Fatalf("payload inválido: %+v", r)
	}
}

// §4.1: sin_personal solo con contrato ≥ 0.8.0. ContratoIris sigue en 0.7.1 (lo sube el líder).
func TestSoportaSinPersonal(t *testing.T) {
	v := func(s string) *string { return &s }
	for _, c := range []struct {
		contrato *string
		ok       bool
	}{{nil, false}, {v("0.7.1"), false}, {v("0.7.9"), false}, {v("0.8.0"), true}, {v("0.8.1"), true}, {v("1.0.0"), true}, {v("basura"), false}} {
		if got := argos.SoportaSinPersonal(c.contrato); got != c.ok {
			t.Errorf("%v: %v", c.contrato, got)
		}
	}
	if argos.ContratoIris != "0.7.1" {
		t.Errorf("ContratoIris cambió: %s", argos.ContratoIris)
	}
}

// El conector activo decide: un Argos 0.7.1 no acepta sin_personal; uno 0.8.0 sí.
func TestReconexionSinPersonalPorConector(t *testing.T) {
	e := nuevoEntorno(t)
	_, conector, _ := e.conectado()
	ctx := pruebas.ContextoSistema(e.pool)
	for _, c := range []struct {
		contrato string
		ok       bool
	}{{"0.7.1", false}, {"0.8.0", true}} {
		if _, err := e.pool.Exec(ctx, `UPDATE integracion.conectores SET contrato_remoto = $2 WHERE id = $1`, conector, c.contrato); err != nil {
			t.Fatal(err)
		}
		ok, err := e.mod.ReconexionSinPersonal(ctx, e.pool)
		if err != nil || ok != c.ok {
			t.Fatalf("contrato %s: %v %v", c.contrato, ok, err)
		}
	}
}

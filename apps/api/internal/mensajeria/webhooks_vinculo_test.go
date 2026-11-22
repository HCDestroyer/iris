package mensajeria_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
)

// Contrato 0.14.0 §6.6: tecnico.argos_vinculado y tecnico.argos_desvinculado van solo a un Argos con
// contrato ≥ 0.14.0, solo al conector del que salió el id_externo y solo en las empresas de su
// alcance, con los campos del contrato (sin conector_id ni carga_id). El vinculado del técnico nuevo
// espera el 2xx del desvinculado anterior del mismo id_externo.
func TestWebhooksVinculoTecnico(t *testing.T) {
	e := nuevoEntorno(t, nil)
	emitir := func(evento string, payload map[string]any) string {
		t.Helper()
		id := uuid.Must(uuid.NewV7())
		crudo, _ := json.Marshal(payload)
		if _, err := e.bd.Exec(e.ctx, `INSERT INTO integracion.outbox (id, evento, empresa_id, payload) VALUES ($1, $2, $3, $4)`,
			id, evento, payload["empresa_id"], crudo); err != nil {
			t.Fatal(err)
		}
		return id.String()
	}
	vinculo := func(tecnico, usuario int64, empresa any, version int, ext string, conector any, carga any) map[string]any {
		return map[string]any{"tecnico_id": tecnico, "usuario_id": usuario, "empresa_id": empresa, "version": version,
			"id_externo": ext, "anterior_id_externo": nil, "conector_id": conector, "carga_id": carga}
	}
	estado := func(id string) string {
		t.Helper()
		var s string
		if err := e.bd.QueryRow(e.ctx, `SELECT estado FROM mensajeria.webhooks_salida WHERE id = $1`, id).Scan(&s); err != nil {
			t.Fatal(err)
		}
		return s
	}

	// Con un contrato anterior a 0.14.0 queda omitido.
	viejo := emitir("tecnico.argos_vinculado", vinculo(7001, 71, 1, 1, "u-71", e.conector, nil))
	e.publicar()
	if estado(viejo) != "omitido" || len(e.receptor.de("tecnico.argos_vinculado", "tecnico_id", 7001)) != 0 {
		t.Fatal("tecnico.argos_vinculado no va a un Argos con contrato < 0.14.0")
	}

	if _, err := e.bd.Exec(e.ctx, `UPDATE integracion.conectores SET contrato_remoto = '0.14.0', empresas = '{1}', alcance_global = false WHERE id = $1`, e.conector); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = e.bd.Exec(e.ctx, `UPDATE integracion.conectores SET contrato_remoto = '0.6.0', empresas = '{}', alcance_global = NULL WHERE id = $1`, e.conector)
	})
	carga := uuid.Must(uuid.NewV7())
	ok := emitir("tecnico.argos_vinculado", vinculo(7002, 72, 1, 3, "u-72", e.conector, carga.String()))
	otroConector := emitir("tecnico.argos_vinculado", vinculo(7003, 73, 1, 1, "u-73", e.conector+1000, nil))
	fueraDeAlcance := emitir("tecnico.argos_vinculado", vinculo(7004, 74, 2, 1, "u-74", e.conector, nil))
	sinEmpresa := emitir("tecnico.argos_vinculado", vinculo(7005, 75, nil, 1, "u-75", e.conector, nil))
	e.publicar()
	ws := e.receptor.de("tecnico.argos_vinculado", "tecnico_id", 7002)
	if len(ws) != 1 || estado(ok) != "entregado" {
		t.Fatalf("vinculado al conector de origen: %d %s", len(ws), estado(ok))
	}
	p := ws[0].Cuerpo["payload"].(map[string]any)
	for _, campo := range []string{"conector_id", "carga_id"} {
		if _, hay := p[campo]; hay {
			t.Fatalf("%s no viaja a Argos: %v", campo, p)
		}
	}
	if fmt.Sprint(p["empresa_id"]) != "1" || fmt.Sprint(p["version"]) != "3" || p["id_externo"] != "u-72" || fmt.Sprint(p["usuario_id"]) != "72" {
		t.Fatalf("payload del vinculado: %v", p)
	}
	if n := e.contar(`SELECT count(*) FROM mensajeria.webhooks_salida WHERE id = $1 AND carga_id = $2`, ok, carga); n != 1 {
		t.Fatal("la carga queda en su columna")
	}
	if estado(otroConector) != "omitido" || estado(fueraDeAlcance) != "omitido" {
		t.Fatalf("otro conector u otra empresa: %s %s", estado(otroConector), estado(fueraDeAlcance))
	}
	if e.contar(`SELECT count(*) FROM mensajeria.webhooks_salida WHERE id = $1`, sinEmpresa) != 0 {
		t.Fatal("sin empresa no se programa")
	}

	// El id_externo u-76 pasa del técnico 7006 al 7007: el vinculado del nuevo espera el 2xx del
	// desvinculado del anterior, sin gastar intentos.
	e.receptor.fallar.Store(1)
	des := emitir("tecnico.argos_desvinculado", map[string]any{"tecnico_id": 7006, "usuario_id": 76, "empresa_id": 1, "version": 2,
		"id_externo": "u-76", "motivo": nil, "conector_id": e.conector})
	e.publicar()
	if estado(des) != "pendiente" {
		t.Fatalf("el desvinculado falló una vez: %s", estado(des))
	}
	nuevo := emitir("tecnico.argos_vinculado", vinculo(7007, 77, 1, 1, "u-76", e.conector, nil))
	e.publicar()
	if estado(nuevo) != "pendiente" || len(e.receptor.de("tecnico.argos_vinculado", "tecnico_id", 7007)) != 0 {
		t.Fatal("el vinculado nuevo espera el 2xx del desvinculado anterior")
	}
	if n := e.contar(`SELECT count(*) FROM mensajeria.webhooks_salida WHERE id = $1 AND intentos = 0`, nuevo); n != 1 {
		t.Fatal("esperar no gasta intentos")
	}
	// Pasado el reintento del desvinculado, sale él y después el vinculado.
	e.relojMu.Lock()
	*e.reloj = e.reloj.Add(time.Hour)
	e.relojMu.Unlock()
	e.publicar()
	if estado(des) != "entregado" {
		t.Fatalf("desvinculado: %s", estado(des))
	}
	e.relojMu.Lock()
	*e.reloj = e.reloj.Add(2 * time.Minute)
	e.relojMu.Unlock()
	e.publicar()
	if estado(nuevo) != "entregado" || len(e.receptor.de("tecnico.argos_vinculado", "tecnico_id", 7007)) != 1 {
		t.Fatalf("vinculado tras el 2xx del desvinculado: %s", estado(nuevo))
	}
	d := e.receptor.de("tecnico.argos_desvinculado", "tecnico_id", 7006)
	if len(d) != 1 {
		t.Fatalf("desvinculado entregado una vez: %d", len(d))
	}
	if pd := d[0].Cuerpo["payload"].(map[string]any); pd["motivo"] != nil || pd["id_externo"] != "u-76" {
		t.Fatalf("payload del desvinculado: %v", pd)
	}
}

// M-3: el mismo técnico cambia de id_externo (A → B) en un solo cambio: el vinculado de B (versión
// mayor) espera el 2xx del desvinculado de A, aunque los dos lleguen juntos a la cola.
func TestVinculadoEsperaElDesvinculadoDelMismoTecnico(t *testing.T) {
	e := nuevoEntorno(t, nil)
	if _, err := e.bd.Exec(e.ctx, `UPDATE integracion.conectores SET contrato_remoto = '0.14.0', alcance_global = true WHERE id = $1`, e.conector); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = e.bd.Exec(e.ctx, `UPDATE integracion.conectores SET contrato_remoto = '0.6.0', alcance_global = NULL WHERE id = $1`, e.conector)
	})
	emitir := func(evento string, payload map[string]any) string {
		t.Helper()
		id := uuid.Must(uuid.NewV7())
		crudo, _ := json.Marshal(payload)
		if _, err := e.bd.Exec(e.ctx, `INSERT INTO integracion.outbox (id, evento, empresa_id, payload) VALUES ($1, $2, 1, $3)`, id, evento, crudo); err != nil {
			t.Fatal(err)
		}
		return id.String()
	}
	estado := func(id string) string {
		t.Helper()
		var s string
		if err := e.bd.QueryRow(e.ctx, `SELECT estado FROM mensajeria.webhooks_salida WHERE id = $1`, id).Scan(&s); err != nil {
			t.Fatal(err)
		}
		return s
	}
	e.receptor.fallar.Store(1)
	des := emitir("tecnico.argos_desvinculado", map[string]any{"tecnico_id": 7101, "usuario_id": 781, "empresa_id": 1, "version": 7,
		"id_externo": "u-781-a", "motivo": nil, "conector_id": e.conector})
	vin := emitir("tecnico.argos_vinculado", map[string]any{"tecnico_id": 7101, "usuario_id": 781, "empresa_id": 1, "version": 8,
		"id_externo": "u-781-b", "anterior_id_externo": "u-781-a", "conector_id": e.conector})
	e.publicar()
	if estado(des) != "pendiente" || estado(vin) != "pendiente" || len(e.receptor.de("tecnico.argos_vinculado", "tecnico_id", 7101)) != 0 {
		t.Fatalf("B espera el 2xx de A: %s %s", estado(des), estado(vin))
	}
	e.relojMu.Lock()
	*e.reloj = e.reloj.Add(time.Hour)
	e.relojMu.Unlock()
	e.publicar()
	if estado(des) != "entregado" || estado(vin) != "entregado" {
		t.Fatalf("con el 2xx de A sale B: %s %s", estado(des), estado(vin))
	}
}

// M-2: un webhook agotado vuelve a la cola con integraciones.reenviar_webhook (auditado, con su
// evento); otro estado, 422; sin el permiso, 403.
func TestReenviarWebhookAgotado(t *testing.T) {
	e := nuevoEntorno(t, nil)
	id := uuid.Must(uuid.NewV7())
	if _, err := e.bd.Exec(e.ctx, `INSERT INTO mensajeria.webhooks_salida (id, evento, empresa_id, payload, ocurrido_en, estado, intentos, ultimo_error)
		VALUES ($1, 'tecnico.argos_vinculado', 1, '{}', now(), 'agotado', 6, 'Argos no respondió')`, id); err != nil {
		t.Fatal(err)
	}
	admin := e.persona([]int64{1}, rolBase("admin"))
	sup := e.persona([]int64{1}, rolBase("supervisor"))
	ruta := "/api/v1/mensajeria/webhooks-salida/" + id.String() + "/reenviar"
	if r := e.pedir(http.MethodPost, ruta, sup.token, uuid.NewString(), nil); r.status != http.StatusForbidden {
		t.Fatalf("sin el permiso: %d %v", r.status, r.cuerpo)
	}
	r := e.pedir(http.MethodPost, ruta, admin.token, uuid.NewString(), nil)
	if r.status != http.StatusOK || r.cuerpo["estado"] != "pendiente" || fmt.Sprint(r.cuerpo["intentos"]) != "0" {
		t.Fatalf("reenviar: %d %v", r.status, r.cuerpo)
	}
	if n := e.contar(`SELECT count(*) FROM mensajeria.webhooks_salida WHERE id = $1 AND estado = 'pendiente' AND intentos = 0 AND ultimo_error IS NULL`, id); n != 1 {
		t.Fatal("de vuelta en la cola")
	}
	if n := e.contar(`SELECT count(*) FROM auditoria.acciones WHERE accion = 'integraciones.reenviar_webhook' AND resultado = 'ok' AND entidad_id = $1`, id.String()); n != 1 {
		t.Fatalf("auditoría: %d", n)
	}
	if n := e.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'integracion.webhook_reenviado' AND payload->>'webhook_id' = $1`, id.String()); n != 1 {
		t.Fatalf("evento: %d", n)
	}
	if r := e.pedir(http.MethodPost, ruta, admin.token, uuid.NewString(), nil); r.status != http.StatusUnprocessableEntity {
		t.Fatalf("ya no está agotado: %d %v", r.status, r.cuerpo)
	}
	if r := e.pedir(http.MethodPost, "/api/v1/mensajeria/webhooks-salida/"+uuid.NewString()+"/reenviar", admin.token, uuid.NewString(), nil); r.status != http.StatusNotFound {
		t.Fatalf("no existe: %d", r.status)
	}
}

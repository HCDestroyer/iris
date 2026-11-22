package mensajeria_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad"
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
	if r := e.reenviarEn(ruta, sup.token, 1); r.status != http.StatusForbidden {
		t.Fatalf("sin el permiso: %d %v", r.status, r.cuerpo)
	}
	r := e.reenviarEn(ruta, admin.token, 1)
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
	if r := e.reenviarEn(ruta, admin.token, 1); r.status != http.StatusUnprocessableEntity {
		t.Fatalf("ya no está agotado: %d %v", r.status, r.cuerpo)
	}
	if r := e.reenviarEn("/api/v1/mensajeria/webhooks-salida/"+uuid.NewString()+"/reenviar", admin.token, 1); r.status != http.StatusNotFound {
		t.Fatalf("no existe: %d", r.status)
	}
}

// Hallazgo 1: un tecnico.argos_* que seguía en el outbox al reconectar (con el conector viejo como
// origen) se programa hacia el conector activo de la misma instancia y termina entregado.
func TestVinculoEnElOutboxAlReconectar(t *testing.T) {
	e := nuevoEntorno(t, nil)
	if _, err := e.bd.Exec(e.ctx, `UPDATE integracion.conectores SET contrato_remoto = '0.14.0', alcance_global = true WHERE id = $1`, e.conector); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = e.bd.Exec(e.ctx, `UPDATE integracion.conectores SET contrato_remoto = '0.6.0', alcance_global = NULL WHERE id = $1`, e.conector)
	})
	// El conector viejo de la misma instancia (misma URL, escrita distinto), ya apagado.
	var viejo, ajeno int64
	if err := e.bd.QueryRow(e.ctx, `INSERT INTO integracion.conectores (sistema, base_url, origen, token_cifrado, secreto_webhook_cifrado, estado)
		VALUES ('argosisp', 'HTTP://Argos.Prueba./', 'enlace_remoto', '\x00', '\x00', 'apagado') RETURNING id`).Scan(&viejo); err != nil {
		t.Fatal(err)
	}
	if err := e.bd.QueryRow(e.ctx, `INSERT INTO integracion.conectores (sistema, base_url, origen, token_cifrado, secreto_webhook_cifrado, estado)
		VALUES ('argosisp', 'http://otro-argos.prueba', 'enlace_remoto', '\x00', '\x00', 'apagado') RETURNING id`).Scan(&ajeno); err != nil {
		t.Fatal(err)
	}
	emitir := func(tecnico, conector int64) string {
		t.Helper()
		id := uuid.Must(uuid.NewV7())
		crudo, _ := json.Marshal(map[string]any{"tecnico_id": tecnico, "usuario_id": tecnico, "empresa_id": 1, "version": 1,
			"id_externo": fmt.Sprintf("u-%d", tecnico), "anterior_id_externo": nil, "conector_id": conector})
		if _, err := e.bd.Exec(e.ctx, `INSERT INTO integracion.outbox (id, evento, empresa_id, payload) VALUES ($1, 'tecnico.argos_vinculado', 1, $2)`,
			id, crudo); err != nil {
			t.Fatal(err)
		}
		return id.String()
	}
	mismo, otro := emitir(7201, viejo), emitir(7202, ajeno)
	e.publicar()
	var estado string
	var origen int64
	if err := e.bd.QueryRow(e.ctx, `SELECT estado, conector_origen FROM mensajeria.webhooks_salida WHERE id = $1`, mismo).Scan(&estado, &origen); err != nil {
		t.Fatal(err)
	}
	if estado != "entregado" || origen != e.conector {
		t.Fatalf("misma instancia: %s %d", estado, origen)
	}
	if n := e.contar(`SELECT count(*) FROM mensajeria.webhooks_salida WHERE id = $1 AND estado = 'omitido' AND conector_origen = $2
		AND ultimo_error LIKE 'otro_conector:%'`, otro, ajeno); n != 1 {
		t.Fatal("otra instancia queda omitida")
	}
}

// Hallazgo 3: un desvinculado que quedó en el outbox sin publicar (el relay falló y está en backoff)
// también frena el vinculado posterior: del mismo técnico con versión menor, o del mismo id_externo.
func TestDesvinculadoEnBackoffFrenaElVinculado(t *testing.T) {
	e := nuevoEntorno(t, nil)
	if _, err := e.bd.Exec(e.ctx, `UPDATE integracion.conectores SET contrato_remoto = '0.14.0', alcance_global = true WHERE id = $1`, e.conector); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = e.bd.Exec(e.ctx, `UPDATE integracion.conectores SET contrato_remoto = '0.6.0', alcance_global = NULL WHERE id = $1`, e.conector)
	})
	insertar := func(evento string, payload map[string]any, publicado bool) string {
		t.Helper()
		id := uuid.Must(uuid.NewV7())
		crudo, _ := json.Marshal(payload)
		q := `INSERT INTO integracion.outbox (id, evento, empresa_id, payload) VALUES ($1, $2, 1, $3)`
		if !publicado {
			// En backoff: el relay no lo toma todavía.
			q = `INSERT INTO integracion.outbox (id, evento, empresa_id, payload, intentos, proximo_intento, ultimo_error)
				VALUES ($1, $2, 1, $3, 3, now() + interval '1 hour', 'falló el suscriptor')`
		}
		if _, err := e.bd.Exec(e.ctx, q, id, evento, crudo); err != nil {
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
	for _, caso := range []struct {
		nombre string
		des    map[string]any
		vin    map[string]any
	}{
		{"mismo técnico", map[string]any{"tecnico_id": 7301, "usuario_id": 7301, "empresa_id": 1, "version": 5, "id_externo": "u-a", "conector_id": e.conector},
			map[string]any{"tecnico_id": 7301, "usuario_id": 7301, "empresa_id": 1, "version": 6, "id_externo": "u-b", "conector_id": e.conector}},
		{"mismo id_externo", map[string]any{"tecnico_id": 7302, "usuario_id": 7302, "empresa_id": 1, "version": 9, "id_externo": "u-c", "conector_id": e.conector},
			map[string]any{"tecnico_id": 7303, "usuario_id": 7303, "empresa_id": 1, "version": 1, "id_externo": "u-c", "conector_id": e.conector}},
	} {
		des := insertar("tecnico.argos_desvinculado", caso.des, false)
		vin := insertar("tecnico.argos_vinculado", caso.vin, true)
		e.publicar()
		if estado(vin) != "pendiente" {
			t.Fatalf("%s: el vinculado espera el desvinculado en backoff: %s", caso.nombre, estado(vin))
		}
		// El relay lo publica: sale el desvinculado y después el vinculado.
		if _, err := e.bd.Exec(e.ctx, `UPDATE integracion.outbox SET proximo_intento = now() WHERE id = $1`, des); err != nil {
			t.Fatal(err)
		}
		e.relojMu.Lock()
		*e.reloj = e.reloj.Add(2 * time.Minute)
		e.relojMu.Unlock()
		e.publicar()
		e.relojMu.Lock()
		*e.reloj = e.reloj.Add(2 * time.Minute)
		e.relojMu.Unlock()
		e.publicar()
		if estado(des) != "entregado" || estado(vin) != "entregado" {
			t.Fatalf("%s: publicado el desvinculado: %s %s", caso.nombre, estado(des), estado(vin))
		}
	}
}

// Bajo: el reenvío respeta la empresa activa: un webhook de otra empresa responde el mismo 404.
func TestReenviarWebhookDeOtraEmpresa(t *testing.T) {
	e := nuevoEntorno(t, nil)
	id := uuid.Must(uuid.NewV7())
	if _, err := e.bd.Exec(e.ctx, `INSERT INTO mensajeria.webhooks_salida (id, evento, empresa_id, payload, ocurrido_en, estado, intentos)
		VALUES ($1, 'tecnico.argos_vinculado', 2, '{}', now(), 'agotado', 6)`, id); err != nil {
		t.Fatal(err)
	}
	soloUno := e.persona([]int64{1}, rolBase("admin"))
	ruta := "/api/v1/mensajeria/webhooks-salida/" + id.String() + "/reenviar"
	if r := e.reenviarEn(ruta, soloUno.token, 1); r.status != http.StatusNotFound {
		t.Fatalf("de otra empresa: %d %v", r.status, r.cuerpo)
	}
	if n := e.contar(`SELECT count(*) FROM mensajeria.webhooks_salida WHERE id = $1 AND estado = 'agotado'`, id); n != 1 {
		t.Fatal("no cambia")
	}
}

// reenviarEn: POST …/reenviar con la empresa activa (X-Iris-Empresa).
func (e *entorno) reenviarEn(ruta, token string, empresa int64) respuesta {
	e.t.Helper()
	req := httptest.NewRequest(http.MethodPost, ruta, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Idempotency-Key", uuid.NewString())
	req.Header.Set(identidad.CabeceraEmpresa, strconv.FormatInt(empresa, 10))
	w := httptest.NewRecorder()
	e.h.ServeHTTP(w, req)
	r := respuesta{status: w.Code, h: w.Header()}
	_ = json.Unmarshal(w.Body.Bytes(), &r.cuerpo)
	return r
}

// Bajo: un desvinculado que el relay no pudo publicar (fallido_en) o cuyo webhook agotó deja el
// vinculado que lo espera agotado a la vista (se reenvía a mano); y la espera tiene tope de 24 h.
func TestVinculadoQueEsperaUnDesvinculadoQueNoSale(t *testing.T) {
	e := nuevoEntorno(t, nil)
	if _, err := e.bd.Exec(e.ctx, `UPDATE integracion.conectores SET contrato_remoto = '0.14.0', alcance_global = true WHERE id = $1`, e.conector); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = e.bd.Exec(e.ctx, `UPDATE integracion.conectores SET contrato_remoto = '0.6.0', alcance_global = NULL WHERE id = $1`, e.conector)
	})
	insertar := func(evento string, payload map[string]any, sql string) string {
		t.Helper()
		id := uuid.Must(uuid.NewV7())
		crudo, _ := json.Marshal(payload)
		if _, err := e.bd.Exec(e.ctx, sql, id, evento, crudo); err != nil {
			t.Fatal(err)
		}
		return id.String()
	}
	const publicar = `INSERT INTO integracion.outbox (id, evento, empresa_id, payload) VALUES ($1, $2, 1, $3)`
	const fallido = `INSERT INTO integracion.outbox (id, evento, empresa_id, payload, intentos, fallido_en, ultimo_error)
		VALUES ($1, $2, 1, $3, 8, now(), 'falló el suscriptor')`
	estado := func(id string) (string, string) {
		t.Helper()
		var s, u string
		if err := e.bd.QueryRow(e.ctx, `SELECT estado, coalesce(ultimo_error, '') FROM mensajeria.webhooks_salida WHERE id = $1`, id).Scan(&s, &u); err != nil {
			t.Fatal(err)
		}
		return s, u
	}
	// El relay agotó el desvinculado del mismo técnico: el vinculado queda agotado.
	insertar("tecnico.argos_desvinculado", map[string]any{"tecnico_id": 7401, "usuario_id": 7401, "empresa_id": 1, "version": 3, "id_externo": "u-x",
		"conector_id": e.conector}, fallido)
	vin := insertar("tecnico.argos_vinculado", map[string]any{"tecnico_id": 7401, "usuario_id": 7401, "empresa_id": 1, "version": 4, "id_externo": "u-y",
		"conector_id": e.conector}, publicar)
	e.publicar()
	if s, u := estado(vin); s != "agotado" || !strings.HasPrefix(u, "depende_de:") {
		t.Fatalf("relay agotado: %s %s", s, u)
	}

	// El webhook del desvinculado (otro técnico, mismo id_externo) agotó: también.
	e.receptor.fallar.Store(1)
	des := insertar("tecnico.argos_desvinculado", map[string]any{"tecnico_id": 7402, "usuario_id": 7402, "empresa_id": 1, "version": 2, "id_externo": "u-z",
		"conector_id": e.conector}, publicar)
	e.publicar()
	if _, err := e.bd.Exec(e.ctx, `UPDATE mensajeria.webhooks_salida SET estado = 'agotado' WHERE id = $1`, des); err != nil {
		t.Fatal(err)
	}
	otro := insertar("tecnico.argos_vinculado", map[string]any{"tecnico_id": 7403, "usuario_id": 7403, "empresa_id": 1, "version": 1, "id_externo": "u-z",
		"conector_id": e.conector}, publicar)
	e.publicar()
	if s, _ := estado(otro); s != "agotado" {
		t.Fatalf("webhook agotado: %s", s)
	}

	// Tope: más de 24 h esperando un desvinculado pendiente.
	e.receptor.fallar.Store(1)
	pend := insertar("tecnico.argos_desvinculado", map[string]any{"tecnico_id": 7404, "usuario_id": 7404, "empresa_id": 1, "version": 5, "id_externo": "u-w",
		"conector_id": e.conector}, publicar)
	espera := insertar("tecnico.argos_vinculado", map[string]any{"tecnico_id": 7404, "usuario_id": 7404, "empresa_id": 1, "version": 6, "id_externo": "u-v",
		"conector_id": e.conector}, publicar)
	e.publicar()
	if s, _ := estado(espera); s != "pendiente" {
		t.Fatalf("antes del tope espera: %s", s)
	}
	if _, err := e.bd.Exec(e.ctx, `UPDATE mensajeria.webhooks_salida SET proximo_intento = now() + interval '10 years' WHERE id = $1`, pend); err != nil {
		t.Fatal(err)
	}
	e.relojMu.Lock()
	*e.reloj = time.Now().Add(25 * time.Hour)
	e.relojMu.Unlock()
	if _, err := e.bd.Exec(e.ctx, `UPDATE mensajeria.webhooks_salida SET proximo_intento = $2 WHERE id = $1`, espera, time.Now()); err != nil {
		t.Fatal(err)
	}
	e.publicar()
	if s, u := estado(espera); s != "agotado" || !strings.Contains(u, "24 h") {
		t.Fatalf("al tope: %s %s", s, u)
	}
}

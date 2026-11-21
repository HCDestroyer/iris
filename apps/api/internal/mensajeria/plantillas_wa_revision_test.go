package mensajeria_test

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
)

// Segunda revisión de seguridad de las plantillas de WhatsApp (hallazgos A a D).

// A: un número QR no cambia de cuenta (WABA) por el formulario: se reconecta con QR.
func TestPlantillasWANumeroQRNoCambiaDeCuenta(t *testing.T) {
	c := nuevaCuentaWA(t, nil)
	if _, err := c.bd.Exec(c.ctx, `UPDATE mensajeria.canales SET tipo_conexion = 'qr', conectado_en = now() WHERE id = $1`, c.canal); err != nil {
		t.Fatal(err)
	}
	ver := c.api(c.admin, http.MethodGet, fmt.Sprintf("/canales/%d", c.canal), nil)
	c.exigir(ver, http.StatusOK)
	otra := nuevaWaba()
	r := c.api(c.admin, http.MethodPost, fmt.Sprintf("/canales/%d/actualizar", c.canal), map[string]any{"version": ver.cuerpo["version"],
		"nombre": ver.cuerpo["nombre"], "numero": ver.cuerpo["numero"], "phoneNumberId": c.pnidWA, "wabaId": otra, "plantillas": map[string]any{}})
	c.exigir(r, http.StatusUnprocessableEntity, "type", "urn:iris:problema:numero_qr")
	if n := c.contar(`SELECT count(*) FROM mensajeria.canales WHERE id = $1 AND waba_id = $2`, c.canal, c.waba); n != 1 {
		t.Fatal("el número QR cambió de cuenta")
	}
	if n := c.contar(`SELECT count(*) FROM mensajeria.plantillas_whatsapp_cuentas WHERE waba_id = $1`, otra); n != 0 {
		t.Fatal("se reclamó la otra cuenta")
	}
	// El nombre sí se puede cambiar.
	c.exigir(c.api(c.admin, http.MethodPost, fmt.Sprintf("/canales/%d/actualizar", c.canal), map[string]any{"version": ver.cuerpo["version"],
		"nombre": "Otro nombre", "numero": ver.cuerpo["numero"], "phoneNumberId": c.pnidWA, "wabaId": c.waba, "plantillas": ver.cuerpo["plantillas"]}), http.StatusOK)
}

// B: la cuenta pasa a otra empresa cuando la anterior ya no tiene números: las plantillas de la
// anterior quedan archivadas (borradas por el sistema) y la nueva dueña sincroniza sin chocar con
// ellas ni ver sus nombres.
func TestPlantillasWACambioDeDuenaArchivaLasAnteriores(t *testing.T) {
	c := nuevaCuentaWA(t, nil)
	r := c.apiClave(c.admin, http.MethodPost, "/plantillas-whatsapp/sugeridas/aviso_corte/crear", "s-"+pruebas.Unico(), map[string]any{"wabaId": c.waba, "enviar": true})
	c.exigir(r, http.StatusCreated)
	idA, metaA := idPW(r.cuerpo), r.cuerpo["metaId"].(string)
	c.estadoWebhook(metaA, "aviso_corte", "APPROVED", "")
	c.meta.fijar(c.waba, metaA, map[string]any{"status": "APPROVED"})
	// La empresa 1 ya no tiene números en la cuenta (Meta lo desconectó).
	if _, err := c.bd.Exec(c.ctx, `UPDATE mensajeria.canales SET desconectado_en = now() WHERE id = $1`, c.canal); err != nil {
		t.Fatal(err)
	}
	adminB := c.persona([]int64{2}, rolBase("admin"))
	adminB.empresa = 2
	pnidB := nuevaWaba()
	c.meta.agregarNumero(c.waba, pnidB)
	c.exigir(crearNumero(c.bandeja, adminB, map[string]any{"empresaId": 2, "wabaId": c.waba, "phoneNumberId": pnidB, "usos": []string{"soporte"},
		"appSecret": "secreto-de-la-empresa-b-0123"}), http.StatusCreated)
	if n := c.contar(`SELECT count(*) FROM mensajeria.plantillas_whatsapp WHERE id = $1 AND estado = 'DELETED' AND borrada_por = 'sistema' AND cardinality(codigos) = 0`, idA); n != 1 {
		t.Fatal("la plantilla de la dueña anterior no quedó archivada")
	}
	if ev := c.eventos("plantilla_whatsapp.borrada"); len(ev) != 1 || ev[0]["origen"] != "sistema" || ev[0]["motivo"] != "cuenta_de_otra_empresa" {
		t.Fatalf("borrada: %v", ev)
	}
	// La nueva dueña sincroniza: la plantilla de Meta entra como suya, sin 23505 por el meta_id ni el nombre.
	if err := c.m.SincronizarCuentaSistema(c.ctx, c.bd, c.waba, "manual"); err != nil {
		t.Fatal(err)
	}
	var errCuenta *string
	if err := c.bd.QueryRow(c.ctx, `SELECT error FROM mensajeria.plantillas_whatsapp_cuentas WHERE waba_id = $1 AND empresa_id = 2`, c.waba).Scan(&errCuenta); err != nil || errCuenta != nil {
		t.Fatalf("error de la cuenta: %v %v", errCuenta, err)
	}
	if n := c.contar(`SELECT count(*) FROM mensajeria.plantillas_whatsapp WHERE waba_id = $1 AND empresa_id = 2 AND meta_id = $2 AND estado = 'APPROVED'`, c.waba, metaA); n != 1 {
		t.Fatal("la nueva dueña no tiene la plantilla de Meta")
	}
	l := c.api(adminB, http.MethodGet, "/plantillas-whatsapp?wabaId="+c.waba+"&borradas=true", nil)
	c.exigir(l, http.StatusOK)
	if d := l.cuerpo["data"].([]any); len(d) != 1 || d[0].(map[string]any)["origen"] != "meta" {
		t.Fatalf("la nueva dueña ve: %v", d)
	}
}

// C: una WABA compartida de antes (un número de otra empresa en la cuenta) sigue con su mapa propio,
// se puede editar sin 409 mientras no cambie de cuenta y no toca las plantillas de la dueña.
func TestPlantillasWANumeroDeOtraEmpresaEnLaCuentaSigueConSuMapa(t *testing.T) {
	c := nuevaCuentaWA(t, map[string]any{"corte_por_falta_de_pago": map[string]any{"nombre": "corte_a", "idioma": "es"}})
	adminB := c.persona([]int64{2}, rolBase("admin"))
	adminB.empresa = 2
	wabaB, pnidB := nuevaWaba(), nuevaWaba()
	c.meta.agregarNumero(wabaB, pnidB)
	nB := crearNumero(c.bandeja, adminB, map[string]any{"empresaId": 2, "wabaId": wabaB, "phoneNumberId": pnidB, "usos": []string{"avisos", "soporte"},
		"appSecret": "secreto-de-la-empresa-b-0123"})
	c.exigir(nB, http.StatusCreated)
	canalB := int64(nB.cuerpo["id"].(float64))
	// El número de B estaba en la cuenta de A antes de que la cuenta tuviera dueña.
	if _, err := c.bd.Exec(c.ctx, `UPDATE mensajeria.canales SET waba_id = $1, plantillas = '{"corte_por_falta_de_pago":{"nombre":"corte_b","idioma":"es"}}' WHERE id = $2`,
		c.waba, canalB); err != nil {
		t.Fatal(err)
	}
	if p, err := c.m.PlantillasDeCanal(c.ctx, c.bd, canalB, ""); err != nil || p["corte_por_falta_de_pago"] != "corte_b" {
		t.Fatalf("el número de B perdió su mapa: %v %v", p, err)
	}
	ver := c.api(adminB, http.MethodGet, fmt.Sprintf("/canales/%d", canalB), nil)
	c.exigir(ver, http.StatusOK)
	c.exigir(c.api(adminB, http.MethodPost, fmt.Sprintf("/canales/%d/actualizar", canalB), map[string]any{"version": ver.cuerpo["version"],
		"nombre": "Avisos B", "numero": ver.cuerpo["numero"], "phoneNumberId": pnidB, "wabaId": c.waba,
		"plantillas": map[string]any{"corte_por_falta_de_pago": map[string]any{"nombre": "corte_b2", "idioma": "es"}}}), http.StatusOK)
	if p, _ := c.m.PlantillasDeCanal(c.ctx, c.bd, canalB, ""); p["corte_por_falta_de_pago"] != "corte_b2" {
		t.Fatalf("mapa de B: %v", p)
	}
	if n := c.contar(`SELECT count(*) FROM mensajeria.plantillas_whatsapp WHERE waba_id = $1 AND nombre LIKE 'corte_b%'`, c.waba); n != 0 {
		t.Fatal("el número de B tocó las plantillas de la cuenta de A")
	}
	if n := c.contar(`SELECT count(*) FROM mensajeria.plantillas_whatsapp_cuentas WHERE waba_id = $1 AND empresa_id = 1`, c.waba); n != 1 {
		t.Fatal("la cuenta cambió de dueña")
	}
}

// C: un número con cuenta que estaba inactivo (la migración no lo copió) refleja su mapa al reactivarse.
func TestPlantillasWANumeroReactivadoReflejaSuMapa(t *testing.T) {
	c := nuevaCuentaWA(t, nil)
	waba, pnid := nuevaWaba(), nuevaWaba()
	c.meta.agregarNumero(waba, pnid)
	n := crearNumero(c.bandeja, c.admin, map[string]any{"empresaId": 1, "wabaId": waba, "phoneNumberId": pnid, "usos": []string{"soporte"},
		"appSecret": "app-secret-de-prueba-0123"})
	c.exigir(n, http.StatusCreated)
	canal := int64(n.cuerpo["id"].(float64))
	c.exigir(c.api(c.admin, http.MethodPost, fmt.Sprintf("/canales/%d/desactivar", canal), nil), http.StatusOK)
	// Como quedó tras la migración: inactivo, con su mapa y sin cuenta.
	if _, err := c.bd.Exec(c.ctx, `UPDATE mensajeria.canales SET plantillas = '{"bienvenida":{"nombre":"bienvenida_vieja","idioma":"es"}}' WHERE id = $1`, canal); err != nil {
		t.Fatal(err)
	}
	if _, err := c.bd.Exec(c.ctx, `DELETE FROM mensajeria.plantillas_whatsapp_cuentas WHERE waba_id = $1`, waba); err != nil {
		t.Fatal(err)
	}
	antes := len(c.jobsDe("mensajeria.sincronizar_plantillas"))
	c.exigir(c.api(c.admin, http.MethodPost, fmt.Sprintf("/canales/%d/activar", canal), nil), http.StatusOK)
	if n := c.contar(`SELECT count(*) FROM mensajeria.plantillas_whatsapp_cuentas WHERE waba_id = $1 AND empresa_id = 1`, waba); n != 1 {
		t.Fatal("la cuenta no quedó con dueña")
	}
	if n := c.contar(`SELECT count(*) FROM mensajeria.plantillas_whatsapp WHERE waba_id = $1 AND nombre = 'bienvenida_vieja' AND 'bienvenida' = ANY(codigos) AND estado = 'PENDING'`, waba); n != 1 {
		t.Fatal("el mapa del número no se reflejó al reactivarlo")
	}
	if len(c.jobsDe("mensajeria.sincronizar_plantillas")) == antes {
		t.Fatal("no pidió la relectura de la cuenta")
	}
}

// D: un aviso de Meta se compara con el último aviso aplicado (reloj de Meta), no con estado_en (reloj
// de Iris); uno más viejo no se aplica y se relee la cuenta.
func TestPlantillasWAWebhookRelojDeMeta(t *testing.T) {
	c := nuevaCuentaWA(t, nil)
	r := c.apiClave(c.admin, http.MethodPost, "/plantillas-whatsapp/sugeridas/aviso_corte/crear", "s-"+pruebas.Unico(), map[string]any{"wabaId": c.waba, "enviar": true})
	c.exigir(r, http.StatusCreated)
	id, metaID := idPW(r.cuerpo), r.cuerpo["metaId"].(string)
	base := c.tiempo
	c.estadoWebhookEn(base+10, metaID, "aviso_corte", "APPROVED", "")
	c.estadoWebhookEn(base+20, metaID, "aviso_corte", "PAUSED", `,"other_info":{"title":"FIRST_PAUSE","description":"calidad"}`)
	// Iris la reanuda con su reloj una hora adelante del de Meta.
	*c.reloj = time.Unix(base+3600, 0).UTC()
	p := c.plantilla(id)
	c.exigir(c.apiClave(c.admin, http.MethodPost, fmt.Sprintf("/plantillas-whatsapp/%d/reanudar", id), "re-"+pruebas.Unico(), map[string]any{"version": p["version"]}),
		http.StatusOK, "estado", "APPROVED")
	// Meta la vuelve a pausar después (su reloj dice base+30): se aplica.
	c.estadoWebhookEn(base+30, metaID, "aviso_corte", "PAUSED", `,"other_info":{"title":"SECOND_PAUSE","description":"calidad"}`)
	if p := c.plantilla(id); p["estado"] != "PAUSED" {
		t.Fatalf("un aviso nuevo de Meta se descartó por el reloj de Iris: %v", p["estado"])
	}
	// Uno anterior al último aplicado no se aplica, y se relee la cuenta.
	antes := len(c.jobsDe("mensajeria.sincronizar_plantillas"))
	c.estadoWebhookEn(base+25, metaID, "aviso_corte", "REJECTED", `,"rejection_info":{"reason":"x","recommendation":"y"}`)
	if p := c.plantilla(id); p["estado"] != "PAUSED" {
		t.Fatalf("un aviso viejo cambió el estado: %v", p["estado"])
	}
	if len(c.jobsDe("mensajeria.sincronizar_plantillas")) != antes+1 {
		t.Fatal("un aviso viejo no pidió releer la cuenta")
	}
}

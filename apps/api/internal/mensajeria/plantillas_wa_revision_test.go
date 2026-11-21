package mensajeria_test

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

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
	// La empresa 1 dio de baja su número en la cuenta (inactivo; una desconexión de Meta no basta).
	if _, err := c.bd.Exec(c.ctx, `UPDATE mensajeria.canales SET activo = false, avisos_empresa = false WHERE id = $1`, c.canal); err != nil {
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
	// Lo que tenía (códigos y usos) queda en el evento y en el historial, para poder deshacerlo.
	if ev := c.eventos("plantilla_whatsapp.borrada"); len(ev) != 1 || ev[0]["origen"] != "sistema" || ev[0]["motivo"] != "cuenta_de_otra_empresa" ||
		fmt.Sprint(ev[0]["codigos"]) != "[corte_por_falta_de_pago]" || fmt.Sprint(ev[0]["usos"]) != "[avisos]" {
		t.Fatalf("borrada: %v", ev)
	}
	if n := c.contar(`SELECT count(*) FROM mensajeria.plantillas_whatsapp_historial WHERE plantilla_id = $1 AND tipo = 'borrada' AND origen = 'sistema'
		AND detalle->'codigos' = '["corte_por_falta_de_pago"]'::jsonb AND detalle->'usos' = '["avisos"]'::jsonb`, idA); n != 1 {
		t.Fatal("el historial no guarda los códigos y usos que tenía")
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

// Activar un número nunca cambia la dueña de la cuenta: solo reclama una cuenta sin dueña. Un número
// desconectado por Meta sigue contando como de su empresa.
func TestPlantillasWAReactivarNoQuitaLaCuenta(t *testing.T) {
	c := nuevaCuentaWA(t, nil)
	r := c.apiClave(c.admin, http.MethodPost, "/plantillas-whatsapp/sugeridas/aviso_corte/crear", "s-"+pruebas.Unico(), map[string]any{"wabaId": c.waba, "enviar": true})
	c.exigir(r, http.StatusCreated)
	idA, metaA := idPW(r.cuerpo), r.cuerpo["metaId"].(string)
	c.estadoWebhook(metaA, "aviso_corte", "APPROVED", "")
	// Meta desconectó el número de A: sigue siendo de A.
	if _, err := c.bd.Exec(c.ctx, `UPDATE mensajeria.canales SET desconectado_en = now() WHERE id = $1`, c.canal); err != nil {
		t.Fatal(err)
	}
	adminB := c.persona([]int64{2}, rolBase("admin"))
	adminB.empresa = 2
	// B no puede crear un número en esa cuenta aunque su token la vea en Meta.
	pnidB := nuevaWaba()
	c.meta.agregarNumero(c.waba, pnidB)
	c.exigir(crearNumero(c.bandeja, adminB, map[string]any{"empresaId": 2, "wabaId": c.waba, "phoneNumberId": pnidB, "usos": []string{"soporte"},
		"appSecret": "secreto-de-la-empresa-b-0123"}), http.StatusConflict, "type", "urn:iris:problema:waba_de_otra_empresa")
	// Un número viejo de B en esa cuenta, inactivo, que se reactiva.
	wabaB, pnidB2 := nuevaWaba(), nuevaWaba()
	c.meta.agregarNumero(wabaB, pnidB2)
	nB := crearNumero(c.bandeja, adminB, map[string]any{"empresaId": 2, "wabaId": wabaB, "phoneNumberId": pnidB2, "usos": []string{"soporte"},
		"appSecret": "secreto-de-la-empresa-b-0123"})
	c.exigir(nB, http.StatusCreated)
	canalB := int64(nB.cuerpo["id"].(float64))
	c.exigir(c.api(adminB, http.MethodPost, fmt.Sprintf("/canales/%d/desactivar", canalB), nil), http.StatusOK)
	if _, err := c.bd.Exec(c.ctx, `UPDATE mensajeria.canales SET waba_id = $1, plantillas = '{"corte_por_falta_de_pago":{"nombre":"corte_b","idioma":"es"}}' WHERE id = $2`,
		c.waba, canalB); err != nil {
		t.Fatal(err)
	}
	// Aunque A ya no tenga números activos en la cuenta, reactivar (sin comprobar nada en Meta) no
	// cambia la dueña: eso solo pasa al crear o actualizar un número con la cuenta comprobada.
	if _, err := c.bd.Exec(c.ctx, `UPDATE mensajeria.canales SET activo = false, avisos_empresa = false WHERE id = $1`, c.canal); err != nil {
		t.Fatal(err)
	}
	c.exigir(c.api(adminB, http.MethodPost, fmt.Sprintf("/canales/%d/activar", canalB), nil), http.StatusOK)
	if n := c.contar(`SELECT count(*) FROM mensajeria.plantillas_whatsapp_cuentas WHERE waba_id = $1 AND empresa_id = 1`, c.waba); n != 1 {
		t.Fatal("reactivar le quitó la cuenta a la dueña")
	}
	if n := c.contar(`SELECT count(*) FROM mensajeria.plantillas_whatsapp WHERE id = $1 AND estado = 'APPROVED'`, idA); n != 1 {
		t.Fatal("reactivar archivó las plantillas de la dueña")
	}
	if p, err := c.m.PlantillasDeCanal(c.ctx, c.bd, canalB, ""); err != nil || p["corte_por_falta_de_pago"] != "corte_b" {
		t.Fatalf("el número de B no sigue con su mapa: %v %v", p, err)
	}
}

// La relectura no pisa una plantilla que recibió un aviso de Meta mientras se leía (se compara el
// meta_evento_en de antes y de después de leer, los dos del reloj de Meta; nunca con el reloj de Iris).
func TestPlantillasWASincronizarNoPisaAvisoPosterior(t *testing.T) {
	c := nuevaCuentaWA(t, nil)
	r := c.apiClave(c.admin, http.MethodPost, "/plantillas-whatsapp/sugeridas/aviso_corte/crear", "s-"+pruebas.Unico(), map[string]any{"wabaId": c.waba, "enviar": true})
	c.exigir(r, http.StatusCreated)
	id, metaID := idPW(r.cuerpo), r.cuerpo["metaId"].(string)
	// Meta todavía la lista en revisión, pero mientras Iris lee llega «aprobada», con el reloj de Meta
	// 100 s atrás del de Iris.
	c.meta.duranteLaLectura(func() { c.estadoWebhookEn(c.reloj.Unix()-100, metaID, "aviso_corte", "APPROVED", "") })
	if err := c.m.SincronizarCuentaSistema(c.ctx, c.bd, c.waba, "manual"); err != nil {
		t.Fatal(err)
	}
	if p := c.plantilla(id); p["estado"] != "APPROVED" {
		t.Fatalf("la relectura pisó un aviso que llegó durante la lectura: %v", p["estado"])
	}
	// Sin avisos durante la lectura, se pone al día con lo que dice Meta.
	if err := c.m.SincronizarCuentaSistema(c.ctx, c.bd, c.waba, "manual"); err != nil {
		t.Fatal(err)
	}
	if p := c.plantilla(id); p["estado"] != "PENDING" {
		t.Fatalf("la relectura no puso al día la plantilla: %v", p["estado"])
	}
}

// La relectura no da por «Borrada en Meta» un borrador que se envió mientras leía (no venía en la lista
// porque Meta aún no lo tenía): solo marca las que estaban en la foto y no cambiaron.
func TestPlantillasWASincronizarNoBorraUnEnvioDuranteLaLectura(t *testing.T) {
	c := nuevaCuentaWA(t, nil)
	b := c.apiClave(c.admin, http.MethodPost, "/plantillas-whatsapp", "cr-"+pruebas.Unico(), map[string]any{"wabaId": c.waba, "etiqueta": "Aviso de visita",
		"idioma": "es", "categoria": "UTILITY", "definicion": map[string]any{"cuerpo": "Mañana te visitamos. ¡Gracias!", "variables": []any{}, "botones": []any{}}})
	c.exigir(b, http.StatusCreated, "estado", "BORRADOR")
	id := idPW(b.cuerpo)
	c.meta.duranteLaLectura(func() {
		c.exigir(c.apiClave(c.admin, http.MethodPost, fmt.Sprintf("/plantillas-whatsapp/%d/enviar", id), "env-"+pruebas.Unico(),
			map[string]any{"version": b.cuerpo["version"]}), http.StatusOK, "estado", "PENDING")
	})
	if err := c.m.SincronizarCuentaSistema(c.ctx, c.bd, c.waba, "manual"); err != nil {
		t.Fatal(err)
	}
	if p := c.plantilla(id); p["estado"] != "PENDING" || p["borradaPor"] != nil {
		t.Fatalf("la relectura dio por borrado un envío hecho durante la lectura: %v %v", p["estado"], p["borradaPor"])
	}
	// La siguiente relectura ya la encuentra en Meta.
	if err := c.m.SincronizarCuentaSistema(c.ctx, c.bd, c.waba, "manual"); err != nil {
		t.Fatal(err)
	}
	if p := c.plantilla(id); p["estado"] != "PENDING" {
		t.Fatalf("después de releer: %v", p["estado"])
	}
}

// La relectura no le devuelve el estado viejo a una plantilla que alguien reanudó mientras se leía.
func TestPlantillasWASincronizarNoPisaReanudarDuranteLaLectura(t *testing.T) {
	c := nuevaCuentaWA(t, nil)
	r := c.apiClave(c.admin, http.MethodPost, "/plantillas-whatsapp/sugeridas/aviso_corte/crear", "s-"+pruebas.Unico(), map[string]any{"wabaId": c.waba, "enviar": true})
	c.exigir(r, http.StatusCreated)
	id, metaID := idPW(r.cuerpo), r.cuerpo["metaId"].(string)
	c.meta.fijar(c.waba, metaID, map[string]any{"status": "PAUSED"})
	c.estadoWebhook(metaID, "aviso_corte", "PAUSED", `,"other_info":{"title":"FIRST_PAUSE","description":"calidad"}`)
	p := c.plantilla(id)
	if p["estado"] != "PAUSED" {
		t.Fatalf("pausa: %v", p["estado"])
	}
	// La lista de Meta ya está armada (pausada) cuando alguien la reanuda en Iris.
	c.meta.duranteLaLectura(func() {
		c.exigir(c.apiClave(c.admin, http.MethodPost, fmt.Sprintf("/plantillas-whatsapp/%d/reanudar", id), "re-"+pruebas.Unico(), map[string]any{"version": p["version"]}),
			http.StatusOK, "estado", "APPROVED")
	})
	if err := c.m.SincronizarCuentaSistema(c.ctx, c.bd, c.waba, "manual"); err != nil {
		t.Fatal(err)
	}
	if p := c.plantilla(id); p["estado"] != "APPROVED" {
		t.Fatalf("la relectura le devolvió el estado viejo a una reanudada durante la lectura: %v", p["estado"])
	}
}

// El archivo de ejemplo del encabezado tiene que ser del tipo y tamaño que Meta pide para ese
// encabezado: una imagen con un PDF responde 422 antes de ir a Meta.
func TestPlantillasWAArchivoEjemploDelTipoDelEncabezado(t *testing.T) {
	c := nuevaCuentaWA(t, nil)
	pdf := uuid.Must(uuid.NewV7())
	if _, err := c.bd.Exec(c.ctx, `INSERT INTO nucleo.adjuntos (id, entidad, entidad_id, tipo, mime, tamano, recibido, sha256, estado, completado_en,
		empresa_id, almacenamiento, clave) VALUES ($1, 'plantilla_whatsapp', $2, 'documento', 'application/pdf', 1024, 1024, repeat('a', 64), 'completo', now(),
		1, 'local', $3)`, pdf, c.waba, "pw/"+pdf.String()); err != nil {
		t.Fatal(err)
	}
	b := c.apiClave(c.admin, http.MethodPost, "/plantillas-whatsapp", "cr-"+pruebas.Unico(), map[string]any{"wabaId": c.waba, "etiqueta": "Factura del mes",
		"idioma": "es", "categoria": "UTILITY", "definicion": map[string]any{"encabezado": map[string]any{"tipo": "imagen", "adjuntoId": pdf.String()},
			"cuerpo": "Te mandamos tu factura del mes. ¡Gracias!", "variables": []any{}, "botones": []any{}}})
	c.exigir(b, http.StatusCreated, "estado", "BORRADOR")
	envios := len(c.meta.registro().creadas)
	r := c.apiClave(c.admin, http.MethodPost, fmt.Sprintf("/plantillas-whatsapp/%d/enviar", idPW(b.cuerpo)), "env-"+pruebas.Unico(), map[string]any{"version": b.cuerpo["version"]})
	c.exigir(r, http.StatusUnprocessableEntity, "type", "urn:iris:problema:archivo_no_coincide")
	if len(c.meta.registro().creadas) != envios {
		t.Fatal("se mandó a Meta con un archivo que no corresponde")
	}
}

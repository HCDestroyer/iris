package mensajeria_test

import (
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/mensajeria"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
)

// Sincronizar: lo nuevo en Meta entra, lo del formulario del número se confirma con Meta, lo que Meta
// ya no tiene queda «Borrada en Meta» con sus códigos (para elegir otra) y nunca se elimina.
func TestPlantillasWASincronizarYBorradaEnMeta(t *testing.T) {
	c := nuevaCuentaWA(t, map[string]any{"bienvenida": map[string]any{"nombre": "bienvenida_v1", "idioma": "es"}})
	// El formulario del número la reflejó en la cuenta, en revisión (nunca aprobada sin Meta), y pidió la relectura.
	var id int64
	if err := c.bd.QueryRow(c.ctx, `SELECT id FROM mensajeria.plantillas_whatsapp WHERE waba_id = $1 AND nombre = 'bienvenida_v1' AND 'bienvenida' = ANY(codigos)
		AND origen = 'canal' AND estado = 'PENDING'`, c.waba).Scan(&id); err != nil {
		t.Fatalf("la plantilla del número no quedó en la cuenta: %v", err)
	}
	if len(c.jobsDe("mensajeria.sincronizar_plantillas")) == 0 {
		t.Fatal("el formulario del número no pidió la relectura")
	}
	if p, err := c.m.PlantillasDeCanal(c.ctx, c.bd, c.canal, ""); err != nil || p["bienvenida"] != "" {
		t.Fatalf("una en revisión no sale: %v %v", p, err)
	}
	c.meta.porPagina(1) // fuerza la paginación por cursor
	bienvenida := c.meta.agregar(c.waba, map[string]any{"name": "bienvenida_v1", "language": "es", "status": "APPROVED", "category": "UTILITY",
		"quality_score": map[string]any{"score": "GREEN"}, "components": []map[string]any{{"type": "BODY", "text": "¡Bienvenido! Tu servicio {{1}} ya está activo.",
			"example": map[string]any{"body_text": [][]string{{"Fibra 100"}}}}}})
	promoMeta := c.meta.agregar(c.waba, map[string]any{"name": "promo_fibra_300", "language": "es", "status": "APPROVED", "category": "MARKETING",
		"parameter_format": "NAMED", "components": []map[string]any{{"type": "BODY", "text": "Hola {{cliente_nombre}}, sube a 300 Mbps.",
			"example": map[string]any{"body_text_named_params": []map[string]any{{"param_name": "cliente_nombre", "example": "Ana"}}}},
			{"type": "BUTTONS", "buttons": []map[string]any{{"type": "QUICK_REPLY", "text": "Me interesa"}}}}})

	cu := c.sincronizar(c.admin)
	res := cu["resumen"].(map[string]any)
	if res["total"].(float64) != 2 || len(res["nuevas"].([]any)) != 1 || res["nuevas"].([]any)[0].(map[string]any)["nombre"] != "promo_fibra_300" || cu["error"] != nil {
		t.Fatalf("cuenta: %v", cu)
	}
	p := c.plantilla(id)
	if p["metaId"] != bienvenida || p["estado"] != "APPROVED" || p["calidad"] != "GREEN" || fmt.Sprint(p["codigos"]) != "[bienvenida]" || p["definicion"].(map[string]any)["cuerpo"] == "" {
		t.Fatalf("después de sincronizar: %v", p)
	}
	if p, err := c.m.PlantillasDeCanal(c.ctx, c.bd, c.canal, mensajeria.UsoPWAvisos); err != nil || p["bienvenida"] != "bienvenida_v1" {
		t.Fatalf("aprobada en Meta, sale: %v %v", p, err)
	}
	// La de Meta: creada fuera de Iris, sin usos, con su variable reconocida.
	lista := c.api(c.admin, http.MethodGet, "/plantillas-whatsapp?wabaId="+c.waba+"&buscar=promo", nil)
	c.exigir(lista, http.StatusOK)
	promo := lista.cuerpo["data"].([]any)[0].(map[string]any)
	if promo["origen"] != "meta" || promo["categoria"] != "MARKETING" || len(promo["usos"].([]any)) != 0 {
		t.Fatalf("promo: %v", promo)
	}
	vars := promo["definicion"].(map[string]any)["variables"].([]any)
	if len(vars) != 1 || vars[0].(map[string]any)["dato"] != "cliente_nombre" || vars[0].(map[string]any)["ejemplo"] != "Ana" {
		t.Fatalf("variables leídas de Meta: %v", vars)
	}
	// Marketing apagado de fábrica: no se puede usar hasta activarlo.
	r := c.apiClave(c.admin, http.MethodPost, fmt.Sprintf("/plantillas-whatsapp/%v/usos", promo["id"]), "usos-"+pruebas.Unico(),
		map[string]any{"usos": []string{"conversaciones"}, "codigos": []string{"pago_recibido"}, "version": promo["version"]})
	c.exigir(r, http.StatusUnprocessableEntity, "type", "urn:iris:problema:marketing_no_permitido")
	aj := c.api(c.admin, http.MethodGet, "/plantillas-whatsapp/ajustes", nil)
	c.exigir(aj, http.StatusOK)
	r = c.apiClave(c.admin, http.MethodPost, "/plantillas-whatsapp/ajustes", "aj-"+pruebas.Unico(), map[string]any{"marketingPermitido": true, "version": aj.cuerpo["version"]})
	c.exigir(r, http.StatusOK, "marketingPermitido", "true")
	c.exigir(c.apiClave(c.admin, http.MethodPost, fmt.Sprintf("/plantillas-whatsapp/%v/usos", promo["id"]), "usos2-"+pruebas.Unico(),
		map[string]any{"usos": []string{"avisos"}, "codigos": []string{"pago_recibido"}, "variables": []map[string]any{{"nombre": "cliente_nombre", "dato": "cliente_nombre"}},
			"version": promo["version"]}), http.StatusOK)
	if p, _ := c.m.PlantillasDeCanal(c.ctx, c.bd, c.canal, ""); p["pago_recibido"] != "promo_fibra_300" {
		t.Fatalf("marketing activado: %v", p)
	}
	// Al apagar marketing, la de marketing deja de salir aunque siga asignada.
	c.exigir(c.apiClave(c.admin, http.MethodPost, "/plantillas-whatsapp/ajustes", "aj2-"+pruebas.Unico(), map[string]any{"marketingPermitido": false,
		"version": r.cuerpo["version"]}), http.StatusOK, "marketingPermitido", "false")
	if p, _ := c.m.PlantillasDeCanal(c.ctx, c.bd, c.canal, ""); p["pago_recibido"] != "" {
		t.Fatalf("marketing apagado sigue saliendo: %v", p)
	}

	// Meta borra la bienvenida y pausa la promo: en Iris la bienvenida queda «Borrada en Meta» con su código.
	c.meta.quitar(c.waba, bienvenida)
	c.meta.fijar(c.waba, promoMeta, map[string]any{"status": "PAUSED"})
	cu = c.sincronizar(c.admin)
	res = cu["resumen"].(map[string]any)
	if b := res["borradas"].([]any); len(b) != 1 || fmt.Sprint(b[0].(map[string]any)["codigos"]) != "[bienvenida]" {
		t.Fatalf("borradas: %v", b)
	}
	if cm := res["cambiadas"].([]any); len(cm) != 1 || cm[0].(map[string]any)["estado"] != "PAUSED" {
		t.Fatalf("cambiadas: %v", cm)
	}
	p = c.plantilla(id)
	if p["estado"] != "DELETED" || p["borradaPor"] != "meta" || fmt.Sprint(p["codigos"]) != "[bienvenida]" || p["atender"] != "borrada_en_uso" || p["nombreLibreEn"] == nil {
		t.Fatalf("borrada en Meta: %v", p)
	}
	if n := c.contar(`SELECT count(*) FROM mensajeria.plantillas_whatsapp WHERE id = $1`, id); n != 1 {
		t.Fatal("la fila se eliminó")
	}
	if ev := c.eventos("plantilla_whatsapp.borrada"); len(ev) != 1 || ev[0]["origen"] != "meta" || ev[0]["estaba_en_uso"] != true {
		t.Fatalf("borrada: %v", ev)
	}
	if ev := c.eventos("plantilla_whatsapp.sincronizada"); len(ev) != 2 {
		t.Fatalf("sincronizada: %v", ev)
	}
	// La lista las cuenta en «Atender» (la borrada en uso y la pausada).
	l := c.api(c.admin, http.MethodGet, "/plantillas-whatsapp?wabaId="+c.waba+"&grupo=atender", nil)
	c.exigir(l, http.StatusOK)
	if l.cuerpo["conteos"].(map[string]any)["atender"].(float64) != 2 || len(l.cuerpo["data"].([]any)) != 2 {
		t.Fatalf("atender: %v", l.cuerpo)
	}
}

// El botón encola la relectura (202) y no relee una cuenta sincronizada hace menos de 2 minutos (429).
func TestPlantillasWASincronizarLimitado(t *testing.T) {
	c := nuevaCuentaWA(t, nil)
	antes, llamadas := len(c.jobsDe("mensajeria.sincronizar_plantillas")), len(c.meta.registro().tokens)
	r := c.apiClave(c.admin, http.MethodPost, "/plantillas-whatsapp/cuentas/"+c.waba+"/sincronizar", "s1-"+pruebas.Unico(), nil)
	c.exigir(r, http.StatusAccepted)
	if len(c.jobsDe("mensajeria.sincronizar_plantillas")) != antes+1 {
		t.Fatal("no encoló la relectura")
	}
	if len(c.meta.registro().tokens) != llamadas {
		t.Fatal("el botón no debe llamar a Meta en la petición")
	}
	if err := c.m.SincronizarCuentaSistema(c.ctx, c.bd, c.waba, "manual"); err != nil {
		t.Fatal(err)
	}
	r = c.apiClave(c.admin, http.MethodPost, "/plantillas-whatsapp/cuentas/"+c.waba+"/sincronizar", "s2-"+pruebas.Unico(), nil)
	c.exigir(r, http.StatusTooManyRequests, "type", "urn:iris:problema:sincronizada_hace_poco")
	if r.h.Get("Retry-After") == "" {
		t.Fatal("sin Retry-After")
	}
	*c.reloj = c.reloj.Add(2*time.Minute + time.Second)
	c.exigir(c.apiClave(c.admin, http.MethodPost, "/plantillas-whatsapp/cuentas/"+c.waba+"/sincronizar", "s3-"+pruebas.Unico(), nil), http.StatusAccepted)
}

// El token sin whatsapp_business_management y Meta caída: error en llano, nada cambia y la cuenta lo guarda.
func TestPlantillasWASinPermisoYMetaCaida(t *testing.T) {
	c := nuevaCuentaWA(t, nil)
	c.meta.modo(true, false)
	cu := c.sincronizar(c.admin)
	if e, _ := cu["error"].(map[string]any); e == nil || e["codigo"] != "meta_sin_permiso" || cu["sincronizadaEn"] != nil {
		t.Fatalf("cuenta sin permiso: %v", cu)
	}
	// Enviar tampoco: el borrador queda como estaba, con el error en llano.
	b := c.apiClave(c.admin, http.MethodPost, "/plantillas-whatsapp", "cr-"+pruebas.Unico(), map[string]any{"wabaId": c.waba, "etiqueta": "Pago recibido",
		"idioma": "es", "categoria": "UTILITY", "enviar": true, "definicion": map[string]any{"cuerpo": "Recibimos tu pago de {{monto_con_moneda}}. ¡Gracias!",
			"variables": []map[string]any{{"nombre": "monto_con_moneda", "dato": "monto_con_moneda", "ejemplo": "Q 250.00"}}, "botones": []any{}}})
	c.exigir(b, http.StatusCreated, "estado", "BORRADOR", "nombre", "pago_recibido")
	e := b.cuerpo["errorEnvio"].(map[string]any)
	if e["type"] != "urn:iris:problema:meta_sin_permiso" || !strings.Contains(fmt.Sprint(e["detail"]), "whatsapp_business_management") || strings.Contains(cadena(b.cuerpo), tokenPrueba) {
		t.Fatalf("errorEnvio: %v", e)
	}
	if len(c.meta.registro().creadas) != 0 {
		t.Fatal("se llamó a crear sin permiso")
	}
	// Meta no responde: la cuenta lo dice y nada cambia.
	c.meta.modo(false, true)
	cu = c.sincronizar(c.admin)
	if e, _ := cu["error"].(map[string]any); e == nil || e["codigo"] != "meta_no_responde" {
		t.Fatalf("cuenta con Meta caída: %v", cu)
	}
	c.meta.modo(false, false)
	r := c.apiClave(c.admin, http.MethodPost, fmt.Sprintf("/plantillas-whatsapp/%d/enviar", idPW(b.cuerpo)), "env-"+pruebas.Unico(),
		map[string]any{"version": b.cuerpo["version"]})
	c.exigir(r, http.StatusOK, "estado", "PENDING")
	if cu = c.cuenta(); cu["error"] != nil {
		t.Fatalf("el envío bueno no limpió el error de la cuenta: %v", cu)
	}
}

// Nombre repetido (en Iris y en Meta), reglas antes de enviar, borrar desde Iris y nombre bloqueado.
func TestPlantillasWANombresReglasYBorrar(t *testing.T) {
	c := nuevaCuentaWA(t, nil)
	nueva := func(nombre, cuerpo string) respuesta {
		return c.apiClave(c.admin, http.MethodPost, "/plantillas-whatsapp", "n-"+pruebas.Unico(), map[string]any{"wabaId": c.waba, "etiqueta": "Recordatorio de pago",
			"nombre": nombre, "idioma": "es", "categoria": "UTILITY", "definicion": map[string]any{"cuerpo": cuerpo,
				"variables": []map[string]any{{"nombre": "fecha_corte", "dato": "fecha_corte", "ejemplo": "viernes 28 de noviembre"}}, "botones": []any{}}})
	}
	a := nueva("recordatorio_pago", "Tu factura vence el {{fecha_corte}}")
	c.exigir(a, http.StatusCreated, "estado", "BORRADOR")
	c.exigir(nueva("recordatorio_pago", "Otra"), http.StatusConflict, "type", "urn:iris:problema:nombre_en_uso", "sugerencia", "recordatorio_pago_2")
	v := c.apiClave(c.admin, http.MethodPost, "/plantillas-whatsapp/validar", "", map[string]any{"wabaId": c.waba, "id": idPW(a.cuerpo),
		"nombre": "recordatorio_pago", "idioma": "es", "categoria": "UTILITY", "definicion": a.cuerpo["definicion"]})
	c.exigir(v, http.StatusOK, "ok", "false")
	e := c.apiClave(c.admin, http.MethodPost, fmt.Sprintf("/plantillas-whatsapp/%d/enviar", idPW(a.cuerpo)), "e-"+pruebas.Unico(), map[string]any{"version": a.cuerpo["version"]})
	c.exigir(e, http.StatusUnprocessableEntity, "type", "urn:iris:problema:reglas_meta")
	if !strings.Contains(cadena(e.cuerpo["errors"]), "variables_bordes") {
		t.Fatalf("errors: %v", e.cuerpo)
	}
	// Meta ya tiene ese nombre (alguien la creó allá): 409 y queda el borrador.
	c.meta.agregar(c.waba, map[string]any{"name": "recordatorio_pago", "language": "es", "status": "APPROVED", "category": "UTILITY", "components": []any{}})
	ed := c.apiClave(c.admin, http.MethodPost, fmt.Sprintf("/plantillas-whatsapp/%d/actualizar", idPW(a.cuerpo)), "ed-"+pruebas.Unico(), map[string]any{
		"etiqueta": "Recordatorio de pago", "nombre": "recordatorio_pago", "idioma": "es", "categoria": "UTILITY", "version": a.cuerpo["version"],
		"definicion": map[string]any{"cuerpo": "Tu factura vence el {{fecha_corte}}. ¡Gracias!",
			"variables": []map[string]any{{"nombre": "fecha_corte", "dato": "fecha_corte", "ejemplo": "viernes 28 de noviembre"}}, "botones": []any{}}})
	c.exigir(ed, http.StatusOK)
	e = c.apiClave(c.admin, http.MethodPost, fmt.Sprintf("/plantillas-whatsapp/%d/enviar", idPW(a.cuerpo)), "e2-"+pruebas.Unico(), map[string]any{"version": ed.cuerpo["version"]})
	c.exigir(e, http.StatusConflict, "type", "urn:iris:problema:nombre_en_uso")
	if strings.Contains(cadena(e.cuerpo), "Content in This Language") {
		t.Fatalf("el texto técnico de Meta no llega al panel: %v", e.cuerpo)
	}
	c.exigir(c.api(c.admin, http.MethodGet, fmt.Sprintf("/plantillas-whatsapp/%d", idPW(a.cuerpo)), nil), http.StatusOK, "estado", "BORRADOR")

	// Otra con nombre libre: enviar, aprobar, borrar desde Iris (en Meta por su id) y el nombre queda bloqueado 30 días.
	b := nueva("vence_pronto", "Tu factura vence el {{fecha_corte}}. Gracias.")
	c.exigir(b, http.StatusCreated)
	e = c.apiClave(c.admin, http.MethodPost, fmt.Sprintf("/plantillas-whatsapp/%d/enviar", idPW(b.cuerpo)), "e3-"+pruebas.Unico(), map[string]any{"version": b.cuerpo["version"]})
	c.exigir(e, http.StatusOK, "estado", "PENDING")
	c.estadoWebhook(e.cuerpo["metaId"].(string), "vence_pronto", "APPROVED", "")
	p := c.plantilla(idPW(b.cuerpo))
	d := c.apiClave(c.admin, http.MethodPost, fmt.Sprintf("/plantillas-whatsapp/%d/borrar", idPW(b.cuerpo)), "d-"+pruebas.Unico(), map[string]any{"version": p["version"]})
	c.exigir(d, http.StatusOK, "estado", "DELETED", "borradaPor", "iris")
	if len(c.meta.registro().borradas) != 1 || c.meta.registro().borradas[0] != e.cuerpo["metaId"] {
		t.Fatalf("borrado en Meta: %v", c.meta.registro().borradas)
	}
	r := nueva("vence_pronto", "Tu factura vence el {{fecha_corte}}. Gracias.")
	c.exigir(r, http.StatusConflict, "type", "urn:iris:problema:nombre_bloqueado", "sugerencia", "vence_pronto_2")
	if r.cuerpo["disponibleEn"] == nil {
		t.Fatalf("disponibleEn: %v", r.cuerpo)
	}
	borrador := nueva("solo_borrador", "Tu factura vence el {{fecha_corte}}. Gracias.")
	c.exigir(c.apiClave(c.admin, http.MethodPost, fmt.Sprintf("/plantillas-whatsapp/%d/borrar", idPW(borrador.cuerpo)), "d2-"+pruebas.Unico(),
		map[string]any{"version": borrador.cuerpo["version"]}), http.StatusOK, "estado", "DELETED")
	if len(c.meta.registro().borradas) != 1 {
		t.Fatal("un borrador no se borra en Meta")
	}
}

// Una aprobada se edita en Meta 1 vez cada 24 h; Meta la reclasifica a marketing y el panel lo avisa.
func TestPlantillasWAEditarAprobadaYReclasificar(t *testing.T) {
	c := nuevaCuentaWA(t, nil)
	r := c.apiClave(c.admin, http.MethodPost, "/plantillas-whatsapp/sugeridas/recordatorio_pago/crear", "s-"+pruebas.Unico(), map[string]any{"wabaId": c.waba, "enviar": true})
	c.exigir(r, http.StatusCreated, "estado", "PENDING", "codigos", "[factura_por_vencer]")
	id, metaID := idPW(r.cuerpo), r.cuerpo["metaId"].(string)
	c.estadoWebhook(metaID, "recordatorio_pago", "APPROVED", "")
	p := c.plantilla(id)
	def := p["definicion"].(map[string]any)
	def["cuerpo"] = "Hola {{cliente_nombre}}, tu factura de {{monto_con_moneda}} vence el {{fecha_corte}}. Si ya pagaste, ignórala."
	e := c.apiClave(c.admin, http.MethodPost, fmt.Sprintf("/plantillas-whatsapp/%d/editar", id), "ed-"+pruebas.Unico(), map[string]any{"definicion": def, "version": p["version"]})
	c.exigir(e, http.StatusOK, "estado", "PENDING")
	if c.meta.registro().editadas[metaID] == nil || c.meta.registro().editadas[metaID]["category"] != nil {
		t.Fatalf("edición en Meta: %v", c.meta.registro().editadas)
	}
	c.estadoWebhook(metaID, "recordatorio_pago", "APPROVED", "")
	p = c.plantilla(id)
	if p["estado"] != "APPROVED" || p["ediciones"].(map[string]any)["ultimas24h"].(float64) != 1 || p["ediciones"].(map[string]any)["puedeEditarEn"] == nil {
		t.Fatalf("ediciones: %v", p)
	}
	e = c.apiClave(c.admin, http.MethodPost, fmt.Sprintf("/plantillas-whatsapp/%d/editar", id), "ed2-"+pruebas.Unico(), map[string]any{"definicion": def, "version": p["version"]})
	c.exigir(e, http.StatusUnprocessableEntity, "type", "urn:iris:problema:limite_ediciones")
	if e.cuerpo["disponibleEn"] == nil {
		t.Fatalf("disponibleEn: %v", e.cuerpo)
	}
	c.tiempo++
	c.webhookWA(fmt.Sprintf(`{"object":"whatsapp_business_account","entry":[{"id":%q,"time":%d,"changes":[{"field":"template_category_update","value":{"message_template_id":%s,`+
		`"message_template_name":"recordatorio_pago","message_template_language":"es","new_category":"UTILITY","correct_category":"MARKETING","category_update_timestamp":%d}}]}]}`,
		c.waba, c.tiempo, metaID, c.reloj.Add(24*time.Hour).Unix()))
	p = c.plantilla(id)
	if p["categoriaProxima"] != "MARKETING" || p["atender"] != "reclasificacion_proxima" {
		t.Fatalf("reclasificación próxima: %v", p)
	}
	c.tiempo++
	c.webhookWA(fmt.Sprintf(`{"object":"whatsapp_business_account","entry":[{"id":%q,"time":%d,"changes":[{"field":"template_category_update","value":{"message_template_id":%s,`+
		`"message_template_name":"recordatorio_pago","message_template_language":"es","previous_category":"UTILITY","new_category":"MARKETING"}}]}]}`, c.waba, c.tiempo, metaID))
	p = c.plantilla(id)
	if p["categoria"] != "MARKETING" || p["categoriaAnterior"] != "UTILITY" || p["atender"] != "reclasificada" || p["categoriaProxima"] != nil {
		t.Fatalf("reclasificada: %v", p)
	}
	ev := c.eventos("plantilla_whatsapp.categoria_cambiada")
	if len(ev) != 2 || ev[1]["de"] != "UTILITY" || ev[1]["a"] != "MARKETING" || ev[1]["en_uso"] != true || ev[1]["pendiente"] != false {
		t.Fatalf("categoria_cambiada: %v", ev)
	}
	c.tiempo++
	c.webhookWA(fmt.Sprintf(`{"object":"whatsapp_business_account","entry":[{"id":%q,"time":%d,"changes":[{"field":"message_template_quality_update","value":{"previous_quality_score":"GREEN",`+
		`"new_quality_score":"YELLOW","message_template_id":%s,"message_template_name":"recordatorio_pago","message_template_language":"es"}}]}]}`, c.waba, c.tiempo, metaID))
	if p = c.plantilla(id); p["calidad"] != "YELLOW" {
		t.Fatalf("calidad: %v", p["calidad"])
	}
	// Un webhook de otra cuenta no toca nada; uno de una plantilla desconocida pide releer la cuenta.
	c.webhookWA(fmt.Sprintf(`{"object":"whatsapp_business_account","entry":[{"id":"999","changes":[{"field":"message_template_status_update","value":{"event":"REJECTED",`+
		`"message_template_id":%s,"message_template_name":"recordatorio_pago","message_template_language":"es"}}]}]}`, metaID))
	if p = c.plantilla(id); p["estado"] != "APPROVED" {
		t.Fatalf("otra cuenta cambió el estado: %v", p["estado"])
	}
	antes := len(c.jobsDe("mensajeria.sincronizar_plantillas"))
	c.estadoWebhook("777", "desconocida", "APPROVED", "")
	if len(c.jobsDe("mensajeria.sincronizar_plantillas")) != antes+1 {
		t.Fatal("una plantilla desconocida no encoló la relectura")
	}
	c.estadoWebhook(metaID, "recordatorio_pago", "REJECTED", `,"reason":"INVALID_FORMAT","rejection_info":{"reason":"Parámetros pegados","recommendation":"Sepáralos"}`)
	p = c.plantilla(id)
	if p["estado"] != "REJECTED" || p["motivo"] != "INVALID_FORMAT" || p["rechazo"].(map[string]any)["recomendacion"] != "Sepáralos" {
		t.Fatalf("rechazo: %v", p)
	}
}

// Avisos de Meta fuera de orden o repetidos (por otro número de la cuenta o reenviados): no deshacen nada.
func TestPlantillasWAWebhookOrdenYRepetidos(t *testing.T) {
	c := nuevaCuentaWA(t, nil)
	r := c.apiClave(c.admin, http.MethodPost, "/plantillas-whatsapp/sugeridas/aviso_corte/crear", "s-"+pruebas.Unico(), map[string]any{"wabaId": c.waba, "enviar": true})
	c.exigir(r, http.StatusCreated, "estado", "PENDING")
	id, metaID := idPW(r.cuerpo), r.cuerpo["metaId"].(string)
	base := c.tiempo
	c.estadoWebhookEn(base+10, metaID, "aviso_corte", "APPROVED", "")
	// Un REJECTED anterior que llega tarde no deshace la aprobación.
	c.estadoWebhookEn(base+5, metaID, "aviso_corte", "REJECTED", `,"rejection_info":{"reason":"x","recommendation":"y"}`)
	if p := c.plantilla(id); p["estado"] != "APPROVED" {
		t.Fatalf("un aviso viejo cambió el estado: %v", p["estado"])
	}
	// Pausada; la persona la reanuda; el mismo aviso de pausa repetido (otro cuerpo) no la vuelve a pausar.
	c.estadoWebhookEn(base+20, metaID, "aviso_corte", "PAUSED", `,"other_info":{"title":"FIRST_PAUSE","description":"calidad"}`)
	p := c.plantilla(id)
	if p["estado"] != "PAUSED" {
		t.Fatalf("pausa: %v", p["estado"])
	}
	c.exigir(c.apiClave(c.admin, http.MethodPost, fmt.Sprintf("/plantillas-whatsapp/%d/reanudar", id), "re-"+pruebas.Unico(), map[string]any{"version": p["version"]}),
		http.StatusOK, "estado", "APPROVED")
	*c.reloj = time.Unix(base+30, 0).UTC() // la reanudación quedó antes del reenvío según el reloj de Meta
	c.estadoWebhookEn(base+20, metaID, "aviso_corte", "PAUSED", `,"other_info":{"title":"FIRST_PAUSE","description":"calidad (reenvío)"}`)
	if p := c.plantilla(id); p["estado"] != "APPROVED" {
		t.Fatalf("el aviso repetido se aplicó dos veces: %v", p["estado"])
	}
	if n := c.contar(`SELECT count(*) FROM mensajeria.plantillas_whatsapp_avisos_meta WHERE waba_id = $1 AND meta_id = $2`, c.waba, metaID); n != 3 {
		t.Fatalf("avisos registrados: %d", n)
	}
}

// Quién puede qué: ver y sincronizar supervisión y soporte; crear, editar y borrar solo administración.
func TestPlantillasWAPermisos(t *testing.T) {
	c := nuevaCuentaWA(t, nil)
	crear := map[string]any{"wabaId": c.waba, "etiqueta": "Prueba", "idioma": "es", "categoria": "UTILITY",
		"definicion": map[string]any{"cuerpo": "Hola, prueba.", "variables": []any{}, "botones": []any{}}}
	for i, p := range []persona{c.supervisor, c.agente} {
		c.exigir(c.api(p, http.MethodGet, "/plantillas-whatsapp?wabaId="+c.waba, nil), http.StatusOK)
		*c.reloj = c.reloj.Add(3 * time.Minute)
		c.exigir(c.apiClave(p, http.MethodPost, "/plantillas-whatsapp/cuentas/"+c.waba+"/sincronizar", fmt.Sprintf("s-%d-%s", i, pruebas.Unico()), nil), http.StatusAccepted)
		c.exigir(c.apiClave(p, http.MethodPost, "/plantillas-whatsapp", "c-"+pruebas.Unico(), crear), http.StatusForbidden)
		c.exigir(c.apiClave(p, http.MethodPost, "/plantillas-whatsapp/ajustes", "a-"+pruebas.Unico(), map[string]any{"marketingPermitido": true, "version": 0}), http.StatusForbidden)
	}
	c.exigir(c.api(c.tecnico, http.MethodGet, "/plantillas-whatsapp", nil), http.StatusForbidden)
	c.exigir(c.api(c.lectura, http.MethodGet, "/plantillas-whatsapp", nil), http.StatusForbidden)
	cr := c.apiClave(c.admin, http.MethodPost, "/plantillas-whatsapp", "c-"+pruebas.Unico(), crear)
	c.exigir(cr, http.StatusCreated)
	c.exigir(c.apiClave(c.supervisor, http.MethodPost, fmt.Sprintf("/plantillas-whatsapp/%d/borrar", idPW(cr.cuerpo)), "b-"+pruebas.Unico(),
		map[string]any{"version": cr.cuerpo["version"]}), http.StatusForbidden)
	c.exigir(c.api(c.admin, http.MethodGet, "/plantillas-whatsapp/sugeridas?wabaId=123456789", nil), http.StatusNotFound, "type", "urn:iris:problema:cuenta_no_encontrada")
	c.exigir(c.pedir(http.MethodGet, rutaMsj+"/plantillas-whatsapp", c.token, "", nil), http.StatusForbidden)
}

// Una WABA es de una sola empresa: la empresa B no puede usar la cuenta de A (ni registrarla en un número
// suyo, ni ver o tocar sus plantillas, ni colarle webhooks ni plantillas por el formulario del número).
func TestPlantillasWAWabaDeUnaSolaEmpresa(t *testing.T) {
	c := nuevaCuentaWA(t, nil)
	r := c.apiClave(c.admin, http.MethodPost, "/plantillas-whatsapp/sugeridas/aviso_corte/crear", "s-"+pruebas.Unico(), map[string]any{"wabaId": c.waba, "enviar": true})
	c.exigir(r, http.StatusCreated)
	idA, metaA := idPW(r.cuerpo), r.cuerpo["metaId"].(string)
	c.estadoWebhook(metaA, "aviso_corte", "APPROVED", "")

	adminB := c.persona([]int64{2}, rolBase("admin"))
	adminB.empresa = 2
	// B registra un número con la WABA de A (aunque su token la vea en Meta): 409, nada se guarda.
	pnidB := nuevaWaba()
	c.meta.agregarNumero(c.waba, pnidB)
	nB := crearNumero(c.bandeja, adminB, map[string]any{"empresaId": 2, "wabaId": c.waba, "phoneNumberId": pnidB, "usos": []string{"avisos", "soporte"},
		"appSecret": "secreto-de-la-empresa-b-0123", "plantillas": map[string]any{"corte_por_falta_de_pago": map[string]any{"nombre": "phishing_b", "idioma": "es"}}})
	c.exigir(nB, http.StatusConflict, "type", "urn:iris:problema:waba_de_otra_empresa")
	if n := c.contar(`SELECT count(*) FROM mensajeria.plantillas_whatsapp WHERE waba_id = $1 AND nombre = 'phishing_b'`, c.waba); n != 0 {
		t.Fatal("B coló una plantilla en la cuenta de A")
	}
	if n := c.contar(`SELECT count(*) FROM mensajeria.canales WHERE phone_number_id = $1`, pnidB); n != 0 {
		t.Fatal("se guardó el número de B")
	}
	// B con su propia WABA: un número cuya WABA no es la suya en Meta no se guarda (se comprueba con su token).
	wabaB, otroPnid := nuevaWaba(), nuevaWaba()
	c.meta.agregarNumero(wabaB, otroPnid)
	nB = crearNumero(c.bandeja, adminB, map[string]any{"empresaId": 2, "wabaId": wabaB, "phoneNumberId": nuevaWaba(), "usos": []string{"soporte"},
		"appSecret": "secreto-de-la-empresa-b-0123"})
	c.exigir(nB, http.StatusUnprocessableEntity, "type", "urn:iris:problema:waba_no_coincide")
	nB = crearNumero(c.bandeja, adminB, map[string]any{"empresaId": 2, "wabaId": wabaB, "phoneNumberId": otroPnid, "usos": []string{"soporte"},
		"appSecret": "secreto-de-la-empresa-b-0123"})
	c.exigir(nB, http.StatusCreated)
	canalB := int64(nB.cuerpo["id"].(float64))
	// …y no puede pasarlo a la WABA de A.
	verB := c.api(adminB, http.MethodGet, fmt.Sprintf("/canales/%d", canalB), nil)
	c.exigir(c.api(adminB, http.MethodPost, fmt.Sprintf("/canales/%d/actualizar", canalB), map[string]any{"version": verB.cuerpo["version"], "nombre": verB.cuerpo["nombre"],
		"numero": verB.cuerpo["numero"], "phoneNumberId": otroPnid, "wabaId": c.waba, "plantillas": map[string]any{}}), http.StatusConflict, "type", "urn:iris:problema:waba_de_otra_empresa")
	// B no ve ni toca la cuenta ni las plantillas de A.
	c.exigir(c.api(adminB, http.MethodGet, fmt.Sprintf("/plantillas-whatsapp/%d", idA), nil), http.StatusNotFound)
	c.exigir(c.api(adminB, http.MethodGet, "/plantillas-whatsapp/sugeridas?wabaId="+c.waba, nil), http.StatusNotFound)
	c.exigir(c.apiClave(adminB, http.MethodPost, "/plantillas-whatsapp/cuentas/"+c.waba+"/sincronizar", "sb-"+pruebas.Unico(), nil), http.StatusNotFound)
	c.exigir(c.apiClave(adminB, http.MethodPost, "/plantillas-whatsapp/sugeridas/recordatorio_pago/crear", "sgb-"+pruebas.Unico(),
		map[string]any{"wabaId": c.waba, "enviar": true}), http.StatusNotFound, "type", "urn:iris:problema:cuenta_no_encontrada")
	cuentasB := c.api(adminB, http.MethodGet, "/plantillas-whatsapp/cuentas", nil)
	c.exigir(cuentasB, http.StatusOK)
	if strings.Contains(cadena(cuentasB.cuerpo), c.waba) {
		t.Fatalf("B ve la cuenta de A: %v", cuentasB.cuerpo)
	}
	// Un webhook firmado con el secreto de B que dice ser de la cuenta de A no cambia nada en A.
	cuerpo := fmt.Sprintf(`{"object":"whatsapp_business_account","entry":[{"id":%q,"time":%d,"changes":[{"field":"message_template_status_update",`+
		`"value":{"event":"DISABLED","message_template_id":%s,"message_template_name":"aviso_corte","message_template_language":"es"}}]}]}`, c.waba, c.tiempo+100, metaA)
	// Un número de B en la cuenta de A anterior a esta regla (dato viejo, sin pasar por el formulario).
	if _, err := c.bd.Exec(c.ctx, `UPDATE mensajeria.canales SET waba_id = $1 WHERE id = $2`, c.waba, canalB); err != nil {
		t.Fatal(err)
	}
	jobs := len(c.jobsDe("mensajeria.sincronizar_plantillas"))
	c.webhookDe(canalB, "secreto-de-la-empresa-b-0123", cuerpo)
	if len(c.jobsDe("mensajeria.sincronizar_plantillas")) != jobs {
		t.Fatal("el webhook de B pidió releer la cuenta de A")
	}
	if p := c.plantilla(idA); p["estado"] != "APPROVED" {
		t.Fatalf("B cambió una plantilla de A: %v", p["estado"])
	}
	// El mismo aviso por el número de A sí se aplica (el de B no gastó su clave de repetidos).
	c.webhookWA(cuerpo)
	if p := c.plantilla(idA); p["estado"] != "DISABLED" {
		t.Fatalf("el aviso de A no se aplicó: %v", p["estado"])
	}
	if _, err := c.bd.Exec(c.ctx, `UPDATE mensajeria.plantillas_whatsapp SET estado = 'APPROVED' WHERE id = $1`, idA); err != nil {
		t.Fatal(err)
	}
	// Los avisos de A siguen saliendo con la de A.
	if p, _ := c.m.PlantillasDeCanal(c.ctx, c.bd, c.canal, ""); p["corte_por_falta_de_pago"] != "aviso_corte" {
		t.Fatalf("plantillas de A: %v", p)
	}
}

// Quien configura números sin plantillas_whatsapp.editar no cambia plantillas por el formulario del número.
func TestPlantillasWAFormularioNumeroExigeEditar(t *testing.T) {
	c := nuevaCuentaWA(t, nil)
	codigo := fmt.Sprintf("numeros_pw_%d", time.Now().UnixNano()%1_000_000_000)
	if _, err := identChat.CrearRol.Ejecutar(c.ctx, identidad.EntradaCrearRol{Codigo: codigo, Nombre: "Números " + pruebas.Unico(),
		Permisos: []nucleo.Permiso{nucleo.PermMensajeriaConfigurar}}); err != nil {
		t.Fatal(err)
	}
	numeros := c.persona([]int64{1}, rolBase(codigo))
	numeros.empresa = 1
	ver := c.api(numeros, http.MethodGet, fmt.Sprintf("/canales/%d", c.canal), nil)
	c.exigir(ver, http.StatusOK)
	cuerpo := func(plantillas map[string]any) map[string]any {
		return map[string]any{"version": ver.cuerpo["version"], "nombre": ver.cuerpo["nombre"], "numero": ver.cuerpo["numero"],
			"phoneNumberId": c.pnidWA, "wabaId": c.waba, "plantillas": plantillas}
	}
	r := c.api(numeros, http.MethodPost, fmt.Sprintf("/canales/%d/actualizar", c.canal), cuerpo(map[string]any{
		"corte_por_falta_de_pago": map[string]any{"nombre": "aviso_falso", "idioma": "es"}}))
	c.exigir(r, http.StatusForbidden)
	if n := c.contar(`SELECT count(*) FROM mensajeria.plantillas_whatsapp WHERE waba_id = $1 AND nombre = 'aviso_falso'`, c.waba); n != 0 {
		t.Fatal("se coló una plantilla sin plantillas_whatsapp.editar")
	}
	// Sin tocar las plantillas, sí guarda el número.
	c.exigir(c.api(numeros, http.MethodPost, fmt.Sprintf("/canales/%d/actualizar", c.canal), cuerpo(ver.cuerpo["plantillas"].(map[string]any))), http.StatusOK)
}

// Un dato del cliente de Argos solo sale con el cliente confirmado en la conversación.
func TestPlantillasWADatosDelClienteSinConfirmar(t *testing.T) {
	c := nuevaCuentaWA(t, nil)
	r := c.apiClave(c.admin, http.MethodPost, "/plantillas-whatsapp/sugeridas/recordatorio_pago/crear", "s-"+pruebas.Unico(), map[string]any{"wabaId": c.waba, "enviar": true})
	c.exigir(r, http.StatusCreated)
	c.estadoWebhook(r.cuerpo["metaId"].(string), "recordatorio_pago", "APPROVED", "")
	num := numeroCliente()
	c.webhookWA(fmt.Sprintf(`{"object":"whatsapp_business_account","entry":[{"changes":[{"field":"messages","value":{"metadata":{"phone_number_id":%q},`+
		`"contacts":[{"wa_id":%q,"profile":{"name":"Desconocido"}}],"messages":[{"from":%q,"id":"wamid.%s","timestamp":"%d","type":"text","text":{"body":"Hola"}}]}}]}]}`,
		c.pnidWA, num, num, pruebas.Unico(), c.reloj.Unix()))
	var conv int64
	if err := c.bd.QueryRow(c.ctx, `SELECT id FROM mensajeria.conversaciones WHERE canal_id = $1 AND numero = $2`, c.canal, "+"+num).Scan(&conv); err != nil {
		t.Fatal(err)
	}
	e := c.api(c.supervisor, http.MethodPost, fmt.Sprintf("/conversaciones/%d/mensajes", conv), map[string]any{"id": nuevoID(),
		"plantilla": map[string]any{"codigo": "factura_por_vencer", "variables": map[string]string{"monto": "250.00", "moneda": "GTQ", "vence_en": "2026-11-28"}}})
	c.exigir(e, http.StatusUnprocessableEntity, "type", "urn:iris:problema:cliente_sin_confirmar")
}

// La migración copia canales.plantillas a la cuenta sin perder nada: solo números activos, la empresa
// dueña, un nombre con todos sus códigos, lo inválido se informa sin abortar, y es repetible.
func TestPlantillasWAMigracionDeCanales(t *testing.T) {
	b := nuevaBandeja(t, nil)
	waba := nuevaWaba()
	migracion, err := os.ReadFile("../../migraciones/20261121065851_mensajeria_plantillas_whatsapp.sql")
	if err != nil {
		t.Fatal(err)
	}
	s := string(migracion)
	paso := strings.NewReplacer("-- +goose StatementBegin", "", "-- +goose StatementEnd", "").Replace(
		s[strings.Index(s, "-- Migración de mensajeria.canales.plantillas"):strings.Index(s, "-- +goose Down")])
	tx, err := b.bd.Begin(b.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(b.ctx) }()
	canales := []struct {
		empresa    int64
		activo     bool
		avisos     bool
		plantillas string
	}{
		{2, true, true, `{"factura_por_vencer":{"nombre":"recordatorio_v2","idioma":"es"},"factura_vencida":{"nombre":"recordatorio_v2","idioma":"es"},"bienvenida":{"nombre":"bienvenida_v1","idioma":"es"},"":{"nombre":"","idioma":""},"pago_recibido":{"nombre":"Pago Mal","idioma":"es"}}`},
		{2, true, false, `{"factura_por_vencer":{"nombre":"recordatorio_viejo","idioma":"es"},"pago_recibido":{"nombre":"pago_v1","idioma":"es_MX"}}`},
		{2, false, false, `{"servicio_reactivado":{"nombre":"de_un_inactivo","idioma":"es"}}`},
		{3, true, false, `{"corte_programado":{"nombre":"de_otra_empresa","idioma":"es"}}`},
	}
	for i, x := range canales {
		if _, err := tx.Exec(b.ctx, `INSERT INTO mensajeria.canales (empresa_id, tipo, canal, nombre, numero, phone_number_id, waba_id, credenciales_cifradas, plantillas, usos, activo, avisos_empresa)
			VALUES ($1, 'whatsapp_cloud', 'whatsapp', $2, $3, $4, $5, '\x00', $6::jsonb, $9::text[], $7, $8)`,
			x.empresa, "Migración "+pruebas.Unico(), fmt.Sprintf("+5022299%04d", i), nuevaWaba(), waba, x.plantillas, x.activo, x.avisos, usosMigracion(x.avisos)); err != nil {
			t.Fatal(err)
		}
	}
	for range 2 { // repetible
		if _, err := tx.Exec(b.ctx, paso); err != nil {
			t.Fatal(err)
		}
	}
	var duena int64
	if err := tx.QueryRow(b.ctx, `SELECT empresa_id FROM mensajeria.plantillas_whatsapp_cuentas WHERE waba_id = $1`, waba).Scan(&duena); err != nil || duena != 2 {
		t.Fatalf("dueña: %d %v", duena, err)
	}
	filas, err := tx.Query(b.ctx, `SELECT nombre, array_to_string(codigos, ','), estado, formato, origen, coalesce(empresa_id, 0) FROM mensajeria.plantillas_whatsapp WHERE waba_id = $1 ORDER BY nombre`, waba)
	if err != nil {
		t.Fatal(err)
	}
	type fila struct {
		nombre, codigos, estado, formato, origen string
		empresa                                  int64
	}
	got, err := pgx.CollectRows(filas, func(r pgx.CollectableRow) (fila, error) {
		var f fila
		return f, r.Scan(&f.nombre, &f.codigos, &f.estado, &f.formato, &f.origen, &f.empresa)
	})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"bienvenida_v1": "bienvenida", "pago_v1": "pago_recibido", "recordatorio_v2": "factura_por_vencer,factura_vencida", "recordatorio_viejo": ""}
	if len(got) != len(want) {
		t.Fatalf("migradas: %v", got)
	}
	for _, f := range got {
		if c, ok := want[f.nombre]; !ok || c != f.codigos || f.estado != "APPROVED" || f.formato != "POSITIONAL" || f.origen != "migrada" || f.empresa != 2 {
			t.Fatalf("migrada %v (códigos esperados %q)", f, want[f.nombre])
		}
	}
	var hist int
	if err := tx.QueryRow(b.ctx, `SELECT count(*) FROM mensajeria.plantillas_whatsapp_historial h JOIN mensajeria.plantillas_whatsapp p ON p.id = h.plantilla_id
		WHERE p.waba_id = $1`, waba).Scan(&hist); err != nil || hist != len(want) {
		t.Fatalf("historial: %d %v", hist, err)
	}
	var n int
	if err := tx.QueryRow(b.ctx, `SELECT count(*) FROM mensajeria.canales WHERE waba_id = $1 AND plantillas ? 'factura_por_vencer'`, waba).Scan(&n); err != nil || n != 2 {
		t.Fatalf("canales.plantillas: %d %v", n, err)
	}
}

// La relectura de cada noche: entre las 2:00 y las 5:00 (Guatemala) se encola un job por cuenta, que
// sincroniza con el actor sistema y no se repite en la misma noche.
func TestPlantillasWASincronizacionNocturna(t *testing.T) {
	c := nuevaCuentaWA(t, nil)
	c.meta.agregar(c.waba, map[string]any{"name": "nocturna", "language": "es", "status": "APPROVED", "category": "UTILITY",
		"components": []map[string]any{{"type": "BODY", "text": "Hola, aviso."}}})
	*c.reloj = time.Date(2026, 11, 22, 18, 0, 0, 0, time.UTC) // 12:00 en Guatemala: nada
	antes := len(c.jobsDe("mensajeria.sincronizar_plantillas"))
	if err := c.m.ProgramarSincronizaciones(c.ctx, c.bd); err != nil {
		t.Fatal(err)
	}
	if len(c.jobsDe("mensajeria.sincronizar_plantillas")) != antes {
		t.Fatal("encoló de día")
	}
	*c.reloj = time.Date(2026, 11, 23, 9, 0, 0, 0, time.UTC) // 03:00 en Guatemala
	if err := c.m.ProgramarSincronizaciones(c.ctx, c.bd); err != nil {
		t.Fatal(err)
	}
	var mia *mensajeria.ArgsSincronizarPlantillas
	for _, j := range c.jobsDe("mensajeria.sincronizar_plantillas")[antes:] {
		if a, ok := j.(mensajeria.ArgsSincronizarPlantillas); ok && a.WabaID == c.waba {
			mia = &a
		}
	}
	if mia == nil || mia.Origen != "nocturna" {
		t.Fatalf("no encoló la cuenta: %v", c.jobsDe("mensajeria.sincronizar_plantillas"))
	}
	if err := c.m.SincronizarCuentaSistema(c.ctx, c.bd, mia.WabaID, mia.Origen); err != nil {
		t.Fatal(err)
	}
	if n := c.contar(`SELECT count(*) FROM mensajeria.plantillas_whatsapp WHERE waba_id = $1 AND nombre = 'nocturna' AND origen = 'meta' AND empresa_id = 1`, c.waba); n != 1 {
		t.Fatal("la nocturna no trajo la plantilla para la dueña")
	}
	if ev := c.eventos("plantilla_whatsapp.sincronizada"); len(ev) != 1 || ev[0]["origen"] != "nocturna" {
		t.Fatalf("sincronizada: %v", ev)
	}
	cuenta := 0
	if err := c.m.ProgramarSincronizaciones(c.ctx, c.bd); err != nil {
		t.Fatal(err)
	}
	for _, j := range c.jobsDe("mensajeria.sincronizar_plantillas") {
		if a, ok := j.(mensajeria.ArgsSincronizarPlantillas); ok && a.WabaID == c.waba {
			cuenta++
		}
	}
	if cuenta != 1 {
		t.Fatalf("encolada %d veces", cuenta)
	}
	c.meta.modo(false, true)
	if err := c.m.SincronizarCuentaSistema(c.ctx, c.bd, c.waba, "webhook"); err != nil {
		t.Fatal(err)
	}
	if n := c.contar(`SELECT count(*) FROM mensajeria.plantillas_whatsapp_cuentas WHERE waba_id = $1 AND error = 'meta_no_responde'`, c.waba); n != 1 {
		t.Fatal("no quedó el error de la cuenta")
	}
}

func usosMigracion(avisos bool) []string {
	if avisos {
		return []string{"avisos", "soporte"}
	}
	return []string{"soporte"}
}

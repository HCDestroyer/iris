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

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
)

// Sincronizar: lo nuevo en Meta entra, lo migrado se empareja por nombre, lo que Meta ya no tiene
// queda «Borrada en Meta» con su código (para elegir otra) y nunca se elimina.
func TestPlantillasWASincronizarYBorradaEnMeta(t *testing.T) {
	c := nuevaCuentaWA(t, map[string]any{"bienvenida": map[string]any{"nombre": "bienvenida_v1", "idioma": "es"}})
	// El formulario del número reflejó su plantilla en la cuenta (aprobada, como hasta hoy).
	var id int64
	if err := c.bd.QueryRow(c.ctx, `SELECT id FROM mensajeria.plantillas_whatsapp WHERE waba_id = $1 AND nombre = 'bienvenida_v1' AND codigo = 'bienvenida'
		AND origen = 'canal' AND estado = 'APPROVED'`, c.waba).Scan(&id); err != nil {
		t.Fatalf("la plantilla del número no quedó en la cuenta: %v", err)
	}
	c.meta.pagina = 1 // fuerza la paginación por cursor
	bienvenida := c.meta.agregar(c.waba, map[string]any{"name": "bienvenida_v1", "language": "es", "status": "APPROVED", "category": "UTILITY",
		"quality_score": map[string]any{"score": "GREEN"}, "components": []map[string]any{{"type": "BODY", "text": "¡Bienvenido! Tu servicio {{1}} ya está activo.",
			"example": map[string]any{"body_text": [][]string{{"Fibra 100"}}}}}})
	c.meta.agregar(c.waba, map[string]any{"name": "promo_fibra_300", "language": "es", "status": "APPROVED", "category": "MARKETING",
		"parameter_format": "NAMED", "components": []map[string]any{{"type": "BODY", "text": "Hola {{cliente_nombre}}, sube a 300 Mbps.",
			"example": map[string]any{"body_text_named_params": []map[string]any{{"param_name": "cliente_nombre", "example": "Ana"}}}},
			{"type": "BUTTONS", "buttons": []map[string]any{{"type": "QUICK_REPLY", "text": "Me interesa"}}}}})

	r := c.apiClave(c.admin, http.MethodPost, "/plantillas-whatsapp/cuentas/"+c.waba+"/sincronizar", "sinc-"+pruebas.Unico(), nil)
	c.exigir(r, http.StatusOK)
	res := r.cuerpo["resumen"].(map[string]any)
	if res["total"].(float64) != 2 || len(res["nuevas"].([]any)) != 1 || res["nuevas"].([]any)[0].(map[string]any)["nombre"] != "promo_fibra_300" {
		t.Fatalf("resumen: %v", res)
	}
	p := c.plantilla(id)
	if p["metaId"] != bienvenida || p["calidad"] != "GREEN" || p["codigo"] != "bienvenida" || p["definicion"].(map[string]any)["cuerpo"] == "" {
		t.Fatalf("migrada después de sincronizar: %v", p)
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
	r = c.apiClave(c.admin, http.MethodPost, fmt.Sprintf("/plantillas-whatsapp/%v/usos", promo["id"]), "usos-"+pruebas.Unico(),
		map[string]any{"usos": []string{"conversaciones"}, "codigo": nil, "version": promo["version"]})
	c.exigir(r, http.StatusUnprocessableEntity, "type", "urn:iris:problema:marketing_no_permitido")
	aj := c.api(c.admin, http.MethodGet, "/plantillas-whatsapp/ajustes", nil)
	c.exigir(aj, http.StatusOK)
	c.exigir(c.apiClave(c.admin, http.MethodPost, "/plantillas-whatsapp/ajustes", "aj-"+pruebas.Unico(), map[string]any{"marketingPermitido": true,
		"version": aj.cuerpo["version"]}), http.StatusOK, "marketingPermitido", "true")
	c.exigir(c.apiClave(c.admin, http.MethodPost, fmt.Sprintf("/plantillas-whatsapp/%v/usos", promo["id"]), "usos2-"+pruebas.Unico(),
		map[string]any{"usos": []string{"conversaciones"}, "codigo": nil, "version": promo["version"]}), http.StatusOK)
	c.exigir(c.apiClave(c.admin, http.MethodPost, "/plantillas-whatsapp/ajustes", "aj2-"+pruebas.Unico(), map[string]any{"marketingPermitido": false,
		"version": aj.cuerpo["version"].(float64) + 1}), http.StatusOK, "marketingPermitido", "false")

	// Meta borra la bienvenida: en Iris queda «Borrada en Meta» con su código, y pide elegir otra.
	c.meta.quitar(c.waba, bienvenida)
	r = c.apiClave(c.admin, http.MethodPost, "/plantillas-whatsapp/cuentas/"+c.waba+"/sincronizar", "sinc2-"+pruebas.Unico(), nil)
	c.exigir(r, http.StatusOK)
	if b := r.cuerpo["resumen"].(map[string]any)["borradas"].([]any); len(b) != 1 || b[0].(map[string]any)["codigo"] != "bienvenida" {
		t.Fatalf("borradas: %v", b)
	}
	p = c.plantilla(id)
	if p["estado"] != "DELETED" || p["borradaPor"] != "meta" || p["codigo"] != "bienvenida" || p["atender"] != "borrada_en_uso" || p["nombreLibreEn"] == nil {
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
	cu := c.api(c.admin, http.MethodGet, "/plantillas-whatsapp/cuentas", nil)
	c.exigir(cu, http.StatusOK)
	hallada := false
	for _, x := range cu.cuerpo["data"].([]any) {
		if x.(map[string]any)["wabaId"] == c.waba {
			hallada = x.(map[string]any)["sincronizadaEn"] != nil && x.(map[string]any)["error"] == nil
		}
	}
	if !hallada {
		t.Fatalf("cuenta: %v", cu.cuerpo)
	}
	// La lista la cuenta en «Atender».
	l := c.api(c.admin, http.MethodGet, "/plantillas-whatsapp?wabaId="+c.waba+"&grupo=atender", nil)
	c.exigir(l, http.StatusOK)
	if l.cuerpo["conteos"].(map[string]any)["atender"].(float64) != 1 || len(l.cuerpo["data"].([]any)) != 1 {
		t.Fatalf("atender: %v", l.cuerpo)
	}
}

// El token sin whatsapp_business_management y Meta caída: error en llano, nada cambia y la cuenta lo guarda.
func TestPlantillasWASinPermisoYMetaCaida(t *testing.T) {
	c := nuevaCuentaWA(t, nil)
	c.meta.sinPermiso = true
	r := c.apiClave(c.admin, http.MethodPost, "/plantillas-whatsapp/cuentas/"+c.waba+"/sincronizar", "sp-"+pruebas.Unico(), nil)
	c.exigir(r, http.StatusFailedDependency, "type", "urn:iris:problema:meta_sin_permiso")
	if !strings.Contains(fmt.Sprint(r.cuerpo["detail"]), "whatsapp_business_management") || strings.Contains(cadena(r.cuerpo), tokenPrueba) {
		t.Fatalf("detalle: %v", r.cuerpo)
	}
	if n := c.contar(`SELECT count(*) FROM mensajeria.plantillas_whatsapp_cuentas WHERE waba_id = $1 AND error = 'meta_sin_permiso' AND sincronizada_en IS NULL`, c.waba); n != 1 {
		t.Fatal("la cuenta no guardó el error")
	}
	// Enviar tampoco: el borrador queda como estaba.
	b := c.apiClave(c.admin, http.MethodPost, "/plantillas-whatsapp", "cr-"+pruebas.Unico(), map[string]any{"wabaId": c.waba, "etiqueta": "Pago recibido",
		"idioma": "es", "categoria": "UTILITY", "enviar": true, "definicion": map[string]any{"cuerpo": "Recibimos tu pago de {{monto_con_moneda}}. ¡Gracias!",
			"variables": []map[string]any{{"nombre": "monto_con_moneda", "dato": "monto_con_moneda", "ejemplo": "Q 250.00"}}, "botones": []any{}}})
	c.exigir(b, http.StatusCreated, "estado", "BORRADOR", "nombre", "pago_recibido")
	if e := b.cuerpo["errorEnvio"].(map[string]any); e["type"] != "urn:iris:problema:meta_sin_permiso" {
		t.Fatalf("errorEnvio: %v", e)
	}
	if len(c.meta.creadas) != 0 {
		t.Fatal("se llamó a crear sin permiso")
	}
	// Meta no responde.
	c.meta.sinPermiso = false
	c.meta.caida = true
	r = c.apiClave(c.admin, http.MethodPost, "/plantillas-whatsapp/cuentas/"+c.waba+"/sincronizar", "caida-"+pruebas.Unico(), nil)
	c.exigir(r, http.StatusBadGateway, "type", "urn:iris:problema:meta_no_responde")
	c.meta.caida = false
	r = c.apiClave(c.admin, http.MethodPost, fmt.Sprintf("/plantillas-whatsapp/%d/enviar", idPW(b.cuerpo)), "env-"+pruebas.Unico(),
		map[string]any{"version": b.cuerpo["version"]})
	c.exigir(r, http.StatusOK, "estado", "PENDING")
	if n := c.contar(`SELECT count(*) FROM mensajeria.plantillas_whatsapp_cuentas WHERE waba_id = $1 AND error IS NULL`, c.waba); n != 1 {
		t.Fatal("el envío bueno no limpió el error de la cuenta")
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
	// Validar: termina con un dato.
	v := c.apiClave(c.admin, http.MethodPost, "/plantillas-whatsapp/validar", "", map[string]any{"wabaId": c.waba, "id": idPW(a.cuerpo),
		"nombre": "recordatorio_pago", "idioma": "es", "categoria": "UTILITY", "definicion": a.cuerpo["definicion"]})
	c.exigir(v, http.StatusOK, "ok", "false")
	// Enviar: 422 reglas_meta con el problema en errors[].
	e := c.apiClave(c.admin, http.MethodPost, fmt.Sprintf("/plantillas-whatsapp/%d/enviar", idPW(a.cuerpo)), "e-"+pruebas.Unico(), map[string]any{"version": a.cuerpo["version"]})
	c.exigir(e, http.StatusUnprocessableEntity, "type", "urn:iris:problema:reglas_meta")
	if !strings.Contains(cadena(e.cuerpo["errors"]), "variables_bordes") {
		t.Fatalf("errors: %v", e.cuerpo)
	}
	// Corregir el borrador y enviarlo; Meta ya tiene ese nombre (alguien la creó allá): 409 y queda el borrador.
	c.meta.agregar(c.waba, map[string]any{"name": "recordatorio_pago", "language": "es", "status": "APPROVED", "category": "UTILITY", "components": []any{}})
	ed := c.apiClave(c.admin, http.MethodPost, fmt.Sprintf("/plantillas-whatsapp/%d/actualizar", idPW(a.cuerpo)), "ed-"+pruebas.Unico(), map[string]any{
		"etiqueta": "Recordatorio de pago", "nombre": "recordatorio_pago", "idioma": "es", "categoria": "UTILITY", "version": a.cuerpo["version"],
		"definicion": map[string]any{"cuerpo": "Tu factura vence el {{fecha_corte}}. ¡Gracias!",
			"variables": []map[string]any{{"nombre": "fecha_corte", "dato": "fecha_corte", "ejemplo": "viernes 28 de noviembre"}}, "botones": []any{}}})
	c.exigir(ed, http.StatusOK)
	e = c.apiClave(c.admin, http.MethodPost, fmt.Sprintf("/plantillas-whatsapp/%d/enviar", idPW(a.cuerpo)), "e2-"+pruebas.Unico(), map[string]any{"version": ed.cuerpo["version"]})
	c.exigir(e, http.StatusConflict, "type", "urn:iris:problema:nombre_en_uso")
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
	if len(c.meta.borradas) != 1 || c.meta.borradas[0] != e.cuerpo["metaId"] {
		t.Fatalf("borrado en Meta: %v", c.meta.borradas)
	}
	r := nueva("vence_pronto", "Tu factura vence el {{fecha_corte}}. Gracias.")
	c.exigir(r, http.StatusConflict, "type", "urn:iris:problema:nombre_bloqueado", "sugerencia", "vence_pronto_2")
	if r.cuerpo["disponibleEn"] == nil {
		t.Fatalf("disponibleEn: %v", r.cuerpo)
	}
	// Un borrador se borra solo en Iris (sin llamar a Meta).
	borrador := nueva("solo_borrador", "Tu factura vence el {{fecha_corte}}. Gracias.")
	c.exigir(c.apiClave(c.admin, http.MethodPost, fmt.Sprintf("/plantillas-whatsapp/%d/borrar", idPW(borrador.cuerpo)), "d2-"+pruebas.Unico(),
		map[string]any{"version": borrador.cuerpo["version"]}), http.StatusOK, "estado", "DELETED")
	if len(c.meta.borradas) != 1 {
		t.Fatal("un borrador no se borra en Meta")
	}
}

// Una aprobada se edita en Meta 1 vez cada 24 h; Meta la reclasifica a marketing y el panel lo avisa.
func TestPlantillasWAEditarAprobadaYReclasificar(t *testing.T) {
	c := nuevaCuentaWA(t, nil)
	r := c.apiClave(c.admin, http.MethodPost, "/plantillas-whatsapp/sugeridas/recordatorio_pago/crear", "s-"+pruebas.Unico(), map[string]any{"wabaId": c.waba, "enviar": true})
	c.exigir(r, http.StatusCreated, "estado", "PENDING", "codigo", "factura_por_vencer")
	id, metaID := idPW(r.cuerpo), r.cuerpo["metaId"].(string)
	c.estadoWebhook(metaID, "recordatorio_pago", "APPROVED", "")
	p := c.plantilla(id)
	def := p["definicion"].(map[string]any)
	def["cuerpo"] = "Hola {{cliente_nombre}}, tu factura de {{monto_con_moneda}} vence el {{fecha_corte}}. Si ya pagaste, ignórala."
	e := c.apiClave(c.admin, http.MethodPost, fmt.Sprintf("/plantillas-whatsapp/%d/editar", id), "ed-"+pruebas.Unico(), map[string]any{"definicion": def, "version": p["version"]})
	c.exigir(e, http.StatusOK, "estado", "PENDING")
	if c.meta.editadas[metaID] == nil || c.meta.editadas[metaID]["category"] != nil {
		t.Fatalf("edición en Meta: %v", c.meta.editadas)
	}
	c.estadoWebhook(metaID, "recordatorio_pago", "APPROVED", "")
	p = c.plantilla(id)
	if p["ediciones"].(map[string]any)["ultimas24h"].(float64) != 1 || p["ediciones"].(map[string]any)["puedeEditarEn"] == nil {
		t.Fatalf("ediciones: %v", p["ediciones"])
	}
	e = c.apiClave(c.admin, http.MethodPost, fmt.Sprintf("/plantillas-whatsapp/%d/editar", id), "ed2-"+pruebas.Unico(), map[string]any{"definicion": def, "version": p["version"]})
	c.exigir(e, http.StatusUnprocessableEntity, "type", "urn:iris:problema:limite_ediciones")
	if e.cuerpo["disponibleEn"] == nil {
		t.Fatalf("disponibleEn: %v", e.cuerpo)
	}
	// Meta avisa que la reclasifica (en 24 h) y luego lo hace.
	c.webhookWA(fmt.Sprintf(`{"object":"whatsapp_business_account","entry":[{"id":%q,"changes":[{"field":"template_category_update","value":{"message_template_id":%s,`+
		`"message_template_name":"recordatorio_pago","message_template_language":"es","new_category":"UTILITY","correct_category":"MARKETING","category_update_timestamp":%d}}]}]}`,
		c.waba, metaID, c.reloj.Add(24*time.Hour).Unix()))
	p = c.plantilla(id)
	if p["categoriaProxima"] != "MARKETING" || p["atender"] != "reclasificacion_proxima" {
		t.Fatalf("reclasificación próxima: %v", p)
	}
	c.webhookWA(fmt.Sprintf(`{"object":"whatsapp_business_account","entry":[{"id":%q,"changes":[{"field":"template_category_update","value":{"message_template_id":%s,`+
		`"message_template_name":"recordatorio_pago","message_template_language":"es","previous_category":"UTILITY","new_category":"MARKETING"}}]}]}`, c.waba, metaID))
	p = c.plantilla(id)
	if p["categoria"] != "MARKETING" || p["categoriaAnterior"] != "UTILITY" || p["atender"] != "reclasificada" || p["categoriaProxima"] != nil {
		t.Fatalf("reclasificada: %v", p)
	}
	ev := c.eventos("plantilla_whatsapp.categoria_cambiada")
	if len(ev) != 2 || ev[1]["de"] != "UTILITY" || ev[1]["a"] != "MARKETING" || ev[1]["en_uso"] != true || ev[1]["pendiente"] != false {
		t.Fatalf("categoria_cambiada: %v", ev)
	}
	// Calidad.
	c.webhookWA(fmt.Sprintf(`{"object":"whatsapp_business_account","entry":[{"id":%q,"changes":[{"field":"message_template_quality_update","value":{"previous_quality_score":"GREEN",`+
		`"new_quality_score":"YELLOW","message_template_id":%s,"message_template_name":"recordatorio_pago","message_template_language":"es"}}]}]}`, c.waba, metaID))
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
	// Rechazo con rejection_info.
	c.estadoWebhook(metaID, "recordatorio_pago", "REJECTED", `,"reason":"INVALID_FORMAT","rejection_info":{"reason":"Parámetros pegados","recommendation":"Sepáralos"}`)
	p = c.plantilla(id)
	if p["estado"] != "REJECTED" || p["motivo"] != "INVALID_FORMAT" || p["rechazo"].(map[string]any)["recomendacion"] != "Sepáralos" {
		t.Fatalf("rechazo: %v", p)
	}
}

// Quién puede qué: ver y sincronizar supervisión y soporte; crear, editar y borrar solo administración.
func TestPlantillasWAPermisos(t *testing.T) {
	c := nuevaCuentaWA(t, nil)
	crear := map[string]any{"wabaId": c.waba, "etiqueta": "Prueba", "idioma": "es", "categoria": "UTILITY",
		"definicion": map[string]any{"cuerpo": "Hola, prueba.", "variables": []any{}, "botones": []any{}}}
	for _, p := range []persona{c.supervisor, c.agente} {
		c.exigir(c.api(p, http.MethodGet, "/plantillas-whatsapp?wabaId="+c.waba, nil), http.StatusOK)
		c.exigir(c.apiClave(p, http.MethodPost, "/plantillas-whatsapp/cuentas/"+c.waba+"/sincronizar", "s-"+pruebas.Unico(), nil), http.StatusOK)
		c.exigir(c.apiClave(p, http.MethodPost, "/plantillas-whatsapp", "c-"+pruebas.Unico(), crear), http.StatusForbidden)
		c.exigir(c.apiClave(p, http.MethodPost, "/plantillas-whatsapp/ajustes", "a-"+pruebas.Unico(), map[string]any{"marketingPermitido": true, "version": 0}), http.StatusForbidden)
	}
	c.exigir(c.api(c.tecnico, http.MethodGet, "/plantillas-whatsapp", nil), http.StatusForbidden)
	c.exigir(c.api(c.lectura, http.MethodGet, "/plantillas-whatsapp", nil), http.StatusForbidden)
	cr := c.apiClave(c.admin, http.MethodPost, "/plantillas-whatsapp", "c-"+pruebas.Unico(), crear)
	c.exigir(cr, http.StatusCreated)
	c.exigir(c.apiClave(c.supervisor, http.MethodPost, fmt.Sprintf("/plantillas-whatsapp/%d/borrar", idPW(cr.cuerpo)), "b-"+pruebas.Unico(),
		map[string]any{"version": cr.cuerpo["version"]}), http.StatusForbidden)
	// Una cuenta que no es de la empresa no existe para ella.
	c.exigir(c.api(c.admin, http.MethodGet, "/plantillas-whatsapp/sugeridas?wabaId=123456789", nil), http.StatusNotFound, "type", "urn:iris:problema:cuenta_no_encontrada")
	// Un token de integración (Argos) no administra plantillas.
	c.exigir(c.pedir(http.MethodGet, rutaMsj+"/plantillas-whatsapp", c.token, "", nil), http.StatusForbidden)
}

// La migración copia canales.plantillas a la cuenta sin perder nada (y es repetible).
func TestPlantillasWAMigracionDeCanales(t *testing.T) {
	b := nuevaBandeja(t, nil)
	sigWaba.Lock()
	waba := fmt.Sprint(time.Now().UnixNano() % 1e14)
	sigWaba.Unlock()
	migracion, err := os.ReadFile("../../migraciones/20261121065851_mensajeria_plantillas_whatsapp.sql")
	if err != nil {
		t.Fatal(err)
	}
	s := string(migracion)
	paso := s[strings.Index(s, "-- Migración de mensajeria.canales.plantillas"):strings.Index(s, "-- +goose Down")]
	tx, err := b.bd.Begin(b.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(b.ctx) }()
	for i, avisos := range []bool{true, false} {
		plantillas := `{"factura_por_vencer":{"nombre":"recordatorio_v2","idioma":"es"},"bienvenida":{"nombre":"bienvenida_v1","idioma":"es"}}`
		if !avisos {
			plantillas = `{"factura_por_vencer":{"nombre":"recordatorio_viejo","idioma":"es"},"pago_recibido":{"nombre":"pago_v1","idioma":"es_MX"}}`
		}
		if _, err := tx.Exec(b.ctx, `INSERT INTO mensajeria.canales (empresa_id, tipo, canal, nombre, numero, phone_number_id, waba_id, credenciales_cifradas, plantillas, usos)
			VALUES (2, 'whatsapp_cloud', 'whatsapp', $1, $2, $3, $4, '\x00', $5::jsonb, ARRAY['soporte'])`,
			"Migración "+pruebas.Unico(), fmt.Sprintf("+5022200%04d", i), fmt.Sprint(time.Now().UnixNano()%1e12+int64(i)), waba, plantillas); err != nil {
			t.Fatal(err)
		}
	}
	for range 2 { // repetible
		if _, err := tx.Exec(b.ctx, paso); err != nil {
			t.Fatal(err)
		}
	}
	filas, err := tx.Query(b.ctx, `SELECT nombre, idioma, coalesce(codigo, ''), estado, formato, origen FROM mensajeria.plantillas_whatsapp WHERE waba_id = $1 ORDER BY nombre`, waba)
	if err != nil {
		t.Fatal(err)
	}
	type fila struct{ nombre, idioma, codigo, estado, formato, origen string }
	got, err := pgx.CollectRows(filas, func(r pgx.CollectableRow) (fila, error) {
		var f fila
		return f, r.Scan(&f.nombre, &f.idioma, &f.codigo, &f.estado, &f.formato, &f.origen)
	})
	if err != nil {
		t.Fatal(err)
	}
	// Las cuatro plantillas de la cuenta; cada código una vez (factura_por_vencer: la del primer número).
	want := map[string]string{"bienvenida_v1": "bienvenida", "pago_v1": "pago_recibido", "recordatorio_v2": "factura_por_vencer", "recordatorio_viejo": ""}
	if len(got) != 4 {
		t.Fatalf("migradas: %v", got)
	}
	for _, f := range got {
		if want[f.nombre] != f.codigo || f.estado != "APPROVED" || f.formato != "POSITIONAL" || f.origen != "migrada" {
			t.Fatalf("migrada %v (código esperado %q)", f, want[f.nombre])
		}
	}
	var hist int
	if err := tx.QueryRow(b.ctx, `SELECT count(*) FROM mensajeria.plantillas_whatsapp_historial h JOIN mensajeria.plantillas_whatsapp p ON p.id = h.plantilla_id
		WHERE p.waba_id = $1`, waba).Scan(&hist); err != nil || hist != 4 {
		t.Fatalf("historial: %d %v", hist, err)
	}
	// La columna de los canales no se tocó.
	var n int
	if err := tx.QueryRow(b.ctx, `SELECT count(*) FROM mensajeria.canales WHERE waba_id = $1 AND plantillas ? 'factura_por_vencer'`, waba).Scan(&n); err != nil || n != 2 {
		t.Fatalf("canales.plantillas: %d %v", n, err)
	}
}

// La relectura de cada noche: entre las 2:00 y las 5:00 (Guatemala) se encola un job por cuenta, que
// sincroniza como el botón (con el actor sistema) y no se repite en la misma noche.
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
	if n := c.contar(`SELECT count(*) FROM mensajeria.plantillas_whatsapp WHERE waba_id = $1 AND nombre = 'nocturna' AND origen = 'meta'`, c.waba); n != 1 {
		t.Fatal("la nocturna no trajo la plantilla")
	}
	if ev := c.eventos("plantilla_whatsapp.sincronizada"); len(ev) != 1 || ev[0]["origen"] != "nocturna" {
		t.Fatalf("sincronizada: %v", ev)
	}
	// Ya sincronizada esta noche: la cuenta no se vuelve a encolar.
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
	// Con Meta caída, el job no falla en bucle: queda el error en la cuenta.
	c.meta.caida = true
	if err := c.m.SincronizarCuentaSistema(c.ctx, c.bd, c.waba, "webhook"); err != nil {
		t.Fatal(err)
	}
	if n := c.contar(`SELECT count(*) FROM mensajeria.plantillas_whatsapp_cuentas WHERE waba_id = $1 AND error = 'meta_no_responde'`, c.waba); n != 1 {
		t.Fatal("no quedó el error de la cuenta")
	}
}

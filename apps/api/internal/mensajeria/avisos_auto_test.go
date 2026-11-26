package mensajeria_test

import (
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos"
	"github.com/HCDestroyer/iris/apps/api/internal/mensajeria"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
)

// Recordatorios que entrega Iris (contrato con Argos 0.15.0 §6.2, §6.3 y §13.3, bloque C).

// usarContrato fija el contrato que informó el conector Argos de la prueba y lo devuelve al terminar.
func (e *entorno) usarContrato(v string) {
	e.t.Helper()
	var antes *string
	if err := e.bd.QueryRow(e.ctx, `SELECT contrato_remoto FROM integracion.conectores WHERE id = $1`, e.conector).Scan(&antes); err != nil {
		e.t.Fatal(err)
	}
	if _, err := e.bd.Exec(e.ctx, `UPDATE integracion.conectores SET contrato_remoto = $2 WHERE id = $1`, e.conector, v); err != nil {
		e.t.Fatal(err)
	}
	e.t.Cleanup(func() {
		_, _ = e.bd.Exec(e.ctx, `UPDATE integracion.conectores SET contrato_remoto = $2 WHERE id = $1`, e.conector, antes)
	})
}

func avisoAuto(cliente int64, extra map[string]any) map[string]any {
	c := avisoPorVencer(cliente, "2026-11-10")
	c["canal_preferido"] = "auto"
	for k, v := range extra {
		c[k] = v
	}
	return c
}

func TestAvisoAutoUnCanalYUnSoloWebhook(t *testing.T) {
	e := nuevoEntorno(t, nil)
	e.usarContrato("0.15.0")
	c := e.cliente(1, "5512-3344")
	e.exigir(e.consentir(c, "acepta", e.reloj.Add(-time.Hour)), http.StatusCreated)

	// auto sin lista: todos los canales declarados (hoy WhatsApp; aquí el simulado). Con suscripción y
	// recordatorio (informativos) y la misma clave de siempre.
	clave := fmt.Sprintf("argos:aviso:factura_por_vencer:%d", c)
	cuerpo := avisoAuto(c, map[string]any{"suscripcion_id": 77, "recordatorio": map[string]any{"disparador": "vencimiento", "orden": 2}})
	r := e.avisar(clave, cuerpo)
	e.exigir(r, http.StatusAccepted, "estado", "en_cola")
	id := r.cuerpo["aviso_id"].(string)
	var preferido, canal, recordatorio string
	var suscripcion int64
	if err := e.bd.QueryRow(e.ctx, `SELECT canal_preferido, canal, suscripcion_id, recordatorio::text FROM mensajeria.avisos WHERE id = $1`, id).
		Scan(&preferido, &canal, &suscripcion, &recordatorio); err != nil {
		t.Fatal(err)
	}
	if preferido != "auto" || canal != "whatsapp" || suscripcion != 77 || !strings.Contains(recordatorio, `"disparador": "vencimiento"`) {
		t.Fatalf("aviso guardado: %s %s %d %s", preferido, canal, suscripcion, recordatorio)
	}
	// Repetir la clave: el mismo aviso. El mismo envio_id con otro canal_preferido es otra petición
	// (C14): 422 idempotencia_reutilizada, nunca otro aviso por otro canal.
	e.exigir(e.avisar(clave, cuerpo), http.StatusAccepted, "aviso_id", id)
	e.exigir(e.avisar(clave, avisoPorVencer(c, "2026-11-10")), http.StatusUnprocessableEntity, "type", "urn:iris:problema:idempotencia_reutilizada")

	e.enviar(id)
	e.publicar()
	w := e.receptor.de("aviso.entregado", "aviso_id", id)
	if len(w) != 1 || w[0].Cuerpo["payload"].(map[string]any)["canal"] != "whatsapp" {
		t.Fatalf("un solo aviso.entregado con el canal real: %+v", w)
	}
	if len(e.receptor.de("aviso.fallido", "aviso_id", id)) != 0 {
		t.Fatal("un aviso entregado no emite aviso.fallido")
	}
	e.exigir(e.pedir(http.MethodGet, "/api/v1/mensajeria/avisos/"+id, e.token, "", nil), http.StatusOK, "canal", "whatsapp", "estado", "entregado")

	// canales_permitidos con un canal declarado: en cola por ese canal.
	c2 := e.cliente(1, "5512-3345")
	e.consentir(c2, "acepta", e.reloj.Add(-time.Hour))
	e.exigir(e.avisar(fmt.Sprintf("argos:aviso:factura_por_vencer:%d", c2), avisoAuto(c2, map[string]any{"canales_permitidos": []string{"whatsapp"}})),
		http.StatusAccepted, "estado", "en_cola")

	// 422 canal_no_permitido (C10): lista sin auto, vacía, repetida o con un canal que Iris no declara.
	malos := map[string]map[string]any{
		"sin_auto":  {"canal_preferido": "whatsapp", "canales_permitidos": []string{"whatsapp"}},
		"vacia":     {"canales_permitidos": []string{}},
		"repetida":  {"canales_permitidos": []string{"whatsapp", "whatsapp"}},
		"correo":    {"canales_permitidos": []string{"correo"}},
		"telegram":  {"canales_permitidos": []string{"whatsapp", "telegram"}},
		"inventado": {"canales_permitidos": []string{"fax"}},
	}
	for nombre, extra := range malos {
		r := e.avisar("argos:aviso:factura_por_vencer:malo-"+nombre, avisoAuto(c2, extra))
		e.exigir(r, http.StatusUnprocessableEntity, "type", "urn:iris:problema:canal_no_permitido")
	}

	// Sin permiso por WhatsApp (causa del canal): no queda ningún canal → bloqueado canal_no_disponible
	// (C12: Argos lo manda por sus canales con otro envio_id). El detalle dice por qué, sin el número.
	c3 := e.cliente(1, "5512-3346")
	r = e.avisar(fmt.Sprintf("argos:aviso:factura_por_vencer:%d", c3), avisoAuto(c3, nil))
	e.exigir(r, http.StatusAccepted, "estado", "bloqueado", "motivo", "canal_no_disponible")
	e.exigir(e.pedir(http.MethodGet, "/api/v1/mensajeria/avisos/"+r.cuerpo["aviso_id"].(string), e.token, "", nil), http.StatusOK,
		"detalle", "whatsapp: sin_permiso", "canal", "<nil>")
	// La baja de WhatsApp tampoco deja canal (solo afecta a ese canal, §6.4).
	c4 := e.cliente(1, "5512-3347")
	e.consentir(c4, "baja", e.reloj.Add(-time.Hour))
	e.exigir(e.avisar(fmt.Sprintf("argos:aviso:factura_por_vencer:%d", c4), avisoAuto(c4, nil)), http.StatusAccepted,
		"estado", "bloqueado", "motivo", "canal_no_disponible")
	// Sin whatsapp, la misma petición sigue diciendo el motivo de siempre.
	e.exigir(e.avisar(fmt.Sprintf("argos:aviso:factura_por_vencer:%d:wa", c4), avisoPorVencer(c4, "2026-11-10")), http.StatusAccepted,
		"estado", "bloqueado", "motivo", "baja")

	// El tope diario aplica a todos los canales: no es una causa del canal.
	c5 := e.cliente(1, "5512-3348")
	e.consentir(c5, "acepta", e.reloj.Add(-time.Hour))
	for i := range 3 {
		e.exigir(e.avisar(fmt.Sprintf("argos:aviso:factura_por_vencer:%d:%d", c5, i), avisoAuto(c5, nil)), http.StatusAccepted, "estado", "en_cola")
	}
	e.exigir(e.avisar(fmt.Sprintf("argos:aviso:factura_por_vencer:%d:x", c5), avisoAuto(c5, nil)), http.StatusAccepted,
		"estado", "bloqueado", "motivo", "limite_diario")

	// Los bloqueados no emiten webhooks; cada aviso, auditado y con su aviso.creado.
	e.publicar()
	if n := e.contar(`SELECT count(*) FROM mensajeria.webhooks_salida WHERE payload->>'aviso_id' IN
		(SELECT id::text FROM mensajeria.avisos WHERE estado = 'bloqueado' AND canal_preferido = 'auto')`); n != 0 {
		t.Fatalf("webhooks de bloqueados: %d", n)
	}
	if n := e.contar(`SELECT count(*) FROM auditoria.acciones WHERE accion = 'mensajeria.enviar' AND entidad = 'aviso' AND entidad_id = $1`, id); n != 1 {
		t.Fatalf("auditoría: %d", n)
	}
	if n := e.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'aviso.creado' AND payload->>'aviso_id' IN
		(SELECT id::text FROM mensajeria.avisos WHERE canal_preferido = 'auto' AND cliente_id = ANY($1))`, []int64{c, c2, c3, c4, c5}); n != 8 {
		t.Fatalf("aviso.creado de los auto: %d", n)
	}
}

func TestAvisoAutoSoloConArgos0150(t *testing.T) {
	e := nuevoEntorno(t, nil)
	e.usarContrato("0.14.3")
	c := e.cliente(1, "5512-3344")
	e.consentir(c, "acepta", e.reloj.Add(-time.Hour))
	e.exigir(e.avisar(fmt.Sprintf("argos:aviso:factura_por_vencer:%d", c), avisoAuto(c, nil)), http.StatusUnprocessableEntity,
		"type", "urn:iris:problema:canal_no_permitido")
	// whatsapp sigue como siempre.
	e.exigir(e.avisar(fmt.Sprintf("argos:aviso:factura_por_vencer:%d:wa", c), avisoPorVencer(c, "2026-11-10")), http.StatusAccepted, "estado", "en_cola")
	if n := e.contar(`SELECT count(*) FROM mensajeria.avisos WHERE cliente_id = $1 AND canal_preferido = 'auto'`, c); n != 0 {
		t.Fatalf("avisos auto con Argos < 0.15.0: %d", n)
	}
}

func TestCodigosNuevosConceptoYVencimiento(t *testing.T) {
	e := nuevoEntorno(t, nil)
	c := e.cliente(1, "5512-3344")
	e.consentir(c, "acepta", e.reloj.Add(-time.Hour))
	pedir := func(clave, codigo string, vars map[string]string) respuesta {
		return e.avisar("argos:aviso:"+codigo+":"+clave, map[string]any{"empresa_id": 1, "cliente_id": c, "plantilla": codigo, "variables": vars})
	}
	venceDe := func(id string) time.Time {
		t.Helper()
		var v time.Time
		if err := e.bd.QueryRow(e.ctx, `SELECT vence_en FROM mensajeria.avisos WHERE id = $1`, id).Scan(&v); err != nil {
			t.Fatal(err)
		}
		return v
	}
	finDia := func(fecha string) time.Time {
		d, _ := time.ParseInLocation(time.DateOnly, fecha, zonaGT)
		return d.AddDate(0, 0, 1)
	}

	// concepto (C7): opcional en las facturas; más de 60, con salto de línea o con URL: 422 variable_invalida.
	base := map[string]string{"monto": "250.00", "moneda": "GTQ", "vence_en": "2026-11-10"}
	con := func(concepto string) map[string]string {
		v := map[string]string{"concepto": concepto}
		for k, x := range base {
			v[k] = x
		}
		return v
	}
	for i, malo := range []string{strings.Repeat("a", 61), "TV\nPremium", "Ve a https://x.example", "pago.com", "", "TV\x07"} {
		e.exigir(pedir(fmt.Sprint("c", i), "factura_vencida", con(malo)), http.StatusUnprocessableEntity, "type", "urn:iris:problema:variable_invalida")
	}
	r := pedir("c-ok", "factura_por_vencer", con(strings.Repeat("ñ", 60)))
	e.exigir(r, http.StatusAccepted, "estado", "en_cola")
	if v := venceDe(r.cuerpo["aviso_id"].(string)); !v.Equal(finDia("2026-11-10")) {
		t.Fatalf("factura_por_vencer vence al terminar vence_en: %v", v)
	}
	// factura_vencida (y los demás sin variable de caducidad) vencen a las 72 h de pedidos (C9).
	r = pedir("v", "factura_vencida", base)
	e.exigir(r, http.StatusAccepted, "estado", "en_cola")
	if v := venceDe(r.cuerpo["aviso_id"].(string)); !v.Equal(e.reloj.Add(72 * time.Hour)) {
		t.Fatalf("factura_vencida vence a las 72 h: %v", v)
	}

	// Códigos nuevos (§6.3).
	*e.reloj = e.reloj.Add(24 * time.Hour) // otro día: el tope diario empieza de nuevo
	r = pedir("cp", "corte_proximo", map[string]string{"monto": "250.00", "moneda": "GTQ", "fecha_corte": "2026-11-12"})
	e.exigir(r, http.StatusAccepted, "estado", "en_cola")
	corte := r.cuerpo["aviso_id"].(string)
	if v := venceDe(corte); !v.Equal(finDia("2026-11-12")) {
		t.Fatalf("corte_proximo vence en fecha_corte: %v", v)
	}
	r = pedir("pv", "promesa_por_vencer", map[string]string{"monto": "100.00", "moneda": "GTQ", "vence_en": "2026-11-09"})
	e.exigir(r, http.StatusAccepted, "estado", "en_cola")
	if v := venceDe(r.cuerpo["aviso_id"].(string)); !v.Equal(finDia("2026-11-09")) {
		t.Fatalf("promesa_por_vencer vence en vence_en: %v", v)
	}
	// debito_fallido: reintenta_el opcional; sin él, 72 h; vacío (nunca a Meta): 422.
	r = pedir("df", "debito_fallido", map[string]string{"monto": "100.00", "moneda": "GTQ"})
	e.exigir(r, http.StatusAccepted, "estado", "en_cola")
	if v := venceDe(r.cuerpo["aviso_id"].(string)); !v.Equal(e.reloj.Add(72 * time.Hour)) {
		t.Fatalf("debito_fallido sin reintento vence a las 72 h: %v", v)
	}
	*e.reloj = e.reloj.Add(24 * time.Hour)
	r = pedir("df2", "debito_fallido", map[string]string{"monto": "100.00", "moneda": "GTQ", "reintenta_el": "2026-11-09"})
	e.exigir(r, http.StatusAccepted, "estado", "en_cola")
	if v := venceDe(r.cuerpo["aviso_id"].(string)); !v.Equal(finDia("2026-11-09")) {
		t.Fatalf("debito_fallido vence en reintenta_el: %v", v)
	}
	e.exigir(pedir("df3", "debito_fallido", map[string]string{"monto": "100.00", "moneda": "GTQ", "reintenta_el": ""}),
		http.StatusUnprocessableEntity, "type", "urn:iris:problema:validacion")
	e.exigir(pedir("dd", "debito_desactivado", map[string]string{}), http.StatusAccepted, "estado", "en_cola")
	*e.reloj = e.reloj.Add(24 * time.Hour)
	e.exigir(pedir("ss", "suscripcion_suspendida", map[string]string{"concepto": "TV Premium"}), http.StatusAccepted, "estado", "en_cola")
	e.exigir(pedir("sr", "suscripcion_reactivada", map[string]string{"concepto": "TV Premium"}), http.StatusAccepted, "estado", "en_cola")
	e.exigir(pedir("ss2", "suscripcion_suspendida", map[string]string{}), http.StatusUnprocessableEntity, "type", "urn:iris:problema:validacion")
	e.exigir(pedir("ss3", "suscripcion_reactivada", map[string]string{"concepto": "http://x.example"}), http.StatusUnprocessableEntity,
		"type", "urn:iris:problema:variable_invalida")

	// GET /plantillas: los códigos nuevos, con sus variables opcionales marcadas.
	l := e.pedir(http.MethodGet, "/api/v1/mensajeria/plantillas", e.token, "", nil)
	e.exigir(l, http.StatusOK)
	vistos := map[string]map[string]any{}
	for _, p := range l.cuerpo["data"].([]any) {
		pm := p.(map[string]any)
		vistos[pm["codigo"].(string)] = pm
	}
	for _, k := range []string{"corte_proximo", "promesa_por_vencer", "debito_fallido", "debito_desactivado", "suscripcion_suspendida", "suscripcion_reactivada"} {
		if vistos[k] == nil || vistos[k]["pide"] != "argos" {
			t.Fatalf("plantilla %s: %v", k, vistos[k])
		}
	}
	if !strings.Contains(fmt.Sprint(vistos["debito_fallido"]["variables"]), "nombre:reintenta_el obligatoria:false") ||
		!strings.Contains(fmt.Sprint(vistos["factura_vencida"]["variables"]), "nombre:concepto obligatoria:false") {
		t.Fatalf("opcionales: %v %v", vistos["debito_fallido"]["variables"], vistos["factura_vencida"]["variables"])
	}

	// Vencido (C9): el corte_proximo que no salió antes de fecha_corte se descarta con aviso.fallido
	// vencido, al prepararlo y también en el barrido de avisos sin acuse.
	*e.reloj = finDia("2026-11-12").Add(10 * time.Hour) // 10:00 en Guatemala, fuera del silencio
	e.enviar(corte)
	e.publicar()
	if w := e.receptor.de("aviso.fallido", "aviso_id", corte); len(w) != 1 || w[0].Cuerpo["payload"].(map[string]any)["motivo"] != "vencido" {
		t.Fatalf("aviso.fallido vencido: %+v", w)
	}
	*e.reloj = e.reloj.Add(4 * 24 * time.Hour)
	if _, err := e.m.VencerAvisos(e.ctx); err != nil {
		t.Fatal(err)
	}
	if n := e.contar(`SELECT count(*) FROM mensajeria.avisos WHERE cliente_id = $1 AND estado = 'fallido' AND motivo = 'vencido' AND enviado_en IS NULL`, c); n == 0 {
		t.Fatal("el barrido debía dar por vencidos los que nunca salieron")
	}
	if n := e.contar(`SELECT count(*) FROM mensajeria.avisos WHERE cliente_id = $1 AND estado = 'fallido' AND motivo = 'agotado' AND enviado_en IS NULL`, c); n != 0 {
		t.Fatalf("uno que nunca salió y venció no es agotado: %d", n)
	}
}

// WhatsApp con una plantilla por posición (sin mapa de datos): las variables opcionales nunca van a
// Meta, ni vacías ni corridas de lugar.
func TestOpcionalesNoVanAMetaPorPosicion(t *testing.T) {
	e := nuevoEntorno(t, func(c *mensajeria.Config) { c.Simulado = false })
	e.usarContrato("0.15.0")
	empresa := int64(2)
	canal, err := e.m.CrearCanal.Ejecutar(e.ctx, mensajeria.EntradaCrearCanal{EmpresaID: &empresa, Nombre: "Avisos", Numero: "+50222224444",
		PhoneNumberID: fmt.Sprint(time.Now().UnixNano() % 1e12), Token: "token-de-meta-0123456789abcdef", AppSecret: "app-secret-0123456789",
		VerifyToken: "verify-token-0123456789", AvisosEmpresa: ptrBool(true), Plantillas: map[string]mensajeria.PlantillaMeta{
			"factura_por_vencer": {Nombre: "factura_v1", Idioma: "es"}, "debito_fallido": {Nombre: "debito_v1", Idioma: "es"}}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = e.bd.Exec(e.ctx, `UPDATE mensajeria.canales SET avisos_empresa = false, activo = false WHERE id = $1`, canal.ID) })
	c := e.cliente(2, "5512-7788")
	e.exigir(e.pedir(http.MethodPost, "/api/v1/mensajeria/consentimientos", e.otro, fmt.Sprintf("argos:consentimiento:2:%d:whatsapp:a", c),
		map[string]any{"empresa_id": 2, "cliente_id": c, "canal": "whatsapp", "estado": "acepta", "origen": "argos_panel", "ocurrido_en": e.reloj.Add(-time.Hour)}),
		http.StatusCreated)
	parametros := func(cuerpo map[string]any, clave string) []string {
		t.Helper()
		r := e.pedir(http.MethodPost, "/api/v1/mensajeria/avisos", e.otro, clave, cuerpo)
		e.exigir(r, http.StatusAccepted, "estado", "en_cola")
		antes := len(e.meta.envios)
		e.enviar(r.cuerpo["aviso_id"].(string))
		if len(e.meta.envios) != antes+1 {
			t.Fatalf("no salió a Meta")
		}
		tpl := e.meta.envios[antes]["template"].(map[string]any)
		out := []string{}
		for _, comp := range tpl["components"].([]any) {
			for _, p := range comp.(map[string]any)["parameters"].([]any) {
				out = append(out, p.(map[string]any)["text"].(string))
			}
		}
		return out
	}
	f := map[string]any{"empresa_id": 2, "cliente_id": c, "plantilla": "factura_por_vencer", "canal_preferido": "auto",
		"variables": map[string]string{"monto": "250.00", "moneda": "GTQ", "vence_en": "2026-11-10", "concepto": "TV Premium"}}
	if p := parametros(f, fmt.Sprintf("argos:aviso:factura_por_vencer:%d", c)); !slices.Equal(p, []string{"250.00", "GTQ", "2026-11-10"}) {
		t.Fatalf("factura por posición: %v", p)
	}
	d := map[string]any{"empresa_id": 2, "cliente_id": c, "plantilla": "debito_fallido", "canal_preferido": "auto",
		"variables": map[string]string{"monto": "99.00", "moneda": "GTQ"}}
	if p := parametros(d, fmt.Sprintf("argos:aviso:debito_fallido:%d", c)); !slices.Equal(p, []string{"99.00", "GTQ"}) {
		t.Fatalf("débito por posición: %v", p)
	}
	// Un código sin plantilla aprobada en el número: con auto no queda canal.
	e.exigir(e.pedir(http.MethodPost, "/api/v1/mensajeria/avisos", e.otro, fmt.Sprintf("argos:aviso:corte_proximo:%d", c),
		map[string]any{"empresa_id": 2, "cliente_id": c, "plantilla": "corte_proximo", "canal_preferido": "auto",
			"variables": map[string]string{"monto": "99.00", "moneda": "GTQ", "fecha_corte": "2026-11-20"}}),
		http.StatusAccepted, "estado", "bloqueado", "motivo", "canal_no_disponible")
}

func ptrBool(b bool) *bool { return &b }

// capacidades.mensajeria[] (C1, C8, C11, C12): canales operativos, códigos usables, préstamo.
func TestDeclararMensajeria(t *testing.T) {
	e := nuevoEntorno(t, func(c *mensajeria.Config) { c.Simulado = false })
	base := 900 + time.Now().UnixNano()%1000*10
	propia, prestataria, sinCanal := base, base+1, base+2
	for _, id := range []int64{propia, prestataria, sinCanal} {
		if _, err := e.bd.Exec(e.ctx, `INSERT INTO nucleo.empresas (id, nombre, origen) VALUES ($1, $2, 'argos') ON CONFLICT DO NOTHING`, id, fmt.Sprint("Empresa ", id)); err != nil {
			t.Fatal(err)
		}
	}
	canal, err := e.m.CrearCanal.Ejecutar(e.ctx, mensajeria.EntradaCrearCanal{EmpresaID: &propia, Nombre: "Avisos", Numero: "+50222225555",
		PhoneNumberID: fmt.Sprint(time.Now().UnixNano() % 1e12), Token: "token-de-meta-0123456789abcdef", AppSecret: "app-secret-0123456789",
		VerifyToken: "verify-token-0123456789", AvisosEmpresa: ptrBool(true), Plantillas: map[string]mensajeria.PlantillaMeta{
			"corte_proximo": {Nombre: "corte_v1", Idioma: "es"}, "factura_por_vencer": {Nombre: "factura_v1", Idioma: "es"},
			// De Iris: nunca se declara a Argos.
			"boleta_recibida": {Nombre: "boleta_v1", Idioma: "es"}}})
	if err != nil {
		t.Fatal(err)
	}
	declarar := mensajeria.DeclararMensajeria(false, func() time.Time { return *e.reloj })
	leer := func() map[int64]argos.CapacidadMensajeria {
		t.Helper()
		filas, err := declarar(e.ctx, e.bd, []int64{propia, prestataria, sinCanal, propia})
		if err != nil {
			t.Fatal(err)
		}
		if len(filas) != 3 {
			t.Fatalf("una fila por empresa del alcance: %+v", filas)
		}
		out := map[int64]argos.CapacidadMensajeria{}
		for _, f := range filas {
			out[f.EmpresaID] = f
		}
		return out
	}
	caps := leer()
	if f := caps[propia]; !f.EntregaAuto || len(f.Canales) != 1 || f.Canales[0].Canal != "whatsapp" ||
		!slices.Equal(f.Canales[0].Codigos, []string{"factura_por_vencer", "corte_proximo"}) {
		t.Fatalf("empresa con número de avisos: %+v", f)
	}
	for _, id := range []int64{prestataria, sinCanal} {
		if f := caps[id]; f.EntregaAuto || f.Canales == nil || len(f.Canales) != 0 {
			t.Fatalf("empresa sin número: %+v", f)
		}
	}

	// Préstamo con el uso avisos y marcado como su número de avisos: las plantillas de la dueña.
	if _, err := e.bd.Exec(e.ctx, `INSERT INTO mensajeria.canal_prestamos (canal_id, empresa_id, usos, avisos_empresa, prestado_en)
		VALUES ($1, $2, '{soporte}', false, now())`, canal.ID, prestataria); err != nil {
		t.Fatal(err)
	}
	if f := leer()[prestataria]; f.EntregaAuto {
		t.Fatalf("préstamo sin avisos: %+v", f)
	}
	if _, err := e.bd.Exec(e.ctx, `UPDATE mensajeria.canal_prestamos SET usos = '{avisos}', avisos_empresa = true WHERE canal_id = $1 AND empresa_id = $2`,
		canal.ID, prestataria); err != nil {
		t.Fatal(err)
	}
	if f := leer()[prestataria]; !f.EntregaAuto || !slices.Equal(f.Canales[0].Codigos, []string{"factura_por_vencer", "corte_proximo"}) {
		t.Fatalf("préstamo con avisos: %+v", f)
	}

	// El token rechazado por Meta al enviar o el número desconectado: no está operativo.
	if _, err := e.bd.Exec(e.ctx, `UPDATE mensajeria.canales SET rechazado_por_envio = true WHERE id = $1`, canal.ID); err != nil {
		t.Fatal(err)
	}
	caps = leer()
	if caps[propia].EntregaAuto || caps[prestataria].EntregaAuto {
		t.Fatalf("número rechazado: %+v", caps)
	}
	if _, err := e.bd.Exec(e.ctx, `UPDATE mensajeria.canales SET rechazado_por_envio = false, activo = false WHERE id = $1`, canal.ID); err != nil {
		t.Fatal(err)
	}
	if leer()[propia].EntregaAuto {
		t.Fatal("número inactivo declarado")
	}

	// Con el simulado (desarrollo), una empresa sin número declara todos los códigos de Argos.
	sim, err := mensajeria.DeclararMensajeria(true, nil)(e.ctx, e.bd, []int64{sinCanal})
	if err != nil || len(sim) != 1 || !sim[0].EntregaAuto || !slices.Contains(sim[0].Canales[0].Codigos, "debito_desactivado") ||
		slices.Contains(sim[0].Canales[0].Codigos, "boleta_recibida") || slices.Contains(sim[0].Canales[0].Codigos, "bienvenida_servicio_sin_cuentas") {
		t.Fatalf("simulado: %+v %v", sim, err)
	}
}

// Las plantillas de la cuenta (WABA): una reclasificada por Meta a marketing sale de codigos si la
// empresa no permite marketing (C11).
func TestDeclararMensajeriaMarketing(t *testing.T) {
	c := nuevaCuentaWA(t, nil)
	// El número de avisos de la empresa 1 no queda para las pruebas que siguen (usan el simulado).
	t.Cleanup(func() { _, _ = c.bd.Exec(c.ctx, `UPDATE mensajeria.canales SET avisos_empresa = false, activo = false WHERE id = $1`, c.canal) })
	r := c.apiClave(c.admin, http.MethodPost, "/plantillas-whatsapp/sugeridas/suscripcion_suspendida/crear", "sug-"+pruebas.Unico(),
		map[string]any{"wabaId": c.waba, "enviar": true})
	c.exigir(r, http.StatusCreated, "codigos", "[suscripcion_suspendida]", "categoria", "UTILITY")
	metaID := r.cuerpo["metaId"].(string)
	declarar := mensajeria.DeclararMensajeria(false, func() time.Time { return *c.reloj })
	codigos := func() []string {
		t.Helper()
		f, err := declarar(c.ctx, c.bd, []int64{1})
		if err != nil || len(f) != 1 {
			t.Fatalf("declarar: %+v %v", f, err)
		}
		if len(f[0].Canales) == 0 {
			return nil
		}
		return f[0].Canales[0].Codigos
	}
	if slices.Contains(codigos(), "suscripcion_suspendida") {
		t.Fatal("en revisión no se declara")
	}
	c.estadoWebhook(metaID, "suscripcion_suspendida", "APPROVED", `,"message_template_category":"UTILITY"`)
	if !slices.Contains(codigos(), "suscripcion_suspendida") {
		t.Fatalf("aprobada: %v", codigos())
	}
	// El aviso sale con concepto como dato con nombre.
	aviso := c.avisoPorCodigo("suscripcion_suspendida", map[string]string{"concepto": "TV Premium"})
	if aviso == nil || !strings.Contains(cadena(aviso["template"].(map[string]any)["components"]), `"text":"TV Premium"`) {
		t.Fatalf("envío: %v", aviso)
	}
	c.tiempo++
	c.webhookWA(fmt.Sprintf(`{"object":"whatsapp_business_account","entry":[{"id":%q,"time":%d,"changes":[{"field":"template_category_update","value":{"message_template_id":%s,`+
		`"message_template_name":"suscripcion_suspendida","message_template_language":"es","previous_category":"UTILITY","new_category":"MARKETING"}}]}]}`,
		c.waba, c.tiempo, metaID))
	if slices.Contains(codigos(), "suscripcion_suspendida") {
		t.Fatalf("reclasificada a marketing sin permiso: %v", codigos())
	}
}

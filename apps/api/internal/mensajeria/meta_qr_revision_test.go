package mensajeria_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/HCDestroyer/iris/apps/api/internal/mensajeria"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
)

// Revisión de seguridad de #144: aislamiento entre empresas que comparten un número y ajustes del
// registro con QR.

// M1: quien usa el número no le quita al dueño su conversación abierta iniciando con una plantilla;
// la regla 1 prefiere la de la dueña y, si no, la más antigua.
func TestRevisionM1NoCapturaConversacionAbierta(t *testing.T) {
	p := nuevoPrestamo(t)
	n := numeroCliente()
	p.texto(n, "wamid.m1-"+pruebas.Unico(), "Hola Red Sur")
	if _, e, _ := p.convDe(n); e != 1 {
		t.Fatal("no fue a la dueña")
	}
	var ci int64
	if err := p.bd.QueryRow(p.ctx, `INSERT INTO mensajeria.contactos_iris (empresa_id, nombre, tipo, telefono) VALUES (2, 'Cliente', 'otro', $1) RETURNING id`, "+"+n).Scan(&ci); err != nil {
		t.Fatal(err)
	}
	r := p.api(p.sup2, http.MethodPost, "/conversaciones/iniciar", map[string]any{"id": uuid.Must(uuid.NewV7()).String(), "canalId": p.canal.ID,
		"contactoIrisId": ci, "plantilla": map[string]any{"codigo": "bienvenida", "variables": map[string]any{"plan": "Fibra"}}})
	if r.status != http.StatusConflict || !strings.HasSuffix(fmt.Sprint(r.cuerpo["type"]), "conversacion_en_otra_empresa") {
		t.Fatalf("iniciar sobre la abierta de la dueña: %d %v", r.status, r.cuerpo)
	}
	if n := p.contar(`SELECT count(*) FROM mensajeria.conversaciones WHERE canal_id = $1 AND numero = $2 AND empresa_id = 2`, p.canal.ID, "+"+n); n != 0 {
		t.Fatal("la empresa 2 abrió una conversación con ese teléfono")
	}
	d := p.api(p.sup2, http.MethodGet, fmt.Sprintf("/conversaciones/destinatario?canalId=%d&contactoIrisId=%d", p.canal.ID, ci), nil)
	p.exigir(d, http.StatusOK, "puede", "false", "motivo", "conversacion_en_otra_empresa")
	// Sigue llegando a la dueña.
	p.texto(n, "wamid.m1b-"+pruebas.Unico(), "¿Y mi instalación?")
	if _, e, _ := p.convDe(n); e != 1 {
		t.Fatal("el siguiente mensaje no fue a la dueña")
	}

	// Regla 1: con abiertas en la 2 (más antigua) y en la dueña, gana la dueña.
	n2 := numeroCliente()
	p.clienteArgos(2, n2)
	p.texto(n2, "wamid.m1c-"+pruebas.Unico(), "Hola")
	c2, e2, _ := p.convDe(n2)
	if e2 != 2 {
		t.Fatal("no fue a la 2")
	}
	var c1 int64
	if err := p.bd.QueryRow(p.ctx, `INSERT INTO mensajeria.conversaciones (empresa_id, canal_id, numero, ultima_actividad) VALUES (1, $1, $2, now()) RETURNING id`,
		p.canal.ID, "+"+n2).Scan(&c1); err != nil {
		t.Fatal(err)
	}
	wamid := "wamid.m1d-" + pruebas.Unico()
	p.texto(n2, wamid, "¿Quién me atiende?")
	if n := p.contar(`SELECT count(*) FROM mensajeria.conversacion_mensajes WHERE conversacion_id = $1 AND proveedor_id = $2`, c1, wamid); n != 1 {
		t.Fatalf("la regla 1 no prefirió a la dueña (conversación de la 2: %d)", c2)
	}
	// Sin la dueña: la de actividad más reciente del cliente (la 3 escribió después).
	if _, err := p.bd.Exec(p.ctx, `INSERT INTO nucleo.empresas (id, nombre, origen) VALUES (3, 'Empresa 3', 'argos') ON CONFLICT DO NOTHING`); err != nil {
		t.Fatal(err)
	}
	if _, err := p.bd.Exec(p.ctx, `INSERT INTO mensajeria.canal_prestamos (canal_id, empresa_id, usos, prestado_en) VALUES ($1, 3, '{soporte}', now())
		ON CONFLICT DO NOTHING`, p.canal.ID); err != nil {
		t.Fatal(err)
	}
	n3 := numeroCliente()
	var c3 int64
	if _, err := p.bd.Exec(p.ctx, `INSERT INTO mensajeria.conversaciones (empresa_id, canal_id, numero, ultima_actividad, ultimo_entrante_en)
		VALUES (2, $1, $2, now() + interval '3 hour', now())`, p.canal.ID, "+"+n3); err != nil {
		t.Fatal(err)
	}
	if err := p.bd.QueryRow(p.ctx, `INSERT INTO mensajeria.conversaciones (empresa_id, canal_id, numero, ultima_actividad, ultimo_entrante_en)
		VALUES (3, $1, $2, now(), now() + interval '1 hour') RETURNING id`, p.canal.ID, "+"+n3).Scan(&c3); err != nil {
		t.Fatal(err)
	}
	wamid = "wamid.m1e-" + pruebas.Unico()
	p.texto(n3, wamid, "Hola")
	if n := p.contar(`SELECT count(*) FROM mensajeria.conversacion_mensajes WHERE conversacion_id = $1 AND proveedor_id = $2`, c3, wamid); n != 1 {
		t.Fatal("la regla 1 no eligió la de actividad más reciente del cliente")
	}
}

// qrCompartido: un número QR de la empresa 1 prestado a la empresa 2 (para soporte).
func qrCompartido(t *testing.T) (*bandeja, string, string, int64) {
	t.Helper()
	b, _, admin := entornoQR(t, nil)
	conectarMeta(b, admin)
	pnid := pnidNuevo()
	r := conectarQR(b, admin, pnid, "AQB-"+pruebas.Unico(), "")
	b.exigir(r, http.StatusCreated)
	canal := int64(r.cuerpo["id"].(float64))
	sup2 := b.persona([]int64{2}, rolBase("supervisor"))
	cola := b.api(sup2, http.MethodPost, "/colas", map[string]any{"nombre": "Norte " + pruebas.Unico()})
	b.exigir(cola, http.StatusCreated)
	b.exigir(b.api(b.supervisor, http.MethodPost, fmt.Sprintf("/canales/%d/prestamos", canal), map[string]any{"empresaId": 2, "usos": []string{"soporte"},
		"colaId": cola.cuerpo["id"]}), http.StatusOK)
	return b, pnid, "9" + pnid[1:], canal
}

func eco(waba, pnid, cliente string, ts int64) string {
	return fmt.Sprintf(`{"object":"whatsapp_business_account","entry":[{"id":%q,"changes":[{"field":"smb_message_echoes","value":{"messaging_product":"whatsapp",`+
		`"metadata":{"phone_number_id":%q},"message_echoes":[{"from":"50255123344","to":%q,"id":"wamid.eco-%s","timestamp":"%d","type":"text","text":{"body":"Desde el teléfono"}}]}}]}]}`,
		waba, pnid, cliente, pruebas.Unico(), ts)
}

func mensajeQR(waba, pnid, cliente string, ts int64) string {
	return fmt.Sprintf(`{"object":"whatsapp_business_account","entry":[{"id":%q,"changes":[{"field":"messages","value":{"messaging_product":"whatsapp",`+
		`"metadata":{"phone_number_id":%q},"contacts":[{"wa_id":%q,"profile":{"name":"C"}}],"messages":[{"from":%q,"id":"wamid.m-%s","timestamp":"%d","type":"text","text":{"body":"Hola"}}]}}]}]}`,
		waba, pnid, cliente, cliente, pruebas.Unico(), ts)
}

// M2: un eco del teléfono de la dueña solo sigue a una conversación abierta; si no hay, es de la
// dueña aunque el cliente sea de otra empresa.
func TestRevisionM2EcoSoloReglaAbierta(t *testing.T) {
	b, pnid, waba, canal := qrCompartido(t)
	n := numeroCliente()
	if _, err := b.bd.Exec(b.ctx, `INSERT INTO integracion.argos_clientes (id, empresa_id, nombre, movil, codigo, sincronizado_en, empresas)
		VALUES ($1, 2, 'De la 2', $2, 'C', now(), '{2}')`, siguienteCliente.Add(1), n); err != nil {
		t.Fatal(err)
	}
	b.webhookApp(eco(waba, pnid, n, b.reloj.Unix()))
	var e int64
	if err := b.bd.QueryRow(b.ctx, `SELECT empresa_id FROM mensajeria.conversaciones WHERE canal_id = $1 AND numero = $2`, canal, "+"+n).Scan(&e); err != nil || e != 1 {
		t.Fatalf("eco de un cliente de la 2 sin conversación abierta: empresa %d %v", e, err)
	}
	// Con una conversación abierta en la 2 (el cliente escribió primero), el eco va ahí.
	n2 := numeroCliente()
	if _, err := b.bd.Exec(b.ctx, `INSERT INTO integracion.argos_clientes (id, empresa_id, nombre, movil, codigo, sincronizado_en, empresas)
		VALUES ($1, 2, 'De la 2', $2, 'C', now(), '{2}')`, siguienteCliente.Add(1), n2); err != nil {
		t.Fatal(err)
	}
	b.webhookApp(mensajeQR(waba, pnid, n2, b.reloj.Unix()))
	b.webhookApp(eco(waba, pnid, n2, b.reloj.Unix()))
	if c := b.contar(`SELECT count(*) FROM mensajeria.conversaciones WHERE canal_id = $1 AND numero = $2`, canal, "+"+n2); c != 1 {
		t.Fatalf("conversaciones de %s: %d", n2, c)
	}
	if c := b.contar(`SELECT count(*) FROM mensajeria.conversacion_mensajes m JOIN mensajeria.conversaciones c ON c.id = m.conversacion_id
		WHERE c.canal_id = $1 AND c.numero = $2 AND c.empresa_id = 2 AND m.origen = 'telefono'`, canal, "+"+n2); c != 1 {
		t.Fatal("el eco no siguió a la conversación abierta de la 2")
	}
}

// M3: en saas el QR siempre usa la URL del número (override_callback_uri) y exige IRIS_BASE_URL.
func TestRevisionM3SaaSSiempreURLDelNumero(t *testing.T) {
	saas := func(c *mensajeria.Config) {
		c.Meta = mensajeria.MetaEntorno{Modo: "saas", AppID: appIDQR, AppSecret: appSecretQR, ConfigID: configIDQR, VerifyToken: "vt-del-entorno-0123456789"}
	}
	b, g, admin := entornoQR(t, saas)
	pnid := pnidNuevo()
	r := conectarQR(b, admin, pnid, "AQB-"+pruebas.Unico(), "")
	b.exigir(r, http.StatusCreated)
	if s := g.cuerpos["POST /9"+pnid[1:]+"/subscribed_apps"]; len(s) != 1 || !strings.Contains(s[0], fmt.Sprintf(`/canales/%v/webhook"`, r.cuerpo["id"])) {
		t.Fatalf("saas sin override: %v", s)
	}
	b2, g2, admin2 := entornoQR(t, func(c *mensajeria.Config) {
		saas(c)
		c.BaseURL = ""
	})
	q := conectarQR(b2, admin2, pnidNuevo(), "AQB-"+pruebas.Unico(), "")
	if q.status != http.StatusUnprocessableEntity || !strings.HasSuffix(fmt.Sprint(q.cuerpo["type"]), "base_url_requerida") {
		t.Fatalf("saas sin IRIS_BASE_URL: %d %v", q.status, q.cuerpo)
	}
	if g2.cuenta("GET /oauth/access_token", "") != 0 {
		t.Fatal("canjeó el código sin poder suscribir el número")
	}
}

// B1: el número debe ser de la WABA.
func TestRevisionB1NumeroDeOtraWABA(t *testing.T) {
	b, g, admin := entornoQR(t, nil)
	conectarMeta(b, admin)
	g.responder(&g.wabaNums, 200, `{"data":[{"id":"1"}]}`)
	r := conectarQR(b, admin, pnidNuevo(), "AQB-"+pruebas.Unico(), "")
	if r.status != http.StatusUnprocessableEntity || !strings.HasSuffix(fmt.Sprint(r.cuerpo["type"]), "numero_no_elegible") {
		t.Fatalf("otra WABA: %d %v", r.status, r.cuerpo)
	}
	if g.cuenta("POST /", "/subscribed_apps") != 0 {
		t.Fatal("suscribió un número que no es de la WABA")
	}
}

// B3: un entry con cambios de dos números se separa; account_update vale para todos los de la WABA.
func TestRevisionB3SeparaPorNumeroYWABA(t *testing.T) {
	b, _, admin := entornoQR(t, nil)
	conectarMeta(b, admin)
	sufijo := pnidNuevo()[1:]
	waba := "9" + sufijo
	ids := []int64{}
	for _, d := range []string{"1", "2"} {
		r := b.api(admin, http.MethodPost, "/canales/qr", map[string]any{"code": "AQB-" + pruebas.Unico(), "wabaId": waba, "phoneNumberId": d + sufijo,
			"nombre": "QR " + d})
		b.exigir(r, http.StatusCreated)
		ids = append(ids, int64(r.cuerpo["id"].(float64)))
	}
	n := numeroCliente()
	cuerpo := fmt.Sprintf(`{"object":"whatsapp_business_account","entry":[{"id":%q,"changes":[`+
		`{"field":"messages","value":{"metadata":{"phone_number_id":%q},"messages":[{"from":%q,"id":"wamid.b3a-%s","timestamp":"%d","type":"text","text":{"body":"Al 1"}}]}},`+
		`{"field":"messages","value":{"metadata":{"phone_number_id":%q},"messages":[{"from":%q,"id":"wamid.b3b-%s","timestamp":"%d","type":"text","text":{"body":"Al 2"}}]}}]}]}`,
		waba, "1"+sufijo, n, pruebas.Unico(), b.reloj.Unix(), "2"+sufijo, n, pruebas.Unico(), b.reloj.Unix())
	b.exigir(b.webhookApp(cuerpo), http.StatusOK)
	for _, id := range ids {
		if c := b.contar(`SELECT count(*) FROM mensajeria.conversaciones WHERE canal_id = $1 AND numero = $2`, id, "+"+n); c != 1 {
			t.Fatalf("número %d: %d conversaciones", id, c)
		}
	}
	b.webhookApp(fmt.Sprintf(`{"object":"whatsapp_business_account","entry":[{"id":%q,"changes":[{"field":"account_update","value":{"event":"PARTNER_REMOVED"}}]}]}`, waba))
	for _, id := range ids {
		b.exigir(b.api(admin, http.MethodGet, fmt.Sprintf("/canales/%d", id), nil), http.StatusOK, "conexion", "desconectado")
	}
}

// B5: el historial no mueve el último mensaje ni la actividad de una conversación con algo más nuevo.
// B6: los contactos del teléfono no renombran un contacto que creó una persona.
func TestRevisionB5B6HistorialYContactos(t *testing.T) {
	b, _, admin := entornoQR(t, nil)
	conectarMeta(b, admin)
	pnid := pnidNuevo()
	r := conectarQR(b, admin, pnid, "AQB-"+pruebas.Unico(), "")
	b.exigir(r, http.StatusCreated)
	canal := int64(r.cuerpo["id"].(float64))
	waba := "9" + pnid[1:]
	b.exigir(b.api(admin, http.MethodPost, fmt.Sprintf("/canales/%d/sincronizar", canal), map[string]any{"contactos": true, "mensajes": true}), http.StatusOK)
	n := numeroCliente()
	b.webhookApp(mensajeQR(waba, pnid, n, b.reloj.Unix()))
	var conv int64
	var ultimo uuid.UUID
	var actividad time.Time
	if err := b.bd.QueryRow(b.ctx, `SELECT id, ultimo_mensaje_id, ultima_actividad FROM mensajeria.conversaciones WHERE canal_id = $1 AND numero = $2`, canal, "+"+n).
		Scan(&conv, &ultimo, &actividad); err != nil {
		t.Fatal(err)
	}
	*b.reloj = b.reloj.Add(time.Hour)
	hist := fmt.Sprintf(`{"object":"whatsapp_business_account","entry":[{"id":%q,"changes":[{"field":"history","value":{"metadata":{"phone_number_id":%q},`+
		`"history":[{"metadata":{"phase":1,"chunk_order":1,"progress":40},"threads":[{"id":%q,"context":{"wa_id":%q},"messages":[`+
		`{"from":%q,"id":"wamid.viejo-%s","timestamp":"%d","type":"text","text":{"body":"De hace un mes"}}]}]}]}}]}]}`,
		waba, pnid, n, n, n, pruebas.Unico(), b.reloj.Add(-30*24*time.Hour).Unix())
	b.webhookApp(hist)
	var ultimo2 uuid.UUID
	var actividad2 time.Time
	if err := b.bd.QueryRow(b.ctx, `SELECT ultimo_mensaje_id, ultima_actividad FROM mensajeria.conversaciones WHERE id = $1`, conv).Scan(&ultimo2, &actividad2); err != nil {
		t.Fatal(err)
	}
	if ultimo2 != ultimo || !actividad2.Equal(actividad) {
		t.Fatalf("el historial cambió la conversación: %v→%v %v→%v", ultimo, ultimo2, actividad, actividad2)
	}
	if c := b.contar(`SELECT count(*) FROM mensajeria.conversacion_mensajes WHERE conversacion_id = $1 AND origen = 'historial'`, conv); c != 1 {
		t.Fatalf("historial: %d", c)
	}

	// B6: un contacto creado por una persona no se renombra desde el teléfono.
	tel := numeroCliente()
	if _, err := b.bd.Exec(b.ctx, `INSERT INTO mensajeria.contactos_iris (empresa_id, nombre, tipo, telefono) VALUES (1, 'Proveedor Ana', 'proveedor', $1)`, "+"+tel); err != nil {
		t.Fatal(err)
	}
	b.webhookApp(fmt.Sprintf(`{"object":"whatsapp_business_account","entry":[{"id":%q,"changes":[{"field":"smb_app_state_sync","value":{"metadata":{"phone_number_id":%q},`+
		`"state_sync":[{"type":"contact","contact":{"full_name":"Anita del teléfono","phone_number":%q},"action":"add"}]}}]}]}`, waba, pnid, tel))
	if c := b.contar(`SELECT count(*) FROM mensajeria.contactos_iris WHERE telefono = $1 AND nombre = 'Proveedor Ana' AND origen = 'agente'`, "+"+tel); c != 1 {
		t.Fatal("renombró un contacto que creó una persona")
	}
	if c := b.contar(`SELECT count(*) FROM mensajeria.contactos_iris WHERE telefono = $1`, "+"+tel); c != 1 {
		t.Fatal("duplicó el contacto")
	}
}

// B7: un pedido de sincronización trabado se puede repetir; las huellas de códigos viejos se purgan;
// quien usa el número prestado no ve con quién más se comparte.
func TestRevisionB7(t *testing.T) {
	b, _, admin := entornoQR(t, nil)
	conectarMeta(b, admin)
	r := conectarQR(b, admin, pnidNuevo(), "AQB-"+pruebas.Unico(), "")
	b.exigir(r, http.StatusCreated)
	canal := fmt.Sprint(r.cuerpo["id"])
	// Un «pedido» sin respuesta de Meta (la llamada no terminó): a los 10 min se puede pedir otra vez.
	if _, err := b.bd.Exec(b.ctx, `UPDATE mensajeria.canales SET sincronizacion = jsonb_set(sincronizacion, '{contactos}', $2::jsonb) WHERE id = $1`,
		canal, fmt.Sprintf(`{"estado":"pedido","pedido_en":%q}`, b.reloj.Add(-5*time.Minute).Format(time.RFC3339))); err != nil {
		t.Fatal(err)
	}
	b.exigir(b.api(admin, http.MethodPost, "/canales/"+canal+"/sincronizar", map[string]any{"contactos": true}), http.StatusUnprocessableEntity)
	*b.reloj = b.reloj.Add(6 * time.Minute)
	b.exigir(b.api(admin, http.MethodPost, "/canales/"+canal+"/sincronizar", map[string]any{"contactos": true}), http.StatusOK)

	// Huellas de códigos de más de 7 días.
	if _, err := b.bd.Exec(b.ctx, `INSERT INTO mensajeria.registros_qr (hash_codigo, created_at) VALUES (sha256(convert_to($1, 'UTF8')), $2)`,
		"viejo-"+pruebas.Unico(), b.reloj.Add(-8*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := b.m.PurgarRegistrosQR(b.ctx, b.bd); err != nil {
		t.Fatal(err)
	}
	if c := b.contar(`SELECT count(*) FROM mensajeria.registros_qr WHERE created_at < $1`, b.reloj.Add(-7*24*time.Hour)); c != 0 {
		t.Fatalf("quedaron %d huellas viejas", c)
	}

	// compartidoCon: la dueña lo ve; quien lo usa, 0.
	p := nuevoPrestamo(t)
	admin2 := p.persona([]int64{2}, rolBase("admin"))
	admin2.empresa = 2
	p.exigir(p.api(admin2, http.MethodGet, fmt.Sprintf("/canales/%d", p.canal.ID), nil), http.StatusOK, "compartidoCon", "0")
	admin1 := p.persona([]int64{1}, rolBase("admin"))
	admin1.empresa = 1
	p.exigir(p.api(admin1, http.MethodGet, fmt.Sprintf("/canales/%d", p.canal.ID), nil), http.StatusOK, "compartidoCon", "1")
}

// Empresas a las que se puede pasar una conversación: con el permiso de pasar (sin
// canales.configurar), solo préstamos activos con uso de conversación y solo para la dueña.
func TestEmpresasDestino(t *testing.T) {
	p := nuevoPrestamo(t)
	if _, err := p.bd.Exec(p.ctx, `INSERT INTO nucleo.empresas (id, nombre, origen) VALUES (3, 'Empresa 3', 'argos') ON CONFLICT DO NOTHING`); err != nil {
		t.Fatal(err)
	}
	// La 3 lo usa solo para avisos: no es destino.
	p.exigir(p.api(p.supervisor, http.MethodPost, fmt.Sprintf("/canales/%d/prestamos", p.canal.ID), map[string]any{"empresaId": 3, "usos": []string{"avisos"}}),
		http.StatusOK)
	n := numeroCliente()
	p.texto(n, "wamid.ed-"+pruebas.Unico(), "Hola")
	c, _, _ := p.convDe(n)
	// soporte (sin canales.configurar) de la cola de la conversación.
	r := p.api(p.agente, http.MethodGet, fmt.Sprintf("/conversaciones/%d/empresas-destino", c), nil)
	p.exigir(r, http.StatusOK)
	if d := r.cuerpo["data"].([]any); len(d) != 1 || d[0].(map[string]any)["empresaId"].(float64) != 2 || d[0].(map[string]any)["nombre"] != "Empresa 2" {
		t.Fatalf("destinos: %v", r.cuerpo)
	}
	p.exigir(p.api(p.agente, http.MethodGet, fmt.Sprintf("/canales/%d/prestamos", p.canal.ID), nil), http.StatusForbidden)
	// Sin el permiso de pasar (lectura): 403.
	p.exigir(p.api(p.lectura, http.MethodGet, fmt.Sprintf("/conversaciones/%d/empresas-destino", c), nil), http.StatusForbidden)
	// Quien usa el número: en su conversación, [].
	n2 := numeroCliente()
	p.clienteArgos(2, n2)
	p.texto(n2, "wamid.ed2-"+pruebas.Unico(), "Hola")
	c2, e2, _ := p.convDe(n2)
	if e2 != 2 {
		t.Fatal("no fue a la 2")
	}
	r = p.api(p.sup2, http.MethodGet, fmt.Sprintf("/conversaciones/%d/empresas-destino", c2), nil)
	p.exigir(r, http.StatusOK)
	if d := r.cuerpo["data"].([]any); len(d) != 0 {
		t.Fatalf("destinos de quien usa el número: %v", d)
	}
	// La conversación de la otra empresa: 404.
	p.exigir(p.api(p.sup2, http.MethodGet, fmt.Sprintf("/conversaciones/%d/empresas-destino", c), nil), http.StatusNotFound)
}

// N1: reabrir una conversación vieja en un número compartido no le quita a otra empresa la abierta.
func TestRevisionN1Reabrir(t *testing.T) {
	p := nuevoPrestamo(t)
	n := numeroCliente()
	p.texto(n, "wamid.n1-"+pruebas.Unico(), "Hola")
	c1, e, _ := p.convDe(n)
	if e != 1 {
		t.Fatal("no fue a la dueña")
	}
	d := p.api(p.supervisor, http.MethodGet, fmt.Sprintf("/conversaciones/%d", c1), nil)
	p.exigir(p.api(p.supervisor, http.MethodPost, fmt.Sprintf("/conversaciones/%d/resolver", c1), map[string]any{"version": d.cuerpo["version"]}), http.StatusOK)
	p.clienteArgos(2, n)
	p.texto(n, "wamid.n1b-"+pruebas.Unico(), "Ahora soy de Cable Norte")
	c2, e2, _ := p.convDe(n)
	if e2 != 2 {
		t.Fatal("no fue a la 2")
	}
	d = p.api(p.supervisor, http.MethodGet, fmt.Sprintf("/conversaciones/%d", c1), nil)
	r := p.api(p.supervisor, http.MethodPost, fmt.Sprintf("/conversaciones/%d/reabrir", c1), map[string]any{"version": d.cuerpo["version"]})
	if r.status != http.StatusConflict || !strings.HasSuffix(fmt.Sprint(r.cuerpo["type"]), "conversacion_en_otra_empresa") {
		t.Fatalf("reabrir: %d %v", r.status, r.cuerpo)
	}
	wamid := "wamid.n1c-" + pruebas.Unico()
	p.texto(n, wamid, "¿Siguen ahí?")
	if c := p.contar(`SELECT count(*) FROM mensajeria.conversacion_mensajes WHERE conversacion_id = $1 AND proveedor_id = $2`, c2, wamid); c != 1 {
		t.Fatal("el mensaje no siguió en la conversación de la 2")
	}
}

// N2: responder en una resuelta (o con plantilla) tampoco le quita a otra empresa la abierta.
func TestRevisionN2Responder(t *testing.T) {
	p := nuevoPrestamo(t)
	n := numeroCliente()
	p.texto(n, "wamid.n2-"+pruebas.Unico(), "Hola")
	c1, _, _ := p.convDe(n)
	d := p.api(p.supervisor, http.MethodGet, fmt.Sprintf("/conversaciones/%d", c1), nil)
	p.exigir(p.api(p.supervisor, http.MethodPost, fmt.Sprintf("/conversaciones/%d/resolver", c1), map[string]any{"version": d.cuerpo["version"]}), http.StatusOK)
	p.clienteArgos(2, n)
	p.texto(n, "wamid.n2b-"+pruebas.Unico(), "Soy de Cable Norte")
	if _, e, _ := p.convDe(n); e != 2 {
		t.Fatal("no fue a la 2")
	}
	for _, cuerpo := range []map[string]any{
		{"id": uuid.Must(uuid.NewV7()).String(), "texto": "Hola de nuevo"},
		{"id": uuid.Must(uuid.NewV7()).String(), "plantilla": map[string]any{"codigo": "bienvenida", "variables": map[string]any{"plan": "Fibra"}}},
	} {
		r := p.api(p.supervisor, http.MethodPost, fmt.Sprintf("/conversaciones/%d/mensajes", c1), cuerpo)
		if r.status != http.StatusConflict || !strings.HasSuffix(fmt.Sprint(r.cuerpo["type"]), "conversacion_en_otra_empresa") {
			t.Fatalf("responder: %d %v", r.status, r.cuerpo)
		}
	}
	// Nada quedó registrado como inicio de la dueña.
	if c := p.contar(`SELECT count(*) FROM mensajeria.conversacion_mensajes WHERE conversacion_id = $1 AND direccion = 'saliente'`, c1); c != 0 {
		t.Fatalf("quedaron %d salientes", c)
	}
	wamid := "wamid.n2c-" + pruebas.Unico()
	p.texto(n, wamid, "¿Hola?")
	if c := p.contar(`SELECT count(*) FROM mensajeria.conversacion_mensajes m JOIN mensajeria.conversaciones c ON c.id = m.conversacion_id
		WHERE m.proveedor_id = $1 AND c.empresa_id = 2`, wamid); c != 1 {
		t.Fatal("el mensaje no siguió en la 2")
	}
}

// N3: un adjunto de otra empresa no se manda ni se registra como boleta en la conversación.
func TestRevisionN3AdjuntoDeOtraEmpresa(t *testing.T) {
	p := nuevoPrestamo(t)
	n := numeroCliente()
	p.clienteArgos(2, n)
	p.webhook(p.mensajeCliente(n, "wamid.n3-"+pruebas.Unico(), `"type":"image","image":{"id":"foto-n3","mime_type":"image/jpeg"}`))
	c, e, _ := p.convDe(n)
	if e != 2 {
		t.Fatal("no fue a la 2")
	}
	ms := p.mensajes(p.sup2, fmt.Sprint(c))
	if err := p.m.DescargarMedio(p.ctx, uuid.MustParse(ms[len(ms)-1]["id"].(string)), false); err != nil {
		t.Fatal(err)
	}
	ms = p.mensajes(p.sup2, fmt.Sprint(c))
	adj := ms[len(ms)-1]["adjunto"].(map[string]any)["id"].(string)
	// El adjunto queda de otra empresa (p. ej. el de una nota de la dueña de una conversación pasada).
	if _, err := p.bd.Exec(p.ctx, `UPDATE nucleo.adjuntos SET empresa_id = 1 WHERE id = $1`, adj); err != nil {
		t.Fatal(err)
	}
	r := p.api(p.sup2, http.MethodPost, fmt.Sprintf("/conversaciones/%d/mensajes", c), map[string]any{"id": uuid.Must(uuid.NewV7()).String(), "adjuntoId": adj})
	p.exigir(r, http.StatusUnprocessableEntity, "type", "urn:iris:problema:adjunto_no_listo")
	boleta := map[string]any{"adjuntoId": adj, "metodo": "deposito", "cuentaBancariaId": 3, "referencia": "N3-" + pruebas.Unico(), "monto": "250.00",
		"fechaDeposito": "2026-11-04"}
	p.exigir(p.api(p.sup2, http.MethodPost, fmt.Sprintf("/conversaciones/%d/boletas", c), boleta), http.StatusUnprocessableEntity, "type",
		"urn:iris:problema:adjunto_no_listo")
	// De su empresa, sí.
	if _, err := p.bd.Exec(p.ctx, `UPDATE nucleo.adjuntos SET empresa_id = 2 WHERE id = $1`, adj); err != nil {
		t.Fatal(err)
	}
	p.exigir(p.api(p.sup2, http.MethodPost, fmt.Sprintf("/conversaciones/%d/mensajes", c), map[string]any{"id": uuid.Must(uuid.NewV7()).String(), "adjuntoId": adj}),
		http.StatusCreated)
	if r := p.api(p.sup2, http.MethodPost, fmt.Sprintf("/conversaciones/%d/boletas", c), boleta); r.status/100 != 2 {
		t.Fatalf("boleta con el adjunto de su empresa: %d %v", r.status, r.cuerpo)
	}
}

// transporteCaido falla como un error de red real: su texto trae la URL completa.
type transporteCaido struct{}

func (transporteCaido) RoundTrip(r *http.Request) (*http.Response, error) {
	return nil, &url.Error{Op: r.Method, URL: r.URL.String(), Err: errors.New("dial tcp: conexión rechazada")}
}

// N4 · B2: con la red caída, ni el secreto ni el código salen en la respuesta ni en la auditoría.
func TestRevisionN4RedCaidaSinSecretos(t *testing.T) {
	b, _, admin := entornoQR(t, func(c *mensajeria.Config) { c.HTTPMeta = &http.Client{Transport: transporteCaido{}} })
	conectarMeta(b, admin)
	r := b.api(admin, http.MethodPost, "/conexion-meta/probar", nil)
	b.exigir(r, http.StatusOK, "estado", "error")
	codigo := "AQB-red-" + pruebas.Unico()
	q := conectarQR(b, admin, pnidNuevo(), codigo, "")
	if q.status != http.StatusBadGateway {
		t.Fatalf("QR con la red caída: %d %v", q.status, q.cuerpo)
	}
	var aud string
	if err := b.bd.QueryRow(b.ctx, `SELECT coalesce(string_agg(entrada::text || coalesce(resultado::text, ''), ''), '') FROM auditoria.acciones
		WHERE accion LIKE 'mensajeria.%conexion%' OR accion LIKE 'mensajeria.conectar_qr%'`).Scan(&aud); err != nil {
		t.Fatal(err)
	}
	sinSecretos(t, cadena(r.cuerpo)+cadena(q.cuerpo)+aud, appSecretQR, codigo, "access_token", "client_secret")
}

// centralFalso es un servicio central que sí responde (para probar M3 en modo compartida).
type centralFalso struct {
	alternos []*mensajeria.WebhookAlterno
}

func (c *centralFalso) CanjearCodigo(context.Context, string) (string, error) {
	return tokenNegocio, nil
}
func (c *centralFalso) ConsultarNumero(context.Context, string, string, string) (mensajeria.NumeroRegistrado, error) {
	return mensajeria.NumeroRegistrado{Numero: "+50255123344"}, nil
}
func (c *centralFalso) SuscribirWABA(_ context.Context, _, _ string, a *mensajeria.WebhookAlterno) error {
	c.alternos = append(c.alternos, a)
	return nil
}
func (c *centralFalso) PedirSincronizacion(context.Context, string, string, string) (string, error) {
	return "req", nil
}

// N4 · M3 en modo compartida: siempre la URL del número; sin IRIS_BASE_URL, 422.
func TestRevisionN4CompartidaSiempreURLDelNumero(t *testing.T) {
	central := &centralFalso{}
	b, _, admin := entornoQR(t, func(c *mensajeria.Config) { c.ServicioCentral = central })
	b.exigir(b.api(admin, http.MethodPost, "/conexion-meta/actualizar", map[string]any{"version": 0, "modo": "compartida", "claveEnlace": "clave-de-enlace-horus-0123456"}),
		http.StatusOK)
	r := conectarQR(b, admin, pnidNuevo(), "AQB-"+pruebas.Unico(), "")
	b.exigir(r, http.StatusCreated)
	if len(central.alternos) != 1 || central.alternos[0] == nil ||
		central.alternos[0].URL != fmt.Sprintf("https://iris.ejemplo.gt/api/v1/mensajeria/canales/%v/webhook", r.cuerpo["id"]) {
		t.Fatalf("compartida sin override: %v", central.alternos)
	}
	central2 := &centralFalso{}
	b2, _, admin2 := entornoQR(t, func(c *mensajeria.Config) {
		c.ServicioCentral = central2
		c.BaseURL = ""
	})
	b2.exigir(b2.api(admin2, http.MethodPost, "/conexion-meta/actualizar", map[string]any{"version": 0, "modo": "compartida", "claveEnlace": "clave-de-enlace-horus-0123456"}),
		http.StatusOK)
	q := conectarQR(b2, admin2, pnidNuevo(), "AQB-"+pruebas.Unico(), "")
	if q.status != http.StatusUnprocessableEntity || !strings.HasSuffix(fmt.Sprint(q.cuerpo["type"]), "base_url_requerida") || len(central2.alternos) != 0 {
		t.Fatalf("compartida sin IRIS_BASE_URL: %d %v", q.status, q.cuerpo)
	}
}

// N4 · B7: el préstamo solo a una empresa registrada (FK).
func TestRevisionN4FKPrestamo(t *testing.T) {
	b := nuevaBandeja(t, nil)
	_, err := b.bd.Exec(b.ctx, `INSERT INTO mensajeria.canal_prestamos (canal_id, empresa_id, usos, prestado_en) VALUES ($1, 987654321, '{soporte}', now())`, b.canal.ID)
	var pg *pgconn.PgError
	if !errors.As(err, &pg) || pg.Code != "23503" {
		t.Fatalf("FK de canal_prestamos.empresa_id: %v", err)
	}
}

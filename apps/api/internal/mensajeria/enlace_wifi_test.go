package mensajeria_test

import (
	"crypto/sha256"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
)

// Enlace de un solo uso para cambiar el Wi-Fi (contrato argosisp.md 0.9.0 §12.4, B14): sirve una sola
// vez, vence a los 10 min, caduca si cambia el cliente de la conversación, y la clave nunca queda en
// Iris (solo cifrada en el depósito de tránsito hasta Argos).
func TestEnlaceWifiUnSoloUso(t *testing.T) {
	e := nuevoEntorno(t, nil)
	var canal, conv, verif int64
	if err := e.bd.QueryRow(e.ctx, `INSERT INTO mensajeria.canales (empresa_id, tipo, canal, nombre, numero, phone_number_id, credenciales_cifradas)
		VALUES (1, 'whatsapp_cloud', 'whatsapp', $1, $2, $3, '\x00') RETURNING id`, "Wi-Fi "+pruebas.Unico(),
		fmt.Sprintf("+50222%06d", time.Now().UnixNano()%1000000), pruebas.Unico()).Scan(&canal); err != nil {
		t.Fatal(err)
	}
	if err := e.bd.QueryRow(e.ctx, `INSERT INTO mensajeria.conversaciones (empresa_id, canal_id, numero, ultima_actividad, cliente_id)
		VALUES (1, $1, '+50255550001', now(), 990) RETURNING id`, canal).Scan(&conv); err != nil {
		t.Fatal(err)
	}
	if err := e.bd.QueryRow(e.ctx, `INSERT INTO mensajeria.asistente_verificaciones (empresa_id, conversacion_id, cliente_id, resultado, creada_en)
		VALUES (1, $1, 990, 'coincide', $2) RETURNING id`, conv, *e.reloj).Scan(&verif); err != nil {
		t.Fatal(err)
	}
	crear := func(token string, vence time.Time) {
		h := sha256.Sum256([]byte(token))
		if _, err := e.bd.Exec(e.ctx, `INSERT INTO mensajeria.enlaces_wifi (id, huella, empresa_id, conversacion_id, cliente_id, servicio_id,
			verificacion_id, metodo, verificado_en, vence_en) VALUES ($1, $2, 1, $3, 990, 9901, $4, 'datos_titular', $5, $6)`,
			uuid.Must(uuid.NewV7()), h[:], conv, verif, *e.reloj, vence); err != nil {
			t.Fatal(err)
		}
	}
	token := strings.Repeat("a", 43)
	crear(token, e.reloj.Add(10*time.Minute))
	ruta := "/api/v1/mensajeria/publico/wifi/" + token
	if r := e.pedir(http.MethodGet, ruta, "", "", nil); r.status != 200 || r.cuerpo["vigente"] != true {
		t.Fatalf("GET vigente: %d %v", r.status, r.cuerpo)
	}
	// Una clave fuera de forma no consume el enlace.
	if r := e.pedir(http.MethodPost, ruta, "", "", map[string]any{"clave": "corta"}); r.status != 422 {
		t.Fatalf("clave corta: %d %v", r.status, r.cuerpo)
	}
	const clave = "Nueva-Clave-Wifi-99"
	if r := e.pedir(http.MethodPost, ruta, "", "", map[string]any{"ssid": "Casa Pérez", "clave": clave}); r.status != 202 {
		t.Fatalf("POST: %d %v", r.status, r.cuerpo)
	}
	// Un solo uso.
	if r := e.pedir(http.MethodPost, ruta, "", "", map[string]any{"ssid": "Otra"}); r.status != 410 {
		t.Fatalf("segundo uso: %d %v", r.status, r.cuerpo)
	}
	if r := e.pedir(http.MethodGet, ruta, "", "", nil); r.status != 200 || r.cuerpo["vigente"] != false {
		t.Fatalf("GET usado: %d %v", r.status, r.cuerpo)
	}
	if n := e.contar(`SELECT count(*) FROM integracion.pendientes WHERE operacion = 'cambiar_wifi' AND entidad = 'conversacion' AND entidad_id = $1
		AND actor = 'asistente_ia' AND payload->'verificacion'->>'metodo' = 'datos_titular' AND payload->'verificacion'->>'cliente_id' = '990'`,
		fmt.Sprint(conv)); n != 1 {
		t.Fatalf("pendiente del cambio: %d", n)
	}
	for _, sql := range []string{
		"SELECT count(*) FROM integracion.pendientes WHERE payload::text LIKE '%' || $1 || '%'",
		"SELECT count(*) FROM auditoria.acciones WHERE entrada::text LIKE '%' || $1 || '%'",
		"SELECT count(*) FROM integracion.outbox WHERE payload::text LIKE '%' || $1 || '%'",
		"SELECT count(*) FROM integracion.cambios_wifi c WHERE row_to_json(c)::text LIKE '%' || $1 || '%'",
	} {
		if n := e.contar(sql, clave); n != 0 {
			t.Fatalf("la clave quedó en Iris: %s", sql)
		}
	}
	// Vencido o con otro cliente en la conversación: 410, sin consumir nada.
	vencido := strings.Repeat("b", 43)
	crearOtra := func(tok string, vence time.Time) {
		if _, err := e.bd.Exec(e.ctx, `INSERT INTO mensajeria.asistente_verificaciones (empresa_id, conversacion_id, cliente_id, resultado, creada_en)
			VALUES (1, $1, 990, 'coincide', $2) RETURNING id`, conv, *e.reloj); err != nil {
			t.Fatal(err)
		}
		if err := e.bd.QueryRow(e.ctx, `SELECT max(id) FROM mensajeria.asistente_verificaciones WHERE conversacion_id = $1`, conv).Scan(&verif); err != nil {
			t.Fatal(err)
		}
		crear(tok, vence)
	}
	crearOtra(vencido, e.reloj.Add(-time.Minute))
	if r := e.pedir(http.MethodPost, "/api/v1/mensajeria/publico/wifi/"+vencido, "", "", map[string]any{"ssid": "X"}); r.status != 410 {
		t.Fatalf("vencido: %d", r.status)
	}
	otro := strings.Repeat("c", 43)
	crearOtra(otro, e.reloj.Add(10*time.Minute))
	if _, err := e.bd.Exec(e.ctx, `UPDATE mensajeria.conversaciones SET cliente_id = 991 WHERE id = $1`, conv); err != nil {
		t.Fatal(err)
	}
	if r := e.pedir(http.MethodPost, "/api/v1/mensajeria/publico/wifi/"+otro, "", "", map[string]any{"ssid": "X"}); r.status != 410 {
		t.Fatalf("otro cliente: %d", r.status)
	}
	if n := e.contar("SELECT count(*) FROM integracion.pendientes WHERE operacion = 'cambiar_wifi'"); n != 1 {
		t.Fatalf("pendientes de Wi-Fi: %d", n)
	}
}

// bienvenida_servicio (0.9.0 §12.5, B16): Iris inserta las cuentas de su instantánea (activas, de la
// empresa, en GTQ, con titular y sin cambios de las últimas 24 h salvo confirmadas); sin cuentas, la
// variante sin cuentas con detalle sin_cuentas; origen solo con una orden de Iris de ese servicio.
func TestAvisoBienvenidaServicio(t *testing.T) {
	e := nuevoEntorno(t, nil)
	t.Cleanup(func() { _, _ = e.bd.Exec(e.ctx, "DELETE FROM integracion.argos_cuentas_bancarias WHERE id BETWEEN 9100 AND 9199") })
	cliente := e.cliente(1, "+502 5512-3344")
	servicio := cliente * 10
	vars := map[string]any{"nombre": "Ana Pérez", "plan": "Fibra 100", "precio_mensual": "250.00", "moneda": "GTQ", "codigo_cliente": "C-0091"}
	pedir := func(clave string, extra map[string]any) respuesta {
		c := map[string]any{"empresa_id": 1, "cliente_id": cliente, "servicio_id": servicio, "plantilla": "bienvenida_servicio", "variables": vars}
		for k, v := range extra {
			c[k] = v
		}
		return e.avisar(clave, c)
	}
	// Sin cuentas válidas: la variante sin cuentas.
	r := pedir("argos:aviso:bienvenida:1", nil)
	e.exigir(r, 202)
	var plantilla, detalle string
	if err := e.bd.QueryRow(e.ctx, "SELECT plantilla, coalesce(detalle, '') FROM mensajeria.avisos WHERE id = $1", r.cuerpo["aviso_id"]).Scan(&plantilla, &detalle); err != nil {
		t.Fatal(err)
	}
	if plantilla != "bienvenida_servicio_sin_cuentas" || detalle != "sin_cuentas" {
		t.Fatalf("sin cuentas: %s %s", plantilla, detalle)
	}
	// Cuentas: una válida, una en USD, una sin titular y una recién cambiada (sin confirmar).
	for _, c := range []string{
		"(9101, 1, 'Banco Industrial', '1234567890', 'Empresa S.A.', 'monetaria', 'GTQ', true, NULL)",
		"(9102, 1, 'Banrural', '5555555555', 'Empresa S.A.', 'monetaria', 'USD', true, NULL)",
		"(9103, 1, 'BAM', '6666666666', NULL, 'ahorro', 'GTQ', true, NULL)",
		"(9104, 1, 'G&T', '7777777777', 'Otro', 'monetaria', 'GTQ', true, now())",
	} {
		if _, err := e.bd.Exec(e.ctx, `INSERT INTO integracion.argos_cuentas_bancarias (id, empresa_id, banco_nombre, numero, titular, tipo, moneda, activa, cambiada_en, sincronizado_en)
			VALUES `+strings.TrimSuffix(c, ")")+`, now())`); err != nil {
			t.Fatal(err)
		}
	}
	// Argos no manda cuentas ni variables de más; un nombre con un número largo no sale.
	malas := map[string]any{"nombre": "Paga a la 12345678", "plan": "Fibra 100", "precio_mensual": "250.00", "moneda": "GTQ", "codigo_cliente": "C-0091"}
	e.exigir(e.avisar("argos:aviso:bienvenida:2", map[string]any{"empresa_id": 1, "cliente_id": cliente, "servicio_id": servicio,
		"plantilla": "bienvenida_servicio", "variables": malas}), 422)
	conCuentas := map[string]any{}
	for k, v := range vars {
		conCuentas[k] = v
	}
	conCuentas["cuentas"] = "Banco X 999999"
	e.exigir(e.avisar("argos:aviso:bienvenida:3", map[string]any{"empresa_id": 1, "cliente_id": cliente, "servicio_id": servicio,
		"plantilla": "bienvenida_servicio", "variables": conCuentas}), 422)
	// origen: tipo distinto o una orden que no es de ese servicio → origen_invalido.
	r = pedir("argos:aviso:bienvenida:4", map[string]any{"origen": map[string]any{"tipo": "argos", "id": "1"}})
	if r.status != 422 || r.cuerpo["type"] != "urn:iris:problema:origen_invalido" {
		t.Fatalf("origen con otro tipo: %d %v", r.status, r.cuerpo)
	}
	r = pedir("argos:aviso:bienvenida:5", map[string]any{"origen": map[string]any{"tipo": "iris_orden", "id": "999999"}})
	if r.status != 422 || r.cuerpo["type"] != "urn:iris:problema:origen_invalido" {
		t.Fatalf("orden ajena: %d %v", r.status, r.cuerpo)
	}
	r = pedir("argos:aviso:bienvenida:6", nil)
	e.exigir(r, 202)
	var variables string
	if err := e.bd.QueryRow(e.ctx, "SELECT plantilla, variables::text FROM mensajeria.avisos WHERE id = $1", r.cuerpo["aviso_id"]).Scan(&plantilla, &variables); err != nil {
		t.Fatal(err)
	}
	if plantilla != "bienvenida_servicio" || !strings.Contains(variables, "1234567890") || strings.Contains(variables, "5555555555") ||
		strings.Contains(variables, "6666666666") || strings.Contains(variables, "7777777777") {
		t.Fatalf("cuentas insertadas por Iris: %s %s", plantilla, variables)
	}
}

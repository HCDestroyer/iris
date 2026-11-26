package mensajeria_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos"
	argosbd "github.com/HCDestroyer/iris/apps/api/internal/integracion/argos/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/mensajeria"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
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
	// La instantánea de Argos: el servicio es del cliente (se revalida al usar el enlace).
	if _, err := e.bd.Exec(e.ctx, `INSERT INTO integracion.argos_clientes (id, empresa_id, nombre, sincronizado_en) VALUES (990, 1, 'Ana', now())
		ON CONFLICT (id) DO NOTHING`); err != nil {
		t.Fatal(err)
	}
	if _, err := e.bd.Exec(e.ctx, `INSERT INTO integracion.argos_servicios (id, empresa_id, cliente_id, sincronizado_en) VALUES (9901, 1, 990, now())
		ON CONFLICT (id) DO UPDATE SET cliente_id = 990`); err != nil {
		t.Fatal(err)
	}
	token := strings.Repeat("a", 43)
	crear(token, e.reloj.Add(10*time.Minute))
	ruta := "/api/v1/mensajeria/publico/wifi/" + token
	if r := e.pedir(http.MethodGet, ruta, "", "", nil); r.status != 200 || r.cuerpo["vigente"] != true ||
		r.h.Get("Cache-Control") != "no-store" || r.h.Get("Referrer-Policy") != "no-referrer" {
		t.Fatalf("GET vigente: %d %v %v", r.status, r.cuerpo, r.h)
	}
	// El token nunca sale en los logs HTTP.
	if m := nucleo.EnmascararRuta(ruta); strings.Contains(m, token) {
		t.Fatalf("ruta sin enmascarar: %s", m)
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
	if r := e.pedir(http.MethodPost, ruta, "", "", map[string]any{"ssid": "Otra"}); r.status != 410 || r.h.Get("Cache-Control") != "no-store" {
		t.Fatalf("segundo uso: %d %v", r.status, r.cuerpo)
	}
	// Un 410 no deja fila en la auditoría (lo provoca cualquiera).
	if n := e.contar("SELECT count(*) FROM auditoria.acciones WHERE accion = 'servicios.cambiar_wifi' AND error::text LIKE '%enlace_vencido%'"); n != 0 {
		t.Fatalf("410 auditado: %d", n)
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
	// El nombre del Wi-Fi tampoco queda en la auditoría (solo la máscara).
	if n := e.contar("SELECT count(*) FROM auditoria.acciones WHERE entrada::text LIKE '%' || $1 || '%'", "Casa Pérez"); n != 0 {
		t.Fatalf("el SSID quedó en la auditoría: %d", n)
	}
	if n := e.contar(`SELECT count(*) FROM auditoria.acciones WHERE accion = 'servicios.cambiar_wifi' AND entrada->>'ssid' = '***'`); n < 1 {
		t.Fatalf("la entrada del cambio lleva el ssid enmascarado: %d", n)
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
	t.Cleanup(func() {
		_, _ = e.bd.Exec(e.ctx, "DELETE FROM integracion.argos_cuentas_bancarias WHERE id BETWEEN 9100 AND 9199")
	})
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

// M2 y M3: el mensaje guardado lleva el enlace tapado (el token solo va a Meta), y una clave escrita en
// el chat queda *** también en el payload crudo de Meta, aunque no nombre el Wi-Fi si cambiar_wifi está
// encendido.
func TestEnlaceTapadoYClaveEnElPayloadCrudo(t *testing.T) {
	e := nuevoEntorno(t, nil)
	cif, err := nucleo.NuevoCifrador(claveCifrado)
	if err != nil {
		t.Fatal(err)
	}
	var canal, conv, verif int64
	if err := e.bd.QueryRow(e.ctx, `INSERT INTO mensajeria.canales (empresa_id, tipo, canal, nombre, numero, phone_number_id, credenciales_cifradas)
		VALUES (1, 'whatsapp_cloud', 'whatsapp', $1, $2, $3, '\x00') RETURNING id`, "Tapado "+pruebas.Unico(),
		fmt.Sprintf("+50223%06d", time.Now().UnixNano()%1000000), pruebas.Unico()).Scan(&canal); err != nil {
		t.Fatal(err)
	}
	if err := e.bd.QueryRow(e.ctx, `INSERT INTO mensajeria.conversaciones (empresa_id, canal_id, numero, ultima_actividad, cliente_id)
		VALUES (1, $1, '+50255550002', now(), 990) RETURNING id`, canal).Scan(&conv); err != nil {
		t.Fatal(err)
	}
	if err := e.bd.QueryRow(e.ctx, `INSERT INTO mensajeria.asistente_verificaciones (empresa_id, conversacion_id, cliente_id, resultado, creada_en)
		VALUES (1, $1, 990, 'coincide', $2) RETURNING id`, conv, *e.reloj).Scan(&verif); err != nil {
		t.Fatal(err)
	}
	token := strings.Repeat("t", 43)
	id := uuid.Must(uuid.NewV7())
	cifrado, err := cif.Cifrar([]byte(token), []byte("enlace_wifi:"+id.String()))
	if err != nil {
		t.Fatal(err)
	}
	h := sha256.Sum256([]byte(token))
	if _, err := e.bd.Exec(e.ctx, `INSERT INTO mensajeria.enlaces_wifi (id, huella, empresa_id, conversacion_id, cliente_id, servicio_id,
		verificacion_id, metodo, verificado_en, vence_en, token_cifrado) VALUES ($1, $2, 1, $3, 990, 9901, $4, 'datos_titular', $5, $6, $7)`,
		id, h[:], conv, verif, *e.reloj, e.reloj.Add(10*time.Minute), cifrado); err != nil {
		t.Fatal(err)
	}
	guardado := "Entra a https://iris.prueba" + mensajeria.EnlaceWifiTapado + " para cambiarlo"
	if strings.Contains(guardado, token) {
		t.Fatal("el texto guardado lleva el token")
	}
	ctx := nucleo.ConBD(context.Background(), e.bd)
	meta, err := e.m.ConTokenEnlaceParaPrueba(ctx, conv, guardado, *e.reloj)
	if err != nil || !strings.Contains(meta, "/wifi/"+token) {
		t.Fatalf("hacia Meta: %q %v", meta, err)
	}
	// Vencido: sale tapado.
	if meta, _ := e.m.ConTokenEnlaceParaPrueba(ctx, conv, guardado, e.reloj.Add(11*time.Minute)); strings.Contains(meta, token) {
		t.Fatal("un enlace vencido no lleva el token")
	}

	// M3: la clave en el payload crudo (mensajeria.entrantes), con cambiar_wifi encendido. Al terminar,
	// los ajustes de la empresa vuelven a como estaban.
	var previos []byte
	_ = e.bd.QueryRow(e.ctx, "SELECT perfiles FROM mensajeria.asistente_ajustes WHERE coalesce(empresa_id, 0) = 1").Scan(&previos)
	t.Cleanup(func() {
		if previos == nil {
			_, _ = e.bd.Exec(e.ctx, "DELETE FROM mensajeria.asistente_ajustes WHERE coalesce(empresa_id, 0) = 1")
			return
		}
		_, _ = e.bd.Exec(e.ctx, "UPDATE mensajeria.asistente_ajustes SET perfiles = $1 WHERE coalesce(empresa_id, 0) = 1", previos)
	})
	if _, err := e.bd.Exec(e.ctx, `INSERT INTO mensajeria.asistente_ajustes (empresa_id, perfiles) VALUES (1, $1)
		ON CONFLICT ((coalesce(empresa_id, 0))) DO UPDATE SET perfiles = EXCLUDED.perfiles`,
		`[{"codigo":"defecto","nombre":"Defecto","acciones":{"cambiar_wifi":true,"pasar_a_persona":true},"limites":{"prioridadMaxima":"media","diasAdelanteMax":3}}]`); err != nil {
		t.Fatal(err)
	}
	wamid := "wamid.TAPAR" + pruebas.Unico()
	crudo, _ := json.Marshal(map[string]any{"entry": []any{map[string]any{"changes": []any{map[string]any{"value": map[string]any{
		"messages": []any{map[string]any{"id": wamid, "type": "text", "text": map[string]any{"body": "contraseña: Perro2024!"}}}}}}}}})
	if _, err := e.bd.Exec(e.ctx, `INSERT INTO mensajeria.entrantes (canal_id, hash_cuerpo, payload) VALUES ($1, $2, $3)`,
		canal, []byte(pruebas.Unico()), crudo); err != nil {
		t.Fatal(err)
	}
	empresa := int64(1)
	tx, err := e.bd.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	texto, err := mensajeria.TaparWifiEntranteParaPrueba(nucleo.ConBD(ctx, tx), canal, wamid, &empresa, "contraseña: Perro2024!")
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(texto, "Perro2024") {
		t.Fatalf("texto: %q", texto)
	}
	if n := e.contar("SELECT count(*) FROM mensajeria.entrantes WHERE payload::text LIKE '%Perro2024%'"); n != 0 {
		t.Fatalf("la clave quedó en el payload crudo: %d", n)
	}
}

// M6 y M7: confirmar una cuenta exige su empresa y el cambiadaEn leído; reactivar una cuenta la deja
// fuera de la bienvenida 24 h.
func TestConfirmarCuentaRecienCambiada(t *testing.T) {
	e := nuevoEntorno(t, nil)
	t.Cleanup(func() {
		_, _ = e.bd.Exec(e.ctx, "DELETE FROM integracion.argos_cuentas_bancarias WHERE id BETWEEN 9200 AND 9299")
	})
	q := argosbd.New(e.bd)
	guardar := func(activa bool) {
		n, err := q.GuardarCuentaBancaria(e.ctx, argosbd.GuardarCuentaBancariaParams{ID: 9201, EmpresaID: 1, BancoNombre: "BI", Numero: "1234567890",
			Titular: ptrTexto("Empresa"), Tipo: ptrTexto("monetaria"), Moneda: "GTQ", Activa: activa, SincronizadoEn: time.Now()})
		if err != nil || n != 1 {
			t.Fatalf("guardar cuenta: %v", err)
		}
	}
	guardar(false)
	if _, err := e.bd.Exec(e.ctx, "UPDATE integracion.argos_cuentas_bancarias SET cambiada_en = NULL WHERE id = 9201"); err != nil {
		t.Fatal(err)
	}
	guardar(true) // reactivada en Argos
	var cambiada *time.Time
	if err := e.bd.QueryRow(e.ctx, "SELECT cambiada_en FROM integracion.argos_cuentas_bancarias WHERE id = 9201").Scan(&cambiada); err != nil || cambiada == nil {
		t.Fatalf("reactivar marca cambiada_en: %v %v", cambiada, err)
	}
	am := argos.Nuevo(argos.Config{})
	admin := e.persona([]int64{1}, rolBase("admin"))
	en1 := pruebas.ContextoUsuarioEnEmpresa(e.bd, admin.id, 1, nucleo.PermIntegracionesConfirmarCuentaBancaria)
	en2 := pruebas.ContextoUsuarioEnEmpresa(e.bd, admin.id, 2, nucleo.PermIntegracionesConfirmarCuentaBancaria)
	in := argos.EntradaConfirmarCuenta{ID: 9201}
	in.Body.CambiadaEn = cambiada
	if _, err := am.ConfirmarCuentaBancaria(en2, in); !esProblema(err, 404) {
		t.Fatalf("otra empresa: %v", err)
	}
	viejo := cambiada.Add(-time.Hour)
	in2 := in
	in2.Body.CambiadaEn = &viejo
	if _, err := am.ConfirmarCuentaBancaria(en1, in2); !esProblema(err, 409) {
		t.Fatalf("cambió después de revisarla: %v", err)
	}
	if r, err := am.ConfirmarCuentaBancaria(en1, in); err != nil || r.ConfirmadaEn == nil {
		t.Fatalf("confirmar: %+v %v", r, err)
	}
}

func esProblema(err error, status int) bool {
	var p *nucleo.Problema
	return errors.As(err, &p) && p.Status == status
}

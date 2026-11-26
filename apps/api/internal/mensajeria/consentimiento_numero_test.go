package mensajeria_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"testing"
	"time"
)

// claveNumero arma la Idempotency-Key de 0.12.1 con el HMAC del número (aquí con un secreto de prueba:
// Iris solo exige la forma y que el número no vaya en claro).
func claveNumero(cliente int64, numero string, cuando time.Time) string {
	h := hmac.New(sha256.New, []byte("secreto-de-webhooks"))
	h.Write([]byte("consentimiento:" + numero))
	return fmt.Sprintf("argos:consentimiento:1:%d:whatsapp:%s:%s", cliente, hex.EncodeToString(h.Sum(nil))[:16], cuando.Format(time.RFC3339))
}

// sembrarContactoAviso siembra un contacto del cliente en la caché de Argos con un número con WhatsApp.
func (e *entorno) sembrarContactoAviso(cliente, contacto int64, numero string, avisos bool, servicios any) {
	e.t.Helper()
	if _, err := e.bd.Exec(e.ctx, `INSERT INTO integracion.argos_contactos (contacto_id, cliente_id, empresa_id, nombre, relacion, servicios, avisos, version)
		VALUES ($1, $2, 1, 'Luis', 'familiar', $3, $4, 1) ON CONFLICT DO NOTHING`, contacto, cliente, servicios, avisos); err != nil {
		e.t.Fatal(err)
	}
	if _, err := e.bd.Exec(e.ctx, `INSERT INTO integracion.argos_contacto_telefonos (contacto_id, numero, digitos, whatsapp, principal)
		VALUES ($1, $2, $3, true, true) ON CONFLICT DO NOTHING`, contacto, numero, strings.TrimPrefix(numero, "+")); err != nil {
		e.t.Fatal(err)
	}
}

func (e *entorno) consentirNumero(cliente int64, numero, estado string, cuando time.Time, clave string) respuesta {
	return e.pedir(http.MethodPost, "/api/v1/mensajeria/consentimientos", e.token, clave,
		map[string]any{"empresa_id": 1, "cliente_id": cliente, "canal": "whatsapp", "estado": estado, "origen": "argos_portal", "ocurrido_en": cuando, "telefono": numero})
}

// Permiso por número (§6.4, 0.10.0 y 0.12.1): el de un contacto es aparte del del cliente; la clave
// lleva el HMAC del número y nunca el número; el número no va a eventos ni en claro a la auditoría.
func TestConsentimientoPorNumero(t *testing.T) {
	e := nuevoEntorno(t, nil)
	c := e.cliente(1, "5512-3344")
	numero := "+50255667788"
	cuando := e.reloj.Add(-time.Hour)
	// Un número que no es de un contacto del cliente: 422 numero_no_es_contacto.
	e.exigir(e.consentirNumero(c, numero, "acepta", cuando, claveNumero(c, numero, cuando)), http.StatusUnprocessableEntity,
		"type", "urn:iris:problema:numero_no_es_contacto")
	e.sembrarContactoAviso(c, siguienteCliente.Add(1)*100, numero, true, nil)
	// Con el número en la clave: 422.
	e.exigir(e.consentirNumero(c, numero, "acepta", cuando, fmt.Sprintf("argos:consentimiento:1:%d:whatsapp:%s:x", c, strings.TrimPrefix(numero, "+"))),
		http.StatusUnprocessableEntity)
	clave := claveNumero(c, numero, cuando)
	e.exigir(e.consentirNumero(c, numero, "acepta", cuando, clave), http.StatusCreated, "estado_vigente", "acepta")
	e.exigir(e.consentirNumero(c, numero, "acepta", cuando, clave), http.StatusOK, "estado_vigente", "acepta")
	// El del cliente no cambia: sigue sin permiso (un aviso al cliente queda bloqueado).
	r := e.avisar(fmt.Sprintf("argos:aviso:x:%d", c), avisoPorVencer(c, "2026-11-10"))
	e.exigir(r, http.StatusAccepted, "estado", "bloqueado", "motivo", "sin_permiso")
	// Una baja posterior del número manda sobre su acepta.
	despues := e.reloj.Add(-time.Minute)
	e.exigir(e.consentirNumero(c, numero, "baja", despues, claveNumero(c, numero, despues)), http.StatusCreated, "estado_vigente", "baja")
	if n := e.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'consentimiento.registrado' AND (payload->>'cliente_id')::bigint = $1
		AND (payload->>'por_numero')::boolean AND payload::text NOT LIKE '%55667788%'`, c); n != 2 {
		t.Fatalf("consentimiento.registrado por número, sin el número: %d", n)
	}
	if n := e.contar(`SELECT count(*) FROM auditoria.acciones WHERE accion = 'mensajeria.registrar_consentimiento' AND entrada::text LIKE '%55667788%'`); n != 0 {
		t.Fatal("el número quedó en claro en la auditoría")
	}
	if n := e.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'contacto.baja' AND (payload->>'cliente_id')::bigint = $1`, c); n != 0 {
		t.Fatal("la baja de un número de contacto no es contacto.baja del cliente")
	}
}

// Avisos de cobro a los contactos (§3.5): salen también a los contactos con «avisos» del servicio, al
// número de la caché y solo con su permiso por número; sin Idempotency-Key ni webhook hacia Argos.
func TestAvisoDeCobroAContactos(t *testing.T) {
	e := nuevoEntorno(t, nil)
	c := e.cliente(1, "5512-3344")
	e.exigir(e.consentir(c, "acepta", e.reloj.Add(-time.Hour)), http.StatusCreated, "estado_vigente", "acepta")
	sembrar := func(contacto int64, numero string, avisos bool, servicios any) {
		t.Helper()
		e.sembrarContactoAviso(c, contacto, numero, avisos, servicios)
	}
	base := siguienteCliente.Add(1) * 100
	con, sinPermiso, sinAvisos, otroServicio := base+1, base+2, base+3, base+4
	sembrar(con, "+50255660001", true, nil)
	sembrar(sinPermiso, "+50255660002", true, nil)
	sembrar(sinAvisos, "+50255660003", false, nil)
	sembrar(otroServicio, "+50255660004", true, []int64{c*10 + 1})
	// El contacto sin permiso en su número principal tiene otro número (secundario) con permiso: va a ese.
	if _, err := e.bd.Exec(e.ctx, `INSERT INTO integracion.argos_contacto_telefonos (contacto_id, numero, digitos, whatsapp, principal)
		VALUES ($1, '+50255660005', '50255660005', true, false)`, sinPermiso); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"+50255660001", "+50255660003", "+50255660004", "+50255660005"} {
		cuando := e.reloj.Add(-time.Hour)
		e.exigir(e.consentirNumero(c, n, "acepta", cuando, claveNumero(c, n, cuando)), http.StatusCreated)
	}
	r := e.avisar(fmt.Sprintf("argos:aviso:cobro:%d", c), avisoPorVencer(c, "2026-11-10"))
	e.exigir(r, http.StatusAccepted, "estado", "en_cola")
	padre := r.cuerpo["aviso_id"].(string)
	var destinos []string
	filas, err := e.bd.Query(e.ctx, `SELECT contacto_argos_id, destino, origen, idempotency_key IS NULL FROM mensajeria.avisos WHERE aviso_padre_id = $1`, padre)
	if err != nil {
		t.Fatal(err)
	}
	for filas.Next() {
		var k int64
		var destino *string
		var origen string
		var sinClave bool
		if err := filas.Scan(&k, &destino, &origen, &sinClave); err != nil {
			t.Fatal(err)
		}
		if origen != "iris" || !sinClave || destino == nil {
			t.Fatalf("copia al contacto %d: %s %v %v", k, origen, sinClave, destino)
		}
		destinos = append(destinos, *destino)
	}
	filas.Close()
	sort.Strings(destinos)
	if strings.Join(destinos, ",") != "+50255660001,+50255660005" {
		t.Fatalf("solo el contacto con avisos, del servicio y con permiso por número: %v", destinos)
	}
	// El aviso que no es de cobro no sale a los contactos.
	r = e.avisar(fmt.Sprintf("argos:aviso:otro:%d", c), map[string]any{"empresa_id": 1, "cliente_id": c, "servicio_id": c * 10, "plantilla": "corte_programado",
		"variables": map[string]string{"fecha": "2026-11-10", "hora_inicio": "08:00", "hora_fin": "10:00"}, "canal_preferido": "whatsapp", "prioridad": "normal"})
	if id, ok := r.cuerpo["aviso_id"].(string); ok && e.contar(`SELECT count(*) FROM mensajeria.avisos WHERE aviso_padre_id = $1`, id) != 0 {
		t.Fatal("solo las plantillas de cobro")
	}
}

// M3 de la revisión: un original bloqueado no genera copias, y las copias a un número de contacto
// tienen el mismo límite diario que un cliente aunque vengan de clientes distintos. Bajo 1: una copia
// cuyo contacto ya no está en la caché no sale.
func TestAvisosAContactosLimites(t *testing.T) {
	e := nuevoEntorno(t, nil)
	numero := "+50255669999"
	clientes := []int64{e.cliente(1, "5512-1111"), e.cliente(1, "5512-2222")}
	for i, c := range clientes {
		e.sembrarContactoAviso(c, siguienteCliente.Add(1)*100+int64(i), numero, true, nil)
		cuando := e.reloj.Add(-time.Hour)
		e.exigir(e.consentirNumero(c, numero, "acepta", cuando, claveNumero(c, numero, cuando)), http.StatusCreated)
	}
	copias := func(padre string) []string {
		t.Helper()
		filas, err := e.bd.Query(e.ctx, `SELECT estado || ':' || coalesce(motivo, '') FROM mensajeria.avisos WHERE aviso_padre_id = $1`, padre)
		if err != nil {
			t.Fatal(err)
		}
		defer filas.Close()
		var out []string
		for filas.Next() {
			var x string
			if err := filas.Scan(&x); err != nil {
				t.Fatal(err)
			}
			out = append(out, x)
		}
		return out
	}
	// Sin permiso del cliente el original queda bloqueado: ninguna copia.
	r := e.avisar(fmt.Sprintf("argos:aviso:bloq:%d", clientes[0]), avisoPorVencer(clientes[0], "2026-11-10"))
	e.exigir(r, http.StatusAccepted, "estado", "bloqueado")
	if cs := copias(r.cuerpo["aviso_id"].(string)); len(cs) != 0 {
		t.Fatalf("un original bloqueado no genera copias: %v", cs)
	}
	for _, c := range clientes {
		e.exigir(e.consentir(c, "acepta", e.reloj.Add(-time.Hour)), http.StatusCreated)
	}
	// 3 copias al número (de dos clientes): la cuarta queda bloqueada por el límite diario.
	var ultimo string
	for i, c := range []int64{clientes[0], clientes[0], clientes[1], clientes[1]} {
		r := e.avisar(fmt.Sprintf("argos:aviso:lim:%d:%d", c, i), avisoPorVencer(c, "2026-11-10"))
		e.exigir(r, http.StatusAccepted, "estado", "en_cola")
		ultimo = r.cuerpo["aviso_id"].(string)
		cs := copias(ultimo)
		if len(cs) != 1 || (i < 3 && cs[0] != "en_cola:") || (i == 3 && cs[0] != "bloqueado:limite_diario") {
			t.Fatalf("copia %d: %v", i, cs)
		}
	}
	// La primera copia en cola: su contacto sale de la caché y ya no se envía.
	var copia string
	if err := e.bd.QueryRow(e.ctx, `SELECT a.id::text FROM mensajeria.avisos a JOIN mensajeria.avisos p ON p.id = a.aviso_padre_id
		WHERE p.cliente_id = $1 AND a.estado = 'en_cola' ORDER BY a.created_at LIMIT 1`, clientes[0]).Scan(&copia); err != nil {
		t.Fatal(err)
	}
	if _, err := e.bd.Exec(e.ctx, `UPDATE integracion.argos_contactos SET quitado = true WHERE cliente_id = $1`, clientes[0]); err != nil {
		t.Fatal(err)
	}
	e.enviar(copia)
	if n := e.contar(`SELECT count(*) FROM mensajeria.avisos WHERE id = $1 AND estado = 'fallido'`, copia); n != 1 {
		t.Fatal("la copia de un contacto que ya no está no sale")
	}
}

// Bajo 2 de la verificación: el límite diario de un número cuenta todos los avisos a ese número en la
// empresa (los del cliente que lo tiene de móvil y las copias), no solo las copias.
func TestLimiteDelNumeroCuentaOriginales(t *testing.T) {
	e := nuevoEntorno(t, nil)
	titular := e.cliente(1, "5598-7766")
	numero := "+50255987766"
	otro := e.cliente(1, "5512-0101")
	e.sembrarContactoAviso(otro, siguienteCliente.Add(1)*100, numero, true, nil)
	cuando := e.reloj.Add(-time.Hour)
	e.exigir(e.consentirNumero(otro, numero, "acepta", cuando, claveNumero(otro, numero, cuando)), http.StatusCreated)
	for _, c := range []int64{titular, otro} {
		e.exigir(e.consentir(c, "acepta", e.reloj.Add(-time.Hour)), http.StatusCreated)
	}
	for i := range 3 {
		e.exigir(e.avisar(fmt.Sprintf("argos:aviso:tit:%d:%d", titular, i), avisoPorVencer(titular, "2026-11-10")), http.StatusAccepted, "estado", "en_cola")
	}
	r := e.avisar(fmt.Sprintf("argos:aviso:otro:%d", otro), avisoPorVencer(otro, "2026-11-10"))
	e.exigir(r, http.StatusAccepted, "estado", "en_cola")
	if n := e.contar(`SELECT count(*) FROM mensajeria.avisos WHERE aviso_padre_id = $1 AND estado = 'bloqueado' AND motivo = 'limite_diario'`,
		r.cuerpo["aviso_id"].(string)); n != 1 {
		t.Fatal("la copia al número que ya recibió 3 avisos queda bloqueada por el límite diario")
	}
}

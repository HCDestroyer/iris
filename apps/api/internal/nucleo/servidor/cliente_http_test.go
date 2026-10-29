package servidor_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
)

const agentePrueba = "NavegadorDePrueba/1.0 (iris)"

// El login guarda el User-Agent y la IP de quien entra en la sesión (y en la auditoría).
func TestLoginGuardaUserAgentEIP(t *testing.T) {
	e := nuevoEntorno(t)
	id, correo := e.usuario("lectura")
	c := e.cliente()
	r := c.pedir(http.MethodPost, "/api/v1/sesion", map[string]any{"correo": correo, "contrasena": contrasena}, "User-Agent", agentePrueba)
	if r.status != http.StatusOK {
		t.Fatalf("entrar: %d %s", r.status, r.crudo)
	}
	var agente, ip string
	if err := pruebas.Pool(t).QueryRow(context.Background(), `SELECT user_agent, ip FROM nucleo.sesiones
		WHERE usuario_id = $1 ORDER BY id DESC LIMIT 1`, id).Scan(&agente, &ip); err != nil {
		t.Fatal(err)
	}
	if agente != agentePrueba || ip != "127.0.0.1" {
		t.Fatalf("sesión guardada con user_agent %q e ip %q", agente, ip)
	}
}

// Con MFA, la sesión que abre el código (POST /sesion/mfa) también guarda el User-Agent.
func TestMFAGuardaUserAgent(t *testing.T) {
	e := nuevoEntorno(t)
	_, correo := e.usuario("lectura")
	_, secreto, _ := conMFA(t, e, correo)
	id := idDe(t, correo)
	c := e.cliente()
	r := c.pedir(http.MethodPost, "/api/v1/sesion", map[string]any{"correo": correo, "contrasena": contrasena}, "User-Agent", agentePrueba)
	if r.cuerpo["mfaRequerido"] != true {
		t.Fatalf("login con MFA: %d %s", r.status, r.crudo)
	}
	c.csrf, _ = r.cuerpo["csrfToken"].(string)
	// El paso del código ya usado al activar no vale dos veces: el siguiente.
	codigo, _ := totp.GenerateCode(secreto, time.Now().Add(30*time.Second))
	if r := c.pedir(http.MethodPost, "/api/v1/sesion/mfa", map[string]any{"codigo": codigo}, "User-Agent", agentePrueba+" mfa"); r.status != http.StatusOK {
		t.Fatalf("mfa: %d %s", r.status, r.crudo)
	}
	var agente string
	if err := pruebas.Pool(t).QueryRow(context.Background(), `SELECT user_agent FROM nucleo.sesiones
		WHERE usuario_id = $1 ORDER BY id DESC LIMIT 1`, id).Scan(&agente); err != nil {
		t.Fatal(err)
	}
	if agente != agentePrueba+" mfa" {
		t.Fatalf("sesión tras el MFA con user_agent %q", agente)
	}
}

// El canje del portal guarda el User-Agent y la IP del cliente en portal_sesiones.
func TestPortalGuardaUserAgentEIP(t *testing.T) {
	e := entornoPortal(t, identidad.Config{CookieSegura: false})
	ticket := emitir(t, e.argos(nucleo.PermPortalEmitir), 7701, 1)
	r := e.cliente().pedir(http.MethodPost, "/api/v1/portal/sesion", map[string]any{"ticket": ticket}, "User-Agent", agentePrueba)
	if r.status != http.StatusOK {
		t.Fatalf("canje: %d %s", r.status, r.crudo)
	}
	var agente, ip string
	if err := pruebas.Pool(t).QueryRow(context.Background(), `SELECT user_agent, ip FROM nucleo.portal_sesiones
		WHERE cliente_id = 7701 ORDER BY id DESC LIMIT 1`).Scan(&agente, &ip); err != nil {
		t.Fatal(err)
	}
	if agente != agentePrueba || ip != "127.0.0.1" {
		t.Fatalf("sesión del portal guardada con user_agent %q e ip %q", agente, ip)
	}
}

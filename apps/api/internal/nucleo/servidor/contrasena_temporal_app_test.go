package servidor_test

import (
	"net/http"
	"testing"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
)

// La app de campo (APK) cambia la contraseña temporal con la misma sesión limitada que el panel:
// cookie iris_sesion + X-CSRF-Token del cuerpo de POST /sesion, X-Iris-Canal: campo, X-Iris-App e
// Idempotency-Key. Esa sesión solo puede cambiarla, ver /yo o salir; después opera normal.
func TestContrasenaTemporalDesdeLaApp(t *testing.T) {
	e := nuevoEntorno(t)
	_, correoAdmin := e.usuario("admin")
	admin := e.cliente()
	admin.entrar(correoAdmin)
	correo := pruebas.Correo("tecnico-app")
	if r := admin.pedir(http.MethodPost, "/api/v1/usuarios", map[string]any{"nombre": "Técnico App", "correo": correo, "contrasena": claveTemporal,
		"roles": []map[string]any{{"rol": "tecnico"}}}); r.status != 201 {
		t.Fatalf("crear: %d %s", r.status, r.crudo)
	}
	app := []string{identidad.CabeceraCanal, "campo", "X-Iris-App", "campo/0.18.0 (android)"}

	// Entrar y salir con la sesión limitada (lo que hace «Usar otra cuenta»).
	c := e.cliente()
	if r := c.entrarCon(correo, claveTemporal); r.status != 200 {
		t.Fatalf("entrar: %d %s", r.status, r.crudo)
	}
	if r := c.pedir(http.MethodDelete, "/api/v1/sesion", nil, app...); r.status != http.StatusNoContent {
		t.Fatalf("salir con la sesión limitada: %d %s", r.status, r.crudo)
	}
	c.pedir(http.MethodGet, "/api/v1/yo", nil, app...).problema(t, 401, "no_autenticado")

	c = e.cliente()
	if r := c.entrarCon(correo, claveTemporal); r.status != 200 {
		t.Fatalf("entrar: %d %s", r.status, r.crudo)
	}
	if r := c.pedir(http.MethodGet, "/api/v1/yo", nil, app...); r.status != 200 || r.cuerpo["usuario"].(map[string]any)["cambioContrasenaRequerido"] != true {
		t.Fatalf("/yo: %d %s", r.status, r.crudo)
	}
	// Nada más: ni el trabajo de campo ni configurar el MFA.
	c.pedir(http.MethodGet, "/api/v1/roles", nil, app...).problema(t, 403, "cambio_contrasena_requerido")
	c.pedir(http.MethodPost, "/api/v1/yo/mfa", nil, append(app, "Idempotency-Key", "mfa-app-1")...).problema(t, 403, "cambio_contrasena_requerido")
	// Sin el CSRF no se cambia (la cookie sola no basta).
	sinCSRF := *c
	sinCSRF.csrf = ""
	sinCSRF.pedir(http.MethodPost, identidad.RutaCambiarContrasena, map[string]any{"actual": claveTemporal, "nueva": "la-del-tecnico-larga"}, app...).problema(t, 403, "csrf_invalido")
	// Temporal equivocada y nueva corta: 422 en su campo.
	c.pedir(http.MethodPost, identidad.RutaCambiarContrasena, map[string]any{"actual": "no-es-la-temporal", "nueva": "la-del-tecnico-larga"},
		append(app, "Idempotency-Key", "contrasena-app-1")...).problema(t, 422, "validacion")
	c.pedir(http.MethodPost, identidad.RutaCambiarContrasena, map[string]any{"actual": claveTemporal, "nueva": "corta"},
		append(app, "Idempotency-Key", "contrasena-app-2")...).problema(t, 422, "validacion")

	r := c.pedir(http.MethodPost, identidad.RutaCambiarContrasena, map[string]any{"actual": claveTemporal, "nueva": "la-del-tecnico-larga"},
		append(app, "Idempotency-Key", "contrasena-app-3")...)
	if r.status != 200 || r.cuerpo["cambioContrasenaRequerido"] != nil {
		t.Fatalf("cambiar desde la app: %d %s", r.status, r.crudo)
	}
	// La misma sesión sigue y ya opera (la app entra directo, sin volver a pedir la contraseña).
	if r := c.pedir(http.MethodGet, "/api/v1/yo", nil, app...); r.status != 200 || r.cuerpo["usuario"].(map[string]any)["cambioContrasenaRequerido"] != nil {
		t.Fatalf("/yo tras cambiarla: %d %s", r.status, r.crudo)
	}
	if r := e.cliente().entrarCon(correo, "la-del-tecnico-larga"); r.status != 200 {
		t.Fatalf("entrar con la nueva: %d %s", r.status, r.crudo)
	}
}

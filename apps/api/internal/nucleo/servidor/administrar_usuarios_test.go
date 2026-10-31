package servidor_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
)

const claveTemporal = "temporal-de-prueba-123"

// rolCon crea un rol propio con estos permisos y devuelve su código.
func rolCon(t *testing.T, permisos ...string) string {
	t.Helper()
	pool := pruebas.Pool(t)
	codigo := "prueba_" + pruebas.Unico()
	if _, err := pool.Exec(context.Background(), `WITH r AS (INSERT INTO nucleo.roles (codigo, nombre) VALUES ($1, 'Prueba') RETURNING id)
		INSERT INTO nucleo.roles_permisos (rol_id, permiso) SELECT id, p FROM r, unnest($2::text[]) AS p`, codigo, permisos); err != nil {
		t.Fatal(err)
	}
	return codigo
}

// entrarCon inicia sesión con una contraseña dada (no la de las pruebas).
func (c *cliente) entrarCon(correo, clave string) respuesta {
	c.t.Helper()
	r := c.pedir(http.MethodPost, "/api/v1/sesion", map[string]any{"correo": correo, "contrasena": clave})
	if r.status == http.StatusOK {
		c.csrf, _ = r.cuerpo["csrfToken"].(string)
	}
	return r
}

// exigeCambio comprueba el flujo de la contraseña claveTemporal: entra, solo puede ver /yo (con
// cambioContrasenaRequerido) y cambiarla; tras cambiarla, opera normal.
func exigeCambio(t *testing.T, e *entorno, correo, clave string) {
	t.Helper()
	c := e.cliente()
	if r := c.entrarCon(correo, clave); r.status != 200 {
		t.Fatalf("entrar con la claveTemporal: %d %s", r.status, r.crudo)
	}
	if r := c.pedir(http.MethodGet, "/api/v1/yo", nil); r.status != 200 || r.cuerpo["usuario"].(map[string]any)["cambioContrasenaRequerido"] != true {
		t.Fatalf("/yo sin cambioContrasenaRequerido: %d %s", r.status, r.crudo)
	}
	c.pedir(http.MethodGet, "/api/v1/roles", nil).problema(t, 403, "cambio_contrasena_requerido")
	if r := cambiar(c, clave, "la-mia-propia-larga-1"); r.status != 200 {
		t.Fatalf("cambiar: %d %s", r.status, r.crudo)
	}
	if r := c.pedir(http.MethodGet, "/api/v1/roles", nil); r.status != 200 {
		t.Fatalf("tras cambiarla: %d %s", r.status, r.crudo)
	}
}

// Una persona creada por otra (panel o API) entra con una contraseña claveTemporal que debe cambiar.
func TestCrearUsuarioExigeCambiarLaContrasena(t *testing.T) {
	e := nuevoEntorno(t)
	_, correoAdmin := e.usuario("admin")
	admin := e.cliente()
	admin.entrar(correoAdmin)
	correo := pruebas.Correo("nueva")
	r := admin.pedir(http.MethodPost, "/api/v1/usuarios", map[string]any{"nombre": "Nueva", "correo": correo, "contrasena": claveTemporal, "roles": []map[string]any{{"rol": "lectura"}}})
	if r.status != 201 || r.cuerpo["cambioContrasenaRequerido"] != true {
		t.Fatalf("crear: %d %s", r.status, r.crudo)
	}
	exigeCambio(t, e, correo, claveTemporal)

	// La consola (create-admin, sistema) no la marca: la elige el propio operador.
	_, correoSistema := e.usuario("lectura")
	if r := e.cliente().entrar(correoSistema); r.status != 200 || r.cuerpo["usuario"].(map[string]any)["cambioContrasenaRequerido"] != nil {
		t.Fatalf("creado por el sistema: %d %s", r.status, r.crudo)
	}
}

// Un administrador que pone la contraseña de otro la deja claveTemporal; la propia, no.
func TestEditarContrasenaDeOtroEsTemporal(t *testing.T) {
	e := nuevoEntorno(t)
	_, correoAdmin := e.usuario("admin")
	admin := e.cliente()
	admin.entrar(correoAdmin)
	id, correo := e.usuario("lectura")
	r := admin.pedir(http.MethodPost, fmt.Sprintf("/api/v1/usuarios/%d/editar", id), map[string]any{"version": 1, "contrasena": claveTemporal})
	if r.status != 200 || r.cuerpo["cambioContrasenaRequerido"] != true {
		t.Fatalf("editar: %d %s", r.status, r.crudo)
	}
	exigeCambio(t, e, correo, claveTemporal)

	// El propio usuario que cambia la suya por editar no queda con una claveTemporal.
	c := e.cliente()
	c.entrarCon(correo, "la-mia-propia-larga-1")
	yo := c.pedir(http.MethodGet, "/api/v1/yo", nil).cuerpo["usuario"].(map[string]any)
	r = c.pedir(http.MethodPost, fmt.Sprintf("/api/v1/usuarios/%d/editar", id), map[string]any{"version": yo["version"], "contrasena": "otra-mia-larga-123"})
	if r.status == 200 && r.cuerpo["cambioContrasenaRequerido"] == true {
		t.Fatalf("la propia quedó claveTemporal: %s", r.crudo)
	}
}

// Por HTTP, una integración solo la crea quien conecta integraciones con todas las empresas, y
// solo con su rol de contrato.
func TestCrearIntegracionPorHTTP(t *testing.T) {
	e := nuevoEntorno(t)
	_, correoSup := e.usuario(rolCon(t, "usuarios.crear", "usuarios.ver"))
	sup := e.cliente()
	sup.entrar(correoSup)
	cuerpo := map[string]any{"nombre": "Bot", "correo": pruebas.Correo("bot"), "tipo": "integracion", "roles": []map[string]any{{"rol": "integracion_argos"}}}
	if r := sup.pedir(http.MethodPost, "/api/v1/usuarios", cuerpo); r.status != 403 || r.cuerpo["type"] != "urn:iris:problema:integracion_no_permitida" {
		t.Fatalf("sin integraciones.conectar: %d %s", r.status, r.crudo)
	}

	_, correoAdmin := e.usuario("admin")
	admin := e.cliente()
	admin.entrar(correoAdmin)
	r := admin.pedir(http.MethodPost, "/api/v1/usuarios", map[string]any{"nombre": "Bot", "correo": pruebas.Correo("bot"), "tipo": "integracion", "roles": []map[string]any{{"rol": "lectura"}}})
	r.problema(t, 422, "validacion")
	if errs, _ := r.cuerpo["errors"].([]any); len(errs) != 1 || errs[0].(map[string]any)["field"] != "roles[0].rol" {
		t.Fatalf("error de campo: %s", r.crudo)
	}

	// Con su rol de contrato (que da permisos sensibles: confirmar la contraseña), sí.
	if r := admin.pedir(http.MethodPost, "/api/v1/sesion/reautenticar", map[string]any{"contrasena": contrasena}); r.status != 200 {
		t.Fatalf("reautenticar: %d %s", r.status, r.crudo)
	}
	if r := admin.pedir(http.MethodPost, "/api/v1/usuarios", cuerpo); r.status != 201 || r.cuerpo["cambioContrasenaRequerido"] != nil {
		t.Fatalf("crear integración: %d %s", r.status, r.crudo)
	}
}

// Cambiar el correo propio exige confirmar la contraseña; el de otro cierra sus sesiones;
// renombrar a otro exige superarlo.
func TestEditarCorreoYNombre(t *testing.T) {
	e := nuevoEntorno(t)
	pool := pruebas.Pool(t)
	idAdmin, correoAdmin := e.usuario("admin")
	admin := e.cliente()
	admin.entrar(correoAdmin)

	nuevo := pruebas.Correo("propio")
	admin.pedir(http.MethodPost, fmt.Sprintf("/api/v1/usuarios/%d/editar", idAdmin), map[string]any{"version": 1, "correo": nuevo}).problema(t, 403, "reautenticacion_requerida")
	admin.pedir(http.MethodPost, "/api/v1/sesion/reautenticar", map[string]any{"contrasena": contrasena})
	if r := admin.pedir(http.MethodPost, fmt.Sprintf("/api/v1/usuarios/%d/editar", idAdmin), map[string]any{"version": 1, "correo": nuevo}); r.status != 200 {
		t.Fatalf("correo propio tras reautenticar: %d %s", r.status, r.crudo)
	}

	id, correo := e.usuario("lectura")
	victima := e.cliente()
	victima.entrar(correo)
	if r := admin.pedir(http.MethodPost, fmt.Sprintf("/api/v1/usuarios/%d/editar", id), map[string]any{"version": 1, "correo": pruebas.Correo("otro")}); r.status != 200 {
		t.Fatalf("correo de otro: %d %s", r.status, r.crudo)
	}
	victima.pedir(http.MethodGet, "/api/v1/yo", nil).problema(t, 401, "no_autenticado")
	var abiertas int
	_ = pool.QueryRow(context.Background(), `SELECT count(*) FROM nucleo.sesiones WHERE usuario_id = $1 AND revocada_en IS NULL`, id).Scan(&abiertas)
	if abiertas != 0 {
		t.Fatalf("sesiones abiertas tras cambiar su correo: %d", abiertas)
	}

	// Quien edita usuarios pero no tiene los permisos del admin no lo renombra.
	idGlobal, _ := e.usuario("admin")
	_, correoEditor := e.usuario(rolCon(t, "usuarios.editar", "usuarios.ver"))
	ed := e.cliente()
	ed.entrar(correoEditor)
	ed.pedir(http.MethodPost, fmt.Sprintf("/api/v1/usuarios/%d/editar", idGlobal), map[string]any{"version": 1, "nombre": "Otro nombre"}).problema(t, 403, "prohibido")
}

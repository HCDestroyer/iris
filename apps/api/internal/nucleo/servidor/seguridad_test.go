package servidor_test

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
)

// Regresión: un token con solo tokens.crear no puede emitir un token con más permisos (ni uno
// sin límite, que tendría todos los del usuario).
func TestTokenDesdeTokenNoEscala(t *testing.T) {
	e := nuevoEntorno(t)
	idAdmin, correoAdmin := e.usuario("admin")
	admin := e.cliente()
	admin.entrar(correoAdmin)

	// Un token limitado a usuarios.editar no cambia la contraseña de su propio usuario (con eso
	// entraría con la sesión completa).
	r := admin.pedir(http.MethodPost, "/api/v1/tokens", map[string]any{"nombre": "editar", "permisos": []string{"usuarios.editar"}})
	editor := e.cliente()
	editor.bearer = r.cuerpo["valor"].(string)
	editor.pedir(http.MethodPost, fmt.Sprintf("/api/v1/usuarios/%d/editar", idAdmin), map[string]any{"version": 1, "contrasena": "tomada-por-token-123"}).problema(t, 403, "prohibido")

	r = admin.pedir(http.MethodPost, "/api/v1/tokens", map[string]any{"nombre": "solo-crear", "permisos": []string{"tokens.crear"}})
	if r.status != 201 {
		t.Fatalf("crear token limitado: %d %s", r.status, r.crudo)
	}
	lim := e.cliente()
	lim.bearer = r.cuerpo["valor"].(string)

	lim.pedir(http.MethodPost, "/api/v1/tokens", map[string]any{"nombre": "sin-limite"}).problema(t, 403, "prohibido")
	lim.pedir(http.MethodPost, "/api/v1/tokens", map[string]any{"nombre": "mas", "permisos": []string{"usuarios.ver"}}).problema(t, 403, "prohibido")
	lim.pedir(http.MethodPost, "/api/v1/tokens", map[string]any{"nombre": "mas", "permisos": []string{"tokens.crear", "usuarios.editar"}}).problema(t, 403, "prohibido")

	// Un subconjunto de lo que tiene el token sí.
	r = lim.pedir(http.MethodPost, "/api/v1/tokens", map[string]any{"nombre": "igual", "permisos": []string{"tokens.crear"}})
	if r.status != 201 {
		t.Fatalf("token hijo con el mismo límite: %d %s", r.status, r.crudo)
	}
	hijo := e.cliente()
	hijo.bearer = r.cuerpo["valor"].(string)
	if yo := hijo.pedir(http.MethodGet, "/api/v1/yo", nil); fmt.Sprint(yo.cuerpo["permisos"]) != "[tokens.crear]" {
		t.Fatalf("permisos del hijo: %s", yo.crudo)
	}

	// Un token sin límite puede crear otro sin límite (mismos permisos efectivos).
	r = admin.pedir(http.MethodPost, "/api/v1/tokens", map[string]any{"nombre": "completo"})
	if r.status != 201 {
		t.Fatalf("token sin límite: %d %s", r.status, r.crudo)
	}
	completo := e.cliente()
	completo.bearer = r.cuerpo["valor"].(string)
	if r := completo.pedir(http.MethodPost, "/api/v1/tokens", map[string]any{"nombre": "otro-completo"}); r.status != 201 {
		t.Fatalf("token sin límite desde token sin límite: %d %s", r.status, r.crudo)
	}
}

// Regresión: un admin con alcance de zona no toma la cuenta de un admin global.
func TestAdminZonalNoTomaAdminGlobal(t *testing.T) {
	e := nuevoEntorno(t)
	idGlobal, correoGlobal := e.usuario("admin")
	zona := int64(5)
	correoZ := pruebas.Correo("zonal")
	if _, err := e.mod.CrearUsuario.Ejecutar(pruebas.ContextoSistema(pruebas.Pool(t)), identidad.EntradaCrearUsuario{
		Nombre: "Zonal", Correo: correoZ, Contrasena: contrasena, Roles: []identidad.AsignacionRol{{Rol: "admin", ZonaID: &zona}},
	}); err != nil {
		t.Fatal(err)
	}
	z := e.cliente()
	z.entrar(correoZ)
	ruta := fmt.Sprintf("/api/v1/usuarios/%d/editar", idGlobal)

	nueva := "tomada-por-zonal-123"
	z.pedir(http.MethodPost, ruta, map[string]any{"version": 1, "contrasena": nueva}).problema(t, 403, "prohibido")
	e.cliente().pedir(http.MethodPost, "/api/v1/sesion", map[string]any{"correo": correoGlobal, "contrasena": nueva}).problema(t, 401, "credenciales_invalidas")
	z.pedir(http.MethodPost, ruta, map[string]any{"version": 1, "correo": pruebas.Correo("robado")}).problema(t, 403, "prohibido")
	z.pedir(http.MethodPost, ruta, map[string]any{"version": 1, "roles": []any{}}).problema(t, 403, "prohibido")
	z.pedir(http.MethodPost, fmt.Sprintf("/api/v1/usuarios/%d/desactivar", idGlobal), map[string]any{"version": 1, "motivo": "x"}).problema(t, 403, "prohibido")
	if r := e.cliente().entrar(correoGlobal); r.status != 200 {
		t.Fatalf("el admin global sigue entrando: %d %s", r.status, r.crudo)
	}

	// El admin global sí puede (tiene sin límite todo lo del objetivo).
	idLectura, _ := e.usuario("lectura")
	g := e.cliente()
	g.entrar(correoGlobal)
	if r := g.pedir(http.MethodPost, fmt.Sprintf("/api/v1/usuarios/%d/editar", idLectura), map[string]any{"version": 1, "contrasena": nueva, "roles": []any{}}); r.status != 200 {
		t.Fatalf("admin global edita a lectura: %d %s", r.status, r.crudo)
	}
}

// Regresión: los tokens de otro usuario solo se emiten para usuarios de integración, y quien
// los emite debe tener sin límite todo lo que el token podrá hacer.
func TestTokenParaOtroSoloIntegracion(t *testing.T) {
	e := nuevoEntorno(t)
	_, correoAdmin := e.usuario("admin")
	idPersona, _ := e.usuario("lectura")
	admin := e.cliente()
	admin.entrar(correoAdmin)

	admin.pedir(http.MethodPost, "/api/v1/tokens", map[string]any{"nombre": "suplantar", "usuarioId": idPersona}).problema(t, 422, "token_para_persona")

	r := admin.pedir(http.MethodPost, "/api/v1/usuarios", map[string]any{"nombre": "Horus", "correo": pruebas.Correo("horus"), "tipo": "integracion", "roles": []map[string]any{{"rol": "admin"}}})
	if r.status != 201 {
		t.Fatalf("crear integración: %d %s", r.status, r.crudo)
	}
	integ := r.cuerpo["id"]

	// Un admin de zona no emite tokens de una integración con permisos globales.
	zona := int64(7)
	correoZ := pruebas.Correo("zonal")
	if _, err := e.mod.CrearUsuario.Ejecutar(pruebas.ContextoSistema(pruebas.Pool(t)), identidad.EntradaCrearUsuario{
		Nombre: "Zonal", Correo: correoZ, Contrasena: contrasena, Roles: []identidad.AsignacionRol{{Rol: "admin", ZonaID: &zona}},
	}); err != nil {
		t.Fatal(err)
	}
	z := e.cliente()
	z.entrar(correoZ)
	z.pedir(http.MethodPost, "/api/v1/tokens", map[string]any{"nombre": "horus", "usuarioId": integ}).problema(t, 403, "prohibido")

	if r := admin.pedir(http.MethodPost, "/api/v1/tokens", map[string]any{"nombre": "horus", "usuarioId": integ}); r.status != 201 {
		t.Fatalf("token de integración por el admin global: %d %s", r.status, r.crudo)
	}
}

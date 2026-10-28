package identidad_test

import (
	"context"
	"errors"
	"net/http"
	"os"
	"slices"
	"strings"
	"testing"

	auditoriabd "github.com/HCDestroyer/iris/apps/api/internal/auditoria/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
)

func TestMain(m *testing.M) { os.Exit(pruebas.Correr(m)) }

var mod = identidad.Nuevo(identidad.Config{})

func TestContrasenas(t *testing.T) {
	h, err := identidad.HashContrasena("una contraseña larga")
	if err != nil || !strings.HasPrefix(h, "$argon2id$v=19$m=19456,t=2,p=1$") {
		t.Fatalf("hash: %s %v", h, err)
	}
	if ok, _ := identidad.VerificarContrasena("una contraseña larga", h); !ok {
		t.Fatal("la contraseña correcta no verificó")
	}
	if ok, _ := identidad.VerificarContrasena("otra", h); ok {
		t.Fatal("una contraseña incorrecta verificó")
	}
	if _, err := identidad.VerificarContrasena("x", "$bcrypt$malo"); err == nil {
		t.Fatal("un hash que no es argon2id debe fallar")
	}
}

func TestFormatoToken(t *testing.T) {
	vistos := map[string]bool{}
	for range 50 {
		tk, err := identidad.NuevoTokenAPI()
		if err != nil {
			t.Fatal(err)
		}
		cuerpo := strings.TrimPrefix(tk, identidad.PrefijoToken)
		if len(cuerpo) != 43 || strings.Trim(cuerpo, "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz") != "" || vistos[tk] {
			t.Fatalf("token inválido o repetido: %s", tk)
		}
		vistos[tk] = true
	}
}

func TestSemillaIdempotenteYRolesBase(t *testing.T) {
	pool := pruebas.Pool(t)
	ctx := pruebas.ContextoSistema(pool)
	for range 2 {
		if err := identidad.SincronizarCatalogo(context.Background(), pool); err != nil {
			t.Fatal(err)
		}
	}
	roles, err := mod.ListarRoles.Ejecutar(ctx, nucleo.ParamsPagina{})
	if err != nil {
		t.Fatal(err)
	}
	porCodigo := map[string]identidad.Rol{}
	for _, r := range roles.Data {
		porCodigo[r.Codigo] = r
	}
	for _, base := range []string{"admin", "supervisor", "despacho", "tecnico", "soporte", "ventas", "lectura"} {
		r, ok := porCodigo[base]
		if !ok || !r.Sistema {
			t.Fatalf("falta el rol base %s", base)
		}
	}
	if len(porCodigo["admin"].Permisos) != len(nucleo.Catalogo) {
		t.Fatalf("admin tiene todos los permisos: %v", porCodigo["admin"].Permisos)
	}
	if !slices.Equal(porCodigo["lectura"].Permisos, []nucleo.Permiso{nucleo.PermPermisosVer, nucleo.PermRolesVer, nucleo.PermUsuariosVer}) {
		t.Fatalf("lectura: %v", porCodigo["lectura"].Permisos)
	}
}

func TestNadieAsignaMasDeLoQueTiene(t *testing.T) {
	pool := pruebas.Pool(t)
	// Un actor con usuarios.crear pero sin el resto no puede crear un admin.
	ctx := pruebas.ContextoUsuario(pool, 1, nucleo.PermUsuariosCrear)
	_, err := mod.CrearUsuario.Ejecutar(ctx, identidad.EntradaCrearUsuario{
		Nombre: "Escalada", Correo: pruebas.Correo("esc"), Contrasena: "contrasena-larga-1",
		Roles: []identidad.AsignacionRol{{Rol: "admin"}},
	})
	var p *nucleo.Problema
	if !errors.As(err, &p) || p.Status != http.StatusForbidden {
		t.Fatalf("esperaba 403 por escalada, llegó %v", err)
	}
	// Con alcance limitado tampoco: hace falta el permiso sin límite.
	limitado := contexto.ConActor(ctx, contexto.ActorDe(ctx), contexto.NuevosPermisos(map[string][]contexto.Alcance{
		"usuarios.crear": {{}}, "usuarios.ver": {{ZonaID: 5}},
	}))
	_, err = mod.CrearUsuario.Ejecutar(limitado, identidad.EntradaCrearUsuario{
		Nombre: "Soporte", Correo: pruebas.Correo("sop"), Contrasena: "contrasena-larga-1",
		Roles: []identidad.AsignacionRol{{Rol: "soporte"}},
	})
	if !errors.As(err, &p) || p.Status != http.StatusForbidden {
		t.Fatalf("esperaba 403 con alcance limitado, llegó %v", err)
	}
	// Con usuarios.ver global sí puede dar soporte.
	ok := pruebas.ContextoUsuario(pool, 1, nucleo.PermUsuariosCrear, nucleo.PermUsuariosVer)
	if _, err := mod.CrearUsuario.Ejecutar(ok, identidad.EntradaCrearUsuario{
		Nombre: "Soporte", Correo: pruebas.Correo("sop"), Contrasena: "contrasena-larga-1",
		Roles: []identidad.AsignacionRol{{Rol: "soporte"}},
	}); err != nil {
		t.Fatal(err)
	}
	// Rol inexistente → 422 con el campo.
	_, err = mod.CrearUsuario.Ejecutar(ok, identidad.EntradaCrearUsuario{
		Nombre: "X", Correo: pruebas.Correo("x"), Contrasena: "contrasena-larga-1",
		Roles: []identidad.AsignacionRol{{Rol: "inventado"}},
	})
	if !errors.As(err, &p) || p.Status != 422 || len(p.Errors) != 1 || p.Errors[0].Field != "roles[0].rol" {
		t.Fatalf("rol inexistente: %v", err)
	}
}

func TestCambioDeContrasenaSeAuditaEnmascarado(t *testing.T) {
	pool := pruebas.Pool(t)
	sis := pruebas.ContextoSistema(pool)
	u, err := mod.CrearUsuario.Ejecutar(sis, identidad.EntradaCrearUsuario{
		Nombre: "Cambia", Correo: pruebas.Correo("cambia"), Contrasena: "contrasena-larga-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := pruebas.ContextoSistema(pool)
	nueva := "otra-contrasena-larga"
	if _, err := mod.EditarUsuario.Ejecutar(ctx, identidad.EntradaEditarUsuario{ID: u.ID, Version: u.Version, Contrasena: &nueva}); err != nil {
		t.Fatal(err)
	}
	acciones, _ := auditoriabd.New(pool).AccionesPorRequest(context.Background(), contexto.RequestIDDe(ctx))
	if len(acciones) != 1 || strings.Contains(string(acciones[0].Entrada), nueva) {
		t.Fatalf("la contraseña nueva no queda en la entrada: %s", acciones[0].Entrada)
	}
	cambios, _ := auditoriabd.New(pool).CambiosDeAccion(context.Background(), acciones[0].ID)
	if len(cambios) != 1 || cambios[0].Campo != "contrasena_hash" || string(cambios[0].Antes) != `"***"` || string(cambios[0].Despues) != `"***"` {
		t.Fatalf("cambios: %+v", cambios)
	}
}

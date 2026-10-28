package auditoria_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	auditoriabd "github.com/HCDestroyer/iris/apps/api/internal/auditoria/bd"
	integracionbd "github.com/HCDestroyer/iris/apps/api/internal/integracion/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
)

func TestMain(m *testing.M) { os.Exit(pruebas.Correr(m)) }

type entradaPrueba struct {
	Nombre     string `json:"nombre"`
	Contrasena string `json:"contrasena"`
	Codigo     string `json:"codigo" auditoria:"sensible"`
	TokenID    int64  `json:"tokenId"`
	Fallar     bool   `json:"fallar"`
}

type entidad struct {
	Nombre    string `json:"nombre"`
	Clave     string `json:"clave" auditoria:"sensible"`
	TokenHash string `json:"token_hash"`
	Version   int    `json:"version"`
}

var errPrueba = nucleo.ReglaNegocio("prueba", "falló a propósito")

func casoPrueba(auditarExito func(string) bool) *auditoria.Caso[entradaPrueba, string] {
	return auditoria.Nuevo(auditoria.Caso[entradaPrueba, string]{
		Accion:       "usuarios.crear",
		AuditarExito: auditarExito,
		Logica: func(ctx context.Context, t *auditoria.Tx, in entradaPrueba) (string, error) {
			t.RegistrarCambios("prueba", 42,
				entidad{Nombre: "antes", Clave: "a", TokenHash: "h1", Version: 1},
				entidad{Nombre: in.Nombre, Clave: "b", TokenHash: "h2", Version: 2})
			if err := t.Emitir("prueba.hecha", map[string]any{"nombre": in.Nombre}); err != nil {
				return "", err
			}
			if in.Fallar {
				return "", errPrueba
			}
			return "ok", nil
		},
	})
}

func acciones(t *testing.T, ctx context.Context) []auditoriabd.AuditoriaAccione {
	t.Helper()
	filas, err := auditoriabd.New(pruebas.Pool(t)).AccionesPorRequest(context.Background(), contexto.RequestIDDe(ctx))
	if err != nil {
		t.Fatal(err)
	}
	return filas
}

func eventos(t *testing.T, ctx context.Context) []integracionbd.IntegracionOutbox {
	t.Helper()
	filas, err := integracionbd.New(pruebas.Pool(t)).EventosPorRequest(context.Background(), contexto.RequestIDDe(ctx))
	if err != nil {
		t.Fatal(err)
	}
	return filas
}

func TestExitoAuditaCambiosYEventoEnLaMismaTransaccion(t *testing.T) {
	pool := pruebas.Pool(t)
	ctx := pruebas.ContextoUsuario(pool, 7, nucleo.PermUsuariosCrear)
	salida, err := casoPrueba(nil).Ejecutar(ctx, entradaPrueba{Nombre: "despues", Contrasena: "secreta", Codigo: "123456", TokenID: 9})
	if err != nil || salida != "ok" {
		t.Fatalf("salida=%q err=%v", salida, err)
	}

	filas := acciones(t, ctx)
	if len(filas) != 1 {
		t.Fatalf("esperaba 1 acción, hay %d", len(filas))
	}
	a := filas[0]
	if a.Resultado != "ok" || a.Accion != "usuarios.crear" || a.ActorTipo != "usuario" || *a.ActorID != 7 || a.Canal != "panel" {
		t.Fatalf("acción inesperada: %+v", a)
	}
	if *a.Entidad != "prueba" || *a.EntidadID != "42" {
		t.Fatalf("entidad: %v %v", *a.Entidad, *a.EntidadID)
	}
	var entrada map[string]any
	_ = json.Unmarshal(a.Entrada, &entrada)
	if entrada["contrasena"] != auditoria.Mascara || entrada["codigo"] != auditoria.Mascara || entrada["nombre"] != "despues" {
		t.Fatalf("entrada sin enmascarar: %v", entrada)
	}
	if entrada["tokenId"] != float64(9) {
		t.Fatalf("los ids no se enmascaran: %v", entrada["tokenId"])
	}

	cambios, err := auditoriabd.New(pool).CambiosDeAccion(context.Background(), a.ID)
	if err != nil {
		t.Fatal(err)
	}
	porCampo := map[string][2]string{}
	for _, c := range cambios {
		porCampo[c.Campo] = [2]string{string(c.Antes), string(c.Despues)}
	}
	if len(cambios) != 3 {
		t.Fatalf("esperaba 3 cambios (nombre, clave, token_hash; version se ignora): %v", porCampo)
	}
	if porCampo["nombre"] != [2]string{`"antes"`, `"despues"`} {
		t.Fatalf("nombre: %v", porCampo["nombre"])
	}
	for _, sensible := range []string{"clave", "token_hash"} {
		if porCampo[sensible] != [2]string{`"***"`, `"***"`} {
			t.Fatalf("%s sin enmascarar: %v", sensible, porCampo[sensible])
		}
	}

	ev := eventos(t, ctx)
	if len(ev) != 1 || ev[0].Evento != "prueba.hecha" || ev[0].PublicadoEn != nil {
		t.Fatalf("evento: %+v", ev)
	}
}

func TestErrorRevierteEventoYSeAuditaAparte(t *testing.T) {
	pool := pruebas.Pool(t)
	ctx := pruebas.ContextoUsuario(pool, 8, nucleo.PermUsuariosCrear)
	_, err := casoPrueba(nil).Ejecutar(ctx, entradaPrueba{Nombre: "x", Contrasena: "secreta", Fallar: true})
	if !errors.Is(err, errPrueba) {
		t.Fatalf("esperaba el error de la lógica, llegó %v", err)
	}
	if ev := eventos(t, ctx); len(ev) != 0 {
		t.Fatalf("rollback = sin evento; hay %d", len(ev))
	}
	filas := acciones(t, ctx)
	if len(filas) != 1 || filas[0].Resultado != "error" {
		t.Fatalf("esperaba 1 acción con error: %+v", filas)
	}
	var detalle map[string]any
	_ = json.Unmarshal(filas[0].Error, &detalle)
	if detalle["type"] != "urn:iris:problema:prueba" || detalle["status"] != float64(422) {
		t.Fatalf("error auditado: %v", detalle)
	}
	var entrada map[string]any
	_ = json.Unmarshal(filas[0].Entrada, &entrada)
	if entrada["contrasena"] != auditoria.Mascara {
		t.Fatalf("la entrada del error también se enmascara: %v", entrada)
	}
	cambios, _ := auditoriabd.New(pool).CambiosDeAccion(context.Background(), filas[0].ID)
	if len(cambios) != 0 {
		t.Fatalf("los cambios de una acción fallida no quedan: %d", len(cambios))
	}
}

func TestPermisos(t *testing.T) {
	pool := pruebas.Pool(t)

	sinPermiso := pruebas.ContextoUsuario(pool, 9, nucleo.PermUsuariosVer)
	_, err := casoPrueba(nil).Ejecutar(sinPermiso, entradaPrueba{Nombre: "x"})
	var p *nucleo.Problema
	if !errors.As(err, &p) || p.Status != http.StatusForbidden {
		t.Fatalf("esperaba 403, llegó %v", err)
	}
	if f := acciones(t, sinPermiso); len(f) != 1 || f[0].Resultado != "error" {
		t.Fatalf("el 403 se audita: %+v", f)
	}
	if ev := eventos(t, sinPermiso); len(ev) != 0 {
		t.Fatal("sin permiso no corre la lógica")
	}

	anonimo := nucleo.ConBD(contexto.ConRequestID(context.Background(), contexto.NuevoRequestID()), pool)
	_, err = casoPrueba(nil).Ejecutar(anonimo, entradaPrueba{Nombre: "x"})
	if !errors.As(err, &p) || p.Status != http.StatusUnauthorized {
		t.Fatalf("esperaba 401, llegó %v", err)
	}
	if f := acciones(t, anonimo); len(f) != 1 || f[0].ActorTipo != "anonimo" {
		t.Fatalf("el 401 se audita como anónimo: %+v", f)
	}

	sistema := pruebas.ContextoSistema(pool)
	if _, err := casoPrueba(nil).Ejecutar(sistema, entradaPrueba{Nombre: "x"}); err != nil {
		t.Fatalf("el sistema tiene todos los permisos: %v", err)
	}
	if f := acciones(t, sistema); len(f) != 1 || f[0].ActorTipo != "sistema" || f[0].Canal != "consola" {
		t.Fatalf("acción del sistema: %+v", f)
	}
}

func TestAuditarExitoFalso(t *testing.T) {
	pool := pruebas.Pool(t)
	lectura := auditoria.Nuevo(auditoria.Caso[entradaPrueba, string]{
		Accion:       "usuarios.ver",
		AuditarExito: func(string) bool { return false },
		Logica: func(_ context.Context, _ *auditoria.Tx, in entradaPrueba) (string, error) {
			if in.Fallar {
				return "", errPrueba
			}
			return "ok", nil
		},
	})
	ctx := pruebas.ContextoUsuario(pool, 10, nucleo.PermUsuariosVer)
	if _, err := lectura.Ejecutar(ctx, entradaPrueba{}); err != nil {
		t.Fatal(err)
	}
	if f := acciones(t, ctx); len(f) != 0 {
		t.Fatalf("AuditarExito=false no deja fila al terminar bien: %d", len(f))
	}
	_, _ = lectura.Ejecutar(ctx, entradaPrueba{Fallar: true})
	if f := acciones(t, ctx); len(f) != 1 || f[0].Resultado != "error" {
		t.Fatalf("los errores se auditan siempre: %+v", f)
	}

	// Con cambios registrados la fila se escribe aunque AuditarExito diga que no.
	ctx2 := pruebas.ContextoUsuario(pool, 10, nucleo.PermUsuariosCrear)
	if _, err := casoPrueba(func(string) bool { return false }).Ejecutar(ctx2, entradaPrueba{Nombre: "y"}); err != nil {
		t.Fatal(err)
	}
	if f := acciones(t, ctx2); len(f) != 1 {
		t.Fatalf("con cambios se audita: %d", len(f))
	}
}

func TestCasoAnidadoUsaSavepoint(t *testing.T) {
	pool := pruebas.Pool(t)
	interno := casoPrueba(nil)
	ganchos := 0
	externo := auditoria.Nuevo(auditoria.Caso[string, string]{
		Accion: "usuarios.editar",
		Logica: func(ctx context.Context, t *auditoria.Tx, _ string) (string, error) {
			if err := t.Emitir("externo.hecho", map[string]any{}); err != nil {
				return "", err
			}
			t.AlConfirmar(func(context.Context) { ganchos++ })
			// El interno falla: su savepoint se revierte y el externo sigue.
			if _, err := interno.Ejecutar(ctx, entradaPrueba{Nombre: "interno", Fallar: true}); !errors.Is(err, errPrueba) {
				return "", errors.New("esperaba el error del interno")
			}
			// Este interno termina bien: su evento queda con el COMMIT del externo.
			if _, err := interno.Ejecutar(ctx, entradaPrueba{Nombre: "interno-ok"}); err != nil {
				return "", err
			}
			return "ok", nil
		},
	})
	ctx := pruebas.ContextoUsuario(pool, 11, nucleo.PermUsuariosEditar, nucleo.PermUsuariosCrear)
	if _, err := externo.Ejecutar(ctx, ""); err != nil {
		t.Fatal(err)
	}
	if ganchos != 1 {
		t.Fatalf("AlConfirmar corre una vez tras el COMMIT: %d", ganchos)
	}
	ev := eventos(t, ctx)
	nombres := []string{}
	for _, e := range ev {
		nombres = append(nombres, e.Evento)
	}
	if len(ev) != 2 || ev[0].Evento != "externo.hecho" || ev[1].Evento != "prueba.hecha" {
		t.Fatalf("eventos: %v", nombres)
	}
	res := map[string]int{}
	for _, a := range acciones(t, ctx) {
		res[a.Accion+":"+a.Resultado]++
	}
	if res["usuarios.editar:ok"] != 1 || res["usuarios.crear:error"] != 1 || res["usuarios.crear:ok"] != 1 {
		t.Fatalf("acciones: %v", res)
	}
}

func TestAuditoriaEsDeSoloInsercion(t *testing.T) {
	pool := pruebas.Pool(t)
	ctx := context.Background()
	for _, sql := range []string{
		"UPDATE auditoria.acciones SET accion = 'x'",
		"DELETE FROM auditoria.acciones",
		"TRUNCATE auditoria.acciones CASCADE",
		"UPDATE auditoria.cambios SET campo = 'x'",
		"DELETE FROM auditoria.cambios",
		"TRUNCATE auditoria.cambios",
	} {
		if _, err := pool.Exec(ctx, sql); err == nil {
			t.Fatalf("%s debió fallar", sql)
		}
	}
}

func TestNuevoValidaElPermiso(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("un permiso fuera del catálogo debe hacer pánico al armar el caso")
		}
	}()
	auditoria.Nuevo(auditoria.Caso[int, int]{
		Accion: "inventado.hacer",
		Logica: func(context.Context, *auditoria.Tx, int) (int, error) { return 0, nil },
	})
}

func TestMascara(t *testing.T) {
	casos := map[string]bool{
		"contrasena": true, "contrasenaHash": true, "token": true, "csrf_token": true, "totp_secreto": true,
		"firma": true, "token_id": false, "tokenId": false, "nombre": false, "correo": false, "prefijo": false,
	}
	for campo, esperado := range casos {
		if auditoria.EsCampoSensible(campo) != esperado {
			t.Errorf("EsCampoSensible(%q) = %v", campo, !esperado)
		}
	}
	if auditoria.SnakeCase("contrasenaHash") != "contrasena_hash" || auditoria.SnakeCase("usuarioID") != "usuario_id" {
		t.Errorf("SnakeCase: %s %s", auditoria.SnakeCase("contrasenaHash"), auditoria.SnakeCase("usuarioID"))
	}
}

// Revisión de la empresa activa, punto 5: una acción operativa de un usuario sin la empresa
// resuelta falla cerrado (500 sin detalle) en vez de correr con EmpresaDe = 0; lo
// administrativo (SinEmpresa) y el actor sistema corren igual.
func TestOperativoSinEmpresaResueltaFallaCerrado(t *testing.T) {
	pool := pruebas.Pool(t)
	corrio := false
	operativo := auditoria.Nuevo(auditoria.Caso[struct{}, int64]{
		Accion: "sonda.operativa", Permiso: nucleo.PermClientesVer,
		Logica: func(ctx context.Context, _ *auditoria.Tx, _ struct{}) (int64, error) {
			corrio = true
			return contexto.EmpresaDe(ctx), nil
		},
	})
	admin := auditoria.Nuevo(auditoria.Caso[struct{}, int64]{
		Accion: "sonda.admin", Permiso: nucleo.PermUsuariosVer, SinEmpresa: true,
		Logica: func(context.Context, *auditoria.Tx, struct{}) (int64, error) { return 1, nil },
	})
	// Usuario sin pasar por el middleware ni por los ayudantes de prueba.
	crudo := contexto.ConActor(context.Background(), contexto.Actor{Tipo: contexto.ActorToken, ID: 3, TokenID: 9},
		contexto.NuevosPermisos(map[string][]contexto.Alcance{"clientes.ver": {{}}, "usuarios.ver": {{}}}))
	crudo = nucleo.ConBD(contexto.ConRequestID(crudo, contexto.NuevoRequestID()), pool)
	_, err := operativo.Ejecutar(crudo, struct{}{})
	if !errors.Is(err, auditoria.ErrEmpresaNoResuelta) || corrio {
		t.Fatalf("debía fallar sin correr: %v corrio=%v", err, corrio)
	}
	if p, interno := nucleo.AProblema(err); !interno || p.Status != http.StatusInternalServerError || strings.Contains(p.Detail, "empresa") {
		t.Fatalf("problema: %+v interno=%v", p, interno)
	}
	if _, err := admin.Ejecutar(crudo, struct{}{}); err != nil {
		t.Fatalf("lo administrativo corre: %v", err)
	}
	// Con estado resuelto (ayudantes de prueba) y con el actor sistema, corre.
	if id, err := operativo.Ejecutar(pruebas.ContextoUsuario(pool, 3, nucleo.PermClientesVer), struct{}{}); err != nil || id != 0 {
		t.Fatalf("sin conector: %d %v", id, err)
	}
	if id, err := operativo.Ejecutar(pruebas.ContextoUsuarioEnEmpresa(pool, 3, 7, nucleo.PermClientesVer), struct{}{}); err != nil || id != 7 {
		t.Fatalf("con empresa: %d %v", id, err)
	}
	if _, err := operativo.Ejecutar(pruebas.ContextoSistema(pool), struct{}{}); err != nil {
		t.Fatalf("sistema: %v", err)
	}
}

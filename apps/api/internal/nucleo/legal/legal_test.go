package legal_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/legal"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/servidor"
)

// Datos legales del titular (contratos/legal.md), de punta a punta por HTTP contra Postgres real
// (IRIS_DB=iris_test_legal).

func TestMain(m *testing.M) { os.Exit(pruebas.Correr(m)) }

var (
	ident          = identidad.Nuevo(identidad.Config{})
	empresasUnaVez sync.Once
)

type entorno struct {
	t     *testing.T
	bd    nucleo.BD
	ctx   context.Context
	h     http.Handler
	reloj time.Time
}

type persona struct {
	id      int64
	correo  string
	cookie  string
	csrf    string
	empresa int64
}

type respuesta struct {
	status    int
	cuerpo    map[string]any
	crudo     string
	cabeceras http.Header
}

func nuevoEntorno(t *testing.T) *entorno {
	t.Helper()
	pool := pruebas.Pool(t)
	ctx := pruebas.ContextoSistema(pool)
	empresasUnaVez.Do(func() {
		if _, err := pool.Exec(ctx, `INSERT INTO nucleo.empresas (id, nombre, origen) VALUES (1, 'Empresa 1', 'argos'), (2, 'Empresa 2', 'argos') ON CONFLICT DO NOTHING`); err != nil {
			t.Fatal(err)
		}
	})
	e := &entorno{t: t, bd: pool, ctx: ctx, reloj: time.Date(2026, 11, 17, 15, 0, 0, 0, time.UTC)}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	m := legal.Nuevo(legal.Config{Reloj: func() time.Time { return e.reloj }})
	e.h, _ = servidor.Nuevo(servidor.Deps{BD: pool, Log: log, Version: "prueba", Identidad: ident, Modulos: []func(huma.API){m.Registrar}})
	limpiar := func() {
		if _, err := pool.Exec(context.Background(), `DELETE FROM nucleo.datos_legales`); err != nil {
			t.Error(err)
		}
	}
	limpiar()
	t.Cleanup(limpiar)
	return e
}

// persona crea un usuario con esos roles base y abre su sesión.
func (e *entorno) persona(empresas []int64, roles ...string) persona {
	e.t.Helper()
	correo := pruebas.Correo("legal")
	var rs []identidad.AsignacionRol
	for _, r := range roles {
		rs = append(rs, identidad.AsignacionRol{Rol: r})
	}
	u, err := ident.CrearUsuario.Ejecutar(e.ctx, identidad.EntradaCrearUsuario{Nombre: "Persona " + pruebas.Unico(), Correo: correo,
		Contrasena: "contrasena-larga-1", Roles: rs, Empresas: empresas})
	if err != nil {
		e.t.Fatal(err)
	}
	p := persona{id: u.ID, correo: correo}
	if len(empresas) > 0 {
		p.empresa = empresas[0]
	}
	s, err := ident.IniciarSesion.Ejecutar(nucleo.ConBD(context.Background(), e.bd), identidad.EntradaIniciarSesion{Correo: correo, Contrasena: "contrasena-larga-1"})
	if err != nil {
		e.t.Fatal(err)
	}
	p.cookie, p.csrf = s.Token, s.CSRFToken
	return p
}

// conPermisos: una persona de esas empresas con un rol propio que solo da esos permisos (sin empresas.todas).
func (e *entorno) conPermisos(empresas []int64, permisos ...string) persona {
	e.t.Helper()
	codigo := "legal_" + strings.ReplaceAll(pruebas.Unico(), "-", "_")
	var rol int64
	if err := e.bd.QueryRow(e.ctx, `INSERT INTO nucleo.roles (codigo, nombre) VALUES ($1, $1) RETURNING id`, codigo).Scan(&rol); err != nil {
		e.t.Fatal(err)
	}
	for _, permiso := range permisos {
		if _, err := e.bd.Exec(e.ctx, `INSERT INTO nucleo.roles_permisos (rol_id, permiso) VALUES ($1, $2)`, rol, permiso); err != nil {
			e.t.Fatal(err)
		}
	}
	p := e.persona(empresas)
	if _, err := e.bd.Exec(e.ctx, `INSERT INTO nucleo.usuarios_roles (usuario_id, rol_id) VALUES ($1, $2)`, p.id, rol); err != nil {
		e.t.Fatal(err)
	}
	return p
}

func (e *entorno) api(p persona, metodo, ruta string, cuerpo any) respuesta {
	e.t.Helper()
	var rd io.Reader
	if cuerpo != nil {
		b, _ := json.Marshal(cuerpo)
		rd = bytes.NewReader(b)
	}
	req := httptest.NewRequest(metodo, ruta, rd)
	if rd != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if p.cookie != "" {
		req.AddCookie(&http.Cookie{Name: identidad.CookieSesion, Value: p.cookie})
		req.AddCookie(&http.Cookie{Name: identidad.CookieCSRF, Value: p.csrf})
		req.Header.Set(identidad.CabeceraCSRF, p.csrf)
	}
	if p.empresa != 0 {
		req.Header.Set(identidad.CabeceraEmpresa, fmt.Sprint(p.empresa))
	}
	w := httptest.NewRecorder()
	e.h.ServeHTTP(w, req)
	r := respuesta{status: w.Code, crudo: w.Body.String(), cabeceras: w.Header()}
	_ = json.Unmarshal(w.Body.Bytes(), &r.cuerpo)
	return r
}

func (e *entorno) esperar(r respuesta, status int) respuesta {
	e.t.Helper()
	if r.status != status {
		e.t.Fatalf("esperaba %d, llegó %d: %s", status, r.status, r.crudo)
	}
	return r
}

func (e *entorno) contar(sql string, args ...any) int {
	e.t.Helper()
	var n int
	if err := e.bd.QueryRow(e.ctx, sql, args...).Scan(&n); err != nil {
		e.t.Fatal(err)
	}
	return n
}

func (e *entorno) ultimoEvento(nombre string) map[string]any {
	e.t.Helper()
	var b []byte
	if err := e.bd.QueryRow(e.ctx, `SELECT payload FROM integracion.outbox WHERE evento = $1 ORDER BY id DESC LIMIT 1`, nombre).Scan(&b); err != nil {
		e.t.Fatalf("%s: %v", nombre, err)
	}
	var p map[string]any
	_ = json.Unmarshal(b, &p)
	return p
}

const (
	rutaPanel   = "/api/v1/datos-legales"
	rutaGuardar = "/api/v1/datos-legales/guardar"
	rutaPublica = "/api/v1/publico/datos-legales"
)

func completos(version int) map[string]any {
	return map[string]any{"version": version, "razonSocial": "  Redes del Sur, S. A.  ", "nombreComercial": "RedSur", "correoPrivacidad": "privacidad@redsur.example",
		"telefono": "+502 2222-3333", "direccion": "5a avenida 1-23, zona 1, Ciudad", "pais": "Guatemala"}
}

func TestDatosLegalesGuardarVerYPublicar(t *testing.T) {
	x := nuevoEntorno(t)
	admin := x.persona([]int64{1}, "admin")

	// Sin datos: el panel ve qué falta y la página pública recibe campos vacíos, nunca un error.
	v := x.esperar(x.api(admin, http.MethodGet, rutaPanel, nil), http.StatusOK).cuerpo
	if v["configurado"] != false || v["version"] != float64(0) || v["completo"] != false || v["actualizadoEn"] != nil ||
		fmt.Sprint(v["faltan"]) != "[razonSocial nombreComercial correoPrivacidad direccion pais]" {
		t.Fatalf("sin datos: %v", v)
	}
	r := x.esperar(x.api(persona{}, http.MethodGet, rutaPublica, nil), http.StatusOK)
	if r.cuerpo["completo"] != false || r.cuerpo["nombreComercial"] != "" || r.cuerpo["actualizadoEn"] != nil || r.cabeceras.Get("Cache-Control") != legal.CachePublica {
		t.Fatalf("público sin datos: %s %v", r.crudo, r.cabeceras)
	}

	// Guardar a medias: se guarda lo que hay y dice qué falta.
	v = x.esperar(x.api(admin, http.MethodPost, rutaGuardar, map[string]any{"version": 0, "razonSocial": "", "nombreComercial": "RedSur",
		"correoPrivacidad": "", "telefono": "", "direccion": "", "pais": "Guatemala"}), http.StatusOK).cuerpo
	if v["configurado"] != true || v["version"] != float64(1) || v["completo"] != false || fmt.Sprint(v["faltan"]) != "[razonSocial correoPrivacidad direccion]" {
		t.Fatalf("a medias: %v", v)
	}
	if ev := x.ultimoEvento("datos_legales.cambiados"); ev["completo"] != false || fmt.Sprint(ev["faltan"]) != "[razonSocial correoPrivacidad direccion]" ||
		!strings.Contains(fmt.Sprint(ev["campos"]), "nombre_comercial") {
		t.Fatalf("evento: %v", ev)
	}
	r = x.esperar(x.api(persona{}, http.MethodGet, rutaPublica, nil), http.StatusOK)
	if r.cuerpo["completo"] != false || r.cuerpo["nombreComercial"] != "RedSur" || r.cuerpo["correo"] != "" {
		t.Fatalf("público a medias: %s", r.crudo)
	}

	// Versión vieja: 409. Con la buena, completos (y sin espacios sobrantes).
	x.esperar(x.api(admin, http.MethodPost, rutaGuardar, completos(0)), http.StatusConflict)
	x.reloj = x.reloj.Add(48 * time.Hour)
	v = x.esperar(x.api(admin, http.MethodPost, rutaGuardar, completos(1)), http.StatusOK).cuerpo
	if v["version"] != float64(2) || v["completo"] != true || fmt.Sprint(v["faltan"]) != "[]" || v["razonSocial"] != "Redes del Sur, S. A." ||
		v["actualizadoEn"] != "2026-11-19T15:00:00Z" {
		t.Fatalf("completos: %v", v)
	}
	ev := x.ultimoEvento("datos_legales.cambiados")
	if ev["completo"] != true || fmt.Sprint(ev["campos"]) != "[correo_privacidad direccion razon_social telefono]" {
		t.Fatalf("evento completo: %v", ev)
	}
	// El evento dice qué campos cambiaron, no sus valores.
	if b, _ := json.Marshal(ev); strings.Contains(string(b), "redsur.example") {
		t.Fatalf("el evento trae valores: %s", b)
	}
	if n := x.contar(`SELECT count(*) FROM auditoria.cambios c JOIN auditoria.acciones a ON a.id = c.accion_id
		WHERE a.accion = 'datos_legales.guardar' AND a.actor_id = $1 AND c.campo = 'correo_privacidad' AND c.despues = '"privacidad@redsur.example"'`, admin.id); n != 1 {
		t.Fatalf("auditoría del cambio: %d", n)
	}

	// Lo público: solo lo que el titular publica, con la fecha del último cambio.
	r = x.esperar(x.api(persona{}, http.MethodGet, rutaPublica, nil), http.StatusOK)
	p := r.cuerpo
	if p["completo"] != true || p["nombreComercial"] != "RedSur" || p["razonSocial"] != "Redes del Sur, S. A." || p["correo"] != "privacidad@redsur.example" ||
		p["telefono"] != "+502 2222-3333" || p["direccion"] != "5a avenida 1-23, zona 1, Ciudad" || p["pais"] != "Guatemala" || p["actualizadoEn"] != "2026-11-19T15:00:00Z" {
		t.Fatalf("público: %s", r.crudo)
	}
	if len(p) != 8 {
		t.Fatalf("el público trae campos de más: %s", r.crudo)
	}

	// Guardar lo mismo: ni versión, ni fecha, ni evento, ni cambios nuevos.
	eventos := x.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'datos_legales.cambiados'`)
	x.reloj = x.reloj.Add(24 * time.Hour)
	v = x.esperar(x.api(admin, http.MethodPost, rutaGuardar, completos(2)), http.StatusOK).cuerpo
	if v["version"] != float64(2) || v["actualizadoEn"] != "2026-11-19T15:00:00Z" {
		t.Fatalf("sin cambios: %v", v)
	}
	if n := x.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'datos_legales.cambiados'`); n != eventos {
		t.Fatalf("sin cambios emitió un evento: %d → %d", eventos, n)
	}
	if n := x.contar(`SELECT count(*) FROM nucleo.datos_legales`); n != 1 {
		t.Fatalf("filas: %d", n)
	}
}

func TestDatosLegalesValidaciones(t *testing.T) {
	x := nuevoEntorno(t)
	admin := x.persona([]int64{1}, "admin")
	casos := []struct {
		campo string
		valor any
	}{
		{"correoPrivacidad", "no-es-un-correo"},
		{"correoPrivacidad", "a@b.example, c@d.example"},
		{"correoPrivacidad", "Ana <ana@isp.example>"},
		{"correoPrivacidad", "ana@localhost"},
		{"telefono", "llámame"},
		{"telefono", "123"},
		{"direccion", "línea 1\nlínea 2"},
		{"razonSocial", strings.Repeat("a", 201)},
		{"pais", strings.Repeat("a", 81)},
	}
	for _, c := range casos {
		cuerpo := completos(0)
		cuerpo[c.campo] = c.valor
		r := x.esperar(x.api(admin, http.MethodPost, rutaGuardar, cuerpo), http.StatusUnprocessableEntity)
		if !strings.Contains(r.crudo, c.campo) {
			t.Fatalf("%s = %q: el 422 no nombra el campo: %s", c.campo, c.valor, r.crudo)
		}
	}
	if n := x.contar(`SELECT count(*) FROM nucleo.datos_legales`); n != 0 {
		t.Fatalf("un dato inválido se guardó: %d", n)
	}
}

func TestDatosLegalesAlcance(t *testing.T) {
	x := nuevoEntorno(t)
	admin := x.persona([]int64{1}, "admin")
	x.esperar(x.api(admin, http.MethodPost, rutaGuardar, completos(0)), http.StatusOK)

	// Sin sesión: el panel no; lo público sí.
	x.esperar(x.api(persona{}, http.MethodGet, rutaPanel, nil), http.StatusUnauthorized)
	x.esperar(x.api(persona{}, http.MethodPost, rutaGuardar, completos(1)), http.StatusUnauthorized)
	x.esperar(x.api(persona{}, http.MethodGet, rutaPublica, nil), http.StatusOK)

	// Sin el permiso: 403.
	soporte := x.persona([]int64{1}, "soporte")
	x.esperar(x.api(soporte, http.MethodGet, rutaPanel, nil), http.StatusForbidden)
	x.esperar(x.api(soporte, http.MethodPost, rutaGuardar, completos(1)), http.StatusForbidden)

	// Con solo «ver»: lee, no guarda. Son de la instalación: hace falta operar en todas las empresas.
	lector := x.conPermisos([]int64{1}, "datos_legales.ver", "empresas.todas")
	x.esperar(x.api(lector, http.MethodGet, rutaPanel, nil), http.StatusOK)
	x.esperar(x.api(lector, http.MethodPost, rutaGuardar, completos(1)), http.StatusForbidden)
	deUnaEmpresa := x.conPermisos([]int64{1}, "datos_legales.ver", "datos_legales.configurar")
	x.esperar(x.api(deUnaEmpresa, http.MethodGet, rutaPanel, nil), http.StatusForbidden)
	x.esperar(x.api(deUnaEmpresa, http.MethodPost, rutaGuardar, completos(1)), http.StatusForbidden)

	if n := x.contar(`SELECT count(*) FROM nucleo.datos_legales WHERE version = 1`); n != 1 {
		t.Fatalf("alguien sin alcance cambió los datos: %d", n)
	}
	// La lectura pública no deja filas de auditoría (la repiten los rastreadores).
	if n := x.contar(`SELECT count(*) FROM auditoria.acciones WHERE accion = 'datos_legales.ver_publico'`); n != 0 {
		t.Fatalf("la lectura pública se auditó: %d", n)
	}
}

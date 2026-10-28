package campo_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/HCDestroyer/iris/apps/api/internal/campo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/servidor"
	"github.com/HCDestroyer/iris/apps/api/internal/soporte"
)

const contrasena = "contrasena-de-prueba-123"

type clienteHTTP struct {
	t    *testing.T
	base string
	http *http.Client
	csrf string
	id   int64
}

type respuesta struct {
	status int
	cuerpo map[string]any
	crudo  []byte
	h      http.Header
}

func (r respuesta) problema(t *testing.T, status int, codigo string) {
	t.Helper()
	if r.status != status || !strings.HasPrefix(r.h.Get("Content-Type"), "application/problem+json") ||
		r.cuerpo["type"] != "urn:iris:problema:"+codigo {
		t.Fatalf("esperaba %d %s, llegó %d %s", status, codigo, r.status, r.crudo)
	}
}

func nuevoServidor(t *testing.T) string {
	t.Helper()
	cmp := campo.Nuevo(campo.Config{})
	h, _ := servidor.Nuevo(servidor.Deps{
		BD: pruebas.Pool(t), Identidad: ident, Version: "prueba", Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Modulos: []func(huma.API){soporte.Nuevo(soporte.Config{}).Registrar, cmp.Registrar, cmp.Agenda().Registrar},
	})
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv.URL
}

// entrar crea un usuario persona con los roles dados e inicia su sesión.
func entrar(t *testing.T, base string, roles ...string) *clienteHTTP {
	t.Helper()
	correo := pruebas.Correo("http")
	asign := []identidad.AsignacionRol{}
	for _, r := range roles {
		asign = append(asign, identidad.AsignacionRol{Rol: r})
	}
	u, err := ident.CrearUsuario.Ejecutar(pruebas.ContextoSistema(pruebas.Pool(t)), identidad.EntradaCrearUsuario{
		Nombre: "Usuario HTTP", Correo: correo, Contrasena: contrasena, Roles: asign,
	})
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(roles, "tecnico") {
		conAgenda(t, u.ID)
	}
	asegurarZona(t, 3)
	jar, _ := cookiejar.New(nil)
	c := &clienteHTTP{t: t, base: base, http: &http.Client{Jar: jar}, id: u.ID}
	r := c.pedir(http.MethodPost, "/api/v1/sesion", map[string]any{"correo": correo, "contrasena": contrasena})
	if r.status != http.StatusOK {
		t.Fatalf("login: %d %s", r.status, r.crudo)
	}
	c.csrf, _ = r.cuerpo["csrfToken"].(string)
	return c
}

func (c *clienteHTTP) pedir(metodo, ruta string, cuerpo any, cabeceras ...string) respuesta {
	c.t.Helper()
	var lector io.Reader
	if cuerpo != nil {
		b, _ := json.Marshal(cuerpo)
		lector = bytes.NewReader(b)
	}
	req, _ := http.NewRequestWithContext(context.Background(), metodo, c.base+ruta, lector)
	if cuerpo != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.csrf != "" {
		req.Header.Set(identidad.CabeceraCSRF, c.csrf)
	}
	for i := 0; i+1 < len(cabeceras); i += 2 {
		req.Header.Set(cabeceras[i], cabeceras[i+1])
	}
	res, err := c.http.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	crudo, _ := io.ReadAll(res.Body)
	r := respuesta{status: res.StatusCode, crudo: crudo, h: res.Header}
	_ = json.Unmarshal(crudo, &r.cuerpo)
	return r
}

func TestHTTPDespachoYTecnico(t *testing.T) {
	base := nuevoServidor(t)
	d := entrar(t, base, "despacho")
	tec := entrar(t, base, "tecnico")
	lec := entrar(t, base, "lectura")

	r := d.pedir(http.MethodPost, "/api/v1/ordenes", map[string]any{"tipo": "visita", "direccion": "Zona 1", "zonaId": 3},
		"Idempotency-Key", "orden-"+pruebas.Unico())
	if r.status != http.StatusCreated || r.cuerpo["estado"] != "pendiente" {
		t.Fatalf("crear: %d %s", r.status, r.crudo)
	}
	id := int64(r.cuerpo["id"].(float64))
	ruta := fmt.Sprintf("/api/v1/ordenes/%d", id)
	r = d.pedir(http.MethodPost, ruta+"/programar", map[string]any{"version": r.cuerpo["version"], "tecnicoId": tec.id,
		"inicio": "2026-10-28T16:00:00Z"})
	if r.status != http.StatusOK || r.cuerpo["estado"] != "programada" {
		t.Fatalf("programar: %d %s", r.status, r.crudo)
	}
	d.pedir(http.MethodPost, ruta+"/cancelar", map[string]any{"version": 1, "motivo": "Duplicada"}).problema(t, 409, "conflicto_version")
	tec.pedir(http.MethodPost, ruta+"/cancelar", map[string]any{"version": 2, "motivo": "No voy"}).problema(t, 403, "prohibido")

	// El técnico: tablero propio, detalle y tipos.
	if r := tec.pedir(http.MethodGet, "/api/v1/ordenes?estado=programada", nil); r.status != 200 || len(r.cuerpo["data"].([]any)) != 1 {
		t.Fatalf("mis órdenes: %d %s", r.status, r.crudo)
	}
	if r := tec.pedir(http.MethodGet, ruta, nil); r.status != 200 || len(r.cuerpo["checklist"].([]any)) == 0 {
		t.Fatalf("detalle: %d %s", r.status, r.crudo)
	}
	if r := tec.pedir(http.MethodGet, "/api/v1/tipos-orden", nil); r.status != 200 || len(r.cuerpo["data"].([]any)) != 5 {
		t.Fatalf("tipos: %d %s", r.status, r.crudo)
	}

	// Paquete y lote con claves camelCase.
	p := tec.pedir(http.MethodGet, "/api/v1/campo/paquete?fecha=2026-10-28", nil)
	if p.status != 200 || p.cuerpo["cursor"] == "" || p.cuerpo["existencias"].(map[string]any)["disponible"] != false {
		t.Fatalf("paquete: %d %s", p.status, p.crudo)
	}
	opID := opID()
	cuerpo := map[string]any{"dispositivoId": "pwa-1", "plataforma": "pwa", "operaciones": []map[string]any{
		{"id": opID, "tipo": "orden.llegar", "ordenId": id, "versionBase": 2, "ocurridoEn": "2026-10-28T15:59:00Z"},
		{"id": opID, "tipo": "orden.llegar", "ordenId": id},
		{"id": "019a0000-0000-7000-8000-000000000001", "tipo": "orden.cerrar", "ordenId": id},
	}}
	l := tec.pedir(http.MethodPost, "/api/v1/sync/lote", cuerpo)
	if l.status != 200 {
		t.Fatalf("lote: %d %s", l.status, l.crudo)
	}
	res := l.cuerpo["resultados"].([]any)
	r0, r1, r2 := res[0].(map[string]any), res[1].(map[string]any), res[2].(map[string]any)
	if r0["estado"] != "aplicada" || r1["estado"] != "duplicada" || r2["estado"] != "rechazada" ||
		r2["error"].(map[string]any)["type"] != "urn:iris:problema:cierre_incompleto" ||
		r2["orden"].(map[string]any)["estado"] != "en_sitio" || l.cuerpo["cursor"] != p.cuerpo["cursor"] {
		t.Fatalf("resultados: %s", l.crudo)
	}
	c := tec.pedir(http.MethodGet, "/api/v1/campo/cambios?cursor="+p.cuerpo["cursor"].(string), nil)
	if c.status != 200 || len(c.cuerpo["cambios"].([]any)) != 1 {
		t.Fatalf("cambios: %d %s", c.status, c.crudo)
	}
	// Lectura no sincroniza.
	lec.pedir(http.MethodPost, "/api/v1/sync/lote", map[string]any{"dispositivoId": "x", "operaciones": []any{}}).problema(t, 403, "prohibido")
	lec.pedir(http.MethodGet, "/api/v1/campo/paquete", nil).problema(t, 403, "prohibido")
	if r := lec.pedir(http.MethodGet, ruta, nil); r.status != 200 {
		t.Fatalf("lectura ve: %d", r.status)
	}
	d.pedir(http.MethodGet, "/api/v1/ordenes/99999999", nil).problema(t, 404, "no_encontrado")
}

func TestLoteConcurrenteMismaOperacion(t *testing.T) {
	e := nuevoEntorno()
	d, tec := despacho(t), tecnico(t)
	cd := como(t, d, "despacho")
	o := programada(t, e, cd, "visita", tec)
	nota := op(campo.OpNotaAgregar, o.ID, map[string]any{"texto": "una sola vez"})
	var wg sync.WaitGroup
	estadosVistos := make([]string, 4)
	for i := range estadosVistos {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, err := e.m.Lote(como(t, tec, "tecnico"), campo.EntradaLote{DispositivoID: "tel-concurrente", Operaciones: []campo.OperacionLote{nota}})
			if err == nil {
				estadosVistos[i] = r.Resultados[0].Estado
			}
		}()
	}
	wg.Wait()
	aplicadas := 0
	for _, s := range estadosVistos {
		if s == "aplicada" {
			aplicadas++
		} else if s != "duplicada" {
			t.Fatalf("estados: %v", estadosVistos)
		}
	}
	if aplicadas != 1 || contar(t, "SELECT count(*) FROM campo.orden_notas WHERE orden_id = $1", o.ID) != 1 {
		t.Fatalf("una sola aplicación: %v", estadosVistos)
	}
}

// Por HTTP: posiciones solo con la cabecera de la app de campo y esquema validado; despacho ve
// la última, la agenda (calendario) y los asignables.
func TestHTTPPosicionesYAgenda(t *testing.T) {
	base := nuevoServidor(t)
	d := entrar(t, base, "despacho")
	tec := entrar(t, base, "tecnico")
	ahora := time.Now().UTC()
	lote := map[string]any{"dispositivoId": "pwa-1", "puntos": []map[string]any{
		{"lat": 14.6, "lon": -90.5, "precision": 12, "registradaEn": ahora.Add(-time.Minute).Format(time.RFC3339)}}}
	tec.pedir(http.MethodPost, "/api/v1/campo/posiciones", lote).problema(t, 403, "requiere_sesion_campo")
	if r := tec.pedir(http.MethodPost, "/api/v1/campo/posiciones", lote, "X-Iris-Canal", "campo"); r.status != 200 || r.cuerpo["aceptadas"] != float64(1) {
		t.Fatalf("posiciones: %d %s", r.status, r.crudo)
	}
	malo := map[string]any{"puntos": []map[string]any{{"lat": 91, "lon": -90.5, "registradaEn": ahora.Format(time.RFC3339)}}}
	tec.pedir(http.MethodPost, "/api/v1/campo/posiciones", malo, "X-Iris-Canal", "campo").problema(t, 422, "validacion")
	d.pedir(http.MethodPost, "/api/v1/campo/posiciones", lote, "X-Iris-Canal", "campo").problema(t, 403, "prohibido")
	r := d.pedir(http.MethodGet, fmt.Sprintf("/api/v1/campo/posiciones/ultimas?tecnicoId=%d", tec.id), nil)
	if r.status != 200 || len(r.cuerpo["data"].([]any)) != 1 {
		t.Fatalf("últimas: %d %s", r.status, r.crudo)
	}
	tec.pedir(http.MethodGet, "/api/v1/campo/posiciones/ultimas", nil).problema(t, 403, "prohibido")
	// Límite de lotes por minuto: 429 con Retry-After.
	var ultimo respuesta
	for range 7 {
		ultimo = tec.pedir(http.MethodPost, "/api/v1/campo/posiciones", lote, "X-Iris-Canal", "campo")
	}
	ultimo.problema(t, 429, "demasiadas_posiciones")
	if ultimo.h.Get("Retry-After") == "" {
		t.Fatal("Retry-After")
	}

	// Agenda: asignables (ruta fija antes que /tecnicos/{id}) y calendario.
	if r := d.pedir(http.MethodGet, "/api/v1/tecnicos/asignables?zonaId=3&tipo=visita", nil); r.status != 200 || r.cuerpo["duracionMin"] != float64(60) {
		t.Fatalf("asignables: %d %s", r.status, r.crudo)
	}
	desde, hasta := ahora.Add(-time.Hour).Format(time.RFC3339), ahora.Add(24*time.Hour).Format(time.RFC3339)
	if r := d.pedir(http.MethodGet, "/api/v1/agenda?desde="+desde+"&hasta="+hasta, nil); r.status != 200 || r.cuerpo["citas"] == nil {
		t.Fatalf("agenda: %d %s", r.status, r.crudo)
	}
	d.pedir(http.MethodGet, "/api/v1/agenda?desde=ayer&hasta="+hasta, nil).problema(t, 422, "validacion")
	d.pedir(http.MethodGet, "/api/v1/tecnicos/99999999", nil).problema(t, 404, "no_encontrado")
	tec.pedir(http.MethodGet, "/api/v1/zonas", nil).problema(t, 403, "prohibido")
}

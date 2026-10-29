package reportes_test

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/servidor"
)

const contrasena = "contrasena-de-prueba-123"

type clienteHTTP struct {
	t    *testing.T
	base string
	http *http.Client
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
	h, _ := servidor.Nuevo(servidor.Deps{
		BD: pruebas.Pool(t), Identidad: ident, Version: "prueba", Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Modulos: []func(huma.API){modulo().Registrar},
	})
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv.URL
}

// entrar crea un usuario con el rol dado e inicia su sesión.
func entrar(t *testing.T, base, rol string) *clienteHTTP {
	t.Helper()
	correo := pruebas.Correo("http")
	if _, err := ident.CrearUsuario.Ejecutar(pruebas.ContextoSistema(pruebas.Pool(t)), identidad.EntradaCrearUsuario{
		Nombre: "Usuario HTTP", Correo: correo, Contrasena: contrasena, Roles: []identidad.AsignacionRol{{Rol: rol}},
	}); err != nil {
		t.Fatal(err)
	}
	jar, _ := cookiejar.New(nil)
	c := &clienteHTTP{t: t, base: base, http: &http.Client{Jar: jar}}
	r := c.pedir(http.MethodPost, "/api/v1/sesion", `{"correo":"`+correo+`","contrasena":"`+contrasena+`"}`)
	if r.status != http.StatusOK {
		t.Fatalf("login: %d %s", r.status, r.crudo)
	}
	return c
}

func (c *clienteHTTP) pedir(metodo, ruta, cuerpo string, cabeceras ...string) respuesta {
	c.t.Helper()
	var lector io.Reader
	if cuerpo != "" {
		lector = strings.NewReader(cuerpo)
	}
	req, _ := http.NewRequestWithContext(context.Background(), metodo, c.base+ruta, lector)
	if cuerpo != "" {
		req.Header.Set("Content-Type", "application/json")
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

func leerCSV(t *testing.T, r respuesta) [][]string {
	t.Helper()
	if r.status != http.StatusOK || r.h.Get("Content-Type") != "text/csv; charset=utf-8" ||
		!strings.HasPrefix(r.h.Get("Content-Disposition"), "attachment; filename=\"reporte-") {
		t.Fatalf("CSV: %d %v %s", r.status, r.h, r.crudo)
	}
	if !strings.HasPrefix(string(r.crudo), "\xEF\xBB\xBF") {
		t.Fatal("el CSV empieza con BOM (Excel)")
	}
	filas, err := csv.NewReader(strings.NewReader(strings.TrimPrefix(string(r.crudo), "\xEF\xBB\xBF"))).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	return filas
}

func TestHTTPReportes(t *testing.T) {
	limpiar(t)
	base := nuevoServidor(t)
	res := gt("2026-10-05", "10:00")
	ticket(t, tk{prioridad: "alta", servicio: p(int64(9)), creado: res.Add(-time.Hour), resuelto: &res,
		venceRespuesta: en(res, time.Hour), respuesta: &res, venceResolucion: en(res, time.Hour)})
	ticket(t, tk{servicio: p(int64(9)), creado: *en(res, 24*time.Hour)})
	// Un asunto que parece fórmula sale con apóstrofo en el CSV.
	if _, err := pruebas.Pool(t).Exec(context.Background(), "UPDATE soporte.tickets SET asunto = '=HYPERLINK(1)' WHERE resuelto_en IS NOT NULL"); err != nil {
		t.Fatal(err)
	}

	lectura := entrar(t, base, "lectura")
	periodo := "?desde=2026-10-01&hasta=2026-10-31"

	// JSON por defecto.
	r := lectura.pedir(http.MethodGet, "/api/v1/reportes/sla"+periodo, "")
	if r.status != http.StatusOK || !strings.HasPrefix(r.h.Get("Content-Type"), "application/json") {
		t.Fatalf("sla: %d %s", r.status, r.crudo)
	}
	total := r.cuerpo["total"].(map[string]any)
	if total["resueltos"] != float64(1) || total["porcentajeRespuesta"] != float64(100) {
		t.Fatalf("total: %s", r.crudo)
	}

	// CSV con ?formato=csv y con Accept.
	filas := leerCSV(t, lectura.pedir(http.MethodGet, "/api/v1/reportes/sla"+periodo+"&formato=csv", ""))
	if strings.Join(filas[0], "|") != "Grupo|Prioridad|Categoría|Resueltos|Con SLA de respuesta|Respuesta en tiempo|% respuesta en tiempo|Con SLA de resolución|Resolución en tiempo|% resolución en tiempo" {
		t.Fatalf("encabezados: %v", filas[0])
	}
	if filas[1][0] != "Total" || filas[1][3] != "1" || filas[1][6] != "100" {
		t.Fatalf("fila total: %v", filas[1])
	}
	if filas[2][1] != "Alta" {
		t.Fatalf("prioridad en español: %v", filas[2])
	}
	for _, ruta := range []string{"resolucion", "tecnicos", "primera-visita"} {
		filas := leerCSV(t, lectura.pedir(http.MethodGet, "/api/v1/reportes/"+ruta+periodo, "", "Accept", "text/csv"))
		if len(filas) < 2 {
			t.Fatalf("%s: %v", ruta, filas)
		}
	}
	filas = leerCSV(t, lectura.pedir(http.MethodGet, "/api/v1/reportes/reincidencias"+periodo+"&limit=1", "", "Accept", "text/csv, application/json;q=0.5"))
	if len(filas) != 2 || filas[1][1] != "'=HYPERLINK(1)" || filas[1][6] != "2026-10-05 10:00" {
		t.Fatalf("reincidencias CSV: %v", filas)
	}
	r = lectura.pedir(http.MethodGet, "/api/v1/reportes/reincidencias"+periodo, "")
	if r.status != http.StatusOK || r.cuerpo["reincidentes"] != float64(1) || len(r.cuerpo["data"].([]any)) != 1 {
		t.Fatalf("reincidencias: %d %s", r.status, r.crudo)
	}

	// Errores en problem+json aunque se pida CSV.
	lectura.pedir(http.MethodGet, "/api/v1/reportes/sla?desde=2025-01-01&hasta=2026-10-31", "", "Accept", "text/csv").
		problema(t, http.StatusUnprocessableEntity, "rango_excedido")
	lectura.pedir(http.MethodGet, "/api/v1/reportes/sla?desde=2026-10-01", "").problema(t, http.StatusUnprocessableEntity, "validacion")
	lectura.pedir(http.MethodGet, "/api/v1/reportes/primera-visita"+periodo+"&dias=120", "").problema(t, http.StatusUnprocessableEntity, "validacion")

	// Soporte no tiene reportes.ver; sin sesión, 401.
	entrar(t, base, "soporte").pedir(http.MethodGet, "/api/v1/reportes/sla"+periodo, "").problema(t, http.StatusForbidden, "prohibido")
	anonimo := &clienteHTTP{t: t, base: base, http: http.DefaultClient}
	if r := anonimo.pedir(http.MethodGet, "/api/v1/reportes/sla"+periodo, ""); r.status != http.StatusUnauthorized {
		t.Fatalf("anónimo: %d", r.status)
	}
	// Supervisor sí.
	if r := entrar(t, base, "supervisor").pedir(http.MethodGet, "/api/v1/reportes/tecnicos"+periodo, ""); r.status != http.StatusOK {
		t.Fatalf("supervisor: %d %s", r.status, r.crudo)
	}
}

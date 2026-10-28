package comercial_test

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
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"

	"github.com/HCDestroyer/iris/apps/api/internal/campo"
	"github.com/HCDestroyer/iris/apps/api/internal/comercial"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/adjuntos"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/servidor"
)

const contrasena = "contrasena-de-prueba-123"

type clienteHTTP struct {
	t    *testing.T
	base string
	http *http.Client
	csrf string
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

func (r respuesta) ok(t *testing.T, status int) map[string]any {
	t.Helper()
	if r.status != status {
		t.Fatalf("esperaba %d, llegó %d %s", status, r.status, r.crudo)
	}
	return r.cuerpo
}

func servidorComercial(t *testing.T) string {
	t.Helper()
	comercial.RegistrarAdjuntos()
	almacen, err := adjuntos.NuevoDisco(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	adj := adjuntos.Nuevo(adjuntos.Config{Almacen: almacen})
	cmp := campo.Nuevo(campo.Config{Adjuntos: campo.SinAdjuntosObligatorios{}})
	com := comercial.Nuevo(comercial.Config{Documentos: comercial.DocumentosDeAdjuntos(adj), Campo: cmp})
	h, _ := servidor.Nuevo(servidor.Deps{
		BD: pruebas.Pool(t), Identidad: ident, Version: "prueba", Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Modulos: []func(huma.API){com.Registrar, adj.RegistrarRutas, cmp.Registrar},
	})
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv.URL
}

func entrar(t *testing.T, base string, roles ...string) *clienteHTTP {
	t.Helper()
	correo := pruebas.Correo("http")
	asign := []identidad.AsignacionRol{}
	for _, r := range roles {
		asign = append(asign, identidad.AsignacionRol{Rol: r})
	}
	if _, err := ident.CrearUsuario.Ejecutar(pruebas.ContextoSistema(pruebas.Pool(t)), identidad.EntradaCrearUsuario{
		Nombre: "Usuario HTTP", Correo: correo, Contrasena: contrasena, Roles: asign,
	}); err != nil {
		t.Fatal(err)
	}
	jar, _ := cookiejar.New(nil)
	c := &clienteHTTP{t: t, base: base, http: &http.Client{Jar: jar}}
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

func TestHTTPEmbudo(t *testing.T) {
	base := servidorComercial(t)
	v := entrar(t, base, "ventas")
	lec := entrar(t, base, "lectura")

	alta := map[string]any{"nombre": "Prospecto HTTP", "direccion": "Zona 10", "telefono": "5555-0000"}
	clave := "prospecto-" + pruebas.Unico()
	p := v.pedir(http.MethodPost, "/api/v1/prospectos", alta, "Idempotency-Key", clave).ok(t, http.StatusCreated)
	repetido := v.pedir(http.MethodPost, "/api/v1/prospectos", alta, "Idempotency-Key", clave).ok(t, http.StatusCreated)
	if p["id"] != repetido["id"] || p["origen"] != "panel" || p["estado"] != "nuevo" {
		t.Fatalf("idempotencia: %v %v", p, repetido)
	}
	lec.pedir(http.MethodPost, "/api/v1/prospectos", alta).problema(t, http.StatusForbidden, "prohibido")
	ruta := fmt.Sprintf("/api/v1/prospectos/%v", p["id"])
	v.pedir(http.MethodPost, ruta+"/contactar", map[string]any{"version": 9}).problema(t, http.StatusConflict, "conflicto_version")
	v.pedir(http.MethodPost, ruta+"/contactar", map[string]any{"version": p["version"]}).ok(t, http.StatusOK)
	d := v.pedir(http.MethodGet, ruta, nil).ok(t, http.StatusOK)
	if d["estado"] != "contactado" || d["acciones"].(map[string]any)["evaluar"] != true {
		t.Fatalf("detalle: %v", d)
	}
	f := v.pedir(http.MethodPost, ruta+"/factibilidad", map[string]any{"version": d["version"], "manual": map[string]any{"resultado": "viable"},
		"notas": "Visita técnica con poste a 50 m"}).ok(t, http.StatusOK)
	if f["resultado"] != "viable" || f["motor"] != "manual" {
		t.Fatalf("factibilidad: %v", f)
	}
	d = v.pedir(http.MethodGet, ruta, nil).ok(t, http.StatusOK)
	c := v.pedir(http.MethodPost, ruta+"/cotizaciones", map[string]any{"version": d["version"], "lineas": []map[string]any{
		{"tipo": "plan", "descripcion": "Fibra 30", "precioUnitario": "175.5"},
		{"tipo": "instalacion", "descripcion": "Instalación", "precioUnitario": "100"},
	}}).ok(t, http.StatusCreated)
	if c["totalMensual"] != "175.50" || c["totalUnico"] != "100.00" || c["pdf"].(map[string]any)["estado"] != "completo" {
		t.Fatalf("cotización: %v", c)
	}
	// El PDF se descarga con adjuntos.ver (ventas lo tiene); un rol sin prospectos.ver no lo ve.
	pdf := c["pdf"].(map[string]any)["urlDescarga"].(string)
	if r := v.pedir(http.MethodGet, pdf, nil); r.status != http.StatusOK || !bytes.HasPrefix(r.crudo, []byte("%PDF-")) {
		t.Fatalf("descarga del PDF: %d", r.status)
	}
	tec := entrar(t, base, "tecnico")
	tec.pedir(http.MethodGet, pdf, nil).problema(t, http.StatusNotFound, "no_encontrado")

	v.pedir(http.MethodPost, fmt.Sprintf("/api/v1/cotizaciones/%v/aceptar", c["id"]), map[string]any{"version": c["version"]}).ok(t, http.StatusOK)
	d = v.pedir(http.MethodGet, ruta, nil).ok(t, http.StatusOK)
	ctr := v.pedir(http.MethodPost, ruta+"/contrato", map[string]any{"version": d["version"]}).ok(t, http.StatusCreated)
	ctr = v.pedir(http.MethodPost, fmt.Sprintf("/api/v1/contratos/%v/firmar", ctr["id"]), map[string]any{"version": ctr["version"],
		"firmanteNombre": "Cliente HTTP", "svg": firmaSVG}).ok(t, http.StatusOK)
	if ctr["estado"] != "firmado" || ctr["pdfFirmado"].(map[string]any)["estado"] != "completo" {
		t.Fatalf("firma: %v", ctr)
	}
	d = v.pedir(http.MethodGet, ruta, nil).ok(t, http.StatusOK)
	g := v.pedir(http.MethodPost, ruta+"/ganar", map[string]any{"version": d["version"]}).ok(t, http.StatusOK)
	if g["estado"] != "ganado" || g["alta"].(map[string]any)["modo"] != "local" || g["alta"].(map[string]any)["ordenId"] == nil {
		t.Fatalf("ganar: %v", g)
	}
	if l := v.pedir(http.MethodGet, "/api/v1/prospectos?estado=ganado,perdido", nil).ok(t, http.StatusOK); len(l["data"].([]any)) == 0 {
		t.Fatalf("lista: %v", l)
	}
	v.pedir(http.MethodGet, "/api/v1/prospectos?estado=raro", nil).problema(t, http.StatusUnprocessableEntity, "validacion")
	if h := v.pedir(http.MethodGet, ruta+"/eventos", nil); h.status != 200 {
		t.Fatalf("historial: %d %s", h.status, h.crudo)
	}
	if cob := v.pedir(http.MethodGet, "/api/v1/comercial/cobertura", nil).ok(t, http.StatusOK); cob["disponible"] != false || cob["motivo"] != "sin_conector" {
		t.Fatalf("cobertura sin conector: %v", cob)
	}
	if pl := v.pedir(http.MethodGet, "/api/v1/comercial/planes", nil).ok(t, http.StatusOK); pl["disponible"] != false {
		t.Fatalf("planes sin conector: %v", pl)
	}
}

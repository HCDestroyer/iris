package soporte_test

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

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
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

// servidorConSoporte levanta la API con identidad y soporte y entra con un usuario de los
// roles dados.
func servidorConSoporte(t *testing.T, roles ...string) *clienteHTTP {
	t.Helper()
	pool := pruebas.Pool(t)
	id := identidad.Nuevo(identidad.Config{})
	h, _ := servidor.Nuevo(servidor.Deps{
		BD: pool, Identidad: id, Version: "prueba", Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Modulos: []func(huma.API){soporte.Nuevo(soporte.Config{}).Registrar},
	})
	// Simula el middleware de empresa activa (I1): toda petición llega con la empresa de prueba.
	conEmpresa := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.ServeHTTP(w, r.WithContext(contexto.ConEmpresa(r.Context(), empresaPrueba)))
	})
	srv := httptest.NewServer(conEmpresa)
	t.Cleanup(srv.Close)

	correo := pruebas.Correo("http")
	asign := []identidad.AsignacionRol{}
	for _, r := range roles {
		asign = append(asign, identidad.AsignacionRol{Rol: r})
	}
	if _, err := id.CrearUsuario.Ejecutar(pruebas.ContextoSistema(pool), identidad.EntradaCrearUsuario{
		Nombre: "Usuario HTTP", Correo: correo, Contrasena: contrasena, Roles: asign,
	}); err != nil {
		t.Fatal(err)
	}
	jar, _ := cookiejar.New(nil)
	c := &clienteHTTP{t: t, base: srv.URL, http: &http.Client{Jar: jar}}
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

func TestHTTPTicketsIdempotenciaYErrores(t *testing.T) {
	c := servidorConSoporte(t, "soporte")
	asunto := "Router sin luz " + pruebas.Unico()
	cuerpo := map[string]any{"asunto": asunto, "prioridad": "alta", "contacto": map[string]any{"nombre": "Luis"}}

	// Misma Idempotency-Key → misma respuesta, un solo ticket.
	clave := "crear-" + pruebas.Unico()
	r1 := c.pedir(http.MethodPost, "/api/v1/tickets", cuerpo, "Idempotency-Key", clave)
	r2 := c.pedir(http.MethodPost, "/api/v1/tickets", cuerpo, "Idempotency-Key", clave)
	if r1.status != http.StatusCreated || r2.status != http.StatusCreated || r1.cuerpo["id"] != r2.cuerpo["id"] ||
		r2.h.Get("Idempotent-Replayed") != "true" {
		t.Fatalf("idempotencia: %d %s / %d %s", r1.status, r1.crudo, r2.status, r2.crudo)
	}
	var n int
	if err := pruebas.Pool(t).QueryRow(context.Background(), "SELECT count(*) FROM soporte.tickets WHERE asunto = $1", asunto).Scan(&n); err != nil || n != 1 {
		t.Fatalf("tickets creados: %d %v", n, err)
	}
	id := int64(r1.cuerpo["id"].(float64))
	if eventos := eventosDe(t, id); len(eventos) != 1 {
		t.Fatalf("un solo ticket.creado: %v", nombres(eventos))
	}
	// La misma clave con otro cuerpo → 422.
	c.pedir(http.MethodPost, "/api/v1/tickets", map[string]any{"asunto": "Otro asunto"}, "Idempotency-Key", clave).
		problema(t, http.StatusUnprocessableEntity, "idempotencia_reutilizada")

	// Validación de entrada (huma) → 422 problem+json con el campo.
	r := c.pedir(http.MethodPost, "/api/v1/tickets", map[string]any{"prioridad": "alta"})
	r.problema(t, http.StatusUnprocessableEntity, "validacion")

	// Acción con versión vieja → 409; transición inválida → 422.
	ruta := fmt.Sprintf("/api/v1/tickets/%d", id)
	c.pedir(http.MethodPost, ruta+"/resolver", map[string]any{"version": 7, "solucion": "Reinicio"}).
		problema(t, http.StatusConflict, "conflicto_version")
	c.pedir(http.MethodPost, ruta+"/reabrir", map[string]any{"version": 1, "motivo": "Volvió"}).
		problema(t, http.StatusUnprocessableEntity, "transicion_invalida")
	c.pedir(http.MethodGet, "/api/v1/tickets/99999999", nil).problema(t, http.StatusNotFound, "no_encontrado")

	// Responder (versión opcional) y reintentar con la misma clave no duplica el mensaje.
	claveResp := "resp-" + pruebas.Unico()
	for range 2 {
		r := c.pedir(http.MethodPost, ruta+"/responder", map[string]any{"cuerpo": "Vamos en camino"}, "Idempotency-Key", claveResp)
		if r.status != http.StatusOK || r.cuerpo["ticket"].(map[string]any)["estado"] != "abierto" {
			t.Fatalf("responder: %d %s", r.status, r.crudo)
		}
	}
	msjs := c.pedir(http.MethodGet, ruta+"/mensajes", nil)
	if msjs.status != http.StatusOK || len(msjs.cuerpo["data"].([]any)) != 1 {
		t.Fatalf("mensajes: %d %s", msjs.status, msjs.crudo)
	}

	// Bandeja con filtros por coma y cursor.
	lista := c.pedir(http.MethodGet, "/api/v1/tickets?estado=nuevo,abierto&prioridad=alta&limit=1", nil)
	if lista.status != http.StatusOK || len(lista.cuerpo["data"].([]any)) != 1 {
		t.Fatalf("lista: %d %s", lista.status, lista.crudo)
	}
	if _, ok := lista.cuerpo["nextCursor"]; !ok {
		t.Fatalf("la lista lleva nextCursor: %s", lista.crudo)
	}
	c.pedir(http.MethodGet, "/api/v1/tickets?estado=inventado", nil).problema(t, http.StatusUnprocessableEntity, "validacion")
	if r := c.pedir(http.MethodGet, "/api/v1/categorias", nil); r.status != http.StatusOK {
		t.Fatalf("categorías: %d %s", r.status, r.crudo)
	}
	if r := c.pedir(http.MethodGet, "/api/v1/slas", nil); r.status != http.StatusOK {
		t.Fatalf("slas: %d %s", r.status, r.crudo)
	}
}

func TestHTTPRolLecturaSoloVe(t *testing.T) {
	c := servidorConSoporte(t, "lectura")
	if r := c.pedir(http.MethodGet, "/api/v1/tickets", nil); r.status != http.StatusOK {
		t.Fatalf("lectura ve la bandeja: %d %s", r.status, r.crudo)
	}
	r := c.pedir(http.MethodPost, "/api/v1/tickets", map[string]any{"asunto": "No debería"})
	r.problema(t, http.StatusForbidden, "prohibido")
	if !strings.Contains(r.cuerpo["detail"].(string), "tickets.crear") {
		t.Fatalf("el detalle nombra el permiso: %s", r.crudo)
	}
}

package adjuntos_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/adjuntos"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/servidor"
)

func TestMain(m *testing.M) { os.Exit(pruebas.Correr(m)) }

const contrasena = "contrasena-de-prueba-123"

// Entidad de prueba «prueba»: id → empresa. "oculta" no la ve nadie; "cerrada" no acepta subidas.
var (
	muEnt      sync.Mutex
	entidadesP = map[string]int64{"cerrada": 1}
	registro   sync.Once
)

func registrarEntidad() {
	registro.Do(func() {
		adjuntos.RegistrarEntidadAdjuntable("prueba", func(_ context.Context, _ adjuntos.DBTX, id string, uso adjuntos.Uso) (int64, error) {
			muEnt.Lock()
			defer muEnt.Unlock()
			e, ok := entidadesP[id]
			if !ok {
				return 0, nucleo.NoEncontrado("")
			}
			if id == "cerrada" && uso == adjuntos.UsoSubir {
				return 0, nucleo.ReglaNegocio("orden_cerrada", "La orden ya está cerrada")
			}
			return e, nil
		})
	})
}

type entorno struct {
	o1, o2 string // órdenes de prueba nuevas en cada entorno: empresa 1 y 2
	t      *testing.T
	pool   *pgxpool.Pool
	ident  *identidad.Modulo
	mod    *adjuntos.Modulo
	api    *httptest.Server
}

func nuevoEntorno(t *testing.T, cfg adjuntos.Config) *entorno {
	t.Helper()
	registrarEntidad()
	pool := pruebas.Pool(t)
	for _, sql := range []string{
		"INSERT INTO nucleo.empresas (id, nombre) VALUES (1, 'Empresa 1'), (2, 'Empresa 2') ON CONFLICT (id) DO NOTHING",
	} {
		if _, err := pool.Exec(context.Background(), sql); err != nil {
			t.Fatal(err)
		}
	}
	if cfg.Almacen == nil {
		d, err := adjuntos.NuevoDisco(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		cfg.Almacen = d
	}
	mod := adjuntos.Nuevo(cfg)
	ident := identidad.Nuevo(identidad.Config{})
	h, _ := servidor.Nuevo(servidor.Deps{
		BD: pool, Identidad: ident, Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Modulos: []func(huma.API){mod.RegistrarRutas},
	})
	api := httptest.NewServer(h)
	t.Cleanup(api.Close)
	o1, o2 := uuid.NewString(), uuid.NewString()
	muEnt.Lock()
	entidadesP[o1], entidadesP[o2] = 1, 2
	muEnt.Unlock()
	return &entorno{o1: o1, o2: o2, t: t, pool: pool, ident: ident, mod: mod, api: api}
}

type clienteHTTP struct {
	t    *testing.T
	base string
	http *http.Client
	csrf string
}

func (e *entorno) usuario(rol string, empresas ...int64) *clienteHTTP {
	e.t.Helper()
	correo := pruebas.Correo("u")
	ctx := pruebas.ContextoSistema(e.pool)
	u, err := e.ident.CrearUsuario.Ejecutar(ctx, identidad.EntradaCrearUsuario{
		Nombre: "Usuario", Correo: correo, Contrasena: contrasena, Roles: []identidad.AsignacionRol{{Rol: rol}},
	})
	if err != nil {
		e.t.Fatal(err)
	}
	for _, emp := range empresas {
		if _, err := e.ident.AsignarEmpresaUsuario.Ejecutar(ctx, identidad.EntradaEmpresaUsuario{UsuarioID: u.ID, EmpresaID: emp}); err != nil {
			e.t.Fatal(err)
		}
	}
	jar, _ := cookiejar.New(nil)
	c := &clienteHTTP{t: e.t, base: e.api.URL, http: &http.Client{Jar: jar}}
	r := c.json(http.MethodPost, "/api/v1/sesion", map[string]any{"correo": correo, "contrasena": contrasena})
	if r.status != 200 {
		e.t.Fatalf("login: %d %s", r.status, r.crudo)
	}
	c.csrf, _ = r.cuerpo["csrfToken"].(string)
	return c
}

type respuesta struct {
	status int
	cuerpo map[string]any
	crudo  []byte
	h      http.Header
}

func (r respuesta) problema(t *testing.T, status int, tipo string) {
	t.Helper()
	if r.status != status || r.cuerpo["type"] != "urn:iris:problema:"+tipo {
		t.Fatalf("esperaba %d %s, llegó %d: %s", status, tipo, r.status, r.crudo)
	}
}

func (c *clienteHTTP) pedir(metodo, ruta string, cuerpo io.Reader, cabeceras ...string) respuesta {
	c.t.Helper()
	req, _ := http.NewRequest(metodo, c.base+ruta, cuerpo)
	if c.csrf != "" {
		req.Header.Set(identidad.CabeceraCSRF, c.csrf)
	}
	req.Header.Set("X-Iris-Canal", "campo")
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

func (c *clienteHTTP) json(metodo, ruta string, cuerpo any) respuesta {
	c.t.Helper()
	b, _ := json.Marshal(cuerpo)
	return c.pedir(metodo, ruta, bytes.NewReader(b), "Content-Type", "application/json")
}

func (c *clienteHTTP) head(id uuid.UUID) respuesta {
	return c.pedir(http.MethodHead, adjuntos.RutaSubida(id), nil, "Tus-Resumable", "1.0.0")
}

func (c *clienteHTTP) patch(id uuid.UUID, desde int, datos []byte) respuesta {
	return c.pedir(http.MethodPatch, adjuntos.RutaSubida(id), bytes.NewReader(datos),
		"Tus-Resumable", "1.0.0", "Content-Type", adjuntos.TipoParte, "Upload-Offset", strconv.Itoa(desde))
}

func imagenPNG(t *testing.T, lado int) []byte {
	t.Helper()
	// Ruido: no se comprime, así el PNG pesa ~4·lado² bytes.
	img := image.NewRGBA(image.Rect(0, 0, lado, lado))
	ruido := make([]byte, 4*lado*lado)
	_, _ = rand.Read(ruido)
	for x := range lado {
		for y := range lado {
			i := 4 * (x*lado + y)
			img.Set(x, y, color.RGBA{R: ruido[i], G: ruido[i+1], B: ruido[i+2], A: 255})
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func huella(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func alta(entidadID, tipo, mime string, datos []byte) map[string]any {
	id, _ := uuid.NewV7()
	return map[string]any{"id": id.String(), "entidad": "prueba", "entidadId": entidadID, "tipo": tipo,
		"mime": mime, "tamano": len(datos), "sha256": huella(datos)}
}

func idDe(t *testing.T, a map[string]any) uuid.UUID {
	t.Helper()
	return uuid.MustParse(a["id"].(string))
}

func (e *entorno) contar(sql string, args ...any) int {
	e.t.Helper()
	var n int
	if err := e.pool.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		e.t.Fatal(err)
	}
	return n
}

// Pruebas ----------------------------------------------------------------------------------

func TestSubidaEnPartesCompletaYDescarga(t *testing.T) {
	e := nuevoEntorno(t, adjuntos.Config{Segmento: 1000})
	tec := e.usuario("tecnico", 1)
	foto := imagenPNG(t, 60)
	if len(foto) < 3000 {
		t.Fatalf("la foto de prueba es muy chica: %d", len(foto))
	}
	a := alta(e.o1, "foto", "image/png", foto)
	a["claveChecklist"] = "antena"
	r := tec.json(http.MethodPost, "/api/v1/adjuntos", a)
	if r.status != 201 || r.cuerpo["estado"] != "pendiente" || r.h.Get("Location") != r.cuerpo["urlSubida"] {
		t.Fatalf("alta: %d %s", r.status, r.crudo)
	}
	id := idDe(t, a)
	ctx := pruebas.ContextoSistema(e.pool)
	if f, err := adjuntos.Faltantes(ctx, e.pool, "prueba", e.o1, []adjuntos.Requisito{{Tipo: "foto", ClaveChecklist: "antena"}, {Tipo: "firma"}}); err != nil ||
		strings.Join(f, ",") != "foto:antena,firma" {
		t.Fatalf("faltantes antes: %v %v", f, err)
	}

	if h := tec.head(id); h.status != 200 || h.h.Get("Upload-Offset") != "0" || h.h.Get("Upload-Length") != strconv.Itoa(len(foto)) || h.h.Get("Tus-Resumable") != "1.0.0" {
		t.Fatalf("head: %d %v", h.status, h.h)
	}
	mitad := len(foto) / 2
	if p := tec.patch(id, 0, foto[:mitad]); p.status != 204 || p.h.Get("Upload-Offset") != strconv.Itoa(mitad) {
		t.Fatalf("patch 1: %d %s", p.status, p.crudo)
	}
	if n := e.contar("SELECT count(*) FROM nucleo.adjuntos_partes WHERE adjunto_id = $1", id); n < 2 {
		t.Fatalf("esperaba varias partes de 1000 bytes, hay %d", n)
	}
	// Un offset que no es el recibido: 409 con el offset real.
	if p := tec.patch(id, 0, foto[:10]); p.status != 409 || p.h.Get("Upload-Offset") != strconv.Itoa(mitad) {
		t.Fatalf("offset distinto: %d %v %s", p.status, p.h, p.crudo)
	}
	if p := tec.patch(id, mitad, foto[mitad:]); p.status != 204 || p.h.Get("Upload-Offset") != strconv.Itoa(len(foto)) {
		t.Fatalf("patch 2: %d %s", p.status, p.crudo)
	}
	// HEAD sobre una subida terminada: offset = largo.
	if h := tec.head(id); h.status != 200 || h.h.Get("Upload-Offset") != h.h.Get("Upload-Length") {
		t.Fatalf("head completo: %d %v", h.status, h.h)
	}
	// Repetir el último PATCH (respuesta perdida) no rompe nada.
	if p := tec.patch(id, len(foto), nil); p.status != 204 {
		t.Fatalf("patch repetido: %d %s", p.status, p.crudo)
	}
	if n := e.contar("SELECT count(*) FROM nucleo.adjuntos_partes WHERE adjunto_id = $1", id); n != 0 {
		t.Fatalf("las partes debían borrarse: %d", n)
	}
	if n := e.contar("SELECT count(*) FROM integracion.outbox WHERE evento = 'adjunto.completado' AND payload->>'adjunto_id' = $1 AND empresa_id = 1", id.String()); n != 1 {
		t.Fatalf("adjunto.completado: %d", n)
	}
	if f, _ := adjuntos.Faltantes(ctx, e.pool, "prueba", e.o1, []adjuntos.Requisito{{Tipo: "foto", ClaveChecklist: "antena"}, {Tipo: "foto", Minimo: 2}}); strings.Join(f, ",") != "foto" {
		t.Fatalf("faltantes después: %v", f)
	}

	d := tec.pedir(http.MethodGet, adjuntos.RutaDescarga(id), nil)
	if d.status != 200 || !bytes.Equal(d.crudo, foto) || d.h.Get("Content-Type") != "image/png" ||
		d.h.Get("Content-Disposition") != `inline; filename="`+id.String()+`.png"` || d.h.Get("X-Content-Type-Options") != "nosniff" ||
		!strings.Contains(d.h.Get("Content-Security-Policy"), "sandbox") {
		t.Fatalf("descarga: %d %v", d.status, d.h)
	}
	if n := e.contar("SELECT count(*) FROM auditoria.acciones WHERE accion = 'adjuntos.ver' AND entidad_id = $1", id.String()); n != 1 {
		t.Fatalf("la descarga debe auditarse: %d", n)
	}
	l := tec.pedir(http.MethodGet, "/api/v1/adjuntos?entidad=prueba&entidadId="+e.o1, nil)
	if l.status != 200 || !strings.Contains(string(l.crudo), id.String()) {
		t.Fatalf("lista: %d %s", l.status, l.crudo)
	}
}

func TestAltaIdempotente(t *testing.T) {
	e := nuevoEntorno(t, adjuntos.Config{})
	tec := e.usuario("tecnico", 1)
	datos := imagenPNG(t, 8)
	a := alta(e.o1, "foto", "image/png", datos)
	r1 := tec.json(http.MethodPost, "/api/v1/adjuntos", a)
	r2 := tec.json(http.MethodPost, "/api/v1/adjuntos", a)
	if r1.status != 201 || r2.status != 200 || r1.cuerpo["urlSubida"] != r2.cuerpo["urlSubida"] || r2.cuerpo["id"] != a["id"] {
		t.Fatalf("alta repetida: %d %s / %d %s", r1.status, r1.crudo, r2.status, r2.crudo)
	}
	if n := e.contar("SELECT count(*) FROM integracion.outbox WHERE evento = 'adjunto.registrado' AND payload->>'adjunto_id' = $1", a["id"]); n != 1 {
		t.Fatalf("adjunto.registrado: %d", n)
	}
	// Mismo id con otros datos: 409.
	a["sha256"] = huella([]byte("otra cosa"))
	tec.json(http.MethodPost, "/api/v1/adjuntos", a).problema(t, 409, "adjunto_existente")
	// Con Idempotency-Key también.
	b, _ := json.Marshal(alta(e.o1, "firma", "image/png", datos))
	k1 := tec.pedir(http.MethodPost, "/api/v1/adjuntos", bytes.NewReader(b), "Content-Type", "application/json", "Idempotency-Key", "clave-1")
	k2 := tec.pedir(http.MethodPost, "/api/v1/adjuntos", bytes.NewReader(b), "Content-Type", "application/json", "Idempotency-Key", "clave-1")
	if k1.status != 201 || k2.status != 201 || !bytes.Equal(k1.crudo, k2.crudo) {
		t.Fatalf("idempotency-key: %d %d", k1.status, k2.status)
	}
}

func TestReanudacionTrasCorte(t *testing.T) {
	e := nuevoEntorno(t, adjuntos.Config{Segmento: 500})
	tec := e.usuario("tecnico", 1)
	foto := imagenPNG(t, 50)
	a := alta(e.o1, "foto", "image/png", foto)
	if r := tec.json(http.MethodPost, "/api/v1/adjuntos", a); r.status != 201 {
		t.Fatalf("alta: %d %s", r.status, r.crudo)
	}
	id := idDe(t, a)
	// El PATCH se corta tras 1700 bytes: la conexión TCP se cierra a mitad del cuerpo.
	u, _ := url.Parse(tec.base)
	conn, err := net.Dial("tcp", u.Host)
	if err != nil {
		t.Fatal(err)
	}
	var galletas []string
	for _, c := range tec.http.Jar.Cookies(u) {
		galletas = append(galletas, c.Name+"="+c.Value)
	}
	_, _ = fmt.Fprintf(conn, "PATCH %s HTTP/1.1\r\nHost: %s\r\nTus-Resumable: 1.0.0\r\nContent-Type: %s\r\nUpload-Offset: 0\r\n"+
		"Content-Length: %d\r\nX-CSRF-Token: %s\r\nCookie: %s\r\n\r\n",
		adjuntos.RutaSubida(id), u.Host, adjuntos.TipoParte, len(foto), tec.csrf, strings.Join(galletas, "; "))
	_, _ = conn.Write(foto[:1700])
	time.Sleep(100 * time.Millisecond)
	_ = conn.Close()
	// Lo recibido antes del corte quedó guardado (también el segmento incompleto).
	var offset int
	limite := time.Now().Add(5 * time.Second)
	for time.Now().Before(limite) {
		h := tec.head(id)
		offset, _ = strconv.Atoi(h.h.Get("Upload-Offset"))
		if offset == 1700 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if offset != 1700 {
		// Tres segmentos completos de 500 y el trozo de 200 que llegó antes del corte.
		t.Fatalf("tras el corte esperaba 1700 bytes guardados, hay %d", offset)
	}
	if p := tec.patch(id, offset, foto[offset:]); p.status != 204 || p.h.Get("Upload-Offset") != strconv.Itoa(len(foto)) {
		t.Fatalf("reanudar: %d %s", p.status, p.crudo)
	}
	d := tec.pedir(http.MethodGet, adjuntos.RutaDescarga(id), nil)
	if d.status != 200 || !bytes.Equal(d.crudo, foto) {
		t.Fatalf("descarga tras reanudar: %d", d.status)
	}
}

func TestSHA256IncorrectoRechaza(t *testing.T) {
	e := nuevoEntorno(t, adjuntos.Config{})
	tec := e.usuario("tecnico", 1)
	foto := imagenPNG(t, 10)
	a := alta(e.o1, "foto", "image/png", foto)
	a["sha256"] = huella([]byte("no es la foto"))
	tec.json(http.MethodPost, "/api/v1/adjuntos", a)
	id := idDe(t, a)
	tec.patch(id, 0, foto).problema(t, 422, "sha256_distinto")
	h := tec.head(id)
	if h.status != 410 {
		t.Fatalf("head de un rechazado: %d", h.status)
	}
	tec.patch(id, len(foto), nil).problema(t, 410, "adjunto_rechazado")
	if n := e.contar("SELECT count(*) FROM nucleo.adjuntos WHERE id = $1 AND estado = 'rechazado' AND motivo_rechazo = 'sha256_distinto'", id); n != 1 {
		t.Fatal("debía quedar rechazado")
	}
	if n := e.contar("SELECT count(*) FROM integracion.outbox WHERE evento = 'adjunto.rechazado' AND payload->>'adjunto_id' = $1", id.String()); n != 1 {
		t.Fatal("falta adjunto.rechazado")
	}
	tec.pedir(http.MethodGet, adjuntos.RutaDescarga(id), nil).problema(t, 409, "adjunto_incompleto")
	if f, _ := adjuntos.Faltantes(pruebas.ContextoSistema(e.pool), e.pool, "prueba", e.o1, []adjuntos.Requisito{{Tipo: "firma"}}); len(f) != 1 {
		t.Fatalf("un rechazado no cuenta: %v", f)
	}
}

func TestTamanoYMIME(t *testing.T) {
	e := nuevoEntorno(t, adjuntos.Config{TamanoMax: 4000})
	tec := e.usuario("tecnico", 1)
	grande := alta(e.o1, "foto", "image/png", make([]byte, 4001))
	tec.json(http.MethodPost, "/api/v1/adjuntos", grande).problema(t, 413, "tamano_excedido")
	tec.json(http.MethodPost, "/api/v1/adjuntos", alta(e.o1, "documento", "text/html", []byte("<html>"))).problema(t, 422, "mime_no_permitido")

	// Más bytes que los declarados: 413 y rechazado.
	foto := imagenPNG(t, 6)
	a := alta(e.o1, "foto", "image/png", foto)
	tec.json(http.MethodPost, "/api/v1/adjuntos", a)
	tec.patch(idDe(t, a), 0, append(append([]byte{}, foto...), 'x')).problema(t, 413, "tamano_excedido")
	if n := e.contar("SELECT count(*) FROM nucleo.adjuntos WHERE id = $1 AND estado = 'rechazado'", idDe(t, a)); n != 1 {
		t.Fatal("debía quedar rechazado por tamaño")
	}

	// Declara PNG pero es HTML: rechazado al completar.
	html := []byte("<!DOCTYPE html><html><script>alert(1)</script></html>")
	b := alta(e.o1, "foto", "image/png", html)
	tec.json(http.MethodPost, "/api/v1/adjuntos", b)
	tec.patch(idDe(t, b), 0, html).problema(t, 422, "mime_distinto")

	// Cabeceras de tus.
	c := alta(e.o1, "foto", "image/png", foto)
	tec.json(http.MethodPost, "/api/v1/adjuntos", c)
	tec.pedir(http.MethodPatch, adjuntos.RutaSubida(idDe(t, c)), bytes.NewReader(foto), "Content-Type", adjuntos.TipoParte, "Upload-Offset", "0").problema(t, 412, "tus_version")
	tec.pedir(http.MethodPatch, adjuntos.RutaSubida(idDe(t, c)), bytes.NewReader(foto), "Tus-Resumable", "1.0.0", "Content-Type", "image/png", "Upload-Offset", "0").problema(t, 415, "tipo_contenido")
}

func TestSinAccesoEs404(t *testing.T) {
	e := nuevoEntorno(t, adjuntos.Config{})
	tec1 := e.usuario("tecnico", 1)
	tec2 := e.usuario("tecnico", 2)
	lectura := e.usuario("lectura", 1)
	foto := imagenPNG(t, 6)

	// Entidad de otra empresa, inexistente o que no acepta subidas.
	tec1.json(http.MethodPost, "/api/v1/adjuntos", alta(e.o2, "foto", "image/png", foto)).problema(t, 404, "no_encontrado")
	tec1.json(http.MethodPost, "/api/v1/adjuntos", alta("nada", "foto", "image/png", foto)).problema(t, 404, "no_encontrado")
	tec1.json(http.MethodPost, "/api/v1/adjuntos", alta("cerrada", "foto", "image/png", foto)).problema(t, 422, "orden_cerrada")
	x := alta(e.o1, "foto", "image/png", foto)
	x["entidad"] = "ticket_inexistente"
	tec1.json(http.MethodPost, "/api/v1/adjuntos", x).problema(t, 422, "entidad_no_adjuntable")
	lectura.json(http.MethodPost, "/api/v1/adjuntos", alta(e.o1, "foto", "image/png", foto)).problema(t, 403, "prohibido")

	a := alta(e.o1, "foto", "image/png", foto)
	tec1.json(http.MethodPost, "/api/v1/adjuntos", a)
	id := idDe(t, a)
	// El técnico de la empresa 2 no ve ni sube al adjunto de la empresa 1.
	if h := tec2.head(id); h.status != 404 {
		t.Fatalf("head ajeno: %d", h.status)
	}
	tec2.patch(id, 0, foto).problema(t, 404, "no_encontrado")
	tec1.patch(id, 0, foto)
	tec2.pedir(http.MethodGet, adjuntos.RutaDescarga(id), nil).problema(t, 404, "no_encontrado")
	tec2.pedir(http.MethodGet, "/api/v1/adjuntos/no-es-uuid", nil).problema(t, 422, "validacion")
	lectura.pedir(http.MethodGet, adjuntos.RutaDescarga(id), nil).problema(t, 403, "prohibido")
	if d := tec1.pedir(http.MethodGet, adjuntos.RutaDescarga(id), nil); d.status != 200 {
		t.Fatalf("el dueño sí descarga: %d", d.status)
	}
	// Mismo id desde otra empresa: no se pisa.
	tec2.json(http.MethodPost, "/api/v1/adjuntos", map[string]any{"id": id.String(), "entidad": "prueba", "entidadId": e.o2,
		"tipo": "foto", "mime": "image/png", "tamano": len(foto), "sha256": huella(foto)}).problema(t, 409, "adjunto_existente")
}

func TestDescargaSeguraSVGYPDF(t *testing.T) {
	e := nuevoEntorno(t, adjuntos.Config{})
	tec := e.usuario("tecnico", 1)
	svg := []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script><path d="M0 0L10 10"/></svg>`)
	a := alta(e.o1, "firma", "image/svg+xml", svg)
	tec.json(http.MethodPost, "/api/v1/adjuntos", a)
	if p := tec.patch(idDe(t, a), 0, svg); p.status != 204 {
		t.Fatalf("svg: %d %s", p.status, p.crudo)
	}
	d := tec.pedir(http.MethodGet, adjuntos.RutaDescarga(idDe(t, a)), nil)
	if d.status != 200 || !strings.HasPrefix(d.h.Get("Content-Disposition"), "attachment;") || d.h.Get("X-Content-Type-Options") != "nosniff" ||
		d.h.Get("Content-Security-Policy") != "default-src 'none'; sandbox" {
		t.Fatalf("svg como descarga: %v", d.h)
	}
	pdf := []byte("%PDF-1.4\n1 0 obj<<>>endobj\ntrailer<<>>\n%%EOF")
	b := alta(e.o1, "documento", "application/pdf", pdf)
	tec.json(http.MethodPost, "/api/v1/adjuntos", b)
	tec.patch(idDe(t, b), 0, pdf)
	d = tec.pedir(http.MethodGet, adjuntos.RutaDescarga(idDe(t, b)), nil)
	if d.h.Get("Content-Type") != "application/pdf" || !strings.HasPrefix(d.h.Get("Content-Disposition"), "attachment;") {
		t.Fatalf("pdf: %v", d.h)
	}
}

func TestDiscoSinPathTraversal(t *testing.T) {
	d, err := adjuntos.NuevoDisco(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, clave := range []string{"../fuera", "a/../../b", "/etc/passwd", "A/B", "a//b", ""} {
		if err := d.Guardar(context.Background(), clave, strings.NewReader("x"), 1, "", ""); err == nil {
			t.Fatalf("la clave %q debía rechazarse", clave)
		}
	}
	if err := d.Guardar(context.Background(), "e1/orden/abc-1", strings.NewReader("hola"), 4, "", ""); err != nil {
		t.Fatal(err)
	}
	rc, err := d.Abrir(context.Background(), "e1/orden/abc-1")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(rc)
	_ = rc.Close()
	if string(b) != "hola" {
		t.Fatal(string(b))
	}
	if _, err := d.Abrir(context.Background(), "e1/orden/otro"); !errors.Is(err, adjuntos.ErrNoExiste) {
		t.Fatal(err)
	}
}

func TestFirmaV4VectorAWS(t *testing.T) {
	// Ejemplo «GET Object» de la documentación de AWS Signature Version 4 para S3.
	req, _ := http.NewRequest(http.MethodGet, "https://examplebucket.s3.amazonaws.com/test.txt", nil)
	req.Header.Set("Range", "bytes=0-9")
	req.Header.Set("X-Amz-Content-Sha256", "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855")
	adjuntos.FirmarV4(req, "AKIAIOSFODNN7EXAMPLE", "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY", "us-east-1", "s3",
		"e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855", time.Date(2013, 5, 24, 0, 0, 0, 0, time.UTC))
	esperada := "AWS4-HMAC-SHA256 Credential=AKIAIOSFODNN7EXAMPLE/20130524/us-east-1/s3/aws4_request, " +
		"SignedHeaders=host;range;x-amz-content-sha256;x-amz-date, Signature=f0e8bdb87c964420e857bd35b5d6ed310bd44f0170aba48dd91039c6036bdb41"
	if got := req.Header.Get("Authorization"); got != esperada {
		t.Fatalf("firma:\n %s\n %s", got, esperada)
	}
}

// s3Falso guarda objetos en memoria y exige una firma V4 y el SHA-256 correcto en el PUT.
type s3Falso struct {
	mu       sync.Mutex
	objetos  map[string][]byte
	llamadas []string
}

func (s *s3Falso) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.llamadas = append(s.llamadas, r.Method+" "+r.URL.Path)
	if !strings.HasPrefix(r.Header.Get("Authorization"), "AWS4-HMAC-SHA256 Credential=iris/") || r.Header.Get("X-Amz-Date") == "" {
		w.WriteHeader(http.StatusForbidden)
		_, _ = fmt.Fprint(w, "<Error><Code>AccessDenied</Code></Error>")
		return
	}
	switch r.Method {
	case http.MethodPut:
		b, _ := io.ReadAll(r.Body)
		if huella(b) != r.Header.Get("X-Amz-Content-Sha256") {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = fmt.Fprint(w, "<Error><Code>XAmzContentSHA256Mismatch</Code></Error>")
			return
		}
		s.objetos[r.URL.Path] = b
	case http.MethodGet:
		b, ok := s.objetos[r.URL.Path]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write(b)
	case http.MethodDelete:
		delete(s.objetos, r.URL.Path)
		w.WriteHeader(http.StatusNoContent)
	}
}

func TestAlmacenS3(t *testing.T) {
	falso := &s3Falso{objetos: map[string][]byte{}}
	srv := httptest.NewServer(falso)
	t.Cleanup(srv.Close)
	s3, err := adjuntos.NuevoS3(adjuntos.ConfigS3{Endpoint: srv.URL, Bucket: "iris-adjuntos", AccessKey: "iris", SecretKey: "secreto", EstiloRuta: true})
	if err != nil {
		t.Fatal(err)
	}
	e := nuevoEntorno(t, adjuntos.Config{Almacen: s3})
	tec := e.usuario("tecnico", 1)
	foto := imagenPNG(t, 12)
	a := alta(e.o1, "foto", "image/png", foto)
	tec.json(http.MethodPost, "/api/v1/adjuntos", a)
	id := idDe(t, a)
	if p := tec.patch(id, 0, foto); p.status != 204 {
		t.Fatalf("patch: %d %s", p.status, p.crudo)
	}
	if b := falso.objetos["/iris-adjuntos/e1/prueba/"+id.String()]; !bytes.Equal(b, foto) {
		t.Fatalf("objeto en S3: %v", falso.llamadas)
	}
	if d := tec.pedir(http.MethodGet, adjuntos.RutaDescarga(id), nil); d.status != 200 || !bytes.Equal(d.crudo, foto) {
		t.Fatalf("descarga desde S3: %d", d.status)
	}
	if err := s3.Borrar(context.Background(), "e1/prueba/"+id.String()); err != nil {
		t.Fatal(err)
	}
	if _, err := s3.Abrir(context.Background(), "e1/prueba/"+id.String()); !errors.Is(err, adjuntos.ErrNoExiste) {
		t.Fatal(err)
	}
}

func TestS3CaidoPermiteReintentarElCierre(t *testing.T) {
	falso := &s3Falso{objetos: map[string][]byte{}}
	caido := true
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if caido {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		falso.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	s3, _ := adjuntos.NuevoS3(adjuntos.ConfigS3{Endpoint: srv.URL, Bucket: "b", AccessKey: "iris", SecretKey: "s", EstiloRuta: true})
	e := nuevoEntorno(t, adjuntos.Config{Almacen: s3})
	tec := e.usuario("tecnico", 1)
	foto := imagenPNG(t, 9)
	a := alta(e.o1, "foto", "image/png", foto)
	tec.json(http.MethodPost, "/api/v1/adjuntos", a)
	id := idDe(t, a)
	tec.patch(id, 0, foto).problema(t, 503, "almacen_no_disponible")
	if h := tec.head(id); h.h.Get("Upload-Offset") != strconv.Itoa(len(foto)) {
		t.Fatalf("lo recibido se conserva: %v", h.h)
	}
	caido = false
	if p := tec.patch(id, len(foto), nil); p.status != 204 {
		t.Fatalf("reintento del cierre: %d %s", p.status, p.crudo)
	}
	if n := e.contar("SELECT count(*) FROM nucleo.adjuntos WHERE id = $1 AND estado = 'completo'", id); n != 1 {
		t.Fatal("debía completarse")
	}
}

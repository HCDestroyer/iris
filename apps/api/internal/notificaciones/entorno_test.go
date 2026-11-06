package notificaciones_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
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
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/HCDestroyer/iris/apps/api/internal/integracion"
	"github.com/HCDestroyer/iris/apps/api/internal/mensajeria"
	"github.com/HCDestroyer/iris/apps/api/internal/notificaciones"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/servidor"
)

// Pruebas de integración contra Postgres real (IRIS_DB=iris_test_i5) con un FCM falso (servidor de
// tokens OAuth y messages:send). El chat se prueba de punta a punta (HTTP de la mensajería → outbox
// → relay → envío); los eventos de la bandeja y de las órdenes se arman con el payload de
// contratos/eventos.md.

func TestMain(m *testing.M) { os.Exit(pruebas.Correr(m)) }

const proyecto = "iris-prueba"

// fcmFalso hace de servidor de tokens de Google y de FCM HTTP v1.
type fcmFalso struct {
	srv *httptest.Server
	mu  sync.Mutex
	// mensajes recibidos (el objeto message de cada POST).
	mensajes []notificaciones.MensajeFCM
	// respuesta por token del dispositivo: status y errorCode (vacío = 200).
	respuestas map[string]respuestaFCM
	tokens     int // tokens OAuth pedidos
	acceso     string
	rechazar   int // cuántos 401 responder antes de aceptar
}

type respuestaFCM struct {
	status int
	codigo string
}

var (
	claveRSA     *rsa.PrivateKey
	claveRSAPEM  string
	claveRSAOnce sync.Once
)

func clave(t *testing.T) (*rsa.PrivateKey, string) {
	t.Helper()
	claveRSAOnce.Do(func() {
		k, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			t.Fatal(err)
		}
		der, _ := x509.MarshalPKCS8PrivateKey(k)
		claveRSA, claveRSAPEM = k, string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
	})
	return claveRSA, claveRSAPEM
}

func nuevoFCM(t *testing.T) *fcmFalso {
	t.Helper()
	f := &fcmFalso{respuestas: map[string]respuestaFCM{}, acceso: "acceso-1"}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		switch r.URL.Path {
		case "/token":
			_ = r.ParseForm()
			if r.Form.Get("grant_type") != "urn:ietf:params:oauth:grant-type:jwt-bearer" || strings.Count(r.Form.Get("assertion"), ".") != 2 {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			f.tokens++
			f.acceso = fmt.Sprintf("acceso-%d", f.tokens)
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": f.acceso, "expires_in": 3600, "token_type": "Bearer"})
		case "/v1/projects/" + proyecto + "/messages:send":
			if f.rechazar > 0 || r.Header.Get("Authorization") != "Bearer "+f.acceso {
				f.rechazar--
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error":{"code":401,"status":"UNAUTHENTICATED"}}`))
				return
			}
			var c struct {
				Message notificaciones.MensajeFCM `json:"message"`
			}
			if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			if res, ok := f.respuestas[c.Message.Token]; ok && res.status != 0 {
				w.WriteHeader(res.status)
				_, _ = fmt.Fprintf(w, `{"error":{"code":%d,"status":"X","details":[{"@type":"type.googleapis.com/google.firebase.fcm.v1.FcmError","errorCode":%q}]}}`,
					res.status, res.codigo)
				return
			}
			f.mensajes = append(f.mensajes, c.Message)
			_, _ = fmt.Fprintf(w, `{"name":"projects/%s/messages/%d"}`, proyecto, len(f.mensajes))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

// para devuelve los mensajes que recibió un token.
func (f *fcmFalso) para(token string) []notificaciones.MensajeFCM {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []notificaciones.MensajeFCM
	for _, m := range f.mensajes {
		if m.Token == token {
			out = append(out, m)
		}
	}
	return out
}

func (f *fcmFalso) responder(token string, status int, codigo string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.respuestas[token] = respuestaFCM{status: status, codigo: codigo}
}

func (f *fcmFalso) cliente(t *testing.T) *notificaciones.ClienteFCM {
	k, _ := clave(t)
	return notificaciones.NuevoClienteFCM(notificaciones.OpcionesFCM{Proyecto: proyecto, URL: f.srv.URL, Clave: k,
		Cuenta: notificaciones.CuentaServicio{Tipo: "service_account", Correo: "push@iris-prueba.iam.gserviceaccount.com", TokenURI: f.srv.URL + "/token"}})
}

type entorno struct {
	t      *testing.T
	bd     nucleo.BD
	ctx    context.Context
	h      http.Handler
	notif  *notificaciones.Modulo
	fcm    *fcmFalso
	relay  *integracion.Relay
	mu     sync.Mutex
	envios []int64
	reloj  time.Time
}

var empresasUnaVez sync.Once

var ident = identidad.Nuevo(identidad.Config{})

// nuevoEntorno arma la API (identidad, mensajería y notificaciones) y el emisor con el FCM falso
// (sin él si conFCM es false). El reloj del emisor: viernes 6 de nov, 10:00 en Guatemala.
func nuevoEntorno(t *testing.T, conFCM bool) *entorno {
	t.Helper()
	pool := pruebas.Pool(t)
	ctx := pruebas.ContextoSistema(pool)
	empresasUnaVez.Do(func() {
		if _, err := pool.Exec(ctx, `INSERT INTO nucleo.empresas (id, nombre, origen) VALUES (1, 'Empresa 1', 'argos'), (2, 'Empresa 2', 'argos') ON CONFLICT DO NOTHING`); err != nil {
			t.Fatal(err)
		}
	})
	// Lo que dejaron otras pruebas no se reparte en esta.
	if _, err := pool.Exec(ctx, `UPDATE integracion.outbox SET publicado_en = now() WHERE publicado_en IS NULL`); err != nil {
		t.Fatal(err)
	}
	e := &entorno{t: t, bd: pool, ctx: ctx, reloj: time.Date(2026, 11, 6, 16, 0, 0, 0, time.UTC)}
	cfg := notificaciones.Config{
		Reloj: func() time.Time { return e.reloj },
		Log:   slog.New(slog.NewTextHandler(io.Discard, nil)),
		Encolar: func(_ context.Context, _ pgx.Tx, a river.JobArgs) error {
			e.mu.Lock()
			defer e.mu.Unlock()
			e.envios = append(e.envios, a.(notificaciones.ArgsEnviar).EnvioID)
			return nil
		},
	}
	if conFCM {
		e.fcm = nuevoFCM(t)
		cfg.FCM = e.fcm.cliente(t)
	}
	e.notif = notificaciones.Nuevo(cfg)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	msj := mensajeria.Nuevo(mensajeria.Config{})
	e.h, _ = servidor.Nuevo(servidor.Deps{BD: pool, Log: log, Version: "prueba", Identidad: ident,
		Idempotencia:       nucleo.OpcionesIdempotencia{Omitir: mensajeria.IdempotenciaPropia},
		Modulos:            []func(huma.API){msj.Registrar, e.notif.Registrar},
		NotificacionesPush: func() string { return e.notif.Estado().Estado }})
	bus := integracion.NuevoBus()
	e.notif.Suscribir(bus, pool)
	e.relay = &integracion.Relay{BD: pool, Bus: bus, Log: log}
	return e
}

// procesar corre el relay del outbox y manda los envíos encolados. Devuelve cuántos encoló.
func (e *entorno) procesar() int {
	e.t.Helper()
	if _, err := e.relay.Ejecutar(e.ctx); err != nil {
		e.t.Fatal(err)
	}
	return e.enviarEncolados()
}

func (e *entorno) enviarEncolados() int {
	e.t.Helper()
	e.mu.Lock()
	ids := e.envios
	e.envios = nil
	e.mu.Unlock()
	for _, id := range ids {
		if err := e.notif.Enviar(e.ctx, id, false); err != nil {
			e.t.Fatalf("enviar %d: %v", id, err)
		}
	}
	return len(ids)
}

// evento simula un evento del outbox con el payload del contrato y lo pasa por el emisor.
func (e *entorno) evento(nombre string, empresa *int64, requestID string, payload map[string]any) notificaciones.EventoPush {
	e.t.Helper()
	b, _ := json.Marshal(payload)
	ev := notificaciones.EventoPush{ID: uuid.Must(uuid.NewV7()), Evento: nombre, OcurridoEn: time.Now().UTC(), EmpresaID: empresa,
		Payload: b, RequestID: requestID}
	e.repetir(ev)
	return ev
}

func (e *entorno) repetir(ev notificaciones.EventoPush) {
	e.t.Helper()
	if _, err := e.notif.PrepararEnvios.Ejecutar(e.ctx, ev); err != nil {
		e.t.Fatal(err)
	}
}

func emp(id int64) *int64 { return &id }

// persona -------------------------------------------------------------------------------------

type persona struct {
	id      int64
	nombre  string
	correo  string
	cookie  string
	csrf    string
	token   string // iris_pat_ (si se usa en lugar de la sesión)
	empresa int64
}

func (e *entorno) persona(empresas []int64, roles ...string) persona {
	e.t.Helper()
	correo := pruebas.Correo("push")
	nombre := "Persona " + pruebas.Unico()
	var rs []identidad.AsignacionRol
	for _, r := range roles {
		rs = append(rs, identidad.AsignacionRol{Rol: r})
	}
	u, err := ident.CrearUsuario.Ejecutar(e.ctx, identidad.EntradaCrearUsuario{Nombre: nombre, Correo: correo,
		Contrasena: "contrasena-larga-1", Roles: rs, Empresas: empresas})
	if err != nil {
		e.t.Fatal(err)
	}
	p := persona{id: u.ID, nombre: nombre, correo: correo}
	if len(empresas) > 0 {
		p.empresa = empresas[0]
	}
	return e.entrar(p)
}

// entrar abre una sesión nueva (como la app: cookie + CSRF).
func (e *entorno) entrar(p persona) persona {
	e.t.Helper()
	s, err := ident.IniciarSesion.Ejecutar(nucleo.ConBD(context.Background(), e.bd), identidad.EntradaIniciarSesion{Correo: p.correo, Contrasena: "contrasena-larga-1"})
	if err != nil {
		e.t.Fatal(err)
	}
	p.cookie, p.csrf = s.Token, s.CSRFToken
	return p
}

type respuesta struct {
	status int
	cuerpo map[string]any
	crudo  string
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
	switch {
	case p.token != "":
		req.Header.Set("Authorization", "Bearer "+p.token)
	case p.cookie != "":
		req.AddCookie(&http.Cookie{Name: identidad.CookieSesion, Value: p.cookie})
		req.AddCookie(&http.Cookie{Name: identidad.CookieCSRF, Value: p.csrf})
		req.Header.Set(identidad.CabeceraCSRF, p.csrf)
	}
	if p.empresa != 0 {
		req.Header.Set(identidad.CabeceraEmpresa, fmt.Sprint(p.empresa))
	}
	w := httptest.NewRecorder()
	e.h.ServeHTTP(w, req)
	r := respuesta{status: w.Code, crudo: w.Body.String()}
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

// dispositivo registra un teléfono de p con un token FCM nuevo y lo devuelve.
func (e *entorno) dispositivo(p persona) string {
	e.t.Helper()
	token := "fcm-" + strings.ReplaceAll(uuid.NewString(), "-", "") + ":APA91b" + pruebas.Unico()
	e.esperar(e.api(p, http.MethodPost, "/api/v1/dispositivos", map[string]any{"token": token, "plataforma": "android", "appVersion": "1.4.0"}), http.StatusCreated)
	return token
}

func (e *entorno) preferencias(p persona, cambiar func(map[string]any)) {
	e.t.Helper()
	r := e.esperar(e.api(p, http.MethodGet, "/api/v1/yo/notificaciones", nil), http.StatusOK)
	c := r.cuerpo
	delete(c, "push")
	delete(c, "bandejaDisponible")
	cambiar(c)
	e.esperar(e.api(p, http.MethodPost, "/api/v1/yo/notificaciones", c), http.StatusOK)
}

// Chat ----------------------------------------------------------------------------------------

func (e *entorno) directa(de, con persona) int64 {
	e.t.Helper()
	r := e.api(de, http.MethodPost, "/api/v1/mensajeria/chat/directas", map[string]any{"usuario_id": con.id})
	if r.status != http.StatusCreated && r.status != http.StatusOK {
		e.t.Fatalf("directa: %d %s", r.status, r.crudo)
	}
	return int64(r.cuerpo["id"].(float64))
}

func (e *entorno) mensaje(de persona, conv int64, cuerpo map[string]any) string {
	e.t.Helper()
	id := uuid.Must(uuid.NewV7()).String()
	cuerpo["id"] = id
	e.esperar(e.api(de, http.MethodPost, fmt.Sprintf("/api/v1/mensajeria/chat/conversaciones/%d/mensajes", conv), cuerpo), http.StatusCreated)
	return id
}

// datos lee el JSON de la clave datos de un mensaje.
func datos(t *testing.T, m notificaciones.MensajeFCM) map[string]any {
	t.Helper()
	var d map[string]any
	if err := json.Unmarshal([]byte(m.Data["datos"]), &d); err != nil {
		t.Fatalf("datos: %v (%q)", err, m.Data["datos"])
	}
	return d
}

func (e *entorno) envio(eventoID uuid.UUID) (estado, motivo string) {
	e.t.Helper()
	var m *string
	if err := e.bd.QueryRow(e.ctx, `SELECT estado, motivo FROM notificaciones.envios WHERE evento_id = $1 ORDER BY id LIMIT 1`, eventoID).Scan(&estado, &m); err != nil {
		e.t.Fatal(err)
	}
	if m != nil {
		motivo = *m
	}
	return estado, motivo
}

func (e *entorno) contar(sql string, args ...any) int {
	e.t.Helper()
	var n int
	if err := e.bd.QueryRow(e.ctx, sql, args...).Scan(&n); err != nil {
		e.t.Fatal(err)
	}
	return n
}

// ultimoEvento devuelve el payload del último evento del outbox con ese nombre.
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

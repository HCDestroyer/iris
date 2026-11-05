package mensajeria_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/HCDestroyer/iris/apps/api/internal/integracion"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/conector"
	"github.com/HCDestroyer/iris/apps/api/internal/mensajeria"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/servidor"
)

func TestMain(m *testing.M) { os.Exit(pruebas.Correr(m)) }

const claveCifrado = "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY="

// receptorArgos es el receptor de webhooks de Argos (§6.6): verifica la firma y guarda lo recibido.
type receptorArgos struct {
	srv      *httptest.Server
	secreto  string
	mu       sync.Mutex
	recibido []webhookRecibido
	fallar   atomic.Int32
}

type webhookRecibido struct {
	ID     string
	Cuerpo map[string]any
	Firma  bool
}

// nuevoReceptor: lo comparten las pruebas del paquete (el conector activo es uno); vive hasta el final.
func nuevoReceptor() *receptorArgos {
	r := &receptorArgos{secreto: "secreto-de-entrada-de-argos-0123456789"}
	r.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if r.fallar.Load() > 0 {
			r.fallar.Add(-1)
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		crudo, _ := io.ReadAll(req.Body)
		var c map[string]any
		dec := json.NewDecoder(bytes.NewReader(crudo))
		dec.UseNumber()
		_ = dec.Decode(&c)
		ok := conector.FirmaValida(r.secreto, req.Header.Get("X-Webhook-Signature"), req.Header.Get("X-Webhook-Timestamp"), crudo)
		r.mu.Lock()
		r.recibido = append(r.recibido, webhookRecibido{ID: req.Header.Get("X-Webhook-Id"), Cuerpo: c, Firma: ok})
		r.mu.Unlock()
		if c["id"] != req.Header.Get("X-Webhook-Id") {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	return r
}

func (r *receptorArgos) de(evento, campo string, valor any) []webhookRecibido {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []webhookRecibido
	for _, w := range r.recibido {
		p, _ := w.Cuerpo["payload"].(map[string]any)
		if w.Cuerpo["evento"] == evento && fmt.Sprint(p[campo]) == fmt.Sprint(valor) {
			out = append(out, w)
		}
	}
	return out
}

// metaFalso es la Graph API: responde un wamid a cada plantilla.
type metaFalso struct {
	srv    *httptest.Server
	mu     sync.Mutex
	envios []map[string]any
	error  atomic.Int32 // código de error de Meta a responder (0 = ninguno)
}

func nuevoMeta(t *testing.T) *metaFalso {
	f := &metaFalso{}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Header.Get("Authorization") != "Bearer token-de-meta-0123456789abcdef" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		var c map[string]any
		_ = json.NewDecoder(req.Body).Decode(&c)
		if code := f.error.Load(); code != 0 {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = fmt.Fprintf(w, `{"error":{"code":%d,"error_user_title":"x"}}`, code)
			return
		}
		f.mu.Lock()
		f.envios = append(f.envios, c)
		n := len(f.envios)
		f.mu.Unlock()
		_, _ = fmt.Fprintf(w, `{"messages":[{"id":"wamid.%d.%s"}]}`, n, uuid.NewString())
	}))
	t.Cleanup(f.srv.Close)
	return f
}

type contactosFalsos struct {
	mu sync.Mutex
	c  map[int64]argos.ContactoArgos
}

func (f *contactosFalsos) ContactoCliente(_ context.Context, id int64) (argos.ContactoArgos, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.c[id], nil
}

type entorno struct {
	t         *testing.T
	m         *mensajeria.Modulo
	h         http.Handler
	bd        nucleo.BD
	ctx       context.Context
	reloj     *time.Time
	contactos *contactosFalsos
	jobs      *[]river.JobArgs
	receptor  *receptorArgos
	meta      *metaFalso
	token     string // integración Argos con la empresa 1
	otro      string // otra integración Argos (empresas 1 y 2)
	relay     *integracion.Relay
	conector  int64
}

var (
	unaVez       sync.Once
	conectorID   int64
	receptorComp *receptorArgos
)

func nuevoEntorno(t *testing.T, ajustar func(*mensajeria.Config)) *entorno {
	t.Helper()
	pool := pruebas.Pool(t)
	ctx := pruebas.ContextoSistema(pool)
	cif, err := nucleo.NuevoCifrador(claveCifrado)
	if err != nil {
		t.Fatal(err)
	}
	unaVez.Do(func() {
		if _, err := pool.Exec(ctx, `INSERT INTO nucleo.empresas (id, nombre, origen) VALUES (1, 'Empresa 1', 'argos'), (2, 'Empresa 2', 'argos') ON CONFLICT DO NOTHING`); err != nil {
			t.Fatal(err)
		}
		receptorComp = nuevoReceptor()
		if err := pool.QueryRow(ctx, `INSERT INTO integracion.conectores (sistema, base_url, origen, token_cifrado, secreto_webhook_cifrado, estado, contrato_remoto)
			VALUES ('argosisp', 'http://argos.prueba', 'enlace_remoto', '\x00', '\x00', 'activo', '0.6.0') RETURNING id`).Scan(&conectorID); err != nil {
			t.Fatal(err)
		}
		sec, err := cif.Cifrar([]byte(receptorComp.secreto), conector.DatosAsociados(conectorID, conector.CampoWebhookSalida))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `UPDATE integracion.conectores SET webhook_salida_url = $2, webhook_salida_secreto_cifrado = $3 WHERE id = $1`,
			conectorID, receptorComp.srv.URL+"/api/v1/integraciones/iris/webhook", sec); err != nil {
			t.Fatal(err)
		}
	})
	// 10:00 en Guatemala (fuera del horario de silencio).
	reloj := time.Date(2026, 11, 5, 16, 0, 0, 0, time.UTC)
	jobs := &[]river.JobArgs{}
	contactos := &contactosFalsos{c: map[int64]argos.ContactoArgos{}}
	meta := nuevoMeta(t)
	cfg := mensajeria.Config{
		Cifrador: cif, Contactos: contactos, Simulado: true, MetaURL: meta.srv.URL, Version: "prueba",
		HTTPArgos: conector.NuevoHTTP([]netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")}),
		Ahora:     func() time.Time { return reloj },
		Encolar: func(_ context.Context, _ pgx.Tx, a river.JobArgs) error {
			*jobs = append(*jobs, a)
			return nil
		},
	}
	if ajustar != nil {
		ajustar(&cfg)
	}
	e := &entorno{t: t, bd: pool, ctx: ctx, contactos: contactos, jobs: jobs, receptor: receptorComp, meta: meta, conector: conectorID}
	e.reloj = &reloj
	cfg.Ahora = func() time.Time { return *e.reloj }
	e.m = mensajeria.Nuevo(cfg)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	e.h, _ = servidor.Nuevo(servidor.Deps{BD: pool, Log: log, Version: "prueba",
		Idempotencia: nucleo.OpcionesIdempotencia{Omitir: mensajeria.IdempotenciaPropia}, Modulos: []func(huma.API){e.m.Registrar}})
	id := identidad.Nuevo(identidad.Config{})
	r, err := id.PrepararIntegracion(ctx, identidad.EntradaPrepararIntegracion{Sistema: "argosisp", Empresas: []int64{1}, Correo: pruebas.Correo("argos")})
	if err != nil {
		t.Fatal(err)
	}
	e.token = r.Token.Valor
	r2, err := id.PrepararIntegracion(ctx, identidad.EntradaPrepararIntegracion{Sistema: "argosisp", Empresas: []int64{1, 2}, Correo: pruebas.Correo("argos2")})
	if err != nil {
		t.Fatal(err)
	}
	e.otro = r2.Token.Valor
	bus := integracion.NuevoBus()
	e.m.SuscribirWebhooks(bus, pool)
	e.relay = &integracion.Relay{BD: pool, Bus: bus, Log: log}
	return e
}

var siguienteCliente atomic.Int64

func init() { siguienteCliente.Store(time.Now().UnixNano() % 1_000_000_000) }

// cliente crea un cliente de Argos de la empresa con contacto.
func (e *entorno) cliente(empresa int64, movil string) int64 {
	id := siguienteCliente.Add(1)
	e.contactos.mu.Lock()
	e.contactos.c[id] = argos.ContactoArgos{Existe: true, Empresas: []int64{empresa}, Movil: movil, Servicios: map[int64]int64{id * 10: empresa}}
	e.contactos.mu.Unlock()
	return id
}

type respuesta struct {
	status int
	cuerpo map[string]any
	h      http.Header
}

func (e *entorno) pedir(metodo, ruta, token, clave string, cuerpo any) respuesta {
	e.t.Helper()
	var b io.Reader
	if cuerpo != nil {
		crudo, _ := json.Marshal(cuerpo)
		b = bytes.NewReader(crudo)
	}
	req := httptest.NewRequest(metodo, ruta, b)
	if b != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if clave != "" {
		req.Header.Set("Idempotency-Key", clave)
	}
	w := httptest.NewRecorder()
	e.h.ServeHTTP(w, req)
	r := respuesta{status: w.Code, h: w.Header()}
	_ = json.Unmarshal(w.Body.Bytes(), &r.cuerpo)
	return r
}

func (e *entorno) consentir(cliente int64, estado string, cuando time.Time) respuesta {
	return e.pedir(http.MethodPost, "/api/v1/mensajeria/consentimientos", e.token,
		fmt.Sprintf("argos:consentimiento:1:%d:whatsapp:%s:%s", cliente, cuando.Format(time.RFC3339), estado),
		map[string]any{"empresa_id": 1, "cliente_id": cliente, "canal": "whatsapp", "estado": estado, "origen": "argos_panel", "ocurrido_en": cuando})
}

func avisoPorVencer(cliente int64, vence string) map[string]any {
	return map[string]any{"empresa_id": 1, "cliente_id": cliente, "servicio_id": cliente * 10, "plantilla": "factura_por_vencer",
		"variables": map[string]string{"monto": "250.00", "moneda": "GTQ", "vence_en": vence}, "canal_preferido": "whatsapp", "prioridad": "normal"}
}

func (e *entorno) avisar(clave string, cuerpo map[string]any) respuesta {
	return e.pedir(http.MethodPost, "/api/v1/mensajeria/avisos", e.token, clave, cuerpo)
}

func (e *entorno) exigir(r respuesta, status int, campos ...string) {
	e.t.Helper()
	if r.status != status {
		e.t.Fatalf("status %d (esperaba %d): %v", r.status, status, r.cuerpo)
	}
	for i := 0; i+1 < len(campos); i += 2 {
		if got := fmt.Sprint(r.cuerpo[campos[i]]); got != campos[i+1] {
			e.t.Fatalf("%s = %q (esperaba %q): %v", campos[i], got, campos[i+1], r.cuerpo)
		}
	}
}

func (e *entorno) enviar(id string) time.Duration {
	e.t.Helper()
	espera, err := e.m.ProcesarEnvio(e.ctx, uuid.MustParse(id), false)
	if err != nil {
		e.t.Fatal(err)
	}
	return espera
}

// publicar corre el relay del outbox y el despacho de webhooks hacia Argos.
func (e *entorno) publicar() {
	e.t.Helper()
	if _, err := e.relay.Ejecutar(e.ctx); err != nil {
		e.t.Fatal(err)
	}
	if err := e.m.DespacharWebhooks(e.ctx, e.bd); err != nil {
		e.t.Fatal(err)
	}
}

func (e *entorno) contar(sql string, args ...any) int {
	e.t.Helper()
	var n int
	if err := e.bd.QueryRow(e.ctx, sql, args...).Scan(&n); err != nil {
		e.t.Fatal(err)
	}
	return n
}

// Pruebas -----------------------------------------------------------------------------------

func TestAvisoEntregadoConUnSoloWebhook(t *testing.T) {
	e := nuevoEntorno(t, nil)
	c := e.cliente(1, "5512-3344")
	e.exigir(e.consentir(c, "acepta", e.reloj.Add(-time.Hour)), http.StatusCreated, "estado_vigente", "acepta")

	clave := fmt.Sprintf("argos:aviso:factura_por_vencer:%d", c)
	r := e.avisar(clave, avisoPorVencer(c, "2026-11-10"))
	e.exigir(r, http.StatusAccepted, "estado", "en_cola")
	if _, hay := r.cuerpo["motivo"]; hay {
		t.Fatalf("en_cola sin motivo: %v", r.cuerpo)
	}
	id := r.cuerpo["aviso_id"].(string)
	if len(*e.jobs) == 0 {
		t.Fatal("no se encoló el envío")
	}

	// Repetir la clave: mismo aviso, sin otro job.
	jobs := len(*e.jobs)
	r2 := e.avisar(clave, avisoPorVencer(c, "2026-11-10"))
	e.exigir(r2, http.StatusAccepted, "aviso_id", id, "estado", "en_cola")
	if r2.h.Get("Idempotent-Replayed") != "true" || len(*e.jobs) != jobs {
		t.Fatalf("la repetición debe devolver el mismo aviso sin encolar: %v %d", r2.h, len(*e.jobs))
	}
	// La misma clave con otro cuerpo: 422.
	e.exigir(e.avisar(clave, avisoPorVencer(c, "2026-11-11")), http.StatusUnprocessableEntity, "type", "urn:iris:problema:idempotencia_reutilizada")

	if espera := e.enviar(id); espera != 0 {
		t.Fatalf("no debía esperar: %v", espera)
	}
	// Repetir con la misma clave devuelve el estado actual.
	e.exigir(e.avisar(clave, avisoPorVencer(c, "2026-11-10")), http.StatusAccepted, "aviso_id", id, "estado", "entregado")

	e.publicar()
	e.publicar() // un segundo despacho no repite
	w := e.receptor.de("aviso.entregado", "aviso_id", id)
	if len(w) != 1 {
		t.Fatalf("webhooks aviso.entregado: %d", len(w))
	}
	if !w[0].Firma || w[0].ID != w[0].Cuerpo["id"] {
		t.Fatalf("firma o X-Webhook-Id: %+v", w[0])
	}
	p := w[0].Cuerpo["payload"].(map[string]any)
	if p["idempotency_key"] != clave || fmt.Sprint(p["empresa_id"]) != "1" || p["canal"] != "whatsapp" || p["entregado_en"] == nil {
		t.Fatalf("payload: %v", p)
	}
	if _, ok := p["leido_en"]; !ok || len(p) != 6 {
		t.Fatalf("payload con los campos del contrato (leido_en null): %v", p)
	}
	if len(e.receptor.de("aviso.fallido", "aviso_id", id)) != 0 {
		t.Fatal("un aviso entregado no emite aviso.fallido")
	}

	d := e.pedir(http.MethodGet, "/api/v1/mensajeria/avisos/"+id, e.token, "", nil)
	e.exigir(d, http.StatusOK, "estado", "entregado", "idempotency_key", clave, "plantilla", "factura_por_vencer", "canal", "whatsapp")
	for _, k := range []string{"servicio_id", "motivo", "detalle", "creado_en", "entregado_en", "leido_en"} {
		if _, ok := d.cuerpo[k]; !ok {
			t.Fatalf("falta %s en GET: %v", k, d.cuerpo)
		}
	}
	// Otra integración no lo ve.
	e.exigir(e.pedir(http.MethodGet, "/api/v1/mensajeria/avisos/"+id, e.otro, "", nil), http.StatusNotFound)
	e.exigir(e.pedir(http.MethodGet, "/api/v1/mensajeria/avisos/no-es-uuid", e.token, "", nil), http.StatusNotFound)
	// Si la integración pierde la empresa del aviso, tampoco lo ve.
	e.exigir(e.pedir(http.MethodGet, "/api/v1/mensajeria/avisos/"+id, e.token, "", nil), http.StatusOK)
	var usuario int64
	if err := e.bd.QueryRow(e.ctx, `SELECT usuario_id FROM mensajeria.avisos WHERE id = $1`, id).Scan(&usuario); err != nil {
		t.Fatal(err)
	}
	if _, err := e.bd.Exec(e.ctx, `UPDATE nucleo.usuarios_empresas SET empresa_id = 2 WHERE usuario_id = $1 AND empresa_id = 1`, usuario); err != nil {
		t.Fatal(err)
	}
	e.exigir(e.pedir(http.MethodGet, "/api/v1/mensajeria/avisos/"+id, e.token, "", nil), http.StatusNotFound)
	if _, err := e.bd.Exec(e.ctx, `UPDATE nucleo.usuarios_empresas SET empresa_id = 1 WHERE usuario_id = $1 AND empresa_id = 2`, usuario); err != nil {
		t.Fatal(err)
	}

	// Auditoría y outbox.
	if n := e.contar(`SELECT count(*) FROM auditoria.acciones WHERE accion = 'mensajeria.enviar' AND entidad = 'aviso' AND entidad_id = $1`, id); n != 1 {
		t.Fatalf("auditoría de mensajeria.enviar: %d", n)
	}
	if n := e.contar(`SELECT count(*) FROM integracion.outbox WHERE evento IN ('aviso.creado','aviso.enviado','aviso.entregado') AND payload->>'aviso_id' = $1`, id); n != 3 {
		t.Fatalf("eventos del aviso: %d", n)
	}
	if n := e.contar(`SELECT count(*) FROM auditoria.acciones WHERE accion = 'mensajeria.entregar_webhook' AND entrada::text LIKE '%entregado%'`); n == 0 {
		t.Fatal("la entrega del webhook no quedó auditada")
	}
}

func TestBloqueos(t *testing.T) {
	e := nuevoEntorno(t, nil)
	clave := func(c int64, s string) string { return fmt.Sprintf("argos:aviso:factura_por_vencer:%d%s", c, s) }

	// Sin permiso registrado.
	c1 := e.cliente(1, "5512-3344")
	e.exigir(e.avisar(clave(c1, ""), avisoPorVencer(c1, "2026-11-10")), http.StatusAccepted, "estado", "bloqueado", "motivo", "sin_permiso")

	// Baja más nueva que un acepta más viejo (que llega después): manda la baja.
	c2 := e.cliente(1, "5512-3345")
	e.exigir(e.consentir(c2, "baja", e.reloj.Add(-time.Hour)), http.StatusCreated, "estado_vigente", "baja")
	e.exigir(e.consentir(c2, "acepta", e.reloj.Add(-48*time.Hour)), http.StatusCreated, "estado_vigente", "baja")
	e.exigir(e.avisar(clave(c2, ""), avisoPorVencer(c2, "2026-11-10")), http.StatusAccepted, "estado", "bloqueado", "motivo", "baja")
	// Un acepta más nuevo la reemplaza.
	e.exigir(e.consentir(c2, "acepta", e.reloj.Add(-time.Minute)), http.StatusCreated, "estado_vigente", "acepta")

	// Sin teléfono válido.
	c3 := e.cliente(1, "sin número")
	e.consentir(c3, "acepta", e.reloj.Add(-time.Hour))
	e.exigir(e.avisar(clave(c3, ""), avisoPorVencer(c3, "2026-11-10")), http.StatusAccepted, "estado", "bloqueado", "motivo", "sin_contacto")

	// Límite diario: 3 automáticos; el cuarto, bloqueado; urgente (corte_programado) lo salta.
	c4 := e.cliente(1, "5512-3346")
	e.consentir(c4, "acepta", e.reloj.Add(-time.Hour))
	for i := range 3 {
		e.exigir(e.avisar(clave(c4, fmt.Sprint(i)), avisoPorVencer(c4, "2026-11-10")), http.StatusAccepted, "estado", "en_cola")
	}
	e.exigir(e.avisar(clave(c4, "x"), avisoPorVencer(c4, "2026-11-10")), http.StatusAccepted, "estado", "bloqueado", "motivo", "limite_diario")
	// Urgente solo lo pide Iris (actor sistema aquí): salta el límite diario.
	u, err := e.m.EnviarAviso.Ejecutar(e.ctx, mensajeria.EntradaAviso{EmpresaID: 1, ClienteID: c4, Plantilla: "corte_programado", Prioridad: "urgente",
		Variables: map[string]string{"inicio": "2026-11-06T22:00", "fin": "2026-11-07T02:00", "zona": "Centro"}, IdempotencyKey: "iris:corte:1"})
	if err != nil || u.Estado != "en_cola" {
		t.Fatalf("urgente de Iris: %+v %v", u, err)
	}
	// Al otro día el límite vuelve a empezar.
	*e.reloj = e.reloj.Add(24 * time.Hour)
	e.exigir(e.avisar(clave(c4, "y"), avisoPorVencer(c4, "2026-11-10")), http.StatusAccepted, "estado", "en_cola")

	// Sin canal configurado y sin simulado (producción): canal_no_disponible.
	p := nuevoEntorno(t, func(c *mensajeria.Config) { c.Simulado = false })
	c5 := p.cliente(1, "5512-3347")
	p.consentir(c5, "acepta", p.reloj.Add(-time.Hour))
	p.exigir(p.avisar(clave(c5, ""), avisoPorVencer(c5, "2026-11-10")), http.StatusAccepted, "estado", "bloqueado", "motivo", "canal_no_disponible")
	l := p.pedir(http.MethodGet, "/api/v1/mensajeria/plantillas", p.token, "", nil)
	p.exigir(l, http.StatusOK)
	if n := len(l.cuerpo["data"].([]any)); n != len(mensajeria.Catalogo) {
		t.Fatalf("plantillas: %d", n)
	}
	// Los bloqueados no emiten webhooks.
	e.publicar()
	if n := e.contar(`SELECT count(*) FROM mensajeria.webhooks_salida WHERE payload->>'aviso_id' IN (SELECT id::text FROM mensajeria.avisos WHERE estado = 'bloqueado')`); n != 0 {
		t.Fatalf("webhooks de bloqueados: %d", n)
	}
}

func TestValidaciones(t *testing.T) {
	e := nuevoEntorno(t, nil)
	c := e.cliente(1, "5512-3344")
	e.consentir(c, "acepta", e.reloj.Add(-time.Hour))

	desconocida := avisoPorVencer(c, "2026-11-10")
	desconocida["variables"] = map[string]string{"monto": "250.00", "moneda": "GTQ", "vence_en": "2026-11-10", "telefono": "55123344"}
	e.exigir(e.avisar("argos:aviso:factura_por_vencer:1", desconocida), http.StatusUnprocessableEntity, "type", "urn:iris:problema:variable_desconocida")

	conNumero := avisoPorVencer(c, "2026-11-10")
	conNumero["telefono"] = "+50255123344"
	r := e.avisar("argos:aviso:factura_por_vencer:2", conNumero)
	if r.status != http.StatusUnprocessableEntity && r.status != http.StatusBadRequest {
		t.Fatalf("un número en el cuerpo debe rechazarse: %d %v", r.status, r.cuerpo)
	}

	formato := avisoPorVencer(c, "10/11/2026")
	e.exigir(e.avisar("argos:aviso:factura_por_vencer:3", formato), http.StatusUnprocessableEntity, "type", "urn:iris:problema:validacion")

	e.exigir(e.avisar("", avisoPorVencer(c, "2026-11-10")), http.StatusBadRequest, "type", "urn:iris:problema:idempotency_key_requerida")

	otraEmpresa := avisoPorVencer(c, "2026-11-10")
	otraEmpresa["empresa_id"] = 2
	e.exigir(e.avisar("argos:aviso:factura_por_vencer:4", otraEmpresa), http.StatusUnprocessableEntity, "type", "urn:iris:problema:cliente_fuera_de_empresa")
	ajeno := e.cliente(2, "5512-3399")
	e.exigir(e.avisar("argos:aviso:factura_por_vencer:5", avisoPorVencer(ajeno, "2026-11-10")), http.StatusUnprocessableEntity,
		"type", "urn:iris:problema:cliente_fuera_de_empresa")
	sinServicio := avisoPorVencer(c, "2026-11-10")
	sinServicio["servicio_id"] = 999
	e.exigir(e.avisar("argos:aviso:factura_por_vencer:6", sinServicio), http.StatusUnprocessableEntity, "type", "urn:iris:problema:servicio_fuera_de_empresa")

	urgente := avisoPorVencer(c, "2026-11-10")
	urgente["prioridad"] = "urgente"
	e.exigir(e.avisar("argos:aviso:factura_por_vencer:7", urgente), http.StatusUnprocessableEntity, "type", "urn:iris:problema:validacion")

	// Las plantillas de Iris (§6.3: texto libre, urgente) no las pide una integración: 422
	// plantilla_no_permitida, también con corte_programado urgente.
	deIris := map[string]map[string]string{
		"boleta_recibida":     {"monto": "10.00", "moneda": "GTQ", "motivo": "Entra a http://phishing.example"},
		"boleta_confirmada":   {"monto": "10.00", "moneda": "GTQ", "motivo": "x"},
		"boleta_rechazada":    {"monto": "10.00", "moneda": "GTQ", "motivo": "x"},
		"boleta_falta_dato":   {"monto": "10.00", "moneda": "GTQ", "motivo": "x"},
		"reconexion_temporal": {"hasta": "2026-11-10"},
		"promesa_aceptada":    {"vence_en": "2026-11-10", "monto": "10.00"},
		"corte_programado":    {"inicio": "2026-11-06T22:00", "fin": "2026-11-07T02:00", "zona": "Llama al 5555-0000"},
	}
	for codigo, v := range deIris {
		for _, prioridad := range []string{"normal", "urgente"} {
			cuerpo := map[string]any{"empresa_id": 1, "cliente_id": c, "plantilla": codigo, "variables": v, "prioridad": prioridad}
			e.exigir(e.avisar("argos:aviso:"+codigo+":"+prioridad, cuerpo), http.StatusUnprocessableEntity, "type", "urn:iris:problema:plantilla_no_permitida")
		}
	}
	if n := e.contar(`SELECT count(*) FROM mensajeria.avisos WHERE cliente_id = $1`, c); n != 0 {
		t.Fatalf("ningún aviso debía crearse: %d", n)
	}
	l := e.pedir(http.MethodGet, "/api/v1/mensajeria/plantillas", e.token, "", nil)
	for _, p := range l.cuerpo["data"].([]any) {
		pm := p.(map[string]any)
		if _, iris := deIris[pm["codigo"].(string)]; (pm["pide"] == "iris") != iris {
			t.Fatalf("pide de %s: %v", pm["codigo"], pm["pide"])
		}
	}

	// Sin token: 401; un usuario sin mensajeria.enviar: 403 (lo cubre el ACL común).
	e.exigir(e.pedir(http.MethodPost, "/api/v1/mensajeria/avisos", "", "k", avisoPorVencer(c, "2026-11-10")), http.StatusUnauthorized)

	// Consentimientos: repetición 200, cliente de otra empresa 422, sin clave 400.
	cuando := e.reloj.Add(-2 * time.Hour)
	r1 := e.consentir(c, "acepta", cuando)
	e.exigir(r1, http.StatusCreated)
	r2 := e.consentir(c, "acepta", cuando)
	e.exigir(r2, http.StatusOK, "consentimiento_id", fmt.Sprint(r1.cuerpo["consentimiento_id"]), "estado_vigente", "acepta")
	e.exigir(e.consentir(ajeno, "acepta", cuando), http.StatusUnprocessableEntity, "type", "urn:iris:problema:cliente_fuera_de_empresa")
	e.exigir(e.pedir(http.MethodPost, "/api/v1/mensajeria/consentimientos", e.token, "",
		map[string]any{"empresa_id": 1, "cliente_id": c, "canal": "whatsapp", "estado": "acepta", "origen": "argos_panel", "ocurrido_en": cuando}),
		http.StatusBadRequest)
	// El mismo cliente en la empresa 2 es otro permiso (clave con la empresa, 0.6.1).
	multi := e.cliente(1, "5512-3355")
	e.contactos.mu.Lock()
	cm := e.contactos.c[multi]
	cm.Empresas = []int64{1, 2}
	e.contactos.c[multi] = cm
	e.contactos.mu.Unlock()
	for _, emp := range []int64{1, 2} {
		e.exigir(e.pedir(http.MethodPost, "/api/v1/mensajeria/consentimientos", e.otro, fmt.Sprintf("argos:consentimiento:%d:%d:whatsapp:x", emp, multi),
			map[string]any{"empresa_id": emp, "cliente_id": multi, "canal": "whatsapp", "estado": "acepta", "origen": "argos_portal", "ocurrido_en": cuando}),
			http.StatusCreated)
	}
}

func TestSilencioYVencido(t *testing.T) {
	e := nuevoEntorno(t, nil)
	// 22:00 en Guatemala.
	*e.reloj = time.Date(2026, 11, 6, 4, 0, 0, 0, time.UTC)
	c := e.cliente(1, "5512-3344")
	e.consentir(c, "acepta", e.reloj.Add(-time.Hour))
	r := e.avisar(fmt.Sprintf("argos:aviso:factura_por_vencer:%d", c), avisoPorVencer(c, "2026-11-05"))
	e.exigir(r, http.StatusAccepted, "estado", "en_cola")
	id := r.cuerpo["aviso_id"].(string)
	ultimo := (*e.jobs)[len(*e.jobs)-1].(mensajeria.ArgsEnviarAviso)
	if !ultimo.InsertOpts().ScheduledAt.Equal(time.Date(2026, 11, 6, 13, 0, 0, 0, time.UTC)) {
		t.Fatalf("debía programarse a las 07:00 de Guatemala: %v", ultimo.InsertOpts().ScheduledAt)
	}
	if espera := e.enviar(id); espera != 9*time.Hour {
		t.Fatalf("espera en silencio: %v", espera)
	}
	if len(e.meta.envios) != 0 {
		t.Fatal("nada sale en silencio")
	}
	// Termina el silencio: la factura venció ayer → fallido vencido.
	*e.reloj = time.Date(2026, 11, 6, 13, 1, 0, 0, time.UTC)
	if espera := e.enviar(id); espera != 0 {
		t.Fatalf("espera: %v", espera)
	}
	e.publicar()
	w := e.receptor.de("aviso.fallido", "aviso_id", id)
	if len(w) != 1 || w[0].Cuerpo["payload"].(map[string]any)["motivo"] != "vencido" {
		t.Fatalf("aviso.fallido vencido: %+v", w)
	}
	e.exigir(e.pedir(http.MethodGet, "/api/v1/mensajeria/avisos/"+id, e.token, "", nil), http.StatusOK, "estado", "fallido", "motivo", "vencido")
}

func TestSimuladoNumeroInvalidoYReintentoDeWebhook(t *testing.T) {
	e := nuevoEntorno(t, nil)
	c := e.cliente(1, "+502 5510 0000")
	e.consentir(c, "acepta", e.reloj.Add(-time.Hour))
	r := e.avisar(fmt.Sprintf("argos:aviso:factura_por_vencer:%d", c), avisoPorVencer(c, "2026-11-10"))
	id := r.cuerpo["aviso_id"].(string)
	e.enviar(id)
	// Argos responde 503 al primer intento: queda pendiente con espera de 1 min.
	e.receptor.fallar.Store(1)
	e.publicar()
	var estado string
	var intentos int
	var proximo time.Time
	if err := e.bd.QueryRow(e.ctx, `SELECT estado, intentos, proximo_intento FROM mensajeria.webhooks_salida WHERE payload->>'aviso_id' = $1`, id).
		Scan(&estado, &intentos, &proximo); err != nil {
		t.Fatal(err)
	}
	if estado != "pendiente" || intentos != 1 || !proximo.Equal(e.reloj.Add(time.Minute)) {
		t.Fatalf("reintento: %s %d %v", estado, intentos, proximo)
	}
	*e.reloj = e.reloj.Add(2 * time.Minute)
	if _, err := e.bd.Exec(e.ctx, `UPDATE mensajeria.webhooks_salida SET proximo_intento = $2 WHERE payload->>'aviso_id' = $1`, id, e.reloj.Add(-time.Second)); err != nil {
		t.Fatal(err)
	}
	e.publicar()
	w := e.receptor.de("aviso.fallido", "aviso_id", id)
	if len(w) != 1 || w[0].Cuerpo["payload"].(map[string]any)["motivo"] != "numero_invalido" {
		t.Fatalf("aviso.fallido numero_invalido: %+v", w)
	}
}

func TestLimitePorMinuto(t *testing.T) {
	e := nuevoEntorno(t, func(c *mensajeria.Config) { c.PorMinuto = 2 })
	var ids []string
	for i := range 3 {
		c := e.cliente(1, fmt.Sprintf("5512-33%02d", 50+i))
		e.consentir(c, "acepta", e.reloj.Add(-time.Hour))
		r := e.avisar(fmt.Sprintf("argos:aviso:factura_por_vencer:%d", c), avisoPorVencer(c, "2026-11-10"))
		ids = append(ids, r.cuerpo["aviso_id"].(string))
	}
	// El remitente simulado de la empresa 1 lo comparten todas las pruebas: se limpia la ventana.
	if _, err := e.bd.Exec(e.ctx, `UPDATE mensajeria.avisos SET enviado_en = enviado_en - interval '1 day' WHERE remitente = 'simulado:1' AND enviado_en IS NOT NULL`); err != nil {
		t.Fatal(err)
	}
	if e.enviar(ids[0]) != 0 || e.enviar(ids[1]) != 0 {
		t.Fatal("los dos primeros salen")
	}
	if espera := e.enviar(ids[2]); espera <= 0 || espera > time.Minute {
		t.Fatalf("el tercero espera al siguiente minuto: %v", espera)
	}
	*e.reloj = e.reloj.Add(61 * time.Second)
	if e.enviar(ids[2]) != 0 {
		t.Fatal("después del minuto sale")
	}
}

func TestWhatsAppCloud(t *testing.T) {
	e := nuevoEntorno(t, func(c *mensajeria.Config) { c.Simulado = false })
	empresa := int64(2)
	numero := fmt.Sprint(time.Now().UnixNano() % 1e12)
	canal, err := e.m.CrearCanal.Ejecutar(e.ctx, mensajeria.EntradaCrearCanal{EmpresaID: &empresa, Nombre: "WhatsApp ISP", Numero: "+50222223333",
		PhoneNumberID: numero, Token: "token-de-meta-0123456789abcdef", AppSecret: "app-secret-0123456789",
		VerifyToken: "verify-token-0123456789", Plantillas: map[string]mensajeria.PlantillaMeta{"factura_por_vencer": {Nombre: "factura_por_vencer_v2", Idioma: "es"}}})
	if err != nil {
		t.Fatal(err)
	}
	// Las credenciales quedan cifradas y fuera de la auditoría.
	if n := e.contar(`SELECT count(*) FROM auditoria.cambios WHERE entidad = 'canal_mensajeria' AND entidad_id = $1 AND despues::text LIKE '%app-secret%'`, fmt.Sprint(canal.ID)); n != 0 {
		t.Fatal("credenciales en la auditoría")
	}
	if n := e.contar(`SELECT count(*) FROM mensajeria.canales WHERE id = $1 AND position('app-secret' in encode(credenciales_cifradas, 'escape')) > 0`, canal.ID); n != 0 {
		t.Fatal("credenciales sin cifrar")
	}

	c := e.cliente(2, "5512-7788")
	cuando := e.reloj.Add(-time.Hour)
	e.exigir(e.pedir(http.MethodPost, "/api/v1/mensajeria/consentimientos", e.otro, fmt.Sprintf("argos:consentimiento:2:%d:whatsapp:a", c),
		map[string]any{"empresa_id": 2, "cliente_id": c, "canal": "whatsapp", "estado": "acepta", "origen": "argos_panel", "ocurrido_en": cuando}), http.StatusCreated)
	l := e.pedir(http.MethodGet, "/api/v1/mensajeria/plantillas?empresa_id=2", e.otro, "", nil)
	e.exigir(l, http.StatusOK)
	for _, p := range l.cuerpo["data"].([]any) {
		pm := p.(map[string]any)
		quiere := "[]"
		if pm["codigo"] == "factura_por_vencer" {
			quiere = "[whatsapp]"
		}
		if fmt.Sprint(pm["canales"]) != quiere {
			t.Fatalf("canales de %s: %v", pm["codigo"], pm["canales"])
		}
	}
	// Una plantilla no aprobada: canal_no_disponible.
	vencida := map[string]any{"empresa_id": 2, "cliente_id": c, "plantilla": "factura_vencida",
		"variables": map[string]string{"monto": "10.00", "moneda": "GTQ", "vence_en": "2026-11-01"}}
	e.exigir(e.pedir(http.MethodPost, "/api/v1/mensajeria/avisos", e.otro, "argos:aviso:factura_vencida:1", vencida),
		http.StatusAccepted, "estado", "bloqueado", "motivo", "canal_no_disponible")

	cuerpo := avisoPorVencer(c, "2026-11-10")
	cuerpo["empresa_id"] = 2
	r := e.pedir(http.MethodPost, "/api/v1/mensajeria/avisos", e.otro, fmt.Sprintf("argos:aviso:factura_por_vencer:%d", c), cuerpo)
	e.exigir(r, http.StatusAccepted, "estado", "en_cola")
	id := r.cuerpo["aviso_id"].(string)
	e.enviar(id)
	if len(e.meta.envios) != 1 {
		t.Fatalf("envíos a Meta: %d", len(e.meta.envios))
	}
	env := e.meta.envios[0]
	tpl := env["template"].(map[string]any)
	if env["to"] != "50255127788" || tpl["name"] != "factura_por_vencer_v2" {
		t.Fatalf("envío a Meta: %v", env)
	}
	var wamid string
	if err := e.bd.QueryRow(e.ctx, `SELECT proveedor_id FROM mensajeria.avisos WHERE id = $1`, id).Scan(&wamid); err != nil {
		t.Fatal(err)
	}
	e.exigir(e.pedir(http.MethodGet, "/api/v1/mensajeria/avisos/"+id, e.otro, "", nil), http.StatusOK, "estado", "en_cola")

	// Verificación de la suscripción.
	ruta := fmt.Sprintf("/api/v1/mensajeria/canales/%d/webhook", canal.ID)
	req := httptest.NewRequest(http.MethodGet, ruta+"?hub.mode=subscribe&hub.verify_token=verify-token-0123456789&hub.challenge=12345", nil)
	w := httptest.NewRecorder()
	e.h.ServeHTTP(w, req)
	if w.Code != http.StatusOK || w.Body.String() != "12345" {
		t.Fatalf("verificación: %d %q", w.Code, w.Body.String())
	}
	req = httptest.NewRequest(http.MethodGet, ruta+"?hub.mode=subscribe&hub.verify_token=otro&hub.challenge=1", nil)
	w = httptest.NewRecorder()
	e.h.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("verify token inválido: %d", w.Code)
	}

	meta := func(cuerpo string, firma string) int {
		req := httptest.NewRequest(http.MethodPost, ruta, strings.NewReader(cuerpo))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Hub-Signature-256", firma)
		w := httptest.NewRecorder()
		e.h.ServeHTTP(w, req)
		return w.Code
	}
	estado := func(st string, ts int64) string {
		return fmt.Sprintf(`{"object":"whatsapp_business_account","entry":[{"changes":[{"field":"messages","value":{"metadata":{"phone_number_id":%q},"statuses":[{"id":%q,"status":%q,"timestamp":"%d"}]}}]}]}`, numero, wamid, st, ts)
	}
	entregado := estado("delivered", e.reloj.Unix())
	if code := meta(entregado, "sha256=00"); code != http.StatusUnauthorized {
		t.Fatalf("firma inválida: %d", code)
	}
	if code := meta(entregado, mensajeria.FirmarMeta("app-secret-0123456789", []byte(entregado))); code != http.StatusOK {
		t.Fatalf("webhook de Meta: %d", code)
	}
	procesar := func() {
		var ids []int64
		filas, err := e.bd.Query(e.ctx, `SELECT id FROM mensajeria.entrantes WHERE canal_id = $1 AND procesado_en IS NULL ORDER BY id`, canal.ID)
		if err != nil {
			t.Fatal(err)
		}
		ids, err = pgx.CollectRows(filas, pgx.RowTo[int64])
		if err != nil {
			t.Fatal(err)
		}
		for _, id := range ids {
			if err := e.m.ProcesarEntrante(e.ctx, id); err != nil {
				t.Fatal(err)
			}
		}
	}
	procesar()
	e.exigir(e.pedir(http.MethodGet, "/api/v1/mensajeria/avisos/"+id, e.otro, "", nil), http.StatusOK, "estado", "entregado")
	// Repetido: deduplicado.
	if code := meta(entregado, mensajeria.FirmarMeta("app-secret-0123456789", []byte(entregado))); code != http.StatusOK {
		t.Fatalf("repetido: %d", code)
	}
	// Leído después: leido_en, sin otro webhook.
	leido := estado("read", e.reloj.Add(time.Minute).Unix())
	meta(leido, mensajeria.FirmarMeta("app-secret-0123456789", []byte(leido)))
	procesar()
	d := e.pedir(http.MethodGet, "/api/v1/mensajeria/avisos/"+id, e.otro, "", nil)
	if d.cuerpo["leido_en"] == nil {
		t.Fatalf("leido_en: %v", d.cuerpo)
	}
	e.publicar()
	if n := len(e.receptor.de("aviso.entregado", "aviso_id", id)); n != 1 {
		t.Fatalf("un solo webhook final: %d", n)
	}

	// Una baja escrita a otro número de la misma app de Meta no cuenta para este canal.
	bajaDe := func(pnid string) string {
		return fmt.Sprintf(`{"object":"whatsapp_business_account","entry":[{"changes":[{"field":"messages","value":{"metadata":{"phone_number_id":%q},"messages":[{"from":"50255127788","id":"wamid.in.%s","timestamp":"%d","type":"text","text":{"body":" baja. "}}]}}]}]}`, pnid, pnid, e.reloj.Unix())
	}
	ajena := bajaDe("999" + numero)
	if code := meta(ajena, mensajeria.FirmarMeta("app-secret-0123456789", []byte(ajena))); code != http.StatusOK {
		t.Fatalf("webhook ajeno: %d", code)
	}
	procesar()
	e.publicar()
	if n := len(e.receptor.de("contacto.baja", "cliente_id", c)); n != 0 {
		t.Fatalf("una baja a otro número no debía registrarse: %d", n)
	}
	if n := e.contar(`SELECT count(*) FROM mensajeria.consentimientos WHERE cliente_id = $1 AND estado = 'baja'`, c); n != 0 {
		t.Fatalf("baja de otro número registrada: %d", n)
	}
	if n := e.contar(`SELECT count(*) FROM mensajeria.entrantes WHERE canal_id = $1 AND error LIKE '%phone_number_id%'`, canal.ID); n != 1 {
		t.Fatalf("el descarte debía quedar anotado: %d", n)
	}

	// El cliente escribe BAJA a este número: baja de avisos (origen cliente) y contacto.baja a Argos.
	baja := bajaDe(numero)
	meta(baja, mensajeria.FirmarMeta("app-secret-0123456789", []byte(baja)))
	procesar()
	e.publicar()
	wb := e.receptor.de("contacto.baja", "cliente_id", c)
	if len(wb) != 1 {
		t.Fatalf("contacto.baja: %d", len(wb))
	}
	pb := wb[0].Cuerpo["payload"].(map[string]any)
	if pb["origen"] != "cliente" || pb["canal"] != "whatsapp" || fmt.Sprint(pb["empresa_id"]) != "2" || len(pb) != 5 {
		t.Fatalf("payload contacto.baja: %v", pb)
	}
	cuerpo2 := avisoPorVencer(c, "2026-11-10")
	cuerpo2["empresa_id"] = 2
	e.exigir(e.pedir(http.MethodPost, "/api/v1/mensajeria/avisos", e.otro, fmt.Sprintf("argos:aviso:factura_por_vencer:%d:2", c), cuerpo2),
		http.StatusAccepted, "estado", "bloqueado", "motivo", "baja")

	// Un número inválido según Meta: fallido numero_invalido.
	c2 := e.cliente(2, "5512-7789")
	e.pedir(http.MethodPost, "/api/v1/mensajeria/consentimientos", e.otro, fmt.Sprintf("argos:consentimiento:2:%d:whatsapp:a", c2),
		map[string]any{"empresa_id": 2, "cliente_id": c2, "canal": "whatsapp", "estado": "acepta", "origen": "argos_panel", "ocurrido_en": cuando})
	cuerpo3 := avisoPorVencer(c2, "2026-11-10")
	cuerpo3["empresa_id"] = 2
	r3 := e.pedir(http.MethodPost, "/api/v1/mensajeria/avisos", e.otro, fmt.Sprintf("argos:aviso:factura_por_vencer:%d", c2), cuerpo3)
	e.meta.error.Store(131026)
	e.enviar(r3.cuerpo["aviso_id"].(string))
	e.meta.error.Store(0)
	e.exigir(e.pedir(http.MethodGet, "/api/v1/mensajeria/avisos/"+r3.cuerpo["aviso_id"].(string), e.otro, "", nil), http.StatusOK,
		"estado", "fallido", "motivo", "numero_invalido")

	// Un error temporal de Meta se reintenta; en el último intento, agotado.
	c3 := e.cliente(2, "5512-7790")
	e.pedir(http.MethodPost, "/api/v1/mensajeria/consentimientos", e.otro, fmt.Sprintf("argos:consentimiento:2:%d:whatsapp:a", c3),
		map[string]any{"empresa_id": 2, "cliente_id": c3, "canal": "whatsapp", "estado": "acepta", "origen": "argos_panel", "ocurrido_en": cuando})
	cuerpo4 := avisoPorVencer(c3, "2026-11-10")
	cuerpo4["empresa_id"] = 2
	r4 := e.pedir(http.MethodPost, "/api/v1/mensajeria/avisos", e.otro, fmt.Sprintf("argos:aviso:factura_por_vencer:%d", c3), cuerpo4)
	id4 := uuid.MustParse(r4.cuerpo["aviso_id"].(string))
	e.meta.error.Store(130429)
	if _, err := e.m.ProcesarEnvio(e.ctx, id4, false); err != mensajeria.ErrReintentar {
		t.Fatalf("temporal: %v", err)
	}
	if _, err := e.m.ProcesarEnvio(e.ctx, id4, true); err != nil {
		t.Fatal(err)
	}
	e.meta.error.Store(0)
	e.exigir(e.pedir(http.MethodGet, "/api/v1/mensajeria/avisos/"+id4.String(), e.otro, "", nil), http.StatusOK, "estado", "fallido", "motivo", "agotado")
}

func TestWebhooksSoloContrato060(t *testing.T) {
	e := nuevoEntorno(t, nil)
	if _, err := e.bd.Exec(e.ctx, `UPDATE integracion.conectores SET contrato_remoto = '0.5.0' WHERE id = $1`, e.conector); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = e.bd.Exec(e.ctx, `UPDATE integracion.conectores SET contrato_remoto = '0.6.0' WHERE id = $1`, e.conector)
	})
	c := e.cliente(1, "5512-3344")
	e.consentir(c, "acepta", e.reloj.Add(-time.Hour))
	r := e.avisar(fmt.Sprintf("argos:aviso:factura_por_vencer:%d", c), avisoPorVencer(c, "2026-11-10"))
	id := r.cuerpo["aviso_id"].(string)
	e.enviar(id)
	e.publicar()
	if len(e.receptor.de("aviso.entregado", "aviso_id", id)) != 0 {
		t.Fatal("no se envía a un conector con contrato < 0.6.0")
	}
	if n := e.contar(`SELECT count(*) FROM mensajeria.webhooks_salida WHERE payload->>'aviso_id' = $1 AND estado = 'omitido'`, id); n != 1 {
		t.Fatalf("omitido: %d", n)
	}
}

func TestAvisosSinAcuseSeAgotan(t *testing.T) {
	e := nuevoEntorno(t, func(c *mensajeria.Config) { c.SinConfirmar = time.Hour })
	c := e.cliente(1, "5512-3344")
	e.consentir(c, "acepta", e.reloj.Add(-time.Hour))
	r := e.avisar(fmt.Sprintf("argos:aviso:factura_por_vencer:%d", c), avisoPorVencer(c, "2026-11-10"))
	*e.reloj = e.reloj.Add(2 * time.Hour)
	if _, err := e.m.VencerAvisos(e.ctx); err != nil {
		t.Fatal(err)
	}
	e.exigir(e.pedir(http.MethodGet, "/api/v1/mensajeria/avisos/"+r.cuerpo["aviso_id"].(string), e.token, "", nil), http.StatusOK,
		"estado", "fallido", "motivo", "agotado")
}

// Producción (IRIS_BASE_URL https en un host no local): el canal simulado no arranca.
func TestSimuladoProhibidoEnProduccion(t *testing.T) {
	casos := []struct {
		simulado bool
		base     string
		error    bool
	}{
		{true, "https://iris.isp.gt", true},
		{true, "https://iris.isp.gt:8443/", true},
		{true, "http://localhost:3000", false},
		{true, "https://localhost", false},
		{true, "https://127.0.0.1:3000", false},
		{true, "http://192.168.1.10:3000", false},
		{true, "", false},
		{false, "https://iris.isp.gt", false},
	}
	for _, c := range casos {
		err := mensajeria.ValidarSimulado(c.simulado, c.base)
		if (err != nil) != c.error {
			t.Errorf("ValidarSimulado(%v, %q) = %v", c.simulado, c.base, err)
		}
		if err != nil && !strings.Contains(err.Error(), "IRIS_MENSAJERIA_SIMULADO") {
			t.Errorf("el error debe nombrar la variable: %v", err)
		}
	}
}

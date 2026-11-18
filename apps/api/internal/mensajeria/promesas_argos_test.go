package mensajeria_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos"
)

// promesasFalsas es un Argos falso (httptest) que responde POST /promesas y /promesas/simular como
// Argos 1.17.0: cabeceras de §7.1 con su formato exacto (400 cabecera_iris_requerida),
// Idempotency-Key obligatoria al crear (400 idempotency_key_requerida), huella de idempotencia = cuerpo
// crudo + X-Iris-Actor (422 idempotency_key_reutilizada; la respuesta guardada se repite tal cual,
// también un 4xx), 409 idempotencia_en_curso, 409 promesa_abierta y 422 validacion sobre vence_en
// (rule futuro o fecha). Los demás rechazos (sin_plantilla, servicio_retirado…) se fuerzan. La bandeja
// le habla con el cliente HTTP real del conector: cabeceras, claves y problem+json son los de producción.
type promesasFalsas struct {
	t   *testing.T
	srv *httptest.Server

	mu sync.Mutex
	// claves: Idempotency-Key → huella y respuesta guardada.
	claves map[string]respuestaPromesa
	// forzar: el próximo rechazo de la ruta («simular» o «crear»), una vez.
	forzar map[string]rechazoArgos
	// enCurso: responde 409 idempotencia_en_curso a los próximos n pedidos de crear (sin guardar).
	enCurso int
	// abiertas: servicios con una promesa activa (409 promesa_abierta).
	abiertas   map[float64]bool
	peticiones []peticionPromesa
	siguiente  int64
	// repetidas: respuestas devueltas desde la clave guardada.
	repetidas int
}

type respuestaPromesa struct {
	huella string
	status int
	cuerpo []byte
}

type rechazoArgos struct {
	status int
	codigo string
	campo  string
	regla  string
}

type peticionPromesa struct {
	ruta, actor, clave string
	cuerpo             map[string]any
}

var (
	reActorArgos = regexp.MustCompile(`^(asistente_ia|usuario:[A-Za-z0-9_-]{1,64})$`)
	reConvArgos  = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,128}$`)
)

// promesasDe pone el Argos falso de promesas en la bandeja.
func promesasDe(t *testing.T, b *bandeja) *promesasFalsas {
	t.Helper()
	p := &promesasFalsas{t: t, claves: map[string]respuestaPromesa{}, forzar: map[string]rechazoArgos{}, abiertas: map[float64]bool{}}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/promesas/simular", func(w http.ResponseWriter, r *http.Request) { p.atender(w, r, "simular") })
	mux.HandleFunc("POST /api/v1/promesas", func(w http.ResponseWriter, r *http.Request) { p.atender(w, r, "crear") })
	p.srv = httptest.NewServer(mux)
	t.Cleanup(p.srv.Close)
	b.argos.mu.Lock()
	b.argos.promesas = p
	b.argos.mu.Unlock()
	return p
}

func (p *promesasFalsas) cliente() *argos.Cliente {
	return argos.NuevoCliente(p.srv.URL, "atk_promesas_0123456789", argos.OpcionesCliente{Intentos: 1})
}

func (p *promesasFalsas) rechazar(ruta string, r rechazoArgos) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.forzar[ruta] = r
}

func (p *promesasFalsas) pedidos(ruta string) []peticionPromesa {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := []peticionPromesa{}
	for _, x := range p.peticiones {
		if x.ruta == ruta {
			out = append(out, x)
		}
	}
	return out
}

// problema arma el problem+json de Argos; el detail trae «detalle interno de Argos» y un dato del
// cliente, que nunca deben llegar a quien usa Iris.
func problema(r rechazoArgos) (int, []byte) {
	cuerpo := map[string]any{"type": "urn:argosisp:problema:" + r.codigo, "title": "Rechazado", "status": r.status,
		"detail": "detalle interno de Argos: cliente Ana López, teléfono 5512-0000"}
	if r.campo != "" {
		cuerpo["errors"] = []map[string]any{{"field": r.campo, "rule": r.regla, "message": "detalle interno de Argos"}}
	}
	b, _ := json.Marshal(cuerpo)
	return r.status, b
}

func (p *promesasFalsas) atender(w http.ResponseWriter, r *http.Request, ruta string) {
	crudo, _ := io.ReadAll(r.Body)
	actor, conv, clave := r.Header.Get("X-Iris-Actor"), r.Header.Get("X-Iris-Conversacion"), r.Header.Get("Idempotency-Key")
	var cuerpo map[string]any
	_ = json.Unmarshal(crudo, &cuerpo)
	p.mu.Lock()
	defer p.mu.Unlock()
	p.peticiones = append(p.peticiones, peticionPromesa{ruta: ruta, actor: actor, clave: clave, cuerpo: cuerpo})
	escribir := func(status int, b []byte) {
		if status >= 400 {
			w.Header().Set("Content-Type", "application/problem+json")
		} else {
			w.Header().Set("Content-Type", "application/json")
		}
		w.WriteHeader(status)
		_, _ = w.Write(b)
	}
	if !reActorArgos.MatchString(actor) || !reConvArgos.MatchString(conv) {
		escribir(problema(rechazoArgos{status: http.StatusBadRequest, codigo: "cabecera_iris_requerida"}))
		return
	}
	var huella string
	if ruta == "crear" {
		if clave == "" {
			escribir(problema(rechazoArgos{status: http.StatusBadRequest, codigo: "idempotency_key_requerida"}))
			return
		}
		h := sha256.Sum256(append(append([]byte{}, crudo...), []byte("\n"+actor)...))
		huella = hex.EncodeToString(h[:])
		if g, ok := p.claves[clave]; ok {
			if g.huella != huella {
				escribir(problema(rechazoArgos{status: http.StatusUnprocessableEntity, codigo: "idempotency_key_reutilizada"}))
				return
			}
			p.repetidas++
			escribir(g.status, g.cuerpo)
			return
		}
		if p.enCurso > 0 {
			p.enCurso--
			escribir(problema(rechazoArgos{status: http.StatusConflict, codigo: "idempotencia_en_curso"}))
			return
		}
	}
	status, b := p.decidir(ruta, cuerpo)
	if ruta == "crear" {
		p.claves[clave] = respuestaPromesa{huella: huella, status: status, cuerpo: b}
	}
	escribir(status, b)
}

func (p *promesasFalsas) decidir(ruta string, cuerpo map[string]any) (int, []byte) {
	if f, ok := p.forzar[ruta]; ok {
		delete(p.forzar, ruta)
		return problema(f)
	}
	servicio, _ := cuerpo["servicio_id"].(float64)
	if v, ok := cuerpo["vence_en"].(string); ok || ruta == "crear" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			return problema(rechazoArgos{status: http.StatusUnprocessableEntity, codigo: "validacion", campo: "vence_en", regla: "fecha"})
		}
		if !t.After(time.Now()) {
			return problema(rechazoArgos{status: http.StatusUnprocessableEntity, codigo: "validacion", campo: "vence_en", regla: "futuro"})
		}
	}
	if p.abiertas[servicio] {
		return problema(rechazoArgos{status: http.StatusConflict, codigo: "promesa_abierta"})
	}
	if ruta == "simular" {
		b, _ := json.Marshal(map[string]any{"resultado": "aprobada", "regla": nil, "motivo": nil, "monto": "250.00", "limites": map[string]any{}})
		return http.StatusOK, b
	}
	p.siguiente++
	p.abiertas[servicio] = true
	b, _ := json.Marshal(map[string]any{"id": 800 + p.siguiente, "estado": "activa", "monto": "250.00", "resultado_evaluacion": nil})
	return http.StatusCreated, b
}

// diaGuatemala: el día (AAAA-MM-DD) a n días de hoy en Guatemala.
func diaGuatemala(n int) string {
	zona, _ := time.LoadLocation("America/Guatemala")
	return time.Now().In(zona).AddDate(0, 0, n).Format("2006-01-02")
}

// Cada rechazo de Argos (1.17.0) a simular y a crear sale con un motivo en llano: los de negocio como
// regla de Iris (409/422) y los de integración como 502 argos_promesa_integracion con el código de
// Argos; nunca el detail de Argos.
func TestPromesaMotivosDeArgos(t *testing.T) {
	x := nuevaBandejaIA(t)
	p := promesasDe(t, x.bandeja)
	conv, cliente := x.conClienteArgos()
	ruta := "/conversaciones/" + conv
	pr := map[string]any{"servicioId": cliente * 10, "venceEl": diaGuatemala(3)}
	casos := []struct {
		nombre string
		rutas  []string
		argos  rechazoArgos
		status int
		codigo string
		texto  string
	}{
		{"sin plantilla", []string{"simular", "crear"}, rechazoArgos{status: 422, codigo: "sin_plantilla"}, 422, "sin_plantilla_facturacion", "no tiene plantilla de facturación en Argos"},
		{"retirado", []string{"simular", "crear"}, rechazoArgos{status: 422, codigo: "servicio_retirado"}, 422, "servicio_retirado", "retirado"},
		{"sin instalar", []string{"simular", "crear"}, rechazoArgos{status: 422, codigo: "servicio_pendiente_instalacion"}, 422, "servicio_sin_instalar", "pendiente de instalación"},
		{"promesa abierta", []string{"simular", "crear"}, rechazoArgos{status: 409, codigo: "promesa_abierta"}, 409, "promesa_abierta", "activa o pendiente"},
		{"fecha inválida", []string{"simular", "crear"}, rechazoArgos{status: 422, codigo: "validacion", campo: "vence_en", regla: "fecha"}, 422, "promesa_fecha_invalida", "no reconoce la fecha"},
		{"regla nueva", []string{"simular", "crear"}, rechazoArgos{status: 422, codigo: "regla_nueva"}, 422, "promesa_no_permitida", "código de Argos: regla_nueva"},
		{"cabeceras", []string{"simular", "crear"}, rechazoArgos{status: 400, codigo: "cabecera_iris_requerida"}, 502, "argos_promesa_integracion", "cabecera_iris_requerida"},
		{"empresa", []string{"simular", "crear"}, rechazoArgos{status: 422, codigo: "empresa_no_coincide"}, 502, "argos_promesa_integracion", "empresa_no_coincide"},
		{"sin clave", []string{"crear"}, rechazoArgos{status: 400, codigo: "idempotency_key_requerida"}, 502, "argos_promesa_integracion", "idempotency_key_requerida"},
		{"clave reutilizada", []string{"crear"}, rechazoArgos{status: 422, codigo: "idempotency_key_reutilizada"}, 502, "argos_promesa_integracion", "idempotency_key_reutilizada"},
		{"otro campo", []string{"simular"}, rechazoArgos{status: 422, codigo: "validacion", campo: "servicio_id", regla: "requerido"}, 502, "argos_promesa_integracion", "validacion"},
	}
	for i, c := range casos {
		for _, r := range c.rutas {
			p.rechazar(r, c.argos)
			var res respuesta
			if r == "simular" {
				res = x.api(x.supervisor, http.MethodPost, ruta+"/promesas/simular", pr)
			} else {
				res = x.apiClave(x.supervisor, http.MethodPost, ruta+"/promesas", fmt.Sprintf("motivo-%d", i), pr)
			}
			if res.status != c.status || tipoDe(res) != c.codigo || !strings.Contains(fmt.Sprint(res.cuerpo["detail"]), c.texto) ||
				strings.Contains(fmt.Sprint(res.cuerpo), "detalle interno de Argos") || strings.Contains(fmt.Sprint(res.cuerpo), "5512") {
				t.Fatalf("%s (%s): %d %v", c.nombre, r, res.status, res.cuerpo)
			}
		}
	}
	// La fecha que ya pasó (la valida Argos de verdad): junto al campo venceEl, en las dos rutas.
	ayer := map[string]any{"servicioId": cliente * 10, "venceEl": diaGuatemala(-1)}
	for _, res := range []respuesta{x.api(x.supervisor, http.MethodPost, ruta+"/promesas/simular", ayer),
		x.apiClave(x.supervisor, http.MethodPost, ruta+"/promesas", "fecha-pasada", ayer)} {
		x.exigirProblema(res, http.StatusUnprocessableEntity, "promesa_fecha_invalida")
		errs, _ := res.cuerpo["errors"].([]any)
		if !strings.Contains(fmt.Sprint(res.cuerpo["detail"]), "ya pasó") || len(errs) != 1 || errs[0].(map[string]any)["field"] != "venceEl" {
			t.Fatalf("fecha pasada: %v", res.cuerpo)
		}
	}
	// 409 idempotencia_en_curso: se puede repetir con la misma clave y el mismo pedido.
	p.mu.Lock()
	p.enCurso = 1
	p.mu.Unlock()
	x.exigirProblema(x.apiClave(x.supervisor, http.MethodPost, ruta+"/promesas", "en-curso", pr), http.StatusConflict, "idempotencia_en_curso")
	x.exigir(x.apiClave(x.supervisor, http.MethodPost, ruta+"/promesas", "en-curso", pr), http.StatusCreated, "estado", "activa")
	// Ya con una promesa activa, Argos responde 409 promesa_abierta (sin forzar).
	x.exigirProblema(x.api(x.supervisor, http.MethodPost, ruta+"/promesas/simular", pr), http.StatusConflict, "promesa_abierta")
	x.exigirProblema(x.apiClave(x.supervisor, http.MethodPost, ruta+"/promesas", "otra", pr), http.StatusConflict, "promesa_abierta")
	if n := x.contar(`SELECT count(*) FROM mensajeria.promesas WHERE conversacion_id = $1`, conv); n != 1 {
		t.Fatalf("promesas guardadas: %d", n)
	}
}

// Idempotencia hacia Argos (huella = cuerpo crudo + X-Iris-Actor): si la persona cambia la fecha y
// vuelve a enviar con la misma Idempotency-Key (sin que Iris guardara nada), Argos no responde
// idempotency_key_reutilizada; el mismo pedido repetido sí lleva la misma clave y la misma respuesta.
func TestPromesaClaveSigueAlPedido(t *testing.T) {
	x := nuevaBandejaIA(t)
	p := promesasDe(t, x.bandeja)
	conv, cliente := x.conClienteArgos()
	ruta := "/conversaciones/" + conv
	ayer := map[string]any{"servicioId": cliente * 10, "venceEl": diaGuatemala(-1)}
	// El mismo pedido dos veces (Iris no guardó nada porque Argos lo rechazó): misma clave hacia Argos,
	// y Argos repite la respuesta guardada.
	x.exigirProblema(x.apiClave(x.supervisor, http.MethodPost, ruta+"/promesas", "form-1", ayer), http.StatusUnprocessableEntity, "promesa_fecha_invalida")
	x.exigirProblema(x.apiClave(x.supervisor, http.MethodPost, ruta+"/promesas", "form-1", ayer), http.StatusUnprocessableEntity, "promesa_fecha_invalida")
	cs := p.pedidos("crear")
	if len(cs) != 2 || cs[0].clave != cs[1].clave || p.repetidas != 1 {
		t.Fatalf("reintento idéntico: %v (repetidas %d)", cs, p.repetidas)
	}
	// Corrige la fecha con la misma clave del formulario: otra clave hacia Argos, y Argos la acepta.
	bien := map[string]any{"servicioId": cliente * 10, "venceEl": diaGuatemala(2)}
	r := x.apiClave(x.supervisor, http.MethodPost, ruta+"/promesas", "form-1", bien)
	x.exigir(r, http.StatusCreated, "estado", "activa")
	cs = p.pedidos("crear")
	if len(cs) != 3 || cs[2].clave == cs[0].clave || !strings.HasPrefix(cs[2].clave, "iris:promesa:") {
		t.Fatalf("la fecha nueva reusó la clave hacia Argos: %v", cs)
	}
	// Repetir ese mismo pedido: la misma promesa, sin volver a Argos.
	x.exigir(x.apiClave(x.supervisor, http.MethodPost, ruta+"/promesas", "form-1", bien), http.StatusCreated, "id", r.cuerpo["id"].(string))
	if len(p.pedidos("crear")) != 3 {
		t.Fatal("la repetición volvió a pedir a Argos")
	}
	// Y con la misma clave pero otra fecha, una vez guardada: el 422 de Iris (no llega a Argos).
	x.exigirProblema(x.apiClave(x.supervisor, http.MethodPost, ruta+"/promesas", "form-1", map[string]any{"servicioId": cliente * 10, "venceEl": diaGuatemala(4)}),
		http.StatusUnprocessableEntity, "idempotencia_reutilizada")
	if len(p.pedidos("crear")) != 3 {
		t.Fatal("una clave ya usada con otro pedido llegó a Argos")
	}
	if cs[2].actor != "usuario:"+fmt.Sprint(x.supervisor.id) {
		t.Fatalf("actor: %q", cs[2].actor)
	}
}

// El asistente recibe los rechazos de negocio de Argos como un motivo legible (no como falla de la
// herramienta), y los de integración como falla.
func TestAutonomoPromesaMotivosDeArgos(t *testing.T) {
	x := nuevaBandejaIA(t)
	p := promesasDe(t, x.bandeja)
	x.autonomo("siempre_primero", map[string]any{"perfiles": []any{perfilCon(map[string]any{"promesa_pago": true}, "alta")}})
	conv, cliente := x.conClienteArgos()
	num := x.numeroDe(conv)
	*x.reloj = x.reloj.Add(21 * time.Second)
	x.ia.decir(`{"categoria": "Pagos y saldo", "confianza": 0.95}`)
	x.fuerte(conv)
	servicio := cliente * 10
	// Un día futuro para el reloj del asistente y para el Argos falso (que usa la hora real).
	fecha := diaGuatemala(3)
	args := fmt.Sprintf(`{"servicio_id": %d, "fecha": %q}`, servicio, fecha)
	p.rechazar("simular", rechazoArgos{status: 422, codigo: "sin_plantilla"})
	x.turno(num, "wamid.pm1", "Quiero pagar después", "Pagos y saldo", llama("simular_promesa", args), dice("No es posible por ahora."))
	cuerpos := x.ia.cuerposAutonomos()
	if a := strings.Join(x.acciones(conv), ","); !strings.Contains(a, "promesa_pago:rechazada") ||
		!strings.Contains(cuerpos[len(cuerpos)-1], "no tiene plantilla de facturación en Argos") {
		t.Fatalf("sin plantilla al asistente: %s", a)
	}
	p.rechazar("crear", rechazoArgos{status: 422, codigo: "servicio_retirado"})
	x.turno(num, "wamid.pm2", "Pago el día que dije", "Pagos y saldo", llama("pedir_promesa", args), dice("No es posible por ahora."))
	cuerpos = x.ia.cuerposAutonomos()
	if a := strings.Join(x.acciones(conv), ","); strings.Count(a, "promesa_pago:rechazada") != 2 || !strings.Contains(cuerpos[len(cuerpos)-1], "retirado en Argos") {
		t.Fatalf("servicio retirado al asistente: %s", a)
	}
	for _, c := range cuerpos {
		if strings.Contains(c, "detalle interno de Argos") {
			t.Fatal("el detail de Argos llegó al modelo")
		}
	}
	if n := x.contar(`SELECT count(*) FROM mensajeria.promesas WHERE conversacion_id = $1`, conv); n != 0 {
		t.Fatalf("promesas: %d", n)
	}
}

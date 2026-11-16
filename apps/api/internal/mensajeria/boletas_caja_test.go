package mensajeria_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/conector"
)

// cajaFalsa es un Argos falso (httptest) que cumple §7.7 del contrato 0.7.0: aprobar y rechazar
// comprobantes con el token de Iris, autorizados por el usuario de Argos vinculado a X-Iris-Actor,
// con la huella de idempotencia que incluye al actor. La bandeja le habla con el cliente HTTP real
// del conector (argos.Cliente): cabeceras, claves y problem+json son los de producción.
type cajaFalsa struct {
	t   *testing.T
	srv *httptest.Server

	mu sync.Mutex
	// contrato: Argos informa contrato ≥ 0.7.0.
	contrato     bool
	vinculos     map[string]*usuarioArgos // id del usuario de Iris → usuario de Argos
	comprobantes map[int64]*comprobanteFalso
	claves       map[string]claveCaja
	caido        bool
	apagado      bool
	// enCurso: responde 409 idempotencia_en_curso a las próximas n decisiones.
	enCurso int
	// sinCode: los problem+json no traen `code` (solo `type`).
	sinCode    bool
	peticiones []peticionCaja
	// alDecidir corre mientras «Argos» atiende una decisión, ya aplicada y antes de responder.
	alDecidir func(id int64)
	pagos     int64
	// sin071: Argos 1.15.0 (contrato 0.7.0): el comprobante no trae verificado_iris_actor.
	sin071 bool
}

type usuarioArgos struct {
	nombre   string
	activo   bool
	permisos map[string]bool
	empresas map[int64]bool
}

type comprobanteFalso struct {
	id, empresa      int64
	canal, estado    string
	monto            string
	pago             *int64
	motivo, causa    *string
	verificadoNombre *string
	// irisActor e irisConversacion: quien REGISTRÓ la boleta (no quien la decidió).
	irisActor        *string
	irisConversacion *string
	// verificadoActor: el X-Iris-Actor de quien la aprobó o rechazó desde Iris (0.7.1); nil = caja.
	verificadoActor *string
}

type claveCaja struct {
	huella string
	cuerpo []byte
}

type peticionCaja struct {
	metodo, ruta, actor, conversacion, clave, empresa, token string
	cuerpo                                                   map[string]any
}

const tokenCaja = "atk_caja_0123456789abcdef"

func nuevaCaja(t *testing.T) *cajaFalsa {
	c := &cajaFalsa{t: t, contrato: true, vinculos: map[string]*usuarioArgos{}, comprobantes: map[int64]*comprobanteFalso{}, claves: map[string]claveCaja{}}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/comprobantes/{id}/aprobar", func(w http.ResponseWriter, r *http.Request) { c.decidir(w, r, "aprobar") })
	mux.HandleFunc("POST /api/v1/comprobantes/{id}/rechazar", func(w http.ResponseWriter, r *http.Request) { c.decidir(w, r, "rechazar") })
	mux.HandleFunc("GET /api/v1/comprobantes/{id}", c.leer)
	c.srv = httptest.NewServer(mux)
	t.Cleanup(c.srv.Close)
	return c
}

// cliente es el cliente HTTP real del conector apuntando al Argos falso.
func (c *cajaFalsa) cliente() *argos.Cliente {
	return argos.NuevoCliente(c.srv.URL, tokenCaja, argos.OpcionesCliente{Intentos: 1})
}

// vincular deja al usuario de Iris vinculado con un usuario de Argos de caja en esas empresas.
func (c *cajaFalsa) vincular(usuario int64, nombre string, empresas ...int64) *usuarioArgos {
	c.mu.Lock()
	defer c.mu.Unlock()
	u := &usuarioArgos{nombre: nombre, activo: true, permisos: map[string]bool{"aprobar": true, "rechazar": true}, empresas: map[int64]bool{}}
	for _, e := range empresas {
		u.empresas[e] = true
	}
	c.vinculos[strconv.FormatInt(usuario, 10)] = u
	return u
}

func (c *cajaFalsa) comprobante(id, empresa int64, monto string) *comprobanteFalso {
	c.mu.Lock()
	defer c.mu.Unlock()
	registro, conv := "usuario:1", "iris:conversacion:1"
	f := &comprobanteFalso{id: id, empresa: empresa, canal: "iris", estado: "pendiente", monto: monto, irisActor: &registro, irisConversacion: &conv}
	c.comprobantes[id] = f
	return f
}

// lecturas cuenta los GET /comprobantes/:id que llegaron.
func (c *cajaFalsa) lecturas() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, p := range c.peticiones {
		if p.metodo == http.MethodGet {
			n++
		}
	}
	return n
}

// decisiones cuenta las peticiones de aprobar y rechazar que llegaron.
func (c *cajaFalsa) decisiones() []peticionCaja {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := []peticionCaja{}
	for _, p := range c.peticiones {
		if p.metodo == http.MethodPost {
			out = append(out, p)
		}
	}
	return out
}

func (c *cajaFalsa) problema(w http.ResponseWriter, status int, codigo string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	p := map[string]any{"type": "urn:argosisp:problema:" + codigo, "title": codigo, "status": status, "detail": "detalle interno de Argos"}
	if !c.sinCode {
		p["code"] = codigo
	}
	_ = json.NewEncoder(w).Encode(p)
}

func (c *cajaFalsa) json(f *comprobanteFalso) map[string]any {
	m := map[string]any{"id": f.id, "empresa_id": f.empresa, "cliente_id": 1201, "servicio_id": nil, "monto": f.monto, "canal": f.canal,
		"estado": f.estado, "pago_id": f.pago, "motivo_rechazo": f.motivo, "causa_rechazo": f.causa, "verificado_por": 9,
		"verificado_por_nombre": f.verificadoNombre, "iris_actor": f.irisActor, "iris_conversacion": f.irisConversacion}
	if !c.sin071 {
		m["verificado_iris_actor"] = f.verificadoActor
	}
	return m
}

// decidirEnPanel: caja resuelve el comprobante en el panel de Argos (nadie de Iris).
func (c *cajaFalsa) decidirEnPanel(id int64, estado, nombre string, motivo, causa *string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	f := c.comprobantes[id]
	f.estado, f.verificadoNombre, f.verificadoActor, f.motivo, f.causa, f.pago = estado, &nombre, nil, motivo, causa, nil
	if estado == "aprobado" {
		c.pagos++
		pago := 90000 + c.pagos
		f.pago = &pago
	}
}

func (c *cajaFalsa) anotar(r *http.Request, crudo []byte) peticionCaja {
	p := peticionCaja{metodo: r.Method, ruta: r.URL.Path, actor: r.Header.Get("X-Iris-Actor"), conversacion: r.Header.Get("X-Iris-Conversacion"),
		clave: r.Header.Get("Idempotency-Key"), empresa: r.Header.Get("X-Empresa-Id"), token: strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")}
	_ = json.Unmarshal(crudo, &p.cuerpo)
	c.peticiones = append(c.peticiones, p)
	return p
}

func (c *cajaFalsa) leer(w http.ResponseWriter, r *http.Request) {
	c.mu.Lock()
	defer c.mu.Unlock()
	p := c.anotar(r, nil)
	if c.caido {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	if p.actor == "" || p.conversacion == "" {
		c.problema(w, http.StatusBadRequest, "cabecera_iris_requerida")
		return
	}
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	f, ok := c.comprobantes[id]
	if !ok {
		c.problema(w, http.StatusNotFound, "no_encontrado")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(c.json(f))
}

func (c *cajaFalsa) decidir(w http.ResponseWriter, r *http.Request, accion string) {
	crudo, _ := io.ReadAll(r.Body)
	c.mu.Lock()
	p := c.anotar(r, crudo)
	if c.caido {
		c.mu.Unlock()
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	responder := func(status int, codigo string) {
		c.problema(w, status, codigo)
		c.mu.Unlock()
	}
	switch {
	case p.token != tokenCaja:
		responder(http.StatusUnauthorized, "no_autenticado")
		return
	case p.actor == "" || p.conversacion == "":
		responder(http.StatusBadRequest, "cabecera_iris_requerida")
		return
	case p.clave == "":
		responder(http.StatusBadRequest, "idempotency_key_requerida")
		return
	case c.apagado:
		responder(http.StatusConflict, "iris_apagado")
		return
	case c.enCurso > 0:
		c.enCurso--
		responder(http.StatusConflict, "idempotencia_en_curso")
		return
	}
	// §7.7: la huella incluye el actor; la misma clave con otro actor (u otro cuerpo) nunca es un replay.
	huella := fmt.Sprintf("%x", sha256.Sum256([]byte(r.Method+" "+r.URL.Path+"\n"+string(crudo)+"\n"+p.actor)))
	if g, ok := c.claves[p.clave]; ok {
		if g.huella != huella {
			responder(http.StatusUnprocessableEntity, "idempotency_key_reutilizada")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Idempotent-Replayed", "true")
		_, _ = w.Write(g.cuerpo)
		c.mu.Unlock()
		return
	}
	if p.actor == "asistente_ia" {
		responder(http.StatusForbidden, "actor_ia_no_permitido")
		return
	}
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	f, ok := c.comprobantes[id]
	if !ok {
		responder(http.StatusNotFound, "no_encontrado")
		return
	}
	u := c.vinculos[strings.TrimPrefix(p.actor, "usuario:")]
	switch {
	case u == nil:
		responder(http.StatusForbidden, "actor_sin_vinculo")
		return
	case !u.activo:
		responder(http.StatusForbidden, "actor_inactivo")
		return
	case !u.permisos[accion]:
		responder(http.StatusForbidden, "actor_sin_permiso")
		return
	case !u.empresas[f.empresa]:
		responder(http.StatusForbidden, "actor_sin_empresa")
		return
	case f.canal != "iris":
		responder(http.StatusConflict, "comprobante_no_es_de_iris")
		return
	case f.estado != "pendiente":
		responder(http.StatusConflict, "comprobante_revisado")
		return
	}
	actor := p.actor
	f.verificadoNombre, f.verificadoActor = &u.nombre, &actor
	if accion == "aprobar" {
		c.pagos++
		pago := 90000 + c.pagos
		f.estado, f.pago = "aprobado", &pago
	} else {
		motivo, _ := p.cuerpo["motivo"].(string)
		causa, _ := p.cuerpo["causa"].(string)
		if causa == "" {
			causa = "otro"
		}
		f.estado, f.motivo, f.causa = "rechazado", &motivo, &causa
	}
	cuerpo, _ := json.Marshal(c.json(f))
	c.claves[p.clave] = claveCaja{huella: huella, cuerpo: cuerpo}
	gancho := c.alDecidir
	c.mu.Unlock()
	if gancho != nil {
		gancho(id)
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(cuerpo)
}

// La bandeja usa el Argos falso por HTTP ----------------------------------------------------------------

func (f *argosFalso) ApruebaComprobantes(context.Context, conector.DBTX) (bool, error) {
	if f.caja == nil {
		return false, nil
	}
	f.caja.mu.Lock()
	defer f.caja.mu.Unlock()
	return f.caja.contrato, nil
}

func (f *argosFalso) AprobarComprobante(ctx context.Context, a argos.AtencionIris, id int64, facturas []int64, nota string) (argos.Comprobante, error) {
	if f.caja == nil {
		return argos.Comprobante{}, argos.SinAprobacion()
	}
	return f.caja.cliente().AprobarComprobante(ctx, a, id, facturas, nota)
}

func (f *argosFalso) RechazarComprobante(ctx context.Context, a argos.AtencionIris, id int64, motivo, causa string) (argos.Comprobante, error) {
	if f.caja == nil {
		return argos.Comprobante{}, argos.SinAprobacion()
	}
	return f.caja.cliente().RechazarComprobante(ctx, a, id, motivo, causa)
}

func (f *argosFalso) ObtenerComprobante(ctx context.Context, a argos.AtencionIris, id int64) (argos.Comprobante, error) {
	if f.caja == nil {
		return argos.Comprobante{}, argos.SinAprobacion()
	}
	return f.caja.cliente().ObtenerComprobante(ctx, a, id)
}

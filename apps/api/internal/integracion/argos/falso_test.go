package argos_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// argosFalso es un servidor de Argos que cumple el contrato (§1.3 y §2.1) para las pruebas.
// Responde a propósito con estilos distintos (camelCase, { data }, arreglos planos, números
// como texto) para cubrir la lectura tolerante del cliente.
type argosFalso struct {
	t   *testing.T
	srv *httptest.Server

	mu         sync.Mutex
	token      string
	codigo     string
	canjes     []map[string]any
	clientes   map[int64]map[string]any
	servicios  map[int64][]map[string]any
	estados    map[int64]map[string]any
	ips        map[string]map[string]any
	zonas      []map[string]any
	routers    []map[string]any
	caido      bool
	fallarUna  int // responde 503 a las próximas n peticiones
	sinA8      bool
	llamadas   map[string]int
	sinPermiso bool
	conectorID any // lo que responde el canje en conector_id (nil = "77")
}

const tokenArgos = "atk_prueba_0123456789abcdef"

func nuevoArgosFalso(t *testing.T) *argosFalso {
	a := &argosFalso{
		t: t, token: tokenArgos, codigo: "codigo-de-argos-0123456789",
		clientes: map[int64]map[string]any{}, servicios: map[int64][]map[string]any{},
		estados: map[int64]map[string]any{}, ips: map[string]map[string]any{}, llamadas: map[string]int{},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/integraciones/iris/conexion/canjear", a.canjear)
	mux.HandleFunc("GET /api/v1/clientes", a.conToken(a.buscar))
	mux.HandleFunc("GET /api/v1/clientes/{id}", a.conToken(a.cliente))
	mux.HandleFunc("GET /api/v1/clientes/{id}/estado-cuenta", a.conToken(a.estadoCuenta))
	mux.HandleFunc("GET /api/v1/servicios", a.conToken(a.serviciosDe))
	mux.HandleFunc("GET /api/v1/servicios/{id}", a.conToken(a.servicio))
	mux.HandleFunc("POST /api/v1/integraciones/iris/resolver", a.conToken(a.resolverA8))
	mux.HandleFunc("GET /api/v1/ip/{ip}", a.conToken(a.resolverIP))
	mux.HandleFunc("GET /api/v1/zonas", a.conToken(func(w http.ResponseWriter, r *http.Request) { a.paginar(w, r, a.zonas) }))
	mux.HandleFunc("GET /api/v1/routers", a.conToken(func(w http.ResponseWriter, r *http.Request) { a.paginar(w, r, a.routers) }))
	a.srv = httptest.NewServer(mux)
	t.Cleanup(a.srv.Close)
	return a
}

func (a *argosFalso) enlace() string { return a.srv.URL + "/conectar#" + a.codigo }

func (a *argosFalso) llamadasA(ruta string) int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.llamadas[ruta]
}

func (a *argosFalso) con(fn func()) {
	a.mu.Lock()
	defer a.mu.Unlock()
	fn()
}

func problema(w http.ResponseWriter, status int, tipo, detalle string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"type": tipo, "title": http.StatusText(status), "status": status, "detail": detalle})
}

func escribir(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func (a *argosFalso) canjear(w http.ResponseWriter, r *http.Request) {
	var p map[string]any
	_ = json.NewDecoder(r.Body).Decode(&p)
	a.mu.Lock()
	defer a.mu.Unlock()
	a.canjes = append(a.canjes, p)
	if r.Header.Get("Authorization") != "" {
		problema(w, 400, "urn:argos:token_no_esperado", "El canje no lleva token")
		return
	}
	if p["codigo"] != a.codigo || !canjeCanonico(p) {
		problema(w, 422, "urn:argos:codigo_invalido", "El código venció o ya se usó")
		return
	}
	a.codigo = "usado-" + a.codigo
	// Como Argos 1.3.0: su token, su receptor (reservado) y el id del conector allá. Las empresas no
	// las manda Argos; el falso las agrega para registrar la empresa 1 (campo opcional).
	escribir(w, map[string]any{"token": a.token, "webhook": map[string]any{"url": "https://argos.prueba/api/v1/integraciones/iris/webhook", "secreto": "secreto-de-argos-para-iris"},
		"conector_id": a.idDelCanje(), "empresas": []any{map[string]any{"id": 1}}})
}

func (a *argosFalso) idDelCanje() any {
	if a.conectorID != nil {
		return a.conectorID
	}
	return "77"
}

func (a *argosFalso) conToken(fn http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		a.mu.Lock()
		a.llamadas[r.URL.Path]++
		caido := a.caido || a.fallarUna > 0
		if a.fallarUna > 0 {
			a.fallarUna--
		}
		sinPermiso := a.sinPermiso
		a.mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer "+a.token {
			problema(w, 401, "urn:argos:no_autenticado", "Token inválido")
			return
		}
		if caido {
			problema(w, 503, "urn:argos:no_disponible", "Mantenimiento")
			return
		}
		if sinPermiso {
			problema(w, 403, "urn:argos:prohibido", "Falta el permiso")
			return
		}
		a.mu.Lock()
		defer a.mu.Unlock()
		fn(w, r)
	}
}

func (a *argosFalso) buscar(w http.ResponseWriter, r *http.Request) {
	q := strings.ToLower(r.URL.Query().Get("q"))
	data := []any{}
	for _, c := range a.clientes {
		if strings.Contains(strings.ToLower(c["nombre"].(string)), q) {
			data = append(data, c)
		}
	}
	escribir(w, map[string]any{"data": data, "nextCursor": nil})
}

func idDe(r *http.Request) int64 {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	return id
}

func (a *argosFalso) cliente(w http.ResponseWriter, r *http.Request) {
	c, ok := a.clientes[idDe(r)]
	if !ok {
		problema(w, 404, "urn:argos:no_encontrado", "No existe")
		return
	}
	escribir(w, map[string]any{"data": c})
}

func (a *argosFalso) estadoCuenta(w http.ResponseWriter, r *http.Request) {
	e, ok := a.estados[idDe(r)]
	if !ok {
		problema(w, 404, "urn:argos:no_encontrado", "No existe")
		return
	}
	escribir(w, e)
}

func (a *argosFalso) serviciosDe(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.URL.Query().Get("cliente_id"), 10, 64)
	lista := a.servicios[id]
	if lista == nil {
		lista = []map[string]any{}
	}
	escribir(w, lista) // arreglo plano, sin { data }
}

func (a *argosFalso) servicio(w http.ResponseWriter, r *http.Request) {
	for _, lista := range a.servicios {
		for _, s := range lista {
			if id, _ := s["id"].(int64); id == idDe(r) {
				escribir(w, s)
				return
			}
		}
	}
	problema(w, 404, "urn:argos:no_encontrado", "No existe")
}

func (a *argosFalso) resolverA8(w http.ResponseWriter, r *http.Request) {
	if a.sinA8 {
		problema(w, 404, "urn:argos:ruta", "Ruta inexistente")
		return
	}
	var p struct {
		IP string `json:"ip"`
	}
	_ = json.NewDecoder(r.Body).Decode(&p)
	res, ok := a.ips[p.IP]
	if !ok {
		problema(w, 404, "urn:argos:no_encontrado", "IP sin cliente")
		return
	}
	escribir(w, res)
}

func (a *argosFalso) resolverIP(w http.ResponseWriter, r *http.Request) {
	res, ok := a.ips[r.PathValue("ip")]
	if !ok {
		problema(w, 404, "urn:argos:no_encontrado", "IP sin cliente")
		return
	}
	// El respaldo responde anidado.
	escribir(w, map[string]any{"servicio": map[string]any{"id": res["servicio_id"], "clienteId": res["cliente_id"], "empresaId": res["empresa_id"]}})
}

// paginar devuelve de a 2 elementos con cursor.
func (a *argosFalso) paginar(w http.ResponseWriter, r *http.Request, todos []map[string]any) {
	desde, _ := strconv.Atoi(r.URL.Query().Get("cursor"))
	hasta := min(desde+2, len(todos))
	var siguiente any
	if hasta < len(todos) {
		siguiente = strconv.Itoa(hasta)
	}
	escribir(w, map[string]any{"data": todos[desde:hasta], "nextCursor": siguiente})
}

// canjeCanonico valida el cuerpo del canje como el contrato Argos ↔ Horus §1.4 (y Argos 1.3.0):
// sistema "iris", base_url, el token iris_pat_ que Iris emite para el otro sistema y su receptor de
// webhooks con el secreto.
func canjeCanonico(p map[string]any) bool {
	token, _ := p["token"].(string)
	base, _ := p["base_url"].(string)
	wh, _ := p["webhook"].(map[string]any)
	url, _ := wh["url"].(string)
	secreto, _ := wh["secreto"].(string)
	return p["sistema"] == "iris" && base != "" && strings.HasPrefix(token, "iris_pat_") && url != "" && len(secreto) >= 16
}

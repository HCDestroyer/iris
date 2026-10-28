package horus_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// horusFalso es un Horus que cumple lo que Iris supone del contrato (canje del enlace y
// lecturas de §1.1). Responde en camelCase, envuelto en { data } y con números como texto,
// para cubrir la lectura tolerante.
type horusFalso struct {
	srv *httptest.Server

	mu         sync.Mutex
	token      string
	codigo     string
	canjes     []map[string]any
	tokenMalo  bool
	sinPermiso bool
}

const tokenHorus = "hf_pat_prueba_0123456789abcdef"

func nuevoHorusFalso(t *testing.T) *horusFalso {
	h := &horusFalso{token: tokenHorus, codigo: "codigo-de-horus-0123456789"}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/integrations/iris/conexion/canjear", h.canjear)
	mux.HandleFunc("GET /api/v1/integrations/iris/servicios/{id}/salud", h.conToken(func(w http.ResponseWriter, r *http.Request) {
		escribir(w, map[string]any{"data": map[string]any{
			"servicioId": r.PathValue("id"), "estado": "en_linea", "bajadaBps": "12500000", "subidaBps": 2500000,
			"latenciaMs": 18.5, "actualizadoEn": "2026-10-28T10:00:00Z",
		}})
	}))
	mux.HandleFunc("GET /api/v1/integrations/iris/routers/{id}", h.conToken(func(w http.ResponseWriter, r *http.Request) {
		escribir(w, map[string]any{"id": r.PathValue("id"), "nombre": "RB-Centro", "estado": "down", "desde": "2026-10-28T09:58:00Z"})
	}))
	h.srv = httptest.NewServer(mux)
	t.Cleanup(h.srv.Close)
	return h
}

func (h *horusFalso) enlace() string { return h.srv.URL + "/conectar#" + h.codigo }

func (h *horusFalso) con(fn func()) {
	h.mu.Lock()
	defer h.mu.Unlock()
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

func (h *horusFalso) canjear(w http.ResponseWriter, r *http.Request) {
	var p map[string]any
	_ = json.NewDecoder(r.Body).Decode(&p)
	h.mu.Lock()
	defer h.mu.Unlock()
	h.canjes = append(h.canjes, p)
	if r.Header.Get("Authorization") != "" {
		problema(w, 400, "urn:horus:token_no_esperado", "El canje no lleva token")
		return
	}
	if p["codigo"] != h.codigo || p["sistema"] != "iris" || p["webhook_secreto"] == "" || p["webhook_url"] == "" {
		problema(w, 422, "urn:horus:codigo_invalido", "El código venció o ya se usó (texto que no debe llegar al panel)")
		return
	}
	h.codigo = "usado-" + h.codigo
	token := h.token
	if h.tokenMalo {
		token = "atk_no_es_de_horus_0123456789"
	}
	escribir(w, map[string]any{"data": map[string]any{"token": token, "conexionId": "hf-77"}})
}

func (h *horusFalso) conToken(fn http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		h.mu.Lock()
		sinPermiso := h.sinPermiso
		h.mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer "+h.token || sinPermiso {
			problema(w, 403, "urn:horus:prohibido", "Falta traffic.customer.read")
			return
		}
		fn(w, r)
	}
}

// argosFalso es lo mínimo de Argos que usa Horus: resolver una IP (A8).
type argosFalso struct {
	srv *httptest.Server

	mu    sync.Mutex
	ips   map[string]map[string]any
	caido bool
}

const tokenArgos = "atk_prueba_0123456789abcdef"

func nuevoArgosFalso(t *testing.T) *argosFalso {
	a := &argosFalso{ips: map[string]map[string]any{}}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/integraciones/iris/resolver", func(w http.ResponseWriter, r *http.Request) {
		a.mu.Lock()
		defer a.mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer "+tokenArgos {
			problema(w, 401, "urn:argos:no_autenticado", "Token inválido")
			return
		}
		if a.caido {
			problema(w, 503, "urn:argos:no_disponible", "Mantenimiento")
			return
		}
		var p struct {
			IP string `json:"ip"`
		}
		_ = json.NewDecoder(r.Body).Decode(&p)
		res, ok := a.ips[p.IP]
		if !ok {
			problema(w, 404, "urn:argos:no_encontrado", "IP sin servicio")
			return
		}
		escribir(w, res)
	})
	a.srv = httptest.NewServer(mux)
	t.Cleanup(a.srv.Close)
	return a
}

func (a *argosFalso) con(fn func()) {
	a.mu.Lock()
	defer a.mu.Unlock()
	fn()
}

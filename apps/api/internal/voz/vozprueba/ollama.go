// Package vozprueba trae un Ollama falso para probar la puerta sin GPU: responde lo mínimo de la
// API de Ollama que la puerta usa y recuerda lo que recibió. Lo usan las pruebas de Go y el compose
// de CI (cmd/voz-ollama-falso). No es parte del paquete que se instala.
package vozprueba

import (
	"encoding/json"
	"io"
	"net/http"
	"sync"
	"time"
)

// Peticion es lo que recibió el Ollama falso.
type Peticion struct {
	Metodo    string
	Ruta      string
	Cabeceras http.Header
	Cuerpo    []byte
}

// Ollama es el Ollama falso.
type Ollama struct {
	mu sync.Mutex
	// Modelos instalados (nombre → tamaño en bytes).
	Modelos map[string]int64
	// Cargados en memoria (nombre → bytes de VRAM).
	Cargados map[string]int64
	// Demora antes de contestar un chat.
	Demora time.Duration
	// Respuesta del chat (por defecto, una respuesta corta válida) y su estado (200).
	Respuesta []byte
	Estado    int
	// Responder: si no es nil, decide la respuesta de cada chat por su cuerpo (nil = la de siempre).
	Responder func(cuerpo []byte) []byte
	// Caido: toda petición responde 503.
	Caido bool
	// Redirigir: el chat responde 307 hacia esta dirección.
	Redirigir  string
	peticiones []Peticion
}

// Nuevo crea un Ollama falso con esos modelos instalados.
func Nuevo(modelos ...string) *Ollama {
	o := &Ollama{Modelos: map[string]int64{}, Cargados: map[string]int64{}}
	for _, m := range modelos {
		o.Modelos[m] = 2 << 30
	}
	return o
}

// Cambiar modifica el falso con el candado tomado.
func (o *Ollama) Cambiar(f func(*Ollama)) {
	o.mu.Lock()
	defer o.mu.Unlock()
	f(o)
}

// Peticiones devuelve lo recibido hasta ahora.
func (o *Ollama) Peticiones() []Peticion {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]Peticion(nil), o.peticiones...)
}

// Chats devuelve solo las peticiones de chat.
func (o *Ollama) Chats() []Peticion {
	var r []Peticion
	for _, p := range o.Peticiones() {
		if p.Ruta == "/v1/chat/completions" {
			r = append(r, p)
		}
	}
	return r
}

// RespuestaPorDefecto es una respuesta de chat válida con uso de tokens.
const RespuestaPorDefecto = `{"id":"chatcmpl-1","object":"chat.completion","model":"falso","choices":[{"index":0,"message":{"role":"assistant","content":"Hola, soy el modelo de prueba."},"finish_reason":"stop"}],"usage":{"prompt_tokens":12,"completion_tokens":7,"total_tokens":19}}`

func (o *Ollama) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	cuerpo, _ := io.ReadAll(io.LimitReader(r.Body, 8<<20))
	o.mu.Lock()
	o.peticiones = append(o.peticiones, Peticion{Metodo: r.Method, Ruta: r.URL.Path, Cabeceras: r.Header.Clone(), Cuerpo: cuerpo})
	caido, demora, resp, estado, redirigir, responder := o.Caido, o.Demora, o.Respuesta, o.Estado, o.Redirigir, o.Responder
	type m struct {
		Name      string    `json:"name"`
		Model     string    `json:"model"`
		Size      int64     `json:"size"`
		Digest    string    `json:"digest"`
		SizeVRAM  int64     `json:"size_vram,omitempty"`
		ExpiresAt time.Time `json:"expires_at,omitempty"`
	}
	var tags, ps []m
	for n, t := range o.Modelos {
		tags = append(tags, m{Name: n, Model: n, Size: t, Digest: "sha256:falso"})
	}
	for n, v := range o.Cargados {
		ps = append(ps, m{Name: n, Model: n, Size: v, SizeVRAM: v, ExpiresAt: time.Now().Add(5 * time.Minute)})
	}
	o.mu.Unlock()
	if caido {
		http.Error(w, "caído", http.StatusServiceUnavailable)
		return
	}
	escribir := func(v any) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(v)
	}
	switch r.Method + " " + r.URL.Path {
	case "GET /api/tags":
		escribir(map[string]any{"models": tags})
	case "GET /api/ps":
		escribir(map[string]any{"models": ps})
	case "POST /v1/chat/completions":
		if redirigir != "" {
			http.Redirect(w, r, redirigir, http.StatusTemporaryRedirect)
			return
		}
		if demora > 0 {
			select {
			case <-time.After(demora):
			case <-r.Context().Done():
				return
			}
		}
		if responder != nil {
			if propia := responder(cuerpo); propia != nil {
				resp = propia
			}
		}
		if resp == nil {
			resp = []byte(RespuestaPorDefecto)
		}
		if estado == 0 {
			estado = http.StatusOK
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Ollama-Interno", "no debe salir")
		w.WriteHeader(estado)
		_, _ = w.Write(resp)
	default:
		http.NotFound(w, r)
	}
}

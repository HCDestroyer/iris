package voz

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
)

// ReNombreModelo: forma de un nombre de modelo (también la valida Iris al leer).
var ReNombreModelo = regexp.MustCompile(`^[a-zA-Z0-9._:/-]{1,128}$`)

var reNombreFuncion = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

// modeloOllama es lo que la puerta sabe de un modelo del contenedor llm.
type modeloOllama struct {
	Nombre  string
	Tamano  int64 // bytes en disco
	Digest  string
	Cargado bool
	VRAM    int64 // bytes en la GPU
	Hasta   time.Time
}

// cacheOllama guarda 2 s la lista de modelos (instalados y cargados) para no cargar a Ollama.
type cacheOllama struct {
	mu      sync.Mutex
	en      time.Time
	modelos []modeloOllama
	err     error
}

func (p *Puerta) pedirOllama(ctx context.Context, ruta string, destino any) error {
	ctx, cancelar := context.WithTimeout(ctx, 2*time.Second)
	defer cancelar()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.cfg.OllamaURL+ruta, nil)
	if err != nil {
		return err
	}
	resp, err := p.ollama.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return errors.New("ollama no responde")
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(destino)
}

// catalogo devuelve los modelos instalados (con nombre válido), con su estado de carga.
func (p *Puerta) catalogo(ctx context.Context) ([]modeloOllama, error) {
	c := &p.cache
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.en.IsZero() && time.Since(c.en) < 2*time.Second {
		return c.modelos, c.err
	}
	var tags struct {
		Models []struct {
			Name   string `json:"name"`
			Size   int64  `json:"size"`
			Digest string `json:"digest"`
		} `json:"models"`
	}
	var ps struct {
		Models []struct {
			Name      string    `json:"name"`
			SizeVRAM  int64     `json:"size_vram"`
			ExpiresAt time.Time `json:"expires_at"`
		} `json:"models"`
	}
	c.en, c.modelos = time.Now(), nil
	if c.err = p.pedirOllama(ctx, "/api/tags", &tags); c.err != nil {
		return nil, c.err
	}
	// /api/ps es informativo: si falla, los modelos salen como no cargados.
	_ = p.pedirOllama(ctx, "/api/ps", &ps)
	for _, t := range tags.Models {
		if !ReNombreModelo.MatchString(t.Name) || len(c.modelos) >= 256 {
			continue
		}
		m := modeloOllama{Nombre: t.Name, Tamano: max(t.Size, 0), Digest: t.Digest}
		for _, x := range ps.Models {
			if x.Name == t.Name {
				m.Cargado, m.VRAM, m.Hasta = true, max(x.SizeVRAM, 0), x.ExpiresAt
			}
		}
		c.modelos = append(c.modelos, m)
	}
	sort.Slice(c.modelos, func(i, j int) bool { return c.modelos[i].Nombre < c.modelos[j].Nombre })
	return c.modelos, nil
}

// utilizables: instalados ∩ modelosPermitidos.
func utilizables(todos []modeloOllama, e Estado) []modeloOllama {
	var r []modeloOllama
	for _, m := range todos {
		if slices.Contains(e.ModelosPermitidos, m.Nombre) {
			r = append(r, m)
		}
	}
	return r
}

// maxSimultaneas: el tope de peticiones al modelo a la vez. Si no se fijó, se calcula para el
// conjunto de modelos permitidos: el peor caso de VRAM (el mayor de ellos, con margen) y, por cada
// plaza más, el coste de un contexto. Sin GPU, una. Es una estimación hasta medir en una GPU real.
func (p *Puerta) maxSimultaneas(modelos []modeloOllama) int {
	if p.cfg.MaxSimultaneas > 0 {
		return p.cfg.MaxSimultaneas
	}
	gpus, ok := p.leerGPU()
	if !ok || len(gpus) == 0 {
		return 1
	}
	var total, peor int64
	for _, g := range gpus {
		if g.VRAMTotalMb != nil {
			total += int64(*g.VRAMTotalMb)
		}
	}
	for _, m := range modelos {
		peor = max(peor, m.Tamano>>20)
	}
	const porPlazaMb = 2048
	n := 1 + (total-peor*12/10)/porPlazaMb
	return int(min(max(n, 1), 8))
}

// --- Petición de chat: lista cerrada de campos y reserialización ---------------------------------

type chatFuncion struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Strict      *bool           `json:"strict,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

type chatHerramienta struct {
	Type     string      `json:"type"`
	Function chatFuncion `json:"function"`
}

type chatLlamada struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type chatMensaje struct {
	Role       string        `json:"role"`
	Content    string        `json:"content"`
	ToolCalls  []chatLlamada `json:"tool_calls,omitempty"`
	ToolCallID string        `json:"tool_call_id,omitempty"`
}

type chatFormato struct {
	Type       string          `json:"type"`
	JSONSchema json.RawMessage `json:"json_schema,omitempty"`
}

type chatPeticion struct {
	Model          string            `json:"model"`
	Messages       []chatMensaje     `json:"messages"`
	Stream         *bool             `json:"stream,omitempty"`
	Temperature    *float64          `json:"temperature,omitempty"`
	MaxTokens      *int              `json:"max_tokens,omitempty"`
	ResponseFormat *chatFormato      `json:"response_format,omitempty"`
	Tools          []chatHerramienta `json:"tools,omitempty"`
	ToolChoice     *string           `json:"tool_choice,omitempty"`
}

type errorChat struct{ codigo, detalle string }

func (e *errorChat) Error() string { return e.codigo }

// profundidadMaxJSON: anidamiento máximo de un esquema (parameters, json_schema).
const profundidadMaxJSON = 32

// recodificarObjeto decodifica un objeto JSON libre (el esquema de una herramienta o de la
// respuesta) y lo vuelve a codificar: profundidad acotada y ninguna clave repetida en ningún nivel.
// Lo que sale hacia Ollama es la recodificación, nunca los bytes del cliente.
func recodificarObjeto(crudo json.RawMessage) (json.RawMessage, bool) {
	dec := json.NewDecoder(bytes.NewReader(crudo))
	dec.UseNumber()
	v, ok := leerValorJSON(dec, 1)
	if !ok || dec.More() {
		return nil, false
	}
	if _, esObjeto := v.(objetoJSON); !esObjeto {
		return nil, false
	}
	out, err := json.Marshal(v)
	return out, err == nil
}

// objetoJSON conserva el orden de las claves al recodificar.
type objetoJSON []campoJSON

type campoJSON struct {
	clave string
	valor any
}

func (o objetoJSON) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, c := range o {
		if i > 0 {
			b.WriteByte(',')
		}
		k, err := json.Marshal(c.clave)
		if err != nil {
			return nil, err
		}
		v, err := json.Marshal(c.valor)
		if err != nil {
			return nil, err
		}
		b.Write(k)
		b.WriteByte(':')
		b.Write(v)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

func leerValorJSON(dec *json.Decoder, nivel int) (any, bool) {
	if nivel > profundidadMaxJSON {
		return nil, false
	}
	t, err := dec.Token()
	if err != nil {
		return nil, false
	}
	d, esDelim := t.(json.Delim)
	if !esDelim {
		return t, true
	}
	switch d {
	case '{':
		o, vistas := objetoJSON{}, map[string]bool{}
		for dec.More() {
			k, err := dec.Token()
			clave, esTexto := k.(string)
			if err != nil || !esTexto || vistas[clave] {
				return nil, false
			}
			vistas[clave] = true
			v, ok := leerValorJSON(dec, nivel+1)
			if !ok {
				return nil, false
			}
			o = append(o, campoJSON{clave, v})
		}
		if _, err := dec.Token(); err != nil {
			return nil, false
		}
		return o, true
	case '[':
		a := []any{}
		for dec.More() {
			v, ok := leerValorJSON(dec, nivel+1)
			if !ok {
				return nil, false
			}
			a = append(a, v)
		}
		if _, err := dec.Token(); err != nil {
			return nil, false
		}
		return a, true
	}
	return nil, false
}

// reserializar decodifica la petición con la lista cerrada de campos, la valida y arma el cuerpo
// nuevo que va a Ollama. Lo que escribió el cliente nunca se reenvía tal cual: un campo
// desconocido (keep_alive, options, num_ctx…) es un 422, y una clave repetida queda en una sola.
func reserializar(cuerpo []byte, maxTokens int) (chatPeticion, []byte, error) {
	var in chatPeticion
	dec := json.NewDecoder(bytes.NewReader(cuerpo))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		if strings.Contains(err.Error(), "unknown field") {
			return in, nil, &errorChat{"campo_no_permitido", ""}
		}
		return in, nil, &errorChat{"peticion_invalida", ""}
	}
	if dec.More() {
		return in, nil, &errorChat{"peticion_invalida", ""}
	}
	mal := func(d string) (chatPeticion, []byte, error) { return in, nil, &errorChat{"peticion_invalida", d} }
	if !ReNombreModelo.MatchString(in.Model) {
		return mal("model")
	}
	if len(in.Messages) == 0 || len(in.Messages) > 512 {
		return mal("messages")
	}
	for _, m := range in.Messages {
		switch m.Role {
		case "system", "user", "assistant", "tool":
		default:
			return mal("role")
		}
		if len(m.ToolCalls) > 32 || len(m.ToolCallID) > 128 {
			return mal("tool_calls")
		}
		for _, l := range m.ToolCalls {
			if l.Type != "function" || !reNombreFuncion.MatchString(l.Function.Name) || len(l.ID) > 128 {
				return mal("tool_calls")
			}
		}
	}
	if in.Stream != nil && *in.Stream {
		return in, nil, &errorChat{"flujo_no_disponible", ""}
	}
	if in.Temperature != nil && (*in.Temperature < 0 || *in.Temperature > 2) {
		return mal("temperature")
	}
	if in.MaxTokens == nil {
		in.MaxTokens = &maxTokens
	}
	if *in.MaxTokens < 1 || *in.MaxTokens > maxTokens {
		return mal("max_tokens")
	}
	if f := in.ResponseFormat; f != nil {
		switch f.Type {
		case "text", "json_object":
			if f.JSONSchema != nil {
				return mal("response_format")
			}
		case "json_schema":
			limpio, ok := recodificarObjeto(f.JSONSchema)
			if !ok {
				return mal("response_format")
			}
			f.JSONSchema = limpio
		default:
			return mal("response_format")
		}
	}
	if len(in.Tools) > 64 {
		return mal("tools")
	}
	for i, h := range in.Tools {
		if h.Type != "function" || !reNombreFuncion.MatchString(h.Function.Name) || len(h.Function.Description) > 4096 {
			return mal("tools")
		}
		if h.Function.Parameters != nil {
			limpio, ok := recodificarObjeto(h.Function.Parameters)
			if !ok {
				return mal("tools")
			}
			in.Tools[i].Function.Parameters = limpio
		}
	}
	if in.ToolChoice != nil && *in.ToolChoice != "auto" && *in.ToolChoice != "none" {
		return mal("tool_choice")
	}
	no := false
	in.Stream = &no
	salida, err := json.Marshal(in)
	if err != nil {
		return mal("")
	}
	return in, salida, nil
}

func (p *Puerta) chat(o respuesta, r *http.Request, cuerpo []byte, e Estado) int {
	fallo := func(estado int, codigo, detalle string) int {
		o.error(estado, codigo, detalle)
		return estado
	}
	in, salida, err := reserializar(cuerpo, p.cfg.MaxTokens)
	if err != nil {
		var ec *errorChat
		errors.As(err, &ec)
		return fallo(http.StatusUnprocessableEntity, ec.codigo, ec.detalle)
	}
	todos, err := p.catalogo(r.Context())
	if err != nil {
		return fallo(http.StatusServiceUnavailable, "llm_no_disponible", "")
	}
	permitidos := utilizables(todos, e)
	if !slices.ContainsFunc(permitidos, func(m modeloOllama) bool { return m.Nombre == in.Model }) {
		// Nunca se dispara una descarga: el modelo está instalado y permitido, o no se usa.
		return fallo(http.StatusUnprocessableEntity, "modelo_no_instalado", "")
	}
	if !p.reservar(p.maxSimultaneas(permitidos)) {
		return fallo(http.StatusServiceUnavailable, "sin_capacidad", "")
	}
	defer p.soltar()

	ctx, cancelar := context.WithTimeout(r.Context(), p.cfg.PlazoLLM)
	defer cancelar()
	// Petición nueva: Content-Length propio y ninguna cabecera del cliente.
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.cfg.OllamaURL+"/v1/chat/completions", bytes.NewReader(salida))
	if err != nil {
		return fallo(http.StatusInternalServerError, "error", "")
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := p.ollama.Do(req)
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return fallo(http.StatusGatewayTimeout, "tiempo_agotado", "")
		}
		return fallo(http.StatusBadGateway, "llm_no_disponible", "")
	}
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(io.LimitReader(resp.Body, TopeRespuestaLLM+1))
	switch {
	case err != nil && errors.Is(ctx.Err(), context.DeadlineExceeded):
		return fallo(http.StatusGatewayTimeout, "tiempo_agotado", "")
	case err != nil:
		return fallo(http.StatusBadGateway, "llm_no_disponible", "")
	case len(b) > TopeRespuestaLLM:
		return fallo(http.StatusBadGateway, "respuesta_demasiado_grande", "")
	case resp.StatusCode != http.StatusOK:
		// El cuerpo de un error de Ollama no se reenvía (puede repetir parte de la petición).
		return fallo(http.StatusBadGateway, "llm_error", http.StatusText(resp.StatusCode))
	case !json.Valid(b):
		return fallo(http.StatusBadGateway, "respuesta_invalida", "")
	}
	o.crudo(http.StatusOK, b)
	return http.StatusOK
}

type modeloLista struct {
	ID     string `json:"id"`
	Object string `json:"object"`
}

func (p *Puerta) modelos(o respuesta, r *http.Request, e Estado) int {
	todos, err := p.catalogo(r.Context())
	if err != nil {
		o.error(http.StatusServiceUnavailable, "llm_no_disponible", "")
		return http.StatusServiceUnavailable
	}
	datos := []modeloLista{}
	for _, m := range utilizables(todos, e) {
		datos = append(datos, modeloLista{ID: m.Nombre, Object: "model"})
	}
	o.json(http.StatusOK, struct {
		Object string        `json:"object"`
		Data   []modeloLista `json:"data"`
	}{"list", datos})
	return http.StatusOK
}

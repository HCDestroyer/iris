package mensajeria

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"syscall"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// Proveedores de IA del asistente (contratos/conversaciones.md §16.1). Dos adaptadores detrás de
// Proveedor: el compatible con OpenAI (Ollama, ChatGPT, OpenRouter, Groq, vLLM, LM Studio…) y
// Anthropic (SDK oficial). Los adaptadores solo traducen el pedido: nada de reglas de negocio.

// Proveedores admitidos.
const (
	ProveedorOllama           = "ollama"
	ProveedorOpenAICompatible = "openai_compatible"
	ProveedorAnthropic        = "anthropic"
)

// AnthropicURLPorDefecto y AnthropicModeloPorDefecto: si el admin los deja vacíos.
const (
	AnthropicURLPorDefecto    = "https://api.anthropic.com"
	AnthropicModeloPorDefecto = "claude-opus-5-5"
)

// PedidoIA es lo que se le pide al modelo: instrucciones fijas (cacheables) y el contexto del caso.
type PedidoIA struct {
	Modelo        string
	Instrucciones string
	Contenido     string
	MaxTokens     int64
}

// RespuestaIA es el texto que devolvió el modelo.
type RespuestaIA struct {
	Texto string
	// Rechazo: el modelo se negó (stop_reason refusal, finish_reason content_filter).
	Rechazo bool
	// Truncada: se cortó por max_tokens.
	Truncada bool
}

// UsoIA son los tokens de una llamada (entrada incluye la caché).
type UsoIA struct {
	Entrada int64
	Salida  int64
}

// ModeloIA es un modelo que ofrece el proveedor.
type ModeloIA struct {
	ID     string `json:"id"`
	Nombre string `json:"nombre"`
}

// Proveedor genera texto y lista modelos.
type Proveedor interface {
	Generar(ctx context.Context, p PedidoIA) (RespuestaIA, UsoIA, error)
	Modelos(ctx context.Context) ([]ModeloIA, error)
}

// ErrorIA es una falla del proveedor con su código (§16.2) y un detalle en español sin secretos.
type ErrorIA struct {
	Codigo  string
	Detalle string
}

func (e *ErrorIA) Error() string { return e.Codigo + ": " + e.Detalle }

// Códigos de error de una sugerencia.
const (
	ErrIANoResponde   = "proveedor_no_responde"
	ErrIAProveedor    = "proveedor_error"
	ErrIAClave        = "clave_invalida"
	ErrIATope         = "tope_alcanzado"
	ErrIASinConfig    = "sin_configurar"
	ErrIARechazo      = "rechazo_modelo"
	ErrIARespInvalida = "respuesta_invalida"
)

// ConfigProveedor es la conexión ya resuelta (la clave en claro solo vive en memoria).
type ConfigProveedor struct {
	Proveedor string
	URL       string
	Clave     string
	Modelo    string
	// PermitirLoopback: IRIS_ASISTENTE_PERMITIR_LOOPBACK (Ollama en el mismo host).
	PermitirLoopback bool
}

// FabricaProveedor arma el proveedor de una configuración (pruebas: uno falso).
type FabricaProveedor func(cfg ConfigProveedor, cliente *http.Client) (Proveedor, error)

// NuevoProveedor es la fábrica real.
func NuevoProveedor(cfg ConfigProveedor, cliente *http.Client) (Proveedor, error) {
	switch cfg.Proveedor {
	case ProveedorOllama, ProveedorOpenAICompatible:
		base, err := baseOpenAI(cfg.URL)
		if err != nil {
			return nil, err
		}
		if err := validarURLIA(base, cfg.PermitirLoopback, cfg.Clave != ""); err != nil {
			return nil, err
		}
		return &proveedorOpenAI{base: base, clave: cfg.Clave, http: cliente}, nil
	case ProveedorAnthropic:
		u := NormalizarURLIA(cfg.Proveedor, cfg.URL)
		if err := validarURLIA(u, cfg.PermitirLoopback, cfg.Clave != ""); err != nil {
			return nil, err
		}
		cli := anthropic.NewClient(option.WithoutEnvironmentDefaults(), option.WithBaseURL(u+"/"), option.WithAPIKey(cfg.Clave),
			option.WithHTTPClient(cliente), option.WithMaxRetries(1))
		return &proveedorAnthropic{cli: cli}, nil
	}
	return nil, &ErrorIA{Codigo: ErrIASinConfig, Detalle: "Proveedor desconocido"}
}

// NormalizarURLIA: sin espacios ni barra final; compatible con OpenAI sin /v1 (se agrega al
// llamar); Anthropic vacía = la oficial. Es la forma con que se compara si la URL cambió.
func NormalizarURLIA(proveedor, crudo string) string {
	u := strings.TrimRight(strings.TrimSpace(crudo), "/")
	switch proveedor {
	case ProveedorAnthropic:
		if u == "" {
			return AnthropicURLPorDefecto
		}
	case ProveedorOllama, ProveedorOpenAICompatible:
		u = strings.TrimSuffix(u, "/v1")
	}
	return u
}

// baseOpenAI normaliza la URL base: sin barra final y sin /v1 (se agrega al llamar).
func baseOpenAI(crudo string) (string, error) {
	u := NormalizarURLIA(ProveedorOpenAICompatible, crudo)
	if u == "" {
		return "", &ErrorIA{Codigo: ErrIASinConfig, Detalle: "Falta la URL del servidor de IA"}
	}
	return u, nil
}

// URL y red ------------------------------------------------------------------------------------

// ErrIAURL es el código de una URL rechazada.
const ErrIAURL = "url_no_permitida"

// ErrIARedirige: el proveedor respondió con una redirección (no se siguen).
const ErrIARedirige = "proveedor_redirige"

func esLocalhost(host string) bool {
	return host == "localhost" || strings.HasSuffix(host, ".localhost")
}

// validarURLIA: http/https, con host, sin usuario ni contraseña ni parámetros; una IP literal debe
// estar permitida y, con clave, https (salvo IP privada o loopback permitido). Las IP de un nombre
// se revisan al conectar (controlIA).
func validarURLIA(crudo string, loopback, conClave bool) error {
	u, err := url.Parse(crudo)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.Hostname() == "" {
		return &ErrorIA{Codigo: ErrIAURL, Detalle: "La URL debe ser http:// o https:// con un servidor"}
	}
	if u.User != nil {
		return &ErrorIA{Codigo: ErrIAURL, Detalle: "La URL no puede llevar usuario ni contraseña (la clave va aparte)"}
	}
	if u.RawQuery != "" || u.Fragment != "" || u.ForceQuery {
		return &ErrorIA{Codigo: ErrIAURL, Detalle: "La URL base no lleva parámetros"}
	}
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	ip, errIP := netip.ParseAddr(host)
	if errIP == nil && !ipPermitidaIA(ip, loopback) {
		return &ErrorIA{Codigo: ErrIAURL, Detalle: "Esa dirección no se permite (loopback, link-local o metadata de la nube)"}
	}
	if esLocalhost(host) && !loopback {
		return &ErrorIA{Codigo: ErrIAURL, Detalle: "Esa dirección no se permite (loopback); se activa con IRIS_ASISTENTE_PERMITIR_LOOPBACK"}
	}
	if host == "metadata" || host == "metadata.google.internal" || strings.HasSuffix(host, ".metadata.google.internal") {
		return &ErrorIA{Codigo: ErrIAURL, Detalle: "Esa dirección no se permite (metadata de la nube)"}
	}
	if conClave && u.Scheme == "http" {
		local := errIP == nil && (ip.Unmap().IsPrivate() || (loopback && ip.Unmap().IsLoopback()))
		if !local && (!loopback || !esLocalhost(host)) {
			return &ErrorIA{Codigo: ErrIAURL, Detalle: "Con clave, la URL debe ser https:// (http solo con una IP privada)"}
		}
	}
	return nil
}

var bloqueadasIA = []netip.Prefix{
	netip.MustParsePrefix("169.254.0.0/16"),     // link-local (metadata de AWS, GCP, Azure)
	netip.MustParsePrefix("fe80::/10"),          // link-local IPv6
	netip.MustParsePrefix("fd00:ec2::254/128"),  // metadata de AWS por IPv6
	netip.MustParsePrefix("100.100.100.200/32"), // metadata de Alibaba
	netip.MustParsePrefix("168.63.129.16/32"),   // Azure (WireServer)
	netip.MustParsePrefix("64:ff9b::/96"),       // NAT64
	netip.MustParsePrefix("64:ff9b:1::/48"),     // NAT64 local
	netip.MustParsePrefix("2001::/32"),          // Teredo (IPv4 embebida)
	netip.MustParsePrefix("::/96"),              // IPv4-compatible (obsoleta)
}

var seisAcuatro = netip.MustParsePrefix("2002::/16")

// ipPermitidaIA: las IP privadas y de la LAN sí (Ollama suele estar ahí); loopback solo con
// IRIS_ASISTENTE_PERMITIR_LOOPBACK; link-local, metadata, NAT64, Teredo, IPv4-compatible, 6to4 con
// una IPv4 no permitida, sin especificar y multicast no.
func ipPermitidaIA(ip netip.Addr, loopback bool) bool {
	if !ip.IsValid() {
		return false
	}
	ip = ip.Unmap()
	if ip.IsLoopback() {
		return loopback
	}
	if ip.IsUnspecified() || ip.IsMulticast() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsInterfaceLocalMulticast() {
		return false
	}
	for _, p := range bloqueadasIA {
		if p.Contains(ip) {
			return false
		}
	}
	if seisAcuatro.Contains(ip) {
		b := ip.As16()
		return ipPermitidaIA(netip.AddrFrom4([4]byte{b[2], b[3], b[4], b[5]}), false)
	}
	return true
}

// controlIA revisa la IP ya resuelta en cada conexión (también si el DNS cambia): así un nombre
// que apunta a la metadata o a loopback no pasa.
func controlIA(loopback bool) func(_, direccion string, _ syscall.RawConn) error {
	return func(_, direccion string, _ syscall.RawConn) error {
		host, _, err := net.SplitHostPort(direccion)
		if err != nil {
			return err
		}
		ip, err := netip.ParseAddr(host)
		if err != nil || !ipPermitidaIA(ip, loopback) {
			return errDestinoIA
		}
		return nil
	}
}

var (
	errDestinoIA     = errors.New("destino no permitido para el asistente")
	errRedireccionIA = errors.New("el proveedor respondió con una redirección")
)

// sinRedirecciones convierte cualquier 3xx en error antes de que el cliente (o el SDK) lo vea: no
// se sigue ninguna redirección.
type sinRedirecciones struct{ base http.RoundTripper }

func (s sinRedirecciones) RoundTrip(r *http.Request) (*http.Response, error) {
	resp, err := s.base.RoundTrip(r)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 && resp.StatusCode <= 399 {
		_ = resp.Body.Close()
		return nil, errRedireccionIA
	}
	return resp, nil
}

// NuevoHTTPIA es el cliente hacia el proveedor: sin proxy (para que la revisión de la IP valga),
// conexión en 10 s, sin keep-alive y sin seguir redirecciones (un 3xx es proveedor_redirige).
func NuevoHTTPIA(timeout time.Duration, loopback bool) *http.Client {
	d := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second, Control: controlIA(loopback)}
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.Proxy = nil
	t.DialContext = d.DialContext
	t.ResponseHeaderTimeout = timeout
	// Un cliente por llamada: sin conexiones ociosas que queden abiertas.
	t.DisableKeepAlives = true
	return &http.Client{Transport: sinRedirecciones{base: t}, Timeout: timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

// clasificarErrorRed traduce una falla de red a un ErrorIA (sin la URL ni secretos).
func clasificarErrorRed(err error) error {
	var e *ErrorIA
	if errors.As(err, &e) {
		return e
	}
	if errors.Is(err, errDestinoIA) {
		return &ErrorIA{Codigo: ErrIAURL, Detalle: "El servidor resolvió a una dirección no permitida (loopback, link-local o metadata)"}
	}
	if errors.Is(err, errRedireccionIA) {
		return &ErrorIA{Codigo: ErrIARedirige, Detalle: "El servidor respondió con una redirección; usa la URL final"}
	}
	var ne net.Error
	if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &ne) && ne.Timeout()) {
		return &ErrorIA{Codigo: ErrIANoResponde, Detalle: "El servidor de IA no respondió a tiempo"}
	}
	if errors.Is(err, context.Canceled) {
		return &ErrorIA{Codigo: ErrIANoResponde, Detalle: "Se canceló el pedido al servidor de IA"}
	}
	return &ErrorIA{Codigo: ErrIANoResponde, Detalle: "No se pudo conectar con el servidor de IA"}
}

func errorPorStatus(status int) *ErrorIA {
	switch {
	case status >= 300 && status <= 399:
		return &ErrorIA{Codigo: ErrIARedirige, Detalle: "El servidor respondió con una redirección; usa la URL final"}
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return &ErrorIA{Codigo: ErrIAClave, Detalle: fmt.Sprintf("La clave no es válida o no tiene permiso (%d)", status)}
	case status == http.StatusNotFound:
		return &ErrorIA{Codigo: ErrIAProveedor, Detalle: "El modelo o la ruta no existen en ese servidor (404)"}
	case status == http.StatusTooManyRequests:
		return &ErrorIA{Codigo: ErrIAProveedor, Detalle: "El proveedor limitó los pedidos (429); intenta más tarde"}
	case status >= 500:
		return &ErrorIA{Codigo: ErrIANoResponde, Detalle: fmt.Sprintf("El servidor de IA falló (%d)", status)}
	}
	return &ErrorIA{Codigo: ErrIAProveedor, Detalle: fmt.Sprintf("El servidor de IA rechazó el pedido (%d)", status)}
}

// Compatible con OpenAI -----------------------------------------------------------------------------

type proveedorOpenAI struct {
	base  string
	clave string
	http  *http.Client
}

type mensajeOpenAI struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

func (p *proveedorOpenAI) pedir(ctx context.Context, metodo, ruta string, cuerpo any, destino any) error {
	var rd io.Reader
	if cuerpo != nil {
		b, err := json.Marshal(cuerpo)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, metodo, p.base+ruta, rd)
	if err != nil {
		return &ErrorIA{Codigo: ErrIAProveedor, Detalle: "URL inválida"}
	}
	if cuerpo != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	if p.clave != "" {
		req.Header.Set("Authorization", "Bearer "+p.clave)
	}
	resp, err := p.http.Do(req)
	if err != nil {
		return clasificarErrorRed(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
		return errorPorStatus(resp.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(destino); err != nil {
		return &ErrorIA{Codigo: ErrIAProveedor, Detalle: "El servidor de IA respondió algo que no es JSON compatible con OpenAI"}
	}
	return nil
}

func (p *proveedorOpenAI) Generar(ctx context.Context, in PedidoIA) (RespuestaIA, UsoIA, error) {
	cuerpo := map[string]any{
		"model": in.Modelo,
		"messages": []mensajeOpenAI{
			{Role: "system", Content: in.Instrucciones},
			{Role: "user", Content: in.Contenido},
		},
		"max_tokens": in.MaxTokens,
		"stream":     false,
	}
	var r struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
				Refusal string `json:"refusal"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int64 `json:"prompt_tokens"`
			CompletionTokens int64 `json:"completion_tokens"`
		} `json:"usage"`
	}
	if err := p.pedir(ctx, http.MethodPost, "/v1/chat/completions", cuerpo, &r); err != nil {
		return RespuestaIA{}, UsoIA{}, err
	}
	uso := UsoIA{Entrada: r.Usage.PromptTokens, Salida: r.Usage.CompletionTokens}
	if len(r.Choices) == 0 {
		return RespuestaIA{}, uso, &ErrorIA{Codigo: ErrIAProveedor, Detalle: "El servidor de IA no devolvió ninguna respuesta"}
	}
	c := r.Choices[0]
	return RespuestaIA{Texto: c.Message.Content, Rechazo: c.FinishReason == "content_filter" || c.Message.Refusal != "",
		Truncada: c.FinishReason == "length"}, uso, nil
}

func (p *proveedorOpenAI) Modelos(ctx context.Context) ([]ModeloIA, error) {
	var r struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := p.pedir(ctx, http.MethodGet, "/v1/models", nil, &r); err != nil {
		return nil, err
	}
	out := make([]ModeloIA, 0, len(r.Data))
	for _, m := range r.Data {
		if m.ID != "" {
			out = append(out, ModeloIA{ID: m.ID, Nombre: m.ID})
		}
	}
	return out, nil
}

// Anthropic (SDK oficial) ---------------------------------------------------------------------------

type proveedorAnthropic struct {
	cli anthropic.Client
}

func (p *proveedorAnthropic) Generar(ctx context.Context, in PedidoIA) (RespuestaIA, UsoIA, error) {
	modelo := in.Modelo
	if modelo == "" {
		modelo = AnthropicModeloPorDefecto
	}
	// Sin temperature ni top_p (los modelos actuales los rechazan). Las instrucciones fijas van en
	// system con un punto de caché: se repiten igual en cada pedido. Con el respaldo del lado del
	// servidor (fallbacks: "default"): si el modelo declina, la API reintenta en el modelo de
	// respaldo dentro de la misma llamada; si ese también declina, es rechazo_modelo.
	r, err := p.cli.Beta.Messages.New(ctx, anthropic.BetaMessageNewParams{
		Model:     modelo,
		MaxTokens: in.MaxTokens,
		System:    []anthropic.BetaTextBlockParam{{Text: in.Instrucciones, CacheControl: anthropic.NewBetaCacheControlEphemeralParam()}},
		Messages:  []anthropic.BetaMessageParam{anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock(in.Contenido))},
		Betas:     []anthropic.AnthropicBeta{anthropic.AnthropicBetaServerSideFallback2026_07_01},
		Fallbacks: anthropic.BetaFallbacksParamOfDefault(),
	})
	if err != nil {
		return RespuestaIA{}, UsoIA{}, errorAnthropic(err)
	}
	uso := UsoIA{Entrada: r.Usage.InputTokens + r.Usage.CacheCreationInputTokens + r.Usage.CacheReadInputTokens, Salida: r.Usage.OutputTokens}
	if r.StopReason == anthropic.BetaStopReasonRefusal {
		return RespuestaIA{Rechazo: true}, uso, nil
	}
	var b strings.Builder
	for _, bloque := range r.Content {
		if t, ok := bloque.AsAny().(anthropic.BetaTextBlock); ok {
			b.WriteString(t.Text)
		}
	}
	return RespuestaIA{Texto: b.String(), Truncada: r.StopReason == anthropic.BetaStopReasonMaxTokens}, uso, nil
}

func (p *proveedorAnthropic) Modelos(ctx context.Context) ([]ModeloIA, error) {
	pag, err := p.cli.Models.List(ctx, anthropic.ModelListParams{Limit: anthropic.Int(100)})
	if err != nil {
		return nil, errorAnthropic(err)
	}
	out := make([]ModeloIA, 0, len(pag.Data))
	for _, m := range pag.Data {
		nombre := m.DisplayName
		if nombre == "" {
			nombre = m.ID
		}
		out = append(out, ModeloIA{ID: m.ID, Nombre: nombre})
	}
	return out, nil
}

func errorAnthropic(err error) error {
	var apierr *anthropic.Error
	if errors.As(err, &apierr) {
		return errorPorStatus(apierr.StatusCode)
	}
	return clasificarErrorRed(err)
}

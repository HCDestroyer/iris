package conector

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
)

// Cliente HTTP genérico hacia la API REST de un conector (Argos, Horus): base + /api/v1,
// timeout por llamada, reintentos con espera exponencial en las lecturas idempotentes ante
// fallas de red, 429 y 5xx, sin seguir redirecciones, y los errores problem+json del otro lado
// como *ErrorRemoto. El token nunca aparece en errores ni logs. Cada conector arma encima sus
// llamadas (argos.Cliente, horus.Cliente).

// Remoto identifica al otro sistema en los códigos y mensajes de error.
type Remoto struct {
	// Codigo es el prefijo de los códigos de problema: argos → argos_no_disponible.
	Codigo string
	// Nombre para los mensajes: «Argos», «Horus».
	Nombre string
	// VariableRedes es la variable de entorno con sus redes internas permitidas.
	VariableRedes string
}

// Cliente llama a la API de un conector.
type Cliente struct {
	remoto    Remoto
	base      string
	token     string
	http      *http.Client
	timeout   time.Duration
	intentos  int
	espera    time.Duration
	userAgent string
}

// OpcionesCliente configuran el cliente.
type OpcionesCliente struct {
	HTTP      *http.Client
	Timeout   time.Duration
	Intentos  int
	Espera    time.Duration
	UserAgent string
}

// NuevoCliente: base es el origen del otro sistema (https://argos.isp.gt, sin /api/v1); token,
// su credencial (atk_…, hf_pat_…; vacío en el canje).
func NuevoCliente(r Remoto, base, token string, op OpcionesCliente) *Cliente {
	if op.HTTP == nil {
		op.HTTP = http.DefaultClient
	}
	if op.Timeout <= 0 {
		op.Timeout = 5 * time.Second
	}
	if op.Intentos <= 0 {
		op.Intentos = 3
	}
	if op.Espera <= 0 {
		op.Espera = 200 * time.Millisecond
	}
	if op.UserAgent == "" {
		op.UserAgent = "Iris"
	}
	return &Cliente{
		remoto: r, base: strings.TrimRight(base, "/"), token: token, http: op.HTTP, timeout: op.Timeout,
		intentos: op.Intentos, espera: op.Espera, userAgent: op.UserAgent,
	}
}

// ErrorRemoto es una respuesta de error del otro sistema (problem+json) o una falla de red
// (Status 0).
type ErrorRemoto struct {
	Remoto  Remoto
	Status  int
	Tipo    string
	Titulo  string
	Detalle string

	reintentarEn time.Duration
	causa        error
}

// NuevoErrorRemoto arma un error del otro sistema (respuestas ilegibles, listas demasiado largas).
func NuevoErrorRemoto(r Remoto, status int, titulo string, causa error) *ErrorRemoto {
	return &ErrorRemoto{Remoto: r, Status: status, Titulo: titulo, causa: causa}
}

func (e *ErrorRemoto) Error() string {
	prefijo := e.Remoto.Codigo
	if prefijo == "" {
		prefijo = "conector"
	}
	if e.Status == 0 {
		return prefijo + ": sin respuesta: " + e.causa.Error()
	}
	msg := fmt.Sprintf("%s: %d", prefijo, e.Status)
	if e.Tipo != "" {
		msg += " " + e.Tipo
	}
	if e.Detalle != "" {
		msg += ": " + e.Detalle
	}
	return msg
}

func (e *ErrorRemoto) Unwrap() error { return e.causa }

// Caido indica que el otro sistema no respondió o respondió con una falla temporal.
func (e *ErrorRemoto) Caido() bool {
	if e.Bloqueado() {
		return false
	}
	return e.Status == 0 || e.Status == http.StatusTooManyRequests || e.Status >= 500
}

// Bloqueado indica que no se llegó a conectar porque el destino es una red interna (red.go).
func (e *ErrorRemoto) Bloqueado() bool {
	return e.Status == 0 && errors.Is(e.causa, ErrDestinoBloqueado)
}

// EsCaido indica si err es una falla temporal del otro sistema.
func EsCaido(err error) bool {
	var er *ErrorRemoto
	return errors.As(err, &er) && er.Caido()
}

// EsNoEncontrado indica si el otro sistema respondió 404.
func EsNoEncontrado(err error) bool {
	var er *ErrorRemoto
	return errors.As(err, &er) && er.Status == http.StatusNotFound
}

// EsBloqueado indica si la llamada se cortó por la protección contra SSRF.
func EsBloqueado(err error) bool {
	var er *ErrorRemoto
	return errors.As(err, &er) && er.Bloqueado()
}

// Problema traduce un error del otro sistema al problema que ve el cliente de Iris (con
// <codigo> = argos, horus):
//
//   - destino en una red interna → 422 destino_no_permitido;
//   - 404 → 404 no_encontrado;
//   - 401/403 → 502 <codigo>_sin_permiso (el token del conector es inválido o le falta un permiso);
//   - red, timeout, 429 o 5xx → 503 <codigo>_no_disponible;
//   - otro 4xx → 502 <codigo>_rechazo con un resumen (status y tipo), nunca el detail crudo: lo
//     que responda el destino no llega tal cual a quien escribió la URL (SSRF). El detail queda
//     en el log.
//
// Cualquier otro error pasa tal cual.
func Problema(err error) error {
	var er *ErrorRemoto
	if !errors.As(err, &er) {
		return err
	}
	r := er.Remoto
	switch {
	case er.Bloqueado():
		return nucleo.ReglaNegocio("destino_no_permitido",
			"La URL de "+r.Nombre+" resuelve a una red interna; si es intencional, agrégala a "+r.VariableRedes)
	case er.Status == http.StatusNotFound:
		return nucleo.NoEncontrado("No existe en " + r.Nombre)
	case er.Status == http.StatusUnauthorized || er.Status == http.StatusForbidden:
		return nucleo.NuevoProblema(http.StatusBadGateway, r.Codigo+"_sin_permiso", r.Nombre+" rechazó la credencial",
			"El token del conector no es válido o le falta un permiso en "+r.Nombre+" ("+strconv.Itoa(er.Status)+")")
	case er.Caido():
		return nucleo.NuevoProblema(http.StatusServiceUnavailable, r.Codigo+"_no_disponible", r.Nombre+" no disponible",
			r.Nombre+" no respondió; vuelve a intentar en unos minutos")
	}
	slog.Warn("conector: el otro sistema rechazó la petición", "sistema", r.Codigo, "status", er.Status, "tipo", er.Tipo, "detalle", er.Detalle)
	return Rechazo(r, r.Nombre+" respondió HTTP "+strconv.Itoa(er.Status)+tipoVisible(er.Tipo))
}

// Rechazo es el 502 <codigo>_rechazo con un detalle propio (sin texto del otro sistema).
func Rechazo(r Remoto, detalle string) *nucleo.Problema {
	return nucleo.NuevoProblema(http.StatusBadGateway, r.Codigo+"_rechazo", r.Nombre+" rechazó la petición", detalle)
}

// tipoSeguro: un type de problem+json corto y sin texto libre (urn:argos:codigo_invalido).
var tipoSeguro = regexp.MustCompile(`^[A-Za-z0-9:._/-]{1,80}$`)

func tipoVisible(tipo string) string {
	if tipoSeguro.MatchString(tipo) {
		return " (" + tipo + ")"
	}
	return ""
}

func reintentable(err error) (bool, time.Duration) {
	var er *ErrorRemoto
	if !errors.As(err, &er) {
		return false, 0
	}
	if er.Caido() {
		return true, er.reintentarEn
	}
	return false, 0
}

const maxRespuesta = 4 << 20

// Pedir hace la llamada a base + /api/v1 + ruta; con idempotente=true reintenta según las
// opciones. destino recibe el JSON normalizado (Decodificar); nil = se descarta el cuerpo.
func (c *Cliente) Pedir(ctx context.Context, metodo, ruta string, consulta url.Values, cuerpo any, idempotente bool, destino any) error {
	intentos := 1
	if idempotente {
		intentos = c.intentos
	}
	espera := c.espera
	for i := 1; ; i++ {
		err := c.una(ctx, metodo, ruta, consulta, cuerpo, destino)
		if err == nil {
			return nil
		}
		otra, sugerida := reintentable(err)
		if !otra || i >= intentos || ctx.Err() != nil {
			return err
		}
		pausa := espera
		if sugerida > 0 {
			pausa = min(sugerida, 5*time.Second)
		}
		select {
		case <-ctx.Done():
			return err
		case <-time.After(pausa):
		}
		espera *= 2
	}
}

func (c *Cliente) una(ctx context.Context, metodo, ruta string, consulta url.Values, cuerpo any, destino any) error {
	ctx, cancelar := context.WithTimeout(ctx, c.timeout)
	defer cancelar()
	u := c.base + "/api/v1" + ruta
	if len(consulta) > 0 {
		u += "?" + consulta.Encode()
	}
	var lector io.Reader
	if cuerpo != nil {
		b, err := json.Marshal(cuerpo)
		if err != nil {
			return err
		}
		lector = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, metodo, u, lector)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json, application/problem+json")
	req.Header.Set("User-Agent", c.userAgent)
	if cuerpo != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	if id := contexto.RequestIDDe(ctx); id != "" {
		req.Header.Set("X-Request-Id", id)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return &ErrorRemoto{Remoto: c.remoto, causa: limpiarErrorURL(err)}
	}
	defer func() { _ = resp.Body.Close() }()
	crudo, err := io.ReadAll(io.LimitReader(resp.Body, maxRespuesta))
	if err != nil {
		return &ErrorRemoto{Remoto: c.remoto, causa: err}
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return c.errorDeRespuesta(resp, crudo)
	}
	if destino == nil || len(bytes.TrimSpace(crudo)) == 0 {
		return nil
	}
	if err := Decodificar(crudo, destino); err != nil {
		return &ErrorRemoto{Remoto: c.remoto, Status: http.StatusBadGateway, Titulo: "Respuesta ilegible", causa: err}
	}
	return nil
}

// limpiarErrorURL quita la URL completa del error (puede llevar la consulta del usuario).
func limpiarErrorURL(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return fmt.Errorf("%s: %w", ue.Op, ue.Err)
	}
	return err
}

func (c *Cliente) errorDeRespuesta(resp *http.Response, crudo []byte) *ErrorRemoto {
	e := &ErrorRemoto{Remoto: c.remoto, Status: resp.StatusCode, causa: errors.New(resp.Status)}
	var p struct {
		Type   string `json:"type"`
		Title  string `json:"title"`
		Detail string `json:"detail"`
	}
	if json.Unmarshal(crudo, &p) == nil {
		e.Tipo, e.Titulo, e.Detalle = p.Type, p.Title, p.Detail
	}
	if len(e.Detalle) > 500 {
		e.Detalle = e.Detalle[:500]
	}
	if v := resp.Header.Get("Retry-After"); v != "" {
		if s, err := strconv.Atoi(v); err == nil && s >= 0 {
			e.reintentarEn = time.Duration(s) * time.Second
		}
	}
	return e
}

// Listas ----------------------------------------------------------------------------------

// Pagina es una página de una lista del otro sistema.
type Pagina[T any] struct {
	Data       []T
	NextCursor string
}

// ListaDe pide una página: el otro sistema puede responder un arreglo o { data, nextCursor }.
func ListaDe[T any](ctx context.Context, c *Cliente, ruta string, v url.Values) (Pagina[T], error) {
	var l lista[T]
	if err := c.Pedir(ctx, http.MethodGet, ruta, v, nil, true, &l); err != nil {
		return Pagina[T]{}, err
	}
	return Pagina[T]{Data: l.data, NextCursor: l.next}, nil
}

// maxPaginas acota las listas completas (catálogos, servicios de un cliente).
const maxPaginas = 50

// Todas pide todas las páginas (de 200) hasta 50.
func Todas[T any](ctx context.Context, c *Cliente, ruta string, v url.Values) ([]T, error) {
	if v == nil {
		v = url.Values{}
	}
	v.Set("limit", "200")
	var salida []T
	for range maxPaginas {
		p, err := ListaDe[T](ctx, c, ruta, v)
		if err != nil {
			return nil, err
		}
		salida = append(salida, p.Data...)
		if p.NextCursor == "" {
			return salida, nil
		}
		v.Set("cursor", p.NextCursor)
	}
	return nil, NuevoErrorRemoto(c.remoto, http.StatusBadGateway, "Lista demasiado larga", errors.New("más de 50 páginas en "+ruta))
}

// Decodificación tolerante ---------------------------------------------------------------

// Decodificar normaliza el JSON (claves a snake_case) y lo lee en destino: el otro sistema
// puede responder en snake_case o camelCase.
func Decodificar(crudo []byte, destino any) error {
	dec := json.NewDecoder(bytes.NewReader(crudo))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return err
	}
	b, err := json.Marshal(normalizar(v))
	if err != nil {
		return err
	}
	return json.Unmarshal(b, destino)
}

func normalizar(v any) any {
	switch x := v.(type) {
	case map[string]any:
		salida := make(map[string]any, len(x))
		for k, interno := range x {
			salida[auditoria.SnakeCase(k)] = normalizar(interno)
		}
		return salida
	case []any:
		for i := range x {
			x[i] = normalizar(x[i])
		}
		return x
	}
	return v
}

// Recurso lee un recurso que puede venir solo o como { data: {...} }:
// c.Pedir(…, &conector.Recurso{Destino: &v}).
type Recurso struct {
	Destino any
}

// UnmarshalJSON implementa json.Unmarshaler.
func (e *Recurso) UnmarshalJSON(b []byte) error {
	var obj map[string]json.RawMessage
	if json.Unmarshal(b, &obj) == nil {
		if data, ok := obj["data"]; ok && len(obj) <= 3 && bytes.HasPrefix(bytes.TrimSpace(data), []byte("{")) {
			return json.Unmarshal(data, e.Destino)
		}
	}
	return json.Unmarshal(b, e.Destino)
}

// lista lee una lista: un arreglo o { data: [...], next_cursor }.
type lista[T any] struct {
	data []T
	next string
}

// UnmarshalJSON implementa json.Unmarshaler.
func (l *lista[T]) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if bytes.HasPrefix(b, []byte("[")) {
		return json.Unmarshal(b, &l.data)
	}
	var obj struct {
		Data       []T     `json:"data"`
		NextCursor *string `json:"next_cursor"`
	}
	if err := json.Unmarshal(b, &obj); err != nil {
		return err
	}
	l.data = obj.Data
	if obj.NextCursor != nil {
		l.next = *obj.NextCursor
	}
	return nil
}

// Numero acepta un número JSON o un texto numérico.
type Numero struct {
	valor *float64
}

// UnmarshalJSON implementa json.Unmarshaler.
func (n *Numero) UnmarshalJSON(b []byte) error {
	s := strings.Trim(strings.TrimSpace(string(b)), `"`)
	if s == "" || s == "null" {
		n.valor = nil
		return nil
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return err
	}
	n.valor = &f
	return nil
}

// Float devuelve el valor (nil si no vino).
func (n *Numero) Float() *float64 {
	if n == nil {
		return nil
	}
	return n.valor
}

// Entero devuelve el valor como entero (nil si no vino o no es entero).
func (n *Numero) Entero() *int64 {
	f := n.Float()
	if f == nil || *f != float64(int64(*f)) {
		return nil
	}
	v := int64(*f)
	return &v
}

// Texto acepta un texto o un número JSON y lo guarda como texto (ids que pueden venir de las
// dos formas).
type Texto string

// UnmarshalJSON implementa json.Unmarshaler.
func (t *Texto) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if string(b) == "null" {
		*t = ""
		return nil
	}
	var s string
	if json.Unmarshal(b, &s) == nil {
		*t = Texto(strings.TrimSpace(s))
		return nil
	}
	var n json.Number
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	if err := d.Decode(&n); err != nil {
		return err
	}
	*t = Texto(n.String())
	return nil
}

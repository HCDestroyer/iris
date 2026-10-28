package correo

import (
	"bytes"
	"context"
	"errors"
	"io"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/emersion/go-message"
	_ "github.com/emersion/go-message/charset" // decodifica charsets distintos de UTF-8 (latin1, windows-1252…)
	"github.com/emersion/go-message/mail"
	"golang.org/x/net/html"
)

// MaxTextoParte es lo máximo que se lee de una parte de texto (el resto se descarta).
const MaxTextoParte = 1 << 20

// Límites del análisis MIME. Un correo que los supera se guarda igual, truncado (Limite dice
// por qué), para que el buzón no se trabe: el cursor avanza y el ticket lleva una nota.
const (
	// MaxDelimitadores: con más «boundary=» o «multipart/» en el crudo no se analiza el cuerpo
	// (solo cabeceras). Un anidamiento profundo cuesta O(n²) al recorrerlo.
	MaxDelimitadores = 50
	// MaxPartes: partes MIME recorridas como mucho.
	MaxPartes = 100
	// MaxProfundidad: niveles de multipart anidados como mucho.
	MaxProfundidad = 10
	// MaxCuerpoAnalizado: bytes del cuerpo (después de las cabeceras) que lee el analizador.
	MaxCuerpoAnalizado = 2 << 20
)

// Motivos de un correo truncado (Correo.Limite).
const (
	LimiteTamano      = "tamano"       // superó el tamaño máximo del buzón: solo cabeceras
	LimiteMIMEAnidado = "mime_anidado" // demasiados delimitadores multipart: solo cabeceras
	LimitePartes      = "partes"
	LimiteProfundidad = "profundidad"
	LimiteBytes       = "bytes"
	LimiteTiempo      = "tiempo" // se canceló el contexto (plazo del worker)
)

// Correo es un mensaje leído, ya reducido a texto seguro.
type Correo struct {
	MessageID       string // sin <>; vacío si no traía
	Remitente       string // dirección en minúsculas
	RemitenteNombre string
	Asunto          string
	InReplyTo       string
	Referencias     []string
	Fecha           *time.Time
	Texto           string // texto plano (de text/plain o del HTML sin etiquetas)
	Adjuntos        []Adjunto
	// Automatico explica por qué es una autorespuesta o un rebote (vacío si es de una persona).
	Automatico string
	// Limite: si no vacío, el correo no se leyó completo (ver Limite*). Texto y Adjuntos traen
	// lo que se alcanzó a leer.
	Limite string
	// Autenticacion: valores de las cabeceras Authentication-Results, de arriba abajo (ver
	// Verificar).
	Autenticacion []string
}

// Adjunto: solo metadatos (la subida es de la tarea de adjuntos).
type Adjunto struct {
	Nombre string `json:"nombre"`
	Tipo   string `json:"tipo"`
	Tamano int64  `json:"tamano"`
}

// Leer interpreta un mensaje RFC 5322. Con soloCabeceras (el correo superó el tamaño máximo) no
// lee el cuerpo. Nunca ejecuta nada: el HTML se reduce a texto (Texto) y los adjuntos a
// metadatos. El análisis del cuerpo está acotado (Max*) y respeta ctx: si se pasa de un límite
// devuelve lo leído con Limite.
func Leer(ctx context.Context, crudo []byte, soloCabeceras bool) (Correo, error) {
	ent, err := message.Read(bytes.NewReader(crudo))
	if err != nil && !message.IsUnknownCharset(err) && !message.IsUnknownEncoding(err) {
		return Correo{}, err
	}
	h := mail.Header{Header: ent.Header}
	var c Correo
	c.MessageID, _ = h.MessageID()
	if c.Asunto, err = h.Subject(); err != nil {
		c.Asunto = h.Get("Subject")
	}
	c.Asunto = limpiarLinea(c.Asunto)
	if de, err := h.AddressList("From"); err == nil && len(de) > 0 {
		c.Remitente = strings.ToLower(strings.TrimSpace(de[0].Address))
		c.RemitenteNombre = limpiarLinea(de[0].Name)
	}
	if ids, err := h.MsgIDList("In-Reply-To"); err == nil && len(ids) > 0 {
		c.InReplyTo = ids[0]
	}
	c.Referencias, _ = h.MsgIDList("References")
	if f, err := h.Date(); err == nil && !f.IsZero() {
		f = f.UTC()
		c.Fecha = &f
	}
	c.Automatico = automatico(h, c.Remitente)
	c.Autenticacion = h.Values("Authentication-Results") // de arriba abajo
	switch {
	case soloCabeceras:
		c.Limite = LimiteTamano
		return c, nil
	case delimitadores(crudo) > MaxDelimitadores:
		c.Limite = LimiteMIMEAnidado
		return c, nil
	}
	c.Texto, c.Adjuntos, c.Limite = cuerpo(ctx, crudo)
	return c, nil
}

// automatico detecta autorespuestas (vacaciones, «fuera de oficina») y rebotes, para no abrir
// tickets ni entrar en bucles de respuestas automáticas (RFC 3834).
func automatico(h mail.Header, remitente string) string {
	if v := strings.ToLower(strings.TrimSpace(h.Get("Auto-Submitted"))); v != "" && v != "no" {
		return "auto_submitted:" + v
	}
	switch v := strings.ToLower(strings.TrimSpace(h.Get("Precedence"))); v {
	case "bulk", "junk", "list", "auto_reply":
		return "precedence:" + v
	}
	for _, k := range []string{"X-Autoreply", "X-Autorespond", "X-Autorespond-Suppress"} {
		if h.Get(k) != "" {
			return "autorespuesta"
		}
	}
	local, _, _ := strings.Cut(remitente, "@")
	if local == "mailer-daemon" || local == "postmaster" || remitente == "" {
		return "rebote"
	}
	if strings.TrimSpace(h.Get("Return-Path")) == "<>" {
		return "rebote"
	}
	if t, _, _ := h.ContentType(); t == "multipart/report" {
		return "rebote"
	}
	return ""
}

var (
	marcaBoundary  = []byte("boundary=")
	marcaMultipart = []byte("multipart/")
)

// delimitadores cuenta, sin analizar nada, las marcas de multipart del crudo.
func delimitadores(crudo []byte) int {
	minus := bytes.ToLower(crudo)
	return bytes.Count(minus, marcaBoundary) + bytes.Count(minus, marcaMultipart)
}

// errLimite corta el análisis cuando se leyó MaxCuerpoAnalizado.
var errLimite = errors.New("correo: límite de bytes analizados")

// lectorAcotado entrega como mucho n bytes y se corta si ctx se cancela.
type lectorAcotado struct {
	ctx context.Context
	r   io.Reader
	n   int64
}

func (l *lectorAcotado) Read(p []byte) (int, error) {
	if err := l.ctx.Err(); err != nil {
		return 0, err
	}
	if l.n <= 0 {
		return 0, errLimite
	}
	if int64(len(p)) > l.n {
		p = p[:l.n]
	}
	n, err := l.r.Read(p)
	l.n -= int64(n)
	return n, err
}

// finCabeceras devuelve dónde empieza el cuerpo (tras la primera línea vacía).
func finCabeceras(crudo []byte) int {
	i, j := bytes.Index(crudo, []byte("\r\n\r\n")), bytes.Index(crudo, []byte("\n\n"))
	switch {
	case i >= 0 && (j < 0 || i < j):
		return i + 4
	case j >= 0:
		return j + 2
	}
	return len(crudo)
}

// recorrido junta lo leído del cuerpo y cuenta partes.
type recorrido struct {
	ctx           context.Context
	plano, enHTML string
	adjuntos      []Adjunto
	partes        int
	limite        string
}

// cuerpo recorre las partes (acotado: ver Max*): el primer text/plain gana; si no hay, el
// primer text/html pasado a texto. Los adjuntos (y las partes que no son texto) quedan como
// metadatos. limite dice si el recorrido se cortó.
func cuerpo(ctx context.Context, crudo []byte) (string, []Adjunto, string) {
	l := &lectorAcotado{ctx: ctx, r: bytes.NewReader(crudo), n: int64(finCabeceras(crudo)) + MaxCuerpoAnalizado}
	ent, err := message.Read(l)
	r := &recorrido{ctx: ctx}
	if err != nil && !message.IsUnknownCharset(err) && !message.IsUnknownEncoding(err) {
		r.cortar(err)
	} else {
		r.entidad(ent, 0)
	}
	texto := r.plano
	if strings.TrimSpace(texto) == "" {
		texto = r.enHTML
	}
	return LimpiarTexto(texto), r.adjuntos, r.limite
}

// cortar anota por qué se detuvo el recorrido (el primer motivo gana). Un error de formato
// que no es un límite solo detiene la lectura (como antes).
func (r *recorrido) cortar(err error) {
	if r.limite != "" {
		return
	}
	switch {
	case r.ctx.Err() != nil:
		r.limite = LimiteTiempo
	case errors.Is(err, errLimite):
		r.limite = LimiteBytes
	}
}

func (r *recorrido) detenido() bool {
	if r.limite == "" && r.ctx.Err() != nil {
		r.limite = LimiteTiempo
	}
	return r.limite != ""
}

// entidad visita una parte: multipart (hasta MaxProfundidad) o una hoja.
func (r *recorrido) entidad(e *message.Entity, profundidad int) bool {
	if r.detenido() {
		return false
	}
	r.partes++
	if r.partes > MaxPartes {
		r.limite = LimitePartes
		return false
	}
	mr := e.MultipartReader()
	if mr == nil {
		r.hoja(e)
		return !r.detenido()
	}
	if profundidad >= MaxProfundidad {
		r.limite = LimiteProfundidad
		return false
	}
	for {
		p, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			return true
		}
		if err != nil && !message.IsUnknownCharset(err) && !message.IsUnknownEncoding(err) {
			r.cortar(err)
			return false
		}
		if !r.entidad(p, profundidad+1) {
			return false
		}
	}
}

// hoja clasifica una parte sin hijos como lo hace mail.Reader: inline si lo dice o si es
// texto sin «attachment»; si no, adjunto.
func (r *recorrido) hoja(e *message.Entity) {
	t, _, _ := e.Header.ContentType()
	disp, _, _ := e.Header.ContentDisposition()
	if disp == "inline" || (disp != "attachment" && strings.HasPrefix(t, "text/")) {
		switch {
		case t == "text/plain" && r.plano == "":
			r.plano = r.leerTexto(e.Body)
		case t == "text/html" && r.enHTML == "":
			r.enHTML = HTMLATexto(r.leerTexto(e.Body))
		case t != "text/plain" && t != "text/html":
			r.adjuntos = append(r.adjuntos, r.adjunto(e.Body, "", t))
		}
		return
	}
	nombre, _ := (&mail.AttachmentHeader{Header: e.Header}).Filename()
	r.adjuntos = append(r.adjuntos, r.adjunto(e.Body, nombre, t))
}

func (r *recorrido) leerTexto(cuerpo io.Reader) string {
	b, err := io.ReadAll(io.LimitReader(cuerpo, MaxTextoParte))
	if err != nil {
		r.cortar(err)
	}
	return strings.ToValidUTF8(string(b), "�")
}

func (r *recorrido) adjunto(cuerpo io.Reader, nombre, tipo string) Adjunto {
	n, err := io.Copy(io.Discard, cuerpo)
	if err != nil {
		r.cortar(err)
	}
	if tipo == "" {
		tipo = "application/octet-stream"
	}
	return Adjunto{Nombre: limpiarLinea(nombre), Tipo: tipo, Tamano: n}
}

// limpiarLinea: una sola línea sin caracteres de control (cabeceras).
func limpiarLinea(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, strings.ToValidUTF8(s, ""))
	return strings.Join(strings.Fields(s), " ")
}

// LimpiarTexto normaliza saltos de línea, quita caracteres de control (salvo \n y \t) y
// colapsa líneas vacías repetidas.
func LimpiarTexto(s string) string {
	s = strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n")
	s = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return r
		}
		if unicode.IsControl(r) || r == '­' || r == '​' {
			return -1
		}
		return r
	}, strings.ToValidUTF8(s, ""))
	lineas := strings.Split(s, "\n")
	var salida []string
	vacias := 0
	for _, l := range lineas {
		l = strings.TrimRight(l, " \t")
		if l == "" {
			vacias++
			if vacias > 1 {
				continue
			}
		} else {
			vacias = 0
		}
		salida = append(salida, l)
	}
	return strings.TrimSpace(strings.Join(salida, "\n"))
}

var bloques = map[string]bool{
	"p": true, "div": true, "br": true, "li": true, "tr": true, "h1": true, "h2": true, "h3": true,
	"h4": true, "h5": true, "h6": true, "blockquote": true, "pre": true, "table": true, "ul": true,
	"ol": true, "hr": true, "section": true, "article": true, "header": true, "footer": true,
}

// ignorados: su contenido no es texto para el lector (ni se interpreta).
var ignorados = map[string]bool{"script": true, "style": true, "head": true, "title": true, "noscript": true, "template": true, "svg": true}

// HTMLATexto reduce HTML a texto plano: quita etiquetas, scripts y estilos, decodifica
// entidades y convierte bloques en saltos de línea. Los enlaces http(s) se conservan entre
// paréntesis. No ejecuta ni resuelve nada.
func HTMLATexto(fuente string) string {
	z := html.NewTokenizer(strings.NewReader(fuente))
	var b strings.Builder
	saltar := 0
	var enlace string
	for {
		switch z.Next() {
		case html.ErrorToken:
			lineas := strings.Split(b.String(), "\n")
			for i, l := range lineas {
				lineas[i] = strings.Join(strings.Fields(l), " ")
			}
			return LimpiarTexto(strings.Join(lineas, "\n"))
		case html.TextToken:
			if saltar == 0 {
				// Los espacios de los bordes separan palabras entre etiquetas (<b>…</b>).
				b.WriteString(" " + string(z.Text()) + " ")
			}
		case html.StartTagToken, html.SelfClosingTagToken:
			nombre, conAttr := z.TagName()
			n := string(nombre)
			if ignorados[n] {
				if tok := z.Token(); tok.Type == html.StartTagToken {
					saltar++
				}
				continue
			}
			if bloques[n] {
				b.WriteByte('\n')
			}
			if n == "a" && conAttr {
				enlace = ""
				for {
					k, v, mas := z.TagAttr()
					if string(k) == "href" {
						if h := strings.TrimSpace(string(v)); strings.HasPrefix(h, "http://") || strings.HasPrefix(h, "https://") {
							enlace = h
						}
					}
					if !mas {
						break
					}
				}
			}
		case html.EndTagToken:
			nombre, _ := z.TagName()
			n := string(nombre)
			if ignorados[n] {
				if saltar > 0 {
					saltar--
				}
				continue
			}
			if n == "a" && enlace != "" {
				if !strings.Contains(b.String()[max(0, b.Len()-len(enlace)-4):], enlace) {
					b.WriteString(" (" + enlace + ")")
				}
				enlace = ""
			}
			if bloques[n] {
				b.WriteByte('\n')
			}
		case html.CommentToken, html.DoctypeToken:
		}
	}
}

// Líneas que abren la cita de una respuesta («El lun, 1 ene … escribió:», «On … wrote:»,
// «-----Mensaje original-----»).
var (
	inicioCita = regexp.MustCompile(`(?i)^\s*(el|on)\s.{0,300}(escribió|wrote)\s*:\s*$`)
	separador  = regexp.MustCompile(`(?i)^\s*-{2,}\s*(mensaje original|original message|forwarded message|mensaje reenviado)\s*-{2,}\s*$`)
)

// QuitarCita deja solo lo nuevo de una respuesta: corta en la línea que abre la cita y quita
// las líneas citadas con «>» del final. Si no queda nada, devuelve el texto completo.
func QuitarCita(texto string) string {
	lineas := strings.Split(texto, "\n")
	fin := len(lineas)
	for i, l := range lineas {
		if inicioCita.MatchString(l) || separador.MatchString(l) {
			fin = i
			break
		}
	}
	for fin > 0 && (strings.HasPrefix(strings.TrimSpace(lineas[fin-1]), ">") || strings.TrimSpace(lineas[fin-1]) == "") {
		fin--
	}
	nuevo := strings.TrimSpace(strings.Join(lineas[:fin], "\n"))
	if nuevo == "" {
		return texto
	}
	return nuevo
}

// Recortar corta s a n caracteres (runas) como mucho.
func Recortar(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n])
}

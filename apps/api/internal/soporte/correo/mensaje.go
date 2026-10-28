package correo

import (
	"bytes"
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
}

// Adjunto: solo metadatos (la subida es de la tarea de adjuntos).
type Adjunto struct {
	Nombre string `json:"nombre"`
	Tipo   string `json:"tipo"`
	Tamano int64  `json:"tamano"`
}

// Leer interpreta un mensaje RFC 5322. Con soloCabeceras (el correo superó el tamaño máximo) no
// lee el cuerpo. Nunca ejecuta nada: el HTML se reduce a texto (Texto) y los adjuntos a
// metadatos.
func Leer(crudo []byte, soloCabeceras bool) (Correo, error) {
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
	if soloCabeceras {
		return c, nil
	}
	c.Texto, c.Adjuntos = cuerpo(crudo)
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

// cuerpo recorre las partes: el primer text/plain gana; si no hay, el primer text/html pasado a
// texto. Los adjuntos (y las partes que no son texto) quedan como metadatos.
func cuerpo(crudo []byte) (string, []Adjunto) {
	r, err := mail.CreateReader(bytes.NewReader(crudo))
	if err != nil && !message.IsUnknownCharset(err) {
		return "", nil
	}
	var plano, enHTML string
	var adjuntos []Adjunto
	for {
		p, err := r.NextPart()
		if errors.Is(err, io.EOF) || (err != nil && !message.IsUnknownCharset(err)) {
			break
		}
		switch h := p.Header.(type) {
		case *mail.InlineHeader:
			t, _, _ := h.ContentType()
			switch {
			case t == "text/plain" && plano == "":
				plano = leerTexto(p.Body)
			case t == "text/html" && enHTML == "":
				enHTML = HTMLATexto(leerTexto(p.Body))
			case t != "text/plain" && t != "text/html":
				adjuntos = append(adjuntos, adjunto(p.Body, "", t))
			}
		case *mail.AttachmentHeader:
			nombre, _ := h.Filename()
			t, _, _ := h.ContentType()
			adjuntos = append(adjuntos, adjunto(p.Body, nombre, t))
		}
	}
	texto := plano
	if strings.TrimSpace(texto) == "" {
		texto = enHTML
	}
	return LimpiarTexto(texto), adjuntos
}

func leerTexto(r io.Reader) string {
	b, _ := io.ReadAll(io.LimitReader(r, MaxTextoParte))
	return strings.ToValidUTF8(string(b), "�")
}

func adjunto(r io.Reader, nombre, tipo string) Adjunto {
	n, _ := io.Copy(io.Discard, r)
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

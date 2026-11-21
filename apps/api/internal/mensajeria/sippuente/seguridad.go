package sippuente

import (
	"bytes"
	"errors"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/emiago/sipgo/sip"
)

// Lo que viene del navegador es texto no confiable (contratos/voz.md §3.12, E4c):
//
//   - validarCrudo: antes de analizarlo, cada fin de línea tiene que ser CRLF (un LF o un CR sueltos
//     no se aceptan: sipgo solo corta en CR y PJSIP también en LF, así que un «\n» dentro de un valor
//     colaría cabeceras o un segundo mensaje), sin caracteres de control salvo TAB, sin cabeceras
//     plegadas y con un solo Content-Length igual al cuerpo.
//   - soloPermitidas: el mensaje que sale hacia la Issabel se rearma solo con las cabeceras de una lista
//     blanca por dirección (nada de P-Asserted-Identity, Remote-Party-ID, Diversion, History-Info,
//     Replaces, Join, Privacy, Referred-By, Route…). Lo demás se descarta.

var (
	errCrudo = errors.New("sippuente: mensaje mal formado")
	// nombreCabecera: un token de RFC 3261 (y espacios antes de los dos puntos).
	nombreCabecera = regexp.MustCompile("^[A-Za-z0-9!%*_+.~'`-]+[ \t]*$")
)

// validarCrudo revisa el texto de un mensaje del navegador.
func validarCrudo(b []byte) error {
	i := bytes.Index(b, []byte("\r\n\r\n"))
	if i <= 0 {
		return errCrudo
	}
	cab, cuerpo := b[:i+2], b[i+4:]
	if cab[0] == ' ' || cab[0] == '\t' || !lineasCRLF(cab, true) || !lineasCRLF(cuerpo, false) {
		return errCrudo
	}
	largos := 0
	for i, l := range strings.Split(string(cab[:len(cab)-2]), "\r\n") {
		if i == 0 {
			continue // la línea de inicio
		}
		nombre, valor, ok := strings.Cut(l, ":")
		if !ok || !nombreCabecera.MatchString(nombre) {
			return errCrudo
		}
		n := strings.ToLower(strings.TrimSpace(nombre))
		if n == "content-length" || n == "l" {
			largos++
			v, err := strconv.Atoi(strings.TrimSpace(valor))
			if err != nil || v != len(cuerpo) {
				return errCrudo
			}
		}
	}
	if largos > 1 || (largos == 0 && len(cuerpo) > 0) {
		return errCrudo
	}
	return nil
}

// lineasCRLF: CR solo seguido de LF, LF solo después de CR, sin controles salvo TAB; en cabeceras,
// ninguna línea empieza con espacio o TAB (plegado).
func lineasCRLF(b []byte, cabeceras bool) bool {
	for i, c := range b {
		switch {
		case c == '\r':
			if i+1 >= len(b) || b[i+1] != '\n' {
				return false
			}
		case c == '\n':
			if i == 0 || b[i-1] != '\r' {
				return false
			}
			if cabeceras && i+1 < len(b) && (b[i+1] == ' ' || b[i+1] == '\t') {
				return false
			}
		case c == '\t':
		case c < 0x20 || c == 0x7f:
			return false
		}
	}
	return true
}

// Cabeceras que el navegador puede mandar a la Issabel (en minúsculas). Las tipadas (Via, From, To,
// Call-ID, CSeq, Contact, Content-Type, Content-Length, Max-Forwards, Refer-To) ya se revisaron.
var (
	cabecerasPeticion = map[string]bool{"via": true, "max-forwards": true, "from": true, "to": true, "call-id": true, "cseq": true, "contact": true,
		"content-type": true, "content-length": true, "expires": true, "allow": true, "supported": true, "user-agent": true, "refer-to": true,
		"session-expires": true, "min-se": true, "rack": true, "reason": true, "accept": true, "allow-events": true}
	cabecerasRespuesta = map[string]bool{"via": true, "from": true, "to": true, "call-id": true, "cseq": true, "contact": true, "content-type": true,
		"content-length": true, "allow": true, "supported": true, "server": true, "user-agent": true, "rseq": true, "require": true,
		"session-expires": true, "accept": true, "allow-events": true}
	// cabecerasHaciaNavegador: lo único de la Issabel que llega al navegador (peticiones y respuestas).
	// Fuera quedan Server/User-Agent (versión de la Issabel), P-Asserted-Identity/Remote-Party-ID
	// (identidad interna), Call-Info y Alert-Info (contestar solo, timbre de una URL), Diversion, los
	// desafíos y cualquier X-*. Via, Record-Route y Contact se quedan: el navegador los necesita para
	// responder y seguir el diálogo (llevan la dirección de la Issabel, que Iris no reescribe).
	cabecerasHaciaNavegador = map[string]bool{"via": true, "max-forwards": true, "from": true, "to": true, "call-id": true, "cseq": true,
		"contact": true, "record-route": true, "content-type": true, "content-length": true, "allow": true, "supported": true, "require": true,
		"unsupported": true, "rseq": true, "rack": true, "expires": true, "min-expires": true, "session-expires": true, "min-se": true,
		"event": true, "subscription-state": true, "accept": true, "allow-events": true, "reason": true, "retry-after": true}
)

// formasCortas: el nombre largo de cada forma compacta de RFC 3261 §7.3.3 y sus extensiones
// (RFC 3265, 3515, 3841, 3892, 4028, 4474). La Issabel puede mandar «k:» en vez de «Supported:».
var formasCortas = map[string]string{"a": "accept-contact", "b": "referred-by", "c": "content-type", "d": "request-disposition",
	"e": "content-encoding", "f": "from", "i": "call-id", "j": "reject-contact", "k": "supported", "l": "content-length", "m": "contact",
	"n": "identity-info", "o": "event", "r": "refer-to", "s": "subject", "t": "to", "u": "allow-events", "v": "via", "x": "session-expires",
	"y": "identity"}

// nombreLargo: el nombre de la cabecera en minúsculas, con la forma compacta ya traducida.
func nombreLargo(nombre string) string {
	n := strings.ToLower(nombre)
	if largo, ok := formasCortas[n]; ok {
		return largo
	}
	return n
}

// soloPermitidas deja en el mensaje solo las cabeceras de la lista (por nombre completo; la forma
// compacta vale lo mismo que su nombre largo).
func soloPermitidas(m conCabeceras, permitidas map[string]bool) {
	for {
		borro := false
		for _, h := range m.Headers() {
			if !permitidas[nombreLargo(h.Name())] {
				m.RemoveHeader(h.Name())
				borro = true
				break
			}
		}
		if !borro {
			return
		}
	}
}

// Require en una respuesta: solo 100rel y timer.
func requireValido(m sip.Message) bool {
	for _, h := range m.GetHeaders("Require") {
		for _, t := range strings.Split(h.Value(), ",") {
			if t = strings.ToLower(strings.TrimSpace(t)); t != "100rel" && t != "timer" {
				return false
			}
		}
	}
	return true
}

// Respuestas del navegador que harían que la Issabel siga un desvío (a la troncal): se cambian por 480.
func esDesvio(codigo int) bool {
	return (codigo >= 300 && codigo < 400) || codigo == 380 || codigo == 485
}

// DTMF por INFO: un solo dígito 0–9, * o #. Las secuencias de las funciones de Asterisk (## transferir,
// *2 transferir con consulta) no pasan. El DTMF en el audio (RFC 4733) no se ve aquí: lo cierra la
// configuración de la Issabel (runbook §9.3).
var errDTMF = errors.New("sippuente: dtmf no permitido")

func digitoINFO(req *sip.Request) (byte, error) {
	cuerpo := strings.TrimSpace(string(req.Body()))
	tipo := ""
	if h := req.GetHeaders("Content-Type"); len(h) == 1 {
		tipo = strings.ToLower(strings.TrimSpace(h[0].Value()))
	}
	if cuerpo == "" {
		return 0, nil // INFO sin cuerpo (keep-alive)
	}
	var senal string
	switch {
	case strings.HasPrefix(tipo, "application/dtmf-relay"):
		for _, l := range strings.Split(cuerpo, "\r\n") {
			k, v, _ := strings.Cut(l, "=")
			switch strings.ToLower(strings.TrimSpace(k)) {
			case "signal":
				senal = strings.TrimSpace(v)
			case "duration":
				if n, err := strconv.Atoi(strings.TrimSpace(v)); err != nil || n < 0 || n > 5000 {
					return 0, errDTMF
				}
			default:
				return 0, errDTMF
			}
		}
	case strings.HasPrefix(tipo, "application/dtmf"):
		senal = cuerpo
	default:
		return 0, errDTMF
	}
	if len(senal) != 1 || !strings.Contains("0123456789*#", senal) {
		return 0, errDTMF
	}
	return senal[0], nil
}

// dtmfPermitido: la secuencia con el dígito anterior (en 5 s) no es una función de Asterisk.
func (d *dialogo) dtmfPermitido(c byte, ahora time.Time) bool {
	previo := d.ultimoDTMF
	if ahora.Sub(d.ultimoDTMFEn) > 5*time.Second {
		previo = 0
	}
	if (previo == '#' && c == '#') || (previo == '*' && c == '2') {
		return false
	}
	d.ultimoDTMF, d.ultimoDTMFEn = c, ahora
	return true
}

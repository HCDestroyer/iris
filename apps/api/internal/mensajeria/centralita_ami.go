package mensajeria

import (
	"regexp"
	"strings"

	"github.com/HCDestroyer/iris/apps/api/internal/mensajeria/ami"
)

// Traducción de los eventos del AMI de Issabel 5 (Asterisk 18) a lo que le importa a Iris (E4a).
// Todo lo que llega es texto no confiable (contratos/voz.md §6.2, fila 24): de cada evento se toma
// una lista cerrada de campos y se valida su forma antes de usarlos.
//
// Canales (Issabel 5 trae chan_sip y PJSIP):
//   - Teléfono de una extensión: `PJSIP/201-0000001a` o `SIP/201-0000001a` (también IAX2). Es el
//     único formato del que se saca una extensión.
//   - Miembro de una cola o grupo: `Local/201@from-queue-0000002b;1` y `;2`. No se toma la extensión
//     del canal Local: el `;2` marca después al teléfono (`PJSIP/201-…`) con el mismo Linkedid y ese
//     DialBegin es el que hace salir la ventana. Así una cola que llama a sus miembros por Local no
//     duplica la ventana ni la muestra a quien no le sonó el teléfono.
//   - Troncal: cualquier otro (`PJSIP/claro-00000031`, `SIP/troncal-…`, `DAHDI/i1/…`).

var canalExtension = regexp.MustCompile(`^(?:PJSIP|SIP|IAX2)/([0-9]{2,8})-[0-9A-Za-z]{1,16}$`)

// extensionDeCanal devuelve la extensión de un canal de teléfono (no de un Local ni de una troncal).
func extensionDeCanal(canal string) (string, bool) {
	m := canalExtension.FindStringSubmatch(strings.TrimSpace(canal))
	if m == nil {
		return "", false
	}
	return m[1], true
}

var soloDigitosCrudos = regexp.MustCompile(`^\+?[0-9 ()-]{1,24}$`)

// normalizarNumero lleva el CallerID a E.164 o devuelve "" (oculto, anónimo o formato raro).
// Guatemala de fábrica: 8 dígitos → +502; 502 + 8 → +502…; 00 + internacional → +; un prefijo de
// salida (9 o 0) delante de 8 dígitos se quita.
func normalizarNumero(crudo string) string {
	s := strings.TrimSpace(crudo)
	if s == "" || !soloDigitosCrudos.MatchString(s) {
		return ""
	}
	mas := strings.HasPrefix(s, "+")
	d := soloDigitos(s)
	switch {
	case mas:
	case strings.HasPrefix(d, "00"):
		d, mas = d[2:], true
	case len(d) == 8:
		d = "502" + d
	case len(d) == 11 && strings.HasPrefix(d, "502"):
	case len(d) == 9 && (d[0] == '9' || d[0] == '0'):
		d = "502" + d[1:]
	default:
		return ""
	}
	if len(d) < 8 || len(d) > 15 || d[0] == '0' {
		return ""
	}
	return "+" + d
}

// pareceExtension: un CallerID de 2 a 7 dígitos es de una extensión (llamada interna).
func pareceExtension(crudo string) bool {
	s := strings.TrimSpace(crudo)
	if len(s) < 2 || len(s) > 7 {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

var linkedidValido = regexp.MustCompile(`^[0-9A-Za-z._-]{1,150}$`)

// Tipos de hecho que salen de un evento del AMI.
const (
	hechoSuena      = "suena"    // una extensión registrada empezó a sonar (entrante)
	hechoMarca      = "marca"    // una extensión registrada marcó hacia afuera (saliente)
	hechoContesta   = "contesta" // contestaron (DialEnd ANSWER)
	hechoCuelga     = "cuelga"   // colgó el canal de origen (Hangup con Uniqueid = Linkedid)
	hechoRegistro   = "registro" // CDR de la llamada
	hechoSinInteres = ""
)

// hechoAMI es lo que Iris toma de un evento: nada más.
type hechoAMI struct {
	tipo      string
	linkedid  string
	extension string // del teléfono que suena, contesta o marca
	numero    string // E.164 o "" (oculto)
	interno   bool
	// CDR: disposición y segundos facturables.
	contestada bool
	segundos   int
}

// traducirEvento lee un evento del AMI. extensiones: las registradas y activas (las demás se
// descartan sin mirar el resto del evento).
func traducirEvento(p ami.Paquete, registrada func(ext string) bool) hechoAMI {
	linked := strings.TrimSpace(p.Valor("Linkedid"))
	if linked == "" {
		linked = strings.TrimSpace(p.Valor("LinkedID"))
	}
	if !linkedidValido.MatchString(linked) {
		return hechoAMI{}
	}
	switch strings.ToLower(p.Valor("Event")) {
	case "dialbegin":
		origen, destino := p.Valor("Channel"), p.Valor("DestChannel")
		if ext, ok := extensionDeCanal(destino); ok {
			if !registrada(ext) {
				return hechoAMI{}
			}
			// Interna: la marca otro teléfono de la centralita, o un CallerID con forma de extensión.
			_, desdeTelefono := extensionDeCanal(origen)
			crudo := p.Valor("CallerIDNum")
			if desdeTelefono || pareceExtension(crudo) {
				return hechoAMI{tipo: hechoSuena, linkedid: linked, extension: ext, interno: true}
			}
			return hechoAMI{tipo: hechoSuena, linkedid: linked, extension: ext, numero: normalizarNumero(crudo)}
		}
		// Saliente: un teléfono registrado marca a una troncal (no a otro teléfono ni a un Local).
		if ext, ok := extensionDeCanal(origen); ok && registrada(ext) && !strings.HasPrefix(destino, "Local/") {
			for _, c := range []string{p.Valor("DestExten"), p.Valor("DestCallerIDNum"), p.Valor("Exten")} {
				if n := normalizarNumero(c); n != "" {
					return hechoAMI{tipo: hechoMarca, linkedid: linked, extension: ext, numero: n}
				}
			}
		}
	case "dialend":
		if !strings.EqualFold(p.Valor("DialStatus"), "ANSWER") {
			return hechoAMI{}
		}
		if ext, ok := extensionDeCanal(p.Valor("DestChannel")); ok && registrada(ext) {
			return hechoAMI{tipo: hechoContesta, linkedid: linked, extension: ext}
		}
		if ext, ok := extensionDeCanal(p.Valor("Channel")); ok && registrada(ext) && !strings.HasPrefix(p.Valor("DestChannel"), "Local/") {
			// La saliente: contestó el cliente.
			return hechoAMI{tipo: hechoContesta, linkedid: linked, extension: ext}
		}
	case "hangup":
		if u := strings.TrimSpace(p.Valor("Uniqueid")); u != "" && u == linked {
			return hechoAMI{tipo: hechoCuelga, linkedid: linked}
		}
	case "cdr":
		h := hechoAMI{tipo: hechoRegistro, linkedid: linked, contestada: strings.EqualFold(p.Valor("Disposition"), "ANSWERED")}
		h.segundos = enteroAcotado(p.Valor("BillableSeconds"), 24*3600)
		return h
	}
	return hechoAMI{}
}

// linkedidDeCDR: el CDR trae LinkedID solo con el mapping de cdr_manager.conf; si no, UniqueID
// (el del canal de origen, que en Asterisk coincide con el Linkedid de la llamada).
func linkedidDeCDR(p ami.Paquete) ami.Paquete {
	if p.Valor("LinkedID") == "" && p.Valor("Linkedid") == "" {
		p.Campos = append(p.Campos, [2]string{"Linkedid", p.Valor("UniqueID")})
	}
	return p
}

func enteroAcotado(s string, tope int) int {
	n := 0
	s = strings.TrimSpace(s)
	if s == "" || len(s) > 7 {
		return 0
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0
		}
		n = n*10 + int(r-'0')
	}
	return min(n, tope)
}

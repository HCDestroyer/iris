package sippuente

import (
	"errors"
	"strings"

	"github.com/emiago/sipgo/sip"
)

// Filtros de lo que el navegador manda a la Issabel (contratos/voz.md §3.12, E4c):
//
//   - Contact: solo una URI del propio navegador (host *.invalid y transport=ws|wss). Si no, el
//     navegador podría registrar o anunciar un destino de internet y la Issabel le mandaría SIP ahí.
//   - SDP: solo candidatos ICE de tipo relay (y, con la IP del relevo configurada, solo esa IP). Así la
//     Issabel solo manda audio al TURN de Iris, nunca a una dirección que elija el navegador.

var errFiltro = errors.New("sippuente: mensaje no permitido")

func contactoPropio(c *sip.ContactHeader) bool {
	if c == nil || (c.Address.Scheme != "sip" && c.Address.Scheme != "sips") || !strings.HasSuffix(strings.ToLower(c.Address.Host), ".invalid") {
		return false
	}
	t, _ := c.Address.UriParams.Get("transport")
	t = strings.ToLower(t)
	return t == "ws" || t == "wss"
}

// contactosValidos: todos los Contact del mensaje son del navegador ("*" solo en un REGISTER).
func contactosValidos(m sip.Message, registro bool) bool {
	for _, h := range m.GetHeaders("Contact") {
		if c, ok := h.(*sip.ContactHeader); ok {
			if !contactoPropio(c) {
				return false
			}
			continue
		}
		if registro && strings.TrimSpace(h.Value()) == "*" {
			continue
		}
		return false
	}
	return len(m.GetHeaders("m")) == 0
}

// filtrarSDP deja en el cuerpo solo candidatos relay (de relevo, si se conoce su IP). Sin cuerpo SDP no
// cambia nada; un cuerpo SDP sin ningún candidato válido en una media, o con c= ajeno, es un error.
func filtrarSDP(m sip.Message, relevo string) error {
	cuerpo := m.Body()
	if len(cuerpo) == 0 {
		return nil
	}
	tipo := ""
	if h := m.GetHeaders("Content-Type"); len(h) == 1 {
		tipo = h[0].Value()
	} else if h := m.GetHeaders("c"); len(h) == 1 {
		tipo = h[0].Value()
	}
	if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(tipo)), "application/sdp") {
		if req, ok := m.(*sip.Request); ok && req.Method == sip.INFO {
			return nil // DTMF por INFO (application/dtmf-relay)
		}
		return errFiltro
	}
	lineas := strings.Split(strings.ReplaceAll(string(cuerpo), "\r\n", "\n"), "\n")
	out := make([]string, 0, len(lineas))
	medias, conCandidato := 0, 0
	tieneCandidato := false
	cerrarMedia := func() {
		if medias > 0 && tieneCandidato {
			conCandidato++
		}
	}
	for _, l := range lineas {
		switch {
		case strings.HasPrefix(l, "m="):
			cerrarMedia()
			medias++
			tieneCandidato = false
			if f := strings.Fields(l); len(f) > 1 && f[1] == "0" {
				tieneCandidato = true // media rechazada: no lleva candidatos
			}
		case strings.HasPrefix(l, "c="):
			if relevo != "" {
				f := strings.Fields(l)
				if len(f) != 3 || (f[2] != relevo && f[2] != "0.0.0.0") {
					return errFiltro
				}
			}
		case strings.HasPrefix(l, "a=candidate:"):
			f := strings.Fields(l)
			// a=candidate:<fundación> <componente> <transporte> <prioridad> <ip> <puerto> typ <tipo> …
			if len(f) < 8 || f[6] != "typ" || f[7] != "relay" || (relevo != "" && f[4] != relevo) {
				continue
			}
			tieneCandidato = true
		}
		out = append(out, l)
	}
	cerrarMedia()
	if medias > 0 && conCandidato != medias {
		return errFiltro
	}
	m.SetBody([]byte(strings.Join(out, "\r\n")))
	return nil
}

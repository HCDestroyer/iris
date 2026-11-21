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
	// Sin contraseña ni ?cabeceras en la URI, y solo los parámetros transport y ob: maddr, lr y los
	// demás cambiarían a dónde manda la Issabel.
	if c.Address.Password != "" || c.Address.Headers.Length() > 0 || c.Address.Wildcard {
		return false
	}
	t := ""
	for _, k := range c.Address.UriParams.Keys() {
		switch strings.ToLower(k) {
		case "transport":
			if t != "" {
				return false // dos transport: cada lado podría leer otro
			}
			t, _ = c.Address.UriParams.Get(k)
			if t == "" {
				return false
			}
		case "ob":
		default:
			return false
		}
	}
	t = strings.ToLower(t)
	return t == "ws" || t == "wss"
}

// filtrarSDP deja en el cuerpo solo candidatos relay (de relevo, si se conoce su IP). Sin cuerpo SDP no
// cambia nada; un cuerpo SDP sin ningún candidato válido en una media, o con c= ajeno, es un error.
// atributosSDP: los a= que el navegador puede mandar (WebRTC); los demás (remote-candidates, …) se quitan.
var atributosSDP = map[string]bool{"rtpmap": true, "fmtp": true, "rtcp": true, "rtcp-mux": true, "rtcp-rsize": true, "rtcp-fb": true, "ice-ufrag": true,
	"ice-pwd": true, "ice-options": true, "fingerprint": true, "setup": true, "mid": true, "sendrecv": true, "sendonly": true, "recvonly": true,
	"inactive": true, "ssrc": true, "ssrc-group": true, "msid": true, "msid-semantic": true, "group": true, "extmap": true, "extmap-allow-mixed": true,
	"candidate": true, "end-of-candidates": true, "ptime": true, "maxptime": true, "tls-id": true}

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
	// El cuerpo del navegador ya pasó validarCrudo (solo CRLF); un CR o LF suelto aquí es un error.
	texto := string(cuerpo)
	if strings.Count(texto, "\r") != strings.Count(texto, "\r\n") || strings.Count(texto, "\n") != strings.Count(texto, "\r\n") {
		return errFiltro
	}
	lineas := strings.Split(texto, "\r\n")
	out := make([]string, 0, len(lineas))
	medias, conCandidato := 0, 0
	tieneCandidato := false
	cerrarMedia := func() {
		if medias > 0 && tieneCandidato {
			conCandidato++
		}
	}
	for _, l := range lineas {
		if l == "" {
			out = append(out, l)
			continue
		}
		if len(l) < 2 || l[1] != '=' || !strings.ContainsRune("vosictbma", rune(l[0])) {
			continue // líneas fuera de la lista (k=, z=, r=, u=, e=, p=…) se quitan
		}
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
		case strings.HasPrefix(l, "a="):
			nombre, valor, _ := strings.Cut(l[2:], ":")
			if !atributosSDP[strings.ToLower(nombre)] {
				continue
			}
			switch strings.ToLower(nombre) {
			case "candidate":
				f := strings.Fields(valor)
				// <fundación> <componente> <transporte> <prioridad> <ip> <puerto> typ <tipo> …
				if len(f) < 8 || f[6] != "typ" || f[7] != "relay" || (relevo != "" && f[4] != relevo) {
					continue
				}
				tieneCandidato = true
			case "rtcp":
				// a=rtcp:<puerto> [IN IP4 <ip>]: con dirección, solo la del relevo.
				if f := strings.Fields(valor); len(f) > 1 && (len(f) != 4 || (relevo != "" && f[3] != relevo)) {
					continue
				}
			}
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

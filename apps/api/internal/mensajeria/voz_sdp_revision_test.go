package mensajeria_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/HCDestroyer/iris/apps/api/internal/mensajeria"
)

// Revisión de #217. H1: lo saneado siempre pasa ValidarSDP, también el tamaño (rearmado con CRLF, un
// SDP con LF y relleno crecía por encima de 8 KB y viajaba a Meta).
func TestSanearSDPSiemprePasaValidar(t *testing.T) {
	base := strings.ReplaceAll(sdpChromiumSinCandidato, "\r\n", "\n")
	// Relleno con LF hasta el tope: al rearmar con CRLF crecería por encima de MaxSDP.
	relleno := base
	for len(relleno)+4 <= mensajeria.MaxSDP {
		relleno += "b=x\n"
	}
	if _, err := mensajeria.SanearSDP(relleno); err == nil {
		t.Fatal("un SDP que al sanear pasa de 8 KB debía rechazarse")
	}
	// El mismo relleno con CRLF desde el principio cabe y lo saneado pasa la validación estricta.
	cabe := sdpChromiumSinCandidato
	for len(cabe)+5 <= mensajeria.MaxSDP {
		cabe += "b=x\r\n"
	}
	san, err := mensajeria.SanearSDP(cabe)
	if err != nil {
		t.Fatalf("relleno que cabe: %v", err)
	}
	if err := mensajeria.ValidarSDP(san); err != nil {
		t.Fatalf("ValidarSDP(SanearSDP(x)): %v", err)
	}
}

// H2 y H3: lo quitado se informa sin repetir, con tope, y solo con nombres [a-z0-9-] (nunca valores ni
// direcciones).
func TestSanearSDPQuitadoAcotado(t *testing.T) {
	var extra strings.Builder
	for range 1000 {
		extra.WriteString("a=z\r\n")
	}
	for i := range 40 {
		fmt.Fprintf(&extra, "a=x-%d:1\r\n", i)
	}
	extra.WriteString("a=x-addr 192.168.1.23 9\r\n")
	sdp := strings.Replace(sdpOfertaChromiumSal, "111 63 9 0 8 13 110 126", "111 63 9 0 8 13 110 126 104 105", 1) +
		"a=rtpmap:104 192.168.1.23/8000\r\n" + extra.String()
	if len(sdp) > mensajeria.MaxSDP {
		t.Fatalf("la prueba se pasó del tope: %d", len(sdp))
	}
	san, quitado, err := mensajeria.SanearSDPQuitado(sdp)
	if err != nil {
		t.Fatalf("sanear: %v", err)
	}
	if len(quitado) != mensajeria.MaxQuitadoSDP+1 || !strings.HasPrefix(quitado[len(quitado)-1], "otros=") {
		t.Fatalf("quitado sin tope: %d %q", len(quitado), quitado)
	}
	visto := map[string]bool{}
	for _, q := range quitado {
		if visto[q] {
			t.Fatalf("quitado repetido: %q", quitado)
		}
		visto[q] = true
		if strings.Contains(q, "192.168") || strings.Contains(q, " ") {
			t.Fatalf("un valor llegó al registro: %q", q)
		}
	}
	if strings.Contains(san, " 104") || strings.Contains(san, " 105") {
		t.Fatalf("quedaron los formatos desconocidos:\n%s", san)
	}
	// Solo nombres: el códec con dirección sale como «otro» y el formato sin rtpmap como «pt105».
	_, q2, err := mensajeria.SanearSDPQuitado(strings.Replace(sdpOfertaChromiumSal, "111 63 9 0 8 13 110 126", "111 63 9 0 8 13 110 126 104 105", 1) +
		"a=rtpmap:104 192.168.1.23/8000\r\na=x-addr 192.168.1.23 9\r\n")
	if err != nil || strings.Join(q2, ",") != "a=x-addr,codec=otro,codec=pt105" {
		t.Fatalf("nombres: %v %q", err, q2)
	}
}

// Lo que llega de Meta se valida entero: un códec fuera de la lista lo rechaza. Y lo estructural que
// se agregó en la revisión (P2/P3): rtpmap repetido, número de formato fuera de 0–127, a=setup y huella.
func TestValidarSDPReglasNuevas(t *testing.T) {
	malos := map[string]string{
		"códec de Meta fuera de la lista": strings.Replace(sdpRespuestaMetaSalDoc, "111 9 0 8 110 126", "111 104 9 0 8 110 126", 1) + "a=rtpmap:104 L16/16000\r\n",
		"Opus cambiado por L16":           strings.Replace(sdpRespuestaMetaSalDoc, "a=rtpmap:111 opus/48000/2", "a=rtpmap:111 L16/16000", 1),
		"rtpmap repetido":                 sdpRespuestaMetaSalDoc + "a=rtpmap:111 opus/48000/2\r\n",
		"formato fuera de 0–127":          strings.Replace(sdpRespuestaMetaSalDoc, "a=rtpmap:126 telephone-event/8000", "a=rtpmap:300 telephone-event/8000", 1),
		"formato no numérico en m=":       strings.Replace(sdpRespuestaMetaSalDoc, "111 9 0 8 110 126", "111 9 0 8 110 x", 1),
		"a=setup desconocido":             strings.Replace(sdpRespuestaMetaSalDoc, "a=setup:active", "a=setup:holdconn", 1),
		"huella con otro algoritmo":       strings.Replace(sdpRespuestaMetaSalDoc, "a=fingerprint:sha-256 ", "a=fingerprint:md5 ", 1),
		"huella sin hexadecimal":          strings.Replace(sdpRespuestaMetaSalDoc, "a=fingerprint:sha-256 46:14:", "a=fingerprint:sha-256 4G:14:", 1),
		"huella sin separar por «:»":      strings.Replace(sdpRespuestaMetaSalDoc, "46:14:2B", "46142B", 1),
	}
	for nombre, malo := range malos {
		if err := mensajeria.ValidarSDP(malo); err == nil {
			t.Errorf("%s: debía rechazarse", nombre)
		}
		// Del navegador, el códec desconocido se quita; lo estructural se rechaza igual.
		if _, err := mensajeria.SanearSDP(malo); err == nil && nombre != "códec de Meta fuera de la lista" {
			t.Errorf("%s: SanearSDP debía rechazarlo también", nombre)
		}
	}
	for nombre, bueno := range map[string]string{
		"oferta de la doc":    sdpOfertaMetaDoc,
		"respuesta de la doc": sdpRespuestaMetaSalDoc,
		"fixture anterior":    sdpOfertaMeta,
		"setup passive":       strings.Replace(sdpRespuestaMetaSalDoc, "a=setup:active", "a=setup:passive", 1),
		"algoritmo en mayús":  strings.Replace(sdpRespuestaMetaSalDoc, "a=fingerprint:sha-256 ", "a=fingerprint:SHA-256 ", 1),
	} {
		if err := mensajeria.ValidarSDP(bueno); err != nil {
			t.Errorf("%s: %v", nombre, err)
		}
	}
}

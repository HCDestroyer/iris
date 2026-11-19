package mensajeria_test

import (
	"strings"
	"testing"

	"github.com/HCDestroyer/iris/apps/api/internal/mensajeria"
)

// SDP reales (primera prueba contra Meta, 0.29.0): los dobles anteriores usaban una oferta del
// navegador con solo Opus y una respuesta de Meta inventada. Estos fixtures son, tal cual:
//
//   - oferta_meta_doc.sdp: la «Sample SDP offer structure» de llamadas iniciadas por el usuario
//     (developers.facebook.com/documentation/business-messaging/whatsapp/calling/reference).
//   - respuesta_meta_saliente_doc.sdp: el SDP de «Sample call connect webhook» (BUSINESS_INITIATED)
//     de la misma página: Meta contesta la oferta del navegador con Opus, G722, PCMU, PCMA y
//     telephone-event, y con a=setup:active.
//   - respuesta_chromium_sin_candidatos.sdp: la respuesta de Chromium 153 a esa oferta, leída justo
//     después de setLocalDescription (como hace medios.ts): sin candidatos y con c=0.0.0.0, igual que
//     la «Sample SDP answer structure» de Meta.
//   - oferta_chromium_saliente.sdp: la oferta real de Chromium 153 (createOffer con micrófono), con
//     red, G722, PCMU, PCMA, CN y a=rtcp-xr.
var (
	sdpOfertaMetaDoc        = leerSDP("oferta_meta_doc.sdp")
	sdpRespuestaMetaSalDoc  = leerSDP("respuesta_meta_saliente_doc.sdp")
	sdpChromiumSinCandidato = leerSDP("respuesta_chromium_sin_candidatos.sdp")
	sdpOfertaChromiumSal    = leerSDP("oferta_chromium_saliente.sdp")
)

// Entrante: la oferta documentada de Meta pasa y la respuesta de Chromium sin candidatos se manda
// tal cual (Meta es ICE-lite y la acepta así: es la forma de su propio ejemplo).
func TestSDPEntranteReal(t *testing.T) {
	if err := mensajeria.ValidarSDP(sdpOfertaMetaDoc); err != nil {
		t.Fatalf("la oferta documentada de Meta debe pasar: %v", err)
	}
	san, err := mensajeria.SanearSDP(sdpChromiumSinCandidato)
	if err != nil {
		t.Fatalf("la respuesta de Chromium debe aceptarse: %v", err)
	}
	if !strings.Contains(san, "a=setup:active\r\n") || !strings.Contains(san, "c=IN IP4 0.0.0.0\r\n") {
		t.Fatalf("respuesta saneada:\n%s", san)
	}
}

// Saliente: la oferta real de Chromium se acepta (y lo que Meta no usa se deja: Meta elige de lo
// ofrecido), y la respuesta documentada de Meta se entrega al navegador.
func TestSDPSalienteReal(t *testing.T) {
	san, err := mensajeria.SanearSDP(sdpOfertaChromiumSal)
	if err != nil {
		t.Fatalf("la oferta real de Chromium para llamar debe aceptarse: %v", err)
	}
	if !strings.Contains(san, "a=setup:actpass\r\n") || !strings.Contains(san, "opus/48000/2") {
		t.Fatalf("oferta saneada:\n%s", san)
	}
	if err := mensajeria.ValidarSDP(sdpRespuestaMetaSalDoc); err != nil {
		t.Fatalf("la respuesta documentada de Meta a una saliente debe pasar: %v", err)
	}
}

// Lo que el navegador agrega fuera de la lista cerrada no tumba la llamada: el atributo desconocido y
// el formato desconocido (con su rtpmap, fmtp y rtcp-fb) se quitan de lo que viaja a Meta, y se
// informa solo su nombre. Lo que llega de Meta (ValidarSDP) se sigue rechazando entero.
func TestSanearSDPQuitaLoDesconocido(t *testing.T) {
	raro := strings.Replace(sdpOfertaChromiumSal, "m=audio 9 UDP/TLS/RTP/SAVPF 111 63 9 0 8 13 110 126",
		"m=audio 9 UDP/TLS/RTP/SAVPF 111 63 9 0 8 13 110 126 104", 1) +
		"a=rtpmap:104 L16/16000\r\na=fmtp:104 algo=1\r\na=rtcp-fb:104 nack\r\na=x-google-flag:conference\r\n"
	san, quitado, err := mensajeria.SanearSDPQuitado(raro)
	if err != nil {
		t.Fatalf("lo desconocido se quita, no se rechaza: %v", err)
	}
	for _, malo := range []string{" 104", "L16", "a=fmtp:104", "a=rtcp-fb:104", "x-google-flag"} {
		if strings.Contains(san, malo) {
			t.Fatalf("quedó %q:\n%s", malo, san)
		}
	}
	if !strings.Contains(san, "m=audio 9 UDP/TLS/RTP/SAVPF 111 63 9 0 8 13 110 126\r\n") {
		t.Fatalf("el m= debía conservar los formatos conocidos:\n%s", san)
	}
	if strings.Join(quitado, ",") != "a=x-google-flag,codec=l16" {
		t.Fatalf("quitado: %q", quitado)
	}
	if err := mensajeria.ValidarSDP(san); err != nil {
		t.Fatalf("lo saneado debe pasar la validación estricta: %v", err)
	}
	if mensajeria.ValidarSDP(sdpRespuestaMetaSalDoc+"a=x-google-flag:conference\r\n") == nil {
		t.Fatal("ValidarSDP (lo que llega de Meta) debía seguir rechazando un atributo desconocido")
	}
}

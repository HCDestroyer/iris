package mensajeria

import (
	"bytes"
	"context"
	"encoding/json"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	mensajeriabd "github.com/HCDestroyer/iris/apps/api/internal/mensajeria/bd"
)

// Documentos de identidad en el texto de un cliente (DPI/CUI y NIT). El proveedor de IA nunca los ve
// (contratos/conversaciones.md §17.9): el servidor los detecta, se los pasa al modelo como *** con la
// marca «el cliente envió un documento», y la herramienta verificar_identidad toma el valor del
// mensaje original, del lado del servidor.

var (
	// reDocClave: lo que sigue a «nit», «dpi», «cui», «documento»… (saltando «es», «:», «número»…).
	reDocClave = regexp.MustCompile(`(?i)\b(?:nit|dpi|cui|documento|identificaci[oó]n|c[eé]dula)\b(?:[\s:=#.\-]|\b(?:es|de|mi|personal|n[uú]mero|num|no)\b)*([0-9](?:[ \-]?[0-9]){3,14}(?:[ \-]?[kK]\b)?)`)
	// reDocSuelto: un número con forma de documento aunque no lo nombre: 7 a 13 dígitos seguidos (NIT
	// o DPI), un NIT con guion y dígito verificador (puede ser K), o un DPI escrito 1234 56789 0101.
	reDocSuelto = regexp.MustCompile(`(?i)\b\d{6,12}-?[\dk]\b|\b\d{4}[ -]\d{5}[ -]\d{4}\b`)
	// reReferenciaAntes: el número es la referencia de una boleta, un ticket o una orden, no un documento.
	reReferenciaAntes = regexp.MustCompile(`(?i)\b(?:referencia|ref|boleta|transacci[oó]n|autorizaci[oó]n|comprobante|dep[oó]sito|transferencia|ticket|tk|orden|ot|contrato)\b[^0-9\n!?]{0,20}$`)
)

// documentosEn devuelve los tramos [inicio, fin) del texto que son un documento de identidad.
func documentosEn(s string) [][2]int {
	var out [][2]int
	for _, g := range reDocClave.FindAllStringSubmatchIndex(s, -1) {
		out = append(out, [2]int{g[2], g[3]})
	}
	for _, g := range reDocSuelto.FindAllStringIndex(s, -1) {
		if reReferenciaAntes.MatchString(s[max(0, g[0]-40):g[0]]) {
			continue
		}
		out = append(out, [2]int{g[0], g[1]})
	}
	slices.SortFunc(out, func(a, b [2]int) int { return a[0] - b[0] })
	// Sin solapes: se unen.
	var unidos [][2]int
	for _, t := range out {
		if n := len(unidos); n > 0 && t[0] <= unidos[n-1][1] {
			unidos[n-1][1] = max(unidos[n-1][1], t[1])
			continue
		}
		unidos = append(unidos, t)
	}
	return unidos
}

// taparDocumentosTexto reemplaza por *** todos los documentos del texto; crudos: lo que se tapó.
func taparDocumentosTexto(s string) (tapado string, crudos []string) {
	tramos := documentosEn(s)
	if len(tramos) == 0 {
		return s, nil
	}
	var b strings.Builder
	ult := 0
	for _, t := range tramos {
		b.WriteString(s[ult:t[0]])
		b.WriteString(auditoria.Mascara)
		crudos = append(crudos, s[t[0]:t[1]])
		ult = t[1]
	}
	b.WriteString(s[ult:])
	return b.String(), crudos
}

// documentoNormalizado: el documento como lo espera Argos (sin espacios ni guiones, K en mayúscula).
func documentoNormalizado(crudo string) string {
	return strings.ToUpper(strings.NewReplacer(" ", "", "-", "").Replace(crudo))
}

// taparDocumentos deja como *** los documentos de identidad de los últimos mensajes del cliente en
// este periodo: en el mensaje guardado (todas las coincidencias) y en el payload crudo de Meta
// (mensajeria.entrantes). Corre al verificar la identidad y al cerrar un turno en el que el asistente
// podía pedirla, aunque la herramienta no haya llegado a correr.
func (m *Modulo) taparDocumentos(ctx context.Context, q *mensajeriabd.Queries, c mensajeriabd.MensajeriaConversacion, desde time.Time) error {
	ms, err := q.UltimosEntrantesTexto(ctx, mensajeriabd.UltimosEntrantesTextoParams{ConversacionID: c.ID, Desde: desde})
	if err != nil {
		return err
	}
	for _, f := range ms {
		if f.Texto == nil {
			continue
		}
		tapado, crudos := taparDocumentosTexto(*f.Texto)
		if len(crudos) == 0 {
			continue
		}
		if err := q.EnmascararTextoMensaje(ctx, mensajeriabd.EnmascararTextoMensajeParams{ID: f.ID, Texto: &tapado}); err != nil {
			return err
		}
		if f.ProveedorID == nil || *f.ProveedorID == "" {
			continue
		}
		if err := taparEntranteMeta(ctx, q, c.CanalID, *f.ProveedorID); err != nil {
			return err
		}
	}
	return nil
}

// taparEntranteMeta tapa los documentos en el payload crudo de Meta de ese mensaje, y solo en el
// texto del mensaje (text.body y el pie de una imagen, un video o un documento): el resto del payload
// (ids, teléfonos, marcas de tiempo, otros mensajes del mismo lote) no se toca.
func taparEntranteMeta(ctx context.Context, q *mensajeriabd.Queries, canal int64, wamid string) error {
	return taparEntranteMetaCon(ctx, q, canal, wamid, tapadorDocumentos)
}

// taparEntranteMetaCon tapa el texto del mensaje en el payload crudo con el tapador dado.
func taparEntranteMetaCon(ctx context.Context, q *mensajeriabd.Queries, canal int64, wamid string, tapador func(string) (string, bool)) error {
	filas, err := q.EntrantesDeMensaje(ctx, mensajeriabd.EntrantesDeMensajeParams{CanalID: canal, ProveedorID: wamid})
	if err != nil {
		return err
	}
	for _, f := range filas {
		nuevo, cambio, err := payloadTapadoCon(f.Payload, wamid, tapador)
		if err != nil {
			return err
		}
		if !cambio {
			continue
		}
		if err := q.FijarPayloadEntrante(ctx, mensajeriabd.FijarPayloadEntranteParams{ID: f.ID, Payload: nuevo}); err != nil {
			return err
		}
	}
	return nil
}

// payloadTapado devuelve el payload con el texto de ese mensaje tapado. UseNumber: los números del
// payload (ids, marcas de tiempo) se reescriben tal como llegaron, sin pasar por float64.
func payloadTapado(payload []byte, wamid string) ([]byte, bool, error) {
	return payloadTapadoCon(payload, wamid, tapadorDocumentos)
}

// tapadorDocumentos tapa los documentos de identidad.
func tapadorDocumentos(s string) (string, bool) {
	t, crudos := taparDocumentosTexto(s)
	return t, len(crudos) > 0
}

func payloadTapadoCon(payload []byte, wamid string, tapador func(string) (string, bool)) ([]byte, bool, error) {
	var v any
	dec := json.NewDecoder(bytes.NewReader(payload))
	dec.UseNumber()
	if dec.Decode(&v) != nil || !taparTextoDeMensaje(v, wamid, tapador) {
		return nil, false, nil
	}
	nuevo, err := json.Marshal(v)
	return nuevo, err == nil, err
}

// taparTextoDeMensaje recorre el payload y tapa el texto del mensaje con ese id. Dice si cambió algo.
func taparTextoDeMensaje(v any, wamid string, tapador func(string) (string, bool)) bool {
	cambio := false
	switch x := v.(type) {
	case []any:
		for _, h := range x {
			cambio = taparTextoDeMensaje(h, wamid, tapador) || cambio
		}
	case map[string]any:
		if id, _ := x["id"].(string); id == wamid {
			tapar := func(m map[string]any, campo string) {
				if s, ok := m[campo].(string); ok {
					if t, ok := tapador(s); ok {
						m[campo], cambio = t, true
					}
				}
			}
			if t, ok := x["text"].(map[string]any); ok {
				tapar(t, "body")
			}
			for _, k := range []string{"image", "video", "document"} {
				if t, ok := x[k].(map[string]any); ok {
					tapar(t, "caption")
				}
			}
			return cambio
		}
		for _, h := range x {
			cambio = taparTextoDeMensaje(h, wamid, tapador) || cambio
		}
	}
	return cambio
}

// cuiValido: un DPI/CUI de Guatemala (13 dígitos: 8 del número, 1 verificador módulo 11, 2 del
// departamento y 2 del municipio).
func cuiValido(d string) bool {
	if len(d) != 13 {
		return false
	}
	suma := 0
	for i := 0; i < 13; i++ {
		if d[i] < '0' || d[i] > '9' {
			return false
		}
		if i < 8 {
			suma += int(d[i]-'0') * (i + 2)
		}
	}
	depto := int(d[9]-'0')*10 + int(d[10]-'0')
	muni := int(d[11]-'0')*10 + int(d[12]-'0')
	return suma%11 == int(d[8]-'0') && depto >= 1 && depto <= 22 && muni >= 1
}

// nitValido: un NIT de Guatemala (dígito verificador módulo 11; K = 10).
func nitValido(d string) bool {
	if len(d) < 7 || len(d) > 12 {
		return false
	}
	suma := 0
	for i := 0; i < len(d)-1; i++ {
		if d[i] < '0' || d[i] > '9' {
			return false
		}
		suma += int(d[i]-'0') * (len(d) - i)
	}
	ver := (11 - suma%11) % 11
	ult := d[len(d)-1]
	if ult == 'K' {
		return ver == 10
	}
	return ult >= '0' && ult <= '9' && int(ult-'0') == ver
}

// documentoCandidato elige, del texto del cliente, el dato que se manda a Argos para verificar la
// identidad: un DPI válido; si no, lo que el cliente nombró como «nit», «dpi», «cui» o «documento»
// (aunque parezca un teléfono: lo nombró como documento), salvo que sea el número del chat; si no, un
// NIT válido que no parezca un teléfono. Sin un candidato claro devuelve "" (se le pide el dato otra
// vez, sin gastar un intento). invalido: nombró un documento de 13 dígitos que no es un DPI válido.
func documentoCandidato(texto, telefono string) (valor string, invalido bool) {
	esDelChat := func(d string) bool { return len(d) >= 8 && telefono != "" && strings.HasSuffix(telefono, d) }
	pareceTelefono := func(d string) bool {
		return (len(d) == 8 && d[0] >= '2' && d[0] <= '7') || esDelChat(d) || (len(d) == 11 && strings.HasPrefix(d, "502"))
	}
	var nombrados, sueltos []string
	for _, g := range reDocClave.FindAllStringSubmatchIndex(texto, -1) {
		nombrados = append(nombrados, documentoNormalizado(texto[g[2]:g[3]]))
	}
	for _, t := range documentosEn(texto) {
		sueltos = append(sueltos, documentoNormalizado(texto[t[0]:t[1]]))
	}
	for _, d := range append(append([]string{}, nombrados...), sueltos...) {
		if cuiValido(d) {
			return d, false
		}
	}
	for _, d := range nombrados {
		switch {
		case esDelChat(d):
		case len(d) == 13:
			invalido = true
		case len(d) >= 7 && len(d) <= 12:
			return d, false
		}
	}
	for _, d := range sueltos {
		if nitValido(d) && !pareceTelefono(d) {
			return d, false
		}
	}
	return "", invalido
}

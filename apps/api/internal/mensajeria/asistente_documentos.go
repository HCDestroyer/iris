package mensajeria

import (
	"context"
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
		for _, crudo := range crudos {
			if err := q.EnmascararEntranteMeta(ctx, mensajeriabd.EnmascararEntranteMetaParams{CanalID: c.CanalID, ProveedorID: *f.ProveedorID, Valor: crudo}); err != nil {
				return err
			}
		}
	}
	return nil
}

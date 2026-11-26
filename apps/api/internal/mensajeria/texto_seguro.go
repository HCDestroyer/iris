package mensajeria

import (
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// Texto que Argos manda dentro de una plantilla (concepto, nombre y plan de la bienvenida): nunca un
// enlace ni un separador Unicode. La misma revisión para todos (revisión de seguridad de #295).

// normalizarDeArgos: NFKC (así «pago․com» con U+2024 o «ｅｖｉｌ．ｓｈｏｐ» quedan en ASCII) y los puntos
// ideográficos («。», y «｡», que NFKC lleva a «。») como punto, para buscar dominios.
func normalizarDeArgos(v string) string {
	return strings.ReplaceAll(norm.NFKC.String(v), "。", ".")
}

var (
	// reFormaEnlace: un esquema (://), www., un punto seguido de dos o más letras (cualquier dominio,
	// no una lista: evil.shop, bit.do, pago.online), una IPv4 o un puerto.
	reFormaEnlace = regexp.MustCompile(`(?i)(://|www\.|\.\pL{2,}|\d{1,3}(\.\d{1,3}){3}|:\d{2,5})`)
	// reBarraConLetra: una barra pegada a una letra (bit.do/x, ruta/abc); «10/5» sí pasa.
	reBarraConLetra = regexp.MustCompile(`\pL/|/\pL`)
	reEspacios4     = regexp.MustCompile(`\s{4,}`)
)

// separadorOFormato: caracteres Unicode de control, de formato o separadores de línea o párrafo (Cc,
// Cf, Zl, Zp: también U+0085, U+2028, U+2029 y los invisibles de dirección o de ancho cero).
func separadorOFormato(r rune) bool {
	return unicode.In(r, unicode.Cc, unicode.Cf, unicode.Zl, unicode.Zp)
}

// motivoTextoInseguro dice por qué un texto de Argos no puede ir en una plantilla ("" = puede). Mide
// el largo (1 a max) sobre el texto tal como llega y sobre el normalizado, y busca enlaces en el
// normalizado. barras: con false, ninguna «/»; con true, solo entre números.
func motivoTextoInseguro(valor string, max int, barras bool) string {
	v := normalizarDeArgos(valor)
	switch {
	case strings.TrimSpace(v) == "" || len([]rune(valor)) > max || len([]rune(v)) > max:
		return "De 1 a " + strconv.Itoa(max) + " caracteres"
	case strings.ContainsFunc(valor, separadorOFormato) || strings.ContainsFunc(v, separadorOFormato):
		return "Sin saltos de línea ni caracteres de control o de formato"
	case reEspacios4.MatchString(v):
		return "Sin 4 o más espacios seguidos"
	case strings.Contains(v, "@") || reFormaEnlace.MatchString(v) || (!barras && strings.Contains(v, "/")) || (barras && reBarraConLetra.MatchString(v)):
		return "Sin enlaces, dominios, direcciones IP ni correos"
	}
	return ""
}

package mensajeria

import (
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// Texto que Argos manda dentro de una plantilla (concepto, nombre y plan de la bienvenida): nunca un
// enlace, un separador Unicode ni, donde se pide, un número largo (teléfono o cuenta). La misma revisión para todos
// (revisiones de seguridad de #295 y #300).

// rellenos: caracteres invisibles que no son separadores para Unicode y se ven como un espacio
// (relleno Hangul U+3164 y U+1160, al que NFKC lo lleva; relleno inicial U+115F; Braille en blanco
// U+2800). Se tratan como espacio.
var rellenos = strings.NewReplacer("ㅤ", " ", "ᅠ", " ", "ᅟ", " ", "⠀", " ")

// normalizarDeArgos: NFKC (así «pago․com» con U+2024 o «ｅｖｉｌ．ｓｈｏｐ» quedan en ASCII), los
// rellenos invisibles como espacio y los puntos ideográficos («。», y «｡», que NFKC lleva a «。») como
// punto, para buscar dominios.
func normalizarDeArgos(v string) string {
	return strings.ReplaceAll(rellenos.Replace(norm.NFKC.String(v)), "。", ".")
}

var (
	// reFormaEnlace: un esquema (://), www., un punto seguido de dos o más letras (cualquier dominio,
	// no una lista: evil.shop, bit.do, pago.online), una IPv4 o un puerto.
	reFormaEnlace = regexp.MustCompile(`(?i)(://|www\.|\.\pL{2,}|\d{1,3}(\.\d{1,3}){3}|:\d{2,5})`)
	// reBarraConLetra: una barra pegada a una letra (bit.do/x, ruta/abc); «10/5» sí pasa.
	reBarraConLetra = regexp.MustCompile(`\pL/|/\pL`)
	reEspacios4     = regexp.MustCompile(`\s{4,}`)
	// reVelocidad: una velocidad del tipo «100/20» son dos números, no uno de 5 dígitos.
	reVelocidad = regexp.MustCompile(`(\p{Nd}{1,4})/(\p{Nd}{1,4})`)
)

// separadorOFormato: caracteres Unicode de control, de formato o separadores de línea o párrafo (Cc,
// Cf, Zl, Zp: también U+0085, U+2028, U+2029 y los invisibles de dirección o de ancho cero).
func separadorOFormato(r rune) bool {
	return unicode.In(r, unicode.Cc, unicode.Cf, unicode.Zl, unicode.Zp)
}

// digitosSinLetra: el mayor número de dígitos (de cualquier sistema: arábigos, devanagari…) que hay
// sin una letra en medio; cualquier otro separador («55,12», «55_12», «(55)12», espacios) no corta,
// salvo la barra de una velocidad («100/20»: dos números).
func digitosSinLetra(v string) int {
	maximo, n := 0, 0
	for _, r := range reVelocidad.ReplaceAllString(v, "${1}a${2}") {
		switch {
		case unicode.IsDigit(r):
			n++
			maximo = max(maximo, n)
		case unicode.IsLetter(r):
			n = 0
		}
	}
	return maximo
}

// reglasTexto: lo que se exige a un texto de Argos.
type reglasTexto struct {
	// max: largo de 1 a max, sobre el texto tal como llega y sobre el normalizado.
	max int
	// barraEntreNumeros: una «/» solo entre números («10/5»); sin ella, ninguna «/».
	barraEntreNumeros bool
	// maxDigitos: a lo más esta cantidad de dígitos sin una letra en medio (0 = sin límite). Con 6, un
	// teléfono (8) o una cuenta no pasan y sí «Internet 100/20 Mbps» o «Plan 300 Mbps 2025».
	maxDigitos int
}

// motivoTextoInseguro dice por qué un texto de Argos no puede ir en una plantilla ("" = puede). Busca
// en el texto normalizado.
func motivoTextoInseguro(valor string, r reglasTexto) string {
	v := normalizarDeArgos(valor)
	switch {
	case strings.TrimSpace(v) == "" || len([]rune(valor)) > r.max || len([]rune(v)) > r.max:
		return "De 1 a " + strconv.Itoa(r.max) + " caracteres"
	case strings.ContainsFunc(valor, separadorOFormato) || strings.ContainsFunc(v, separadorOFormato):
		return "Sin saltos de línea ni caracteres de control o de formato"
	case reEspacios4.MatchString(v):
		return "Sin 4 o más espacios seguidos"
	case strings.Contains(v, "@") || reFormaEnlace.MatchString(v) || (!r.barraEntreNumeros && strings.Contains(v, "/")) ||
		(r.barraEntreNumeros && reBarraConLetra.MatchString(v)):
		return "Sin enlaces, dominios, direcciones IP ni correos"
	case r.maxDigitos > 0 && digitosSinLetra(v) > r.maxDigitos:
		return "Sin números de " + strconv.Itoa(r.maxDigitos+1) + " o más dígitos (un teléfono o una cuenta)"
	}
	return ""
}

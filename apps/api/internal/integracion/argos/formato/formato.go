// Package formato normaliza para mostrar los datos de contacto que llegan de Argos (teléfonos,
// documento fiscal y coordenadas) sin perder el original. Son funciones puras: las usan las
// instantáneas de Argos, las copias de tickets y órdenes y el paquete de la app de campo, y el
// panel tiene su espejo en apps/web/lib/datos (mismos casos en testdata/telefonos.json).
//
// Argos guarda teléfono y móvil como texto libre (hasta 30 caracteres, sin validar el formato) y
// el documento fiscal sin guiones ni espacios. Iris no corrige lo que Argos tiene: guarda el
// original y calcula aparte cómo mostrarlo.
package formato

import (
	"math"
	"regexp"
	"strings"
)

// Telefono es un número tal como lo tiene Argos y cómo se muestra en Iris.
type Telefono struct {
	Tipo     string  `json:"tipo" enum:"telefono,movil" doc:"De qué campo de Argos viene: telefono o movil"`
	Original string  `json:"original" doc:"El texto tal como está en Argos (de un campo con varios números, solo esta parte)"`
	E164     *string `json:"e164" nullable:"true" doc:"El número en E.164 (+50255123344) para llamar o escribir; null si no se pudo interpretar"`
	Mostrar  string  `json:"mostrar" doc:"Cómo se muestra: +502 5512 3344 para Guatemala; el original (sin espacios de más) si es de otro país o no se pudo interpretar"`
}

// Solo espacios y tabuladores ASCII cuentan como espacio (y saltos de línea como separador): el
// espejo en TS usa las mismas clases, así un espacio Unicode (no separable, ancho fijo) da lo mismo
// en los dos lados (no se interpreta y se muestra tal cual).
//
// separadores de un campo con varios números: «5512-3344 / 2233-4455», «…, …», «… y …», líneas.
var separadores = regexp.MustCompile(`[ \t]*(?:[/,;|\r\n]|[ \t]+y[ \t]+|[ \t]+o[ \t]+)[ \t]*`)

var espacios = regexp.MustCompile(`[ \t]+`)

func recortar(s string) string { return strings.Trim(s, " \t") }

// Telefonos parte el texto de un campo de Argos en sus números (puede traer varios) y normaliza
// cada uno. Un campo vacío no da ninguno.
func Telefonos(tipo, texto string) []Telefono {
	salida := []Telefono{}
	for _, parte := range separadores.Split(recortar(texto), -1) {
		parte = espacios.ReplaceAllString(recortar(parte), " ")
		if parte == "" {
			continue
		}
		t := Telefono{Tipo: tipo, Original: parte, Mostrar: parte}
		if e164, mostrar, ok := NormalizarTelefono(parte); ok {
			t.E164, t.Mostrar = &e164, mostrar
		}
		salida = append(salida, t)
	}
	return salida
}

// NormalizarTelefono interpreta un número. Guatemala (+502): 8 dígitos que empiezan en 2–7, con
// o sin 502, +502 o 00502 delante y con cualquier separador; se muestra «+502 5512 3344». Otro
// país: solo si viene con + o 00 y tiene de 8 a 15 dígitos; se muestra como vino. Cualquier otra
// cosa (extensiones, números cortos, letras) no se interpreta: ok = false.
func NormalizarTelefono(texto string) (e164, mostrar string, ok bool) {
	t := recortar(texto)
	if t == "" || strings.ContainsFunc(t, func(r rune) bool {
		return (r < '0' || r > '9') && !strings.ContainsRune(" +-().", r)
	}) {
		return "", "", false
	}
	internacional := strings.HasPrefix(t, "+")
	digitos := soloDigitos(t)
	if !internacional && strings.HasPrefix(digitos, "00") {
		internacional, digitos = true, digitos[2:]
	}
	if strings.Count(t, "+") > 1 || (strings.Contains(t, "+") && !internacional) {
		return "", "", false
	}
	local := ""
	switch {
	case len(digitos) == 8 && !internacional:
		local = digitos
	case len(digitos) == 11 && strings.HasPrefix(digitos, "502"):
		local = digitos[3:]
	case internacional && !strings.HasPrefix(digitos, "502") && len(digitos) >= 8 && len(digitos) <= 15 && digitos[0] != '0':
		return "+" + digitos, espacios.ReplaceAllString(t, " "), true
	default:
		return "", "", false
	}
	if local[0] < '2' || local[0] > '7' {
		return "", "", false
	}
	return "+502" + local, "+502 " + local[:4] + " " + local[4:], true
}

func soloDigitos(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// Principal devuelve el número para contactar al cliente: el primer móvil que se pudo
// interpretar, si no el primer teléfono que se pudo interpretar y, si ninguno, el primero tal
// como vino. nil si no hay ninguno.
func Principal(telefonos []Telefono) *Telefono {
	for _, tipo := range []string{"movil", "telefono"} {
		for i := range telefonos {
			if telefonos[i].Tipo == tipo && telefonos[i].E164 != nil {
				return &telefonos[i]
			}
		}
	}
	if len(telefonos) > 0 {
		return &telefonos[0]
	}
	return nil
}

// Documento muestra el documento fiscal que Argos guarda sin guiones ni espacios: NIT con el
// dígito verificador separado (1234567-8, 123456-K), CUI en 4-5-4 (1234 56789 0101) y CF como
// «CF». El pasaporte, o un número que no tiene la forma esperada, se muestra tal cual.
func Documento(tipo, numero string) string {
	n := mayusculasASCII(recortar(numero))
	switch {
	case n == "":
		return ""
	case tipo == "cf" || n == "CF":
		return "CF"
	case tipo == "cui" && len(n) == 13 && soloDigitos(n) == n:
		return n[:4] + " " + n[4:9] + " " + n[9:]
	case tipo == "nit" && esNIT(n):
		return n[:len(n)-1] + "-" + n[len(n)-1:]
	}
	return n
}

func esNIT(n string) bool {
	if len(n) < 2 || len(n) > 13 || strings.Contains(n, "-") {
		return false
	}
	cuerpo, dv := n[:len(n)-1], n[len(n)-1]
	return soloDigitos(cuerpo) == cuerpo && ((dv >= '0' && dv <= '9') || dv == 'K')
}

// DecimalesCoordenadas: 6 decimales (≈ 11 cm), lo que da un GPS de teléfono y lo que se muestra.
const DecimalesCoordenadas = 6

// Coordenadas normaliza latitud y longitud: las dos o ninguna, dentro de rango, redondeadas a
// 6 decimales; el 0,0 (lo que deja un formulario sin llenar) cuenta como sin coordenadas.
func Coordenadas(lat, lon *float64) (*float64, *float64) {
	if lat == nil || lon == nil || math.IsNaN(*lat) || math.IsNaN(*lon) ||
		*lat < -90 || *lat > 90 || *lon < -180 || *lon > 180 || (*lat == 0 && *lon == 0) {
		return nil, nil
	}
	la, lo := redondear(*lat), redondear(*lon)
	return &la, &lo
}

func redondear(v float64) float64 {
	f := math.Pow(10, DecimalesCoordenadas)
	return math.Round(v*f) / f
}

// mayusculasASCII: solo a–z (strings.ToUpper y toUpperCase de JS difieren fuera de ASCII, p. ej. ß).
func mayusculasASCII(s string) string {
	return strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' {
			return r - 'a' + 'A'
		}
		return r
	}, s)
}

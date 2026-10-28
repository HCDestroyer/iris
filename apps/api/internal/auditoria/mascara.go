package auditoria

import (
	"encoding/json"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"unicode"
)

// Mascara reemplaza los valores sensibles en auditoria.acciones.entrada y auditoria.cambios.
const Mascara = "***"

// Un campo es sensible si su nombre (en snake_case) contiene una de estas palabras, o si el
// campo del struct lleva la etiqueta `auditoria:"sensible"`. Los ids (`token_id`) no se ocultan.
var patronSensible = regexp.MustCompile(`(^|_)(contrasena|contrasenas|password|passwd|secreto|secret|token|hash|totp|firma|csrf|credencial|credenciales|api_key)($|_|[0-9])`)

// EsCampoSensible indica si el nombre (camelCase o snake_case) es sensible por patrón.
func EsCampoSensible(nombre string) bool {
	n := SnakeCase(nombre)
	if strings.HasSuffix(n, "_id") || strings.HasSuffix(n, "_ids") || n == "id" {
		return false
	}
	return patronSensible.MatchString(n)
}

// SnakeCase convierte `contrasenaHash` en `contrasena_hash` (lo que ya es snake_case queda igual).
func SnakeCase(s string) string {
	var b strings.Builder
	runas := []rune(s)
	for i, r := range runas {
		if unicode.IsUpper(r) {
			if i > 0 && runas[i-1] != '_' && (unicode.IsLower(runas[i-1]) || unicode.IsDigit(runas[i-1]) ||
				(i+1 < len(runas) && unicode.IsLower(runas[i+1]))) {
				b.WriteByte('_')
			}
			b.WriteRune(unicode.ToLower(r))
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// Enmascarar devuelve v como JSON genérico con los campos sensibles en "***".
func Enmascarar(v any) any {
	plano := aJSON(v)
	return recorrer(plano, etiquetados(reflect.TypeOf(v)))
}

func aJSON(v any) any {
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	var plano any
	_ = json.Unmarshal(b, &plano)
	return plano
}

func recorrer(v any, marcados map[string]bool) any {
	switch x := v.(type) {
	case map[string]any:
		for k, interno := range x {
			if interno != nil && (marcados[k] || EsCampoSensible(k)) {
				x[k] = Mascara
				continue
			}
			x[k] = recorrer(interno, marcados)
		}
		return x
	case []any:
		for i := range x {
			x[i] = recorrer(x[i], marcados)
		}
		return x
	}
	return v
}

var cacheEtiquetas sync.Map // reflect.Type → map[string]bool

// etiquetados devuelve los nombres JSON de los campos con `auditoria:"sensible"` del tipo y de
// sus structs internos.
func etiquetados(t reflect.Type) map[string]bool {
	if t == nil {
		return nil
	}
	if m, ok := cacheEtiquetas.Load(t); ok {
		return m.(map[string]bool)
	}
	m := map[string]bool{}
	recolectar(t, m, map[reflect.Type]bool{})
	cacheEtiquetas.Store(t, m)
	return m
}

func recolectar(t reflect.Type, m map[string]bool, vistos map[reflect.Type]bool) {
	for t.Kind() == reflect.Pointer || t.Kind() == reflect.Slice || t.Kind() == reflect.Array || t.Kind() == reflect.Map {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct || vistos[t] {
		return
	}
	vistos[t] = true
	for i := range t.NumField() {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		nombre := f.Name
		if tag, ok := f.Tag.Lookup("json"); ok {
			if n := strings.Split(tag, ",")[0]; n != "" && n != "-" {
				nombre = n
			}
		}
		if f.Tag.Get("auditoria") == "sensible" {
			m[nombre] = true
		}
		recolectar(f.Type, m, vistos)
	}
}

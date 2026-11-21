package nucleo

import (
	"net/url"
	"reflect"
	"slices"
	"strings"
	"sync"
)

// Parámetros de consulta desconocidos.
//
// huma ignora en silencio los parámetros de consulta que la entrada no declara. En una lista eso
// es peligroso: un filtro mal escrito (`cliente_id` en vez de `clienteId`) no filtra y la
// respuesta trae todo lo que el alcance permite. ParametrosDesconocidos devuelve, ordenados, los
// nombres que llegaron y que la entrada no declara con la etiqueta `query:"…"` (también los de
// structs embebidos, como ParamsPagina), para que la ruta responda 422 en vez de ampliar el
// resultado.

var nombresConsulta sync.Map // reflect.Type → map[string]bool

// ParametrosDesconocidos: los nombres de q que el struct de entrada (o un puntero a él) no declara.
func ParametrosDesconocidos(entrada any, q url.Values) []string {
	t := reflect.TypeOf(entrada)
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	conocidos, ok := nombresConsulta.Load(t)
	if !ok {
		m := map[string]bool{}
		juntarNombresConsulta(t, m)
		conocidos, _ = nombresConsulta.LoadOrStore(t, m)
	}
	var fuera []string
	for nombre := range q {
		if !conocidos.(map[string]bool)[nombre] {
			fuera = append(fuera, nombre)
		}
	}
	slices.Sort(fuera)
	return fuera
}

// ErroresParametrosDesconocidos arma un ErrorCampo por nombre (regla `desconocido`).
func ErroresParametrosDesconocidos(nombres []string) []ErrorCampo {
	errs := make([]ErrorCampo, 0, len(nombres))
	for _, n := range nombres {
		errs = append(errs, ErrorCampo{Field: n, Message: "Parámetro de consulta desconocido en esta ruta; revisa el nombre", Rule: "desconocido"})
	}
	return errs
}

func juntarNombresConsulta(t reflect.Type, m map[string]bool) {
	if t.Kind() != reflect.Struct {
		return
	}
	for i := range t.NumField() {
		f := t.Field(i)
		if nombre, ok := f.Tag.Lookup("query"); ok {
			nombre, _, _ = strings.Cut(nombre, ",")
			if nombre != "" && nombre != "-" {
				m[nombre] = true
			}
			continue
		}
		if f.Anonymous {
			ft := f.Type
			if ft.Kind() == reflect.Pointer {
				ft = ft.Elem()
			}
			juntarNombresConsulta(ft, m)
		}
	}
}

package nucleo

import (
	"context"
	"net/http"
	"net/url"
	"reflect"
	"slices"
	"strings"
	"sync"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
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

// UnirAlias resuelve un filtro que llega con su nombre (campo) o con su alias en snake_case
// (campoAlias, el que usa Argos): devuelve el valor que vale (0 = sin filtro) o un ErrorCampo si
// llegaron los dos con valores distintos.
func UnirAlias(valor, alias int64, campo, campoAlias string) (int64, *ErrorCampo) {
	switch {
	case alias == 0:
		return valor, nil
	case valor == 0 || valor == alias:
		return alias, nil
	}
	return 0, &ErrorCampo{Field: campoAlias, Rule: "distinto",
		Message: campoAlias + " es un alias de " + campo + " y llegaron con valores distintos; manda solo uno"}
}

// ExigirFiltroCliente: un token (de integración, como el de Argos, o de API de una persona) que
// lista datos de clientes sin filtro de cliente recibe 422 filtro_cliente_requerido, salvo que
// tenga el permiso explícito de listar todo. Las personas con sesión (panel, app de campo con
// cookie) no cambian. recurso es el nombre para el mensaje («tickets», «órdenes»).
func ExigirFiltroCliente(ctx context.Context, clienteID int64, listarTodos Permiso, recurso string) error {
	switch contexto.ActorDe(ctx).Tipo {
	case contexto.ActorIntegracion, contexto.ActorToken:
	default:
		return nil
	}
	if clienteID > 0 || contexto.PermisosDe(ctx).Tiene(string(listarTodos)) {
		return nil
	}
	return NuevoProblema(http.StatusUnprocessableEntity, "filtro_cliente_requerido", "Falta el filtro de cliente",
		"Con un token, la lista de "+recurso+" exige clienteId (o su alias cliente_id); para listar todo, el token necesita "+string(listarTodos),
		ErrorCampo{Field: "clienteId", Rule: "requerido", Message: "Indica el cliente de Argos (clienteId o cliente_id)"})
}

// NormalizarFiltrosCliente: lo común de las listas que lee Argos (tickets, órdenes). Rechaza los
// parámetros desconocidos y une los alias cliente_id y servicio_id en clienteID y servicioID
// (422 validacion si alguno llega con los dos nombres y valores distintos).
func NormalizarFiltrosCliente(desconocidos []string, clienteID *int64, clienteAlias int64, servicioID *int64, servicioAlias int64) error {
	if len(desconocidos) > 0 {
		return Validacion(ErroresParametrosDesconocidos(desconocidos)...)
	}
	var errs []ErrorCampo
	var e *ErrorCampo
	if *clienteID, e = UnirAlias(*clienteID, clienteAlias, "clienteId", "cliente_id"); e != nil {
		errs = append(errs, *e)
	}
	if *servicioID, e = UnirAlias(*servicioID, servicioAlias, "servicioId", "servicio_id"); e != nil {
		errs = append(errs, *e)
	}
	if len(errs) > 0 {
		return Validacion(errs...)
	}
	return nil
}

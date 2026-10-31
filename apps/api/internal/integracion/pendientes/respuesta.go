package pendientes

import (
	"bytes"
	"encoding/json"
	"strconv"
)

// Respuesta de Argos guardada en el pendiente (la del primer 2xx): la usan los dependientes
// (cuerpos con marcadores, plantilla.go) y quien encoló (p. ej. comercial toma el id del
// cliente). Se guarda solo una LISTA BLANCA de campos escalares (CamposRespuesta), en la raíz o
// dentro de { data: {...} }; todo lo demás (credenciales PPP, claves WiFi, tokens, datos
// personales, listas y objetos anidados) se descarta. Si hace falta otro campo, se agrega aquí.

// CamposRespuesta: los únicos campos que se guardan de una respuesta (y sus formas camelCase).
var CamposRespuesta = []string{"id", "codigo", "codigo_externo", "cliente_id", "servicio_id", "estado", "empresa_id", "factura_id"}

// Listas que también se guardan (contrato 0.5.0 §3.3): los cargos que Argos emitió al consumir
// productos consumible_cobro, cada uno solo con sus campos escalares permitidos.
var listasRespuesta = map[string][]string{"cargos": {"id", "producto_id", "monto"}}

var camposPermitidos = func() map[string]bool {
	m := map[string]bool{}
	for _, c := range CamposRespuesta {
		m[c] = true
		m[camel(c)] = true
	}
	return m
}()

func camel(s string) string {
	b := []byte{}
	mayus := false
	for i := 0; i < len(s); i++ {
		switch {
		case s[i] == '_':
			mayus = true
		case mayus:
			b = append(b, s[i]-'a'+'A')
			mayus = false
		default:
			b = append(b, s[i])
		}
	}
	return string(b)
}

// MaxRespuesta: el cuerpo de más de esto no se lee (queda null; un marcador que lo pida falla).
const MaxRespuesta = 64 << 10

// limpiarRespuesta devuelve los campos permitidos de la respuesta (en la raíz y en data), o nil
// si no es un objeto JSON, pasa de MaxRespuesta o no trae ninguno.
func limpiarRespuesta(crudo []byte) []byte {
	crudo = bytes.TrimSpace(crudo)
	if len(crudo) == 0 || len(crudo) > MaxRespuesta {
		return nil
	}
	var raiz map[string]json.RawMessage
	if json.Unmarshal(crudo, &raiz) != nil {
		return nil
	}
	salida := escalaresPermitidos(raiz)
	if d, ok := raiz["data"]; ok {
		var data map[string]json.RawMessage
		if json.Unmarshal(d, &data) == nil {
			if dentro := escalaresPermitidos(data); len(dentro) > 0 {
				salida["data"] = dentro
			}
		}
	}
	if len(salida) == 0 {
		return nil
	}
	b, err := json.Marshal(salida)
	if err != nil {
		return nil
	}
	return b
}

func escalaresPermitidos(obj map[string]json.RawMessage) map[string]any {
	salida := map[string]any{}
	for k, v := range obj {
		if campos, ok := listasRespuesta[k]; ok {
			if l := listaPermitida(v, campos); l != nil {
				salida[k] = l
			}
			continue
		}
		if !camposPermitidos[k] {
			continue
		}
		v = bytes.TrimSpace(v)
		if len(v) == 0 || v[0] == '{' || v[0] == '[' {
			continue
		}
		salida[k] = v
	}
	return salida
}

// CampoDeRespuesta lee un campo (ruta con puntos, p. ej. "id" o "servicio.id") de una respuesta
// guardada. Si la respuesta viene envuelta en { data: {...} } (forma de Argos), se busca dentro.
// Solo devuelve valores escalares (número, texto, booleano).
func CampoDeRespuesta(respuesta []byte, campo string) (json.RawMessage, bool) {
	if len(respuesta) == 0 || campo == "" {
		return nil, false
	}
	var raiz map[string]json.RawMessage
	if json.Unmarshal(respuesta, &raiz) != nil {
		return nil, false
	}
	if data, ok := raiz["data"]; ok && bytes.HasPrefix(bytes.TrimSpace(data), []byte("{")) {
		if _, directo := raiz[primerTramo(campo)]; !directo {
			_ = json.Unmarshal(data, &raiz)
		}
	}
	actual := raiz
	tramos := partir(campo)
	for i, t := range tramos {
		v, ok := actual[t]
		if !ok {
			return nil, false
		}
		if i == len(tramos)-1 {
			v = bytes.TrimSpace(v)
			if len(v) == 0 || v[0] == '{' || v[0] == '[' || string(v) == "null" {
				return nil, false
			}
			return v, true
		}
		var sig map[string]json.RawMessage
		if json.Unmarshal(v, &sig) != nil {
			return nil, false
		}
		actual = sig
	}
	return nil, false
}

// IDDeRespuesta: el campo "id" de una respuesta guardada como entero positivo (número o texto).
func IDDeRespuesta(respuesta []byte) (int64, bool) {
	v, ok := CampoDeRespuesta(respuesta, "id")
	if !ok {
		return 0, false
	}
	s := string(v)
	if u, err := strconv.Unquote(s); err == nil {
		s = u
	}
	id, err := strconv.ParseInt(s, 10, 64)
	return id, err == nil && id > 0
}

func primerTramo(campo string) string { return partir(campo)[0] }

func partir(campo string) []string {
	var tramos []string
	inicio := 0
	for i := 0; i < len(campo); i++ {
		if campo[i] == '.' {
			tramos = append(tramos, campo[inicio:i])
			inicio = i + 1
		}
	}
	return append(tramos, campo[inicio:])
}

// listaPermitida: una lista de objetos, cada uno con solo esos campos escalares (máx. 200).
func listaPermitida(v json.RawMessage, campos []string) []map[string]json.RawMessage {
	var lista []map[string]json.RawMessage
	if json.Unmarshal(v, &lista) != nil {
		return nil
	}
	salida := make([]map[string]json.RawMessage, 0, len(lista))
	for i, o := range lista {
		if i >= 200 {
			break
		}
		limpio := map[string]json.RawMessage{}
		for _, c := range campos {
			for _, k := range []string{c, camel(c)} {
				x := bytes.TrimSpace(o[k])
				if len(x) > 0 && x[0] != '{' && x[0] != '[' {
					limpio[c] = x
				}
			}
		}
		if len(limpio) > 0 {
			salida = append(salida, limpio)
		}
	}
	return salida
}

// CargoArgos es un cargo único que Argos emitió al consumir un producto consumible_cobro.
type CargoArgos struct {
	ID         int64  `json:"id"`
	ProductoID int64  `json:"productoId"`
	Monto      string `json:"monto" doc:"Monto en texto decimal (como lo manda Argos)"`
}

// cargosDeRespuesta lee cargos[] y factura_id de la respuesta guardada de un consumo.
func cargosDeRespuesta(respuesta []byte) ([]CargoArgos, *int64) {
	if len(respuesta) == 0 {
		return nil, nil
	}
	var r struct {
		Cargos    []map[string]json.RawMessage `json:"cargos"`
		FacturaID json.RawMessage              `json:"factura_id"`
	}
	if json.Unmarshal(respuesta, &r) != nil {
		return nil, nil
	}
	entero := func(v json.RawMessage) int64 {
		s := string(bytes.TrimSpace(v))
		if u, err := strconv.Unquote(s); err == nil {
			s = u
		}
		n, _ := strconv.ParseInt(s, 10, 64)
		return n
	}
	var cargos []CargoArgos
	for _, c := range r.Cargos {
		monto := string(bytes.TrimSpace(c["monto"]))
		if u, err := strconv.Unquote(monto); err == nil {
			monto = u
		}
		cargos = append(cargos, CargoArgos{ID: entero(c["id"]), ProductoID: entero(c["producto_id"]), Monto: monto})
	}
	var factura *int64
	if n := entero(r.FacturaID); n > 0 {
		factura = &n
	}
	return cargos, factura
}

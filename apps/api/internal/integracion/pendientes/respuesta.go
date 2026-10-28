package pendientes

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strconv"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
)

// Respuesta de Argos guardada en el pendiente (la del primer 2xx): la usan los dependientes
// (cuerpos con marcadores, plantilla.go) y quien encoló (p. ej. comercial toma el id del
// cliente). Se guarda como mucho MaxRespuesta bytes y con las claves sensibles en "***"
// (contraseñas, tokens, secretos y la clave PPP, aunque Argos la devuelva como «definida»).

// MaxRespuesta: más grande, no se guarda (queda null; un marcador que la pida falla).
const MaxRespuesta = 64 << 10

// Lo de auditoria.EsCampoSensible más las claves de acceso de Argos (clave_ppp, password_ppp,
// pass, pwd).
var claveSensible = regexp.MustCompile(`(?i)(^|_)(clave|pass|pwd)($|_)`)

func esClaveSensible(k string) bool {
	return auditoria.EsCampoSensible(k) || claveSensible.MatchString(auditoria.SnakeCase(k))
}

// limpiarRespuesta devuelve el JSON de la respuesta con lo sensible en "***", o nil si no es
// JSON (o está vacía) o pasa de MaxRespuesta.
func limpiarRespuesta(crudo []byte) []byte {
	crudo = bytes.TrimSpace(crudo)
	if len(crudo) == 0 {
		return nil
	}
	d := json.NewDecoder(bytes.NewReader(crudo))
	d.UseNumber()
	var v any
	if d.Decode(&v) != nil {
		return nil
	}
	b, err := json.Marshal(enmascarar(v))
	if err != nil || len(b) > MaxRespuesta {
		return nil
	}
	return b
}

func enmascarar(v any) any {
	switch x := v.(type) {
	case map[string]any:
		for k, h := range x {
			if esClaveSensible(k) {
				x[k] = auditoria.Mascara
			} else {
				x[k] = enmascarar(h)
			}
		}
	case []any:
		for i := range x {
			x[i] = enmascarar(x[i])
		}
	}
	return v
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
			if len(v) == 0 || v[0] == '{' || v[0] == '[' || string(v) == "null" || string(v) == `"`+auditoria.Mascara+`"` {
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

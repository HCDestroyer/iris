package pendientes

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// Cuerpos de dependientes armados con la respuesta de su dependencia (p. ej. POST /servicios
// toma cliente_id de la respuesta de POST /clientes). Quien encola pone en el cuerpo un
// marcador explícito en lugar del valor:
//
//	{"cliente_id": {"$iris_ref": {"dependencia": 0, "campo": "id"}}, "plan_id": 300}
//
// (pendientes.Ref(0, "id")), donde dependencia es el índice en DependeDe y campo la ruta en la
// respuesta guardada de esa dependencia (CampoDeRespuesta: dentro de { data } si viene así).
// Se resuelve UNA sola vez, cuando todas las dependencias quedan hechas (o al encolar, si ya lo
// están): el cuerpo final se guarda en payload y ya no cambia, así que todos los intentos mandan
// los mismos bytes (Argos guarda la huella del cuerpo crudo: contrato §3.1). La plantilla queda
// en su columna. Si el campo no está en la respuesta, el dependiente queda fallido_negocio con
// datos_incompletos («respuesta_sin_<campo>»): nunca se adivina.

const claveRef = "$iris_ref"

type ref struct {
	Dependencia int    `json:"dependencia"`
	Campo       string `json:"campo"`
}

// Ref es el marcador del valor campo de la respuesta de la dependencia número i (índice en
// DependeDe).
func Ref(dependencia int, campo string) any {
	return map[string]any{claveRef: ref{Dependencia: dependencia, Campo: campo}}
}

// tieneMarcadores indica si el cuerpo lleva algún marcador.
func tieneMarcadores(cuerpo []byte) bool {
	return bytes.Contains(cuerpo, []byte(`"`+claveRef+`"`))
}

// errResolucion: el marcador no se puede resolver con la respuesta (dato ausente). Es de
// negocio: reintentar no lo arregla.
type errResolucion struct{ motivo string }

func (e errResolucion) Error() string { return e.motivo }

// resolverPlantilla reemplaza los marcadores con los valores de las respuestas (en el orden de
// DependeDe) y devuelve el cuerpo final.
func resolverPlantilla(plantilla []byte, respuestas [][]byte) ([]byte, error) {
	d := json.NewDecoder(bytes.NewReader(plantilla))
	d.UseNumber()
	var v any
	if err := d.Decode(&v); err != nil {
		return nil, err
	}
	r, err := sustituir(v, respuestas)
	if err != nil {
		return nil, err
	}
	return json.Marshal(r)
}

func sustituir(v any, respuestas [][]byte) (any, error) {
	switch x := v.(type) {
	case map[string]any:
		if m, ok := x[claveRef]; ok {
			if len(x) != 1 {
				return nil, fmt.Errorf("pendientes: marcador %s con claves de más", claveRef)
			}
			b, _ := json.Marshal(m)
			var r ref
			if err := json.Unmarshal(b, &r); err != nil || r.Campo == "" {
				return nil, fmt.Errorf("pendientes: marcador %s inválido", claveRef)
			}
			if r.Dependencia < 0 || r.Dependencia >= len(respuestas) {
				return nil, fmt.Errorf("pendientes: el marcador pide la dependencia %d y hay %d", r.Dependencia, len(respuestas))
			}
			valor, ok := CampoDeRespuesta(respuestas[r.Dependencia], r.Campo)
			if !ok {
				return nil, errResolucion{motivo: "respuesta_sin_" + r.Campo}
			}
			return valor, nil
		}
		for k, h := range x {
			s, err := sustituir(h, respuestas)
			if err != nil {
				return nil, err
			}
			x[k] = s
		}
	case []any:
		for i := range x {
			s, err := sustituir(x[i], respuestas)
			if err != nil {
				return nil, err
			}
			x[i] = s
		}
	}
	return v, nil
}

// validarPlantilla comprueba que cada marcador esté bien armado y apunte a una dependencia que
// existe (n = len(DependeDe)). Error de programación de quien encola: se detecta al encolar.
func validarPlantilla(cuerpo []byte, n int) error {
	d := json.NewDecoder(bytes.NewReader(cuerpo))
	d.UseNumber()
	var v any
	if err := d.Decode(&v); err != nil {
		return err
	}
	var recorrer func(any) error
	recorrer = func(v any) error {
		switch x := v.(type) {
		case map[string]any:
			if m, ok := x[claveRef]; ok {
				b, _ := json.Marshal(m)
				var r ref
				if len(x) != 1 || json.Unmarshal(b, &r) != nil || r.Campo == "" || r.Dependencia < 0 || r.Dependencia >= n {
					return fmt.Errorf("pendientes: marcador %s inválido o fuera de DependeDe (%d dependencias): %s", claveRef, n, b)
				}
				return nil
			}
			for _, h := range x {
				if err := recorrer(h); err != nil {
					return err
				}
			}
		case []any:
			for _, h := range x {
				if err := recorrer(h); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return recorrer(v)
}

package nucleo

import (
	"strings"
	"sync"
)

// Rutas públicas que llevan un secreto en un segmento (el token de una encuesta, un ticket de
// canje…). Lo que escribe la ruta en logs, trazas o métricas pasa por EnmascararRuta, que
// cambia esos segmentos por «***»: quien lee los logs no puede usar el enlace.
//
// Una plantilla es una ruta con los segmentos secretos entre llaves ({token}); los demás
// segmentos son literales. Se aplica como prefijo: "/api/v1/encuestas/publica/{token}" cubre
// también ".../{token}/responder" y cualquier subruta futura.

// MascaraSecreto reemplaza un segmento secreto.
const MascaraSecreto = "***"

var rutasSecretas = struct {
	sync.RWMutex
	plantillas [][]string
}{}

// RutasConSecretoBase: las que núcleo conoce. Un módulo que publique otra ruta con un secreto
// la agrega con RegistrarRutaConSecreto al armarse.
var RutasConSecretoBase = []string{
	// Encuesta pública de satisfacción (I2, PR #41): el token es la credencial del enlace.
	"/api/v1/encuestas/publica/{token}",
}

func init() {
	for _, p := range RutasConSecretoBase {
		RegistrarRutaConSecreto(p)
	}
}

// RegistrarRutaConSecreto agrega una plantilla (segmentos secretos entre llaves). Idempotente.
func RegistrarRutaConSecreto(plantilla string) {
	segs := segmentos(plantilla)
	rutasSecretas.Lock()
	defer rutasSecretas.Unlock()
	for _, p := range rutasSecretas.plantillas {
		if strings.Join(p, "/") == strings.Join(segs, "/") {
			return
		}
	}
	rutasSecretas.plantillas = append(rutasSecretas.plantillas, segs)
}

// EnmascararRuta devuelve la ruta con los segmentos secretos de las plantillas registradas
// cambiados por «***». Los literales se comparan sin mayúsculas y sin segmentos vacíos (//),
// para que una variante que el router no acepte tampoco deje el secreto en el log. Si la ruta
// trae query (?…), se descarta: ninguna ruta debería llevar secretos ahí y el log no la necesita.
func EnmascararRuta(ruta string) string {
	if i := strings.IndexAny(ruta, "?#"); i >= 0 {
		ruta = ruta[:i]
	}
	partes := strings.Split(ruta, "/")
	// Índices de los segmentos no vacíos.
	llenos := make([]int, 0, len(partes))
	for i, p := range partes {
		if p != "" {
			llenos = append(llenos, i)
		}
	}
	rutasSecretas.RLock()
	defer rutasSecretas.RUnlock()
	cambio := false
	for _, plantilla := range rutasSecretas.plantillas {
		if len(plantilla) > len(llenos) {
			continue
		}
		coincide := true
		for j, seg := range plantilla {
			if !esSecreto(seg) && !strings.EqualFold(seg, partes[llenos[j]]) {
				coincide = false
				break
			}
		}
		if !coincide {
			continue
		}
		for j, seg := range plantilla {
			if esSecreto(seg) {
				partes[llenos[j]] = MascaraSecreto
				cambio = true
			}
		}
	}
	if !cambio {
		return ruta
	}
	return strings.Join(partes, "/")
}

func esSecreto(seg string) bool {
	return len(seg) > 2 && seg[0] == '{' && seg[len(seg)-1] == '}'
}

func segmentos(ruta string) []string {
	var res []string
	for _, p := range strings.Split(ruta, "/") {
		if p != "" {
			res = append(res, p)
		}
	}
	return res
}

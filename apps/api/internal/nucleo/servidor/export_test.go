package servidor

import "time"

// FijarPlazosSSE cambia los plazos del flujo SSE en el proxy (pruebas); devuelve cómo restaurarlos.
func FijarPlazosSSE(escritura, maximo time.Duration) func() {
	e, m := plazoEscrituraSSE, maximoFlujoSSE
	plazoEscrituraSSE, maximoFlujoSSE = escritura, maximo
	return func() { plazoEscrituraSSE, maximoFlujoSSE = e, m }
}

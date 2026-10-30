package argos

import (
	"strconv"
	"testing"
	"time"
)

// Cuotas separadas: «Refrescar» y la verificación de altas no se gastan entre sí, la de una empresa
// no bloquea a otra (hasta el tope del conector) y la del usuario cuenta todas sus lecturas.
func TestCuotasSeparadas(t *testing.T) {
	r := nuevosRefrescos()
	ahora := time.Now()
	for i := range CuotaRefrescarEmpresa {
		if !r.tomarCuota(1, cobro{uso: usoRefrescar, usuario: int64(100 + i), empresa: 1}, ahora) {
			t.Fatalf("refresco %d de la empresa 1", i)
		}
	}
	if r.tomarCuota(1, cobro{uso: usoRefrescar, usuario: 999, empresa: 1}, ahora) {
		t.Fatal("agotada la cuota de «Refrescar» de la empresa 1")
	}
	if !r.tomarCuota(1, cobro{uso: usoRefrescar, usuario: 999, empresa: 2}, ahora) {
		t.Fatal("la empresa 2 tiene su propia cuota de «Refrescar»")
	}
	if !r.tomarCuota(1, cobro{uso: usoVerificar, usuario: 999, empresa: 1}, ahora) {
		t.Fatal("la verificación de altas no depende de la cuota de «Refrescar»")
	}
	// Tope del conector: entre todas las empresas.
	r = nuevosRefrescos()
	n := 0
	for emp := int64(1); n < CuotaRefrescarConector; emp++ {
		for range CuotaRefrescarEmpresa {
			if n == CuotaRefrescarConector {
				break
			}
			if !r.tomarCuota(1, cobro{uso: usoRefrescar, empresa: emp}, ahora) {
				t.Fatalf("refresco %d", n)
			}
			n++
		}
	}
	if r.tomarCuota(1, cobro{uso: usoRefrescar, empresa: 99}, ahora) {
		t.Fatal("agotado el tope del conector")
	}
	// Una cuota agotada no descuenta las demás.
	r = nuevosRefrescos()
	for range CuotaUsuario {
		r.tomarCuota(1, cobro{usuario: 7}, ahora)
	}
	if r.tomarCuota(1, cobro{uso: usoVerificar, usuario: 7, empresa: 1}, ahora) || r.cuotas["v:1"].n != 0 {
		t.Fatal("pasada la del usuario no se cobra la del conector")
	}
	// Pasado el minuto, se renueva.
	if !r.tomarCuota(1, cobro{usuario: 7}, ahora.Add(time.Minute)) {
		t.Fatal("la ventana se renueva al minuto")
	}
}

// Con muchas entradas en memoria se van las ventanas vencidas, no las vigentes.
func TestCuotasDesalojoDeVencidas(t *testing.T) {
	r := nuevosRefrescos()
	viejo := time.Now().Add(-2 * time.Minute)
	for i := range maxEntradasEnMemoria + 1 {
		r.cuotas["u:"+itoa(int64(1_000_000+i))] = ventanaCuota{inicio: viejo, n: 1}
	}
	ahora := time.Now()
	for range CuotaUsuario {
		r.tomarCuota(1, cobro{usuario: 5}, ahora)
	}
	if r.tomarCuota(1, cobro{usuario: 5}, ahora) {
		t.Fatal("la cuota vigente del usuario 5 no debía borrarse")
	}
	if len(r.cuotas) > 10 {
		t.Fatalf("las ventanas vencidas debían desalojarse: %d", len(r.cuotas))
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

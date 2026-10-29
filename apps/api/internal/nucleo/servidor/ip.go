package servidor

import (
	"net"
	"net/http"
	"net/netip"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
)

// middlewareIPReal reescribe RemoteAddr con la IP del cliente (nucleo.IPCliente), así la ven igual
// el log, las sesiones (nucleo.sesiones.ip, portal_sesiones.ip), la encuesta pública y los casos
// de uso, que la leen con nucleo.HostDe. Es el primero de la cadena. Sin redes confiables no
// cambia nada.
func middlewareIPReal(confiables []netip.Prefix) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if len(confiables) == 0 {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := nucleo.IPCliente(r.RemoteAddr, r.Header.Values("X-Forwarded-For"), confiables)
			_, puerto, err := net.SplitHostPort(r.RemoteAddr)
			if err != nil {
				puerto = "0"
			}
			r2 := r.Clone(r.Context())
			r2.RemoteAddr = net.JoinHostPort(ip, puerto)
			next.ServeHTTP(w, r2)
		})
	}
}

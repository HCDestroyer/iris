package servidor

import (
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// IP del cliente detrás de proxies (pendiente de la PR #4): el panel hace de proxy de /api, así
// que RemoteAddr es siempre la IP del panel. Solo si la conexión viene de una red confiable
// (IRIS_PROXIES_CONFIABLES) se lee X-Forwarded-For; si no, la cabecera se ignora (cualquiera
// puede mandarla).

// IPCliente devuelve la IP del cliente. Si remoteAddr no es de un proxy confiable, es esa. Si lo
// es, recorre X-Forwarded-For de derecha a izquierda (cada proxy agrega a la derecha la IP de
// quien le habló) y devuelve la primera que no es de un proxy confiable: lo que está más a la
// izquierda lo escribió el cliente y no se cree. Si no aparece ninguna no confiable (todas lo
// son, una entrada mal formada corta el recorrido o no hay XFF), devuelve la IP del socket:
// nunca una IP de la red confiable que pudo escribir el propio cliente.
func IPCliente(remoteAddr string, xff []string, confiables []netip.Prefix) string {
	host := remoteAddr
	if h, _, err := net.SplitHostPort(remoteAddr); err == nil {
		host = h
	}
	ip, err := netip.ParseAddr(host)
	if err != nil || !esConfiable(ip, confiables) {
		return host
	}
	var saltos []string
	for _, h := range xff {
		for _, p := range strings.Split(h, ",") {
			if p = strings.TrimSpace(p); p != "" {
				saltos = append(saltos, p)
			}
		}
	}
	for i := len(saltos) - 1; i >= 0; i-- {
		siguiente, err := parsearSalto(saltos[i])
		if err != nil {
			break
		}
		if !esConfiable(siguiente, confiables) {
			return siguiente.String()
		}
	}
	return ip.Unmap().String()
}

// parsearSalto acepta "1.2.3.4", "1.2.3.4:5678", "2001:db8::1" y "[2001:db8::1]:443".
func parsearSalto(s string) (netip.Addr, error) {
	if ap, err := netip.ParseAddrPort(s); err == nil {
		return ap.Addr().Unmap(), nil
	}
	ip, err := netip.ParseAddr(strings.Trim(s, "[]"))
	return ip.Unmap(), err
}

func esConfiable(ip netip.Addr, confiables []netip.Prefix) bool {
	ip = ip.Unmap()
	for _, red := range confiables {
		if red.Contains(ip) {
			return true
		}
	}
	return false
}

// middlewareIPReal reescribe RemoteAddr con la IP del cliente (IPCliente), así la ven igual el
// log, las sesiones (nucleo.sesiones.ip) y los casos de uso. Sin redes confiables no cambia nada.
func middlewareIPReal(confiables []netip.Prefix) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if len(confiables) == 0 {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := IPCliente(r.RemoteAddr, r.Header.Values("X-Forwarded-For"), confiables)
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

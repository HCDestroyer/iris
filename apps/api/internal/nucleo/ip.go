package nucleo

import (
	"net"
	"net/netip"
	"strings"
)

// IP del cliente detrás de proxies. El panel (Next) hace de proxy de /api, así que la conexión
// llega siempre desde su contenedor. Solo si viene de una red confiable (IRIS_PROXIES_CONFIABLES)
// se lee X-Forwarded-For; si no, la cabecera se ignora, porque cualquiera puede mandarla.
//
// El servidor (servidor.middlewareIPReal, el primero de la cadena) reescribe RemoteAddr con
// IPCliente una sola vez por petición. Después, todo el que necesite la IP (log de acceso,
// sesiones del panel y del portal, encuesta pública, auditoría) la lee con HostDe(RemoteAddr) y
// nunca vuelve a mirar X-Forwarded-For. Se descartó el middleware RealIP de chi porque confía en
// cualquiera.

// IPCliente devuelve la IP del cliente. Si remoteAddr no es de un proxy confiable, es esa. Si lo
// es, recorre X-Forwarded-For de derecha a izquierda (cada proxy agrega a la derecha la IP de
// quien le habló) y devuelve la primera que no es de un proxy confiable: lo que está más a la
// izquierda lo escribió el cliente y no se cree. Si no aparece ninguna no confiable (todas lo
// son, una entrada mal formada corta el recorrido o no hay XFF), devuelve la IP del socket:
// nunca una IP de la red confiable que pudo escribir el propio cliente.
func IPCliente(remoteAddr string, xff []string, confiables []netip.Prefix) string {
	host := HostDe(remoteAddr)
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
	return ip.String()
}

// HostDe devuelve la IP de un RemoteAddr ("1.2.3.4:5678", "[2001:db8::1]:443" o sin puerto),
// sin el puerto y con una IPv4 mapeada en IPv6 normalizada a IPv4. Si no es una IP, lo devuelve
// sin el puerto.
func HostDe(remoteAddr string) string {
	host := remoteAddr
	if h, _, err := net.SplitHostPort(remoteAddr); err == nil {
		host = h
	}
	if ip, err := netip.ParseAddr(strings.Trim(host, "[]")); err == nil {
		return ip.Unmap().String()
	}
	return host
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

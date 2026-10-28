package conector

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"syscall"
	"time"
)

// Protección contra SSRF. La URL del otro sistema (Argos, Horus) la escribe un administrador
// (enlace de conexión) o la manda quien canjea un código; con ella Iris hace peticiones desde su
// red. Toda conexión hacia un conector (canje, lecturas, jobs en el worker) pasa por un
// net.Dialer cuyo Control mira la IP a la que de verdad se conecta, después de resolver el DNS
// (un nombre que resuelve a una IP interna, o que cambia de IP entre la validación y la
// conexión, también se corta). Se bloquean loopback, redes privadas, enlace local
// (169.254.0.0/16, fe80::/10; incluye metadatos de la nube), CGNAT (100.64.0.0/10), no
// especificadas, multicast y otras reservadas, salvo las redes permitidas de cada sistema
// (IRIS_ARGOS_REDES_PERMITIDAS, IRIS_HORUS_REDES_PERMITIDAS: desarrollo y laboratorio, donde el
// otro sistema vive en la red local).

// ErrDestinoBloqueado: la URL del conector resuelve a una red interna no permitida.
var ErrDestinoBloqueado = errors.New("destino no permitido: red interna")

// redesBloqueadas son las que no tienen ya un método en netip.Addr.
var redesBloqueadas = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),       // «esta red»
	netip.MustParsePrefix("100.64.0.0/10"),   // CGNAT
	netip.MustParsePrefix("192.0.0.0/24"),    // asignaciones IETF
	netip.MustParsePrefix("192.0.2.0/24"),    // documentación
	netip.MustParsePrefix("198.18.0.0/15"),   // pruebas de rendimiento
	netip.MustParsePrefix("198.51.100.0/24"), // documentación
	netip.MustParsePrefix("203.0.113.0/24"),  // documentación
	netip.MustParsePrefix("240.0.0.0/4"),     // reservada y broadcast
	netip.MustParsePrefix("64:ff9b::/96"),    // NAT64: lleva una IPv4 adentro
	netip.MustParsePrefix("64:ff9b:1::/48"),  // NAT64 local
	netip.MustParsePrefix("2001:db8::/32"),   // documentación
	netip.MustParsePrefix("2002::/16"),       // 6to4: lleva una IPv4 adentro
	netip.MustParsePrefix("100::/64"),        // descarte
}

// DestinoPermitido indica si Iris puede conectarse a ip (permitidas tiene prioridad).
func DestinoPermitido(ip netip.Addr, permitidas []netip.Prefix) bool {
	ip = ip.Unmap()
	for _, p := range permitidas {
		if p.Contains(ip) {
			return true
		}
	}
	if !ip.IsValid() || ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() {
		return false
	}
	for _, p := range redesBloqueadas {
		if p.Contains(ip) {
			return false
		}
	}
	return true
}

// controlDestino es el Control del net.Dialer: corre con la IP ya resuelta, antes de conectar.
func controlDestino(permitidas []netip.Prefix) func(network, address string, _ syscall.RawConn) error {
	return func(_, address string, _ syscall.RawConn) error {
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return ErrDestinoBloqueado
		}
		ip, err := netip.ParseAddr(host)
		if err != nil || !DestinoPermitido(ip, permitidas) {
			return fmt.Errorf("%w (%s)", ErrDestinoBloqueado, host)
		}
		return nil
	}
}

// NuevoHTTP arma el cliente HTTP hacia un conector: dial con la protección de arriba, sin proxy
// del entorno (un HTTP_PROXY haría que el dial vaya al proxy y la IP final no se revise) y sin
// seguir redirecciones (el token no debe viajar a otro host).
func NuevoHTTP(permitidas []netip.Prefix) *http.Client {
	d := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second, Control: controlDestino(permitidas)}
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.Proxy = nil
	t.DialContext = d.DialContext
	return &http.Client{
		Transport:     t,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

package voz

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"time"
)

// ProxyDescarga es la única salida a red del paquete: un proxy CONNECT que solo deja pasar hacia
// los hosts del registro de modelos, puerto 443. Lo usa el contenedor de un solo uso de
// `iris-voz modelo descargar`, que no tiene otra ruta (voz.md §4.4 y §6.3).
type ProxyDescarga struct {
	// Hosts permitidos: un nombre exacto, o un sufijo si empieza por punto (".ejemplo.com").
	Hosts []string
}

// Permitido dice si el proxy deja conectar con ese host.
func (p ProxyDescarga) Permitido(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	if _, err := netip.ParseAddr(host); err == nil {
		return false
	}
	for _, h := range p.Hosts {
		h = strings.ToLower(strings.TrimSpace(h))
		if h == "" {
			continue
		}
		if strings.HasPrefix(h, ".") {
			if strings.HasSuffix(host, h) && len(host) > len(h) {
				return true
			}
		} else if host == h {
			return true
		}
	}
	return false
}

func ipPublica(ip netip.Addr) bool {
	ip = ip.Unmap()
	return ip.IsValid() && !ip.IsLoopback() && !ip.IsPrivate() && !ip.IsUnspecified() && !ip.IsMulticast() &&
		!ip.IsLinkLocalUnicast() && !ip.IsLinkLocalMulticast() && !ip.IsInterfaceLocalMulticast()
}

func (p ProxyDescarga) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	host, puerto, err := net.SplitHostPort(r.RequestURI)
	if r.Method != http.MethodConnect || err != nil || puerto != "443" || !p.Permitido(host) {
		http.Error(w, "destino no permitido", http.StatusForbidden)
		return
	}
	ctx, cancelar := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancelar()
	ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil || len(ips) == 0 {
		http.Error(w, "no se pudo resolver", http.StatusBadGateway)
		return
	}
	var destino net.Conn
	for _, ip := range ips {
		if !ipPublica(ip) {
			continue
		}
		destino, err = (&net.Dialer{}).DialContext(ctx, "tcp", netip.AddrPortFrom(ip.Unmap(), 443).String())
		if err == nil {
			break
		}
		destino = nil
	}
	if destino == nil {
		http.Error(w, "no se pudo conectar", http.StatusBadGateway)
		return
	}
	defer func() { _ = destino.Close() }()
	sec, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "sin túnel", http.StatusInternalServerError)
		return
	}
	cliente, buf, err := sec.Hijack()
	if err != nil {
		return
	}
	defer func() { _ = cliente.Close() }()
	if _, err := cliente.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n")); err != nil {
		return
	}
	fin := make(chan struct{}, 2)
	go func() { _, _ = io.Copy(destino, buf); fin <- struct{}{} }()
	go func() { _, _ = io.Copy(cliente, destino); fin <- struct{}{} }()
	<-fin
}

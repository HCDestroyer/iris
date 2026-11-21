package mensajeria

import (
	"crypto/hmac"
	"crypto/sha1" //nolint:gosec // la credencial efímera de TURN (TURN REST API, coturn use-auth-secret) es HMAC-SHA1 por estándar
	"encoding/base64"
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// TURNEntorno es el relevo de audio del teléfono de Iris (contratos/voz.md §3.12, E4c): el servicio
// turn (coturn) de la instalación, con credenciales efímeras.
type TURNEntorno struct {
	// URLs que recibe el navegador (turn: y turns:).
	URLs []string
	// Secreto compartido con coturn (static-auth-secret). Nunca sale del servidor.
	Secreto string
	// Vida de cada credencial (10 min).
	Vida time.Duration
	// RelayIP: la IP de donde sale el audio del TURN hacia la Issabel (IRIS_TURN_RELAY_IP). Con ella el
	// puente solo deja pasar SDP con candidatos de esa IP.
	RelayIP string
}

// Configurado: hay URLs y secreto.
func (t TURNEntorno) Configurado() bool {
	return len(t.URLs) > 0 && len(t.Secreto) >= 16
}

// TURNDesdeVariables lee IRIS_TURN_URLS (separadas por comas), IRIS_TURN_SECRETO e IRIS_TURN_VIDA.
func TURNDesdeVariables() (TURNEntorno, error) {
	var t TURNEntorno
	var errs []string
	for _, u := range strings.Split(os.Getenv("IRIS_TURN_URLS"), ",") {
		u = strings.TrimSpace(u)
		if u == "" {
			continue
		}
		if err := validarURLTURN(u); err != nil {
			errs = append(errs, "IRIS_TURN_URLS: "+err.Error())
			continue
		}
		t.URLs = append(t.URLs, u)
	}
	t.Secreto = strings.TrimSpace(os.Getenv("IRIS_TURN_SECRETO"))
	if t.Secreto != "" && len(t.Secreto) < 16 {
		errs = append(errs, "IRIS_TURN_SECRETO debe tener al menos 16 caracteres")
	}
	if v := strings.TrimSpace(os.Getenv("IRIS_TURN_RELAY_IP")); v != "" {
		ip, err := netip.ParseAddr(v)
		if err != nil {
			errs = append(errs, "IRIS_TURN_RELAY_IP no es una IP: "+v)
		}
		t.RelayIP = ip.String()
	}
	if v := strings.TrimSpace(os.Getenv("IRIS_TURN_VIDA")); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil || d < time.Minute || d > 24*time.Hour {
			errs = append(errs, "IRIS_TURN_VIDA inválida (1m a 24h): "+v)
		}
		t.Vida = d
	}
	if len(errs) > 0 {
		return t, errors.New(strings.Join(errs, "; "))
	}
	return t, nil
}

// validarURLTURN: turn: o turns: con host y, si trae, transport=udp|tcp (RFC 7065).
func validarURLTURN(u string) error {
	esquema, resto, ok := strings.Cut(u, ":")
	if !ok || (esquema != "turn" && esquema != "turns") || resto == "" || strings.ContainsAny(resto, " \"'<>\\") {
		return fmt.Errorf("%q no es una URL turn o turns", u)
	}
	hp, consulta, _ := strings.Cut(resto, "?")
	if hp == "" || strings.Contains(hp, "/") || strings.Contains(hp, "@") {
		return fmt.Errorf("%q no es una URL turn o turns", u)
	}
	if consulta != "" {
		v, err := url.ParseQuery(consulta)
		if err != nil || len(v) != 1 || (v.Get("transport") != "udp" && v.Get("transport") != "tcp") {
			return fmt.Errorf("%q: solo ?transport=udp o ?transport=tcp", u)
		}
	}
	return nil
}

// ServidorICE es un servidor TURN con su credencial efímera (RTCIceServer del navegador).
type ServidorICE struct {
	URLs       []string `json:"urls"`
	Username   string   `json:"username"`
	Credential string   `json:"credential"`
}

// credencialTURN: username = <vence_unix>:iris-<usuario>, credential = base64(HMAC-SHA1(secreto, username)).
func (t TURNEntorno) credencial(usuario int64, ahora time.Time) (ServidorICE, time.Time) {
	vence := ahora.Add(t.Vida).Truncate(time.Second)
	nombre := strconv.FormatInt(vence.Unix(), 10) + ":iris-" + strconv.FormatInt(usuario, 10)
	h := hmac.New(sha1.New, []byte(t.Secreto))
	h.Write([]byte(nombre))
	return ServidorICE{URLs: append([]string(nil), t.URLs...), Username: nombre, Credential: base64.StdEncoding.EncodeToString(h.Sum(nil))}, vence
}

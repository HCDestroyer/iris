package nucleo

import (
	"errors"
	"net/url"
	"strings"
)

// NormalizarOrigen devuelve el origen de una URL como esquema://host[:puerto] en minúsculas, sin el
// puerto por defecto (443 en https, 80 en http), sin ruta, consulta ni credenciales. Lo usan las
// listas de orígenes permitidos de los conectores (IRIS_ARGOS_ORIGENES, IRIS_HORUS_ORIGENES) y la
// comparación con el origen de un enlace de conexión.
func NormalizarOrigen(crudo string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(crudo))
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" || u.User != nil {
		return "", errors.New("debe ser https://host[:puerto]")
	}
	esquema := strings.ToLower(u.Scheme)
	host := strings.ToLower(u.Hostname())
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	puerto := u.Port()
	if (esquema == "https" && puerto == "443") || (esquema == "http" && puerto == "80") {
		puerto = ""
	}
	if puerto != "" {
		host += ":" + puerto
	}
	return esquema + "://" + host, nil
}

package soporte

import (
	"crypto/x509"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/config"
)

// ConfigCorreoDesdeEntorno lee la configuración del correo (documentada en .env.example):
//
//   - IRIS_CORREO_REDES_PERMITIDAS: redes internas (CIDR o IP, por coma) a las que sí se puede
//     conectar por IMAP/SMTP. Vacía en producción (solo IPs públicas: protección SSRF).
//   - IRIS_CORREO_CA: archivo PEM con autoridades extra para verificar TLS (p. ej. la de un
//     Mailpit o un servidor de laboratorio). El certificado siempre se verifica.
//   - IRIS_CORREO_TAMANO_MAX: bytes; un correo más grande se importa solo con sus cabeceras.
//   - IRIS_CORREO_TIMEOUT: tiempo máximo de cada conexión (30s).
func ConfigCorreoDesdeEntorno() (ConfigCorreo, error) {
	var c ConfigCorreo
	var errs []string
	redes, err := config.ParsearRedes(os.Getenv("IRIS_CORREO_REDES_PERMITIDAS"))
	if err != nil {
		errs = append(errs, "IRIS_CORREO_REDES_PERMITIDAS inválido: "+err.Error())
	}
	c.Red.RedesPermitidas = redes
	if ruta := strings.TrimSpace(os.Getenv("IRIS_CORREO_CA")); ruta != "" {
		pem, err := os.ReadFile(ruta) //nolint:gosec // ruta de configuración del operador
		if err != nil {
			errs = append(errs, "IRIS_CORREO_CA: "+err.Error())
		} else {
			raices, err := x509.SystemCertPool()
			if err != nil || raices == nil {
				raices = x509.NewCertPool()
			}
			if !raices.AppendCertsFromPEM(pem) {
				errs = append(errs, "IRIS_CORREO_CA: el archivo no trae certificados PEM")
			}
			c.Red.Raices = raices
		}
	}
	if v := os.Getenv("IRIS_CORREO_TAMANO_MAX"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 1024 {
			errs = append(errs, "IRIS_CORREO_TAMANO_MAX inválido: "+v)
		}
		c.TamanoMax = n
	}
	if v := os.Getenv("IRIS_CORREO_TIMEOUT"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil || d <= 0 {
			errs = append(errs, "IRIS_CORREO_TIMEOUT inválido: "+v)
		}
		c.Red.Timeout = d
	}
	if len(errs) > 0 {
		return c, fmt.Errorf("configuración del correo: %s", strings.Join(errs, "; "))
	}
	return c, nil
}

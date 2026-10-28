// Package config lee la configuración de los comandos desde variables de entorno IRIS_*
// (documentadas en apps/api/.env.example).
package config

import (
	"fmt"
	"log/slog"
	"net/netip"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config de la API y el worker.
type Config struct {
	// IRIS_DB_URL: cadena de conexión de Postgres (obligatoria).
	DBURL string
	// IRIS_PUERTO (o PORT): puerto HTTP. 3500 por defecto.
	Puerto int
	// IRIS_LOG_NIVEL: debug, info, warn, error.
	LogNivel slog.Level
	// IRIS_COOKIE_SEGURA: Secure en las cookies (true por defecto).
	CookieSegura bool
	// IRIS_DURACION_SESION: vida de una sesión (12h).
	DuracionSesion time.Duration
	// IRIS_CLAVE_CIFRADO: 32 bytes en base64 para cifrar secretos (TOTP). Sin ella, no hay MFA.
	ClaveCifrado string
	// IRIS_RELAY_INTERVALO: cada cuánto corre el relay del outbox aunque no haya NOTIFY (5s).
	RelayIntervalo time.Duration
	// IRIS_IDEMPOTENCIA_RECLAMO: tras cuánto una Idempotency-Key que quedó en proceso (el
	// proceso cayó) se puede retomar (2m).
	IdempotenciaReclamo time.Duration
	// IRIS_BASE_URL: origen público de Iris (el del panel, que hace de proxy de /api). Con él
	// se arman la URL del webhook que se entrega a Argos y los enlaces de conexión.
	BaseURL string
	// IRIS_PROXIES_CONFIABLES: redes (CIDR o IP, separadas por coma) de los proxies cuyo
	// X-Forwarded-For se cree. Vacía = no se confía en ninguno (se usa la IP de la conexión).
	ProxiesConfiables []netip.Prefix
	// IRIS_ARGOS_REDES_PERMITIDAS: redes internas (CIDR o IP, separadas por coma) a las que el
	// conector sí puede llamar. Vacía = Argos debe estar en una IP pública: loopback, privadas,
	// enlace local, CGNAT y demás reservadas se bloquean al conectar (protección SSRF). Solo
	// para desarrollo y laboratorio.
	ArgosRedesPermitidas []netip.Prefix
	// IRIS_ARGOS_TIMEOUT: tiempo máximo de cada llamada a Argos (5s).
	ArgosTimeout time.Duration
	// IRIS_WEBHOOK_VENTANA: desfase máximo aceptado en X-Webhook-Timestamp (5m).
	WebhookVentana time.Duration
	// IRIS_CONECTORES_PERMITIR_HTTP: acepta conectores con base_url http:// (solo desarrollo;
	// false por defecto: el token viaja en Authorization y exige https).
	ConectoresPermitirHTTP bool
	// Version de la compilación (-ldflags "-X main.version=…").
	Version string
}

// Cargar lee el entorno.
func Cargar() (Config, error) {
	c := Config{
		DBURL:          os.Getenv("IRIS_DB_URL"),
		Puerto:         3500,
		LogNivel:       slog.LevelInfo,
		CookieSegura:   true,
		DuracionSesion: 12 * time.Hour,
		ClaveCifrado:   os.Getenv("IRIS_CLAVE_CIFRADO"),
		RelayIntervalo: 5 * time.Second,

		IdempotenciaReclamo: 2 * time.Minute,
		ArgosTimeout:        5 * time.Second,
		WebhookVentana:      5 * time.Minute,
	}
	var errs []string
	if c.DBURL == "" {
		errs = append(errs, "IRIS_DB_URL es obligatoria")
	}
	puerto := os.Getenv("IRIS_PUERTO")
	if puerto == "" {
		puerto = os.Getenv("PORT")
	}
	if puerto != "" {
		n, err := strconv.Atoi(puerto)
		if err != nil || n <= 0 || n > 65535 {
			errs = append(errs, "IRIS_PUERTO inválido: "+puerto)
		}
		c.Puerto = n
	}
	if v := os.Getenv("IRIS_LOG_NIVEL"); v != "" {
		if err := c.LogNivel.UnmarshalText([]byte(v)); err != nil {
			errs = append(errs, "IRIS_LOG_NIVEL inválido: "+v)
		}
	}
	if v := os.Getenv("IRIS_COOKIE_SEGURA"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			errs = append(errs, "IRIS_COOKIE_SEGURA inválido: "+v)
		}
		c.CookieSegura = b
	}
	if v := os.Getenv("IRIS_CONECTORES_PERMITIR_HTTP"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			errs = append(errs, "IRIS_CONECTORES_PERMITIR_HTTP inválido: "+v)
		}
		c.ConectoresPermitirHTTP = b
	}
	if v := strings.TrimRight(os.Getenv("IRIS_BASE_URL"), "/"); v != "" {
		u, err := url.Parse(v)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
			errs = append(errs, "IRIS_BASE_URL inválido: "+v)
		}
		c.BaseURL = v
	}
	redes, err := ParsearRedes(os.Getenv("IRIS_PROXIES_CONFIABLES"))
	if err != nil {
		errs = append(errs, "IRIS_PROXIES_CONFIABLES inválido: "+err.Error())
	}
	c.ProxiesConfiables = redes
	permitidas, err := ParsearRedes(os.Getenv("IRIS_ARGOS_REDES_PERMITIDAS"))
	if err != nil {
		errs = append(errs, "IRIS_ARGOS_REDES_PERMITIDAS inválido: "+err.Error())
	}
	c.ArgosRedesPermitidas = permitidas
	for nombre, destino := range map[string]*time.Duration{
		"IRIS_DURACION_SESION": &c.DuracionSesion, "IRIS_RELAY_INTERVALO": &c.RelayIntervalo,
		"IRIS_IDEMPOTENCIA_RECLAMO": &c.IdempotenciaReclamo,
		"IRIS_ARGOS_TIMEOUT":        &c.ArgosTimeout, "IRIS_WEBHOOK_VENTANA": &c.WebhookVentana,
	} {
		if v := os.Getenv(nombre); v != "" {
			d, err := time.ParseDuration(v)
			if err != nil || d <= 0 {
				errs = append(errs, nombre+" inválido: "+v)
			}
			*destino = d
		}
	}
	if len(errs) > 0 {
		return c, fmt.Errorf("configuración: %s", strings.Join(errs, "; "))
	}
	return c, nil
}

// ParsearRedes lee una lista de CIDR o IP separadas por coma (una IP sola es /32 o /128).
func ParsearRedes(v string) ([]netip.Prefix, error) {
	var redes []netip.Prefix
	for _, parte := range strings.Split(v, ",") {
		parte = strings.TrimSpace(parte)
		if parte == "" {
			continue
		}
		if strings.Contains(parte, "/") {
			p, err := netip.ParsePrefix(parte)
			if err != nil {
				return nil, fmt.Errorf("%q no es un CIDR", parte)
			}
			redes = append(redes, p.Masked())
			continue
		}
		ip, err := netip.ParseAddr(parte)
		if err != nil {
			return nil, fmt.Errorf("%q no es una IP ni un CIDR", parte)
		}
		ip = ip.Unmap()
		redes = append(redes, netip.PrefixFrom(ip, ip.BitLen()))
	}
	return redes, nil
}

// Logger arma el slog JSON del proceso.
func (c Config) Logger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: c.LogNivel}))
}

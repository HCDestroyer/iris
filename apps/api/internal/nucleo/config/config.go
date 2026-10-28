// Package config lee la configuración de los comandos desde variables de entorno IRIS_*
// (documentadas en apps/api/.env.example).
package config

import (
	"fmt"
	"log/slog"
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
	for nombre, destino := range map[string]*time.Duration{
		"IRIS_DURACION_SESION": &c.DuracionSesion, "IRIS_RELAY_INTERVALO": &c.RelayIntervalo,
		"IRIS_IDEMPOTENCIA_RECLAMO": &c.IdempotenciaReclamo,
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

// Logger arma el slog JSON del proceso.
func (c Config) Logger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: c.LogNivel}))
}

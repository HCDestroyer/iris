package voz

import (
	"crypto/tls"
	"log/slog"
	"net"
	"net/http"
	"time"
)

// Servidor arma el servidor HTTP del puerto de control: solo HTTP/1.1 sobre TLS 1.3 (la ruta se
// compara literal), con plazos y tope de cabeceras. No hay ningún proxy delante.
func Servidor(p *Puerta) *http.Server {
	return &http.Server{
		Handler:           p,
		TLSConfig:         p.TLS(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      p.cfg.PlazoLLM + 30*time.Second,
		IdleTimeout:       90 * time.Second,
		MaxHeaderBytes:    16 << 10,
		// Sin HTTP/2: un mapa vacío lo desactiva.
		TLSNextProto: map[string]func(*http.Server, *tls.Conn, http.Handler){},
		ErrorLog:     slog.NewLogLogger(p.cfg.Log.Handler(), slog.LevelDebug),
	}
}

// Escuchar sirve la puerta en ese listener TCP hasta que el servidor se cierra.
func Escuchar(s *http.Server, l net.Listener) error {
	return s.Serve(tls.NewListener(l, s.TLSConfig))
}

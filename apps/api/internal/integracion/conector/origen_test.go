package conector

import (
	"net/url"
	"testing"
)

// 0.14.0 §6.6: la base_url se guarda normalizada (identidad de la instancia de Argos).
func TestOrigenNormalizado(t *testing.T) {
	for crudo, quiere := range map[string]string{
		"https://Argos.Example.COM./api/":  "https://argos.example.com/api",
		"https://argos.example.com:443":    "https://argos.example.com",
		"http://argos.local:80/x":          "http://argos.local/x",
		"https://argos.example.com:8443/":  "https://argos.example.com:8443",
		"http://[::1]:8080/api":            "http://[::1]:8080/api",
		"HTTPS://argos.example.com/Ruta/A": "https://argos.example.com/Ruta/A",
	} {
		u, err := url.Parse(crudo)
		if err != nil {
			t.Fatal(err)
		}
		if got := origenDe(u); got != quiere {
			t.Errorf("%s → %s; quería %s", crudo, got, quiere)
		}
	}
}

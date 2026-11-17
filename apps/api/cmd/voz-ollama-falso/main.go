// voz-ollama-falso es un Ollama de mentira para probar el paquete «Iris Voz» sin GPU (compose de CI,
// infra/voz/docker-compose.prueba.yml). No se publica ni se instala.
package main

import (
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/voz/vozprueba"
)

func main() {
	modelos := strings.Split(os.Getenv("MODELOS"), ",")
	if os.Getenv("MODELOS") == "" {
		modelos = []string{"modelo-a:latest"}
	}
	srv := &http.Server{Addr: ":11434", Handler: vozprueba.Nuevo(modelos...), ReadHeaderTimeout: 5 * time.Second}
	log.Fatal(srv.ListenAndServe())
}

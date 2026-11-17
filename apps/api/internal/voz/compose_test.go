//go:build vozcompose

package voz_test

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/HCDestroyer/iris/apps/api/internal/voz"
	"github.com/HCDestroyer/iris/apps/api/internal/voz/vozprueba"
)

// TestCompose prueba el paquete instalado de verdad (infra/voz/prueba.sh lo levanta con Docker
// Compose): empareja con `iris-voz pair`, usa el modelo por el canal autenticado y desenlaza.
//
//	IRIS_VOZ_URL      https://127.0.0.1:8443
//	IRIS_VOZ_CLI      ruta de install-voz.sh
//	IRIS_VOZ_DIR      carpeta de la instalación
//	IRIS_VOZ_MODELO   modelo permitido que se usa en el chat
//	IRIS_VOZ_REAL=1   detrás hay un Ollama de verdad (se comprueba que la respuesta trae texto)
func TestCompose(t *testing.T) {
	url, cli, dir, modelo := os.Getenv("IRIS_VOZ_URL"), os.Getenv("IRIS_VOZ_CLI"), os.Getenv("IRIS_VOZ_DIR"), os.Getenv("IRIS_VOZ_MODELO")
	if url == "" || cli == "" || dir == "" || modelo == "" {
		t.Fatal("faltan IRIS_VOZ_URL, IRIS_VOZ_CLI, IRIS_VOZ_DIR o IRIS_VOZ_MODELO")
	}
	ctx := t.Context()
	irisVoz := func(entrada string, args ...string) string {
		t.Helper()
		cmd := exec.CommandContext(ctx, "bash", append([]string{cli}, append(args, "--dir", dir)...)...)
		cmd.Stdin = strings.NewReader(entrada)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("iris-voz %v: %v\n%s", args, err, out)
		}
		return string(out)
	}
	cert := vozprueba.Certificado(t)

	// Sin ventana de enlace no se pasa del saludo TLS.
	codigo, _ := voz.NuevoCodigo()
	if _, err := voz.Enlazar(ctx, voz.OpcionesEnlace{URL: url, Codigo: codigo, Cert: cert}); err == nil {
		t.Fatal("sin ventana de enlace debía fallar")
	}

	// `iris-voz pair` lee el código por la entrada (nunca como argumento) y muestra la huella.
	salida := irisVoz(codigo+"\n", "pair")
	r, err := voz.Enlazar(ctx, voz.OpcionesEnlace{URL: url, Codigo: codigo, Cert: cert})
	if err != nil {
		t.Fatalf("enlazar: %v\n%s", err, salida)
	}
	if !strings.Contains(salida, r.HuellaServidor) {
		t.Fatalf("la huella que vio Iris (%s) no es la que imprime el servidor:\n%s", r.HuellaServidor, salida)
	}

	// Al activar: token aleatorio sin convivencia.
	derivado, err := voz.NuevoCliente(voz.OpcionesCliente{URL: url, Huellas: []string{r.HuellaServidor}, Cert: cert, Token: r.Token, VersionIris: r.Manifiesto.IrisMin})
	if err != nil {
		t.Fatal(err)
	}
	token, _ := voz.NuevoToken()
	if err := derivado.Credencial(ctx, voz.PeticionCredencial{Token: token}); err != nil {
		t.Fatalf("rotar al activar: %v", err)
	}
	c, err := voz.NuevoCliente(voz.OpcionesCliente{URL: url, Huellas: []string{r.HuellaServidor}, Cert: cert, Token: token, VersionIris: r.Manifiesto.IrisMin})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Cerrar()

	s, err := c.Salud(ctx)
	if err != nil || s.LLM.Estado != voz.EstadoListo || s.Version != r.Manifiesto.Version {
		t.Fatalf("salud: %v %+v", err, s)
	}
	m, err := c.Metricas(ctx)
	if err != nil || len(m.Modelos) == 0 || m.Memoria.TotalMb == 0 {
		t.Fatalf("métricas: %v %+v", err, m)
	}

	cuerpo, _ := json.Marshal(map[string]any{"model": modelo, "max_tokens": 16, "stream": false, "tool_choice": "none",
		"messages": []map[string]string{{"role": "system", "content": "Responde con una sola palabra."}, {"role": "user", "content": "Di hola."}}})
	out, err := c.Hacer(ctx, "POST", voz.RutaChat, cuerpo, voz.TopeRespuestaLLM)
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	var resp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			PromptTokens int `json:"prompt_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(out, &resp); err != nil || len(resp.Choices) == 0 || resp.Usage.PromptTokens == 0 {
		t.Fatalf("respuesta del chat: %v %s", err, out)
	}
	if os.Getenv("IRIS_VOZ_REAL") == "1" && strings.TrimSpace(resp.Choices[0].Message.Content) == "" {
		t.Fatalf("el modelo real no contestó nada: %s", out)
	}
	// Lo que no está en la lista cerrada no pasa, tampoco con un Ollama de verdad detrás.
	malo := bytes.Replace(cuerpo, []byte(`"stream":false`), []byte(`"stream":false,"keep_alive":"24h"`), 1)
	if _, err := c.Hacer(ctx, "POST", voz.RutaChat, malo, voz.TopeRespuestaLLM); err == nil || !strings.Contains(err.Error(), "campo_no_permitido") {
		t.Fatalf("keep_alive debía rechazarse: %v", err)
	}

	// `iris-voz unpair`: nada vale hasta un enlace nuevo.
	irisVoz("", "unpair")
	c.Cerrar()
	if _, err := c.Salud(ctx); err == nil {
		t.Fatal("tras desenlazar no debía aceptar nada")
	}
}

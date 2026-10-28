// Comando openapi: escribe el OpenAPI 3.1 de la API (generado por huma desde el código) en
// packages/contratos/openapi.json. No necesita base de datos.
//
//	go run ./cmd/openapi                 # desde apps/api
//	go run ./cmd/openapi -salida -       # a la salida estándar
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/danielgtaylor/huma/v2"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/servidor"
	"github.com/HCDestroyer/iris/apps/api/internal/soporte"
)

// versionContrato es la versión del contrato publicada en info.version.
const versionContrato = "0.1.0"

func main() {
	salida := flag.String("salida", "../../packages/contratos/openapi.json", "archivo de salida (- = stdout)")
	flag.Parse()
	if err := escribir(*salida); err != nil {
		fmt.Fprintln(os.Stderr, "openapi:", err)
		os.Exit(1)
	}
}

func escribir(salida string) error {
	_, api := servidor.Nuevo(servidor.Deps{
		Log:       slog.New(slog.NewTextHandler(io.Discard, nil)),
		Version:   versionContrato,
		Identidad: identidad.Nuevo(identidad.Config{}),
		Modulos:   []func(huma.API){soporte.Nuevo(soporte.Config{}).Registrar},
	})
	crudo, err := api.OpenAPI().MarshalJSON()
	if err != nil {
		return err
	}
	var b bytes.Buffer
	if err := json.Indent(&b, crudo, "", "  "); err != nil {
		return err
	}
	b.WriteByte('\n')
	if salida == "-" {
		_, err := os.Stdout.Write(b.Bytes())
		return err
	}
	if err := os.MkdirAll(filepath.Dir(salida), 0o750); err != nil {
		return err
	}
	return os.WriteFile(salida, b.Bytes(), 0o644) //nolint:gosec // el contrato es público
}

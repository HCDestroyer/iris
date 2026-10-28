package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/config"
)

const usoSalud = "uso: iris salud [--url http://127.0.0.1:$PORT/api/v1/salud] [--timeout 3s]"

// salud es el healthcheck de la imagen (distroless: sin shell ni curl): GET a /api/v1/salud de
// la API local y sale 0 si responde 200, 1 si no. Solo lee IRIS_PUERTO o PORT (no necesita la
// base ni el resto de la configuración).
func salud(ctx context.Context, args []string, salida io.Writer) error {
	fs := flag.NewFlagSet("salud", flag.ContinueOnError)
	fs.SetOutput(salida)
	destino := fs.String("url", "", "URL a consultar (por defecto http://127.0.0.1:$PORT/api/v1/salud)")
	plazo := fs.Duration("timeout", 3*time.Second, "tiempo máximo de la consulta")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 || *plazo <= 0 {
		return errors.New(usoSalud)
	}
	if *destino == "" {
		puerto, err := config.PuertoDesdeEntorno()
		if err != nil {
			return err
		}
		*destino = "http://127.0.0.1:" + strconv.Itoa(puerto) + "/api/v1/salud"
	}
	if err := comprobarSalud(ctx, *destino, *plazo); err != nil {
		return err
	}
	_, _ = fmt.Fprintln(salida, "ok")
	return nil
}

// comprobarSalud hace el GET con el plazo dado, sin proxy ni redirecciones; error si la API no
// responde o no responde 200.
func comprobarSalud(ctx context.Context, destino string, plazo time.Duration) error {
	ctx, cancelar := context.WithTimeout(ctx, plazo)
	defer cancelar()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, destino, nil)
	if err != nil {
		return fmt.Errorf("salud: %w", err)
	}
	cliente := &http.Client{
		Transport: &http.Transport{Proxy: nil, DisableKeepAlives: true},
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	res, err := cliente.Do(req)
	if err != nil {
		return fmt.Errorf("salud: %w", err)
	}
	defer func() { _ = res.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 64<<10))
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("salud: %s respondió %d", destino, res.StatusCode)
	}
	return nil
}

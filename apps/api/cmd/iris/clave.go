package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/clave"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/config"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad"
)

const usoClave = "uso: iris clave rotar [--simular] [--ignorar-ilegibles]   (con IRIS_CLAVE_CIFRADO = nueva e IRIS_CLAVE_CIFRADO_ANTERIOR = anterior)"

// rotarClave vuelve a cifrar con IRIS_CLAVE_CIFRADO todo lo cifrado con
// IRIS_CLAVE_CIFRADO_ANTERIOR (TOTP, conectores, buzones) en una transacción, auditada como
// acción de sistema (canal consola). Es idempotente: lo que ya está con la nueva se cuenta y no
// se toca. Con --simular solo cuenta. Ver docs/runbooks/rotar-clave-cifrado.md.
func rotarClave(ctx context.Context, cfg config.Config, args []string, salida io.Writer) error {
	fs := flag.NewFlagSet("clave rotar", flag.ContinueOnError)
	fs.SetOutput(salida)
	simular := fs.Bool("simular", false, "solo contar lo que se rotaría, sin escribir")
	ignorar := fs.Bool("ignorar-ilegibles", false, "rotar aunque haya valores que no se descifran con ninguna clave (quedan como están)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return errors.New(usoClave)
	}
	if cfg.ClaveCifrado == "" || cfg.ClaveCifradoAnterior == "" {
		return errors.New("iris clave rotar necesita IRIS_CLAVE_CIFRADO (la nueva) e IRIS_CLAVE_CIFRADO_ANTERIOR (la que se rota)")
	}
	c, err := cifrador(cfg)
	if err != nil {
		return err
	}
	pool, err := conectar(ctx, cfg)
	if err != nil {
		return err
	}
	defer pool.Close()
	if err := identidad.SincronizarCatalogo(ctx, pool); err != nil {
		return err
	}
	cctx := nucleo.ConBD(contexto.Sistema(ctx, contexto.CanalConsola), pool)
	r, err := clave.Nuevo(c).Rotar.Ejecutar(cctx, clave.EntradaRotar{Simular: *simular, IgnorarIlegibles: *ignorar})
	if err != nil {
		return err
	}
	imprimirRotacion(salida, r)
	return nil
}

func imprimirRotacion(w io.Writer, r clave.Resultado) {
	verbo := "Re-cifrados"
	if r.Simulada {
		verbo = "Por re-cifrar (simulación, no se escribió nada)"
	}
	_, _ = fmt.Fprintf(w, "%-50s %7s %8s %8s %9s\n", "columna", "total", "rotadas", "ya", "ilegibles")
	for _, c := range r.Columnas {
		_, _ = fmt.Fprintf(w, "%-50s %7d %8d %8d %9d\n", c.Columna, c.Total, c.Rotadas, c.YaRotadas, c.Ilegibles)
	}
	_, _ = fmt.Fprintf(w, "%s: %d\n", verbo, r.Rotadas())
	for _, il := range r.Ilegibles {
		_, _ = fmt.Fprintf(w, "Ilegible (quedó como estaba): %s id %d\n", il.Columna, il.ID)
	}
}

// Package migraciones lleva las migraciones goose (SQL embebido) y prepara el esquema de River.
//
// Crear una migración: `goose -dir migraciones create <nombre> sql` (marca de tiempo real).
// Aplicarlas: `go run ./cmd/iris migrar` (o `make migrar`).
package migraciones

import (
	"context"
	"embed"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"
)

//go:embed *.sql
var archivos embed.FS

// EsquemaRiver es el schema donde viven las tablas de River.
const EsquemaRiver = "river"

// Migrar aplica las migraciones goose pendientes y luego las de River (en el schema river).
// Usa advisory locks: dos procesos no migran a la vez.
func Migrar(ctx context.Context, pool *pgxpool.Pool, log *slog.Logger) error {
	if log == nil {
		log = slog.Default()
	}
	db := stdlib.OpenDBFromPool(pool)
	defer func() { _ = db.Close() }()

	candado, err := lock.NewPostgresSessionLocker()
	if err != nil {
		return err
	}
	proveedor, err := goose.NewProvider(goose.DialectPostgres, db, archivos, goose.WithSessionLocker(candado))
	if err != nil {
		return fmt.Errorf("goose: %w", err)
	}
	resultados, err := proveedor.Up(ctx)
	if err != nil {
		return fmt.Errorf("goose up: %w", err)
	}
	for _, r := range resultados {
		log.InfoContext(ctx, "migración aplicada", "archivo", r.Source.Path, "duracion", r.Duration)
	}

	if _, err := pool.Exec(ctx, "CREATE SCHEMA IF NOT EXISTS "+EsquemaRiver); err != nil {
		return fmt.Errorf("river: crear schema: %w", err)
	}
	migrador, err := rivermigrate.New(riverpgxv5.New(pool), &rivermigrate.Config{Schema: EsquemaRiver, Logger: log})
	if err != nil {
		return fmt.Errorf("river: %w", err)
	}
	res, err := migrador.Migrate(ctx, rivermigrate.DirectionUp, nil)
	if err != nil {
		return fmt.Errorf("river migrate: %w", err)
	}
	for _, v := range res.Versions {
		log.InfoContext(ctx, "migración de River aplicada", "version", v.Version)
	}
	return nil
}

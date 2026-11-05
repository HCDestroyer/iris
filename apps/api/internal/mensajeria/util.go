package mensajeria

import (
	"errors"
	"strconv"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
)

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

func conflictoCanal(err error) error {
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "23505" {
		return nucleo.Conflicto("canal_existente", "La empresa ya tiene un canal activo de ese medio, o el número ya está en otro canal activo")
	}
	return err
}

func texto(s string) *string { return &s }

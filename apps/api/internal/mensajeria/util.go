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
	// canal_existente (contratos/conversaciones.md §1): el phone_number_id ya está en otro número
	// activo, o (al activar) la empresa ya tiene otro número de avisos activo. Una empresa puede
	// tener varios números activos.
	if errors.As(err, &pg) && pg.Code == "23505" {
		if pg.ConstraintName == "canales_avisos_uk" {
			return nucleo.Conflicto("canal_existente", "La empresa ya tiene otro número de avisos activo")
		}
		return nucleo.Conflicto("canal_existente", "Ese phone number ID ya está en otro número activo")
	}
	return err
}

func texto(s string) *string { return &s }

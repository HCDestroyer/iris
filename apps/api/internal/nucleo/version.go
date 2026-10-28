package nucleo

import (
	"errors"

	"github.com/jackc/pgx/v5"
)

// Control optimista por `version` (CLAUDE.md §6): toda edición manda la versión que leyó.
//
// Patrón con sqlc:
//
//	UPDATE … SET …, version = version + 1 WHERE id = @id AND version = @version RETURNING *;
//
//	fila, err := q.ActualizarX(ctx, params)
//	if err := nucleo.VerificarVersion(err, "usuario", id); err != nil { return err }
//
// Leer antes la fila con FOR UPDATE (404 si no existe) deja como única causa de "sin filas"
// una versión distinta.

// VerificarVersion convierte el pgx.ErrNoRows de un UPDATE … WHERE version = @version en un
// 409 conflicto_version. Cualquier otro error pasa tal cual.
func VerificarVersion(err error, entidad string, id any) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ConflictoVersion(entidad, id)
	}
	return err
}

// ExigirVersion compara la versión enviada con la actual (cuando la fila ya se leyó con
// FOR UPDATE) y devuelve 409 si difieren.
func ExigirVersion(enviada, actual int32, entidad string, id any) error {
	if enviada != actual {
		return ConflictoVersion(entidad, id)
	}
	return nil
}

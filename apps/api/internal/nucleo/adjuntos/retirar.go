package adjuntos

import (
	"context"
	"errors"
	"log/slog"
	"sync"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	adjuntosbd "github.com/HCDestroyer/iris/apps/api/internal/nucleo/adjuntos/bd"
)

// Filtro lo registra (opcional) un módulo cuyos adjuntos no se ven todos aunque se vea la
// entidad: el chat solo muestra la foto de un mensaje sin eliminar. Corre después del
// Verificador, en la transacción; false = 404 en la descarga y fuera de la lista.
type Filtro func(ctx context.Context, q DBTX, a Adjunto, uso Uso) (bool, error)

var (
	muFiltros sync.RWMutex
	filtros   = map[string]Filtro{}
)

// RegistrarFiltro fija (o reemplaza) el Filtro de una entidad adjuntable.
func RegistrarFiltro(entidad string, f Filtro) {
	muFiltros.Lock()
	defer muFiltros.Unlock()
	filtros[entidad] = f
}

func filtroDe(entidad string) Filtro {
	muFiltros.RLock()
	defer muFiltros.RUnlock()
	return filtros[entidad]
}

// pasaFiltro: sin filtro registrado, todo adjunto de una entidad visible pasa.
func pasaFiltro(ctx context.Context, q DBTX, a Adjunto, uso Uso) (bool, error) {
	f := filtroDe(a.Entidad)
	if f == nil {
		return true, nil
	}
	return f(ctx, q, a, uso)
}

// MotivoRetirado: el módulo dueño dejó de mostrar el adjunto (mensaje del chat eliminado, foto
// nunca enviada).
const MotivoRetirado = "retirado"

// Retirar deja el adjunto rechazado (motivo «retirado») en cualquier estado, borra sus partes y
// emite adjunto.rechazado, en la transacción del caso de uso que lo llama. Devuelve la clave del
// almacenamiento para borrar el contenido después del COMMIT (BorrarContenido); "" si ya estaba
// rechazado o no existe.
func Retirar(ctx context.Context, t *auditoria.Tx, id uuid.UUID) (string, error) {
	q := adjuntosbd.New(t)
	motivo := MotivoRetirado
	f, err := q.RetirarAdjunto(ctx, adjuntosbd.RetirarAdjuntoParams{ID: id, Motivo: &motivo})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if err := q.BorrarPartes(ctx, id); err != nil {
		return "", err
	}
	a := aAdjunto(f)
	t.RegistrarCambios("adjunto", a.ID, nil, map[string]any{"estado": a.Estado, "motivo_rechazo": motivo})
	if err := t.Emitir(EventoRechazado, eventoRechazado{eventoAdjunto: eventoDe(a), Motivo: motivo}); err != nil {
		return "", err
	}
	return f.Clave, nil
}

// BorrarContenido quita el objeto del almacenamiento (después del COMMIT de Retirar). Sin
// almacenamiento o si falla, lo deja en el log: el adjunto ya no se puede ver ni descargar.
func BorrarContenido(ctx context.Context, almacen Almacen, clave string) {
	if clave == "" {
		return
	}
	if almacen == nil {
		slog.WarnContext(ctx, "adjuntos: sin almacenamiento para borrar un adjunto retirado", "clave", clave)
		return
	}
	if err := almacen.Borrar(ctx, clave); err != nil && !errors.Is(err, ErrNoExiste) {
		slog.WarnContext(ctx, "adjuntos: borrar del almacenamiento un adjunto retirado", "clave", clave, "err", err)
	}
}

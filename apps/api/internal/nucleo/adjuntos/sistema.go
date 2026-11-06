package adjuntos

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	adjuntosbd "github.com/HCDestroyer/iris/apps/api/internal/nucleo/adjuntos/bd"
)

// EntradaCompleto es un archivo que trae el sistema, no un usuario (p. ej. la foto que un cliente
// mandó por WhatsApp y que la mensajería descargó de Meta).
type EntradaCompleto struct {
	EmpresaID *int64
	Entidad   string
	EntidadID string
	// Tipo: foto o documento (audio y video van como documento).
	Tipo  string
	Mime  string
	Datos []byte
}

// GuardarCompleto escribe el archivo en el almacenamiento y lo registra completo en la transacción
// del caso de uso que lo trae (emite adjunto.completado). Si la transacción se revierte, el objeto
// queda en el almacenamiento con una clave que nadie usa (reintentar con el mismo id la reescribe).
// No verifica la entidad: quien llama ya lo hizo.
func GuardarCompleto(ctx context.Context, t *auditoria.Tx, almacen Almacen, id uuid.UUID, in EntradaCompleto) (Adjunto, error) {
	if almacen == nil {
		return Adjunto{}, errors.New("adjuntos: sin almacenamiento configurado")
	}
	if len(in.Datos) == 0 {
		return Adjunto{}, errors.New("adjuntos: archivo vacío")
	}
	if in.Tipo != "foto" && in.Tipo != "documento" {
		return Adjunto{}, fmt.Errorf("adjuntos: tipo %q no permitido para el sistema", in.Tipo)
	}
	suma := sha256.Sum256(in.Datos)
	sha := hex.EncodeToString(suma[:])
	clave := fmt.Sprintf("e%d/%s/%s", empresaDe(in.EmpresaID), in.Entidad, id)
	mime := normalizarMIME(in.Mime)
	if err := almacen.Guardar(ctx, clave, bytes.NewReader(in.Datos), int64(len(in.Datos)), mime, sha); err != nil {
		return Adjunto{}, err
	}
	f, err := adjuntosbd.New(t).InsertarAdjuntoCompleto(ctx, adjuntosbd.InsertarAdjuntoCompletoParams{ID: id, EmpresaID: in.EmpresaID,
		Entidad: in.Entidad, EntidadID: in.EntidadID, Tipo: in.Tipo, Mime: mime, Tamano: int64(len(in.Datos)), Sha256: sha,
		Almacenamiento: almacen.Nombre(), Clave: clave})
	if err != nil {
		return Adjunto{}, err
	}
	a := aAdjunto(f)
	return a, t.Emitir(EventoCompletado, eventoCompletado{eventoAdjunto: eventoDe(a), Mime: a.Mime, Tamano: a.Tamano, SHA256: a.SHA256})
}

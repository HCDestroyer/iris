package identidad

import (
	"context"
	"sync"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
)

// GanchoCerrarSesion corre dentro de la transacción de sesion.cerrar (DELETE /sesion), después de
// revocar la sesión: lo que otro módulo ata a la sesión se limpia con ella (p. ej. los dispositivos
// de push de internal/notificaciones). Si devuelve error, el cierre de sesión se revierte.
type GanchoCerrarSesion func(ctx context.Context, t *auditoria.Tx, sesionID int64) error

var (
	ganchosCerrarMu sync.RWMutex
	ganchosCerrar   []GanchoCerrarSesion
)

// AlCerrarSesion registra un gancho (normalmente desde el init de otro módulo).
func AlCerrarSesion(fn GanchoCerrarSesion) {
	ganchosCerrarMu.Lock()
	defer ganchosCerrarMu.Unlock()
	ganchosCerrar = append(ganchosCerrar, fn)
}

func correrGanchosCerrar(ctx context.Context, t *auditoria.Tx, sesionID int64) error {
	ganchosCerrarMu.RLock()
	gs := append([]GanchoCerrarSesion(nil), ganchosCerrar...)
	ganchosCerrarMu.RUnlock()
	for _, g := range gs {
		if err := g(ctx, t, sesionID); err != nil {
			return err
		}
	}
	return nil
}

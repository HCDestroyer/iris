package identidad

import (
	"context"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
	identidadbd "github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad/bd"
)

// nuevoCerrarSesion revoca la sesión del panel con que llegó la petición (también una que
// quedó pendiente de MFA).
func nuevoCerrarSesion() *auditoria.Caso[struct{}, struct{}] {
	return auditoria.Nuevo(auditoria.Caso[struct{}, struct{}]{
		Accion: "sesion.cerrar",
		Acceso: auditoria.Publico, // la sesión pendiente de MFA no tiene actor todavía
		Logica: func(ctx context.Context, t *auditoria.Tx, _ struct{}) (struct{}, error) {
			s := contexto.SesionDe(ctx)
			if s.ID == 0 {
				return struct{}{}, nucleo.NoAutenticado("No hay una sesión abierta")
			}
			t.FijarEntidad("sesion", s.ID)
			_, err := identidadbd.New(t).RevocarSesion(ctx, s.ID)
			return struct{}{}, err
		},
	})
}

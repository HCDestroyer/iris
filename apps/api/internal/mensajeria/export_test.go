package mensajeria

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
)

// Lo que corren los workers, para las pruebas (sin River).

func (m *Modulo) ProcesarEnvio(ctx context.Context, id uuid.UUID, ultimo bool) (time.Duration, error) {
	return m.procesarEnvio(ctx, id, ultimo)
}

func (m *Modulo) DespacharWebhooks(ctx context.Context, bd nucleo.BD) error {
	return m.despacharWebhooks(ctx, bd)
}

func (m *Modulo) ProcesarEntrante(ctx context.Context, id int64) error {
	_, err := m.procesarMeta.Ejecutar(ctx, id)
	return err
}

func (m *Modulo) VencerAvisos(ctx context.Context) (int, error) {
	return m.vencerAvisos.Ejecutar(ctx, struct{}{})
}

var ErrReintentar = errReintentar

func (m *Modulo) PurgarFeedChat(ctx context.Context, bd nucleo.BD) error {
	return m.purgarFeedChat(ctx, bd)
}

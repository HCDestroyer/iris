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

func (m *Modulo) FijarTrasLeerCabeza(f func()) { m.trasLeerCabeza = f }

func (m *Modulo) ProcesarMensaje(ctx context.Context, id uuid.UUID, ultimo bool) (time.Duration, error) {
	return m.procesarMensaje(ctx, id, ultimo)
}

func (m *Modulo) DescargarMedio(ctx context.Context, id uuid.UUID, ultimo bool) error {
	return m.descargarMedio(ctx, id, ultimo)
}

func (m *Modulo) VencerSLA(ctx context.Context) (int, error) {
	return m.vencerSLA.Ejecutar(ctx, struct{}{})
}

func (m *Modulo) DespacharBoletas(ctx context.Context, bd nucleo.BD) error {
	return m.despacharBoletas(ctx, bd)
}

func (m *Modulo) VencerDisponibilidad(ctx context.Context, bd nucleo.BD) int {
	return m.vencerDisponibilidades(ctx, bd)
}

func (m *Modulo) DispararBots(ctx context.Context, bd nucleo.BD) int {
	return m.dispararBots(ctx, bd)
}

func (m *Modulo) PurgarFotosSinEnviar(ctx context.Context, bd nucleo.BD) error {
	return m.purgarFotosSinEnviar(ctx, bd)
}

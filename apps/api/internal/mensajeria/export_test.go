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
	n, _ := m.vencerDisponibilidades(ctx, bd)
	return n
}

func (m *Modulo) DispararBots(ctx context.Context, bd nucleo.BD) int {
	n, _ := m.dispararBots(ctx, bd)
	return n
}

// VencerDisponibilidadConError y DispararBotsConError devuelven además el primer error de una fila
// (el barrido sigue con las demás).
func (m *Modulo) VencerDisponibilidadConError(ctx context.Context, bd nucleo.BD) (int, error) {
	return m.vencerDisponibilidades(ctx, bd)
}

func (m *Modulo) DispararBotsConError(ctx context.Context, bd nucleo.BD) (int, error) {
	return m.dispararBots(ctx, bd)
}

// FijarFallarFila hace fallar las filas del barrido para las que f devuelve un error.
func (m *Modulo) FijarFallarFila(f func(id int64) error) { m.fallarFila = f }

func (m *Modulo) PurgarFotosSinEnviar(ctx context.Context, bd nucleo.BD) error {
	return m.purgarFotosSinEnviar(ctx, bd)
}

// ConsultasResumen: cuántas veces GET /chat/resumen calculó (sin el tope de frecuencia).
func (m *Modulo) ConsultasResumen() int64 { return m.resumenes.consultas.Load() }

// VaciarResumenes olvida los resúmenes guardados (pruebas que comparan contadores al instante).
func (m *Modulo) VaciarResumenes() { m.resumenes.vaciar() }

// FijarRelojResumen cambia el reloj del tope de frecuencia de GET /chat/resumen.
func (m *Modulo) FijarRelojResumen(f func() time.Time) { m.resumenes.ahora = f }

func (m *Modulo) PurgarRegistrosQR(ctx context.Context, bd nucleo.BD) error {
	return m.purgarRegistrosQR(ctx, bd)
}

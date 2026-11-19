package mensajeria

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	mensajeriabd "github.com/HCDestroyer/iris/apps/api/internal/mensajeria/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
)

// Ráfaga levantada (contratos/voz.md §3.5): desde el instante en que se levanta, las entrantes
// anteriores de ese número a ese canal dejan de contar para la ráfaga. La levanta una persona a mano
// («Dejar que vuelva a llamar», `llamadas.desbloquear_numero`) o una saliente de la empresa a ese
// número cuando Meta la acepta (quien llama quiere que le puedan devolver la llamada).

// OrigenRafagaManual y OrigenRafagaLlamada: quién levantó la ráfaga.
const (
	OrigenRafagaManual  = "manual"
	OrigenRafagaLlamada = "llamada"
)

// rafagaActiva: el número tiene la ráfaga encima ahora (la próxima entrante se rechazaría, o hay una
// tarjeta de ráfaga vigente).
func (m *Modulo) rafagaActiva(ctx context.Context, q *mensajeriabd.Queries, canal int64, numero string, ahora time.Time) (*mensajeriabd.MensajeriaLlamada, bool, error) {
	v, err := q.RafagaVigente(ctx, mensajeriabd.RafagaVigenteParams{CanalID: &canal, Numero: numero, Ahora: ahora})
	if err == nil {
		return &v, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, false, err
	}
	r, err := q.LlamadasRecientesDeNumero(ctx, mensajeriabd.LlamadasRecientesDeNumeroParams{CanalID: &canal, Numero: numero, Desde: ahora.Add(-RafagaVentana)})
	if err != nil {
		return nil, false, err
	}
	return nil, r.Cuantas >= RafagaLlamadas, nil
}

// levantarRafaga anota la marca, emite `voz.rafaga_levantada` (nunca el número) y refresca en el hilo la
// tarjeta de la ráfaga vigente. A mano la marca se mueve SIEMPRE (levantada_en = ahora), haya o no
// ráfaga a la vista: así gana la carrera contra una entrante que se esté procesando a la vez; sin
// ráfaga solo se omite el evento (la auditoría queda siempre, la escribe el caso de uso). Una saliente
// sin ráfaga no deja rastro. No toma el candado voz:rafaga (se interbloquearía con guardarConv).
// Devuelve si había ráfaga.
func (m *Modulo) levantarRafaga(ctx context.Context, t *auditoria.Tx, canal int64, numero string, empresa *int64, conversacion *int64, usuario *int64,
	origen string, llamada *uuid.UUID) (bool, error) {
	q := mensajeriabd.New(t)
	ahora := m.ahora()
	vigente, activa, err := m.rafagaActiva(ctx, q, canal, numero, ahora)
	if err != nil {
		return false, err
	}
	if !activa && origen != OrigenRafagaManual {
		return false, nil
	}
	if err := q.LevantarRafaga(ctx, mensajeriabd.LevantarRafagaParams{CanalID: canal, Numero: numero, EmpresaID: empresa, Ahora: ahora,
		LevantadaPor: usuario, Origen: origen}); err != nil {
		return false, err
	}
	t.Anotar(map[string]any{"rafaga_activa": activa, "origen": origen})
	if !activa {
		return false, nil // a mano sin ráfaga a la vista: marca movida y auditada, sin evento
	}
	if err := t.Emitir("voz.rafaga_levantada", map[string]any{"empresa_id": empresa, "canal_id": canal, "conversacion_id": conversacion,
		"origen": origen, "usuario_id": usuario, "llamada_id": llamada}); err != nil {
		return false, err
	}
	if vigente != nil {
		if err := m.emitirEventoLlamada(ctx, t, *vigente, EventoLlamadaActualizada, nil); err != nil {
			return false, err
		}
	}
	return activa, nil
}

// RafagaLevantada es la respuesta de POST /conversaciones/{id}/llamadas/desbloquear.
type RafagaLevantada struct {
	// EstabaActiva: el número tenía la ráfaga encima (si no, no cambia nada para el próximo timbre).
	EstabaActiva bool `json:"estabaActiva" doc:"El número tenía la ráfaga encima: desde ahora su próxima llamada vuelve a sonar"`
}

func nuevoDesbloquearNumero(m *Modulo) *auditoria.Caso[EntradaConvVoz, RafagaLevantada] {
	return auditoria.Nuevo(auditoria.Caso[EntradaConvVoz, RafagaLevantada]{
		Accion: "llamadas.desbloquear_numero", Permiso: nucleo.PermLlamadasDesbloquearNumero,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaConvVoz) (RafagaLevantada, error) {
			a, err := alcanceVoz(ctx, nucleo.PermLlamadasDesbloquearNumero)
			if err != nil {
				return RafagaLevantada{}, conversacionClienteNoEncontrada()
			}
			t.FijarEmpresa(a.empresa)
			t.FijarEntidad("conversacion", in.ID)
			q := mensajeriabd.New(t)
			c, err := convVisible(ctx, q, a, in.ID, true)
			if err != nil {
				return RafagaLevantada{}, err
			}
			// En un número prestado la ráfaga es del número (cuenta las llamadas de todas las empresas): solo
			// la levanta la empresa dueña. La prestataria tampoco puede llamar desde él (canalParaSalientes).
			canal, err := q.CanalPorID(ctx, c.CanalID)
			if err != nil {
				return RafagaLevantada{}, err
			}
			if derefEmpresa(canal.EmpresaID) != a.empresa {
				return RafagaLevantada{}, nucleo.NuevoProblema(http.StatusConflict, "numero_prestado", "Conflicto",
					"Este número es prestado: solo la empresa dueña del número puede dejar que vuelva a llamar")
			}
			usuario := a.usuario
			activa, err := m.levantarRafaga(ctx, t, c.CanalID, c.Numero, c.EmpresaID, &c.ID, &usuario, OrigenRafagaManual, nil)
			if err != nil {
				return RafagaLevantada{}, err
			}
			return RafagaLevantada{EstabaActiva: activa}, nil
		},
	})
}

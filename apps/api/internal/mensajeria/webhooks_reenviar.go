package mensajeria

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	mensajeriabd "github.com/HCDestroyer/iris/apps/api/internal/mensajeria/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
)

// Reenviar un webhook de Iris hacia Argos que agotó sus reintentos (contrato 0.14.0 §6.6). Un
// tecnico.argos_vinculado agotado deja la carga inicial abierta y las operaciones con `tecnico`
// bloqueadas; un desvinculado agotado retiene el vinculado siguiente. Una persona con
// integraciones.reenviar_webhook lo vuelve a la cola con los intentos en cero.

// EventoWebhookReenviado: un webhook agotado volvió a la cola.
const EventoWebhookReenviado = "integracion.webhook_reenviado"

// EntradaReenviarWebhook: POST /mensajeria/webhooks-salida/{id}/reenviar.
type EntradaReenviarWebhook struct {
	ID uuid.UUID `path:"id" doc:"Id del webhook (el del evento, X-Webhook-Id)"`
}

// WebhookSalida es el estado de un webhook de Iris hacia Argos (sin su cuerpo).
type WebhookSalida struct {
	ID             uuid.UUID `json:"id"`
	Evento         string    `json:"evento"`
	EmpresaID      *int64    `json:"empresaId" nullable:"true"`
	Estado         string    `json:"estado" enum:"pendiente,entregado,agotado,omitido"`
	Intentos       int32     `json:"intentos"`
	ProximoIntento time.Time `json:"proximoIntento"`
}

func nuevoReenviarWebhook(m *Modulo) *auditoria.Caso[EntradaReenviarWebhook, WebhookSalida] {
	return auditoria.Nuevo(auditoria.Caso[EntradaReenviarWebhook, WebhookSalida]{
		Accion: "integraciones.reenviar_webhook", Permiso: nucleo.PermIntegracionesReenviarWebhook, SinEmpresa: true,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaReenviarWebhook) (WebhookSalida, error) {
			t.FijarEntidad("webhook_salida", in.ID.String())
			t.FijarEmpresa(0)
			q := mensajeriabd.New(t)
			antes, err := q.WebhookParaReenviar(ctx, in.ID)
			if errors.Is(err, pgx.ErrNoRows) {
				return WebhookSalida{}, nucleo.NoEncontrado("El webhook no existe")
			}
			if err != nil {
				return WebhookSalida{}, err
			}
			// Fuera de la empresa activa: el mismo 404 que uno que no existe.
			if !visibleEnEmpresa(ctx, antes.EmpresaID) {
				return WebhookSalida{}, nucleo.NoEncontrado("El webhook no existe")
			}
			if antes.Estado != "agotado" {
				return WebhookSalida{}, nucleo.ReglaNegocio("webhook_no_agotado",
					"Solo se reenvía un webhook que agotó sus reintentos; este está "+antes.Estado)
			}
			f, err := q.ReenviarWebhookSalida(ctx, mensajeriabd.ReenviarWebhookSalidaParams{ID: in.ID, ProximoIntento: m.ahora()})
			if err != nil {
				return WebhookSalida{}, err
			}
			t.RegistrarCambios("webhook_salida", in.ID.String(), map[string]any{"estado": antes.Estado, "intentos": antes.Intentos},
				map[string]any{"estado": f.Estado, "intentos": f.Intentos})
			if err := t.EmitirEnEmpresa(EventoWebhookReenviado, f.EmpresaID, map[string]any{"webhook_id": f.ID, "evento": f.Evento,
				"empresa_id": f.EmpresaID, "intentos_anteriores": antes.Intentos}); err != nil {
				return WebhookSalida{}, err
			}
			return WebhookSalida{ID: f.ID, Evento: f.Evento, EmpresaID: f.EmpresaID, Estado: f.Estado, Intentos: f.Intentos, ProximoIntento: f.ProximoIntento}, nil
		},
	})
}

// visibleEnEmpresa: el webhook es de la empresa activa (o se ven todas); uno sin empresa solo sin
// empresa activa (como pendientes.visible).
func visibleEnEmpresa(ctx context.Context, empresa *int64) bool {
	e := contexto.EmpresasDe(ctx)
	switch e.Estado {
	case contexto.EmpresaNoResuelta:
		return true
	case contexto.EmpresaActiva:
		return empresa != nil && *empresa == contexto.EmpresaDe(ctx)
	}
	return empresa == nil
}

type entradaReenviarHTTP struct {
	nucleo.ConIdempotencia
	ID uuid.UUID `path:"id" doc:"Id del webhook (el del evento, X-Webhook-Id)"`
}

func (m *Modulo) registrarReenviarWebhook(api huma.API) {
	o := op("reenviarWebhookSalida", http.MethodPost, "/webhooks-salida/{id}/reenviar", "Reenviar a Argos un webhook agotado",
		nucleo.PermIntegracionesReenviarWebhook, http.StatusOK, http.StatusNotFound)
	o.Description += " Vuelve a la cola un webhook de Iris hacia Argos que agotó sus reintentos (intentos en cero). Otro estado: 422 `webhook_no_agotado`. " +
		"Auditada; evento `integracion.webhook_reenviado`."
	huma.Register(api, o, func(ctx context.Context, in *entradaReenviarHTTP) (*cuerpo[WebhookSalida], error) {
		return responder(m.ReenviarWebhook.Ejecutar(ctx, EntradaReenviarWebhook{ID: in.ID}))
	})
}

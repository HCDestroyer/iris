package mensajeria

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/conector"
	mensajeriabd "github.com/HCDestroyer/iris/apps/api/internal/mensajeria/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
)

// Webhooks Iris → Argos (contrato 0.6.0 §6.6, forma de 0.6.1). Siguen la regla del outbox: el
// relay del worker entrega aviso.entregado, aviso.fallido y contacto.baja al suscriptor
// «mensajeria.webhooks», que los deja en mensajeria.webhooks_salida con el id del evento (estable:
// X-Webhook-Id = id del cuerpo, Argos deduplica por él). El proceso de mensajería los entrega:
//
//	POST <webhook.url del canje> { id, evento, ocurrido_en, payload }
//	X-Webhook-Id, X-Webhook-Timestamp, X-Webhook-Signature: v1=<hex(HMAC-SHA256(secreto, "<ts>.<cuerpo>"))>
//
// con el secreto de entrada que Argos dio en el canje (§1.3) y reintentos a 1 min, 5 min, 30 min,
// 2 h y 12 h (después, agotado). Solo al conector Argos activo con contrato ≥ 0.6.0 y con receptor;
// si no lo tiene, el webhook queda omitido. Solo los avisos que pidió Argos (origen argos).

// ContratoWebhooks es la primera versión del contrato con los webhooks de mensajería.
const ContratoWebhooks = "0.6.0"

// EsperasWebhook son las esperas entre intentos (§1.3).
var EsperasWebhook = []time.Duration{time.Minute, 5 * time.Minute, 30 * time.Minute, 2 * time.Hour, 12 * time.Hour}

// EventosWebhook son los eventos del outbox que van a Argos.
var EventosWebhook = []string{"aviso.entregado", "aviso.fallido", "contacto.baja"}

// payloadArgos arma el payload del contrato (solo sus campos) desde el del outbox. ok = false si no
// va a Argos.
func payloadArgos(evento string, crudo json.RawMessage) (map[string]any, bool) {
	var p map[string]any
	if json.Unmarshal(crudo, &p) != nil {
		return nil, false
	}
	campos := map[string][]string{
		"aviso.entregado": {"aviso_id", "idempotency_key", "empresa_id", "canal", "entregado_en", "leido_en"},
		"aviso.fallido":   {"aviso_id", "idempotency_key", "empresa_id", "canal", "motivo", "detalle"},
		"contacto.baja":   {"cliente_id", "empresa_id", "canal", "ocurrido_en", "origen"},
	}[evento]
	if campos == nil {
		return nil, false
	}
	if evento != "contacto.baja" && p["origen"] != "argos" {
		return nil, false
	}
	out := make(map[string]any, len(campos))
	for _, c := range campos {
		out[c] = p[c] // ausente = null
	}
	return out, true
}

type entradaProgramarWebhook struct {
	Evento integracion.Evento `json:"-"`
	ID     uuid.UUID          `json:"evento_id"`
	Nombre string             `json:"evento"`
}

// SuscribirWebhooks registra en el bus del relay (cmd/worker) el suscriptor que deja los webhooks
// hacia Argos en mensajeria.webhooks_salida. Idempotente: el mismo evento no se duplica.
func (m *Modulo) SuscribirWebhooks(bus *integracion.Bus, bd nucleo.BD) {
	programar := auditoria.Nuevo(auditoria.Caso[entradaProgramarWebhook, struct{}]{
		Accion: "mensajeria.programar_webhook", Acceso: auditoria.Autenticado,
		AuditarExito: func(struct{}) bool { return false },
		Logica: func(ctx context.Context, t *auditoria.Tx, in entradaProgramarWebhook) (struct{}, error) {
			e := in.Evento
			payload, ok := payloadArgos(e.Evento, e.Payload)
			if !ok {
				return struct{}{}, nil
			}
			crudo, err := json.Marshal(payload)
			if err != nil {
				return struct{}{}, err
			}
			return struct{}{}, mensajeriabd.New(t).InsertarWebhookSalida(ctx, mensajeriabd.InsertarWebhookSalidaParams{
				ID: e.ID, Evento: e.Evento, EmpresaID: e.EmpresaID, Payload: crudo, OcurridoEn: e.OcurridoEn})
		},
	})
	for _, ev := range EventosWebhook {
		bus.Suscribir("mensajeria.webhooks", ev, func(ctx context.Context, e integracion.Evento) error {
			_, err := programar.Ejecutar(ctxSistema(ctx, bd), entradaProgramarWebhook{Evento: e, ID: e.ID, Nombre: e.Evento})
			return err
		})
	}
}

// CuerpoWebhookArgos es el cuerpo que recibe Argos.
type CuerpoWebhookArgos struct {
	ID         string          `json:"id"`
	Evento     string          `json:"evento"`
	OcurridoEn time.Time       `json:"ocurrido_en"`
	Payload    json.RawMessage `json:"payload"`
}

type entradaEntregar struct {
	ID         uuid.UUID `json:"webhook_id"`
	Evento     string    `json:"evento"`
	Estado     string    `json:"estado"`
	Error      *string   `json:"error,omitempty"`
	ConectorID *int64    `json:"conector_id,omitempty"`
	intentosYa int32
}

func nuevoEntregarWebhook(m *Modulo) *auditoria.Caso[entradaEntregar, struct{}] {
	return auditoria.Nuevo(auditoria.Caso[entradaEntregar, struct{}]{
		Accion: "mensajeria.entregar_webhook", Acceso: auditoria.Autenticado,
		// Solo los finales dejan fila (entregado, agotado u omitido); un reintento no.
		AuditarExito: func(struct{}) bool { return false },
		Logica: func(ctx context.Context, t *auditoria.Tx, in entradaEntregar) (struct{}, error) {
			ahora := m.ahora()
			p := mensajeriabd.ResultadoWebhookParams{ID: in.ID, Estado: in.Estado, ProximoIntento: ahora, UltimoError: in.Error, ConectorID: in.ConectorID}
			switch in.Estado {
			case "entregado":
				p.EntregadoEn = &ahora
			case "pendiente":
				i := int(in.intentosYa)
				if i >= len(EsperasWebhook) {
					p.Estado = "agotado"
				} else {
					p.ProximoIntento = ahora.Add(EsperasWebhook[i])
				}
			}
			if p.Estado != "pendiente" {
				t.FijarEntidad("webhook_salida", in.ID)
				t.Anotar(map[string]any{"estado": p.Estado, "evento": in.Evento, "conector_id": in.ConectorID, "error": in.Error, "intentos": in.intentosYa + 1})
				// Fila de auditoría del final (RegistrarCambios fuerza la fila aunque AuditarExito diga que no).
				t.RegistrarCambios("webhook_salida", in.ID, map[string]any{"estado": "pendiente"}, map[string]any{"estado": p.Estado})
			}
			return struct{}{}, mensajeriabd.New(t).ResultadoWebhook(ctx, p)
		},
	})
}

// despacharWebhooks entrega los webhooks vencidos (hasta 50 por corrida).
func (m *Modulo) despacharWebhooks(ctx context.Context, bd nucleo.BD) error {
	q := mensajeriabd.New(bd)
	ids, err := q.WebhooksVencidos(ctx, mensajeriabd.WebhooksVencidosParams{Ahora: m.ahora(), Limite: 50})
	if err != nil {
		return err
	}
	var primero error
	for _, id := range ids {
		if err := m.entregarUno(ctx, bd, id); err != nil && primero == nil {
			primero = err
		}
	}
	return primero
}

// entregarUno toma el webhook (lo reserva 5 min), lo manda fuera de transacción y guarda el
// resultado.
func (m *Modulo) entregarUno(ctx context.Context, bd nucleo.BD, id uuid.UUID) error {
	var w mensajeriabd.MensajeriaWebhooksSalida
	err := nucleo.EnTx(nucleo.ConBD(ctx, bd), func(ctx context.Context, tx pgx.Tx) error {
		q := mensajeriabd.New(tx)
		var err error
		if w, err = q.WebhookParaEnviar(ctx, id); err != nil {
			return err
		}
		return q.ReservarWebhook(ctx, id)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil // otro despacho lo tomó o ya no está pendiente
	}
	if err != nil {
		return err
	}
	in := entradaEntregar{ID: w.ID, Evento: w.Evento, intentosYa: w.Intentos}
	c, err := mensajeriabd.New(bd).ConectorArgosParaWebhooks(ctx)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		// Sin conector activo (apagado o reconectando): se reintenta con las esperas.
		in.Estado, in.Error = "pendiente", texto("sin conector Argos activo")
	case err != nil:
		return err
	case !conector.VersionAlMenos(c.ContratoRemoto, ContratoWebhooks) || c.WebhookSalidaUrl == nil:
		in.Estado, in.ConectorID = "omitido", &c.ID
		in.Error = texto("el conector Argos no habla el contrato " + ContratoWebhooks + " o no informó su receptor de webhooks")
	default:
		in.ConectorID = &c.ID
		if err := m.enviarWebhook(ctx, c, w); err != nil {
			in.Estado, in.Error = "pendiente", texto(recortar(err.Error(), 300))
		} else {
			in.Estado = "entregado"
		}
	}
	_, err = m.entregarWebhook.Ejecutar(ctx, in)
	return err
}

// enviarWebhook firma y manda el webhook a Argos. El secreto nunca sale en errores.
func (m *Modulo) enviarWebhook(ctx context.Context, c mensajeriabd.ConectorArgosParaWebhooksRow, w mensajeriabd.MensajeriaWebhooksSalida) error {
	if m.cfg.Cifrador == nil {
		return errors.New("sin IRIS_CLAVE_CIFRADO para descifrar el secreto del receptor")
	}
	secreto, err := m.cfg.Cifrador.Descifrar(c.WebhookSalidaSecretoCifrado, conector.DatosAsociados(c.ID, conector.CampoWebhookSalida))
	if err != nil {
		return errors.New("no se pudo descifrar el secreto del receptor de Argos")
	}
	defer clear(secreto)
	cuerpo, err := json.Marshal(CuerpoWebhookArgos{ID: w.ID.String(), Evento: w.Evento, OcurridoEn: w.OcurridoEn.UTC(), Payload: w.Payload})
	if err != nil {
		return err
	}
	marca := strconv.FormatInt(m.ahora().Unix(), 10)
	ctx, cancelar := context.WithTimeout(ctx, 10*time.Second)
	defer cancelar()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, *c.WebhookSalidaUrl, bytes.NewReader(cuerpo))
	if err != nil {
		return errors.New("URL del receptor inválida")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Iris/"+m.cfg.Version)
	// X-Webhook-Id = id del cuerpo (0.6.1: si no coinciden, Argos responde 400).
	req.Header.Set("X-Webhook-Id", w.ID.String())
	req.Header.Set("X-Webhook-Timestamp", marca)
	req.Header.Set("X-Webhook-Signature", conector.FirmarWebhook(string(secreto), marca, cuerpo))
	resp, err := m.cfg.HTTPArgos.Do(req)
	if err != nil {
		if conector.EsBloqueado(err) || errors.Is(err, conector.ErrDestinoBloqueado) {
			return errors.New("receptor de Argos en una red interna no permitida (IRIS_ARGOS_REDES_PERMITIDAS)")
		}
		return errors.New("sin respuesta de Argos")
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("respuesta %d de Argos", resp.StatusCode)
	}
	return nil
}

// Jobs del despacho -----------------------------------------------------------------------------

// ArgsDespacharWebhooks entrega los webhooks vencidos (lo encola el reloj del proceso).
type ArgsDespacharWebhooks struct{}

// Kind implementa river.JobArgs.
func (ArgsDespacharWebhooks) Kind() string { return "mensajeria.despachar_webhooks" }

// InsertOpts: uno cada 10 s.
func (ArgsDespacharWebhooks) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: Cola, MaxAttempts: 3, UniqueOpts: river.UniqueOpts{ByPeriod: 10 * time.Second}}
}

type trabajadorDespacho struct {
	river.WorkerDefaults[ArgsDespacharWebhooks]
	m  *Modulo
	bd nucleo.BD
}

func (w *trabajadorDespacho) Work(ctx context.Context, _ *river.Job[ArgsDespacharWebhooks]) error {
	return w.m.despacharWebhooks(ctxSistema(ctx, w.bd), w.bd)
}

package conector

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	conectorbd "github.com/HCDestroyer/iris/apps/api/internal/integracion/conector/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
)

// Webhooks entrantes: POST /api/v1/integraciones/<sistema>/webhook/{conector_id} con cuerpo
// { id, evento, ocurrido_en, payload } (empresa_id en el payload, si aplica) y firma HMAC
// (firma.go). Se verifica la firma (antes que la ventana de tiempo, para no revelar qué
// conectores existen), se guarda en integracion.entrantes y se encola el job del sistema que lo
// procesa, todo en la misma transacción. Responder 2xx significa «guardado»; el efecto ocurre
// en el job.
//
// Deduplicación: por el id del CUERPO, que va firmado. X-Webhook-Id no está firmado, así que
// debe coincidir con él (422 si no): un webhook capturado no se puede reenviar con otro id para
// que se procese de nuevo.
//
// Los 401 (firma inválida o vencida) son de anónimos y no se auditan (cualquiera los provoca
// sin límite): se cuentan y se registran en el log con muestreo (auditarRechazo). El tamaño del
// cuerpo lo limita la ruta (1 MiB).

// MaxCuerpoWebhook es el tamaño máximo del cuerpo de un webhook.
const MaxCuerpoWebhook = 1 << 20

// CuerpoWebhook es el cuerpo que envía el otro sistema (documenta el OpenAPI).
type CuerpoWebhook struct {
	ID         string          `json:"id" minLength:"1" maxLength:"200" doc:"Id estable del webhook (texto o número), igual a X-Webhook-Id; es la clave de deduplicación (va firmado)"`
	Evento     string          `json:"evento" minLength:"1" maxLength:"100" doc:"Argos: cliente.actualizado, servicio.estado_cambiado…; Horus: horus.router_estado_cambiado, horus.hallazgo_abierto…"`
	OcurridoEn string          `json:"ocurrido_en,omitempty" doc:"RFC 3339. Si falta o no se entiende, se usa la hora de recepción"`
	Payload    json.RawMessage `json:"payload" doc:"Datos del evento; lleva empresa_id cuando aplica"`
}

// cuerpoCrudo lee el cuerpo con tolerancia: id texto o número, ocurrido_en cualquier cosa.
type cuerpoCrudo struct {
	ID         json.RawMessage `json:"id"`
	Evento     string          `json:"evento"`
	OcurridoEn json.RawMessage `json:"ocurrido_en"`
	Payload    json.RawMessage `json:"payload"`
}

// idWebhook devuelve el id como texto ("" si falta o no es texto ni número).
func (c cuerpoCrudo) idWebhook() string {
	var t Texto
	if len(c.ID) == 0 || json.Unmarshal(c.ID, &t) != nil {
		return ""
	}
	return string(t)
}

// ocurrido devuelve ocurrido_en si es RFC 3339; si no, la hora de recepción.
func (c cuerpoCrudo) ocurrido(recibido time.Time) time.Time {
	var s string
	if json.Unmarshal(c.OcurridoEn, &s) == nil {
		if t, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(s)); err == nil {
			return t.UTC()
		}
	}
	return recibido.UTC()
}

// EntradaWebhook es la entrada de integraciones.webhook (cabeceras y cuerpo crudo).
type EntradaWebhook struct {
	ConectorID int64  `json:"conector_id"`
	WebhookID  string `json:"webhook_id"`
	Marca      string `json:"timestamp"`
	Firma      string `json:"-"`
	Cuerpo     []byte `json:"-"`
}

// WebhookRecibido es la respuesta al emisor.
type WebhookRecibido struct {
	EntranteID int64 `json:"entranteId"`
	Duplicado  bool  `json:"duplicado" doc:"true: ese id de webhook ya se había recibido; no se vuelve a procesar"`
}

func firmaInvalida(detalle string) *nucleo.Problema {
	return nucleo.NuevoProblema(http.StatusUnauthorized, "firma_invalida", "Firma inválida", detalle)
}

// EmpresaDelPayload lee empresa_id del payload (nil si no viene o no es un id).
func EmpresaDelPayload(payload []byte) *int64 {
	var p struct {
		EmpresaID *Numero `json:"empresa_id"`
	}
	if Decodificar(payload, &p) != nil {
		return nil
	}
	if id := p.EmpresaID.Entero(); id != nil && *id > 0 {
		return id
	}
	return nil
}

func nuevoRecibirWebhook(c *Conexion) *auditoria.Caso[EntradaWebhook, WebhookRecibido] {
	return auditoria.Nuevo(auditoria.Caso[EntradaWebhook, WebhookRecibido]{
		Accion: "integraciones.webhook", Acceso: auditoria.Publico,
		// Los reintentos del emisor (duplicados) no dejan otra fila; los 401 anónimos tampoco.
		AuditarExito: func(r WebhookRecibido) bool { return !r.Duplicado },
		AuditarError: c.n.auditarRechazo("integraciones.webhook"),
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaWebhook) (WebhookRecibido, error) {
			q := conectorbd.New(t)
			t.FijarEmpresa(0)
			t.FijarEntidad("conector", in.ConectorID)
			if in.WebhookID == "" || in.Marca == "" || in.Firma == "" {
				return WebhookRecibido{}, firmaInvalida("Faltan X-Webhook-Id, X-Webhook-Timestamp o X-Webhook-Signature")
			}
			f, err := q.ConectorPorID(ctx, in.ConectorID)
			if esSinFilas(err) || (err == nil && f.Sistema != c.s.Nombre) {
				// Mismo error que una firma mala: no revela qué conectores existen.
				return WebhookRecibido{}, firmaInvalida("La firma no corresponde al conector")
			}
			if err != nil {
				return WebhookRecibido{}, err
			}
			l, err := c.n.Leer(f)
			if err != nil {
				return WebhookRecibido{}, err
			}
			// Primero la firma: con la ventana antes, un timestamp viejo daría firma_vencida solo
			// en los conectores que existen (enumeración).
			if !FirmaValida(l.secreto, in.Firma, in.Marca, in.Cuerpo) {
				return WebhookRecibido{}, firmaInvalida("La firma no corresponde al conector")
			}
			recibido := time.Now()
			if !MarcaEnVentana(in.Marca, recibido, c.n.cfg.VentanaWebhook) {
				return WebhookRecibido{}, nucleo.NuevoProblema(http.StatusUnauthorized, "firma_vencida", "Firma fuera de ventana",
					"X-Webhook-Timestamp está fuera de la ventana aceptada")
			}
			t.IdentificarActor(ActorDe(c.s.Nombre, f.ID))
			if f.Estado != "activo" {
				// 503: el emisor reintenta y el webhook entra cuando se active el conector.
				return WebhookRecibido{}, c.Apagado().ConCabecera("Retry-After", "300")
			}
			if len(in.WebhookID) > 200 {
				return WebhookRecibido{}, nucleo.Validacion(nucleo.ErrorCampo{Field: "X-Webhook-Id", Message: "Máximo 200 caracteres", Rule: "maxLength"})
			}
			var cuerpo cuerpoCrudo
			if err := json.Unmarshal(in.Cuerpo, &cuerpo); err != nil || strings.TrimSpace(cuerpo.Evento) == "" {
				return WebhookRecibido{}, nucleo.Validacion(nucleo.ErrorCampo{Field: "evento", Message: "El cuerpo debe ser { id, evento, ocurrido_en, payload }", Rule: "formato"})
			}
			if len(cuerpo.Evento) > 100 {
				return WebhookRecibido{}, nucleo.Validacion(nucleo.ErrorCampo{Field: "evento", Message: "Máximo 100 caracteres", Rule: "maxLength"})
			}
			// El id firmado es la clave de deduplicación; la cabecera (sin firmar) debe decir lo mismo.
			webhookID := cuerpo.idWebhook()
			switch {
			case webhookID == "":
				return WebhookRecibido{}, nucleo.Validacion(nucleo.ErrorCampo{Field: "id", Message: "El cuerpo debe traer el id del webhook", Rule: "requerido"})
			case len(webhookID) > 200:
				return WebhookRecibido{}, nucleo.Validacion(nucleo.ErrorCampo{Field: "id", Message: "Máximo 200 caracteres", Rule: "maxLength"})
			case webhookID != strings.TrimSpace(in.WebhookID):
				return WebhookRecibido{}, nucleo.Validacion(nucleo.ErrorCampo{Field: "id", Message: "No coincide con X-Webhook-Id", Rule: "igual"})
			}
			ocurrido := cuerpo.ocurrido(recibido)
			payload := cuerpo.Payload
			if len(bytes.TrimSpace(payload)) == 0 || string(bytes.TrimSpace(payload)) == "null" {
				payload = json.RawMessage("{}")
			}
			empresa := EmpresaDelPayload(payload)
			if empresa != nil {
				t.FijarEmpresa(*empresa)
			}
			t.Anotar(map[string]any{"evento": cuerpo.Evento})
			nuevo, err := q.InsertarEntrante(ctx, conectorbd.InsertarEntranteParams{
				ConectorID: f.ID, WebhookID: webhookID, Evento: cuerpo.Evento, EmpresaID: empresa,
				Payload: payload, OcurridoEn: &ocurrido, RequestID: contexto.RequestIDDe(ctx),
			})
			if esSinFilas(err) {
				previo, err := q.EntrantePorWebhook(ctx, conectorbd.EntrantePorWebhookParams{ConectorID: f.ID, WebhookID: webhookID})
				if err != nil {
					return WebhookRecibido{}, err
				}
				t.Anotar(map[string]any{"duplicado": true, "entrante_id": previo.ID})
				return WebhookRecibido{EntranteID: previo.ID, Duplicado: true}, nil
			}
			if err != nil {
				return WebhookRecibido{}, err
			}
			t.Anotar(map[string]any{"entrante_id": nuevo.ID})
			if err := q.TocarWebhookConector(ctx, f.ID); err != nil {
				return WebhookRecibido{}, err
			}
			if c.n.cfg.Encolar != nil {
				if err := c.n.cfg.Encolar(ctx, t.Tx, c.s.TrabajoEntrante(nuevo.ID)); err != nil {
					return WebhookRecibido{}, err
				}
			}
			return WebhookRecibido{EntranteID: nuevo.ID}, nil
		},
	})
}

// Procesar un entrante (ayudantes para el job de cada sistema) ------------------------------

// Entrante es un webhook guardado.
type Entrante = conectorbd.IntegracionEntrante

// LeerEntrante lee un entrante sin bloquearlo (fase previa del job, fuera de transacción).
func LeerEntrante(ctx context.Context, q conectorbd.DBTX, id int64) (Entrante, error) {
	return conectorbd.New(q).EntrantePorID(ctx, id)
}

// TomarEntrante bloquea el entrante dentro de la transacción del caso de uso que lo aplica y
// prepara la auditoría: entidad, empresa y, si el caso falla, el error en el entrante (AlFallar,
// con la causa que deje *causa). procesado = true si ya se había procesado (idempotencia).
func TomarEntrante(ctx context.Context, t *auditoria.Tx, id int64, causa *string) (e Entrante, procesado bool, err error) {
	t.FijarEntidad("entrante", id)
	t.AlFallar(func(ctx context.Context, tx pgx.Tx) error {
		return conectorbd.New(tx).MarcarEntranteError(ctx, conectorbd.MarcarEntranteErrorParams{ID: id, Error: causa})
	})
	e, err = conectorbd.New(t).EntranteParaProcesar(ctx, id)
	if err != nil {
		return e, false, err
	}
	if e.ProcesadoEn != nil {
		return e, true, nil
	}
	if e.EmpresaID != nil {
		t.FijarEmpresa(*e.EmpresaID)
	} else {
		t.FijarEmpresa(0)
	}
	t.Anotar(map[string]any{"evento": e.Evento, "webhook_id": e.WebhookID})
	return e, false, nil
}

// MarcarEntrante deja el entrante procesado con su resultado (aplicado | ignorado) y, si se
// ignoró por un motivo (payload inválido…), la nota.
func MarcarEntrante(ctx context.Context, t *auditoria.Tx, id int64, resultado string, nota *string) error {
	return conectorbd.New(t).MarcarEntranteProcesado(ctx, conectorbd.MarcarEntranteProcesadoParams{ID: id, Resultado: &resultado, Nota: nota})
}

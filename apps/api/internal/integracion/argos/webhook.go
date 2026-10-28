package argos

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	argosbd "github.com/HCDestroyer/iris/apps/api/internal/integracion/argos/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
)

// Webhooks Argos → Iris (§2.3): POST /api/v1/integraciones/argosisp/webhook/{conector_id} con
// cuerpo { id, evento, ocurrido_en, payload } (empresa_id en el payload) y firma HMAC (firma.go).
// Se verifica la firma, se guarda en integracion.entrantes (deduplicado por X-Webhook-Id) y se
// encola el job que lo procesa, todo en la misma transacción. Responder 2xx significa «guardado»;
// el efecto (refrescar la instantánea) ocurre en el job.

// CuerpoWebhook es el cuerpo que envía Argos (documenta el OpenAPI).
type CuerpoWebhook struct {
	ID         string          `json:"id" maxLength:"200" doc:"Id estable del webhook (igual a X-Webhook-Id)"`
	Evento     string          `json:"evento" minLength:"1" maxLength:"100" doc:"cliente.actualizado, servicio.creado, servicio.config_cambiada, asignacion.cambiada, servicio.estado_cambiado, …"`
	OcurridoEn *time.Time      `json:"ocurrido_en,omitempty"`
	Payload    json.RawMessage `json:"payload" doc:"Datos del evento; lleva empresa_id"`
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
	Duplicado  bool  `json:"duplicado" doc:"true: ese X-Webhook-Id ya se había recibido; no se vuelve a procesar"`
}

func firmaInvalida(detalle string) *nucleo.Problema {
	return nucleo.NuevoProblema(http.StatusUnauthorized, "firma_invalida", "Firma inválida", detalle)
}

func nuevoRecibirWebhook(m *Modulo) *auditoria.Caso[EntradaWebhook, WebhookRecibido] {
	return auditoria.Nuevo(auditoria.Caso[EntradaWebhook, WebhookRecibido]{
		Accion: "integraciones.webhook", Acceso: auditoria.Publico,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaWebhook) (WebhookRecibido, error) {
			q := argosbd.New(t)
			t.FijarEmpresa(0)
			t.FijarEntidad("conector", in.ConectorID)
			if in.WebhookID == "" || in.Marca == "" || in.Firma == "" {
				return WebhookRecibido{}, firmaInvalida("Faltan X-Webhook-Id, X-Webhook-Timestamp o X-Webhook-Signature")
			}
			f, err := q.ConectorPorID(ctx, in.ConectorID)
			if errors.Is(err, pgx.ErrNoRows) || (err == nil && f.Sistema != Sistema) {
				// Mismo error que una firma mala: no revela qué conectores existen.
				return WebhookRecibido{}, firmaInvalida("La firma no corresponde al conector")
			}
			if err != nil {
				return WebhookRecibido{}, err
			}
			c, err := m.leerConector(f)
			if err != nil {
				return WebhookRecibido{}, err
			}
			if !marcaEnVentana(in.Marca, time.Now(), m.cfg.VentanaWebhook) {
				return WebhookRecibido{}, nucleo.NuevoProblema(http.StatusUnauthorized, "firma_vencida", "Firma fuera de ventana",
					"X-Webhook-Timestamp está fuera de la ventana aceptada")
			}
			if !firmaValida(c.secreto, in.Firma, in.Marca, in.Cuerpo) {
				return WebhookRecibido{}, firmaInvalida("La firma no corresponde al conector")
			}
			t.IdentificarActor(actorArgos(f.ID))
			if f.Estado != "activo" {
				// 503: Argos reintenta y el webhook entra cuando se active el conector.
				return WebhookRecibido{}, ConectorApagado().ConCabecera("Retry-After", "300")
			}
			if len(in.WebhookID) > 200 {
				return WebhookRecibido{}, nucleo.Validacion(nucleo.ErrorCampo{Field: "X-Webhook-Id", Message: "Máximo 200 caracteres", Rule: "maxLength"})
			}
			var cuerpo CuerpoWebhook
			if err := json.Unmarshal(in.Cuerpo, &cuerpo); err != nil || strings.TrimSpace(cuerpo.Evento) == "" {
				return WebhookRecibido{}, nucleo.Validacion(nucleo.ErrorCampo{Field: "evento", Message: "El cuerpo debe ser { id, evento, ocurrido_en, payload }", Rule: "formato"})
			}
			if cuerpo.ID != "" && cuerpo.ID != in.WebhookID {
				return WebhookRecibido{}, nucleo.Validacion(nucleo.ErrorCampo{Field: "id", Message: "No coincide con X-Webhook-Id", Rule: "igual"})
			}
			payload := cuerpo.Payload
			if len(bytes.TrimSpace(payload)) == 0 || string(bytes.TrimSpace(payload)) == "null" {
				payload = json.RawMessage("{}")
			}
			datos := leerPayload(payload)
			if datos.EmpresaID != nil {
				t.FijarEmpresa(*datos.EmpresaID)
			}
			t.Anotar(map[string]any{"evento": cuerpo.Evento})
			nuevo, err := q.InsertarEntrante(ctx, argosbd.InsertarEntranteParams{
				ConectorID: f.ID, WebhookID: in.WebhookID, Evento: cuerpo.Evento, EmpresaID: datos.EmpresaID,
				Payload: payload, OcurridoEn: cuerpo.OcurridoEn, RequestID: requestID(ctx),
			})
			if errors.Is(err, pgx.ErrNoRows) {
				previo, err := q.EntrantePorWebhook(ctx, argosbd.EntrantePorWebhookParams{ConectorID: f.ID, WebhookID: in.WebhookID})
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
			if m.cfg.Encolar != nil {
				if err := m.cfg.Encolar(ctx, t.Tx, ArgsProcesarEntrante{EntranteID: nuevo.ID}); err != nil {
					return WebhookRecibido{}, err
				}
			}
			return WebhookRecibido{EntranteID: nuevo.ID}, nil
		},
	})
}

// payloadEntrante son los datos que Iris lee del payload de un webhook. Supuesto: ids planos
// (cliente_id, servicio_id) o anidados ({ cliente: { id } }, { servicio: { id, cliente_id } }).
type payloadEntrante struct {
	EmpresaID  *int64 `json:"empresa_id"`
	ClienteID  *int64 `json:"cliente_id"`
	ServicioID *int64 `json:"servicio_id"`
	ID         *int64 `json:"id"`
	Cliente    *Ref   `json:"cliente"`
	Servicio   *struct {
		ID        *int64 `json:"id"`
		ClienteID *int64 `json:"cliente_id"`
	} `json:"servicio"`
	De string `json:"de"`
	A  string `json:"a"`
}

func leerPayload(crudo []byte) payloadEntrante {
	var p payloadEntrante
	_ = decodificar(crudo, &p)
	return p
}

func (p payloadEntrante) cliente() *int64 {
	switch {
	case p.ClienteID != nil:
		return p.ClienteID
	case p.Cliente != nil && p.Cliente.ID != nil:
		return p.Cliente.ID
	case p.Servicio != nil && p.Servicio.ClienteID != nil:
		return p.Servicio.ClienteID
	}
	return nil
}

func (p payloadEntrante) servicio() *int64 {
	switch {
	case p.ServicioID != nil:
		return p.ServicioID
	case p.Servicio != nil && p.Servicio.ID != nil:
		return p.Servicio.ID
	}
	return p.ID
}

// Eventos que refrescan la instantánea (§2.3).
const (
	EventoClienteActualizado     = "cliente.actualizado"
	EventoServicioCreado         = "servicio.creado"
	EventoServicioConfigCambiada = "servicio.config_cambiada"
	EventoAsignacionCambiada     = "asignacion.cambiada"
	EventoServicioEstadoCambiado = "servicio.estado_cambiado"
)

// EventoServicioRetirado es el evento interno (outbox) que emite Iris cuando Argos avisa que un
// servicio pasó a retirado: gancho para que I2 proponga la orden de retiro (Fase 2).
const EventoServicioRetirado = "argos.servicio_retirado"

// Procesar un entrante (job) ---------------------------------------------------------------

// ResultadoEntrante dice qué se hizo con un webhook.
type ResultadoEntrante struct {
	EntranteID int64  `json:"entrante_id"`
	Resultado  string `json:"resultado"` // aplicado | ignorado | ya_procesado
}

type entradaAplicar struct {
	EntranteID int64  `json:"entrante_id"`
	ClienteID  *int64 `json:"cliente_id,omitempty"`
	ficha      *fichaArgos
	borrar     bool
	errArgos   error
}

// ProcesarEntrante aplica un webhook guardado: lee de Argos lo que haga falta (fuera de
// transacción) y en un caso de uso refresca la instantánea, emite los eventos internos y marca
// el entrante procesado. Es idempotente: un entrante procesado no se vuelve a aplicar y el
// refresco es un upsert con la hora de lectura.
//
// Solo se refrescan clientes que ya están en la instantánea (los que Iris mostró alguna vez):
// el resto se marca «ignorado» y se leerá al abrir su ficha. Los eventos que aún no tienen
// efecto en Iris (inventario.equipo_por_recuperar, sesion.terminada…; Fase 2) quedan guardados
// como «ignorado».
type ProcesarEntrante struct {
	m       *Modulo
	aplicar *auditoria.Caso[entradaAplicar, ResultadoEntrante]
}

func nuevoProcesarEntrante(m *Modulo) *ProcesarEntrante {
	p := &ProcesarEntrante{m: m}
	p.aplicar = auditoria.Nuevo(auditoria.Caso[entradaAplicar, ResultadoEntrante]{
		Accion: "integraciones.procesar_webhook", Acceso: auditoria.Autenticado,
		AuditarExito: func(r ResultadoEntrante) bool { return r.Resultado != "ya_procesado" },
		Logica: func(ctx context.Context, t *auditoria.Tx, in entradaAplicar) (ResultadoEntrante, error) {
			var causa string
			t.AlFallar(func(ctx context.Context, tx pgx.Tx) error {
				return argosbd.New(tx).MarcarEntranteError(ctx, argosbd.MarcarEntranteErrorParams{ID: in.EntranteID, Error: &causa})
			})
			r, err := p.aplicarEn(ctx, t, in)
			if err != nil {
				causa = err.Error()
			}
			return r, err
		},
	})
	return p
}

func (p *ProcesarEntrante) aplicarEn(ctx context.Context, t *auditoria.Tx, in entradaAplicar) (ResultadoEntrante, error) {
	q := argosbd.New(t)
	t.FijarEntidad("entrante", in.EntranteID)
	r := ResultadoEntrante{EntranteID: in.EntranteID}
	e, err := q.EntranteParaProcesar(ctx, in.EntranteID)
	if err != nil {
		return r, err
	}
	if e.ProcesadoEn != nil {
		r.Resultado = "ya_procesado"
		return r, nil
	}
	if e.EmpresaID != nil {
		t.FijarEmpresa(*e.EmpresaID)
	} else {
		t.FijarEmpresa(0)
	}
	t.Anotar(map[string]any{"evento": e.Evento, "webhook_id": e.WebhookID})
	if in.errArgos != nil {
		return r, in.errArgos
	}
	r.Resultado = "ignorado"
	switch {
	case in.borrar && in.ClienteID != nil:
		if err := q.BorrarCliente(ctx, *in.ClienteID); err != nil {
			return r, err
		}
		r.Resultado = "aplicado"
	case in.ficha != nil:
		if err := guardarFicha(ctx, q, *in.ficha); err != nil {
			return r, err
		}
		r.Resultado = "aplicado"
	}
	if e.Evento == EventoServicioEstadoCambiado {
		datos := leerPayload(e.Payload)
		if strings.EqualFold(datos.A, "retirado") {
			if err := t.Emitir(EventoServicioRetirado, map[string]any{
				"servicio_id": datos.servicio(), "cliente_id": datos.cliente(), "de": datos.De, "a": datos.A,
				"entrante_id": e.ID,
			}); err != nil {
				return r, err
			}
			r.Resultado = "aplicado"
		}
	}
	return r, q.MarcarEntranteProcesado(ctx, argosbd.MarcarEntranteProcesadoParams{ID: e.ID, Resultado: &r.Resultado})
}

// Ejecutar procesa el entrante. ctx necesita la base (nucleo.ConBD) y un actor (el worker usa
// el actor sistema).
func (p *ProcesarEntrante) Ejecutar(ctx context.Context, entranteID int64) (ResultadoEntrante, error) {
	bd := nucleo.BDDe(ctx)
	if bd == nil {
		return ResultadoEntrante{}, nucleo.ErrSinBD
	}
	q := argosbd.New(bd)
	e, err := q.EntrantePorID(ctx, entranteID)
	if err != nil {
		return ResultadoEntrante{}, err
	}
	in := entradaAplicar{EntranteID: e.ID}
	if e.ProcesadoEn != nil {
		return ResultadoEntrante{EntranteID: e.ID, Resultado: "ya_procesado"}, nil
	}
	clienteID, err := clienteARefrescar(ctx, q, e)
	if err != nil {
		return ResultadoEntrante{}, err
	}
	if clienteID != nil {
		in.ClienteID = clienteID
		in.ficha, in.borrar, in.errArgos = p.leerDeArgos(ctx, q, e.ConectorID, *clienteID)
	}
	return p.aplicar.Ejecutar(ctx, in)
}

// clienteARefrescar decide qué cliente refrescar: el del evento, si ya está en la instantánea.
func clienteARefrescar(ctx context.Context, q *argosbd.Queries, e argosbd.IntegracionEntrante) (*int64, error) {
	datos := leerPayload(e.Payload)
	var clienteID *int64
	switch e.Evento {
	case EventoClienteActualizado:
		clienteID = datos.cliente()
		if clienteID == nil {
			clienteID = datos.ID
		}
	case EventoServicioCreado, EventoServicioConfigCambiada, EventoAsignacionCambiada, EventoServicioEstadoCambiado:
		clienteID = datos.cliente()
		if s := datos.servicio(); clienteID == nil && s != nil {
			srv, err := q.ServicioPorID(ctx, *s)
			if err == nil {
				clienteID = &srv.ClienteID
			} else if !errors.Is(err, pgx.ErrNoRows) {
				return nil, err
			}
		}
	default:
		return nil, nil
	}
	if clienteID == nil {
		return nil, nil
	}
	if _, err := q.ClientePorID(ctx, *clienteID); errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	return clienteID, nil
}

func (p *ProcesarEntrante) leerDeArgos(ctx context.Context, q *argosbd.Queries, conectorID, clienteID int64) (*fichaArgos, bool, error) {
	f, err := q.ConectorPorID(ctx, conectorID)
	if err != nil {
		return nil, false, err
	}
	if f.Estado != "activo" {
		return nil, false, ConectorApagado()
	}
	c, err := p.m.leerConector(f)
	if err != nil {
		return nil, false, err
	}
	ficha, err := leerFichaArgos(ctx, p.m.clienteDe(c), clienteID)
	if EsNoEncontrado(err) {
		return nil, true, nil
	}
	if err != nil {
		return nil, false, err
	}
	return &ficha, false, nil
}

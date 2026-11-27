package mensajeria

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
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

// ContratoCuadrillas es la primera versión del contrato con cuadrilla.creada y
// cuadrilla.actualizada (0.11.0 §3.9 y §6.6, bodega de cuadrilla).
const ContratoCuadrillas = "0.11.0"

// Motivos de un webhook omitido (el prefijo de ultimo_error; pendientes los lee en depende_de).
const (
	MotivoSinReceptor        = "sin_receptor"
	MotivoOtroConector       = "otro_conector"
	MotivoEmpresaNoPermitida = "empresa_no_permitida"
)

// TopeEsperaDesvinculado: un vinculado no espera más que esto el 2xx del desvinculado anterior;
// después queda agotado a la vista (se reenvía a mano).
var TopeEsperaDesvinculado = 24 * time.Hour

// ContratoVinculos es la primera versión del contrato con tecnico.argos_vinculado y
// tecnico.argos_desvinculado (0.14.0 §6.6).
const ContratoVinculos = "0.14.0"

// EsperasWebhook son las esperas entre intentos (§1.3).
var EsperasWebhook = []time.Duration{time.Minute, 5 * time.Minute, 30 * time.Minute, 2 * time.Hour, 12 * time.Hour}

// EventosWebhook son los eventos del outbox que van a Argos.
var EventosWebhook = []string{"aviso.entregado", "aviso.fallido", "contacto.baja", "prospecto.estado_cambiado",
	"ticket.estado_cambiado", "ticket.respondido", "solicitud_servicio.estado_cambiado",
	"cuadrilla.creada", "cuadrilla.actualizada", eventoVinculado, eventoDesvinculado, eventoCorteManual}

const (
	eventoVinculado    = "tecnico.argos_vinculado"
	eventoDesvinculado = "tecnico.argos_desvinculado"
	// eventoCorteManual: el estado del corte manual del cable (0.15.0 §13.2, B34; lo emite campo).
	eventoCorteManual = "corte_manual.estado_cambiado"
)

// contratoMinimoEvento: los eventos que Argos solo recibe con un contrato más nuevo que
// ContratoWebhooks (§6.6). Con uno anterior, el webhook queda omitido.
var contratoMinimoEvento = map[string]string{
	// 0.10.0 §3.6: solo de prospectos registrados desde el panel de Argos, sin datos personales.
	"prospecto.estado_cambiado": "0.10.0",
	// 0.9.0 §6.6: tickets (de cualquier origen, con cliente) y solicitudes de servicio del portal.
	"ticket.estado_cambiado":             "0.9.0",
	"ticket.respondido":                  "0.9.0",
	"solicitud_servicio.estado_cambiado": "0.9.0",
	// 0.11.0 §3.9 y §6.6: bodega de cuadrilla.
	"cuadrilla.creada":      ContratoCuadrillas,
	"cuadrilla.actualizada": ContratoCuadrillas,
	// 0.14.0 §6.6: vínculo de cada técnico.
	eventoVinculado:    ContratoVinculos,
	eventoDesvinculado: ContratoVinculos,
	// 0.15.0 §6.6 y §13.2: corte manual de respaldo.
	eventoCorteManual: "0.15.0",
}

// conAlcance: los eventos que Argos recibe solo si su empresa está en el alcance del conector (§6.6:
// tickets y solicitudes de clientes dentro del alcance).
var conAlcance = map[string]bool{"ticket.estado_cambiado": true, "ticket.respondido": true, "solicitud_servicio.estado_cambiado": true,
	// 0.15.0 §13.2: el corte manual solo a la empresa del alcance (y solo al conector que lo pidió, abajo).
	eventoCorteManual: true}

// ContratoMinimo devuelve la versión del contrato con que Argos recibe el evento.
func ContratoMinimo(evento string) string {
	if v, ok := contratoMinimoEvento[evento]; ok {
		return v
	}
	return ContratoWebhooks
}

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
		// motivo_codigo: Iris no tiene códigos de motivo en el embudo (el motivo es texto de una
		// persona y nunca viaja): siempre null.
		"prospecto.estado_cambiado": {"prospecto_id", "empresa_id", "de", "a", "motivo_codigo"},
		// 0.9.0: nunca el cuerpo, notas internas, un motivo escrito por una persona ni datos de otro cliente.
		"ticket.estado_cambiado":             {"ticket_id", "numero", "empresa_id", "cliente_id", "servicio_id", "origen", "de", "a", "puede_confirmar"},
		"ticket.respondido":                  {"ticket_id", "numero", "empresa_id", "cliente_id", "mensaje_id", "autor_tipo", "masivo", "aviso_por_correo"},
		"solicitud_servicio.estado_cambiado": {"solicitud_id", "numero", "empresa_id", "cliente_id", "tipo", "servicio_id", "de", "a"},
		// 0.11.0 §6.6: uno por empresa, con los miembros completos (id_externo null sin vínculo I1).
		"cuadrilla.creada":      {"cuadrilla_id", "empresa_id", "version", "nombre", "activa", "miembros", "jefe_usuario_id"},
		"cuadrilla.actualizada": {"cuadrilla_id", "empresa_id", "version", "nombre", "activa", "miembros", "jefe_usuario_id"},
		// 0.14.0 §6.6: el conector de origen y la carga no viajan (van en su columna y en el outbox).
		eventoVinculado:    {"tecnico_id", "usuario_id", "empresa_id", "version", "id_externo", "anterior_id_externo"},
		eventoDesvinculado: {"tecnico_id", "usuario_id", "empresa_id", "version", "id_externo", "motivo"},
		// 0.15.0 §6.6: version es el contador de Iris por solicitud_id; nunca la orden interna ni el servicio.
		eventoCorteManual: {"solicitud_id", "empresa_id", "version", "estado", "motivo_codigo", "referencia", "referencia_tipo", "ocurrido_en"},
	}[evento]
	if campos == nil {
		return nil, false
	}
	switch evento {
	case "contacto.baja":
	case "ticket.estado_cambiado", "ticket.respondido", "solicitud_servicio.estado_cambiado":
		// Solo de tickets y solicitudes con cliente y empresa (el alcance se mira al entregar).
		if p["cliente_id"] == nil || p["empresa_id"] == nil {
			return nil, false
		}
		if evento == "ticket.estado_cambiado" {
			p["puede_confirmar"] = p["a"] == "resuelto"
		}
	case "prospecto.estado_cambiado":
		if p["origen"] != "argos_panel" {
			return nil, false
		}
	case "cuadrilla.creada", "cuadrilla.actualizada", eventoVinculado, eventoDesvinculado, eventoCorteManual:
		// Sin empresa (modo sin conector) no hay bodega ni vínculo en Argos.
		if p["empresa_id"] == nil {
			return nil, false
		}
	default:
		if p["origen"] != "argos" {
			return nil, false
		}
	}
	out := make(map[string]any, len(campos))
	for _, c := range campos {
		out[c] = p[c] // ausente = null
	}
	return out, true
}

// empresaDelPayload: empresa_id del payload que va a Argos (0 si falta).
func empresaDelPayload(crudo []byte) int64 {
	var p struct {
		EmpresaID *int64 `json:"empresa_id"`
	}
	if json.Unmarshal(crudo, &p) != nil || p.EmpresaID == nil {
		return 0
	}
	return *p.EmpresaID
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
			origen := vinculoDe(e.Evento, e.Payload)
			return struct{}{}, mensajeriabd.New(t).InsertarWebhookSalida(ctx, mensajeriabd.InsertarWebhookSalidaParams{
				ID: e.ID, Evento: e.Evento, EmpresaID: e.EmpresaID, Payload: crudo, OcurridoEn: e.OcurridoEn, ProximoIntento: m.ahora(),
				ConectorOrigen: origen.ConectorID, CargaID: origen.carga()})
		},
	})
	for _, ev := range EventosWebhook {
		bus.Suscribir("mensajeria.webhooks", ev, func(ctx context.Context, e integracion.Evento) error {
			_, err := programar.Ejecutar(ctxSistema(ctx, bd), entradaProgramarWebhook{Evento: e, ID: e.ID, Nombre: e.Evento})
			return err
		})
	}
}

// origenVinculo: el conector del que salió el vínculo de un tecnico.argos_* o el corte manual (solo a él se manda) y
// la carga inicial a la que pertenece (0.14.0 §6.6). Ninguno de los dos viaja a Argos.
type origenVinculo struct {
	ConectorID *int64     `json:"conector_id"`
	CargaID    *uuid.UUID `json:"carga_id"`
}

func (o origenVinculo) carga() pgtype.UUID {
	if o.CargaID == nil {
		return pgtype.UUID{}
	}
	return pgtype.UUID{Bytes: *o.CargaID, Valid: true}
}

func vinculoDe(evento string, crudo json.RawMessage) origenVinculo {
	var o origenVinculo
	if evento != eventoVinculado && evento != eventoDesvinculado && evento != eventoCorteManual {
		return o
	}
	if json.Unmarshal(crudo, &o) != nil {
		return origenVinculo{}
	}
	return o
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
		return q.ReservarWebhook(ctx, mensajeriabd.ReservarWebhookParams{ID: id, Hasta: m.ahora().Add(5 * time.Minute)})
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
	case !conector.VersionAlMenos(conector.ContratoAcordado(c.ContratoRemoto, c.ContratoInformado), ContratoMinimo(w.Evento)) || c.WebhookSalidaUrl == nil:
		in.Estado, in.ConectorID = "omitido", &c.ID
		in.Error = texto(MotivoSinReceptor + ": el conector Argos no habla el contrato " + ContratoMinimo(w.Evento) + " o no informó su receptor de webhooks")
	case conAlcance[w.Evento] && !c.AlcanceGlobal && !slices.Contains(c.Empresas, empresaDelPayload(w.Payload)):
		in.Estado, in.ConectorID = "omitido", &c.ID
		in.Error = texto(MotivoEmpresaNoPermitida + ": la empresa del evento no está en el alcance del conector Argos")
	case w.Evento == eventoCorteManual && (w.ConectorOrigen == nil || *w.ConectorOrigen != c.ID):
		// 0.15.0 §13.2: el estado vuelve solo al conector que pidió el corte (uno reemplazado no la conoce).
		in.Estado, in.ConectorID = "omitido", &c.ID
		in.Error = texto(MotivoOtroConector + ": la solicitud de corte no salió de este conector Argos")
	case (w.Evento == eventoVinculado || w.Evento == eventoDesvinculado) && (w.ConectorOrigen == nil || *w.ConectorOrigen != c.ID):
		// 0.14.0 §6.6: solo al conector del que salió el id_externo.
		in.Estado, in.ConectorID = "omitido", &c.ID
		in.Error = texto(MotivoOtroConector + ": el vínculo del técnico no salió de este conector Argos")
	case (w.Evento == eventoVinculado || w.Evento == eventoDesvinculado) && !c.AlcanceGlobal &&
		(w.EmpresaID == nil || !slices.Contains(c.Empresas, *w.EmpresaID)):
		in.Estado, in.ConectorID = "omitido", &c.ID
		in.Error = texto(MotivoEmpresaNoPermitida + ": la empresa no está en el alcance del conector Argos")
	case w.Evento == eventoVinculado:
		// El vinculado espera el 2xx del desvinculado anterior del mismo id_externo (§6.6).
		var p struct {
			IDExterno string `json:"id_externo"`
			UsuarioID int64  `json:"usuario_id"`
			Version   int32  `json:"version"`
		}
		_ = json.Unmarshal(w.Payload, &p)
		q := mensajeriabd.New(bd)
		fallido, err := q.DesvinculadoAnteriorFallido(ctx, mensajeriabd.DesvinculadoAnteriorFallidoParams{
			IDExterno: p.IDExterno, Antes: w.CreatedAt, UsuarioID: p.UsuarioID, Version: p.Version, EventoID: w.ID})
		if err != nil {
			return err
		}
		if fallido {
			// No va a salir solo: queda agotado a la vista (depende_de en pendientes lo bloquea).
			return q.AgotarWebhook(ctx, mensajeriabd.AgotarWebhookParams{ID: w.ID,
				UltimoError: texto("depende_de: el desvinculado anterior de este usuario de Argos no salió (agotado); reenvíalo y después este")})
		}
		espera, err := q.DesvinculadoAnteriorSinEntregar(ctx, mensajeriabd.DesvinculadoAnteriorSinEntregarParams{
			IDExterno: p.IDExterno, Antes: w.CreatedAt, UsuarioID: p.UsuarioID, Version: p.Version, EventoID: w.ID})
		if err != nil {
			return err
		}
		if espera && m.ahora().Sub(w.CreatedAt) >= TopeEsperaDesvinculado {
			return q.AgotarWebhook(ctx, mensajeriabd.AgotarWebhookParams{ID: w.ID,
				UltimoError: texto("depende_de: más de 24 h esperando el 2xx del desvinculado anterior de este usuario de Argos")})
		}
		if espera {
			return mensajeriabd.New(bd).PostergarWebhook(ctx, mensajeriabd.PostergarWebhookParams{ID: w.ID,
				ProximoIntento: m.ahora().Add(time.Minute), UltimoError: texto("espera el 2xx del desvinculado anterior de este usuario de Argos")})
		}
		in.ConectorID = &c.ID
		if err := m.enviarWebhook(ctx, c, w); err != nil {
			in.Estado, in.Error = "pendiente", texto(recortar(err.Error(), 300))
		} else {
			in.Estado = "entregado"
		}
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
	ctx = ctxSistema(ctx, w.bd)
	// Cada despacho corre aunque otro falle: un webhook que no sale no detiene las boletas, ni al revés.
	return errors.Join(
		w.m.despacharWebhooks(ctx, w.bd),
		w.m.despacharBoletas(ctx, w.bd),
		// Resoluciones que avisó el webhook de Argos (comprobante.aprobado / comprobante.rechazado).
		w.m.despacharResoluciones(ctx, w.bd),
	)
}

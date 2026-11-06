package mensajeria

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync/atomic"
	"time"
	"unicode"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	mensajeriabd "github.com/HCDestroyer/iris/apps/api/internal/mensajeria/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
)

// Webhook de Meta (estados de entrega, mensajes del cliente y calidad del número):
// POST /api/v1/mensajeria/canales/{id}/webhook, firmado con el app secret del canal
// (X-Hub-Signature-256). Se guarda en mensajeria.entrantes (deduplicado por el SHA-256 del cuerpo) y
// se procesa en un job. GET en la misma ruta es la verificación de la suscripción (hub.challenge).

// MaxCuerpoMeta: tope del cuerpo de un webhook de Meta.
const MaxCuerpoMeta = 1 << 20

// EntradaMeta es lo que llega en el POST.
type EntradaMeta struct {
	CanalID int64  `json:"canal_id"`
	Firma   string `json:"-"`
	Cuerpo  []byte `json:"-"`
}

// MarshalJSON: lo auditado es el canal y el tamaño, nunca el cuerpo (trae teléfonos y mensajes).
func (e EntradaMeta) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]any{"canal_id": e.CanalID, "bytes": len(e.Cuerpo)})
}

// MetaRecibido es la respuesta (200).
type MetaRecibido struct {
	Duplicado bool `json:"duplicado"`
}

// EntradaVerificarMeta es el GET de verificación.
type EntradaVerificarMeta struct {
	CanalID   int64  `json:"canal_id"`
	Modo      string `json:"modo"`
	Token     string `json:"-"`
	Challenge string `json:"-"`
}

var rechazosMeta atomic.Int64

func firmaMetaInvalida() *nucleo.Problema {
	return nucleo.NuevoProblema(http.StatusUnauthorized, "firma_invalida", "Firma inválida", "X-Hub-Signature-256 no corresponde al cuerpo")
}

// noAuditar401: los 401/403 anónimos no dejan fila (cualquiera los provoca); se cuentan y van al
// log con muestreo.
func noAuditar401(err error) bool {
	p, _ := nucleo.AProblema(err)
	if p.Status != http.StatusUnauthorized && p.Status != http.StatusForbidden {
		return true
	}
	if n := rechazosMeta.Add(1); n == 1 || n%100 == 0 {
		slog.Warn("mensajeria: webhooks de Meta rechazados sin auditar", "codigo", p.Codigo(), "total", n)
	}
	return false
}

func nuevoRecibirMeta(m *Modulo) *auditoria.Caso[EntradaMeta, MetaRecibido] {
	return auditoria.Nuevo(auditoria.Caso[EntradaMeta, MetaRecibido]{
		Accion: "mensajeria.recibir_webhook", Acceso: auditoria.Publico, AuditarError: noAuditar401,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaMeta) (MetaRecibido, error) {
			q := mensajeriabd.New(t)
			c, err := q.CanalPorID(ctx, in.CanalID)
			if errors.Is(err, pgx.ErrNoRows) {
				return MetaRecibido{}, firmaMetaInvalida()
			}
			if err != nil {
				return MetaRecibido{}, err
			}
			cr, err := m.credencialesDe(c)
			if err != nil {
				return MetaRecibido{}, err
			}
			if !FirmaMetaValida(cr.AppSecret, in.Firma, in.Cuerpo) {
				return MetaRecibido{}, firmaMetaInvalida()
			}
			if !json.Valid(in.Cuerpo) {
				return MetaRecibido{}, nucleo.SolicitudInvalida("El cuerpo no es JSON")
			}
			t.FijarEntidad("canal_mensajeria", c.ID)
			fijarEmpresaCanal(t, c.EmpresaID)
			h := sha256.Sum256(in.Cuerpo)
			id, err := q.InsertarEntrante(ctx, mensajeriabd.InsertarEntranteParams{CanalID: c.ID, HashCuerpo: h[:], Payload: in.Cuerpo})
			if errors.Is(err, pgx.ErrNoRows) {
				return MetaRecibido{Duplicado: true}, nil
			}
			if err != nil {
				return MetaRecibido{}, err
			}
			t.Anotar(map[string]any{"entrante_id": id})
			return MetaRecibido{}, m.encolar(ctx, t.Tx, ArgsProcesarEntrante{EntranteID: id})
		},
	})
}

func nuevoVerificarMeta(m *Modulo) *auditoria.Caso[EntradaVerificarMeta, string] {
	return auditoria.Nuevo(auditoria.Caso[EntradaVerificarMeta, string]{
		Accion: "mensajeria.verificar_webhook", Acceso: auditoria.Publico, AuditarError: noAuditar401,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaVerificarMeta) (string, error) {
			prohibido := nucleo.NuevoProblema(http.StatusForbidden, "verificacion_invalida", "Verificación inválida", "hub.verify_token no corresponde al canal")
			c, err := mensajeriabd.New(t).CanalPorID(ctx, in.CanalID)
			if errors.Is(err, pgx.ErrNoRows) {
				return "", prohibido
			}
			if err != nil {
				return "", err
			}
			cr, err := m.credencialesDe(c)
			if err != nil {
				return "", err
			}
			if in.Modo != "subscribe" || cr.VerifyToken == "" || subtle.ConstantTimeCompare([]byte(in.Token), []byte(cr.VerifyToken)) != 1 {
				return "", prohibido
			}
			t.FijarEntidad("canal_mensajeria", c.ID)
			fijarEmpresaCanal(t, c.EmpresaID)
			return in.Challenge, nil
		},
	})
}

// ArgsProcesarEntrante procesa un webhook de Meta guardado.
type ArgsProcesarEntrante struct {
	EntranteID int64 `json:"entrante_id"`
}

// Kind implementa river.JobArgs.
func (ArgsProcesarEntrante) Kind() string { return "mensajeria.procesar_entrante" }

// InsertOpts: cola de mensajería, uno por entrante.
func (ArgsProcesarEntrante) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: Cola, MaxAttempts: 10, UniqueOpts: river.UniqueOpts{ByArgs: true}}
}

type trabajadorEntrante struct {
	river.WorkerDefaults[ArgsProcesarEntrante]
	m  *Modulo
	bd nucleo.BD
}

func (w *trabajadorEntrante) Work(ctx context.Context, job *river.Job[ArgsProcesarEntrante]) error {
	_, err := w.m.procesarMeta.Ejecutar(ctxSistema(ctx, w.bd), job.Args.EntranteID)
	return err
}

// palabrasBaja: el cliente pide no recibir avisos (§6.4).
var palabrasBaja = map[string]bool{"BAJA": true, "ALTO": true}

func palabraDe(s string) string {
	return strings.ToUpper(strings.TrimFunc(strings.TrimSpace(s), func(r rune) bool { return unicode.IsPunct(r) || unicode.IsSpace(r) }))
}

func nuevoProcesarMeta(m *Modulo) *auditoria.Caso[int64, struct{}] {
	return auditoria.Nuevo(auditoria.Caso[int64, struct{}]{
		Accion: "mensajeria.procesar_webhook", Acceso: auditoria.Autenticado,
		AuditarExito: func(struct{}) bool { return false },
		Logica: func(ctx context.Context, t *auditoria.Tx, id int64) (struct{}, error) {
			q := mensajeriabd.New(t)
			e, err := q.EntranteParaProcesar(ctx, id)
			if err != nil {
				return struct{}{}, err
			}
			if e.ProcesadoEn != nil {
				return struct{}{}, nil
			}
			t.FijarEntidad("entrante_mensajeria", e.ID)
			ahora := m.ahora()
			var w metaWebhook
			if err := json.Unmarshal(e.Payload, &w); err != nil {
				msg := "cuerpo ilegible"
				return struct{}{}, q.MarcarEntrante(ctx, mensajeriabd.MarcarEntranteParams{ID: e.ID, ProcesadoEn: &ahora, Error: &msg})
			}
			canal := e.CanalID
			fila, err := q.CanalPorID(ctx, canal)
			if err != nil {
				return struct{}{}, err
			}
			// Una app de Meta puede servir a varias empresas: solo cuenta lo que es del número de este
			// canal (si no, una baja escrita al número de B quedaría registrada en A).
			ajenos := 0
			for _, en := range w.Entry {
				for _, ch := range en.Changes {
					if ch.Value.Metadata.PhoneNumberID != fila.PhoneNumberID {
						ajenos++
						continue
					}
					for _, s := range ch.Value.Statuses {
						// Primero un mensaje de una conversación; si no, un aviso.
						if ok, err := m.estadoMensajeConv(ctx, t, q, canal, s, ahora); err != nil || ok {
							if err != nil {
								return struct{}{}, err
							}
							continue
						}
						if err := m.aplicarEstado(ctx, t, q, canal, s, ahora); err != nil {
							return struct{}{}, err
						}
					}
					perfiles := map[string]string{}
					for _, c := range ch.Value.Contacts {
						perfiles[c.WaID] = c.Profile.Name
					}
					for _, msg := range ch.Value.Messages {
						if err := m.recibirMensaje(ctx, t, fila, msg, perfiles[msg.From], ahora); err != nil {
							return struct{}{}, err
						}
						if msg.Type != "text" || !palabrasBaja[palabraDe(msg.Text.Body)] {
							continue
						}
						if err := m.bajaPorChat(ctx, t, q, canal, e.ID, "+"+strings.TrimPrefix(msg.From, "+"), instanteMeta(msg.Timestamp, ahora)); err != nil {
							return struct{}{}, err
						}
					}
					if ch.Field == "phone_number_quality_update" {
						if err := m.calidadDelNumero(ctx, t, q, canal, ch.Value.Event, ahora); err != nil {
							return struct{}{}, err
						}
					}
				}
			}
			var nota *string
			if ajenos > 0 {
				t.Anotar(map[string]any{"cambios_de_otro_numero": ajenos})
				nota = texto(fmt.Sprintf("%d cambios descartados: phone_number_id distinto del canal", ajenos))
				slog.WarnContext(ctx, "mensajeria: webhook de Meta con cambios de otro número; descartados", "canal_id", canal, "entrante_id", e.ID, "descartados", ajenos)
			}
			return struct{}{}, q.MarcarEntrante(ctx, mensajeriabd.MarcarEntranteParams{ID: e.ID, ProcesadoEn: &ahora, Error: nota})
		},
	})
}

// aplicarEstado: delivered → entregado; read → entregado con leído (o solo leído, sin webhook, si
// ya estaba entregado); failed → fallido con su motivo. sent no cambia nada. Un estado de un aviso
// que ya terminó no lo cambia (cada aviso tiene un solo final).
func (m *Modulo) aplicarEstado(ctx context.Context, t *auditoria.Tx, q *mensajeriabd.Queries, canal int64, s metaEstado, ahora time.Time) error {
	if s.ID == "" {
		return nil
	}
	a, err := q.AvisoPorProveedor(ctx, mensajeriabd.AvisoPorProveedorParams{CanalID: &canal, ProveedorID: &s.ID})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	t.FijarEmpresa(a.EmpresaID)
	cuando := instanteMeta(s.Timestamp, ahora)
	switch {
	case s.Status == "delivered" && a.Estado == EstadoEnCola:
		return m.entregar(ctx, t, a, cuando, nil)
	case s.Status == "read" && a.Estado == EstadoEnCola:
		return m.entregar(ctx, t, a, cuando, &cuando)
	case s.Status == "read" && a.Estado == EstadoEntregado && a.LeidoEn == nil:
		f, err := q.MarcarLeido(ctx, mensajeriabd.MarcarLeidoParams{ID: a.ID, LeidoEn: &cuando})
		if err != nil {
			return err
		}
		return t.Emitir("aviso.leido", map[string]any{"aviso_id": f.ID, "leido_en": f.LeidoEn, "origen": f.Origen})
	case s.Status == "failed" && a.Estado == EstadoEnCola:
		motivo, detalle := motivoFallaMeta(s)
		return m.fallar(ctx, t, a, motivo, &detalle)
	}
	return nil
}

// bajaPorChat registra la baja de cada cliente al que este canal le avisó a ese número (origen
// cliente) y emite contacto.baja hacia Argos.
func (m *Modulo) bajaPorChat(ctx context.Context, t *auditoria.Tx, q *mensajeriabd.Queries, canal, entrante int64, numero string, cuando time.Time) error {
	clientes, err := q.ClientesAvisadosEn(ctx, mensajeriabd.ClientesAvisadosEnParams{CanalID: &canal, Destino: &numero})
	if err != nil {
		return err
	}
	for _, c := range clientes {
		t.FijarEmpresa(c.EmpresaID)
		h := hechoConsentimiento{EmpresaID: c.EmpresaID, ClienteID: c.ClienteID, Canal: CanalWhatsApp, Estado: "baja", Origen: "cliente",
			OcurridoEn: cuando.UTC().Format(time.RFC3339Nano)}
		if _, err := m.guardarConsentimiento(ctx, t, h, nil, &entrante); err != nil {
			return err
		}
	}
	return nil
}

// calidadDelNumero pausa el número 24 h si Meta lo marca (FLAGGED, DOWNGRADE) y lo libera al
// recuperarse (UNFLAGGED, UPGRADE) (§6.5).
func (m *Modulo) calidadDelNumero(ctx context.Context, t *auditoria.Tx, q *mensajeriabd.Queries, canal int64, evento string, ahora time.Time) error {
	var hasta *time.Time
	switch strings.ToUpper(evento) {
	case "FLAGGED", "DOWNGRADE":
		h := ahora.Add(24 * time.Hour)
		hasta = &h
	case "UNFLAGGED", "UPGRADE":
	default:
		return nil
	}
	t.Anotar(map[string]any{"calidad": evento, "pausado_hasta": hasta})
	if err := q.PausarCanal(ctx, mensajeriabd.PausarCanalParams{ID: canal, PausadoHasta: hasta}); err != nil {
		return err
	}
	return t.Emitir("canal_mensajeria.calidad", map[string]any{"canal_id": canal, "evento": evento, "pausado_hasta": hasta})
}

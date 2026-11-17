package mensajeria

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	mensajeriabd "github.com/HCDestroyer/iris/apps/api/internal/mensajeria/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
)

// Voz, etapa E1 (contratos/voz.md): las llamadas entrantes de WhatsApp las contestan personas en el
// panel. El audio va del navegador a Meta; aquí solo pasa la señalización (SDP) y el registro.

// Estados de una llamada (§2.3).
const (
	LlamadaSonando        = "sonando"
	LlamadaConectando     = "conectando"
	LlamadaEnCurso        = "en_curso"
	LlamadaTerminada      = "terminada"
	LlamadaPerdida        = "perdida"
	LlamadaRechazada      = "rechazada"
	LlamadaFueraDeHorario = "fuera_de_horario"
	LlamadaFallida        = "fallida"
)

// Acciones de la Graph API sobre una llamada.
const (
	MetaPreAceptar = "pre_accept"
	MetaAceptar    = "accept"
	MetaRechazar   = "reject"
	MetaTerminar   = "terminate"
)

// Tipos del feed (§3.10).
const (
	EventoLlamadaEntrante    = "llamada_entrante"
	EventoLlamadaActualizada = "llamada_actualizada"
	EventoLlamadaTerminada   = "llamada_terminada"
	EventoDevolucion         = "devolucion_actualizada"
)

// Tiempos y límites de fábrica de E1.
const (
	// ReservaLlamada: lo que dura la reserva de reclamar (§3.3).
	ReservaLlamada = 12 * time.Second
	// EsperaMedio: tras contestar (pre_accept), lo que se espera a confirmar-medio antes de soltar.
	EsperaMedio = 5 * time.Second
	// PlazoMeta: plazo de cada llamada a la Graph API.
	PlazoMeta = 4 * time.Second
	// FlujoVivo: un flujo SSE con latido en este tiempo cuenta como vivo (§3.4).
	FlujoVivo = 40 * time.Second
	// GraciaSinFlujo: sin flujo vivo durante una llamada en curso, se termina (§3.4).
	GraciaSinFlujo = 15 * time.Second
	// ConnectViejo: un connect más viejo que esto no hace sonar nada (§6.2, fila 10).
	ConnectViejo = 60 * time.Second
	// Ráfaga: la cuarta llamada de un número en 10 minutos se rechaza (límite de E1).
	RafagaLlamadas = 3
	RafagaVentana  = 10 * time.Minute
	// VenceDevolucion: una devolución pendiente vence a las 72 h.
	VenceDevolucion = 72 * time.Hour
	// AvisoPerdidaCada: el mensaje «No pudimos contestarte» sale una vez por hora y conversación.
	AvisoPerdidaCada = time.Hour
	// VentanaBuzon: un audio que llega en este tiempo tras una llamada dejada al buzón es su recado.
	VentanaBuzon = 5 * time.Minute
	// MensajePerdidaNeutro: lo que se le escribe a quien llamó a un número prestado y era de la empresa
	// prestataria (no se usa el texto de la dueña del número).
	MensajePerdidaNeutro = "No pudimos contestar tu llamada. Escríbenos por aquí y te atendemos."
	// MaxNavegadoresRecibir: navegadores por usuario con «Recibir llamadas» (los más viejos se quitan).
	MaxNavegadoresRecibir = 10
	// MaxSDP: tamaño máximo de un SDP.
	MaxSDP = 8 << 10
)

// CanalGraph es lo que hace falta para llamar a la Graph API por un número.
type CanalGraph struct {
	PhoneNumberID string
	Token         string
}

// AjustesMetaLlamadas son los ajustes de llamadas del número en Meta (lo que E1 lee y escribe).
type AjustesMetaLlamadas struct {
	Habilitadas  bool `json:"habilitadas"`
	IconoVisible bool `json:"iconoVisible"`
	Devolucion   bool `json:"devolucion"`
	// LimiteMensajeria: destinatarios por día del número (≥ 2 000 para tener llamadas).
	LimiteMensajeria int64 `json:"limiteMensajeria"`
	// Horas: el espejo de call_hours (nil = abierto siempre: Iris aplica su horario de su lado).
	Horas *HorasMeta `json:"horas,omitempty"`
}

// HorasMeta es call_hours de Meta: zona, hasta 2 franjas por día y hasta 20 feriados.
type HorasMeta struct {
	Zona     string        `json:"zona"`
	Franjas  []FranjaMeta  `json:"franjas"`
	Feriados []FeriadoMeta `json:"feriados"`
}

// FranjaMeta: un tramo de un día (1 = lunes … 7 = domingo), en HHMM.
type FranjaMeta struct {
	Dia    int    `json:"dia"`
	Abre   string `json:"abre"`
	Cierra string `json:"cierra"`
}

// FeriadoMeta: una fecha con su tramo (abre = cierra = "0000": cerrado todo el día).
type FeriadoMeta struct {
	Fecha  string `json:"fecha"`
	Abre   string `json:"abre"`
	Cierra string `json:"cierra"`
}

func hhmm(minutos int) string {
	if minutos >= 24*60 {
		return "2359"
	}
	return fmt.Sprintf("%02d%02d", minutos/60, minutos%60)
}

// horasMetaDe convierte el horario de Iris a call_hours si cabe en los límites de Meta (2 franjas
// por día, 20 feriados, un tramo por feriado). Si no cabe, o no hay horario, nil: call_hours queda
// abierto y el horario lo aplica Iris al llegar la llamada (§2.11).
func horasMetaDe(c *calendario) *HorasMeta {
	if c == nil || len(c.feriados) > 20 {
		return nil
	}
	h := &HorasMeta{Zona: c.zona.String(), Franjas: []FranjaMeta{}, Feriados: []FeriadoMeta{}}
	for d := 1; d <= 7; d++ {
		if len(c.semanal[d]) > 2 {
			return nil
		}
		for _, t := range c.semanal[d] {
			h.Franjas = append(h.Franjas, FranjaMeta{Dia: d, Abre: hhmm(t.ini), Cierra: hhmm(t.fin)})
		}
	}
	fechas := make([]string, 0, len(c.feriados))
	for f := range c.feriados {
		fechas = append(fechas, f)
	}
	slices.Sort(fechas)
	for _, f := range fechas {
		ts := c.feriados[f]
		switch len(ts) {
		case 0:
			h.Feriados = append(h.Feriados, FeriadoMeta{Fecha: f, Abre: "0000", Cierra: "0000"})
		case 1:
			h.Feriados = append(h.Feriados, FeriadoMeta{Fecha: f, Abre: hhmm(ts[0].ini), Cierra: hhmm(ts[0].fin)})
		default:
			return nil
		}
	}
	return h
}

var diasMeta = []string{"", "MONDAY", "TUESDAY", "WEDNESDAY", "THURSDAY", "FRIDAY", "SATURDAY", "SUNDAY"}

// GraphLlamadas es la parte de la Graph API de Meta que usa la voz. Va detrás de una interfaz porque
// todo lo que se sabe de la Calling API es un supuesto a validar con un número de prueba (§10.1):
// las pruebas usan un doble fiel a esos supuestos y el cliente real cambia en un solo sitio.
type GraphLlamadas interface {
	// Accion manda pre_accept, accept, reject o terminate para un call_id. sdp solo con pre_accept y
	// accept (la respuesta SDP del navegador); grabar pide la grabación de Meta al aceptar.
	Accion(ctx context.Context, canal CanalGraph, callID, accion, sdp string, grabar bool) error
	// Ajustes lee los ajustes de llamadas del número.
	Ajustes(ctx context.Context, canal CanalGraph) (AjustesMetaLlamadas, error)
	// FijarAjustes escribe icono, devolución y si están habilitadas.
	FijarAjustes(ctx context.Context, canal CanalGraph, a AjustesMetaLlamadas) error
}

// ErrMetaLlamada: Meta rechazó la acción o no respondió. Nunca lleva el mensaje crudo de Meta.
type ErrMetaLlamada struct {
	Status int
	// SinRespuesta: no se sabe si Meta la aplicó (red o plazo).
	SinRespuesta bool
}

func (e *ErrMetaLlamada) Error() string {
	if e.SinRespuesta {
		return "meta: sin respuesta"
	}
	return "meta: rechazó la acción (" + strconv.Itoa(e.Status) + ")"
}

// graphHTTP es el cliente real (supuestos de §10.1-2 y -7).
type graphHTTP struct{ m *Modulo }

func (g graphHTTP) pedir(ctx context.Context, metodo string, canal CanalGraph, ruta string, cuerpo any) ([]byte, error) {
	ctx, cancelar := context.WithTimeout(ctx, PlazoMeta)
	defer cancelar()
	var rd io.Reader
	if cuerpo != nil {
		b, err := json.Marshal(cuerpo)
		if err != nil {
			return nil, err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, metodo, g.m.cfg.MetaURL+"/"+canal.PhoneNumberID+ruta, rd)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+canal.Token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Iris/"+g.m.cfg.Version)
	resp, err := g.m.cfg.HTTPMeta.Do(req)
	if err != nil {
		return nil, &ErrMetaLlamada{SinRespuesta: true}
	}
	defer func() { _ = resp.Body.Close() }()
	leido, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, &ErrMetaLlamada{Status: resp.StatusCode, SinRespuesta: resp.StatusCode >= 500}
	}
	return leido, nil
}

func (g graphHTTP) Accion(ctx context.Context, canal CanalGraph, callID, accion, sdp string, grabar bool) error {
	c := map[string]any{"messaging_product": "whatsapp", "call_id": callID, "action": accion}
	if sdp != "" {
		c["session"] = map[string]any{"sdp_type": "answer", "sdp": sdp}
	}
	// grabar no se usa todavía: la grabación de Meta llega con E1c, cuando se valide con un número real.
	_ = grabar
	_, err := g.pedir(ctx, http.MethodPost, canal, "/calls", c)
	return err
}

type metaAjustesLlamadas struct {
	Calling struct {
		Status                   string `json:"status"`
		CallIconVisibility       string `json:"call_icon_visibility"`
		CallbackPermissionStatus string `json:"callback_permission_status"`
	} `json:"calling"`
}

func (g graphHTTP) Ajustes(ctx context.Context, canal CanalGraph) (AjustesMetaLlamadas, error) {
	b, err := g.pedir(ctx, http.MethodGet, canal, "/settings", nil)
	if err != nil {
		return AjustesMetaLlamadas{}, err
	}
	var r metaAjustesLlamadas
	if err := json.Unmarshal(b, &r); err != nil {
		return AjustesMetaLlamadas{}, &ErrMetaLlamada{SinRespuesta: true}
	}
	a := AjustesMetaLlamadas{Habilitadas: strings.EqualFold(r.Calling.Status, "ENABLED"), IconoVisible: !strings.EqualFold(r.Calling.CallIconVisibility, "DISABLE_ALL"),
		Devolucion: strings.EqualFold(r.Calling.CallbackPermissionStatus, "ENABLED")}
	// El límite de mensajería no viene en /settings: se lee aparte y, si no se puede leer, queda sin
	// cumplir (falla cerrado). La suscripción al campo calls no se puede leer con el token del número.
	if b, err := g.pedir(ctx, http.MethodGet, canal, "?fields=messaging_limit_tier", nil); err == nil {
		var n struct {
			Tier string `json:"messaging_limit_tier"`
		}
		_ = json.Unmarshal(b, &n)
		a.LimiteMensajeria = limiteDeTier(n.Tier)
	}
	return a, nil
}

func limiteDeTier(t string) int64 {
	switch strings.ToUpper(t) {
	case "TIER_2K":
		return 2000
	case "TIER_10K":
		return 10000
	case "TIER_100K":
		return 100000
	case "TIER_UNLIMITED":
		return 1 << 40
	}
	return 0
}

func (g graphHTTP) FijarAjustes(ctx context.Context, canal CanalGraph, a AjustesMetaLlamadas) error {
	si := func(b bool, v, f string) string {
		if b {
			return v
		}
		return f
	}
	calling := map[string]any{
		"status":                     si(a.Habilitadas, "ENABLED", "DISABLED"),
		"call_icon_visibility":       si(a.IconoVisible, "DEFAULT", "DISABLE_ALL"),
		"callback_permission_status": si(a.Devolucion, "ENABLED", "DISABLED")}
	horas := map[string]any{"status": "DISABLED"}
	if a.Horas != nil {
		semana, feriados := []map[string]any{}, []map[string]any{}
		for _, f := range a.Horas.Franjas {
			semana = append(semana, map[string]any{"day_of_week": diasMeta[f.Dia], "open_time": f.Abre, "close_time": f.Cierra})
		}
		for _, f := range a.Horas.Feriados {
			feriados = append(feriados, map[string]any{"date": f.Fecha, "start_time": f.Abre, "end_time": f.Cierra})
		}
		horas = map[string]any{"status": "ENABLED", "timezone_id": a.Horas.Zona, "weekly_operating_hours": semana, "holiday_schedule": feriados}
	}
	calling["call_hours"] = horas
	_, err := g.pedir(ctx, http.MethodPost, canal, "/settings", map[string]any{"calling": calling})
	return err
}

func (m *Modulo) graph() GraphLlamadas {
	if m.cfg.Llamadas != nil {
		return m.cfg.Llamadas
	}
	return graphHTTP{m: m}
}

// metaLlamada es un elemento de calls[] del webhook de Meta (campo calls; supuesto §10.1-2).
type metaLlamada struct {
	ID        string `json:"id"`
	From      string `json:"from"`
	To        string `json:"to"`
	Event     string `json:"event"`
	Timestamp string `json:"timestamp"`
	Direction string `json:"direction"`
	Session   *struct {
		SDPType string `json:"sdp_type"`
		SDP     string `json:"sdp"`
	} `json:"session"`
	Status    string          `json:"status"`
	StartTime string          `json:"start_time"`
	EndTime   string          `json:"end_time"`
	Duration  json.Number     `json:"duration"`
	Recording json.RawMessage `json:"recording"`
}

// Formas (§3.2) -------------------------------------------------------------------------------------

// CanalDeLlamada es el número de WhatsApp de la llamada.
type CanalDeLlamada struct {
	ID     int64  `json:"id"`
	Nombre string `json:"nombre"`
	Numero string `json:"numero"`
}

// ContactoDeLlamada: con quién es la llamada.
type ContactoDeLlamada struct {
	Tipo         string `json:"tipo" enum:"cliente,desconocido"`
	ClienteID    *int64 `json:"clienteId,omitempty"`
	ServicioID   *int64 `json:"servicioId,omitempty"`
	Verificacion string `json:"verificacion,omitempty" enum:"numero,fuerte" doc:"La de la conversación; una llamada no la cambia"`
}

// TramoLlamada: quién tuvo la llamada.
type TramoLlamada struct {
	Tipo        string      `json:"tipo" enum:"asistente,persona"`
	Usuario     *UsuarioRef `json:"usuario,omitempty"`
	Inicio      time.Time   `json:"inicio"`
	Fin         *time.Time  `json:"fin,omitempty"`
	DuracionSeg *int32      `json:"duracionSeg,omitempty"`
	MotivoFin   *string     `json:"motivoFin,omitempty"`
}

// EstadoGrabacion es lo que se sabe de la grabación de una llamada.
type EstadoGrabacion struct {
	Estado string `json:"estado" enum:"pendiente,disponible,fallida,borrada"`
}

// PuedeLlamada: lo que este usuario puede hacer con la llamada (lo calcula el servidor).
type PuedeLlamada struct {
	Contestar         bool `json:"contestar"`
	Rechazar          bool `json:"rechazar"`
	Colgar            bool `json:"colgar"`
	Tomar             bool `json:"tomar" doc:"E3: siempre false en E1"`
	Transferir        bool `json:"transferir" doc:"E3: siempre false en E1"`
	Anotar            bool `json:"anotar"`
	EscucharGrabacion bool `json:"escucharGrabacion" doc:"E1c (grabación): siempre false por ahora"`
}

// Llamada es una llamada como la ve el panel. Nunca lleva el SDP, el call_id de Meta ni el token.
type Llamada struct {
	ID             uuid.UUID          `json:"id"`
	EmpresaID      *int64             `json:"empresaId,omitempty"`
	Origen         string             `json:"origen" enum:"whatsapp,centralita"`
	Direccion      string             `json:"direccion" enum:"entrante,saliente"`
	ConversacionID *int64             `json:"conversacionId,omitempty"`
	MensajeID      *uuid.UUID         `json:"mensajeId,omitempty"`
	Canal          *CanalDeLlamada    `json:"canal,omitempty"`
	Numero         string             `json:"numero"`
	Titulo         string             `json:"titulo" doc:"Nombre del contacto o, si no se conoce, el número"`
	Contacto       ContactoDeLlamada  `json:"contacto"`
	Cola           *ColaRef           `json:"cola,omitempty"`
	Estado         string             `json:"estado" enum:"sonando,marcando,conectando,en_curso,terminada,perdida,rechazada,fuera_de_horario,fallida"`
	AtendidaPor    *string            `json:"atendidaPor,omitempty" enum:"persona,asistente"`
	Usuario        *UsuarioRef        `json:"usuario,omitempty" doc:"Quien la tiene, o quien la reclamó mientras se conecta"`
	Tramos         []TramoLlamada     `json:"tramos"`
	SonoEn         time.Time          `json:"sonoEn"`
	SuenaHasta     *time.Time         `json:"suenaHasta,omitempty" doc:"Mientras suena: cuándo se da por perdida"`
	ContestadaEn   *time.Time         `json:"contestadaEn,omitempty"`
	TerminadaEn    *time.Time         `json:"terminadaEn,omitempty"`
	DuracionSeg    *int32             `json:"duracionSeg,omitempty"`
	MotivoFin      *string            `json:"motivoFin,omitempty"`
	Nota           *string            `json:"nota,omitempty"`
	Resultado      *string            `json:"resultado,omitempty" enum:"resuelto,ticket_creado,devolver,equivocado,otro"`
	Grabacion      *EstadoGrabacion   `json:"grabacion,omitempty"`
	Puede          PuedeLlamada       `json:"puede"`
	Devolucion     *DevolucionLlamada `json:"devolucion,omitempty"`
}

// LlamadaEnMensaje es el objeto llamada de un mensaje de tipo llamada del hilo.
type LlamadaEnMensaje struct {
	ID          uuid.UUID        `json:"id"`
	Direccion   string           `json:"direccion" enum:"entrante,saliente"`
	Estado      string           `json:"estado" enum:"sonando,marcando,conectando,en_curso,terminada,perdida,rechazada,fuera_de_horario,fallida"`
	AtendidaPor *string          `json:"atendidaPor,omitempty" enum:"persona,asistente"`
	DuracionSeg *int32           `json:"duracionSeg,omitempty"`
	MotivoFin   *string          `json:"motivoFin,omitempty"`
	Usuario     *UsuarioRef      `json:"usuario,omitempty"`
	TieneNota   bool             `json:"tieneNota"`
	Grabacion   *EstadoGrabacion `json:"grabacion,omitempty"`
}

// DevolucionLlamada es una fila de «Por devolver».
type DevolucionLlamada struct {
	ID             int64     `json:"id"`
	ConversacionID int64     `json:"conversacionId"`
	LlamadaID      uuid.UUID `json:"llamadaId"`
	PedidaPor      string    `json:"pedidaPor" enum:"sistema,asistente,cliente"`
	Estado         string    `json:"estado" enum:"pendiente,hecha,descartada,vencida"`
	VenceEn        time.Time `json:"venceEn"`
	CreadaEn       time.Time `json:"creadaEn"`
}

func aDevolucion(f mensajeriabd.MensajeriaLlamadaDevolucione) DevolucionLlamada {
	return DevolucionLlamada{ID: f.ID, ConversacionID: f.ConversacionID, LlamadaID: f.LlamadaID, PedidaPor: f.PedidaPor, Estado: f.Estado,
		VenceEn: f.VenceEn, CreadaEn: f.CreatedAt}
}

// Errores propios (§3.11) ---------------------------------------------------------------------------

func llamadaNoEncontrada() *nucleo.Problema {
	return nucleo.NoEncontrado("La llamada no existe o no la puedes ver")
}

func llamadaYaContestada(quien string, mismaPersona bool) *nucleo.Problema {
	d := "Otra persona ya contestó esta llamada"
	if quien != "" {
		d = quien + " ya contestó esta llamada"
	}
	if mismaPersona {
		d = "Ya contestaste esta llamada en otra pestaña"
	}
	if mismaPersona {
		// El panel lo distingue por errors[0].rule = misma_persona y solo cierra el aviso.
		return nucleo.NuevoProblema(http.StatusConflict, "llamada_ya_contestada", "Conflicto", d,
			nucleo.ErrorCampo{Field: "mismaPersona", Message: "true", Rule: "misma_persona"})
	}
	return nucleo.Conflicto("llamada_ya_contestada", d)
}

func llamadaTerminada() *nucleo.Problema {
	return nucleo.Conflicto("llamada_terminada", "La llamada ya terminó")
}

func reservaLlamadaVencida() *nucleo.Problema {
	return nucleo.Conflicto("reserva_vencida", "Se acabó el tiempo para contestar: la llamada volvió a sonar")
}

func sdpInvalido(campo, detalle string) *nucleo.Problema {
	return nucleo.NuevoProblema(http.StatusUnprocessableEntity, "sdp_invalido", "SDP inválido", "La descripción del audio no es válida",
		nucleo.ErrorCampo{Field: campo, Message: detalle, Rule: "sdp_invalido"})
}

func metaRechazo() *nucleo.Problema {
	return nucleo.NuevoProblema(http.StatusBadGateway, "meta_rechazo", "WhatsApp rechazó la acción", "WhatsApp no aceptó la acción sobre la llamada. Vuelve a intentar")
}

func reglaVoz(codigo, detalle string, errores ...nucleo.ErrorCampo) *nucleo.Problema {
	return nucleo.NuevoProblema(http.StatusUnprocessableEntity, codigo, "No se puede", detalle, errores...)
}

// Alcance -------------------------------------------------------------------------------------------

// alcanceVoz: una persona con sesión, conversaciones.ver y el permiso de la acción. Lo que no se ve
// responde 404 (también para un token de API o una integración).
func alcanceVoz(ctx context.Context, permiso nucleo.Permiso) (alcanceConv, error) {
	a, err := alcanceConvDe(ctx)
	if err != nil {
		return a, llamadaNoEncontrada()
	}
	if !contexto.PermisosDe(ctx).Tiene(string(permiso)) {
		return a, nucleo.Prohibido("Falta el permiso " + string(permiso))
	}
	return a, nil
}

// llamadaVisible lee la llamada (con FOR UPDATE si bloquear) si el actor ve su conversación.
func llamadaVisible(ctx context.Context, q *mensajeriabd.Queries, a alcanceConv, id uuid.UUID, bloquear bool) (mensajeriabd.MensajeriaLlamada, error) {
	var l mensajeriabd.MensajeriaLlamada
	var err error
	if bloquear {
		l, err = q.LlamadaParaActualizar(ctx, id)
	} else {
		l, err = q.LlamadaPorID(ctx, id)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return l, llamadaNoEncontrada()
	}
	if err != nil {
		return l, err
	}
	if derefEmpresa(l.EmpresaID) != a.empresa || l.ConversacionID == nil {
		return l, llamadaNoEncontrada()
	}
	ok, err := q.ConversacionVisible(ctx, mensajeriabd.ConversacionVisibleParams{ID: *l.ConversacionID, Empresa: a.empresa, Usuario: a.usuario, Supervisa: a.supervisa})
	if err != nil {
		return l, err
	}
	if !ok {
		return l, llamadaNoEncontrada()
	}
	return l, nil
}

func llamadaFinal(estado string) bool {
	switch estado {
	case LlamadaTerminada, LlamadaPerdida, LlamadaRechazada, LlamadaFueraDeHorario, LlamadaFallida:
		return true
	}
	return false
}

// SDP -----------------------------------------------------------------------------------------------

// huellaSDP: lo único del SDP que va a la auditoría.
func huellaSDP(sdp string) string {
	h := sha256.Sum256([]byte(sdp))
	return hex.EncodeToString(h[:])
}

// atributosSDP: los únicos atributos (a=) que se aceptan en un SDP. Todo lo demás se rechaza.
var atributosSDP = map[string]bool{"rtpmap": true, "fmtp": true, "rtcp-fb": true, "extmap": true, "extmap-allow-mixed": true, "mid": true, "msid": true,
	"msid-semantic": true, "group": true, "ice-ufrag": true, "ice-pwd": true, "ice-options": true, "ice-lite": true, "fingerprint": true, "setup": true,
	"candidate": true, "end-of-candidates": true, "sendrecv": true, "sendonly": true, "recvonly": true, "inactive": true, "rtcp-mux": true,
	"rtcp-rsize": true, "rtcp": true, "ssrc": true, "ssrc-group": true, "ptime": true, "maxptime": true}

// redesSDPBloqueadas: además de loopback, privadas y enlace local: CGNAT, NAT64, protocolo y reservadas.
var redesSDPBloqueadas = []netip.Prefix{netip.MustParsePrefix("100.64.0.0/10"), netip.MustParsePrefix("64:ff9b::/96"), netip.MustParsePrefix("64:ff9b:1::/48"),
	netip.MustParsePrefix("192.0.0.0/24"), netip.MustParsePrefix("198.18.0.0/15"), netip.MustParsePrefix("240.0.0.0/4"), netip.MustParsePrefix("0.0.0.0/8")}

// MaxCandidatosSDP: candidatos ICE que se aceptan en un SDP.
const MaxCandidatosSDP = 32

// ipDeMedio: la dirección es pública (o, con admiteCero, 0.0.0.0 / ::, el marcador de «la dirá ICE»).
func ipDeMedio(texto string, admiteCero bool) bool {
	ip, err := netip.ParseAddr(texto)
	if err != nil || ip.Zone() != "" {
		return false
	}
	ip = ip.Unmap()
	if ip.IsUnspecified() {
		return admiteCero
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsInterfaceLocalMulticast() || ip.IsMulticast() {
		return false
	}
	for _, p := range redesSDPBloqueadas {
		if p.Contains(ip) {
			return false
		}
	}
	return true
}

// ValidarSDP aplica las reglas de §6.2 (fila 11) antes de reenviar un SDP (el del navegador hacia
// Meta y la oferta de Meta hacia el navegador). Es una lista cerrada: solo las líneas y los atributos
// conocidos, una sola sección de audio segura, solo Opus y telephone-event, a=setup y huella DTLS, y
// ninguna dirección (c=, a=rtcp, candidatos y su raddr) hacia loopback, redes privadas, enlace local,
// CGNAT o NAT64. Máximo 8 KB y 32 candidatos.
func ValidarSDP(sdp string) error {
	if len(sdp) == 0 || len(sdp) > MaxSDP {
		return errors.New("tamaño fuera de rango")
	}
	medios, candidatos, setup, huella := 0, 0, false, false
	var formatos []string
	codecs := map[string]string{}
	for _, l := range strings.Split(strings.ReplaceAll(sdp, "\r\n", "\n"), "\n") {
		l = strings.TrimRight(l, "\r")
		if l == "" {
			continue
		}
		if len(l) < 2 || l[1] != '=' || strings.ContainsFunc(l, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
			return errors.New("línea mal formada")
		}
		valor := l[2:]
		switch l[0] {
		case 'v', 'o', 's', 't', 'b':
		case 'c':
			p := strings.Fields(valor)
			if len(p) != 3 || p[0] != "IN" || (p[1] != "IP4" && p[1] != "IP6") || !ipDeMedio(p[2], true) {
				return errors.New("dirección de conexión no permitida")
			}
		case 'm':
			medios++
			p := strings.Fields(valor)
			if len(p) < 4 || p[0] != "audio" || !strings.Contains(p[2], "SAVPF") {
				return errors.New("solo se admite una sección de audio segura")
			}
			formatos = p[3:]
		case 'a':
			nombre, resto, _ := strings.Cut(valor, ":")
			if !atributosSDP[nombre] {
				return errors.New("atributo no permitido")
			}
			switch nombre {
			case "rtpmap":
				pt, codec, _ := strings.Cut(resto, " ")
				n, _, _ := strings.Cut(codec, "/")
				codecs[pt] = strings.ToLower(n)
			case "setup":
				setup = true
			case "fingerprint":
				huella = true
			case "rtcp":
				// a=rtcp:<puerto> [IN IP4 <dirección>]
				if p := strings.Fields(resto); len(p) != 1 && (len(p) != 4 || p[1] != "IN" || !ipDeMedio(p[3], true)) {
					return errors.New("dirección de RTCP no permitida")
				}
			case "candidate":
				candidatos++
				p := strings.Fields(resto)
				if candidatos > MaxCandidatosSDP || len(p) < 8 {
					return errors.New("candidatos de más o mal formados")
				}
				// Un nombre mDNS (.local) del navegador no dice nada de la red; cualquier otro nombre, no.
				mdns := strings.HasSuffix(strings.ToLower(p[4]), ".local") && !strings.ContainsAny(p[4], ":/")
				if !ipDeMedio(p[4], false) && !mdns {
					return errors.New("candidato a una red interna o con un nombre que no es una dirección")
				}
				for k := 8; k+1 < len(p); k += 2 {
					if p[k] == "raddr" && !ipDeMedio(p[k+1], true) {
						return errors.New("candidato con una dirección relacionada interna")
					}
				}
			}
		default:
			return errors.New("línea no permitida")
		}
	}
	if medios != 1 {
		return errors.New("debe traer una sola sección de audio")
	}
	if !setup || !huella {
		return errors.New("faltan a=setup o la huella DTLS")
	}
	if len(formatos) == 0 {
		return errors.New("sin formatos de audio")
	}
	for _, pt := range formatos {
		if c, ok := codecs[pt]; !ok || (c != "opus" && c != "telephone-event") {
			return errors.New("solo Opus y telephone-event")
		}
	}
	return nil
}

// Auditoría y feed ----------------------------------------------------------------------------------

// emitirEventoLlamada pone la llamada en el feed de la bandeja (a todos los que ven la conversación
// o, con usuario, solo a esa persona).
func (m *Modulo) emitirEventoLlamada(ctx context.Context, t *auditoria.Tx, l mensajeriabd.MensajeriaLlamada, tipo string, usuario *int64) error {
	if l.ConversacionID == nil {
		return nil
	}
	q := mensajeriabd.New(t)
	clave := strconv.FormatInt(derefEmpresa(l.EmpresaID), 10)
	if err := q.CandadoChat(ctx, "conv:eventos:"+clave); err != nil {
		return err
	}
	if _, err := q.InsertarEventoConv(ctx, mensajeriabd.InsertarEventoConvParams{EmpresaID: l.EmpresaID, ConversacionID: *l.ConversacionID, Tipo: tipo,
		UsuarioID: usuario, OcurridoEn: m.ahora(), LlamadaID: pgUUID(&l.ID)}); err != nil {
		return err
	}
	// El mensaje de la llamada en el hilo cambia con ella.
	if l.MensajeID.Valid && tipo != EventoLlamadaEntrante && tipo != EventoDevolucion {
		if _, err := q.InsertarEventoConv(ctx, mensajeriabd.InsertarEventoConvParams{EmpresaID: l.EmpresaID, ConversacionID: *l.ConversacionID,
			Tipo: EventoConvMensajeActualizad, MensajeID: l.MensajeID, OcurridoEn: m.ahora()}); err != nil {
			return err
		}
	}
	return q.NotificarConv(ctx, clave)
}

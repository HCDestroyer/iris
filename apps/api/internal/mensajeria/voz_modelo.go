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
	"log/slog"
	"net/http"
	"net/netip"
	"regexp"
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
	// PresupuestoMetaVoz: cuánto puede seguir tomando lotes un ciclo del job de escrituras a Meta.
	PresupuestoMetaVoz = 4 * time.Second
	// PlazoSaldoLlamada: lo que se espera a Argos por «tiene saldo vencido» al ver una llamada.
	PlazoSaldoLlamada = 2 * time.Second
	// VigenciaSaldo y VigenciaSaldoFallo: cuánto se recuerda la respuesta de Argos (y un fallo).
	VigenciaSaldo      = 60 * time.Second
	VigenciaSaldoFallo = 10 * time.Second
	// CabeceraNavegador: el id del navegador («Recibir llamadas») en las acciones de una llamada.
	CabeceraNavegador = "X-Iris-Navegador"
	// LoteMetaVoz: filas que toma el job de escrituras a Meta por ciclo. Las aparta lote × plazo: lo
	// que tarda, en el peor caso, en atenderlas todas; así otro ciclo no retoma las mismas.
	LoteMetaVoz = 5
	// MaxNavegadoresRecibir: navegadores por usuario con «Recibir llamadas» (los más viejos se quitan).
	MaxNavegadoresRecibir = 10
	// MaxSDP: tamaño máximo de un SDP.
	MaxSDP = 8 << 10
)

// CanalGraph es lo que hace falta para llamar a la Graph API por un número.
type CanalGraph struct {
	PhoneNumberID string
	Token         string
	// WabaID: la cuenta de WhatsApp Business del número (para consultar sus plantillas).
	WabaID string
}

// AjustesMetaLlamadas son los ajustes de llamadas del número en Meta (lo que E1 lee y escribe).
type AjustesMetaLlamadas struct {
	Habilitadas  bool `json:"habilitadas"`
	IconoVisible bool `json:"iconoVisible"`
	Devolucion   bool `json:"devolucion"`
	// LimiteMensajeria: destinatarios por día del portafolio de negocio del número (Meta pide
	// 2 000 o más para tener llamadas). Solo vale con LimiteLeido.
	LimiteMensajeria int64 `json:"limiteMensajeria"`
	// LimiteLeido: Meta devolvió el límite y se entendió. Si es false no se sabe: nunca «falta».
	LimiteLeido bool `json:"limiteLeido"`
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
	// Llamar inicia una saliente (action connect) con la oferta SDP del navegador y devuelve el call_id.
	Llamar(ctx context.Context, canal CanalGraph, numero, sdpOferta string) (string, error)
	// Permiso lee de Meta el permiso de llamada de un número (call_permissions).
	Permiso(ctx context.Context, canal CanalGraph, numero string) (PermisoMeta, error)
	// Plantilla dice si la cuenta tiene esa plantilla, si está aprobada y su categoría.
	Plantilla(ctx context.Context, canal CanalGraph, nombre, idioma string) (PlantillaMetaInfo, error)
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
	// El límite de mensajería no viene en /settings: se lee aparte, del nodo del número. El campo es
	// whatsapp_business_manager_messaging_limit (el límite es del portafolio de negocio, compartido
	// por todos sus números); messaging_limit_tier está deprecado y solo se usa de respaldo, en una
	// segunda consulta (pedir un campo que ya no existe haría fallar la primera). Si no se puede leer
	// o no se entiende, queda «sin comprobar»: nunca se afirma que falta sobre un dato no leído. La
	// suscripción al campo calls no se puede leer con el token del número.
	for _, campo := range []string{"whatsapp_business_manager_messaging_limit", "messaging_limit_tier"} {
		b, err := g.pedir(ctx, http.MethodGet, canal, "?fields="+campo, nil)
		if err != nil {
			continue
		}
		var n map[string]json.RawMessage
		var nivel string
		if json.Unmarshal(b, &n) != nil || json.Unmarshal(n[campo], &nivel) != nil {
			continue
		}
		if limite, ok := LimiteDeNivel(nivel); ok {
			a.LimiteMensajeria, a.LimiteLeido = limite, true
			break
		}
		if nivel != "" {
			// Meta cambió el formato (o añadió un nivel): que se note. El literal va acotado, por si acaso.
			slog.WarnContext(ctx, "voz: Meta devolvió un límite de mensajería que Iris no reconoce; el requisito queda sin comprobar",
				"campo", campo, "valor", literalAcotado(nivel))
		}
	}
	return a, nil
}

// literalAcotado deja de un valor de Meta solo letras, dígitos y guion bajo, y como mucho 40
// caracteres: lo justo para reconocerlo en un registro sin copiar texto arbitrario.
func literalAcotado(s string) string {
	var b strings.Builder
	for _, r := range s {
		if b.Len() >= 40 {
			break
		}
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// LimiteIlimitado: el valor de LimiteMensajeria para un portafolio sin límite.
const LimiteIlimitado = int64(1) << 40

var reNivelMeta = regexp.MustCompile(`^TIER_(\d{1,9})(K|M)?$`)

// LimiteDeNivel traduce el nivel de mensajería de Meta (TIER_250, TIER_2K, TIER_10K, TIER_100K,
// TIER_UNLIMITED…) a destinatarios por día. ok es false si el valor no se entiende: Meta no publica
// la lista cerrada de literales (su documentación solo da el ejemplo TIER_250), así que se lee el
// número del propio literal y lo desconocido no se interpreta.
func LimiteDeNivel(nivel string) (int64, bool) {
	nivel = strings.ToUpper(strings.TrimSpace(nivel))
	if nivel == "TIER_UNLIMITED" {
		return LimiteIlimitado, true
	}
	m := reNivelMeta.FindStringSubmatch(nivel)
	if m == nil {
		return 0, false
	}
	n, err := strconv.ParseInt(m[1], 10, 64)
	if err != nil || n <= 0 {
		return 0, false
	}
	switch m[2] {
	case "K":
		n *= 1000
	case "M":
		n *= 1000000
	}
	return n, true
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
	ID             uuid.UUID         `json:"id"`
	EmpresaID      *int64            `json:"empresaId,omitempty"`
	Origen         string            `json:"origen" enum:"whatsapp,centralita"`
	Direccion      string            `json:"direccion" enum:"entrante,saliente"`
	ConversacionID *int64            `json:"conversacionId,omitempty"`
	MensajeID      *uuid.UUID        `json:"mensajeId,omitempty"`
	Canal          *CanalDeLlamada   `json:"canal,omitempty"`
	Numero         string            `json:"numero"`
	Titulo         string            `json:"titulo" doc:"Nombre del contacto o, si no se conoce, el número"`
	Contacto       ContactoDeLlamada `json:"contacto"`
	Cola           *ColaRef          `json:"cola,omitempty"`
	Senales        *SenalesLlamada   `json:"senales,omitempty" doc:"Señales del cliente para decidir al contestar. Solo en llamadas vivas (sonando, conectando, en curso) de un cliente identificado"`
	Estado         string            `json:"estado" enum:"sonando,marcando,conectando,en_curso,terminada,perdida,rechazada,fuera_de_horario,fallida"`
	AtendidaPor    *string           `json:"atendidaPor,omitempty" enum:"persona,asistente"`
	Usuario        *UsuarioRef       `json:"usuario,omitempty" doc:"Quien la tiene, o quien la reclamó mientras se conecta"`
	Tramos         []TramoLlamada    `json:"tramos"`
	SonoEn         time.Time         `json:"sonoEn"`
	SuenaHasta     *time.Time        `json:"suenaHasta,omitempty" doc:"Mientras suena: cuándo se da por perdida"`
	ContestadaEn   *time.Time        `json:"contestadaEn,omitempty"`
	TerminadaEn    *time.Time        `json:"terminadaEn,omitempty"`
	DuracionSeg    *int32            `json:"duracionSeg,omitempty"`
	MotivoFin      *string           `json:"motivoFin,omitempty"`
	Nota           *string           `json:"nota,omitempty"`
	Resultado      *string           `json:"resultado,omitempty" enum:"resuelto,ticket_creado,devolver,equivocado,otro"`
	Grabacion      *EstadoGrabacion  `json:"grabacion,omitempty"`
	Puede          PuedeLlamada      `json:"puede"`
	// DeEstaPestana: solo en GET /llamadas/activa y solo para una saliente.
	DeEstaPestana *bool              `json:"deEstaPestana,omitempty" doc:"Solo en GET /llamadas/activa, para una saliente: true si se hizo desde la pestaña de la cabecera X-Iris-Pestana. false: es de otra pestaña (o de una que ya no existe): esta no tiene el audio"`
	DevolucionID  *int64             `json:"devolucionId,omitempty" doc:"La devolución que atiende esta saliente"`
	Devolucion    *DevolucionLlamada `json:"devolucion,omitempty"`
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
	// DevolucionID: la devolución que atiende una saliente («Devolver la llamada»).
	DevolucionID *int64 `json:"devolucionId,omitempty"`
}

// DevolucionLlamada es una fila de «Por devolver».
// SenalesLlamada: lo que el aviso de la llamada muestra del cliente, sin una segunda lectura.
type SenalesLlamada struct {
	Servicio      *ServicioDeLlamada `json:"servicio,omitempty" doc:"El servicio de la conversación o, si no tiene, uno activo del cliente (instantánea de Argos). Solo con clientes.ver"`
	TicketAbierto *TicketDeLlamada   `json:"ticketAbierto,omitempty" doc:"El ticket abierto más reciente del cliente. Solo con tickets.ver"`
	// SaldoVencido: solo en GET /llamadas/{id} y solo con clientes.estado_cuenta; se lee de Argos en
	// vivo y nunca trae montos. Ausente: no se sabe (sin permiso, Argos no respondió) o no se pidió.
	SaldoVencido *bool `json:"saldoVencido,omitempty" doc:"true: el cliente tiene saldo vencido. Solo en GET /llamadas/{id} y solo con clientes.estado_cuenta; ausente si no se sabe. Nunca montos"`
}

// ServicioDeLlamada es el servicio principal del cliente que llama.
type ServicioDeLlamada struct {
	ID     int64  `json:"id"`
	Codigo string `json:"codigo"`
	Plan   string `json:"plan"`
	Estado string `json:"estado"`
}

// TicketDeLlamada es un ticket abierto del cliente que llama.
type TicketDeLlamada struct {
	ID     int64  `json:"id"`
	Numero string `json:"numero"`
	Asunto string `json:"asunto"`
}

type DevolucionLlamada struct {
	ID             int64     `json:"id"`
	ConversacionID int64     `json:"conversacionId"`
	Titulo         string    `json:"titulo" doc:"Nombre del contacto de la conversación o, si no se conoce, el número"`
	Numero         string    `json:"numero" doc:"Teléfono del contacto en E.164"`
	LlamadaID      uuid.UUID `json:"llamadaId"`
	PedidaPor      string    `json:"pedidaPor" enum:"sistema,asistente,cliente"`
	Estado         string    `json:"estado" enum:"pendiente,hecha,descartada,vencida"`
	VenceEn        time.Time `json:"venceEn"`
	CreadaEn       time.Time `json:"creadaEn"`
}

// conTitulos completa el título y el número de cada devolución con los de su conversación.
func (m *Modulo) conTitulos(ctx context.Context, q *mensajeriabd.Queries, usuario int64, ds []DevolucionLlamada) error {
	if len(ds) == 0 {
		return nil
	}
	ids := make([]int64, 0, len(ds))
	for _, d := range ds {
		ids = append(ids, d.ConversacionID)
	}
	cs, err := m.resumirConvs(ctx, q, usuario, ids)
	if err != nil {
		return err
	}
	por := map[int64]ConversacionCliente{}
	for _, c := range cs {
		por[c.ID] = c
	}
	for i := range ds {
		if c, ok := por[ds[i].ConversacionID]; ok {
			ds[i].Titulo, ds[i].Numero = c.Titulo, c.Numero
		}
	}
	return nil
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

// otroNavegador: la reserva es de esta misma persona, pero en otro navegador: desde este no se
// contesta, confirma ni suelta. El panel solo cierra el aviso (errors[0].rule = otro_navegador).
func otroNavegador() *nucleo.Problema {
	return nucleo.NuevoProblema(http.StatusConflict, "llamada_ya_contestada", "Conflicto", "Estás contestando esta llamada en otro navegador",
		nucleo.ErrorCampo{Field: "navegador", Message: "true", Rule: "otro_navegador"})
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
	"rtcp-rsize": true, "rtcp": true, "ssrc": true, "ssrc-group": true, "ptime": true, "maxptime": true,
	// Chromium 153 lo pone en toda oferta de audio (informe RTCP de tiempo de ida y vuelta).
	"rtcp-xr": true}

// codecsSDP: los formatos de audio que se aceptan en una sección. No basta con Opus: la oferta real
// del navegador trae también red, G722, PCMU, PCMA y CN, y la respuesta de Meta a una saliente trae
// G722, PCMU y PCMA (doc de la Calling API, «Sample call connect webhook»); Meta además admite G.711
// si el número lo tiene encendido (call settings, `additional_codecs`). Todo es audio: lo demás
// (video, datos) se sigue rechazando.
var codecsSDP = map[string]bool{"opus": true, "telephone-event": true, "red": true, "g722": true, "pcmu": true, "pcma": true, "cn": true}

// redesSDPBloqueadas: además de loopback, privadas y enlace local: CGNAT, NAT64, protocolo y reservadas.
var redesSDPBloqueadas = []netip.Prefix{netip.MustParsePrefix("100.64.0.0/10"), netip.MustParsePrefix("64:ff9b::/96"), netip.MustParsePrefix("64:ff9b:1::/48"),
	netip.MustParsePrefix("192.0.0.0/24"), netip.MustParsePrefix("198.18.0.0/15"),
	// IPv6 que embeben una IPv4 (compatibles ::a.b.c.d, traducidas ::ffff:0:a.b.c.d, Teredo) y las
	// site-local en desuso.
	netip.MustParsePrefix("::/96"), netip.MustParsePrefix("::ffff:0:0:0/96"), netip.MustParsePrefix("2001::/32"), netip.MustParsePrefix("fec0::/10"),
	// Rangos de documentación: nunca son una dirección pública real (y hay redes internas que los usan).
	netip.MustParsePrefix("192.0.2.0/24"), netip.MustParsePrefix("198.51.100.0/24"), netip.MustParsePrefix("203.0.113.0/24"), netip.MustParsePrefix("2001:db8::/32"), netip.MustParsePrefix("240.0.0.0/4"), netip.MustParsePrefix("0.0.0.0/8")}

var seisACuatro = netip.MustParsePrefix("2002::/16")

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
	// 6to4 (2002::/16) lleva una IPv4 dentro: vale lo que valga esa.
	if ip.Is6() && seisACuatro.Contains(ip) {
		b := ip.As16()
		return ipDeMedio(netip.AddrFrom4([4]byte{b[2], b[3], b[4], b[5]}).String(), false)
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

// ValidarSDP aplica las reglas de §6.2 (fila 11) a un SDP que se entrega tal cual: la oferta de
// Meta hacia el navegador (y, más adelante, el del motor). Es una lista cerrada: solo las líneas y
// los atributos conocidos, una sola sección de audio segura, solo Opus y telephone-event, a=setup y
// huella DTLS, y ninguna dirección (c=, a=rtcp, candidatos y su raddr) hacia loopback, redes
// privadas, enlace local, CGNAT o NAT64. Máximo 8 KB y 32 candidatos.
func ValidarSDP(sdp string) error {
	_, _, err := revisarSDP(sdp, false)
	return err
}

// quitarFormatos saca de un SDP ya revisado los formatos de audio indicados: del m= y sus líneas
// rtpmap, fmtp y rtcp-fb.
func quitarFormatos(lineas []string, fuera map[string]bool) []string {
	r := lineas[:0:0]
	for _, l := range lineas {
		if strings.HasPrefix(l, "m=") {
			p := strings.Fields(l[2:])
			q := p[:3:3]
			for _, pt := range p[3:] {
				if !fuera[pt] {
					q = append(q, pt)
				}
			}
			r = append(r, "m="+strings.Join(q, " "))
			continue
		}
		quitar := false
		for _, pre := range []string{"a=rtpmap:", "a=fmtp:", "a=rtcp-fb:"} {
			if resto, ok := strings.CutPrefix(l, pre); ok {
				pt, _, _ := strings.Cut(resto, " ")
				quitar = fuera[pt]
			}
		}
		if !quitar {
			r = append(r, l)
		}
	}
	return r
}

// nombreSDP deja un nombre de atributo o códec para el registro: ASCII visible, como mucho 32 bytes.
func nombreSDP(s string) string {
	s = strings.Map(func(r rune) rune {
		if r <= ' ' || r > '~' {
			return '_'
		}
		return r
	}, s)
	if len(s) > 32 {
		return s[:32]
	}
	return s
}

// SanearSDP prepara la respuesta SDP del navegador antes de mandarla a Meta. Un navegador con el
// micrófono concedido pone sus direcciones de red local en los candidatos host y en raddr: eso no se
// rechaza, se quita. Salen los candidatos con dirección privada, CGNAT, loopback, enlace local, NAT64
// o con nombre (mDNS .local), sale raddr/rport de los que quedan, y c= y a=rtcp quedan en 0.0.0.0
// (Meta es ICE-lite con dirección pública: descubre al navegador por sus comprobaciones).
//
// Lo que el navegador agrega y Meta no necesita no tumba la llamada: un atributo a= fuera de la lista
// cerrada y un formato de audio fuera de codecsSDP (con sus rtpmap, fmtp y rtcp-fb) se QUITAN de lo
// que viaja a Meta; el navegador conserva su descripción local y Meta solo elige de lo ofrecido. Es
// seguro porque solo se quita (nunca viaja algo que no esté en la lista) y DTLS, ICE y Opus son
// obligatorios. Lo estructuralmente inválido (otra sección, video, datos, RTP sin cifrar, una línea
// que no es v/o/s/t/b/c/m/a, sin Opus, sin a=setup o sin huella) se rechaza igual que en ValidarSDP.
func SanearSDP(sdp string) (string, error) {
	s, _, err := revisarSDP(sdp, true)
	return s, err
}

// SanearSDPQuitado es SanearSDP y además dice qué se quitó por no estar en la lista (solo nombres de
// atributos y códecs, nunca valores ni direcciones), para el registro de diagnóstico.
func SanearSDPQuitado(sdp string) (string, []string, error) {
	return revisarSDP(sdp, true)
}

func revisarSDP(sdp string, sanear bool) (string, []string, error) {
	if len(sdp) == 0 || len(sdp) > MaxSDP {
		return "", nil, errors.New("tamaño fuera de rango")
	}
	medios, candidatos, setup, huella := 0, 0, false, false
	var formatos, salida, quitado []string
	codecs := map[string]string{}
	for _, l := range strings.Split(strings.ReplaceAll(sdp, "\r\n", "\n"), "\n") {
		l = strings.TrimRight(l, "\r")
		if l == "" {
			continue
		}
		if len(l) < 2 || l[1] != '=' || strings.ContainsFunc(l, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
			return "", nil, errors.New("línea mal formada")
		}
		valor := l[2:]
		switch l[0] {
		case 'v', 'o', 's', 't', 'b':
		case 'c':
			p := strings.Fields(valor)
			if len(p) != 3 || p[0] != "IN" || (p[1] != "IP4" && p[1] != "IP6") {
				return "", nil, errors.New("dirección de conexión mal formada")
			}
			if sanear {
				l = "c=IN IP4 0.0.0.0"
			} else if !ipDeMedio(p[2], true) {
				return "", nil, errors.New("dirección de conexión no permitida")
			}
		case 'm':
			medios++
			p := strings.Fields(valor)
			if len(p) < 4 || p[0] != "audio" || !strings.Contains(p[2], "SAVPF") {
				return "", nil, errors.New("solo se admite una sección de audio segura")
			}
			formatos = p[3:]
		case 'a':
			nombre, resto, _ := strings.Cut(valor, ":")
			if !atributosSDP[nombre] {
				if sanear && nombre != "" && medios <= 1 {
					quitado = append(quitado, "a="+nombreSDP(nombre))
					continue
				}
				return "", nil, errors.New("atributo no permitido")
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
				p := strings.Fields(resto)
				if len(p) != 1 && (len(p) != 4 || p[1] != "IN" || (p[2] != "IP4" && p[2] != "IP6")) {
					return "", nil, errors.New("a=rtcp mal formado")
				}
				if len(p) == 4 {
					if sanear {
						l = "a=rtcp:" + p[0] + " IN IP4 0.0.0.0"
					} else if !ipDeMedio(p[3], true) {
						return "", nil, errors.New("dirección de RTCP no permitida")
					}
				}
			case "candidate":
				p := strings.Fields(resto)
				if len(p) < 8 || p[6] != "typ" {
					return "", nil, errors.New("candidato mal formado")
				}
				if !ipDeMedio(p[4], false) {
					if sanear {
						continue // red local del navegador, o un nombre (mDNS): no viaja a Meta
					}
					return "", nil, errors.New("candidato a una red interna o con un nombre que no es una dirección")
				}
				candidatos++
				if candidatos > MaxCandidatosSDP {
					if sanear {
						continue
					}
					return "", nil, errors.New("candidatos de más")
				}
				// raddr y rport se buscan en cualquier posición de las extensiones (no de dos en dos: una
				// extensión suelta no puede esconderlos).
				limpio := p[:8:8]
				for k := 8; k < len(p); k++ {
					if p[k] != "raddr" && p[k] != "rport" {
						limpio = append(limpio, p[k])
						continue
					}
					if k+1 >= len(p) {
						return "", nil, errors.New("candidato mal formado")
					}
					if !sanear {
						if p[k] == "raddr" && !ipDeMedio(p[k+1], true) {
							return "", nil, errors.New("candidato con una dirección relacionada interna")
						}
						limpio = append(limpio, p[k], p[k+1])
					}
					k++
				}
				if sanear {
					l = "a=candidate:" + strings.Join(limpio, " ")
				}
			}
		default:
			return "", nil, errors.New("línea no permitida")
		}
		salida = append(salida, l)
	}
	if medios != 1 {
		return "", nil, errors.New("debe traer una sola sección de audio")
	}
	if !setup || !huella {
		return "", nil, errors.New("faltan a=setup o la huella DTLS")
	}
	if len(formatos) == 0 {
		return "", nil, errors.New("sin formatos de audio")
	}
	// Opus siempre: Meta lo tiene encendido en todo número y no se puede quitar (call settings).
	conOpus := false
	fuera := map[string]bool{}
	for _, pt := range formatos {
		conOpus = conOpus || codecs[pt] == "opus"
		if c, ok := codecs[pt]; !ok || !codecsSDP[c] {
			if !sanear {
				return "", nil, errors.New("formato de audio no admitido")
			}
			fuera[pt] = true
			quitado = append(quitado, "codec="+nombreSDP(codecs[pt]))
		}
	}
	if !conOpus {
		return "", nil, errors.New("sin Opus")
	}
	if len(fuera) > 0 {
		salida = quitarFormatos(salida, fuera)
	}
	return strings.Join(salida, "\r\n") + "\r\n", quitado, nil
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

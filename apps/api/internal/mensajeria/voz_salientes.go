package mensajeria

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
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

// Voz E1b (contratos/voz.md §2.5 y §3.7): llamadas salientes del negocio por WhatsApp y el permiso
// del contacto para que lo llamemos. Nunca un número libre: el destino es el contacto de la
// conversación. Solo personas (el asistente no llama) y solo la empresa dueña del número.

const (
	PermisoSinPedir   = "sin_pedir"
	PermisoPedido     = "pedido"
	PermisoTemporal   = "temporal"
	PermisoPermanente = "permanente"
	PermisoNegado     = "negado"
	PermisoRevocado   = "revocado"
	// PermisoVencido: solo se muestra (un temporal cuya fecha ya pasó); no se guarda.
	PermisoVencido = "vencido"
	// CabeceraPestana: el id de la pestaña del panel (aleatorio por pestaña) en llamar, respuesta-sdp,
	// llamadas/activa y el flujo de eventos.
	CabeceraPestana = "X-Iris-Pestana"
	// PlazoCrearSaliente: lo más que tarda llamar en dejar creada la llamada en el servidor (la
	// lectura del permiso en Meta más un margen). PlazoResponderLlamar: lo más que tarda en responder.
	PlazoCrearSaliente   = PlazoMeta + 2*time.Second
	PlazoResponderLlamar = 2*PlazoMeta + 2*time.Second

	// AutomaticoPermisoLlamada marca el mensaje con que se pide el permiso de llamada.
	AutomaticoPermisoLlamada = "permiso_llamada"
	// TextoPermisoLlamada: lo que se ve en el hilo (el cliente ve los botones de WhatsApp).
	TextoPermisoLlamada = "Queremos llamarte por WhatsApp para atenderte. ¿Nos das permiso?"
	// LlamadaMarcando: la saliente espera a que el cliente conteste.
	LlamadaMarcando = "marcando"
	// MetaConectar: la acción de Graph que inicia una saliente.
	MetaConectar = "connect"
	// EventoLlamadaRespuesta: llegó la respuesta SDP de una saliente (solo a quien llamó; el SDP no
	// viaja en el feed: se lee con GET /llamadas/{id}/respuesta-sdp).
	EventoLlamadaRespuesta = "llamada_respuesta"
	// SegundosMarcando: cuánto se espera a que el cliente conteste una saliente.
	SegundosMarcando = 60
	// MaxConectadasCanal: salientes conectadas por número en 24 h (límite de Meta).
	MaxConectadasCanal = 100
	// SinContestarRevoca: a la cuarta saliente seguida sin contestar Meta revoca el permiso.
	SinContestarRevoca = 4
	// VigenciaPermisoMeta: cuánto vale una lectura de call_permissions.
	VigenciaPermisoMeta = 60 * time.Second
	// VentanaSalienteNuestra: un call_id saliente desconocido solo se considera de Iris si Iris mandó
	// un connect a ese número en este tiempo.
	VentanaSalienteNuestra = 2 * time.Minute
	// PermisoTemporalDias: lo que dura un permiso temporal si Meta no dice cuándo vence.
	PermisoTemporalDias = 7
)

// PermisoMeta es lo que dice Meta del permiso de llamada de un número (GET call_permissions).
type PermisoMeta struct {
	// Estado: temporal, permanente o sin_pedir.
	Estado  string
	VenceEn *time.Time
	// PuedeLlamar y PuedeSolicitar: lo que Meta deja hacer ahora (sus propios límites).
	PuedeLlamar    bool
	PuedeSolicitar bool
	// SolicitudesEnDia: cuántas solicitudes de permiso cuenta Meta a ese número en las últimas 24 h
	// (nil si Meta no lo dice). Sirve para saber si una solicitud sin confirmar llegó a salir.
	SolicitudesEnDia *int
}

// PermisoLlamada es el permiso del contacto para que lo llamemos.
type PermisoLlamada struct {
	Estado             string     `json:"estado" enum:"sin_pedir,pedido,temporal,permanente,negado,revocado,vencido" doc:"vencido: tuvo un permiso temporal que ya pasó (se le puede volver a pedir)"`
	VenceEn            *time.Time `json:"venceEn,omitempty" doc:"Con temporal: hasta cuándo vale"`
	PuedeSolicitar     bool       `json:"puedeSolicitar" doc:"Se le puede pedir el permiso ahora (1 solicitud por 24 h y 2 por 7 días)"`
	ProximaSolicitudEn *time.Time `json:"proximaSolicitudEn,omitempty" doc:"Desde cuándo se le puede volver a pedir"`
	PuedeLlamar        bool       `json:"puedeLlamar" doc:"Tú puedes llamarlo ahora (permiso vigente, llamadas encendidas y llamadas.llamar)"`
	Motivo             *string    `json:"motivo,omitempty" enum:"llamadas_inactivas,sin_permiso_llamada,no_autorizado" doc:"Por qué no se puede llamar"`
	SinContestar       int32      `json:"sinContestar" doc:"Llamadas seguidas sin contestar: a la cuarta WhatsApp revoca el permiso"`
	// SolicitudSinConfirmar: la última solicitud se quedó sin respuesta de WhatsApp al enviarla.
	SolicitudSinConfirmar bool `json:"solicitudSinConfirmar,omitempty" doc:"La última solicitud se envió pero WhatsApp no confirmó si la recibió: cuenta para el límite hasta que se sepa (lo resuelve el servidor en un par de minutos, o la respuesta del cliente)"`
}

func llamadasInactivas() *nucleo.Problema {
	return reglaVoz("llamadas_inactivas", "Este número no tiene las llamadas de WhatsApp encendidas para tu empresa")
}

// solicitudesDe lee las marcas de tiempo de las solicitudes de permiso.
func solicitudesDe(crudo []byte) []time.Time {
	var ts []time.Time
	_ = json.Unmarshal(crudo, &ts)
	return ts
}

// proximaSolicitud aplica los límites de Meta: 1 solicitud por 24 h y 2 por 7 días. nil = ya se puede.
func proximaSolicitud(solicitudes []time.Time, ahora time.Time) *time.Time {
	var semana []time.Time
	for _, s := range solicitudes {
		if ahora.Sub(s) < 7*24*time.Hour {
			semana = append(semana, s)
		}
	}
	slices.SortFunc(semana, func(a, b time.Time) int { return a.Compare(b) })
	var proxima *time.Time
	if n := len(semana); n > 0 {
		if p := semana[n-1].Add(24 * time.Hour); p.After(ahora) {
			proxima = &p
		}
		if n >= 2 {
			if p := semana[n-2].Add(7 * 24 * time.Hour); proxima == nil || p.After(*proxima) {
				proxima = &p
			}
		}
	}
	return proxima
}

// permisoVigente: temporal sin vencer, o permanente.
func permisoVigente(f mensajeriabd.MensajeriaLlamadaPermiso, ahora time.Time) bool {
	return f.Estado == PermisoPermanente || (f.Estado == PermisoTemporal && (f.VenceEn == nil || ahora.Before(*f.VenceEn)))
}

// solicitudesDelContacto: las solicitudes de permiso a ese contacto desde cualquier número de la
// empresa. El límite (1 por 24 h y 2 por 7 días) es por contacto, no por número del negocio.
func solicitudesDelContacto(ctx context.Context, q *mensajeriabd.Queries, empresa int64, numero string) ([]time.Time, error) {
	filas, err := q.SolicitudesDeNumero(ctx, mensajeriabd.SolicitudesDeNumeroParams{Empresa: empresa, Numero: numero})
	if err != nil {
		return nil, err
	}
	var todas []time.Time
	for _, f := range filas {
		todas = append(todas, solicitudesDe(f)...)
	}
	return todas, nil
}

func (m *Modulo) aPermiso(ctx context.Context, f mensajeriabd.MensajeriaLlamadaPermiso, activas bool, delContacto []time.Time) PermisoLlamada {
	ahora := m.ahora()
	p := PermisoLlamada{Estado: f.Estado, SinContestar: f.SinContestar, SolicitudSinConfirmar: f.SolicitudSinConfirmar}
	if f.Estado == "" {
		p.Estado = PermisoSinPedir
	}
	vigente := permisoVigente(f, ahora)
	if f.Estado == PermisoTemporal {
		if vigente {
			p.VenceEn = f.VenceEn
		} else {
			p.Estado, p.VenceEn = PermisoVencido, f.VenceEn
		}
	}
	if prox := proximaSolicitud(delContacto, ahora); prox != nil {
		p.ProximaSolicitudEn = prox
	}
	p.PuedeSolicitar = activas && !vigente && p.ProximaSolicitudEn == nil
	switch {
	case !activas:
		p.Motivo = nombrePtr("llamadas_inactivas")
	case !vigente:
		p.Motivo = nombrePtr("sin_permiso_llamada")
	case !contexto.PermisosDe(ctx).Tiene(string(nucleo.PermLlamadasLlamar)):
		p.Motivo = nombrePtr("no_autorizado")
	default:
		p.PuedeLlamar = true
	}
	return p
}

// canalParaSalientes: el número de la conversación, si es de la empresa de quien actúa (un número
// prestado solo lo usa la dueña para llamar), está por API y tiene las llamadas encendidas.
func canalParaSalientes(ctx context.Context, q *mensajeriabd.Queries, a alcanceConv, c mensajeriabd.MensajeriaConversacion) (mensajeriabd.MensajeriaCanal, mensajeriabd.MensajeriaVozAjustesCanal, bool, error) {
	canal, err := q.CanalPorID(ctx, c.CanalID)
	if err != nil {
		return canal, mensajeriabd.MensajeriaVozAjustesCanal{}, false, err
	}
	aj, err := q.VozAjustesCanal(ctx, canal.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		return canal, aj, false, nil
	}
	if err != nil {
		return canal, aj, false, err
	}
	activas := aj.LlamadasActivas && canal.TipoConexion == ConexionAPI && derefEmpresa(canal.EmpresaID) == a.empresa && derefEmpresa(c.EmpresaID) == a.empresa
	return canal, aj, activas, nil
}

func permisoDe(ctx context.Context, q *mensajeriabd.Queries, empresa, canal int64, numero string, bloquear bool) (mensajeriabd.MensajeriaLlamadaPermiso, error) {
	var f mensajeriabd.MensajeriaLlamadaPermiso
	var err error
	if bloquear {
		if err := q.CrearPermisoLlamada(ctx, mensajeriabd.CrearPermisoLlamadaParams{EmpresaID: empresaPtrDe(empresa), CanalID: canal, Numero: numero}); err != nil {
			return f, err
		}
		return q.PermisoLlamadaParaActualizar(ctx, mensajeriabd.PermisoLlamadaParaActualizarParams{Empresa: empresa, CanalID: canal, Numero: numero})
	}
	f, err = q.PermisoLlamada(ctx, mensajeriabd.PermisoLlamadaParams{Empresa: empresa, CanalID: canal, Numero: numero})
	if errors.Is(err, pgx.ErrNoRows) {
		return mensajeriabd.MensajeriaLlamadaPermiso{EmpresaID: empresaPtrDe(empresa), CanalID: canal, Numero: numero, Estado: PermisoSinPedir, Solicitudes: []byte("[]")}, nil
	}
	return f, err
}

func guardarPermiso(ctx context.Context, q *mensajeriabd.Queries, f mensajeriabd.MensajeriaLlamadaPermiso) (mensajeriabd.MensajeriaLlamadaPermiso, error) {
	if len(f.Solicitudes) == 0 {
		f.Solicitudes = []byte("[]")
	}
	return q.GuardarPermisoLlamada(ctx, mensajeriabd.GuardarPermisoLlamadaParams{ID: f.ID, Estado: f.Estado, VenceEn: f.VenceEn, Solicitudes: f.Solicitudes,
		SinContestar: f.SinContestar, UltimaSolicitudMensajeID: f.UltimaSolicitudMensajeID, SincronizadoEn: f.SincronizadoEn,
		SolicitudSinConfirmar: f.SolicitudSinConfirmar})
}

// cambiarPermiso guarda el estado nuevo y, si cambió, emite llamada.permiso_cambiado (sin el número).
func cambiarPermiso(ctx context.Context, t *auditoria.Tx, f mensajeriabd.MensajeriaLlamadaPermiso, a string, vence *time.Time, conv *int64) (mensajeriabd.MensajeriaLlamadaPermiso, error) {
	de := f.Estado
	f.Estado, f.VenceEn = a, vence
	g, err := guardarPermiso(ctx, mensajeriabd.New(t), f)
	if err != nil || de == a {
		return g, err
	}
	return g, t.Emitir("llamada.permiso_cambiado", map[string]any{"conversacion_id": conv, "empresa_id": f.EmpresaID, "canal_id": f.CanalID,
		"de": de, "a": a, "vence_en": vence})
}

// EntradaConvVoz identifica la conversación.
type EntradaConvVoz struct {
	ID int64 `json:"id"`
}

func nuevoVerPermisoLlamada(m *Modulo) *auditoria.Caso[EntradaConvVoz, PermisoLlamada] {
	return auditoria.Nuevo(auditoria.Caso[EntradaConvVoz, PermisoLlamada]{
		Accion: "llamadas.ver_permiso", Permiso: nucleo.PermConversacionesVer, AuditarExito: sinAuditar[PermisoLlamada],
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaConvVoz) (PermisoLlamada, error) {
			a, err := alcanceConvDe(ctx)
			if err != nil {
				return PermisoLlamada{}, err
			}
			q := mensajeriabd.New(t)
			c, err := convVisible(ctx, q, a, in.ID, false)
			if err != nil {
				return PermisoLlamada{}, err
			}
			canal, _, activas, err := canalParaSalientes(ctx, q, a, c)
			if err != nil {
				return PermisoLlamada{}, err
			}
			f, err := permisoDe(ctx, q, a.empresa, canal.ID, c.Numero, false)
			if err != nil {
				return PermisoLlamada{}, err
			}
			delContacto, err := solicitudesDelContacto(ctx, q, a.empresa, c.Numero)
			if err != nil {
				return PermisoLlamada{}, err
			}
			return m.aPermiso(ctx, f, activas, delContacto), nil
		},
	})
}

func nuevoSolicitarPermisoLlamada(m *Modulo) *auditoria.Caso[EntradaConvVoz, PermisoLlamada] {
	return auditoria.Nuevo(auditoria.Caso[EntradaConvVoz, PermisoLlamada]{
		Accion: "llamadas.solicitar_permiso", Permiso: nucleo.PermLlamadasSolicitarPermiso,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaConvVoz) (PermisoLlamada, error) {
			a, err := alcanceConvDe(ctx)
			if err != nil {
				return PermisoLlamada{}, err
			}
			t.FijarEmpresa(a.empresa)
			t.FijarEntidad("conversacion", in.ID)
			q := mensajeriabd.New(t)
			c, err := convVisible(ctx, q, a, in.ID, true)
			if err != nil {
				return PermisoLlamada{}, err
			}
			canal, aj, activas, err := canalParaSalientes(ctx, q, a, c)
			if err != nil {
				return PermisoLlamada{}, err
			}
			if !activas {
				return PermisoLlamada{}, llamadasInactivas()
			}
			ahora := m.ahora()
			// El límite es por contacto entre todos los números de la empresa: se decide de una en una
			// (dos solicitudes a la vez desde dos números no lo burlan). Orden de candados, igual en
			// llamar, aquí y en el webhook de la respuesta: conversación → permiso.
			if err := q.CandadoChat(ctx, "voz:permiso:"+strconv.FormatInt(a.empresa, 10)+":"+c.Numero); err != nil {
				return PermisoLlamada{}, err
			}
			f, err := permisoDe(ctx, q, a.empresa, canal.ID, c.Numero, true)
			if err != nil {
				return PermisoLlamada{}, err
			}
			if permisoVigente(f, ahora) {
				return PermisoLlamada{}, reglaVoz("permiso_ya_vigente", "Este contacto ya dio permiso para que lo llamen")
			}
			solicitudes := solicitudesDe(f.Solicitudes)
			delContacto, err := solicitudesDelContacto(ctx, q, a.empresa, c.Numero)
			if err != nil {
				return PermisoLlamada{}, err
			}
			if prox := proximaSolicitud(delContacto, ahora); prox != nil {
				return PermisoLlamada{}, reglaVoz("limite_solicitudes", "WhatsApp deja pedir el permiso una vez cada 24 horas y dos veces cada 7 días",
					nucleo.ErrorCampo{Field: "proximaSolicitudEn", Message: prox.UTC().Format(time.RFC3339), Rule: "limite_solicitudes"})
			}
			// Mismo trato que cualquier mensaje al cliente: baja, consentimiento y cliente sin confirmar.
			fuera := !ventanaDe(c.UltimoEntranteEn, ahora).Abierta
			if err := permisoPlantilla(ctx, q, c, fuera); err != nil {
				return PermisoLlamada{}, err
			}
			if fuera && aj.PlantillaPermiso == nil {
				return PermisoLlamada{}, reglaVoz("sin_plantilla_permiso",
					"Fuera de la ventana de 24 h hace falta la plantilla de permiso de llamada aprobada del número (se indica en los ajustes de llamadas)")
			}
			texto, automatico := TextoPermisoLlamada, AutomaticoPermisoLlamada
			msg, err := m.agregarMensaje(ctx, t, &c, nuevoMensaje{direccion: DirSaliente, tipo: "texto", texto: &texto, autor: &a.usuario,
				estadoEntrega: nombrePtr(EntregaEnCola), automatico: &automatico})
			if err != nil {
				return PermisoLlamada{}, err
			}
			if err := m.encolar(ctx, t.Tx, ArgsEnviarMensaje{MensajeID: msg.ID}); err != nil {
				return PermisoLlamada{}, err
			}
			if _, err := guardarConv(ctx, q, c, false); err != nil {
				return PermisoLlamada{}, err
			}
			// Solo se guardan las de la última semana.
			var recientes []time.Time
			for _, s := range solicitudes {
				if ahora.Sub(s) < 7*24*time.Hour {
					recientes = append(recientes, s)
				}
			}
			f.Solicitudes, _ = json.Marshal(append(recientes, ahora))
			f.UltimaSolicitudMensajeID = pgUUID(&msg.ID)
			f.SolicitudSinConfirmar = false
			f, err = cambiarPermiso(ctx, t, f, PermisoPedido, nil, &c.ID)
			if err != nil {
				return PermisoLlamada{}, err
			}
			if err := t.Emitir("llamada.permiso_solicitado", map[string]any{"conversacion_id": c.ID, "empresa_id": c.EmpresaID, "canal_id": canal.ID,
				"usuario_id": a.usuario, "mensaje_id": msg.ID}); err != nil {
				return PermisoLlamada{}, err
			}
			return m.aPermiso(ctx, f, true, append(delContacto, ahora)), nil
		},
	})
}

// recibirPermisoLlamada aplica la respuesta del cliente a la solicitud de permiso (webhook messages,
// interactive.call_permission_reply). Vale para todas las filas de ese número en ese canal.
func (m *Modulo) recibirPermisoLlamada(ctx context.Context, t *auditoria.Tx, canal mensajeriabd.MensajeriaCanal, msg metaMensaje, ahora time.Time) error {
	r := msg.Interactive.CallPermissionReply
	numero := "+" + strings.TrimPrefix(msg.From, "+")
	if !numeroWA.MatchString(numero) {
		return nil
	}
	q := mensajeriabd.New(t)
	filas, err := q.PermisosDeNumero(ctx, mensajeriabd.PermisosDeNumeroParams{CanalID: canal.ID, Numero: numero})
	if err != nil {
		return err
	}
	if len(filas) == 0 {
		// Nadie se lo pidió desde Iris (lo dio desde el perfil del negocio): queda en la empresa dueña.
		if err := q.CrearPermisoLlamada(ctx, mensajeriabd.CrearPermisoLlamadaParams{EmpresaID: canal.EmpresaID, CanalID: canal.ID, Numero: numero}); err != nil {
			return err
		}
		if filas, err = q.PermisosDeNumero(ctx, mensajeriabd.PermisosDeNumeroParams{CanalID: canal.ID, Numero: numero}); err != nil {
			return err
		}
	}
	estado, vence := PermisoNegado, (*time.Time)(nil)
	if strings.EqualFold(r.Response, "accept") {
		estado = PermisoPermanente
		if !r.IsPermanent {
			estado = PermisoTemporal
			v := ahora.Add(PermisoTemporalDias * 24 * time.Hour)
			if n, err := r.ExpirationTimestamp.Int64(); err == nil && n > ahora.Unix() && n < ahora.Add(366*24*time.Hour).Unix() {
				v = time.Unix(n, 0).UTC()
			}
			vence = &v
		}
	}
	for _, f := range filas {
		// Si respondió, la solicitud le llegó: deja de estar sin confirmar (y cuenta para el límite).
		f.SolicitudSinConfirmar = false
		if estado != PermisoNegado {
			f.SinContestar = 0
		}
		if _, err := cambiarPermiso(ctx, t, f, estado, vence, nil); err != nil {
			return err
		}
	}
	return nil
}

// Llamar ------------------------------------------------------------------------------------------------

// EntradaLlamar es POST /conversaciones/{id}/llamar.
type EntradaLlamar struct {
	ID        int64  `json:"id"`
	Navegador string `json:"navegador,omitempty"`
	// Pestana: el id de la pestaña que llama (X-Iris-Pestana). Solo su flujo de eventos sostiene la llamada.
	Pestana         string `json:"pestana,omitempty"`
	DevolucionID    *int64 `json:"devolucionId,omitempty"`
	ConfirmarRiesgo bool   `json:"confirmarRiesgo,omitempty"`
	SDPOferta       string `json:"-"`
	// Huella: lo único del SDP que queda en la auditoría (la de la oferta saneada, la que va a Meta).
	Huella string `json:"sdp_sha256"`

	saneado string
	meta    *PermisoMeta
	// metaLeida: el permiso se acaba de leer de Meta (no es el recordado).
	metaLeida bool
}

// previoLlamar es lo que se lee antes de preguntar a Meta por el permiso. No sale por HTTP.
type previoLlamar struct {
	canal   CanalGraph
	numero  string
	permiso mensajeriabd.MensajeriaLlamadaPermiso
}

// MarshalJSON: nada de esto va a la auditoría (lleva el token del número).
func (previoLlamar) MarshalJSON() ([]byte, error) { return []byte("{}"), nil }

func nuevoPrevioLlamar(m *Modulo) *auditoria.Caso[EntradaLlamar, previoLlamar] {
	return auditoria.Nuevo(auditoria.Caso[EntradaLlamar, previoLlamar]{
		Accion: "llamadas.llamar", Permiso: nucleo.PermLlamadasLlamar, AuditarExito: sinAuditar[previoLlamar],
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaLlamar) (previoLlamar, error) {
			a, err := alcanceVoz(ctx, nucleo.PermLlamadasLlamar)
			if err != nil {
				return previoLlamar{}, conversacionClienteNoEncontrada()
			}
			t.FijarEmpresa(a.empresa)
			t.FijarEntidad("conversacion", in.ID)
			q := mensajeriabd.New(t)
			c, err := convVisible(ctx, q, a, in.ID, false)
			if err != nil {
				return previoLlamar{}, err
			}
			if in.Navegador == "" {
				return previoLlamar{}, reglaVoz("navegador_requerido", "Falta la cabecera X-Iris-Navegador con el id de este navegador")
			}
			if in.Pestana == "" {
				return previoLlamar{}, reglaVoz("pestana_requerida", "Falta la cabecera X-Iris-Pestana con el id de esta pestaña")
			}
			if in.saneado == "" {
				return previoLlamar{}, sdpInvalido("sdpOferta", "la oferta SDP no es válida")
			}
			canal, _, activas, err := canalParaSalientes(ctx, q, a, c)
			if err != nil {
				return previoLlamar{}, err
			}
			if !activas {
				return previoLlamar{}, llamadasInactivas()
			}
			cg, err := m.canalGraphDe(ctx, q, canal.ID)
			if err != nil {
				return previoLlamar{}, err
			}
			f, err := permisoDe(ctx, q, a.empresa, canal.ID, c.Numero, false)
			if err != nil {
				return previoLlamar{}, err
			}
			return previoLlamar{canal: cg, numero: c.Numero, permiso: f}, nil
		},
	})
}

// zonaVoz: los límites «por día» y «del mes» se cuentan en la hora local.
var zonaVoz = func() *time.Location {
	if z, err := time.LoadLocation("America/Guatemala"); err == nil {
		return z
	}
	return time.FixedZone("GT", -6*3600)
}()

func inicioDia(t time.Time) time.Time {
	l := t.In(zonaVoz)
	return time.Date(l.Year(), l.Month(), l.Day(), 0, 0, 0, 0, zonaVoz).UTC()
}

func inicioMes(t time.Time) time.Time {
	l := t.In(zonaVoz)
	return time.Date(l.Year(), l.Month(), 1, 0, 0, 0, 0, zonaVoz).UTC()
}

func nuevoLlamar(m *Modulo) *auditoria.Caso[EntradaLlamar, Llamada] {
	return auditoria.Nuevo(auditoria.Caso[EntradaLlamar, Llamada]{
		Accion: "llamadas.llamar", Permiso: nucleo.PermLlamadasLlamar,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaLlamar) (Llamada, error) {
			a, err := alcanceVoz(ctx, nucleo.PermLlamadasLlamar)
			if err != nil {
				return Llamada{}, conversacionClienteNoEncontrada()
			}
			t.FijarEmpresa(a.empresa)
			q := mensajeriabd.New(t)
			// Las salientes de una empresa se deciden de una en una: los conteos de abajo (una por
			// persona y por contacto, por día, por número y el tope del mes) no se burlan con peticiones
			// en paralelo. El índice único de saliente viva por usuario es la segunda barrera.
			if err := q.CandadoChat(ctx, "voz:salientes:"+strconv.FormatInt(a.empresa, 10)); err != nil {
				return Llamada{}, err
			}
			c, err := convVisible(ctx, q, a, in.ID, true)
			if err != nil {
				return Llamada{}, err
			}
			if in.Navegador == "" || in.saneado == "" || in.meta == nil {
				return Llamada{}, reglaVoz("navegador_requerido", "Falta la cabecera X-Iris-Navegador con el id de este navegador")
			}
			if in.Pestana == "" {
				return Llamada{}, reglaVoz("pestana_requerida", "Falta la cabecera X-Iris-Pestana con el id de esta pestaña")
			}
			canal, _, activas, err := canalParaSalientes(ctx, q, a, c)
			if err != nil {
				return Llamada{}, err
			}
			if !activas {
				return Llamada{}, llamadasInactivas()
			}
			ahora := m.ahora()
			// El permiso: Meta es la verdad. Lo que dijo se guarda (para el panel) y decide.
			f, err := permisoDe(ctx, q, a.empresa, canal.ID, c.Numero, true)
			if err != nil {
				return Llamada{}, err
			}
			if in.metaLeida {
				f.SincronizadoEn = &ahora
			}
			estado, vence := in.meta.Estado, in.meta.VenceEn
			if estado == PermisoSinPedir && (f.Estado == PermisoPedido || f.Estado == PermisoNegado || f.Estado == PermisoRevocado) {
				estado = f.Estado // Meta solo dice «no hay»: se conserva el porqué que ya sabíamos
			} else if estado == PermisoSinPedir && permisoVigente(f, ahora) {
				estado = PermisoRevocado
			}
			if f, err = cambiarPermiso(ctx, t, f, estado, vence, &c.ID); err != nil {
				return Llamada{}, err
			}
			if !permisoVigente(f, ahora) || !in.meta.PuedeLlamar {
				return Llamada{}, reglaVoz("sin_permiso_llamada", "Este contacto no ha dado permiso para que lo llamen por WhatsApp (o WhatsApp no deja llamarlo ahora)")
			}
			// Una llamada a la vez por persona y por contacto. La saliente viva cuenta en cualquier empresa
			// (quien opera en varias): es lo mismo que impone el índice único, pero con su 409.
			if viva, err := q.SalienteVivaDeUsuario(ctx, &a.usuario); err != nil {
				return Llamada{}, err
			} else if viva {
				return Llamada{}, nucleo.Conflicto("llamada_en_curso", "Ya tienes una llamada: termínala antes de hacer otra")
			}
			if _, err := q.LlamadaActivaDeUsuario(ctx, mensajeriabd.LlamadaActivaDeUsuarioParams{Empresa: a.empresa, UsuarioID: &a.usuario}); err == nil {
				return Llamada{}, nucleo.Conflicto("llamada_en_curso", "Ya tienes una llamada: termínala antes de hacer otra")
			} else if !errors.Is(err, pgx.ErrNoRows) {
				return Llamada{}, err
			}
			if viva, err := q.LlamadaVivaDeNumero(ctx, mensajeriabd.LlamadaVivaDeNumeroParams{CanalID: &canal.ID, Numero: c.Numero}); err != nil {
				return Llamada{}, err
			} else if viva {
				return Llamada{}, nucleo.Conflicto("llamada_en_curso", "Ya hay una llamada con este contacto")
			}
			// Límites: Meta (conectadas por número en 24 h), la empresa (por día) y el tope de minutos del mes.
			voz, err := ajustesVoz(ctx, q, a.empresa)
			if err != nil {
				return Llamada{}, err
			}
			if n, err := q.ConectadasDelCanalDesde(ctx, mensajeriabd.ConectadasDelCanalDesdeParams{CanalID: &canal.ID, Desde: ahora.Add(-24 * time.Hour)}); err != nil {
				return Llamada{}, err
			} else if n >= MaxConectadasCanal {
				return Llamada{}, reglaVoz("limite_llamadas", "Este número llegó al límite de WhatsApp de llamadas conectadas en 24 horas")
			}
			if n, err := q.SalientesDesde(ctx, mensajeriabd.SalientesDesdeParams{Empresa: a.empresa, Desde: inicioDia(ahora)}); err != nil {
				return Llamada{}, err
			} else if n >= int64(voz.MaxSalientesPorDia) {
				return Llamada{}, reglaVoz("limite_llamadas", "La empresa llegó a su límite de llamadas salientes de hoy")
			}
			seg, err := q.SegundosSalientes(ctx, mensajeriabd.SegundosSalientesParams{Empresa: a.empresa, Desde: inicioMes(ahora), Hasta: ahora.Add(time.Hour), Menos: uuid.Nil,
				Ahora: ahora})
			if err != nil {
				return Llamada{}, err
			}
			if seg >= int64(voz.TopeMinutosSalientesMes)*60 {
				return Llamada{}, reglaVoz("tope_alcanzado", "La empresa llegó a su tope de minutos de llamadas salientes de este mes")
			}
			// Timbrar y colgar: las que se colgaron mientras marcaban a este contacto hoy (0 = sin límite).
			if n, err := q.ColgadasMarcandoDesde(ctx, mensajeriabd.ColgadasMarcandoDesdeParams{Empresa: a.empresa, Numero: c.Numero, Desde: inicioDia(ahora)}); err != nil {
				return Llamada{}, err
			} else if voz.MaxColgadasMarcandoDia > 0 && n >= int64(voz.MaxColgadasMarcandoDia) {
				return Llamada{}, reglaVoz("limite_llamadas", "Hoy ya se colgaron varias llamadas a este contacto antes de que contestara: no se le puede volver a llamar hasta mañana",
					nucleo.ErrorCampo{Field: "contacto", Message: "true", Rule: "colgadas_marcando"})
			}
			if in.DevolucionID != nil {
				if _, err := q.DevolucionPendiente(ctx, mensajeriabd.DevolucionPendienteParams{ID: *in.DevolucionID, ConversacionID: c.ID}); errors.Is(err, pgx.ErrNoRows) {
					return Llamada{}, nucleo.Conflicto("devolucion_cerrada", "Esa devolución ya no está pendiente")
				} else if err != nil {
					return Llamada{}, err
				}
			}
			// Lo último, cuando ya se sabe que la llamada puede salir (ningún límite la para): el aviso de
			// que, si no contesta, WhatsApp revoca el permiso.
			if f.SinContestar >= SinContestarRevoca-1 && !in.ConfirmarRiesgo {
				return Llamada{}, reglaVoz("confirmar_riesgo", "Lleva tres llamadas seguidas sin contestar: si no contesta esta, WhatsApp revoca el permiso. Confirma para llamar",
					nucleo.ErrorCampo{Field: "confirmarRiesgo", Message: "true", Rule: "confirmar_riesgo"})
			}
			id, err := uuid.NewV7()
			if err != nil {
				return Llamada{}, err
			}
			t.FijarEntidad("llamada", id)
			l, err := q.InsertarLlamadaSaliente(ctx, mensajeriabd.InsertarLlamadaSalienteParams{LlamadaID: id, EmpresaID: c.EmpresaID, CanalID: &canal.ID, ConversacionID: &c.ID,
				Externo: "pendiente:" + id.String(), Numero: c.Numero, ClienteID: c.ClienteID, Servicio: c.ServicioID, ColaID: c.ColaID, UsuarioID: &a.usuario,
				Navegador: &in.Navegador, Pestana: &in.Pestana, PestanaVistaEn: &ahora, DevolucionID: in.DevolucionID, SonoEn: ahora, SuenaHasta: ahora.Add(SegundosMarcando * time.Second)})
			if err != nil {
				return Llamada{}, err
			}
			msg, err := m.agregarMensaje(ctx, t, &c, nuevoMensaje{direccion: DirSaliente, tipo: "llamada", autor: &a.usuario, creadoEn: ahora,
				estadoEntrega: nombrePtr(EntregaEnviado)})
			if err != nil {
				return Llamada{}, err
			}
			if err := q.FijarLlamadaDeMensaje(ctx, mensajeriabd.FijarLlamadaDeMensajeParams{ID: msg.ID, LlamadaID: pgUUID(&l.ID)}); err != nil {
				return Llamada{}, err
			}
			if err := q.FijarMensajeDeLlamada(ctx, mensajeriabd.FijarMensajeDeLlamadaParams{ID: l.ID, MensajeID: pgUUID(&msg.ID)}); err != nil {
				return Llamada{}, err
			}
			l.MensajeID = pgUUID(&msg.ID)
			if _, err := guardarConv(ctx, q, c, false); err != nil {
				return Llamada{}, err
			}
			if err := t.Emitir("llamada.saliente_iniciada", map[string]any{"llamada_id": l.ID, "conversacion_id": c.ID, "empresa_id": c.EmpresaID, "canal_id": canal.ID,
				"usuario_id": a.usuario, "devolucion_id": in.DevolucionID}); err != nil {
				return Llamada{}, err
			}
			if err := m.emitirEventoLlamada(ctx, t, l, EventoLlamadaActualizada, nil); err != nil {
				return Llamada{}, err
			}
			return m.aLlamada(ctx, q, a, l)
		},
	})
}

// resultadoLlamar es la tercera fase del connect.
type resultadoLlamar struct {
	ID     uuid.UUID `json:"id"`
	Fallo  bool      `json:"fallo"`
	callID string
}

// registrarHuerfana anota un call_id que existe en Meta y no es de ninguna llamada viva de Iris, para
// que el job de escrituras lo termine (con reintentos), y tapa su SDP en los webhooks guardados.
func registrarHuerfana(ctx context.Context, q *mensajeriabd.Queries, canal int64, callID string, ahora time.Time) error {
	if callID == "" || len(callID) > 200 || strings.HasPrefix(callID, "pendiente:") {
		return nil
	}
	if err := q.RegistrarHuerfana(ctx, mensajeriabd.RegistrarHuerfanaParams{CanalID: canal, CallID: callID, ProximoEn: ahora}); err != nil {
		return err
	}
	return taparSDPDeLlamada(ctx, q, canal, callID, ahora.Add(-10*time.Minute))
}

// nuevoResultadoLlamar guarda el call_id que dio Meta al connect (es el que manda), o deja la llamada
// fallida. Si la llamada ya no estaba marcando (se colgó mientras Meta respondía), ese call_id queda
// como huérfana para terminarla en Meta. Si un webhook se adelantó con otro call_id, no era de esta
// llamada: lo que trajo se descarta y ese otro se termina. Devuelve true si dejó algo por terminar.
func nuevoResultadoLlamar(m *Modulo) *auditoria.Caso[resultadoLlamar, bool] {
	return auditoria.Nuevo(auditoria.Caso[resultadoLlamar, bool]{
		Accion: "llamadas.resultado_llamar", Acceso: auditoria.Autenticado, SinEmpresa: true,
		Logica: func(ctx context.Context, t *auditoria.Tx, in resultadoLlamar) (bool, error) {
			if err := soloSistemaVoz(ctx); err != nil {
				return false, err
			}
			q := mensajeriabd.New(t)
			l, err := q.LlamadaParaActualizar(ctx, in.ID)
			if err != nil || l.CanalID == nil {
				return false, err
			}
			t.FijarEmpresa(derefEmpresa(l.EmpresaID))
			t.FijarEntidad("llamada", l.ID)
			ahora := m.ahora()
			var c *mensajeriabd.MensajeriaConversacion
			if l.ConversacionID != nil {
				f, err := q.ConversacionClienteParaActualizar(ctx, *l.ConversacionID)
				if err != nil {
					return false, err
				}
				c = &f
			}
			adelantado := l.ExternoWebhook
			if llamadaFinal(l.Estado) {
				// Se colgó mientras el connect estaba en vuelo: en Meta la llamada existe.
				pendientes := false
				for _, id := range []string{in.callID, derefTexto(adelantado)} {
					if !in.Fallo || id != in.callID {
						if err := registrarHuerfana(ctx, q, *l.CanalID, id, ahora); err != nil {
							return false, err
						}
						pendientes = pendientes || id != ""
					}
				}
				return pendientes, nil
			}
			if in.Fallo {
				if err := registrarHuerfana(ctx, q, *l.CanalID, derefTexto(adelantado), ahora); err != nil {
					return false, err
				}
				return adelantado != nil, m.cerrarLlamada(ctx, t, &l, c, LlamadaFallida, "error_meta", ahora, "")
			}
			g, err := q.FijarExternoLlamada(ctx, mensajeriabd.FijarExternoLlamadaParams{ID: l.ID, Externo: in.callID})
			if errors.Is(err, pgx.ErrNoRows) {
				return false, nil
			}
			if err != nil {
				return false, err
			}
			l = g
			if adelantado == nil {
				return false, nil
			}
			// Un webhook llegó antes que esta respuesta. Solo vale si traía este mismo call_id.
			if *adelantado != in.callID {
				if err := q.QuitarWebhookAdelantado(ctx, mensajeriabd.QuitarWebhookAdelantadoParams{ID: l.ID, Conservar: false}); err != nil {
					return false, err
				}
				t.Anotar(map[string]any{"webhook_de_otra_llamada": true})
				return true, registrarHuerfana(ctx, q, *l.CanalID, *adelantado, ahora)
			}
			if err := q.QuitarWebhookAdelantado(ctx, mensajeriabd.QuitarWebhookAdelantadoParams{ID: l.ID, Conservar: true}); err != nil {
				return false, err
			}
			if l.SdpRespuesta == nil {
				// Era de esta llamada, pero su respuesta SDP no pasó la validación.
				if err := m.cerrarLlamada(ctx, t, &l, c, LlamadaFallida, "error_meta", ahora, MetaTerminar); err != nil {
					return false, err
				}
				if c != nil {
					_, err = guardarConv(ctx, q, *c, false)
				}
				return false, err
			}
			return false, m.emitirEventoLlamada(ctx, t, l, EventoLlamadaRespuesta, l.UsuarioID)
		},
	})
}

func derefTexto(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// Llamar hace la saliente en fases, sin llamar a Meta dentro de una transacción: lee (permiso del
// usuario, conversación, número), pregunta a Meta por el permiso del contacto (lectura recordada
// 60 s), guarda la llamada marcando con la intención connect, manda el connect con la oferta saneada
// y guarda el call_id. Si Meta lo rechaza: 502 meta_rechazo y la llamada queda fallida.
func (m *Modulo) Llamar(ctx context.Context, in EntradaLlamar) (Llamada, error) {
	if s, err := SanearSDP(in.SDPOferta); err == nil {
		in.saneado, in.Huella = s, huellaSDP(s)
	}
	pre, err := m.Voz.previoLlamar.Ejecutar(ctx, in)
	if err != nil {
		return Llamada{}, err
	}
	meta := PermisoMeta{Estado: pre.permiso.Estado, VenceEn: pre.permiso.VenceEn, PuedeLlamar: permisoVigente(pre.permiso, m.ahora())}
	if pre.permiso.SincronizadoEn == nil || m.ahora().Sub(*pre.permiso.SincronizadoEn) >= VigenciaPermisoMeta || !meta.PuedeLlamar {
		if meta, err = m.graph().Permiso(ctx, pre.canal, pre.numero); err != nil {
			return Llamada{}, metaRechazo()
		}
		in.metaLeida = true
	}
	if !slices.Contains([]string{PermisoTemporal, PermisoPermanente, PermisoPedido, PermisoNegado, PermisoRevocado}, meta.Estado) {
		meta.Estado = PermisoSinPedir
	}
	in.meta = &meta
	l, err := m.Voz.llamar.Ejecutar(ctx, in)
	if err != nil {
		return Llamada{}, err
	}
	bd := nucleo.BDDe(ctx)
	sctx := ctxSistema(context.WithoutCancel(ctx), bd)
	callID, errMeta := m.graph().Llamar(sctx, pre.canal, pre.numero, in.saneado)
	if errMeta == nil && (callID == "" || len(callID) > 200 || strings.HasPrefix(callID, "pendiente:")) {
		errMeta = &ErrMetaLlamada{SinRespuesta: true}
	}
	porTerminar, err := m.Voz.resultadoLlamar.Ejecutar(sctx, resultadoLlamar{ID: l.ID, Fallo: errMeta != nil, callID: callID})
	if err != nil {
		return Llamada{}, err
	}
	// Lo que quedó por terminar en Meta se intenta ya; si falla, lo repite el job de escrituras.
	if porTerminar {
		if err := m.despacharHuerfanas(sctx, bd); err != nil {
			slog.WarnContext(ctx, "voz: terminar en Meta una llamada sin dueño", "err", err)
		}
	}
	if errMeta != nil {
		return Llamada{}, metaRechazo()
	}
	return m.Voz.Ver.Ejecutar(ctx, EntradaLlamada{ID: l.ID})
}

// RespuestaSDP es GET /llamadas/{id}/respuesta-sdp.
type RespuestaSDP struct {
	SDPRespuesta string `json:"sdpRespuesta" doc:"La respuesta SDP de Meta para el RTCPeerConnection del navegador"`
}

func nuevoRespuestaSDP(*Modulo) *auditoria.Caso[EntradaLlamada, RespuestaSDP] {
	return auditoria.Nuevo(auditoria.Caso[EntradaLlamada, RespuestaSDP]{
		Accion: "llamadas.respuesta_sdp", Permiso: nucleo.PermLlamadasLlamar, AuditarExito: sinAuditar[RespuestaSDP],
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaLlamada) (RespuestaSDP, error) {
			a, err := alcanceVoz(ctx, nucleo.PermLlamadasLlamar)
			if err != nil {
				return RespuestaSDP{}, err
			}
			l, err := mensajeriabd.New(t).LlamadaPorID(ctx, in.ID)
			// Solo quien llamó, en su empresa: cualquier otro, 404 (también un supervisor).
			if err != nil || derefEmpresa(l.EmpresaID) != a.empresa || l.Direccion != "saliente" || l.UsuarioID == nil || *l.UsuarioID != a.usuario {
				return RespuestaSDP{}, llamadaNoEncontrada()
			}
			if llamadaFinal(l.Estado) {
				return RespuestaSDP{}, llamadaTerminada()
			}
			if l.Navegador == nil || *l.Navegador != in.Navegador {
				return RespuestaSDP{}, otroNavegador()
			}
			// La respuesta es para la pestaña que tiene el audio: otra pestaña del mismo navegador no la recibe.
			if l.Pestana == nil || *l.Pestana != in.Pestana {
				return RespuestaSDP{}, nucleo.NuevoProblema(http.StatusConflict, "llamada_ya_contestada", "Conflicto", "Esta llamada se hizo desde otra pestaña",
					nucleo.ErrorCampo{Field: "pestana", Message: "true", Rule: "otra_pestana"})
			}
			// Lo que trajo un webhook adelantado no se entrega hasta saber que es de esta llamada.
			if l.SdpRespuesta == nil || l.ExternoWebhook != nil {
				return RespuestaSDP{}, nucleo.Conflicto("sin_respuesta", "WhatsApp todavía no mandó la respuesta de esta llamada")
			}
			return RespuestaSDP{SDPRespuesta: *l.SdpRespuesta}, nil
		},
	})
}

// Webhooks de una saliente --------------------------------------------------------------------------------

// eventoSaliente aplica a una saliente el connect de Meta (la respuesta SDP del cliente) y sus
// estados (RINGING, ACCEPTED, REJECTED). El terminate lo aplica terminarPorMeta, como en las entrantes.
//
// La llamada se busca solo por su call_id. Si no hay ninguna con ese call_id:
//   - y hay una saliente a ese número que todavía espera la respuesta del connect, el webhook pudo
//     adelantarse: lo que trae queda apartado en esa llamada, sin aplicarse, hasta que la respuesta
//     del connect diga su call_id (resultadoLlamar). No se vincula por número;
//   - si no, es una llamada del negocio que Iris no tiene viva: se termina en Meta (huérfana).
func (m *Modulo) eventoSaliente(ctx context.Context, t *auditoria.Tx, canal mensajeriabd.MensajeriaCanal, ev metaLlamada, ahora time.Time) error {
	q := mensajeriabd.New(t)
	esConnect := strings.EqualFold(ev.Event, "connect")
	estado := strings.ToUpper(ev.Status)
	l, err := q.LlamadaPorExterno(ctx, mensajeriabd.LlamadaPorExternoParams{CanalID: &canal.ID, IDExterno: ev.ID})
	if errors.Is(err, pgx.ErrNoRows) {
		numero := "+" + strings.TrimPrefix(ev.To, "+")
		if numeroWA.MatchString(numero) {
			p, err := q.SalienteSinCallID(ctx, mensajeriabd.SalienteSinCallIDParams{CanalID: &canal.ID, Numero: numero})
			if err == nil {
				if !esConnect || p.ExternoWebhook != nil {
					return nil // un estado, o un segundo connect, antes de saber el call_id: no se aplica
				}
				var sdp *string
				if ev.Session != nil && ev.Session.SDP != "" && ValidarSDP(ev.Session.SDP) == nil {
					sdp = &ev.Session.SDP
				}
				return q.FijarWebhookAdelantado(ctx, mensajeriabd.FijarWebhookAdelantadoParams{ID: p.ID, Externo: &ev.ID, Sdp: sdp})
			}
			if !errors.Is(err, pgx.ErrNoRows) {
				return err
			}
		}
		// Sin llamada viva para ese call_id. Solo se termina en Meta si hay rastro de que es de Iris: un
		// connect de Iris a ese mismo número en los últimos 2 minutos (pendiente, cerrada o fallida).
		// Si no lo hay, el número puede estar haciendo salientes desde otro sistema: no se toca; queda
		// anotada (sin el número) y se avisa para diagnóstico.
		if !esConnect && estado != "RINGING" && estado != "ACCEPTED" {
			return nil
		}
		nuestra := false
		if numeroWA.MatchString(numero) {
			if nuestra, err = q.SalienteRecienteANumero(ctx, mensajeriabd.SalienteRecienteANumeroParams{CanalID: &canal.ID, Numero: numero, Desde: ahora.Add(-VentanaSalienteNuestra)}); err != nil {
				return err
			}
		}
		if nuestra {
			t.Anotar(map[string]any{"saliente_sin_llamada": "terminada"})
			return registrarHuerfana(ctx, q, canal.ID, ev.ID, ahora)
		}
		if esConnect {
			if err := taparSDPDeLlamada(ctx, q, canal.ID, ev.ID, ahora.Add(-10*time.Minute)); err != nil {
				return err
			}
		}
		id, err := q.RegistrarAjena(ctx, mensajeriabd.RegistrarAjenaParams{CanalID: canal.ID, CallID: ev.ID, Ahora: ahora})
		if errors.Is(err, pgx.ErrNoRows) {
			return nil // ya estaba anotada: un aviso por llamada
		}
		if err != nil {
			return err
		}
		t.Anotar(map[string]any{"saliente_sin_llamada": "ajena", "huerfana_id": id})
		slog.WarnContext(ctx, "voz: llamada saliente del número que no es de Iris; no se toca", "canal_id", canal.ID, "huerfana_id", id)
		return nil
	}
	if err != nil {
		return err
	}
	if l.Direccion != "saliente" {
		return nil
	}
	if l, err = q.LlamadaParaActualizar(ctx, l.ID); err != nil {
		return err
	}
	if llamadaFinal(l.Estado) {
		// Ya está cerrada en Iris: el SDP que traiga no se queda guardado y, si Meta la da por viva
		// sin que Iris le haya pedido terminarla, se termina.
		if esConnect {
			if err := taparSDPDeLlamada(ctx, q, canal.ID, ev.ID, ahora.Add(-10*time.Minute)); err != nil {
				return err
			}
		}
		if (esConnect || estado == "RINGING" || estado == "ACCEPTED") && (l.AccionMeta == nil || *l.AccionMeta != MetaTerminar) {
			return registrarHuerfana(ctx, q, canal.ID, ev.ID, ahora)
		}
		return nil
	}
	t.FijarEmpresa(derefEmpresa(l.EmpresaID))
	var c *mensajeriabd.MensajeriaConversacion
	if l.ConversacionID != nil {
		f, err := q.ConversacionClienteParaActualizar(ctx, *l.ConversacionID)
		if err != nil {
			return err
		}
		c = &f
	}
	cerrar := func(estado, motivo, accion string) error {
		if err := m.cerrarLlamada(ctx, t, &l, c, estado, motivo, ahora, accion); err != nil {
			return err
		}
		if c != nil {
			_, err := guardarConv(ctx, q, *c, false)
			return err
		}
		return nil
	}
	if strings.EqualFold(ev.Event, "connect") {
		if l.Estado != LlamadaMarcando || ev.Session == nil || ev.Session.SDP == "" {
			return nil
		}
		// La respuesta de Meta pasa la validación estricta antes de entregarse al navegador.
		if err := ValidarSDP(ev.Session.SDP); err != nil {
			return cerrar(LlamadaFallida, "error_meta", MetaTerminar)
		}
		if err := q.FijarRespuestaSDP(ctx, mensajeriabd.FijarRespuestaSDPParams{ID: l.ID, Sdp: &ev.Session.SDP}); err != nil {
			return err
		}
		return m.emitirEventoLlamada(ctx, t, l, EventoLlamadaRespuesta, l.UsuarioID)
	}
	switch estado {
	case "ACCEPTED":
		return m.salienteContestada(ctx, t, &l, ahora)
	case "REJECTED":
		if l.Estado == LlamadaMarcando {
			return cerrar(LlamadaPerdida, "nadie_contesto", "")
		}
	}
	return nil
}

// salienteContestada pasa una saliente que marca a en_curso: el cliente contestó en `cuando`. Reinicia
// la cuenta de llamadas sin contestar y deja hecha la devolución que atendía. La llama el estado
// ACCEPTED y, si Meta no lo mandó, el terminate que trae una duración (terminarPorMeta).
func (m *Modulo) salienteContestada(ctx context.Context, t *auditoria.Tx, l *mensajeriabd.MensajeriaLlamada, cuando time.Time) error {
	if l.Estado != LlamadaMarcando || l.UsuarioID == nil || l.CanalID == nil {
		return nil
	}
	q := mensajeriabd.New(t)
	var err error
	persona := "persona"
	l.Estado, l.AtendidaPor, l.ContestadaEn = LlamadaEnCurso, &persona, &cuando
	if err := q.InsertarTramo(ctx, mensajeriabd.InsertarTramoParams{LlamadaID: l.ID, UsuarioID: *l.UsuarioID, Comienzo: cuando}); err != nil {
		return err
	}
	if *l, err = guardarLlamada(ctx, q, *l); err != nil {
		return err
	}
	if f, err := q.PermisoLlamadaParaActualizar(ctx, mensajeriabd.PermisoLlamadaParaActualizarParams{Empresa: derefEmpresa(l.EmpresaID), CanalID: *l.CanalID, Numero: l.Numero}); err == nil {
		f.SinContestar = 0
		if _, err := guardarPermiso(ctx, q, f); err != nil {
			return err
		}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if l.DevolucionID != nil {
		d, err := q.MarcarDevolucionHecha(ctx, mensajeriabd.MarcarDevolucionHechaParams{ID: *l.DevolucionID, UsuarioID: l.UsuarioID, LlamadaID: pgUUID(&l.ID)})
		if err == nil {
			if err := t.Emitir("llamada.devolucion_cerrada", map[string]any{"devolucion_id": d.ID, "conversacion_id": d.ConversacionID, "empresa_id": d.EmpresaID,
				"llamada_id": d.LlamadaID, "estado": "hecha", "usuario_id": l.UsuarioID, "llamada_devuelta_id": l.ID}); err != nil {
				return err
			}
			if err := m.emitirEventoLlamada(ctx, t, *l, EventoDevolucion, nil); err != nil {
				return err
			}
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
	}
	if err := t.Emitir("llamada.contestada", map[string]any{"llamada_id": l.ID, "conversacion_id": l.ConversacionID, "empresa_id": l.EmpresaID,
		"atendida_por": "persona", "usuario_id": l.UsuarioID, "espera_seg": int(max(0, cuando.Sub(l.SonoEn)/time.Second))}); err != nil {
		return err
	}
	return m.emitirEventoLlamada(ctx, t, *l, EventoLlamadaActualizada, nil)
}

// trasSaliente corre al cerrar una saliente: una sin contestar cuenta para la revocación del permiso
// (a la cuarta Meta lo revoca) y una contestada, para el tope de minutos del mes.
func (m *Modulo) trasSaliente(ctx context.Context, t *auditoria.Tx, l *mensajeriabd.MensajeriaLlamada, ahora time.Time) error {
	q := mensajeriabd.New(t)
	if err := q.FijarRespuestaSDP(ctx, mensajeriabd.FijarRespuestaSDPParams{ID: l.ID}); err != nil {
		return err
	}
	l.SdpRespuesta = nil
	if l.ExternoWebhook != nil && l.CanalID != nil {
		if err := taparSDPDeLlamada(ctx, q, *l.CanalID, *l.ExternoWebhook, ahora.Add(-10*time.Minute)); err != nil {
			return err
		}
	}
	empresa := derefEmpresa(l.EmpresaID)
	// También cuenta como sin contestar la que el servidor cortó mientras marcaba (la pestaña que llamó
	// murió) si llegó a sonar en Meta (el connect se aceptó): Meta sí la cuenta, y confirmarRiesgo no
	// puede ir por detrás de esa cuenta.
	cortada := l.Estado == LlamadaTerminada && l.ContestadaEn == nil && l.MotivoFin != nil && *l.MotivoFin == "error_medio" &&
		!strings.HasPrefix(l.IDExterno, "pendiente:")
	if (l.Estado == LlamadaPerdida || cortada) && l.CanalID != nil {
		f, err := q.PermisoLlamadaParaActualizar(ctx, mensajeriabd.PermisoLlamadaParaActualizarParams{Empresa: empresa, CanalID: *l.CanalID, Numero: l.Numero})
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		f.SinContestar++
		if f.SinContestar >= SinContestarRevoca && permisoVigente(f, ahora) {
			_, err = cambiarPermiso(ctx, t, f, PermisoRevocado, nil, l.ConversacionID)
			return err
		}
		_, err = guardarPermiso(ctx, q, f)
		return err
	}
	if l.Estado != LlamadaTerminada || l.DuracionSeg == nil || l.ContestadaEn == nil {
		return nil
	}
	voz, err := ajustesVoz(ctx, q, empresa)
	if err != nil {
		return err
	}
	tope := int64(voz.TopeMinutosSalientesMes) * 60
	if tope <= 0 {
		return nil
	}
	antes, err := q.SegundosSalientes(ctx, mensajeriabd.SegundosSalientesParams{Empresa: empresa, Desde: inicioMes(ahora), Hasta: ahora.Add(time.Hour), Menos: l.ID,
		Ahora: ahora})
	if err != nil {
		return err
	}
	despues := antes + int64(*l.DuracionSeg)
	for _, p := range []int64{80, 100} {
		if antes*100 < tope*p && despues*100 >= tope*p {
			if err := t.Emitir("voz.tope_alcanzado", map[string]any{"empresa_id": l.EmpresaID, "tipo": "salientes", "porcentaje": p,
				"mes": ahora.In(zonaVoz).Format("2006-01"), "minutos": despues / 60, "tope": voz.TopeMinutosSalientesMes}); err != nil {
				return err
			}
		}
	}
	return nil
}

// Graph: connect y call_permissions (supuestos de §10.1-5) -----------------------------------------------

func (g graphHTTP) Llamar(ctx context.Context, canal CanalGraph, numero, sdpOferta string) (string, error) {
	b, err := g.pedir(ctx, http.MethodPost, canal, "/calls", map[string]any{"messaging_product": "whatsapp", "to": strings.TrimPrefix(numero, "+"),
		"action": MetaConectar, "session": map[string]any{"sdp_type": "offer", "sdp": sdpOferta}})
	if err != nil {
		return "", err
	}
	var r struct {
		Calls []struct {
			ID string `json:"id"`
		} `json:"calls"`
	}
	if json.Unmarshal(b, &r) != nil || len(r.Calls) == 0 || r.Calls[0].ID == "" {
		return "", &ErrMetaLlamada{SinRespuesta: true}
	}
	return r.Calls[0].ID, nil
}

func (g graphHTTP) Permiso(ctx context.Context, canal CanalGraph, numero string) (PermisoMeta, error) {
	b, err := g.pedir(ctx, http.MethodGet, canal, "/call_permissions?user_wa_id="+url.QueryEscape(strings.TrimPrefix(numero, "+")), nil)
	if err != nil {
		return PermisoMeta{}, err
	}
	var r struct {
		Permission struct {
			Status         string `json:"status"`
			ExpirationTime int64  `json:"expiration_time"`
		} `json:"permission"`
		Actions []struct {
			Name   string `json:"action_name"`
			Puede  bool   `json:"can_perform_action"`
			Limits []struct {
				Periodo string `json:"time_period"`
				Uso     *int   `json:"current_usage"`
			} `json:"limits"`
		} `json:"actions"`
	}
	if json.Unmarshal(b, &r) != nil {
		return PermisoMeta{}, &ErrMetaLlamada{SinRespuesta: true}
	}
	p := PermisoMeta{Estado: PermisoSinPedir}
	switch strings.ToLower(r.Permission.Status) {
	case "temporary":
		p.Estado = PermisoTemporal
		if r.Permission.ExpirationTime > 0 {
			v := time.Unix(r.Permission.ExpirationTime, 0).UTC()
			p.VenceEn = &v
		}
	case "permanent":
		p.Estado = PermisoPermanente
	}
	for _, a := range r.Actions {
		switch a.Name {
		case "start_call":
			p.PuedeLlamar = a.Puede
		case "send_call_permission_request":
			p.PuedeSolicitar = a.Puede
			for _, l := range a.Limits {
				if l.Periodo == "PT24H" && l.Uso != nil {
					p.SolicitudesEnDia = l.Uso
				}
			}
		}
	}
	// Sin permiso no se llama, diga lo que diga la lista de acciones.
	p.PuedeLlamar = p.PuedeLlamar && p.Estado != PermisoSinPedir
	return p, nil
}

// PlantillaMetaInfo es lo que dice Meta de una plantilla de la cuenta.
type PlantillaMetaInfo struct {
	Existe    bool
	Aprobada  bool
	Categoria string
}

// Plantilla busca en la cuenta de WhatsApp (WABA) una plantilla por nombre e idioma (supuesto de
// §10.1: GET /{waba_id}/message_templates?name=…).
func (g graphHTTP) Plantilla(ctx context.Context, canal CanalGraph, nombre, idioma string) (PlantillaMetaInfo, error) {
	if canal.WabaID == "" {
		return PlantillaMetaInfo{}, &ErrMetaLlamada{Status: http.StatusNotFound}
	}
	b, err := g.pedir(ctx, http.MethodGet, CanalGraph{PhoneNumberID: canal.WabaID, Token: canal.Token},
		"/message_templates?fields=name,status,category,language&limit=50&name="+url.QueryEscape(nombre), nil)
	if err != nil {
		return PlantillaMetaInfo{}, err
	}
	var r struct {
		Data []struct {
			Name     string `json:"name"`
			Status   string `json:"status"`
			Category string `json:"category"`
			Language string `json:"language"`
		} `json:"data"`
	}
	if json.Unmarshal(b, &r) != nil {
		return PlantillaMetaInfo{}, &ErrMetaLlamada{SinRespuesta: true}
	}
	for _, d := range r.Data {
		if d.Name == nombre && d.Language == idioma {
			return PlantillaMetaInfo{Existe: true, Aprobada: strings.EqualFold(d.Status, "APPROVED"), Categoria: strings.ToUpper(d.Category)}, nil
		}
	}
	return PlantillaMetaInfo{}, nil
}

// despacharHuerfanas termina en Meta las llamadas que existen allá y no en Iris. terminate es
// idempotente por call_id; se reintenta (apartadas lote × plazo) hasta 2 minutos y 6 intentos.
func (m *Modulo) despacharHuerfanas(ctx context.Context, bd nucleo.BD) error {
	q := mensajeriabd.New(bd)
	ahora := m.ahora()
	filas, err := q.TomarHuerfanas(ctx, mensajeriabd.TomarHuerfanasParams{Ahora: ahora, Lote: LoteMetaVoz, Aparte: ahora.Add(LoteMetaVoz * PlazoMeta)})
	if err != nil {
		return err
	}
	for _, h := range filas {
		estado := "hecha"
		cg, err := m.canalGraphDe(ctx, q, h.CanalID)
		if err == nil {
			err = m.graph().Accion(ctx, cg, h.CallID, MetaTerminar, "", false)
		}
		if err != nil {
			if h.Intentos < 6 {
				slog.WarnContext(ctx, "voz: terminar en Meta una llamada sin dueño; se reintenta", "huerfana_id", h.ID, "intento", h.Intentos)
				continue
			}
			estado = "agotada"
		}
		if err := q.CerrarHuerfana(ctx, mensajeriabd.CerrarHuerfanaParams{ID: h.ID, Estado: estado}); err != nil {
			return err
		}
	}
	return nil
}

// solicitudDePermisoFallida corre cuando el mensaje de una solicitud de permiso queda fallido.
//
//   - Si se sabe que no salió (Meta lo rechazó, la plantilla ya no está, el número se desactivó…): la
//     solicitud se deshace: no cuenta para el límite de 1 por 24 h y 2 por 7 días y el permiso vuelve
//     a «sin pedir».
//   - Si los intentos se agotaron sin respuesta de Meta (sinRespuesta), pudo llegar al cliente: no se
//     descuenta a ciegas. Queda «sin confirmar» (sigue contando) y la resuelve el job de escrituras
//     preguntando a Meta (resolverSolicitudes), o la respuesta del cliente si llega antes.
func (m *Modulo) solicitudDePermisoFallida(ctx context.Context, t *auditoria.Tx, f mensajeriabd.MensajeriaConversacionMensaje, c mensajeriabd.MensajeriaConversacion, sinRespuesta bool) error {
	q := mensajeriabd.New(t)
	p, err := q.PermisoLlamadaParaActualizar(ctx, mensajeriabd.PermisoLlamadaParaActualizarParams{Empresa: empresaDeConv(c), CanalID: c.CanalID, Numero: c.Numero})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if id := uuidDe(p.UltimaSolicitudMensajeID); id == nil || *id != f.ID {
		return nil
	}
	if sinRespuesta {
		return q.MarcarSolicitudSinConfirmar(ctx, p.ID)
	}
	_, err = descontarSolicitud(ctx, t, p, &c.ID)
	return err
}

// descontarSolicitud quita la última solicitud de la cuenta y, si el permiso seguía «pedido», lo
// devuelve a «sin pedir».
func descontarSolicitud(ctx context.Context, t *auditoria.Tx, p mensajeriabd.MensajeriaLlamadaPermiso, conv *int64) (mensajeriabd.MensajeriaLlamadaPermiso, error) {
	solicitudes := solicitudesDe(p.Solicitudes)
	if n := len(solicitudes); n > 0 {
		slices.SortFunc(solicitudes, func(a, b time.Time) int { return a.Compare(b) })
		solicitudes = solicitudes[:n-1]
	}
	if solicitudes == nil {
		solicitudes = []time.Time{}
	}
	p.Solicitudes, _ = json.Marshal(solicitudes)
	p.UltimaSolicitudMensajeID = pgUUID(nil)
	p.SolicitudSinConfirmar = false
	estado := p.Estado
	if estado == PermisoPedido {
		estado = PermisoSinPedir
	}
	return cambiarPermiso(ctx, t, p, estado, p.VenceEn, conv)
}

// solicitudResuelta es lo que dijo Meta de una solicitud sin confirmar.
type solicitudResuelta struct {
	ID int64 `json:"permiso_id"`
	// Resultado: llego (Meta la cuenta o hay permiso), no_llego (Meta no la cuenta: se libera el cupo)
	// o vencida (pasó un día sin saberlo: ya no cuenta para el límite de 24 h y se deja de preguntar).
	Resultado string `json:"resultado"`
	meta      PermisoMeta
}

func nuevoResolverSolicitud(m *Modulo) *auditoria.Caso[solicitudResuelta, bool] {
	return auditoria.Nuevo(auditoria.Caso[solicitudResuelta, bool]{
		Accion: "llamadas.resolver_solicitud", Acceso: auditoria.Autenticado, SinEmpresa: true,
		Logica: func(ctx context.Context, t *auditoria.Tx, in solicitudResuelta) (bool, error) {
			if err := soloSistemaVoz(ctx); err != nil {
				return false, err
			}
			q := mensajeriabd.New(t)
			p, err := q.PermisoLlamadaPorID(ctx, in.ID)
			if err != nil || !p.SolicitudSinConfirmar {
				return false, err
			}
			t.FijarEmpresa(derefEmpresa(p.EmpresaID))
			t.FijarEntidad("llamada_permiso", p.ID)
			switch in.Resultado {
			case "no_llego":
				_, err = descontarSolicitud(ctx, t, p, nil)
			default:
				p.SolicitudSinConfirmar = false
				estado, vence := p.Estado, p.VenceEn
				if in.meta.Estado == PermisoTemporal || in.meta.Estado == PermisoPermanente {
					estado, vence = in.meta.Estado, in.meta.VenceEn
				}
				_, err = cambiarPermiso(ctx, t, p, estado, vence, nil)
			}
			return true, err
		},
	})
}

// resolverSolicitudes pregunta a Meta por las solicitudes de permiso que quedaron sin confirmar.
// Solo se libera el cupo si Meta dice que no cuenta ninguna solicitud a ese número; si la cuenta, o
// ya hay permiso, llegó. Si Meta no lo dice o no responde, sigue contando y se vuelve a preguntar
// con espera creciente (1, 2, 5, 15 y después cada 60 minutos) hasta que pase un día.
func (m *Modulo) resolverSolicitudes(ctx context.Context, bd nucleo.BD) error {
	q := mensajeriabd.New(bd)
	ahora := m.ahora()
	filas, err := q.TomarSolicitudesSinConfirmar(ctx, mensajeriabd.TomarSolicitudesSinConfirmarParams{Ahora: ahora, Lote: LoteMetaVoz})
	if err != nil {
		return err
	}
	for _, p := range filas {
		r := solicitudResuelta{ID: p.ID}
		ultima := time.Time{}
		for _, s := range solicitudesDe(p.Solicitudes) {
			if s.After(ultima) {
				ultima = s
			}
		}
		if ahora.Sub(ultima) >= 24*time.Hour {
			r.Resultado = "vencida"
		} else {
			cg, err := m.canalGraphDe(ctx, q, p.CanalID)
			if err != nil {
				continue
			}
			meta, err := m.graph().Permiso(ctx, cg, p.Numero)
			if err != nil {
				continue // Meta no responde: sigue contando; se pregunta otra vez más adelante
			}
			r.meta = meta
			switch {
			case meta.Estado == PermisoTemporal || meta.Estado == PermisoPermanente:
				r.Resultado = "llego"
			case meta.SolicitudesEnDia == nil:
				continue // Meta no dice cuántas cuenta: no se libera a ciegas
			case *meta.SolicitudesEnDia == 0:
				r.Resultado = "no_llego"
			default:
				r.Resultado = "llego"
			}
		}
		if _, err := m.Voz.resolverSolicitud.Ejecutar(ctx, r); err != nil {
			slog.WarnContext(ctx, "voz: resolver una solicitud de permiso sin confirmar", "permiso_id", p.ID, "err", err)
		}
	}
	return nil
}

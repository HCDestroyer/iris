package mensajeria

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	mensajeriabd "github.com/HCDestroyer/iris/apps/api/internal/mensajeria/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/adjuntos"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
)

// Bandeja de conversaciones con clientes (contratos/conversaciones.md): modelo, alcance y feed.

// Estados de una conversación.
const (
	EstadoConvAbierta   = "abierta"
	EstadoConvEsperando = "esperando_cliente"
	EstadoConvResuelta  = "resuelta"
)

// Dirección de un mensaje.
const (
	DirEntrante = "entrante"
	DirSaliente = "saliente"
	DirNota     = "nota"
	DirSistema  = "sistema"
)

// Estados de entrega de un mensaje saliente.
const (
	EntregaEnCola    = "en_cola"
	EntregaEnviado   = "enviado"
	EntregaEntregado = "entregado"
	EntregaLeido     = "leido"
	EntregaFallido   = "fallido"
)

// Tipos de evento del feed de la bandeja.
const (
	EventoConvMensajeNuevo      = "mensaje_nuevo"
	EventoConvMensajeActualizad = "mensaje_actualizado"
	EventoConvActualizada       = "conversacion_actualizada"
	EventoConvMencion           = "mencion"
	EventoConvGuardia           = "guardia"
	EventoConvAvisoSinAtender   = "aviso_sin_atender"
)

// CanalConversaciones: LISTEN/NOTIFY del feed de la bandeja (carga: la empresa, «0» sin empresa).
const CanalConversaciones = "iris_conversaciones"

// EntidadAdjuntosConversacion es la conversación con un cliente en el módulo de adjuntos.
const EntidadAdjuntosConversacion = "conversacion"

// MaxTextoWhatsApp: caracteres de un mensaje de texto de WhatsApp.
const MaxTextoWhatsApp = 4096

// VentanaWhatsApp: después del último mensaje del cliente solo se pueden mandar plantillas.
const VentanaWhatsApp = 24 * time.Hour

func init() {
	for _, t := range []string{"mensajeria.colas", "mensajeria.etiquetas", "mensajeria.respuestas_rapidas", "mensajeria.contactos_iris",
		"mensajeria.conversaciones", "mensajeria.conversacion_mensajes", "mensajeria.conv_eventos", "mensajeria.boletas", "mensajeria.promesas",
		"mensajeria.disponibilidad", "mensajeria.ajustes_bandeja"} {
		nucleo.RegistrarTablaConEmpresa(t)
	}
}

// Tipos de la API ----------------------------------------------------------------------------

// UsuarioRef es un usuario de Iris tal como lo muestra la bandeja.
type UsuarioRef struct {
	ID     int64  `json:"id"`
	Nombre string `json:"nombre"`
}

// CanalRef es el número de WhatsApp de la conversación.
type CanalRef struct {
	ID     int64    `json:"id"`
	Nombre string   `json:"nombre"`
	Numero string   `json:"numero" doc:"Número remitente en E.164"`
	Usos   []string `json:"usos" doc:"avisos, soporte, cobros, ventas"`
}

// ColaRef es la cola de la conversación.
type ColaRef struct {
	ID     int64  `json:"id"`
	Nombre string `json:"nombre"`
}

// EtiquetaRef es una etiqueta puesta en una conversación.
type EtiquetaRef struct {
	ID     int64  `json:"id"`
	Nombre string `json:"nombre"`
	Color  string `json:"color" enum:"neutro,marca,info,exito,alerta,peligro"`
}

// ContactoConversacion dice con quién es la conversación (solo ids: los datos de un cliente salen
// de la instantánea de Argos).
type ContactoConversacion struct {
	Tipo           string  `json:"tipo" enum:"cliente,contacto_iris,prospecto,desconocido"`
	ClienteID      *int64  `json:"clienteId" nullable:"true" doc:"Cliente en Argos"`
	ServicioID     *int64  `json:"servicioId" nullable:"true" doc:"Servicio en Argos"`
	ContactoIrisID *int64  `json:"contactoIrisId" nullable:"true"`
	ProspectoID    *int64  `json:"prospectoId" nullable:"true"`
	Asociacion     *string `json:"asociacion" nullable:"true" enum:"automatica,manual" doc:"automatica: una sola coincidencia del número en la instantánea de Argos"`
	Verificado     bool    `json:"verificado" doc:"El número coincide con un teléfono del cliente en Argos (o lo confirmó un supervisor). Sin verificar: ni plantillas fuera de ventana, ni boletas, ni promesas"`
}

// VentanaConversacion es la ventana de 24 h de WhatsApp.
type VentanaConversacion struct {
	Abierta  bool       `json:"abierta" doc:"Se puede mandar texto y adjuntos; si no, solo plantillas aprobadas"`
	CierraEn *time.Time `json:"cierraEn" nullable:"true" doc:"24 h después del último mensaje del cliente"`
}

// SLAConversacion es el tiempo de respuesta de la cola.
type SLAConversacion struct {
	Estado      string     `json:"estado" enum:"sin,en_tiempo,por_vencer,vencido" doc:"sin: nada pendiente o la cola no tiene SLA"`
	VenceEn     *time.Time `json:"venceEn" nullable:"true"`
	EsperaDesde *time.Time `json:"esperaDesde" nullable:"true" doc:"Primer mensaje del cliente sin contestar"`
}

// UltimoMensajeResumen es la vista previa en la lista.
type UltimoMensajeResumen struct {
	Direccion string  `json:"direccion" enum:"entrante,saliente,nota,sistema"`
	Tipo      string  `json:"tipo"`
	Texto     *string `json:"texto" nullable:"true" doc:"Primeros 140 caracteres (texto plano)"`
	Evento    *string `json:"evento" nullable:"true" doc:"Mensaje de sistema: su tipo"`
	// Automatico: el mismo valor que en MensajeConversacion (la lista muestra «Bot: …»).
	Automatico *string   `json:"automatico" nullable:"true" enum:"bot,fuera_de_horario" doc:"Saliente sin autor: el bot o la respuesta de fuera de horario"`
	CreadoEn   time.Time `json:"creadoEn"`
}

// ConversacionCliente es una conversación con un cliente tal como la ve un usuario.
type ConversacionCliente struct {
	ID              int64                 `json:"id"`
	EmpresaID       *int64                `json:"empresaId" nullable:"true"`
	Version         int32                 `json:"version"`
	Canal           CanalRef              `json:"canal"`
	Cola            *ColaRef              `json:"cola,omitempty" doc:"Ausente: sin cola"`
	Numero          string                `json:"numero" doc:"Teléfono del contacto en E.164"`
	PerfilNombre    *string               `json:"perfilNombre" nullable:"true" doc:"Nombre del perfil de WhatsApp"`
	Titulo          string                `json:"titulo" doc:"Cliente de Argos > contacto de Iris > perfil de WhatsApp > número"`
	Contacto        ContactoConversacion  `json:"contacto"`
	Estado          string                `json:"estado" enum:"abierta,esperando_cliente,resuelta"`
	ReabiertaEn     *time.Time            `json:"reabiertaEn" nullable:"true"`
	Asignado        *UsuarioRef           `json:"asignado,omitempty" doc:"Ausente: sin asignar"`
	Etiquetas       []EtiquetaRef         `json:"etiquetas"`
	Ventana         VentanaConversacion   `json:"ventana"`
	SLA             SLAConversacion       `json:"sla"`
	UltimoMensaje   *UltimoMensajeResumen `json:"ultimoMensaje,omitempty"`
	Espera          *EsperaConversacion   `json:"espera,omitempty" doc:"Ausente si está asignada o resuelta: quién está disponible en la cola y cuándo contesta el bot"`
	NoLeidos        int64                 `json:"noLeidos" doc:"Mensajes del cliente después de tu lectura"`
	SoloLectura     bool                  `json:"soloLectura" doc:"El número ya no está compartido con tu empresa (préstamo revocado, §15.3): solo se puede leer"`
	UltimaActividad time.Time             `json:"ultimaActividad"`
	CreadaEn        time.Time             `json:"creadaEn"`
}

// EsperaConversacion es lo que el panel muestra de una conversación sin asignar (fase 1.1).
type EsperaConversacion struct {
	Disponibles      []UsuarioRef `json:"disponibles" doc:"Disponibles en la cola (hasta 5); sin cola, en la empresa"`
	TotalDisponibles int64        `json:"totalDisponibles"`
	BotContestaEn    *time.Time   `json:"botContestaEn" nullable:"true" doc:"Cuándo contesta el bot si nadie la toma (cuenta regresiva)"`
	BotMotivo        *string      `json:"botMotivo" nullable:"true" enum:"sin_confirmacion,sin_disponibles"`
	BotAtendio       bool         `json:"botAtendio" doc:"El bot ya contestó en esta espera"`
}

// MiembroEquipo es un miembro o supervisor de la cola de la conversación.
type MiembroEquipo struct {
	ID         int64  `json:"id"`
	Nombre     string `json:"nombre"`
	Rol        string `json:"rol" enum:"miembro,supervisor"`
	Disponible bool   `json:"disponible"`
}

// VinculoConversacion es algo creado desde la conversación.
type VinculoConversacion struct {
	Tipo       string    `json:"tipo" enum:"ticket,orden,prospecto,boleta,promesa"`
	ID         string    `json:"id"`
	Referencia string    `json:"referencia" doc:"TK-004812, OT-000102, PR-000001…"`
	CreadoEn   time.Time `json:"creadoEn"`
}

// ParticipanteConversacion la ve sin ser de su cola.
type ParticipanteConversacion struct {
	ID     int64  `json:"id"`
	Nombre string `json:"nombre"`
	Origen string `json:"origen" enum:"mencion,guardia"`
}

// ServicioResumenConversacion es un servicio de la instantánea de Argos.
type ServicioResumenConversacion struct {
	ID     int64  `json:"id"`
	Codigo string `json:"codigo"`
	Plan   string `json:"plan"`
	Estado string `json:"estado"`
	Zona   string `json:"zona"`
}

// ClienteResumenConversacion sale de la instantánea de lectura de Argos (nunca se edita en Iris).
type ClienteResumenConversacion struct {
	ID             int64                         `json:"id"`
	Nombre         string                        `json:"nombre"`
	Codigo         string                        `json:"codigo"`
	Servicios      []ServicioResumenConversacion `json:"servicios"`
	SincronizadoEn time.Time                     `json:"sincronizadoEn"`
}

// ConversacionClienteDetalle agrega vínculos, participantes y el resumen del cliente.
type ConversacionClienteDetalle struct {
	ConversacionCliente
	Vinculos       []VinculoConversacion       `json:"vinculos"`
	Participantes  []ParticipanteConversacion  `json:"participantes"`
	ClienteResumen *ClienteResumenConversacion `json:"clienteResumen,omitempty" doc:"Ausente sin cliente o sin instantánea"`
	Supervisores   []UsuarioRef                `json:"supervisores" doc:"Supervisores de su cola"`
	Equipo         []MiembroEquipo             `json:"equipo" doc:"Miembros y supervisores de su cola con su disponibilidad (sin cola, [])"`
}

// AdjuntoMensaje es el archivo de un mensaje (módulo de adjuntos).
type AdjuntoMensaje struct {
	ID          uuid.UUID `json:"id"`
	Estado      string    `json:"estado" enum:"pendiente,subiendo,completo,rechazado"`
	Mime        string    `json:"mime"`
	Tamano      int64     `json:"tamano"`
	URLDescarga string    `json:"urlDescarga"`
}

// PlantillaMensaje es la plantilla aprobada de un mensaje saliente.
type PlantillaMensaje struct {
	Codigo    string            `json:"codigo"`
	Variables map[string]string `json:"variables"`
}

// EventoSistema es un mensaje de sistema («Andrea creó TK-004812»): el panel arma el texto.
type EventoSistema struct {
	Tipo           string  `json:"tipo" doc:"conversacion_creada, asignada, tomada, transferida, resuelta, reabierta, etiquetada, cliente_asociado, contacto_iris_creado, ticket_creado, orden_creada, prospecto_creado, boleta_registrada, boleta_fallida, promesa_creada, respondio_aviso, fuera_de_horario, guardia_avisada, manual_enviado; 1.1: aviso_sin_atender, bot_atendio, menu_elegido, pasada, iniciada"`
	ActorNombre    *string `json:"actorNombre,omitempty"`
	AsignadoNombre *string `json:"asignadoNombre,omitempty"`
	ColaNombre     *string `json:"colaNombre,omitempty"`
	Referencia     *string `json:"referencia,omitempty" doc:"TK-…, OT-…, PR-…"`
	EntidadID      *string `json:"entidadId,omitempty"`
	Plantilla      *string `json:"plantilla,omitempty" doc:"respondio_aviso: la plantilla del aviso"`
	Detalle        *string `json:"detalle,omitempty"`
	Minutos        *int32  `json:"minutos,omitempty" doc:"bot_atendio: minutos que esperó"`
	Disponibles    *int64  `json:"disponibles,omitempty" doc:"aviso_sin_atender: a cuántos se avisó"`
}

// MensajeConversacion es un mensaje de la conversación.
type MensajeConversacion struct {
	ID             uuid.UUID         `json:"id"`
	ConversacionID int64             `json:"conversacionId"`
	Seq            int64             `json:"seq"`
	Direccion      string            `json:"direccion" enum:"entrante,saliente,nota,sistema"`
	Tipo           string            `json:"tipo" enum:"texto,imagen,documento,audio,video,ubicacion,plantilla,contacto,sistema"`
	Texto          *string           `json:"texto" nullable:"true" doc:"Texto plano: nunca se interpreta como HTML"`
	Adjunto        *AdjuntoMensaje   `json:"adjunto,omitempty"`
	Ubicacion      *UbicacionChat    `json:"ubicacion,omitempty"`
	Plantilla      *PlantillaMensaje `json:"plantilla,omitempty"`
	Autor          *UsuarioRef       `json:"autor,omitempty" doc:"Saliente y nota; ausente en entrante, sistema y respuestas automáticas"`
	EstadoEntrega  *string           `json:"estadoEntrega" nullable:"true" enum:"en_cola,enviado,entregado,leido,fallido"`
	MotivoFalla    *string           `json:"motivoFalla" nullable:"true"`
	Menciones      []UsuarioRef      `json:"menciones"`
	Evento         *EventoSistema    `json:"evento,omitempty"`
	CreadoEn       time.Time         `json:"creadoEn"`
	EnviadoEn      *time.Time        `json:"enviadoEn" nullable:"true"`
	EntregadoEn    *time.Time        `json:"entregadoEn" nullable:"true"`
	LeidoEn        *time.Time        `json:"leidoEn" nullable:"true"`
	Automatico     *string           `json:"automatico" nullable:"true" enum:"bot,fuera_de_horario" doc:"Saliente sin autor: el bot o la respuesta de fuera de horario"`
	Origen         *string           `json:"origen" nullable:"true" enum:"telefono,historial" doc:"No salió de Iris: telefono (mandado desde la app WhatsApp Business, «Desde el teléfono») o historial (traído de la app). null = Iris (§15.2)"`
}

// Alcance -------------------------------------------------------------------------------------

// alcanceConv: quién mira, en qué empresa (0 = sin conector) y si supervisa.
type alcanceConv struct {
	usuario   int64
	empresa   int64
	supervisa bool
}

// alcanceConvDe exige una persona con sesión (no un token de API ni una integración) y
// conversaciones.ver: cualquier lectura o acción sobre conversaciones (también las que piden otro
// permiso, como tickets.crear) pasa por aquí. Si no, 404 (no se revela que existe).
func alcanceConvDe(ctx context.Context) (alcanceConv, error) {
	actor := contexto.ActorDe(ctx)
	if actor.Tipo != contexto.ActorUsuario || actor.ID == 0 || !contexto.PermisosDe(ctx).Tiene(string(nucleo.PermConversacionesVer)) {
		return alcanceConv{}, conversacionClienteNoEncontrada()
	}
	a := alcanceConv{usuario: actor.ID}
	if contexto.EmpresasDe(ctx).Estado == contexto.EmpresaActiva {
		a.empresa = contexto.EmpresaDe(ctx)
	}
	a.supervisa = contexto.PermisosDe(ctx).Tiene(string(nucleo.PermConversacionesSupervisar))
	return a, nil
}

// empresaPtr: la empresa para guardar (nil sin conector).
func (a alcanceConv) empresaPtr() *int64 {
	if a.empresa == 0 {
		return nil
	}
	e := a.empresa
	return &e
}

func conversacionClienteNoEncontrada() *nucleo.Problema {
	return nucleo.NoEncontrado("La conversación no existe o no la puedes ver")
}

// convVisible lee la conversación (con FOR UPDATE si bloquear) o 404 si el actor no la ve.
func convVisible(ctx context.Context, q *mensajeriabd.Queries, a alcanceConv, id int64, bloquear bool) (mensajeriabd.MensajeriaConversacion, error) {
	ok, err := q.ConversacionVisible(ctx, mensajeriabd.ConversacionVisibleParams{ID: id, Empresa: a.empresa, Usuario: a.usuario, Supervisa: a.supervisa})
	if err != nil {
		return mensajeriabd.MensajeriaConversacion{}, err
	}
	if !ok {
		return mensajeriabd.MensajeriaConversacion{}, conversacionClienteNoEncontrada()
	}
	if bloquear {
		// Una acción: la conversación de un préstamo revocado es de solo lectura (§15.3).
		c, err := q.ConversacionClienteParaActualizar(ctx, id)
		if err != nil {
			return c, err
		}
		if lectura, err := soloLecturaConv(ctx, q, c); err != nil || lectura {
			if err == nil {
				err = conversacionSoloLectura()
			}
			return mensajeriabd.MensajeriaConversacion{}, err
		}
		return c, nil
	}
	return q.ConversacionClientePorID(ctx, id)
}

// exigirVersion: 409 si la versión leída ya cambió.
func exigirVersionConv(c mensajeriabd.MensajeriaConversacion, version int32) error {
	if c.Version != version {
		return nucleo.ConflictoVersion("conversacion", c.ID)
	}
	return nil
}

func empresaDeConv(c mensajeriabd.MensajeriaConversacion) int64 {
	if c.EmpresaID == nil {
		return 0
	}
	return *c.EmpresaID
}

// Estado de la conversación ---------------------------------------------------------------------

// guardarConv escribe el estado (la fila se leyó con FOR UPDATE). subir: cambió algo que el
// usuario edita (asignación, cola, estado, etiquetas, contacto) y sube la versión.
func guardarConv(ctx context.Context, q *mensajeriabd.Queries, c mensajeriabd.MensajeriaConversacion, subir bool) (mensajeriabd.MensajeriaConversacion, error) {
	sube := int32(0)
	if subir {
		sube = 1
	}
	return q.GuardarConversacion(ctx, mensajeriabd.GuardarConversacionParams{ID: c.ID, PerfilNombre: c.PerfilNombre, ColaID: c.ColaID,
		AsignadoA: c.AsignadoA, AsignadoEn: c.AsignadoEn, Estado: c.Estado, ClienteID: c.ClienteID, ServicioID: c.ServicioID,
		ContactoIrisID: c.ContactoIrisID, ProspectoID: c.ProspectoID, Asociacion: c.Asociacion, ClienteVerificado: c.ClienteVerificado,
		VerificadoPor: c.VerificadoPor, UltimoEntranteEn: c.UltimoEntranteEn,
		EsperaDesde: c.EsperaDesde, SlaVenceEn: c.SlaVenceEn, SlaAvisado: c.SlaAvisado, ResueltaEn: c.ResueltaEn, ReabiertaEn: c.ReabiertaEn,
		UltimaActividad: c.UltimaActividad, SubeVersion: sube})
}

// slaDe calcula el vencimiento del SLA desde la espera con el SLA de la cola (nil = sin SLA).
func (m *Modulo) slaDe(ctx context.Context, q *mensajeriabd.Queries, c mensajeriabd.MensajeriaConversacion) (*time.Time, error) {
	if c.EsperaDesde == nil || c.ColaID == nil {
		return nil, nil
	}
	cola, err := q.ColaPorID(ctx, mensajeriabd.ColaPorIDParams{ID: *c.ColaID, Empresa: empresaDeConv(c)})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil || cola.SlaRespuestaMin == nil {
		return nil, err
	}
	return m.venceSLA(ctx, q, c, cola, *c.EsperaDesde)
}

// venceSLA: desde + SLA de la cola; si el mensaje llegó con el horario (de la cola o del número)
// cerrado, el SLA empieza a contar al abrir.
func (m *Modulo) venceSLA(ctx context.Context, q *mensajeriabd.Queries, c mensajeriabd.MensajeriaConversacion, cola mensajeriabd.MensajeriaCola, desde time.Time) (*time.Time, error) {
	if cola.SlaRespuestaMin == nil {
		return nil, nil
	}
	cal, err := calendarioEfectivo(ctx, q, cola.HorarioID, c.CanalID)
	if err != nil {
		return nil, err
	}
	inicio, ok := slaConHorario(cal, desde)
	if !ok {
		return nil, nil
	}
	v := inicio.Add(time.Duration(*cola.SlaRespuestaMin) * time.Minute)
	return &v, nil
}

func estadoSLA(vence, espera *time.Time, slaMin, porVencerMin *int32, ahora time.Time) SLAConversacion {
	s := SLAConversacion{Estado: "sin", VenceEn: vence, EsperaDesde: espera}
	if vence == nil {
		return s
	}
	margen := time.Minute
	switch {
	case porVencerMin != nil:
		margen = time.Duration(*porVencerMin) * time.Minute
	case slaMin != nil:
		margen = max(time.Minute, time.Duration(*slaMin)*time.Minute/4)
	}
	switch {
	case !ahora.Before(*vence):
		s.Estado = "vencido"
	case !ahora.Before(vence.Add(-margen)):
		s.Estado = "por_vencer"
	default:
		s.Estado = "en_tiempo"
	}
	return s
}

func ventanaDe(ultimoEntrante *time.Time, ahora time.Time) VentanaConversacion {
	if ultimoEntrante == nil {
		return VentanaConversacion{}
	}
	cierra := ultimoEntrante.Add(VentanaWhatsApp)
	return VentanaConversacion{Abierta: ahora.Before(cierra), CierraEn: &cierra}
}

func contactoDeConv(c mensajeriabd.MensajeriaConversacion) ContactoConversacion {
	k := ContactoConversacion{Tipo: "desconocido", ClienteID: c.ClienteID, ServicioID: c.ServicioID, ContactoIrisID: c.ContactoIrisID,
		ProspectoID: c.ProspectoID, Asociacion: c.Asociacion, Verificado: c.ClienteID != nil && c.ClienteVerificado}
	switch {
	case c.ClienteID != nil:
		k.Tipo = "cliente"
	case c.ContactoIrisID != nil:
		k.Tipo = "contacto_iris"
	case c.ProspectoID != nil:
		k.Tipo = "prospecto"
	}
	return k
}

// resumirConvs arma las conversaciones (en el orden de ids) tal como las ve el usuario.
func (m *Modulo) resumirConvs(ctx context.Context, q *mensajeriabd.Queries, usuario int64, ids []int64) ([]ConversacionCliente, error) {
	if len(ids) == 0 {
		return []ConversacionCliente{}, nil
	}
	filas, err := q.ResumenConversacionesCliente(ctx, mensajeriabd.ResumenConversacionesClienteParams{Usuario: usuario, Ids: ids})
	if err != nil {
		return nil, err
	}
	etiquetas := map[int64][]EtiquetaRef{}
	es, err := q.EtiquetasDeConversaciones(ctx, ids)
	if err != nil {
		return nil, err
	}
	for _, e := range es {
		etiquetas[e.ConversacionID] = append(etiquetas[e.ConversacionID], EtiquetaRef{ID: e.ID, Nombre: e.Nombre, Color: e.Color})
	}
	ahora := m.ahora()
	disp := map[[2]int64][]UsuarioRef{}
	porID := make(map[int64]ConversacionCliente, len(filas))
	for _, f := range filas {
		c := mensajeriabd.MensajeriaConversacion{ID: f.ID, ClienteID: f.ClienteID, ServicioID: f.ServicioID, ContactoIrisID: f.ContactoIrisID,
			ProspectoID: f.ProspectoID, Asociacion: f.Asociacion, ClienteVerificado: f.ClienteVerificado}
		v := ConversacionCliente{ID: f.ID, EmpresaID: f.EmpresaID, Version: f.Version,
			Canal:  CanalRef{ID: f.CanalID, Nombre: f.CanalNombre, Numero: f.CanalNumero, Usos: f.CanalUsos},
			Numero: f.Numero, PerfilNombre: f.PerfilNombre, Contacto: contactoDeConv(c), Estado: f.Estado, ReabiertaEn: f.ReabiertaEn,
			Etiquetas: etiquetas[f.ID], Ventana: ventanaDe(f.UltimoEntranteEn, ahora),
			SLA:      estadoSLA(f.SlaVenceEn, f.EsperaDesde, f.SlaRespuestaMin, f.SlaPorVencerMin, ahora),
			NoLeidos: f.NoLeidos, SoloLectura: f.SoloLectura, UltimaActividad: f.UltimaActividad, CreadaEn: f.CreatedAt}
		if v.Etiquetas == nil {
			v.Etiquetas = []EtiquetaRef{}
		}
		if f.ColaID != nil && f.ColaNombre != nil {
			v.Cola = &ColaRef{ID: *f.ColaID, Nombre: *f.ColaNombre}
		}
		if f.AsignadoA != nil && f.AsignadoNombre != nil {
			v.Asignado = &UsuarioRef{ID: *f.AsignadoA, Nombre: *f.AsignadoNombre}
		}
		switch {
		case f.ClienteNombre != nil && *f.ClienteNombre != "":
			v.Titulo = *f.ClienteNombre
		case f.ContactoIrisNombre != nil:
			v.Titulo = *f.ContactoIrisNombre
		case f.PerfilNombre != nil && *f.PerfilNombre != "":
			v.Titulo = *f.PerfilNombre
		default:
			v.Titulo = f.Numero
		}
		if f.UltimoDireccion != nil && f.UltimoCreadoEn != nil {
			u := &UltimoMensajeResumen{Direccion: *f.UltimoDireccion, CreadoEn: *f.UltimoCreadoEn, Automatico: f.UltimoAutomatico}
			if f.UltimoTipo != nil {
				u.Tipo = *f.UltimoTipo
			}
			if f.UltimoTexto != nil {
				t := recortar(*f.UltimoTexto, largoVistaPrevia)
				u.Texto = &t
			}
			if len(f.UltimoEvento) > 0 {
				var e EventoSistema
				if json.Unmarshal(f.UltimoEvento, &e) == nil {
					u.Evento = &e.Tipo
				}
			}
			v.UltimoMensaje = u
		}
		if f.AsignadoA == nil && f.Estado != EstadoConvResuelta {
			e, err := esperaDe(ctx, q, disp, f.EmpresaID, f.ColaID)
			if err != nil {
				return nil, err
			}
			e.BotContestaEn, e.BotMotivo, e.BotAtendio = f.BotVenceEn, f.BotMotivo, f.BotAtendioEn != nil
			v.Espera = &e
		}
		porID[f.ID] = v
	}
	salida := make([]ConversacionCliente, 0, len(ids))
	for _, id := range ids {
		if v, ok := porID[id]; ok {
			salida = append(salida, v)
		}
	}
	return salida, nil
}

func (m *Modulo) resumirConv(ctx context.Context, q *mensajeriabd.Queries, usuario, id int64) (ConversacionCliente, error) {
	cs, err := m.resumirConvs(ctx, q, usuario, []int64{id})
	if err != nil {
		return ConversacionCliente{}, err
	}
	if len(cs) == 0 {
		return ConversacionCliente{}, conversacionClienteNoEncontrada()
	}
	return cs[0], nil
}

// esperaDe arma los disponibles de la cola de una conversación que espera (con caché por cola).
func esperaDe(ctx context.Context, q *mensajeriabd.Queries, cache map[[2]int64][]UsuarioRef, empresa, cola *int64) (EsperaConversacion, error) {
	e := int64(0)
	if empresa != nil {
		e = *empresa
	}
	clave := [2]int64{e, -1}
	if cola != nil {
		clave[1] = *cola
	}
	us, ok := cache[clave]
	if !ok {
		var err error
		if us, err = disponiblesDe(ctx, q, e, cola); err != nil {
			return EsperaConversacion{}, err
		}
		cache[clave] = us
	}
	out := EsperaConversacion{Disponibles: []UsuarioRef{}, TotalDisponibles: int64(len(us))}
	out.Disponibles = append(out.Disponibles, us[:min(len(us), maxDisponiblesEspera)]...)
	return out, nil
}

// detalleConv arma el detalle de una conversación.
func (m *Modulo) detalleConv(ctx context.Context, q *mensajeriabd.Queries, usuario int64, c mensajeriabd.MensajeriaConversacion) (ConversacionClienteDetalle, error) {
	r, err := m.resumirConv(ctx, q, usuario, c.ID)
	if err != nil {
		return ConversacionClienteDetalle{}, err
	}
	d := ConversacionClienteDetalle{ConversacionCliente: r, Vinculos: []VinculoConversacion{}, Participantes: []ParticipanteConversacion{}}
	if d.Supervisores, d.Equipo, err = equipoDeCola(ctx, q, empresaDeConv(c), c.ColaID); err != nil {
		return d, err
	}
	vs, err := q.VinculosDeConversacion(ctx, c.ID)
	if err != nil {
		return d, err
	}
	for _, v := range vs {
		d.Vinculos = append(d.Vinculos, VinculoConversacion{Tipo: v.Tipo, ID: v.EntidadID, Referencia: v.Referencia, CreadoEn: v.CreatedAt})
	}
	ps, err := q.ParticipantesDeConversacion(ctx, c.ID)
	if err != nil {
		return d, err
	}
	for _, p := range ps {
		d.Participantes = append(d.Participantes, ParticipanteConversacion{ID: p.UsuarioID, Nombre: p.Nombre, Origen: p.Origen})
	}
	if c.ClienteID != nil {
		cl, err := q.ClienteInstantanea(ctx, *c.ClienteID)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
		case err != nil:
			return d, err
		default:
			res := &ClienteResumenConversacion{ID: cl.ID, Nombre: cl.Nombre, Codigo: cl.Codigo, Servicios: []ServicioResumenConversacion{}, SincronizadoEn: cl.SincronizadoEn}
			ss, err := q.ServiciosInstantanea(ctx, cl.ID)
			if err != nil {
				return d, err
			}
			for _, s := range ss {
				if c.EmpresaID != nil && s.EmpresaID != *c.EmpresaID {
					continue
				}
				res.Servicios = append(res.Servicios, ServicioResumenConversacion{ID: s.ID, Codigo: s.Codigo, Plan: s.PlanNombre, Estado: s.Estado, Zona: s.ZonaNombre})
			}
			d.ClienteResumen = res
		}
	}
	return d, nil
}

// Mensajes ----------------------------------------------------------------------------------------

// mensajesConv convierte filas de mensajes (con nombres de autores y menciones y estado de adjuntos).
func mensajesConv(ctx context.Context, q *mensajeriabd.Queries, filas []mensajeriabd.MensajeriaConversacionMensaje) ([]MensajeConversacion, error) {
	if len(filas) == 0 {
		return []MensajeConversacion{}, nil
	}
	var personas []int64
	var adjs []uuid.UUID
	for _, f := range filas {
		if f.AutorID != nil {
			personas = append(personas, *f.AutorID)
		}
		personas = append(personas, f.Menciones...)
		if id := uuidDe(f.AdjuntoID); id != nil {
			adjs = append(adjs, *id)
		}
	}
	nombres := map[int64]string{}
	if len(personas) > 0 {
		ns, err := q.NombresUsuarios(ctx, personas)
		if err != nil {
			return nil, err
		}
		for _, n := range ns {
			nombres[n.ID] = n.Nombre
		}
	}
	estados := map[uuid.UUID]mensajeriabd.EstadosAdjuntosRow{}
	if len(adjs) > 0 {
		es, err := q.EstadosAdjuntos(ctx, adjs)
		if err != nil {
			return nil, err
		}
		for _, e := range es {
			estados[e.ID] = e
		}
	}
	out := make([]MensajeConversacion, 0, len(filas))
	for _, f := range filas {
		out = append(out, aMensajeConv(f, nombres, estados))
	}
	return out, nil
}

func aMensajeConv(f mensajeriabd.MensajeriaConversacionMensaje, nombres map[int64]string, estados map[uuid.UUID]mensajeriabd.EstadosAdjuntosRow) MensajeConversacion {
	m := MensajeConversacion{ID: f.ID, ConversacionID: f.ConversacionID, Seq: f.Seq, Direccion: f.Direccion, Tipo: f.Tipo, Texto: f.Texto,
		EstadoEntrega: f.EstadoEntrega, MotivoFalla: f.MotivoFalla, Menciones: []UsuarioRef{}, CreadoEn: f.CreadoEn, EnviadoEn: f.EnviadoEn,
		EntregadoEn: f.EntregadoEn, LeidoEn: f.LeidoEn, Automatico: f.Automatico, Origen: f.Origen}
	if f.AutorID != nil {
		m.Autor = &UsuarioRef{ID: *f.AutorID, Nombre: nombres[*f.AutorID]}
	}
	for _, u := range f.Menciones {
		m.Menciones = append(m.Menciones, UsuarioRef{ID: u, Nombre: nombres[u]})
	}
	if id := uuidDe(f.AdjuntoID); id != nil {
		a := &AdjuntoMensaje{ID: *id, Estado: adjuntos.EstadoPendiente, URLDescarga: adjuntos.RutaDescarga(*id)}
		if e, ok := estados[*id]; ok {
			a.Estado, a.Mime, a.Tamano = e.Estado, e.Mime, e.Tamano
		}
		m.Adjunto = a
	} else if f.MedioID != nil {
		// Medio entrante que aún se descarga de Meta.
		m.Adjunto = &AdjuntoMensaje{Estado: adjuntos.EstadoPendiente}
	}
	if f.Lat != nil && f.Lon != nil {
		m.Ubicacion = &UbicacionChat{Lat: *f.Lat, Lon: *f.Lon}
	}
	if f.Plantilla != nil {
		p := &PlantillaMensaje{Codigo: *f.Plantilla, Variables: map[string]string{}}
		_ = json.Unmarshal(f.Variables, &p.Variables)
		m.Plantilla = p
	}
	if len(f.Evento) > 0 {
		var e EventoSistema
		if json.Unmarshal(f.Evento, &e) == nil {
			m.Evento = &e
		}
	}
	return m
}

// nuevoMensaje son los datos de un mensaje a agregar.
type nuevoMensaje struct {
	id            uuid.UUID
	direccion     string
	tipo          string
	texto         *string
	adjunto       *uuid.UUID
	medio         *string
	lat, lon      *float64
	plantilla     *string
	variables     []byte
	autor         *int64
	menciones     []int64
	evento        *EventoSistema
	respuesta     *int64
	proveedor     *string
	hash          []byte
	clave         *string
	creadoEn      time.Time
	estadoEntrega *string
	automatico    *string
	// origen: telefono (eco) o historial (§15.2).
	origen *string
	// sinFeed: no se avisa al flujo en tiempo real (historial: miles de mensajes viejos).
	sinFeed bool
}

// agregarMensaje asigna el seq (la conversación debe estar bloqueada), lo guarda y lo pone en el
// feed. Actualiza c con la última actividad.
func (m *Modulo) agregarMensaje(ctx context.Context, t *auditoria.Tx, c *mensajeriabd.MensajeriaConversacion, n nuevoMensaje) (mensajeriabd.MensajeriaConversacionMensaje, error) {
	q := mensajeriabd.New(t)
	if n.id == uuid.Nil {
		id, err := uuid.NewV7()
		if err != nil {
			return mensajeriabd.MensajeriaConversacionMensaje{}, err
		}
		n.id = id
	}
	ahora := m.ahora()
	if n.creadoEn.IsZero() {
		n.creadoEn = ahora
	}
	seq, err := q.AvanzarSeqConversacion(ctx, mensajeriabd.AvanzarSeqConversacionParams{ID: c.ID, MensajeID: pgUUID(&n.id), Ahora: ahora})
	if err != nil {
		return mensajeriabd.MensajeriaConversacionMensaje{}, err
	}
	c.UltimoSeq = seq
	if ahora.After(c.UltimaActividad) {
		c.UltimaActividad = ahora
	}
	var evento []byte
	if n.evento != nil {
		if evento, err = json.Marshal(n.evento); err != nil {
			return mensajeriabd.MensajeriaConversacionMensaje{}, err
		}
	}
	if n.menciones == nil {
		n.menciones = []int64{}
	}
	f, err := q.InsertarMensajeConversacion(ctx, mensajeriabd.InsertarMensajeConversacionParams{ID: n.id, ConversacionID: c.ID, EmpresaID: c.EmpresaID,
		Seq: seq, Direccion: n.direccion, Tipo: n.tipo, Texto: n.texto, AdjuntoID: pgUUID(n.adjunto), MedioID: n.medio, Lat: n.lat, Lon: n.lon,
		Plantilla: n.plantilla, Variables: n.variables, AutorID: n.autor, Menciones: n.menciones, Evento: evento, RespuestaRapidaID: n.respuesta,
		EstadoEntrega: n.estadoEntrega, ProveedorID: n.proveedor, HashContenido: n.hash, IdempotencyKey: n.clave, CreadoEn: n.creadoEn, Automatico: n.automatico,
		Origen: n.origen})
	if err != nil || n.sinFeed {
		return f, err
	}
	return f, m.emitirEventoConv(ctx, t, *c, EventoConvMensajeNuevo, &f.ID, nil)
}

// sistema agrega un mensaje de sistema.
func (m *Modulo) sistema(ctx context.Context, t *auditoria.Tx, c *mensajeriabd.MensajeriaConversacion, e EventoSistema) error {
	_, err := m.agregarMensaje(ctx, t, c, nuevoMensaje{direccion: DirSistema, tipo: "sistema", evento: &e})
	return err
}

// Feed ----------------------------------------------------------------------------------------------

// emitirEventoConv agrega el cambio al feed de la bandeja con el candado de la empresa hasta el
// COMMIT (los ids quedan en orden de COMMIT) y avisa con NOTIFY.
func (m *Modulo) emitirEventoConv(ctx context.Context, t *auditoria.Tx, c mensajeriabd.MensajeriaConversacion, tipo string, mensaje *uuid.UUID, usuario *int64) error {
	q := mensajeriabd.New(t)
	clave := strconv.FormatInt(empresaDeConv(c), 10)
	if err := q.CandadoChat(ctx, "conv:eventos:"+clave); err != nil {
		return err
	}
	if _, err := q.InsertarEventoConv(ctx, mensajeriabd.InsertarEventoConvParams{EmpresaID: c.EmpresaID, ConversacionID: c.ID, Tipo: tipo,
		MensajeID: pgUUID(mensaje), UsuarioID: usuario, OcurridoEn: m.ahora()}); err != nil {
		return err
	}
	return q.NotificarConv(ctx, clave)
}

// Errores comunes ------------------------------------------------------------------------------------

func usuarioNoDisponibleConv() *nucleo.Problema {
	return nucleo.ReglaNegocio("usuario_no_disponible", "Ese usuario no existe, no está activo, no opera en esta empresa o no atiende conversaciones")
}

func campoConv(campo, mensaje, regla string) *nucleo.Problema {
	return nucleo.Validacion(nucleo.ErrorCampo{Field: campo, Message: mensaje, Rule: regla})
}

func reglaConv(codigo, detalle string) *nucleo.Problema {
	return nucleo.NuevoProblema(http.StatusUnprocessableEntity, codigo, "No se puede", detalle)
}

func nombrePtr(s string) *string { return &s }

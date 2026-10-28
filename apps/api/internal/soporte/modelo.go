package soporte

import (
	"encoding/json"
	"slices"
	"time"

	"github.com/google/uuid"

	soportebd "github.com/HCDestroyer/iris/apps/api/internal/soporte/bd"
)

// Estados del ticket.
const (
	EstadoNuevo    = "nuevo"
	EstadoAbierto  = "abierto"
	EstadoEnEspera = "en_espera"
	EstadoResuelto = "resuelto"
	EstadoCerrado  = "cerrado"
)

// Prioridades, de menor a mayor.
var Prioridades = []string{"baja", "media", "alta", "critica"}

// Origenes de un ticket.
var Origenes = []string{"panel", "portal", "correo", "telegram", "horus", "argos", "api"}

func rangoPrioridad(p string) int { return slices.Index(Prioridades, p) }

// Contacto capturado a mano (sin conector con Argos o cliente no identificado).
type Contacto struct {
	Nombre   *string `json:"nombre" nullable:"true"`
	Telefono *string `json:"telefono" nullable:"true"`
	Correo   *string `json:"correo" nullable:"true"`
	// Verificado: false si el ticket nació de un correo cuyo remitente no se pudo autenticar
	// (el From pudo ser falsificado). Capturado a mano: true.
	Verificado bool `json:"verificado" doc:"false = el ticket nació de un correo cuyo remitente no se pudo autenticar (DMARC/DKIM); mostrarlo como no verificado"`
}

// EntradaContacto son los datos de contacto al crear un ticket.
type EntradaContacto struct {
	Nombre   *string `json:"nombre,omitempty" maxLength:"120"`
	Telefono *string `json:"telefono,omitempty" maxLength:"40"`
	Correo   *string `json:"correo,omitempty" format:"email" maxLength:"254"`
}

// Ticket es un caso de soporte. Cliente, servicio y zona son ids de Argos.
type Ticket struct {
	ID                int64      `json:"id"`
	Numero            string     `json:"numero" doc:"Número legible (TK-000123)"`
	EmpresaID         *int64     `json:"empresaId" nullable:"true" doc:"Empresa en Argos; null sin conector"`
	ClienteID         *int64     `json:"clienteId" nullable:"true" doc:"Cliente en Argos"`
	ServicioID        *int64     `json:"servicioId" nullable:"true" doc:"Servicio en Argos"`
	ZonaID            *int64     `json:"zonaId" nullable:"true" doc:"Zona (Argos); define el alcance"`
	Contacto          Contacto   `json:"contacto"`
	CategoriaID       *int64     `json:"categoriaId" nullable:"true"`
	Prioridad         string     `json:"prioridad" enum:"baja,media,alta,critica"`
	Estado            string     `json:"estado" enum:"nuevo,abierto,en_espera,resuelto,cerrado"`
	Origen            string     `json:"origen" enum:"panel,portal,correo,telegram,horus,argos,api"`
	Asunto            string     `json:"asunto"`
	AsignadoA         *int64     `json:"asignadoA" nullable:"true" doc:"Usuario asignado"`
	CuadrillaID       *int64     `json:"cuadrillaId" nullable:"true"`
	NivelEscalamiento int32      `json:"nivelEscalamiento" doc:"Veces que se escaló"`
	SLAID             *int64     `json:"slaId" nullable:"true"`
	SLARespuesta      RelojSLA   `json:"slaRespuesta"`
	SLAResolucion     RelojSLA   `json:"slaResolucion"`
	EnEsperaDesde     *time.Time `json:"enEsperaDesde" nullable:"true"`
	CerradoEn         *time.Time `json:"cerradoEn" nullable:"true"`
	TicketPadreID     *int64     `json:"ticketPadreId" nullable:"true" doc:"Incidente maestro"`
	FusionadoEnID     *int64     `json:"fusionadoEnId" nullable:"true" doc:"Ticket que absorbió a este (fusión)"`
	Version           int32      `json:"version"`
	CreadoEn          time.Time  `json:"creadoEn"`
	ActualizadoEn     time.Time  `json:"actualizadoEn"`
}

func aTicket(f soportebd.SoporteTicket, ahora time.Time) Ticket {
	ref := ahora
	if f.CerradoEn != nil {
		ref = *f.CerradoEn
	}
	return Ticket{
		ID: f.ID, Numero: f.Numero, EmpresaID: f.EmpresaID, ClienteID: f.ClienteID, ServicioID: f.ServicioID,
		ZonaID: f.ZonaID, Contacto: Contacto{Nombre: f.ContactoNombre, Telefono: f.ContactoTelefono, Correo: f.ContactoCorreo, Verificado: f.ContactoVerificado},
		CategoriaID: f.CategoriaID, Prioridad: f.Prioridad, Estado: f.Estado, Origen: f.Origen, Asunto: f.Asunto,
		AsignadoA: f.AsignadoA, CuadrillaID: f.CuadrillaID, NivelEscalamiento: f.NivelEscalamiento, SLAID: f.SlaID,
		SLARespuesta:  reloj(f.SlaRespuestaMin, f.RiesgoRespuesta, f.VenceRespuesta, f.PrimeraRespuestaEn, false, ref),
		SLAResolucion: reloj(f.SlaResolucionMin, f.RiesgoResolucion, f.VenceResolucion, f.ResueltoEn, f.Estado == EstadoEnEspera, ref),
		EnEsperaDesde: f.EnEsperaDesde, CerradoEn: f.CerradoEn, TicketPadreID: f.TicketPadreID,
		FusionadoEnID: f.FusionadoEnID, Version: f.Version, CreadoEn: f.CreatedAt, ActualizadoEn: f.UpdatedAt,
	}
}

// Mensaje de la conversación: público (lo ve el cliente) o nota interna.
type Mensaje struct {
	ID          int64       `json:"id"`
	TicketID    int64       `json:"ticketId"`
	Publico     bool        `json:"publico" doc:"false = nota interna"`
	AutorTipo   string      `json:"autorTipo" enum:"usuario,cliente,sistema"`
	AutorID     *int64      `json:"autorId" nullable:"true"`
	AutorNombre string      `json:"autorNombre"`
	Canal       string      `json:"canal"`
	Cuerpo      string      `json:"cuerpo"`
	Adjuntos    []uuid.UUID `json:"adjuntos" doc:"Ids de adjuntos (la subida llega con POST /adjuntos)"`
	CreadoEn    time.Time   `json:"creadoEn"`
}

func aMensaje(m soportebd.SoporteTicketMensaje) Mensaje {
	adj := m.Adjuntos
	if adj == nil {
		adj = []uuid.UUID{}
	}
	return Mensaje{
		ID: m.ID, TicketID: m.TicketID, Publico: m.Publico, AutorTipo: m.AutorTipo, AutorID: m.AutorID,
		AutorNombre: m.AutorNombre, Canal: m.Canal, Cuerpo: m.Cuerpo, Adjuntos: adj, CreadoEn: m.CreatedAt,
	}
}

// EventoTicket es una entrada del historial.
type EventoTicket struct {
	ID          int64          `json:"id"`
	TicketID    int64          `json:"ticketId"`
	Tipo        string         `json:"tipo" enum:"creado,estado,asignado,escalado,fusionado,sla_en_riesgo,sla_vencido"`
	De          *string        `json:"de" nullable:"true"`
	A           *string        `json:"a" nullable:"true"`
	Motivo      *string        `json:"motivo" nullable:"true"`
	Datos       map[string]any `json:"datos"`
	ActorTipo   string         `json:"actorTipo"`
	ActorID     *int64         `json:"actorId" nullable:"true"`
	ActorNombre string         `json:"actorNombre"`
	CreadoEn    time.Time      `json:"creadoEn"`
}

func aEventoTicket(e soportebd.SoporteTicketEvento) EventoTicket {
	datos := map[string]any{}
	_ = json.Unmarshal(e.Datos, &datos)
	return EventoTicket{
		ID: e.ID, TicketID: e.TicketID, Tipo: e.Tipo, De: e.De, A: e.A, Motivo: e.Motivo, Datos: datos,
		ActorTipo: e.ActorTipo, ActorID: e.ActorID, ActorNombre: e.ActorNombre, CreadoEn: e.CreatedAt,
	}
}

// Categoria de ticket, con su SLA (null = el SLA por defecto).
type Categoria struct {
	ID            int64  `json:"id"`
	Codigo        string `json:"codigo"`
	Nombre        string `json:"nombre"`
	SLAID         *int64 `json:"slaId" nullable:"true" doc:"null = SLA por defecto"`
	RequiereOrden bool   `json:"requiereOrden" doc:"Suele terminar en una orden de trabajo"`
	Activa        bool   `json:"activa"`
	Version       int32  `json:"version"`
}

func aCategoria(c soportebd.SoporteCategoria) Categoria {
	return Categoria{ID: c.ID, Codigo: c.Codigo, Nombre: c.Nombre, SLAID: c.SlaID, RequiereOrden: c.RequiereOrden, Activa: c.Activa, Version: c.Version}
}

// ObjetivoSLA son los minutos de respuesta y resolución de una prioridad.
type ObjetivoSLA struct {
	Prioridad     string `json:"prioridad" enum:"baja,media,alta,critica"`
	RespuestaMin  int32  `json:"respuestaMin"`
	ResolucionMin int32  `json:"resolucionMin"`
}

// SLA con sus objetivos por prioridad.
type SLA struct {
	ID               int64         `json:"id"`
	Codigo           string        `json:"codigo"`
	Nombre           string        `json:"nombre"`
	Continuo         bool          `json:"continuo" doc:"24/7: sin calendario ni feriados"`
	CalendarioID     *int64        `json:"calendarioId" nullable:"true"`
	CalendarioNombre *string       `json:"calendarioNombre" nullable:"true"`
	ZonaHoraria      *string       `json:"zonaHoraria" nullable:"true"`
	PorDefecto       bool          `json:"porDefecto" doc:"Se usa para categorías sin SLA"`
	Activo           bool          `json:"activo"`
	Objetivos        []ObjetivoSLA `json:"objetivos"`
	Version          int32         `json:"version"`
}

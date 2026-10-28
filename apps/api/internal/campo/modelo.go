package campo

import (
	"encoding/json"
	"math"
	"math/big"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	campobd "github.com/HCDestroyer/iris/apps/api/internal/campo/bd"
)

// Estados de la orden.
const (
	EstadoPendiente  = "pendiente"
	EstadoProgramada = "programada"
	EstadoEnCamino   = "en_camino"
	EstadoEnSitio    = "en_sitio"
	EstadoPausada    = "pausada"
	EstadoCerrada    = "cerrada"
	EstadoFallida    = "fallida"
	EstadoCancelada  = "cancelada"
)

// Tipos de ítem del checklist.
const (
	ItemSiNo      = "si_no"
	ItemTexto     = "texto"
	ItemNumero    = "numero"
	ItemFoto      = "foto"
	ItemMedicion  = "medicion"
	ItemSeleccion = "seleccion"
)

// MotivosFalla son los códigos de orden.fallar (el detalle va en texto aparte).
var MotivosFalla = []string{"cliente_ausente", "sin_acceso", "sin_cobertura", "falta_material", "equipo_defectuoso", "clima", "cliente_rechaza", "otro"}

// ContactoOrden: contacto en sitio (capturado a mano o tomado de la instantánea de Argos).
type ContactoOrden struct {
	Nombre   *string `json:"nombre" nullable:"true"`
	Telefono *string `json:"telefono" nullable:"true"`
}

// Orden es una orden de trabajo. Cliente, servicio, zona y bodega son ids de Argos.
type Orden struct {
	ID               int64         `json:"id"`
	Numero           string        `json:"numero" doc:"Número legible (OT-000123)"`
	EmpresaID        *int64        `json:"empresaId" nullable:"true" doc:"Empresa en Argos; null sin conector"`
	Tipo             string        `json:"tipo" enum:"instalacion,visita,retiro,cambio_equipo,mantenimiento"`
	TipoID           int64         `json:"tipoId"`
	PlantillaID      *int64        `json:"plantillaId" nullable:"true" doc:"Plantilla de checklist"`
	TicketID         *int64        `json:"ticketId" nullable:"true" doc:"Ticket de origen"`
	ProspectoID      *int64        `json:"prospectoId" nullable:"true" doc:"Prospecto de origen (fase 4)"`
	ClienteID        *int64        `json:"clienteId" nullable:"true" doc:"Cliente en Argos"`
	ServicioID       *int64        `json:"servicioId" nullable:"true" doc:"Servicio en Argos"`
	ZonaID           *int64        `json:"zonaId" nullable:"true" doc:"Zona (Argos); define el alcance"`
	CuadrillaID      *int64        `json:"cuadrillaId" nullable:"true"`
	BodegaID         *int64        `json:"bodegaId" nullable:"true" doc:"Bodega del técnico en Argos (materiales y devoluciones)"`
	Direccion        string        `json:"direccion"`
	Lat              *float64      `json:"lat" nullable:"true"`
	Lon              *float64      `json:"lon" nullable:"true"`
	Contacto         ContactoOrden `json:"contacto"`
	Descripcion      string        `json:"descripcion"`
	TecnicoID        *int64        `json:"tecnicoId" nullable:"true" doc:"Usuario técnico asignado"`
	AsignadaEn       *time.Time    `json:"asignadaEn" nullable:"true"`
	Estado           string        `json:"estado" enum:"pendiente,programada,en_camino,en_sitio,pausada,cerrada,fallida,cancelada"`
	ProgramadaInicio *time.Time    `json:"programadaInicio" nullable:"true"`
	ProgramadaFin    *time.Time    `json:"programadaFin" nullable:"true"`
	EnCaminoEn       *time.Time    `json:"enCaminoEn" nullable:"true"`
	EnSitioEn        *time.Time    `json:"enSitioEn" nullable:"true"`
	FinalizadaEn     *time.Time    `json:"finalizadaEn" nullable:"true" doc:"Cierre, falla o cancelación"`
	Resultado        *string       `json:"resultado" nullable:"true" enum:"exitosa,parcial,fallida,cancelada"`
	MotivoFalla      *string       `json:"motivoFalla" nullable:"true" doc:"Código de la falla (fallida)"`
	Motivo           *string       `json:"motivo" nullable:"true" doc:"Detalle de la falla, la cancelación o la reapertura"`
	Version          int32         `json:"version"`
	CreadoEn         time.Time     `json:"creadoEn"`
	ActualizadoEn    time.Time     `json:"actualizadoEn"`
	Origen           string        `json:"origen,omitempty" enum:"panel,api,argos,sistema" doc:"De dónde vino: panel, api, argos (retiro automático por servicio retirado) o sistema"`
	PausadaDesde     *string       `json:"pausadaDesde,omitempty" enum:"en_camino,en_sitio" doc:"Solo pausada: el estado al que vuelve al reanudar"`
	PausadaEn        *time.Time    `json:"pausadaEn,omitempty" doc:"Solo pausada: inicio de la pausa en curso"`
	TiempoPausadoSeg int64         `json:"tiempoPausadoSeg" required:"false" doc:"Segundos pausados en las pausas ya terminadas (la en curso se cuenta desde pausadaEn)"`
	// Aviso: la acción se aplicó, pero con una advertencia (checklist_no_vigente,
	// existencia_insuficiente). Solo en la respuesta de una acción.
	Aviso *string `json:"aviso,omitempty" enum:"checklist_no_vigente,existencia_insuficiente,version_anterior,pausa_ajustada" doc:"La acción se aplicó con una advertencia: checklist_no_vigente (otra marca más reciente gana; queda en el historial), existencia_insuficiente (más material que la existencia de la bodega según Argos), version_anterior (formulario agregado con la versión que veía el teléfono, que ya no es la actual) o pausa_ajustada (el inicio de la pausa caía antes del fin de la anterior; se ajustó)"`
}

func aOrden(f campobd.CampoOrden, tipo string) Orden {
	return Orden{
		ID: f.ID, Numero: f.Numero, EmpresaID: f.EmpresaID, Tipo: tipo, TipoID: f.TipoID, PlantillaID: f.PlantillaID,
		TicketID: f.TicketID, ProspectoID: f.ProspectoID, ClienteID: f.ClienteID, ServicioID: f.ServicioID,
		ZonaID: f.ZonaID, CuadrillaID: f.CuadrillaID, BodegaID: f.BodegaID, Direccion: f.Direccion, Lat: f.Lat, Lon: f.Lon,
		Contacto: ContactoOrden{Nombre: f.ContactoNombre, Telefono: f.ContactoTelefono}, Descripcion: f.Descripcion,
		TecnicoID: f.TecnicoID, AsignadaEn: f.AsignadaEn, Estado: f.Estado, ProgramadaInicio: f.ProgramadaInicio,
		ProgramadaFin: f.ProgramadaFin, EnCaminoEn: f.EnCaminoEn, EnSitioEn: f.EnSitioEn, FinalizadaEn: f.FinalizadaEn,
		Resultado: f.Resultado, MotivoFalla: f.MotivoFalla, Motivo: f.Motivo, Version: f.Version,
		CreadoEn: f.CreatedAt, ActualizadoEn: f.UpdatedAt, Origen: f.Origen,
		PausadaDesde: f.PausadaDesde, PausadaEn: f.PausadaEn, TiempoPausadoSeg: f.PausadaSeg,
	}
}

// ItemChecklist es un ítem de un formulario de la orden con la marca vigente (si la hay).
type ItemChecklist struct {
	ItemID       int64      `json:"itemId"`
	FormularioID uuid.UUID  `json:"formularioId" required:"false" doc:"Formulario de la orden al que pertenece (formularios[].id)"`
	Codigo       string     `json:"codigo"`
	Etiqueta     string     `json:"etiqueta"`
	Tipo         string     `json:"tipo" enum:"si_no,texto,numero,foto,medicion,seleccion"`
	Obligatorio  bool       `json:"obligatorio"`
	Unidad       string     `json:"unidad" doc:"Para medicion y numero (dBm, Mbps…)"`
	Opciones     []string   `json:"opciones,omitempty" doc:"Solo seleccion: las opciones válidas"`
	Posicion     int32      `json:"posicion"`
	Valor        any        `json:"valor" nullable:"true" doc:"si_no: boolean; texto y seleccion: string (una de las opciones); numero y medicion: number; foto: uuid del adjunto. null = sin marcar"`
	MarcadoPor   *int64     `json:"marcadoPor" nullable:"true"`
	OcurridoEn   *time.Time `json:"ocurridoEn" nullable:"true" doc:"Hora de la marca vigente (gana la más reciente)"`
}

// FormularioOrden es un formulario de la orden: una plantilla en la versión con que se agregó
// (lo respondido nunca cambia aunque la plantilla se edite). Sus ítems vienen en checklist[]
// con su formularioId.
type FormularioOrden struct {
	ID            uuid.UUID `json:"id" doc:"Agregado por el técnico sin señal: el id de su operación del lote"`
	PlantillaID   int64     `json:"plantillaId"`
	Codigo        string    `json:"codigo"`
	Nombre        string    `json:"nombre" doc:"Nombre de la plantilla en su versión"`
	VersionID     int64     `json:"versionId"`
	NumeroVersion int32     `json:"numeroVersion"`
	RequiereFirma bool      `json:"requiereFirma"`
	Origen        string    `json:"origen" enum:"tipo,despacho,tecnico" doc:"tipo: el de la orden al crearla; despacho: lo asignó despacho; tecnico: lo agregó el técnico en campo"`
	AgregadoPor   *int64    `json:"agregadoPor" nullable:"true"`
	AgregadoEn    time.Time `json:"agregadoEn"`
	ConRespuestas bool      `json:"conRespuestas" doc:"Tiene alguna marca o adjunto: ya no se puede quitar"`
}

// PausaOrden es una pausa de la orden (en curso si fin es null).
type PausaOrden struct {
	ID          int64      `json:"id"`
	Motivo      string     `json:"motivo" enum:"esperando_material,cliente_ausente,clima,acceso,otro"`
	Nota        *string    `json:"nota" nullable:"true"`
	DesdeEstado string     `json:"desdeEstado" enum:"en_camino,en_sitio"`
	Inicio      time.Time  `json:"inicio"`
	Fin         *time.Time `json:"fin" nullable:"true" doc:"null = en curso"`
	DuracionSeg *int64     `json:"duracionSeg" nullable:"true" doc:"null = en curso"`
	Cierre      *string    `json:"cierre" nullable:"true" enum:"reanudada,cancelada,reprogramada"`
	PausadaPor  *int64     `json:"pausadaPor" nullable:"true"`
	CerradaPor  *int64     `json:"cerradaPor" nullable:"true" doc:"Quien la reanudó, canceló o reprogramó"`
}

// NotaOrden: nota de la orden (técnico o despacho).
type NotaOrden struct {
	ID          uuid.UUID `json:"id" doc:"Id de la operación del lote que la creó (o uno v7 del servidor)"`
	Texto       string    `json:"texto"`
	AutorID     *int64    `json:"autorId" nullable:"true"`
	AutorNombre string    `json:"autorNombre"`
	Canal       string    `json:"canal"`
	OcurridoEn  time.Time `json:"ocurridoEn"`
}

// MaterialOrden: material usado (producto de Argos).
type MaterialOrden struct {
	ID               uuid.UUID `json:"id" doc:"Id de la operación del lote que lo registró (o uno v7 del servidor)"`
	ProductoID       int64     `json:"productoId"`
	Descripcion      string    `json:"descripcion"`
	Cantidad         float64   `json:"cantidad"`
	BodegaID         *int64    `json:"bodegaId" nullable:"true"`
	ExcedeExistencia bool      `json:"excedeExistencia" doc:"Se usó más de lo que había en la bodega según Argos"`
	OcurridoEn       time.Time `json:"ocurridoEn"`
}

// EquipoOrden: equipo instalado o retirado.
type EquipoOrden struct {
	ID         uuid.UUID `json:"id" doc:"Id de la operación del lote que lo registró (o uno v7 del servidor)"`
	Accion     string    `json:"accion" enum:"instalar,retirar"`
	UnidadID   *int64    `json:"unidadId" nullable:"true" doc:"Unidad en el inventario de Argos"`
	Serie      *string   `json:"serie" nullable:"true"`
	MAC        *string   `json:"mac" nullable:"true" doc:"AA:BB:CC:DD:EE:FF"`
	Modelo     string    `json:"modelo"`
	Condicion  *string   `json:"condicion" nullable:"true" enum:"buena,danada,perdido" doc:"Solo retirar"`
	Validado   bool      `json:"validado" doc:"Retirado y verificado contra el comodato del servicio en Argos (false: sin conector o Argos no respondió)"`
	OcurridoEn time.Time `json:"ocurridoEn"`
}

// FirmaOrden: firma del cliente (sin el SVG: es dato sensible; se baja como adjunto).
type FirmaOrden struct {
	Nombre     string     `json:"nombre"`
	Documento  string     `json:"documento"`
	AdjuntoID  *uuid.UUID `json:"adjuntoId" nullable:"true" doc:"PNG de la firma (adjunto)"`
	OcurridoEn time.Time  `json:"ocurridoEn"`
}

// AdjuntoOrden es un adjunto registrado en la orden (la subida es de I1).
type AdjuntoOrden struct {
	ID          uuid.UUID `json:"id"`
	Tipo        string    `json:"tipo" enum:"foto,firma,documento"`
	ItemID      *int64    `json:"itemId" nullable:"true" doc:"Ítem del checklist al que corresponde (foto)"`
	Descripcion string    `json:"descripcion"`
	OcurridoEn  time.Time `json:"ocurridoEn"`
}

// OrdenDetalle es la orden con su trabajo registrado.
type OrdenDetalle struct {
	Orden
	RequiereFirma bool              `json:"requiereFirma" doc:"Algún formulario pide la firma del cliente"`
	Formularios   []FormularioOrden `json:"formularios" required:"false" doc:"Formularios vigentes, en el orden en que se agregaron"`
	Checklist     []ItemChecklist   `json:"checklist" doc:"Ítems de todos los formularios (formularioId), en el orden de los formularios"`
	Pausas        []PausaOrden      `json:"pausas" required:"false" doc:"Pausas de la orden (la en curso, con fin null)"`
	Notas         []NotaOrden       `json:"notas"`
	Materiales    []MaterialOrden   `json:"materiales"`
	Equipos       []EquipoOrden     `json:"equipos"`
	Firma         *FirmaOrden       `json:"firma,omitempty" doc:"Ausente si no hay firma"`
	Adjuntos      []AdjuntoOrden    `json:"adjuntos"`
	Cita          *CitaOrden        `json:"cita,omitempty" doc:"Cita de la agenda (con la ventana acordada con el cliente); ausente si no está programada"`
}

// CitaOrden es la cita de la agenda de una orden programada.
type CitaOrden struct {
	TecnicoID     int64      `json:"tecnicoId"`
	Inicio        time.Time  `json:"inicio"`
	Fin           time.Time  `json:"fin"`
	VentanaInicio *time.Time `json:"ventanaInicio" nullable:"true" doc:"Ventana de llegada acordada con el cliente"`
	VentanaFin    *time.Time `json:"ventanaFin" nullable:"true"`
}

// TipoOrden con sus plantillas.
type TipoOrden struct {
	ID            int64       `json:"id"`
	Codigo        string      `json:"codigo" enum:"instalacion,visita,retiro,cambio_equipo,mantenimiento"`
	Nombre        string      `json:"nombre"`
	DuracionMin   int32       `json:"duracionMin" doc:"Duración estimada"`
	PlantillaID   *int64      `json:"plantillaId" nullable:"true" doc:"Plantilla por defecto"`
	ExigeInstalar bool        `json:"exigeInstalar" doc:"El cierre exige al menos un equipo instalado"`
	ExigeRetirar  bool        `json:"exigeRetirar" doc:"El cierre exige al menos un equipo retirado"`
	Plantillas    []Plantilla `json:"plantillas"`
}

// Plantilla de checklist (formulario) en su versión actual.
type Plantilla struct {
	ID            int64           `json:"id"`
	Codigo        string          `json:"codigo"`
	Nombre        string          `json:"nombre"`
	RequiereFirma bool            `json:"requiereFirma"`
	Items         []ItemPlantilla `json:"items"`
}

// ItemPlantilla es un ítem de una versión de plantilla.
type ItemPlantilla struct {
	ID          int64    `json:"id"`
	Codigo      string   `json:"codigo"`
	Etiqueta    string   `json:"etiqueta"`
	Tipo        string   `json:"tipo" enum:"si_no,texto,numero,foto,medicion,seleccion"`
	Obligatorio bool     `json:"obligatorio"`
	Unidad      string   `json:"unidad"`
	Opciones    []string `json:"opciones,omitempty" doc:"Solo seleccion"`
	Posicion    int32    `json:"posicion"`
}

// aItemPlantilla convierte una fila de ítem.
func aItemPlantilla(it campobd.CampoPlantillaItem) ItemPlantilla {
	return ItemPlantilla{ID: it.ID, Codigo: it.Codigo, Etiqueta: it.Etiqueta, Tipo: it.Tipo, Obligatorio: it.Obligatorio,
		Unidad: it.Unidad, Opciones: opcionesDe(it.Opciones), Posicion: it.Posicion}
}

// opcionesDe lee las opciones (jsonb) de un ítem seleccion.
func opcionesDe(crudo []byte) []string {
	var o []string
	if err := json.Unmarshal(crudo, &o); err != nil || len(o) == 0 {
		return nil
	}
	return o
}

// EventoOrden es una entrada del historial.
type EventoOrden struct {
	ID          int64          `json:"id"`
	Tipo        string         `json:"tipo" enum:"creada,estado,asignada,programada,formulario"`
	De          *string        `json:"de" nullable:"true"`
	A           *string        `json:"a" nullable:"true"`
	Motivo      *string        `json:"motivo" nullable:"true"`
	Datos       map[string]any `json:"datos" doc:"GPS (lat, lon, precision), técnico, fechas, formulario (accion agregado|quitado, formulario_id, plantilla_id, nombre, origen) o pausa (pausa_id, motivo, nota), según el tipo"`
	ActorTipo   string         `json:"actorTipo"`
	ActorID     *int64         `json:"actorId" nullable:"true"`
	ActorNombre string         `json:"actorNombre"`
	Canal       string         `json:"canal"`
	OcurridoEn  time.Time      `json:"ocurridoEn" doc:"Hora del hecho (la del teléfono corregida, si vino del lote)"`
	CreadoEn    time.Time      `json:"creadoEn" doc:"Hora de registro en el servidor"`
}

func aEvento(f campobd.CampoOrdenEvento) EventoOrden {
	datos := map[string]any{}
	_ = json.Unmarshal(f.Datos, &datos)
	return EventoOrden{
		ID: f.ID, Tipo: f.Tipo, De: f.De, A: f.A, Motivo: f.Motivo, Datos: datos, ActorTipo: f.ActorTipo,
		ActorID: f.ActorID, ActorNombre: f.ActorNombre, Canal: f.Canal, OcurridoEn: f.OcurridoEn, CreadoEn: f.CreatedAt,
	}
}

// numero convierte un numeric de Postgres a float64 (cantidades de material).
func numero(n pgtype.Numeric) float64 {
	f, err := n.Float64Value()
	if err != nil || !f.Valid {
		return 0
	}
	return f.Float64
}

// aNumeric lleva una cantidad a numeric(12,3) (milésimas, redondeadas).
func aNumeric(v float64) pgtype.Numeric {
	return pgtype.Numeric{Int: big.NewInt(int64(math.Round(v * 1000))), Exp: -3, Valid: true}
}

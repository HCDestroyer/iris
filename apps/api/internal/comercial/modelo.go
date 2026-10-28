package comercial

import (
	"time"

	"github.com/google/uuid"

	comercialbd "github.com/HCDestroyer/iris/apps/api/internal/comercial/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/adjuntos"
)

// Prospecto es un posible cliente en el embudo.
type Prospecto struct {
	ID                int64      `json:"id"`
	Numero            string     `json:"numero" doc:"PR-000001"`
	EmpresaID         *int64     `json:"empresaId" nullable:"true" doc:"Empresa en Argos (null sin conector)"`
	Origen            string     `json:"origen" enum:"panel,web,referido,argos,otro"`
	OrigenDetalle     *string    `json:"origenDetalle" nullable:"true" doc:"Quién lo refirió, campaña…"`
	Nombre            string     `json:"nombre"`
	Documento         *string    `json:"documento" nullable:"true" doc:"NIT o DPI"`
	Telefono          *string    `json:"telefono" nullable:"true"`
	Correo            *string    `json:"correo" nullable:"true"`
	Direccion         string     `json:"direccion"`
	Lat               *float64   `json:"lat" nullable:"true"`
	Lon               *float64   `json:"lon" nullable:"true"`
	ZonaID            *int64     `json:"zonaId" nullable:"true"`
	PlanInteresID     *int64     `json:"planInteresId" nullable:"true" doc:"Plan de Argos que le interesa"`
	PlanInteresNombre *string    `json:"planInteresNombre" nullable:"true"`
	VendedorID        *int64     `json:"vendedorId" nullable:"true" doc:"Responsable de ventas (usuario de Iris)"`
	Estado            string     `json:"estado" enum:"nuevo,contactado,factibilidad,cotizado,contrato,ganado,perdido"`
	Notas             string     `json:"notas"`
	MotivoPerdida     *string    `json:"motivoPerdida" nullable:"true"`
	PerdidoEn         *time.Time `json:"perdidoEn" nullable:"true"`
	GanadoEn          *time.Time `json:"ganadoEn" nullable:"true"`
	ObraConfirmada    bool       `json:"obraConfirmada" doc:"Se ganó con factibilidad requiere_obra confirmada"`
	Alta              *Alta      `json:"alta,omitempty" doc:"Alta del cliente (solo ganado)"`
	CreadoEn          time.Time  `json:"creadoEn"`
	ActualizadoEn     time.Time  `json:"actualizadoEn"`
	Version           int32      `json:"version"`
}

// Alta es el resultado de ganar.
type Alta struct {
	Modo            string  `json:"modo" enum:"local,argos" doc:"local: sin conector, la orden con los datos capturados; argos: cliente y servicio por la cola de pendientes"`
	Estado          string  `json:"estado" enum:"esperando_cliente,esperando_servicio,completa"`
	ClienteArgosID  *int64  `json:"clienteArgosId" nullable:"true"`
	ServicioArgosID *int64  `json:"servicioArgosId" nullable:"true"`
	OrdenID         *int64  `json:"ordenId" nullable:"true" doc:"Orden de instalación (al completarse el alta)"`
	OrdenNumero     *string `json:"ordenNumero" nullable:"true"`
	RouterID        *int64  `json:"routerId" nullable:"true" doc:"Router de Argos del servicio"`
	Error           *string `json:"error" nullable:"true" doc:"El alta no pudo avanzar y necesita revisión manual (no se reintenta sola)"`
}

func aProspecto(f comercialbd.ComercialProspecto) Prospecto {
	p := Prospecto{
		ID: f.ID, Numero: f.Numero, EmpresaID: f.EmpresaID, Origen: f.Origen, OrigenDetalle: f.OrigenDetalle, Nombre: f.Nombre,
		Documento: f.Documento, Telefono: f.Telefono, Correo: f.Correo, Direccion: f.Direccion, Lat: f.Lat, Lon: f.Lon,
		ZonaID: f.ZonaID, PlanInteresID: f.PlanInteresID, PlanInteresNombre: f.PlanInteresNombre, VendedorID: f.VendedorID,
		Estado: f.Estado, Notas: f.Notas, MotivoPerdida: f.MotivoPerdida, PerdidoEn: f.PerdidoEn, GanadoEn: f.GanadoEn,
		ObraConfirmada: f.ObraConfirmada, CreadoEn: f.CreatedAt, ActualizadoEn: f.UpdatedAt, Version: f.Version,
	}
	if f.AltaModo != nil {
		p.Alta = &Alta{Modo: *f.AltaModo, Estado: deref(f.AltaEstado), ClienteArgosID: f.ClienteArgosID,
			ServicioArgosID: f.ServicioArgosID, OrdenID: f.OrdenID, RouterID: f.RouterID, Error: f.AltaError}
	}
	return p
}

// ProspectoDetalle es el prospecto con su embudo: factibilidad vigente, cotización viva, contrato
// vivo y las acciones hacia Argos (pendientes) del alta.
type ProspectoDetalle struct {
	Prospecto
	Factibilidad *Factibilidad      `json:"factibilidad,omitempty" doc:"La última evaluación"`
	Cotizacion   *Cotizacion        `json:"cotizacion,omitempty" doc:"La vigente o aceptada"`
	Contrato     *Contrato          `json:"contrato,omitempty" doc:"El borrador o firmado"`
	Pendientes   []PendienteArgos   `json:"pendientes" doc:"Acciones en Argos del alta (POST /clientes y POST /servicios)"`
	Acciones     AccionesDisponible `json:"acciones" doc:"Qué acción del embudo corresponde ahora (para los botones del panel; el servidor valida igual)"`
}

// AccionesDisponible: guía para el panel.
type AccionesDisponible struct {
	Contactar    bool `json:"contactar"`
	Evaluar      bool `json:"evaluar"`
	Cotizar      bool `json:"cotizar"`
	Aceptar      bool `json:"aceptar"`
	Contratar    bool `json:"contratar"`
	Firmar       bool `json:"firmar"`
	Ganar        bool `json:"ganar"`
	ConfirmaObra bool `json:"confirmaObra" doc:"Ganar exige confirmarObra (factibilidad requiere_obra)"`
	Perder       bool `json:"perder"`
	Reabrir      bool `json:"reabrir"`
}

// PendienteArgos resume un pendiente de la cola hacia Argos.
type PendienteArgos struct {
	ID             int64   `json:"id"`
	Operacion      string  `json:"operacion" doc:"crear_cliente, crear_servicio"`
	IdempotencyKey string  `json:"idempotencyKey"`
	Estado         string  `json:"estado" doc:"pendiente, en_curso, hecho, fallido_negocio, agotado, bloqueado"`
	Intentos       int32   `json:"intentos"`
	UltimoError    *string `json:"ultimoError" nullable:"true"`
	Version        int32   `json:"version" doc:"Para POST /integraciones/pendientes/{id}/reintentar"`
}

// EventoProspecto es una fila del historial.
type EventoProspecto struct {
	ID        int64          `json:"id"`
	Tipo      string         `json:"tipo" doc:"creado, estado, asignado, factibilidad, cotizacion, contrato, alta…"`
	De        *string        `json:"de" nullable:"true"`
	A         *string        `json:"a" nullable:"true"`
	Motivo    *string        `json:"motivo" nullable:"true"`
	Datos     map[string]any `json:"datos"`
	ActorTipo string         `json:"actorTipo"`
	ActorID   *int64         `json:"actorId" nullable:"true"`
	CreadoEn  time.Time      `json:"creadoEn"`
}

// Actividad es una llamada, visita, nota o mensaje del vendedor.
type Actividad struct {
	ID            int64      `json:"id"`
	ProspectoID   int64      `json:"prospectoId"`
	Tipo          string     `json:"tipo" enum:"llamada,visita,nota,mensaje"`
	Descripcion   string     `json:"descripcion"`
	Resultado     *string    `json:"resultado" nullable:"true"`
	OcurridaEn    time.Time  `json:"ocurridaEn"`
	ProximaAccion *string    `json:"proximaAccion" nullable:"true"`
	ProximaFecha  *time.Time `json:"proximaFecha" nullable:"true"`
	AutorID       *int64     `json:"autorId" nullable:"true"`
	CreadoEn      time.Time  `json:"creadoEn"`
}

func aActividad(f comercialbd.ComercialActividad) Actividad {
	return Actividad{ID: f.ID, ProspectoID: f.ProspectoID, Tipo: f.Tipo, Descripcion: f.Descripcion, Resultado: f.Resultado,
		OcurridaEn: f.OcurridaEn, ProximaAccion: f.ProximaAccion, ProximaFecha: f.ProximaFecha, AutorID: f.AutorID, CreadoEn: f.CreatedAt}
}

// Factibilidad es una evaluación técnica.
type Factibilidad struct {
	ID              int64     `json:"id"`
	ProspectoID     int64     `json:"prospectoId"`
	Resultado       string    `json:"resultado" enum:"viable,no_viable,requiere_obra,indeterminado" doc:"indeterminado: no hay datos para decidir (motivo); nunca se inventa"`
	Motor           string    `json:"motor" doc:"distancia (router más cercano de la instantánea de Argos), manual (visita técnica), ozmap (fase 6)"`
	Motivo          *string   `json:"motivo" nullable:"true" doc:"Por qué es indeterminado: sin_conector, conector_apagado, sin_coordenadas, sin_empresa, sin_instantanea, sin_routers_con_coordenadas"`
	Lat             *float64  `json:"lat" nullable:"true" doc:"Coordenadas evaluadas (las del prospecto en ese momento)"`
	Lon             *float64  `json:"lon" nullable:"true"`
	RouterID        *int64    `json:"routerId" nullable:"true" doc:"Router sugerido (Argos)"`
	RouterNombre    *string   `json:"routerNombre" nullable:"true"`
	Caja            *string   `json:"caja" nullable:"true" doc:"Caja sugerida (fase 6, OZmap)"`
	DistanciaM      *float64  `json:"distanciaM" nullable:"true" doc:"Distancia en línea recta al router, en metros"`
	ViableM         *int32    `json:"viableM" nullable:"true" doc:"Umbral aplicado: viable hasta estos metros"`
	ObraM           *int32    `json:"obraM" nullable:"true" doc:"Umbral aplicado: requiere obra hasta estos metros"`
	Notas           string    `json:"notas"`
	AnulaID         *int64    `json:"anulaId" nullable:"true" doc:"Manual que anula esta evaluación automática no_viable"`
	MotivoAnulacion *string   `json:"motivoAnulacion" nullable:"true"`
	EvaluadaPor     *int64    `json:"evaluadaPor" nullable:"true"`
	CreadoEn        time.Time `json:"creadoEn"`
}

func aFactibilidad(f comercialbd.ComercialFactibilidad) Factibilidad {
	return Factibilidad{ID: f.ID, ProspectoID: f.ProspectoID, Resultado: f.Resultado, Motor: f.Motor, Motivo: f.Motivo,
		Lat: f.Lat, Lon: f.Lon, RouterID: f.RouterID, RouterNombre: f.RouterNombre, Caja: f.Caja, DistanciaM: f.DistanciaM,
		ViableM: f.ViableM, ObraM: f.ObraM, Notas: f.Notas, AnulaID: f.AnulaID, MotivoAnulacion: f.MotivoAnulacion, EvaluadaPor: f.EvaluadaPor, CreadoEn: f.CreatedAt}
}

// DocumentoPDF es un PDF guardado como adjunto del núcleo.
type DocumentoPDF struct {
	AdjuntoID   uuid.UUID `json:"adjuntoId"`
	Estado      string    `json:"estado" enum:"pendiente,subiendo,completo,rechazado,no_generado" doc:"no_generado: no se pudo guardar; POST …/pdf lo vuelve a generar"`
	SHA256      *string   `json:"sha256" nullable:"true"`
	URLDescarga string    `json:"urlDescarga" doc:"GET (adjuntos.ver)"`
}

func documentoDe(id *uuid.UUID, estado, sha *string) *DocumentoPDF {
	if id == nil {
		return nil
	}
	d := &DocumentoPDF{AdjuntoID: *id, Estado: "no_generado", URLDescarga: adjuntos.RutaDescarga(*id)}
	if estado != nil {
		d.Estado = *estado
		d.SHA256 = sha
	}
	return d
}

// LineaCotizacion es una línea de la cotización. Montos como texto decimal con 2 cifras.
type LineaCotizacion struct {
	Posicion       int32  `json:"posicion"`
	Tipo           string `json:"tipo" enum:"plan,instalacion,equipo,descuento,otro"`
	PlanID         *int64 `json:"planId" nullable:"true"`
	ProductoID     *int64 `json:"productoId" nullable:"true"`
	Descripcion    string `json:"descripcion"`
	Cantidad       string `json:"cantidad" doc:"Decimal (2 cifras)"`
	PrecioUnitario string `json:"precioUnitario" doc:"Decimal (2 cifras)"`
	Recurrente     bool   `json:"recurrente" doc:"Mensual (plan) o único"`
	Total          string `json:"total" doc:"Decimal (2 cifras); negativo en los descuentos"`
}

// Cotizacion es una versión de la cotización de un prospecto.
type Cotizacion struct {
	ID             int64             `json:"id"`
	Numero         string            `json:"numero" doc:"COT-000001"`
	ProspectoID    int64             `json:"prospectoId"`
	Revision       int32             `json:"revision" doc:"1, 2, 3… (una nueva reemplaza la anterior sin borrarla)"`
	Estado         string            `json:"estado" enum:"vigente,aceptada,reemplazada"`
	Vencida        bool              `json:"vencida" doc:"vigente y pasada su vigencia (no se puede aceptar)"`
	Moneda         string            `json:"moneda"`
	TotalMensual   string            `json:"totalMensual" doc:"Decimal (2 cifras)"`
	TotalUnico     string            `json:"totalUnico" doc:"Decimal (2 cifras): instalación, equipos y otros, menos descuentos únicos"`
	VigenteHasta   time.Time         `json:"vigenteHasta"`
	PlanID         *int64            `json:"planId" nullable:"true" doc:"Plan de Argos de la línea de plan"`
	Notas          string            `json:"notas"`
	Lineas         []LineaCotizacion `json:"lineas"`
	PDF            *DocumentoPDF     `json:"pdf,omitempty"`
	ReemplazadaPor *int64            `json:"reemplazadaPor" nullable:"true"`
	AceptadaEn     *time.Time        `json:"aceptadaEn" nullable:"true"`
	AceptadaPor    *int64            `json:"aceptadaPor" nullable:"true"`
	CreadoPor      *int64            `json:"creadoPor" nullable:"true"`
	CreadoEn       time.Time         `json:"creadoEn"`
	Version        int32             `json:"version"`
}

// Contrato es el contrato de un prospecto.
type Contrato struct {
	ID                int64         `json:"id"`
	Numero            string        `json:"numero" doc:"CTR-000001"`
	ProspectoID       int64         `json:"prospectoId"`
	CotizacionID      int64         `json:"cotizacionId"`
	Plantilla         string        `json:"plantilla"`
	Estado            string        `json:"estado" enum:"borrador,firmado,anulado"`
	PDF               *DocumentoPDF `json:"pdf,omitempty" doc:"El borrador para leer antes de firmar"`
	PDFFirmado        *DocumentoPDF `json:"pdfFirmado,omitempty" doc:"El PDF firmado (su SHA-256 queda en la auditoría)"`
	FirmanteNombre    *string       `json:"firmanteNombre" nullable:"true"`
	FirmanteDocumento *string       `json:"firmanteDocumento" nullable:"true"`
	FirmadoEn         *time.Time    `json:"firmadoEn" nullable:"true"`
	FirmadoPor        *int64        `json:"firmadoPor" nullable:"true" doc:"Usuario de Iris que registró la firma"`
	MotivoAnulacion   *string       `json:"motivoAnulacion" nullable:"true"`
	CreadoEn          time.Time     `json:"creadoEn"`
	Version           int32         `json:"version"`
}

package agenda

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	agendabd "github.com/HCDestroyer/iris/apps/api/internal/agenda/bd"
)

// Zona de trabajo: espejo de Argos (mismo id) o local; polígono GeoJSON propio opcional.
type Zona struct {
	ID             int64          `json:"id" doc:"Id de Argos (origen argos) o local (desde 10^12)"`
	EmpresaID      *int64         `json:"empresaId" nullable:"true" doc:"Empresa en Argos; null = todas (o sin conector)"`
	Origen         string         `json:"origen" enum:"argos,local,migrada" doc:"argos: espejo del catálogo de Argos; local: creada en Iris; migrada: id que ya estaba en uso sin catálogo"`
	Nombre         string         `json:"nombre"`
	Poligono       map[string]any `json:"poligono" nullable:"true" doc:"GeoJSON Polygon o MultiPolygon ([lon, lat]) propio de Iris"`
	Activa         bool           `json:"activa"`
	SincronizadoEn *time.Time     `json:"sincronizadoEn" nullable:"true" doc:"Último refresco desde Argos"`
	Version        int32          `json:"version"`
	CreadoEn       time.Time      `json:"creadoEn"`
	ActualizadoEn  time.Time      `json:"actualizadoEn"`
}

func aZona(f agendabd.AgendaZona) Zona {
	var poligono map[string]any
	if len(f.Poligono) > 0 {
		_ = json.Unmarshal(f.Poligono, &poligono)
	}
	return Zona{
		ID: f.ID, EmpresaID: f.EmpresaID, Origen: f.Origen, Nombre: f.Nombre, Poligono: poligono, Activa: f.Activa,
		SincronizadoEn: f.SincronizadoEn, Version: f.Version, CreadoEn: f.CreatedAt, ActualizadoEn: f.UpdatedAt,
	}
}

// Capacidad diaria por tipo de orden (día local).
type Capacidad struct {
	Tipo    *string `json:"tipo" nullable:"true" enum:"instalacion,visita,retiro,cambio_equipo,mantenimiento" doc:"Tipo de orden; null = todos los tipos"`
	Cupos   *int32  `json:"cupos" nullable:"true" minimum:"1" maximum:"200" doc:"Citas por día; null = sin límite de cupos"`
	Minutos *int32  `json:"minutos" nullable:"true" minimum:"1" maximum:"10080" doc:"Minutos programados por día; null = sin límite de minutos"`
}

// Cuadrilla de técnicos.
type Cuadrilla struct {
	ID            int64       `json:"id"`
	EmpresaID     *int64      `json:"empresaId" nullable:"true"`
	Nombre        string      `json:"nombre"`
	Activa        bool        `json:"activa"`
	ZonaIDs       []int64     `json:"zonaIds" doc:"Zonas que atiende; vacío = todas"`
	Capacidad     []Capacidad `json:"capacidad" doc:"Capacidad diaria de la cuadrilla (suma de sus técnicos)"`
	Version       int32       `json:"version"`
	CreadoEn      time.Time   `json:"creadoEn"`
	ActualizadoEn time.Time   `json:"actualizadoEn"`
}

// Turno semanal (hora local, America/Guatemala).
type Turno struct {
	Dia    int16  `json:"dia" minimum:"1" maximum:"7" doc:"1 = lunes … 7 = domingo"`
	Inicio string `json:"inicio" pattern:"^([01][0-9]|2[0-3]):[0-5][0-9]$" doc:"HH:MM"`
	Fin    string `json:"fin" pattern:"^(([01][0-9]|2[0-3]):[0-5][0-9]|24:00)$" doc:"HH:MM (hasta 24:00); posterior al inicio"`
}

// Tecnico es la ficha de agenda de un usuario técnico.
type Tecnico struct {
	ID            int64     `json:"id" doc:"Id de la ficha"`
	UsuarioID     int64     `json:"usuarioId" doc:"Usuario (es el tecnicoId de las órdenes)"`
	Nombre        string    `json:"nombre"`
	Correo        string    `json:"correo"`
	CuadrillaID   *int64    `json:"cuadrillaId" nullable:"true"`
	BodegaID      *int64    `json:"bodegaId" nullable:"true" doc:"Bodega del técnico en Argos"`
	Habilidades   []string  `json:"habilidades"`
	BaseLat       *float64  `json:"baseLat" nullable:"true"`
	BaseLon       *float64  `json:"baseLon" nullable:"true"`
	Activo        bool      `json:"activo" doc:"Ficha activa y usuario activo"`
	Version       int32     `json:"version"`
	CreadoEn      time.Time `json:"creadoEn"`
	ActualizadoEn time.Time `json:"actualizadoEn"`
}

// TecnicoDetalle: la ficha con sus turnos, capacidad y ausencias vigentes o futuras.
type TecnicoDetalle struct {
	Tecnico
	Turnos    []Turno     `json:"turnos"`
	Capacidad []Capacidad `json:"capacidad"`
	Ausencias []Ausencia  `json:"ausencias" doc:"Vigentes o futuras, sin anular"`
}

// Ausencia de un técnico.
type Ausencia struct {
	ID        int64      `json:"id"`
	TecnicoID int64      `json:"tecnicoId" doc:"Ficha"`
	Inicio    time.Time  `json:"inicio"`
	Fin       time.Time  `json:"fin"`
	Motivo    string     `json:"motivo" enum:"vacaciones,enfermedad,capacitacion,permiso,otro"`
	Detalle   string     `json:"detalle"`
	AnuladaEn *time.Time `json:"anuladaEn" nullable:"true"`
	Version   int32      `json:"version"`
	CreadoEn  time.Time  `json:"creadoEn"`
}

func aAusencia(f agendabd.AgendaAusencia) Ausencia {
	return Ausencia{ID: f.ID, TecnicoID: f.TecnicoID, Inicio: f.Inicio, Fin: f.Fin, Motivo: f.Motivo, Detalle: f.Detalle,
		AnuladaEn: f.AnuladaEn, Version: f.Version, CreadoEn: f.CreatedAt}
}

// Cita de una orden (lo que la agenda guarda al programar).
type Cita struct {
	OrdenID       int64      `json:"ordenId"`
	TecnicoID     int64      `json:"tecnicoId" doc:"Usuario técnico"`
	Inicio        time.Time  `json:"inicio"`
	Fin           time.Time  `json:"fin"`
	VentanaInicio *time.Time `json:"ventanaInicio" nullable:"true" doc:"Ventana de llegada acordada con el cliente"`
	VentanaFin    *time.Time `json:"ventanaFin" nullable:"true"`
	Version       int32      `json:"version"`
}

func aCita(f agendabd.AgendaCita) Cita {
	return Cita{OrdenID: f.OrdenID, TecnicoID: f.TecnicoID, Inicio: f.Inicio, Fin: f.Fin, VentanaInicio: f.VentanaInicio,
		VentanaFin: f.VentanaFin, Version: f.Version}
}

// hhmm convierte un time de Postgres a HH:MM (24:00 para el fin del día).
func hhmm(t pgtype.Time) string {
	min := t.Microseconds / int64(time.Minute/time.Microsecond)
	return fmt.Sprintf("%02d:%02d", min/60, min%60)
}

// duracionDe: el time de Postgres como duración desde la medianoche.
func duracionDe(t pgtype.Time) time.Duration { return time.Duration(t.Microseconds) * time.Microsecond }

// aTime: HH:MM → time de Postgres.
func aTime(v string) (pgtype.Time, bool) {
	var h, mi int
	if _, err := fmt.Sscanf(v, "%d:%d", &h, &mi); err != nil || len(v) != 5 || h < 0 || mi < 0 || mi > 59 || h > 24 || (h == 24 && mi != 0) {
		return pgtype.Time{}, false
	}
	return pgtype.Time{Microseconds: int64(h*60+mi) * int64(time.Minute/time.Microsecond), Valid: true}, true
}

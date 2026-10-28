package agenda

import (
	"encoding/json"
	"time"

	agendabd "github.com/HCDestroyer/iris/apps/api/internal/agenda/bd"
)

// Ruta del día de un técnico (resumen).
type Ruta struct {
	ID                   int64      `json:"id"`
	EmpresaID            *int64     `json:"empresaId" nullable:"true"`
	TecnicoID            int64      `json:"tecnicoId" doc:"Usuario técnico (el tecnicoId de las órdenes)"`
	TecnicoNombre        string     `json:"tecnicoNombre"`
	Fecha                string     `json:"fecha" doc:"Día local (YYYY-MM-DD, America/Guatemala)"`
	Estado               string     `json:"estado" enum:"borrador,publicada,reemplazada" doc:"borrador: propuesta sin publicar; publicada: la que ve el técnico; reemplazada: publicada antes que la actual (historial)"`
	Modo                 string     `json:"modo" enum:"optimizada,manual" doc:"optimizada: orden de Iris; manual: el orden que pidió despacho"`
	Motor                string     `json:"motor" enum:"osrm,haversine" doc:"Con qué se calcularon tiempos y distancias"`
	Degradada            bool       `json:"degradada" doc:"true = sin OSRM: orden por ventana y distancia en línea recta (haversine × 1,3 a 30 km/h); tiempos aproximados"`
	MotivoMotor          *string    `json:"motivoMotor" nullable:"true" enum:"sin_osrm,osrm_no_disponible,datos_cambiaron" doc:"Por qué se degradó (null con OSRM)"`
	OrigenSalida         string     `json:"origenSalida" enum:"base,posicion,ultima_parada,primera_parada" doc:"De dónde sale: base de la ficha, última posición (hoy), la última parada ya hecha o, sin ninguna, la primera parada"`
	SalidaLat            *float64   `json:"salidaLat" nullable:"true" doc:"null si la salida es la posición del técnico y no puedes ver posiciones (campo.ver_posiciones)"`
	SalidaLon            *float64   `json:"salidaLon" nullable:"true"`
	SaleEn               time.Time  `json:"saleEn" doc:"Hora de salida (inicio del turno o la primera cita)"`
	TerminaEn            *time.Time `json:"terminaEn" nullable:"true" doc:"Salida estimada de la última parada"`
	FinTurno             *time.Time `json:"finTurno" nullable:"true"`
	ExcedeTurno          bool       `json:"excedeTurno" doc:"La ruta termina después del fin del turno"`
	DistanciaM           int32      `json:"distanciaM"`
	ViajeSeg             int32      `json:"viajeSeg"`
	EsperaSeg            int32      `json:"esperaSeg" doc:"Esperas por llegar antes de la ventana"`
	TotalParadas         int32      `json:"totalParadas"`
	FueraDeVentana       int32      `json:"fueraDeVentana" doc:"Paradas a las que se llega después del fin de su ventana"`
	Desactualizada       bool       `json:"desactualizada" doc:"Una orden cambió de técnico o de hora (o entró una nueva ese día) después de proponerla: hay que volver a proponer. La publicada sigue vigente para el técnico hasta publicar otra"`
	DesactualizadaEn     *time.Time `json:"desactualizadaEn" nullable:"true"`
	MotivoDesactualizada *string    `json:"motivoDesactualizada" nullable:"true"`
	PropuestaPor         *int64     `json:"propuestaPor" nullable:"true"`
	PropuestaEn          time.Time  `json:"propuestaEn"`
	PublicadaPor         *int64     `json:"publicadaPor" nullable:"true"`
	PublicadaEn          *time.Time `json:"publicadaEn" nullable:"true"`
	ReemplazadaEn        *time.Time `json:"reemplazadaEn" nullable:"true"`
	ReemplazadaPorID     *int64     `json:"reemplazadaPorId" nullable:"true"`
	Version              int32      `json:"version"`
}

// ParadaRuta: una parada con lo estimado y lo real.
type ParadaRuta struct {
	Posicion        int16      `json:"posicion" doc:"1 = primera"`
	OrdenID         int64      `json:"ordenId" doc:"0 si la orden está fuera de tu alcance (oculta)"`
	Numero          string     `json:"numero"`
	Tipo            string     `json:"tipo" enum:"instalacion,visita,retiro,cambio_equipo,mantenimiento," doc:"Vacío si oculta"`
	Estado          string     `json:"estado" enum:"pendiente,programada,en_camino,en_sitio,cerrada,fallida,cancelada," doc:"Estado actual de la orden; vacío si oculta"`
	Oculta          bool       `json:"oculta" doc:"Fuera de tu alcance: solo la posición en la secuencia (sin orden, estado, horas, lugar ni viaje); el viaje de la parada siguiente sale en 0"`
	Vigente         bool       `json:"vigente" doc:"La orden sigue en este técnico y día; false = se movió después de proponer"`
	Direccion       string     `json:"direccion"`
	Lat             *float64   `json:"lat" nullable:"true"`
	Lon             *float64   `json:"lon" nullable:"true"`
	SinCoordenadas  bool       `json:"sinCoordenadas" doc:"Sin coordenadas: viaje 0 y al final de la ruta optimizada"`
	DuracionMin     int32      `json:"duracionMin" doc:"Duración estimada del tipo de orden"`
	VentanaInicio   *time.Time `json:"ventanaInicio" nullable:"true" doc:"Ventana de llegada acordada con el cliente"`
	VentanaFin      *time.Time `json:"ventanaFin" nullable:"true"`
	CitaInicio      *time.Time `json:"citaInicio" nullable:"true" doc:"Inicio de la cita al proponer"`
	Fija            bool       `json:"fija" doc:"Ya estaba en camino, en sitio o terminada al proponer: va primero, en el orden real"`
	LlegadaEstimada *time.Time `json:"llegadaEstimada" nullable:"true" doc:"null si oculta"`
	SalidaEstimada  *time.Time `json:"salidaEstimada" nullable:"true" doc:"null si oculta"`
	ViajeSeg        int32      `json:"viajeSeg" doc:"Viaje desde la parada anterior (o la salida)"`
	DistanciaM      int32      `json:"distanciaM"`
	EsperaSeg       int32      `json:"esperaSeg"`
	RetrasoSeg      int32      `json:"retrasoSeg" doc:"Llegada estimada después del fin de la ventana"`
	FueraDeVentana  bool       `json:"fueraDeVentana"`
	EnCaminoEn      *time.Time `json:"enCaminoEn" nullable:"true" doc:"Real"`
	LlegadaReal     *time.Time `json:"llegadaReal" nullable:"true" doc:"Real: llegada al sitio"`
	FinalizadaEn    *time.Time `json:"finalizadaEn" nullable:"true" doc:"Real: cierre o falla"`
	DesvioSeg       *int32     `json:"desvioSeg" nullable:"true" doc:"llegadaReal − llegadaEstimada (positivo = tarde)"`
}

// PuntoReal del recorrido.
type PuntoReal struct {
	Lat          float64   `json:"lat"`
	Lon          float64   `json:"lon"`
	RegistradaEn time.Time `json:"registradaEn"`
}

// RecorridoReal: lo que hizo el técnico ese día (campo.posiciones y las horas de sus órdenes).
type RecorridoReal struct {
	Disponible    bool        `json:"disponible" doc:"false = no se puede mostrar (motivo); nunca suponer «no se movió»"`
	Motivo        *string     `json:"motivo" nullable:"true" enum:"sin_permiso,sin_posiciones" doc:"sin_permiso: falta campo.ver_posiciones; sin_posiciones: la app no envió ubicación ese día"`
	Puntos        []PuntoReal `json:"puntos" doc:"Posiciones del día en orden (hasta 1000, muestreadas)"`
	TotalPuntos   int32       `json:"totalPuntos"`
	DistanciaM    int32       `json:"distanciaM" doc:"Suma en línea recta entre posiciones"`
	PrimeraEn     *time.Time  `json:"primeraEn" nullable:"true"`
	UltimaEn      *time.Time  `json:"ultimaEn" nullable:"true"`
	OrdenVisitado []int64     `json:"ordenVisitado" doc:"Órdenes en el orden real de llegada (las ocultas no van)"`
}

// RutaDetalle: la ruta con sus paradas, el trazo y el recorrido real.
type RutaDetalle struct {
	Ruta
	Geometria map[string]any `json:"geometria" nullable:"true" doc:"GeoJSON LineString [lon, lat] de la salida y las paradas: por calles con OSRM, rectas si degradada. null si hay paradas ocultas o si sale de la posición del técnico y no puedes ver posiciones"`
	Paradas   []ParadaRuta   `json:"paradas"`
	Real      RecorridoReal  `json:"real"`
}

func fechaTexto(t time.Time) string { return t.Format(time.DateOnly) }

func aRuta(f agendabd.AgendaRuta, nombre string, paradas, fuera int32) Ruta {
	r := Ruta{ID: f.ID, EmpresaID: f.EmpresaID, TecnicoID: f.TecnicoID, TecnicoNombre: nombre, Fecha: fechaTexto(f.Fecha),
		Estado: f.Estado, Modo: f.Modo, Motor: f.Motor, Degradada: f.Motor != "osrm", MotivoMotor: f.MotivoMotor,
		OrigenSalida: f.OrigenSalida, SalidaLat: f.SalidaLat, SalidaLon: f.SalidaLon, SaleEn: f.SaleEn, TerminaEn: f.TerminaEn,
		FinTurno: f.FinTurno, DistanciaM: f.DistanciaM, ViajeSeg: f.ViajeSeg, EsperaSeg: f.EsperaSeg, TotalParadas: paradas,
		FueraDeVentana: fuera, Desactualizada: f.Desactualizada, DesactualizadaEn: f.DesactualizadaEn,
		MotivoDesactualizada: f.MotivoDesactualizada, PropuestaPor: f.PropuestaPor, PropuestaEn: f.PropuestaEn,
		PublicadaPor: f.PublicadaPor, PublicadaEn: f.PublicadaEn, ReemplazadaEn: f.ReemplazadaEn,
		ReemplazadaPorID: f.ReemplazadaPorID, Version: f.Version}
	r.ExcedeTurno = f.TerminaEn != nil && f.FinTurno != nil && f.TerminaEn.After(*f.FinTurno)
	return r
}

func geometriaDe(crudo []byte) map[string]any {
	if len(crudo) == 0 {
		return nil
	}
	var g map[string]any
	if err := json.Unmarshal(crudo, &g); err != nil {
		return nil
	}
	return g
}

// RutaDelTecnico es la ruta publicada que recibe la app de campo (paquete del día).
type RutaDelTecnico struct {
	ID             int64
	Version        int32
	Fecha          string
	PublicadaEn    time.Time
	Desactualizada bool
	Motor          string
	Paradas        []ParadaDelTecnico
}

// ParadaDelTecnico: el orden de visita con la llegada estimada.
type ParadaDelTecnico struct {
	OrdenID         int64
	Posicion        int16
	LlegadaEstimada time.Time
	SalidaEstimada  time.Time
	FueraDeVentana  bool
}

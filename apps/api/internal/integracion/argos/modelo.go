package argos

import (
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/integracion/conector"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
)

// Tipos que expone la API de Iris (camelCase). El OpenAPI se genera de aquí.

// ClienteResumen es un cliente de Argos tal como lo muestra Iris.
type ClienteResumen struct {
	ID        int64    `json:"id" doc:"Id del cliente en Argos"`
	EmpresaID int64    `json:"empresaId" doc:"Empresa en Argos"`
	Codigo    string   `json:"codigo"`
	Nombre    string   `json:"nombre"`
	Telefono  string   `json:"telefono"`
	Direccion string   `json:"direccion"`
	Lat       *float64 `json:"lat" nullable:"true"`
	Lon       *float64 `json:"lon" nullable:"true"`
	ZonaID    *int64   `json:"zonaId" nullable:"true"`
	Estado    string   `json:"estado" doc:"Estado del cliente en Argos"`
}

// Servicio de un cliente de Argos.
type Servicio struct {
	ID           int64    `json:"id" doc:"Id del servicio en Argos"`
	EmpresaID    int64    `json:"empresaId"`
	ClienteID    int64    `json:"clienteId"`
	Codigo       string   `json:"codigo"`
	PlanID       *int64   `json:"planId" nullable:"true"`
	PlanNombre   string   `json:"planNombre"`
	RouterID     *int64   `json:"routerId" nullable:"true"`
	RouterNombre string   `json:"routerNombre"`
	ZonaID       *int64   `json:"zonaId" nullable:"true"`
	ZonaNombre   string   `json:"zonaNombre"`
	IP           string   `json:"ip"`
	Direccion    string   `json:"direccion"`
	Lat          *float64 `json:"lat" nullable:"true"`
	Lon          *float64 `json:"lon" nullable:"true"`
	Estado       string   `json:"estado" doc:"Estado del servicio en Argos (activo, suspendido, retirado…)"`
}

// EstadoCuenta resumido (§2.1): solo el saldo y las facturas vencidas. Nunca se guarda: se lee
// de Argos al abrir la ficha.
type EstadoCuenta struct {
	Saldo            *string `json:"saldo" nullable:"true" doc:"Saldo en texto decimal (numeric(14,2))"`
	Moneda           string  `json:"moneda"`
	FacturasVencidas *int    `json:"facturasVencidas" nullable:"true"`
	MontoVencido     *string `json:"montoVencido" nullable:"true" doc:"Monto vencido en texto decimal, si Argos lo informa"`
}

// Ficha de un cliente: datos, servicios y estado de cuenta.
type Ficha struct {
	Cliente      ClienteResumen `json:"cliente"`
	Servicios    []Servicio     `json:"servicios"`
	EstadoCuenta *EstadoCuenta  `json:"estadoCuenta,omitempty" doc:"Ausente si no se pudo leer (ver estadoCuentaError); nunca se muestran totales calculados sobre datos que no se leyeron"`
	// EstadoCuentaError explica por qué no hay estado de cuenta.
	EstadoCuentaError *string   `json:"estadoCuentaError" nullable:"true" enum:"sin_permiso,argos_no_disponible,argos_rechazo" doc:"sin_permiso: falta clientes.estado_cuenta; argos_no_disponible: Argos no respondió; argos_rechazo: Argos lo negó"`
	Stale             bool      `json:"stale" doc:"true: Argos no respondió y se muestra la última instantánea"`
	SincronizadoEn    time.Time `json:"sincronizadoEn" doc:"Cuándo se leyó de Argos esta instantánea"`
}

// ResultadoBusqueda de clientes.
type ResultadoBusqueda struct {
	Data       []ClienteResumen `json:"data"`
	NextCursor *string          `json:"nextCursor" nullable:"true" doc:"Cursor de la página siguiente; null si no hay más"`
	Stale      bool             `json:"stale" doc:"true: Argos no respondió y los resultados salen de las instantáneas locales (solo clientes ya abiertos)"`
}

// IPResuelta es el dueño de una IP según Argos.
type IPResuelta struct {
	IP         string `json:"ip"`
	ClienteID  *int64 `json:"clienteId" nullable:"true"`
	ServicioID *int64 `json:"servicioId" nullable:"true"`
	EmpresaID  *int64 `json:"empresaId" nullable:"true"`
	Fuente     string `json:"fuente" enum:"argos,instantanea" doc:"argos: respuesta de Argos; instantanea: Argos no respondió y se usó la instantánea local"`
	Stale      bool   `json:"stale"`
}

// ConectorApagado: no hay conector Argos activo; el panel trabaja con datos locales.
func ConectorApagado() *nucleo.Problema {
	return conector.ConectorApagado(Remoto.Nombre)
}

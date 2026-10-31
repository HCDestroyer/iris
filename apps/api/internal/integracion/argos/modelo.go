package argos

import (
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos/formato"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/conector"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
)

// Tipos que expone la API de Iris (camelCase). El OpenAPI se genera de aquí.

// ClienteResumen es un cliente de Argos tal como lo muestra Iris.
type ClienteResumen struct {
	ID        int64    `json:"id" doc:"Id del cliente en Argos"`
	EmpresaID int64    `json:"empresaId" doc:"Primera de empresas (0 sin servicios). En Argos el cliente no tiene empresa: la tienen sus servicios; usa empresas"`
	Empresas  []int64  `json:"empresas" required:"false" doc:"Empresas de Argos del cliente: las de sus servicios. Vacía = sin servicios (lo ven todas las empresas, como en Argos). En la búsqueda, solo las que se conocen (la instantánea o lo que mande Argos)"`
	Codigo    string   `json:"codigo" doc:"Código del cliente en Argos (C-000455)"`
	Nombre    string   `json:"nombre"`
	Telefono  string   `json:"telefono" doc:"Teléfono tal como está en Argos (texto libre). Para mostrar y llamar usa telefonos"`
	Direccion string   `json:"direccion" doc:"Dirección del cliente (su domicilio en Argos). No es la de instalación: esa es la de cada servicio"`
	Lat       *float64 `json:"lat" nullable:"true" doc:"Coordenadas del cliente, en grados decimales con 6 decimales; null si Argos no las tiene (o son 0,0)"`
	Lon       *float64 `json:"lon" nullable:"true"`
	ZonaID    *int64   `json:"zonaId" nullable:"true"`
	Estado    string   `json:"estado" doc:"Resumen del estado de sus servicios (el más grave: suspendido, limitado, activo, pendiente_instalacion, retirado), porque Argos no tiene estado de cliente; vacío sin servicios conocidos. En la búsqueda, lo que mande Argos (hoy vacío)"`
	// Datos de contacto y fiscales (Argos 1.4.0).
	Tipo                string             `json:"tipo" doc:"persona o empresa (Argos); vacío si Argos no lo informó"`
	TipoDocumentoFiscal string             `json:"tipoDocumentoFiscal" doc:"nit, cui, pasaporte o cf (Argos); vacío si no lo informó"`
	NitCui              string             `json:"nitCui" doc:"Documento fiscal tal como en Argos: sin guiones ni espacios"`
	NitCuiMostrar       string             `json:"nitCuiMostrar" doc:"El documento para mostrar: NIT 1234567-8, CUI 1234 56789 0101, CF"`
	Movil               string             `json:"movil" doc:"Móvil tal como está en Argos (texto libre)"`
	Correo              string             `json:"correo" doc:"Correo del cliente en Argos (email)"`
	Telefonos           []formato.Telefono `json:"telefonos" doc:"Teléfono y móvil normalizados para mostrar (+502 5512 3344) y llamar (E.164), sin perder el original; un campo con varios números da varios"`
	ZonaNombre          string             `json:"zonaNombre" doc:"Nombre de la zona del cliente en Argos"`
	SincronizadoEn      *time.Time         `json:"sincronizadoEn" nullable:"true" doc:"Cuándo se leyó de Argos (instantánea); null en un resultado de búsqueda leído en vivo"`
	// Servicios: solo en la búsqueda con ?servicios=true (selector de cliente del panel).
	Servicios []Servicio `json:"servicios" required:"false" nullable:"true" doc:"Solo en GET /clientes con servicios=true: los servicios del cliente en la empresa activa (todas sin empresa activa), para mostrarlos al buscar. null: no se pidieron o no se pudieron leer. Solo informativo: el servicio elegido se toma de la ficha GET /clientes/{id}"`
}

// Servicio de un cliente de Argos.
type Servicio struct {
	ID             int64     `json:"id" doc:"Id del servicio en Argos"`
	EmpresaID      int64     `json:"empresaId"`
	ClienteID      int64     `json:"clienteId"`
	Codigo         string    `json:"codigo"`
	PlanID         *int64    `json:"planId" nullable:"true"`
	PlanNombre     string    `json:"planNombre"`
	RouterID       *int64    `json:"routerId" nullable:"true"`
	RouterNombre   string    `json:"routerNombre"`
	ZonaID         *int64    `json:"zonaId" nullable:"true"`
	ZonaNombre     string    `json:"zonaNombre"`
	IP             string    `json:"ip" doc:"IPv4 vigente del servicio en Argos"`
	Direccion      string    `json:"direccion" doc:"Dirección de instalación del servicio. Vacía si Argos no tiene una propia: entonces aplica la del cliente"`
	Lat            *float64  `json:"lat" nullable:"true" doc:"Coordenadas de instalación (6 decimales); null si Argos no las tiene"`
	Lon            *float64  `json:"lon" nullable:"true"`
	Estado         string    `json:"estado" doc:"Estado del servicio en Argos (pendiente_instalacion, activo, limitado, suspendido, retirado)"`
	UserPPP        string    `json:"userPpp" doc:"Usuario PPPoE; vacío fuera de pppoe"`
	TipoConexion   string    `json:"tipoConexion" doc:"pppoe, ip_estatica o dhcp_fija"`
	SincronizadoEn time.Time `json:"sincronizadoEn" doc:"Cuándo se leyó de Argos"`
}

// CopiaArgos es lo que Iris sabía del cliente y del servicio al crear un ticket o una orden. Se
// conserva (integracion.argos_copias): si la ficha no se puede leer (conector apagado, Argos
// caído) o el servicio se eliminó en Argos, el ticket y la orden la muestran. Solo lleva lo
// operativo (nombre, dirección, teléfono principal, servicio, plan, IP): ni documento fiscal, ni
// correo, ni empresas. Si Argos elimina al cliente, queda anonimizada (solo los ids).
type CopiaArgos struct {
	Cliente        CopiaCliente      `json:"cliente"`
	Servicio       *CopiaServicio    `json:"servicio,omitempty" doc:"El servicio del ticket o la orden; ausente si no tenía, Iris no lo conocía o la copia está anonimizada"`
	Direccion      string            `json:"direccion" doc:"La que aplica: la de instalación del servicio o, si no tiene, la del cliente"`
	Telefono       *formato.Telefono `json:"telefono,omitempty" doc:"El de contacto: el primer móvil válido, si no el primer teléfono válido y, si ninguno se pudo interpretar, el primero tal cual; ausente si el cliente no tiene teléfonos"`
	Completa       bool              `json:"completa" doc:"false: se copió de una instantánea anterior a los datos coherentes (puede faltar la IP o el teléfono móvil)"`
	Anonimizada    bool              `json:"anonimizada" doc:"true: Argos eliminó al cliente y la copia quedó solo con los ids"`
	SincronizadoEn time.Time         `json:"sincronizadoEn" doc:"Cuándo se había leído de Argos lo copiado"`
	CopiadoEn      time.Time         `json:"copiadoEn" doc:"Cuándo se copió (al crear el ticket o la orden)"`
}

// CopiaCliente: lo operativo del cliente en una copia.
type CopiaCliente struct {
	ID         int64    `json:"id"`
	Codigo     string   `json:"codigo"`
	Nombre     string   `json:"nombre"`
	Direccion  string   `json:"direccion" doc:"Dirección del cliente (no la de instalación)"`
	Lat        *float64 `json:"lat" nullable:"true"`
	Lon        *float64 `json:"lon" nullable:"true"`
	ZonaNombre string   `json:"zonaNombre"`
	Estado     string   `json:"estado" doc:"Resumen del estado de sus servicios al copiar"`
}

// CopiaServicio: lo operativo del servicio en una copia.
type CopiaServicio struct {
	ID           int64    `json:"id"`
	PlanNombre   string   `json:"planNombre"`
	Estado       string   `json:"estado"`
	Direccion    string   `json:"direccion" doc:"Dirección de instalación; vacía si Argos no tenía una propia"`
	Lat          *float64 `json:"lat" nullable:"true"`
	Lon          *float64 `json:"lon" nullable:"true"`
	IP           string   `json:"ip"`
	RouterNombre string   `json:"routerNombre"`
	ZonaNombre   string   `json:"zonaNombre"`
}

// EstadoCuenta resumido (§2.1): solo el saldo y las facturas vencidas. Nunca se guarda: se lee
// de Argos al abrir la ficha.
type EstadoCuenta struct {
	Saldo            *string `json:"saldo" nullable:"true" doc:"Saldo en texto decimal (numeric(14,2))"`
	Moneda           string  `json:"moneda"`
	FacturasVencidas *int    `json:"facturasVencidas" nullable:"true"`
	MontoVencido     *string `json:"montoVencido" nullable:"true" doc:"Monto vencido en texto decimal, si Argos lo informa"`
}

// ServicioEliminado es un servicio que Argos eliminó (webhook servicio.eliminado, contrato 0.3.1):
// ya no está en la instantánea, pero órdenes y tickets que lo referencian lo muestran «eliminado
// en Argos». Código y plan son la última copia que tenía Iris (vacíos si nunca la tuvo).
type ServicioEliminado struct {
	ID          int64     `json:"id" doc:"Id del servicio en Argos (ya no existe allá)"`
	EmpresaID   *int64    `json:"empresaId" nullable:"true"`
	ClienteID   *int64    `json:"clienteId" nullable:"true"`
	Codigo      string    `json:"codigo" doc:"Último código conocido; vacío si Iris no tenía la instantánea"`
	PlanNombre  string    `json:"planNombre" doc:"Último plan conocido; vacío si Iris no tenía la instantánea"`
	EliminadoEn time.Time `json:"eliminadoEn" doc:"Cuándo lo eliminó Argos (ocurrido_en del webhook)"`
}

// Ficha de un cliente: datos, servicios y estado de cuenta.
type Ficha struct {
	Cliente   ClienteResumen `json:"cliente"`
	Servicios []Servicio     `json:"servicios"`
	// ServiciosEliminados: los que Argos eliminó; un ticket u orden con uno de estos servicioId lo
	// muestra «eliminado en Argos».
	ServiciosEliminados []ServicioEliminado `json:"serviciosEliminados" required:"false" doc:"Servicios del cliente que Argos eliminó (servicio.eliminado, contrato 0.3.1): ya no están en servicios. Un ticket u orden cuyo servicioId está aquí lo muestra «eliminado en Argos». Filtrados por la empresa activa, como servicios"`
	EstadoCuenta        *EstadoCuenta       `json:"estadoCuenta,omitempty" doc:"Ausente si no se pudo leer (ver estadoCuentaError); nunca se muestran totales calculados sobre datos que no se leyeron"`
	// EstadoCuentaError explica por qué no hay estado de cuenta.
	EstadoCuentaError *string   `json:"estadoCuentaError" nullable:"true" enum:"sin_permiso,argos_no_disponible,argos_rechazo" doc:"sin_permiso: falta clientes.estado_cuenta; argos_no_disponible: Argos no respondió; argos_rechazo: Argos lo negó"`
	Stale             bool      `json:"stale" doc:"true: Argos no respondió y se muestra la última instantánea"`
	SincronizadoEn    time.Time `json:"sincronizadoEn" doc:"Cuándo se leyó de Argos esta instantánea"`
	// RefrescoOmitido: con ?refrescar=true, por qué no se llamó a Argos.
	RefrescoOmitido *string `json:"refrescoOmitido,omitempty" enum:"cuota,reciente,en_espera" doc:"Solo con ?refrescar=true cuando no se llamó a Argos: cuota (se agotó la cuota de lecturas a pedido del usuario o del conector; intenta en un minuto), reciente (se leyó hace menos de 30 s) o en_espera (Argos falló hace menos de un minuto). La ficha es la instantánea"`
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

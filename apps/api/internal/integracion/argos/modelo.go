package argos

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"

	argosbd "github.com/HCDestroyer/iris/apps/api/internal/integracion/argos/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
)

// Tipos que expone la API de Iris (camelCase). El OpenAPI se genera de aquí.

// Conector con Argos u Horus. Nunca lleva el token ni el secreto del webhook.
type Conector struct {
	ID              int64      `json:"id"`
	Sistema         string     `json:"sistema" enum:"argosisp,horus"`
	BaseURL         string     `json:"baseUrl" doc:"Origen del otro sistema"`
	Origen          string     `json:"origen" enum:"enlace_remoto,enlace_local" doc:"enlace_remoto: el enlace lo generó el otro sistema y lo canjeó Iris; enlace_local: lo generó Iris y lo canjeó el otro"`
	Estado          string     `json:"estado" enum:"apagado,activo"`
	RemotoID        *string    `json:"remotoId" nullable:"true" doc:"Id de la conexión en el otro sistema, si lo informó"`
	Empresas        []int64    `json:"empresas" doc:"Empresas de Argos que cubre el token (vacío si Argos no lo informó)"`
	WebhookURL      string     `json:"webhookUrl" doc:"URL a la que el otro sistema envía los webhooks"`
	ActivadoEn      *time.Time `json:"activadoEn" nullable:"true"`
	UltimoWebhookEn *time.Time `json:"ultimoWebhookEn" nullable:"true"`
	Version         int32      `json:"version"`
	CreadoEn        time.Time  `json:"creadoEn"`
	ActualizadoEn   time.Time  `json:"actualizadoEn"`
}

func (m *Modulo) aConector(f argosbd.IntegracionConectore) Conector {
	empresas := f.Empresas
	if empresas == nil {
		empresas = []int64{}
	}
	return Conector{
		ID: f.ID, Sistema: f.Sistema, BaseURL: f.BaseUrl, Origen: f.Origen, Estado: f.Estado,
		RemotoID: f.RemotoID, Empresas: empresas, WebhookURL: m.urlWebhook(f.ID), ActivadoEn: f.ActivadoEn,
		UltimoWebhookEn: f.UltimoWebhookEn, Version: f.Version, CreadoEn: f.CreatedAt, ActualizadoEn: f.UpdatedAt,
	}
}

func (m *Modulo) urlWebhook(id int64) string {
	return fmt.Sprintf("%s/api/v1/integraciones/argosisp/webhook/%d", m.cfg.BaseURL, id)
}

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

// Conector leído y descifrado (solo dentro del proceso; nunca se serializa).
type conectorLeido struct {
	argosbd.IntegracionConectore
	token   string
	secreto string
}

func datosAsociados(id int64, campo string) []byte {
	return fmt.Appendf(nil, "conector:%d:%s", id, campo)
}

func (m *Modulo) cifrar(id int64, campo, valor string) ([]byte, error) {
	if m.cfg.Cifrador == nil {
		return nil, sinCifrado()
	}
	return m.cfg.Cifrador.Cifrar([]byte(valor), datosAsociados(id, campo))
}

func (m *Modulo) leerConector(f argosbd.IntegracionConectore) (conectorLeido, error) {
	if m.cfg.Cifrador == nil {
		return conectorLeido{}, sinCifrado()
	}
	token, err := m.cfg.Cifrador.Descifrar(f.TokenCifrado, datosAsociados(f.ID, "token"))
	if err != nil {
		return conectorLeido{}, fmt.Errorf("conector %d: descifrar token: %w", f.ID, err)
	}
	secreto, err := m.cfg.Cifrador.Descifrar(f.SecretoWebhookCifrado, datosAsociados(f.ID, "secreto_webhook"))
	if err != nil {
		return conectorLeido{}, fmt.Errorf("conector %d: descifrar secreto: %w", f.ID, err)
	}
	return conectorLeido{IntegracionConectore: f, token: string(token), secreto: string(secreto)}, nil
}

// conectorActivo devuelve el conector Argos activo o 503 conector_apagado.
func (m *Modulo) conectorActivo(ctx context.Context, q *argosbd.Queries) (conectorLeido, error) {
	f, err := q.ConectorActivo(ctx, Sistema)
	if errors.Is(err, pgx.ErrNoRows) {
		return conectorLeido{}, ConectorApagado()
	}
	if err != nil {
		return conectorLeido{}, err
	}
	return m.leerConector(f)
}

// ConectorApagado: no hay conector Argos activo; el panel trabaja con datos locales.
func ConectorApagado() *nucleo.Problema {
	return nucleo.NuevoProblema(http.StatusServiceUnavailable, "conector_apagado", "Conector con Argos apagado",
		"No hay un conector con ArgosISP activo; trabaja con los datos capturados en Iris")
}

func sinCifrado() *nucleo.Problema {
	return nucleo.ReglaNegocio("cifrado_no_disponible", "Los conectores necesitan IRIS_CLAVE_CIFRADO para guardar el token cifrado")
}

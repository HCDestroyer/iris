// Package mensajeria es el servicio de mensajería de Iris (contrato Iris ↔ ArgosISP 0.6.0 §6,
// fase 1): avisos con plantillas aprobadas, permisos de contacto, reglas contra spam y webhooks de
// entrega hacia Argos. Corre en su propio proceso (cmd/mensajeria) con su schema `mensajeria`; la
// API reenvía /api/v1/mensajeria a ese proceso. Ver README.md.
package mensajeria

import (
	"context"
	"fmt"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/conector"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
)

// Cola de River de la mensajería: la trabaja solo el proceso de mensajería.
const Cola = "mensajeria"

// MetaURLPorDefecto es la Graph API de Meta (WhatsApp Cloud API).
const MetaURLPorDefecto = "https://graph.facebook.com/v21.0"

// Contactos da el contacto de un cliente de Argos desde la instantánea (§2.2), refrescada si tiene
// más de 24 h. La implementa argos.Modulo.ContactoCliente.
type Contactos interface {
	ContactoCliente(ctx context.Context, clienteID int64) (argos.ContactoArgos, error)
}

// Ventana es un horario diario en minutos desde la medianoche local; Inicio > Fin cruza la
// medianoche (21:00–07:00).
type Ventana struct {
	Inicio, Fin int
}

// Config del módulo.
type Config struct {
	// Cifrador de las credenciales de los canales y del secreto del receptor de Argos. nil = no se
	// pueden configurar canales ni firmar webhooks.
	Cifrador *nucleo.Cifrador
	// Contactos de Argos (nil = sin Argos: ningún cliente tiene contacto).
	Contactos Contactos
	// Encolar jobs de River en la transacción del caso de uso (nil = no se encola: OpenAPI).
	Encolar conector.Encolador
	// Simulado: las empresas sin canal configurado usan el canal simulado (desarrollo y CI).
	// Nunca en producción (IRIS_MENSAJERIA_SIMULADO): sin canal, el aviso es bloqueado/canal_no_disponible.
	Simulado bool
	// LimiteDiario de avisos automáticos por cliente y día (3, §6.5).
	LimiteDiario int
	// PorMinuto: envíos por minuto por número remitente (20, §6.5).
	PorMinuto int
	// Silencio: horario sin envíos (21:00–07:00, hora de Guatemala).
	Silencio Ventana
	// Zona de los días y el silencio (America/Guatemala).
	Zona *time.Location
	// MetaURL de la Graph API (pruebas: un servidor falso).
	MetaURL string
	// HTTPMeta hacia Meta; HTTPArgos hacia el receptor de Argos (con la protección contra SSRF).
	HTTPMeta  *http.Client
	HTTPArgos *http.Client
	// SinConfirmar: un aviso en cola sin acuse del canal después de esto falla con agotado (72 h).
	SinConfirmar time.Duration
	// Ahora (pruebas).
	Ahora func() time.Time
	// Version de Iris (User-Agent).
	Version string
}

// Modulo reúne los casos de uso.
type Modulo struct {
	cfg Config

	EnviarAviso             *EnviarAviso
	VerAviso                *auditoria.Caso[EntradaVerAviso, AvisoDetalle]
	RegistrarConsentimiento *RegistrarConsentimiento
	ListarPlantillas        *auditoria.Caso[EntradaPlantillas, ListaPlantillas]
	CrearCanal              *auditoria.Caso[EntradaCrearCanal, Canal]
	ActualizarCanal         *auditoria.Caso[EntradaActualizarCanal, Canal]
	ActivarCanal            *auditoria.Caso[EntradaCambioCanal, Canal]
	DesactivarCanal         *auditoria.Caso[EntradaCambioCanal, Canal]
	ListarCanales           *auditoria.Caso[struct{}, ListaCanales]
	RecibirMeta             *auditoria.Caso[EntradaMeta, MetaRecibido]
	VerificarMeta           *auditoria.Caso[EntradaVerificarMeta, string]

	prepararEnvio   *auditoria.Caso[entradaPreparar, preparado]
	registrarEnvio  *auditoria.Caso[entradaRegistrar, registrado]
	procesarMeta    *auditoria.Caso[int64, struct{}]
	vencerAvisos    *auditoria.Caso[struct{}, int]
	entregarWebhook *auditoria.Caso[entradaEntregar, struct{}]
}

// Nuevo arma el módulo con los valores por defecto.
func Nuevo(cfg Config) *Modulo {
	if cfg.LimiteDiario <= 0 {
		cfg.LimiteDiario = 3
	}
	if cfg.PorMinuto <= 0 {
		cfg.PorMinuto = 20
	}
	if cfg.Silencio == (Ventana{}) {
		cfg.Silencio = Ventana{Inicio: 21 * 60, Fin: 7 * 60}
	}
	if cfg.Zona == nil {
		zona, err := time.LoadLocation("America/Guatemala")
		if err != nil {
			zona = time.FixedZone("America/Guatemala", -6*3600)
		}
		cfg.Zona = zona
	}
	if cfg.MetaURL == "" {
		cfg.MetaURL = MetaURLPorDefecto
	}
	cfg.MetaURL = strings.TrimRight(cfg.MetaURL, "/")
	if cfg.HTTPMeta == nil {
		cfg.HTTPMeta = &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	if cfg.HTTPArgos == nil {
		cfg.HTTPArgos = conector.NuevoHTTP(nil)
	}
	if cfg.SinConfirmar <= 0 {
		cfg.SinConfirmar = 72 * time.Hour
	}
	if cfg.Ahora == nil {
		cfg.Ahora = time.Now
	}
	m := &Modulo{cfg: cfg}
	m.EnviarAviso = nuevoEnviarAviso(m)
	m.VerAviso = nuevoVerAviso(m)
	m.RegistrarConsentimiento = nuevoRegistrarConsentimiento(m)
	m.ListarPlantillas = nuevoListarPlantillas(m)
	m.CrearCanal = nuevoCrearCanal(m)
	m.ActualizarCanal = nuevoActualizarCanal(m)
	m.ActivarCanal = nuevoCambioCanal(m, "mensajeria.activar_canal", true)
	m.DesactivarCanal = nuevoCambioCanal(m, "mensajeria.desactivar_canal", false)
	m.ListarCanales = nuevoListarCanales(m)
	m.RecibirMeta = nuevoRecibirMeta(m)
	m.VerificarMeta = nuevoVerificarMeta(m)
	m.prepararEnvio = nuevoPrepararEnvio(m)
	m.registrarEnvio = nuevoRegistrarEnvio(m)
	m.procesarMeta = nuevoProcesarMeta(m)
	m.vencerAvisos = nuevoVencerAvisos(m)
	m.entregarWebhook = nuevoEntregarWebhook(m)
	return m
}

// Config devuelve la configuración (con los valores por defecto).
func (m *Modulo) Config() Config { return m.cfg }

func (m *Modulo) ahora() time.Time { return m.cfg.Ahora().UTC() }

// encolar inserta un job en la transacción (no hace nada sin Encolar).
func (m *Modulo) encolar(ctx context.Context, tx pgx.Tx, args river.JobArgs) error {
	if m.cfg.Encolar == nil {
		return nil
	}
	return m.cfg.Encolar(ctx, tx, args)
}

// Entorno ------------------------------------------------------------------------------------

// Entorno es la configuración del proceso de mensajería que viene de IRIS_MENSAJERIA_*.
type Entorno struct {
	// Puerto HTTP del proceso (IRIS_MENSAJERIA_PUERTO, 3510).
	Puerto int
	// URL con que la API reenvía /api/v1/mensajeria (IRIS_MENSAJERIA_URL; vacía = 503).
	URL          string
	Simulado     bool
	LimiteDiario int
	PorMinuto    int
	Silencio     Ventana
	MetaURL      string
	// RedesArgos: redes internas permitidas hacia el receptor de Argos (IRIS_ARGOS_REDES_PERMITIDAS).
	RedesArgos []netip.Prefix
}

// EntornoDesdeVariables lee IRIS_MENSAJERIA_*:
//
//	IRIS_MENSAJERIA_PUERTO (3510), IRIS_MENSAJERIA_URL (la usa la API), IRIS_MENSAJERIA_SIMULADO
//	(false), IRIS_MENSAJERIA_LIMITE_DIARIO (3), IRIS_MENSAJERIA_POR_MINUTO (20),
//	IRIS_MENSAJERIA_SILENCIO (21:00-07:00), IRIS_MENSAJERIA_META_URL (Graph API v21.0).
func EntornoDesdeVariables() (Entorno, error) {
	e := Entorno{Puerto: 3510, LimiteDiario: 3, PorMinuto: 20, Silencio: Ventana{Inicio: 21 * 60, Fin: 7 * 60}, MetaURL: MetaURLPorDefecto,
		URL: strings.TrimSpace(os.Getenv("IRIS_MENSAJERIA_URL"))}
	var errs []string
	enteros := map[string]*int{"IRIS_MENSAJERIA_PUERTO": &e.Puerto, "IRIS_MENSAJERIA_LIMITE_DIARIO": &e.LimiteDiario, "IRIS_MENSAJERIA_POR_MINUTO": &e.PorMinuto}
	for nombre, destino := range enteros {
		if v := strings.TrimSpace(os.Getenv(nombre)); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil || n <= 0 || n > 65535 {
				errs = append(errs, nombre+" inválido: "+v)
				continue
			}
			*destino = n
		}
	}
	if v := strings.TrimSpace(os.Getenv("IRIS_MENSAJERIA_SIMULADO")); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			errs = append(errs, "IRIS_MENSAJERIA_SIMULADO inválido: "+v)
		}
		e.Simulado = b
	}
	if v := strings.TrimSpace(os.Getenv("IRIS_MENSAJERIA_SILENCIO")); v != "" {
		s, err := ParsearVentana(v)
		if err != nil {
			errs = append(errs, "IRIS_MENSAJERIA_SILENCIO inválido (HH:MM-HH:MM): "+v)
		}
		e.Silencio = s
	}
	if v := strings.TrimSpace(os.Getenv("IRIS_MENSAJERIA_META_URL")); v != "" {
		if !strings.HasPrefix(v, "https://") && !strings.HasPrefix(v, "http://") {
			errs = append(errs, "IRIS_MENSAJERIA_META_URL inválida: "+v)
		}
		e.MetaURL = v
	}
	if len(errs) > 0 {
		return e, fmt.Errorf("configuración de mensajería: %s", strings.Join(errs, "; "))
	}
	return e, nil
}

// ValidarSimulado impide el canal simulado en producción: con IRIS_BASE_URL https en un host que no
// es local (la señal de producción del instalador), el proceso no arranca con
// IRIS_MENSAJERIA_SIMULADO=true. Así un aviso de Argos nunca se da por entregado sin salir a WhatsApp.
func ValidarSimulado(simulado bool, baseURL string) error {
	if !simulado {
		return nil
	}
	u, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || u.Scheme != "https" {
		return nil
	}
	host := strings.ToLower(u.Hostname())
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return nil
	}
	if ip, err := netip.ParseAddr(host); err == nil && ip.IsLoopback() {
		return nil
	}
	return fmt.Errorf("IRIS_MENSAJERIA_SIMULADO=true no se permite en producción (IRIS_BASE_URL=%s): el canal simulado da por "+
		"entregados avisos que nunca salen a WhatsApp. Ponlo en false y configura el canal de cada empresa (POST /api/v1/mensajeria/canales)", u.Scheme+"://"+u.Host)
}

// ParsearVentana lee «HH:MM-HH:MM».
func ParsearVentana(s string) (Ventana, error) {
	a, b, ok := strings.Cut(strings.TrimSpace(s), "-")
	if !ok {
		return Ventana{}, fmt.Errorf("falta el guion")
	}
	ini, err1 := minutos(a)
	fin, err2 := minutos(b)
	if err1 != nil || err2 != nil || ini == fin {
		return Ventana{}, fmt.Errorf("horario inválido")
	}
	return Ventana{Inicio: ini, Fin: fin}, nil
}

func minutos(s string) (int, error) {
	t, err := time.Parse("15:04", strings.TrimSpace(s))
	if err != nil {
		return 0, err
	}
	return t.Hour()*60 + t.Minute(), nil
}

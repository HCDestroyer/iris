// Package notificaciones manda notificaciones push a la app de campo (Android) por Firebase Cloud
// Messaging HTTP v1, sin el servicio de push de Expo (contratos/push.md).
//
//   - API: registro de dispositivos (POST /dispositivos, quitar) y preferencias de la cuenta
//     (GET/POST /yo/notificaciones, con version y 409).
//   - Worker: un suscriptor del outbox convierte chat.mensaje_enviado, chat.mencion,
//     conversacion.mencion, conversacion.aviso_sin_atender, orden.asignada y orden.programada en
//     envíos (uno por evento y dispositivo: un evento repetido no manda dos veces) y encola un job
//     de River por envío. El job vuelve a comprobar todo con los permisos de ese momento, arma el
//     mensaje y llama a FCM; reintenta 429/5xx con espera exponencial y no reintenta 4xx.
//
// Sin IRIS_FCM_PROYECTO ni IRIS_FCM_CREDENCIALES el push queda apagado: la API funciona, los
// dispositivos y las preferencias se guardan y /salud dice «sin_configurar».
package notificaciones

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"time"
	_ "time/tzdata" // zonas horarias embebidas: la imagen distroless no trae /usr/share/zoneinfo

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad"
)

// ZonaPorDefecto de las preferencias (No molestar y «hoy»).
const ZonaPorDefecto = "America/Guatemala"

// Estados del push (GET /salud y GET /yo/notificaciones).
const (
	PushConfigurado   = "configurado"
	PushSinConfigurar = "sin_configurar"
	PushError         = "error"
)

// EstadoPush dice si el servidor puede mandar push.
type EstadoPush struct {
	Estado  string
	Detalle string // por qué está en error (sin secretos)
}

// Texto para mostrar («Notificaciones push: sin configurar»).
func (e EstadoPush) Texto() string {
	switch e.Estado {
	case PushConfigurado:
		return "Notificaciones push: activas"
	case PushError:
		return "Notificaciones push: error en la configuración"
	}
	return "Notificaciones push: sin configurar"
}

// Encolador inserta un job de River en la transacción del caso de uso.
type Encolador func(ctx context.Context, tx pgx.Tx, args river.JobArgs) error

// Config del módulo.
type Config struct {
	// FCM manda los mensajes (nil = push apagado: no se crean envíos).
	FCM Emisor
	// Estado del push para mostrar (si queda vacío: configurado con FCM, sin_configurar sin él).
	Estado EstadoPush
	// Encolar el job de envío en la transacción del suscriptor (nil = no se encola: la API).
	Encolar Encolador
	// Reloj (pruebas).
	Reloj func() time.Time
	// RetencionEnvios: cuánto se guardan los envíos (30 días).
	RetencionEnvios time.Duration
	Log             *slog.Logger
}

// Modulo reúne los casos de uso de notificaciones.
type Modulo struct {
	cfg Config

	RegistrarDispositivo *auditoria.Caso[EntradaRegistrarDispositivo, DispositivoRegistrado]
	ListarDispositivos   *auditoria.Caso[EntradaListarDispositivos, nucleo.Pagina[Dispositivo]]
	QuitarDispositivo    *auditoria.Caso[EntradaQuitarDispositivo, Dispositivo]
	VerPreferencias      *auditoria.Caso[struct{}, Preferencias]
	GuardarPreferencias  *auditoria.Caso[EntradaPreferencias, Preferencias]
	// Emisor (worker).
	PrepararEnvios *auditoria.Caso[EventoPush, int]
	PrepararEnvio  *auditoria.Caso[int64, *EnvioListo]
	CerrarEnvio    *auditoria.Caso[ResultadoEnvio, bool]
	PurgarCaducos  *auditoria.Caso[struct{}, ResultadoPurga]
}

// Nuevo arma el módulo.
func Nuevo(cfg Config) *Modulo {
	if cfg.Reloj == nil {
		cfg.Reloj = time.Now
	}
	if cfg.RetencionEnvios <= 0 {
		cfg.RetencionEnvios = 30 * 24 * time.Hour
	}
	if cfg.Log == nil {
		cfg.Log = slog.Default()
	}
	if cfg.Estado.Estado == "" {
		cfg.Estado.Estado = PushSinConfigurar
		if cfg.FCM != nil {
			cfg.Estado.Estado = PushConfigurado
		}
	}
	m := &Modulo{cfg: cfg}
	m.RegistrarDispositivo = m.nuevoRegistrarDispositivo()
	m.ListarDispositivos = m.nuevoListarDispositivos()
	m.QuitarDispositivo = m.nuevoQuitarDispositivo()
	m.VerPreferencias = m.nuevoVerPreferencias()
	m.GuardarPreferencias = m.nuevoGuardarPreferencias()
	m.PrepararEnvios = m.nuevoPrepararEnvios()
	m.PrepararEnvio = m.nuevoPrepararEnvio()
	m.CerrarEnvio = m.nuevoCerrarEnvio()
	m.PurgarCaducos = m.nuevoPurgarCaducos()
	return m
}

// Estado del push.
func (m *Modulo) Estado() EstadoPush { return m.cfg.Estado }

// Activo: hay a quién mandar los mensajes.
func (m *Modulo) Activo() bool { return m.cfg.FCM != nil }

func (m *Modulo) ahora() time.Time { return m.cfg.Reloj().UTC().Truncate(time.Microsecond) }

func init() {
	// Al cerrar sesión (DELETE /sesion) se quitan los dispositivos de esa sesión, en la misma
	// transacción.
	identidad.AlCerrarSesion(quitarDeSesion)
}

// ConfigDesdeEntorno lee IRIS_FCM_PROYECTO e IRIS_FCM_CREDENCIALES. Sin ninguna de las dos, el push
// queda sin configurar (nil, sin_configurar). Con una sola o con un JSON que no sirve, en error
// (nil, error con el motivo): la API sigue arrancando. El contenido del archivo nunca va al log.
func ConfigDesdeEntorno() (Emisor, EstadoPush) {
	proyecto := strings.TrimSpace(os.Getenv("IRIS_FCM_PROYECTO"))
	ruta := strings.TrimSpace(os.Getenv("IRIS_FCM_CREDENCIALES"))
	switch {
	case proyecto == "" && ruta == "":
		return nil, EstadoPush{Estado: PushSinConfigurar}
	case proyecto == "":
		return nil, EstadoPush{Estado: PushError, Detalle: "falta IRIS_FCM_PROYECTO"}
	case ruta == "":
		return nil, EstadoPush{Estado: PushError, Detalle: "falta IRIS_FCM_CREDENCIALES"}
	}
	cuenta, clave, err := LeerCuentaServicio(ruta)
	if err != nil {
		return nil, EstadoPush{Estado: PushError, Detalle: "IRIS_FCM_CREDENCIALES: " + err.Error()}
	}
	if cuenta.ProyectoID != "" && cuenta.ProyectoID != proyecto {
		// Con otro proyecto FCM respondería SENDER_ID_MISMATCH a todos los teléfonos: no se manda nada.
		return nil, EstadoPush{Estado: PushError, Detalle: "la cuenta de servicio es del proyecto " + cuenta.ProyectoID + ", no del de IRIS_FCM_PROYECTO"}
	}
	return NuevoClienteFCM(OpcionesFCM{Proyecto: proyecto, Cuenta: cuenta, Clave: clave}), EstadoPush{Estado: PushConfigurado}
}

// AvisarEstado deja en el log (al arrancar) cómo quedó el push.
func AvisarEstado(log *slog.Logger, e EstadoPush) {
	switch {
	case e.Estado == PushSinConfigurar:
		log.Info("notificaciones push sin configurar (IRIS_FCM_PROYECTO e IRIS_FCM_CREDENCIALES vacías): no se mandan push")
	case e.Estado == PushError:
		log.Error("notificaciones push apagadas: la configuración no sirve", "motivo", e.Detalle)
	case e.Detalle != "":
		log.Warn("notificaciones push", "aviso", e.Detalle)
	}
}

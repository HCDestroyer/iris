// Package soporte: tickets con SLA por categoría y prioridad (calendario laboral y feriados de
// Guatemala), conversación con notas internas, historial y cron de SLA.
//
// Cada acción es un caso de uso de internal/auditoria (transacción, permiso, auditoría y
// outbox); los handlers (rutas.go) y el trabajo de River (trabajo_sla.go) solo validan y
// llaman. Las transiciones de estado son acciones explícitas (POST /tickets/{id}/<acción>).
package soporte

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/soporte/correo"
)

// Tablas operativas con empresa_id que se adoptan (POST /empresas/{id}/adoptar-sin-empresa)
// al pasar de «sin conector» a empresas registradas. Los mensajes y el historial copian la
// empresa de su ticket, así que se adoptan con él. Las categorías, SLA y calendarios no: en
// ellos empresa_id null significa «de todas las empresas», no «sin empresa».
func init() {
	nucleo.RegistrarTablaConEmpresa("soporte.tickets")
	nucleo.RegistrarTablaConEmpresa("soporte.ticket_mensajes")
	nucleo.RegistrarTablaConEmpresa("soporte.ticket_eventos")
	nucleo.RegistrarTablaConEmpresa("soporte.buzones")
	nucleo.RegistrarTablaConEmpresa("soporte.correos_entrantes")
	nucleo.RegistrarTablaConEmpresa("soporte.correos_salientes")
}

// Config del módulo.
type Config struct {
	// Reloj da la hora actual (time.Now por defecto). Las pruebas lo fijan.
	Reloj func() time.Time
	// Cifrador (IRIS_CLAVE_CIFRADO) cifra las contraseñas de los buzones. Sin él no se pueden
	// crear ni activar buzones (422 cifrado_no_configurado).
	Cifrador *nucleo.Cifrador
	// Correo: red (SSRF, CA, tiempo) y límites de la entrada por correo.
	Correo ConfigCorreo
	// Encolar inserta un job de River en la transacción del caso de uso (en cmd/worker,
	// river.Client.InsertTx). nil = no se encola (API sin worker, OpenAPI, pruebas).
	Encolar func(ctx context.Context, tx pgx.Tx, args river.JobArgs) error
}

// ConfigCorreo de la entrada y salida por correo.
type ConfigCorreo struct {
	Red correo.Opciones
	// TamanoMax (IRIS_CORREO_TAMANO_MAX, bytes): un correo más grande solo se importa con sus
	// cabeceras (10 MiB por defecto).
	TamanoMax int64
}

func (c ConfigCorreo) tamanoMax() int64 {
	if c.TamanoMax <= 0 {
		return 10 << 20
	}
	return c.TamanoMax
}

// Modulo reúne los casos de uso de soporte.
type Modulo struct {
	cfg Config

	CrearTicket         *auditoria.Caso[EntradaCrearTicket, Ticket]
	AsignarTicket       *auditoria.Caso[EntradaAsignarTicket, Ticket]
	ResponderTicket     *auditoria.Caso[EntradaResponderTicket, TicketConMensaje]
	AgregarNotaInterna  *auditoria.Caso[EntradaNotaInterna, TicketConMensaje]
	EscalarTicket       *auditoria.Caso[EntradaEscalarTicket, Ticket]
	PonerTicketEnEspera *auditoria.Caso[EntradaPonerEnEspera, Ticket]
	ReanudarTicket      *auditoria.Caso[EntradaReanudarTicket, Ticket]
	ResolverTicket      *auditoria.Caso[EntradaResolverTicket, Ticket]
	ReabrirTicket       *auditoria.Caso[EntradaReabrirTicket, Ticket]
	CerrarTicket        *auditoria.Caso[EntradaCerrarTicket, Ticket]
	FusionarTickets     *auditoria.Caso[EntradaFusionarTickets, ResultadoFusion]

	VerTicket        *auditoria.Caso[EntradaVerTicket, Ticket]
	ListarTickets    *auditoria.Caso[EntradaListarTickets, nucleo.Pagina[Ticket]]
	ListarMensajes   *auditoria.Caso[EntradaListarMensajes, nucleo.Pagina[Mensaje]]
	ListarEventos    *auditoria.Caso[EntradaListarEventos, nucleo.Pagina[EventoTicket]]
	ListarCategorias *auditoria.Caso[EntradaListarCategorias, nucleo.Pagina[Categoria]]
	ListarSLAs       *auditoria.Caso[struct{}, nucleo.Pagina[SLA]]

	RevisarSLA *auditoria.Caso[EntradaRevisarSLA, ResultadoRevisarSLA]

	CrearBuzon      *auditoria.Caso[EntradaCrearBuzon, Buzon]
	EditarBuzon     *auditoria.Caso[EntradaEditarBuzon, Buzon]
	ActivarBuzon    *auditoria.Caso[EntradaCambiarBuzon, Buzon]
	DesactivarBuzon *auditoria.Caso[EntradaCambiarBuzon, Buzon]
	VerBuzon        *auditoria.Caso[EntradaVerBuzon, Buzon]
	ListarBuzones   *auditoria.Caso[EntradaListarBuzones, nucleo.Pagina[Buzon]]
	ProbarBuzon     *auditoria.Caso[EntradaVerBuzon, ResultadoPrueba]

	// Internos del worker (actor sistema).
	ProgramarLecturas *auditoria.Caso[EntradaProgramarLecturas, int]
	RegistrarCorreo   *auditoria.Caso[EntradaRegistrarCorreo, ResultadoRegistrarCorreo]
	CerrarLectura     *auditoria.Caso[EntradaCerrarLectura, struct{}]
	ProcesarCorreo    *auditoria.Caso[EntradaProcesarCorreo, ResultadoProcesarCorreo]
	EncolarRespuesta  *auditoria.Caso[EntradaEncolarRespuesta, ResultadoEncolarRespuesta]
	RegistrarEnvio    *auditoria.Caso[EntradaRegistrarEnvio, struct{}]
}

// Nuevo arma el módulo.
func Nuevo(cfg Config) *Modulo {
	if cfg.Reloj == nil {
		cfg.Reloj = time.Now
	}
	m := &Modulo{cfg: cfg}
	m.CrearTicket = m.nuevoCrearTicket()
	m.AsignarTicket = m.nuevoAsignarTicket()
	m.ResponderTicket = m.nuevoResponderTicket()
	m.AgregarNotaInterna = m.nuevoAgregarNotaInterna()
	m.EscalarTicket = m.nuevoEscalarTicket()
	m.PonerTicketEnEspera = m.nuevoPonerTicketEnEspera()
	m.ReanudarTicket = m.nuevoReanudarTicket()
	m.ResolverTicket = m.nuevoResolverTicket()
	m.ReabrirTicket = m.nuevoReabrirTicket()
	m.CerrarTicket = m.nuevoCerrarTicket()
	m.FusionarTickets = m.nuevoFusionarTickets()
	m.VerTicket = m.nuevoVerTicket()
	m.ListarTickets = m.nuevoListarTickets()
	m.ListarMensajes = m.nuevoListarMensajes()
	m.ListarEventos = m.nuevoListarEventos()
	m.ListarCategorias = nuevoListarCategorias()
	m.ListarSLAs = nuevoListarSLAs()
	m.RevisarSLA = m.nuevoRevisarSLA()
	m.CrearBuzon = m.nuevoCrearBuzon()
	m.EditarBuzon = m.nuevoEditarBuzon()
	m.ActivarBuzon = m.nuevoCambiarBuzon(true)
	m.DesactivarBuzon = m.nuevoCambiarBuzon(false)
	m.VerBuzon = m.nuevoVerBuzon()
	m.ListarBuzones = m.nuevoListarBuzones()
	m.ProbarBuzon = m.nuevoProbarBuzon()
	m.ProgramarLecturas = m.nuevoProgramarLecturas()
	m.RegistrarCorreo = m.nuevoRegistrarCorreo()
	m.CerrarLectura = m.nuevoCerrarLectura()
	m.ProcesarCorreo = m.nuevoProcesarCorreo()
	m.EncolarRespuesta = m.nuevoEncolarRespuesta()
	m.RegistrarEnvio = m.nuevoRegistrarEnvio()
	return m
}

// ahora es la hora del módulo en UTC, truncada a microsegundos (la precisión de Postgres).
func (m *Modulo) ahora() time.Time { return m.cfg.Reloj().UTC().Truncate(time.Microsecond) }

// soloLecturas: las consultas no dejan fila de auditoría al terminar bien (sí al fallar).
func soloLecturas[Out any](Out) bool { return false }

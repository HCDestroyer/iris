// Package soporte: tickets con SLA por categoría y prioridad (calendario laboral y feriados de
// Guatemala), conversación con notas internas, historial y cron de SLA.
//
// Cada acción es un caso de uso de internal/auditoria (transacción, permiso, auditoría y
// outbox); los handlers (rutas.go) y el trabajo de River (trabajo_sla.go) solo validan y
// llaman. Las transiciones de estado son acciones explícitas (POST /tickets/{id}/<acción>).
package soporte

import (
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
)

// Tablas operativas con empresa_id que se adoptan (POST /empresas/{id}/adoptar-sin-empresa)
// al pasar de «sin conector» a empresas registradas. Los mensajes y el historial copian la
// empresa de su ticket, así que se adoptan con él. Las categorías, SLA y calendarios no: en
// ellos empresa_id null significa «de todas las empresas», no «sin empresa».
func init() {
	nucleo.RegistrarTablaConEmpresa("soporte.tickets")
	nucleo.RegistrarTablaConEmpresa("soporte.ticket_mensajes")
	nucleo.RegistrarTablaConEmpresa("soporte.ticket_eventos")
}

// Config del módulo.
type Config struct {
	// Reloj da la hora actual (time.Now por defecto). Las pruebas lo fijan.
	Reloj func() time.Time
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
	return m
}

// ahora es la hora del módulo en UTC, truncada a microsegundos (la precisión de Postgres).
func (m *Modulo) ahora() time.Time { return m.cfg.Reloj().UTC().Truncate(time.Microsecond) }

// soloLecturas: las consultas no dejan fila de auditoría al terminar bien (sí al fallar).
func soloLecturas[Out any](Out) bool { return false }

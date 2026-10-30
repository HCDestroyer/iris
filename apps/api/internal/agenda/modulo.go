// Package agenda: zonas (espejo de Argos más las locales, con polígono propio), cuadrillas,
// técnicos (turnos semanales, ausencias y capacidad diaria por tipo de orden) y las citas de las
// órdenes programadas, con la ventana acordada con el cliente.
//
// Campo delega en agenda la programación de una orden (ordenes.programar, ordenes.reprogramar y
// el cambio de técnico de ordenes.asignar): ReservarCita valida turno, ausencia, solape y
// capacidad (422 con el motivo), guarda la cita y emite orden.programada en la transacción de la
// acción. Agenda no importa campo ni soporte: lee sus tablas por SQL y les ofrece la validación
// de zonas y cuadrillas (ValidarZona, ZonaDeArgos, ValidarCuadrilla).
package agenda

import (
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
)

// Tablas operativas con empresa_id que se adoptan al pasar de «sin conector» a empresas
// registradas (las zonas espejo de Argos ya traen su empresa).
func init() {
	nucleo.RegistrarTablaConEmpresa("agenda.zonas")
	nucleo.RegistrarTablaConEmpresa("agenda.cuadrillas")
	nucleo.RegistrarTablaConEmpresa("agenda.citas")
	nucleo.RegistrarTablaConEmpresa("agenda.rutas")
}

// Config del módulo.
type Config struct {
	// Reloj da la hora actual (time.Now por defecto).
	Reloj func() time.Time
	// ZonaHoraria de los turnos y del día de la capacidad (America/Guatemala por defecto).
	ZonaHoraria *time.Location
	// Enrutador da tiempos, distancias y trazos por calles (OSRM, OSRMDesdeEntorno). nil = sin
	// OSRM: las rutas se proponen en línea recta (haversine) y lo dicen (degradada).
	Enrutador Enrutador
}

// Modulo reúne los casos de uso de agenda.
type Modulo struct {
	cfg Config

	ListarZonas      *auditoria.Caso[EntradaListarCatalogo, nucleo.Pagina[Zona]]
	CrearZona        *auditoria.Caso[EntradaCrearZona, Zona]
	EditarZona       *auditoria.Caso[EntradaEditarZona, Zona]
	ActivarZona      *auditoria.Caso[EntradaVersion, Zona]
	DesactivarZona   *auditoria.Caso[EntradaVersion, Zona]
	SincronizarZonas *auditoria.Caso[struct{}, ResultadoEspejo]

	ListarCuadrillas    *auditoria.Caso[EntradaListarCatalogo, nucleo.Pagina[Cuadrilla]]
	CrearCuadrilla      *auditoria.Caso[EntradaCrearCuadrilla, Cuadrilla]
	EditarCuadrilla     *auditoria.Caso[EntradaEditarCuadrilla, Cuadrilla]
	ActivarCuadrilla    *auditoria.Caso[EntradaVersion, Cuadrilla]
	DesactivarCuadrilla *auditoria.Caso[EntradaVersion, Cuadrilla]
	CapacidadCuadrilla  *auditoria.Caso[EntradaFijarCapacidad, Cuadrilla]

	ListarTecnicos    *auditoria.Caso[EntradaListarTecnicos, nucleo.Pagina[Tecnico]]
	VerTecnico        *auditoria.Caso[EntradaVerTecnico, TecnicoDetalle]
	CandidatosTecnico *auditoria.Caso[EntradaCandidatosTecnico, nucleo.Pagina[CandidatoTecnico]]
	CrearTecnico      *auditoria.Caso[EntradaCrearTecnico, TecnicoDetalle]
	EditarTecnico     *auditoria.Caso[EntradaEditarTecnico, TecnicoDetalle]
	ActivarTecnico    *auditoria.Caso[EntradaVersion, TecnicoDetalle]
	DesactivarTecnico *auditoria.Caso[EntradaVersion, TecnicoDetalle]
	FijarTurnos       *auditoria.Caso[EntradaFijarTurnos, TecnicoDetalle]
	CapacidadTecnico  *auditoria.Caso[EntradaFijarCapacidad, TecnicoDetalle]
	RegistrarAusencia *auditoria.Caso[EntradaRegistrarAusencia, RespuestaAusencia]
	AnularAusencia    *auditoria.Caso[EntradaAnularAusencia, Ausencia]

	Asignables *auditoria.Caso[EntradaAsignables, RespuestaAsignables]
	Calendario *auditoria.Caso[EntradaCalendario, Calendario]

	// Rutas del día (ruta_casos.go). Proponer se llama con ProponerRuta (OSRM fuera de la
	// transacción).
	Proponer    *auditoria.Caso[EntradaProponerRuta, RutaDetalle]
	Publicar    *auditoria.Caso[EntradaPublicarRuta, RutaDetalle]
	VerRuta     *auditoria.Caso[EntradaVerRuta, RutaDetalle]
	ListarRutas *auditoria.Caso[EntradaListarRutas, nucleo.Pagina[Ruta]]
}

// Nuevo arma el módulo.
func Nuevo(cfg Config) *Modulo {
	if cfg.Reloj == nil {
		cfg.Reloj = time.Now
	}
	if cfg.ZonaHoraria == nil {
		loc, err := time.LoadLocation("America/Guatemala")
		if err != nil {
			panic("agenda: zona horaria America/Guatemala: " + err.Error())
		}
		cfg.ZonaHoraria = loc
	}
	m := &Modulo{cfg: cfg}
	m.ListarZonas = m.nuevoListarZonas()
	m.CrearZona = m.nuevoCrearZona()
	m.EditarZona = m.nuevoEditarZona()
	m.ActivarZona = m.nuevoActivarZona(true)
	m.DesactivarZona = m.nuevoActivarZona(false)
	m.SincronizarZonas = m.nuevoSincronizarZonas()
	m.ListarCuadrillas = m.nuevoListarCuadrillas()
	m.CrearCuadrilla = m.nuevoCrearCuadrilla()
	m.EditarCuadrilla = m.nuevoEditarCuadrilla()
	m.ActivarCuadrilla = m.nuevoActivarCuadrilla(true)
	m.DesactivarCuadrilla = m.nuevoActivarCuadrilla(false)
	m.CapacidadCuadrilla = m.nuevoCapacidadCuadrilla()
	m.ListarTecnicos = m.nuevoListarTecnicos()
	m.VerTecnico = m.nuevoVerTecnico()
	m.CandidatosTecnico = m.nuevoCandidatosTecnico()
	m.CrearTecnico = m.nuevoCrearTecnico()
	m.EditarTecnico = m.nuevoEditarTecnico()
	m.ActivarTecnico = m.nuevoActivarTecnico(true)
	m.DesactivarTecnico = m.nuevoActivarTecnico(false)
	m.FijarTurnos = m.nuevoFijarTurnos()
	m.CapacidadTecnico = m.nuevoCapacidadTecnico()
	m.RegistrarAusencia = m.nuevoRegistrarAusencia()
	m.AnularAusencia = m.nuevoAnularAusencia()
	m.Asignables = m.nuevoAsignables()
	m.Calendario = m.nuevoCalendario()
	m.Proponer = m.nuevoProponerRuta()
	m.Publicar = m.nuevoPublicarRuta()
	m.VerRuta = m.nuevoVerRuta()
	m.ListarRutas = m.nuevoListarRutas()
	return m
}

// ahora es la hora del módulo en UTC, truncada a microsegundos (la precisión de Postgres).
func (m *Modulo) ahora() time.Time { return m.cfg.Reloj().UTC().Truncate(time.Microsecond) }

// Zona horaria de los turnos.
func (m *Modulo) ZonaHoraria() *time.Location { return m.cfg.ZonaHoraria }

// soloLecturas: las consultas no dejan fila de auditoría al terminar bien (sí al fallar).
func soloLecturas[Out any](Out) bool { return false }

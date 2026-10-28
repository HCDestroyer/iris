// Package comercial es el seguimiento comercial (Fase 4): prospectos con su origen y actividades,
// factibilidad (PlantaExterna: por ahora la distancia al router más cercano de la instantánea de
// Argos; la fase 6 la reemplaza por OZmap), cotización con planes de Argos y PDF, contrato con
// la firma del cliente y PDF firmado, y ganar: alta del cliente y del servicio en Argos por la
// cola de pendientes (contratos/argosisp.md §3) y la orden de instalación.
//
// Cada acción es un caso de uso de internal/auditoria (permiso con el nombre de la acción,
// auditoría y outbox en la misma transacción). Lo que habla con otro sistema o guarda un PDF
// (Argos, adjuntos) corre fuera de la transacción: el método del módulo hace esa fase y llama al
// caso de uso (como agenda.ProponerRuta).
package comercial

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/campo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
)

// Tablas operativas con empresa_id que se adoptan al pasar de «sin conector» a empresas
// registradas (POST /empresas/{id}/adoptar-sin-empresa).
func init() {
	nucleo.RegistrarTablaConEmpresa("comercial.prospectos")
	nucleo.RegistrarTablaConEmpresa("comercial.actividades")
	nucleo.RegistrarTablaConEmpresa("comercial.factibilidades")
	nucleo.RegistrarTablaConEmpresa("comercial.cotizaciones")
	nucleo.RegistrarTablaConEmpresa("comercial.contratos")
}

// Encolador inserta un job de River en la transacción del caso de uso.
type Encolador func(ctx context.Context, tx pgx.Tx, args river.JobArgs) error

// Config del módulo.
type Config struct {
	// Reloj da la hora actual (time.Now por defecto).
	Reloj func() time.Time
	// ZonaHoraria para mostrar fechas en los PDF (America/Guatemala por defecto).
	ZonaHoraria *time.Location
	// Planta evalúa la factibilidad (PlantaPorDistancia por defecto; la fase 6 enchufa OZmap).
	Planta PlantaExterna
	// Planes lee los planes de Argos para cotizar (PlanesDeArgos). nil = sin conector: las
	// líneas de plan van con descripción y precio a mano solo en modo sin conector.
	Planes LectorPlanes
	// Altas recupera el id que Argos dio a un alta ya hecha por la cola de pendientes
	// (AltasDeArgos). nil = el alta en Argos no avanza (queda esperando).
	Altas RecuperadorAltas
	// Documentos guarda los PDF como adjuntos del núcleo (DocumentosDeAdjuntos). nil = no se
	// guardan (la cotización y el contrato salen con pdf null).
	Documentos Documentos
	// Campo crea la orden de instalación al ganar (campo.Nuevo). nil = no se crea (solo OpenAPI).
	Campo *campo.Modulo
	// Encolar avisa a la cola de pendientes (pendientes.ArgsProcesar) al encolar el alta. nil =
	// la toma el periódico de la cola (cada minuto).
	Encolar Encolador
	// VigenciaCotizacion: días que vale una cotización si no se indica (15).
	VigenciaCotizacion time.Duration
}

// Modulo reúne los casos de uso de comercial. Los que hablan con otro sistema o guardan un PDF
// son métodos (EvaluarFactibilidad, CrearCotizacion, CrearContrato, FirmarContrato, Planes,
// CompletarAlta): hacen esa fase fuera de la transacción y llaman a sus casos de uso.
type Modulo struct {
	cfg Config

	ListarProspectos *auditoria.Caso[EntradaListarProspectos, nucleo.Pagina[Prospecto]]
	VerProspecto     *auditoria.Caso[EntradaVerProspecto, ProspectoDetalle]
	CrearProspecto   *auditoria.Caso[EntradaCrearProspecto, Prospecto]
	EditarProspecto  *auditoria.Caso[EntradaEditarProspecto, Prospecto]
	AsignarProspecto *auditoria.Caso[EntradaAsignarProspecto, Prospecto]
	Contactar        *auditoria.Caso[EntradaTransicion, Prospecto]
	Perder           *auditoria.Caso[EntradaPerder, Prospecto]
	Reabrir          *auditoria.Caso[EntradaTransicion, Prospecto]
	EventosProspecto *auditoria.Caso[EntradaVerProspecto, []EventoProspecto]
	Ganar            *auditoria.Caso[EntradaGanar, Prospecto]

	RegistrarActividad *auditoria.Caso[EntradaActividad, Actividad]
	ListarActividades  *auditoria.Caso[EntradaListarActividades, nucleo.Pagina[Actividad]]

	VerUmbrales   *auditoria.Caso[struct{}, Umbrales]
	FijarUmbrales *auditoria.Caso[EntradaUmbrales, Umbrales]
	Cobertura     *auditoria.Caso[struct{}, Cobertura]

	AceptarCotizacion  *auditoria.Caso[EntradaAceptarCotizacion, Cotizacion]
	VerCotizacion      *auditoria.Caso[EntradaVerCotizacion, Cotizacion]
	ListarCotizaciones *auditoria.Caso[EntradaVerProspecto, nucleo.Pagina[Cotizacion]]
	VerContrato        *auditoria.Caso[EntradaVerContrato, Contrato]

	prepararFactibilidad *auditoria.Caso[EntradaFactibilidad, previoFactibilidad]
	guardarFactibilidad  *auditoria.Caso[entradaGuardarFactibilidad, Factibilidad]
	prepararCotizacion   *auditoria.Caso[EntradaCrearCotizacion, previoCotizacion]
	crearCotizacion      *auditoria.Caso[entradaCrearCotizacion, Cotizacion]
	datosPDFCotizacion   *auditoria.Caso[EntradaVerCotizacion, datosCotizacionPDF]
	verPlanes            *auditoria.Caso[struct{}, previoPlanes]
	crearContrato        *auditoria.Caso[EntradaCrearContrato, Contrato]
	datosPDFBorrador     *auditoria.Caso[EntradaVerContrato, datosContratoPDF]
	prepararFirma        *auditoria.Caso[EntradaFirmar, datosContratoPDF]
	firmar               *auditoria.Caso[entradaFirmaLista, Contrato]
	registrarCliente     *auditoria.Caso[entradaAltaArgos, Prospecto]
	completarAlta        *auditoria.Caso[entradaAltaArgos, Prospecto]
}

// Nuevo arma el módulo.
func Nuevo(cfg Config) *Modulo {
	if cfg.Reloj == nil {
		cfg.Reloj = time.Now
	}
	if cfg.ZonaHoraria == nil {
		loc, err := time.LoadLocation("America/Guatemala")
		if err != nil {
			panic("comercial: zona horaria America/Guatemala: " + err.Error())
		}
		cfg.ZonaHoraria = loc
	}
	if cfg.Planta == nil {
		cfg.Planta = PlantaPorDistancia{}
	}
	if cfg.VigenciaCotizacion <= 0 {
		cfg.VigenciaCotizacion = 15 * 24 * time.Hour
	}
	m := &Modulo{cfg: cfg}
	m.ListarProspectos = m.nuevoListarProspectos()
	m.VerProspecto = m.nuevoVerProspecto()
	m.CrearProspecto = m.nuevoCrearProspecto()
	m.EditarProspecto = m.nuevoEditarProspecto()
	m.AsignarProspecto = m.nuevoAsignarProspecto()
	m.Contactar = m.nuevoContactar()
	m.Perder = m.nuevoPerder()
	m.Reabrir = m.nuevoReabrir()
	m.EventosProspecto = m.nuevoEventosProspecto()
	m.Ganar = m.nuevoGanar()
	m.RegistrarActividad = m.nuevoRegistrarActividad()
	m.ListarActividades = m.nuevoListarActividades()
	m.VerUmbrales = m.nuevoVerUmbrales()
	m.FijarUmbrales = m.nuevoFijarUmbrales()
	m.Cobertura = m.nuevoCobertura()
	m.AceptarCotizacion = m.nuevoAceptarCotizacion()
	m.VerCotizacion = m.nuevoVerCotizacion()
	m.ListarCotizaciones = m.nuevoListarCotizaciones()
	m.VerContrato = m.nuevoVerContrato()
	m.prepararFactibilidad = m.nuevoPrepararFactibilidad()
	m.guardarFactibilidad = m.nuevoGuardarFactibilidad()
	m.prepararCotizacion = m.nuevoPrepararCotizacion()
	m.crearCotizacion = m.nuevoCrearCotizacion()
	m.datosPDFCotizacion = m.nuevoDatosPDFCotizacion()
	m.verPlanes = m.nuevoVerPlanes()
	m.crearContrato = m.nuevoCrearContratoCaso()
	m.datosPDFBorrador = m.nuevoDatosPDFBorrador()
	m.prepararFirma = m.nuevoPrepararFirma()
	m.firmar = m.nuevoFirmar()
	m.registrarCliente = m.nuevoRegistrarCliente()
	m.completarAlta = m.nuevoCompletarAlta()
	return m
}

// ahora es la hora del módulo en UTC, truncada a microsegundos (la precisión de Postgres).
func (m *Modulo) ahora() time.Time { return m.cfg.Reloj().UTC().Truncate(time.Microsecond) }

// soloLecturas: las consultas no dejan fila de auditoría al terminar bien (sí al fallar).
func soloLecturas[Out any](Out) bool { return false }

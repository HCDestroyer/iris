// Package campo: órdenes de trabajo (instalación, visita, retiro, cambio de equipo y
// mantenimiento) con su máquina de estados, checklist por plantilla, materiales, equipos, firma
// y adjuntos, y la API de sincronización de la app de campo (contratos/sync.md): paquete del
// día, cambios por cursor y lote de operaciones sin conexión.
//
// Cada acción es un caso de uso de internal/auditoria (transacción, permiso, auditoría y
// outbox); los handlers (rutas.go) solo validan y llaman, y el lote (sync_lote.go) llama a los
// mismos casos de uso, una operación por transacción. Las transiciones son acciones
// explícitas (POST /ordenes/{id}/<acción>). Campo no llama a Argos: al cerrar emite
// orden.cerrada y el núcleo (I1) encola los pendientes (contratos/argosisp.md §3).
package campo

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/HCDestroyer/iris/apps/api/internal/agenda"
	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/inventario"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/soporte"
)

// Tablas operativas con empresa_id que se adoptan al pasar de «sin conector» a empresas
// registradas. Lo demás del esquema cuelga de la orden (se filtra por ella).
func init() {
	nucleo.RegistrarTablaConEmpresa("campo.ordenes")
}

// Requisito de adjuntos para cerrar una orden: Minimo adjuntos completos del Tipo (foto,
// firma, documento) y, para las fotos del checklist, con su ClaveChecklist (el código del ítem).
// Misma forma que adjuntos.Requisito de I1 (internal/nucleo/adjuntos).
type Requisito struct {
	Tipo           string
	ClaveChecklist string
	Minimo         int
}

// Adjuntos es el enlace con el módulo de adjuntos de I1 (subida reanudable verificada por
// sha256). Lo conecta el líder al integrar las dos ramas (adaptador en cmd/iris):
//
//	Faltantes       → adjuntos.Faltantes(ctx, nucleo.TxDe(ctx), "orden", id, requisitos)
//	                  (solo cuenta adjuntos en estado completo; claves "firma", "foto:<item>")
//	AdjuntoDeOrden  → adjuntos.DeEntidad(ctx, nucleo.TxDe(ctx), adjuntoID, "orden", id): el
//	                  adjunto existe (basta con la subida creada) y es de esta orden
//
// ctx trae la transacción de la acción (nucleo.TxDe). Hasta entonces, SinAdjuntosObligatorios.
type Adjuntos interface {
	Faltantes(ctx context.Context, ordenID int64, requisitos []Requisito) (faltan []string, err error)
	AdjuntoDeOrden(ctx context.Context, adjuntoID uuid.UUID, ordenID int64) error
}

// SinAdjuntosObligatorios no exige subidas ni valida adjuntos. Solo para pruebas que lo pidan
// explícitamente (el valor por defecto es AdjuntosNucleo).
type SinAdjuntosObligatorios struct{}

// Faltantes implementa Adjuntos.
func (SinAdjuntosObligatorios) Faltantes(context.Context, int64, []Requisito) ([]string, error) {
	return nil, nil
}

// AdjuntoDeOrden implementa Adjuntos.
func (SinAdjuntosObligatorios) AdjuntoDeOrden(context.Context, uuid.UUID, int64) error { return nil }

// EquipoComodato es un equipo de Argos asignado en comodato al servicio.
type EquipoComodato struct {
	UnidadID int64   `json:"unidadId"`
	Serie    string  `json:"serie"`
	MAC      *string `json:"mac" nullable:"true"`
	Modelo   string  `json:"modelo"`
}

// Existencia de un producto en una bodega de Argos.
type Existencia struct {
	ProductoID  int64   `json:"productoId"`
	Descripcion string  `json:"descripcion"`
	Cantidad    float64 `json:"cantidad"`
	Unidad      string  `json:"unidad"`
	// Modalidad (Argos ≥ 1.6.0, contrato 0.5.0): un comodato se registra como equipo, no como material.
	Modalidad string `json:"modalidad,omitempty" enum:"comodato,consumible_gasto,consumible_cobro," doc:"Modalidad del producto en Argos (≥ 1.6.0): comodato (va como equipo, con serie), consumible_gasto o consumible_cobro (Argos lo cobra al cliente al consumirlo). Vacía con un Argos anterior"`
}

// Inventario lee de Argos los equipos en comodato de un servicio y las existencias de una
// bodega (GET /servicios/:id/equipos y /inventario/existencias?bodega_id=, contrato §2). Lo
// implementa el conector de Argos (I1). Se llama siempre fuera de una transacción. nil (sin
// conector) = el paquete los manda vacíos con disponible = false.
type Inventario interface {
	EquiposEnComodato(ctx context.Context, empresaID, servicioID int64) ([]EquipoComodato, error)
	Existencias(ctx context.Context, empresaID, bodegaID int64) ([]Existencia, error)
	// BodegaDeEmpresa: la bodega existe en la empresa (para que despacho use otra bodega).
	BodegaDeEmpresa(ctx context.Context, empresaID, bodegaID int64) (bool, error)
}

// Config del módulo.
type Config struct {
	// Reloj da la hora actual (time.Now por defecto). Las pruebas lo fijan.
	Reloj func() time.Time
	// AsegurarArgos (argos.Modulo.AsegurarInstantanea): antes de crear una orden con cliente y
	// servicio, los deja en la instantánea para validarlos. nil = no se verifica.
	AsegurarArgos argos.Asegurador
	// Encuestas genera la encuesta de satisfacción al cerrar con éxito una instalación o una
	// visita, en la transacción del cierre (soporte.Modulo.GenerarEncuestaOrden). nil = no se
	// generan (OpenAPI, pruebas).
	Encuestas func(ctx context.Context, t *auditoria.Tx, o soporte.EncuestaDeOrden) error
	// Adjuntos verifica los adjuntos obligatorios al cerrar (SinAdjuntosObligatorios por
	// defecto).
	Adjuntos Adjuntos
	// Inventario de Argos para el paquete y el aviso de existencia (nil = sin conector).
	Inventario Inventario
	// Recepciones de órdenes de traslado (traslado.recibir del lote y su estado en cambios;
	// inventario.Modulo de I1). nil = la operación se rechaza con conector_apagado.
	Recepciones Recepciones
	// ZonaHoraria para el día del paquete (America/Guatemala por defecto).
	ZonaHoraria *time.Location
	// Agenda valida y guarda la cita al programar, reprogramar o cambiar el técnico (turno,
	// ausencia, solape, capacidad) y emite orden.programada. Por defecto, una con el mismo reloj
	// y zona horaria.
	Agenda *agenda.Modulo
	// Posiciones: límites de la ingesta de ubicación en vivo (LimitesPosicionesDesdeEntorno).
	Posiciones LimitesPosiciones
	// Soporte: los casos de uso de tickets con que la orden iguala su asignación con la del
	// ticket (tickets.asignar, tickets.ver). Por defecto, uno con el mismo reloj.
	Soporte *soporte.Modulo
}

// Modulo reúne los casos de uso de campo.
type Modulo struct {
	cfg Config

	CrearOrden          *argos.ConInstantanea[EntradaCrearOrden, Orden]
	AsignarOrden        *auditoria.Caso[EntradaAsignarOrden, Orden]
	ProgramarOrden      *auditoria.Caso[EntradaProgramarOrden, Orden]
	ReprogramarOrden    *auditoria.Caso[EntradaProgramarOrden, Orden]
	IniciarCamino       *auditoria.Caso[EntradaPosicion, Orden]
	Llegar              *auditoria.Caso[EntradaPosicion, Orden]
	CerrarOrden         *auditoria.Caso[EntradaCerrarOrden, Orden]
	FallarOrden         *auditoria.Caso[EntradaFallarOrden, Orden]
	CancelarOrden       *auditoria.Caso[EntradaMotivo, Orden]
	ReabrirOrden        *auditoria.Caso[EntradaMotivo, Orden]
	MarcarChecklist     *auditoria.Caso[EntradaMarcarChecklist, Orden]
	AgregarNota         *auditoria.Caso[EntradaAgregarNota, Orden]
	UsarMaterial        *auditoria.Caso[EntradaUsarMaterial, Orden]
	InstalarEquipo      *auditoria.Caso[EntradaEquipo, Orden]
	RetirarEquipo       *auditoria.Caso[EntradaEquipo, Orden]
	CapturarFirma       *auditoria.Caso[EntradaCapturarFirma, Orden]
	RegistrarAdjunto    *auditoria.Caso[EntradaRegistrarAdjunto, Orden]
	VerOrden            *auditoria.Caso[EntradaVerOrden, OrdenDetalle]
	VerOrdenPropia      *auditoria.Caso[EntradaVerOrden, OrdenDetalle]
	ListarOrdenes       *auditoria.Caso[EntradaListarOrdenes, nucleo.Pagina[Orden]]
	ListarMisOrdenes    *auditoria.Caso[EntradaListarOrdenes, nucleo.Pagina[Orden]]
	ListarEventos       *auditoria.Caso[EntradaListarEventos, nucleo.Pagina[EventoOrden]]
	ListarEventosPropia *auditoria.Caso[EntradaListarEventos, nucleo.Pagina[EventoOrden]]
	ListarTipos         *auditoria.Caso[struct{}, nucleo.Pagina[TipoOrden]]

	// Plantillas de formularios (plantillas.go), formularios de la orden (formularios.go) y
	// pausa (pausas.go).
	ListarPlantillas       *auditoria.Caso[EntradaListarPlantillas, nucleo.Pagina[PlantillaFormulario]]
	VerPlantilla           *auditoria.Caso[EntradaVerPlantilla, PlantillaFormularioDetalle]
	CrearPlantilla         *auditoria.Caso[EntradaCrearPlantilla, PlantillaFormulario]
	EditarPlantilla        *auditoria.Caso[EntradaEditarPlantilla, PlantillaFormulario]
	ActivarPlantilla       *auditoria.Caso[EntradaVersionPlantilla, PlantillaFormulario]
	DesactivarPlantilla    *auditoria.Caso[EntradaVersionPlantilla, PlantillaFormulario]
	AsignarFormulario      *auditoria.Caso[EntradaAgregarFormulario, Orden]
	QuitarFormulario       *auditoria.Caso[EntradaQuitarFormulario, Orden]
	AgregarFormularioCampo *auditoria.Caso[EntradaAgregarFormularioCampo, Orden]
	QuitarFormularioCampo  *auditoria.Caso[EntradaQuitarFormularioCampo, Orden]
	PausarOrden            *auditoria.Caso[EntradaPausar, Orden]
	ReanudarOrden          *auditoria.Caso[EntradaReanudar, Orden]

	// App de campo (sync_*.go).
	LeerPaquete          *auditoria.Caso[EntradaPaquete, Paquete]
	LeerCambios          *auditoria.Caso[EntradaCambios, Cambios]
	RecibirTraslado      *auditoria.Caso[entradaRecibirTraslado, inventario.EstadoRecepcion]
	RegistrarDispositivo *auditoria.Caso[EntradaDispositivo, InicioLote]
	RegistrarRechazo     *auditoria.Caso[EntradaRechazo, *EstadoOrden]

	// Ubicación en vivo (posiciones.go) y retiro automático (retiro.go).
	RegistrarPosiciones *auditoria.Caso[EntradaPosiciones, RespuestaPosiciones]
	UltimasPosiciones   *auditoria.Caso[EntradaUltimasPosiciones, UltimasPosiciones]
	MantenerPosiciones  *auditoria.Caso[struct{}, ResultadoMantenimiento]
	RetiroDesdeArgos    *auditoria.Caso[EntradaRetiroArgos, *Orden]
	// MarcarDatosArgos: argos.datos_cambiados marca las órdenes abiertas (contrato 0.5.0; I1).
	MarcarDatosArgos *auditoria.Caso[EntradaDatosArgos, ResultadoDatosArgos]

	// Ticket y orden de la mano (ticket_orden.go).
	CrearOrdenDeTicket *auditoria.Caso[EntradaCrearOrdenDeTicket, Orden]
	IgualarAsignacion  *auditoria.Caso[EntradaIgualarAsignacion, AsignacionTicketOrden]
}

// Nuevo arma el módulo.
func Nuevo(cfg Config) *Modulo {
	if cfg.Reloj == nil {
		cfg.Reloj = time.Now
	}
	if cfg.Adjuntos == nil {
		// Producción: el módulo de adjuntos de I1. SinAdjuntosObligatorios solo si una prueba lo
		// pide explícitamente.
		cfg.Adjuntos = AdjuntosNucleo{}
	}
	if cfg.ZonaHoraria == nil {
		loc, err := time.LoadLocation("America/Guatemala")
		if err != nil {
			panic("campo: zona horaria America/Guatemala: " + err.Error())
		}
		cfg.ZonaHoraria = loc
	}
	if cfg.Agenda == nil {
		cfg.Agenda = agenda.Nuevo(agenda.Config{Reloj: cfg.Reloj, ZonaHoraria: cfg.ZonaHoraria})
	}
	if cfg.Soporte == nil {
		cfg.Soporte = soporte.Nuevo(soporte.Config{Reloj: cfg.Reloj})
	}
	cfg.Posiciones = cfg.Posiciones.conDefectos()
	m := &Modulo{cfg: cfg}
	m.CrearOrden = &argos.ConInstantanea[EntradaCrearOrden, Orden]{
		Caso: m.nuevoCrearOrden(), Permiso: nucleo.PermOrdenesCrear, Asegurar: cfg.AsegurarArgos,
		Ids: func(in EntradaCrearOrden) (*int64, *int64) { return in.ClienteID, in.ServicioID },
	}
	m.AsignarOrden = m.nuevoAsignarOrden()
	m.ProgramarOrden = m.nuevoProgramarOrden(false)
	m.ReprogramarOrden = m.nuevoProgramarOrden(true)
	m.IniciarCamino = m.nuevoIniciarCamino()
	m.Llegar = m.nuevoLlegar()
	m.CerrarOrden = m.nuevoCerrarOrden()
	m.FallarOrden = m.nuevoFallarOrden()
	m.CancelarOrden = m.nuevoCancelarOrden()
	m.ReabrirOrden = m.nuevoReabrirOrden()
	m.MarcarChecklist = m.nuevoMarcarChecklist()
	m.AgregarNota = m.nuevoAgregarNota()
	m.UsarMaterial = m.nuevoUsarMaterial()
	m.InstalarEquipo = m.nuevoEquipo(AccionInstalar)
	m.RetirarEquipo = m.nuevoEquipo(AccionRetirar)
	m.CapturarFirma = m.nuevoCapturarFirma()
	m.RegistrarAdjunto = m.nuevoRegistrarAdjunto()
	m.VerOrden = m.nuevoVerOrden(nucleo.PermOrdenesVer)
	m.VerOrdenPropia = m.nuevoVerOrden(nucleo.PermOrdenesVerPropias)
	m.ListarOrdenes = m.nuevoListarOrdenes(nucleo.PermOrdenesVer)
	m.ListarMisOrdenes = m.nuevoListarOrdenes(nucleo.PermOrdenesVerPropias)
	m.ListarEventos = m.nuevoListarEventos(nucleo.PermOrdenesVer)
	m.ListarEventosPropia = m.nuevoListarEventos(nucleo.PermOrdenesVerPropias)
	m.ListarTipos = nuevoListarTipos()
	m.ListarPlantillas = m.nuevoListarPlantillas()
	m.VerPlantilla = m.nuevoVerPlantilla()
	m.CrearPlantilla = m.nuevoCrearPlantilla()
	m.EditarPlantilla = m.nuevoEditarPlantilla()
	m.ActivarPlantilla = m.nuevoActivarPlantilla(true)
	m.DesactivarPlantilla = m.nuevoActivarPlantilla(false)
	m.AsignarFormulario = m.nuevoAsignarFormulario()
	m.QuitarFormulario = m.nuevoQuitarFormulario()
	m.AgregarFormularioCampo = m.nuevoAgregarFormularioCampo()
	m.QuitarFormularioCampo = m.nuevoQuitarFormularioCampo()
	m.PausarOrden = m.nuevoPausarOrden()
	m.ReanudarOrden = m.nuevoReanudarOrden()
	m.LeerPaquete = m.nuevoLeerPaquete()
	m.LeerCambios = m.nuevoLeerCambios()
	m.RecibirTraslado = m.nuevoRecibirTraslado()
	m.RegistrarDispositivo = m.nuevoRegistrarDispositivo()
	m.RegistrarRechazo = m.nuevoRegistrarRechazo()
	m.RegistrarPosiciones = m.nuevoRegistrarPosiciones()
	m.UltimasPosiciones = m.nuevoUltimasPosiciones()
	m.MantenerPosiciones = m.nuevoMantenerPosiciones()
	m.RetiroDesdeArgos = m.nuevoRetiroDesdeArgos()
	m.MarcarDatosArgos = m.nuevoMarcarDatosArgos()
	m.CrearOrdenDeTicket = m.nuevoCrearOrdenDeTicket()
	m.IgualarAsignacion = m.nuevoIgualarAsignacion()
	return m
}

// Agenda devuelve el módulo de agenda con que programa (para registrar sus rutas con el mismo).
func (m *Modulo) Agenda() *agenda.Modulo { return m.cfg.Agenda }

// ahora es la hora del módulo en UTC, truncada a microsegundos (la precisión de Postgres).
func (m *Modulo) ahora() time.Time { return m.cfg.Reloj().UTC().Truncate(time.Microsecond) }

// soloLecturas: las consultas no dejan fila de auditoría al terminar bien (sí al fallar).
func soloLecturas[Out any](Out) bool { return false }

package nucleo

import (
	"slices"

	"github.com/danielgtaylor/huma/v2"
)

// Permiso es el nombre de una acción del ACL (`modulo.verbo`), CLAUDE.md §8.
//
// Este archivo es el ÚNICO catálogo: `iris migrar` lo sincroniza con nucleo.permisos y arma
// los roles base, y el OpenAPI lo publica como el enum del esquema `Permiso`. Un caso de uso
// que exige un permiso que no está aquí no arranca (auditoria.Nuevo entra en pánico).
//
// Para agregar permisos (I2: tickets.*, ordenes.*…): una constante, una línea en Catalogo y,
// si corresponde, en RolesBase; luego `make openapi`.
type Permiso string

const (
	PermUsuariosVer        Permiso = "usuarios.ver"
	PermUsuariosCrear      Permiso = "usuarios.crear"
	PermUsuariosEditar     Permiso = "usuarios.editar"
	PermUsuariosDesactivar Permiso = "usuarios.desactivar"
	PermRolesVer           Permiso = "roles.ver"
	PermPermisosVer        Permiso = "permisos.ver"
	PermTokensVer          Permiso = "tokens.ver"
	PermTokensCrear        Permiso = "tokens.crear"
	PermTokensRevocar      Permiso = "tokens.revocar" //nolint:gosec // nombre de permiso, no una credencial

	// Soporte (I2): una acción explícita por permiso, POST /tickets/{id}/<acción>.
	PermTicketsVer           Permiso = "tickets.ver"
	PermTicketsCrear         Permiso = "tickets.crear"
	PermTicketsAsignar       Permiso = "tickets.asignar"
	PermTicketsResponder     Permiso = "tickets.responder"
	PermTicketsNotaInterna   Permiso = "tickets.nota_interna"
	PermTicketsEscalar       Permiso = "tickets.escalar"
	PermTicketsPonerEnEspera Permiso = "tickets.poner_en_espera"
	PermTicketsReanudar      Permiso = "tickets.reanudar"
	PermTicketsResolver      Permiso = "tickets.resolver"
	PermTicketsReabrir       Permiso = "tickets.reabrir"
	PermTicketsCerrar        Permiso = "tickets.cerrar"
	PermTicketsFusionar      Permiso = "tickets.fusionar"
	PermTicketsVerNotas      Permiso = "tickets.ver_notas"

	// Buzones de correo (I2): entrada de tickets por IMAP y respuesta por SMTP.
	PermBuzonesActivar    Permiso = "buzones.activar"
	PermBuzonesCrear      Permiso = "buzones.crear"
	PermBuzonesDesactivar Permiso = "buzones.desactivar"
	PermBuzonesEditar     Permiso = "buzones.editar"
	PermBuzonesVer        Permiso = "buzones.ver"

	// Órdenes de trabajo (I2): una acción explícita por permiso, POST /ordenes/{id}/<acción>.
	// ordenes.ver_propias: solo las órdenes asignadas al actor (técnico); ordenes.registrar:
	// checklist, notas, materiales, equipos, firma y adjuntos de la orden.
	PermOrdenesAsignar       Permiso = "ordenes.asignar"
	PermOrdenesAsignarForm   Permiso = "ordenes.asignar_formulario"
	PermOrdenesCancelar      Permiso = "ordenes.cancelar"
	PermOrdenesCerrar        Permiso = "ordenes.cerrar"
	PermOrdenesCrear         Permiso = "ordenes.crear"
	PermOrdenesFallar        Permiso = "ordenes.fallar"
	PermOrdenesIniciarCamino Permiso = "ordenes.iniciar_camino"
	PermOrdenesLlegar        Permiso = "ordenes.llegar"
	PermOrdenesPausar        Permiso = "ordenes.pausar"
	PermOrdenesProgramar     Permiso = "ordenes.programar"
	PermOrdenesReabrir       Permiso = "ordenes.reabrir"
	PermOrdenesReanudar      Permiso = "ordenes.reanudar"
	PermOrdenesRegistrar     Permiso = "ordenes.registrar"
	PermOrdenesReprogramar   Permiso = "ordenes.reprogramar"
	PermOrdenesVer           Permiso = "ordenes.ver"
	PermOrdenesVerPropias    Permiso = "ordenes.ver_propias"

	// Plantillas de formularios (I2): catálogo administrable con versiones (ver lo usan despacho
	// y el técnico; el resto, admin y supervisor).
	PermPlantillasVer        Permiso = "plantillas.ver"
	PermPlantillasCrear      Permiso = "plantillas.crear"
	PermPlantillasEditar     Permiso = "plantillas.editar"
	PermPlantillasActivar    Permiso = "plantillas.activar"
	PermPlantillasDesactivar Permiso = "plantillas.desactivar"

	// App de campo (I2): paquete del día, cambios y lote de sincronización (contratos/sync.md);
	// posiciones en vivo (el técnico las envía; despacho ve la última de cada técnico).
	PermCampoSincronizar         Permiso = "campo.sincronizar"
	PermCampoRegistrarPosiciones Permiso = "campo.registrar_posiciones"
	PermCampoVerPosiciones       Permiso = "campo.ver_posiciones"

	// Agenda (I2): catálogos (zonas, cuadrillas, técnicos con turnos, ausencias y capacidad) y
	// calendario de citas. Una acción explícita por permiso.
	PermAgendaVer                 Permiso = "agenda.ver"
	PermAgendaCrearZona           Permiso = "agenda.crear_zona"
	PermAgendaEditarZona          Permiso = "agenda.editar_zona"
	PermAgendaActivarZona         Permiso = "agenda.activar_zona"
	PermAgendaDesactivarZona      Permiso = "agenda.desactivar_zona"
	PermAgendaCrearCuadrilla      Permiso = "agenda.crear_cuadrilla"
	PermAgendaEditarCuadrilla     Permiso = "agenda.editar_cuadrilla"
	PermAgendaActivarCuadrilla    Permiso = "agenda.activar_cuadrilla"
	PermAgendaDesactivarCuadrilla Permiso = "agenda.desactivar_cuadrilla"
	PermAgendaCrearTecnico        Permiso = "agenda.crear_tecnico"
	PermAgendaEditarTecnico       Permiso = "agenda.editar_tecnico"
	PermAgendaActivarTecnico      Permiso = "agenda.activar_tecnico"
	PermAgendaDesactivarTecnico   Permiso = "agenda.desactivar_tecnico"
	PermAgendaFijarTurnos         Permiso = "agenda.fijar_turnos"
	PermAgendaFijarCapacidad      Permiso = "agenda.fijar_capacidad"
	PermAgendaRegistrarAusencia   Permiso = "agenda.registrar_ausencia"
	PermAgendaAnularAusencia      Permiso = "agenda.anular_ausencia"

	// Rutas del día (I2): propuesta con OSRM, publicación y ruta propuesta frente a la real.
	PermRutasVer      Permiso = "rutas.ver"
	PermRutasProponer Permiso = "rutas.proponer"
	PermRutasPublicar Permiso = "rutas.publicar"
)

// Empresas (multiempresa igual que Argos).
const (
	PermEmpresasAdoptar        Permiso = "empresas.adoptar"
	PermEmpresasCrear          Permiso = "empresas.crear"
	PermEmpresasEditar         Permiso = "empresas.editar"
	PermEmpresasTodas          Permiso = "empresas.todas"
	PermEmpresasVer            Permiso = "empresas.ver"
	PermUsuariosAsignarEmpresa Permiso = "usuarios.asignar_empresa"
	PermUsuariosQuitarEmpresa  Permiso = "usuarios.quitar_empresa"
	// PermUsuariosReiniciarMFA borra el TOTP de otro usuario y cierra sus sesiones (solo admin).
	PermUsuariosReiniciarMFA Permiso = "usuarios.reiniciar_mfa"
)

// Conector Argos (lectura) e integraciones.
const (
	PermClientesEstadoCuenta    Permiso = "clientes.estado_cuenta"
	PermClientesVer             Permiso = "clientes.ver"
	PermIntegracionesActivar    Permiso = "integraciones.activar"
	PermIntegracionesConectar   Permiso = "integraciones.conectar"
	PermIntegracionesDesactivar Permiso = "integraciones.desactivar"
	PermIntegracionesVer        Permiso = "integraciones.ver"
)

// Adjuntos (fotos, firmas, documentos de órdenes y tickets) y cola de pendientes hacia Argos.
const (
	PermAdjuntosSubir              Permiso = "adjuntos.subir"
	PermAdjuntosVer                Permiso = "adjuntos.ver"
	PermIntegracionesReintentar    Permiso = "integraciones.reintentar"
	PermIntegracionesVerPendientes Permiso = "integraciones.ver_pendientes"
)

// Portal de soporte (/soporte).
const (
	PermPortalEmitir Permiso = "portal.emitir"
)

// DefPermiso es una entrada del catálogo.
type DefPermiso struct {
	Codigo      Permiso
	Descripcion string
}

// Catalogo es la lista ordenada de todos los permisos.
var Catalogo = []DefPermiso{
	{PermAdjuntosSubir, "Registrar y subir adjuntos (fotos, firmas, documentos) de las órdenes y tickets que puedes trabajar"},
	{PermAdjuntosVer, "Ver y descargar los adjuntos de las órdenes y tickets que puedes ver (lectura auditada)"},
	{PermAgendaActivarCuadrilla, "Activar una cuadrilla"},
	{PermAgendaActivarTecnico, "Activar la ficha de agenda de un técnico"},
	{PermAgendaActivarZona, "Activar una zona"},
	{PermAgendaAnularAusencia, "Anular una ausencia registrada de un técnico"},
	{PermAgendaCrearCuadrilla, "Crear cuadrillas y las zonas que atienden"},
	{PermAgendaCrearTecnico, "Dar de alta la ficha de agenda de un técnico (cuadrilla, bodega, habilidades, base)"},
	{PermAgendaCrearZona, "Crear zonas locales (sin conector o fuera del catálogo de Argos)"},
	{PermAgendaDesactivarCuadrilla, "Desactivar una cuadrilla"},
	{PermAgendaDesactivarTecnico, "Desactivar la ficha de agenda de un técnico (deja de ser asignable)"},
	{PermAgendaDesactivarZona, "Desactivar una zona"},
	{PermAgendaEditarCuadrilla, "Editar una cuadrilla y sus zonas"},
	{PermAgendaEditarTecnico, "Editar la ficha de agenda de un técnico"},
	{PermAgendaEditarZona, "Editar una zona (nombre de las locales y polígono)"},
	{PermAgendaFijarCapacidad, "Fijar la capacidad diaria por tipo de orden de un técnico o una cuadrilla"},
	{PermAgendaFijarTurnos, "Fijar los turnos semanales de un técnico"},
	{PermAgendaRegistrarAusencia, "Registrar ausencias de un técnico (vacaciones, enfermedad…)"},
	{PermAgendaVer, "Ver la agenda: zonas, cuadrillas, técnicos, citas y disponibilidad (según el alcance)"},
	{PermBuzonesActivar, "Activar un buzón de correo (empieza a leer y abrir tickets)"},
	{PermBuzonesCrear, "Registrar buzones de correo (IMAP y SMTP) para la entrada de tickets"},
	{PermBuzonesDesactivar, "Desactivar un buzón de correo (deja de leerse)"},
	{PermBuzonesEditar, "Editar un buzón de correo y probar su conexión (nunca ver la contraseña)"},
	{PermBuzonesVer, "Ver los buzones de correo y su estado de lectura (nunca la contraseña)"},
	{PermCampoRegistrarPosiciones, "Enviar la ubicación en vivo desde la app de campo (solo el técnico con sesión de campo)"},
	{PermCampoSincronizar, "Usar la app de campo: paquete del día, cambios y lote de sincronización"},
	{PermCampoVerPosiciones, "Ver la última ubicación de los técnicos (según el alcance)"},
	{PermClientesEstadoCuenta, "Ver el estado de cuenta resumido de un cliente de Argos (saldo y facturas vencidas)"},
	{PermClientesVer, "Buscar clientes de Argos y ver su ficha y servicios (lectura auditada)"},
	{PermEmpresasAdoptar, "Asignar una empresa a los datos operativos guardados sin empresa (modo sin conector); solo admin global"},
	{PermEmpresasCrear, "Registrar a mano una empresa de Argos (id, nombre, NIT) cuando el conector no la trajo"},
	{PermEmpresasEditar, "Editar el nombre y el NIT de una empresa registrada"},
	{PermEmpresasTodas, "Operar en todas las empresas sin asignación (rol global); el resto solo en sus empresas asignadas"},
	{PermEmpresasVer, "Ver las empresas registradas"},
	{PermIntegracionesActivar, "Activar un conector con ArgosISP u Horus"},
	{PermIntegracionesConectar, "Conectar Iris con ArgosISP u Horus (enlace de conexión)"},
	{PermIntegracionesDesactivar, "Desactivar un conector con ArgosISP u Horus"},
	{PermIntegracionesReintentar, "Reintentar a mano una acción pendiente hacia Argos (fallida o agotada)"},
	{PermIntegracionesVer, "Ver los conectores y su estado (nunca el token ni el secreto)"},
	{PermIntegracionesVerPendientes, "Ver la cola de acciones pendientes hacia Argos y su último error"},
	{PermOrdenesAsignar, "Asignar órdenes de trabajo a un técnico o cuadrilla"},
	{PermOrdenesAsignarForm, "Agregar formularios (plantillas) a una orden o quitarlos mientras no tengan respuestas"},
	{PermOrdenesCancelar, "Cancelar órdenes de trabajo"},
	{PermOrdenesCerrar, "Cerrar órdenes de trabajo con su checklist, equipos, firma y adjuntos"},
	{PermOrdenesCrear, "Crear órdenes de trabajo (también desde un ticket)"},
	{PermOrdenesFallar, "Marcar una orden como fallida con su motivo"},
	{PermOrdenesIniciarCamino, "Marcar una orden en camino"},
	{PermOrdenesLlegar, "Marcar la llegada al sitio de una orden"},
	{PermOrdenesPausar, "Pausar una orden en camino o en sitio, con su motivo"},
	{PermOrdenesProgramar, "Programar órdenes pendientes (técnico y fecha)"},
	{PermOrdenesReabrir, "Reabrir órdenes fallidas o canceladas"},
	{PermOrdenesReanudar, "Reanudar una orden pausada"},
	{PermOrdenesRegistrar, "Registrar el trabajo de una orden: checklist, notas, materiales, equipos, firma y adjuntos"},
	{PermOrdenesReprogramar, "Reprogramar órdenes (nueva fecha o técnico)"},
	{PermOrdenesVer, "Ver órdenes de trabajo (según el alcance) y su historial"},
	{PermOrdenesVerPropias, "Ver solo las órdenes de trabajo asignadas a uno mismo (técnico)"},
	{PermPermisosVer, "Ver el catálogo de permisos"},
	{PermPlantillasActivar, "Activar una plantilla de formulario"},
	{PermPlantillasCrear, "Crear plantillas de formulario (checklist)"},
	{PermPlantillasDesactivar, "Desactivar una plantilla de formulario (deja de ofrecerse; los formularios ya agregados siguen)"},
	{PermPlantillasEditar, "Editar plantillas de formulario (con uso, crea una versión nueva)"},
	{PermPlantillasVer, "Ver las plantillas de formulario y sus versiones"},
	{PermPortalEmitir, "Emitir tickets de acceso al portal de soporte para un cliente de Argos (lo usa Argos con su token de integración)"},
	{PermRolesVer, "Ver roles y sus permisos"},
	{PermRutasProponer, "Proponer la ruta del día de un técnico (orden de visita con OSRM y las ventanas de cita)"},
	{PermRutasPublicar, "Publicar la ruta del día de un técnico (la recibe en la app de campo)"},
	{PermRutasVer, "Ver las rutas del día: propuesta, publicada y el recorrido real (según el alcance)"},
	{PermTicketsAsignar, "Asignar tickets a un usuario o a una cuadrilla"},
	{PermTicketsCerrar, "Cerrar tickets (sin resolver, con motivo)"},
	{PermTicketsCrear, "Crear tickets de soporte"},
	{PermTicketsEscalar, "Escalar tickets (subir prioridad o nivel)"},
	{PermTicketsFusionar, "Fusionar un ticket duplicado en otro"},
	{PermTicketsNotaInterna, "Agregar notas internas a un ticket"},
	{PermTicketsPonerEnEspera, "Poner tickets en espera (pausa el SLA de resolución)"},
	{PermTicketsReabrir, "Reabrir tickets resueltos"},
	{PermTicketsReanudar, "Reanudar tickets en espera"},
	{PermTicketsResolver, "Resolver tickets con su solución"},
	{PermTicketsResponder, "Responder al cliente en un ticket (mensaje público)"},
	{PermTicketsVer, "Ver tickets, su conversación, historial, categorías y SLA"},
	{PermTicketsVerNotas, "Ver las notas internas de los tickets"},
	{PermTokensCrear, "Crear tokens de API (iris_pat_) para un usuario"},
	{PermTokensRevocar, "Revocar tokens de API"},
	{PermTokensVer, "Ver tokens de API (nunca el valor)"},
	{PermUsuariosAsignarEmpresa, "Asignar a un usuario una empresa en que operar (solo empresas en que operas tú)"},
	{PermUsuariosCrear, "Crear usuarios y asignarles roles"},
	{PermUsuariosDesactivar, "Desactivar usuarios (no se borran)"},
	{PermUsuariosEditar, "Editar usuarios, su contraseña y sus roles"},
	{PermUsuariosQuitarEmpresa, "Quitar a un usuario una empresa (solo empresas en que operas tú)"},
	{PermUsuariosReiniciarMFA, "Reiniciar el MFA de otro usuario (borra su TOTP y cierra sus sesiones)"},
	{PermUsuariosVer, "Ver y buscar usuarios"},
}

// DefRol es un rol base. Permisos nil = todos los del catálogo (admin).
type DefRol struct {
	Codigo      string
	Nombre      string
	Descripcion string
	Todos       bool
	Permisos    []Permiso
}

// permisosTickets son todos los de soporte (rol soporte y supervisor).
var permisosTickets = []Permiso{
	PermTicketsVer, PermTicketsCrear, PermTicketsAsignar, PermTicketsResponder, PermTicketsNotaInterna,
	PermTicketsEscalar, PermTicketsPonerEnEspera, PermTicketsReanudar, PermTicketsResolver,
	PermTicketsReabrir, PermTicketsCerrar, PermTicketsFusionar, PermTicketsVerNotas,
}

// permisosOrdenes son todos los de órdenes de trabajo (despacho, supervisor).
var permisosOrdenes = []Permiso{
	PermOrdenesVer, PermOrdenesVerPropias, PermOrdenesCrear, PermOrdenesAsignar, PermOrdenesProgramar,
	PermOrdenesReprogramar, PermOrdenesIniciarCamino, PermOrdenesLlegar, PermOrdenesRegistrar,
	PermOrdenesCerrar, PermOrdenesFallar, PermOrdenesCancelar, PermOrdenesReabrir, PermCampoSincronizar,
	PermOrdenesAsignarForm, PermOrdenesPausar, PermOrdenesReanudar, PermPlantillasVer,
}

// permisosPlantillas son los de administrar plantillas de formulario (supervisor).
var permisosPlantillas = []Permiso{PermPlantillasCrear, PermPlantillasEditar, PermPlantillasActivar, PermPlantillasDesactivar}

// permisosAgenda son todos los de agenda (supervisor).
var permisosAgenda = []Permiso{
	PermAgendaVer, PermAgendaCrearZona, PermAgendaEditarZona, PermAgendaActivarZona, PermAgendaDesactivarZona,
	PermAgendaCrearCuadrilla, PermAgendaEditarCuadrilla, PermAgendaActivarCuadrilla, PermAgendaDesactivarCuadrilla,
	PermAgendaCrearTecnico, PermAgendaEditarTecnico, PermAgendaActivarTecnico, PermAgendaDesactivarTecnico,
	PermAgendaFijarTurnos, PermAgendaFijarCapacidad, PermAgendaRegistrarAusencia, PermAgendaAnularAusencia,
	PermCampoVerPosiciones,
}

// permisosRutas son los de rutas del día (despacho, supervisor).
var permisosRutas = []Permiso{PermRutasVer, PermRutasProponer, PermRutasPublicar}

// RolesBase son los roles que crea y mantiene el seed (CLAUDE.md §8). Sus permisos se
// sincronizan en cada `iris migrar`: lo que no está aquí se les quita.
var RolesBase = []DefRol{
	{Codigo: "admin", Nombre: "Administrador", Descripcion: "Todos los permisos", Todos: true},
	{Codigo: "supervisor", Nombre: "Supervisor", Descripcion: "Supervisa soporte, campo y despacho",
		Permisos: append([]Permiso{PermUsuariosVer, PermRolesVer, PermPermisosVer, PermTokensVer,
			PermClientesVer, PermClientesEstadoCuenta, PermIntegracionesVer, PermEmpresasVer,
			PermAdjuntosSubir, PermAdjuntosVer, PermIntegracionesReintentar, PermIntegracionesVerPendientes,
			PermBuzonesVer, PermBuzonesCrear, PermBuzonesEditar, PermBuzonesActivar, PermBuzonesDesactivar},
			slices.Concat(permisosTickets, permisosOrdenes, permisosAgenda, permisosRutas, permisosPlantillas)...)},
	{Codigo: "despacho", Nombre: "Despacho", Descripcion: "Programa órdenes y rutas",
		Permisos: append([]Permiso{PermUsuariosVer,
			PermClientesVer, PermAdjuntosSubir, PermAdjuntosVer, PermIntegracionesVerPendientes,
			PermTicketsVer, PermTicketsCrear, PermTicketsAsignar, PermTicketsNotaInterna, PermTicketsVerNotas,
			PermAgendaVer, PermAgendaFijarTurnos, PermAgendaRegistrarAusencia, PermAgendaAnularAusencia,
			PermCampoVerPosiciones},
			slices.Concat(permisosOrdenes, permisosRutas)...)},
	{Codigo: "tecnico", Nombre: "Técnico", Descripcion: "Trabaja órdenes en campo",
		Permisos: []Permiso{PermClientesVer, PermAdjuntosSubir, PermAdjuntosVer,
			PermOrdenesVerPropias, PermOrdenesIniciarCamino, PermOrdenesLlegar, PermOrdenesRegistrar,
			PermOrdenesCerrar, PermOrdenesFallar, PermCampoSincronizar, PermCampoRegistrarPosiciones,
			PermOrdenesPausar, PermOrdenesReanudar, PermPlantillasVer}},
	{Codigo: "soporte", Nombre: "Soporte", Descripcion: "Atiende tickets",
		Permisos: append([]Permiso{PermUsuariosVer,
			PermClientesVer, PermClientesEstadoCuenta, PermAdjuntosSubir, PermAdjuntosVer}, permisosTickets...)},
	{Codigo: "ventas", Nombre: "Ventas", Descripcion: "Seguimiento comercial",
		Permisos: []Permiso{PermClientesVer, PermClientesEstadoCuenta}},
	{Codigo: "lectura", Nombre: "Lectura", Descripcion: "Solo consulta",
		Permisos: []Permiso{PermUsuariosVer, PermRolesVer, PermPermisosVer,
			PermClientesVer, PermTicketsVer, PermOrdenesVer, PermAgendaVer, PermRutasVer}},
	// Roles de los usuarios de tipo integración (decisión del líder): el token que Iris entrega
	// a cada sistema (iris integracion crear) lleva solo lo de su contrato.
	//  - Argos: tickets.ver, tickets.crear y ordenes.ver (contratos/argosisp.md §1.2); sso.emitir
	//    (también §1.2) se agrega cuando exista ese permiso. portal.emitir NO es
	//    del §1.2: es el pedido A9 (botón «Soporte» del portal de Argos), propuesto por Iris y
	//    pendiente de acordar con Argos; se deja por decisión del líder.
	//  - Horus: tickets.ver y tickets.crear (contratos/horus.md §1.2); sso.emitir cuando exista.
	{Codigo: "integracion_argos", Nombre: "Integración Argos", Descripcion: "Token de ArgosISP en Iris (contrato §1.2 y pedido A9 propuesto)",
		Permisos: []Permiso{PermTicketsVer, PermTicketsCrear, PermOrdenesVer, PermPortalEmitir}},
	{Codigo: "integracion_horus", Nombre: "Integración Horus", Descripcion: "Token de Horus Flow en Iris (contrato §1.2)",
		Permisos: []Permiso{PermTicketsVer, PermTicketsCrear}},
}

// PermisosDeRol devuelve los permisos efectivos de un rol base.
func (r DefRol) PermisosDeRol() []Permiso {
	if r.Todos {
		return CodigosPermisos()
	}
	return r.Permisos
}

// CodigosPermisos devuelve los códigos del catálogo.
func CodigosPermisos() []Permiso {
	salida := make([]Permiso, 0, len(Catalogo))
	for _, p := range Catalogo {
		salida = append(salida, p.Codigo)
	}
	return salida
}

// EsPermiso indica si el código está en el catálogo.
func EsPermiso(codigo string) bool {
	return slices.ContainsFunc(Catalogo, func(p DefPermiso) bool { return string(p.Codigo) == codigo })
}

// DescripcionPermiso devuelve la descripción de un permiso del catálogo.
func DescripcionPermiso(codigo Permiso) string {
	for _, p := range Catalogo {
		if p.Codigo == codigo {
			return p.Descripcion
		}
	}
	return ""
}

// Schema publica Permiso como componente con enum en el OpenAPI (huma.SchemaProvider).
func (Permiso) Schema(r huma.Registry) *huma.Schema {
	const nombre = "Permiso"
	if _, ok := r.Map()[nombre]; !ok {
		enum := make([]any, 0, len(Catalogo))
		for _, p := range CodigosPermisos() {
			enum = append(enum, string(p))
		}
		r.Map()[nombre] = &huma.Schema{
			Type:        huma.TypeString,
			Title:       nombre,
			Description: "Permiso del ACL: nombre de la acción (`modulo.verbo`). Catálogo en internal/nucleo/permisos.go.",
			Enum:        enum,
		}
	}
	return &huma.Schema{Ref: "#/components/schemas/" + nombre}
}

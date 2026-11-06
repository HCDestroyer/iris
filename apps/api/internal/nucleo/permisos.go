package nucleo

import (
	"slices"
	"strings"

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

	// Roles propios (los base no se editan ni se desactivan) y su asignación con alcance.
	PermRolesCrear             Permiso = "roles.crear"
	PermRolesEditar            Permiso = "roles.editar"
	PermRolesActivar           Permiso = "roles.activar"
	PermRolesDesactivar        Permiso = "roles.desactivar"
	PermUsuariosAsignarRol     Permiso = "usuarios.asignar_rol"
	PermUsuariosQuitarRol      Permiso = "usuarios.quitar_rol"
	PermAccesosVer             Permiso = "accesos.ver"
	PermUsuariosCerrarSesiones Permiso = "usuarios.cerrar_sesiones"

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
	// ordenes.aplicar_datos_argos / ordenes.descartar_datos_argos: atender «Dato de Argos cambió».
	PermOrdenesAplicarDatosArgos   Permiso = "ordenes.aplicar_datos_argos"
	PermOrdenesDescartarDatosArgos Permiso = "ordenes.descartar_datos_argos"
	PermOrdenesAsignar             Permiso = "ordenes.asignar"
	PermOrdenesAsignarForm         Permiso = "ordenes.asignar_formulario"
	PermOrdenesCancelar            Permiso = "ordenes.cancelar"
	PermOrdenesCerrar              Permiso = "ordenes.cerrar"
	PermOrdenesCrear               Permiso = "ordenes.crear"
	PermOrdenesFallar              Permiso = "ordenes.fallar"
	PermOrdenesIniciarCamino       Permiso = "ordenes.iniciar_camino"
	PermOrdenesLlegar              Permiso = "ordenes.llegar"
	PermOrdenesPausar              Permiso = "ordenes.pausar"
	PermOrdenesProgramar           Permiso = "ordenes.programar"
	PermOrdenesReabrir             Permiso = "ordenes.reabrir"
	PermOrdenesReanudar            Permiso = "ordenes.reanudar"
	PermOrdenesRegistrar           Permiso = "ordenes.registrar"
	// ordenes.tomar: el técnico toma para sí una orden libre de su zona o cuadrilla, si la
	// empresa lo permite (ajuste tecnicos_toman_libres de campo).
	PermOrdenesTomar       Permiso = "ordenes.tomar"
	PermOrdenesReprogramar Permiso = "ordenes.reprogramar"
	PermOrdenesVer         Permiso = "ordenes.ver"
	PermOrdenesVerPropias  Permiso = "ordenes.ver_propias"

	// Ticket y orden de la mano (I2). tickets.crear_orden: crear la orden de trabajo del ticket y
	// asignarla (y programarla) en un paso; exige además ordenes.crear, ordenes.asignar y, con
	// fecha, ordenes.programar. ordenes.igualar_asignacion: copiar la asignación del ticket a su
	// orden o al revés; exige además ordenes.asignar o tickets.asignar (el lado que cambia).
	PermTicketsCrearOrden        Permiso = "tickets.crear_orden"
	PermOrdenesIgualarAsignacion Permiso = "ordenes.igualar_asignacion"

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
	// campo.ver_actividad: última conexión, sincronización, versión de la app y pendientes de cada
	// técnico (Administración → Actividad de la app).
	PermCampoVerActividad Permiso = "campo.ver_actividad"
	// campo.configurar: ajustes de la app de campo de la empresa (p. ej. que los técnicos tomen
	// órdenes libres).
	PermCampoConfigurar Permiso = "campo.configurar"
	// Inventario del técnico (contrato Iris ↔ ArgosISP 0.5.0 §3.3; I1): su propia bodega en Argos
	// (por su vínculo) y recibir órdenes de traslado con firma desde la app de campo.
	PermCampoVerBodega       Permiso = "campo.ver_bodega"
	PermCampoRecibirTraslado Permiso = "campo.recibir_traslado"

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
	// Tareas propias del técnico (I2): algo suyo con hora y duración que no es una orden. Las
	// crea, completa y anula el técnico (las suyas, también sin señal) o quien lo supervisa
	// (agenda.ver con alcance sobre su cuadrilla).
	PermAgendaCrearTarea     Permiso = "agenda.crear_tarea"
	PermAgendaCompletarTarea Permiso = "agenda.completar_tarea"
	PermAgendaAnularTarea    Permiso = "agenda.anular_tarea"
	// Vínculo del técnico con su usuario de Argos (contrato 0.5.0 §3.3; I1): define desde qué
	// bodega de Argos se consumen materiales y comodatos, así que es de administración.
	PermAgendaVincularArgos    Permiso = "agenda.vincular_argos"
	PermAgendaDesvincularArgos Permiso = "agenda.desvincular_argos"

	// Rutas del día (I2): propuesta con OSRM, publicación y ruta propuesta frente a la real.
	PermRutasVer      Permiso = "rutas.ver"
	PermRutasProponer Permiso = "rutas.proponer"
	PermRutasPublicar Permiso = "rutas.publicar"

	// Seguimiento comercial (I2): prospecto → factibilidad → cotización → contrato → ganar. Una
	// acción explícita por permiso (POST /prospectos/{id}/<acción>).
	PermProspectosVer                Permiso = "prospectos.ver"
	PermProspectosCrear              Permiso = "prospectos.crear"
	PermProspectosEditar             Permiso = "prospectos.editar"
	PermProspectosAsignar            Permiso = "prospectos.asignar"
	PermProspectosContactar          Permiso = "prospectos.contactar"
	PermProspectosRegistrarActividad Permiso = "prospectos.registrar_actividad"
	PermProspectosPerder             Permiso = "prospectos.perder"
	PermProspectosReabrir            Permiso = "prospectos.reabrir"
	PermProspectosGanar              Permiso = "prospectos.ganar"
	PermFactibilidadEvaluar          Permiso = "factibilidad.evaluar"
	PermFactibilidadConfigurar       Permiso = "factibilidad.configurar"
	// factibilidad.registrar_manual: el resultado de una visita técnica (supervisor, técnico).
	PermFactibilidadRegistrarManual Permiso = "factibilidad.registrar_manual"
	PermCotizacionesCrear           Permiso = "cotizaciones.crear"
	PermCotizacionesAceptar         Permiso = "cotizaciones.aceptar"
	PermContratosCrear              Permiso = "contratos.crear"
	PermContratosFirmar             Permiso = "contratos.firmar"

	// Reportes (I2): solo lectura, agregados de soporte y campo (con el alcance por zona o
	// cuadrilla del rol).
	PermReportesVer Permiso = "reportes.ver"
	// Encuestas de satisfacción (I2): la lista con calificación, comentario y el enlace para
	// compartir mientras esté pendiente.
	PermEncuestasVer Permiso = "encuestas.ver"
	// encuestas.compartir: pedir el enlace de una encuesta pendiente de canal enlace para
	// mandárselo al cliente (acción auditada).
	PermEncuestasCompartir Permiso = "encuestas.compartir"
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
	PermAdjuntosSubir           Permiso = "adjuntos.subir"
	PermAdjuntosVer             Permiso = "adjuntos.ver"
	PermIntegracionesReintentar Permiso = "integraciones.reintentar"
	// PermIntegracionesResolverPendiente: dar por resuelto a mano un pendiente rechazado por Argos
	// (ya corregido en Argos); sus dependientes (el cargo que esperaba al consumo) salen.
	PermIntegracionesResolverPendiente Permiso = "integraciones.resolver_pendiente"
	PermIntegracionesVerPendientes     Permiso = "integraciones.ver_pendientes"
)

// Mensajería (contrato Iris ↔ ArgosISP 0.6.0 §6; servicio cmd/mensajeria).
const (
	// PermMensajeriaEnviar: pedir avisos y consultar los propios (POST/GET /mensajeria/avisos). Es de la
	// integración integracion_argos (§1.2); ningún rol de persona lo recibe salvo admin (todos).
	PermMensajeriaEnviar Permiso = "mensajeria.enviar"
	// PermMensajeriaRegistrarConsentimiento: registrar el permiso de contacto de un cliente (§6.4).
	PermMensajeriaRegistrarConsentimiento Permiso = "mensajeria.registrar_consentimiento"
	// PermMensajeriaVer: ver las plantillas vigentes y en qué canales están aprobadas (§6.3).
	PermMensajeriaVer Permiso = "mensajeria.ver"
	// PermMensajeriaConfigurar: configurar los canales de una empresa (credenciales de WhatsApp).
	PermMensajeriaConfigurar Permiso = "mensajeria.configurar"
)

// Chat interno del equipo (servicio cmd/mensajeria; contratos/chat.md).
const (
	// PermChatVer: ver las conversaciones de las que eres miembro (y las de tu alcance con
	// chat.supervisar), sus mensajes y el flujo de eventos.
	PermChatVer Permiso = "chat.ver"
	// PermChatEnviar: abrir conversaciones, enviar, eliminar los propios (15 min) y unirse al hilo
	// de una orden que puedes ver.
	PermChatEnviar Permiso = "chat.enviar"
	// PermChatSupervisar: ser miembro de los chats de cuadrilla y de orden de tu alcance (zona o
	// cuadrilla; sin alcance, todos los de la empresa). Nunca da acceso a las directas.
	PermChatSupervisar Permiso = "chat.supervisar"
)

// Bandeja de conversaciones con clientes (servicio cmd/mensajeria; contratos/conversaciones.md).
const (
	// PermConversacionesVer: ver las conversaciones de tus colas, las sin cola, las asignadas a ti y
	// las que te mencionan, con sus mensajes, contadores y el flujo de eventos.
	PermConversacionesVer Permiso = "conversaciones.ver"
	// PermConversacionesSupervisar: ver todas las conversaciones de la empresa (todas las colas).
	PermConversacionesSupervisar Permiso = "conversaciones.supervisar"
	// PermConversacionesResponder: enviar mensajes y plantillas, notas internas con menciones.
	PermConversacionesResponder Permiso = "conversaciones.responder"
	// PermConversacionesIniciar: escribir primero (plantilla aprobada) a un cliente o contacto con
	// permiso de contacto. Sensible: manda mensajes a clientes por iniciativa propia.
	PermConversacionesIniciar Permiso = "conversaciones.iniciar"
	// PermConversacionesPasar: pasar la conversación que tienes a un compañero de la cola.
	PermConversacionesPasar Permiso = "conversaciones.pasar"
	// PermConversacionesTomar: asignarse una conversación sin asignar.
	PermConversacionesTomar Permiso = "conversaciones.tomar"
	// PermConversacionesAsignar: asignar una conversación a otro agente o quitar la asignación.
	PermConversacionesAsignar Permiso = "conversaciones.asignar"
	// PermConversacionesTransferir: pasar una conversación a otra cola con un resumen.
	PermConversacionesTransferir Permiso = "conversaciones.transferir"
	// PermConversacionesEtiquetar: poner y quitar etiquetas.
	PermConversacionesEtiquetar Permiso = "conversaciones.etiquetar"
	// PermConversacionesResolver: resolver y reabrir.
	PermConversacionesResolver Permiso = "conversaciones.resolver"
	// PermConversacionesAsociar: asociar a un cliente de Argos y administrar contactos de Iris.
	PermConversacionesAsociar Permiso = "conversaciones.asociar"
	// PermConversacionesRegistrarBoleta: mandar a Argos la boleta que envió el cliente (§7.2).
	PermConversacionesRegistrarBoleta Permiso = "conversaciones.registrar_boleta"
	// PermConversacionesRegistrarPromesa: simular y pedir a Argos una promesa de pago (§7.4).
	PermConversacionesRegistrarPromesa Permiso = "conversaciones.registrar_promesa"
	// PermConversacionesConfigurar: etiquetas y respuestas rápidas de la empresa.
	PermConversacionesConfigurar Permiso = "conversaciones.configurar"
	// PermCanalesConfigurar: colas, uso, cola y horario de cada número de WhatsApp, horarios y
	// feriados (no las credenciales: esas son de mensajeria.configurar).
	PermCanalesConfigurar Permiso = "canales.configurar"
	// PermManualesVer: buscar y leer la biblioteca de manuales y sus sugerencias.
	PermManualesVer Permiso = "manuales.ver"
	// PermManualesEditar: crear, editar, activar y desactivar manuales.
	PermManualesEditar Permiso = "manuales.editar"
	// PermManualesRevocarEnlace: revocar un enlace público de un manual ya enviado.
	PermManualesRevocarEnlace Permiso = "manuales.revocar_enlace"
)

// Portal de soporte (/soporte).
const (
	PermPortalEmitir Permiso = "portal.emitir"
	// PermSSOEmitir: pedir un ticket de autologin («Ver en Iris»; contrato argosisp.md §1.2–§1.3).
	PermSSOEmitir Permiso = "sso.emitir"
)

// PermisosSensibles son los de administración (usuarios, tokens, empresas, buzones,
// integraciones, plantillas, configuración comercial y emisión de accesos), no los de ver ni los
// operativos. Una sola fuente para el autologin desde Argos: quien tiene alguno (o
// empresas.todas) solo entra con TOTP, y en una sesión abierta por el autologin toda acción con
// uno de estos permisos exige re-autenticarse con la contraseña. Un permiso nuevo de esta clase
// debe agregarse aquí (lo verifica TestPermisosSensiblesCubrenLaAdministracion).
var PermisosSensibles = map[Permiso]bool{
	PermUsuariosCrear: true, PermUsuariosEditar: true, PermUsuariosDesactivar: true, PermUsuariosReiniciarMFA: true,
	PermUsuariosAsignarEmpresa: true, PermUsuariosQuitarEmpresa: true,
	PermTokensCrear: true, PermTokensRevocar: true,
	PermRolesCrear: true, PermRolesEditar: true, PermRolesActivar: true, PermRolesDesactivar: true,
	PermUsuariosAsignarRol: true, PermUsuariosQuitarRol: true, PermUsuariosCerrarSesiones: true,
	PermEmpresasCrear: true, PermEmpresasEditar: true, PermEmpresasAdoptar: true,
	PermBuzonesCrear: true, PermBuzonesEditar: true, PermBuzonesActivar: true, PermBuzonesDesactivar: true,
	PermIntegracionesConectar: true, PermIntegracionesActivar: true, PermIntegracionesDesactivar: true, PermIntegracionesReintentar: true,
	PermIntegracionesResolverPendiente: true, PermPlantillasCrear: true, PermPlantillasEditar: true, PermPlantillasActivar: true, PermPlantillasDesactivar: true,
	PermFactibilidadConfigurar: true, PermPortalEmitir: true, PermSSOEmitir: true,
	PermAgendaVincularArgos: true, PermAgendaDesvincularArgos: true, PermMensajeriaConfigurar: true, PermCanalesConfigurar: true, PermConversacionesConfigurar: true,
	PermConversacionesRegistrarPromesa: true, PermConversacionesRegistrarBoleta: true, PermConversacionesSupervisar: true,
	PermConversacionesIniciar: true, PermCampoConfigurar: true,
}

// EsSensible indica si el permiso es de administración (PermisosSensibles).
func EsSensible(p Permiso) bool { return PermisosSensibles[p] }

// DefPermiso es una entrada del catálogo.
type DefPermiso struct {
	Codigo      Permiso
	Descripcion string
}

// Catalogo es la lista ordenada de todos los permisos.
var Catalogo = []DefPermiso{
	{PermAccesosVer, "Ver el historial de accesos (quién entró, cuándo, por qué canal y con qué resultado) y las sesiones activas de los usuarios"},
	{PermAdjuntosSubir, "Registrar y subir adjuntos (fotos, firmas, documentos) de las órdenes y tickets que puedes trabajar"},
	{PermAdjuntosVer, "Ver y descargar los adjuntos de las órdenes y tickets que puedes ver (lectura auditada)"},
	{PermAgendaActivarCuadrilla, "Activar una cuadrilla"},
	{PermAgendaActivarTecnico, "Activar la ficha de agenda de un técnico"},
	{PermAgendaActivarZona, "Activar una zona"},
	{PermAgendaAnularAusencia, "Anular una ausencia registrada de un técnico"},
	{PermAgendaAnularTarea, "Anular una tarea propia de un técnico (la suya o la de un técnico que supervisas)"},
	{PermAgendaCompletarTarea, "Marcar como hecha una tarea propia de un técnico (la suya o la de un técnico que supervisas)"},
	{PermAgendaCrearCuadrilla, "Crear cuadrillas y las zonas que atienden"},
	{PermAgendaCrearTarea, "Agendar una tarea propia (algo que no es una orden), también sin señal desde la app de campo"},
	{PermAgendaCrearTecnico, "Dar de alta la ficha de agenda de un técnico (cuadrilla, bodega, habilidades, base)"},
	{PermAgendaCrearZona, "Crear zonas locales (sin conector o fuera del catálogo de Argos)"},
	{PermAgendaDesactivarCuadrilla, "Desactivar una cuadrilla"},
	{PermAgendaDesactivarTecnico, "Desactivar la ficha de agenda de un técnico (deja de ser asignable)"},
	{PermAgendaDesactivarZona, "Desactivar una zona"},
	{PermAgendaDesvincularArgos, "Quitar el vínculo de la ficha de un técnico con su usuario de Argos"},
	{PermAgendaEditarCuadrilla, "Editar una cuadrilla y sus zonas"},
	{PermAgendaEditarTecnico, "Editar la ficha de agenda de un técnico"},
	{PermAgendaEditarZona, "Editar una zona (nombre de las locales y polígono)"},
	{PermAgendaFijarCapacidad, "Fijar la capacidad diaria por tipo de orden de un técnico o una cuadrilla"},
	{PermAgendaFijarTurnos, "Fijar los turnos semanales de un técnico"},
	{PermAgendaRegistrarAusencia, "Registrar ausencias de un técnico (vacaciones, enfermedad…)"},
	{PermAgendaVer, "Ver la agenda: zonas, cuadrillas, técnicos, citas y disponibilidad (según el alcance)"},
	{PermAgendaVincularArgos, "Vincular la ficha de un técnico con su usuario de Argos (inventario por técnico, Argos ≥ 1.6.0) y ver los técnicos de Argos"},
	{PermBuzonesActivar, "Activar un buzón de correo (empieza a leer y abrir tickets)"},
	{PermBuzonesCrear, "Registrar buzones de correo (IMAP y SMTP) para la entrada de tickets"},
	{PermBuzonesDesactivar, "Desactivar un buzón de correo (deja de leerse)"},
	{PermBuzonesEditar, "Editar un buzón de correo y probar su conexión (nunca ver la contraseña)"},
	{PermBuzonesVer, "Ver los buzones de correo y su estado de lectura (nunca la contraseña)"},
	{PermCampoConfigurar, "Fijar los ajustes de la app de campo de la empresa (p. ej. que los técnicos tomen órdenes libres)"},
	{PermCampoRecibirTraslado, "Recibir con firma, desde la app de campo, una orden de traslado de Argos hacia tu bodega (operación traslado.recibir del lote)"},
	{PermCampoRegistrarPosiciones, "Enviar la ubicación en vivo desde la app de campo (solo el técnico con sesión de campo)"},
	{PermCampoSincronizar, "Usar la app de campo: paquete del día, cambios y lote de sincronización"},
	{PermCampoVerActividad, "Ver la actividad de la app de campo de cada técnico: última conexión y sincronización, versión, pendientes y última ubicación (según el alcance)"},
	{PermCampoVerBodega, "Ver tu bodega en Argos (existencias, unidades, kardex) y tus órdenes de traslado por recibir (por tu vínculo con Argos)"},
	{PermCampoVerPosiciones, "Ver la última ubicación de los técnicos (según el alcance)"},
	{PermCanalesConfigurar, "Configurar las colas de conversaciones y el uso, la cola y el horario de cada número de WhatsApp (no sus credenciales)"},
	{PermChatEnviar, "Abrir conversaciones del chat interno, enviar mensajes, eliminar los propios en 15 minutos y unirse al hilo de una orden que puedes ver"},
	{PermChatSupervisar, "Ser miembro de los chats de cuadrilla y de orden de tu alcance (zona o cuadrilla; sin alcance, todos los de la empresa). No da acceso a los mensajes directos"},
	{PermChatVer, "Ver tus conversaciones del chat interno del equipo y sus mensajes"},
	{PermClientesEstadoCuenta, "Ver el estado de cuenta resumido de un cliente de Argos (saldo y facturas vencidas)"},
	{PermClientesVer, "Buscar clientes de Argos y ver su ficha y servicios (lectura auditada)"},
	{PermContratosCrear, "Generar el contrato de un prospecto con la cotización aceptada"},
	{PermContratosFirmar, "Registrar la firma del cliente en un contrato (genera el PDF firmado)"},
	{PermConversacionesAsignar, "Asignar una conversación con un cliente a otro agente o quitar la asignación"},
	{PermConversacionesAsociar, "Asociar una conversación a un cliente de Argos y administrar los contactos propios de Iris"},
	{PermConversacionesConfigurar, "Administrar las etiquetas y las respuestas rápidas de las conversaciones de la empresa"},
	{PermConversacionesEtiquetar, "Poner y quitar etiquetas a una conversación"},
	{PermConversacionesIniciar, "Escribir primero por WhatsApp, con una plantilla aprobada, a un cliente o contacto con permiso de contacto desde un número de tus colas"},
	{PermConversacionesPasar, "Pasar la conversación que tienes a un compañero de la misma cola, con una nota interna"},
	{PermConversacionesRegistrarBoleta, "Mandar a Argos la boleta de pago que envió el cliente por WhatsApp (caja la aprueba en Argos)"},
	{PermConversacionesRegistrarPromesa, "Simular y pedir a Argos una promesa de pago desde una conversación (Argos decide)"},
	{PermConversacionesResolver, "Resolver y reabrir conversaciones"},
	{PermConversacionesResponder, "Responder a los clientes por WhatsApp (texto, adjuntos y plantillas aprobadas) y escribir notas internas con menciones"},
	{PermConversacionesSupervisar, "Ver todas las conversaciones con clientes de la empresa, de todas las colas"},
	{PermConversacionesTomar, "Tomar (asignarse) una conversación sin asignar"},
	{PermConversacionesTransferir, "Pasar una conversación a otra cola con un resumen"},
	{PermConversacionesVer, "Ver las conversaciones con clientes de tus colas, las asignadas a ti y las que te mencionan"},
	{PermCotizacionesAceptar, "Marcar como aceptada por el cliente una cotización vigente"},
	{PermCotizacionesCrear, "Crear cotizaciones (y versiones nuevas) de un prospecto con los planes de Argos"},
	{PermEmpresasAdoptar, "Asignar una empresa a los datos operativos guardados sin empresa (modo sin conector); solo admin global"},
	{PermEmpresasCrear, "Registrar a mano una empresa de Argos (id, nombre, NIT) cuando el conector no la trajo"},
	{PermEmpresasEditar, "Editar el nombre y el NIT de una empresa registrada"},
	{PermEmpresasTodas, "Operar en todas las empresas sin asignación (rol global); el resto solo en sus empresas asignadas"},
	{PermEmpresasVer, "Ver las empresas registradas"},
	{PermEncuestasCompartir, "Pedir el enlace de una encuesta pendiente que no salió por correo, para compartirlo con el cliente (auditado)"},
	{PermEncuestasVer, "Ver las encuestas de satisfacción (estado, calificación y comentario; nunca el enlace)"},
	{PermFactibilidadConfigurar, "Configurar los umbrales de distancia de la factibilidad de la empresa"},
	{PermFactibilidadEvaluar, "Evaluar la factibilidad técnica de un prospecto (distancia al router o planta externa)"},
	{PermFactibilidadRegistrarManual, "Registrar la factibilidad de una visita técnica (manual; puede anular una automática con motivo)"},
	{PermIntegracionesActivar, "Activar un conector con ArgosISP u Horus"},
	{PermIntegracionesConectar, "Conectar Iris con ArgosISP u Horus (enlace de conexión)"},
	{PermIntegracionesDesactivar, "Desactivar un conector con ArgosISP u Horus"},
	{PermIntegracionesReintentar, "Reintentar a mano una acción pendiente hacia Argos (fallida o agotada)"},
	{PermIntegracionesResolverPendiente, "Dar por resuelta a mano una acción hacia Argos que Argos rechazó o que nació incompleta (ya corregida en Argos): no vuelve a salir y lo que esperaba por ella sale"},
	{PermIntegracionesVer, "Ver los conectores y su estado (nunca el token ni el secreto)"},
	{PermIntegracionesVerPendientes, "Ver la cola de acciones pendientes hacia Argos y su último error"},
	{PermManualesEditar, "Crear, editar, activar y desactivar artículos de la biblioteca de manuales"},
	{PermManualesRevocarEnlace, "Revocar el enlace público de un manual enviado a un cliente"},
	{PermManualesVer, "Buscar y leer la biblioteca de manuales y sus sugerencias para una conversación"},
	{PermMensajeriaConfigurar, "Configurar los canales de mensajería de una empresa (WhatsApp: número, credenciales y plantillas aprobadas)"},
	{PermMensajeriaEnviar, "Pedir avisos por la mensajería de Iris y consultar su estado (lo usa Argos con su token de integración)"},
	{PermMensajeriaRegistrarConsentimiento, "Registrar el permiso de contacto (acepta o baja) de un cliente de Argos por un canal"},
	{PermMensajeriaVer, "Ver las plantillas de mensajería vigentes y en qué canales están aprobadas"},
	{PermOrdenesAplicarDatosArgos, "Aplicar a una orden abierta los datos nuevos de Argos (dirección, coordenadas, referencia y contacto) tras el aviso «Dato de Argos cambió»"},
	{PermOrdenesAsignar, "Asignar órdenes de trabajo a un técnico o cuadrilla"},
	{PermOrdenesAsignarForm, "Agregar formularios (plantillas) a una orden o quitarlos mientras no tengan respuestas"},
	{PermOrdenesCancelar, "Cancelar órdenes de trabajo"},
	{PermOrdenesCerrar, "Cerrar órdenes de trabajo con su checklist, equipos, firma y adjuntos"},
	{PermOrdenesCrear, "Crear órdenes de trabajo (también desde un ticket)"},
	{PermOrdenesDescartarDatosArgos, "Descartar el aviso «Dato de Argos cambió» de una orden sin cambiar sus datos"},
	{PermOrdenesFallar, "Marcar una orden como fallida con su motivo"},
	{PermOrdenesIgualarAsignacion, "Igualar la asignación (técnico y cuadrilla) de una orden y su ticket, en el sentido que se elija"},
	{PermOrdenesIniciarCamino, "Marcar una orden en camino"},
	{PermOrdenesLlegar, "Marcar la llegada al sitio de una orden"},
	{PermOrdenesPausar, "Pausar una orden en camino o en sitio, con su motivo"},
	{PermOrdenesProgramar, "Programar órdenes pendientes (técnico y fecha)"},
	{PermOrdenesReabrir, "Reabrir órdenes fallidas o canceladas"},
	{PermOrdenesReanudar, "Reanudar una orden pausada"},
	{PermOrdenesRegistrar, "Registrar el trabajo de una orden: checklist, notas, materiales, equipos, firma y adjuntos"},
	{PermOrdenesReprogramar, "Reprogramar órdenes (nueva fecha o técnico)"},
	{PermOrdenesTomar, "Tomar para ti una orden libre (sin técnico) de tu zona o cuadrilla, si la empresa lo permite"},
	{PermOrdenesVer, "Ver órdenes de trabajo (según el alcance) y su historial"},
	{PermOrdenesVerPropias, "Ver solo las órdenes de trabajo asignadas a uno mismo (técnico)"},
	{PermPermisosVer, "Ver el catálogo de permisos"},
	{PermPlantillasActivar, "Activar una plantilla de formulario"},
	{PermPlantillasCrear, "Crear plantillas de formulario (checklist)"},
	{PermPlantillasDesactivar, "Desactivar una plantilla de formulario (deja de ofrecerse; los formularios ya agregados siguen)"},
	{PermPlantillasEditar, "Editar plantillas de formulario (con uso, crea una versión nueva)"},
	{PermPlantillasVer, "Ver las plantillas de formulario y sus versiones"},
	{PermPortalEmitir, "Emitir tickets de acceso al portal de soporte para un cliente de Argos (lo usa Argos con su token de integración)"},
	{PermProspectosAsignar, "Asignar un prospecto a un vendedor"},
	{PermProspectosContactar, "Marcar un prospecto como contactado"},
	{PermProspectosCrear, "Registrar prospectos (panel, web, referidos…)"},
	{PermProspectosEditar, "Editar los datos de un prospecto"},
	{PermProspectosGanar, "Ganar un prospecto: alta del cliente y el servicio en Argos y orden de instalación"},
	{PermProspectosPerder, "Marcar un prospecto como perdido, con motivo"},
	{PermProspectosReabrir, "Reabrir un prospecto perdido"},
	{PermProspectosRegistrarActividad, "Registrar llamadas, visitas y notas de un prospecto"},
	{PermProspectosVer, "Ver prospectos, su embudo, actividades, cotizaciones y contratos (según el alcance)"},
	{PermReportesVer, "Ver los reportes de SLA, resolución, órdenes por técnico, primera visita y reincidencias (según el alcance; también en CSV)"},
	{PermRolesActivar, "Volver a activar un rol propio desactivado"},
	{PermRolesCrear, "Crear roles propios eligiendo sus permisos (solo los que tienes sin límite de alcance)"},
	{PermRolesDesactivar, "Desactivar un rol propio: deja de dar permisos y de poder asignarse (los roles base no)"},
	{PermRolesEditar, "Editar el nombre, la descripción y los permisos de un rol propio (los roles base no)"},
	{PermRolesVer, "Ver roles y sus permisos"},
	{PermRutasProponer, "Proponer la ruta del día de un técnico (orden de visita con OSRM y las ventanas de cita)"},
	{PermRutasPublicar, "Publicar la ruta del día de un técnico (la recibe en la app de campo)"},
	{PermRutasVer, "Ver las rutas del día: propuesta, publicada y el recorrido real (según el alcance)"},
	{PermSSOEmitir, "Pedir un ticket de autologin («Ver en Iris») para abrir un ticket, una orden o un prospecto (lo usa Argos con el token del canje)"},
	{PermTicketsAsignar, "Asignar tickets a un usuario o a una cuadrilla"},
	{PermTicketsCerrar, "Cerrar tickets (sin resolver, con motivo)"},
	{PermTicketsCrear, "Crear tickets de soporte"},
	{PermTicketsCrearOrden, "Crear la orden de trabajo de un ticket ya asignada (y programada si se elige fecha) en un paso"},
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
	{PermUsuariosAsignarRol, "Asignar un rol a un usuario, con alcance opcional por zona o cuadrilla"},
	{PermUsuariosCerrarSesiones, "Cerrar las sesiones activas de otro usuario (una o todas)"},
	{PermUsuariosCrear, "Crear usuarios y asignarles roles"},
	{PermUsuariosDesactivar, "Desactivar usuarios (no se borran)"},
	{PermUsuariosEditar, "Editar usuarios, su contraseña y sus roles"},
	{PermUsuariosQuitarEmpresa, "Quitar a un usuario una empresa (solo empresas en que operas tú)"},
	{PermUsuariosQuitarRol, "Quitar un rol (o una asignación con alcance) a un usuario"},
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
	PermTicketsCrearOrden, PermOrdenesIgualarAsignacion, PermOrdenesAplicarDatosArgos, PermOrdenesDescartarDatosArgos,
}

// permisosPlantillas son los de administrar plantillas de formulario (supervisor).
var permisosPlantillas = []Permiso{PermPlantillasCrear, PermPlantillasEditar, PermPlantillasActivar, PermPlantillasDesactivar}

// permisosAgenda son todos los de agenda (supervisor).
var permisosAgenda = []Permiso{
	PermAgendaVer, PermAgendaCrearZona, PermAgendaEditarZona, PermAgendaActivarZona, PermAgendaDesactivarZona,
	PermAgendaCrearCuadrilla, PermAgendaEditarCuadrilla, PermAgendaActivarCuadrilla, PermAgendaDesactivarCuadrilla,
	PermAgendaCrearTecnico, PermAgendaEditarTecnico, PermAgendaActivarTecnico, PermAgendaDesactivarTecnico,
	PermAgendaFijarTurnos, PermAgendaFijarCapacidad, PermAgendaRegistrarAusencia, PermAgendaAnularAusencia,
	PermCampoVerPosiciones, PermCampoVerActividad, PermAgendaCrearTarea, PermAgendaCompletarTarea, PermAgendaAnularTarea,
}

// permisosRutas son los de rutas del día (despacho, supervisor).
var permisosRutas = []Permiso{PermRutasVer, PermRutasProponer, PermRutasPublicar}

// permisosAtencion son los de atender conversaciones con clientes (despacho, ventas, soporte y
// supervisor); cada rol suma los suyos (contratos/conversaciones.md §4).
var permisosAtencion = []Permiso{PermConversacionesVer, PermConversacionesResponder, PermConversacionesTomar, PermConversacionesPasar,
	PermConversacionesEtiquetar, PermConversacionesResolver, PermManualesVer}

// permisosComercial son todos los del seguimiento comercial (ventas, supervisor) salvo
// factibilidad.configurar (umbrales de la empresa: supervisor y admin).
var permisosComercial = []Permiso{
	PermProspectosVer, PermProspectosCrear, PermProspectosEditar, PermProspectosAsignar, PermProspectosContactar,
	PermProspectosRegistrarActividad, PermProspectosPerder, PermProspectosReabrir, PermProspectosGanar,
	PermFactibilidadEvaluar, PermCotizacionesCrear, PermCotizacionesAceptar, PermContratosCrear, PermContratosFirmar,
}

// RolesBase son los roles que crea y mantiene el seed (CLAUDE.md §8). Sus permisos se
// sincronizan en cada `iris migrar`: lo que no está aquí se les quita.
var RolesBase = []DefRol{
	{Codigo: "admin", Nombre: "Administrador", Descripcion: "Todos los permisos", Todos: true},
	{Codigo: "supervisor", Nombre: "Supervisor", Descripcion: "Supervisa soporte, campo y despacho",
		Permisos: append([]Permiso{PermUsuariosVer, PermRolesVer, PermPermisosVer, PermTokensVer,
			PermClientesVer, PermClientesEstadoCuenta, PermIntegracionesVer, PermEmpresasVer,
			PermAdjuntosSubir, PermAdjuntosVer, PermIntegracionesReintentar, PermIntegracionesResolverPendiente, PermIntegracionesVerPendientes,
			PermBuzonesVer, PermBuzonesCrear, PermBuzonesEditar, PermBuzonesActivar, PermBuzonesDesactivar,
			PermChatVer, PermChatEnviar, PermChatSupervisar,
			PermConversacionesSupervisar, PermConversacionesAsignar, PermConversacionesTransferir, PermConversacionesAsociar,
			PermConversacionesRegistrarBoleta, PermConversacionesRegistrarPromesa, PermConversacionesConfigurar, PermCanalesConfigurar, PermManualesEditar, PermManualesRevocarEnlace,
			PermConversacionesIniciar},
			slices.Concat(permisosAtencion, permisosTickets, permisosOrdenes, permisosAgenda, permisosRutas, permisosPlantillas, permisosComercial,
				[]Permiso{PermFactibilidadConfigurar, PermFactibilidadRegistrarManual, PermReportesVer, PermEncuestasVer, PermEncuestasCompartir})...)},
	{Codigo: "despacho", Nombre: "Despacho", Descripcion: "Programa órdenes y rutas",
		Permisos: append([]Permiso{PermUsuariosVer,
			PermClientesVer, PermAdjuntosSubir, PermAdjuntosVer, PermIntegracionesVerPendientes,
			PermTicketsVer, PermTicketsCrear, PermTicketsAsignar, PermTicketsNotaInterna, PermTicketsVerNotas,
			PermAgendaVer, PermAgendaFijarTurnos, PermAgendaRegistrarAusencia, PermAgendaAnularAusencia,
			PermAgendaCrearTarea, PermAgendaCompletarTarea, PermAgendaAnularTarea,
			PermCampoVerPosiciones, PermCampoVerActividad, PermChatVer, PermChatEnviar},
			slices.Concat(permisosOrdenes, permisosRutas, permisosAtencion)...)},
	{Codigo: "tecnico", Nombre: "Técnico", Descripcion: "Trabaja órdenes en campo",
		Permisos: []Permiso{PermClientesVer, PermAdjuntosSubir, PermAdjuntosVer,
			PermOrdenesVerPropias, PermOrdenesIniciarCamino, PermOrdenesLlegar, PermOrdenesRegistrar,
			PermOrdenesCerrar, PermOrdenesFallar, PermCampoSincronizar, PermCampoRegistrarPosiciones,
			PermOrdenesPausar, PermOrdenesReanudar, PermPlantillasVer, PermFactibilidadRegistrarManual,
			PermCampoVerBodega, PermCampoRecibirTraslado, PermChatVer, PermChatEnviar,
			PermAgendaCrearTarea, PermAgendaCompletarTarea, PermAgendaAnularTarea, PermOrdenesTomar}},
	{Codigo: "soporte", Nombre: "Soporte", Descripcion: "Atiende tickets",
		Permisos: append([]Permiso{PermUsuariosVer,
			PermClientesVer, PermClientesEstadoCuenta, PermAdjuntosSubir, PermAdjuntosVer, PermEncuestasVer, PermEncuestasCompartir,
			PermChatVer, PermChatEnviar, PermConversacionesTransferir, PermConversacionesAsociar,
			PermConversacionesRegistrarBoleta, PermConversacionesRegistrarPromesa, PermConversacionesIniciar}, slices.Concat(permisosTickets, permisosAtencion)...)},
	{Codigo: "ventas", Nombre: "Ventas", Descripcion: "Seguimiento comercial",
		Permisos: append([]Permiso{PermClientesVer, PermClientesEstadoCuenta, PermAdjuntosVer, PermAdjuntosSubir, PermAgendaVer,
			PermConversacionesTransferir, PermConversacionesAsociar, PermConversacionesIniciar}, slices.Concat(permisosComercial, permisosAtencion)...)},
	{Codigo: "lectura", Nombre: "Lectura", Descripcion: "Solo consulta",
		Permisos: []Permiso{PermUsuariosVer, PermRolesVer, PermPermisosVer,
			PermClientesVer, PermTicketsVer, PermOrdenesVer, PermAgendaVer, PermRutasVer, PermProspectosVer,
			PermReportesVer, PermChatVer, PermConversacionesVer, PermManualesVer}},
	// Roles de los usuarios de tipo integración (decisión del líder): el token que Iris entrega
	// a cada sistema (iris integracion crear) lleva solo lo de su contrato.
	//  - Argos: tickets.ver, tickets.crear, ordenes.ver y sso.emitir (contratos/argosisp.md §1.2).
	//    portal.emitir NO es
	//    del §1.2: es el pedido A9 (botón «Soporte» del portal de Argos), propuesto por Iris y
	//    pendiente de acordar con Argos; se deja por decisión del líder.
	//  - Horus: tickets.ver y tickets.crear (contratos/horus.md §1.2); sso.emitir cuando exista.
	{Codigo: "integracion_argos", Nombre: "Integración Argos", Descripcion: "Token de ArgosISP en Iris (contrato §1.2 y pedido A9 propuesto)",
		Permisos: []Permiso{PermTicketsVer, PermTicketsCrear, PermOrdenesVer, PermPortalEmitir, PermSSOEmitir,
			// Mensajería (contrato 0.6.0 §1.2 y §6): avisos, permisos de contacto y plantillas.
			PermMensajeriaEnviar, PermMensajeriaRegistrarConsentimiento, PermMensajeriaVer}},
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

// modulosPermiso agrupa los permisos por el prefijo de su código (`modulo.verbo`) en los módulos
// que ve el administrador al armar un rol: código del módulo y nombre legible.
var modulosPermiso = map[string][2]string{
	"tickets": {"soporte", "Soporte (tickets)"}, "buzones": {"soporte", "Soporte (tickets)"},
	"encuestas": {"encuestas", "Encuestas"},
	"ordenes":   {"ordenes", "Órdenes de trabajo"}, "plantillas": {"ordenes", "Órdenes de trabajo"},
	"adjuntos": {"ordenes", "Órdenes de trabajo"},
	"campo":    {"campo", "App de campo"},
	"chat":     {"chat", "Chat del equipo"},
	"agenda":   {"agenda", "Agenda y rutas"}, "rutas": {"agenda", "Agenda y rutas"},
	"prospectos": {"comercial", "Comercial"}, "factibilidad": {"comercial", "Comercial"},
	"cotizaciones": {"comercial", "Comercial"}, "contratos": {"comercial", "Comercial"},
	"reportes": {"reportes", "Reportes"},
	"clientes": {"integraciones", "Argos e integraciones"}, "integraciones": {"integraciones", "Argos e integraciones"},
	"portal": {"integraciones", "Argos e integraciones"}, "sso": {"integraciones", "Argos e integraciones"},
	"usuarios": {"usuarios", "Usuarios y accesos"}, "accesos": {"usuarios", "Usuarios y accesos"},
	"tokens": {"usuarios", "Usuarios y accesos"},
	"roles":  {"roles", "Roles y permisos"}, "permisos": {"roles", "Roles y permisos"},
	"empresas":       {"empresas", "Empresas"},
	"mensajeria":     {"mensajeria", "Mensajería"},
	"conversaciones": {"conversaciones", "Conversaciones con clientes"}, "canales": {"conversaciones", "Conversaciones con clientes"},
	"manuales": {"conversaciones", "Conversaciones con clientes"},
}

// ModuloDe devuelve el módulo (código y nombre legible) de un permiso.
func ModuloDe(p Permiso) (codigo, nombre string) {
	prefijo, _, _ := strings.Cut(string(p), ".")
	if m, ok := modulosPermiso[prefijo]; ok {
		return m[0], m[1]
	}
	return "otros", "Otros"
}

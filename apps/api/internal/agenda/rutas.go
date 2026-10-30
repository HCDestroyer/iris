package agenda

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
)

// Handlers delgados: leen la petición, llaman al caso de uso y arman la respuesta.

const etiqueta = "agenda"

var seguridad = []map[string][]string{{"sesion": {}}, {"bearer": {}}}

func operacion(id, metodo, ruta, resumen string, permiso nucleo.Permiso, status int, errores ...int) huma.Operation {
	return huma.Operation{
		OperationID: id, Method: metodo, Path: ruta, Summary: resumen,
		Description: "Permiso: `" + string(permiso) + "`.", Tags: []string{etiqueta}, DefaultStatus: status,
		Errors:     append([]int{http.StatusUnauthorized, http.StatusForbidden, http.StatusUnprocessableEntity}, errores...),
		Security:   seguridad,
		Extensions: map[string]any{"x-permiso": string(permiso)},
	}
}

func accion(id, ruta, resumen, detalle string, permiso nucleo.Permiso) huma.Operation {
	op := operacion(id, http.MethodPost, ruta, resumen, permiso, http.StatusOK, http.StatusNotFound, http.StatusConflict)
	if detalle != "" {
		op.Description += " " + detalle
	}
	return op
}

type cuerpo[T any] struct {
	Body T
}

type conClave[T any] struct {
	nucleo.ConIdempotencia
	Body T
}

type conID[T any] struct {
	nucleo.ConIdempotencia
	ID   int64 `path:"id" minimum:"1"`
	Body T
}

type conAusencia[T any] struct {
	nucleo.ConIdempotencia
	ID         int64 `path:"id" minimum:"1"`
	AusenciaID int64 `path:"ausenciaId" minimum:"1"`
	Body       T
}

func responder[T any](v T, err error) (*cuerpo[T], error) {
	if err != nil {
		return nil, err
	}
	return &cuerpo[T]{Body: v}, nil
}

// registrarConID publica POST <ruta> con {id}; fijar pone el id en la entrada.
func registrarConID[In, Out any](api huma.API, op huma.Operation, ejecutar func(context.Context, In) (Out, error), fijar func(*In, int64)) {
	huma.Register(api, op, func(ctx context.Context, in *conID[In]) (*cuerpo[Out], error) {
		e := in.Body
		fijar(&e, in.ID)
		return responder(ejecutar(ctx, e))
	})
}

// Registrar publica las rutas de agenda.
func (m *Modulo) Registrar(api huma.API) {
	// Zonas.
	huma.Register(api, operacion("listarZonas", http.MethodGet, "/api/v1/zonas", "Zonas (espejo de Argos y locales)", nucleo.PermAgendaVer, http.StatusOK),
		func(ctx context.Context, in *EntradaListarCatalogo) (*cuerpo[nucleo.Pagina[Zona]], error) {
			return responder(m.ListarZonas.Ejecutar(ctx, *in))
		})
	crearZona := operacion("crearZona", http.MethodPost, "/api/v1/zonas", "Crear una zona local", nucleo.PermAgendaCrearZona, http.StatusCreated)
	crearZona.Description += " Exige el permiso sin límite de zona o cuadrilla. Las zonas de Argos llegan solas (espejo del catálogo). Emite `zona.creada`."
	huma.Register(api, crearZona, func(ctx context.Context, in *conClave[EntradaCrearZona]) (*cuerpo[Zona], error) {
		return responder(m.CrearZona.Ejecutar(ctx, in.Body))
	})
	registrarConID(api, accion("editarZona", "/api/v1/zonas/{id}/editar", "Editar una zona", "Nombre (solo locales y migradas) y polígono GeoJSON. Emite `zona.actualizada`.",
		nucleo.PermAgendaEditarZona), m.EditarZona.Ejecutar, func(e *EntradaEditarZona, id int64) { e.ID = id })
	registrarConID(api, accion("activarZona", "/api/v1/zonas/{id}/activar", "Activar una zona", "", nucleo.PermAgendaActivarZona),
		m.ActivarZona.Ejecutar, func(e *EntradaVersion, id int64) { e.ID = id })
	registrarConID(api, accion("desactivarZona", "/api/v1/zonas/{id}/desactivar", "Desactivar una zona", "Una zona inactiva no se puede usar en tickets ni órdenes nuevos.",
		nucleo.PermAgendaDesactivarZona), m.DesactivarZona.Ejecutar, func(e *EntradaVersion, id int64) { e.ID = id })

	// Cuadrillas.
	huma.Register(api, operacion("listarCuadrillas", http.MethodGet, "/api/v1/cuadrillas", "Cuadrillas con sus zonas y capacidad", nucleo.PermAgendaVer, http.StatusOK),
		func(ctx context.Context, in *EntradaListarCatalogo) (*cuerpo[nucleo.Pagina[Cuadrilla]], error) {
			return responder(m.ListarCuadrillas.Ejecutar(ctx, *in))
		})
	crearCuadrilla := operacion("crearCuadrilla", http.MethodPost, "/api/v1/cuadrillas", "Crear una cuadrilla", nucleo.PermAgendaCrearCuadrilla, http.StatusCreated)
	crearCuadrilla.Description += " Exige el permiso sin límite de zona o cuadrilla. Emite `cuadrilla.creada`."
	huma.Register(api, crearCuadrilla, func(ctx context.Context, in *conClave[EntradaCrearCuadrilla]) (*cuerpo[Cuadrilla], error) {
		return responder(m.CrearCuadrilla.Ejecutar(ctx, in.Body))
	})
	registrarConID(api, accion("editarCuadrilla", "/api/v1/cuadrillas/{id}/editar", "Editar una cuadrilla", "Nombre y zonas (reemplaza). Emite `cuadrilla.actualizada`.",
		nucleo.PermAgendaEditarCuadrilla), m.EditarCuadrilla.Ejecutar, func(e *EntradaEditarCuadrilla, id int64) { e.ID = id })
	registrarConID(api, accion("activarCuadrilla", "/api/v1/cuadrillas/{id}/activar", "Activar una cuadrilla", "", nucleo.PermAgendaActivarCuadrilla),
		m.ActivarCuadrilla.Ejecutar, func(e *EntradaVersion, id int64) { e.ID = id })
	registrarConID(api, accion("desactivarCuadrilla", "/api/v1/cuadrillas/{id}/desactivar", "Desactivar una cuadrilla", "",
		nucleo.PermAgendaDesactivarCuadrilla), m.DesactivarCuadrilla.Ejecutar, func(e *EntradaVersion, id int64) { e.ID = id })
	registrarConID(api, accion("capacidadCuadrilla", "/api/v1/cuadrillas/{id}/capacidad", "Fijar la capacidad diaria de una cuadrilla",
		"Reemplaza las líneas (por tipo de orden o todos): cupos y/o minutos por día, sumando sus técnicos.", nucleo.PermAgendaFijarCapacidad),
		m.CapacidadCuadrilla.Ejecutar, func(e *EntradaFijarCapacidad, id int64) { e.ID = id })

	// Técnicos.
	listar := operacion("listarTecnicos", http.MethodGet, "/api/v1/tecnicos", "Fichas de técnicos (según el alcance)", nucleo.PermAgendaVer, http.StatusOK)
	huma.Register(api, listar, func(ctx context.Context, in *EntradaListarTecnicos) (*cuerpo[nucleo.Pagina[Tecnico]], error) {
		return responder(m.ListarTecnicos.Ejecutar(ctx, *in))
	})
	asignables := operacion("tecnicosAsignables", http.MethodGet, "/api/v1/tecnicos/asignables", "Técnicos asignables con su disponibilidad del día", nucleo.PermAgendaVer, http.StatusOK)
	asignables.Description += " Técnicos activos que atienden la zona (su cuadrilla) y pueden ver sus órdenes, con turnos, ausencias, citas, huecos libres y capacidad usada del día (hora de Guatemala). Los disponibles primero."
	huma.Register(api, asignables, func(ctx context.Context, in *EntradaAsignables) (*cuerpo[RespuestaAsignables], error) {
		return responder(m.Asignables.Ejecutar(ctx, *in))
	})
	candidatos := operacion("candidatosTecnico", http.MethodGet, "/api/v1/tecnicos/candidatos", "Usuarios que pueden recibir ficha de técnico", nucleo.PermAgendaCrearTecnico, http.StatusOK)
	candidatos.Description += " Personas activas sin ficha, con `ordenes.ver_propias` u `ordenes.ver`, que operan en la empresa activa. Es el selector de usuario al dar de alta un técnico (no exige `usuarios.ver`)."
	huma.Register(api, candidatos, func(ctx context.Context, in *EntradaCandidatosTecnico) (*cuerpo[nucleo.Pagina[CandidatoTecnico]], error) {
		return responder(m.CandidatosTecnico.Ejecutar(ctx, *in))
	})
	huma.Register(api, operacion("verTecnico", http.MethodGet, "/api/v1/tecnicos/{id}", "Ficha de un técnico con turnos, capacidad y ausencias", nucleo.PermAgendaVer, http.StatusOK, http.StatusNotFound),
		func(ctx context.Context, in *EntradaVerTecnico) (*cuerpo[TecnicoDetalle], error) {
			return responder(m.VerTecnico.Ejecutar(ctx, *in))
		})
	crearTecnico := operacion("crearTecnico", http.MethodPost, "/api/v1/tecnicos", "Dar de alta la ficha de agenda de un técnico", nucleo.PermAgendaCrearTecnico, http.StatusCreated)
	crearTecnico.Description += " El usuario debe ser una persona activa con `ordenes.ver_propias` u `ordenes.ver` que opere en la empresa activa. Nace sin turnos. Emite `tecnico.creado`."
	huma.Register(api, crearTecnico, func(ctx context.Context, in *conClave[EntradaCrearTecnico]) (*cuerpo[TecnicoDetalle], error) {
		return responder(m.CrearTecnico.Ejecutar(ctx, in.Body))
	})
	registrarConID(api, accion("editarTecnico", "/api/v1/tecnicos/{id}/editar", "Editar la ficha de un técnico",
		"Reemplaza cuadrilla, bodega, habilidades y base. Emite `tecnico.actualizado`.", nucleo.PermAgendaEditarTecnico),
		m.EditarTecnico.Ejecutar, func(e *EntradaEditarTecnico, id int64) { e.ID = id })
	registrarConID(api, accion("activarTecnico", "/api/v1/tecnicos/{id}/activar", "Activar la ficha de un técnico", "", nucleo.PermAgendaActivarTecnico),
		m.ActivarTecnico.Ejecutar, func(e *EntradaVersion, id int64) { e.ID = id })
	registrarConID(api, accion("desactivarTecnico", "/api/v1/tecnicos/{id}/desactivar", "Desactivar la ficha de un técnico",
		"Deja de ser asignable; sus citas no se mueven.", nucleo.PermAgendaDesactivarTecnico),
		m.DesactivarTecnico.Ejecutar, func(e *EntradaVersion, id int64) { e.ID = id })
	registrarConID(api, accion("turnosTecnico", "/api/v1/tecnicos/{id}/turnos", "Fijar los turnos semanales de un técnico",
		"Reemplaza los turnos (hora de Guatemala, sin cruces en un día). Las citas ya programadas no se mueven.", nucleo.PermAgendaFijarTurnos),
		m.FijarTurnos.Ejecutar, func(e *EntradaFijarTurnos, id int64) { e.ID = id })
	registrarConID(api, accion("capacidadTecnico", "/api/v1/tecnicos/{id}/capacidad", "Fijar la capacidad diaria de un técnico",
		"Reemplaza las líneas (por tipo de orden o todos): cupos y/o minutos por día.", nucleo.PermAgendaFijarCapacidad),
		m.CapacidadTecnico.Ejecutar, func(e *EntradaFijarCapacidad, id int64) { e.ID = id })
	ausencia := operacion("registrarAusencia", http.MethodPost, "/api/v1/tecnicos/{id}/ausencias", "Registrar una ausencia del técnico",
		nucleo.PermAgendaRegistrarAusencia, http.StatusCreated, http.StatusNotFound)
	ausencia.Description += " Devuelve las citas vivas que quedan dentro (no se mueven solas: reprográmalas). Emite `tecnico.ausencia_registrada`."
	huma.Register(api, ausencia, func(ctx context.Context, in *conID[EntradaRegistrarAusencia]) (*cuerpo[RespuestaAusencia], error) {
		e := in.Body
		e.ID = in.ID
		return responder(m.RegistrarAusencia.Ejecutar(ctx, e))
	})
	huma.Register(api, accion("anularAusencia", "/api/v1/tecnicos/{id}/ausencias/{ausenciaId}/anular", "Anular una ausencia",
		"Queda en el historial. Emite `tecnico.ausencia_anulada`.", nucleo.PermAgendaAnularAusencia),
		func(ctx context.Context, in *conAusencia[EntradaAnularAusencia]) (*cuerpo[Ausencia], error) {
			e := in.Body
			e.TecnicoID, e.AusenciaID = in.ID, in.AusenciaID
			return responder(m.AnularAusencia.Ejecutar(ctx, e))
		})

	// Calendario de despacho.
	cal := operacion("calendarioAgenda", http.MethodGet, "/api/v1/agenda", "Calendario de despacho: citas y ausencias en un rango", nucleo.PermAgendaVer, http.StatusOK)
	cal.Description += " Citas (con su orden, ventana y estado) de la empresa activa según el alcance (zona o cuadrilla de la orden, o cuadrilla del técnico), y ausencias de los técnicos visibles. Hasta 31 días."
	huma.Register(api, cal, func(ctx context.Context, in *EntradaCalendario) (*cuerpo[Calendario], error) {
		return responder(m.Calendario.Ejecutar(ctx, *in))
	})

	// Rutas del día.
	listarRutas := operacion("listarRutas", http.MethodGet, "/api/v1/rutas", "Rutas del día (borradores y publicadas)", nucleo.PermRutasVer, http.StatusOK)
	listarRutas.Description += " De la empresa activa, de los técnicos que cubre tu alcance (su cuadrilla). Sin paradas ni recorrido: GET /rutas/{id}."
	huma.Register(api, listarRutas, func(ctx context.Context, in *EntradaListarRutas) (*cuerpo[nucleo.Pagina[Ruta]], error) {
		return responder(m.ListarRutas.Ejecutar(ctx, *in))
	})
	proponer := operacion("proponerRuta", http.MethodPost, "/api/v1/rutas/proponer", "Proponer la ruta del día de un técnico",
		nucleo.PermRutasProponer, http.StatusOK, http.StatusNotFound, http.StatusConflict)
	proponer.Description += " Ordena las citas del técnico ese día (las ya en camino, en sitio o terminadas van primero, en el orden real)" +
		" para llegar dentro de la ventana acordada con el cliente con el menor viaje (OSRM /table; trazo con /route), con la duración" +
		" estimada por tipo de orden. Si OSRM no responde, ordena por ventana y distancia en línea recta y lo dice (`degradada`, `motivoMotor`)." +
		" Crea o rehace el borrador (una publicada no se toca hasta publicar otra). 422 `ruta_vacia`, `demasiadas_paradas`," +
		" `validacion` (ordenIds). Emite `ruta.propuesta`."
	huma.Register(api, proponer, func(ctx context.Context, in *conClave[EntradaProponerRuta]) (*cuerpo[RutaDetalle], error) {
		return responder(m.ProponerRuta(ctx, in.Body))
	})
	verRuta := operacion("verRuta", http.MethodGet, "/api/v1/rutas/{id}", "Ruta propuesta frente a la real",
		nucleo.PermRutasVer, http.StatusOK, http.StatusNotFound)
	verRuta.Description += " Paradas con la llegada estimada y la real (en sitio), el desvío, si la orden sigue en la ruta (`vigente`)," +
		" y el recorrido real del técnico ese día (campo.posiciones; exige `campo.ver_posiciones`). Paradas fuera de tu alcance salen ocultas."
	huma.Register(api, verRuta, func(ctx context.Context, in *EntradaVerRuta) (*cuerpo[RutaDetalle], error) {
		return responder(m.VerRuta.Ejecutar(ctx, *in))
	})
	registrarConID(api, accion("publicarRuta", "/api/v1/rutas/{id}/publicar", "Publicar la ruta del día",
		"Publica el borrador: el técnico la recibe en el paquete de la app de campo (órdenes en el orden de la ruta). La publicada"+
			" anterior queda `reemplazada` (nunca se borra). 422 `ruta_desactualizada` (una orden cambió de técnico o de hora: propónla"+
			" de nuevo) y `transicion_invalida`. Emite `ruta.publicada`.", nucleo.PermRutasPublicar),
		m.Publicar.Ejecutar, func(e *EntradaPublicarRuta, id int64) { e.ID = id })
}

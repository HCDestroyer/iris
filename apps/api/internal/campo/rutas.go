package campo

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
)

// Handlers delgados: leen la petición, llaman al caso de uso y arman la respuesta.

const etiqueta = "campo"

// maxBytesLote: el lote lleva firmas (SVG de hasta 200 000 caracteres): 4 MiB en vez del 1 MiB
// de huma, solo en /sync/lote.
const maxBytesLote = 4 << 20

var seguridad = []map[string][]string{{"sesion": {}}, {"bearer": {}}}

func operacion(id, metodo, ruta, resumen string, permiso nucleo.Permiso, status int, errores ...int) huma.Operation {
	return huma.Operation{
		OperationID:   id,
		Method:        metodo,
		Path:          ruta,
		Summary:       resumen,
		Description:   "Permiso: `" + string(permiso) + "`.",
		Tags:          []string{etiqueta},
		DefaultStatus: status,
		Errors:        append([]int{http.StatusUnauthorized, http.StatusForbidden, http.StatusUnprocessableEntity}, errores...),
		Security:      seguridad,
		Extensions:    map[string]any{"x-permiso": string(permiso)},
	}
}

// accion documenta una acción sobre una orden: 404, 409 (versión) y 422.
func accion(id, ruta, resumen, detalle string, permiso nucleo.Permiso) huma.Operation {
	op := operacion(id, http.MethodPost, "/api/v1/ordenes/{id}/"+ruta, resumen, permiso, http.StatusOK, http.StatusNotFound, http.StatusConflict)
	op.Description += " " + detalle
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

type vacio struct{}

// cuerpoPosiciones: el lote de posiciones no lleva Idempotency-Key (es idempotente por punto).
type cuerpoPosiciones struct {
	Body EntradaPosiciones
}

func responder[T any](v T, err error) (*cuerpo[T], error) {
	if err != nil {
		return nil, err
	}
	return &cuerpo[T]{Body: v}, nil
}

// registrarAccion publica POST /ordenes/{id}/<ruta>; fijar pone el id de la ruta en la entrada.
func registrarAccion[In any](api huma.API, op huma.Operation, ejecutar func(context.Context, In) (Orden, error), fijar func(*In, int64)) {
	huma.Register(api, op, func(ctx context.Context, in *conID[In]) (*cuerpo[Orden], error) {
		e := in.Body
		if err := sinNulos(e); err != nil {
			return nil, err // validación de entrada: Postgres no guarda NUL (ni en la auditoría)
		}
		fijar(&e, in.ID)
		return responder(ejecutar(ctx, e))
	})
}

// puedeVerTodas: con ordenes.ver se usan los casos según el alcance; si no, los de
// ordenes.ver_propias (técnico).
func puedeVerTodas(ctx context.Context) bool {
	return contexto.PermisosDe(ctx).Tiene(string(nucleo.PermOrdenesVer))
}

// Registrar publica las rutas de campo.
func (m *Modulo) Registrar(api huma.API) {
	tipos := operacion("listarTiposOrden", http.MethodGet, "/api/v1/tipos-orden", "Tipos de orden con sus plantillas de checklist", "", http.StatusOK)
	tipos.Description, tipos.Extensions = "Cualquier usuario autenticado (catálogo).", nil
	huma.Register(api, tipos,
		func(ctx context.Context, _ *vacio) (*cuerpo[nucleo.Pagina[TipoOrden]], error) {
			return responder(m.ListarTipos.Ejecutar(ctx, struct{}{}))
		})

	listar := operacion("listarOrdenes", http.MethodGet, "/api/v1/ordenes", "Listar órdenes de trabajo (tablero)", nucleo.PermOrdenesVer, http.StatusOK)
	listar.Description = "Permiso: `ordenes.ver` (según el alcance) u `ordenes.ver_propias` (solo las asignadas al actor)."
	huma.Register(api, listar, func(ctx context.Context, in *EntradaListarOrdenes) (*cuerpo[nucleo.Pagina[Orden]], error) {
		if puedeVerTodas(ctx) {
			return responder(m.ListarOrdenes.Ejecutar(ctx, *in))
		}
		return responder(m.ListarMisOrdenes.Ejecutar(ctx, *in))
	})

	crear := operacion("crearOrden", http.MethodPost, "/api/v1/ordenes", "Crear una orden de trabajo", nucleo.PermOrdenesCrear, http.StatusCreated)
	crear.Description += " Con `ticketId` la orden hereda los datos del ticket y deja una nota interna en él. Emite `orden.creada`."
	huma.Register(api, crear, func(ctx context.Context, in *conClave[EntradaCrearOrden]) (*cuerpo[Orden], error) {
		e := in.Body
		if err := sinNulos(e); err != nil {
			return nil, err
		}
		e.ClaveIdempotencia = in.IdempotencyKey
		return responder(m.CrearOrden.Ejecutar(ctx, e))
	})

	ver := operacion("verOrden", http.MethodGet, "/api/v1/ordenes/{id}", "Ver una orden con su checklist, notas, materiales, equipos, firma y adjuntos", nucleo.PermOrdenesVer, http.StatusOK, http.StatusNotFound)
	ver.Description = "Permiso: `ordenes.ver` u `ordenes.ver_propias`."
	huma.Register(api, ver, func(ctx context.Context, in *EntradaVerOrden) (*cuerpo[OrdenDetalle], error) {
		if puedeVerTodas(ctx) {
			return responder(m.VerOrden.Ejecutar(ctx, *in))
		}
		return responder(m.VerOrdenPropia.Ejecutar(ctx, *in))
	})

	eventos := operacion("listarEventosOrden", http.MethodGet, "/api/v1/ordenes/{id}/eventos", "Historial de la orden (estado, asignación, programación y GPS)", nucleo.PermOrdenesVer, http.StatusOK, http.StatusNotFound)
	eventos.Description = "Permiso: `ordenes.ver` u `ordenes.ver_propias`."
	huma.Register(api, eventos, func(ctx context.Context, in *EntradaListarEventos) (*cuerpo[nucleo.Pagina[EventoOrden]], error) {
		if puedeVerTodas(ctx) {
			return responder(m.ListarEventos.Ejecutar(ctx, *in))
		}
		return responder(m.ListarEventosPropia.Ejecutar(ctx, *in))
	})

	registrarAccion(api, accion("asignarOrden", "asignar", "Asignar una orden a un técnico o cuadrilla",
		"pendiente, programada o en_camino. Sin técnico vuelve a pendiente (libera la cita); otro técnico en una orden programada pasa por la agenda con la misma hora (422 como programar) y en camino la deja programada. Emite `orden.asignada` (y `orden.programada` si había cita).",
		nucleo.PermOrdenesAsignar), m.AsignarOrden.Ejecutar, func(e *EntradaAsignarOrden, id int64) { e.ID = id })
	registrarAccion(api, accion("programarOrden", "programar", "Programar una orden (técnico y fecha)",
		"pendiente → programada. La agenda valida la cita (ficha del técnico, turno, ausencia, solape, capacidad y ventana: 422 `tecnico_sin_agenda`, `fuera_de_turno`, `tecnico_ausente`, `solape`, `capacidad_excedida`, `fuera_de_ventana`) y emite `orden.programada` (y campo `orden.asignada` si cambia el técnico).",
		nucleo.PermOrdenesProgramar), m.ProgramarOrden.Ejecutar, func(e *EntradaProgramarOrden, id int64) { e.ID = id })
	registrarAccion(api, accion("reprogramarOrden", "reprogramar", "Reprogramar una orden",
		"programada, en_camino o fallida → programada, con motivo. Mismas validaciones de agenda que programar; sin ventana nueva se conserva la anterior si el nuevo inicio cae en ella. Emite `orden.programada`.",
		nucleo.PermOrdenesReprogramar), m.ReprogramarOrden.Ejecutar, func(e *EntradaProgramarOrden, id int64) { e.ID = id })
	registrarAccion(api, accion("iniciarCaminoOrden", "iniciar-camino", "Marcar la orden en camino",
		"programada → en_camino, con GPS opcional. Emite `orden.estado_cambiada`.",
		nucleo.PermOrdenesIniciarCamino), m.IniciarCamino.Ejecutar, func(e *EntradaPosicion, id int64) { e.ID, e.Sync = id, nil })
	registrarAccion(api, accion("llegarOrden", "llegar", "Marcar la llegada al sitio",
		"programada o en_camino → en_sitio, con GPS opcional. Emite `orden.estado_cambiada`.",
		nucleo.PermOrdenesLlegar), m.Llegar.Ejecutar, func(e *EntradaPosicion, id int64) { e.ID, e.Sync = id, nil })
	registrarAccion(api, accion("cerrarOrden", "cerrar", "Cerrar una orden",
		"en_sitio → cerrada. Exige el checklist obligatorio, los equipos del tipo y la firma si la plantilla la pide (422 `cierre_incompleto`) y los adjuntos subidos (422 `faltan_adjuntos`). Emite `orden.cerrada` (el núcleo hace los pendientes en Argos).",
		nucleo.PermOrdenesCerrar), m.CerrarOrden.Ejecutar, func(e *EntradaCerrarOrden, id int64) { e.ID, e.Sync = id, nil })
	registrarAccion(api, accion("fallarOrden", "fallar", "Marcar la orden como fallida",
		"programada, en_camino o en_sitio → fallida, con su motivo. Se puede reprogramar. Emite `orden.estado_cambiada`.",
		nucleo.PermOrdenesFallar), m.FallarOrden.Ejecutar, func(e *EntradaFallarOrden, id int64) { e.ID, e.Sync = id, nil })
	registrarAccion(api, accion("cancelarOrden", "cancelar", "Cancelar una orden",
		"Desde cualquier estado no final, con motivo. Emite `orden.estado_cambiada`.",
		nucleo.PermOrdenesCancelar), m.CancelarOrden.Ejecutar, func(e *EntradaMotivo, id int64) { e.ID = id })
	registrarAccion(api, accion("reabrirOrden", "reabrir", "Reabrir una orden fallida o cancelada",
		"fallida o cancelada → pendiente, con motivo (una cerrada no se reabre). Emite `orden.estado_cambiada`.",
		nucleo.PermOrdenesReabrir), m.ReabrirOrden.Ejecutar, func(e *EntradaMotivo, id int64) { e.ID = id })

	registrarAccion(api, accion("marcarChecklistOrden", "checklist", "Marcar un ítem del checklist",
		"Gana la marca más reciente (por hora del hecho); la otra queda en el historial (`aviso: checklist_no_vigente`).",
		nucleo.PermOrdenesRegistrar), m.MarcarChecklist.Ejecutar, func(e *EntradaMarcarChecklist, id int64) { e.ID, e.Sync = id, nil })
	registrarAccion(api, accion("agregarNotaOrden", "notas", "Agregar una nota a la orden",
		"En cualquier estado.", nucleo.PermOrdenesRegistrar), m.AgregarNota.Ejecutar, func(e *EntradaAgregarNota, id int64) { e.ID, e.Sync = id, nil })
	registrarAccion(api, accion("usarMaterialOrden", "materiales", "Registrar material usado",
		"Producto de Argos desde la bodega de la orden.", nucleo.PermOrdenesRegistrar), m.UsarMaterialPanel,
		func(e *EntradaUsarMaterial, id int64) { e.ID, e.Sync = id, nil })
	registrarAccion(api, accion("instalarEquipoOrden", "equipos-instalados", "Registrar un equipo instalado (comodato)",
		"Serie o unidad de Argos; MAC normalizada. 422 `equipo_duplicado`.", nucleo.PermOrdenesRegistrar), m.InstalarEquipo.Ejecutar,
		func(e *EntradaEquipo, id int64) { e.ID, e.Sync = id, nil })
	registrarAccion(api, accion("retirarEquipoOrden", "equipos-retirados", "Registrar un equipo retirado",
		"Con su condición (buena, danada o perdido: no se recuperó).", nucleo.PermOrdenesRegistrar), m.RetirarEquipoPanel,
		func(e *EntradaEquipo, id int64) { e.ID, e.Sync = id, nil })
	registrarAccion(api, accion("capturarFirmaOrden", "firma", "Capturar la firma del cliente",
		"Reemplaza la anterior. El SVG nunca sale por la API ni en la auditoría.", nucleo.PermOrdenesRegistrar), m.CapturarFirma.Ejecutar,
		func(e *EntradaCapturarFirma, id int64) { e.ID, e.Sync = id, nil })
	registrarAccion(api, accion("registrarAdjuntoOrden", "adjuntos", "Registrar un adjunto en la orden",
		"El adjunto se crea con `POST /adjuntos`; basta con que la subida exista. En cualquier estado.", nucleo.PermOrdenesRegistrar),
		m.RegistrarAdjunto.Ejecutar, func(e *EntradaRegistrarAdjunto, id int64) { e.ID, e.Sync = id, nil })

	// App de campo (contratos/sync.md).
	paquete := operacion("paqueteCampo", http.MethodGet, "/api/v1/campo/paquete", "Paquete del día del técnico", nucleo.PermCampoSincronizar, http.StatusOK)
	paquete.Description += " Órdenes asignadas al actor con la instantánea de cliente y servicio, checklist, comodato y existencias (si hay conector con Argos), la cita de la agenda (con la ventana acordada con el cliente), ruta (aún ausente) y el cursor de cambios."
	huma.Register(api, paquete, func(ctx context.Context, in *EntradaPaquete) (*cuerpo[Paquete], error) {
		return responder(m.Paquete(contexto.ConCanal(ctx, contexto.CanalCampo), *in))
	})

	cambios := operacion("cambiosCampo", http.MethodGet, "/api/v1/campo/cambios", "Cambios en las órdenes del técnico desde el cursor", nucleo.PermCampoSincronizar, http.StatusOK)
	cambios.Description += " Órdenes nuevas, actualizadas, reprogramadas, canceladas o reasignadas (sin orden: quitarla del teléfono). El cursor es del técnico y solo trae las órdenes de la empresa activa: al cambiar de empresa (X-Iris-Empresa), vuelve a pedir el paquete y usa su cursor."
	huma.Register(api, cambios, func(ctx context.Context, in *EntradaCambios) (*cuerpo[Cambios], error) {
		return responder(m.Cambios(contexto.ConCanal(ctx, contexto.CanalCampo), *in))
	})

	pos := operacion("registrarPosiciones", http.MethodPost, "/api/v1/campo/posiciones", "Lote de posiciones GPS del técnico (ubicación en vivo)", nucleo.PermCampoRegistrarPosiciones, http.StatusOK, http.StatusTooManyRequests)
	pos.Description += " Solo el técnico con sesión de la app de campo (`X-Iris-Canal: campo`; si no, 403 `requiere_sesion_campo`). Hasta 500 puntos; lotes por minuto limitados por técnico (429 `demasiadas_posiciones` con `Retry-After`). Puntos de más de 24 h, adelantados o más seguidos que el intervalo mínimo se descartan (se cuentan en `motivos`); un punto repetido (misma hora) no se duplica. Sin auditoría por fila."
	huma.Register(api, pos, func(ctx context.Context, in *cuerpoPosiciones) (*cuerpo[RespuestaPosiciones], error) {
		return responder(m.RegistrarPosiciones.Ejecutar(ctx, in.Body))
	})
	ultimas := operacion("ultimasPosiciones", http.MethodGet, "/api/v1/campo/posiciones/ultimas", "Última posición de cada técnico (despacho)", nucleo.PermCampoVerPosiciones, http.StatusOK)
	ultimas.Description += " De la empresa activa y según el alcance (cuadrilla del técnico o zonas que atiende su cuadrilla). Por defecto, las de las últimas 24 h."
	huma.Register(api, ultimas, func(ctx context.Context, in *EntradaUltimasPosiciones) (*cuerpo[UltimasPosiciones], error) {
		return responder(m.UltimasPosiciones.Ejecutar(ctx, *in))
	})

	lote := operacion("loteSync", http.MethodPost, "/api/v1/sync/lote", "Lote de operaciones de la app de campo", nucleo.PermCampoSincronizar, http.StatusOK)
	lote.Description += " Cada operación usa el permiso de su acción y es idempotente por `id`. Siempre 200 con un resultado por operación, en el mismo orden (aplicada, duplicada o rechazada con su problem+json). Cuerpo de hasta 4 MiB y 200 operaciones (una firma SVG ocupa hasta 200 000 caracteres)."
	lote.MaxBodyBytes = maxBytesLote
	huma.Register(api, lote, func(ctx context.Context, in *conClave[EntradaLote]) (*cuerpo[RespuestaLote], error) {
		return responder(m.Lote(ctx, in.Body))
	})
}

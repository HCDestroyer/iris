package mensajeria

import (
	"context"
	"net/http"
	"reflect"
	"strconv"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/google/uuid"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
)

// Rutas del chat (/api/v1/mensajeria/chat; contratos/chat.md).

type salidaConStatus[T any] struct {
	Status int
	Body   T
}

// otraRespuesta declara en el OpenAPI una respuesta exitosa más (p. ej. 201 junto al 200) con el
// mismo cuerpo T: huma solo documenta el cuerpo del status por defecto, y un status en Errors se
// documenta como Problema.
func otraRespuesta[T any](api huma.API, o *huma.Operation, status int, descripcion string) {
	if o.Responses == nil {
		o.Responses = map[string]*huma.Response{}
	}
	esquema := api.OpenAPI().Components.Schemas.Schema(reflect.TypeFor[T](), true, "")
	o.Responses[strconv.Itoa(status)] = &huma.Response{Description: descripcion,
		Content: map[string]*huma.MediaType{"application/json": {Schema: esquema}}}
}

func conStatus[T any](v T, nueva bool, err error) (*salidaConStatus[T], error) {
	if err != nil {
		return nil, err
	}
	s := http.StatusOK
	if nueva {
		s = http.StatusCreated
	}
	return &salidaConStatus[T]{Status: s, Body: v}, nil
}

type entradaAbrirDirectaHTTP struct {
	nucleo.ConIdempotencia
	Body EntradaAbrirDirecta
}

type entradaAbrirCuadrillaHTTP struct {
	nucleo.ConIdempotencia
	CuadrillaID int64 `path:"cuadrilla_id" minimum:"1"`
}

type entradaAbrirOrdenHTTP struct {
	nucleo.ConIdempotencia
	OrdenID int64 `path:"orden_id" minimum:"1"`
}

// CuerpoEnviarMensaje es el cuerpo de POST /chat/conversaciones/{id}/mensajes.
type CuerpoEnviarMensaje struct {
	ID        uuid.UUID      `json:"id" doc:"uuid v7 generado en el cliente al escribirlo (la cola sin conexión lo reenvía igual): el servidor es idempotente por él"`
	Texto     *string        `json:"texto,omitempty" maxLength:"4000" doc:"Texto plano (sin HTML ni Markdown: se guarda y se muestra tal cual). Se recortan los espacios de los extremos"`
	AdjuntoID *uuid.UUID     `json:"adjunto_id,omitempty" doc:"Una foto registrada antes en POST /api/v1/adjuntos con entidad chat_conversacion y entidadId = la conversación (puede seguir subiéndose)"`
	Ubicacion *UbicacionChat `json:"ubicacion,omitempty" doc:"Ubicación compartida"`
	CreadoEn  *time.Time     `json:"creado_en,omitempty" doc:"Hora del teléfono al escribirlo (RFC 3339; no futura)"`
	Menciones []int64        `json:"menciones,omitempty" maxItems:"20" doc:"Usuarios mencionados (@Nombre en el texto): deben estar en la conversación y no ser el autor. Cada uno recibe chat.mencion (push de tipo mencion)"`
}

// CuerpoAgregarMiembro es el cuerpo de POST /chat/conversaciones/{id}/miembros.
type CuerpoAgregarMiembro struct {
	UsuarioID int64 `json:"usuario_id" minimum:"1" doc:"A quién (persona activa de la empresa con chat.ver)"`
}

type entradaAgregarMiembroHTTP struct {
	nucleo.ConIdempotencia
	ID   int64 `path:"id" minimum:"1"`
	Body CuerpoAgregarMiembro
}

type entradaEnviarHTTP struct {
	nucleo.ConIdempotencia
	ID   int64 `path:"id" minimum:"1"`
	Body CuerpoEnviarMensaje
}

type salidaEnviar struct {
	Status   int
	Repetido string `header:"Idempotent-Replayed" doc:"true si el id ya se había recibido (el cuerpo trae el mensaje actual)"`
	Body     MensajeChat
}

type entradaEliminarHTTP struct {
	nucleo.ConIdempotencia
	ID string `path:"id" format:"uuid"`
}

// CuerpoLeerChat es el cuerpo de POST /chat/conversaciones/{id}/leer.
type CuerpoLeerChat struct {
	HastaMensajeID uuid.UUID `json:"hasta_mensaje_id" doc:"El último mensaje que viste; la lectura solo avanza"`
}

type entradaLeerHTTP struct {
	nucleo.ConIdempotencia
	ID   int64 `path:"id" minimum:"1"`
	Body CuerpoLeerChat
}

type entradaEventosHTTP struct {
	UltimoID string `header:"Last-Event-ID" maxLength:"20" doc:"El id del último evento recibido (lo manda EventSource al reconectar)"`
	Desde    string `query:"desde" maxLength:"20" doc:"Igual que Last-Event-ID, para clientes que no pueden fijar cabeceras. Sin los dos: desde ahora"`
}

func (m *Modulo) registrarChat(api huma.API) {
	const base = "/chat"
	lista := op("listarConversacionesChat", http.MethodGet, base+"/conversaciones", "Mis conversaciones del chat", nucleo.PermChatVer, http.StatusOK)
	lista.Description += " Las que eres miembro (directas, tu cuadrilla, tus órdenes, hilos a los que te uniste) y, con `chat.supervisar`, las de " +
		"cuadrilla y orden de tu alcance. De la más reciente a la más vieja, con vista previa y tus no leídos."
	huma.Register(api, lista, func(ctx context.Context, in *EntradaListarConversaciones) (*cuerpo[nucleo.Pagina[ConversacionChat]], error) {
		return responder(m.Chat.ListarConversaciones.Ejecutar(ctx, *in))
	})

	resumen := op("resumenChat", http.MethodGet, base+"/resumen", "Contadores del chat (ícono del encabezado y pestañas)", nucleo.PermChatVer, http.StatusOK)
	resumen.Description += " Sobre todas las conversaciones que ves en la empresa activa: no leídos, menciones sin leer, conversaciones con no leídos y " +
		"`requiere_accion` (no leídos de las directas + menciones en cuadrillas y órdenes), en total y por tipo."
	huma.Register(api, resumen, func(ctx context.Context, _ *struct{}) (*cuerpo[ResumenChat], error) {
		return responder(m.Chat.Resumen.Ejecutar(ctx, EntradaResumenChat{}))
	})

	ver := op("verConversacionChat", http.MethodGet, base+"/conversaciones/{id}", "Una conversación con las lecturas de sus miembros",
		nucleo.PermChatVer, http.StatusOK, http.StatusNotFound)
	huma.Register(api, ver, func(ctx context.Context, in *EntradaConversacion) (*cuerpo[ConversacionChatDetalle], error) {
		return responder(m.Chat.VerConversacion.Ejecutar(ctx, *in))
	})

	miembros := op("listarMiembrosChat", http.MethodGet, base+"/conversaciones/{id}/miembros", "Quiénes están en una conversación",
		nucleo.PermChatVer, http.StatusOK, http.StatusNotFound)
	miembros.Description += " Personas activas de la empresa que ven la conversación (tú incluido) y por qué: directa, tecnico, despacho, agregado o supervision. " +
		"Sin paginar (acotada). Sirve para el autocompletado de @menciones."
	huma.Register(api, miembros, func(ctx context.Context, in *EntradaConversacion) (*cuerpo[MiembrosChat], error) {
		return responder(m.Chat.ListarMiembros.Ejecutar(ctx, *in))
	})

	agregar := op("agregarMiembroChat", http.MethodPost, base+"/conversaciones/{id}/miembros", "Agregar a alguien al chat de una orden",
		nucleo.PermChatSupervisar, http.StatusOK, http.StatusForbidden, http.StatusNotFound)
	agregar.Description += " Con `chat.supervisar` que cubra la orden (403 `fuera_de_alcance`). Solo hilos de orden (422 `tipo_no_admite_miembros`) " +
		"que no estén cerrados (422 `conversacion_cerrada`). El agregado: persona activa de la empresa con `chat.ver` (422 `usuario_no_disponible`). " +
		"201 si se agregó (emite chat.miembro_agregado); 200 si ya lo veía. Responde los miembros."
	otraRespuesta[MiembrosChat](api, &agregar, http.StatusCreated, "Se agregó: los miembros con el nuevo")
	huma.Register(api, agregar, func(ctx context.Context, in *entradaAgregarMiembroHTTP) (*salidaConStatus[MiembrosChat], error) {
		r, err := m.Chat.AgregarMiembro.Ejecutar(ctx, EntradaAgregarMiembro{ConversacionID: in.ID, UsuarioID: in.Body.UsuarioID})
		return conStatus(r, r.Nuevo, err)
	})

	deOrden := op("conversacionDeOrdenChat", http.MethodGet, base+"/ordenes/{orden_id}/conversacion", "El chat de una orden, sin abrirlo",
		nucleo.PermChatVer, http.StatusOK, http.StatusNotFound)
	deOrden.Description += " Para la pestaña «Chat» de la orden: no lo crea ni te une. 404 si la orden no tiene chat o no lo ves; entonces " +
		"`POST /chat/ordenes/{orden_id}/abrir`."
	huma.Register(api, deOrden, func(ctx context.Context, in *EntradaConversacionDeOrden) (*cuerpo[ConversacionChatDetalle], error) {
		return responder(m.Chat.ConversacionDeOrden.Ejecutar(ctx, *in))
	})

	personas := op("personasChat", http.MethodGet, base+"/personas", "Personas con quien abrir una conversación directa", nucleo.PermChatEnviar, http.StatusOK)
	personas.Description += " «Nuevo mensaje» → Persona: personas activas de la empresa con `chat.ver`, sin ti, por nombre, con la directa que ya tienen."
	huma.Register(api, personas, func(ctx context.Context, in *EntradaPersonasChat) (*cuerpo[nucleo.Pagina[PersonaChat]], error) {
		return responder(m.Chat.Personas.Ejecutar(ctx, *in))
	})

	cuadrillas := op("cuadrillasChat", http.MethodGet, base+"/cuadrillas", "Cuadrillas cuyo chat puedes abrir", nucleo.PermChatEnviar, http.StatusOK)
	cuadrillas.Description += " «Nuevo mensaje» → Cuadrilla: la tuya (técnico) y las que cubre tu `chat.supervisar`, con técnicos, zonas y su chat si ya existe. Sin paginar (acotada)."
	huma.Register(api, cuadrillas, func(ctx context.Context, in *EntradaCuadrillasChat) (*cuerpo[CuadrillasChat], error) {
		return responder(m.Chat.Cuadrillas.Ejecutar(ctx, *in))
	})

	directa := op("abrirDirectaChat", http.MethodPost, base+"/directas", "Abrir (o traer) la conversación directa con un usuario",
		nucleo.PermChatEnviar, http.StatusOK)
	directa.Description += " 201 si se creó, 200 si ya existía. 422 `usuario_no_disponible` si el otro no existe, no está activo, no opera en la empresa activa o no tiene `chat.ver`."
	otraRespuesta[ConversacionChatDetalle](api, &directa, http.StatusCreated, "Se creó la conversación")
	huma.Register(api, directa, func(ctx context.Context, in *entradaAbrirDirectaHTTP) (*salidaConStatus[ConversacionChatDetalle], error) {
		r, err := m.Chat.AbrirDirecta.Ejecutar(ctx, in.Body)
		return conStatus(r.ConversacionChatDetalle, r.Nueva, err)
	})

	cuad := op("abrirCuadrillaChat", http.MethodPost, base+"/cuadrillas/{cuadrilla_id}/abrir", "Abrir (o traer) el chat de una cuadrilla",
		nucleo.PermChatEnviar, http.StatusOK, http.StatusNotFound)
	cuad.Description += " Uno por cuadrilla y empresa. Lo abre un técnico de la cuadrilla o quien la supervisa (`chat.supervisar`); si no, 404."
	otraRespuesta[ConversacionChatDetalle](api, &cuad, http.StatusCreated, "Se creó la conversación")
	huma.Register(api, cuad, func(ctx context.Context, in *entradaAbrirCuadrillaHTTP) (*salidaConStatus[ConversacionChatDetalle], error) {
		r, err := m.Chat.AbrirCuadrilla.Ejecutar(ctx, EntradaAbrirCuadrilla{CuadrillaID: in.CuadrillaID})
		return conStatus(r.ConversacionChatDetalle, r.Nueva, err)
	})

	orden := op("abrirOrdenChat", http.MethodPost, base+"/ordenes/{orden_id}/abrir", "Abrir (o traer) el hilo de una orden",
		nucleo.PermChatEnviar, http.StatusOK, http.StatusNotFound)
	orden.Description += " Uno por orden. Lo abre su técnico, quien la supervisa (`chat.supervisar`) o despacho (`ordenes.ver` con alcance sobre la orden), " +
		"que queda como miembro. 201 si se creó o te uniste; 200 si ya estabas."
	otraRespuesta[ConversacionChatDetalle](api, &orden, http.StatusCreated, "Se creó el hilo o te uniste")
	huma.Register(api, orden, func(ctx context.Context, in *entradaAbrirOrdenHTTP) (*salidaConStatus[ConversacionChatDetalle], error) {
		r, err := m.Chat.AbrirOrden.Ejecutar(ctx, EntradaAbrirOrden{OrdenID: in.OrdenID})
		return conStatus(r.ConversacionChatDetalle, r.Nueva, err)
	})

	mensajes := op("listarMensajesChat", http.MethodGet, base+"/conversaciones/{id}/mensajes", "Mensajes de una conversación",
		nucleo.PermChatVer, http.StatusOK, http.StatusNotFound)
	mensajes.Description += " Siempre en orden de `seq` ascendente. `direccion=anteriores` (por defecto) pagina hacia atrás desde el más reciente; " +
		"`siguientes`, hacia adelante. `nextCursor` sigue en la misma dirección. `desde_seq` sirve de ancla sin cursor."
	huma.Register(api, mensajes, func(ctx context.Context, in *EntradaListarMensajes) (*cuerpo[nucleo.Pagina[MensajeChat]], error) {
		return responder(m.Chat.ListarMensajes.Ejecutar(ctx, *in))
	})

	enviar := op("enviarMensajeChat", http.MethodPost, base+"/conversaciones/{id}/mensajes", "Enviar un mensaje",
		nucleo.PermChatEnviar, http.StatusCreated, http.StatusNotFound, http.StatusConflict, http.StatusTooManyRequests)
	enviar.Description += " Idempotente por `id` (uuid v7 del cliente): reenviarlo con el mismo contenido responde 200 con el mensaje actual " +
		"(`Idempotent-Replayed: true`); con otro contenido, 409 `mensaje_id_reutilizado`. Acepta además `Idempotency-Key` " +
		"(la aplica el caso: la misma clave con otro `id`, 422 `idempotencia_reutilizada`; no se guarda la respuesta). " +
		"Texto hasta 4000 caracteres, una foto y/o una ubicación (al menos uno: 422 `mensaje_vacio`). 30 por minuto por usuario (429 `demasiados_mensajes` con `Retry-After`). " +
		"`menciones`: hasta 20, de quienes están en la conversación (422 `mencion_invalida`, `mencion_sin_texto`). Hilo de una orden cerrada, fallida o cancelada: 422 `conversacion_cerrada`."
	otraRespuesta[MensajeChat](api, &enviar, http.StatusOK, "Reenvío del mismo id: el mensaje tal como está ahora")
	huma.Register(api, enviar, func(ctx context.Context, in *entradaEnviarHTTP) (*salidaEnviar, error) {
		e := EntradaEnviarMensaje{ConversacionID: in.ID, ID: in.Body.ID, Texto: in.Body.Texto, AdjuntoID: in.Body.AdjuntoID,
			Ubicacion: in.Body.Ubicacion, CreadoEn: in.Body.CreadoEn, ConTexto: in.Body.Texto != nil, ConUbicacion: in.Body.Ubicacion != nil,
			Menciones: in.Body.Menciones, IdempotencyKey: in.IdempotencyKey}
		r, err := m.Chat.EnviarMensaje.Ejecutar(ctx, e)
		if err != nil {
			return nil, err
		}
		s := &salidaEnviar{Status: http.StatusCreated, Body: r.MensajeChat}
		if r.Repetido {
			s.Status, s.Repetido = http.StatusOK, "true"
		}
		return s, nil
	})

	eliminar := op("eliminarMensajeChat", http.MethodPost, base+"/mensajes/{id}/eliminar", "Eliminar un mensaje propio",
		nucleo.PermChatEnviar, http.StatusOK, http.StatusNotFound)
	eliminar.Description += " Solo su autor y hasta 15 minutos después de enviarlo (403 `no_es_autor`, 422 `plazo_vencido`). Borra el texto, la foto y la ubicación " +
		"y deja la lápida («Mensaje eliminado»). Repetirlo devuelve la misma lápida."
	huma.Register(api, eliminar, func(ctx context.Context, in *entradaEliminarHTTP) (*cuerpo[MensajeChat], error) {
		id, err := uuid.Parse(in.ID)
		if err != nil {
			return nil, nucleo.NoEncontrado("")
		}
		r, err := m.Chat.EliminarMensaje.Ejecutar(ctx, EntradaEliminarMensaje{ID: id})
		return responder(r.MensajeChat, err)
	})

	leer := op("leerConversacionChat", http.MethodPost, base+"/conversaciones/{id}/leer", "Marcar leído hasta un mensaje",
		nucleo.PermChatVer, http.StatusOK, http.StatusNotFound)
	leer.Description += " La lectura solo avanza (marcar uno anterior no la retrocede). Responde tus no leídos."
	huma.Register(api, leer, func(ctx context.Context, in *entradaLeerHTTP) (*cuerpo[LecturaPropia], error) {
		return responder(m.Chat.MarcarLeido.Ejecutar(ctx, EntradaMarcarLeido{ConversacionID: in.ID, HastaMensajeID: in.Body.HastaMensajeID}))
	})

	cambios := op("cambiosChat", http.MethodGet, base+"/cambios", "Cambios del chat desde un cursor (sondeo)", nucleo.PermChatVer,
		http.StatusOK, http.StatusGone)
	cambios.Description += " La alternativa a SSE para la app (mismo cursor que el `id` de SSE). Sin cursor responde la cabeza actual sin eventos. " +
		"410 `cursor_vencido` si el cursor es anterior a la retención (7 días): recargar las conversaciones y seguir sin cursor."
	huma.Register(api, cambios, func(ctx context.Context, in *EntradaCambios) (*cuerpo[CambiosChat], error) {
		return responder(m.Chat.Cambios.Ejecutar(ctx, *in))
	})

	eventos := op("eventosChat", http.MethodGet, base+"/eventos", "Tiempo real del chat (Server-Sent Events)", nucleo.PermChatVer,
		http.StatusOK, http.StatusGone, http.StatusTooManyRequests, http.StatusServiceUnavailable)
	eventos.Description += " `text/event-stream`: `id` (cursor), `event` (`mensaje_nuevo`, `mensaje_eliminado`, `leido`, `conversacion_actualizada`) y " +
		"`data` (el mismo JSON que un elemento de GET /chat/cambios). Reanuda con `Last-Event-ID` (o `?desde=`); comentario `: latido` cada 25 s; " +
		"el servidor cierra a los 30 min y el cliente se reconecta. Sesión del panel (cookie) o Bearer: en cada latido y antes de entregar " +
		"se revalida (sesión cerrada, token revocado, usuario inactivo o sin chat.ver: se cierra). Hasta 5 flujos por usuario (el sexto, " +
		"429 `demasiados_flujos`) y un tope por proceso (503 `flujos_agotados`, IRIS_CHAT_SSE_MAX)."
	eventos.Responses = map[string]*huma.Response{"200": {
		Description: "Flujo de eventos",
		Content:     map[string]*huma.MediaType{"text/event-stream": {Schema: &huma.Schema{Type: huma.TypeString}}},
	}}
	huma.Register(api, eventos, func(ctx context.Context, in *entradaEventosHTTP) (*huma.StreamResponse, error) {
		desde := in.UltimoID
		if desde == "" {
			desde = in.Desde
		}
		f, sub, quitar, err := m.abrirFlujoSSE(ctx, desde)
		if err != nil {
			return nil, err
		}
		return &huma.StreamResponse{Body: func(hctx huma.Context) {
			r, w := humachi.Unwrap(hctx)
			if r == nil || w == nil {
				quitar()
				return
			}
			m.transmitir(r.Context(), w, f, sub, quitar)
		}}, nil
	})
}

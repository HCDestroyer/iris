package mensajeria

import (
	"context"
	"net/http"
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

	ver := op("verConversacionChat", http.MethodGet, base+"/conversaciones/{id}", "Una conversación con las lecturas de sus miembros",
		nucleo.PermChatVer, http.StatusOK, http.StatusNotFound)
	huma.Register(api, ver, func(ctx context.Context, in *EntradaConversacion) (*cuerpo[ConversacionChatDetalle], error) {
		return responder(m.Chat.VerConversacion.Ejecutar(ctx, *in))
	})

	directa := op("abrirDirectaChat", http.MethodPost, base+"/directas", "Abrir (o traer) la conversación directa con un usuario",
		nucleo.PermChatEnviar, http.StatusOK, http.StatusCreated)
	directa.Description += " 201 si se creó, 200 si ya existía. 422 `usuario_no_disponible` si el otro no existe, no está activo, no opera en la empresa activa o no tiene `chat.ver`."
	huma.Register(api, directa, func(ctx context.Context, in *entradaAbrirDirectaHTTP) (*salidaConStatus[ConversacionChatDetalle], error) {
		r, err := m.Chat.AbrirDirecta.Ejecutar(ctx, in.Body)
		return conStatus(r.ConversacionChatDetalle, r.Nueva, err)
	})

	cuad := op("abrirCuadrillaChat", http.MethodPost, base+"/cuadrillas/{cuadrilla_id}/abrir", "Abrir (o traer) el chat de una cuadrilla",
		nucleo.PermChatEnviar, http.StatusOK, http.StatusCreated, http.StatusNotFound)
	cuad.Description += " Uno por cuadrilla y empresa. Lo abre un técnico de la cuadrilla o quien la supervisa (`chat.supervisar`); si no, 404."
	huma.Register(api, cuad, func(ctx context.Context, in *entradaAbrirCuadrillaHTTP) (*salidaConStatus[ConversacionChatDetalle], error) {
		r, err := m.Chat.AbrirCuadrilla.Ejecutar(ctx, EntradaAbrirCuadrilla{CuadrillaID: in.CuadrillaID})
		return conStatus(r.ConversacionChatDetalle, r.Nueva, err)
	})

	orden := op("abrirOrdenChat", http.MethodPost, base+"/ordenes/{orden_id}/abrir", "Abrir (o traer) el hilo de una orden",
		nucleo.PermChatEnviar, http.StatusOK, http.StatusCreated, http.StatusNotFound)
	orden.Description += " Uno por orden. Lo abre su técnico, quien la supervisa (`chat.supervisar`) o despacho (`ordenes.ver` con alcance sobre la orden), " +
		"que queda como miembro. 201 si se creó o te uniste; 200 si ya estabas."
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
		"(`Idempotent-Replayed: true`); con otro contenido, 409 `mensaje_id_reutilizado`. Acepta además `Idempotency-Key`. " +
		"Texto hasta 4000 caracteres, una foto y/o una ubicación (al menos uno: 422 `mensaje_vacio`). 30 por minuto por usuario (429 `demasiados_mensajes` con `Retry-After`)."
	enviar.Responses = map[string]*huma.Response{"200": {Description: "Reenvío del mismo id: el mensaje tal como está ahora"}}
	huma.Register(api, enviar, func(ctx context.Context, in *entradaEnviarHTTP) (*salidaEnviar, error) {
		e := EntradaEnviarMensaje{ConversacionID: in.ID, ID: in.Body.ID, Texto: in.Body.Texto, AdjuntoID: in.Body.AdjuntoID,
			Ubicacion: in.Body.Ubicacion, CreadoEn: in.Body.CreadoEn, ConTexto: in.Body.Texto != nil, ConUbicacion: in.Body.Ubicacion != nil}
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
		http.StatusOK, http.StatusGone)
	eventos.Description += " `text/event-stream`: `id` (cursor), `event` (`mensaje_nuevo`, `mensaje_eliminado`, `leido`, `conversacion_actualizada`) y " +
		"`data` (el mismo JSON que un elemento de GET /chat/cambios). Reanuda con `Last-Event-ID` (o `?desde=`); comentario `: latido` cada 25 s; " +
		"el servidor cierra a los 30 min y el cliente se reconecta. Sesión del panel (cookie) o Bearer."
	eventos.Responses = map[string]*huma.Response{"200": {
		Description: "Flujo de eventos",
		Content:     map[string]*huma.MediaType{"text/event-stream": {Schema: &huma.Schema{Type: huma.TypeString}}},
	}}
	huma.Register(api, eventos, func(ctx context.Context, in *entradaEventosHTTP) (*huma.StreamResponse, error) {
		desde := in.UltimoID
		if desde == "" {
			desde = in.Desde
		}
		f, err := m.Chat.abrirFlujo.Ejecutar(ctx, entradaFlujo{Desde: desde})
		if err != nil {
			return nil, err
		}
		return &huma.StreamResponse{Body: func(hctx huma.Context) {
			r, w := humachi.Unwrap(hctx)
			if r == nil || w == nil {
				return
			}
			m.transmitir(r.Context(), w, f)
		}}, nil
	})
}

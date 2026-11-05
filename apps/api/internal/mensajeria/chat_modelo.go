package mensajeria

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	mensajeriabd "github.com/HCDestroyer/iris/apps/api/internal/mensajeria/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/adjuntos"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
)

// Chat interno del equipo (contratos/chat.md): conversaciones directas, de cuadrilla y de orden.

// Tipos de conversación.
const (
	TipoDirecta   = "directa"
	TipoCuadrilla = "cuadrilla"
	TipoOrden     = "orden"
)

// Tipos de evento del feed (SSE y GET /chat/cambios).
const (
	EventoMensajeNuevo            = "mensaje_nuevo"
	EventoMensajeEliminado        = "mensaje_eliminado"
	EventoLeido                   = "leido"
	EventoConversacionActualizada = "conversacion_actualizada"
)

// CanalChat es el canal de LISTEN/NOTIFY del feed: la carga es la empresa («0» = sin empresa).
const CanalChat = "iris_chat"

// EntidadAdjuntosChat es la conversación en el módulo de adjuntos (fotos de los mensajes).
const EntidadAdjuntosChat = "chat_conversacion"

// MaxTextoChat: caracteres de un mensaje.
const MaxTextoChat = 4000

// largoVistaPrevia: caracteres del último mensaje en la lista de conversaciones.
const largoVistaPrevia = 140

func init() {
	nucleo.RegistrarTablaConEmpresa("mensajeria.chat_conversaciones")
	nucleo.RegistrarTablaConEmpresa("mensajeria.chat_mensajes")
	nucleo.RegistrarTablaConEmpresa("mensajeria.chat_eventos")
}

// UsuarioChat es un usuario tal como lo muestra el chat.
type UsuarioChat struct {
	ID     int64  `json:"id"`
	Nombre string `json:"nombre"`
}

// VistaPreviaChat es el último mensaje de una conversación.
type VistaPreviaChat struct {
	ID          uuid.UUID `json:"id"`
	AutorID     int64     `json:"autor_id"`
	AutorNombre string    `json:"autor_nombre"`
	Contenido   string    `json:"contenido" enum:"texto,foto,ubicacion,eliminado" doc:"Qué es: con texto, texto (aunque traiga foto)"`
	Texto       *string   `json:"texto" nullable:"true" doc:"Primeros 140 caracteres (texto plano); null si no tiene texto o se eliminó"`
	EnviadoEn   time.Time `json:"enviado_en"`
}

// ConversacionChat es una conversación vista por un usuario (con sus no leídos).
type ConversacionChat struct {
	ID          int64        `json:"id"`
	Tipo        string       `json:"tipo" enum:"directa,cuadrilla,orden"`
	EmpresaID   *int64       `json:"empresa_id" nullable:"true" doc:"Empresa de Argos (null sin conector)"`
	Titulo      string       `json:"titulo" doc:"Directa: el otro usuario; cuadrilla: su nombre; orden: su número"`
	CuadrillaID *int64       `json:"cuadrilla_id" nullable:"true"`
	OrdenID     *int64       `json:"orden_id" nullable:"true"`
	OrdenEstado *string      `json:"orden_estado" nullable:"true" doc:"Estado actual de la orden (hilo de orden)"`
	OtroUsuario *UsuarioChat `json:"otro_usuario,omitempty" doc:"Directa: con quién"`
	// Ultimo es la vista previa del último mensaje (null si no hay).
	Ultimo              *VistaPreviaChat `json:"ultimo_mensaje,omitempty" doc:"Ausente si aún no hay mensajes"`
	NoLeidos            int64            `json:"no_leidos" doc:"Mensajes de otros, sin eliminar, después de tu lectura"`
	LeidoHastaMensajeID *uuid.UUID       `json:"leido_hasta_mensaje_id" nullable:"true"`
	UltimaActividad     time.Time        `json:"ultima_actividad"`
	CreadaEn            time.Time        `json:"creada_en"`
}

// LecturaMiembroChat es hasta dónde leyó un usuario (el «visto»).
type LecturaMiembroChat struct {
	UsuarioID      int64     `json:"usuario_id"`
	Nombre         string    `json:"nombre"`
	HastaMensajeID uuid.UUID `json:"hasta_mensaje_id"`
	HastaSeq       int64     `json:"hasta_seq"`
	LeidoEn        time.Time `json:"leido_en"`
}

// ConversacionChatDetalle es una conversación con las lecturas de sus miembros.
type ConversacionChatDetalle struct {
	ConversacionChat
	Lecturas []LecturaMiembroChat `json:"lecturas" doc:"Hasta dónde leyó cada usuario que ya leyó algo (para «visto»)"`
}

// UbicacionChat compartida en un mensaje.
type UbicacionChat struct {
	Lat float64 `json:"lat" minimum:"-90" maximum:"90"`
	Lon float64 `json:"lon" minimum:"-180" maximum:"180"`
}

// AdjuntoChat es la foto de un mensaje (módulo de adjuntos).
type AdjuntoChat struct {
	ID          uuid.UUID `json:"id"`
	Estado      string    `json:"estado" enum:"pendiente,subiendo,completo,rechazado" doc:"La foto puede seguir subiéndose: se muestra al estar completo"`
	URLDescarga string    `json:"url_descarga"`
}

// MensajeChat es un mensaje del chat. Nunca se edita; eliminado = lápida (sin texto, foto ni ubicación).
type MensajeChat struct {
	ID             uuid.UUID      `json:"id" doc:"uuid v7 generado por el cliente"`
	ConversacionID int64          `json:"conversacion_id"`
	Seq            int64          `json:"seq" doc:"Orden en la conversación (lo asigna el servidor al recibirlo)"`
	AutorID        int64          `json:"autor_id"`
	AutorNombre    string         `json:"autor_nombre"`
	Texto          *string        `json:"texto" nullable:"true" doc:"Texto plano: nunca se interpreta como HTML"`
	Adjunto        *AdjuntoChat   `json:"adjunto,omitempty" doc:"La foto (ausente si no tiene)"`
	Ubicacion      *UbicacionChat `json:"ubicacion,omitempty" doc:"Ausente si no comparte ubicación"`
	CreadoEn       *time.Time     `json:"creado_en" nullable:"true" doc:"Hora del teléfono al escribirlo (cola sin conexión)"`
	EnviadoEn      time.Time      `json:"enviado_en" doc:"Hora del servidor al recibirlo"`
	Eliminado      bool           `json:"eliminado" doc:"true: mostrar «Mensaje eliminado»"`
	EliminadoEn    *time.Time     `json:"eliminado_en" nullable:"true"`
}

// LecturaPropia es la respuesta de marcar leído.
type LecturaPropia struct {
	ConversacionID      int64      `json:"conversacion_id"`
	LeidoHastaMensajeID *uuid.UUID `json:"leido_hasta_mensaje_id" nullable:"true"`
	NoLeidos            int64      `json:"no_leidos"`
}

// EventoChat es un cambio del feed: el mismo cuerpo en SSE (data) y en GET /chat/cambios.
type EventoChat struct {
	ID             string            `json:"id" doc:"Cursor del feed (el id de SSE / Last-Event-ID)"`
	Tipo           string            `json:"tipo" enum:"mensaje_nuevo,mensaje_eliminado,leido,conversacion_actualizada"`
	ConversacionID int64             `json:"conversacion_id"`
	OcurridoEn     time.Time         `json:"ocurrido_en"`
	NoLeidos       int64             `json:"no_leidos" doc:"Tus no leídos de la conversación al leer el evento"`
	Mensaje        *MensajeChat      `json:"mensaje,omitempty" doc:"mensaje_nuevo y mensaje_eliminado (estado actual: si ya se eliminó, la lápida)"`
	Lectura        *LecturaChat      `json:"lectura,omitempty" doc:"leido"`
	Conversacion   *ConversacionChat `json:"conversacion,omitempty" doc:"conversacion_actualizada"`
}

// LecturaChat es el «visto» de un usuario en el feed.
type LecturaChat struct {
	UsuarioID      int64     `json:"usuario_id"`
	HastaMensajeID uuid.UUID `json:"hasta_mensaje_id"`
	LeidoEn        time.Time `json:"leido_en"`
}

// Alcance ----------------------------------------------------------------------------------------

// alcanceChat: quién mira y qué empresa (mismas reglas que agenda y campo) y su alcance de
// chat.supervisar.
type alcanceChat struct {
	usuario    int64
	empresa    *int64
	todas      bool
	global     bool
	zonas      []int64
	cuadrillas []int64
	// Alcance de ordenes.ver (despacho unido al hilo de una orden).
	ordGlobal     bool
	ordZonas      []int64
	ordCuadrillas []int64
}

func alcanceDe(ctx context.Context) (alcanceChat, error) {
	actor := contexto.ActorDe(ctx)
	if !actor.EsUsuario() {
		return alcanceChat{}, nucleo.Prohibido("El chat es de los usuarios de Iris")
	}
	a := alcanceChat{usuario: actor.ID, zonas: []int64{}, cuadrillas: []int64{}, ordZonas: []int64{}, ordCuadrillas: []int64{}}
	switch contexto.EmpresasDe(ctx).Estado {
	case contexto.EmpresaActiva:
		id := contexto.EmpresaDe(ctx)
		a.empresa = &id
	case contexto.EmpresaSinConector:
	case contexto.EmpresaNoResuelta, contexto.EmpresaPorElegir, contexto.EmpresaSinAsignar:
		// Un usuario siempre llega con la empresa resuelta (auditoria.Caso corta lo demás).
		cero := int64(0)
		a.empresa = &cero
	}
	p := contexto.PermisosDe(ctx)
	if p.Tiene(string(nucleo.PermChatSupervisar)) {
		g, z, c := p.Alcances(string(nucleo.PermChatSupervisar))
		a.global = g
		a.zonas = append(a.zonas, z...)
		a.cuadrillas = append(a.cuadrillas, c...)
	}
	// El despacho «unido» al hilo de una orden la sigue viendo mientras ordenes.ver la cubra.
	if p.Tiene(string(nucleo.PermOrdenesVer)) {
		g, z, c := p.Alcances(string(nucleo.PermOrdenesVer))
		a.ordGlobal = g
		a.ordZonas = append(a.ordZonas, z...)
		a.ordCuadrillas = append(a.ordCuadrillas, c...)
	}
	return a, nil
}

func (a alcanceChat) clave() string {
	if a.empresa == nil {
		return "0"
	}
	return strconv.FormatInt(*a.empresa, 10)
}

func (a alcanceChat) empresaVisible(empresa *int64) bool {
	return a.todas || mismoID(a.empresa, empresa)
}

func mismoID(a, b *int64) bool { return (a == nil && b == nil) || (a != nil && b != nil && *a == *b) }

func (a alcanceChat) cubre(zona, cuadrilla *int64) bool {
	switch {
	case a.global:
		return true
	case zona != nil && contiene(a.zonas, *zona):
		return true
	case cuadrilla != nil && contiene(a.cuadrillas, *cuadrilla):
		return true
	}
	return false
}

func contiene(xs []int64, v int64) bool {
	for _, x := range xs {
		if x == v {
			return true
		}
	}
	return false
}

func (m *Modulo) visible(ctx context.Context, q *mensajeriabd.Queries, a alcanceChat, conversacion int64) (bool, error) {
	return q.ChatVisible(ctx, mensajeriabd.ChatVisibleParams{Usuario: a.usuario, Empresa: a.empresa, Todas: a.todas, SupGlobal: a.global,
		SupZonas: a.zonas, SupCuadrillas: a.cuadrillas, OrdGlobal: a.ordGlobal, OrdZonas: a.ordZonas, OrdCuadrillas: a.ordCuadrillas, ConversacionID: conversacion})
}

// conversacionVisible lee la conversación (con FOR UPDATE si bloquear) o 404 si no existe o el
// actor no la ve (no revela que existe).
func (m *Modulo) conversacionVisible(ctx context.Context, q *mensajeriabd.Queries, a alcanceChat, id int64, bloquear bool) (mensajeriabd.MensajeriaChatConversacion, error) {
	ok, err := m.visible(ctx, q, a, id)
	if err != nil {
		return mensajeriabd.MensajeriaChatConversacion{}, err
	}
	if !ok {
		return mensajeriabd.MensajeriaChatConversacion{}, conversacionNoEncontrada()
	}
	if bloquear {
		return q.ConversacionParaActualizar(ctx, id)
	}
	return q.ConversacionPorID(ctx, id)
}

// Errores ----------------------------------------------------------------------------------------

func conversacionNoEncontrada() *nucleo.Problema {
	return nucleo.NoEncontrado("La conversación no existe o no eres miembro")
}

func campoChat(campo, mensaje, regla string) *nucleo.Problema {
	return nucleo.Validacion(nucleo.ErrorCampo{Field: campo, Message: mensaje, Rule: regla})
}

// Conversión -------------------------------------------------------------------------------------

func uuidDe(u pgtype.UUID) *uuid.UUID {
	if !u.Valid {
		return nil
	}
	v := uuid.UUID(u.Bytes)
	return &v
}

func pgUUID(u *uuid.UUID) pgtype.UUID {
	if u == nil {
		return pgtype.UUID{}
	}
	return pgtype.UUID{Bytes: *u, Valid: true}
}

func aMensaje(f mensajeriabd.MensajeriaChatMensaje, autor string, adjuntoEstado *string) MensajeChat {
	msj := MensajeChat{ID: f.ID, ConversacionID: f.ConversacionID, Seq: f.Seq, AutorID: f.AutorID, AutorNombre: autor, Texto: f.Texto,
		CreadoEn: f.CreadoEn, EnviadoEn: f.EnviadoEn, Eliminado: f.EliminadoEn != nil, EliminadoEn: f.EliminadoEn}
	if id := uuidDe(f.AdjuntoID); id != nil {
		estado := adjuntos.EstadoPendiente
		if adjuntoEstado != nil {
			estado = *adjuntoEstado
		}
		msj.Adjunto = &AdjuntoChat{ID: *id, Estado: estado, URLDescarga: adjuntos.RutaDescarga(*id)}
	}
	if f.Lat != nil && f.Lon != nil {
		msj.Ubicacion = &UbicacionChat{Lat: *f.Lat, Lon: *f.Lon}
	}
	return msj
}

// resumir arma las conversaciones (en el orden de ids) tal como las ve el usuario.
func resumir(ctx context.Context, q *mensajeriabd.Queries, usuario int64, ids []int64) ([]ConversacionChat, error) {
	if len(ids) == 0 {
		return []ConversacionChat{}, nil
	}
	filas, err := q.ResumenConversaciones(ctx, mensajeriabd.ResumenConversacionesParams{Usuario: usuario, Ids: ids})
	if err != nil {
		return nil, err
	}
	var personas []int64
	for _, f := range filas {
		if f.OtroID != 0 {
			personas = append(personas, f.OtroID)
		}
		if f.UltimoAutorID != nil {
			personas = append(personas, *f.UltimoAutorID)
		}
	}
	nombres := map[int64]string{}
	if len(personas) > 0 {
		ns, err := q.NombresUsuarios(ctx, personas)
		if err != nil {
			return nil, err
		}
		for _, n := range ns {
			nombres[n.ID] = n.Nombre
		}
	}
	porID := make(map[int64]ConversacionChat, len(filas))
	for _, f := range filas {
		c := f.MensajeriaChatConversacion
		v := ConversacionChat{ID: c.ID, Tipo: c.Tipo, EmpresaID: c.EmpresaID, CuadrillaID: c.CuadrillaID, OrdenID: c.OrdenID, OrdenEstado: f.OrdenEstado,
			NoLeidos: f.NoLeidos, LeidoHastaMensajeID: uuidDe(f.LeidoHastaMensajeID), UltimaActividad: c.UltimaActividad, CreadaEn: c.CreatedAt}
		switch c.Tipo {
		case TipoDirecta:
			if f.OtroID != 0 {
				v.OtroUsuario = &UsuarioChat{ID: f.OtroID, Nombre: nombres[f.OtroID]}
				v.Titulo = nombres[f.OtroID]
			}
		case TipoCuadrilla:
			if f.CuadrillaNombre != nil {
				v.Titulo = *f.CuadrillaNombre
			}
		case TipoOrden:
			if f.OrdenNumero != nil {
				v.Titulo = *f.OrdenNumero
			}
		}
		if id := uuidDe(c.UltimoMensajeID); id != nil && f.UltimoAutorID != nil && f.UltimoEnviadoEn != nil {
			p := &VistaPreviaChat{ID: *id, AutorID: *f.UltimoAutorID, AutorNombre: nombres[*f.UltimoAutorID], EnviadoEn: *f.UltimoEnviadoEn}
			switch {
			case f.UltimoEliminadoEn != nil:
				p.Contenido = "eliminado"
			case f.UltimoTexto != nil:
				p.Contenido = "texto"
				t := recortar(*f.UltimoTexto, largoVistaPrevia)
				p.Texto = &t
			case uuidDe(f.UltimoAdjuntoID) != nil:
				p.Contenido = "foto"
			default:
				p.Contenido = "ubicacion"
			}
			v.Ultimo = p
		}
		porID[c.ID] = v
	}
	salida := make([]ConversacionChat, 0, len(ids))
	for _, id := range ids {
		if v, ok := porID[id]; ok {
			salida = append(salida, v)
		}
	}
	return salida, nil
}

func resumirUna(ctx context.Context, q *mensajeriabd.Queries, usuario, id int64) (ConversacionChat, error) {
	cs, err := resumir(ctx, q, usuario, []int64{id})
	if err != nil {
		return ConversacionChat{}, err
	}
	if len(cs) == 0 {
		return ConversacionChat{}, conversacionNoEncontrada()
	}
	return cs[0], nil
}

// Feed --------------------------------------------------------------------------------------------

// emitirEventoChat agrega el cambio al feed. Toma el candado de la empresa hasta el COMMIT: así los
// ids del feed de una empresa quedan en orden de COMMIT (un cursor nunca salta un evento que confirma
// tarde). Va al final de la transacción. El NOTIFY sale con el COMMIT.
func emitirEventoChat(ctx context.Context, t *auditoria.Tx, c mensajeriabd.MensajeriaChatConversacion, tipo string, mensaje *uuid.UUID, usuario *int64, ahora time.Time) error {
	q := mensajeriabd.New(t)
	clave := "0"
	if c.EmpresaID != nil {
		clave = strconv.FormatInt(*c.EmpresaID, 10)
	}
	if err := q.CandadoChat(ctx, "chat:eventos:"+clave); err != nil {
		return err
	}
	if _, err := q.InsertarEventoChat(ctx, mensajeriabd.InsertarEventoChatParams{EmpresaID: c.EmpresaID, ConversacionID: c.ID, Tipo: tipo,
		MensajeID: pgUUID(mensaje), UsuarioID: usuario, OcurridoEn: ahora}); err != nil {
		return err
	}
	return q.NotificarChat(ctx, clave)
}

// eventosPara arma los eventos del feed como los ve el usuario.
func eventosPara(ctx context.Context, q *mensajeriabd.Queries, a alcanceChat, filas []mensajeriabd.MensajeriaChatEvento) ([]EventoChat, error) {
	if len(filas) == 0 {
		return []EventoChat{}, nil
	}
	var msjIDs []uuid.UUID
	var convs, convsActualizadas []int64
	for _, f := range filas {
		if id := uuidDe(f.MensajeID); id != nil && f.Tipo != EventoLeido {
			msjIDs = append(msjIDs, *id)
		}
		convs = append(convs, f.ConversacionID)
		if f.Tipo == EventoConversacionActualizada {
			convsActualizadas = append(convsActualizadas, f.ConversacionID)
		}
	}
	mensajes := map[uuid.UUID]MensajeChat{}
	if len(msjIDs) > 0 {
		ms, err := q.MensajesPorIDs(ctx, msjIDs)
		if err != nil {
			return nil, err
		}
		for _, f := range ms {
			mensajes[f.MensajeriaChatMensaje.ID] = aMensaje(f.MensajeriaChatMensaje, f.AutorNombre, f.AdjuntoEstado)
		}
	}
	noLeidos := map[int64]int64{}
	nl, err := q.NoLeidos(ctx, mensajeriabd.NoLeidosParams{Usuario: a.usuario, Ids: convs})
	if err != nil {
		return nil, err
	}
	for _, n := range nl {
		noLeidos[n.ConversacionID] = n.NoLeidos
	}
	resumenes := map[int64]ConversacionChat{}
	if len(convsActualizadas) > 0 {
		cs, err := resumir(ctx, q, a.usuario, convsActualizadas)
		if err != nil {
			return nil, err
		}
		for _, c := range cs {
			resumenes[c.ID] = c
		}
	}
	salida := make([]EventoChat, 0, len(filas))
	for _, f := range filas {
		e := EventoChat{ID: strconv.FormatInt(f.ID, 10), Tipo: f.Tipo, ConversacionID: f.ConversacionID, OcurridoEn: f.OcurridoEn,
			NoLeidos: noLeidos[f.ConversacionID]}
		switch f.Tipo {
		case EventoMensajeNuevo, EventoMensajeEliminado:
			if id := uuidDe(f.MensajeID); id != nil {
				if msj, ok := mensajes[*id]; ok {
					e.Mensaje = &msj
				}
			}
		case EventoLeido:
			if id := uuidDe(f.MensajeID); id != nil && f.UsuarioID != nil {
				e.Lectura = &LecturaChat{UsuarioID: *f.UsuarioID, HastaMensajeID: *id, LeidoEn: f.OcurridoEn}
			}
		case EventoConversacionActualizada:
			if c, ok := resumenes[f.ConversacionID]; ok {
				e.Conversacion = &c
			}
		}
		salida = append(salida, e)
	}
	return salida, nil
}

// cursorFeed lee el cursor del feed (decimal; vacío = la cabeza de la empresa) y lo valida contra la
// purga. Un cursor mayor que la cabeza de su empresa pero no que la global vale (otra empresa).
func cursorFeed(ctx context.Context, q *mensajeriabd.Queries, a alcanceChat, campo, crudo string) (int64, error) {
	estado, err := q.EstadoFeedChat(ctx, a.empresa)
	if err != nil {
		return 0, err
	}
	cabeza := max(estado.Cabeza, estado.PurgadosHasta)
	if crudo == "" {
		return max(estado.CabezaEmpresa, estado.PurgadosHasta), nil
	}
	desde, err := strconv.ParseInt(crudo, 10, 64)
	if err != nil || desde < 0 {
		return 0, campoChat(campo, "Cursor inválido (el id de un evento del feed)", "cursor")
	}
	if desde < estado.PurgadosHasta || desde > cabeza {
		return 0, nucleo.NuevoProblema(http.StatusGone, "cursor_vencido", "Cursor vencido",
			"El feed ya no tiene los eventos desde ese cursor: vuelve a cargar las conversaciones y sigue desde la cabeza (sin cursor)")
	}
	return desde, nil
}

// hashContenido identifica el contenido de un mensaje (un reenvío con el mismo id debe traer lo mismo).
func hashContenido(conversacion int64, texto *string, adjunto *uuid.UUID, u *UbicacionChat) []byte {
	b, _ := json.Marshal(struct {
		C int64          `json:"c"`
		T *string        `json:"t"`
		A *uuid.UUID     `json:"a"`
		U *UbicacionChat `json:"u"`
	}{conversacion, texto, adjunto, u})
	h := sha256.Sum256(b)
	return h[:]
}

func esSinFilas(err error) bool { return errors.Is(err, pgx.ErrNoRows) }

func claveDirecta(a, b int64) string {
	if a > b {
		a, b = b, a
	}
	return fmt.Sprintf("%d:%d", a, b)
}

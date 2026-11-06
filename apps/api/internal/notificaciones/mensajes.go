package notificaciones

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/mensajeria"
	notificacionesbd "github.com/HCDestroyer/iris/apps/api/internal/notificaciones/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
)

// Armado del mensaje (contratos/push.md §4). Todo se lee al enviar: el texto nunca se guarda en
// los envíos ni va al log.

// VersionContrato de las claves de data.
const VersionContrato = "1"

// MaxTexto de la vista previa de un mensaje.
const MaxTexto = 200

// TextoPublico es la versión pública (pantalla bloqueada) de cada tipo.
var TextoPublico = map[string]string{
	TipoChat:    "Nuevo mensaje",
	TipoMencion: "Te mencionaron",
	TipoOrden:   "Orden actualizada",
	TipoBandeja: "Cliente esperando",
}

// TituloConversacionCliente: el título de una mención de la bandeja sin el nombre del cliente
// (contenido = remitente, o sin nombre conocido).
const TituloConversacionCliente = "Conversación con un cliente"

// contenidoPush es el mensaje armado de un tipo.
type contenidoPush struct {
	tipo, titulo, cuerpo    string
	grupo, etiqueta, enlace string
	acciones                []string
	ocurrido                time.Time
	datos                   map[string]any
	urgente                 bool
	ttl                     time.Duration
	collapse                string
}

// mensaje arma el cuerpo de FCM (solo datos, todo texto).
func (c contenidoPush) mensaje(env notificacionesbd.NotificacionesEnvio, d notificacionesbd.NotificacionesDispositivo) MensajeFCM {
	datos, _ := json.Marshal(c.datos)
	empresa := ""
	if d.EmpresaID != nil {
		empresa = strconv.FormatInt(*d.EmpresaID, 10)
	}
	ttl := c.ttl
	if ttl <= 0 {
		ttl = time.Hour
	}
	return MensajeFCM{
		Token: d.Token,
		Data: map[string]string{
			"v": VersionContrato, "id": strconv.FormatInt(env.ID, 10), "tipo": c.tipo, "canal": c.tipo,
			"usuarioId": strconv.FormatInt(d.UsuarioID, 10), "empresaId": empresa,
			"titulo": c.titulo, "cuerpo": c.cuerpo, "publicoTitulo": TextoPublico[c.tipo],
			"grupo": c.grupo, "etiqueta": c.etiqueta, "enlace": c.enlace, "acciones": strings.Join(c.acciones, ","),
			"ocurridoEn": c.ocurrido.UTC().Format(time.RFC3339), "datos": string(datos),
		},
		Android: AndroidFCM{Prioridad: "HIGH", TTL: strconv.Itoa(int(ttl/time.Second)) + "s", CollapseKey: c.collapse},
	}
}

// armar lee lo que se avisa y comprueba que el destinatario todavía lo ve. motivo no vacío = no
// se manda (y por qué).
func (m *Modulo) armar(ctx context.Context, t *auditoria.Tx, d destinatario, env notificacionesbd.NotificacionesEnvio, datos map[string]any,
	ahora time.Time) (contenidoPush, string, error) {
	tipo := env.Tipo
	switch {
	case tipo == TipoOrden:
		return m.armarOrden(ctx, t, d, datos, ahora)
	case tipo == TipoBandeja:
		return m.armarBandeja(ctx, t, d, env, datos)
	case tipo == TipoMencion && datos["origen"] == "bandeja":
		return m.armarNota(ctx, t, d, datos)
	}
	return m.armarChat(ctx, t, d, tipo, datos)
}

func uuidDe(v any) (uuid.UUID, bool) {
	s, _ := v.(string)
	u, err := uuid.Parse(s)
	return u, err == nil
}

func int64De(v any) int64 {
	f, _ := v.(float64)
	return int64(f)
}

func (m *Modulo) armarChat(ctx context.Context, t *auditoria.Tx, d destinatario, tipo string, datos map[string]any) (contenidoPush, string, error) {
	id, ok := uuidDe(datos["mensaje_id"])
	if !ok {
		return contenidoPush{}, OmitidoNoAplica, nil
	}
	msj, err := notificacionesbd.New(t).MensajeChatParaPush(ctx, id)
	if esSinFilas(err) {
		return contenidoPush{}, OmitidoNoAplica, nil
	}
	if err != nil {
		return contenidoPush{}, "", err
	}
	if msj.EliminadoEn != nil || msj.AutorID == d.disp.UsuarioID {
		return contenidoPush{}, OmitidoNoAplica, nil
	}
	if !mismoID(msj.EmpresaID, d.disp.EmpresaID) {
		return contenidoPush{}, OmitidoSinPermiso, nil
	}
	ve, err := mensajeria.ChatVisiblePara(d.ctx, t, msj.ConversacionID)
	if err != nil {
		return contenidoPush{}, "", err
	}
	if !ve {
		return contenidoPush{}, OmitidoSinPermiso, nil
	}
	completo := d.pref.Contenido == ContenidoCompleto
	contenido, vista := "texto", ""
	switch {
	case msj.Texto != nil:
		vista = recortar(*msj.Texto)
	case msj.AdjuntoID.Valid:
		contenido, vista = "foto", "📷 Foto"
	case msj.Lat != nil:
		contenido, vista = "ubicacion", "📍 Ubicación"
	}
	tituloConv := msj.AutorNombre
	switch {
	case msj.ConversacionTipo == "cuadrilla" && msj.CuadrillaNombre != nil:
		tituloConv = *msj.CuadrillaNombre
	case msj.ConversacionTipo == "orden" && msj.OrdenNumero != nil:
		tituloConv = *msj.OrdenNumero
	}
	remitente := map[string]any{"id": msj.AutorID, "nombre": msj.AutorNombre, "iniciales": iniciales(msj.AutorNombre)}
	var texto any
	if completo {
		texto = vista
	}
	c := contenidoPush{tipo: tipo, titulo: tituloConv, ocurrido: msj.EnviadoEn, enlace: fmt.Sprintf("iris://chat/%d", msj.ConversacionID),
		acciones: []string{"responder", "leido"}}
	if tipo == TipoMencion {
		c.urgente = true
		c.grupo, c.etiqueta = fmt.Sprintf("mencion-%d", msj.ConversacionID), "mencion-"+msj.ID.String()
		c.cuerpo = msj.AutorNombre + " te mencionó"
		if completo && vista != "" {
			c.cuerpo += ": " + vista
		}
		c.datos = map[string]any{"origen": "chat", "conversacionId": msj.ConversacionID, "conversacionTitulo": tituloConv,
			"mensajeId": msj.ID, "remitente": remitente, "texto": texto}
		return c, "", nil
	}
	c.grupo, c.etiqueta = fmt.Sprintf("chat-%d", msj.ConversacionID), "chat-"+msj.ID.String()
	switch {
	case !completo:
		c.cuerpo = "Nuevo mensaje de " + msj.AutorNombre
	case msj.ConversacionTipo == "directa":
		c.cuerpo = vista
	default:
		c.cuerpo = msj.AutorNombre + ": " + vista
	}
	c.datos = map[string]any{"conversacionId": msj.ConversacionID, "conversacionTipo": msj.ConversacionTipo, "conversacionTitulo": tituloConv,
		"mensajeId": msj.ID, "seq": msj.Seq, "remitente": remitente, "contenido": contenido, "texto": texto,
		"ordenId": msj.OrdenID, "cuadrillaId": msj.CuadrillaID}
	return c, "", nil
}

func (m *Modulo) armarNota(ctx context.Context, t *auditoria.Tx, d destinatario, datos map[string]any) (contenidoPush, string, error) {
	id, ok := uuidDe(datos["mensaje_id"])
	if !ok {
		return contenidoPush{}, OmitidoNoAplica, nil
	}
	n, err := notificacionesbd.New(t).NotaConversacionParaPush(ctx, id)
	if esSinFilas(err) {
		return contenidoPush{}, OmitidoNoAplica, nil
	}
	if err != nil {
		return contenidoPush{}, "", err
	}
	if n.Direccion != "nota" || (n.AutorID != nil && *n.AutorID == d.disp.UsuarioID) {
		return contenidoPush{}, OmitidoNoAplica, nil
	}
	if !mismoID(n.EmpresaID, d.disp.EmpresaID) {
		return contenidoPush{}, OmitidoSinPermiso, nil
	}
	ve, err := mensajeria.ConversacionVisiblePara(d.ctx, t, n.ConversacionID)
	if err != nil {
		return contenidoPush{}, "", err
	}
	if !ve {
		return contenidoPush{}, OmitidoSinPermiso, nil
	}
	completo := d.pref.Contenido == ContenidoCompleto
	var texto any
	c := contenidoPush{tipo: TipoMencion, titulo: n.ContactoNombre, urgente: true, ocurrido: n.CreadoEn,
		grupo: fmt.Sprintf("mencion-%d", n.ConversacionID), etiqueta: "mencion-" + n.ID.String(),
		enlace: fmt.Sprintf("iris://conversaciones/%d", n.ConversacionID), acciones: []string{"responder", "leido"},
		cuerpo: n.AutorNombre + " te mencionó"}
	// Con «solo quién escribe» tampoco sale el nombre del cliente.
	if c.titulo == "" || !completo {
		c.titulo = TituloConversacionCliente
	}
	if completo && n.Texto != nil {
		texto = recortar(*n.Texto)
		c.cuerpo += ": " + recortar(*n.Texto)
	}
	var autor int64
	if n.AutorID != nil {
		autor = *n.AutorID
	}
	c.datos = map[string]any{"origen": "bandeja", "conversacionId": n.ConversacionID, "conversacionTitulo": c.titulo, "mensajeId": n.ID,
		"remitente": map[string]any{"id": autor, "nombre": n.AutorNombre, "iniciales": iniciales(n.AutorNombre)}, "texto": texto}
	return c, "", nil
}

func (m *Modulo) armarBandeja(ctx context.Context, t *auditoria.Tx, d destinatario, env notificacionesbd.NotificacionesEnvio, datos map[string]any) (contenidoPush, string, error) {
	id := int64De(datos["conversacion_id"])
	cv, err := notificacionesbd.New(t).ConversacionParaPush(ctx, id)
	if esSinFilas(err) {
		return contenidoPush{}, OmitidoNoAplica, nil
	}
	if err != nil {
		return contenidoPush{}, "", err
	}
	// Ya la tomó alguien o se resolvió: no hace falta preguntar.
	if cv.AsignadoA != nil || cv.Estado == "resuelta" {
		return contenidoPush{}, OmitidoNoAplica, nil
	}
	if !mismoID(cv.EmpresaID, d.disp.EmpresaID) {
		return contenidoPush{}, OmitidoSinPermiso, nil
	}
	ve, err := mensajeria.ConversacionVisiblePara(d.ctx, t, cv.ID)
	if err != nil {
		return contenidoPush{}, "", err
	}
	if !ve {
		return contenidoPush{}, OmitidoSinPermiso, nil
	}
	cola := "la bandeja"
	var colaNombre any
	if cv.ColaNombre != nil {
		cola, colaNombre = *cv.ColaNombre, *cv.ColaNombre
	}
	c := contenidoPush{tipo: TipoBandeja, titulo: "¿Alguien la atiende?", cuerpo: "Cliente esperando en " + cola, ocurrido: env.CreatedAt,
		grupo: fmt.Sprintf("conversacion-%d", cv.ID), etiqueta: fmt.Sprintf("conversacion-%d", cv.ID),
		enlace: fmt.Sprintf("iris://conversaciones/%d", cv.ID), acciones: []string{"ver"}}
	var contacto any
	if d.pref.Contenido == ContenidoCompleto && cv.ContactoNombre != "" {
		contacto = cv.ContactoNombre
		c.cuerpo += " · " + cv.ContactoNombre
	}
	c.datos = map[string]any{"conversacionId": cv.ID, "cola": colaNombre, "contacto": contacto, "botContestaEn": datos["bot_contesta_en"]}
	return c, "", nil
}

func (m *Modulo) armarOrden(ctx context.Context, t *auditoria.Tx, d destinatario, datos map[string]any, ahora time.Time) (contenidoPush, string, error) {
	o, err := notificacionesbd.New(t).OrdenParaPush(ctx, int64De(datos["orden_id"]))
	if esSinFilas(err) {
		return contenidoPush{}, OmitidoNoAplica, nil
	}
	if err != nil {
		return contenidoPush{}, "", err
	}
	switch o.Estado {
	case "cerrada", "fallida", "cancelada":
		return contenidoPush{}, OmitidoNoAplica, nil
	}
	if o.TecnicoID == nil || *o.TecnicoID != d.disp.UsuarioID {
		return contenidoPush{}, OmitidoNoAplica, nil
	}
	if !mismoID(o.EmpresaID, d.disp.EmpresaID) {
		return contenidoPush{}, OmitidoSinPermiso, nil
	}
	p := contexto.PermisosDe(d.ctx)
	if !p.Tiene(string(nucleo.PermOrdenesVerPropias)) && !p.Tiene(string(nucleo.PermOrdenesVer)) {
		return contenidoPush{}, OmitidoSinPermiso, nil
	}
	cambio := "reprogramada"
	if datos["asignada"] == true {
		cambio = "asignada"
	}
	inicio, fin := o.ProgramadaInicio, o.ProgramadaFin
	if o.VentanaInicio != nil && o.VentanaFin != nil {
		inicio, fin = o.VentanaInicio, o.VentanaFin
	}
	hoy := false
	titulo := o.Numero + " " + cambio
	var partes []string
	if inicio != nil {
		dia, esHoy := diaRelativo(*inicio, ahora, d.zona)
		hoy = esHoy
		titulo += " para " + dia
		franja := inicio.In(d.zona).Format("15:04")
		if fin != nil {
			franja += "–" + fin.In(d.zona).Format("15:04")
		}
		partes = append(partes, franja)
	}
	if o.ZonaNombre != nil {
		partes = append(partes, *o.ZonaNombre)
	}
	partes = append(partes, o.TipoNombre)
	var cliente any
	if o.ClienteNombre != "" {
		cliente = o.ClienteNombre
		partes = append(partes, o.ClienteNombre)
	}
	c := contenidoPush{tipo: TipoOrden, titulo: titulo, cuerpo: strings.Join(partes, " · "), urgente: hoy, ocurrido: ahora,
		grupo: fmt.Sprintf("orden-%d", o.ID), etiqueta: fmt.Sprintf("orden-%d", o.ID), enlace: fmt.Sprintf("iris://orden/%d", o.ID),
		acciones: []string{"ver_orden", "como_llegar"}, ttl: 12 * time.Hour, collapse: fmt.Sprintf("orden-%d", o.ID)}
	c.datos = map[string]any{"ordenId": o.ID, "numero": o.Numero, "cambio": cambio, "tipo": o.Tipo, "tipoNombre": o.TipoNombre,
		"zona": o.ZonaNombre, "cliente": cliente, "inicio": o.ProgramadaInicio, "fin": o.ProgramadaFin,
		"ventanaInicio": o.VentanaInicio, "ventanaFin": o.VentanaFin, "hoy": hoy}
	return c, "", nil
}

var (
	diasCortos  = []string{"dom", "lun", "mar", "mié", "jue", "vie", "sáb"}
	mesesCortos = []string{"ene", "feb", "mar", "abr", "may", "jun", "jul", "ago", "sep", "oct", "nov", "dic"}
)

// diaRelativo: «hoy», «mañana» o «el lun 9 nov», en la zona del usuario.
func diaRelativo(cuando, ahora time.Time, zona *time.Location) (string, bool) {
	c, a := cuando.In(zona), ahora.In(zona)
	dc := time.Date(c.Year(), c.Month(), c.Day(), 0, 0, 0, 0, zona)
	da := time.Date(a.Year(), a.Month(), a.Day(), 0, 0, 0, 0, zona)
	switch {
	case dc.Equal(da):
		return "hoy", true
	case dc.Equal(da.AddDate(0, 0, 1)):
		return "mañana", false
	}
	return fmt.Sprintf("el %s %d %s", diasCortos[c.Weekday()], c.Day(), mesesCortos[c.Month()-1]), false
}

// recortar deja el texto en MaxTexto caracteres (con «…»), en una línea.
func recortar(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	r := []rune(s)
	if len(r) <= MaxTexto {
		return s
	}
	return strings.TrimRightFunc(string(r[:MaxTexto-1]), unicode.IsSpace) + "…"
}

// iniciales: «Carlos Ruiz» → «CR».
func iniciales(nombre string) string {
	var b strings.Builder
	for i, p := range strings.Fields(nombre) {
		if i == 2 {
			break
		}
		r := []rune(p)
		b.WriteString(strings.ToUpper(string(r[0])))
	}
	return b.String()
}

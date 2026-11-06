package mensajeria

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	mensajeriabd "github.com/HCDestroyer/iris/apps/api/internal/mensajeria/bd"
)

// «¿Alguien la atiende?» y el bot «Cuando no hay agentes» (contratos/conversaciones.md §14.6).
//
// Con el horario abierto, al llegar un mensaje del cliente a una conversación sin asignar:
//   - hay disponibles en la cola: se les avisa y, si nadie la toma en minutosSinConfirmacion,
//     contesta el bot (saludo y menú);
//   - no hay nadie: el bot saluda en el momento y, si nadie la tomó en minutosSinDisponibles,
//     toma la conversación (menú).
//
// Con el horario cerrado sigue la respuesta de fuera de horario (fuera_horario.go), con el menú del
// bot después. Los temporizadores viven en la fila (bot_vence_en) y los dispara el mantenimiento
// cada minuto, revisando con la fila bloqueada: tomar, asignar, pasar, transferir o resolver los
// cancelan (limpiarEspera).

// Lo que contesta cuando no hay agentes.
const (
	BotAtiendeBot      = "bot"
	BotAtiendeAutonomo = "agente_autonomo"

	MotivoBotSinConfirmacion = "sin_confirmacion"
	MotivoBotSinDisponibles  = "sin_disponibles"
	MotivoBotFueraDeHorario  = "fuera_de_horario"

	menuEsperando = "esperando"
	menuRepetido  = "repetido"

	// AutomaticoBot y AutomaticoFueraDeHorario marcan los salientes sin autor.
	AutomaticoBot            = "bot"
	AutomaticoFueraDeHorario = "fuera_de_horario"
)

// SaludoBotPorDefecto es el saludo de un número sin configurar.
const SaludoBotPorDefecto = "Gracias por escribir. En este momento todos nuestros agentes están ocupados; te respondemos en cuanto se libere uno."

// OpcionMenuBot es una opción del menú del bot.
type OpcionMenuBot struct {
	Texto       string  `json:"texto" minLength:"1" maxLength:"60" doc:"Lo que ve el cliente (p. ej. Ventas)"`
	ColaID      int64   `json:"colaId" minimum:"1" doc:"Cola a la que pasa la conversación"`
	EtiquetaIDs []int64 `json:"etiquetaIds" maxItems:"5" uniqueItems:"true" doc:"Etiquetas que se le ponen"`
}

// BotNumero es lo que hace un número cuando no hay agentes (§14.6).
type BotNumero struct {
	Activo                 bool            `json:"activo" doc:"Aviso «¿Alguien la atiende?» y bot en horario (fuera de horario manda fueraDeHorario)"`
	Atiende                string          `json:"atiende" enum:"bot,agente_autonomo" doc:"agente_autonomo: fase 3 (422 no_disponible_aun)"`
	MinutosSinConfirmacion int32           `json:"minutosSinConfirmacion" minimum:"1" maximum:"1440" doc:"Con agentes disponibles: minutos sin que nadie la tome antes de que conteste el bot (30)"`
	MinutosSinDisponibles  int32           `json:"minutosSinDisponibles" minimum:"1" maximum:"1440" doc:"Sin agentes disponibles: minutos después del saludo antes de que el bot tome la conversación (3)"`
	Saludo                 string          `json:"saludo" maxLength:"1024" doc:"Texto plano del saludo del bot"`
	Menu                   []OpcionMenuBot `json:"menu" maxItems:"9" doc:"Menú simple: el cliente contesta con el número de la opción"`
}

// botDe lee la configuración del número con los valores por defecto.
func botDe(crudo []byte) BotNumero {
	b := BotNumero{}
	_ = json.Unmarshal(crudo, &b)
	if b.Atiende == "" {
		b.Atiende = BotAtiendeBot
	}
	if b.MinutosSinConfirmacion <= 0 {
		b.MinutosSinConfirmacion = 30
	}
	if b.MinutosSinDisponibles <= 0 {
		b.MinutosSinDisponibles = 3
	}
	if strings.TrimSpace(b.Saludo) == "" {
		b.Saludo = SaludoBotPorDefecto
	}
	if b.Menu == nil {
		b.Menu = []OpcionMenuBot{}
	}
	for i := range b.Menu {
		if b.Menu[i].EtiquetaIDs == nil {
			b.Menu[i].EtiquetaIDs = []int64{}
		}
	}
	return b
}

// validarBot aplica las reglas de la configuración.
func validarBot(ctx context.Context, t *auditoria.Tx, empresa int64, b *BotNumero) error {
	if b.Atiende == "" {
		b.Atiende = BotAtiendeBot
	}
	switch b.Atiende {
	case BotAtiendeBot:
	case BotAtiendeAutonomo:
		return noDisponibleAun("Que atienda un agente autónomo llegará en la fase 3")
	default:
		return campoConv("bot.atiende", "bot o agente_autonomo", "enum")
	}
	b.Saludo = strings.TrimSpace(b.Saludo)
	if b.Saludo == "" {
		return campoConv("bot.saludo", "Escribe el saludo del bot", "required")
	}
	if utf8.RuneCountInString(b.Saludo) > 1024 {
		return campoConv("bot.saludo", "Hasta 1024 caracteres", "maxLength")
	}
	if b.MinutosSinConfirmacion < 1 || b.MinutosSinConfirmacion > 1440 {
		return campoConv("bot.minutosSinConfirmacion", "Entre 1 y 1440 minutos", "rango")
	}
	if b.MinutosSinDisponibles < 1 || b.MinutosSinDisponibles > 1440 {
		return campoConv("bot.minutosSinDisponibles", "Entre 1 y 1440 minutos", "rango")
	}
	if len(b.Menu) > 9 {
		return campoConv("bot.menu", "Hasta 9 opciones", "maxItems")
	}
	q := mensajeriabd.New(t)
	if b.Menu == nil {
		b.Menu = []OpcionMenuBot{}
	}
	for i := range b.Menu {
		o := &b.Menu[i]
		campo := fmt.Sprintf("bot.menu[%d]", i)
		o.Texto = strings.TrimSpace(o.Texto)
		if o.Texto == "" || utf8.RuneCountInString(o.Texto) > 60 {
			return campoConv(campo+".texto", "Entre 1 y 60 caracteres", "texto")
		}
		cola, err := q.ColaPorID(ctx, mensajeriabd.ColaPorIDParams{ID: o.ColaID, Empresa: empresa})
		if errors.Is(err, pgx.ErrNoRows) || (err == nil && !cola.Activa) {
			return campoConv(campo+".colaId", "No es una cola activa de la empresa", "cola")
		}
		if err != nil {
			return err
		}
		if o.EtiquetaIDs == nil {
			o.EtiquetaIDs = []int64{}
		}
		if len(o.EtiquetaIDs) > 5 {
			return campoConv(campo+".etiquetaIds", "Hasta 5 etiquetas", "maxItems")
		}
		if len(o.EtiquetaIDs) > 0 {
			es, err := q.EtiquetasActivasPorIDs(ctx, mensajeriabd.EtiquetasActivasPorIDsParams{Ids: o.EtiquetaIDs, Empresa: empresa})
			if err != nil {
				return err
			}
			if len(es) != len(o.EtiquetaIDs) {
				return campoConv(campo+".etiquetaIds", "Alguna no es una etiqueta activa de la empresa", "etiqueta")
			}
		}
	}
	return nil
}

// minutosBot: los de la cola si los tiene; si no, los del número.
func minutosBot(cola *mensajeriabd.MensajeriaCola, b BotNumero) (sinConfirmacion, sinDisponibles int32) {
	sinConfirmacion, sinDisponibles = b.MinutosSinConfirmacion, b.MinutosSinDisponibles
	if cola != nil {
		if cola.BotSinConfirmacionMin != nil {
			sinConfirmacion = *cola.BotSinConfirmacionMin
		}
		if cola.BotSinDisponiblesMin != nil {
			sinDisponibles = *cola.BotSinDisponiblesMin
		}
	}
	return sinConfirmacion, sinDisponibles
}

func textoMenu(b BotNumero) string {
	if len(b.Menu) == 0 {
		return ""
	}
	var s strings.Builder
	s.WriteString("Responde con el número de una opción:")
	for i, o := range b.Menu {
		fmt.Fprintf(&s, "\n%d. %s", i+1, o.Texto)
	}
	return s.String()
}

// guardarBot escribe el estado de la espera de la conversación.
func guardarBot(ctx context.Context, q *mensajeriabd.Queries, c mensajeriabd.MensajeriaConversacion) error {
	return q.GuardarBotConversacion(ctx, mensajeriabd.GuardarBotConversacionParams{ID: c.ID, EsperaEn: c.BotEsperaEn, VenceEn: c.BotVenceEn,
		Motivo: c.BotMotivo, AtendioEn: c.BotAtendioEn, Menu: c.BotMenu})
}

// limpiarEspera cancela el temporizador y el menú y empieza una espera nueva (al tomar, asignar,
// pasar, transferir, resolver o iniciar). La fila debe estar bloqueada.
func limpiarEspera(ctx context.Context, q *mensajeriabd.Queries, c *mensajeriabd.MensajeriaConversacion) error {
	if c.BotEsperaEn == nil && c.BotVenceEn == nil && c.BotAtendioEn == nil && c.BotMenu == nil {
		return nil
	}
	c.BotEsperaEn, c.BotVenceEn, c.BotMotivo, c.BotAtendioEn, c.BotMenu = nil, nil, nil, nil, nil
	return guardarBot(ctx, q, *c)
}

// puedeContestarBot: nunca a una baja vigente (del número o del cliente) ni con la ventana de 24 h
// cerrada.
func (m *Modulo) puedeContestarBot(ctx context.Context, q *mensajeriabd.Queries, c mensajeriabd.MensajeriaConversacion) (bool, error) {
	if !ventanaDe(c.UltimoEntranteEn, m.ahora()).Abierta {
		return false, nil
	}
	if c.ClienteID != nil {
		v, err := q.ConsentimientoVigente(ctx, mensajeriabd.ConsentimientoVigenteParams{EmpresaID: empresaDeConv(c), ClienteID: *c.ClienteID, Canal: CanalWhatsApp})
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return false, err
		}
		if err == nil && v.Estado == "baja" {
			return false, nil
		}
	}
	v, err := q.ConsentimientoNumeroVigente(ctx, mensajeriabd.ConsentimientoNumeroVigenteParams{Empresa: empresaDeConv(c), Telefono: c.Numero})
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return false, err
	}
	return err != nil || v.Estado != "baja", nil
}

// mensajeAutomatico deja un saliente sin autor en cola de envío.
func (m *Modulo) mensajeAutomatico(ctx context.Context, t *auditoria.Tx, c *mensajeriabd.MensajeriaConversacion, texto, automatico string) (mensajeriabd.MensajeriaConversacionMensaje, error) {
	texto = recortar(texto, MaxTextoWhatsApp)
	msg, err := m.agregarMensaje(ctx, t, c, nuevoMensaje{direccion: DirSaliente, tipo: "texto", texto: &texto, estadoEntrega: nombrePtr(EntregaEnCola),
		automatico: &automatico})
	if err != nil {
		return msg, err
	}
	return msg, m.encolar(ctx, t.Tx, ArgsEnviarMensaje{MensajeID: msg.ID})
}

func (m *Modulo) colaDeConv(ctx context.Context, q *mensajeriabd.Queries, c mensajeriabd.MensajeriaConversacion) (*mensajeriabd.MensajeriaCola, error) {
	if c.ColaID == nil {
		return nil, nil
	}
	cola, err := q.ColaPorID(ctx, mensajeriabd.ColaPorIDParams{ID: *c.ColaID, Empresa: empresaDeConv(c)})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &cola, nil
}

// alRecibirBandeja corre después de guardar un mensaje del cliente: respuesta al menú, fuera de
// horario y, en horario, el aviso o el bot.
func (m *Modulo) alRecibirBandeja(ctx context.Context, t *auditoria.Tx, c *mensajeriabd.MensajeriaConversacion, canal mensajeriabd.MensajeriaCanal,
	f mensajeriabd.MensajeriaConversacionMensaje) error {
	q := mensajeriabd.New(t)
	bot := botDe(canal.Bot)
	if bot.Activo && c.BotMenu != nil && c.AsignadoA == nil {
		if err := m.respuestaMenu(ctx, t, c, bot, f); err != nil {
			return err
		}
		return nil
	}
	cerrado, err := m.alRecibirFueraDeHorario(ctx, t, c, canal, f, bot)
	if err != nil || cerrado {
		return err
	}
	if !bot.Activo || c.AsignadoA != nil || c.Estado == EstadoConvResuelta || c.BotEsperaEn != nil {
		return nil
	}
	return m.empezarEspera(ctx, t, q, c, bot)
}

// empezarEspera: avisa a los disponibles (temporizador sin_confirmacion) o, si no hay nadie, saluda
// y deja el temporizador sin_disponibles.
func (m *Modulo) empezarEspera(ctx context.Context, t *auditoria.Tx, q *mensajeriabd.Queries, c *mensajeriabd.MensajeriaConversacion, bot BotNumero) error {
	cola, err := m.colaDeConv(ctx, q, *c)
	if err != nil {
		return err
	}
	sinConf, sinDisp := minutosBot(cola, bot)
	disp, err := disponiblesDe(ctx, q, empresaDeConv(*c), c.ColaID)
	if err != nil {
		return err
	}
	ahora := m.ahora()
	c.BotEsperaEn = &ahora
	if len(disp) > 0 {
		vence := ahora.Add(time.Duration(sinConf) * time.Minute)
		c.BotVenceEn, c.BotMotivo = &vence, nombrePtr(MotivoBotSinConfirmacion)
		if err := guardarBot(ctx, q, *c); err != nil {
			return err
		}
		return m.avisarSinAtender(ctx, t, c, cola, disp, &vence)
	}
	puede, err := m.puedeContestarBot(ctx, q, *c)
	if err != nil {
		return err
	}
	if puede {
		msg, err := m.mensajeAutomatico(ctx, t, c, bot.Saludo, AutomaticoBot)
		if err != nil {
			return err
		}
		if err := m.auditarRespuestaBot(ctx, *c, "saludo_sin_disponibles", &msg); err != nil {
			return err
		}
	}
	vence := ahora.Add(time.Duration(sinDisp) * time.Minute)
	c.BotVenceEn, c.BotMotivo = &vence, nombrePtr(MotivoBotSinDisponibles)
	if err := guardarBot(ctx, q, *c); err != nil {
		return err
	}
	return m.emitirEventoConv(ctx, t, *c, EventoConvActualizada, nil, nil)
}

// avisarSinAtender: evento de sistema, aviso del flujo a cada disponible y outbox (futuro push).
func (m *Modulo) avisarSinAtender(ctx context.Context, t *auditoria.Tx, c *mensajeriabd.MensajeriaConversacion, cola *mensajeriabd.MensajeriaCola,
	disp []UsuarioRef, vence *time.Time) error {
	n := int64(len(disp))
	ev := EventoSistema{Tipo: "aviso_sin_atender", Disponibles: &n}
	if cola != nil {
		ev.ColaNombre = &cola.Nombre
	}
	msg, err := m.agregarMensaje(ctx, t, c, nuevoMensaje{direccion: DirSistema, tipo: "sistema", evento: &ev})
	if err != nil {
		return err
	}
	ids := make([]int64, 0, len(disp))
	for _, u := range disp {
		uu := u.ID
		ids = append(ids, uu)
		if err := m.emitirEventoConv(ctx, t, *c, EventoConvAvisoSinAtender, &msg.ID, &uu); err != nil {
			return err
		}
	}
	if err := m.emitirEventoConv(ctx, t, *c, EventoConvActualizada, nil, nil); err != nil {
		return err
	}
	return t.Emitir("conversacion.aviso_sin_atender", map[string]any{"conversacion_id": c.ID, "empresa_id": c.EmpresaID, "cola_id": c.ColaID,
		"usuarios": ids, "bot_contesta_en": vence})
}

// respuestaMenu: el cliente contestó al menú del bot.
func (m *Modulo) respuestaMenu(ctx context.Context, t *auditoria.Tx, c *mensajeriabd.MensajeriaConversacion, bot BotNumero, f mensajeriabd.MensajeriaConversacionMensaje) error {
	q := mensajeriabd.New(t)
	puede, err := m.puedeContestarBot(ctx, q, *c)
	if err != nil {
		return err
	}
	if !puede {
		c.BotMenu = nil
		return guardarBot(ctx, q, *c)
	}
	opcion := 0
	if f.Texto != nil {
		if n, err := strconv.Atoi(palabraDe(*f.Texto)); err == nil && n >= 1 && n <= len(bot.Menu) {
			opcion = n
		}
	}
	if opcion == 0 {
		if *c.BotMenu == menuEsperando {
			c.BotMenu = nombrePtr(menuRepetido)
			msg, err := m.mensajeAutomatico(ctx, t, c, "No entendí tu respuesta.\n\n"+textoMenu(bot), AutomaticoBot)
			if err != nil {
				return err
			}
			if err := m.auditarRespuestaBot(ctx, *c, "menu_repetido", &msg); err != nil {
				return err
			}
		} else {
			// Segunda respuesta que no es una opción: el bot deja de insistir.
			c.BotMenu = nil
		}
		return guardarBot(ctx, q, *c)
	}
	o := bot.Menu[opcion-1]
	empresa := empresaDeConv(*c)
	anterior := c.ColaID
	cola, err := q.ColaPorID(ctx, mensajeriabd.ColaPorIDParams{ID: o.ColaID, Empresa: empresa})
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	var destino *mensajeriabd.MensajeriaCola
	if err == nil && cola.Activa {
		destino = &cola
		c.ColaID = &cola.ID
		if c.EsperaDesde != nil {
			if c.SlaVenceEn, err = m.venceSLA(ctx, q, *c, cola, *c.EsperaDesde); err != nil {
				return err
			}
			c.SlaAvisado = false
		}
	}
	// Etiquetas de la opción: se suman (máx. 10, solo las activas).
	previas, err := q.EtiquetasDeConversaciones(ctx, []int64{c.ID})
	if err != nil {
		return err
	}
	tiene := []int64{}
	for _, p := range previas {
		tiene = append(tiene, p.ID)
	}
	puestas := []int64{}
	if len(o.EtiquetaIDs) > 0 {
		es, err := q.EtiquetasActivasPorIDs(ctx, mensajeriabd.EtiquetasActivasPorIDsParams{Ids: o.EtiquetaIDs, Empresa: empresa})
		if err != nil {
			return err
		}
		for _, e := range es {
			if slices.Contains(tiene, e.ID) || len(tiene) >= 10 {
				continue
			}
			if err := q.InsertarEtiquetaConversacion(ctx, mensajeriabd.InsertarEtiquetaConversacionParams{ConversacionID: c.ID, EtiquetaID: e.ID}); err != nil {
				return err
			}
			tiene, puestas = append(tiene, e.ID), append(puestas, e.ID)
		}
	}
	c.BotMenu = nil
	textoOpcion := o.Texto
	ev := EventoSistema{Tipo: "menu_elegido", Detalle: &textoOpcion}
	if destino != nil {
		ev.ColaNombre = &destino.Nombre
	}
	if err := m.sistema(ctx, t, c, ev); err != nil {
		return err
	}
	g, err := guardarConv(ctx, q, *c, true)
	if err != nil {
		return err
	}
	g.BotEsperaEn, g.BotVenceEn, g.BotMotivo, g.BotAtendioEn, g.BotMenu = c.BotEsperaEn, c.BotVenceEn, c.BotMotivo, c.BotAtendioEn, c.BotMenu
	*c = g
	if err := guardarBot(ctx, q, *c); err != nil {
		return err
	}
	if err := m.auditarRespuestaBot(ctx, *c, "menu_elegido", nil); err != nil {
		return err
	}
	if err := t.Emitir("conversacion.menu_elegido", map[string]any{"conversacion_id": c.ID, "empresa_id": c.EmpresaID, "opcion": opcion,
		"texto": o.Texto, "de_cola_id": anterior, "a_cola_id": c.ColaID, "etiquetas": puestas}); err != nil {
		return err
	}
	// Se avisa a los disponibles de la cola nueva (sin otro temporizador: el bot ya atendió).
	if destino != nil {
		disp, err := disponiblesDe(ctx, q, empresa, c.ColaID)
		if err != nil {
			return err
		}
		if len(disp) > 0 {
			return m.avisarSinAtender(ctx, t, c, destino, disp, nil)
		}
	}
	return m.emitirEventoConv(ctx, t, *c, EventoConvActualizada, nil, nil)
}

// contestarConMenu manda el menú (si hay) y lo deja esperando respuesta.
func (m *Modulo) contestarConMenu(ctx context.Context, t *auditoria.Tx, c *mensajeriabd.MensajeriaConversacion, prefijo string, bot BotNumero) (*mensajeriabd.MensajeriaConversacionMensaje, error) {
	texto := strings.TrimSpace(prefijo)
	if menu := textoMenu(bot); menu != "" {
		if texto != "" {
			texto += "\n\n"
		}
		texto += menu
		c.BotMenu = nombrePtr(menuEsperando)
	}
	if texto == "" {
		return nil, nil
	}
	msg, err := m.mensajeAutomatico(ctx, t, c, texto, AutomaticoBot)
	if err != nil {
		return nil, err
	}
	return &msg, nil
}

// Temporizadores (mantenimiento) ------------------------------------------------------------------

// nuevoDispararBot: un temporizador vencido. Vuelve a revisar con la fila bloqueada: si la tomaron,
// la resolvieron o ya no vence, no hace nada (idempotente).
func nuevoDispararBot(m *Modulo) *auditoria.Caso[int64, bool] {
	return auditoria.Nuevo(auditoria.Caso[int64, bool]{
		// Cada vez que el bot contesta por un temporizador queda una fila (actor sistema).
		Accion: "mensajeria.bot_conversacion", Acceso: auditoria.Autenticado, AuditarExito: func(ok bool) bool { return ok },
		Logica: func(ctx context.Context, t *auditoria.Tx, id int64) (bool, error) {
			if m.fallarFila != nil {
				if err := m.fallarFila(id); err != nil {
					return false, err
				}
			}
			q := mensajeriabd.New(t)
			c, err := q.ConversacionClienteParaActualizar(ctx, id)
			if errors.Is(err, pgx.ErrNoRows) {
				return false, nil
			}
			if err != nil {
				return false, err
			}
			ahora := m.ahora()
			if c.BotVenceEn == nil || c.BotMotivo == nil || c.BotVenceEn.After(ahora) {
				return false, nil
			}
			t.FijarEmpresa(empresaDeConv(c))
			t.FijarEntidad("conversacion", c.ID)
			motivo := *c.BotMotivo
			c.BotVenceEn, c.BotMotivo = nil, nil
			canal, err := q.CanalPorID(ctx, c.CanalID)
			if err != nil {
				return false, err
			}
			bot := botDe(canal.Bot)
			if c.AsignadoA != nil || c.Estado == EstadoConvResuelta || !bot.Activo || !canal.Activo {
				return false, guardarBot(ctx, q, c)
			}
			cola, err := m.colaDeConv(ctx, q, c)
			if err != nil {
				return false, err
			}
			sinConf, sinDisp := minutosBot(cola, bot)
			minutos := sinConf
			prefijo := bot.Saludo
			if motivo == MotivoBotSinDisponibles {
				// El saludo ya salió al empezar la espera: ahora el bot toma la conversación con el menú.
				minutos, prefijo = sinDisp, ""
			}
			puede, err := m.puedeContestarBot(ctx, q, c)
			if err != nil {
				return false, err
			}
			var mensaje any
			if puede {
				msg, err := m.contestarConMenu(ctx, t, &c, prefijo, bot)
				if err != nil {
					return false, err
				}
				if msg != nil {
					mensaje = msg.ID
				}
			}
			c.BotAtendioEn = &ahora
			t.Anotar(map[string]any{"motivo": motivo, "mensaje_id": mensaje})
			if err := m.sistema(ctx, t, &c, EventoSistema{Tipo: "bot_atendio", Detalle: &motivo, Minutos: &minutos}); err != nil {
				return false, err
			}
			if err := guardarBot(ctx, q, c); err != nil {
				return false, err
			}
			if err := m.emitirEventoConv(ctx, t, c, EventoConvActualizada, nil, nil); err != nil {
				return false, err
			}
			return true, t.Emitir("conversacion.bot_atendio", map[string]any{"conversacion_id": c.ID, "empresa_id": c.EmpresaID, "cola_id": c.ColaID,
				"motivo": motivo, "mensaje_id": mensaje, "menu": c.BotMenu != nil})
		},
	})
}

// dispararBots corre los temporizadores vencidos hasta ahora (lo llama el mantenimiento). Un
// error en una conversación queda en el log y sigue con la siguiente; devuelve los disparos y el
// primer error (el mantenimiento solo lo registra).
func (m *Modulo) dispararBots(ctx context.Context, bd mensajeriabd.DBTX) (int, error) {
	ids, err := mensajeriabd.New(bd).BotVencidos(ctx, m.ahora())
	if err != nil {
		slog.ErrorContext(ctx, "mensajeria: leer temporizadores del bot", "err", err)
		return 0, err
	}
	n := 0
	var primero error
	for _, id := range ids {
		ok, err := m.dispararBot.Ejecutar(ctx, id)
		if err != nil {
			slog.ErrorContext(ctx, "mensajeria: temporizador del bot", "conversacion_id", id, "err", err)
			if primero == nil {
				primero = err
			}
			continue
		}
		if ok {
			n++
		}
	}
	return n, primero
}

// botAuditado es una respuesta del bot fuera del temporizador (saludo inmediato, menú de fuera de
// horario, menú repetido o elegido): una fila de auditoría como actor sistema.
type botAuditado struct {
	ConversacionID int64   `json:"conversacion_id"`
	EmpresaID      *int64  `json:"empresa_id"`
	Motivo         string  `json:"motivo"`
	MensajeID      *string `json:"mensaje_id"`
}

func nuevoAuditarBot() *auditoria.Caso[botAuditado, struct{}] {
	return auditoria.Nuevo(auditoria.Caso[botAuditado, struct{}]{
		Accion: "mensajeria.bot_respondio", Acceso: auditoria.Autenticado,
		Logica: func(_ context.Context, t *auditoria.Tx, in botAuditado) (struct{}, error) {
			e := int64(0)
			if in.EmpresaID != nil {
				e = *in.EmpresaID
			}
			t.FijarEmpresa(e)
			t.FijarEntidad("conversacion", in.ConversacionID)
			return struct{}{}, nil
		},
	})
}

// auditarRespuestaBot deja la fila (en la misma transacción, como caso anidado).
func (m *Modulo) auditarRespuestaBot(ctx context.Context, c mensajeriabd.MensajeriaConversacion, motivo string, msg *mensajeriabd.MensajeriaConversacionMensaje) error {
	in := botAuditado{ConversacionID: c.ID, EmpresaID: c.EmpresaID, Motivo: motivo}
	if msg != nil {
		id := msg.ID.String()
		in.MensajeID = &id
	}
	_, err := m.auditarBot.Ejecutar(ctx, in)
	return err
}

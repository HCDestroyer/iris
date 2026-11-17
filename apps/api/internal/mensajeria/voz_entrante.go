package mensajeria

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	mensajeriabd "github.com/HCDestroyer/iris/apps/api/internal/mensajeria/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
)

// Llamadas entrantes: el webhook calls de Meta (connect y terminate), a quién le suena y cómo se
// cierra una llamada. Todo corre dentro de la transacción de mensajeria.procesar_webhook, que para
// un connect se ejecuta además en línea en la misma petición (excepción documentada, §3.3).

// guardarLlamada escribe la fila que calculó el caso de uso (leída con FOR UPDATE).
func guardarLlamada(ctx context.Context, q *mensajeriabd.Queries, l mensajeriabd.MensajeriaLlamada) (mensajeriabd.MensajeriaLlamada, error) {
	return q.GuardarLlamada(ctx, mensajeriabd.GuardarLlamadaParams{ID: l.ID, Estado: l.Estado, AtendidaPor: l.AtendidaPor, UsuarioID: l.UsuarioID,
		ReclamadaPor: l.ReclamadaPor, ReclamadaHasta: l.ReclamadaHasta, MotivoFin: l.MotivoFin, ContestadaEn: l.ContestadaEn, TerminadaEn: l.TerminadaEn,
		DuracionSeg: l.DuracionSeg, Nota: l.Nota, Resultado: l.Resultado, SdpOferta: l.SdpOferta, AccionMeta: l.AccionMeta,
		AccionMetaEstado: l.AccionMetaEstado, AccionMetaEn: l.AccionMetaEn, AccionMetaPor: l.AccionMetaPor, PreAceptadaEn: l.PreAceptadaEn, ReclamadaNavegador: navegadorDeReserva(l),
		AvisoPerdida: l.AvisoPerdida})
}

// intencionMeta anota la acción que se va a pedir a Meta (primera fase, dentro de la transacción).
// Un pre_accept o un accept recuerdan de quién era la reserva: su resultado solo vale si la llamada
// sigue en manos de esa persona (M1).
func intencionMeta(l *mensajeriabd.MensajeriaLlamada, accion string, ahora time.Time) {
	p := "pendiente"
	l.AccionMeta, l.AccionMetaEstado, l.AccionMetaEn, l.AccionMetaPor = &accion, &p, &ahora, nil
	if accion == MetaPreAceptar || accion == MetaAceptar {
		l.AccionMetaPor = l.ReclamadaPor
	}
}

// navegadorDeReserva: el navegador solo vale mientras haya reserva.
func navegadorDeReserva(l mensajeriabd.MensajeriaLlamada) *string {
	if l.ReclamadaPor == nil {
		return nil
	}
	return l.ReclamadaNavegador
}

func accionPendiente(l mensajeriabd.MensajeriaLlamada) bool {
	return l.AccionMetaEstado != nil && *l.AccionMetaEstado == "pendiente"
}

type claveSinMeta struct{}

// sinMetaEnLinea marca un contexto (el del mantenimiento) en el que las acciones con Meta no se
// ejecutan tras el COMMIT: quedan anotadas y las hace el job de escrituras a Meta.
func sinMetaEnLinea(ctx context.Context) context.Context {
	return context.WithValue(ctx, claveSinMeta{}, true)
}

// taparSDPEntrantes deja como *** el SDP de la llamada en los webhooks guardados (§6.2, fila 11): al
// terminar la llamada el SDP no queda en ningún sitio.
func taparSDPEntrantes(ctx context.Context, q *mensajeriabd.Queries, l mensajeriabd.MensajeriaLlamada) error {
	if l.CanalID == nil {
		return nil
	}
	return taparSDPDeLlamada(ctx, q, *l.CanalID, l.IDExterno, l.SonoEn.Add(-10*time.Minute))
}

// taparSDPDeLlamada: lo mismo por call_id (también para un connect que no llega a ser una llamada).
func taparSDPDeLlamada(ctx context.Context, q *mensajeriabd.Queries, canalID int64, callID string, desde time.Time) error {
	l := struct{ IDExterno string }{callID}
	filas, err := q.EntrantesDeLlamada(ctx, mensajeriabd.EntrantesDeLlamadaParams{CanalID: canalID, Desde: desde, Marca: callID})
	if err != nil {
		return err
	}
	for _, f := range filas {
		var doc map[string]any
		if json.Unmarshal(f.Payload, &doc) != nil {
			continue
		}
		cambio := false
		entradas, _ := doc["entry"].([]any)
		for _, e := range entradas {
			cambios, _ := e.(map[string]any)["changes"].([]any)
			for _, c := range cambios {
				valor, _ := c.(map[string]any)["value"].(map[string]any)
				llamadas, _ := valor["calls"].([]any)
				for _, x := range llamadas {
					ll, _ := x.(map[string]any)
					if ll["id"] != l.IDExterno {
						continue
					}
					if ses, ok := ll["session"].(map[string]any); ok && ses["sdp"] != "***" {
						ses["sdp"], cambio = "***", true
					}
				}
			}
		}
		if !cambio {
			continue
		}
		b, err := json.Marshal(doc)
		if err != nil {
			return err
		}
		if err := q.FijarPayloadEntrante(ctx, mensajeriabd.FijarPayloadEntranteParams{ID: f.ID, Payload: b}); err != nil {
			return err
		}
	}
	return nil
}

// canalGraphDe: el número y su token para llamar a Meta.
func (m *Modulo) canalGraphDe(ctx context.Context, q *mensajeriabd.Queries, canalID int64) (CanalGraph, error) {
	c, err := q.CanalPorID(ctx, canalID)
	if err != nil {
		return CanalGraph{}, err
	}
	cr, err := m.credencialesDe(c)
	if err != nil {
		return CanalGraph{}, err
	}
	return CanalGraph{PhoneNumberID: c.PhoneNumberID, Token: cr.Token, WabaID: c.WabaID}, nil
}

// ajustesVoz: los de la empresa (sin fila, los de fábrica).
func ajustesVoz(ctx context.Context, q *mensajeriabd.Queries, empresa int64) (mensajeriabd.MensajeriaVozAjuste, error) {
	f, err := q.VozAjustes(ctx, empresa)
	if errors.Is(err, pgx.ErrNoRows) {
		return mensajeriabd.MensajeriaVozAjuste{Grabar: "nunca", DuracionMaxPersonaMin: 60, SegundosAsignado: 10, RetencionGrabacionesDias: 30,
			MaxSalientesPorDia: 50, TopeMinutosSalientesMes: 300, MaxColgadasMarcandoDia: 3}, nil
	}
	return f, err
}

// recibirEventoLlamada aplica un elemento de calls[] del webhook.
func (m *Modulo) recibirEventoLlamada(ctx context.Context, t *auditoria.Tx, canal mensajeriabd.MensajeriaCanal, ev metaLlamada, perfil string, ahora time.Time) error {
	if ev.ID == "" || len(ev.ID) > 200 {
		return nil
	}
	switch strings.ToLower(ev.Event) {
	case "connect":
		// El connect de una saliente trae la respuesta SDP del cliente.
		if d := strings.ToUpper(ev.Direction); d != "" && d != "USER_INITIATED" {
			return m.eventoSaliente(ctx, t, canal, ev, ahora)
		}
		return m.recibirLlamada(ctx, t, canal, ev, perfil, ahora)
	case "status":
		return m.eventoSaliente(ctx, t, canal, ev, ahora)
	case "terminate":
		return m.terminarPorMeta(ctx, t, canal, ev, ahora)
	}
	return nil
}

// recibirLlamada crea la llamada entrante (sonando), su mensaje y decide a quién le suena (§3.5).
// Es idempotente por (canal, call_id): un connect repetido no hace nada.
func (m *Modulo) recibirLlamada(ctx context.Context, t *auditoria.Tx, canal mensajeriabd.MensajeriaCanal, ev metaLlamada, perfil string, ahora time.Time) error {
	q := mensajeriabd.New(t)
	numero := "+" + strings.TrimPrefix(ev.From, "+")
	if !numeroWA.MatchString(numero) {
		return nil
	}
	aj, err := q.VozAjustesCanal(ctx, canal.ID)
	sinAjustes := errors.Is(err, pgx.ErrNoRows)
	if err != nil && !sinAjustes {
		return err
	}
	// 1. Número sin llamadas encendidas (o por QR): no debería llegar. Se rechaza sin registrar nada.
	if sinAjustes || !aj.LlamadasActivas || canal.TipoConexion != ConexionAPI {
		m.rechazarSinRegistro(ctx, t, canal.ID, ev.ID)
		// No queda llamada, pero el webhook guardado trae la oferta: se tapa igual.
		return taparSDPDeLlamada(ctx, q, canal.ID, ev.ID, ahora.Add(-10*time.Minute))
	}
	d, err := m.rutearEntrante(ctx, q, canal, numero)
	if err != nil {
		return err
	}
	t.FijarEmpresa(d.empresa)
	c, nueva, err := m.conversacionDe(ctx, t, canal, d, numero, perfil, ahora, "cliente")
	if err != nil {
		return err
	}
	id, err := uuid.NewV7()
	if err != nil {
		return err
	}
	voz, err := ajustesVoz(ctx, q, d.empresa)
	if err != nil {
		return err
	}
	// Número prestado (§15.3 del chat): si la llamada corresponde a la empresa prestataria no se usa
	// nada de la dueña: ni su cola, ni su horario, ni sus textos, ni sus candidatos.
	ajena := d.empresa != derefEmpresa(canal.EmpresaID)
	cola, segundos := c.ColaID, aj.SegundosSonando
	if ajena {
		segundos = 30
	} else if aj.ColaID != nil {
		cola = aj.ColaID
	}
	// La oferta de Meta pasa la misma validación que el SDP del navegador antes de entregarse.
	var sdp *string
	if ev.Session != nil && ev.Session.SDP != "" {
		if err := ValidarSDP(ev.Session.SDP); err != nil {
			slog.WarnContext(ctx, "voz: la oferta SDP de Meta no pasa la validación; la llamada no suena", "canal_id", canal.ID, "motivo", err.Error())
		} else {
			sdp = &ev.Session.SDP
		}
	}
	sono := instanteMeta(ev.Timestamp, ahora)
	if sono.After(ahora) {
		sono = ahora
	}
	l, err := q.InsertarLlamada(ctx, mensajeriabd.InsertarLlamadaParams{LlamadaID: id, EmpresaID: c.EmpresaID, CanalID: &canal.ID, ConversacionID: &c.ID,
		Externo: ev.ID, Numero: numero, ClienteID: c.ClienteID, Servicio: c.ServicioID, ColaID: cola, SonoEn: sono,
		SuenaHasta: ahora.Add(time.Duration(segundos) * time.Second), SdpOferta: sdp})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil // ya se recibió (Meta reintentó, o el job después del proceso en línea)
	}
	if err != nil {
		return err
	}
	// La llamada es actividad del cliente: reabre una conversación resuelta.
	antes := c
	if c.Estado == EstadoConvResuelta {
		c.Estado, c.ReabiertaEn, c.ResueltaEn = EstadoConvAbierta, &ahora, nil
		if err := m.conservarAsignacion(ctx, t, &c); err != nil {
			return err
		}
	}
	msg, err := m.agregarMensaje(ctx, t, &c, nuevoMensaje{direccion: DirEntrante, tipo: "llamada", creadoEn: sono})
	if err != nil {
		return err
	}
	if err := q.FijarLlamadaDeMensaje(ctx, mensajeriabd.FijarLlamadaDeMensajeParams{ID: msg.ID, LlamadaID: pgUUID(&l.ID)}); err != nil {
		return err
	}
	if err := q.FijarMensajeDeLlamada(ctx, mensajeriabd.FijarMensajeDeLlamadaParams{ID: l.ID, MensajeID: pgUUID(&msg.ID)}); err != nil {
		return err
	}
	l.MensajeID = pgUUID(&msg.ID)
	if c, err = guardarConv(ctx, q, c, nueva || antes.Estado != c.Estado); err != nil {
		return err
	}
	if err := t.Emitir("llamada.entrante", map[string]any{"llamada_id": l.ID, "conversacion_id": c.ID, "empresa_id": c.EmpresaID, "canal_id": canal.ID,
		"cola_id": l.ColaID, "cliente_id": l.ClienteID, "origen": "whatsapp"}); err != nil {
		return err
	}
	if err := t.Emitir("conversacion.mensaje_recibido", map[string]any{"conversacion_id": c.ID, "mensaje_id": msg.ID, "empresa_id": c.EmpresaID,
		"canal_id": c.CanalID, "tipo": "llamada", "cliente_id": c.ClienteID, "asignado_a": c.AsignadoA, "cola_id": c.ColaID}); err != nil {
		return err
	}
	// Un connect viejo (más de 60 s) no hace sonar nada: Meta ya dejó de llamar.
	if ahora.Sub(sono) > ConnectViejo || sdp == nil {
		return m.cerrarLlamada(ctx, t, &l, &c, LlamadaPerdida, "nadie_contesto", ahora, "")
	}
	// 2. Ráfaga: la cuarta llamada del número en 10 minutos se rechaza, sin mensaje.
	recientes, err := q.LlamadasRecientesDeNumero(ctx, mensajeriabd.LlamadasRecientesDeNumeroParams{CanalID: &canal.ID, Numero: numero, Desde: ahora.Add(-RafagaVentana)})
	if err != nil {
		return err
	}
	if recientes > RafagaLlamadas {
		return m.cerrarLlamada(ctx, t, &l, &c, LlamadaRechazada, "limite", ahora, MetaRechazar)
	}
	// En un número prestado, las llamadas de la empresa prestataria todavía no suenan (E1: los ajustes de
	// llamadas son de la dueña del número). Queda como perdida en la prestataria, con su devolución
	// pendiente y un mensaje neutro. Va después de la ráfaga: tampoco aquí hay mensajes ni avisos sin límite.
	if ajena {
		return m.cerrarLlamada(ctx, t, &l, &c, LlamadaPerdida, "nadie_contesto", ahora, MetaRechazar)
	}
	// 3. Fuera del horario de Iris (el de los ajustes del número; si no, el de la cola; si no, el del número).
	horario := aj.HorarioID
	if horario == nil && cola != nil {
		if co, err := q.ColaPorID(ctx, mensajeriabd.ColaPorIDParams{ID: *cola, Empresa: d.empresa}); err == nil {
			horario = co.HorarioID
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
	}
	cal, err := calendarioEfectivo(ctx, q, horario, canal.ID)
	if err != nil {
		return err
	}
	if cal != nil {
		if abierto, _, apertura := cal.estado(ahora); !abierto {
			return m.llamadaFueraDeHorario(ctx, t, &l, &c, canal, aj, apertura, ahora)
		}
	}
	// 5 y 6. Personas candidatas: primero quien tiene la conversación (segundosAsignado); después, la cola.
	if _, err := m.ofrecerLlamada(ctx, t, &l, c, voz, ahora); err != nil {
		return err
	}
	return nil
}

// rechazarSinRegistro pide a Meta rechazar una llamada que no se registra (número sin llamadas
// encendidas). Va después del COMMIT: la transacción no llama a nadie por red.
func (m *Modulo) rechazarSinRegistro(ctx context.Context, t *auditoria.Tx, canalID int64, callID string) {
	cg, err := m.canalGraphDe(ctx, mensajeriabd.New(t), canalID)
	if err != nil {
		return
	}
	t.Anotar(map[string]any{"llamada_rechazada_sin_registro": true})
	t.AlConfirmar(func(ctx context.Context) {
		if err := m.graph().Accion(context.WithoutCancel(ctx), cg, callID, MetaRechazar, "", false); err != nil {
			slog.WarnContext(ctx, "voz: rechazar una llamada de un número sin llamadas encendidas", "canal_id", canalID, "err", err)
		}
	})
}

// ofrecerLlamada decide a quién le suena y se lo avisa por el feed. Devuelve cuántos candidatos
// nuevos hubo. Se llama al llegar la llamada, al soltarla y en el mantenimiento (para ampliar de la
// persona asignada a toda la cola cuando pasan segundosAsignado).
func (m *Modulo) ofrecerLlamada(ctx context.Context, t *auditoria.Tx, l *mensajeriabd.MensajeriaLlamada, c mensajeriabd.MensajeriaConversacion,
	voz mensajeriabd.MensajeriaVozAjuste, ahora time.Time) (int, error) {
	q := mensajeriabd.New(t)
	empresa := derefEmpresa(l.EmpresaID)
	receptores, err := q.ReceptoresParaLlamada(ctx, mensajeriabd.ReceptoresParaLlamadaParams{Empresa: empresa, VivoDesde: ahora.Add(-FlujoVivo), ColaID: l.ColaID})
	if err != nil {
		return 0, err
	}
	// Mientras duren segundosAsignado, solo a quien tiene la conversación (si puede recibirla).
	if c.AsignadoA != nil && voz.SegundosAsignado > 0 && ahora.Before(l.SonoEn.Add(time.Duration(voz.SegundosAsignado)*time.Second)) {
		todos, err := q.ReceptoresParaLlamada(ctx, mensajeriabd.ReceptoresParaLlamadaParams{Empresa: empresa, VivoDesde: ahora.Add(-FlujoVivo)})
		if err != nil {
			return 0, err
		}
		for _, u := range todos {
			if u == *c.AsignadoA {
				receptores = []int64{u}
				break
			}
		}
	}
	vivos, err := q.CandidatosVivos(ctx, l.ID)
	if err != nil {
		return 0, err
	}
	ya := map[int64]bool{}
	for _, u := range vivos {
		ya[u] = true
	}
	nuevos := 0
	for _, u := range receptores {
		if ya[u] {
			continue
		}
		if err := q.InsertarCandidato(ctx, mensajeriabd.InsertarCandidatoParams{LlamadaID: l.ID, EmpresaID: l.EmpresaID, UsuarioID: u, Desde: ahora}); err != nil {
			return nuevos, err
		}
		// Un candidato retirado (rechazó, o soltó dos veces) no vuelve a recibir esta llamada.
		if ok, err := q.EsCandidato(ctx, mensajeriabd.EsCandidatoParams{ID: l.ID, UsuarioID: u, Empresa: empresa}); err != nil || !ok {
			if err != nil {
				return nuevos, err
			}
			continue
		}
		nuevos++
		usuario := u
		if err := m.emitirEventoLlamada(ctx, t, *l, EventoLlamadaEntrante, &usuario); err != nil {
			return nuevos, err
		}
	}
	return nuevos, nil
}

// volverASonar avisa otra vez a los candidatos vivos (la reserva se soltó o venció).
func (m *Modulo) volverASonar(ctx context.Context, t *auditoria.Tx, l mensajeriabd.MensajeriaLlamada) error {
	vivos, err := mensajeriabd.New(t).CandidatosVivos(ctx, l.ID)
	if err != nil {
		return err
	}
	for _, u := range vivos {
		usuario := u
		if err := m.emitirEventoLlamada(ctx, t, l, EventoLlamadaEntrante, &usuario); err != nil {
			return err
		}
	}
	return m.emitirEventoLlamada(ctx, t, l, EventoLlamadaActualizada, nil)
}

// llamadaFueraDeHorario aplica §3.6: rechazar con el mensaje de fuera de horario del número (una vez
// por ventana cerrada, compartida con el chat) o dejarla al buzón de voz de Meta.
func (m *Modulo) llamadaFueraDeHorario(ctx context.Context, t *auditoria.Tx, l *mensajeriabd.MensajeriaLlamada, c *mensajeriabd.MensajeriaConversacion,
	canal mensajeriabd.MensajeriaCanal, aj mensajeriabd.MensajeriaVozAjustesCanal, apertura, ahora time.Time) error {
	accion := MetaRechazar
	if aj.FueraDeHorario == "buzon" {
		// El buzón lo da Meta: Iris no toca la llamada y el audio llega como un mensaje de audio.
		accion = ""
	}
	if err := m.cerrarLlamada(ctx, t, l, c, LlamadaFueraDeHorario, "fuera_de_horario", ahora, accion); err != nil {
		return err
	}
	if aj.FueraDeHorario == "buzon" || derefEmpresa(c.EmpresaID) != derefEmpresa(canal.EmpresaID) {
		return nil
	}
	hasta := apertura
	if hasta.IsZero() {
		hasta = ahora.Add(24 * time.Hour)
	}
	f := fueraDeHorarioDe(canal.FueraHorario)
	if f.Responder && f.Mensaje != "" && (c.FueraHorarioHasta == nil || !ahora.Before(*c.FueraHorarioHasta)) {
		return m.responderFueraDeHorario(ctx, t, c, f, hasta, BotNumero{})
	}
	return nil
}

// cerrarLlamada lleva la llamada a un estado final: borra el SDP y la reserva, retira a los
// candidatos, cierra los tramos, actualiza el mensaje del hilo, avisa por el feed y emite el evento.
// accionMeta (reject o terminate) deja anotada la intención: quien llama la ejecuta tras el COMMIT
// con m.cumplirMeta. Una llamada que ya terminó no cambia.
func (m *Modulo) cerrarLlamada(ctx context.Context, t *auditoria.Tx, l *mensajeriabd.MensajeriaLlamada, c *mensajeriabd.MensajeriaConversacion,
	estado, motivo string, ahora time.Time, accionMeta string) error {
	if llamadaFinal(l.Estado) {
		return nil
	}
	q := mensajeriabd.New(t)
	etapa := l.Estado
	l.Estado, l.MotivoFin, l.TerminadaEn = estado, &motivo, &ahora
	l.SdpOferta, l.ReclamadaPor, l.ReclamadaHasta, l.PreAceptadaEn = nil, nil, nil, nil
	if l.ContestadaEn != nil {
		d := int32(min(max(0, ahora.Sub(*l.ContestadaEn)/time.Second), 24*3600)) //nolint:gosec // acotado a un día
		l.DuracionSeg = &d
	}
	l.AccionMeta, l.AccionMetaEstado, l.AccionMetaEn, l.AccionMetaPor = nil, nil, nil, nil
	// Una saliente a la que Meta todavía no le dio call_id no tiene nada que terminar allá.
	if strings.HasPrefix(l.IDExterno, "pendiente:") {
		accionMeta = ""
	}
	if accionMeta != "" {
		if ctx.Value(claveSinMeta{}) != nil {
			// Desde el mantenimiento no se llama a Meta: queda anotada, ya vencida, para el job de escrituras.
			intencionMeta(l, accionMeta, ahora.Add(-2*PlazoMeta))
		} else {
			intencionMeta(l, accionMeta, ahora)
			// Segunda fase, después del COMMIT: la transacción no llama a nadie por red.
			m.trasCommitMeta(ctx, t, l.ID)
		}
	}
	if err := q.RetirarCandidatos(ctx, mensajeriabd.RetirarCandidatosParams{LlamadaID: l.ID, Ahora: &ahora}); err != nil {
		return err
	}
	if err := q.CerrarTramos(ctx, mensajeriabd.CerrarTramosParams{LlamadaID: l.ID, Cierre: ahora, Motivo: nombrePtr("fin_llamada")}); err != nil {
		return err
	}
	base := map[string]any{"llamada_id": l.ID, "conversacion_id": l.ConversacionID, "empresa_id": l.EmpresaID}
	con := func(extra map[string]any) map[string]any {
		for k, v := range extra {
			base[k] = v
		}
		return base
	}
	var err error
	switch estado {
	case LlamadaTerminada:
		err = t.Emitir("llamada.terminada", con(map[string]any{"direccion": l.Direccion, "atendida_por": l.AtendidaPor, "usuario_id": l.UsuarioID,
			"duracion_seg": l.DuracionSeg, "motivo_fin": motivo, "tramos": 1}))
	case LlamadaPerdida:
		var devolucion *int64
		// Solo una entrante perdida deja devolución pendiente y mensaje automático.
		if c != nil && l.Direccion != "saliente" {
			if devolucion, err = m.trasPerdida(ctx, t, l, c, ahora); err != nil {
				return err
			}
		}
		// usuarios: a quién se le avisa por push (quien tiene la conversación y quienes supervisan la cola).
		usuarios := []int64{}
		if c != nil && c.AsignadoA != nil && l.Direccion != "saliente" {
			usuarios = append(usuarios, *c.AsignadoA)
		}
		if l.ColaID != nil && l.Direccion != "saliente" {
			sups, errS := q.SupervisoresDeColas(ctx, []int64{*l.ColaID})
			if errS != nil {
				return errS
			}
			for _, s := range sups {
				if !slices.Contains(usuarios, s.ID) {
					usuarios = append(usuarios, s.ID)
				}
			}
		}
		err = t.Emitir("llamada.perdida", con(map[string]any{"direccion": l.Direccion, "cola_id": l.ColaID,
			"sono_seg": int(max(0, ahora.Sub(l.SonoEn)/time.Second)), "devolucion_id": devolucion, "usuarios": usuarios}))
	case LlamadaRechazada, LlamadaFueraDeHorario:
		err = t.Emitir("llamada.rechazada", con(map[string]any{"motivo": motivo, "usuario_id": l.UsuarioID}))
	case LlamadaFallida:
		err = t.Emitir("llamada.fallida", con(map[string]any{"motivo": motivo, "etapa": etapa}))
	}
	if err != nil {
		return err
	}
	if *l, err = guardarLlamada(ctx, q, *l); err != nil {
		return err
	}
	if l.Direccion == "saliente" {
		if err := m.trasSaliente(ctx, t, l, ahora); err != nil {
			return err
		}
	}
	if err := taparSDPEntrantes(ctx, q, *l); err != nil {
		return err
	}
	return m.emitirEventoLlamada(ctx, t, *l, EventoLlamadaTerminada, nil)
}

// trasPerdida deja la devolución pendiente (una por conversación) y, si el número lo tiene
// encendido, el mensaje «No pudimos contestarte», una vez por hora.
func (m *Modulo) trasPerdida(ctx context.Context, t *auditoria.Tx, l *mensajeriabd.MensajeriaLlamada, c *mensajeriabd.MensajeriaConversacion, ahora time.Time) (*int64, error) {
	q := mensajeriabd.New(t)
	var devolucion *int64
	d, err := q.InsertarDevolucion(ctx, mensajeriabd.InsertarDevolucionParams{EmpresaID: l.EmpresaID, ConversacionID: c.ID, LlamadaID: l.ID,
		VenceEn: ahora.Add(VenceDevolucion)})
	switch {
	case err == nil:
		devolucion = &d.ID
		if err := t.Emitir("llamada.devolucion_pedida", map[string]any{"devolucion_id": d.ID, "conversacion_id": c.ID, "empresa_id": l.EmpresaID,
			"llamada_id": l.ID, "pedida_por": "sistema"}); err != nil {
			return nil, err
		}
		if err := m.emitirEventoLlamada(ctx, t, *l, EventoDevolucion, nil); err != nil {
			return nil, err
		}
	case !errors.Is(err, pgx.ErrNoRows):
		return nil, err
	}
	if l.CanalID == nil {
		return devolucion, nil
	}
	// El texto configurado es de la dueña del número: en la conversación de otra empresa (número
	// prestado) sale uno neutro, que no nombra a nadie.
	canal, err := q.CanalPorID(ctx, *l.CanalID)
	if err != nil {
		return devolucion, err
	}
	texto := MensajePerdidaNeutro
	if derefEmpresa(canal.EmpresaID) == derefEmpresa(l.EmpresaID) {
		aj, err := q.VozAjustesCanal(ctx, *l.CanalID)
		if err != nil || !aj.MensajePerdida {
			if errors.Is(err, pgx.ErrNoRows) {
				err = nil
			}
			return devolucion, err
		}
		texto = aj.MensajePerdidaTexto
	}
	reciente, err := q.AvisoPerdidaReciente(ctx, mensajeriabd.AvisoPerdidaRecienteParams{ConversacionID: &c.ID, Desde: ahora.Add(-AvisoPerdidaCada)})
	if err != nil || reciente {
		return devolucion, err
	}
	// Nunca a quien pidió no recibir mensajes.
	if puede, err := m.puedeContestarBot(ctx, q, *c); err != nil || !puede {
		return devolucion, err
	}
	if _, err := m.mensajeAutomatico(ctx, t, c, texto, AutomaticoBot); err != nil {
		return devolucion, err
	}
	l.AvisoPerdida = true
	return devolucion, nil
}

// terminarPorMeta aplica el webhook terminate: cierra la llamada si seguía viva y completa los
// tiempos con los de Meta. Nunca reabre una llamada cerrada.
func (m *Modulo) terminarPorMeta(ctx context.Context, t *auditoria.Tx, canal mensajeriabd.MensajeriaCanal, ev metaLlamada, ahora time.Time) error {
	q := mensajeriabd.New(t)
	l, err := q.LlamadaPorExterno(ctx, mensajeriabd.LlamadaPorExternoParams{CanalID: &canal.ID, IDExterno: ev.ID})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	t.FijarEmpresa(derefEmpresa(l.EmpresaID))
	if !llamadaFinal(l.Estado) {
		var c *mensajeriabd.MensajeriaConversacion
		if l.ConversacionID != nil {
			f, err := q.ConversacionClienteParaActualizar(ctx, *l.ConversacionID)
			if err != nil {
				return err
			}
			c = &f
		}
		// Una saliente que seguía marcando pero Meta dice que duró algo: sí se contestó (el ACCEPTED no
		// llegó o llegó tarde). Queda terminada con su duración y cuenta para el tope de minutos.
		if n, err := ev.Duration.Int64(); err == nil && n > 0 && n < 24*3600 && l.Direccion == "saliente" && l.Estado == LlamadaMarcando {
			desde := ahora.Add(-time.Duration(n) * time.Second)
			if desde.Before(l.SonoEn) {
				desde = l.SonoEn
			}
			if err := m.salienteContestada(ctx, t, &l, desde); err != nil {
				return err
			}
		}
		estado, motivo := LlamadaPerdida, "nadie_contesto"
		if l.Estado == LlamadaEnCurso {
			estado, motivo = LlamadaTerminada, "cliente_colgo"
		}
		if err := m.cerrarLlamada(ctx, t, &l, c, estado, motivo, ahora, ""); err != nil {
			return err
		}
		if c != nil {
			if _, err := guardarConv(ctx, q, *c, false); err != nil {
				return err
			}
		}
	}
	// Meta es la verdad sobre la duración: solo completa datos.
	if l.Estado == LlamadaTerminada {
		cambio := false
		if n, err := ev.Duration.Int64(); err == nil && n >= 0 && n < 24*3600 {
			d := int32(n) //nolint:gosec // 0 ≤ n < 86 400
			l.DuracionSeg, cambio = &d, true
		}
		if ev.EndTime != "" {
			fin := instanteMeta(ev.EndTime, ahora)
			l.TerminadaEn, cambio = &fin, true
		}
		if cambio {
			if l, err = guardarLlamada(ctx, q, l); err != nil {
				return err
			}
			return m.emitirEventoLlamada(ctx, t, l, EventoLlamadaActualizada, nil)
		}
	}
	return nil
}

// cumplirMeta es la segunda y la tercera fase de una acción con Meta (§3.3): llama a la Graph API
// fuera de toda transacción y anota el resultado. Devuelve el error de Meta (nil si la aplicó).
func (m *Modulo) cumplirMeta(ctx context.Context, bd nucleo.BD, id uuid.UUID, sdp string) error {
	ctx = context.WithoutCancel(ctx)
	q := mensajeriabd.New(bd)
	l, err := q.LlamadaPorID(ctx, id)
	if err != nil || l.AccionMeta == nil || l.AccionMetaEstado == nil || *l.AccionMetaEstado != "pendiente" || l.CanalID == nil {
		return err
	}
	cg, err := m.canalGraphDe(ctx, q, *l.CanalID)
	if err != nil {
		return err
	}
	errMeta := m.graph().Accion(ctx, cg, l.IDExterno, *l.AccionMeta, sdp, l.Grabada)
	var em *ErrMetaLlamada
	rechazada := errors.As(errMeta, &em) && !em.SinRespuesta
	cambio, err := m.Voz.resultadoMeta.Ejecutar(ctxSistema(ctx, bd), resultadoMeta{ID: id, Accion: *l.AccionMeta, Fallo: errMeta != nil, Rechazada: rechazada})
	if err != nil {
		return err
	}
	// Meta rechazó el terminate y la intención pasó a reject (resultadoMeta): se manda ya.
	if cambio && rechazada && *l.AccionMeta == MetaTerminar {
		return m.cumplirMeta(ctx, bd, id, "")
	}
	return errMeta
}

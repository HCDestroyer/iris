package mensajeria

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	mensajeriabd "github.com/HCDestroyer/iris/apps/api/internal/mensajeria/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad"
)

// Tiempo real de la bandeja (contratos/conversaciones.md §9): el mismo patrón que el chat interno.
// Feed mensajeria.conv_eventos, NOTIFY iris_conversaciones (despierta), GET /conversaciones/cambios
// (sondeo) y GET /conversaciones/eventos (SSE) con el mismo cursor.

// EventoConversacion es un cambio del feed: el mismo cuerpo en SSE (data) y en /cambios.
type EventoConversacion struct {
	ID             string               `json:"id" doc:"Cursor del feed (el id de SSE / Last-Event-ID)"`
	Tipo           string               `json:"tipo" enum:"mensaje_nuevo,mensaje_actualizado,conversacion_actualizada,mencion,guardia,aviso_sin_atender,sugerencia,llamada_entrante,llamada_actualizada,llamada_terminada,devolucion_actualizada,llamada_respuesta,centralita_llamada"`
	Llamada        *Llamada             `json:"llamada,omitempty" doc:"llamada_entrante (solo a quien le suena), llamada_actualizada, llamada_terminada y devolucion_actualizada: la llamada como la ves ahora, sin SDP"`
	ConversacionID int64                `json:"conversacionId" doc:"0 en centralita_llamada (no es de una conversación)"`
	OcurridoEn     time.Time            `json:"ocurridoEn"`
	NoLeidos       int64                `json:"noLeidos" doc:"Tus no leídos de la conversación al leer el evento"`
	Mensaje        *MensajeConversacion `json:"mensaje,omitempty" doc:"mensaje_nuevo, mensaje_actualizado, mencion y guardia (estado actual del mensaje)"`
	Conversacion   *ConversacionCliente `json:"conversacion,omitempty" doc:"conversacion_actualizada, mencion y guardia (como la ves ahora)"`
	Sugerencia     *Sugerencia          `json:"sugerencia,omitempty" doc:"sugerencia: la sugerencia del asistente como está ahora (§16.2)"`
	Centralita     *LlamadaCentralita   `json:"centralita,omitempty" doc:"centralita_llamada: la llamada de la centralita como la ves ahora (solo a quien le sonó; contratos/voz.md §3.12)"`
}

// CambiosConversaciones es el feed desde un cursor.
type CambiosConversaciones struct {
	Data       []EventoConversacion `json:"data"`
	NextCursor string               `json:"nextCursor" doc:"Siempre presente: el cursor de la siguiente consulta (o el id para Last-Event-ID)"`
	HayMas     bool                 `json:"hayMas"`
}

// EntradaCambiosConv es GET /conversaciones/cambios.
type EntradaCambiosConv struct {
	Cursor string `query:"cursor" maxLength:"20" doc:"El nextCursor anterior o el id del último evento SSE. Vacío: la cabeza (sin eventos)"`
	Limit  int    `query:"limit" minimum:"1" maximum:"200" default:"100"`
}

func cursorConv(ctx context.Context, q *mensajeriabd.Queries, a alcanceConv, campo, crudo string) (int64, error) {
	estado, err := q.EstadoFeedConv(ctx, a.empresa)
	if err != nil {
		return 0, err
	}
	cabeza := max(estado.Cabeza, estado.PurgadosHasta)
	if crudo == "" {
		return max(estado.CabezaEmpresa, estado.PurgadosHasta), nil
	}
	desde, err := strconv.ParseInt(crudo, 10, 64)
	if err != nil || desde < 0 {
		return 0, campoConv(campo, "Cursor inválido (el id de un evento del feed)", "cursor")
	}
	if desde < estado.PurgadosHasta || desde > cabeza {
		return 0, nucleo.NuevoProblema(http.StatusGone, "cursor_vencido", "Cursor vencido",
			"El feed ya no tiene los eventos desde ese cursor: vuelve a cargar la bandeja y sigue sin cursor")
	}
	return desde, nil
}

// leerFeedConv lee los eventos que ve el usuario desde el cursor (la cabeza antes que los eventos,
// como el chat: un evento que confirma en medio no se salta).
func (m *Modulo) leerFeedConv(ctx context.Context, q *mensajeriabd.Queries, a alcanceConv, desde int64, limite int) ([]EventoConversacion, bool, int64, error) {
	estado, err := q.EstadoFeedConv(ctx, a.empresa)
	if err != nil {
		return nil, false, desde, err
	}
	filas, err := q.EventosConv(ctx, mensajeriabd.EventosConvParams{Desde: desde, Empresa: a.empresa, Usuario: a.usuario, Supervisa: a.supervisa,
		Sugerencias: a.sugerencias, Conversaciones: !a.soloCentralita,
		Limite: int32(limite + 1)}) //nolint:gosec // limite ≤ 200
	if err != nil {
		return nil, false, desde, err
	}
	hayMas := len(filas) > limite
	if hayMas {
		filas = filas[:limite]
	}
	evs, err := m.eventosConvPara(ctx, q, a, filas)
	if err != nil {
		return nil, false, desde, err
	}
	siguiente := desde
	if len(filas) > 0 {
		siguiente = filas[len(filas)-1].ID
	} else if !hayMas {
		siguiente = max(siguiente, estado.CabezaEmpresa)
	}
	return evs, hayMas, siguiente, nil
}

func (m *Modulo) eventosConvPara(ctx context.Context, q *mensajeriabd.Queries, a alcanceConv, filas []mensajeriabd.MensajeriaConvEvento) ([]EventoConversacion, error) {
	if len(filas) == 0 {
		return []EventoConversacion{}, nil
	}
	var msjIDs, sugIDs []uuid.UUID
	var convs []int64
	vistas := map[int64]bool{}
	for _, f := range filas {
		if id := uuidDe(f.MensajeID); id != nil {
			msjIDs = append(msjIDs, *id)
		}
		if id := uuidDe(f.SugerenciaID); id != nil {
			sugIDs = append(sugIDs, *id)
		}
		if f.ConversacionID != nil && !vistas[*f.ConversacionID] {
			vistas[*f.ConversacionID] = true
			convs = append(convs, *f.ConversacionID)
		}
	}
	centralita, err := m.llamadasCentralitaDelFeed(ctx, q, a, filas)
	if err != nil {
		return nil, err
	}
	mensajes := map[uuid.UUID]MensajeConversacion{}
	if len(msjIDs) > 0 {
		fs, err := q.MensajesConversacionPorIDs(ctx, msjIDs)
		if err != nil {
			return nil, err
		}
		ms, err := mensajesConv(ctx, q, fs)
		if err != nil {
			return nil, err
		}
		for _, x := range ms {
			mensajes[x.ID] = x
		}
	}
	sugerencias := map[uuid.UUID]Sugerencia{}
	if len(sugIDs) > 0 {
		fs, err := q.SugerenciasPorIDs(ctx, sugIDs)
		if err != nil {
			return nil, err
		}
		for _, f := range fs {
			sugerencias[f.ID] = aSugerencia(f)
		}
	}
	llamadas := map[uuid.UUID]Llamada{}
	var llIDs []uuid.UUID
	for _, f := range filas {
		if id := uuidDe(f.LlamadaID); id != nil {
			llIDs = append(llIDs, *id)
		}
	}
	if len(llIDs) > 0 {
		fs, err := q.LlamadasPorIDs(ctx, llIDs)
		if err != nil {
			return nil, err
		}
		ls, err := m.aLlamadas(ctx, q, a, fs)
		if err != nil {
			return nil, err
		}
		for _, l := range ls {
			llamadas[l.ID] = l
		}
	}
	cs, err := m.resumirConvs(ctx, q, a.usuario, convs)
	if err != nil {
		return nil, err
	}
	resumenes := map[int64]ConversacionCliente{}
	for _, c := range cs {
		resumenes[c.ID] = c
	}
	out := make([]EventoConversacion, 0, len(filas))
	for _, f := range filas {
		if f.ConversacionID == nil {
			e := EventoConversacion{ID: strconv.FormatInt(f.ID, 10), Tipo: f.Tipo, OcurridoEn: f.OcurridoEn}
			if id := uuidDe(f.CentralitaLlamadaID); id != nil {
				if x, ok := centralita[*id]; ok {
					e.Centralita = &x
				}
			}
			out = append(out, e)
			continue
		}
		conv := *f.ConversacionID
		r := resumenes[conv]
		e := EventoConversacion{ID: strconv.FormatInt(f.ID, 10), Tipo: f.Tipo, ConversacionID: conv, OcurridoEn: f.OcurridoEn, NoLeidos: r.NoLeidos}
		if id := uuidDe(f.MensajeID); id != nil {
			if x, ok := mensajes[*id]; ok {
				e.Mensaje = &x
			}
		}
		if id := uuidDe(f.SugerenciaID); id != nil {
			if x, ok := sugerencias[*id]; ok {
				e.Sugerencia = &x
			}
		}
		if id := uuidDe(f.LlamadaID); id != nil {
			if x, ok := llamadas[*id]; ok {
				e.Llamada = &x
			}
		}
		if f.Tipo != EventoConvMensajeNuevo && f.Tipo != EventoConvMensajeActualizad && f.Tipo != EventoConvSugerencia {
			if _, ok := resumenes[conv]; ok {
				rc := r
				e.Conversacion = &rc
			}
		}
		out = append(out, e)
	}
	return out, nil
}

func nuevoCambiosConv(m *Modulo) *auditoria.Caso[EntradaCambiosConv, CambiosConversaciones] {
	return auditoria.Nuevo(auditoria.Caso[EntradaCambiosConv, CambiosConversaciones]{
		Accion: "conversaciones.cambios", Acceso: auditoria.Autenticado, AuditarExito: sinAuditar[CambiosConversaciones],
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaCambiosConv) (CambiosConversaciones, error) {
			q := mensajeriabd.New(t)
			a, err := alcanceFlujoDe(ctx, q)
			if err != nil {
				return CambiosConversaciones{}, err
			}
			desde, err := cursorConv(ctx, q, a, "cursor", in.Cursor)
			if err != nil {
				return CambiosConversaciones{}, err
			}
			limite := in.Limit
			if limite <= 0 || limite > 200 {
				limite = 100
			}
			if in.Cursor == "" {
				return CambiosConversaciones{Data: []EventoConversacion{}, NextCursor: itoa(desde)}, nil
			}
			evs, hayMas, siguiente, err := m.leerFeedConv(ctx, q, a, desde, limite)
			if err != nil {
				return CambiosConversaciones{}, err
			}
			return CambiosConversaciones{Data: evs, NextCursor: itoa(siguiente), HayMas: hayMas}, nil
		},
	})
}

// SSE -----------------------------------------------------------------------------------------------

// EscucharConversaciones hace LISTEN iris_conversaciones y despierta los flujos SSE de la bandeja
// de la empresa de cada aviso. Se reconecta solo; termina con ctx.
func (m *Modulo) EscucharConversaciones(ctx context.Context, pool *pgxpool.Pool, log *slog.Logger) {
	if log == nil {
		log = slog.Default()
	}
	espera := time.Second
	for ctx.Err() == nil {
		err := func() error {
			conn, err := pool.Acquire(ctx)
			if err != nil {
				return err
			}
			defer conn.Release()
			if _, err := conn.Exec(ctx, "LISTEN "+CanalConversaciones); err != nil {
				return err
			}
			m.difusorConv.avisar("*")
			for {
				n, err := conn.Conn().WaitForNotification(ctx)
				if err != nil {
					return err
				}
				m.difusorConv.avisar(n.Payload)
			}
		}()
		if ctx.Err() != nil {
			return
		}
		log.WarnContext(ctx, "conversaciones: se perdió el LISTEN; reintentando", "err", err, "espera", espera)
		select {
		case <-ctx.Done():
			return
		case <-time.After(espera):
		}
		espera = min(espera*2, 30*time.Second)
	}
}

type flujoConv struct {
	alcance alcanceConv
	desde   int64
	// navegador: el id de «Recibir llamadas» del navegador que abrió el flujo (vacío en paneles viejos).
	navegador string
	// pestana: el id de la pestaña que abrió el flujo (aleatorio por pestaña).
	pestana string
}

func nuevoAbrirFlujoConv(*Modulo) *auditoria.Caso[entradaFlujo, flujoConv] {
	return auditoria.Nuevo(auditoria.Caso[entradaFlujo, flujoConv]{
		// conversaciones.ver, o una extensión activa de la centralita (alcanceFlujoDe).
		Accion: "conversaciones.eventos", Acceso: auditoria.Autenticado, AuditarExito: sinAuditar[flujoConv],
		Logica: func(ctx context.Context, t *auditoria.Tx, in entradaFlujo) (flujoConv, error) {
			q := mensajeriabd.New(t)
			a, err := alcanceFlujoDe(ctx, q)
			if err != nil {
				return flujoConv{}, err
			}
			desde, err := cursorConv(ctx, q, a, "Last-Event-ID", in.Desde)
			return flujoConv{alcance: a, desde: desde}, err
		},
	})
}

func (m *Modulo) abrirFlujoConvSSE(ctx context.Context, desde string) (flujoConv, *suscripcion, func(), error) {
	f, err := m.abrirFlujoConv.Ejecutar(ctx, entradaFlujo{Desde: desde})
	if err != nil {
		return flujoConv{}, nil, nil, err
	}
	sub, quitar, err := m.difusorConv.suscribir(f.alcance.usuario, strconv.FormatInt(f.alcance.empresa, 10), m.cfg.ChatSSEPorUsuario, m.cfg.ChatSSEMax)
	return f, sub, quitar, err
}

// revalidarConv vuelve a leer la credencial y los permisos de hoy. false = cortar.
func (m *Modulo) revalidarConv(ctx context.Context, bd nucleo.BD, donde string) (alcanceConv, bool) {
	m.contarRevalidacion(contexto.ActorDe(ctx).ID, donde)
	nctx, err := identidad.Revalidar(ctx, bd)
	if err != nil {
		return alcanceConv{}, false
	}
	// Sin conversaciones.ver sigue solo si conserva su extensión activa (se vuelve a leer).
	a, err := alcanceFlujoDe(nctx, mensajeriabd.New(bd))
	return a, err == nil
}

// transmitirConv escribe el flujo SSE de la bandeja (mismas reglas que el del chat).
func (m *Modulo) transmitirConv(ctx context.Context, w http.ResponseWriter, f flujoConv, sub *suscripcion, quitar func()) {
	defer quitar()
	rc := http.NewResponseController(w)
	_ = rc.SetReadDeadline(time.Time{})
	plazo := func() { _ = rc.SetWriteDeadline(time.Now().Add(max(30*time.Second, 2*m.cfg.ChatLatido))) }
	plazo()
	h := w.Header()
	h.Set("Content-Type", "text/event-stream; charset=utf-8")
	h.Set("Cache-Control", "no-cache, no-store, no-transform")
	h.Set("X-Accel-Buffering", "no")
	h.Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	bw := bufio.NewWriter(w)
	enviar := func() bool {
		if err := bw.Flush(); err != nil {
			return false
		}
		return rc.Flush() == nil
	}
	_, _ = fmt.Fprintf(bw, "retry: 3000\n\n")
	if !enviar() {
		return
	}
	bd := nucleo.BDDe(ctx)
	if bd == nil {
		return
	}
	latido := time.NewTicker(m.cfg.ChatLatido)
	defer latido.Stop()
	fin := time.NewTimer(m.cfg.ChatDuracionMax)
	defer fin.Stop()
	desde := f.desde
	q := mensajeriabd.New(bd)
	// Este flujo es el «panel abierto» de quien recibe llamadas (contratos/voz.md §3.4): toca sus
	// filas al abrirse y en cada latido, siempre con un alcance recién autorizado (el de apertura o el
	// de la revalidación del latido). Con su navegador, el latido es solo de ese navegador: otra
	// pestaña u otro equipo de la misma persona no lo mantiene vivo (ni su «Recibir llamadas» ni la
	// saliente que hizo desde él).
	var tocado time.Time
	tocar := func(a alcanceConv) {
		tocado = m.ahora()
		if a.soloCentralita {
			return // no recibe llamadas de WhatsApp
		}
		toque := mensajeriabd.TocarReceptoresParams{Empresa: a.empresa, UsuarioID: a.usuario, Ahora: tocado}
		if f.navegador != "" {
			toque.Navegador = &f.navegador
		}
		_ = q.TocarReceptores(ctx, toque)
		// Y el de la saliente que se hizo desde esta misma pestaña (solo ella la sostiene).
		if f.pestana != "" {
			_ = q.TocarPestanaDeLlamada(ctx, mensajeriabd.TocarPestanaDeLlamadaParams{UsuarioID: &a.usuario, Pestana: f.pestana, Ahora: ptrT(tocado)})
		}
	}
	// Mismas reglas que el chat (ver transmitir): sin revalidar arriba, a es el alcance de la última
	// autorización (apertura, revalidación posterior a la lectura o latido).
	a := f.alcance
	tocar(a)
	for {
		// Con despertares seguidos el select puede elegir sub.c una y otra vez antes que el latido: si
		// ya pasó un latido desde el último toque, se revalida y se toca aquí (a lo más una vez por
		// ChatLatido), para que el hueco entre toques no pase de FlujoVivo y la entrante le suene.
		if m.ahora().Sub(tocado) >= m.cfg.ChatLatido {
			ahora, ok := m.revalidarConv(ctx, bd, revalidaToque)
			if !ok {
				return
			}
			a = ahora
			tocar(a)
		}
		relecturas := 0
		for {
			if m.antesDeLeerFlujo != nil {
				m.antesDeLeerFlujo(a.usuario)
			}
			plazo()
			// Revalida después de leer y antes de entregar o de avanzar el cursor.
			evs, hayMas, siguiente, ahora, vale, err := m.leerPaginaFlujoConv(ctx, bd, q, a, desde)
			if err != nil {
				if ctx.Err() == nil {
					slog.WarnContext(ctx, "conversaciones: leer el feed del flujo", "err", err)
				}
				return
			}
			if !vale {
				return
			}
			if ahora != a {
				// Otro alcance: se relee sin mover el cursor, hasta maxRelecturasFlujo veces seguidas.
				if relecturas++; relecturas > maxRelecturasFlujo {
					cerrarPorRelecturas(ctx, "conversaciones", a.usuario)
					return
				}
				a = ahora
				continue
			}
			relecturas = 0
			if len(evs) > 0 {
				for _, e := range evs {
					datos, _ := json.Marshal(e)
					_, _ = fmt.Fprintf(bw, "id: %s\nevent: %s\ndata: %s\n\n", e.ID, e.Tipo, datos)
				}
				if !enviar() {
					return
				}
			}
			desde = siguiente
			if !hayMas {
				break
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-fin.C:
			return
		case <-sub.c:
		case <-latido.C:
			// La única revalidación del latido: su alcance toca los receptores y lee lo que sigue.
			ahora, ok := m.revalidarConv(ctx, bd, revalidaLatido)
			if !ok {
				return
			}
			a = ahora
			tocar(a)
			plazo()
			_, _ = fmt.Fprintf(bw, ": latido\n\n")
			if !enviar() {
				return
			}
		}
	}
}

// leerPaginaFlujoConv es leerPaginaFlujo para la bandeja: lee la página y, en el mismo turno del
// semáforo, revalida si la página mueve el cursor.
func (m *Modulo) leerPaginaFlujoConv(ctx context.Context, bd nucleo.BD, q *mensajeriabd.Queries, a alcanceConv, desde int64) (
	evs []EventoConversacion, hayMas bool, siguiente int64, ahora alcanceConv, vale bool, err error) {
	select {
	case m.lecturas <- struct{}{}:
	case <-ctx.Done():
		return nil, false, desde, a, false, ctx.Err()
	}
	defer func() { <-m.lecturas }()
	evs, hayMas, siguiente, err = m.leerFeedConv(ctx, q, a, desde, 100)
	if err != nil || (len(evs) == 0 && siguiente == desde) {
		return evs, hayMas, siguiente, a, err == nil, err
	}
	ahora, vale = m.revalidarConv(ctx, bd, revalidaPagina)
	return evs, hayMas, siguiente, ahora, vale, nil
}

// purgarFeedConv borra los eventos de la bandeja más viejos que ChatRetencion.
func (m *Modulo) purgarFeedConv(ctx context.Context, bd nucleo.BD) error {
	_, err := mensajeriabd.New(bd).PurgarEventosConv(ctx, m.ahora().Add(-m.cfg.ChatRetencion))
	return err
}

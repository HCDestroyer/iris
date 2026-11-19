package mensajeria

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	mensajeriabd "github.com/HCDestroyer/iris/apps/api/internal/mensajeria/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad"
)

// Tiempo real del chat: GET /chat/eventos (SSE). Cada flujo lee el feed (mensajeria.chat_eventos)
// con la misma consulta que GET /chat/cambios; el NOTIFY iris_chat solo lo despierta. Así reanudar
// con Last-Event-ID, el sondeo y el tiempo real son el mismo cursor, y un NOTIFY perdido solo atrasa
// hasta el siguiente latido.

// difusor reparte los NOTIFY iris_chat entre los flujos SSE del proceso, por empresa, y lleva la
// cuenta de flujos abiertos (por usuario y en total) para los topes.
type difusor struct {
	mu         sync.Mutex
	subs       map[*suscripcion]struct{}
	porUsuario map[int64]int
}

type suscripcion struct {
	usuario int64
	empresa string
	c       chan struct{}
}

func nuevoDifusor() *difusor {
	return &difusor{subs: map[*suscripcion]struct{}{}, porUsuario: map[int64]int{}}
}

// suscribir reserva un flujo: con porUsuario flujos del usuario abiertos, 429
// demasiados_flujos (el cliente cierra uno antes de abrir otro); con maximo en el proceso, 503.
func (d *difusor) suscribir(usuario int64, empresa string, porUsuario, maximo int) (*suscripcion, func(), error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.porUsuario[usuario] >= porUsuario {
		return nil, nil, nucleo.DemasiadosIntentos("demasiados_flujos",
			fmt.Sprintf("Ya tienes %d flujos de eventos abiertos: cierra uno antes de abrir otro", porUsuario), 30*time.Second)
	}
	if len(d.subs) >= maximo {
		return nil, nil, nucleo.NuevoProblema(http.StatusServiceUnavailable, "flujos_agotados", "Sin lugar para más flujos",
			"El servicio de mensajería tiene el máximo de flujos de eventos abiertos: usa GET /chat/cambios o vuelve a intentar").
			ConCabecera("Retry-After", "30")
	}
	s := &suscripcion{usuario: usuario, empresa: empresa, c: make(chan struct{}, 1)}
	d.subs[s] = struct{}{}
	d.porUsuario[usuario]++
	var una sync.Once
	return s, func() {
		una.Do(func() {
			d.mu.Lock()
			defer d.mu.Unlock()
			delete(d.subs, s)
			if d.porUsuario[usuario]--; d.porUsuario[usuario] <= 0 {
				delete(d.porUsuario, usuario)
			}
		})
	}, nil
}

// avisar despierta a los flujos de la empresa ("*" = todos). Nunca bloquea.
func (d *difusor) avisar(empresa string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for s := range d.subs {
		if empresa == "*" || s.empresa == empresa {
			select {
			case s.c <- struct{}{}:
			default:
			}
		}
	}
}

// EscucharChat hace LISTEN iris_chat y despierta los flujos SSE de la empresa de cada aviso. Se
// reconecta solo (al volver despierta a todos, por lo que se haya perdido); termina con ctx.
func (m *Modulo) EscucharChat(ctx context.Context, pool *pgxpool.Pool, log *slog.Logger) {
	if log == nil {
		log = slog.Default()
	}
	espera := time.Second
	for ctx.Err() == nil {
		err := m.escucharChat(ctx, pool)
		if ctx.Err() != nil {
			return
		}
		log.WarnContext(ctx, "chat: se perdió el LISTEN; reintentando", "err", err, "espera", espera)
		select {
		case <-ctx.Done():
			return
		case <-time.After(espera):
		}
		espera = min(espera*2, 30*time.Second)
	}
}

func (m *Modulo) escucharChat(ctx context.Context, pool *pgxpool.Pool) error {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, "LISTEN "+CanalChat); err != nil {
		return err
	}
	m.difusor.avisar("*")
	for {
		n, err := conn.Conn().WaitForNotification(ctx)
		if err != nil {
			return err
		}
		m.difusor.avisar(n.Payload)
	}
}

// Abrir el flujo ------------------------------------------------------------------------------------

type entradaFlujo struct {
	Desde string `json:"desde,omitempty"`
}

type flujo struct {
	alcance alcanceChat
	desde   int64
}

func nuevoAbrirFlujo(*Modulo) *auditoria.Caso[entradaFlujo, flujo] {
	return auditoria.Nuevo(auditoria.Caso[entradaFlujo, flujo]{
		Accion: "chat.eventos", Permiso: nucleo.PermChatVer, AuditarExito: sinAuditar[flujo],
		Logica: func(ctx context.Context, t *auditoria.Tx, in entradaFlujo) (flujo, error) {
			a, err := alcanceDe(ctx)
			if err != nil {
				return flujo{}, err
			}
			desde, err := cursorFeed(ctx, mensajeriabd.New(t), a, "Last-Event-ID", in.Desde)
			return flujo{alcance: a, desde: desde}, err
		},
	})
}

// abrirFlujoSSE autoriza el flujo (permiso, cursor) y reserva su lugar en los topes. quitar libera
// el lugar (lo llama transmitir al terminar).
func (m *Modulo) abrirFlujoSSE(ctx context.Context, desde string) (flujo, *suscripcion, func(), error) {
	f, err := m.Chat.abrirFlujo.Ejecutar(ctx, entradaFlujo{Desde: desde})
	if err != nil {
		return flujo{}, nil, nil, err
	}
	sub, quitar, err := m.difusor.suscribir(f.alcance.usuario, f.alcance.clave(), m.cfg.ChatSSEPorUsuario, m.cfg.ChatSSEMax)
	return f, sub, quitar, err
}

// revalidar vuelve a leer la credencial del flujo y el alcance del usuario desde la base (sesión o
// token vigente, usuario activo y en la empresa, permisos de hoy). false = cortar el flujo.
func (m *Modulo) revalidar(ctx context.Context, bd nucleo.BD) (alcanceChat, bool) {
	nctx, err := identidad.Revalidar(ctx, bd)
	if err != nil {
		return alcanceChat{}, false
	}
	if !contexto.PermisosDe(nctx).Tiene(string(nucleo.PermChatVer)) {
		return alcanceChat{}, false
	}
	a, err := alcanceDe(nctx)
	return a, err == nil
}

// igual compara dos alcances del chat (las zonas y cuadrillas sin importar el orden).
func (a alcanceChat) igual(b alcanceChat) bool {
	mismos := func(x, y []int64) bool {
		x, y = slices.Clone(x), slices.Clone(y)
		slices.Sort(x)
		slices.Sort(y)
		return slices.Equal(x, y)
	}
	return a.usuario == b.usuario && mismoID(a.empresa, b.empresa) && a.todas == b.todas && a.global == b.global &&
		a.ordGlobal == b.ordGlobal && mismos(a.zonas, b.zonas) && mismos(a.cuadrillas, b.cuadrillas) &&
		mismos(a.ordZonas, b.ordZonas) && mismos(a.ordCuadrillas, b.ordCuadrillas)
}

// maxRelecturasFlujo: veces seguidas que un flujo SSE (chat o bandeja) relee una página porque el
// alcance cambió entre la revalidación y la lectura. Pasado el tope se cierra (el cliente reconecta).
const maxRelecturasFlujo = 3

// leerPaginaFlujo lee una página del feed y, en el mismo turno del semáforo de lecturas
// (ChatLecturas), vuelve a revalidar la credencial si la página mueve el cursor (trae eventos o salta
// eventos que este alcance no ve). Lo leído se confirmó antes de esa segunda revalidación: nada
// enviado después de perder el acceso se entrega, y un alcance que cambió en medio (rol quitado o
// agregado) se detecta antes de entregar o saltar nada. vale = false: cortar el flujo. Sin mover el
// cursor no revalida y ahora es a.
func (m *Modulo) leerPaginaFlujo(ctx context.Context, bd nucleo.BD, q *mensajeriabd.Queries, a alcanceChat, desde int64) (
	evs []EventoChat, hayMas bool, siguiente int64, ahora alcanceChat, vale bool, err error) {
	select {
	case m.lecturas <- struct{}{}:
	case <-ctx.Done():
		return nil, false, desde, a, false, ctx.Err()
	}
	defer func() { <-m.lecturas }()
	evs, hayMas, siguiente, err = m.leerFeed(ctx, q, a, desde, 100)
	if err != nil || (len(evs) == 0 && siguiente == desde) {
		return evs, hayMas, siguiente, a, err == nil, err
	}
	m.contarRevalidacion(a.usuario, revalidaPagina)
	ahora, vale = m.revalidar(ctx, bd)
	return evs, hayMas, siguiente, ahora, vale, nil
}

// Dónde revalida un flujo SSE (solo para el gancho de pruebas alRevalidarFlujo).
const (
	revalidaPagina = "pagina" // después de leer una página que mueve el cursor
	revalidaLatido = "latido"
)

func (m *Modulo) contarRevalidacion(usuario int64, donde string) {
	if m.alRevalidarFlujo != nil {
		m.alRevalidarFlujo(usuario, donde)
	}
}

// cerrarPorRelecturas deja constancia (solo ids y motivo) de un flujo que se cierra porque el
// alcance cambió en cada lectura (maxRelecturasFlujo); el cliente reconecta.
func cerrarPorRelecturas(ctx context.Context, flujo string, usuario int64) {
	slog.InfoContext(ctx, flujo+": flujo SSE cerrado por el tope de relecturas", "flujo", flujo,
		"usuario_id", usuario, "motivo", "alcance_inestable", "relecturas", maxRelecturasFlujo)
}

// transmitir escribe el flujo SSE hasta que el cliente se va, ctx termina, pasa ChatDuracionMax o
// la credencial deja de valer. Sin transacción abierta: cada lectura del feed usa el pool.
func (m *Modulo) transmitir(ctx context.Context, w http.ResponseWriter, f flujo, sub *suscripcion, quitar func()) {
	defer quitar()
	rc := http.NewResponseController(w)
	// El servidor tiene WriteTimeout y ReadTimeout de 60 s: este flujo vive más (y el ReadTimeout,
	// al vencer, cancelaría el contexto de la petición). En su lugar, cada escritura tiene su plazo:
	// un cliente que dejó de leer (sin señal) no deja la goroutine colgada.
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
	// Sin revalidar arriba: a es siempre el alcance de la última autorización (la de apertura en el
	// primer ciclo; después, la revalidación posterior a la última lectura que movió el cursor o la
	// del latido). Leer con un alcance viejo no entrega nada: toda entrega y todo avance del cursor
	// pasan por la revalidación posterior a la lectura (leerPaginaFlujo), y una página que no mueve
	// el cursor no entrega nada. Una credencial perdida sin eventos que muevan el cursor se corta en
	// el siguiente latido.
	a := f.alcance
	for {
		// Todo lo pendiente desde el cursor, en páginas.
		relecturas := 0
		for {
			if m.antesDeLeerFlujo != nil {
				m.antesDeLeerFlujo(a.usuario)
			}
			plazo()
			// a no basta: entre su autorización y la lectura (la espera, el semáforo, el pool) se
			// pudo cerrar la sesión y enviar un mensaje. leerPaginaFlujo revalida otra vez, después de
			// leer y antes de entregar o de avanzar el cursor.
			evs, hayMas, siguiente, ahora, vale, err := m.leerPaginaFlujo(ctx, bd, q, a, desde)
			if err != nil {
				if ctx.Err() == nil {
					slog.WarnContext(ctx, "chat: leer el feed del flujo", "err", err)
				}
				return
			}
			if !vale {
				return
			}
			if !ahora.igual(a) {
				// El alcance cambió en medio: la página se descarta y se relee con el de ahora, sin
				// mover el cursor (con uno más amplio no se salta lo que ahora le toca ver).
				if relecturas++; relecturas > maxRelecturasFlujo {
					cerrarPorRelecturas(ctx, "chat", a.usuario)
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
			// La única revalidación del latido: su alcance es el de la lectura que sigue.
			m.contarRevalidacion(a.usuario, revalidaLatido)
			ahora, ok := m.revalidar(ctx, bd)
			if !ok {
				return
			}
			a = ahora
			plazo()
			_, _ = fmt.Fprintf(bw, ": latido\n\n")
			if !enviar() {
				return
			}
		}
	}
}

// Purga -------------------------------------------------------------------------------------------

// purgarFeedChat borra los eventos del feed más viejos que ChatRetencion (reloj del módulo).
func (m *Modulo) purgarFeedChat(ctx context.Context, bd nucleo.BD) error {
	_, err := mensajeriabd.New(bd).PurgarEventosChat(ctx, m.ahora().Add(-m.cfg.ChatRetencion))
	return err
}

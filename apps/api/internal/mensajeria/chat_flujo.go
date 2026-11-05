package mensajeria

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	mensajeriabd "github.com/HCDestroyer/iris/apps/api/internal/mensajeria/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
)

// Tiempo real del chat: GET /chat/eventos (SSE). Cada flujo lee el feed (mensajeria.chat_eventos)
// con la misma consulta que GET /chat/cambios; el NOTIFY iris_chat solo lo despierta. Así reanudar
// con Last-Event-ID, el sondeo y el tiempo real son el mismo cursor, y un NOTIFY perdido solo atrasa
// hasta el siguiente latido.

// difusor reparte los NOTIFY iris_chat entre los flujos SSE del proceso, por empresa.
type difusor struct {
	mu   sync.Mutex
	subs map[*suscripcion]struct{}
}

type suscripcion struct {
	empresa string
	c       chan struct{}
}

func nuevoDifusor() *difusor { return &difusor{subs: map[*suscripcion]struct{}{}} }

func (d *difusor) suscribir(empresa string) (*suscripcion, func()) {
	s := &suscripcion{empresa: empresa, c: make(chan struct{}, 1)}
	d.mu.Lock()
	d.subs[s] = struct{}{}
	d.mu.Unlock()
	return s, func() {
		d.mu.Lock()
		delete(d.subs, s)
		d.mu.Unlock()
	}
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

// transmitir escribe el flujo SSE hasta que el cliente se va, ctx termina o pasa ChatDuracionMax.
// Sin transacción abierta: cada lectura del feed usa el pool.
func (m *Modulo) transmitir(ctx context.Context, w http.ResponseWriter, f flujo) {
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
	sub, quitar := m.difusor.suscribir(f.alcance.clave())
	defer quitar()
	latido := time.NewTicker(m.cfg.ChatLatido)
	defer latido.Stop()
	fin := time.NewTimer(m.cfg.ChatDuracionMax)
	defer fin.Stop()
	desde := f.desde
	q := mensajeriabd.New(bd)
	for {
		// Todo lo pendiente desde el cursor, en páginas.
		for {
			plazo()
			evs, hayMas, siguiente, err := m.leerFeed(ctx, q, f.alcance, desde, 100)
			if err != nil {
				if ctx.Err() == nil {
					slog.WarnContext(ctx, "chat: leer el feed del flujo", "err", err)
				}
				return
			}
			for _, e := range evs {
				datos, _ := json.Marshal(e)
				_, _ = fmt.Fprintf(bw, "id: %s\nevent: %s\ndata: %s\n\n", e.ID, e.Tipo, datos)
			}
			desde = siguiente
			if len(evs) > 0 && !enviar() {
				return
			}
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

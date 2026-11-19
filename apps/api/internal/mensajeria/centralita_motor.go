package mensajeria

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/HCDestroyer/iris/apps/api/internal/mensajeria/ami"
	mensajeriabd "github.com/HCDestroyer/iris/apps/api/internal/mensajeria/bd"
)

// El motor de la centralita (E4a): una conexión AMI por centralita activa, de solo lectura, en el
// proceso de mensajería. Con varias réplicas solo una conecta cada centralita: la que tiene su
// advisory lock (en una conexión de Postgres dedicada mientras dura la sesión).
//
//   - Reconexión con espera exponencial (1 s a 60 s). Una caída de menos de 60 s no cambia el estado
//     (ni avisa): así un corte breve no inunda de eventos.
//   - Huella distinta o permisos de más: la conexión se detiene hasta que un administrador lo
//     resuelva (eso sube la versión de la centralita y el motor la vuelve a arrancar).
//   - Ping cada 20 s; sin respuesta en 10 s se reconecta.
//   - Solo pasan a la base los eventos de extensiones registradas o de llamadas que Iris ya sigue,
//     con un tope de eventos por segundo; los demás se descartan sin mirar.
//   - Las llamadas que suenan o están en curso mientras Iris no escucha no se recuperan (fuera de
//     alcance de E4a); las que quedaron abiertas se cierran solas (CentralitaSonandoMax/EnCursoMax).

// CanalCentralitas: NOTIFY que despierta al motor cuando cambia una centralita o sus extensiones.
const CanalCentralitas = "iris_centralitas"

// Ajustes del motor (pruebas: se acortan con Config).
const (
	centralitaRevision   = 30 * time.Second
	centralitaGracia     = 60 * time.Second
	centralitaEsperaMax  = 60 * time.Second
	centralitaPing       = 20 * time.Second
	centralitaEventosSeg = 200
	centralitaSeguidas   = 2000
)

type corridaCentralita struct {
	version   int32
	cancelar  context.CancelFunc
	hecho     chan struct{}
	refrescar chan struct{}
}

// CorrerCentralitas mantiene una sesión AMI por centralita activa hasta que termina ctx.
func (m *Modulo) CorrerCentralitas(ctx context.Context, pool *pgxpool.Pool, log *slog.Logger) {
	if log == nil {
		log = slog.Default()
	}
	despertar := make(chan string, 64)
	go m.escucharCentralitas(ctx, pool, log, despertar)
	corridas := map[int64]*corridaCentralita{}
	var mu sync.Mutex
	revisar := func() {
		fs, err := mensajeriabd.New(pool).CentralitasActivas(ctx)
		if err != nil {
			if ctx.Err() == nil {
				log.WarnContext(ctx, "centralita: leer las centralitas activas", "err", err)
			}
			return
		}
		mu.Lock()
		defer mu.Unlock()
		activas := map[int64]int32{}
		for _, f := range fs {
			activas[f.ID] = f.Version
		}
		for id, c := range corridas {
			select {
			case <-c.hecho:
				delete(corridas, id)
				continue
			default:
			}
			if v, ok := activas[id]; !ok || v != c.version {
				c.cancelar()
				<-c.hecho
				delete(corridas, id)
			}
		}
		for id, v := range activas {
			if _, ok := corridas[id]; ok {
				continue
			}
			cctx, cancelar := context.WithCancel(ctx)
			c := &corridaCentralita{version: v, cancelar: cancelar, hecho: make(chan struct{}), refrescar: make(chan struct{}, 1)}
			corridas[id] = c
			go func() {
				defer close(c.hecho)
				m.correrCentralita(cctx, pool, log.With("centralita_id", id), id, v, c.refrescar)
			}()
		}
	}
	revisar()
	t := time.NewTicker(m.cfg.CentralitaRevision)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			mu.Lock()
			for _, c := range corridas {
				c.cancelar()
				<-c.hecho
			}
			mu.Unlock()
			return
		case carga := <-despertar:
			revisar()
			if id, err := strconv.ParseInt(carga, 10, 64); err == nil {
				mu.Lock()
				if c, ok := corridas[id]; ok {
					select {
					case c.refrescar <- struct{}{}:
					default:
					}
				}
				mu.Unlock()
			}
		case <-t.C:
			revisar()
		}
	}
}

func (m *Modulo) escucharCentralitas(ctx context.Context, pool *pgxpool.Pool, log *slog.Logger, despertar chan<- string) {
	espera := time.Second
	for ctx.Err() == nil {
		err := func() error {
			conn, err := pool.Acquire(ctx)
			if err != nil {
				return err
			}
			defer conn.Release()
			if _, err := conn.Exec(ctx, "LISTEN "+CanalCentralitas); err != nil {
				return err
			}
			espera = time.Second
			for {
				n, err := conn.Conn().WaitForNotification(ctx)
				if err != nil {
					return err
				}
				select {
				case despertar <- n.Payload:
				default:
				}
			}
		}()
		if ctx.Err() != nil {
			return
		}
		log.WarnContext(ctx, "centralita: se perdió el LISTEN; reintentando", "err", err)
		select {
		case <-ctx.Done():
			return
		case <-time.After(espera):
		}
		espera = min(espera*2, 30*time.Second)
	}
}

// correrCentralita toma el candado de la centralita y mantiene su sesión AMI.
func (m *Modulo) correrCentralita(ctx context.Context, pool *pgxpool.Pool, log *slog.Logger, id int64, version int32, refrescar <-chan struct{}) {
	for ctx.Err() == nil {
		conn, err := pool.Acquire(ctx)
		if err != nil {
			esperar(ctx, m.cfg.CentralitaRevision)
			continue
		}
		clave := "centralita:" + strconv.FormatInt(id, 10)
		var tengo bool
		if err := conn.QueryRow(ctx, "SELECT pg_try_advisory_lock(hashtextextended($1, 0))", clave).Scan(&tengo); err != nil || !tengo {
			conn.Release()
			esperar(ctx, m.cfg.CentralitaRevision) // otra réplica la tiene
			continue
		}
		m.sesionesCentralita(ctx, pool, log, id, version, refrescar)
		// Suelta el candado antes de devolver la conexión al pool (el candado es de la sesión).
		_, _ = conn.Exec(context.WithoutCancel(ctx), "SELECT pg_advisory_unlock(hashtextextended($1, 0))", clave)
		conn.Release()
		return
	}
}

func esperar(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// sesionesCentralita conecta, escucha y reconecta mientras la centralita siga activa con esta versión.
func (m *Modulo) sesionesCentralita(ctx context.Context, pool *pgxpool.Pool, log *slog.Logger, id int64, version int32, refrescar <-chan struct{}) {
	sctx := ctxSistema(ctx, pool)
	q := mensajeriabd.New(pool)
	espera := time.Second
	var caida *time.Time
	avisada := false
	fijar := func(estado string, codigo, huella *string) {
		e := entradaEstadoCentralita{ID: id, Version: version, Estado: estado, Codigo: codigo, Huella: huella, Desde: caida}
		if _, err := m.Centralita.fijarEstado.Ejecutar(sctx, e); err != nil && ctx.Err() == nil {
			log.WarnContext(ctx, "centralita: guardar el estado", "err", err)
		}
	}
	detenida := func(codigo string, huella *string) {
		fijar(CentralitaError, &codigo, huella)
		<-ctx.Done() // hasta que un administrador lo resuelva (cambia la versión)
	}
	for ctx.Err() == nil {
		f, err := q.CentralitaDelMotor(ctx, id)
		if err != nil || !f.Activa || f.Version != version || f.HuellaTls == nil {
			return
		}
		if f.ErrorCodigo != nil && (*f.ErrorCodigo == "huella_cambiada" || *f.ErrorCodigo == "permisos_de_mas") {
			<-ctx.Done()
			return
		}
		secreto, err := m.descifrarCentralita(f)
		if err != nil {
			log.WarnContext(ctx, "centralita: no se pudo descifrar la contraseña del AMI")
			detenida("credenciales", nil)
			return
		}
		o := m.opcionesAMI(f.Host, f.Puerto, f.Usuario, secreto, *f.HuellaTls)
		o.Eventos = "call,cdr"
		c, err := ami.Conectar(ctx, o)
		secreto = ""
		if err != nil {
			var eh *ami.ErrHuella
			switch {
			case errors.As(err, &eh):
				log.WarnContext(ctx, "centralita: la huella del certificado cambió; la conexión se detiene hasta que un administrador la confirme")
				v := eh.Vista
				detenida("huella_cambiada", &v)
				return
			case errors.Is(err, ami.ErrCredenciales):
				log.WarnContext(ctx, "centralita: el AMI rechazó el usuario o la contraseña")
				cod := "credenciales"
				fijar(CentralitaError, &cod, nil)
				if !esperar(ctx, 5*centralitaEsperaMax) {
					return
				}
				continue
			}
			ahora := m.ahora()
			if caida == nil {
				caida = &ahora
			}
			if !avisada && ahora.Sub(*caida) >= m.cfg.CentralitaGracia {
				avisada = true
				fijar(CentralitaDesconectada, nil, nil)
			}
			if !esperar(ctx, espera) {
				return
			}
			espera = min(espera*2, centralitaEsperaMax)
			continue
		}
		// El mínimo privilegio se comprueba en cada conexión, no solo al probar.
		p, err := c.Accion(ctx, "ListCommands")
		if err == nil && len(ami.PermisosDeMas(p)) > 0 {
			c.Cerrar()
			log.WarnContext(ctx, "centralita: el usuario AMI tiene permisos de escritura; la conexión se detiene", "acciones", ami.PermisosDeMas(p))
			detenida("permisos_de_mas", nil)
			return
		}
		fijar(CentralitaConectada, nil, nil)
		caida, avisada, espera = nil, false, time.Second
		m.escucharAMI(ctx, sctx, q, log, f, c, refrescar)
		c.Cerrar()
		ahora := m.ahora()
		caida = &ahora
	}
}

// escucharAMI procesa los eventos de una sesión hasta que se corta.
func (m *Modulo) escucharAMI(ctx, sctx context.Context, q *mensajeriabd.Queries, log *slog.Logger, f mensajeriabd.MensajeriaCentralita, c *ami.Conexion,
	refrescar <-chan struct{}) {
	extensiones := map[string]bool{}
	cargar := func() {
		xs, err := q.ExtensionesDelMotor(ctx, f.ID)
		if err != nil {
			return
		}
		nuevas := make(map[string]bool, len(xs))
		for _, x := range xs {
			nuevas[x.Extension] = true
		}
		extensiones = nuevas
	}
	cargar()
	registrada := func(ext string) bool { return extensiones[ext] }
	// Las llamadas que Iris sigue (para que Hangup, DialEnd y CDR de otras ni toquen la base).
	seguidas := map[string]time.Time{}
	if vivas, err := q.LlamadasCentralitaVivas(ctx, mensajeriabd.LlamadasCentralitaVivasParams{CentralitaID: f.ID, Antes: m.ahora().Add(time.Hour)}); err == nil {
		for _, l := range vivas {
			seguidas[l.IDExterno] = l.SonoEn
		}
	}
	ping := time.NewTicker(m.cfg.CentralitaPing)
	defer ping.Stop()
	mant := time.NewTicker(time.Minute)
	defer mant.Stop()
	recarga := time.NewTicker(m.cfg.CentralitaRevision)
	defer recarga.Stop()
	var tocado time.Time
	ventana, enVentana, descartados := m.ahora(), 0, int64(0)
	avisarDescarte := time.Time{}
	for {
		select {
		case <-ctx.Done():
			return
		case <-refrescar:
			cargar()
		case <-recarga.C:
			cargar()
		case <-mant.C:
			if _, err := m.Centralita.cerrarViejas.Ejecutar(sctx, f.ID); err != nil && ctx.Err() == nil {
				log.WarnContext(ctx, "centralita: cerrar llamadas viejas", "err", err)
			}
			limite := m.ahora().Add(-CentralitaEnCursoMax)
			for k, en := range seguidas {
				if en.Before(limite) {
					delete(seguidas, k)
				}
			}
		case <-ping.C:
			pctx, cancelar := context.WithTimeout(ctx, 10*time.Second)
			_, err := c.Accion(pctx, "Ping")
			cancelar()
			if err != nil {
				log.InfoContext(ctx, "centralita: el AMI no contestó el Ping; se reconecta")
				return
			}
		case p, ok := <-c.Eventos():
			if !ok {
				if errors.Is(c.Err(), ami.ErrProtocolo) {
					log.WarnContext(ctx, "centralita: el AMI mandó algo que no es válido o demasiado grande; se reconecta")
				}
				return
			}
			ahora := m.ahora()
			if ahora.Sub(tocado) >= time.Minute {
				tocado = ahora
				_ = q.TocarCentralita(ctx, mensajeriabd.TocarCentralitaParams{ID: f.ID, Ahora: &ahora, HaceMinuto: ahora.Add(-time.Minute)})
			}
			// Tope de eventos por segundo (una centralita que inunda no tumba a Iris).
			if ahora.Sub(ventana) >= time.Second {
				ventana, enVentana = ahora, 0
			}
			if enVentana++; enVentana > m.cfg.CentralitaEventosSeg {
				descartados++
				if ahora.Sub(avisarDescarte) >= time.Minute {
					avisarDescarte = ahora
					log.WarnContext(ctx, "centralita: demasiados eventos por segundo; se descartan", "descartados", descartados, "cola", c.Descartados.Load())
				}
				continue
			}
			if p.Valor("Event") == "Cdr" {
				p = linkedidDeCDR(p)
			}
			h := traducirEvento(p, registrada)
			if h.tipo == hechoSinInteres {
				continue
			}
			switch h.tipo {
			case hechoSuena, hechoMarca:
				if h.interno {
					continue
				}
				if _, ok := seguidas[h.linkedid]; !ok && len(seguidas) >= centralitaSeguidas {
					continue
				}
				seguidas[h.linkedid] = ahora
			default:
				if _, ok := seguidas[h.linkedid]; !ok {
					continue
				}
			}
			cambio, err := m.Centralita.procesar.Ejecutar(sctx, entradaHechoAMI{CentralitaID: f.ID, Hecho: h, Tipo: h.tipo, Extension: h.extension})
			if err != nil && ctx.Err() == nil {
				log.WarnContext(ctx, "centralita: procesar un evento", "tipo", h.tipo, "err", err)
			}
			if h.tipo == hechoRegistro || (h.tipo == hechoCuelga && cambio) {
				delete(seguidas, h.linkedid)
			}
			if (h.tipo == hechoMarca || h.tipo == hechoSuena) && !cambio && err == nil {
				// No se guardó (saliente sin cliente): no se sigue.
				if ex, e := q.LlamadaCentralitaExiste(ctx, mensajeriabd.LlamadaCentralitaExisteParams{CentralitaID: f.ID, IDExterno: h.linkedid}); e == nil && !ex {
					delete(seguidas, h.linkedid)
				}
			}
		}
	}
}

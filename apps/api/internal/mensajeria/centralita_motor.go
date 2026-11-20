package mensajeria

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/HCDestroyer/iris/apps/api/internal/mensajeria/ami"
	mensajeriabd "github.com/HCDestroyer/iris/apps/api/internal/mensajeria/bd"
)

// El motor de la centralita (E4a): una conexión AMI por centralita activa, de solo lectura, en el
// proceso de mensajería. Con varias réplicas solo una conecta cada centralita: la que tiene su
// arriendo (motor_dueno, motor_hasta), que renueva en cada latido. No retiene conexiones de
// Postgres: el aviso de cambios llega por el mismo LISTEN de la bandeja (EscucharConversaciones).
//
//   - Reconexión con espera exponencial (1 s a 60 s). Una caída de menos de 60 s no cambia el estado
//     (ni avisa): así un corte breve no inunda de eventos.
//   - Huella distinta o permisos de más: la conexión se detiene hasta que un administrador lo
//     resuelva (eso sube la versión de la centralita y el motor la vuelve a arrancar).
//   - Ping cada 20 s; sin respuesta en 10 s se reconecta.
//   - Solo pasan a la base los eventos de extensiones registradas o de llamadas que Iris ya sigue;
//     los demás se descartan sin mirar. El tope de eventos por segundo se aplica después de filtrar y
//     solo a las llamadas nuevas: lo de las llamadas ya seguidas (contestar, colgar, CDR) nunca se
//     descarta. La cola del lector guarda lugar para los cierres (Hangup y Cdr).
//   - Cada consulta propia del motor (arriendo, última señal, estado) tiene plazo: una fila bloqueada
//     (rotación de claves) no lo traba.
//   - El arriendo se renueva antes de cada conexión y durante cada espera de reconexión: si otra
//     réplica lo tomó, esta no se conecta.
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
	despertar := m.despertarCentralitas
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

// correrCentralita toma el arriendo de la centralita y mantiene su sesión AMI.
func (m *Modulo) correrCentralita(ctx context.Context, pool *pgxpool.Pool, log *slog.Logger, id int64, version int32, refrescar <-chan struct{}) {
	q := mensajeriabd.New(pool)
	for ctx.Err() == nil {
		if m.tomarMotor(ctx, q, id) != arriendoTomado {
			esperar(ctx, m.cfg.CentralitaRevision) // otra réplica la tiene (o la base no contestó a tiempo)
			continue
		}
		if perdido := m.sesionesCentralita(ctx, pool, log, id, version, refrescar); perdido {
			// Otra réplica tomó el arriendo: no se conecta; se vuelve a esperar a que lo suelte.
			esperar(ctx, m.cfg.CentralitaRevision)
			continue
		}
		_ = q.SoltarMotorCentralita(context.WithoutCancel(ctx), mensajeriabd.SoltarMotorCentralitaParams{ID: id, Dueno: &m.idProceso})
		return
	}
}

// arriendo: lo que pasó al tomar o renovar el arriendo.
type arriendo int

const (
	arriendoTomado   arriendo = iota // es de esta réplica
	arriendoAjeno                    // lo tiene otra réplica (o la centralita se apagó)
	arriendoIncierto                 // la base no contestó a tiempo (p. ej. la fila está bloqueada por una rotación de claves)
)

// segundosArriendo: el arriendo vale 3 latidos.
func (m *Modulo) segundosArriendo() int32 {
	return int32(max(3*m.cfg.CentralitaPing/time.Second, 3)) //nolint:gosec // segundos acotados
}

// plazoBD: lo que el motor espera a la base en cada consulta propia (el arriendo, la última señal,
// el estado). Una fila bloqueada (la rotación de IRIS_CLAVE_CIFRADO la lee con FOR UPDATE) no lo deja
// trabado: sigue haciendo Ping y vuelve a intentar en el latido siguiente.
func (m *Modulo) plazoBD() time.Duration {
	return min(m.cfg.CentralitaPlazo, m.cfg.CentralitaPing)
}

// tomarMotor toma o renueva el arriendo, con plazo.
func (m *Modulo) tomarMotor(ctx context.Context, q *mensajeriabd.Queries, id int64) arriendo {
	tctx, cancelar := context.WithTimeout(ctx, m.plazoBD())
	defer cancelar()
	_, err := q.TomarMotorCentralita(tctx, mensajeriabd.TomarMotorCentralitaParams{ID: id, Dueno: &m.idProceso, Segundos: m.segundosArriendo()})
	switch {
	case err == nil:
		return arriendoTomado
	case errors.Is(err, pgx.ErrNoRows):
		return arriendoAjeno
	default:
		return arriendoIncierto
	}
}

// esperarConArriendo espera d renovando el arriendo en cada latido (una espera de reconexión de
// hasta 60 s, o de 5 min con credenciales rechazadas, no lo deja vencer). seguir es false si terminó
// ctx o se perdió el arriendo; perdido dice cuál.
func (m *Modulo) esperarConArriendo(ctx context.Context, q *mensajeriabd.Queries, id int64, d time.Duration) (seguir, perdido bool) {
	fin := time.Now().Add(d)
	for {
		resta := time.Until(fin)
		if resta <= 0 {
			return true, false
		}
		if !esperar(ctx, min(resta, m.cfg.CentralitaPing)) {
			return false, false
		}
		if m.tomarMotor(ctx, q, id) != arriendoTomado {
			return false, ctx.Err() == nil
		}
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

// sesionesCentralita conecta, escucha y reconecta mientras la centralita siga activa con esta versión
// y esta réplica tenga el arriendo: lo renueva antes de cada conexión y durante cada espera. Devuelve
// perdido = true si otra réplica lo tomó (entonces no conecta).
func (m *Modulo) sesionesCentralita(ctx context.Context, pool *pgxpool.Pool, log *slog.Logger, id int64, version int32, refrescar <-chan struct{}) (perdido bool) {
	sctx := ctxSistema(ctx, pool)
	q := mensajeriabd.New(pool)
	espera := time.Second
	var caida *time.Time
	avisada := false
	fijar := func(estado string, codigo, huella *string) {
		e := entradaEstadoCentralita{ID: id, Version: version, Estado: estado, Codigo: codigo, Huella: huella, Desde: caida}
		fctx, cancelar := context.WithTimeout(sctx, m.cfg.CentralitaPlazo)
		defer cancelar()
		if _, err := m.Centralita.fijarEstado.Ejecutar(fctx, e); err != nil && ctx.Err() == nil {
			log.WarnContext(ctx, "centralita: guardar el estado", "err", err)
		}
	}
	detenida := func(codigo string, huella *string) {
		fijar(CentralitaError, &codigo, huella)
		<-ctx.Done() // hasta que un administrador lo resuelva (cambia la versión)
	}
	// reintentar: la conexión falló; tras la gracia sin conectar avisa y espera (exponencial) con el
	// arriendo renovado.
	reintentar := func() (sigue, perd bool) {
		ahora := time.Now()
		if caida == nil {
			caida = &ahora
		}
		if !avisada && ahora.Sub(*caida) >= m.cfg.CentralitaGracia {
			avisada = true
			fijar(CentralitaDesconectada, nil, nil)
		}
		sigue, perd = m.esperarConArriendo(ctx, q, id, espera)
		espera = min(espera*2, centralitaEsperaMax)
		return sigue, perd
	}
	for ctx.Err() == nil {
		// Cada vuelta (también cada reconexión) renueva el arriendo: si otra réplica lo tomó, no se conecta.
		// Sin confirmar el arriendo (otra réplica lo tiene o la base no contestó a tiempo) no se conecta.
		if a := m.tomarMotor(ctx, q, id); a != arriendoTomado {
			if ctx.Err() != nil {
				return false
			}
			if a == arriendoAjeno {
				log.WarnContext(ctx, "centralita: otra réplica tiene el arriendo de la conexión; esta no se conecta")
			}
			return true
		}
		f, err := q.CentralitaDelMotor(ctx, id)
		if err != nil || !f.Activa || f.Version != version || f.HuellaTls == nil {
			return false
		}
		if f.ErrorCodigo != nil && (*f.ErrorCodigo == "huella_cambiada" || *f.ErrorCodigo == "permisos_de_mas") {
			<-ctx.Done()
			return false
		}
		secreto, err := m.descifrarCentralita(f)
		if err != nil {
			log.WarnContext(ctx, "centralita: no se pudo descifrar la contraseña del AMI")
			detenida("credenciales", nil)
			return false
		}
		o := m.opcionesAMI(f.Host, f.Puerto, f.Usuario, secreto, *f.HuellaTls)
		o.Eventos = "call,cdr"
		c, err := ami.Conectar(ctx, o)
		if err != nil {
			var eh *ami.ErrHuella
			switch {
			case errors.As(err, &eh):
				log.WarnContext(ctx, "centralita: la huella del certificado cambió; la conexión se detiene hasta que un administrador la confirme")
				v := eh.Vista
				detenida("huella_cambiada", &v)
				return false
			case errors.Is(err, ami.ErrCredenciales):
				log.WarnContext(ctx, "centralita: el AMI rechazó el usuario o la contraseña")
				cod := "credenciales"
				fijar(CentralitaError, &cod, nil)
				if seguir, perd := m.esperarConArriendo(ctx, q, id, 5*centralitaEsperaMax); !seguir {
					return perd
				}
				continue
			}
			if seguir, perd := reintentar(); !seguir {
				return perd
			}
			continue
		}
		// El mínimo privilegio se comprueba en cada conexión, no solo al probar. Sin respuesta en el
		// plazo (o con la conexión caída) es un error de conexión: se reconecta, nunca queda conectada
		// sin haber comprobado los permisos.
		lctx, cancelar := context.WithTimeout(ctx, m.cfg.CentralitaPlazo)
		p, err := c.Accion(lctx, "ListCommands")
		cancelar()
		if err == nil && !ami.Exito(p) {
			err = ami.ErrProtocolo // sin Success no hay permisos comprobados: como no contestar
		}
		if err != nil {
			c.Cerrar()
			if ctx.Err() != nil {
				return false
			}
			log.InfoContext(ctx, "centralita: el AMI no contestó bien ListCommands; se reconecta")
			if seguir, perd := reintentar(); !seguir {
				return perd
			}
			continue
		}
		if de := ami.PermisosDeMas(p); len(de) > 0 {
			c.Cerrar()
			log.WarnContext(ctx, "centralita: el usuario AMI tiene permisos de escritura; la conexión se detiene", "acciones", de)
			detenida("permisos_de_mas", nil)
			return false
		}
		fijar(CentralitaConectada, nil, nil)
		caida, avisada, espera = nil, false, time.Second
		m.escucharAMI(ctx, sctx, q, log, f, c, refrescar)
		c.Cerrar()
		ahora := time.Now()
		caida = &ahora
	}
	return false
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
	renovado := time.Now() // sesionesCentralita lo acaba de tomar
	ventana, enVentana, descartados := time.Now(), 0, int64(0)
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
			limite := time.Now().Add(-CentralitaEnCursoMax)
			for k, en := range seguidas {
				if en.Before(limite) {
					delete(seguidas, k)
				}
			}
		case <-ping.C:
			switch m.tomarMotor(ctx, q, f.ID) {
			case arriendoTomado:
				renovado = time.Now()
			case arriendoAjeno:
				log.WarnContext(ctx, "centralita: se perdió el arriendo de la conexión (otra réplica la tomó)")
				return
			case arriendoIncierto:
				// La base no contestó a tiempo: se sigue (nadie más puede tomarlo mientras la fila esté
				// bloqueada) salvo que el arriendo ya pudiera haber vencido.
				if ctx.Err() != nil {
					return
				}
				if time.Since(renovado) >= time.Duration(m.segundosArriendo())*time.Second-m.cfg.CentralitaPing {
					log.WarnContext(ctx, "centralita: no se pudo renovar el arriendo a tiempo; se reconecta")
					return
				}
			}
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
			ahora := time.Now()
			if ahora.Sub(tocado) >= time.Minute {
				tocado = ahora
				tctx, cancelar := context.WithTimeout(ctx, m.plazoBD())
				_ = q.TocarCentralita(tctx, mensajeriabd.TocarCentralitaParams{ID: f.ID, Ahora: &ahora, HaceMinuto: ahora.Add(-time.Minute)})
				cancelar()
			}
			// Primero se traduce y se filtra (en memoria, sin tocar la base): los eventos ajenos (de
			// extensiones no registradas o de llamadas que Iris no sigue) se descartan sin contar.
			if p.Valor("Event") == "Cdr" {
				p = linkedidDeCDR(p)
			}
			h := traducirEvento(p, registrada)
			if h.tipo == hechoSinInteres {
				continue
			}
			_, seguida := seguidas[h.linkedid]
			switch h.tipo {
			case hechoSuena, hechoMarca:
				if h.interno {
					continue
				}
				if !seguida && len(seguidas) >= centralitaSeguidas {
					continue
				}
			default:
				if !seguida {
					continue
				}
			}
			// Tope por segundo de llamadas nuevas que van a la base (una centralita que inunda no tumba a
			// Iris). Lo de las llamadas ya seguidas (contestar, colgar, CDR, otro teléfono que suena en el
			// mismo grupo) nunca se descarta: una avalancha no deja llamadas a medias.
			if (h.tipo == hechoSuena || h.tipo == hechoMarca) && !seguida {
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
			}
			if h.tipo == hechoSuena || h.tipo == hechoMarca {
				seguidas[h.linkedid] = ahora
			}
			cambio, err := m.Centralita.procesar.Ejecutar(sctx, entradaHechoAMI{CentralitaID: f.ID, Hecho: h, Tipo: h.tipo, Extension: h.extension})
			if err != nil && ctx.Err() == nil {
				log.WarnContext(ctx, "centralita: procesar un evento", "tipo", h.tipo, "err", err)
			}
			// Tras colgar se sigue esperando el CDR (trae la duración exacta); el CDR cierra el seguimiento.
			if h.tipo == hechoRegistro {
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

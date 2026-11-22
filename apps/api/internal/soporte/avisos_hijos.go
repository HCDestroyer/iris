package soporte

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	soportebd "github.com/HCDestroyer/iris/apps/api/internal/soporte/bd"
)

// Aviso a los hijos de un incidente maestro. resolver (o la reapertura por Horus) encola, en su
// transacción, el job soporte.avisar_hijos (único por maestro, aviso y versión); el worker lo
// procesa en tandas de TandaAvisos, cada una en su transacción. Por cada hijo no cerrado: mensaje
// público de Iris, ticket.respondido (sale por el portal o, si el hijo vino por correo, por
// correo), la versión del hijo sube (TocarTicket) y queda en su historial (aviso_incidente). Es
// idempotente por soporte.avisos_hijos (maestro, aviso:versión, hijo): un reintento sigue donde
// quedó. Los hijos no cambian de estado ni cuenta como su primera respuesta.

// Tipos de aviso.
const (
	AvisoResuelto  = "resuelto"
	AvisoReabierto = "reabierto"
)

// TandaAvisos: hijos por transacción.
const TandaAvisos = 200

// ArgsAvisarHijos es el job que avisa a los hijos de un maestro.
type ArgsAvisarHijos struct {
	MaestroID int64  `json:"maestro_id"`
	Aviso     string `json:"aviso"`
	Version   int32  `json:"version"` // versión del maestro tras el cambio (distingue avisos)
	Texto     string `json:"texto"`   // solución (resuelto)
	// Incidente: la falla general de Argos (soporte.incidentes_red.id) de los avisos falla_abierta y
	// falla_cerrada: un aviso de cada tipo por cliente e incidente (§3.11), solo a hijos con cliente.
	Incidente int64 `json:"incidente,omitempty"`
}

// Kind implementa river.JobArgs.
func (ArgsAvisarHijos) Kind() string { return "soporte.avisar_hijos" }

// InsertOpts: único por maestro, aviso y versión mientras no termine.
func (ArgsAvisarHijos) InsertOpts() river.InsertOpts {
	return river.InsertOpts{MaxAttempts: 10, UniqueOpts: unicoMientrasPendiente}
}

// encolarAviso encola el aviso en la transacción del caso (sin Encolar, p. ej. OpenAPI, nada).
func (m *Modulo) encolarAviso(ctx context.Context, t *auditoria.Tx, maestro soportebd.SoporteTicket, aviso, texto string) error {
	t.Anotar(map[string]any{"aviso_hijos": aviso})
	if m.cfg.Encolar == nil {
		return nil
	}
	return m.cfg.Encolar(ctx, t.Tx, ArgsAvisarHijos{MaestroID: maestro.ID, Aviso: aviso, Version: maestro.Version, Texto: texto})
}

func cuerpoAviso(a ArgsAvisarHijos) string {
	switch a.Aviso {
	case AvisoFallaAbierta:
		// Sin servicio ni dirección (§3.11).
		return "Detectamos una falla en la red de tu zona que puede afectar tu servicio. Ya estamos trabajando para resolverla; " +
			"te avisaremos cuando se resuelva."
	case AvisoFallaCerrada:
		return "La falla de red de tu zona se resolvió. Si tu servicio todavía no funciona, responde a este mensaje."
	}
	if a.Aviso == AvisoReabierto {
		return "Detectamos de nuevo la falla de red que afecta tu servicio. Nuestro equipo ya está trabajando en ella; " +
			"te avisaremos cuando se resuelva."
	}
	return "El incidente de red que afectaba tu servicio se resolvió: " + a.Texto +
		"\n\nSi todavía tienes problemas, responde a este mensaje."
}

func (m *Modulo) nuevoAvisarTanda() *auditoria.Caso[ArgsAvisarHijos, int] {
	return auditoria.Nuevo(auditoria.Caso[ArgsAvisarHijos, int]{
		Accion:       "tickets.avisar_hijos",
		Acceso:       auditoria.Autenticado,
		AuditarExito: func(n int) bool { return n > 0 },
		Logica: func(ctx context.Context, t *auditoria.Tx, in ArgsAvisarHijos) (int, error) {
			if err := esSistema(ctx); err != nil {
				return 0, err
			}
			q := soportebd.New(t)
			t.FijarEntidad("ticket", in.MaestroID)
			maestro, err := q.TicketPorID(ctx, in.MaestroID)
			if errors.Is(err, pgx.ErrNoRows) {
				return 0, nil
			}
			if err != nil {
				return 0, err
			}
			t.FijarEmpresa(deref(maestro.EmpresaID))
			clave := fmt.Sprintf("%s:%d", in.Aviso, in.Version)
			if in.Incidente != 0 {
				clave = fmt.Sprintf("%s:i%d", in.Aviso, in.Incidente)
			}
			hijos, err := q.HijosSinAviso(ctx, soportebd.HijosSinAvisoParams{MaestroID: in.MaestroID, Aviso: clave, Limite: TandaAvisos})
			if err != nil {
				return 0, err
			}
			cuerpo := cuerpoAviso(in)
			avisados := 0
			for _, h := range hijos {
				if in.Incidente != 0 {
					enviar, err := m.avisoFallaPermitido(ctx, t, q, in, h)
					if err != nil {
						return 0, err
					}
					if !enviar {
						if err := q.RegistrarAvisoHijoSinMensaje(ctx, soportebd.RegistrarAvisoHijoSinMensajeParams{MaestroID: in.MaestroID, Aviso: clave, HijoID: h.ID}); err != nil {
							return 0, err
						}
						continue
					}
				}
				avisados++
				msj, err := mensajeSistema(ctx, q, h, true, cuerpo)
				if err != nil {
					return 0, err
				}
				if err := q.RegistrarAvisoHijo(ctx, soportebd.RegistrarAvisoHijoParams{MaestroID: in.MaestroID, Aviso: clave, HijoID: h.ID, MensajeID: &msj.ID}); err != nil {
					return 0, err
				}
				tocado, err := q.TocarTicket(ctx, h.ID)
				if err != nil {
					return 0, err
				}
				if err := historial(ctx, t, q, tocado, "aviso_incidente", nil, nil, ptr("Incidente "+maestro.Numero+" "+in.Aviso),
					map[string]any{"maestro_id": maestro.ID, "aviso": in.Aviso, "mensaje_id": msj.ID}); err != nil {
					return 0, err
				}
				if err := t.Emitir("ticket.respondido", map[string]any{"ticket_id": h.ID, "mensaje_id": msj.ID}); err != nil {
					return 0, err
				}
			}
			t.Anotar(map[string]any{"aviso": clave, "hijos": len(hijos), "avisados": avisados})
			return len(hijos), nil
		},
	})
}

// AvisarHijos procesa el aviso en tandas hasta terminar (lo llama el worker, actor sistema).
func (m *Modulo) AvisarHijos(ctx context.Context, a ArgsAvisarHijos) (int, error) {
	total := 0
	for {
		n, err := m.AvisarTanda.Ejecutar(ctx, a)
		total += n
		if err != nil || n < TandaAvisos {
			return total, err
		}
	}
}

// TrabajadorAvisarHijos corre soporte.avisar_hijos.
type TrabajadorAvisarHijos struct {
	river.WorkerDefaults[ArgsAvisarHijos]
	BD     nucleo.BD
	Modulo *Modulo
}

// Work implementa river.Worker.
func (w *TrabajadorAvisarHijos) Work(ctx context.Context, j *river.Job[ArgsAvisarHijos]) error {
	_, err := w.Modulo.AvisarHijos(ctxWorker(ctx, w.BD), j.Args)
	return err
}

// Timeout del job.
func (w *TrabajadorAvisarHijos) Timeout(*river.Job[ArgsAvisarHijos]) time.Duration {
	return 10 * time.Minute
}

// avisoFallaPermitido: el aviso de una falla general sale solo a los hijos que absorbió ese incidente y
// tienen cliente (§3.11: solo a números con cliente), una vez por cliente, incidente y tipo, y como
// máximo uno de cada tipo cada 6 h por cliente (entre incidentes). Si sale, registra el aviso y emite
// soporte.aviso_falla: la mensajería lo manda por WhatsApp con las reglas de §6.5.
func (m *Modulo) avisoFallaPermitido(ctx context.Context, t *auditoria.Tx, q *soportebd.Queries, in ArgsAvisarHijos, h soportebd.SoporteTicket) (bool, error) {
	if h.ClienteID == nil || h.EmpresaID == nil {
		return false, nil
	}
	hijo, err := q.EsHijoDeIncidente(ctx, soportebd.EsHijoDeIncidenteParams{Incidente: in.Incidente, TicketID: h.ID})
	if err != nil || !hijo {
		return false, err
	}
	tipo := "apertura"
	if in.Aviso == AvisoFallaCerrada {
		tipo = "cierre"
	}
	ahora := m.ahora()
	reciente, err := q.AvisoFallaDesde(ctx, soportebd.AvisoFallaDesdeParams{ClienteID: *h.ClienteID, Tipo: tipo, Desde: ahora.Add(-VentanaAvisoCierre), Incidente: in.Incidente})
	if err != nil || reciente {
		return false, err
	}
	n, err := q.RegistrarAvisoFallaCliente(ctx, soportebd.RegistrarAvisoFallaClienteParams{Incidente: in.Incidente, ClienteID: *h.ClienteID, Tipo: tipo,
		TicketID: h.ID, EnviadoEn: ahora})
	if err != nil || n != 1 {
		return false, err
	}
	return true, t.Emitir("soporte.aviso_falla", map[string]any{"incidente": in.Incidente, "empresa_id": *h.EmpresaID, "cliente_id": *h.ClienteID,
		"tipo": tipo, "ticket_id": h.ID})
}

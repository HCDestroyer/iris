package mensajeria

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	mensajeriabd "github.com/HCDestroyer/iris/apps/api/internal/mensajeria/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
)

// Envío de un aviso en cola (job mensajeria.enviar_aviso), en dos casos de uso del actor sistema
// con la llamada al canal en medio, fuera de transacción:
//
//  1. preparar: el aviso sigue en cola; si es horario de silencio, espera a que termine; si ya
//     venció (variable de caducidad), falla con vencido; si el canal ya no está activo, falla con
//     rechazado_por_canal; si el número está pausado o ya mandó PorMinuto en el último minuto,
//     espera. Si no, reserva el envío (cuenta para el límite) y lo devuelve.
//  2. registrar: con el id del canal queda enviado (aviso.enviado; el simulado lo confirma
//     entregado al instante); un rechazo permanente falla con su motivo; uno temporal se reintenta
//     (River) y, en el último intento, falla con agotado.
//
// Cada aviso termina en un solo estado final (entregado o fallido) y emite un solo evento final.

const (
	accionNada    = "nada"
	accionEsperar = "esperar"
	accionEnviar  = "enviar"
)

type entradaPreparar struct {
	AvisoID uuid.UUID `json:"aviso_id"`
}

type preparado struct {
	Accion  string        `json:"accion"`
	Espera  time.Duration `json:"espera,omitempty"`
	Final   bool          `json:"final,omitempty"`
	mensaje Mensaje
	r       *resuelto
}

type entradaRegistrar struct {
	AvisoID   uuid.UUID   `json:"aviso_id"`
	Resultado *Resultado  `json:"resultado,omitempty"`
	Error     *ErrorCanal `json:"error,omitempty"`
	Ultimo    bool        `json:"ultimo"`
}

type registrado struct {
	Reintentar bool `json:"reintentar"`
	Cambio     bool `json:"cambio"`
}

// Tope de la espera de un envío en curso que no terminó (el proceso cayó a mitad).
const reservaVencida = 2 * time.Minute

func nuevoPrepararEnvio(m *Modulo) *auditoria.Caso[entradaPreparar, preparado] {
	return auditoria.Nuevo(auditoria.Caso[entradaPreparar, preparado]{
		Accion: "mensajeria.preparar_envio", Acceso: auditoria.Autenticado,
		AuditarExito: func(p preparado) bool { return p.Final },
		Logica: func(ctx context.Context, t *auditoria.Tx, in entradaPreparar) (preparado, error) {
			q := mensajeriabd.New(t)
			a, err := q.AvisoParaActualizar(ctx, in.AvisoID)
			if errors.Is(err, pgx.ErrNoRows) {
				return preparado{Accion: accionNada}, nil
			}
			if err != nil {
				return preparado{}, err
			}
			t.FijarEntidad("aviso", a.ID)
			t.FijarEmpresa(a.EmpresaID)
			ahora := m.ahora()
			switch {
			case a.Estado != EstadoEnCola, a.EnviadoEn != nil && a.ProveedorID != nil:
				return preparado{Accion: accionNada}, nil
			case a.EnviadoEn != nil && ahora.Sub(*a.EnviadoEn) < reservaVencida:
				return preparado{Accion: accionEsperar, Espera: reservaVencida - ahora.Sub(*a.EnviadoEn)}, nil
			}
			if silencio, fin := m.cfg.Silencio.enSilencio(ahora, m.cfg.Zona); silencio {
				return preparado{Accion: accionEsperar, Espera: fin.Sub(ahora)}, nil
			}
			if a.VenceEn != nil && !ahora.Before(*a.VenceEn) {
				return preparado{Accion: accionNada, Final: true}, m.fallar(ctx, t, a, MotivoVencido, texto("venció antes de poder enviarse"))
			}
			r, err := m.resolverPorID(ctx, q, a.CanalID, a.EmpresaID)
			if err != nil {
				return preparado{}, err
			}
			if r == nil || a.Destino == nil {
				return preparado{Accion: accionNada, Final: true}, m.fallar(ctx, t, a, MotivoRechazado, texto("canal_no_disponible"))
			}
			if r.fila != nil && r.fila.PausadoHasta != nil && r.fila.PausadoHasta.After(ahora) {
				return preparado{Accion: accionEsperar, Espera: r.fila.PausadoHasta.Sub(ahora)}, nil
			}
			if err := q.BloquearRemitente(ctx, r.remitente); err != nil {
				return preparado{}, err
			}
			recientes, err := q.EnviadosRecientes(ctx, mensajeriabd.EnviadosRecientesParams{Remitente: r.remitente, Desde: ahora.Add(-time.Minute)})
			if err != nil {
				return preparado{}, err
			}
			if n := len(recientes); n >= m.cfg.PorMinuto {
				libre := recientes[n-m.cfg.PorMinuto].Add(time.Minute)
				return preparado{Accion: accionEsperar, Espera: max(libre.Sub(ahora), time.Second)}, nil
			}
			if err := q.ReservarEnvio(ctx, mensajeriabd.ReservarEnvioParams{ID: a.ID, EnviadoEn: &ahora}); err != nil {
				return preparado{}, err
			}
			p, _ := PlantillaPorCodigo(a.Plantilla)
			variables := map[string]string{}
			if err := json.Unmarshal(a.Variables, &variables); err != nil {
				return preparado{}, err
			}
			return preparado{Accion: accionEnviar, r: r, mensaje: Mensaje{AvisoID: a.ID, Destino: *a.Destino, Plantilla: p, Variables: variables}}, nil
		},
	})
}

func nuevoRegistrarEnvio(m *Modulo) *auditoria.Caso[entradaRegistrar, registrado] {
	return auditoria.Nuevo(auditoria.Caso[entradaRegistrar, registrado]{
		Accion: "mensajeria.registrar_envio", Acceso: auditoria.Autenticado,
		AuditarExito: func(r registrado) bool { return r.Cambio },
		Logica: func(ctx context.Context, t *auditoria.Tx, in entradaRegistrar) (registrado, error) {
			q := mensajeriabd.New(t)
			a, err := q.AvisoParaActualizar(ctx, in.AvisoID)
			if err != nil {
				return registrado{}, err
			}
			t.FijarEntidad("aviso", a.ID)
			t.FijarEmpresa(a.EmpresaID)
			if a.Estado != EstadoEnCola {
				return registrado{}, nil
			}
			ahora := m.ahora()
			switch e := in.Error; {
			case e == nil && in.Resultado != nil:
				f, err := q.MarcarEnviado(ctx, mensajeriabd.MarcarEnviadoParams{ID: a.ID, ProveedorID: &in.Resultado.ProveedorID, EnviadoEn: &ahora})
				if err != nil {
					return registrado{}, err
				}
				if err := t.Emitir("aviso.enviado", map[string]any{"aviso_id": f.ID, "canal": f.Canal, "canal_id": f.CanalID, "origen": f.Origen}); err != nil {
					return registrado{}, err
				}
				if in.Resultado.Entregado {
					return registrado{Cambio: true}, m.entregar(ctx, t, f, ahora, nil)
				}
				return registrado{Cambio: true}, nil
			case e != nil && e.Pausar && a.CanalID != nil:
				hasta := ahora.Add(time.Hour)
				if err := q.PausarCanal(ctx, mensajeriabd.PausarCanalParams{ID: *a.CanalID, PausadoHasta: &hasta}); err != nil {
					return registrado{}, err
				}
				t.Anotar(map[string]any{"canal_pausado_hasta": hasta})
				fallthrough
			case e != nil && e.Temporal:
				if in.Ultimo {
					return registrado{Cambio: true}, m.fallar(ctx, t, a, MotivoAgotado, &e.Detalle)
				}
				return registrado{Reintentar: true}, q.ContarIntento(ctx, a.ID)
			case e != nil:
				return registrado{Cambio: true}, m.fallar(ctx, t, a, e.Motivo, &e.Detalle)
			}
			return registrado{}, nil
		},
	})
}

// fallar deja el aviso fallido y emite aviso.fallido (el webhook final hacia Argos).
func (m *Modulo) fallar(ctx context.Context, t *auditoria.Tx, a mensajeriabd.MensajeriaAviso, motivo string, detalle *string) error {
	if detalle != nil {
		d := recortar(*detalle, 500)
		detalle = &d
	}
	f, err := mensajeriabd.New(t).MarcarFallido(ctx, mensajeriabd.MarcarFallidoParams{ID: a.ID, Motivo: &motivo, Detalle: detalle, Canal: a.Canal})
	if err != nil {
		return err
	}
	t.RegistrarCambios("aviso", f.ID, auditable(a), auditable(f))
	return t.Emitir("aviso.fallido", map[string]any{"aviso_id": f.ID, "idempotency_key": f.IdempotencyKey, "empresa_id": f.EmpresaID,
		"canal": f.Canal, "motivo": motivo, "detalle": f.Detalle, "origen": f.Origen, "cliente_id": f.ClienteID})
}

// entregar deja el aviso entregado y emite aviso.entregado (el webhook final hacia Argos). Una
// lectura posterior solo emite aviso.leido (sin webhook).
func (m *Modulo) entregar(ctx context.Context, t *auditoria.Tx, a mensajeriabd.MensajeriaAviso, entregado time.Time, leido *time.Time) error {
	f, err := mensajeriabd.New(t).MarcarEntregado(ctx, mensajeriabd.MarcarEntregadoParams{ID: a.ID, EntregadoEn: &entregado, LeidoEn: leido})
	if err != nil {
		return err
	}
	t.RegistrarCambios("aviso", f.ID, auditable(a), auditable(f))
	return t.Emitir("aviso.entregado", map[string]any{"aviso_id": f.ID, "idempotency_key": f.IdempotencyKey, "empresa_id": f.EmpresaID,
		"canal": f.Canal, "entregado_en": f.EntregadoEn, "leido_en": f.LeidoEn, "origen": f.Origen, "cliente_id": f.ClienteID})
}

// trabajadorEnvio corre el job de un aviso.
type trabajadorEnvio struct {
	river.WorkerDefaults[ArgsEnviarAviso]
	m  *Modulo
	bd nucleo.BD
}

var errReintentar = errors.New("mensajeria: el canal no aceptó el envío; se reintenta")

func (w *trabajadorEnvio) Work(ctx context.Context, job *river.Job[ArgsEnviarAviso]) error {
	espera, err := w.m.procesarEnvio(ctxSistema(ctx, w.bd), job.Args.AvisoID, job.Attempt >= job.MaxAttempts)
	if err != nil {
		return err
	}
	if espera > 0 {
		return river.JobSnooze(espera)
	}
	return nil
}

// procesarEnvio corre un intento de envío de un aviso. espera > 0: volver a intentar después (horario
// de silencio, límite por minuto, número pausado); errReintentar: el canal falló temporalmente.
func (m *Modulo) procesarEnvio(ctx context.Context, id uuid.UUID, ultimo bool) (time.Duration, error) {
	p, err := m.prepararEnvio.Ejecutar(ctx, entradaPreparar{AvisoID: id})
	if err != nil {
		return 0, err
	}
	switch p.Accion {
	case accionNada:
		return 0, nil
	case accionEsperar:
		return p.Espera, nil
	}
	res, errEnvio := p.r.a.EnviarPlantilla(ctx, p.mensaje)
	in := entradaRegistrar{AvisoID: id, Ultimo: ultimo}
	var ec *ErrorCanal
	switch {
	case errEnvio == nil:
		in.Resultado = &res
	case errors.As(errEnvio, &ec):
		in.Error = ec
	default:
		in.Error = &ErrorCanal{Motivo: MotivoRechazado, Detalle: "error del canal", Temporal: true}
	}
	r, err := m.registrarEnvio.Ejecutar(context.WithoutCancel(ctx), in)
	if err != nil {
		return 0, err
	}
	if r.Reintentar {
		return 0, errReintentar
	}
	return 0, nil
}

// Avisos sin acuse ---------------------------------------------------------------------------------

func nuevoVencerAvisos(m *Modulo) *auditoria.Caso[struct{}, int] {
	return auditoria.Nuevo(auditoria.Caso[struct{}, int]{
		Accion: "mensajeria.vencer_avisos", Acceso: auditoria.Autenticado,
		AuditarExito: func(n int) bool { return n > 0 },
		Logica: func(ctx context.Context, t *auditoria.Tx, _ struct{}) (int, error) {
			q := mensajeriabd.New(t)
			ids, err := q.AvisosSinConfirmar(ctx, m.ahora().Add(-m.cfg.SinConfirmar))
			if err != nil {
				return 0, err
			}
			n := 0
			for _, id := range ids {
				a, err := q.AvisoParaActualizar(ctx, id)
				if err != nil {
					return n, err
				}
				if a.Estado != EstadoEnCola {
					continue
				}
				t.FijarEmpresa(a.EmpresaID)
				if err := m.fallar(ctx, t, a, MotivoAgotado, texto("sin acuse del canal")); err != nil {
					return n, err
				}
				n++
			}
			t.Anotar(map[string]any{"vencidos": n})
			return n, nil
		},
	})
}

package conector

import (
	"context"
	"log/slog"
	"strconv"
	"time"

	"github.com/riverqueue/river"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion"
	conectorbd "github.com/HCDestroyer/iris/apps/api/internal/integracion/conector/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
)

// Reconexión sin corte (contrato Iris ↔ ArgosISP 0.6.3 §1.3, «Reconexión»; solo sistemas con
// Sistema.ConfirmarReconexion, hoy Argos). Argos sigue usando lo del conector anterior (su iris_pat_
// y el secreto de sus webhooks) hasta la primera llamada autenticada con el atk_ nuevo. Por eso, al
// activar el conector nuevo:
//
//   - el anterior queda apagado (Iris ya no llama a Argos con él), pero el iris_pat_ que Iris le
//     entregó no se revoca: el nuevo guarda reemplaza_conector_id y reemplaza_token_id;
//   - los webhooks firmados con el secreto del anterior se aceptan y se guardan en el nuevo
//     (webhook.go), mientras el reemplazo no esté confirmado;
//   - la primera llamada con el atk_ nuevo que responde 2xx (normalmente el PUT del contrato; sirve
//     cualquiera, el cliente del conector activo avisa) confirma el reemplazo: el iris_pat_
//     anterior se revoca y las columnas vuelven a null (integraciones.confirmar_reconexion);
//   - si la confirmación nunca llega, el job ArgsVencerReemplazo lo revoca a las 24 h (el plazo en
//     que Argos descarta un canje pendiente).

// PlazoConfirmarReconexion: sin confirmación, el iris_pat_ del conector reemplazado se revoca a las
// 24 h (contrato 0.6.3: Argos descarta el canje pendiente en ese plazo).
const PlazoConfirmarReconexion = 24 * time.Hour

// ArgsVencerReemplazo revoca el iris_pat_ del conector reemplazado si el reemplazo no se confirmó.
// Único por conector reemplazado (y el que lo reemplazó: un conector reactivado puede volver a ser
// reemplazado por otro).
type ArgsVencerReemplazo struct {
	ConectorViejoID int64 `json:"conector_viejo_id" river:"unique"`
	ConectorNuevoID int64 `json:"conector_nuevo_id" river:"unique"`
}

// Kind implementa river.JobArgs.
func (ArgsVencerReemplazo) Kind() string { return "integracion.vencer_reemplazo_conector" }

// InsertOpts: a las 24 h, único por conector reemplazado.
func (ArgsVencerReemplazo) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: integracion.ColaIntegracion, MaxAttempts: 25, ScheduledAt: time.Now().Add(PlazoConfirmarReconexion),
		UniqueOpts: river.UniqueOpts{ByArgs: true}}
}

// ReemplazoCerrado: resultado de confirmar o vencer un reemplazo.
type ReemplazoCerrado struct {
	Cerrado bool `json:"cerrado"`
	// TokenRevocado: el iris_pat_ del conector reemplazado que se revocó (0 = ninguno).
	TokenRevocado int64 `json:"token_revocado"`
}

func nuevoConfirmarReemplazo(n *Nucleo) *auditoria.Caso[int64, ReemplazoCerrado] {
	return auditoria.Nuevo(auditoria.Caso[int64, ReemplazoCerrado]{
		// Lo dispara cualquier llamada del conector activo (worker, panel, API): no depende de quién.
		Accion: "integraciones.confirmar_reconexion", Acceso: auditoria.Publico, SinEmpresa: true,
		AuditarExito: func(r ReemplazoCerrado) bool { return r.Cerrado },
		Logica: func(ctx context.Context, t *auditoria.Tx, id int64) (ReemplazoCerrado, error) {
			t.FijarEntidad("conector", id)
			t.FijarEmpresa(0)
			f, err := conectorbd.New(t).ConfirmarReemplazo(ctx, id)
			if esSinFilas(err) {
				return ReemplazoCerrado{}, nil
			}
			if err != nil {
				return ReemplazoCerrado{}, err
			}
			r := ReemplazoCerrado{Cerrado: true}
			t.Anotar(map[string]any{"reemplaza_a": f.ConectorViejoID})
			if f.TokenID != nil {
				n.revocarEntregado(ctx, t.Tx, *f.TokenID, "reconexion_confirmada")
				r.TokenRevocado = *f.TokenID
				t.Anotar(map[string]any{"token_entregado_revocado": *f.TokenID})
			}
			return r, nil
		},
	})
}

func nuevoVencerReemplazo(n *Nucleo) *auditoria.Caso[ArgsVencerReemplazo, ReemplazoCerrado] {
	return auditoria.Nuevo(auditoria.Caso[ArgsVencerReemplazo, ReemplazoCerrado]{
		Accion: "integraciones.vencer_reconexion", Acceso: auditoria.Autenticado, SinEmpresa: true,
		AuditarExito: func(r ReemplazoCerrado) bool { return r.Cerrado },
		Logica: func(ctx context.Context, t *auditoria.Tx, in ArgsVencerReemplazo) (ReemplazoCerrado, error) {
			t.FijarEntidad("conector", in.ConectorNuevoID)
			t.FijarEmpresa(0)
			f, err := conectorbd.New(t).VencerReemplazo(ctx, conectorbd.VencerReemplazoParams{ConectorID: in.ConectorNuevoID, ConectorViejoID: in.ConectorViejoID})
			if esSinFilas(err) {
				// Ya confirmado (token revocado) o quitado al desactivar.
				return ReemplazoCerrado{}, nil
			}
			if err != nil {
				return ReemplazoCerrado{}, err
			}
			r := ReemplazoCerrado{Cerrado: true}
			t.Anotar(map[string]any{"reemplaza_a": in.ConectorViejoID, "sin_confirmar": true})
			// Si el anterior volvió a activarse, su token sigue en uso.
			if f.TokenID != nil && !f.ViejoActivo {
				n.revocarEntregado(ctx, t.Tx, *f.TokenID, "reconexion_sin_confirmar")
				r.TokenRevocado = *f.TokenID
				t.Anotar(map[string]any{"token_entregado_revocado": *f.TokenID})
			}
			return r, nil
		},
	})
}

// confirmadorDe es el aviso que el cliente del conector activo llama tras su primera respuesta 2xx
// mientras el reemplazo está pendiente. Nunca falla la llamada: si no se pudo confirmar (sin base,
// error), la siguiente llamada lo intenta de nuevo y el job de 24 h lo cierra igual.
func (n *Nucleo) confirmadorDe(id int64) func(context.Context) {
	return func(ctx context.Context) {
		r, err := n.ConfirmarReemplazo.Ejecutar(ctx, id)
		if err != nil {
			slog.WarnContext(ctx, "conector: no se pudo confirmar la reconexión; se reintenta en la próxima llamada", "conector_id", id, "err", err)
			return
		}
		if r.Cerrado {
			slog.InfoContext(ctx, "conector: reconexión confirmada; token del conector anterior revocado", "conector_id", id,
				"token_revocado", strconv.FormatInt(r.TokenRevocado, 10))
		}
	}
}

type trabajadorVencerReemplazo struct {
	river.WorkerDefaults[ArgsVencerReemplazo]
	n  *Nucleo
	bd nucleo.BD
}

func (w *trabajadorVencerReemplazo) Work(ctx context.Context, j *river.Job[ArgsVencerReemplazo]) error {
	ctx = nucleo.ConBD(contexto.Sistema(ctx, contexto.CanalWorker), w.bd)
	_, err := w.n.VencerReemplazo.Ejecutar(ctx, j.Args)
	return err
}

// RegistrarReconexion agrega el vencimiento de las reconexiones sin confirmar al worker (lo llama el
// conector Argos, dueño del núcleo).
func (n *Nucleo) RegistrarReconexion(workers *river.Workers, bd nucleo.BD) {
	river.AddWorker(workers, &trabajadorVencerReemplazo{n: n, bd: bd})
}

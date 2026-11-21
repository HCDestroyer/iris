package argos

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/conector"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
)

// Carga inicial de vínculos y permiso de la verificación (contrato Iris ↔ ArgosISP 0.14.0 §4.5 y
// §6.6). Cuando Iris informa contrato ≥ 0.14.0 a un Argos que también lo habla, una sola vez por
// conector:
//
//   - el iris_pat_ vigente del conector suma adjuntos.verificar_integracion (§1.2);
//   - agenda emite un tecnico.argos_vinculado por cada vínculo vigente que salió de ese conector,
//     marcados con la carga (carga_id viaja en el outbox y en mensajeria.webhooks_salida, nunca a
//     Argos);
//   - con el 2xx de todos (los omitidos no cuentan: no salieron), Iris cierra la carga con
//     PUT /integraciones/iris/contrato { contrato, receptor_argos, vinculos_cargados: <n> }. Hasta
//     entonces Argos responde 409 vinculos_en_carga, que la cola reintenta.
//
// Todo queda detrás de la versión que informa Iris (Config.ContratoIris, por defecto ContratoIris):
// mientras Iris informe una anterior a 0.14.0, nada de esto corre.

// CargaVinculos emite los tecnico.argos_vinculado de la carga inicial (agenda.CargarVinculos) en la
// transacción de ctx y devuelve cuántos van a Argos.
type CargaVinculos func(ctx context.Context, conectorID int64, carga uuid.UUID) (int, error)

// UsarVinculos conecta la carga de vínculos de agenda (cmd/worker: agenda se crea después del
// conector). nil = la carga sale vacía (0 vínculos).
func (m *Modulo) UsarVinculos(f CargaVinculos) { m.cfg.Vinculos = f }

// contratoIris: la versión que Iris informa a Argos (Config.ContratoIris o la de esta versión).
func (m *Modulo) contratoIris() string { return contratoIrisDe(m.cfg) }

func contratoIrisDe(cfg Config) string {
	if cfg.ContratoIris != "" {
		return cfg.ContratoIris
	}
	return ContratoIris
}

// InformaVinculos: Iris informa contrato ≥ 0.14.0 (la carga de vínculos y el permiso dependen de ello).
func (m *Modulo) InformaVinculos() bool {
	v := m.contratoIris()
	return conector.VersionAlMenos(&v, ContratoClavesPorOperacion)
}

// ResultadoCargaVinculos: el estado de la carga del conector.
type ResultadoCargaVinculos struct {
	ConectorID int64      `json:"conector_id"`
	CargaID    *uuid.UUID `json:"carga_id"`
	Vinculos   *int32     `json:"vinculos"`
	Nueva      bool       `json:"nueva"`
	// Tokens: iris_pat_ vigentes que sumaron adjuntos.verificar_integracion.
	Tokens   int64      `json:"tokens"`
	Cerrada  *time.Time `json:"cerrada_en"`
	Entregas int64      `json:"entregados,omitempty"`
}

func nuevoIniciarCargaVinculos(m *Modulo) *auditoria.Caso[int64, ResultadoCargaVinculos] {
	return auditoria.Nuevo(auditoria.Caso[int64, ResultadoCargaVinculos]{
		Accion: "integraciones.cargar_vinculos_argos", Acceso: auditoria.Autenticado, SinEmpresa: true,
		AuditarExito: func(r ResultadoCargaVinculos) bool { return r.Nueva },
		Logica: func(ctx context.Context, t *auditoria.Tx, conectorID int64) (ResultadoCargaVinculos, error) {
			r := ResultadoCargaVinculos{ConectorID: conectorID}
			if contexto.ActorDe(ctx).Tipo != contexto.ActorSistema {
				return r, nucleo.Prohibido("Solo el sistema carga los vínculos de Argos")
			}
			t.FijarEntidad("conector", conectorID)
			t.FijarEmpresa(0)
			err := t.QueryRow(ctx, `SELECT vinculos_carga_id, vinculos_carga_n, vinculos_cargados_en FROM integracion.conectores
				WHERE id = $1 AND sistema = $2 FOR UPDATE`, conectorID, Sistema).Scan(&r.CargaID, &r.Vinculos, &r.Cerrada)
			if err != nil {
				return r, err
			}
			if r.CargaID != nil {
				return r, nil // ya empezó (o terminó): una sola carga por conector
			}
			// §4.5: el iris_pat_ vigente del conector suma el permiso de la verificación si tiene lista de
			// permisos (uno sin lista ya tiene los de su rol). La ruta de la verificación, además, solo
			// responde a un conector al que Iris informó 0.14.0 (pendientes.ConectorDeToken).
			tag, err := t.Exec(ctx, `UPDATE nucleo.tokens_api SET permisos = array_append(permisos, $2), updated_at = now()
				WHERE id = (SELECT token_entregado_id FROM integracion.conectores WHERE id = $1)
				  AND revocado_en IS NULL AND eliminado_en IS NULL AND permisos IS NOT NULL AND NOT ($2 = ANY(permisos))`,
				conectorID, string(nucleo.PermAdjuntosVerificarIntegracion))
			if err != nil {
				return r, err
			}
			r.Tokens = tag.RowsAffected()
			carga := uuid.Must(uuid.NewV7())
			n := 0
			if m.cfg.Vinculos != nil {
				if n, err = m.cfg.Vinculos(ctx, conectorID, carga); err != nil {
					return r, err
				}
			}
			n32 := int32(n) //nolint:gosec // vínculos de una instalación
			if _, err := t.Exec(ctx, `UPDATE integracion.conectores SET vinculos_carga_id = $2, vinculos_carga_n = $3 WHERE id = $1`,
				conectorID, carga, n32); err != nil {
				return r, err
			}
			r.CargaID, r.Vinculos, r.Nueva = &carga, &n32, true
			t.Anotar(map[string]any{"carga_id": carga, "vinculos": n, "tokens_con_permiso": r.Tokens})
			return r, t.Emitir("conector.actualizado", map[string]any{"conector_id": conectorID, "sistema": Sistema,
				"campos": []string{"vinculos_carga"}})
		},
	})
}

func nuevoMarcarCargaCerrada(m *Modulo) *auditoria.Caso[ResultadoCargaVinculos, ResultadoCargaVinculos] {
	return auditoria.Nuevo(auditoria.Caso[ResultadoCargaVinculos, ResultadoCargaVinculos]{
		Accion: "integraciones.cerrar_carga_vinculos_argos", Acceso: auditoria.Autenticado, SinEmpresa: true,
		Logica: func(ctx context.Context, t *auditoria.Tx, in ResultadoCargaVinculos) (ResultadoCargaVinculos, error) {
			if contexto.ActorDe(ctx).Tipo != contexto.ActorSistema {
				return in, nucleo.Prohibido("Solo el sistema cierra la carga de vínculos")
			}
			t.FijarEntidad("conector", in.ConectorID)
			t.FijarEmpresa(0)
			ahora := time.Now().UTC()
			tag, err := t.Exec(ctx, `UPDATE integracion.conectores SET vinculos_cargados_en = $3
				WHERE id = $1 AND vinculos_carga_id = $2 AND vinculos_cargados_en IS NULL`, in.ConectorID, in.CargaID, ahora)
			if err != nil {
				return in, err
			}
			if tag.RowsAffected() == 0 {
				return in, nil
			}
			in.Cerrada = &ahora
			t.Anotar(map[string]any{"carga_id": in.CargaID, "vinculos_cargados": in.Entregas})
			return in, t.Emitir("conector.actualizado", map[string]any{"conector_id": in.ConectorID, "sistema": Sistema,
				"campos": []string{"vinculos_cargados"}})
		},
	})
}

// iniciarCargaVinculos: tras informar el contrato. Solo si Iris informó ≥ 0.14.0 y Argos lo habla.
func (k *InformarContrato) iniciarCargaVinculos(ctx context.Context, r ResultadoContrato) error {
	if !conector.VersionAlMenos(r.ContratoInformado, ContratoClavesPorOperacion) ||
		!conector.VersionAlMenos(r.ContratoRemoto, ContratoClavesPorOperacion) {
		return nil
	}
	_, err := k.m.iniciarCarga.Ejecutar(ctx, r.ConectorID)
	return err
}

// CerrarCargaVinculos cierra la carga del conector activo si todos sus vinculados tuvieron el 2xx
// (o se omitieron). Devuelve true si la cerró. Lo llama el job periódico (cada minuto) y el informe
// del contrato.
func (m *Modulo) CerrarCargaVinculos(ctx context.Context) (bool, error) {
	bd := nucleo.BDDe(ctx)
	if bd == nil {
		return false, nucleo.ErrSinBD
	}
	var in ResultadoCargaVinculos
	var carga uuid.UUID
	var n int32
	err := bd.QueryRow(ctx, `SELECT id, vinculos_carga_id, vinculos_carga_n FROM integracion.conectores
		WHERE sistema = $1 AND estado = 'activo' AND vinculos_carga_id IS NOT NULL AND vinculos_cargados_en IS NULL
		ORDER BY id DESC LIMIT 1`, Sistema).Scan(&in.ConectorID, &carga, &n)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	in.CargaID, in.Vinculos = &carga, &n
	// Cada vinculado de la carga ya llegó a la cola de webhooks y ninguno espera: el 2xx de todos los
	// que salieron. Uno agotado deja la carga abierta (Argos sigue con vinculos_en_carga): lo ve una
	// persona en la cola de webhooks.
	var total, abiertos int64
	if err := bd.QueryRow(ctx, `SELECT count(*), count(*) FILTER (WHERE estado = 'entregado'),
		count(*) FILTER (WHERE estado NOT IN ('entregado', 'omitido'))
		FROM mensajeria.webhooks_salida WHERE carga_id = $1`, carga).Scan(&total, &in.Entregas, &abiertos); err != nil {
		return false, err
	}
	if total < int64(n) || abiertos > 0 {
		return false, nil
	}
	c, err := m.conexion.PorID(ctx, bd, in.ConectorID)
	if err != nil {
		return false, err
	}
	receptor := c.WebhookSalidaUrl != nil && len(c.WebhookSalidaSecretoCifrado) > 0
	var puesto EstadoIris
	if err := m.clienteDe(c).Pedir(ctx, http.MethodPut, rutaContrato, nil, map[string]any{"contrato": m.contratoIris(),
		"receptor_argos": receptor, "vinculos_cargados": in.Entregas}, true, &conector.Recurso{Destino: &puesto}); err != nil {
		return false, err
	}
	r, err := m.marcarCargaCerrada.Ejecutar(ctx, in)
	return r.Cerrada != nil, err
}

// ArgsCerrarCargaVinculos revisa cada minuto si la carga de vínculos ya se puede cerrar.
type ArgsCerrarCargaVinculos struct{}

// Kind implementa river.JobArgs.
func (ArgsCerrarCargaVinculos) Kind() string { return "integracion.argos_cerrar_carga_vinculos" }

// InsertOpts: único por minuto.
func (ArgsCerrarCargaVinculos) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: integracion.ColaIntegracion, MaxAttempts: 3, UniqueOpts: river.UniqueOpts{ByPeriod: time.Minute}}
}

type trabajadorCerrarCarga struct {
	river.WorkerDefaults[ArgsCerrarCargaVinculos]
	m  *Modulo
	bd nucleo.BD
}

func (w *trabajadorCerrarCarga) Work(ctx context.Context, _ *river.Job[ArgsCerrarCargaVinculos]) error {
	_, err := w.m.CerrarCargaVinculos(nucleo.ConBD(contexto.Sistema(ctx, contexto.CanalWorker), w.bd))
	return err
}

func (w *trabajadorCerrarCarga) Timeout(*river.Job[ArgsCerrarCargaVinculos]) time.Duration {
	return time.Minute
}

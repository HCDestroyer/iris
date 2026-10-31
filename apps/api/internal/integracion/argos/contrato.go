package argos

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/conector"
	conectorbd "github.com/HCDestroyer/iris/apps/api/internal/integracion/conector/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
)

// Versión y contrato (contrato Iris ↔ ArgosISP 0.5.0 §3.3, «Versión y compatibilidad»):
//
//   - Iris manda `contrato` en el canje (§1.3 y §1.4) y, a un conector ya activo, con
//     PUT /integraciones/iris/contrato { contrato } (job al activar o reconectar, y uno periódico
//     que relee la versión en cada periodo: detecta una actualización o una vuelta atrás de Argos);
//   - Argos devuelve `version_argos` y `contrato` en el canje y en GET /integraciones/iris/estado;
//     Iris los guarda en el conector y elige `tecnico` (Argos ≥ 1.6.0) o `bodega_id`.

// ContratoIris es la versión del contrato Iris ↔ ArgosISP que habla esta versión de Iris.
const ContratoIris = "0.5.0"

// VersionInventarioTecnico es la primera versión de Argos que entiende `tecnico` (§3.3). Argos
// 1.5.x lo ignora y, sin bodega_id, responde 422 «bodega_id required».
const VersionInventarioTecnico = "1.6.0"

// CapacidadInventarioTecnico es la capacidad del conector con Argos ≥ 1.6.0.
const CapacidadInventarioTecnico = "inventario_por_tecnico"

// capacidades del conector Argos según su versión.
func capacidades(version, _ *string) []string {
	if conector.VersionAlMenos(version, VersionInventarioTecnico) {
		return []string{CapacidadInventarioTecnico}
	}
	return []string{}
}

// SoportaInventarioTecnico indica si Argos (por su version_argos) entiende `tecnico`.
func SoportaInventarioTecnico(version *string) bool {
	return conector.VersionAlMenos(version, VersionInventarioTecnico)
}

// InventarioPorTecnico indica si el conector Argos registrado más reciente (activo o, si no hay,
// el último) es de Argos ≥ 1.6.0. Lo consulta la cola de pendientes al encolar (q puede ser la
// transacción). false sin conector.
func (m *Modulo) InventarioPorTecnico(ctx context.Context, q conector.DBTX) (bool, error) {
	var version *string
	err := q.QueryRow(ctx, `SELECT version_remota FROM integracion.conectores WHERE sistema = $1
		ORDER BY (estado = 'activo') DESC, id DESC LIMIT 1`, Sistema).Scan(&version)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return SoportaInventarioTecnico(version), err
}

// EstadoIris es lo que Iris lee de GET /integraciones/iris/estado (y de PUT …/contrato).
type EstadoIris struct {
	VersionArgos string `json:"version_argos"`
	Contrato     string `json:"contrato"`
}

// ArgsInformarContrato informa el contrato de Iris a un conector activo y lee su versión.
type ArgsInformarContrato struct {
	ConectorID int64 `json:"conector_id"`
}

// Kind implementa river.JobArgs.
func (ArgsInformarContrato) Kind() string { return "integracion.argos_informar_contrato" }

// InsertOpts: uno por conector a la vez (idempotente: repetirlo no cambia nada en Argos).
func (ArgsInformarContrato) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: integracion.ColaIntegracion, MaxAttempts: 8, UniqueOpts: river.UniqueOpts{ByArgs: true, ByPeriod: time.Minute}}
}

// ArgsRevisarContrato (periódico) encola ArgsInformarContrato para el conector Argos activo en cada
// periodo (detecta una actualización o una vuelta atrás de Argos).
type ArgsRevisarContrato struct{}

// Kind implementa river.JobArgs.
func (ArgsRevisarContrato) Kind() string { return "integracion.argos_revisar_contrato" }

// InsertOpts: único por hora.
func (ArgsRevisarContrato) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: integracion.ColaIntegracion, MaxAttempts: 3, UniqueOpts: river.UniqueOpts{ByPeriod: time.Hour}}
}

// ResultadoContrato es lo que quedó guardado.
type ResultadoContrato struct {
	ConectorID        int64   `json:"conector_id"`
	VersionRemota     *string `json:"version_remota"`
	ContratoRemoto    *string `json:"contrato_remoto"`
	ContratoInformado *string `json:"contrato_informado"`
	Cambio            bool    `json:"cambio"`
}

type entradaGuardarContrato struct {
	ConectorID int64   `json:"conector_id"`
	Version    *string `json:"version_remota"`
	Contrato   *string `json:"contrato_remoto"`
	Informado  *string `json:"contrato_informado"`
	// SinVersion: GET …/estado respondió 404/405 (Argos 1.5.x, o volvió a ella): la versión guardada
	// se borra. SinInformado: PUT …/contrato respondió 404/405.
	SinVersion   bool `json:"sin_version,omitempty"`
	SinInformado bool `json:"sin_informado,omitempty"`
}

// InformarContrato: PUT /integraciones/iris/contrato y GET /integraciones/iris/estado fuera de
// transacción; lo leído se guarda en un caso de uso (auditado si cambió, con conector.actualizado).
type InformarContrato struct {
	m       *Modulo
	guardar *auditoria.Caso[entradaGuardarContrato, ResultadoContrato]
}

func nuevoInformarContrato(m *Modulo) *InformarContrato {
	k := &InformarContrato{m: m}
	k.guardar = auditoria.Nuevo(auditoria.Caso[entradaGuardarContrato, ResultadoContrato]{
		Accion: "integraciones.informar_contrato", Acceso: auditoria.Autenticado, SinEmpresa: true,
		AuditarExito: func(r ResultadoContrato) bool { return r.Cambio },
		Logica: func(ctx context.Context, t *auditoria.Tx, in entradaGuardarContrato) (ResultadoContrato, error) {
			q := conectorbd.New(t)
			t.FijarEntidad("conector", in.ConectorID)
			t.FijarEmpresa(0)
			antes, err := q.ConectorPorIDParaActualizar(ctx, in.ConectorID)
			if err != nil {
				return ResultadoContrato{}, err
			}
			despues, err := q.FijarVersionConector(ctx, conectorbd.FijarVersionConectorParams{
				ID: in.ConectorID, VersionRemota: in.Version, ContratoRemoto: in.Contrato, ContratoInformado: in.Informado,
				SinVersion: in.SinVersion, SinInformado: in.SinInformado,
			})
			if err != nil {
				return ResultadoContrato{}, err
			}
			r := ResultadoContrato{ConectorID: despues.ID, VersionRemota: despues.VersionRemota, ContratoRemoto: despues.ContratoRemoto,
				ContratoInformado: despues.ContratoInformado}
			campos := []string{}
			if !mismoTexto(antes.VersionRemota, despues.VersionRemota) {
				campos = append(campos, "version_remota")
			}
			if !mismoTexto(antes.ContratoRemoto, despues.ContratoRemoto) {
				campos = append(campos, "contrato_remoto")
			}
			if !mismoTexto(antes.ContratoInformado, despues.ContratoInformado) {
				campos = append(campos, "contrato_informado")
			}
			if len(campos) == 0 {
				return r, nil
			}
			r.Cambio = true
			t.RegistrarCambios("conector", despues.ID, map[string]any{"version_remota": antes.VersionRemota, "contrato_remoto": antes.ContratoRemoto,
				"contrato_informado": antes.ContratoInformado}, map[string]any{"version_remota": despues.VersionRemota,
				"contrato_remoto": despues.ContratoRemoto, "contrato_informado": despues.ContratoInformado})
			return r, t.Emitir("conector.actualizado", map[string]any{"conector_id": despues.ID, "sistema": despues.Sistema, "campos": campos})
		},
	})
	return k
}

func mismoTexto(a, b *string) bool {
	return (a == nil && b == nil) || (a != nil && b != nil && *a == *b)
}

// Ejecutar informa el contrato al conector (debe estar activo) y guarda la versión que responde
// Argos. Un Argos anterior a 1.6.0 responde 404/405 a las dos rutas: no es error (queda sin versión
// y Iris sigue con bodega_id). Argos caído: error (River reintenta).
func (k *InformarContrato) Ejecutar(ctx context.Context, conectorID int64) (ResultadoContrato, error) {
	bd := nucleo.BDDe(ctx)
	if bd == nil {
		return ResultadoContrato{}, nucleo.ErrSinBD
	}
	c, err := k.m.conexion.PorID(ctx, bd, conectorID)
	if err != nil {
		return ResultadoContrato{}, err
	}
	cli := k.m.clienteDe(c)
	in := entradaGuardarContrato{ConectorID: conectorID}
	var puesto EstadoIris
	err = cli.Pedir(ctx, http.MethodPut, "/integraciones/iris/contrato", nil, map[string]string{"contrato": ContratoIris}, true,
		&conector.Recurso{Destino: &puesto})
	switch {
	case err == nil:
		informado := ContratoIris
		in.Informado = &informado
		in.Version, in.Contrato = conector.TextoVersion(puesto.VersionArgos, 40), conector.TextoVersion(puesto.Contrato, 20)
	case anteriorA16(err):
		in.SinInformado = true
	default:
		return ResultadoContrato{}, err
	}
	var estado EstadoIris
	err = cli.Pedir(ctx, http.MethodGet, "/integraciones/iris/estado", nil, nil, true, &conector.Recurso{Destino: &estado})
	switch {
	case err == nil:
		if v := conector.TextoVersion(estado.VersionArgos, 40); v != nil {
			in.Version = v
		}
		if v := conector.TextoVersion(estado.Contrato, 20); v != nil {
			in.Contrato = v
		}
	case anteriorA16(err):
		// Sin /estado: Argos anterior a 1.6.0 (o volvió a ella). Nada de lo que informaba antes vale:
		// sin versión, Iris manda bodega_id.
		in.Version, in.Contrato, in.SinVersion = nil, nil, true
	default:
		return ResultadoContrato{}, err
	}
	return k.guardar.Ejecutar(ctx, in)
}

// anteriorA16: la ruta no existe en Argos (404 o 405): Argos anterior a 1.6.0.
func anteriorA16(err error) bool {
	var er *ErrorArgos
	return errors.As(err, &er) && (er.Status == http.StatusNotFound || er.Status == http.StatusMethodNotAllowed)
}

// encolarInformarContrato: en la transacción de la activación (AlActivar).
func (m *Modulo) encolarInformarContrato(ctx context.Context, tx pgx.Tx, conectorID int64) error {
	if m.cfg.Encolar == nil {
		return nil
	}
	return m.cfg.Encolar(ctx, tx, ArgsInformarContrato{ConectorID: conectorID})
}

type trabajadorContrato struct {
	river.WorkerDefaults[ArgsInformarContrato]
	m  *Modulo
	bd nucleo.BD
}

func (w *trabajadorContrato) Work(ctx context.Context, job *river.Job[ArgsInformarContrato]) error {
	ctx = nucleo.ConBD(contexto.Sistema(ctx, contexto.CanalWorker), w.bd)
	_, err := w.m.InformarContrato.Ejecutar(ctx, job.Args.ConectorID)
	var p *nucleo.Problema
	if errors.As(err, &p) && p.Codigo() == "conector_apagado" {
		// Se apagó o lo reemplazó otro (que tiene su propio job): nada que hacer.
		return river.JobCancel(err)
	}
	return err
}

type trabajadorRevisarContrato struct {
	river.WorkerDefaults[ArgsRevisarContrato]
	m  *Modulo
	bd nucleo.BD
}

func (w *trabajadorRevisarContrato) Work(ctx context.Context, _ *river.Job[ArgsRevisarContrato]) error {
	ctx = nucleo.ConBD(contexto.Sistema(ctx, contexto.CanalWorker), w.bd)
	return w.m.RevisarContrato(ctx)
}

// RevisarContrato encola el informe del contrato y la lectura de la versión del conector Argos activo
// en cada periodo: es el job «de una vez» para los conectores que ya estaban activos antes de esta
// versión de Iris y detecta una actualización o una vuelta atrás de Argos.
func (m *Modulo) RevisarContrato(ctx context.Context) error {
	if m.cfg.Encolar == nil {
		return nil
	}
	return nucleo.EnTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		f, err := conectorbd.New(tx).ConectorActivo(ctx, Sistema)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		// Siempre: la versión se vuelve a leer cada periodo (detecta una actualización y también una
		// vuelta atrás de Argos, que dejaría a Iris mandando tecnico a un Argos 1.5.x).
		return m.cfg.Encolar(ctx, tx, ArgsInformarContrato{ConectorID: f.ID})
	})
}

package argos

import (
	"context"
	"errors"
	"net/http"
	"strconv"
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
//     Iris los guarda en el conector y elige `tecnico` (Argos ≥ 1.6.0) o `bodega_id`;
//   - 0.6.2: el canje y el PUT llevan además `receptor_argos` (en el canje siempre true; en el PUT,
//     si el conector tiene guardado el receptor de §6.6);
//   - 0.6.3: el PUT (o cualquier llamada autenticada) con el atk_ de una reconexión la confirma en
//     Argos; Iris solo lo usa con el conector nuevo ya activo (la activación encola este job).

// ContratoIris es la versión del contrato Iris ↔ ArgosISP que habla esta versión de Iris.
const ContratoIris = "0.6.4"

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
	// Estado del diagnóstico de esta lectura (ok, ruta_no_existe, sin_permiso, no_responde, error).
	Estado string `json:"version_estado"`
	Cambio bool   `json:"cambio"`
}

// Estados del diagnóstico de la versión (Conector.versionEstado).
const (
	VersionOK           = "ok"
	VersionRutaNoExiste = "ruta_no_existe"
	VersionSinPermiso   = "sin_permiso"
	VersionNoResponde   = "no_responde"
	VersionError        = "error"
)

type entradaGuardarContrato struct {
	ConectorID int64   `json:"conector_id"`
	Version    *string `json:"version_remota"`
	Contrato   *string `json:"contrato_remoto"`
	Informado  *string `json:"contrato_informado"`
	// SinVersion: GET …/estado respondió 404 JSON o 405 (Argos 1.5.x, o volvió a ella): la versión
	// guardada se borra. SinInformado: PUT …/contrato respondió lo mismo.
	SinVersion   bool `json:"sin_version,omitempty"`
	SinInformado bool `json:"sin_informado,omitempty"`
	// Diagnóstico de esta lectura (siempre se guarda, cambie o no la versión).
	Estado  string `json:"version_estado"`
	Detalle string `json:"version_detalle"`
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
			detalle := recortarDetalle(in.Detalle)
			despues, err := q.FijarVersionConector(ctx, conectorbd.FijarVersionConectorParams{
				ID: in.ConectorID, VersionRemota: in.Version, ContratoRemoto: in.Contrato, ContratoInformado: in.Informado,
				SinVersion: in.SinVersion, SinInformado: in.SinInformado, VersionEstado: in.Estado, VersionDetalle: &detalle,
			})
			if err != nil {
				return ResultadoContrato{}, err
			}
			r := ResultadoContrato{ConectorID: despues.ID, VersionRemota: despues.VersionRemota, ContratoRemoto: despues.ContratoRemoto,
				ContratoInformado: despues.ContratoInformado, Estado: in.Estado}
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
			if !mismoTexto(antes.VersionEstado, despues.VersionEstado) {
				campos = append(campos, "version_estado")
			}
			if len(campos) == 0 {
				return r, nil
			}
			r.Cambio = true
			t.RegistrarCambios("conector", despues.ID, map[string]any{"version_remota": antes.VersionRemota, "contrato_remoto": antes.ContratoRemoto,
				"contrato_informado": antes.ContratoInformado, "version_estado": antes.VersionEstado}, map[string]any{"version_remota": despues.VersionRemota,
				"contrato_remoto": despues.ContratoRemoto, "contrato_informado": despues.ContratoInformado, "version_estado": despues.VersionEstado})
			t.Anotar(map[string]any{"version_detalle": detalle})
			return r, t.Emitir("conector.actualizado", map[string]any{"conector_id": despues.ID, "sistema": despues.Sistema, "campos": campos})
		},
	})
	return k
}

func mismoTexto(a, b *string) bool {
	return (a == nil && b == nil) || (a != nil && b != nil && *a == *b)
}

func recortarDetalle(s string) string {
	if r := []rune(s); len(r) > 500 {
		return string(r[:499]) + "…"
	}
	return s
}

// Rutas que lee el job (para el detalle).
const (
	rutaContrato = "/integraciones/iris/contrato"
	rutaEstado   = "/integraciones/iris/estado"
)

// lectura es el resultado de una de las dos llamadas.
type lectura struct {
	estado  string
	detalle string
	// sinRuta: 404 JSON o 405 (la ruta no existe en esa API: Argos 1.5.x).
	sinRuta bool
	// html: 404/405 con una página HTML (la dirección no es la de la API: p. ej. el panel de Nuxt).
	html bool
}

// clasificar traduce el error de una llamada a Argos al diagnóstico (sin texto de Argos).
func clasificar(err error, base, metodo, ruta string) lectura {
	llamada := metodo + " " + base + "/api/v1" + ruta
	var er *ErrorArgos
	if !errors.As(err, &er) {
		return lectura{estado: VersionError, detalle: "No se pudo llamar a Argos en " + base + " (" + llamada + ")"}
	}
	status := strconv.Itoa(er.Status)
	switch {
	case er.Bloqueado():
		return lectura{estado: VersionError, detalle: "La dirección " + base + " resuelve a una red interna no permitida; si es intencional, agrégala a " + Remoto.VariableRedes}
	case (er.Status == http.StatusNotFound || er.Status == http.StatusMethodNotAllowed) && er.HTML:
		return lectura{estado: VersionRutaNoExiste, html: true,
			detalle: "La dirección " + base + " no responde como la API de Argos: revisa que sea la de la API, no la del panel (" + llamada + " respondió HTTP " + status + " con una página HTML)"}
	case er.Status == http.StatusNotFound || er.Status == http.StatusMethodNotAllowed:
		return lectura{estado: VersionRutaNoExiste, sinRuta: true,
			detalle: "Argos en " + base + " no tiene " + metodo + " /api/v1" + ruta + " (HTTP " + status + "): es anterior a " + VersionInventarioTecnico + "; Iris sigue con bodega_id"}
	case er.Status == http.StatusUnauthorized || er.Status == http.StatusForbidden:
		return lectura{estado: VersionSinPermiso,
			detalle: "Argos en " + base + " rechazó el token del conector en " + metodo + " /api/v1" + ruta + " (HTTP " + status + "): revisa el usuario técnico Iris en Argos o vuelve a conectar"}
	case er.Caido():
		if er.Status == 0 {
			return lectura{estado: VersionNoResponde, detalle: "Argos no respondió en " + base + " (" + llamada + "): sin conexión o tiempo agotado"}
		}
		return lectura{estado: VersionNoResponde, detalle: "Argos no respondió en " + base + " (" + llamada + " respondió HTTP " + status + ")"}
	}
	return lectura{estado: VersionError, detalle: "Argos en " + base + " respondió HTTP " + status + " a " + metodo + " /api/v1" + ruta}
}

// Ejecutar informa el contrato al conector (debe estar activo) y guarda la versión que responde
// Argos junto con el diagnóstico de la lectura (version_estado y version_detalle, con la URL base y
// el status):
//
//   - un 401/403 en el PUT no corta: igual se lee GET …/estado (y queda sin_permiso del PUT);
//   - un 404/405 con una página HTML es ruta_no_existe («no es la API, es el panel») y no borra la
//     versión guardada si era ≥ 1.6.0;
//   - un 404 JSON o un 405 en GET …/estado es la vuelta atrás real a 1.5.x: la versión se borra y
//     Iris sigue con bodega_id;
//   - Argos caído (sin respuesta, 429, 5xx): no_responde y error (River reintenta).
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
	base := cli.Base()
	versionAntes := c.VersionRemota
	in := entradaGuardarContrato{ConectorID: conectorID}
	var puesto EstadoIris
	// receptor_argos (0.6.2): true si el conector tiene guardado el receptor de Argos (url y secreto
	// cifrado, §6.6). Los canjeados antes de B1 no lo tienen: false, y Argos pide reconectar.
	receptor := c.WebhookSalidaUrl != nil && len(c.WebhookSalidaSecretoCifrado) > 0
	errPut := cli.Pedir(ctx, http.MethodPut, rutaContrato, nil, map[string]any{"contrato": ContratoIris, "receptor_argos": receptor}, true,
		&conector.Recurso{Destino: &puesto})
	var put lectura
	if errPut == nil {
		informado := ContratoIris
		in.Informado = &informado
		in.Version, in.Contrato = conector.TextoVersion(puesto.VersionArgos, 40), conector.TextoVersion(puesto.Contrato, 20)
		put = lectura{estado: VersionOK}
	} else {
		put = clasificar(errPut, base, http.MethodPut, rutaContrato)
		// Solo la ausencia real de la ruta (Argos 1.5.x) deja el contrato sin informar; un 404 HTML es
		// una dirección equivocada y no dice nada de lo que Argos tiene registrado.
		in.SinInformado = put.sinRuta
	}
	var estado EstadoIris
	errGet := cli.Pedir(ctx, http.MethodGet, rutaEstado, nil, nil, true, &conector.Recurso{Destino: &estado})
	if errGet == nil {
		if v := conector.TextoVersion(estado.VersionArgos, 40); v != nil {
			in.Version = v
		}
		if v := conector.TextoVersion(estado.Contrato, 20); v != nil {
			in.Contrato = v
		}
		in.Estado = VersionOK
		version := "sin versión"
		if in.Version != nil {
			version = *in.Version
		}
		in.Detalle = "Argos " + version + " respondió en " + base + " (GET /api/v1" + rutaEstado + ": HTTP 200)"
		if put.estado != VersionOK {
			// El PUT falló pero la versión se leyó: queda el motivo del PUT (sin_permiso, sobre todo).
			in.Estado = put.estado
			in.Detalle = put.detalle + ". La versión " + version + " se leyó de GET /api/v1" + rutaEstado + " (HTTP 200)"
		}
	} else {
		get := clasificar(errGet, base, http.MethodGet, rutaEstado)
		in.Estado, in.Detalle = get.estado, get.detalle
		switch {
		case get.sinRuta:
			// Sin /estado: Argos anterior a 1.6.0 (o volvió a ella). Nada de lo que informaba antes vale:
			// sin versión, Iris manda bodega_id.
			in.Version, in.Contrato, in.SinVersion = nil, nil, true
		case get.html && !conector.VersionAlMenos(versionAntes, VersionInventarioTecnico):
			// Una página HTML sin una versión ≥ 1.6.0 guardada: no hay nada que conservar.
			in.Version, in.Contrato, in.SinVersion = nil, nil, true
		default:
			// HTML con una versión ≥ 1.6.0 guardada, token rechazado o Argos caído: la versión guardada
			// se conserva (no es una vuelta atrás). Lo que respondió el PUT (si respondió) se descarta.
			if errPut == nil {
				in.Version, in.Contrato = nil, nil
			}
		}
	}
	r, err := k.guardar.Ejecutar(ctx, in)
	if err != nil {
		return r, err
	}
	// Argos caído: River reintenta (el diagnóstico ya quedó guardado).
	if errGet != nil && EsCaido(errGet) {
		return r, errGet
	}
	return r, nil
}

// RevisarContratoYa encola InformarContrato para el conector Argos activo, en tx: la cola de
// pendientes recibió un «bodega_id required» (Argos volvió a 1.5.x) y la versión guardada se
// relee enseguida en vez de esperar al periódico.
func (m *Modulo) RevisarContratoYa(ctx context.Context, tx pgx.Tx) error {
	if m.cfg.Encolar == nil {
		return nil
	}
	var id int64
	err := tx.QueryRow(ctx, `SELECT id FROM integracion.conectores WHERE sistema = $1 AND estado = 'activo'
		ORDER BY id DESC LIMIT 1`, Sistema).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	return m.encolarInformarContrato(ctx, tx, id)
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

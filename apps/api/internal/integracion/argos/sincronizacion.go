package argos

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion"
	argosbd "github.com/HCDestroyer/iris/apps/api/internal/integracion/argos/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/conector"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
)

// Primera sincronización (contrato Iris ↔ ArgosISP 0.8.0 §1.5 y §2.5, bloque B7):
//
//   - empieza sola al activar en Iris un conector de Argos que nunca se sincronizó (AlActivar encola
//     el job, único por conector), siempre DESPUÉS de la primera llamada autenticada
//     (PUT /integraciones/iris/contrato, que confirma el canje): el job la hace primero;
//   - recorre los ocho recursos en el orden del contrato, una petición a la vez, de 200 en 200 y como
//     máximo 5 por segundo (Config.RitmoSincronizacion); respeta 429 con Retry-After;
//   - guarda el progreso por recurso (estado, procesados, último cursor, error) y, si se corta,
//     sigue desde el último cursor guardado;
//   - solo lee de Argos: no crea nada allá ni trae usuarios (S8); cada fila va a la instantánea con
//     su sincronizado_en y una lista nunca revive una lápida;
//   - con Argos ≥ 0.8.0 (contrato_remoto): excluir_retirados=true, cuentas y bancos sin cabeceras de
//     Iris, e informe del avance con PUT /integraciones/iris/sincronizacion al cambiar de etapa o de
//     estado (nunca por página). Con uno anterior: listas completas, cuentas y bancos con_errores
//     «Argos anterior a 0.8.0» y sin informe (§4.1).

// ContratoPrimeraSincronizacion es el primer contrato con el que Argos acepta lo nuevo de §2.5.
const ContratoPrimeraSincronizacion = "0.8.0"

// SoportaPrimeraSincronizacion indica si Argos (por el contrato que informa) tiene
// excluir_retirados, las cabeceras opcionales en cuentas y bancos y PUT …/sincronizacion.
func SoportaPrimeraSincronizacion(contrato *string) bool {
	return conector.VersionAlMenos(contrato, ContratoPrimeraSincronizacion)
}

// Recursos de la primera sincronización, en el orden del contrato (§2.5).
const (
	RecursoEmpresas         = "empresas"
	RecursoZonas            = "zonas"
	RecursoPlanes           = "planes"
	RecursoRouters          = "routers"
	RecursoCuentasBancarias = "cuentas_bancarias"
	RecursoBancos           = "bancos"
	RecursoClientes         = "clientes"
	RecursoServicios        = "servicios"
)

// RecursosSincronizacion en su orden.
var RecursosSincronizacion = []string{RecursoEmpresas, RecursoZonas, RecursoPlanes, RecursoRouters,
	RecursoCuentasBancarias, RecursoBancos, RecursoClientes, RecursoServicios}

// Estados de la sincronización y de cada etapa.
const (
	SincPendiente  = "pendiente"
	SincEnCurso    = "en_curso"
	SincCompleta   = "completa"
	SincConErrores = "con_errores"
)

// LimitePaginaSincronizacion: el máximo de Argos para las listas por cursor (§2.5).
const LimitePaginaSincronizacion = 200

// MotivoArgosAnterior080 es el error de cuentas y bancos con un Argos anterior a 0.8.0 (§2.5).
const MotivoArgosAnterior080 = "Argos anterior a 0.8.0"

// Eventos de la primera sincronización (catálogo de CLAUDE.md).
const (
	EventoSincronizacionIniciada  = "argos.sincronizacion_iniciada"
	EventoSincronizacionTerminada = "argos.sincronizacion_terminada"
)

// CargaCatalogoBancario lee y guarda las cuentas bancarias o los bancos de Argos en la primera
// sincronización (contrato 0.8.0 §2.5 pasos 5 y 6; la instantánea de §7.8 la arma el bloque B5).
// Devuelve cuántas filas guardó. Sin carga registrada, la etapa solo lee la lista y la cuenta.
type CargaCatalogoBancario func(ctx context.Context, cli *Cliente) (int, error)

// FijarCargasBancarias registra la carga de cuentas y bancos (nil = solo leer y contar).
func (m *Modulo) FijarCargasBancarias(cuentas, bancos CargaCatalogoBancario) {
	m.cargaCuentas, m.cargaBancos = cuentas, bancos
}

// ArgsPrimeraSincronizacion corre (o reanuda) la primera sincronización de un conector.
type ArgsPrimeraSincronizacion struct {
	ConectorID int64 `json:"conector_id"`
}

// Kind implementa river.JobArgs.
func (ArgsPrimeraSincronizacion) Kind() string { return "integracion.argos_primera_sincronizacion" }

// InsertOpts: uno por conector (también mientras corre: lanzarlo dos veces no lo duplica). Los
// reintentos siguen desde el último cursor guardado.
func (ArgsPrimeraSincronizacion) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: integracion.ColaIntegracion, MaxAttempts: IntentosPrimeraSincronizacion, UniqueOpts: unicoMientrasPendiente}
}

// IntentosPrimeraSincronizacion: con Argos caído River reintenta con su espera exponencial
// (unas 6 h en total); en el último, la sincronización queda con_errores y se reanuda a mano.
const IntentosPrimeraSincronizacion = 12

// ArgsInformarSincronizacion manda a Argos el estado de una sincronización tal como estaba en
// ActualizadaEn (uno por cambio de etapa o de estado).
type ArgsInformarSincronizacion struct {
	SincronizacionID uuid.UUID `json:"sincronizacion_id"`
	ActualizadaEn    time.Time `json:"actualizada_en"`
}

// Kind implementa river.JobArgs.
func (ArgsInformarSincronizacion) Kind() string { return "integracion.argos_informar_sincronizacion" }

// InsertOpts: único por sincronización e instante. Un 5xx o un timeout se reintentan.
func (ArgsInformarSincronizacion) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: integracion.ColaIntegracion, MaxAttempts: 10, UniqueOpts: river.UniqueOpts{ByArgs: true}}
}

// EtapaSincronizacion es el progreso de un recurso (API de Iris).
type EtapaSincronizacion struct {
	Recurso     string     `json:"recurso" enum:"empresas,zonas,planes,routers,cuentas_bancarias,bancos,clientes,servicios"`
	Estado      string     `json:"estado" enum:"pendiente,en_curso,completa,con_errores"`
	Procesados  int32      `json:"procesados" doc:"Filas que se leyeron y guardaron"`
	Omitidos    int32      `json:"omitidos" doc:"Filas que no se guardaron: un servicio que Argos eliminó (lápida) o cuyo cliente no está en Iris"`
	Total       *int32     `json:"total" nullable:"true" doc:"null si no se sabe (las listas por cursor no lo dicen)"`
	IniciadaEn  *time.Time `json:"iniciadaEn" nullable:"true"`
	TerminadaEn *time.Time `json:"terminadaEn" nullable:"true"`
	Error       *string    `json:"error" nullable:"true" doc:"Último error, en español y sin datos de clientes (hasta 300 caracteres)"`
}

// Sincronizacion es la primera sincronización de un conector (API de Iris).
type Sincronizacion struct {
	ID               uuid.UUID  `json:"id" doc:"sincronizacion_id (UUID v7 de Iris) que va en el informe a Argos"`
	ConectorID       int64      `json:"conectorId"`
	Estado           string     `json:"estado" enum:"en_curso,completa,con_errores"`
	Origen           string     `json:"origen" enum:"activacion,manual" doc:"activacion: empezó sola al activar el conector; manual: la lanzó o reanudó un administrador"`
	ExcluirRetirados bool       `json:"excluirRetirados" doc:"true: solo clientes con un servicio no retirado (Argos ≥ 0.8.0); false: listas completas (Argos anterior)"`
	ContratoArgos    *string    `json:"contratoArgos" nullable:"true" doc:"Contrato que informaba Argos al empezar"`
	IniciadaEn       time.Time  `json:"iniciadaEn"`
	ActualizadaEn    time.Time  `json:"actualizadaEn"`
	TerminadaEn      *time.Time `json:"terminadaEn" nullable:"true"`
	// Informe a Argos (PUT /integraciones/iris/sincronizacion).
	InformeEstado  string                `json:"informeEstado" enum:"pendiente,informado,descartado,no_soportado,error" doc:"pendiente: por mandar; informado: Argos lo guardó; descartado: Argos ya tenía uno más nuevo (409 sincronizacion_vieja); no_soportado: Argos anterior a 0.8.0; error: no se pudo (se reintenta)"`
	InformadoEn    *time.Time            `json:"informadoEn" nullable:"true"`
	InformeDetalle *string               `json:"informeDetalle" nullable:"true"`
	Etapas         []EtapaSincronizacion `json:"etapas"`
}

func aEtapa(f argosbd.IntegracionArgosSincronizacionEtapa) EtapaSincronizacion {
	return EtapaSincronizacion{Recurso: f.Recurso, Estado: f.Estado, Procesados: f.Procesados, Omitidos: f.Omitidos, Total: f.Total,
		IniciadaEn: f.IniciadaEn, TerminadaEn: f.TerminadaEn, Error: f.Error}
}

func aSincronizacion(f argosbd.IntegracionArgosSincronizacione, etapas []argosbd.IntegracionArgosSincronizacionEtapa) Sincronizacion {
	s := Sincronizacion{ID: f.ID, ConectorID: f.ConectorID, Estado: f.Estado, Origen: f.Origen, ExcluirRetirados: f.ExcluirRetirados,
		ContratoArgos: f.ContratoRemoto, IniciadaEn: f.IniciadaEn, ActualizadaEn: f.ActualizadaEn, TerminadaEn: f.TerminadaEn,
		InformeEstado: f.InformeEstado, InformadoEn: f.InformadoEn, InformeDetalle: f.InformeDetalle, Etapas: make([]EtapaSincronizacion, 0, len(etapas))}
	for _, e := range etapas {
		s.Etapas = append(s.Etapas, aEtapa(e))
	}
	return s
}

// errorEtapa recorta el error a 300 caracteres (§2.5: sin datos de clientes; los textos los arma
// Iris, nunca el detail de Argos).
func errorEtapa(s string) *string {
	if r := []rune(s); len(r) > 300 {
		s = string(r[:299]) + "…"
	}
	return &s
}

// Crear la sincronización ----------------------------------------------------------------------

type entradaCrearSincronizacion struct {
	ConectorID int64   `json:"conector_id"`
	Contrato   *string `json:"contrato_remoto"`
	Origen     string  `json:"origen"`
}

// ResultadoCrearSincronizacion: la sincronización en curso (nueva o la que ya corría).
type resultadoCrearSincronizacion struct {
	fila  argosbd.IntegracionArgosSincronizacione
	nueva bool
}

// crearSincronizacion: una nueva (UUID v7) en curso con sus ocho etapas pendientes. Si ya hay una en
// curso para el conector, la devuelve tal cual.
func crearSincronizacion(ctx context.Context, t *auditoria.Tx, m *Modulo, in entradaCrearSincronizacion) (resultadoCrearSincronizacion, error) {
	q := argosbd.New(t)
	if f, err := q.SincronizacionEnCurso(ctx, in.ConectorID); err == nil {
		return resultadoCrearSincronizacion{fila: f}, nil
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return resultadoCrearSincronizacion{}, err
	}
	id, err := uuid.NewV7()
	if err != nil {
		return resultadoCrearSincronizacion{}, err
	}
	ahora, err := q.RelojBD(ctx)
	if err != nil {
		return resultadoCrearSincronizacion{}, err
	}
	soporta := SoportaPrimeraSincronizacion(in.Contrato)
	var por *int64
	if a := t.Actor(); a.EsUsuario() {
		por = &a.ID
	}
	f, err := q.CrearSincronizacion(ctx, argosbd.CrearSincronizacionParams{ID: id, ConectorID: in.ConectorID, ContratoRemoto: in.Contrato,
		ExcluirRetirados: soporta, Informar: soporta, Ahora: ahora, IniciadaPor: por, Origen: in.Origen})
	if err != nil {
		return resultadoCrearSincronizacion{}, err
	}
	for i, r := range RecursosSincronizacion {
		if err := q.CrearEtapa(ctx, argosbd.CrearEtapaParams{SincronizacionID: id, Recurso: r, Orden: int16(i + 1)}); err != nil {
			return resultadoCrearSincronizacion{}, err
		}
	}
	t.FijarEntidad("sincronizacion_argos", id.String())
	t.Anotar(map[string]any{"conector_id": in.ConectorID, "excluir_retirados": soporta, "informar": soporta})
	if err := m.encolarInforme(ctx, t.Tx, f); err != nil {
		return resultadoCrearSincronizacion{}, err
	}
	return resultadoCrearSincronizacion{fila: f, nueva: true}, t.Emitir(EventoSincronizacionIniciada, map[string]any{
		"sincronizacion_id": id, "conector_id": in.ConectorID, "origen": in.Origen, "excluir_retirados": soporta, "contrato_remoto": in.Contrato,
	})
}

// encolarInforme encola el informe del estado actual (si Argos lo soporta), en tx.
func (m *Modulo) encolarInforme(ctx context.Context, tx pgx.Tx, f argosbd.IntegracionArgosSincronizacione) error {
	if !f.Informar || m.cfg.Encolar == nil {
		return nil
	}
	return m.cfg.Encolar(ctx, tx, ArgsInformarSincronizacion{SincronizacionID: f.ID, ActualizadaEn: f.ActualizadaEn})
}

// Guardar el avance -----------------------------------------------------------------------------

// avanceEtapa es lo que se guarda de una etapa (y, si cambió de estado, la sincronización).
type avanceEtapa struct {
	SincronizacionID uuid.UUID `json:"sincronizacion_id"`
	Recurso          string    `json:"recurso"`
	Estado           string    `json:"estado"`
	Procesados       int32     `json:"procesados"`
	Omitidos         int32     `json:"omitidos"`
	Total            *int32    `json:"total"`
	Cursor           *string   `json:"-"`
	Error            *string   `json:"error"`
	// guardar escribe las filas de la página en la misma transacción y devuelve cuántas guardó y
	// cuántas omitió, que se suman a Procesados y Omitidos (nil = solo el avance).
	guardar guardarFilas
	// Final: estado final de la sincronización (en_curso si no terminó).
	Final string `json:"final"`
}

type resultadoAvance struct {
	etapa    argosbd.IntegracionArgosSincronizacionEtapa
	sinc     argosbd.IntegracionArgosSincronizacione
	cambioDe bool
}

func nuevoGuardarAvance(m *Modulo) *auditoria.Caso[avanceEtapa, resultadoAvance] {
	return auditoria.Nuevo(auditoria.Caso[avanceEtapa, resultadoAvance]{
		Accion: "integraciones.sincronizar_avance", Acceso: auditoria.Autenticado, SinEmpresa: true,
		// Solo los cambios de etapa o de estado dejan auditoría (no cada página).
		AuditarExito: func(r resultadoAvance) bool { return r.cambioDe },
		Logica: func(ctx context.Context, t *auditoria.Tx, in avanceEtapa) (resultadoAvance, error) {
			t.FijarEmpresa(0)
			t.FijarEntidad("sincronizacion_argos", in.SincronizacionID.String())
			q := argosbd.New(t)
			s, err := q.SincronizacionParaActualizar(ctx, in.SincronizacionID)
			if err != nil {
				return resultadoAvance{}, err
			}
			if s.Estado != SincEnCurso {
				// Alguien la dio por terminada (o la relanzó) mientras corría: no se pisa.
				return resultadoAvance{sinc: s}, nucleo.ReglaNegocio("sincronizacion_terminada", "La sincronización ya terminó")
			}
			etapas, err := q.EtapasDeSincronizacion(ctx, in.SincronizacionID)
			if err != nil {
				return resultadoAvance{}, err
			}
			var antes *argosbd.IntegracionArgosSincronizacionEtapa
			for i := range etapas {
				if etapas[i].Recurso == in.Recurso {
					antes = &etapas[i]
				}
			}
			if antes == nil {
				return resultadoAvance{}, pgx.ErrNoRows
			}
			if in.guardar != nil {
				procesados, omitidos, err := in.guardar(ctx, t, q)
				if err != nil {
					return resultadoAvance{}, err
				}
				in.Procesados += int32(procesados)
				in.Omitidos += int32(omitidos)
			}
			ahora, err := q.RelojBD(ctx)
			if err != nil {
				return resultadoAvance{}, err
			}
			iniciada, terminada := antes.IniciadaEn, antes.TerminadaEn
			if iniciada == nil {
				iniciada = &ahora
			}
			if in.Estado == SincCompleta || in.Estado == SincConErrores {
				terminada = &ahora
			} else {
				terminada = nil
			}
			e, err := q.ActualizarEtapa(ctx, argosbd.ActualizarEtapaParams{SincronizacionID: in.SincronizacionID, Recurso: in.Recurso, Estado: in.Estado,
				Procesados: in.Procesados, Omitidos: in.Omitidos, Total: in.Total, Cursor: in.Cursor, IniciadaEn: iniciada, TerminadaEn: terminada, Error: in.Error})
			if err != nil {
				return resultadoAvance{}, err
			}
			r := resultadoAvance{etapa: e, sinc: s}
			final := in.Final
			if final == "" {
				final = SincEnCurso
			}
			if antes.Estado == in.Estado && final == SincEnCurso {
				return r, nil
			}
			// Cambio de etapa o de estado: actualizada_en avanza y se informa a Argos.
			r.cambioDe = true
			var fin *time.Time
			if final != SincEnCurso {
				fin = &ahora
			}
			if r.sinc, err = q.ActualizarSincronizacion(ctx, argosbd.ActualizarSincronizacionParams{ID: s.ID, Estado: final, ActualizadaEn: ahora, TerminadaEn: fin}); err != nil {
				return resultadoAvance{}, err
			}
			t.Anotar(map[string]any{"de": antes.Estado, "procesados": in.Procesados, "omitidos": in.Omitidos})
			if err := m.encolarInforme(ctx, t.Tx, r.sinc); err != nil {
				return resultadoAvance{}, err
			}
			if final == SincEnCurso {
				return r, nil
			}
			conErrores := []string{}
			for _, x := range etapas {
				estado := x.Estado
				if x.Recurso == in.Recurso {
					estado = in.Estado
				}
				if estado == SincConErrores {
					conErrores = append(conErrores, x.Recurso)
				}
			}
			return r, t.Emitir(EventoSincronizacionTerminada, map[string]any{
				"sincronizacion_id": s.ID, "conector_id": s.ConectorID, "estado": final, "etapas_con_errores": conErrores,
			})
		},
	})
}

// Correr la sincronización ----------------------------------------------------------------------

// PrimeraSincronizacion corre o reanuda la primera sincronización del conector (el job).
type PrimeraSincronizacion struct {
	m       *Modulo
	crear   *auditoria.Caso[entradaCrearSincronizacion, resultadoCrearSincronizacion]
	avance  *auditoria.Caso[avanceEtapa, resultadoAvance]
	esperar func(ctx context.Context, d time.Duration) error
}

func nuevaPrimeraSincronizacion(m *Modulo) *PrimeraSincronizacion {
	p := &PrimeraSincronizacion{m: m, avance: nuevoGuardarAvance(m), esperar: dormir}
	p.crear = auditoria.Nuevo(auditoria.Caso[entradaCrearSincronizacion, resultadoCrearSincronizacion]{
		Accion: "integraciones.iniciar_sincronizacion", Acceso: auditoria.Autenticado, SinEmpresa: true,
		AuditarExito: func(r resultadoCrearSincronizacion) bool { return r.nueva },
		Logica: func(ctx context.Context, t *auditoria.Tx, in entradaCrearSincronizacion) (resultadoCrearSincronizacion, error) {
			t.FijarEmpresa(0)
			return crearSincronizacion(ctx, t, m, in)
		},
	})
	return p
}

func dormir(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(d):
		return nil
	}
}

// ResultadoPrimeraSincronizacion: lo que quedó (Omitido: no había nada que hacer).
type ResultadoPrimeraSincronizacion struct {
	Omitido bool
	Sincronizacion
}

// errCorte: Argos no responde; el job se reintenta y sigue desde el último cursor.
type errCorte struct{ causa error }

func (e errCorte) Error() string { return e.causa.Error() }
func (e errCorte) Unwrap() error { return e.causa }

// corrida es el estado de una ejecución (ritmo entre peticiones).
type corrida struct {
	p        *PrimeraSincronizacion
	cli      *Cliente
	sinc     argosbd.IntegracionArgosSincronizacione
	conector conector.Leido
	ultima   time.Time
	ultimo   bool // último intento del job: un corte deja la etapa con_errores
}

// Ejecutar corre la sincronización en curso del conector o, si nunca tuvo una, crea la primera
// (origen activacion). Una ya terminada no se repite (volver a lanzarla es integraciones.sincronizar).
// ultimoIntento: un corte de Argos deja la etapa con_errores en vez de pedir otro intento.
func (p *PrimeraSincronizacion) Ejecutar(ctx context.Context, conectorID int64, ultimoIntento bool) (ResultadoPrimeraSincronizacion, error) {
	bd := nucleo.BDDe(ctx)
	if bd == nil {
		return ResultadoPrimeraSincronizacion{}, nucleo.ErrSinBD
	}
	if _, err := p.m.conexion.PorID(ctx, bd, conectorID); err != nil {
		return ResultadoPrimeraSincronizacion{}, err
	}
	q := argosbd.New(bd)
	sinc, err := q.SincronizacionEnCurso(ctx, conectorID)
	if errors.Is(err, pgx.ErrNoRows) {
		if _, err := q.UltimaSincronizacion(ctx, conectorID); err == nil {
			return ResultadoPrimeraSincronizacion{Omitido: true}, nil
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return ResultadoPrimeraSincronizacion{}, err
		}
	} else if err != nil {
		return ResultadoPrimeraSincronizacion{}, err
	}
	// Primero la llamada autenticada que confirma el canje (PUT …/contrato) y relee el contrato de
	// Argos (§1.5: «siempre después de esa primera llamada»). Si Argos no responde, se reintenta.
	if _, err := p.m.InformarContrato.Ejecutar(ctx, conectorID); err != nil {
		return ResultadoPrimeraSincronizacion{}, err
	}
	c, err := p.m.conexion.PorID(ctx, bd, conectorID)
	if err != nil {
		return ResultadoPrimeraSincronizacion{}, err
	}
	if sinc.ID == uuid.Nil {
		r, err := p.crear.Ejecutar(ctx, entradaCrearSincronizacion{ConectorID: conectorID, Contrato: c.ContratoRemoto, Origen: "activacion"})
		if err != nil {
			return ResultadoPrimeraSincronizacion{}, err
		}
		sinc = r.fila
	}
	k := &corrida{p: p, cli: p.m.clienteDe(c), sinc: sinc, conector: c, ultimo: ultimoIntento}
	if err := k.correr(ctx); err != nil {
		return ResultadoPrimeraSincronizacion{}, err
	}
	return p.m.leerSincronizacion(ctx, bd, sinc.ID)
}

func (m *Modulo) leerSincronizacion(ctx context.Context, q argosbd.DBTX, id uuid.UUID) (ResultadoPrimeraSincronizacion, error) {
	f, err := argosbd.New(q).SincronizacionPorID(ctx, id)
	if err != nil {
		return ResultadoPrimeraSincronizacion{}, err
	}
	etapas, err := argosbd.New(q).EtapasDeSincronizacion(ctx, id)
	if err != nil {
		return ResultadoPrimeraSincronizacion{}, err
	}
	return ResultadoPrimeraSincronizacion{Sincronizacion: aSincronizacion(f, etapas)}, nil
}

func (k *corrida) correr(ctx context.Context) error {
	etapas, err := argosbd.New(nucleo.BDDe(ctx)).EtapasDeSincronizacion(ctx, k.sinc.ID)
	if err != nil {
		return err
	}
	conErrores := false
	for i, e := range etapas {
		ultima := i == len(etapas)-1
		if e.Estado == SincCompleta || e.Estado == SincConErrores {
			conErrores = conErrores || e.Estado == SincConErrores
			if ultima {
				// Todo estaba hecho (un corte justo antes de cerrar): se cierra la sincronización.
				return k.cerrar(ctx, e, conErrores)
			}
			continue
		}
		estado, err := k.etapa(ctx, e, ultima, conErrores)
		if err != nil {
			return err
		}
		conErrores = conErrores || estado == SincConErrores
	}
	return nil
}

// cerrar fija el estado final repitiendo el de la última etapa.
func (k *corrida) cerrar(ctx context.Context, e argosbd.IntegracionArgosSincronizacionEtapa, conErrores bool) error {
	final := SincCompleta
	if conErrores {
		final = SincConErrores
	}
	_, err := k.p.avance.Ejecutar(ctx, avanceEtapa{SincronizacionID: k.sinc.ID, Recurso: e.Recurso, Estado: e.Estado, Procesados: e.Procesados,
		Omitidos: e.Omitidos, Total: e.Total, Cursor: e.Cursor, Error: e.Error, Final: final})
	return err
}

// guardar escribe el avance de la etapa; final solo en la última etapa terminada.
func (k *corrida) guardar(ctx context.Context, e argosbd.IntegracionArgosSincronizacionEtapa, estado string, final string, mensaje *string,
	guardar guardarFilas) (argosbd.IntegracionArgosSincronizacionEtapa, error) {
	r, err := k.p.avance.Ejecutar(ctx, avanceEtapa{SincronizacionID: k.sinc.ID, Recurso: e.Recurso, Estado: estado, Procesados: e.Procesados,
		Omitidos: e.Omitidos, Total: e.Total, Cursor: e.Cursor, Error: mensaje, guardar: guardar, Final: final})
	if err != nil {
		return e, err
	}
	return r.etapa, nil
}

// etapa corre un recurso de principio a fin (o desde su cursor) y devuelve su estado final.
func (k *corrida) etapa(ctx context.Context, e argosbd.IntegracionArgosSincronizacionEtapa, ultima, conErroresAntes bool) (string, error) {
	finalDe := func(estado string) string {
		if !ultima {
			return ""
		}
		if conErroresAntes || estado == SincConErrores {
			return SincConErrores
		}
		return SincCompleta
	}
	var err error
	if e.Estado == SincPendiente {
		if e, err = k.guardar(ctx, e, SincEnCurso, "", nil, nil); err != nil {
			return "", err
		}
	}
	// Cada etapa: lee página por página y guarda el avance con las filas en la misma transacción.
	errEtapa := k.recorrer(ctx, &e)
	if errEtapa == nil {
		_, err := k.guardar(ctx, e, SincCompleta, finalDe(SincCompleta), nil, nil)
		return SincCompleta, err
	}
	var corte errCorte
	if errors.As(errEtapa, &corte) && !k.ultimo {
		// Argos no responde: queda en curso con el motivo; River reintenta y sigue desde el cursor.
		if _, err := k.guardar(ctx, e, SincEnCurso, "", errorEtapa(mensajeCorte(corte.causa)), nil); err != nil {
			return "", err
		}
		return "", errEtapa
	}
	var p *nucleo.Problema
	if errors.As(errEtapa, &p) && p.Codigo() == "sincronizacion_terminada" {
		return "", river.JobCancel(errEtapa)
	}
	if corte.causa != nil {
		errEtapa = corte.causa
	}
	slog.WarnContext(ctx, "argos: una etapa de la primera sincronización terminó con errores", "recurso", e.Recurso, "error", errEtapa)
	_, err = k.guardar(ctx, e, SincConErrores, finalDe(SincConErrores), errorEtapa(mensajeEtapa(errEtapa)), nil)
	return SincConErrores, err
}

// esperarTurno: como máximo una petición cada RitmoSincronizacion (5 por segundo en total, §2.5).
func (k *corrida) esperarTurno(ctx context.Context) error {
	ritmo := k.p.m.cfg.RitmoSincronizacion
	if espera := time.Until(k.ultima.Add(ritmo)); espera > 0 {
		if err := k.p.esperar(ctx, espera); err != nil {
			return err
		}
	}
	k.ultima = time.Now()
	return nil
}

// leer hace una lectura respetando el ritmo y un 429 con Retry-After (hasta 2 min dentro del job;
// más que eso corta y River reintenta).
func (k *corrida) leer(ctx context.Context, fn func(context.Context) error) error {
	for range 5 {
		if err := k.esperarTurno(ctx); err != nil {
			return errCorte{causa: err}
		}
		err := fn(conector.UnIntento(ctx))
		if err == nil {
			return nil
		}
		var er *ErrorArgos
		if errors.As(err, &er) && er.Status == http.StatusTooManyRequests && er.ReintentarEn() > 0 && er.ReintentarEn() <= 2*time.Minute {
			if err := k.p.esperar(ctx, er.ReintentarEn()); err != nil {
				return errCorte{causa: err}
			}
			continue
		}
		if errors.As(err, &er) && er.Caido() && !er.Ilegible {
			// Reintento corto con el mismo ritmo (como Pedir), y si sigue caído, corte.
			if err := k.p.esperar(ctx, k.p.m.cfg.Espera); err != nil {
				return errCorte{causa: err}
			}
			continue
		}
		return err
	}
	return errCorte{causa: fmt.Errorf("argos no responde: %w", ErrArgosNoResponde)}
}

// ErrArgosNoResponde: varios intentos seguidos sin respuesta útil de Argos.
var ErrArgosNoResponde = errors.New("argos no responde")

func mensajeCorte(err error) string {
	return "Argos no respondió; se reintenta y se sigue desde donde quedó (" + resumenError(err) + ")"
}

func mensajeEtapa(err error) string {
	var er *ErrorArgos
	switch {
	case errors.Is(err, errArgosAnterior):
		return MotivoArgosAnterior080
	case errors.As(err, &er) && (er.Status == http.StatusUnauthorized || er.Status == http.StatusForbidden):
		return "Argos rechazó el token del conector (HTTP " + strconv.Itoa(er.Status) + "); revisa el usuario técnico Iris en Argos o vuelve a conectar"
	case errors.As(err, &er) && er.Ilegible:
		return "Argos respondió algo que Iris no pudo leer; se puede reanudar cuando se corrija"
	case errors.As(err, &er) && er.Status == http.StatusNotFound:
		return "Argos no tiene esta lista (HTTP 404)"
	case errors.As(err, &er) && er.Status > 0:
		return "Argos respondió HTTP " + strconv.Itoa(er.Status)
	case errors.As(err, &er) || errors.Is(err, ErrArgosNoResponde):
		return "Argos no respondió (" + resumenError(err) + ")"
	}
	return "No se pudo guardar lo leído de Argos"
}

// resumenError: un texto corto sin el detalle de Argos ni datos de clientes.
func resumenError(err error) string {
	var er *ErrorArgos
	if errors.As(err, &er) {
		if er.Status > 0 {
			return "HTTP " + strconv.Itoa(er.Status)
		}
		return "sin conexión o tiempo agotado"
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return "tiempo agotado"
	}
	return "sin conexión"
}

var errArgosAnterior = errors.New(MotivoArgosAnterior080)

// recorrer lee el recurso desde e.Cursor y guarda cada página con su avance (e se actualiza).
func (k *corrida) recorrer(ctx context.Context, e *argosbd.IntegracionArgosSincronizacionEtapa) error {
	switch e.Recurso {
	case RecursoEmpresas:
		n := int32(len(k.conector.Empresas))
		e.Procesados, e.Total = n, &n
		return nil
	case RecursoZonas:
		return k.zonas(ctx, e)
	case RecursoPlanes:
		return k.paginas(ctx, e, "/planes", nil, contar)
	case RecursoRouters:
		return k.paginas(ctx, e, "/routers", nil, k.routers)
	case RecursoCuentasBancarias, RecursoBancos:
		return k.bancario(ctx, e)
	case RecursoClientes:
		return k.paginas(ctx, e, "/clientes", k.filtroRetirados(), k.clientes)
	case RecursoServicios:
		return k.paginas(ctx, e, "/servicios", k.filtroRetirados(), k.servicios)
	}
	return fmt.Errorf("recurso desconocido %q", e.Recurso)
}

// guardarFilas escribe las filas de una página y devuelve cuántas guardó y cuántas omitió.
type guardarFilas func(ctx context.Context, t *auditoria.Tx, q *argosbd.Queries) (procesados, omitidos int, err error)

// guardarPagina escribe la página y el avance (procesados, omitidos y cursor) en una transacción.
func (k *corrida) guardarPagina(ctx context.Context, e *argosbd.IntegracionArgosSincronizacionEtapa, siguiente string, guardar guardarFilas) error {
	if siguiente != "" {
		e.Cursor = &siguiente
	}
	nueva, err := k.guardar(ctx, *e, SincEnCurso, "", nil, guardar)
	if err != nil {
		return err
	}
	*e = nueva
	return nil
}

func (k *corrida) zonas(ctx context.Context, e *argosbd.IntegracionArgosSincronizacionEtapa) error {
	var zonas []ZonaArgos
	// Arreglo sin paginar (§2.5): se relee entero si se cortó.
	leidoEn, err := argosbd.New(nucleo.BDDe(ctx)).RelojBD(ctx)
	if err != nil {
		return err
	}
	if err := k.leer(ctx, func(ctx context.Context) error {
		var err error
		zonas, err = k.cli.Zonas(ctx)
		return err
	}); err != nil {
		return err
	}
	e.Procesados, e.Omitidos, e.Cursor = 0, 0, nil
	return k.guardarPagina(ctx, e, "", func(ctx context.Context, _ *auditoria.Tx, q *argosbd.Queries) (int, int, error) {
		for _, z := range zonas {
			if err := q.GuardarZona(ctx, argosbd.GuardarZonaParams{ID: z.ID, EmpresaID: z.Empresa(), Nombre: z.Nombre, SincronizadoEn: leidoEn}); err != nil {
				return 0, 0, err
			}
		}
		return len(zonas), 0, nil
	})
}

// pagina lee una página de una lista por cursor.
func (k *corrida) pagina(ctx context.Context, ruta string, extra url.Values, cursor string) (crudo []json.RawMessage, siguiente string, err error) {
	v := url.Values{"limit": {strconv.Itoa(LimitePaginaSincronizacion)}}
	for c, vals := range extra {
		v[c] = vals
	}
	if cursor != "" {
		v.Set("cursor", cursor)
	}
	err = k.leer(ctx, func(ctx context.Context) error {
		p, err := conector.ListaDe[json.RawMessage](ctx, k.cli.Cliente, ruta, v)
		crudo, siguiente = p.Data, p.NextCursor
		return err
	})
	return crudo, siguiente, err
}

// paginas recorre una lista por cursor desde e.Cursor; procesar arma lo que se guarda de cada página
// (un error es una página ilegible).
func (k *corrida) paginas(ctx context.Context, e *argosbd.IntegracionArgosSincronizacionEtapa, ruta string, extra url.Values,
	procesar func([]json.RawMessage) (guardarFilas, error)) error {
	cursor := ""
	if e.Cursor != nil {
		cursor = *e.Cursor
	}
	for range maxPaginasSincronizacion {
		crudo, siguiente, err := k.pagina(ctx, ruta, extra, cursor)
		if err != nil {
			return err
		}
		guardar, err := procesar(crudo)
		if err != nil {
			return err
		}
		if err := k.guardarPagina(ctx, e, siguiente, guardar); err != nil {
			return err
		}
		if siguiente == "" || siguiente == cursor {
			return nil
		}
		cursor = siguiente
	}
	return fmt.Errorf("%w: más de %d páginas en %s", conector.ErrListaDemasiadoLarga, maxPaginasSincronizacion, ruta)
}

// maxPaginasSincronizacion acota una lista (2 millones de filas de 200 en 200).
const maxPaginasSincronizacion = 10000

// decodificar lee las filas de una página (forma tolerante de Argos); una ilegible es un error.
func decodificar[T any](crudo []json.RawMessage) ([]T, error) {
	salida := make([]T, 0, len(crudo))
	for _, b := range crudo {
		var v T
		if err := conector.Decodificar(b, &v); err != nil {
			return nil, &ErrorArgos{Remoto: Remoto, Status: http.StatusBadGateway, Titulo: "Respuesta ilegible", Ilegible: true}
		}
		salida = append(salida, v)
	}
	return salida, nil
}

// contar: una página que solo se cuenta (planes: Iris no los guarda, el servicio trae su plan).
func contar(crudo []json.RawMessage) (guardarFilas, error) {
	return func(context.Context, *auditoria.Tx, *argosbd.Queries) (int, int, error) { return len(crudo), 0, nil }, nil
}

func (k *corrida) routers(crudo []json.RawMessage) (guardarFilas, error) {
	routers, err := decodificar[RouterArgos](crudo)
	if err != nil {
		return nil, err
	}
	return func(ctx context.Context, t *auditoria.Tx, q *argosbd.Queries) (int, int, error) {
		leidoEn, err := q.RelojBD(ctx)
		if err != nil {
			return 0, 0, err
		}
		ids := make([]int64, 0, len(routers))
		for _, r := range routers {
			ids = append(ids, r.ID)
		}
		if err := bloquear(ctx, q, eliminadoRouter, ids); err != nil {
			return 0, 0, err
		}
		conLapida, err := q.EliminadosEntre(ctx, argosbd.EliminadosEntreParams{Tipo: eliminadoRouter, Ids: ids})
		if err != nil {
			return 0, 0, err
		}
		for _, r := range routers {
			lat, lon := r.LatLon()
			// Sin vivoEn: una lista nunca revive una lápida (§2.5); GuardarRouter la respeta.
			if err := guardarRouter(ctx, t, q, argosbd.GuardarRouterParams{ID: r.ID, EmpresaID: r.Empresa(), Nombre: r.Nombre,
				ZonaID: idDe(r.ZonaID, r.Zona), Lat: lat, Lon: lon, SincronizadoEn: leidoEn}, nil); err != nil {
				return 0, 0, err
			}
		}
		return len(routers) - len(conLapida), len(conLapida), nil
	}, nil
}

// bancario: cuentas bancarias o bancos (§2.5 pasos 5 y 6). Con Argos < 0.8.0 las cabeceras de Iris
// son obligatorias y la primera carga no tiene conversación: con_errores «Argos anterior a 0.8.0».
func (k *corrida) bancario(ctx context.Context, e *argosbd.IntegracionArgosSincronizacionEtapa) error {
	if !SoportaPrimeraSincronizacion(k.sinc.ContratoRemoto) {
		return errArgosAnterior
	}
	carga, ruta := k.p.m.cargaCuentas, "/cuentas-bancarias"
	if e.Recurso == RecursoBancos {
		carga, ruta = k.p.m.cargaBancos, "/bancos"
	}
	var n int
	if err := k.leer(ctx, func(ctx context.Context) error {
		if carga != nil {
			var err error
			n, err = carga(ctx, k.cli)
			return err
		}
		// Arreglo sin paginar, sin X-Iris-Actor ni X-Iris-Conversacion (opcionales desde 0.8.0).
		p, err := conector.ListaDe[json.RawMessage](ctx, k.cli.Cliente, ruta, nil)
		n = len(p.Data)
		return err
	}); err != nil {
		return err
	}
	total := int32(n)
	e.Procesados, e.Omitidos, e.Total = total, 0, &total
	return nil
}

// filtroRetirados: excluir_retirados=true con Argos ≥ 0.8.0 (decisión del PM, §10.2 punto 9).
func (k *corrida) filtroRetirados() url.Values {
	if k.sinc.ExcluirRetirados {
		return url.Values{"excluir_retirados": {"true"}}
	}
	return nil
}

func (k *corrida) clientes(crudo []json.RawMessage) (guardarFilas, error) {
	clientes, err := decodificar[ClienteArgos](crudo)
	if err != nil {
		return nil, err
	}
	return func(ctx context.Context, _ *auditoria.Tx, q *argosbd.Queries) (int, int, error) {
		leidoEn, err := q.RelojBD(ctx)
		if err != nil {
			return 0, 0, err
		}
		for _, c := range clientes {
			lat, lon := c.LatLon()
			zonaNombre, err := nombreZonaCliente(ctx, q, c, nil)
			if err != nil {
				return 0, 0, err
			}
			if err := q.GuardarClienteLista(ctx, argosbd.GuardarClienteListaParams{
				ID: c.ID, Codigo: c.Codigo, Nombre: c.Nombre, Telefono: c.Telefono, Direccion: c.Direccion, Lat: lat, Lon: lon,
				ZonaID: c.IDZona(), Estado: c.Estado, SincronizadoEn: leidoEn, Tipo: c.Tipo, TipoDocumentoFiscal: c.TipoDocumentoFiscal,
				NitCui: c.NitCui, Movil: c.Movil, Correo: c.CorreoCliente(), ZonaNombre: zonaNombre, VersionDatos: VersionDatos,
				Referencia: strings.TrimSpace(c.Referencia),
			}); err != nil {
				return 0, 0, err
			}
		}
		return len(clientes), 0, nil
	}, nil
}

// servicios: solo los servicios cuyo cliente está en la instantánea (la FK lo exige; un cliente
// creado en Argos durante la carga llega a demanda) y sin lápida. Las empresas de sus clientes se
// recalculan con lo guardado. Los demás cuentan como omitidos.
func (k *corrida) servicios(crudo []json.RawMessage) (guardarFilas, error) {
	servicios, err := decodificar[ServicioArgos](crudo)
	if err != nil {
		return nil, err
	}
	return func(ctx context.Context, t *auditoria.Tx, q *argosbd.Queries) (int, int, error) {
		leidoEn, err := q.RelojBD(ctx)
		if err != nil {
			return 0, 0, err
		}
		clientes := make([]int64, 0, len(servicios))
		for _, s := range servicios {
			clientes = append(clientes, s.ClienteID)
		}
		filas, err := q.ClientesEnInstantanea(ctx, clientes)
		if err != nil {
			return 0, 0, err
		}
		zonas := make(map[int64]*int64, len(filas))
		for _, f := range filas {
			zonas[f.ID] = f.ZonaID
		}
		ids := make([]int64, 0, len(servicios))
		for _, s := range servicios {
			if _, ok := zonas[s.ClienteID]; ok {
				ids = append(ids, s.ID)
			}
		}
		if err := bloquear(ctx, q, eliminadoServicio, ids); err != nil {
			return 0, 0, err
		}
		conLapida, err := q.EliminadosEntre(ctx, argosbd.EliminadosEntreParams{Tipo: eliminadoServicio, Ids: ids})
		if err != nil {
			return 0, 0, err
		}
		lapida := make(map[int64]bool, len(conLapida))
		for _, id := range conLapida {
			lapida[id] = true
		}
		guardados := 0
		tocados := []int64{}
		for _, s := range servicios {
			zona, ok := zonas[s.ClienteID]
			if !ok || lapida[s.ID] {
				continue
			}
			v := AServicio(s, zona)
			if err := guardarServicio(ctx, t, q, argosbd.GuardarServicioParams{
				ID: v.ID, EmpresaID: v.EmpresaID, ClienteID: v.ClienteID, Codigo: v.Codigo,
				PlanID: v.PlanID, PlanNombre: v.PlanNombre, RouterID: v.RouterID, RouterNombre: v.RouterNombre,
				ZonaID: v.ZonaID, ZonaNombre: v.ZonaNombre, Ip: v.IP, Direccion: v.Direccion, Lat: v.Lat, Lon: v.Lon,
				Estado: v.Estado, SincronizadoEn: leidoEn, UserPpp: v.UserPPP, TipoConexion: v.TipoConexion,
				Referencia: v.Referencia, ContactoNombre: v.ContactoNombre, ContactoTelefono: v.ContactoTelefono,
				PrecioMensual: v.PrecioMensual, Moneda: v.Moneda, PrefijoEnlaceV6: v.PrefijoEnlaceV6,
				PrefijoDelegadoV6: v.PrefijoDelegadoV6, Planta: plantaJSON(v.Planta),
			}, nil); err != nil {
				return 0, 0, err
			}
			guardados++
			tocados = append(tocados, s.ClienteID)
		}
		if err := q.RecalcularEmpresasClientes(ctx, tocados); err != nil {
			return 0, 0, err
		}
		return guardados, len(servicios) - guardados, nil
	}, nil
}

// Informar el avance a Argos -------------------------------------------------------------------

// cuerpoInforme es el PUT /integraciones/iris/sincronizacion (§2.5).
type cuerpoInforme struct {
	SincronizacionID string         `json:"sincronizacion_id"`
	Estado           string         `json:"estado"`
	IniciadaEn       string         `json:"iniciada_en"`
	ActualizadaEn    string         `json:"actualizada_en"`
	Etapas           []etapaInforme `json:"etapas"`
}

type etapaInforme struct {
	Recurso     string  `json:"recurso"`
	Estado      string  `json:"estado"`
	Procesados  int32   `json:"procesados"`
	Total       *int32  `json:"total"`
	Error       *string `json:"error"`
	TerminadaEn *string `json:"terminada_en"`
}

func instante(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05.000Z") }

// RutaInformeSincronizacion es la ruta nueva de Argos 0.8.0 (A28).
const RutaInformeSincronizacion = "/integraciones/iris/sincronizacion"

type entradaFijarInforme struct {
	ID      uuid.UUID  `json:"sincronizacion_id"`
	Estado  string     `json:"informe_estado"`
	En      *time.Time `json:"informado_en"`
	Hasta   *time.Time `json:"informado_hasta"`
	Detalle *string    `json:"informe_detalle"`
}

// InformarSincronizacion manda el estado de la sincronización a Argos (el job).
type InformarSincronizacion struct {
	m     *Modulo
	fijar *auditoria.Caso[entradaFijarInforme, argosbd.IntegracionArgosSincronizacione]
}

func nuevoInformarSincronizacion(m *Modulo) *InformarSincronizacion {
	k := &InformarSincronizacion{m: m}
	k.fijar = auditoria.Nuevo(auditoria.Caso[entradaFijarInforme, argosbd.IntegracionArgosSincronizacione]{
		Accion: "integraciones.informar_sincronizacion", Acceso: auditoria.Autenticado, SinEmpresa: true,
		Logica: func(ctx context.Context, t *auditoria.Tx, in entradaFijarInforme) (argosbd.IntegracionArgosSincronizacione, error) {
			t.FijarEmpresa(0)
			t.FijarEntidad("sincronizacion_argos", in.ID.String())
			return argosbd.New(t).FijarInformeSincronizacion(ctx, argosbd.FijarInformeSincronizacionParams{ID: in.ID, InformeEstado: in.Estado,
				InformadoEn: in.En, InformadoHasta: in.Hasta, InformeDetalle: in.Detalle})
		},
	})
	return k
}

// ResultadoInforme: qué pasó con el informe.
type ResultadoInforme struct {
	// Estado: informado, descartado, no_soportado, error u omitido (ya había uno más nuevo).
	Estado string
}

// Ejecutar manda el informe tal como está ahora la sincronización, si no lo supera otro más nuevo.
// 409 sincronizacion_vieja: se descarta (no se reintenta). 5xx o timeout: error (River reintenta).
func (k *InformarSincronizacion) Ejecutar(ctx context.Context, args ArgsInformarSincronizacion) (ResultadoInforme, error) {
	bd := nucleo.BDDe(ctx)
	if bd == nil {
		return ResultadoInforme{}, nucleo.ErrSinBD
	}
	q := argosbd.New(bd)
	s, err := q.SincronizacionPorID(ctx, args.SincronizacionID)
	if err != nil {
		return ResultadoInforme{}, err
	}
	if !s.Informar {
		return ResultadoInforme{Estado: "no_soportado"}, nil
	}
	// Otro cambio posterior tiene su propio job; este ya es viejo.
	if s.ActualizadaEn.After(args.ActualizadaEn) || (s.InformadoHasta != nil && !s.InformadoHasta.Before(s.ActualizadaEn)) {
		return ResultadoInforme{Estado: "omitido"}, nil
	}
	etapas, err := q.EtapasDeSincronizacion(ctx, s.ID)
	if err != nil {
		return ResultadoInforme{}, err
	}
	c, err := k.m.conexion.PorID(ctx, bd, s.ConectorID)
	if err != nil {
		return ResultadoInforme{}, err
	}
	cuerpo := cuerpoInforme{SincronizacionID: s.ID.String(), Estado: s.Estado, IniciadaEn: instante(s.IniciadaEn), ActualizadaEn: instante(s.ActualizadaEn),
		Etapas: make([]etapaInforme, 0, len(etapas))}
	for _, e := range etapas {
		ei := etapaInforme{Recurso: e.Recurso, Estado: e.Estado, Procesados: e.Procesados, Total: e.Total, Error: e.Error}
		if e.TerminadaEn != nil {
			t := instante(*e.TerminadaEn)
			ei.TerminadaEn = &t
		}
		cuerpo.Etapas = append(cuerpo.Etapas, ei)
	}
	err = k.m.clienteDe(c).Pedir(ctx, http.MethodPut, RutaInformeSincronizacion, nil, cuerpo, true, nil)
	ahora := time.Now().UTC()
	hasta := s.ActualizadaEn
	var er *ErrorArgos
	switch {
	case err == nil:
		_, err := k.fijar.Ejecutar(ctx, entradaFijarInforme{ID: s.ID, Estado: "informado", En: &ahora, Hasta: &hasta})
		return ResultadoInforme{Estado: "informado"}, err
	case errors.As(err, &er) && er.Status == http.StatusConflict && er.Codigo == "sincronizacion_vieja":
		d := "Argos ya tenía un informe más nuevo (409 sincronizacion_vieja): este se descartó"
		_, errF := k.fijar.Ejecutar(ctx, entradaFijarInforme{ID: s.ID, Estado: "descartado", Detalle: &d})
		return ResultadoInforme{Estado: "descartado"}, errF
	case errors.As(err, &er) && !er.HTML && (er.Status == http.StatusNotFound || er.Status == http.StatusMethodNotAllowed):
		d := "Argos no tiene PUT /api/v1" + RutaInformeSincronizacion + " (HTTP " + strconv.Itoa(er.Status) + "): es anterior a 0.8.0"
		_, errF := k.fijar.Ejecutar(ctx, entradaFijarInforme{ID: s.ID, Estado: "no_soportado", Detalle: &d})
		return ResultadoInforme{Estado: "no_soportado"}, errF
	case errors.As(err, &er) && er.Caido():
		d := "Argos no respondió al informe (" + resumenError(err) + "); se reintenta"
		if _, errF := k.fijar.Ejecutar(ctx, entradaFijarInforme{ID: s.ID, Estado: "error", Detalle: &d}); errF != nil {
			return ResultadoInforme{}, errF
		}
		return ResultadoInforme{Estado: "error"}, err
	}
	d := "Argos rechazó el informe (" + resumenError(err) + ")"
	_, errF := k.fijar.Ejecutar(ctx, entradaFijarInforme{ID: s.ID, Estado: "error", Detalle: &d})
	if errF != nil {
		return ResultadoInforme{}, errF
	}
	return ResultadoInforme{Estado: "error"}, river.JobCancel(err)
}

// Workers ---------------------------------------------------------------------------------------

type trabajadorPrimeraSincronizacion struct {
	river.WorkerDefaults[ArgsPrimeraSincronizacion]
	m  *Modulo
	bd nucleo.BD
}

func (w *trabajadorPrimeraSincronizacion) Work(ctx context.Context, job *river.Job[ArgsPrimeraSincronizacion]) error {
	ctx = nucleo.ConBD(contexto.Sistema(ctx, contexto.CanalWorker), w.bd)
	_, err := w.m.PrimeraSincronizacion.Ejecutar(ctx, job.Args.ConectorID, job.Attempt >= job.MaxAttempts)
	var p *nucleo.Problema
	if errors.As(err, &p) && (p.Codigo() == "conector_apagado" || p.Codigo() == "sincronizacion_terminada") {
		// Se apagó (al activarlo otra vez se reanuda a mano) o ya terminó: nada que hacer.
		return river.JobCancel(err)
	}
	var er *ErrorArgos
	if errors.As(err, &er) && er.ReintentarEn() > 0 {
		return river.JobSnooze(min(er.ReintentarEn(), 6*time.Hour))
	}
	return err
}

func (w *trabajadorPrimeraSincronizacion) Timeout(*river.Job[ArgsPrimeraSincronizacion]) time.Duration {
	return time.Hour
}

type trabajadorInformarSincronizacion struct {
	river.WorkerDefaults[ArgsInformarSincronizacion]
	m  *Modulo
	bd nucleo.BD
}

func (w *trabajadorInformarSincronizacion) Work(ctx context.Context, job *river.Job[ArgsInformarSincronizacion]) error {
	ctx = nucleo.ConBD(contexto.Sistema(ctx, contexto.CanalWorker), w.bd)
	_, err := w.m.InformarSincronizacion.Ejecutar(ctx, job.Args)
	var p *nucleo.Problema
	if errors.Is(err, pgx.ErrNoRows) || (errors.As(err, &p) && p.Codigo() == "conector_apagado") {
		return river.JobCancel(err)
	}
	return err
}

// encolarPrimeraSincronizacion: en la transacción de la activación (AlActivar), solo si el conector
// nunca tuvo una (una reconexión crea un conector nuevo: se vuelve a cargar).
func (m *Modulo) encolarPrimeraSincronizacion(ctx context.Context, tx pgx.Tx, conectorID int64) error {
	if m.cfg.Encolar == nil {
		return nil
	}
	if _, err := argosbd.New(tx).UltimaSincronizacion(ctx, conectorID); err == nil {
		return nil
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	return m.cfg.Encolar(ctx, tx, ArgsPrimeraSincronizacion{ConectorID: conectorID})
}

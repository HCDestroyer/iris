package argos

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion"
	argosbd "github.com/HCDestroyer/iris/apps/api/internal/integracion/argos/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/conector"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
)

// Servicios de una falla general por verificar en Argos (contrato 0.13.0 §3.11). La apertura sale con
// los servicios que Iris ya conoce; los demás (los ids que la instantánea no tiene y el resto de los
// afectados de GET /incidentes-red/:id/servicios) se leen aquí, en un job aparte por entrante:
//
//   - con un presupuesto de lecturas por vuelta (PresupuestoVerificar); si se acaba, el job vuelve en
//     unos segundos y sigue donde quedó;
//   - por tandas de TandaVerificar, cada una en su transacción, que sale como
//     argos.falla_general_servicios con los válidos y los conteos;
//   - un error de Argos con un id solo cuenta un intento de ese id (a los IntentosVerificar queda
//     no_verificado, contado aparte de los descartados) y el job vuelve en un minuto; un error al leer
//     una página de la lista reintenta solo la lectura paginada.

// Presupuesto y tandas.
const (
	TandaVerificar       = 100
	PresupuestoVerificar = 300
	IntentosVerificar    = 3
)

// ArgsVerificarIncidente verifica los servicios por leer de una falla general.
type ArgsVerificarIncidente struct {
	EntranteID int64 `json:"entrante_id"`
}

// Kind implementa river.JobArgs.
func (ArgsVerificarIncidente) Kind() string { return "integracion.argos_verificar_incidente" }

// InsertOpts: uno por entrante.
func (ArgsVerificarIncidente) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: integracion.ColaIntegracion, MaxAttempts: 25, UniqueOpts: river.UniqueOpts{ByArgs: true}}
}

type marcaServicio struct {
	id       int64
	estado   string
	intentos int32
}

// tandaVerificada es lo que se guarda (y se publica) en una transacción.
type tandaVerificada struct {
	lectura argosbd.IntegracionArgosIncidenteLectura
	marcas  []marcaServicio
	nuevos  []int64
	pagina  bool
	cursor  *string
	paginar bool
	cerrar  bool
}

// MarshalJSON no hace falta: la auditoría guarda solo los conteos (Anotar).

func (m *Modulo) nuevoGuardarVerificacion() *auditoria.Caso[tandaVerificada, int] {
	return auditoria.Nuevo(auditoria.Caso[tandaVerificada, int]{
		Accion: "integraciones.verificar_incidente", Acceso: auditoria.Autenticado,
		AuditarExito: func(n int) bool { return n > 0 },
		Logica: func(ctx context.Context, t *auditoria.Tx, in tandaVerificada) (int, error) {
			if contexto.ActorDe(ctx).Tipo != contexto.ActorSistema {
				return 0, nucleo.Prohibido("Solo el sistema")
			}
			q := argosbd.New(t)
			l := in.lectura
			t.FijarEmpresa(l.EmpresaID)
			t.FijarEntidad("argos_entrante", l.EntranteID)
			if in.pagina {
				if len(in.nuevos) > 0 {
					if err := q.AgregarServiciosLectura(ctx, argosbd.AgregarServiciosLecturaParams{EntranteID: l.EntranteID, Servicios: in.nuevos}); err != nil {
						return 0, err
					}
				}
				if err := q.AvanzarLecturaIncidente(ctx, argosbd.AvanzarLecturaIncidenteParams{EntranteID: l.EntranteID, Cursor: in.cursor, Paginar: in.paginar}); err != nil {
					return 0, err
				}
			}
			validos := []int64{}
			descartados, noVerificados := 0, 0
			for _, x := range in.marcas {
				if err := q.MarcarServicioLectura(ctx, argosbd.MarcarServicioLecturaParams{EntranteID: l.EntranteID, ServicioID: x.id, Estado: x.estado, Intentos: x.intentos}); err != nil {
					return 0, err
				}
				switch x.estado {
				case "valido":
					validos = append(validos, x.id)
				case "descartado":
					descartados++
				case "no_verificado":
					noVerificados++
				}
			}
			if in.cerrar {
				if err := q.TerminarLecturaIncidente(ctx, l.EntranteID); err != nil {
					return 0, err
				}
			}
			n := len(validos) + descartados + noVerificados
			t.Anotar(map[string]any{"validos": len(validos), "descartados": descartados, "no_verificados": noVerificados, "nuevos": len(in.nuevos)})
			if n == 0 {
				return 0, nil
			}
			return n, t.Emitir(EventoInternoFallaServicios, map[string]any{"incidente_id": l.IncidenteID, "empresa_id": l.EmpresaID,
				"entrante_id": l.EntranteID, "servicios": validos, "descartados": descartados, "no_verificados": noVerificados})
		},
	})
}

// VerificarIncidente lee lo que falta de una falla general hasta agotar el presupuesto. Devuelve cuánto
// esperar antes de la siguiente vuelta (0 = terminó).
func (m *Modulo) VerificarIncidente(ctx context.Context, entrante int64) (time.Duration, error) {
	bd := nucleo.BDDe(ctx)
	if bd == nil {
		return 0, nucleo.ErrSinBD
	}
	q := argosbd.New(bd)
	l, err := q.LecturaIncidente(ctx, entrante)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	if l.TerminadaEn != nil {
		return 0, nil
	}
	var cli *Cliente
	cliente := func() (*Cliente, error) {
		if cli == nil {
			c, err := m.ProcesarEntrante.clienteArgos(ctx, bd, l.ConectorID)
			if err != nil {
				return nil, err
			}
			cli = c
		}
		return cli, nil
	}
	for presupuesto := PresupuestoVerificar; presupuesto > 0; {
		// Cada vuelta relee la lectura: si la falla se cerró (o la terminó otro), se corta.
		if l, err = q.LecturaIncidente(ctx, entrante); err != nil || l.TerminadaEn != nil {
			return 0, err
		}
		if l.Paginar {
			c, err := cliente()
			if err != nil {
				return 0, err
			}
			total, err := q.ContarServiciosLectura(ctx, entrante)
			if err != nil {
				return 0, err
			}
			paginas, err := q.ContarPaginaLectura(ctx, entrante)
			if err != nil {
				return 0, err
			}
			tanda := tandaVerificada{lectura: l, pagina: true}
			if int(total) >= maxServiciosIncidente || paginas > MaxPaginasIncidente {
				tanda.paginar = false
			} else {
				v := url.Values{"empresa_id": {strconv.FormatInt(l.EmpresaID, 10)}, "limit": {strconv.Itoa(LimiteServiciosIncidente)}}
				if l.Cursor != nil {
					v.Set("cursor", *l.Cursor)
				}
				presupuesto--
				p, err := conector.ListaDe[int64](ctx, c.Cliente, "/incidentes-red/"+url.PathEscape(l.IncidenteID)+"/servicios", v)
				switch {
				case EsNoEncontrado(err):
					// Fuera del alcance (404): se sigue con lo que hay.
				case err != nil:
					// Solo la lectura paginada se reintenta (River).
					return 0, err
				default:
					tanda.nuevos = p.Data
					// Sin datos, sin cursor o con el mismo cursor: no hay más que leer.
					if p.NextCursor != "" && len(p.Data) > 0 && (l.Cursor == nil || p.NextCursor != *l.Cursor) {
						tanda.cursor, tanda.paginar = &p.NextCursor, true
					}
				}
			}
			if _, err := m.guardarVerificacion.Ejecutar(ctx, tanda); err != nil {
				return 0, err
			}
			l.Paginar, l.Cursor = tanda.paginar, tanda.cursor
			continue
		}
		pend, err := q.PendientesLecturaIncidente(ctx, argosbd.PendientesLecturaIncidenteParams{EntranteID: entrante, Limite: TandaVerificar})
		if err != nil {
			return 0, err
		}
		tanda := tandaVerificada{lectura: l}
		if len(pend) == 0 {
			tanda.cerrar = true
			_, err := m.guardarVerificacion.Ejecutar(ctx, tanda)
			return 0, err
		}
		espera := time.Duration(0)
		for _, x := range pend {
			if presupuesto == 0 {
				break
			}
			s, err := q.ServicioPorID(ctx, x.ServicioID)
			if err == nil {
				// Ya llegó a la instantánea: sin leer Argos.
				tanda.marcas = append(tanda.marcas, marcaServicio{id: x.ServicioID, estado: estadoPorEmpresa(s.EmpresaID == l.EmpresaID), intentos: x.Intentos})
				continue
			}
			if !errors.Is(err, pgx.ErrNoRows) {
				return 0, err
			}
			c, err := cliente()
			if err != nil {
				return 0, err
			}
			presupuesto--
			srv, err := c.ObtenerServicio(ctx, x.ServicioID)
			var remoto *conector.ErrorRemoto
			switch {
			case EsNoEncontrado(err), errors.As(err, &remoto) && remoto.Status == http.StatusForbidden:
				tanda.marcas = append(tanda.marcas, marcaServicio{id: x.ServicioID, estado: "descartado", intentos: x.Intentos})
			case err != nil:
				// Solo este id: un intento más; los demás siguen en la próxima vuelta.
				estado := "pendiente"
				if x.Intentos+1 >= IntentosVerificar {
					estado = "no_verificado"
				}
				tanda.marcas = append(tanda.marcas, marcaServicio{id: x.ServicioID, estado: estado, intentos: x.Intentos + 1})
				espera = time.Minute
			default:
				tanda.marcas = append(tanda.marcas, marcaServicio{id: x.ServicioID, estado: estadoPorEmpresa(srv.EmpresaID == l.EmpresaID), intentos: x.Intentos})
			}
			if espera > 0 {
				break
			}
		}
		if _, err := m.guardarVerificacion.Ejecutar(ctx, tanda); err != nil {
			return 0, err
		}
		if espera > 0 {
			return espera, nil
		}
	}
	// Presupuesto agotado: sigue en otra vuelta.
	return 5 * time.Second, nil
}

func estadoPorEmpresa(misma bool) string {
	if misma {
		return "valido"
	}
	return "descartado"
}

type trabajadorVerificarIncidente struct {
	river.WorkerDefaults[ArgsVerificarIncidente]
	m  *Modulo
	bd nucleo.BD
}

func (w *trabajadorVerificarIncidente) Work(ctx context.Context, job *river.Job[ArgsVerificarIncidente]) error {
	ctx = nucleo.ConBD(contexto.Sistema(ctx, contexto.CanalWorker), w.bd)
	espera, err := w.m.VerificarIncidente(ctx, job.Args.EntranteID)
	if err != nil {
		return err
	}
	if espera > 0 {
		return river.JobSnooze(espera)
	}
	return nil
}

func (w *trabajadorVerificarIncidente) Timeout(*river.Job[ArgsVerificarIncidente]) time.Duration {
	return 10 * time.Minute
}

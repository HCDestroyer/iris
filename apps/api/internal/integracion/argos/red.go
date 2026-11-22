package argos

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	argosbd "github.com/HCDestroyer/iris/apps/api/internal/integracion/argos/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/conector"
	conectorbd "github.com/HCDestroyer/iris/apps/api/internal/integracion/conector/bd"
)

// Caídas repetidas y falla general (contrato Iris ↔ ArgosISP 0.13.0 §3.11, A49 y A50; pedidos B29 y
// B30). El conector valida lo que llega por el webhook firmado de Argos y lo publica como eventos
// internos; soporte decide los tickets (soporte/red_argos.go).
//
//   - servicio.caidas_repetidas → argos.caidas_repetidas, solo si la empresa está en el alcance del
//     conector y el servicio es de esa empresa (instantánea o GET /servicios/:id).
//   - red.falla_general_abierta → argos.falla_general_abierta enseguida, solo con los servicios que
//     Iris conoce de esa empresa (la instantánea; los de otra empresa se descartan). Los que no conoce
//     y el resto de los afectados (GET /incidentes-red/:id/servicios?empresa_id=, paginado) se leen en
//     el job argos.verificar_incidente, con su propio presupuesto, y salen por tandas como
//     argos.falla_general_servicios (red_verificar.go). Un error de Argos con un id afecta solo a ese id.
//   - red.falla_general_cerrada → argos.falla_general_cerrada (sin servicios).
//
// El evento nunca trae nombres ni direcciones; Iris tampoco los publica.

// Eventos de Argos (§2.3).
const (
	EventoCaidasRepetidas     = "servicio.caidas_repetidas"
	EventoFallaGeneralAbierta = "red.falla_general_abierta"
	EventoFallaGeneralCerrada = "red.falla_general_cerrada"
)

// Eventos internos que publica el conector.
const (
	EventoInternoCaidas          = "argos.caidas_repetidas"
	EventoInternoFallaAbierta    = "argos.falla_general_abierta"
	EventoInternoFallaCerrada    = "argos.falla_general_cerrada"
	EventoInternoFallaServicios  = "argos.falla_general_servicios"
	MaxServiciosEventoFalla      = 5000
	LimiteServiciosIncidente     = 1000
	maxServiciosIncidente        = 200_000
	maxLecturasServicioPorEvento = 1
)

var (
	reIncidente = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,100}$`)
	reAlcanceID = regexp.MustCompile(`^[A-Za-z0-9._:/-]{1,100}$`)
	alcances    = []string{"router", "olt", "pon", "zona"}
)

// payloadCaidas es servicio.caidas_repetidas (§2.3).
type payloadCaidas struct {
	ServicioID     int64           `json:"servicio_id"`
	EmpresaID      *int64          `json:"empresa_id"`
	ClienteID      *int64          `json:"cliente_id"`
	RouterID       *int64          `json:"router_id"`
	Sesiones       int             `json:"sesiones"`
	Desde          *time.Time      `json:"desde"`
	Hasta          *time.Time      `json:"hasta"`
	Umbral         *int            `json:"umbral"`
	VentanaMinutos int             `json:"ventana_minutos"`
	Causas         json.RawMessage `json:"causas"`
}

// payloadFalla es red.falla_general_abierta / _cerrada (§2.3).
type payloadFalla struct {
	IncidenteID        string          `json:"incidente_id"`
	EmpresaID          *int64          `json:"empresa_id"`
	Alcance            string          `json:"alcance"`
	AlcanceID          json.RawMessage `json:"alcance_id"`
	ServiciosAfectados struct {
		Cantidad int     `json:"cantidad"`
		IDs      []int64 `json:"ids"`
	} `json:"servicios_afectados"`
	Desde *time.Time `json:"desde"`
	Hasta *time.Time `json:"hasta"`
}

// eventoRed es lo validado de un webhook de red, listo para emitir en la transacción.
type eventoRed struct {
	evento  string
	empresa int64
	payload map[string]any
	// lectura: lo que queda por leer en Argos (ids desconocidos y la lectura paginada del resto).
	lectura *lecturaIncidente
}

type lecturaIncidente struct {
	conector    int64
	incidente   string
	empresa     int64
	paginar     bool
	desconocido []int64
}

// alcanceID lee alcance_id como texto (Argos puede mandar un número o un texto).
func alcanceID(crudo json.RawMessage) (string, bool) {
	var n json.Number
	if err := json.Unmarshal(crudo, &n); err == nil && n.String() != "" {
		return n.String(), reAlcanceID.MatchString(n.String())
	}
	var s string
	if err := json.Unmarshal(crudo, &s); err == nil {
		s = strings.TrimSpace(s)
		return s, reAlcanceID.MatchString(s)
	}
	return "", false
}

// servicioDeEmpresa dice si el servicio es de la empresa: por la instantánea si Iris lo conoce, o
// leyéndolo en Argos (404 o 403 = no existe o es de otra empresa: se descarta). Un error de Argos
// distinto se devuelve (el job se reintenta).
func (p *ProcesarEntrante) servicioDeEmpresa(ctx context.Context, q *argosbd.Queries, cli **Cliente, conectorID, servicio, empresa int64, bd conector.DBTX, lecturas *int) (bool, error) {
	s, err := q.ServicioPorID(ctx, servicio)
	if err == nil {
		return s.EmpresaID == empresa, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return false, err
	}
	if *lecturas >= maxLecturasServicioPorEvento {
		return false, nil
	}
	*lecturas++
	if *cli == nil {
		c, err := p.clienteArgos(ctx, bd, conectorID)
		if err != nil {
			return false, err
		}
		*cli = c
	}
	srv, err := (*cli).ObtenerServicio(ctx, servicio)
	var remoto *conector.ErrorRemoto
	switch {
	case EsNoEncontrado(err), errors.As(err, &remoto) && remoto.Status == http.StatusForbidden:
		return false, nil
	case err != nil:
		return false, err
	}
	return srv.EmpresaID == empresa, nil
}

// leerRed valida un webhook de red: omitido ≠ "" = se ignora (queda en la auditoría); un error de
// Argos reintenta el job.
func (p *ProcesarEntrante) leerRed(ctx context.Context, bd conector.DBTX, e conector.Entrante) (*eventoRed, string, error) {
	con, err := conectorbd.New(bd).ConectorPorID(ctx, e.ConectorID)
	if err != nil {
		return nil, "", err
	}
	q := argosbd.New(bd)
	var cli *Cliente
	lecturas := 0
	if e.Evento == EventoCaidasRepetidas {
		var c payloadCaidas
		if err := conector.Decodificar(e.Payload, &c); err != nil || c.ServicioID <= 0 || c.EmpresaID == nil || c.Sesiones <= 0 || c.VentanaMinutos <= 0 {
			return nil, "payload_invalido", nil
		}
		if !empresaEnAlcance(con, *c.EmpresaID) {
			return nil, "empresa_fuera_de_alcance", nil
		}
		ok, err := p.servicioDeEmpresa(ctx, q, &cli, e.ConectorID, c.ServicioID, *c.EmpresaID, bd, &lecturas)
		if err != nil {
			return nil, "", err
		}
		if !ok {
			return nil, "servicio_de_otra_empresa", nil
		}
		return &eventoRed{evento: EventoInternoCaidas, empresa: *c.EmpresaID, payload: map[string]any{
			"servicio_id": c.ServicioID, "empresa_id": *c.EmpresaID, "cliente_id": c.ClienteID, "router_id": c.RouterID,
			"sesiones": c.Sesiones, "ventana_minutos": c.VentanaMinutos, "desde": fechaRed(c.Desde), "hasta": fechaRed(c.Hasta),
			"entrante_id": e.ID,
		}}, "", nil
	}
	var f payloadFalla
	if err := conector.Decodificar(e.Payload, &f); err != nil || !reIncidente.MatchString(f.IncidenteID) || f.EmpresaID == nil ||
		!slices.Contains(alcances, f.Alcance) || f.Desde == nil {
		return nil, "payload_invalido", nil
	}
	aid, ok := alcanceID(f.AlcanceID)
	if !ok {
		return nil, "payload_invalido", nil
	}
	if !empresaEnAlcance(con, *f.EmpresaID) {
		return nil, "empresa_fuera_de_alcance", nil
	}
	base := map[string]any{"incidente_id": f.IncidenteID, "empresa_id": *f.EmpresaID, "alcance": f.Alcance, "alcance_id": aid,
		"desde": fechaRed(f.Desde), "hasta": fechaRed(f.Hasta), "entrante_id": e.ID}
	if e.Evento == EventoFallaGeneralCerrada {
		return &eventoRed{evento: EventoInternoFallaCerrada, empresa: *f.EmpresaID, payload: base}, "", nil
	}
	ids := f.ServiciosAfectados.IDs
	if len(ids) > MaxServiciosEventoFalla {
		ids = ids[:MaxServiciosEventoFalla]
	}
	// Enseguida, solo con la instantánea: ninguna lectura a Argos en este job.
	vistos := map[int64]bool{}
	validos := make([]int64, 0, len(ids))
	desconocidos := []int64{}
	descartados := 0
	for _, id := range ids {
		if id <= 0 || vistos[id] {
			continue
		}
		vistos[id] = true
		s, err := q.ServicioPorID(ctx, id)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			desconocidos = append(desconocidos, id)
		case err != nil:
			return nil, "", err
		case s.EmpresaID == *f.EmpresaID:
			validos = append(validos, id)
		default:
			descartados++
		}
	}
	paginar := f.ServiciosAfectados.Cantidad > len(ids)
	base["servicios"] = validos
	base["descartados"] = descartados
	base["por_verificar"] = len(desconocidos)
	base["paginando"] = paginar
	r := &eventoRed{evento: EventoInternoFallaAbierta, empresa: *f.EmpresaID, payload: base}
	if len(desconocidos) > 0 || paginar {
		r.lectura = &lecturaIncidente{conector: e.ConectorID, incidente: f.IncidenteID, empresa: *f.EmpresaID, paginar: paginar, desconocido: desconocidos}
	}
	return r, "", nil
}

func fechaRed(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.UTC().Format(time.RFC3339Nano)
	return &s
}

func emitirRed(ctx context.Context, t *auditoria.Tx, r eventoRed, entrante int64, encolar Encolador) error {
	t.FijarEmpresa(r.empresa)
	if l := r.lectura; l != nil {
		q := argosbd.New(t)
		if err := q.CrearLecturaIncidente(ctx, argosbd.CrearLecturaIncidenteParams{EntranteID: entrante, ConectorID: l.conector,
			IncidenteID: l.incidente, EmpresaID: l.empresa, Paginar: l.paginar}); err != nil {
			return err
		}
		if len(l.desconocido) > 0 {
			if err := q.AgregarServiciosLectura(ctx, argosbd.AgregarServiciosLecturaParams{EntranteID: entrante, Servicios: l.desconocido}); err != nil {
				return err
			}
		}
		if encolar != nil {
			if err := encolar(ctx, t.Tx, ArgsVerificarIncidente{EntranteID: entrante}); err != nil {
				return err
			}
		}
	}
	return t.Emitir(r.evento, r.payload)
}

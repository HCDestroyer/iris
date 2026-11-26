package argos

import (
	"context"
	"errors"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	argosbd "github.com/HCDestroyer/iris/apps/api/internal/integracion/argos/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/conector"
)

// Tipo de servicio (contrato argosisp.md 0.15.0 §13.1, A53 / B33). Argos suma a los servicios y a
// los planes `tipo_servicio` (internet por defecto, cable, iptv, mixto) y `componentes[]` (los de
// internet, cable e iptv que incluye). Iris los guarda en la instantánea como lista blanca y aplica
// A1: un servicio tiene router y velocidad solo si sus componentes incluyen internet o iptv; un tipo
// desconocido cuenta como internet, salvo que router_id sea null.
//
// Compatibilidad (§4.6): un Argos < 0.15.0 no manda los campos; sin ellos (null en la instantánea)
// el servicio se trata como internet, con router y velocidad, como hasta ahora. Lo que Iris MANDA de
// nuevo a Argos (el filtro tipo_servicio de GET /planes) va solo con un Argos que informa ≥ 0.15.0.

// ContratoTipoServicio es el primer contrato con el tipo de servicio y el corte manual (0.15.0).
const ContratoTipoServicio = "0.15.0"

// Tipos de servicio de §13.1.
const (
	TipoInternet = "internet"
	TipoCable    = "cable"
	TipoIPTV     = "iptv"
	TipoMixto    = "mixto"
	// TipoOtro: lo que Iris muestra de un tipo que Argos agregue después (A1: cuenta como internet,
	// salvo sin router).
	TipoOtro = "otro"
)

// Componentes de §13.1 (la lista blanca).
var componentesValidos = []string{TipoInternet, TipoCable, TipoIPTV}

// CodigoTipoSinRouter: Argos responde 422 tipo_sin_router a reservar-ip, aprovisionar,
// GET /planes?router_id= y cambiar-plan con un servicio de cable (§13.1, A3). También es el motivo
// de una factibilidad indeterminada de cable (A4) y de los planes no disponibles en la orden.
const CodigoTipoSinRouter = "tipo_sin_router"

var reTipo = regexp.MustCompile(`^[a-z][a-z0-9_]{0,29}$`)

// NormalizarTipo deja el tipo y los componentes como se guardan: el tipo en minúsculas (nil si no
// vino o no tiene forma de código) y solo los componentes conocidos, sin repetir y en orden (nil si
// no vino ninguno). Un tipo simple sin componentes toma el mismo tipo como componente (§13.1).
func NormalizarTipo(tipo string, componentes []string) (*string, []string) {
	var t *string
	if v := strings.ToLower(strings.TrimSpace(tipo)); reTipo.MatchString(v) {
		t = &v
	}
	var cs []string
	for _, c := range componentesValidos {
		for _, x := range componentes {
			if strings.EqualFold(strings.TrimSpace(x), c) {
				cs = append(cs, c)
				break
			}
		}
	}
	if len(cs) == 0 && t != nil && slices.Contains(componentesValidos, *t) {
		cs = []string{*t}
	}
	return t, cs
}

// ConRouter aplica A1: el servicio (o el plan) tiene router y velocidad si sus componentes incluyen
// internet o iptv. Sin componentes: un tipo conocido decide (cable no, los demás sí); un tipo
// desconocido o ausente cuenta como internet, salvo que routerConocido sea true y router sea nil
// (un servicio desconocido sin router no tiene router). Para un plan, routerConocido = false.
func ConRouter(tipo *string, componentes []string, router *int64, routerConocido bool) bool {
	if len(componentes) > 0 {
		return slices.Contains(componentes, TipoInternet) || slices.Contains(componentes, TipoIPTV)
	}
	if tipo == nil {
		return true // Argos anterior a 0.15.0: como hoy
	}
	switch *tipo {
	case TipoCable:
		return false
	case TipoInternet, TipoIPTV, TipoMixto:
		return true
	}
	return !routerConocido || router != nil
}

// IncluyeCable: el servicio tiene el componente cable (cable o mixto; B8: en un mixto solo se toca
// el cable).
func IncluyeCable(tipo *string, componentes []string) bool {
	if len(componentes) > 0 {
		return slices.Contains(componentes, TipoCable)
	}
	return tipo != nil && (*tipo == TipoCable || *tipo == TipoMixto)
}

// TipoVisible es el tipo que muestra Iris: internet, cable, iptv, mixto u otro; "" si Argos no lo
// informó.
func TipoVisible(tipo *string) string {
	if tipo == nil {
		return ""
	}
	switch *tipo {
	case TipoInternet, TipoCable, TipoIPTV, TipoMixto:
		return *tipo
	}
	return TipoOtro
}

// SoportaTipoServicio indica si un contrato incluye el tipo de servicio (0.15.0).
func SoportaTipoServicio(contrato *string) bool {
	return conector.VersionAlMenos(contrato, ContratoTipoServicio)
}

// ArgosConTipoServicio indica si el conector Argos registrado más reciente (activo o, si no hay, el
// último) informa contrato ≥ 0.15.0. Falla cerrado: false sin conector o sin contrato informado.
func ArgosConTipoServicio(ctx context.Context, q conector.DBTX) (bool, error) {
	return contratoArgosAlMenos(ctx, q, ContratoTipoServicio)
}

// guardarPlan escribe la instantánea de un plan (lista blanca: nombre, tipo, componentes y activo).
func guardarPlan(ctx context.Context, q *argosbd.Queries, p PlanArgos, leidoEn time.Time) error {
	tipo, componentes := NormalizarTipo(p.TipoServicio, p.Componentes)
	return q.GuardarPlan(ctx, argosbd.GuardarPlanParams{
		ID: p.ID, Nombre: strings.TrimSpace(p.Nombre), TipoServicio: tipo, Componentes: componentes,
		Activo: *activoDe(p.Activo, p.Estado), SincronizadoEn: leidoEn,
	})
}

// PlanConTipo es lo que Iris sabe del tipo de un plan (instantánea de planes).
type PlanConTipo struct {
	ID           int64
	Nombre       string
	TipoServicio *string
	Componentes  []string
	Conocido     bool // false: el plan no está en la instantánea (se trata como internet)
}

// ConRouter: A1 para el plan (sin router propio: un tipo desconocido cuenta como internet).
func (p PlanConTipo) ConRouter() bool { return ConRouter(p.TipoServicio, p.Componentes, nil, false) }

// TipoDePlan lee el tipo de un plan de la instantánea; Conocido = false si no está.
func TipoDePlan(ctx context.Context, q conector.DBTX, planID int64) (PlanConTipo, error) {
	f, err := argosbd.New(q).PlanPorID(ctx, planID)
	if errors.Is(err, pgx.ErrNoRows) {
		return PlanConTipo{ID: planID}, nil
	}
	if err != nil {
		return PlanConTipo{}, err
	}
	return PlanConTipo{ID: f.ID, Nombre: f.Nombre, TipoServicio: f.TipoServicio, Componentes: f.Componentes, Conocido: true}, nil
}

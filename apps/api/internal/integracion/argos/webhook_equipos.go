package argos

import (
	"context"
	"regexp"
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	argosbd "github.com/HCDestroyer/iris/apps/api/internal/integracion/argos/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/conector"
	conectorbd "github.com/HCDestroyer/iris/apps/api/internal/integracion/conector/bd"
)

// Equipos del cliente (contrato argosisp.md 0.12.0 §2.3 y §3.10, B27): Argos avisa los cambios de
// equipo y los cambios de estado de cada unidad. Iris guarda una instantánea de lectura (estado y
// servicio de la unidad) y su historial, que muestran la orden y la ficha. Sin costos, proveedor ni
// RMA: Argos no los manda y Iris guarda solo la lista blanca de abajo.

// Webhooks de Argos (0.12.0 §2.3). Solo a conectores con contrato ≥ 0.12.0.
const (
	EventoEquipoCambiado       = "inventario.equipo_cambiado"
	EventoUnidadEstadoCambiado = "inventario.unidad_estado_cambiado"
)

// Eventos internos (outbox) que dejan.
const (
	EventoEquipoCambiadoInterno = "argos.equipo_cambiado"
	EventoUnidadEstadoInterno   = "argos.unidad_estado_cambiado"
)

// EstadosUnidad: los estados de una unidad en Argos (0.12.0 §3.10).
var EstadosUnidad = map[string]bool{
	"disponible": true, "en_comodato": true, "por_recuperar": true, "revision": true,
	"garantia": true, "dada_de_baja": true, "no_recuperada": true,
}

// destinosSale: el destino de la unidad que sale en un cambio (A48).
var destinosSale = map[string]bool{"funciona": true, "revision": true, "garantia": true, "danado": true}

// reMotivoUnidad: un motivo es un código simple (nunca texto libre del panel de Argos).
var reMotivoUnidad = regexp.MustCompile(`^[a-z0-9_]{1,60}$`)

func motivoUnidad(s *string) *string {
	if s == nil || !reMotivoUnidad.MatchString(*s) {
		return nil
	}
	v := *s
	return &v
}

// equipoCambiado es el payload de inventario.equipo_cambiado (§2.3).
type equipoCambiado struct {
	ServicioID    *conector.Numero `json:"servicio_id"`
	ClienteID     *conector.Numero `json:"cliente_id"`
	EmpresaID     *conector.Numero `json:"empresa_id"`
	SaleUnidadID  *conector.Numero `json:"sale_unidad_id"`
	EntraUnidadID *conector.Numero `json:"entra_unidad_id"`
	Motivo        *string          `json:"motivo"`
	DestinoSale   *string          `json:"destino_sale"`
	Origen        *string          `json:"origen"`
	OrigenID      *conector.Texto  `json:"origen_id"`
}

// unidadEstadoCambiado es el payload de inventario.unidad_estado_cambiado (§2.3).
type unidadEstadoCambiado struct {
	UnidadID   *conector.Numero `json:"unidad_id"`
	EmpresaID  *conector.Numero `json:"empresa_id"`
	ServicioID *conector.Numero `json:"servicio_id"`
	De         *string          `json:"de"`
	A          *string          `json:"a"`
	Motivo     *string          `json:"motivo"`
}

// eventoEquipos es lo que el webhook validado deja en la transacción.
type eventoEquipos struct {
	evento    string
	empresa   int64
	ocurrido  time.Time
	entrante  int64
	estado    *argosbd.GuardarEstadoUnidadParams
	servicios []argosbd.GuardarServicioUnidadParams
	historial []argosbd.AgregarHistorialUnidadParams
	payload   map[string]any
}

func positivo(n *conector.Numero) *int64 {
	v := n.Entero()
	if v == nil || *v <= 0 {
		return nil
	}
	return v
}

// deEquipos valida un webhook de equipos del cliente. omitido dice por qué se ignora: contrato del
// conector anterior a la 0.12.0, payload inválido o empresa fuera del alcance del token.
func deEquipos(e conector.Entrante, con conectorbd.IntegracionConectore) (*eventoEquipos, string) {
	if !conector.VersionAlMenos(con.ContratoRemoto, ContratoEquipos) {
		return nil, "contrato_anterior"
	}
	ocurrido := e.RecibidoEn
	if e.OcurridoEn != nil {
		ocurrido = *e.OcurridoEn
	}
	ev := &eventoEquipos{evento: e.Evento, ocurrido: ocurrido, entrante: e.ID}
	switch e.Evento {
	case EventoUnidadEstadoCambiado:
		var p unidadEstadoCambiado
		if err := conector.Decodificar(e.Payload, &p); err != nil {
			return nil, "payload_invalido"
		}
		unidad, empresa := positivo(p.UnidadID), positivo(p.EmpresaID)
		if unidad == nil || empresa == nil || p.A == nil || !EstadosUnidad[*p.A] || (p.De != nil && !EstadosUnidad[*p.De]) ||
			(p.ServicioID != nil && positivo(p.ServicioID) == nil) {
			return nil, "payload_invalido"
		}
		if !empresaEnAlcance(con, *empresa) {
			return nil, "empresa_fuera_de_alcance"
		}
		servicio := positivo(p.ServicioID)
		ev.empresa = *empresa
		ev.estado = &argosbd.GuardarEstadoUnidadParams{UnidadID: *unidad, EmpresaID: *empresa, ServicioID: servicio,
			Estado: p.A, OcurridoEn: &ocurrido, EntranteID: e.ID}
		ev.historial = []argosbd.AgregarHistorialUnidadParams{{UnidadID: *unidad, EmpresaID: *empresa, ServicioID: servicio,
			Tipo: "estado", De: p.De, A: p.A, Motivo: motivoUnidad(p.Motivo), OcurridoEn: ocurrido, EntranteID: e.ID}}
		ev.payload = map[string]any{"unidad_id": *unidad, "empresa_id": *empresa, "servicio_id": servicio, "de": p.De, "a": *p.A,
			"motivo": motivoUnidad(p.Motivo), "entrante_id": e.ID}
		return ev, ""
	case EventoEquipoCambiado:
		var p equipoCambiado
		if err := conector.Decodificar(e.Payload, &p); err != nil {
			return nil, "payload_invalido"
		}
		servicio, empresa := positivo(p.ServicioID), positivo(p.EmpresaID)
		sale, entra := positivo(p.SaleUnidadID), positivo(p.EntraUnidadID)
		if servicio == nil || empresa == nil || sale == nil || entra == nil || *sale == *entra ||
			(p.DestinoSale != nil && !destinosSale[*p.DestinoSale]) {
			return nil, "payload_invalido"
		}
		if !empresaEnAlcance(con, *empresa) {
			return nil, "empresa_fuera_de_alcance"
		}
		var origen, origenID *string
		if p.Origen != nil && *p.Origen == "iris_orden" {
			origen, origenID = p.Origen, texto(p.OrigenID)
		}
		motivo := motivoUnidad(p.Motivo)
		ev.empresa = *empresa
		ev.servicios = []argosbd.GuardarServicioUnidadParams{
			{UnidadID: *sale, EmpresaID: *empresa, OcurridoEn: &ocurrido, EntranteID: e.ID},
			{UnidadID: *entra, EmpresaID: *empresa, ServicioID: servicio, OcurridoEn: &ocurrido, EntranteID: e.ID},
		}
		comun := argosbd.AgregarHistorialUnidadParams{EmpresaID: *empresa, ServicioID: servicio, Motivo: motivo,
			Origen: origen, OrigenID: origenID, OcurridoEn: ocurrido, EntranteID: e.ID}
		hSale, hEntra := comun, comun
		hSale.UnidadID, hSale.Tipo, hSale.DestinoSale = *sale, "sale", p.DestinoSale
		hEntra.UnidadID, hEntra.Tipo = *entra, "entra"
		ev.historial = []argosbd.AgregarHistorialUnidadParams{hSale, hEntra}
		ev.payload = map[string]any{"servicio_id": *servicio, "cliente_id": positivo(p.ClienteID), "empresa_id": *empresa,
			"sale_unidad_id": *sale, "entra_unidad_id": *entra, "motivo": motivo, "destino_sale": p.DestinoSale,
			"origen": origen, "origen_id": origenID, "entrante_id": e.ID}
		return ev, ""
	}
	return nil, "evento_desconocido"
}

// aplicarEquipos guarda la instantánea y el historial y emite el evento interno.
func aplicarEquipos(ctx context.Context, t *auditoria.Tx, q *argosbd.Queries, ev eventoEquipos) error {
	if ev.estado != nil {
		if err := q.GuardarEstadoUnidad(ctx, *ev.estado); err != nil {
			return err
		}
	}
	for _, s := range ev.servicios {
		if err := q.GuardarServicioUnidad(ctx, s); err != nil {
			return err
		}
	}
	for _, h := range ev.historial {
		if err := q.AgregarHistorialUnidad(ctx, h); err != nil {
			return err
		}
	}
	interno := EventoUnidadEstadoInterno
	if ev.evento == EventoEquipoCambiado {
		interno = EventoEquipoCambiadoInterno
	}
	empresa := ev.empresa
	return t.EmitirEnEmpresa(interno, &empresa, ev.payload)
}

// --- Lectura para la orden y la ficha -----------------------------------------------------------

// MovimientoUnidad es un renglón del historial de una unidad (sin costos, proveedor ni RMA).
type MovimientoUnidad struct {
	Tipo        string    `json:"tipo" enum:"estado,sale,entra" doc:"estado: cambió de estado; sale / entra: salió o entró en un cambio de equipo"`
	De          *string   `json:"de" nullable:"true"`
	A           *string   `json:"a" nullable:"true"`
	Motivo      *string   `json:"motivo" nullable:"true"`
	DestinoSale *string   `json:"destinoSale" nullable:"true" enum:"funciona,revision,garantia,danado,"`
	ServicioID  *int64    `json:"servicioId" nullable:"true"`
	OrdenID     *string   `json:"ordenId" nullable:"true" doc:"La orden de Iris que hizo el cambio (origen iris_orden), si la hubo"`
	OcurridoEn  time.Time `json:"ocurridoEn"`
}

// EstadoUnidad es el estado de una unidad según los avisos de Argos, con su historial reciente.
type EstadoUnidad struct {
	UnidadID      int64              `json:"unidadId"`
	EmpresaID     int64              `json:"empresaId"`
	ServicioID    *int64             `json:"servicioId" nullable:"true" doc:"El servicio al que la asocia Argos (null: ninguno)"`
	Estado        *string            `json:"estado" nullable:"true" enum:"disponible,en_comodato,por_recuperar,revision,garantia,dada_de_baja,no_recuperada," doc:"null: Argos aún no avisó su estado"`
	EstadoEn      *time.Time         `json:"estadoEn" nullable:"true"`
	ActualizadoEn time.Time          `json:"actualizadoEn"`
	Historial     []MovimientoUnidad `json:"historial" doc:"Del más nuevo al más viejo (hasta 20)"`
}

// HistorialMaximo: renglones del historial por unidad.
const HistorialMaximo = 20

// EstadosDeUnidades lee la instantánea de las unidades del servicio y de las dadas (las de una
// orden), solo de las empresas dadas (las del actor), con su historial. Vacío si Argos no avisó
// nada todavía.
func EstadosDeUnidades(ctx context.Context, q conector.DBTX, empresas []int64, servicio int64, unidades []int64) ([]EstadoUnidad, error) {
	bq := argosbd.New(q)
	if unidades == nil {
		unidades = []int64{}
	}
	filas, err := bq.UnidadesDeServicioOIDs(ctx, argosbd.UnidadesDeServicioOIDsParams{Empresas: empresas, ServicioID: servicio, Unidades: unidades})
	if err != nil {
		return nil, err
	}
	out := make([]EstadoUnidad, 0, len(filas))
	if len(filas) == 0 {
		return out, nil
	}
	ids := make([]int64, 0, len(filas))
	idx := map[int64]int{}
	for _, f := range filas {
		idx[f.UnidadID] = len(out)
		ids = append(ids, f.UnidadID)
		out = append(out, EstadoUnidad{UnidadID: f.UnidadID, EmpresaID: f.EmpresaID, ServicioID: f.ServicioID, Estado: f.Estado,
			EstadoEn: f.EstadoEn, ActualizadoEn: f.ActualizadoEn, Historial: []MovimientoUnidad{}})
	}
	hist, err := bq.HistorialDeUnidades(ctx, argosbd.HistorialDeUnidadesParams{Unidades: ids, Empresas: empresas, Limite: HistorialMaximo})
	if err != nil {
		return nil, err
	}
	for _, h := range hist {
		i, ok := idx[h.UnidadID]
		if !ok {
			continue
		}
		mov := MovimientoUnidad{Tipo: h.Tipo, De: h.De, A: h.A, Motivo: h.Motivo, DestinoSale: h.DestinoSale, ServicioID: h.ServicioID,
			OcurridoEn: h.OcurridoEn}
		if h.Origen != nil && *h.Origen == "iris_orden" {
			mov.OrdenID = h.OrigenID
		}
		out[i].Historial = append(out[i].Historial, mov)
	}
	return out, nil
}

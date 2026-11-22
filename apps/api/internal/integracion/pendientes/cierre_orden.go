package pendientes

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos"
	pendientesbd "github.com/HCDestroyer/iris/apps/api/internal/integracion/pendientes/bd"
)

// Acciones en Argos al cerrar una orden (contratos/argosisp.md §3). El payload que se espera en
// orden.cerrada (lo emite campo, I2) está en contratos/eventos.md, «Pendientes hacia Argos».

// Tipos de orden y resultado que disparan acciones.
const (
	TipoInstalacion  = "instalacion"
	TipoRetiro       = "retiro"
	TipoCambioEquipo = "cambio_equipo"
	ResultadoExitosa = "exitosa"
	AccionInstalar   = "instalar"
	AccionRetirar    = "retirar"
	CondicionPerdido = "perdido"
	OrigenTipo       = "iris_orden"
)

// Operaciones (integracion.pendientes.operacion).
const (
	OpActivarInstalacion = "activar_instalacion"
	OpAsignarComodato    = "asignar_comodato"
	OpConsumo            = "consumo"
	OpDevolucion         = "devolucion"
	OpPerdida            = "perdida"
	OpRetiro             = "retiro"
	OpCargo              = "cargo"
	// OpRecepcion: el técnico recibe una orden de traslado (0.5.0 §3.3; no es de una orden de trabajo).
	OpRecepcion = "recepcion"
)

// IDArgos es un id de Argos: entero, en JSON como número o como texto numérico.
type IDArgos int64

// UnmarshalJSON acepta 12 o "12".
func (i *IDArgos) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(bytes.TrimSpace(b)), `"`)
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n <= 0 {
		return fmt.Errorf("id de Argos inválido: %s", b)
	}
	*i = IDArgos(n)
	return nil
}

// IDTexto es un id de Iris en JSON como texto o número (orden_id: uuid o bigint).
type IDTexto string

// UnmarshalJSON acepta "…" o 12.
func (i *IDTexto) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		*i = IDTexto(strings.TrimSpace(s))
		return nil
	}
	var n json.Number
	if err := json.Unmarshal(b, &n); err != nil {
		return fmt.Errorf("id inválido: %s", b)
	}
	*i = IDTexto(n.String())
	return nil
}

// Material usado en la orden.
type Material struct {
	ProductoID *IDArgos    `json:"producto_id"`
	Cantidad   json.Number `json:"cantidad"`
	BodegaID   *IDArgos    `json:"bodega_id"`
	// Modalidad del producto en Argos (0.5.0) al registrarlo; vacía si no se sabía.
	Modalidad string `json:"modalidad"`
	// Fecha (del teléfono) y RecibidoEnIris (del servidor): 0.12.0 §3.3, para la ventana de Argos.
	Fecha          *time.Time `json:"fecha"`
	RecibidoEnIris *time.Time `json:"recibido_en_iris"`
	// DesdeApp: lo registró un técnico desde el lote (RegistradoPor); si no, despacho en el panel.
	DesdeApp      bool   `json:"desde_app"`
	RegistradoPor *int64 `json:"registrado_por"`
	// BodegaTipo: el tipo de la bodega según Argos al registrarlo desde el panel (o null si no se
	// supo). Con Argos ≥ 0.11.0, despacho solo usa bodegas tipo = bodega (M-4).
	BodegaTipo *string `json:"bodega_tipo"`
}

// TecnicoArgos es `tecnico` en orden.cerrada (contrato 0.5.0 §3.3): el vínculo de la ficha.
type TecnicoArgos struct {
	IDExterno string `json:"id_externo"`
}

// Equipo instalado o retirado en la orden.
type Equipo struct {
	ID        string   `json:"id"`
	Accion    string   `json:"accion"` // instalar | retirar | cambio | no_recuperado
	UnidadID  *IDArgos `json:"unidad_id"`
	Serie     string   `json:"serie"`
	MAC       string   `json:"mac"`
	Condicion string   `json:"condicion"` // al retirar: bueno, danado… o perdido (no se recuperó)
	BodegaID  *IDArgos `json:"bodega_id"` // bodega a la que se devuelve (si no, la de la orden)
	// Cambio y no recuperado (0.12.0 §3.10). La nota no viaja en el evento: se lee de la orden.
	SaleUnidadID   *IDArgos `json:"sale_unidad_id"`
	Motivo         string   `json:"motivo"`
	DestinoSale    string   `json:"destino_sale"`
	CobroPropuesto *struct {
		Motivo string `json:"motivo"`
	} `json:"cobro_propuesto"`
	FotoRef   string `json:"foto_ref"`
	Ubicacion *struct {
		Lat        *float64 `json:"lat"`
		Lng        *float64 `json:"lng"`
		PrecisionM *float64 `json:"precision_m"`
	} `json:"ubicacion"`
	Fecha          *time.Time `json:"fecha"`
	RecibidoEnIris *time.Time `json:"recibido_en_iris"`
	// RegistradoPor: el técnico que registró el cambio o el intento; va en X-Iris-Actor (A-271-1),
	// aunque la orden se reasigne después.
	RegistradoPor *int64 `json:"registrado_por"`
	// DesdeApp: lo registró un técnico desde el lote; en comodatos y devoluciones, con Argos ≥ 0.11.0,
	// sale con su I1 y su X-Iris-Actor (0.13.1 §3.9).
	DesdeApp bool `json:"desde_app"`
}

// EsperaPersonaEquipoPanel: un comodato o una devolución registrados desde el panel con Argos ≥
// 0.11.0 (M-5): sin tecnico ni el técnico de la orden como actor, y sin saber si la bodega es tipo
// bodega; lo hace una persona en Argos.
const EsperaPersonaEquipoPanel = "registrado desde el panel: con Argos ≥ 0.11.0 el comodato o la devolución sin técnico lo hace una persona en Argos desde una bodega general"

// Acciones de equipo de 0.12.0 (§3.10).
const (
	AccionCambio       = "cambio"
	AccionNoRecuperado = "no_recuperado"
	OpCambio           = "cambio"
	OpNoRecuperado     = "no_recuperado"
	// MotivoPerdidaSinIntento: un retiro «perdido» de una app anterior, sin foto ni ubicación, con
	// Argos ≥ 0.12.0: no se manda (la pérdida la declara una persona en Argos).
	MotivoPerdidaSinIntento = "perdida_sin_intento"
	// EsperaPersonaPerdida: con Argos < 0.12.0, la pérdida espera a una persona con
	// ordenes.declarar_perdida; nunca sale sola desde el cierre.
	EsperaPersonaPerdida = "requiere una persona con ordenes.declarar_perdida: revisa el intento y reintenta a mano (Argos < 0.12.0)"
)

// notasEquipo: las notas de los equipos de la orden (la nota y la del cobro propuesto), que no viajan
// en el evento (§10.7 i). Por id del equipo.
type notasEquipo struct{ nota, cobro *string }

// opcionesPlan es lo que planificar necesita saber de Argos y de Iris (sin E/S en planificar).
type opcionesPlan struct {
	porTecnico bool
	cuadrillas map[int64]bool
	// personales: las bodegas personales de técnicos según Argos (0.13.1 §3.9).
	personales map[int64]bool
	// bodegaApp: Argos con contrato ≥ 0.11.0 (§3.9): el material de la app sale con tecnico,
	// bodega_id y X-Iris-Actor de quien lo registró.
	bodegaApp bool
	// vinculos: el vínculo I1 (id_externo) de cada usuario que registró algo de la orden: el
	// `tecnico` del cuerpo es el del actor, no el del técnico actual de la orden (0.13.1).
	vinculos map[int64]string
	// equipos: Argos con contrato ≥ 0.12.0 (cambiar y no-recuperado, fecha y recibido_en_iris).
	equipos bool
	notas   map[string]notasEquipo
}

// Cargo único que la orden indica cobrar.
type Cargo struct {
	ProductoID  *IDArgos    `json:"producto_id"`
	Monto       json.Number `json:"monto"`
	Descripcion string      `json:"descripcion"`
}

// OrdenCerrada es el payload de orden.cerrada que necesita esta cola.
type OrdenCerrada struct {
	OrdenID    IDTexto  `json:"orden_id"`
	Tipo       string   `json:"tipo"`
	Resultado  string   `json:"resultado"`
	EmpresaID  *int64   `json:"empresa_id"`
	ClienteID  *IDArgos `json:"cliente_id"`
	ServicioID *IDArgos `json:"servicio_id"`
	TecnicoID  *int64   `json:"tecnico_id"`
	BodegaID   *IDArgos `json:"bodega_id"`
	// Tecnico: { id_externo } del técnico en Argos, o null sin vínculo (0.5.0).
	Tecnico         *TecnicoArgos `json:"tecnico"`
	RetirarServicio bool          `json:"retirar_servicio"`
	Materiales      []Material    `json:"materiales"`
	Equipos         []Equipo      `json:"equipos"`
	Cargo           *Cargo        `json:"cargo"`

	// EventoID del outbox (no viene en el payload).
	EventoID uuid.UUID `json:"-"`
}

// ResultadoCierre: qué se encoló.
type ResultadoCierre struct {
	OrdenID    string  `json:"orden_id"`
	Pendientes []int64 `json:"pendientes"`
	Omitido    bool    `json:"omitido"`
}

// plan es una llamada a encolar.
type plan struct {
	operacion, clave, ruta string
	payload                map[string]any
	depende                []int  // índices de otros planes de la misma orden
	incompleto             string // dato faltante: nace fallido_negocio
	// esperaPersona: nace fallido_negocio (sin ser dato faltante) hasta que una persona lo decida.
	esperaPersona string
	// conTecnico: el cuerpo lleva `tecnico` (se corrige al reintentar a mano tras vincular).
	conTecnico bool
	// conActor: X-Iris-Actor es `actor` (quien lo registró), nunca el técnico actual de la orden;
	// sin actor, sale sin X-Iris-Actor (y el plan queda incompleto).
	conActor bool
	// actor: quien va en X-Iris-Actor (tecnico_usuario_id) si no es el técnico de la orden.
	actor *int64
	// externos: pendientes ya encolados (los pasos del titular, §3.4) que también deben estar
	// hechos antes; van en depende_de después de los de depende.
	externos []int64
}

// MotivoTitularResueltoAMano: el último cambio de titular de la orden se resolvió a mano en Argos y
// nadie indicó el cliente final: el cobro y el consumo no salen solos (podrían ir al cliente
// equivocado).
const MotivoTitularResueltoAMano = "titular resuelto a mano: confirma el cliente"

// despuesDelTitular: la activación, el consumo y el cobro de la instalación (lo que emite cargos)
// esperan a los pasos del titular (contrato 0.10.0 §3.4, «Orden»: titular → datos → plan →
// activar). Si el titular cambió (A39), el cobro va al cliente que quedó en el servicio
// (titular.cliente_id de la respuesta, B21). Si ese cambio se resolvió a mano, va al cliente que
// indicó quien lo resolvió o, sin él, el cobro y el consumo nacen incompletos.
func despuesDelTitular(planes []plan, pasos []pendientesbd.IntegracionPendiente) {
	if len(pasos) == 0 {
		return
	}
	// Los resueltos (a mano, reemplazados por otra toma, por cancelar o validados) ya cuentan como
	// hechos: no se esperan (un marcador nunca se resolvería con ellos).
	ids := make([]int64, 0, len(pasos))
	titular := -1                                // índice en ids del último cambio de titular vivo
	var aMano *pendientesbd.IntegracionPendiente // último cambio de titular resuelto a mano
	for i, p := range pasos {
		if p.Estado == EstadoResueltoManual {
			if p.Operacion == OpCambiarTitular && (p.ResueltoComo == nil || *p.ResueltoComo == ComoManual) {
				titular, aMano = -1, &pasos[i]
			}
			continue
		}
		ids = append(ids, p.ID)
		if p.Operacion == OpCambiarTitular {
			titular, aMano = len(ids)-1, nil
		}
	}
	for i := range planes {
		p := &planes[i]
		if p.operacion != OpActivarInstalacion && p.operacion != OpCargo && p.operacion != OpConsumo {
			continue
		}
		p.externos = ids
		if p.incompleto != "" {
			continue
		}
		switch {
		case aMano != nil && (p.operacion == OpCargo || p.operacion == OpConsumo):
			if aMano.ResueltoClienteID == nil {
				p.incompleto = MotivoTitularResueltoAMano
			} else if p.operacion == OpCargo {
				p.payload["cliente_id"] = *aMano.ResueltoClienteID
			}
		case p.operacion == OpCargo && titular >= 0:
			p.payload["cliente_id"] = Ref(len(p.depende)+titular, "titular.cliente_id")
		}
	}
}

// omision es algo de la orden que no se manda a Argos, con su motivo (integracion.pendiente_omitido).
type omision struct {
	motivo      string
	operaciones []string
}

// Motivos de omisión (además de sin_conector).
const (
	// MotivoMaterialComodato: un material de modalidad comodato no va en el consumo (422
	// producto_es_comodato): se registra como equipo, con serie, por POST /servicios/:id/equipos.
	MotivoMaterialComodato = "material_comodato"
)

func origen(ordenID string) map[string]any {
	return map[string]any{"origen_tipo": OrigenTipo, "origen_id": ordenID}
}

func con(base map[string]any, extra map[string]any) map[string]any {
	for k, v := range extra {
		base[k] = v
	}
	return base
}

// numero lee un número de la orden: ok = es numérico; positivo = además > 0 (cantidades y
// montos deben serlo).
func numero(n json.Number) (valor json.Number, ok, positivo bool) {
	if n == "" {
		return "", false, false
	}
	f, err := strconv.ParseFloat(n.String(), 64)
	if err != nil {
		return "", false, false
	}
	return n, true, f > 0
}

// Motivos de datos incompletos por valores no positivos.
const (
	MotivoCantidadNoPositiva = "cantidad_no_positiva"
	MotivoMontoNoPositivo    = "monto_no_positivo"
)

func idRuta(id *IDArgos) string {
	if id == nil {
		return "{falta}"
	}
	return strconv.FormatInt(int64(*id), 10)
}

// planificar traduce la orden cerrada a las llamadas del contrato §3. Pura (sin E/S).
//
// porTecnico: Argos ≥ 1.6.0 (contrato 0.5.0 §3.3). Con el técnico vinculado, consumo, comodato y
// devolución mandan `tecnico: { id_externo }` (Argos usa la bodega activa del técnico en la empresa
// del servicio) en vez de `bodega_id`; una bodega explícita distinta de la de la orden (la indicó
// despacho) sigue mandando `bodega_id`, que en Argos manda sobre `tecnico`. Sin vínculo o con un
// Argos anterior, todo sigue con `bodega_id`.
//
// cuadrillas: las bodegas de cuadrilla de Argos (0.11.0 §3.9). Un consumo desde una de ellas manda
// `tecnico` y `bodega_id` (la que eligió el técnico en la app) y sale con X-Iris-Actor; Argos valida
// que sea miembro antes de mirar existencias. Nunca se cambia de bodega por cuenta de Iris.
func planificar(o OrdenCerrada, op opcionesPlan) ([]plan, []omision) {
	porTecnico, cuadrillas := op.porTecnico, op.cuadrillas
	orden := string(o.OrdenID)
	sid := idRuta(o.ServicioID)
	var planes []plan
	var omisiones []omision
	var tecnico map[string]any
	if porTecnico && o.Tecnico != nil && strings.TrimSpace(o.Tecnico.IDExterno) != "" {
		tecnico = map[string]any{"id_externo": strings.TrimSpace(o.Tecnico.IDExterno)}
	}
	// explicita: una bodega propia de la línea o el equipo, distinta de la de la orden.
	explicita := func(b *IDArgos) bool { return b != nil && (o.BodegaID == nil || *b != *o.BodegaID) }
	agregar := func(p plan) int {
		planes = append(planes, p)
		return len(planes) - 1
	}
	faltaServicio := func(p plan) plan {
		if o.ServicioID == nil && p.incompleto == "" {
			p.incompleto = "orden.cerrada sin servicio_id"
		}
		return p
	}
	bodegaDe := func(b *IDArgos) *IDArgos {
		if b != nil {
			return b
		}
		return o.BodegaID
	}

	// fechas: fecha y recibido_en_iris (0.12.0 §3.10).
	fechas := func(p map[string]any, fecha, recibido *time.Time) {
		if fecha != nil {
			p["fecha"] = fecha.UTC().Format(time.RFC3339)
		}
		if recibido != nil {
			p["recibido_en_iris"] = recibido.UTC().Format(time.RFC3339)
		}
	}
	// delActor: comodato o devolución que el técnico registró en la app (0.13.1 §3.9): `tecnico` es
	// el I1 de quien lo registró y sale con su X-Iris-Actor; sin él o sin vínculo, incompleto.
	delActor := func(p *plan, e Equipo) {
		p.conTecnico, p.conActor, p.actor = true, true, e.RegistradoPor
		var v string
		if e.RegistradoPor != nil {
			v = strings.TrimSpace(op.vinculos[*e.RegistradoPor])
		}
		if v == "" {
			p.payload["tecnico"] = nil
			p.incompleto = "quien registró el equipo no está vinculado a Argos (I1)"
			return
		}
		p.payload["tecnico"] = map[string]any{"id_externo": v}
	}

	// equipoDe (M-5): quién va en una devolución o un comodato. Lo que registró el técnico en la app
	// sale con tecnico = el I1 de quien lo registró y su X-Iris-Actor (con Argos ≥ 1.6.0; antes, con
	// bodega_id). Lo del panel nunca lleva tecnico ni el técnico de la orden como actor: con Argos
	// ≥ 0.11.0 espera a una persona (la bodega debe ser tipo bodega y aquí no se sabe); antes, la
	// devolución sale con bodega_id y el comodato sin tecnico, como en 0.4.
	equipoDe := func(p *plan, e Equipo, devolucion bool) {
		switch {
		case e.DesdeApp && (op.bodegaApp || op.porTecnico):
			delete(p.payload, "bodega_id")
			delActor(p, e)
		case e.DesdeApp:
		case op.bodegaApp:
			p.esperaPersona = EsperaPersonaEquipoPanel
		case !devolucion:
			delete(p.payload, "bodega_id")
		}
	}

	// noRecuperado: un intento de recuperación (0.12.0) o, con Argos anterior, la pérdida que espera
	// a una persona. Nunca sale como pérdida desde el cierre.
	noRecuperado := func(e Equipo) (int, bool) {
		if e.UnidadID == nil {
			return 0, false
		}
		uid := idRuta(e.UnidadID)
		if !op.equipos {
			return agregar(faltaServicio(plan{
				operacion: OpPerdida, clave: "iris:perdida:" + orden + ":" + uid,
				ruta: "/servicios/" + sid + "/equipos/" + uid + "/perdida", payload: origen(orden), esperaPersona: EsperaPersonaPerdida,
			})), true
		}
		p := plan{operacion: OpNoRecuperado, clave: "iris:no_recuperado:" + orden + ":" + uid,
			ruta: "/servicios/" + sid + "/equipos/" + uid + "/no-recuperado", payload: origen(orden), conActor: true, actor: e.RegistradoPor}
		if e.RegistradoPor == nil {
			p.incompleto = "intento de recuperación sin quien lo registró"
		}
		p.payload["motivo"] = e.Motivo
		if e.FotoRef != "" {
			p.payload["foto_ref"] = e.FotoRef
		}
		if e.Ubicacion != nil {
			p.payload["ubicacion"] = map[string]any{"lat": e.Ubicacion.Lat, "lng": e.Ubicacion.Lng, "precision_m": e.Ubicacion.PrecisionM}
		} else {
			p.incompleto = "intento de recuperación sin ubicación"
		}
		if n := op.notas[e.ID]; n.nota != nil {
			p.payload["nota"] = *n.nota
		}
		fechas(p.payload, e.Fecha, e.RecibidoEnIris)
		return agregar(faltaServicio(p)), true
	}
	// cambios: 0.12.0, una sola llamada por unidad que sale; con Argos anterior, una devolución y una
	// asignación (devuelve si la que sale funciona o no).
	cambios := func() {
		for _, e := range o.Equipos {
			if e.Accion != AccionCambio || e.SaleUnidadID == nil {
				continue
			}
			sale := idRuta(e.SaleUnidadID)
			entra := map[string]any{}
			switch {
			case e.UnidadID != nil:
				entra["unidad_id"] = int64(*e.UnidadID)
			case strings.TrimSpace(e.Serie) != "":
				entra["serie"] = strings.TrimSpace(e.Serie)
			case strings.TrimSpace(e.MAC) != "":
				entra["mac"] = strings.TrimSpace(e.MAC)
			}
			if !op.equipos {
				condicion := "danada"
				if e.DestinoSale == "funciona" {
					condicion = "buena"
				}
				dev := plan{operacion: OpDevolucion, clave: "iris:devolucion:" + orden + ":" + sale,
					ruta:    "/servicios/" + sid + "/equipos/" + sale + "/devolver",
					payload: con(origen(orden), map[string]any{"bodega_id": o.BodegaID, "condicion": condicion})}
				equipoDe(&dev, e, true)
				d := agregar(faltaServicio(dev))
				asig := plan{operacion: OpAsignarComodato, clave: "iris:comodato:" + orden + ":" + claveEquipo(e),
					ruta: "/servicios/" + sid + "/equipos", payload: con(origen(orden), entra), depende: []int{d}}
				equipoDe(&asig, e, false)
				if len(entra) == 0 {
					asig.incompleto = "cambio sin la unidad que entra"
				}
				agregar(faltaServicio(asig))
				continue
			}
			p := plan{operacion: OpCambio, clave: "iris:cambio:" + orden + ":" + sale, ruta: "/servicios/" + sid + "/equipos/cambiar",
				payload: origen(orden), conActor: true, actor: e.RegistradoPor}
			if e.RegistradoPor == nil {
				p.incompleto = "cambio de equipo sin quien lo registró"
			}
			p.payload["sale"] = map[string]any{"unidad_id": int64(*e.SaleUnidadID)}
			p.payload["entra"] = entra
			p.payload["motivo"], p.payload["destino_sale"] = e.Motivo, e.DestinoSale
			n := op.notas[e.ID]
			if e.CobroPropuesto != nil {
				cobro := map[string]any{"motivo": e.CobroPropuesto.Motivo}
				if n.cobro != nil {
					cobro["nota"] = *n.cobro
				}
				p.payload["cobro_propuesto"] = cobro
			}
			p.payload["nota"] = n.nota
			fechas(p.payload, e.Fecha, e.RecibidoEnIris)
			if len(entra) == 0 {
				p.incompleto = "cambio sin la unidad que entra"
			}
			agregar(faltaServicio(p))
		}
	}

	// Equipos retirados (devolución o pérdida): retiro y primer paso del cambio de equipo.
	retirados := func() []int {
		var idx []int
		for _, e := range o.Equipos {
			if e.Accion == AccionNoRecuperado {
				if i, ok := noRecuperado(e); ok {
					idx = append(idx, i)
				}
				continue
			}
			if e.Accion != AccionRetirar {
				continue
			}
			if e.UnidadID == nil {
				idx = append(idx, agregar(faltaServicio(plan{
					operacion: OpDevolucion, clave: "iris:devolucion:" + orden + ":" + claveEquipo(e),
					ruta: "/servicios/" + sid + "/equipos/{falta}/devolver", payload: origen(orden),
					incompleto: "equipo retirado sin unidad_id (serie " + e.Serie + ")",
				})))
				continue
			}
			uid := idRuta(e.UnidadID)
			if strings.EqualFold(e.Condicion, CondicionPerdido) {
				// Desde 0.12.0 Iris nunca declara la pérdida desde el cierre (§3.10, §10.7 d).
				if op.equipos {
					omisiones = append(omisiones, omision{motivo: MotivoPerdidaSinIntento, operaciones: []string{"unidad:" + uid}})
					continue
				}
				idx = append(idx, agregar(faltaServicio(plan{
					operacion: OpPerdida, clave: "iris:perdida:" + orden + ":" + uid,
					ruta: "/servicios/" + sid + "/equipos/" + uid + "/perdida", payload: origen(orden), esperaPersona: EsperaPersonaPerdida,
				})))
				continue
			}
			p := plan{
				operacion: OpDevolucion, clave: "iris:devolucion:" + orden + ":" + uid,
				ruta:    "/servicios/" + sid + "/equipos/" + uid + "/devolver",
				payload: con(origen(orden), map[string]any{"bodega_id": bodegaDe(e.BodegaID), "condicion": e.Condicion}),
			}
			equipoDe(&p, e, true)
			switch {
			case p.incompleto != "" || p.esperaPersona != "":
			case !p.conTecnico && bodegaDe(e.BodegaID) == nil:
				p.incompleto = "devolución sin bodega_id (ni en el equipo ni en la orden)"
			case e.Condicion == "":
				p.incompleto = "devolución sin condicion del equipo"
			}
			idx = append(idx, agregar(faltaServicio(p)))
		}
		return idx
	}
	// Equipos instalados (comodato): instalación y segundo paso del cambio de equipo.
	instalados := func(depende []int) {
		for _, e := range o.Equipos {
			if e.Accion != AccionInstalar {
				continue
			}
			p := plan{
				operacion: OpAsignarComodato, clave: "iris:comodato:" + orden + ":" + claveEquipo(e),
				ruta: "/servicios/" + sid + "/equipos", payload: origen(orden), depende: depende,
			}
			equipoDe(&p, e, false)
			switch {
			case e.UnidadID != nil:
				p.payload["unidad_id"] = int64(*e.UnidadID)
			case strings.TrimSpace(e.Serie) != "":
				p.payload["serie"] = strings.TrimSpace(e.Serie)
			default:
				p.incompleto = "equipo instalado sin unidad_id ni serie"
			}
			agregar(faltaServicio(p))
		}
	}

	if o.Resultado == ResultadoExitosa {
		switch o.Tipo {
		case TipoInstalacion:
			agregar(faltaServicio(plan{
				operacion: OpActivarInstalacion, clave: "iris:instalacion:" + orden,
				ruta: "/servicios/" + sid + "/activar-instalacion", payload: origen(orden),
			}))
			instalados(nil)
		case TipoRetiro:
			retirados()
			if o.RetirarServicio {
				agregar(faltaServicio(plan{
					operacion: OpRetiro, clave: "iris:retiro:" + orden,
					ruta: "/servicios/" + sid + "/retirar", payload: origen(orden),
				}))
			}
		case TipoCambioEquipo:
			// Una devolución y una asignación, en ese orden: cada asignación espera a todas las
			// devoluciones o pérdidas de la orden. Los cambios de 0.12.0 van en una sola llamada.
			instalados(retirados())
			cambios()
		}
	}

	// Materiales usados (cualquier tipo y resultado): un consumo por orden (lineas[], 0.5.0) desde la
	// bodega del técnico o una bodega. Solo consumibles: un comodato va como equipo (se omite).
	consumo := -1
	// Productos del consumo (cualquier modalidad consumible): un cargo de la orden con uno de ellos
	// espera al consumo; si Argos ya lo cobró por modalidad, responde 409 cargo_duplicado (conciliado).
	enConsumo := map[int64]bool{}
	var comodatos []string
	materiales := make([]Material, 0, len(o.Materiales))
	for _, mat := range o.Materiales {
		if mat.Modalidad == argos.ModalidadComodato {
			comodatos = append(comodatos, "producto:"+idRuta(mat.ProductoID))
			continue
		}
		materiales = append(materiales, mat)
	}
	if len(comodatos) > 0 {
		omisiones = append(omisiones, omision{motivo: MotivoMaterialComodato, operaciones: comodatos})
	}
	if len(materiales) > 0 {
		p := plan{operacion: OpConsumo, clave: "iris:consumo:" + orden, ruta: "/inventario/consumos"}
		porBodega := tecnico == nil
		app, panel, sinActor, panelEspecial := false, false, false, false
		var actores []int64
		for _, mat := range materiales {
			b := bodegaDe(mat.BodegaID)
			if mat.DesdeApp {
				app = true
				switch {
				case mat.RegistradoPor == nil:
					sinActor = true
				case !slices.Contains(actores, *mat.RegistradoPor):
					actores = append(actores, *mat.RegistradoPor)
				}
				continue
			}
			panel = true
			// M-4: lista de permitidas. Una bodega de cuadrilla, personal o de tipo desconocido (no se
			// leyó de Argos al registrarlo) no sale desde el panel.
			if b != nil && (cuadrillas[int64(*b)] || op.personales[int64(*b)] || mat.BodegaTipo == nil || *mat.BodegaTipo != "bodega") {
				panelEspecial = true
			}
			if !op.bodegaApp && explicita(mat.BodegaID) {
				porBodega = true // Argos < 0.11.0: despacho indicó otra bodega, manda bodega_id
			}
		}
		// Argos ≥ 0.11.0 (§3.9; 0.13.1 §10.10): lo de la app sale con tecnico (el I1 de quien lo
		// registró) + bodega_id + su X-Iris-Actor; lo de despacho en el panel, solo con bodega_id
		// de una bodega tipo bodega. Nunca un consumo a nombre de otro.
		var deApp map[string]any
		conApp := op.bodegaApp && app
		if op.bodegaApp {
			porBodega = true
		}
		switch {
		case op.bodegaApp && panelEspecial:
			p.incompleto = "despacho indicó una bodega de cuadrilla, personal o de tipo desconocido (desde el panel solo bodegas tipo bodega)"
		case conApp && panel:
			p.incompleto = "materiales de la app y del panel en una orden (un consumo por orden)"
		case conApp && sinActor:
			p.incompleto = "material de la app sin quien lo registró"
		case conApp && len(actores) > 1:
			p.incompleto = "materiales registrados por varios técnicos en una orden (un consumo por orden, un X-Iris-Actor)"
		case conApp:
			p.actor = &actores[0]
			if v := strings.TrimSpace(op.vinculos[actores[0]]); v != "" {
				deApp = map[string]any{"id_externo": v}
			} else {
				p.incompleto = "quien registró el material no está vinculado a Argos (I1)"
			}
		}
		var bodega *IDArgos
		lineas := make([]map[string]any, 0, len(materiales))
		for _, mat := range materiales {
			b := bodegaDe(mat.BodegaID)
			cant, ok, positivo := numero(mat.Cantidad)
			switch {
			case porBodega && b == nil:
				p.incompleto = "material sin bodega_id (ni en la línea ni en la orden) y sin técnico vinculado en Argos ≥ 1.6"
			case porBodega && bodega != nil && *bodega != *b:
				p.incompleto = "materiales de varias bodegas en una orden (un consumo por orden)"
			case mat.ProductoID == nil:
				p.incompleto = "material sin producto_id"
			case !ok:
				p.incompleto = "material sin cantidad numérica"
			case !positivo:
				p.incompleto = MotivoCantidadNoPositiva
			}
			if b != nil && bodega == nil {
				bodega = b
			}
			if mat.ProductoID != nil {
				enConsumo[int64(*mat.ProductoID)] = true
			}
			linea := map[string]any{"producto_id": mat.ProductoID, "cantidad": cant}
			lineas = append(lineas, linea)
		}
		p.payload = con(origen(orden), map[string]any{"lineas": lineas, "servicio_id": o.ServicioID})
		if op.equipos {
			// 0.12.0 §3.3: la fecha más reciente de los materiales y cuándo los aceptó Iris.
			var fecha, recibido *time.Time
			for _, mat := range materiales {
				if mat.Fecha != nil && (fecha == nil || mat.Fecha.After(*fecha)) {
					fecha = mat.Fecha
				}
				if mat.RecibidoEnIris != nil && (recibido == nil || mat.RecibidoEnIris.After(*recibido)) {
					recibido = mat.RecibidoEnIris
				}
			}
			fechas(p.payload, fecha, recibido)
		}
		switch {
		case conApp:
			p.payload["bodega_id"] = bodega
			p.payload["tecnico"], p.conTecnico, p.conActor = deApp, true, true
		case porBodega:
			p.payload["bodega_id"] = bodega
		default:
			p.payload["tecnico"], p.conTecnico = tecnico, true
		}
		consumo = agregar(faltaServicio(p))
	}

	// Cargo único si la orden lo indica.
	if o.Cargo != nil {
		p := plan{operacion: OpCargo, clave: "iris:cargo:" + orden, ruta: "/facturas/cargo-unico",
			payload: con(origen(orden), map[string]any{"cliente_id": o.ClienteID, "servicio_id": o.ServicioID})}
		if o.Cargo.ProductoID != nil {
			p.payload["producto_id"] = o.Cargo.ProductoID
			// Mismo producto en el consumo: el cargo va después del consumo (nunca se descarta: el consumo
			// puede fallar o el producto cambiar de modalidad); si Argos ya lo cobró por modalidad,
			// responde 409 cargo_duplicado y se concilia como hecho (§3.3).
			if consumo >= 0 && enConsumo[int64(*o.Cargo.ProductoID)] {
				p.depende = []int{consumo}
			}
		}
		monto, montoOK, montoPositivo := numero(o.Cargo.Monto)
		if montoOK {
			p.payload["monto"] = monto
		}
		if d := strings.TrimSpace(o.Cargo.Descripcion); d != "" {
			p.payload["descripcion"] = d
		}
		switch {
		case o.ClienteID == nil:
			p.incompleto = "cargo sin cliente_id"
		case o.Cargo.Monto != "" && !montoOK:
			p.incompleto = "cargo con monto no numérico"
		case montoOK && !montoPositivo:
			p.incompleto = MotivoMontoNoPositivo
		case o.Cargo.ProductoID == nil && p.payload["monto"] == nil:
			p.incompleto = "cargo sin producto_id ni monto"
		}
		agregar(p)
	}
	return planes, omisiones
}

// notasDeEquipos lee de la orden las notas de sus cambios e intentos (no viajan en el evento).
func notasDeEquipos(ctx context.Context, q *pendientesbd.Queries, o OrdenCerrada) (map[string]notasEquipo, error) {
	salida := map[string]notasEquipo{}
	ids := []uuid.UUID{}
	for _, e := range o.Equipos {
		if e.Accion != AccionCambio && e.Accion != AccionNoRecuperado {
			continue
		}
		if id, err := uuid.Parse(e.ID); err == nil {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return salida, nil
	}
	filas, err := q.NotasDeEquipos(ctx, ids)
	if err != nil {
		return nil, err
	}
	for _, f := range filas {
		salida[f.ID.String()] = notasEquipo{nota: f.Nota, cobro: f.CobroNota}
	}
	return salida, nil
}

// bodegasDeCuadrilla: las bodegas de los materiales de la orden que son de una cuadrilla, y las que
// son la personal de un técnico.
func bodegasDeCuadrilla(ctx context.Context, q *pendientesbd.Queries, o OrdenCerrada) (map[int64]bool, map[int64]bool, error) {
	ids := []int64{}
	if o.BodegaID != nil {
		ids = append(ids, int64(*o.BodegaID))
	}
	for _, mat := range o.Materiales {
		if mat.BodegaID != nil {
			ids = append(ids, int64(*mat.BodegaID))
		}
	}
	salida, personales := map[int64]bool{}, map[int64]bool{}
	if len(ids) == 0 {
		return salida, personales, nil
	}
	filas, err := q.BodegasDeCuadrilla(ctx, ids)
	if err != nil {
		return nil, nil, err
	}
	for _, b := range filas {
		salida[b] = true
	}
	filas, err = q.BodegasPersonales(ctx, ids)
	if err != nil {
		return nil, nil, err
	}
	for _, b := range filas {
		personales[b] = true
	}
	return salida, personales, nil
}

// vinculosDeActores: el vínculo I1 de quienes registraron materiales de la orden desde la app.
func vinculosDeActores(ctx context.Context, q *pendientesbd.Queries, o OrdenCerrada) (map[int64]string, error) {
	usuarios := []int64{}
	for _, mat := range o.Materiales {
		if mat.RegistradoPor != nil && !slices.Contains(usuarios, *mat.RegistradoPor) {
			usuarios = append(usuarios, *mat.RegistradoPor)
		}
	}
	for _, e := range o.Equipos {
		if e.RegistradoPor != nil && !slices.Contains(usuarios, *e.RegistradoPor) {
			usuarios = append(usuarios, *e.RegistradoPor)
		}
	}
	salida := map[int64]string{}
	if len(usuarios) == 0 {
		return salida, nil
	}
	filas, err := q.VinculosArgosDeTecnicos(ctx, usuarios)
	if err != nil {
		return nil, err
	}
	for _, f := range filas {
		salida[f.UsuarioID] = f.IDExterno
	}
	return salida, nil
}

// claveEquipo: la serie (contrato: iris:comodato:<orden_id>:<serie>); sin serie, el unidad_id.
// Escapada para que sea una cabecera válida.
func claveEquipo(e Equipo) string {
	if s := strings.TrimSpace(e.Serie); s != "" {
		return url.PathEscape(s)
	}
	if e.UnidadID != nil {
		return strconv.FormatInt(int64(*e.UnidadID), 10)
	}
	return "sin-serie"
}

type eventoOmitido struct {
	OrdenID     string   `json:"orden_id"`
	Motivo      string   `json:"motivo"`
	Operaciones []string `json:"operaciones"`
}

func nuevoEncolarCierreOrden(m *Modulo) *auditoria.Caso[OrdenCerrada, ResultadoCierre] {
	return auditoria.Nuevo(auditoria.Caso[OrdenCerrada, ResultadoCierre]{
		Accion: "integraciones.encolar_cierre_orden", Acceso: auditoria.Autenticado,
		AuditarExito: func(r ResultadoCierre) bool { return len(r.Pendientes) > 0 || r.Omitido },
		Logica: func(ctx context.Context, t *auditoria.Tx, o OrdenCerrada) (ResultadoCierre, error) {
			orden := string(o.OrdenID)
			res := ResultadoCierre{OrdenID: orden, Pendientes: []int64{}}
			// Argos ≥ 1.6.0 entiende `tecnico` (contrato 0.5.0 §3.3); si no, bodega_id.
			porTecnico := false
			if m.cfg.Argos != nil && o.Tecnico != nil {
				var err error
				if porTecnico, err = m.cfg.Argos.InventarioPorTecnico(ctx, t); err != nil {
					return res, err
				}
			}
			cuadrillas, personales, err := bodegasDeCuadrilla(ctx, pendientesbd.New(t), o)
			if err != nil {
				return res, err
			}
			vinculos, err := vinculosDeActores(ctx, pendientesbd.New(t), o)
			if err != nil {
				return res, err
			}
			equipos, bodegaApp := false, false
			if m.cfg.Argos != nil {
				if equipos, err = m.cfg.Argos.EquiposDelCliente(ctx, t); err != nil {
					return res, err
				}
				if bodegaApp, err = m.cfg.Argos.BodegaDeCuadrilla(ctx, t); err != nil {
					return res, err
				}
			}
			notas, err := notasDeEquipos(ctx, pendientesbd.New(t), o)
			if err != nil {
				return res, err
			}
			planes, omisiones := planificar(o, opcionesPlan{porTecnico: porTecnico, cuadrillas: cuadrillas, personales: personales,
				bodegaApp: bodegaApp, vinculos: vinculos, equipos: equipos, notas: notas})
			if len(planes) == 0 && len(omisiones) == 0 {
				return res, nil
			}
			t.FijarEntidad("orden", orden)
			var empresa int64
			if o.EmpresaID != nil {
				empresa = *o.EmpresaID
			}
			t.FijarEmpresa(empresa)
			registrado := false
			if m.cfg.Argos != nil {
				var err error
				if registrado, err = m.cfg.Argos.HayConectorRegistrado(ctx, t); err != nil {
					return res, err
				}
			}
			if !registrado {
				// Modo sin conector (nunca se conectó Argos): la orden queda cerrada y no se encola
				// nada. Con un conector apagado sí se encola: el worker reintenta hasta que se active.
				ops := make([]string, 0, len(planes))
				for _, p := range planes {
					ops = append(ops, p.clave)
				}
				slog.WarnContext(ctx, "pendientes: orden cerrada sin conector Argos; no se encolan sus acciones",
					"orden", orden, "operaciones", ops)
				res.Omitido = true
				return res, t.Emitir(EventoOmitido, eventoOmitido{OrdenID: orden, Motivo: "sin_conector", Operaciones: ops})
			}
			for _, om := range omisiones {
				slog.WarnContext(ctx, "pendientes: parte de la orden no se manda a Argos", "orden", orden, "motivo", om.motivo, "operaciones", om.operaciones)
				if err := t.Emitir(EventoOmitido, eventoOmitido{OrdenID: orden, Motivo: om.motivo, Operaciones: om.operaciones}); err != nil {
					return res, err
				}
			}
			q := pendientesbd.New(t)
			// Paso «Confirmar titular y plan» (0.10.0 §3.4): la activación y el cobro esperan a todos
			// sus pasos, y el cobro va a nombre del titular final (B21).
			pasos, err := q.PasosTitularDeOrden(ctx, orden)
			if err != nil {
				return res, err
			}
			despuesDelTitular(planes, pasos)
			ids := make([]int64, len(planes))
			ahora := m.ahora()
			var evento pgtype.UUID
			if o.EventoID != uuid.Nil {
				evento = pgtype.UUID{Bytes: o.EventoID, Valid: true}
			}
			nuevos := 0
			var fallidos []int64
			for i, p := range planes {
				depende := make([]int64, 0, len(p.depende)+len(p.externos))
				for _, d := range p.depende {
					depende = append(depende, ids[d])
				}
				depende = append(depende, p.externos...)
				cuerpo, err := json.Marshal(p.payload)
				if err != nil {
					return res, err
				}
				estado := EstadoPendiente
				var ultimo *string
				switch {
				case p.incompleto != "":
					estado, ultimo = EstadoFallidoNegocio, errorf("datos incompletos: %s", p.incompleto)
				case p.esperaPersona != "":
					estado, ultimo = EstadoFallidoNegocio, &p.esperaPersona
				}
				porResolver := p.incompleto == "" && tieneMarcadores(cuerpo)
				var plantilla []byte
				proximo := ahora
				if porResolver {
					if err := validarPlantilla(cuerpo, len(depende)); err != nil {
						return res, err
					}
					plantilla, proximo = cuerpo, NuncaVence
				}
				var empresaID *int64
				if empresa != 0 {
					empresaID = &empresa
				}
				var tecnicoUsuario *int64
				switch {
				case (p.conTecnico || p.conActor) && p.actor != nil:
					tecnicoUsuario = p.actor // quien lo registró (X-Iris-Actor)
				case p.conTecnico && !p.conActor:
					tecnicoUsuario = o.TecnicoID
				}
				f, err := q.InsertarPendiente(ctx, pendientesbd.InsertarPendienteParams{
					EmpresaID: empresaID, Sistema: argos.Sistema, Operacion: p.operacion, IdempotencyKey: p.clave,
					Entidad: "orden", EntidadID: orden, Metodo: "POST", Ruta: p.ruta, Payload: cuerpo, DependeDe: depende,
					DatosIncompletos: p.incompleto != "", Estado: estado, UltimoError: ultimo, ProximoIntento: proximo, EventoID: evento,
					TecnicoUsuarioID: tecnicoUsuario, Plantilla: plantilla, PorResolver: porResolver,
				})
				if errors.Is(err, pgx.ErrNoRows) {
					// Cierre repetido (relay o reapertura): misma Idempotency-Key.
					previo, err := q.PendientePorClave(ctx, p.clave)
					if err != nil {
						return res, err
					}
					ids[i] = previo.ID
					res.Pendientes = append(res.Pendientes, previo.ID)
					corregido, err := m.cierreRepetido(ctx, t, previo, p, cuerpo, depende, ahora, tecnicoUsuario)
					if err != nil {
						return res, err
					}
					if corregido {
						nuevos++
					}
					continue
				}
				if err != nil {
					return res, err
				}
				ids[i] = f.ID
				res.Pendientes = append(res.Pendientes, f.ID)
				nuevos++
				t.RegistrarCambios("pendiente", f.ID, nil, map[string]any{"operacion": f.Operacion, "estado": f.Estado, "idempotency_key": f.IdempotencyKey})
				if f.Estado == EstadoFallidoNegocio {
					if err := t.Emitir(EventoFallido, eventoFallo{eventoPendiente: eventoDe(f), Detalle: *ultimo}); err != nil {
						return res, err
					}
					fallidos = append(fallidos, f.ID)
					continue
				}
				if f.PorResolver {
					if f, err = resolverSiListo(ctx, t, f, ahora); err != nil {
						return res, err
					}
				}
				if len(p.externos) > 0 && f.Estado == EstadoPendiente {
					// Un paso del titular ya rechazado o agotado: la activación (o el cobro) nace bloqueada.
					if err := bloquearSiDependenciaFallida(ctx, t, p.externos); err != nil {
						return res, err
					}
				}
			}
			// Lo que dependa de una acción que nació fallida (datos incompletos) queda bloqueado.
			for _, id := range fallidos {
				if err := bloquearDependientes(ctx, t, id, EstadoFallidoNegocio); err != nil {
					return res, err
				}
			}
			if nuevos == 0 {
				return res, nil
			}
			return res, m.avisarCola(ctx, t.Tx)
		},
	})
}

// Suscribir registra el suscriptor de orden.cerrada en el bus del worker (actor sistema).
// Idempotente: un evento repetido no duplica pendientes. Un payload ilegible o sin orden_id se
// registra en el log y no se reintenta.
func (m *Modulo) Suscribir(bus *integracion.Bus) {
	bus.Suscribir("integracion.pendientes_orden_cerrada", EventoOrdenCerrada, func(ctx context.Context, e integracion.Evento) error {
		var o OrdenCerrada
		if err := json.Unmarshal(e.Payload, &o); err != nil || o.OrdenID == "" {
			slog.ErrorContext(ctx, "pendientes: orden.cerrada ilegible o sin orden_id; no se encola nada", "evento", e.ID, "err", err)
			return nil
		}
		if e.EmpresaID != nil {
			// La empresa del sobre (la que fijó campo al cerrar) manda sobre la del payload.
			o.EmpresaID = e.EmpresaID
		}
		o.EventoID = e.ID
		_, err := m.EncolarCierreOrden.Ejecutar(ctx, o)
		return err
	})
}

// cierreRepetido decide qué hacer cuando la orden se cierra de nuevo y la acción ya existía
// (misma Idempotency-Key):
//
//   - el previo está fallido_negocio con datos_incompletos y el plan nuevo trae los datos → se
//     corrige: payload y ruta nuevos, vuelve a pendiente (auditado, integracion.pendiente_corregido);
//   - mismo payload y ruta → evento repetido, nada que hacer;
//   - otro payload en cualquier otro caso (hecho, en_curso, pendiente, agotado, fallido de
//     Argos, o incompleto que sigue incompleto con otros datos) → no se pisa (la Idempotency-Key
//     pudo viajar ya a Argos con los datos anteriores): se marca el conflicto
//     cierre_repetido_con_otros_datos con el payload nuevo, visible en la lista del panel, para
//     corregirlo a mano en Argos (integracion.pendiente_conflicto, una vez por payload).
//
// Devuelve true si el pendiente volvió a la cola.
func (m *Modulo) cierreRepetido(ctx context.Context, t *auditoria.Tx, previo pendientesbd.IntegracionPendiente, p plan,
	cuerpo []byte, depende []int64, ahora time.Time, actor *int64) (bool, error) {
	// El actor es parte de la huella (A-271-1): el mismo cuerpo con otro X-Iris-Actor no es el mismo.
	mismoActor := (previo.TecnicoUsuarioID == nil && actor == nil) ||
		(previo.TecnicoUsuarioID != nil && actor != nil && *previo.TecnicoUsuarioID == *actor) || !p.conActor
	q := pendientesbd.New(t)
	if previo.Estado == EstadoFallidoNegocio && previo.DatosIncompletos && p.incompleto == "" {
		f, err := q.CorregirPendiente(ctx, pendientesbd.CorregirPendienteParams{Payload: cuerpo, Ruta: p.ruta, DependeDe: depende, Ahora: ahora, ID: previo.ID,
			TecnicoUsuarioID: actor})
		if err != nil {
			return false, err
		}
		t.RegistrarCambios("pendiente", f.ID,
			map[string]any{"estado": previo.Estado, "ruta": previo.Ruta, "payload": string(sinNotas(previo.Payload)), "datos_incompletos": true},
			map[string]any{"estado": f.Estado, "ruta": f.Ruta, "payload": string(sinNotas(f.Payload)), "datos_incompletos": false})
		return true, t.Emitir(EventoCorregido, eventoDe(f))
	}
	if previo.Ruta == p.ruta && (mismoJSON(previo.Payload, cuerpo) || (len(previo.Plantilla) > 0 && mismoJSON(previo.Plantilla, cuerpo))) && mismoActor {
		return false, nil // el mismo cierre (con marcadores: la misma plantilla) y el mismo actor
	}
	// Mismos datos con otra forma de decir la bodega (bodega_id ↔ tecnico, al vincular al técnico o
	// actualizar Argos a 1.6.0): lo ya encolado se queda como está (su huella de idempotencia).
	if previo.Ruta == p.ruta && mismoJSON(sinBodega(previo.Payload), sinBodega(cuerpo)) && mismoActor {
		return false, nil
	}
	if previo.Conflicto != nil && mismoJSON(previo.ConflictoPayload, cuerpo) {
		return false, nil // ya avisado con estos datos
	}
	motivo := ConflictoCierreRepetido
	f, err := q.MarcarConflicto(ctx, pendientesbd.MarcarConflictoParams{Conflicto: &motivo, ConflictoPayload: cuerpo, Ahora: ahora, ID: previo.ID})
	if err != nil {
		return false, err
	}
	t.RegistrarCambios("pendiente", f.ID, map[string]any{"conflicto": previo.Conflicto}, map[string]any{"conflicto": motivo})
	slog.WarnContext(ctx, "pendientes: la orden se cerró de nuevo con otros datos; corregir a mano en Argos",
		"pendiente", f.ID, "operacion", f.Operacion, "estado", f.Estado)
	// Las notas del técnico nunca van en eventos ni en la auditoría (B-271-5).
	var nuevo, anterior map[string]any
	_ = json.Unmarshal(sinNotas(cuerpo), &nuevo)
	_ = json.Unmarshal(sinNotas(previo.Payload), &anterior)
	return false, t.Emitir(EventoConflicto, eventoConflicto{eventoPendiente: eventoDe(f), Estado: f.Estado, Motivo: motivo,
		PayloadAnterior: anterior, PayloadNuevo: nuevo})
}

// ConflictoCierreRepetido: la orden se volvió a cerrar con otros datos para una acción que no se
// puede pisar.
const ConflictoCierreRepetido = "cierre_repetido_con_otros_datos"

type eventoConflicto struct {
	eventoPendiente
	Estado          string         `json:"estado"`
	Motivo          string         `json:"motivo"`
	PayloadAnterior map[string]any `json:"payload_anterior"`
	PayloadNuevo    map[string]any `json:"payload_nuevo"`
}

func mismoJSON(a, b []byte) bool {
	var x, y any
	if json.Unmarshal(a, &x) != nil || json.Unmarshal(b, &y) != nil {
		return false
	}
	return reflect.DeepEqual(x, y)
}

// sinNotas: el cuerpo sin la nota del técnico ni la del cobro propuesto (§10.7 i: nunca en eventos
// ni en la auditoría). Si no es un objeto JSON, se devuelve tal cual.
func sinNotas(cuerpo []byte) []byte {
	var m map[string]any
	if json.Unmarshal(cuerpo, &m) != nil {
		return cuerpo
	}
	_, hay := m["nota"]
	cobro, _ := m["cobro_propuesto"].(map[string]any)
	_, hayCobro := cobro["nota"]
	if !hay && !hayCobro {
		return cuerpo
	}
	delete(m, "nota")
	if hayCobro {
		delete(cobro, "nota")
	}
	b, err := json.Marshal(m)
	if err != nil {
		return cuerpo
	}
	return b
}

// sinBodega: el cuerpo sin bodega_id ni tecnico (para comparar un cierre repetido).
func sinBodega(cuerpo []byte) []byte {
	var m map[string]any
	if json.Unmarshal(cuerpo, &m) != nil {
		return cuerpo
	}
	delete(m, "bodega_id")
	delete(m, "tecnico")
	b, err := json.Marshal(m)
	if err != nil {
		return cuerpo
	}
	return b
}

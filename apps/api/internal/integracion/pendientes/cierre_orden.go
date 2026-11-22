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
}

// TecnicoArgos es `tecnico` en orden.cerrada (contrato 0.5.0 §3.3): el vínculo de la ficha.
type TecnicoArgos struct {
	IDExterno string `json:"id_externo"`
}

// Equipo instalado o retirado en la orden.
type Equipo struct {
	Accion    string   `json:"accion"` // instalar | retirar
	UnidadID  *IDArgos `json:"unidad_id"`
	Serie     string   `json:"serie"`
	MAC       string   `json:"mac"`
	Condicion string   `json:"condicion"` // al retirar: bueno, danado… o perdido (no se recuperó)
	BodegaID  *IDArgos `json:"bodega_id"` // bodega a la que se devuelve (si no, la de la orden)
	// Aprovisionado (0.9.0 §12.2): es el equipo de un intento de activación vivo; Argos lo asigna en
	// comodato al aprovisionar, así que el cierre no manda su POST /servicios/:id/equipos.
	Aprovisionado bool `json:"aprovisionado"`
}

// ActivacionCerrada es orden.cerrada.activacion (0.9.0 §12.2).
type ActivacionCerrada struct {
	Modo                       string   `json:"modo"`
	Estado                     string   `json:"estado"`
	AprovisionamientoID        *IDArgos `json:"aprovisionamiento_id"`
	CerradaSinActivacionMotivo *string  `json:"cerrada_sin_activacion_motivo"`
}

// porAprovisionar: la activación de esta orden va (o fue) por aprovisionar, no por
// activar-instalacion: el modo no es «al cerrar» o hay un intento vivo. Sin el campo (un cierre
// anterior a la 0.9.0), como siempre.
func (a *ActivacionCerrada) porAprovisionar() bool {
	if a == nil {
		return false
	}
	if a.Modo != "" && a.Modo != "al_cerrar" {
		return true
	}
	return a.Estado == "pendiente" || a.Estado == "en_proceso" || a.Estado == "listo"
}

// Cargo único que la orden indica cobrar. Desde la 0.14.3 lleva lineas[] con el cuerpo real de Argos
// (POST /facturas/cargo-unico, igual que A36): { concepto_id, precio_unitario, descripcion? }. La
// forma anterior (producto_id, monto) solo llega en cierres emitidos antes de esta versión y se
// manda como entonces.
type Cargo struct {
	Lineas      []LineaCargo `json:"lineas"`
	ProductoID  *IDArgos     `json:"producto_id"`
	Monto       json.Number  `json:"monto"`
	Descripcion string       `json:"descripcion"`
}

// LineaCargo es una línea del cargo (un concepto de GET /conceptos-cargo y su precio bruto).
type LineaCargo struct {
	ConceptoID     *IDArgos    `json:"concepto_id"`
	PrecioUnitario json.Number `json:"precio_unitario"`
	Descripcion    string      `json:"descripcion"`
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
	// Activacion (0.9.0 §12.2): modo y estado de la activación desde la orden; null en otros tipos.
	Activacion *ActivacionCerrada `json:"activacion"`

	// EventoID del outbox (no viene en el payload).
	EventoID uuid.UUID `json:"-"`
	// conceptoInstalacion: el concepto con codigo_sistema "instalacion" de la empresa, leído de Argos
	// fuera de la transacción (0 = no se pudo leer).
	conceptoInstalacion int64
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
	// conTecnico: el cuerpo lleva `tecnico` (se corrige al reintentar a mano tras vincular).
	conTecnico bool
	// externos: pendientes ya encolados (los pasos del titular, §3.4) que también deben estar
	// hechos antes; van en depende_de después de los de depende.
	externos []int64
	// lineas: las líneas del cargo (0.14.3); se filtra la de instalación al encolar, con la orden
	// bloqueada, y quedan fijas en el cuerpo.
	lineas []map[string]any
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
	// MotivoConsumibleCobro: el cargo (forma anterior a la 0.14.3) es de un producto consumible_cobro
	// del consumo: lo cobra Argos con el consumo (§3.3), no va en el cargo.
	MotivoConsumibleCobro = "consumible_cobro"
	// MotivoCargoSinLineas: el cargo del cierre se quedó sin líneas al quitar la de instalación (un
	// cobro de A36 vigente con ella o cobro_equipo en false): no sale (0.14.3 §3).
	MotivoCargoSinLineas = "cargo_sin_lineas"
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
func planificar(o OrdenCerrada, porTecnico bool) ([]plan, []omision) {
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

	// Equipos retirados (devolución o pérdida): retiro y primer paso del cambio de equipo.
	retirados := func() []int {
		var idx []int
		for _, e := range o.Equipos {
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
				idx = append(idx, agregar(faltaServicio(plan{
					operacion: OpPerdida, clave: "iris:perdida:" + orden + ":" + uid,
					ruta: "/servicios/" + sid + "/equipos/" + uid + "/perdida", payload: origen(orden),
				})))
				continue
			}
			p := plan{
				operacion: OpDevolucion, clave: "iris:devolucion:" + orden + ":" + uid,
				ruta:    "/servicios/" + sid + "/equipos/" + uid + "/devolver",
				payload: con(origen(orden), map[string]any{"bodega_id": bodegaDe(e.BodegaID), "condicion": e.Condicion}),
			}
			if tecnico != nil && !explicita(e.BodegaID) {
				// 0.5.0: a la bodega activa del técnico en la empresa del servicio.
				delete(p.payload, "bodega_id")
				p.payload["tecnico"], p.conTecnico = tecnico, true
			}
			switch {
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
			if e.Accion != AccionInstalar || e.Aprovisionado {
				// El equipo aprovisionado ya lo asignó Argos en comodato (0.9.0 §12.2): no se repite.
				continue
			}
			p := plan{
				operacion: OpAsignarComodato, clave: "iris:comodato:" + orden + ":" + claveEquipo(e),
				ruta: "/servicios/" + sid + "/equipos", payload: origen(orden), depende: depende,
			}
			if tecnico != nil {
				// 0.5.0: la unidad sale de la bodega del técnico (409 unidad_en_otra_bodega si no).
				p.payload["tecnico"], p.conTecnico = tecnico, true
			}
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
			// Con la activación desde la orden (0.9.0 §12.2) el cierre no manda activar-instalacion:
			// Argos activa al aprovisionar (o lo hará un agente desde el panel).
			if !o.Activacion.porAprovisionar() {
				agregar(faltaServicio(plan{
					operacion: OpActivarInstalacion, clave: "iris:instalacion:" + orden,
					ruta: "/servicios/" + sid + "/activar-instalacion", payload: origen(orden),
				}))
			}
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
			// devoluciones o pérdidas de la orden.
			instalados(retirados())
		}
	}

	// Materiales usados (cualquier tipo y resultado): un consumo por orden (lineas[], 0.5.0) desde la
	// bodega del técnico o una bodega. Solo consumibles: un comodato va como equipo (se omite).
	consumo := -1
	// Productos del consumo (cualquier modalidad consumible): un cargo de la orden con uno de ellos
	// espera al consumo; si Argos ya lo cobró por modalidad, responde 409 cargo_duplicado (conciliado).
	enConsumo := map[int64]bool{}
	// cobrados: productos consumible_cobro del consumo (Argos los cobra al consumir, §3.3).
	cobrados := map[int64]bool{}
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
		for _, mat := range materiales {
			if explicita(mat.BodegaID) {
				porBodega = true // despacho indicó otra bodega: manda bodega_id
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
				if mat.Modalidad == argos.ModalidadConsumibleCobro {
					cobrados[int64(*mat.ProductoID)] = true
				}
			}
			linea := map[string]any{"producto_id": mat.ProductoID, "cantidad": cant}
			lineas = append(lineas, linea)
		}
		p.payload = con(origen(orden), map[string]any{"lineas": lineas, "servicio_id": o.ServicioID})
		if porBodega {
			p.payload["bodega_id"] = bodega
		} else {
			p.payload["tecnico"], p.conTecnico = tecnico, true
		}
		consumo = agregar(faltaServicio(p))
	}

	// Cargo único si la orden lo indica.
	if o.Cargo != nil && len(o.Cargo.Lineas) > 0 {
		// 0.14.3: lineas[] (cuerpo real de Argos). La línea de instalación se decide al encolar.
		p := plan{operacion: OpCargo, clave: "iris:cargo:" + orden, ruta: "/facturas/cargo-unico",
			payload: con(origen(orden), map[string]any{"cliente_id": o.ClienteID, "servicio_id": o.ServicioID})}
		for _, l := range o.Cargo.Lineas {
			precio, ok, positivo := numero(l.PrecioUnitario)
			switch {
			case l.ConceptoID == nil:
				p.incompleto = "línea del cargo sin concepto_id"
			case !ok:
				p.incompleto = "línea del cargo sin precio_unitario numérico"
			case !positivo:
				p.incompleto = MotivoMontoNoPositivo
			}
			// precio_unitario: string decimal (bruto con IVA), como en A36.
			linea := map[string]any{"concepto_id": l.ConceptoID, "precio_unitario": argos.PrecioCanonico(precio.String())}
			if d := strings.TrimSpace(l.Descripcion); d != "" {
				linea["descripcion"] = d
			}
			p.lineas = append(p.lineas, linea)
		}
		p.payload["lineas"] = p.lineas
		if o.ClienteID == nil && p.incompleto == "" {
			p.incompleto = "cargo sin cliente_id"
		}
		agregar(p)
	} else if o.Cargo != nil {
		// Forma anterior a la 0.14.3 (producto_id, monto).
		p := plan{operacion: OpCargo, clave: "iris:cargo:" + orden, ruta: "/facturas/cargo-unico",
			payload: con(origen(orden), map[string]any{"cliente_id": o.ClienteID, "servicio_id": o.ServicioID})}
		if o.Cargo.ProductoID != nil && cobrados[int64(*o.Cargo.ProductoID)] {
			// Un consumible_cobro lo cobra Argos con el consumo (§3.3): nunca va en el cargo.
			omisiones = append(omisiones, omision{motivo: MotivoConsumibleCobro, operaciones: []string{p.clave}})
			return planes, omisiones
		}
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
			planes, omisiones := planificar(o, porTecnico)
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
			var err error
			if m.cfg.Argos != nil {
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
			// Cobro de la instalación (0.14.3 §3): con la orden bloqueada, el cargo sale sin la línea de
			// instalación si hay un cobro de A36 vigente con ella o la empresa no tiene cobro_equipo.
			if planes, omisiones, err = m.lineaInstalacion(ctx, t, o, planes, omisiones); err != nil {
				return res, err
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
				if p.incompleto != "" {
					estado, ultimo = EstadoFallidoNegocio, errorf("datos incompletos: %s", p.incompleto)
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
				if p.conTecnico {
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
					corregido, err := m.cierreRepetido(ctx, t, previo, p, cuerpo, depende, ahora)
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
		if o.Cargo != nil && len(o.Cargo.Lineas) > 0 && m.cfg.Argos != nil && o.EmpresaID != nil {
			// El concepto de instalación de la empresa (codigo_sistema, 0.14.3 §4.3), fuera de la
			// transacción. Si no se puede leer, el cargo igual se decide con los cobros de A36 vigentes.
			if id, err := m.cfg.Argos.ConceptoInstalacion(ctx, *o.EmpresaID); err == nil {
				o.conceptoInstalacion = id
			} else {
				slog.WarnContext(ctx, "pendientes: no se pudo leer el concepto de instalación de Argos", "orden", o.OrdenID, "err", err)
			}
		}
		_, err := m.EncolarCierreOrden.Ejecutar(ctx, o)
		return err
	})
}

// lineaInstalacion quita del cargo del cierre la línea de instalación (el concepto con codigo_sistema
// "instalacion", o el de un cobro de A36 vigente) si la orden ya tiene un cobro de A36 vigente con
// ella o la empresa no tiene cobro_equipo (0.14.3 §3). Corre con la orden bloqueada (FOR UPDATE, igual
// que el registro de A36): dos cargos vigentes con la línea de instalación nunca conviven. Si no queda
// ninguna línea, el cargo no sale (pendiente_omitido cargo_sin_lineas). El cuerpo queda fijo.
func (m *Modulo) lineaInstalacion(ctx context.Context, t *auditoria.Tx, o OrdenCerrada, planes []plan, omisiones []omision) ([]plan, []omision, error) {
	idx := -1
	for i, p := range planes {
		if p.operacion == OpCargo && p.lineas != nil {
			idx = i
		}
	}
	if idx < 0 {
		return planes, omisiones, nil
	}
	q := pendientesbd.New(t)
	instalacion := map[int64]bool{}
	if o.conceptoInstalacion > 0 {
		instalacion[o.conceptoInstalacion] = true
	}
	vigente := false
	if orden, err := strconv.ParseInt(string(o.OrdenID), 10, 64); err == nil {
		if _, err := q.BloquearOrdenCampo(ctx, orden); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return nil, nil, err
		}
		conceptos, err := q.ConceptosInstalacionVigentes(ctx, orden)
		if err != nil {
			return nil, nil, err
		}
		for _, c := range conceptos {
			instalacion[c], vigente = true, true
		}
	}
	cobro := true
	if m.cfg.Argos != nil {
		var err error
		if cobro, err = m.cfg.Argos.Aprovisionamiento(ctx, t, o.EmpresaID, argos.Funcion(argos.FuncionCobroEquipo)); err != nil {
			return nil, nil, err
		}
	}
	if cobro && !vigente {
		return planes, omisiones, nil
	}
	p := &planes[idx]
	quedan := SinLineaInstalacion(p.lineas, instalacion)
	if len(quedan) == len(p.lineas) {
		return planes, omisiones, nil
	}
	motivo := "cobro_equipo_apagado"
	if vigente {
		motivo = "cobro_a36_vigente"
	}
	t.Anotar(map[string]any{"cargo_sin_instalacion": motivo})
	if len(quedan) == 0 {
		omisiones = append(omisiones, omision{motivo: MotivoCargoSinLineas, operaciones: []string{p.clave}})
		return append(planes[:idx], planes[idx+1:]...), omisiones, nil
	}
	p.lineas = quedan
	p.payload["lineas"] = quedan
	return planes, omisiones, nil
}

// SinLineaInstalacion: las líneas cuyo concepto_id no está en instalacion.
func SinLineaInstalacion(lineas []map[string]any, instalacion map[int64]bool) []map[string]any {
	quedan := make([]map[string]any, 0, len(lineas))
	for _, l := range lineas {
		if c, ok := conceptoDe(l["concepto_id"]); ok && instalacion[c] {
			continue
		}
		quedan = append(quedan, l)
	}
	return quedan
}

// conceptoDe lee un concepto_id de una línea (del plan o de un cuerpo ya guardado).
func conceptoDe(v any) (int64, bool) {
	switch x := v.(type) {
	case *IDArgos:
		if x == nil {
			return 0, false
		}
		return int64(*x), true
	case IDArgos:
		return int64(x), true
	case float64:
		return int64(x), x == float64(int64(x))
	case json.Number:
		n, err := x.Int64()
		return n, err == nil
	case int64:
		return x, true
	}
	return 0, false
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
	cuerpo []byte, depende []int64, ahora time.Time) (bool, error) {
	q := pendientesbd.New(t)
	if previo.Estado == EstadoFallidoNegocio && previo.DatosIncompletos && p.incompleto == "" {
		f, err := q.CorregirPendiente(ctx, pendientesbd.CorregirPendienteParams{Payload: cuerpo, Ruta: p.ruta, DependeDe: depende, Ahora: ahora, ID: previo.ID})
		if err != nil {
			return false, err
		}
		t.RegistrarCambios("pendiente", f.ID,
			map[string]any{"estado": previo.Estado, "ruta": previo.Ruta, "payload": string(previo.Payload), "datos_incompletos": true},
			map[string]any{"estado": f.Estado, "ruta": f.Ruta, "payload": string(f.Payload), "datos_incompletos": false})
		return true, t.Emitir(EventoCorregido, eventoDe(f))
	}
	if previo.Ruta == p.ruta && (mismoJSON(previo.Payload, cuerpo) || (len(previo.Plantilla) > 0 && mismoJSON(previo.Plantilla, cuerpo))) {
		return false, nil // el mismo cierre (con marcadores: la misma plantilla)
	}
	// Mismos datos con otra forma de decir la bodega (bodega_id ↔ tecnico, al vincular al técnico o
	// actualizar Argos a 1.6.0): lo ya encolado se queda como está (su huella de idempotencia).
	if previo.Ruta == p.ruta && mismoJSON(sinBodega(previo.Payload), sinBodega(cuerpo)) {
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
	var nuevo, anterior map[string]any
	_ = json.Unmarshal(cuerpo, &nuevo)
	_ = json.Unmarshal(previo.Payload, &anterior)
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

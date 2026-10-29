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
}

// Equipo instalado o retirado en la orden.
type Equipo struct {
	Accion    string   `json:"accion"` // instalar | retirar
	UnidadID  *IDArgos `json:"unidad_id"`
	Serie     string   `json:"serie"`
	MAC       string   `json:"mac"`
	Condicion string   `json:"condicion"` // al retirar: bueno, danado… o perdido (no se recuperó)
	BodegaID  *IDArgos `json:"bodega_id"` // bodega a la que se devuelve (si no, la de la orden)
}

// Cargo único que la orden indica cobrar.
type Cargo struct {
	ProductoID  *IDArgos    `json:"producto_id"`
	Monto       json.Number `json:"monto"`
	Descripcion string      `json:"descripcion"`
}

// OrdenCerrada es el payload de orden.cerrada que necesita esta cola.
type OrdenCerrada struct {
	OrdenID         IDTexto    `json:"orden_id"`
	Tipo            string     `json:"tipo"`
	Resultado       string     `json:"resultado"`
	EmpresaID       *int64     `json:"empresa_id"`
	ClienteID       *IDArgos   `json:"cliente_id"`
	ServicioID      *IDArgos   `json:"servicio_id"`
	TecnicoID       *int64     `json:"tecnico_id"`
	BodegaID        *IDArgos   `json:"bodega_id"`
	RetirarServicio bool       `json:"retirar_servicio"`
	Materiales      []Material `json:"materiales"`
	Equipos         []Equipo   `json:"equipos"`
	Cargo           *Cargo     `json:"cargo"`

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
}

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
func planificar(o OrdenCerrada) []plan {
	orden := string(o.OrdenID)
	sid := idRuta(o.ServicioID)
	var planes []plan
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
			switch {
			case bodegaDe(e.BodegaID) == nil:
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
			// devoluciones o pérdidas de la orden.
			instalados(retirados())
		}
	}

	// Materiales usados (cualquier tipo y resultado): un consumo por orden desde una bodega.
	if len(o.Materiales) > 0 {
		p := plan{operacion: OpConsumo, clave: "iris:consumo:" + orden, ruta: "/inventario/consumos"}
		var bodega *IDArgos
		lineas := make([]map[string]any, 0, len(o.Materiales))
		for _, mat := range o.Materiales {
			b := bodegaDe(mat.BodegaID)
			cant, ok, positivo := numero(mat.Cantidad)
			switch {
			case b == nil:
				p.incompleto = "material sin bodega_id (ni en la línea ni en la orden)"
			case bodega != nil && *bodega != *b:
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
			linea := map[string]any{"producto_id": mat.ProductoID, "cantidad": cant}
			lineas = append(lineas, linea)
		}
		p.payload = con(origen(orden), map[string]any{"bodega_id": bodega, "lineas": lineas, "servicio_id": o.ServicioID})
		agregar(faltaServicio(p))
	}

	// Cargo único si la orden lo indica.
	if o.Cargo != nil {
		p := plan{operacion: OpCargo, clave: "iris:cargo:" + orden, ruta: "/facturas/cargo-unico",
			payload: con(origen(orden), map[string]any{"cliente_id": o.ClienteID, "servicio_id": o.ServicioID})}
		if o.Cargo.ProductoID != nil {
			p.payload["producto_id"] = o.Cargo.ProductoID
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
	return planes
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
			planes := planificar(o)
			if len(planes) == 0 {
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
			q := pendientesbd.New(t)
			ids := make([]int64, len(planes))
			ahora := m.ahora()
			var evento pgtype.UUID
			if o.EventoID != uuid.Nil {
				evento = pgtype.UUID{Bytes: o.EventoID, Valid: true}
			}
			nuevos := 0
			var fallidos []int64
			for i, p := range planes {
				depende := make([]int64, 0, len(p.depende))
				for _, d := range p.depende {
					depende = append(depende, ids[d])
				}
				cuerpo, err := json.Marshal(p.payload)
				if err != nil {
					return res, err
				}
				estado := EstadoPendiente
				var ultimo *string
				if p.incompleto != "" {
					estado, ultimo = EstadoFallidoNegocio, errorf("datos incompletos: %s", p.incompleto)
				}
				var empresaID *int64
				if empresa != 0 {
					empresaID = &empresa
				}
				f, err := q.InsertarPendiente(ctx, pendientesbd.InsertarPendienteParams{
					EmpresaID: empresaID, Sistema: argos.Sistema, Operacion: p.operacion, IdempotencyKey: p.clave,
					Entidad: "orden", EntidadID: orden, Metodo: "POST", Ruta: p.ruta, Payload: cuerpo, DependeDe: depende,
					DatosIncompletos: p.incompleto != "", Estado: estado, UltimoError: ultimo, ProximoIntento: ahora, EventoID: evento,
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
	if previo.Ruta == p.ruta && mismoJSON(previo.Payload, cuerpo) {
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

package campo

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"math/big"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	campobd "github.com/HCDestroyer/iris/apps/api/internal/campo/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/pendientes"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
)

// Cobro de la instalación desde la orden (contrato argosisp.md 0.9.0 §3 y A36, pedido B15). Un agente
// lo registra con el concepto y el precio de la plantilla (cambiar el precio exige
// ordenes.cobro_editar_monto, con motivo auditado; nunca Q0) y sale en el momento hacia Argos por la
// cola de pendientes (POST /facturas/cargo-unico con lineas[{ concepto_id, precio_unitario }],
// X-Iris-Actor e Idempotency-Key iris:cargo:<orden_id>:<cargo_id>). El cierre ya no manda el cargo de
// instalación de la plantilla. La orden sigue la factura: facturado, pagado o anulado.

// Estados del cobro.
const (
	CobroPendiente = "pendiente"
	CobroFacturado = "facturado"
	CobroPagado    = "pagado"
	CobroAnulado   = "anulado"
	CobroFallido   = "fallido"
	// CobroPorConfirmar: Argos respondió 422 capacidad_no_disponible tras un envío sin respuesta; un
	// supervisor confirma si la factura existe (0.14.3 §3). Cuenta como vigente.
	CobroPorConfirmar = "por_confirmar"
)

// Eventos del cobro (catálogo de CLAUDE.md).
const (
	EventoCobroRegistrado = "orden.cobro_registrado"
	EventoCobroCambiado   = "orden.cobro_cambiado"
)

var rePrecio = regexp.MustCompile(`^[0-9]{1,10}(\.[0-9]{1,2})?$`)

// EntradaRegistrarCobro es la entrada de ordenes.registrar_cobro.
type EntradaRegistrarCobro struct {
	ID             int64   `json:"-"`
	ConceptoID     *int64  `json:"conceptoId,omitempty" minimum:"1" doc:"Concepto de Argos (GET /conceptos-cargo; por defecto «Instalación»). Sin él, el de la plantilla"`
	PrecioUnitario *string `json:"precioUnitario,omitempty" pattern:"^[0-9]{1,10}(\\.[0-9]{1,2})?$" doc:"Bruto con IVA, mayor que 0. Sin él, el de la plantilla; uno distinto exige ordenes.cobro_editar_monto y motivo"`
	MotivoPrecio   *string `json:"motivoPrecio,omitempty" minLength:"10" maxLength:"500" doc:"Obligatorio si el precio no es el de la plantilla"`
	Descripcion    *string `json:"descripcion,omitempty" minLength:"1" maxLength:"200"`

	// conceptoInstalacion: leído de Argos antes de la transacción (0 = desconocido).
	conceptoInstalacion int64
}

// Cobro es el cobro de la instalación de una orden.
type Cobro struct {
	ID             uuid.UUID `json:"id" doc:"cargo_id de la clave iris:cargo:<orden>:<cargo_id>"`
	Estado         string    `json:"estado" enum:"pendiente,facturado,pagado,anulado,fallido,por_confirmar" doc:"por_confirmar: un supervisor confirma en Argos si la factura existe (cuenta como vigente)"`
	ConceptoID     int64     `json:"conceptoId"`
	PrecioUnitario string    `json:"precioUnitario"`
	PrecioEditado  bool      `json:"precioEditado" doc:"El precio no es el de la plantilla (con motivo)"`
	Descripcion    *string   `json:"descripcion" nullable:"true"`
	FacturaID      *int64    `json:"facturaId" nullable:"true"`
	Numero         *string   `json:"numero" nullable:"true"`
	Total          *string   `json:"total" nullable:"true"`
	Moneda         *string   `json:"moneda" nullable:"true"`
	Detalle        *string   `json:"detalle" nullable:"true" doc:"Con fallido: lo que respondió Argos"`
	CreadoEn       time.Time `json:"creadoEn"`
}

func numeroTexto(n pgtype.Numeric) *string {
	if !n.Valid {
		return nil
	}
	f, err := n.Float64Value()
	if err != nil || !f.Valid {
		return nil
	}
	s := strconv.FormatFloat(f.Float64, 'f', 2, 64)
	return &s
}

func aCobro(f campobd.CampoCobro) Cobro {
	c := Cobro{ID: f.ID, Estado: f.Estado, ConceptoID: f.ConceptoID, Descripcion: f.Descripcion, FacturaID: f.FacturaID,
		Numero: f.Numero, Total: numeroTexto(f.Total), Moneda: f.Moneda, Detalle: f.Detalle, CreadoEn: f.CreatedAt}
	if p := numeroTexto(f.PrecioUnitario); p != nil {
		c.PrecioUnitario = *p
	}
	c.PrecioEditado = f.MotivoPrecio != nil
	return c
}

// precioNumerico lee un precio decimal positivo con dos decimales como mucho.
func precioNumerico(s string) (pgtype.Numeric, string, bool) {
	s = strings.TrimSpace(s)
	if !rePrecio.MatchString(s) {
		return pgtype.Numeric{}, "", false
	}
	r, ok := new(big.Rat).SetString(s)
	if !ok || r.Sign() <= 0 {
		return pgtype.Numeric{}, "", false
	}
	texto := r.FloatString(2)
	var n pgtype.Numeric
	if err := n.Scan(texto); err != nil {
		return pgtype.Numeric{}, "", false
	}
	return n, texto, true
}

// RegistradorCobro lee de Argos, fuera de la transacción, el concepto de instalación de la empresa de
// la orden y registra el cobro (ordenes.registrar_cobro).
type RegistradorCobro struct {
	m    *Modulo
	caso *auditoria.Caso[EntradaRegistrarCobro, Cobro]
}

// Ejecutar registra el cobro de la instalación.
func (r *RegistradorCobro) Ejecutar(ctx context.Context, in EntradaRegistrarCobro) (Cobro, error) {
	in.conceptoInstalacion = 0
	if leer := r.m.cfg.Equipos.ConceptoInstalacion; leer != nil && contexto.PermisosDe(ctx).Tiene(string(nucleo.PermOrdenesRegistrarCobro)) {
		if bd := nucleo.BDDe(ctx); bd != nil {
			// Solo con la orden visible para quien registra: si no, no se pregunta nada a Argos.
			if f, err := campobd.New(bd).OrdenPorID(ctx, in.ID); err == nil && f.EmpresaID != nil &&
				verificarAcceso(ctx, nucleo.PermOrdenesRegistrarCobro, f) == nil {
				if id, err := leer(ctx, *f.EmpresaID); err == nil {
					in.conceptoInstalacion = id
				}
			}
		}
	}
	return r.caso.Ejecutar(ctx, in)
}

func (m *Modulo) nuevoRegistrarCobro() *auditoria.Caso[EntradaRegistrarCobro, Cobro] {
	return auditoria.Nuevo(auditoria.Caso[EntradaRegistrarCobro, Cobro]{
		Accion: "ordenes.registrar_cobro", Permiso: nucleo.PermOrdenesRegistrarCobro,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaRegistrarCobro) (Cobro, error) {
			if err := sinNulos(in); err != nil {
				return Cobro{}, err
			}
			q := campobd.New(t)
			f, err := tomar(ctx, t, q, in.ID, nucleo.PermOrdenesRegistrarCobro, nil, nil)
			if err != nil {
				return Cobro{}, err
			}
			a := contexto.ActorDe(ctx)
			if !a.EsUsuario() {
				return Cobro{}, nucleo.Prohibido("El cobro lo registra una persona (X-Iris-Actor)")
			}
			instalacion := in.conceptoInstalacion
			tipo, err := tipoDe(ctx, q, f)
			if err != nil {
				return Cobro{}, err
			}
			if tipo.Codigo != tipoInstalacion {
				return Cobro{}, nucleo.ReglaNegocio("orden_no_es_instalacion", "El cobro de la instalación se registra en una orden de instalación")
			}
			if f.ServicioID == nil || f.ClienteID == nil {
				return Cobro{}, nucleo.ReglaNegocio("orden_sin_servicio", "La orden no tiene el cliente y el servicio de Argos")
			}
			if f.Estado == EstadoCancelada || f.Estado == EstadoFallida {
				return Cobro{}, transicionInvalida("cobrar la instalación de", f.Estado)
			}
			if m.cfg.Equipos.Capacidad == nil {
				return Cobro{}, nucleo.ReglaNegocio("cobro_no_disponible", "Iris no tiene conexión con Argos para cobrar la instalación")
			}
			if _, err := q.CobroVivoDeOrden(ctx, f.ID); err == nil {
				return Cobro{}, nucleo.Conflicto("cobro_registrado", "La instalación ya tiene un cobro (si se anuló en Argos, se puede registrar otro)")
			} else if !errors.Is(err, pgx.ErrNoRows) {
				return Cobro{}, err
			}
			// Concepto y precio de la plantilla de la orden.
			var concepto *int64
			var plantilla pgtype.Numeric
			if f.PlantillaID != nil {
				p, err := q.CobroDePlantilla(ctx, *f.PlantillaID)
				if err != nil && !errors.Is(err, pgx.ErrNoRows) {
					return Cobro{}, err
				}
				concepto, plantilla = p.CobroConceptoID, p.CobroPrecio
			}
			conceptoCambiado := false
			if in.ConceptoID != nil {
				conceptoCambiado = concepto == nil || *concepto != *in.ConceptoID
				concepto = in.ConceptoID
			}
			if concepto == nil {
				return Cobro{}, campoInvalido("conceptoId", "Elige el concepto del cobro (la plantilla no trae uno)", "required")
			}
			precioPlantilla := numeroTexto(plantilla)
			precioTexto := ""
			switch {
			case in.PrecioUnitario != nil:
				precioTexto = *in.PrecioUnitario
			case precioPlantilla != nil:
				precioTexto = *precioPlantilla
			default:
				return Cobro{}, campoInvalido("precioUnitario", "Escribe el precio (la plantilla no trae uno)", "required")
			}
			precio, texto, ok := precioNumerico(precioTexto)
			if !ok {
				// Nunca Q0 (§3): Argos exigiría motivo de bonificación y un permiso que el token no tiene.
				return Cobro{}, campoInvalido("precioUnitario", "El precio debe ser mayor que 0, con dos decimales como mucho", "positivo")
			}
			var motivo *string
			if precioPlantilla == nil || *precioPlantilla != texto || conceptoCambiado {
				if !cubre(ctx, nucleo.PermOrdenesCobroEditarMonto, f) {
					return Cobro{}, fueraDeAlcance("Cambiar el precio o el concepto de la plantilla exige ordenes.cobro_editar_monto")
				}
				motivo = textoLimpio(in.MotivoPrecio)
				if motivo == nil || utf8.RuneCountInString(*motivo) < 10 {
					return Cobro{}, campoInvalido("motivoPrecio", "Escribe por qué cambia el precio o el concepto (10 a 500 caracteres)", "required")
				}
			}
			// La línea de instalación es la del concepto con codigo_sistema "instalacion" (0.14.3 §4.3). Si
			// no se puede leer, cuenta como instalación (lo más estricto).
			esInstalacion := true
			if instalacion > 0 {
				esInstalacion = *concepto == instalacion
			}
			if esInstalacion {
				// Con cobro_equipo en false, Iris no manda la línea de instalación (0.14.3 §3).
				if ok, err := m.cfg.Equipos.Capacidad(ctx, t, f.EmpresaID, argos.Funcion(argos.FuncionCobroEquipo)); err != nil {
					return Cobro{}, err
				} else if !ok {
					return Cobro{}, nucleo.ReglaNegocio(argos.CodigoCapacidadNoDisponible, "El cobro de la instalación no está disponible para esta empresa")
				}
				// El cargo del cierre (iris:cargo:<orden>) vigente con la línea de instalación: otro cobro sería
				// una segunda factura. Solo si Argos lo rechazó (sin «por confirmar») o se resolvió a mano.
				if con, err := pendientes.CargoDelCierreConInstalacion(ctx, t, strconv.FormatInt(f.ID, 10), instalacion); err != nil {
					return Cobro{}, err
				} else if con {
					return Cobro{}, nucleo.Conflicto("cobro_al_cerrar", "El cierre de la orden ya pidió el cobro de la instalación a Argos")
				}
			}
			descripcion := textoLimpio(in.Descripcion)
			id, err := uuid.NewV7()
			if err != nil {
				return Cobro{}, err
			}
			clave := "iris:cargo:" + strconv.FormatInt(f.ID, 10) + ":" + id.String()
			linea := map[string]any{"concepto_id": *concepto, "precio_unitario": texto}
			if descripcion != nil {
				linea["descripcion"] = *descripcion
			}
			cuerpo, err := json.Marshal(map[string]any{"servicio_id": *f.ServicioID, "cliente_id": *f.ClienteID, "lineas": []any{linea},
				"origen_tipo": pendientes.OrigenTipo, "origen_id": strconv.FormatInt(f.ID, 10)})
			if err != nil {
				return Cobro{}, err
			}
			reg, creado, err := pendientes.Encolar(ctx, t, pendientes.Accion{
				EmpresaID: f.EmpresaID, Operacion: pendientes.OpCargo, IdempotencyKey: clave, Entidad: "orden",
				EntidadID: strconv.FormatInt(f.ID, 10), Ruta: "/facturas/cargo-unico", Cuerpo: cuerpo, Desde: m.ahora(),
				Actor: pendientes.ActorUsuario(a.ID),
			}, m.cfg.Titular.Encolar)
			if err != nil {
				return Cobro{}, err
			}
			if !creado {
				return Cobro{}, errors.New("campo: la clave del cobro ya existía")
			}
			c, err := q.CrearCobro(ctx, campobd.CrearCobroParams{ID: id, OrdenID: f.ID, EmpresaID: f.EmpresaID, ServicioID: *f.ServicioID,
				ClienteID: *f.ClienteID, ConceptoID: *concepto, PrecioUnitario: precio, PrecioPlantilla: plantilla, MotivoPrecio: motivo,
				Descripcion: descripcion, RegistradoPor: &a.ID, IdempotencyKey: clave, PendienteID: &reg.ID, EsInstalacion: esInstalacion})
			if esViolacionUnica(err) {
				return Cobro{}, nucleo.Conflicto("cobro_registrado", "La instalación ya tiene un cobro")
			}
			if err != nil {
				return Cobro{}, err
			}
			t.RegistrarCambios("cobro", c.ID, nil, map[string]any{"concepto_id": c.ConceptoID, "precio_unitario": texto,
				"precio_plantilla": precioPlantilla, "motivo_precio": motivo, "pendiente_id": reg.ID})
			if _, err := guardar(ctx, t, q, f, f); err != nil {
				return Cobro{}, err
			}
			return aCobro(c), t.Emitir(EventoCobroRegistrado, map[string]any{
				"orden_id": f.ID, "cargo_id": c.ID, "servicio_id": c.ServicioID, "concepto_id": c.ConceptoID, "precio_unitario": texto,
				"precio_editado": motivo != nil, "pendiente_id": reg.ID,
			})
		},
	})
}

func (m *Modulo) nuevoVerCobros() *auditoria.Caso[EntradaVerOrden, []Cobro] {
	return auditoria.Nuevo(auditoria.Caso[EntradaVerOrden, []Cobro]{
		Accion: "ordenes.ver_cobros", Acceso: auditoria.Autenticado, AuditarExito: soloLecturas[[]Cobro],
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaVerOrden) ([]Cobro, error) {
			q := campobd.New(t)
			f, err := q.OrdenPorID(ctx, in.ID)
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, noEncontrada()
			}
			if err != nil {
				return nil, err
			}
			if err := verificarAcceso(ctx, nucleo.PermOrdenesVer, f); err != nil {
				return nil, err
			}
			filas, err := q.CobrosDeOrden(ctx, f.ID)
			if err != nil {
				return nil, err
			}
			r := make([]Cobro, 0, len(filas))
			for _, c := range filas {
				r = append(r, aCobro(c))
			}
			return r, nil
		},
	})
}

// entradaCobroArgos: lo que se supo del cobro (cola o webhook; actor sistema).
type entradaCobroArgos struct {
	IdempotencyKey *string `json:"idempotency_key,omitempty"`
	OrdenID        int64   `json:"orden_id,omitempty"`
	FacturaID      *int64  `json:"factura_id,omitempty"`
	Estado         string  `json:"estado"`
	Numero         *string `json:"numero,omitempty"`
	Total          *string `json:"total,omitempty"`
	Moneda         *string `json:"moneda,omitempty"`
	Detalle        *string `json:"detalle,omitempty"`
}

// transicionCobro: lo que la factura puede hacer con el cobro.
func transicionCobro(de, a string) bool {
	switch a {
	case CobroFacturado:
		return de == CobroPendiente || de == CobroPagado || de == CobroFallido || de == CobroPorConfirmar
	case CobroPagado:
		return de == CobroPendiente || de == CobroFacturado
	case CobroAnulado:
		return de == CobroPendiente || de == CobroFacturado || de == CobroPagado
	case CobroFallido:
		return de == CobroPendiente || de == CobroPorConfirmar
	case CobroPorConfirmar:
		return de == CobroPendiente
	}
	return false
}

func (m *Modulo) nuevoSeguirCobro() *auditoria.Caso[entradaCobroArgos, *Cobro] {
	return auditoria.Nuevo(auditoria.Caso[entradaCobroArgos, *Cobro]{
		Accion: "ordenes.seguir_cobro", Acceso: auditoria.Autenticado,
		AuditarExito: func(c *Cobro) bool { return c != nil },
		Logica: func(ctx context.Context, t *auditoria.Tx, in entradaCobroArgos) (*Cobro, error) {
			q := campobd.New(t)
			var c campobd.CampoCobro
			var err error
			switch {
			case in.IdempotencyKey != nil:
				c, err = q.CobroPorClave(ctx, *in.IdempotencyKey)
			case in.FacturaID != nil && in.OrdenID > 0:
				c, err = q.CobroPorFactura(ctx, campobd.CobroPorFacturaParams{FacturaID: *in.FacturaID, OrdenID: in.OrdenID})
			default:
				return nil, nil
			}
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, nil
			}
			if err != nil {
				return nil, err
			}
			t.FijarEntidad("orden", c.OrdenID)
			t.FijarEmpresa(deref(c.EmpresaID))
			f, err := q.OrdenPorIDParaActualizar(ctx, c.OrdenID)
			if err != nil {
				return nil, err
			}
			antes, err := q.CobroParaActualizar(ctx, c.ID)
			if err != nil {
				return nil, err
			}
			p := campobd.GuardarCobroParams{ID: antes.ID, Estado: antes.Estado, FacturaID: antes.FacturaID, Numero: antes.Numero,
				Total: antes.Total, Moneda: antes.Moneda, Detalle: antes.Detalle}
			if in.FacturaID != nil {
				p.FacturaID = in.FacturaID
			}
			if in.Numero != nil {
				p.Numero = in.Numero
			}
			if in.Total != nil {
				var n pgtype.Numeric
				if n.Scan(*in.Total) == nil {
					p.Total = n
				}
			}
			if in.Moneda != nil {
				p.Moneda = in.Moneda
			}
			revive := antes.Estado == CobroFallido && (in.Estado == CobroFacturado || in.Estado == CobroPagado)
			if revive && in.Estado != antes.Estado {
				otro, err := m.otroCobroVigente(ctx, t, q, antes)
				if err != nil {
					return nil, err
				}
				if otro {
					// 0.14.3 §3: no revive en silencio con otro cobro de instalación vigente; se avisa.
					slog.ErrorContext(ctx, "campo: Argos facturó un cobro fallido y la orden ya tiene otro vigente con la instalación; revisar (posible cobro doble)",
						"orden", antes.OrdenID, "cargo", antes.ID)
					t.Anotar(map[string]any{"revivir_rechazado": true})
					situacion, motivo := "revivir_rechazado", "Argos facturó un cobro que estaba fallido y la orden ya tiene otro vigente: revisa un posible cobro doble"
					if err := historial(ctx, t, q, antes.OrdenID, "cobro", &antes.Estado, &situacion, &motivo,
						map[string]any{"cargo_id": antes.ID.String(), "factura_id": in.FacturaID}, m.ahora()); err != nil {
						return nil, err
					}
					return nil, nil
				}
			}
			if in.Estado != antes.Estado && transicionCobro(antes.Estado, in.Estado) {
				p.Estado = in.Estado
				if in.Estado == CobroFallido || in.Estado == CobroPorConfirmar {
					p.Detalle = recortar(textoLimpio(in.Detalle), 300)
				}
			}
			d, err := q.GuardarCobro(ctx, p)
			if esViolacionUnica(err) {
				// Volver a vivo choca con otro cobro registrado entretanto: queda como estaba.
				t.Anotar(map[string]any{"revivir_rechazado": true})
				return nil, nil
			}
			if err != nil {
				return nil, err
			}
			r := aCobro(d)
			if d.Estado == antes.Estado && mismoID(d.FacturaID, antes.FacturaID) {
				return &r, nil
			}
			t.RegistrarCambios("cobro", d.ID, map[string]any{"estado": antes.Estado, "factura_id": antes.FacturaID},
				map[string]any{"estado": d.Estado, "factura_id": d.FacturaID})
			if d.Estado == antes.Estado {
				return &r, nil
			}
			if _, err := guardar(ctx, t, q, f, f); err != nil {
				return nil, err
			}
			return &r, t.Emitir(EventoCobroCambiado, map[string]any{
				"orden_id": d.OrdenID, "cargo_id": d.ID, "de": antes.Estado, "a": d.Estado, "factura_id": d.FacturaID,
			})
		},
	})
}

// SuscribirCobros: el 2xx o el rechazo del cargo (cola) y factura.* / pago.* de Argos
// (argos.factura_cambiada) siguen el cobro de la orden (cmd/worker). Idempotente.
func (m *Modulo) SuscribirCobros(bus *integracion.Bus, bd nucleo.BD) {
	pendiente := func(ctx context.Context, e integracion.Evento) error {
		var p struct {
			Operacion      string `json:"operacion"`
			IdempotencyKey string `json:"idempotency_key"`
		}
		if json.Unmarshal(e.Payload, &p) != nil || p.Operacion != pendientes.OpCargo {
			return nil
		}
		ctx = nucleo.ConBD(ctx, bd)
		reg, err := pendientes.PorClave(ctx, bd, p.IdempotencyKey)
		if err != nil {
			return nil
		}
		if e.Evento == pendientes.EventoFallido && reg.CargoTerminal {
			// 422 capacidad_no_disponible (0.14.3 §3): se avisa en la orden.
			if _, err := m.avisoCobro.Ejecutar(ctx, reg); err != nil {
				return err
			}
		}
		if strings.Count(p.IdempotencyKey, ":") != 3 {
			return nil
		}
		in := entradaCobroArgos{IdempotencyKey: &p.IdempotencyKey}
		switch e.Evento {
		case pendientes.EventoHecho:
			in.Estado = CobroFacturado
			if v, ok := pendientes.CampoDeRespuesta(reg.Respuesta, "factura.id"); ok {
				var n json.Number
				if json.Unmarshal(v, &n) == nil {
					if id, err := n.Int64(); err == nil && id > 0 {
						in.FacturaID = &id
					}
				}
			}
			in.Numero = campoTexto(reg.Respuesta, "factura.numero")
			in.Total = campoTexto(reg.Respuesta, "factura.total")
			in.Moneda = campoTexto(reg.Respuesta, "factura.moneda")
		case pendientes.EventoFallido:
			in.Estado, in.Detalle = CobroFallido, reg.UltimoError
			if reg.PorConfirmar {
				in.Estado = CobroPorConfirmar
			}
		case pendientes.EventoResueltoManual:
			// Resuelto a mano: se da por cobrado en Argos (el cupo no se libera sin una factura anulada).
			in.Estado, in.Detalle = CobroFacturado, reg.UltimoError
		default:
			return nil
		}
		_, err = m.SeguirCobro.Ejecutar(ctx, in)
		return err
	}
	for _, ev := range []string{pendientes.EventoHecho, pendientes.EventoFallido, pendientes.EventoResueltoManual} {
		bus.Suscribir("campo.cobro_pendiente", ev, pendiente)
	}
	bus.Suscribir("campo.cobro_factura", argos.EventoFacturaCambiada, func(ctx context.Context, e integracion.Evento) error {
		var p struct {
			Webhook   string  `json:"webhook"`
			FacturaID *int64  `json:"factura_id"`
			OrigenID  *string `json:"origen_id"`
			Numero    *string `json:"numero"`
			Total     *string `json:"total"`
			Moneda    *string `json:"moneda"`
		}
		if json.Unmarshal(e.Payload, &p) != nil || p.OrigenID == nil || p.FacturaID == nil {
			return nil
		}
		orden, err := strconv.ParseInt(*p.OrigenID, 10, 64)
		if err != nil {
			return nil
		}
		estado := map[string]string{argos.EventoFacturaEmitida: CobroFacturado, argos.EventoFacturaAnulada: CobroAnulado,
			argos.EventoPagoRegistrado: CobroPagado, argos.EventoPagoAnulado: CobroFacturado}[p.Webhook]
		if estado == "" {
			return nil
		}
		// 0.14.3 §3: factura.* y pago.* solo se aplican al cobro ligado a ese factura_id; si no hay, quedan
		// como aviso en la orden (el webhook ya está en integracion.entrantes).
		if _, err := campobd.New(bd).CobroPorFactura(ctx, campobd.CobroPorFacturaParams{FacturaID: *p.FacturaID, OrdenID: orden}); errors.Is(err, pgx.ErrNoRows) {
			_, err := m.avisoFactura.Ejecutar(nucleo.ConBD(ctx, bd), avisoFacturaEntrada{OrdenID: orden, FacturaID: *p.FacturaID, Webhook: p.Webhook})
			return err
		} else if err != nil {
			return err
		}
		_, err = m.SeguirCobro.Ejecutar(nucleo.ConBD(ctx, bd), entradaCobroArgos{OrdenID: orden, FacturaID: p.FacturaID, Estado: estado,
			Numero: p.Numero, Total: p.Total, Moneda: p.Moneda})
		return err
	})
}

// cobroRegistrado: la orden tiene un cobro de instalación vivo (el cierre no manda el de la plantilla).
func cobroRegistrado(ctx context.Context, q *campobd.Queries, orden int64) (bool, error) {
	_, err := q.CobroVivoDeOrden(ctx, orden)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

func (m *Modulo) registrarCobros(api huma.API) {
	op := operacion("registrarCobroOrden", http.MethodPost, "/api/v1/ordenes/{id}/cobros", "Registrar el cobro de la instalación",
		nucleo.PermOrdenesRegistrarCobro, http.StatusOK, http.StatusNotFound, http.StatusConflict)
	op.Description += " Factura única en Argos (POST /facturas/cargo-unico) con el concepto y el precio de la plantilla; sale en el momento por " +
		"la cola de pendientes (iris:cargo:<orden>:<cargo_id>, X-Iris-Actor). Cambiar el precio exige `ordenes.cobro_editar_monto` y motivo. " +
		"Nunca Q0. 409 `cobro_registrado` (uno vivo por orden; tras un anulado, otro con otro cargo_id). El cierre ya no manda el cargo de " +
		"instalación de la plantilla. Emite `orden.cobro_registrado`."
	huma.Register(api, op, func(ctx context.Context, in *conID[EntradaRegistrarCobro]) (*cuerpo[Cobro], error) {
		e := in.Body
		e.ID = in.ID
		return responder(m.RegistrarCobro.Ejecutar(ctx, e))
	})
	ver := operacion("cobrosOrden", http.MethodGet, "/api/v1/ordenes/{id}/cobros", "Cobros de la instalación de la orden",
		nucleo.PermOrdenesVer, http.StatusOK, http.StatusNotFound)
	huma.Register(api, ver, func(ctx context.Context, in *EntradaVerOrden) (*cuerpo[[]Cobro], error) {
		return responder(m.VerCobros.Ejecutar(ctx, *in))
	})
}

// nuevoAvisoCobro deja en el historial de la orden que Argos rechazó un cobro con 422
// capacidad_no_disponible (0.14.3 §3): por confirmar, reenviado sin la línea de instalación o sin
// reemplazo (el supervisor lo revisa). Idempotente por pendiente y situación.
func (m *Modulo) nuevoAvisoCobro() *auditoria.Caso[pendientes.Registro, bool] {
	return auditoria.Nuevo(auditoria.Caso[pendientes.Registro, bool]{
		Accion: "ordenes.aviso_cobro", Acceso: auditoria.Autenticado, AuditarExito: func(b bool) bool { return b },
		Logica: func(ctx context.Context, t *auditoria.Tx, reg pendientes.Registro) (bool, error) {
			orden, err := strconv.ParseInt(reg.EntidadID, 10, 64)
			if err != nil {
				return false, nil
			}
			q := campobd.New(t)
			f, err := q.OrdenPorIDParaActualizar(ctx, orden)
			if errors.Is(err, pgx.ErrNoRows) {
				return false, nil
			}
			if err != nil {
				return false, err
			}
			situacion := "sin_reemplazo"
			switch {
			case reg.PorConfirmar:
				situacion = "por_confirmar"
			case reg.ReemplazadoPor != nil:
				situacion = "reemplazado"
			case reg.ReemplazoDe != nil:
				situacion = "reemplazo_rechazado"
			}
			var ya int
			if err := t.QueryRow(ctx, `SELECT count(*) FROM campo.orden_eventos WHERE orden_id = $1 AND tipo = 'cobro'
				AND datos->>'pendiente_id' = $2 AND a = $3`, f.ID, strconv.FormatInt(reg.ID, 10), situacion).Scan(&ya); err != nil {
				return false, err
			}
			if ya > 0 {
				return false, nil
			}
			t.FijarEntidad("orden", f.ID)
			t.FijarEmpresa(deref(f.EmpresaID))
			datos := map[string]any{"pendiente_id": strconv.FormatInt(reg.ID, 10), "idempotency_key": reg.IdempotencyKey,
				"causa": pendientes.CodigoCapacidadNoDisponible, "reemplazo_id": reg.ReemplazadoPor, "reemplazo_de": reg.ReemplazoDe}
			motivo := map[string]string{
				"por_confirmar":       "Cobro por confirmar: Argos rechazó el cobro tras un envío sin respuesta; un supervisor confirma si la factura existe",
				"reemplazado":         "El cobro de la instalación no está disponible: las demás líneas salieron en un cobro nuevo",
				"reemplazo_rechazado": "Argos también rechazó el cobro de reemplazo: revísalo a mano",
				"sin_reemplazo":       "El cobro de la instalación no está disponible para esta empresa",
			}[situacion]
			if err := historial(ctx, t, q, f.ID, "cobro", nil, &situacion, &motivo, datos, m.ahora()); err != nil {
				return false, err
			}
			return true, nil
		},
	})
}

// otroCobroVigente: la orden tiene otro cobro de A36 vivo, o (si este es de instalación) el cargo del
// cierre vigente con la línea de instalación.
func (m *Modulo) otroCobroVigente(ctx context.Context, t *auditoria.Tx, q *campobd.Queries, c campobd.CampoCobro) (bool, error) {
	if v, err := q.CobroVivoDeOrden(ctx, c.OrdenID); err == nil && v.ID != c.ID {
		return true, nil
	} else if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return false, err
	}
	if !c.EsInstalacion {
		return false, nil
	}
	return pendientes.CargoDelCierreConInstalacion(ctx, t, strconv.FormatInt(c.OrdenID, 10), 0)
}

type avisoFacturaEntrada struct {
	OrdenID   int64  `json:"orden_id"`
	FacturaID int64  `json:"factura_id"`
	Webhook   string `json:"webhook"`
}

// nuevoAvisoFactura: un factura.* o pago.* de una factura sin cobro ligado (otro cargo de la orden, una
// factura por anular o una desconocida) no cambia ningún cobro: queda como aviso en la orden (una vez
// por factura y webhook).
func (m *Modulo) nuevoAvisoFactura() *auditoria.Caso[avisoFacturaEntrada, bool] {
	return auditoria.Nuevo(auditoria.Caso[avisoFacturaEntrada, bool]{
		Accion: "ordenes.aviso_factura", Acceso: auditoria.Autenticado, AuditarExito: func(b bool) bool { return b },
		Logica: func(ctx context.Context, t *auditoria.Tx, in avisoFacturaEntrada) (bool, error) {
			q := campobd.New(t)
			f, err := q.OrdenPorIDParaActualizar(ctx, in.OrdenID)
			if errors.Is(err, pgx.ErrNoRows) {
				return false, nil
			}
			if err != nil {
				return false, err
			}
			situacion := "factura_sin_cobro"
			if p, err := pendientes.PorFactura(ctx, t, in.FacturaID); err == nil {
				switch {
				case p.FacturaPorAnular != nil && *p.FacturaPorAnular == in.FacturaID:
					situacion = "factura_por_anular"
				case p.EntidadID == strconv.FormatInt(f.ID, 10):
					return false, nil // la factura de un cargo de la orden (cierre o reemplazo): nada que avisar
				}
			} else if !errors.Is(err, pgx.ErrNoRows) {
				return false, err
			}
			var ya int
			if err := t.QueryRow(ctx, `SELECT count(*) FROM campo.orden_eventos WHERE orden_id = $1 AND tipo = 'cobro'
				AND datos->>'factura_id' = $2 AND datos->>'webhook' = $3`, f.ID, strconv.FormatInt(in.FacturaID, 10), in.Webhook).Scan(&ya); err != nil {
				return false, err
			}
			if ya > 0 {
				return false, nil
			}
			t.FijarEntidad("orden", f.ID)
			t.FijarEmpresa(deref(f.EmpresaID))
			motivo := "Argos avisó " + in.Webhook + " de una factura que no está ligada a ningún cobro de la orden: revísala"
			if situacion == "factura_por_anular" {
				motivo = "Argos avisó " + in.Webhook + " de la factura por anular del cobro por confirmar"
			}
			return true, historial(ctx, t, q, f.ID, "cobro", nil, &situacion, &motivo,
				map[string]any{"factura_id": strconv.FormatInt(in.FacturaID, 10), "webhook": in.Webhook}, m.ahora())
		},
	})
}

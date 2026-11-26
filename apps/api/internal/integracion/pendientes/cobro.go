package pendientes

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos"
	pendientesbd "github.com/HCDestroyer/iris/apps/api/internal/integracion/pendientes/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
)

// Cargo rechazado por 422 capacidad_no_disponible (contrato 0.14.3 §3, «0.9.0:»):
//
//   - El cargo queda fallido y terminal: nunca se reintenta con su clave, ni solo ni a mano (Argos no
//     guarda la clave en un 4xx y el reintento crearía la factura completa).
//   - Si tenía otras líneas, salen sin la de instalación como un cobro nuevo, con otro cargo_id
//     (iris:cargo:<orden_id>:<cargo_id>). El reemplazo nunca lleva la línea de instalación ni genera
//     otro reemplazo: si también recibe el 422, queda fallido y se avisa al supervisor.
//   - Si el 422 llega tras un envío sin respuesta (timeout o 5xx), Argos pudo haber creado la factura
//     con el primer envío: el cargo queda «por confirmar» (cuenta como vigente) y nada sale hasta que
//     un supervisor confirme (ConfirmarCobro). Nunca se concilia solo por origen_id.

// cargoSinCapacidad marca el cargo terminal y, si corresponde, encola su reemplazo.
func (m *Modulo) cargoSinCapacidad(ctx context.Context, t *auditoria.Tx, antes, f pendientesbd.IntegracionPendiente, concepto int64,
	ahora time.Time) (pendientesbd.IntegracionPendiente, *pendientesbd.IntegracionPendiente, error) {
	q := pendientesbd.New(t)
	porConfirmar := antes.EnvioSinRespuesta && antes.ReemplazoDe == nil
	g, err := q.MarcarCargoTerminal(ctx, pendientesbd.MarcarCargoTerminalParams{ID: f.ID, PorConfirmar: porConfirmar})
	if err != nil {
		return f, nil, err
	}
	t.RegistrarCambios("pendiente", g.ID, map[string]any{"cargo_terminal": false, "por_confirmar": false},
		map[string]any{"cargo_terminal": true, "por_confirmar": porConfirmar})
	switch {
	case porConfirmar:
		t.Anotar(map[string]any{"cobro_por_confirmar": true})
		slog.WarnContext(ctx, "pendientes: cobro por confirmar (422 capacidad_no_disponible tras un envío sin respuesta); un supervisor debe confirmar en Argos",
			"pendiente", g.ID, "orden", g.EntidadID)
		return g, nil, nil
	case antes.ReemplazoDe != nil:
		t.Anotar(map[string]any{"reemplazo_rechazado": true})
		slog.ErrorContext(ctx, "pendientes: el cobro de reemplazo también recibió 422 capacidad_no_disponible; no se reenvía, revisar a mano",
			"pendiente", g.ID, "orden", g.EntidadID, "reemplaza_a", *antes.ReemplazoDe)
		return g, nil, nil
	}
	return m.reemplazarCargo(ctx, t, g, concepto, ahora)
}

// reemplazarCargo encola el cobro de reemplazo de un cargo terminal: sus líneas sin la de instalación,
// con otro cargo_id. Nada si el cargo ya es un reemplazo, ya tiene uno, no tiene lineas[] (forma
// anterior a la 0.14.3) o no le queda ninguna otra línea. Sin el concepto de instalación (0), no se
// adivina qué línea quitar: el reemplazo nace «por resolver» (fallido e incompleto, visible al
// supervisor) con todas las líneas, para no perderlas.
func (m *Modulo) reemplazarCargo(ctx context.Context, t *auditoria.Tx, f pendientesbd.IntegracionPendiente, concepto int64,
	ahora time.Time) (pendientesbd.IntegracionPendiente, *pendientesbd.IntegracionPendiente, error) {
	if f.ReemplazoDe != nil || f.ReemplazadoPor != nil {
		return f, nil, nil
	}
	cuerpo, lineas, ok := lineasDe(f.Payload)
	if !ok {
		return f, nil, nil
	}
	quedan := lineas
	if concepto > 0 {
		quedan = SinLineaInstalacion(lineas, map[int64]bool{concepto: true})
		if len(quedan) == 0 || len(quedan) == len(lineas) {
			return f, nil, nil
		}
	} else if len(lineas) < 2 {
		return f, nil, nil
	}
	cuerpo["lineas"] = quedan
	b, err := json.Marshal(cuerpo)
	if err != nil {
		return f, nil, err
	}
	id, err := uuid.NewV7()
	if err != nil {
		return f, nil, err
	}
	q := pendientesbd.New(t)
	clave := ClaveCargo(f.EntidadID, id)
	params := pendientesbd.InsertarPendienteParams{
		EmpresaID: f.EmpresaID, Sistema: f.Sistema, Operacion: OpCargo, IdempotencyKey: clave, Entidad: f.Entidad, EntidadID: f.EntidadID,
		Metodo: f.Metodo, Ruta: f.Ruta, Payload: b, DependeDe: []int64{}, Estado: EstadoPendiente, ProximoIntento: ahora,
		EventoID: f.EventoID, Actor: f.Actor,
	}
	porResolver := concepto <= 0
	if porResolver {
		params.Estado, params.DatosIncompletos = EstadoFallidoNegocio, true
		params.UltimoError = errorf("datos incompletos: %s", MotivoReemplazoSinConcepto)
	}
	n, err := q.InsertarPendiente(ctx, params)
	if err != nil {
		return f, nil, err
	}
	if err := q.EnlazarReemplazo(ctx, pendientesbd.EnlazarReemplazoParams{De: f.ID, ID: n.ID}); err != nil {
		return f, nil, err
	}
	if err := q.MarcarReemplazado(ctx, pendientesbd.MarcarReemplazadoParams{Por: n.ID, ID: f.ID}); err != nil {
		return f, nil, err
	}
	if n, err = q.PendientePorID(ctx, n.ID); err != nil {
		return f, nil, err
	}
	if f, err = q.PendientePorID(ctx, f.ID); err != nil {
		return f, nil, err
	}
	t.RegistrarCambios("pendiente", n.ID, nil, map[string]any{"operacion": n.Operacion, "estado": n.Estado, "idempotency_key": clave,
		"reemplazo_de": f.ID, "por_resolver": porResolver})
	t.RegistrarCambios("pendiente", f.ID, map[string]any{"reemplazado_por": nil}, map[string]any{"reemplazado_por": n.ID})
	if porResolver {
		slog.ErrorContext(ctx, "pendientes: cobro de reemplazo por resolver (sin el concepto de instalación de Argos); revisar a mano",
			"pendiente", f.ID, "reemplazo", n.ID, "orden", f.EntidadID)
		if err := t.Emitir(EventoFallido, eventoFallo{eventoPendiente: eventoDe(n), Detalle: MotivoReemplazoSinConcepto}); err != nil {
			return f, nil, err
		}
		return f, &n, nil
	}
	slog.WarnContext(ctx, "pendientes: cargo sin capacidad cobro_equipo; sus demás líneas salen como cobro de reemplazo",
		"pendiente", f.ID, "reemplazo", n.ID, "orden", f.EntidadID)
	return f, &n, nil
}

// MotivoReemplazoSinConcepto: el cobro de reemplazo no sabe qué línea es la de instalación (Argos no
// informó el concepto con codigo_sistema instalacion): queda por resolver, con todas las líneas.
const MotivoReemplazoSinConcepto = "reemplazo_sin_concepto_instalacion: quita a mano la línea de instalación en Argos"

// ClaveCargo es la Idempotency-Key de un cobro de la orden con su cargo_id (0.9.0 §3).
func ClaveCargo(orden string, cargo uuid.UUID) string {
	return "iris:cargo:" + orden + ":" + cargo.String()
}

// lineasDe lee el cuerpo de un cargo y sus lineas[] (números exactos).
func lineasDe(payload []byte) (map[string]any, []map[string]any, bool) {
	dec := json.NewDecoder(bytes.NewReader(payload))
	dec.UseNumber()
	var cuerpo map[string]any
	if dec.Decode(&cuerpo) != nil {
		return nil, nil, false
	}
	crudas, ok := cuerpo["lineas"].([]any)
	if !ok || len(crudas) == 0 {
		return nil, nil, false
	}
	lineas := make([]map[string]any, 0, len(crudas))
	for _, l := range crudas {
		x, ok := l.(map[string]any)
		if !ok {
			return nil, nil, false
		}
		lineas = append(lineas, x)
	}
	return cuerpo, lineas, true
}

// --- Confirmar un cobro «por confirmar» --------------------------------------------------------

// EntradaConfirmarCobro: el supervisor concilia un cobro «por confirmar» (0.14.3 §3). Con existe, la
// factura («Factura #» del panel de Argos, que es su factura_id, o la serie y el número del DTE) se lee
// en Argos y se valida; sin existe, es la decisión aparte «no existe», con motivo, que funciona con
// cualquier versión de Argos (salvo que el cargo tenga una factura por anular: entonces una lectura en
// la misma acción debe verla anulada).
type EntradaConfirmarCobro struct {
	ID            int64   `json:"-"`
	Version       int32   `json:"version" minimum:"1"`
	Existe        bool    `json:"existe" doc:"true: la factura existe en Argos (facturaId, o serie y numeroDte; Iris la lee y la valida). false: el supervisor comprobó en Argos que no existe (el cargo queda fallido y salen sus demás líneas sin la de instalación)"`
	FacturaID     *int64  `json:"facturaId,omitempty" required:"false" minimum:"1" doc:"Con existe: la «Factura #» del panel de Argos (su factura_id)"`
	Serie         *string `json:"serie,omitempty" required:"false" minLength:"1" maxLength:"60" doc:"Con existe y sin facturaId: serie del DTE (con numeroDte)"`
	NumeroDTE     *string `json:"numeroDte,omitempty" required:"false" minLength:"1" maxLength:"60" doc:"Con existe y sin facturaId: número del DTE (con serie)"`
	PagosAnulados bool    `json:"pagosAnulados,omitempty" required:"false" doc:"«No existe» con una factura por anular que tenía pagos (saldo < total): el supervisor confirma que el pago se anuló o pasó a abono en Argos"`
	Motivo        string  `json:"motivo" minLength:"5" maxLength:"1000" doc:"Qué se revisó en Argos (queda en la auditoría)"`

	factura     argos.FacturaArgos
	errFactura  error
	leida       bool
	concepto    int64
	errConcepto error
	// porAnular: con existe y una factura por anular guardada, su lectura en esta misma acción.
	porAnular      argos.FacturaArgos
	errPorAnular   error
	porAnularLeida bool
}

// ConfirmarCobro lee Argos fuera de la transacción (la factura indicada, la factura por anular o el
// concepto de instalación para el reemplazo) y aplica la conciliación con la orden bloqueada.
func (m *Modulo) ConfirmarCobro(ctx context.Context, in EntradaConfirmarCobro) (Pendiente, error) {
	f, err := m.cargoPorConfirmar(ctx, in.ID)
	if err != nil {
		return Pendiente{}, err
	}
	a := contexto.ActorDe(ctx)
	if in.Existe {
		b, err := buscarFactura(in)
		if err != nil {
			return Pendiente{}, err
		}
		if !a.EsUsuario() {
			return Pendiente{}, nucleo.Prohibido("La conciliación la hace una persona (X-Iris-Actor)")
		}
		in.factura, in.errFactura = m.cfg.Argos.LeerFacturaOrigenIris(ctx, deref64(f.EmpresaID), f.EntidadID, b, a.ID)
		in.leida = true
		if f.FacturaPorAnularID != nil && *f.FacturaPorAnularID != b.FacturaID {
			// Con una factura por anular guardada, «existe» exige verla anulada (o quitarla antes).
			in.porAnular, in.errPorAnular = m.cfg.Argos.LeerFacturaOrigenIris(ctx, deref64(f.EmpresaID), f.EntidadID,
				argos.BuscarFactura{FacturaID: *f.FacturaPorAnularID}, a.ID)
			in.porAnularLeida = true
		}
	} else {
		if f.FacturaPorAnularID != nil {
			if !a.EsUsuario() {
				return Pendiente{}, nucleo.Prohibido("La conciliación la hace una persona (X-Iris-Actor)")
			}
			in.factura, in.errFactura = m.cfg.Argos.LeerFacturaOrigenIris(ctx, deref64(f.EmpresaID), f.EntidadID,
				argos.BuscarFactura{FacturaID: *f.FacturaPorAnularID}, a.ID)
			in.leida = true
		}
		in.concepto, in.errConcepto = m.cfg.Argos.ConceptoInstalacion(ctx, deref64(f.EmpresaID))
	}
	return m.confirmarCobro.Ejecutar(ctx, in)
}

// cargoPorConfirmar: comprobaciones antes de leer Argos (permiso, visibilidad, «por confirmar»).
func (m *Modulo) cargoPorConfirmar(ctx context.Context, id int64) (pendientesbd.IntegracionPendiente, error) {
	if !contexto.PermisosDe(ctx).Tiene(string(nucleo.PermIntegracionesConfirmarCobro)) {
		// Antes de leer Argos: sin el permiso no se dispara ninguna lectura.
		return pendientesbd.IntegracionPendiente{}, nucleo.Prohibido("Confirmar un cobro exige integraciones.confirmar_cobro")
	}
	bd := nucleo.BDDe(ctx)
	if bd == nil {
		return pendientesbd.IntegracionPendiente{}, nucleo.ErrSinBD
	}
	f, err := pendientesbd.New(bd).PendientePorID(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && !visible(ctx, f.EmpresaID)) {
		return f, nucleo.NoEncontrado("")
	}
	if err != nil {
		return f, err
	}
	if !f.PorConfirmar {
		return f, nucleo.Conflicto("cobro_no_por_confirmar", "Este pendiente no es un cobro por confirmar")
	}
	if m.cfg.Argos == nil {
		return f, nucleo.NoDisponible("Iris no tiene conexión con Argos")
	}
	return f, nil
}

// buscarFactura: facturaId, o serie y numeroDte (juntos y excluyentes con facturaId).
func buscarFactura(in EntradaConfirmarCobro) (argos.BuscarFactura, error) {
	limpio := func(s *string) string {
		if s == nil {
			return ""
		}
		return strings.TrimSpace(*s)
	}
	b := argos.BuscarFactura{Serie: limpio(in.Serie), NumeroDTE: limpio(in.NumeroDTE)}
	if in.FacturaID != nil {
		b.FacturaID = *in.FacturaID
	}
	switch {
	case b.FacturaID > 0 && b.Serie == "" && b.NumeroDTE == "":
	case b.FacturaID == 0 && b.Serie != "" && b.NumeroDTE != "":
	default:
		return b, nucleo.Validacion(nucleo.ErrorCampo{Field: "facturaId", Message: "Indica la «Factura #» del panel de Argos, o la serie y el número del DTE", Rule: "factura"})
	}
	return b, nil
}

func nuevoConfirmarCobro(m *Modulo) *auditoria.Caso[EntradaConfirmarCobro, Pendiente] {
	return auditoria.Nuevo(auditoria.Caso[EntradaConfirmarCobro, Pendiente]{
		Accion: "integraciones.confirmar_cobro", Permiso: nucleo.PermIntegracionesConfirmarCobro,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaConfirmarCobro) (Pendiente, error) {
			q := pendientesbd.New(t)
			previo, err := q.PendientePorID(ctx, in.ID)
			if errors.Is(err, pgx.ErrNoRows) {
				return Pendiente{}, nucleo.NoEncontrado("")
			}
			if err != nil {
				return Pendiente{}, err
			}
			// La orden primero (mismo orden de bloqueo que el cierre y A36), luego el cargo.
			if err := bloquearOrden(ctx, q, previo); err != nil {
				return Pendiente{}, err
			}
			f, err := q.PendientePorIDParaActualizar(ctx, in.ID)
			if err != nil {
				return Pendiente{}, err
			}
			if !visible(ctx, f.EmpresaID) {
				return Pendiente{}, nucleo.NoEncontrado("")
			}
			t.FijarEntidad("pendiente", f.ID)
			if f.EmpresaID != nil {
				t.FijarEmpresa(*f.EmpresaID)
			}
			if err := nucleo.ExigirVersion(f.Version, in.Version, "pendiente", f.ID); err != nil {
				return Pendiente{}, err
			}
			if !f.PorConfirmar || f.Estado != EstadoFallidoNegocio {
				return Pendiente{}, nucleo.Conflicto("cobro_no_por_confirmar", "Este pendiente ya no es un cobro por confirmar")
			}
			motivo := strings.TrimSpace(in.Motivo)
			if n := utf8.RuneCountInString(motivo); n < 5 || n > 1000 {
				return Pendiente{}, nucleo.Validacion(nucleo.ErrorCampo{Field: "motivo", Message: "Escribe qué revisaste en Argos (5 a 1000 caracteres)", Rule: "longitud"})
			}
			var usuario *int64
			if a := contexto.ActorDe(ctx); a.EsUsuario() {
				id := a.ID
				usuario = &id
			}
			ahora := m.ahora()
			if in.Existe {
				return m.confirmarEmitido(ctx, t, f, in, motivo, usuario, ahora)
			}
			if f.FacturaPorAnularID != nil {
				// «No existe» con una factura por anular: solo si la lectura de esta acción la ve anulada (y,
				// si tenía pagos, el supervisor confirma que se anularon o pasaron a abono).
				if err := porAnularAnulada(in, *f.FacturaPorAnularID); err != nil {
					return Pendiente{}, err
				}
			} else if f.ExigePorAnular {
				// Tuvo una factura por anular y se quitó: «no existe» sigue bloqueado hasta que otra factura por
				// anular se lea anulada.
				return Pendiente{}, nucleo.ReglaNegocio("factura_por_anular_vigente",
					"Este cobro tuvo una factura por anular: indica la factura por anular y confirma que está anulada en Argos")
			}
			concepto := in.concepto
			if in.errConcepto != nil {
				// Sin el concepto (Argos anterior, caído o sin él): el reemplazo queda por resolver, visible.
				concepto = 0
			}
			g, err := q.ConfirmarCobroInexistente(ctx, pendientesbd.ConfirmarCobroInexistenteParams{Motivo: motivo, ConfirmadoPor: usuario,
				Ahora: ahora, ID: f.ID, Version: in.Version})
			if errors.Is(err, pgx.ErrNoRows) {
				return Pendiente{}, nucleo.ConflictoVersion("pendiente", f.ID)
			}
			if err != nil {
				return Pendiente{}, err
			}
			t.RegistrarCambios("pendiente", g.ID, map[string]any{"por_confirmar": true}, map[string]any{"por_confirmar": false, "existe": false,
				"confirmado_motivo": motivo})
			if err := t.Emitir(EventoFallido, eventoFallo{eventoPendiente: eventoDe(g), Intentos: g.Intentos, Status: g.UltimoStatus,
				Detalle: "cobro confirmado sin factura en Argos"}); err != nil {
				return Pendiente{}, err
			}
			g, reemplazo, err := m.reemplazarCargo(ctx, t, g, concepto, ahora)
			if err != nil {
				return Pendiente{}, err
			}
			if reemplazo != nil && reemplazo.Estado == EstadoPendiente {
				return aPendiente(g), m.avisarCola(ctx, t.Tx)
			}
			return aPendiente(g), nil
		},
	})
}

// bloquearOrden: campo.ordenes FOR UPDATE de la orden del pendiente (si es de una orden de campo).
func bloquearOrden(ctx context.Context, q *pendientesbd.Queries, f pendientesbd.IntegracionPendiente) error {
	if f.Entidad != "orden" {
		return nil
	}
	orden, err := strconv.ParseInt(f.EntidadID, 10, 64)
	if err != nil {
		return nil
	}
	if _, err := q.BloquearOrdenCampo(ctx, orden); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	return nil
}

// toleranciaEmision: la factura conciliada debe ser de después del primer envío del cargo, con 5 min
// de tolerancia de reloj (0.14.3 §3).
const toleranciaEmision = 5 * time.Minute

// confirmarEmitido valida la factura leída de Argos contra el cargo bloqueado (empresa, servicio,
// cliente, iris_orden, origen_id, líneas exactas, no anulada, emitida después del primer envío y no
// ligada a otro cargo) y deja el cargo hecho con ella. Cualquier desajuste rechaza la conciliación: el
// cargo sigue por confirmar.
func (m *Modulo) confirmarEmitido(ctx context.Context, t *auditoria.Tx, f pendientesbd.IntegracionPendiente, in EntradaConfirmarCobro,
	motivo string, usuario *int64, ahora time.Time) (Pendiente, error) {
	switch {
	case errors.Is(in.errFactura, argos.ErrFacturaNoEncontrada):
		return Pendiente{}, nucleo.ReglaNegocio("factura_no_encontrada",
			"Ese número no es una factura de esta orden en Argos: el cobro sigue por confirmar (si comprobaste que no existe, confírmalo aparte)")
	case errors.Is(in.errFactura, argos.ErrLecturaNoDisponible):
		return Pendiente{}, nucleo.ReglaNegocio("conciliacion_no_disponible",
			"Argos aún no permite leer la factura (contrato < 0.14.3): no se puede conciliar como «existe»")
	case in.errFactura != nil:
		slog.WarnContext(ctx, "pendientes: no se pudo leer la factura de Argos para conciliar el cobro", "pendiente", f.ID, "err", in.errFactura)
		return Pendiente{}, nucleo.NoDisponible("No se pudo leer la factura en Argos: reintenta")
	}
	if f.FacturaPorAnularID != nil && *f.FacturaPorAnularID != in.factura.ID {
		// Bajo 7: con una factura por anular guardada, «existe» exige que una lectura de esta acción la vea
		// anulada; si no, primero se quita.
		switch {
		case !in.porAnularLeida:
			return Pendiente{}, nucleo.ReglaNegocio("factura_por_anular_vigente", "El cobro tiene una factura por anular: quítala o anúlala primero")
		case in.errPorAnular != nil && !errors.Is(in.errPorAnular, argos.ErrFacturaNoEncontrada):
			return Pendiente{}, nucleo.NoDisponible("No se pudo leer la factura por anular en Argos: reintenta")
		case in.errPorAnular != nil || !strings.EqualFold(in.porAnular.Estado, "anulada"):
			return Pendiente{}, nucleo.ReglaNegocio("factura_por_anular_vigente",
				"El cobro tiene una factura por anular que no está anulada en Argos: quítala o anúlala primero")
		}
	}
	q := pendientesbd.New(t)
	campo := facturaDistinta(f, in.factura, in.factura.SinConcepto)
	if campo == "" {
		ligada, err := q.FacturaLigada(ctx, pendientesbd.FacturaLigadaParams{FacturaID: in.factura.ID, ID: f.ID})
		if err != nil {
			return Pendiente{}, err
		}
		if ligada {
			campo = "ya está ligada a otro cobro de Iris"
		}
	}
	if campo == "" && (f.PrimerEnvioEn == nil || in.factura.EmitidaEn.Before(f.PrimerEnvioEn.Add(-toleranciaEmision))) {
		campo = "es anterior al primer envío del cobro"
	}
	if campo != "" {
		return Pendiente{}, nucleo.NuevoProblema(http.StatusUnprocessableEntity, "factura_no_coincide", "La factura no es de este cobro",
			"La factura de Argos no coincide con el cobro ("+campo+"): el cobro sigue por confirmar",
			nucleo.ErrorCampo{Field: "facturaId", Message: "No coincide: " + campo, Rule: "factura_del_cobro"})
	}
	if in.factura.SinConcepto {
		// Factura anterior a la 0.14.3: no se puede validar el concepto. Se guarda como factura por anular
		// (no liga el cargo); «no existe» se habilita cuando una lectura la vea anulada.
		id := in.factura.ID
		g, err := q.FijarFacturaPorAnular(ctx, pendientesbd.FijarFacturaPorAnularParams{FacturaID: &id, Motivo: &motivo, ID: f.ID, Version: in.Version})
		if errors.Is(err, pgx.ErrNoRows) {
			return Pendiente{}, nucleo.ConflictoVersion("pendiente", f.ID)
		}
		if esUnica(err) {
			return Pendiente{}, nucleo.ReglaNegocio("factura_no_coincide", "Esa factura ya está ligada a otro cobro de Iris")
		}
		if err != nil {
			return Pendiente{}, err
		}
		t.RegistrarCambios("pendiente", g.ID, map[string]any{"factura_por_anular_id": f.FacturaPorAnularID},
			map[string]any{"factura_por_anular_id": id, "con_pagos": in.factura.ConPagos(), "motivo": motivo})
		t.Anotar(map[string]any{"factura_por_anular": id})
		return aPendiente(g), nil
	}
	factura := map[string]any{"id": in.factura.ID, "numero": in.factura.Numero, "estado": in.factura.Estado, "total": in.factura.Total,
		"moneda": in.factura.Moneda}
	respuesta, err := json.Marshal(map[string]any{"documento": "factura", "factura": factura})
	if err != nil {
		return Pendiente{}, err
	}
	g, err := q.ConfirmarCobroEmitido(ctx, pendientesbd.ConfirmarCobroEmitidoParams{Ahora: ahora, Respuesta: respuesta, FacturaID: in.factura.ID,
		Motivo: motivo, ConfirmadoPor: usuario, ID: f.ID, Version: in.Version})
	if errors.Is(err, pgx.ErrNoRows) {
		return Pendiente{}, nucleo.ConflictoVersion("pendiente", f.ID)
	}
	if esUnica(err) {
		return Pendiente{}, nucleo.ReglaNegocio("factura_no_coincide", "Esa factura ya está ligada a otro cobro de Iris")
	}
	if err != nil {
		return Pendiente{}, err
	}
	t.RegistrarCambios("pendiente", g.ID, map[string]any{"estado": f.Estado, "por_confirmar": true},
		map[string]any{"estado": g.Estado, "por_confirmar": false, "existe": true, "factura_id": in.factura.ID, "confirmado_motivo": motivo})
	if err := t.Emitir(EventoHecho, eventoDe(g)); err != nil {
		return Pendiente{}, err
	}
	if err := m.desbloquearDependientes(ctx, t, g.ID, ahora); err != nil {
		return Pendiente{}, err
	}
	return aPendiente(g), nil
}

// esUnica: violación de una restricción única (23505).
func esUnica(err error) bool {
	var pe *pgconn.PgError
	return errors.As(err, &pe) && pe.Code == "23505"
}

// facturaDistinta dice qué no coincide entre la factura de Argos y el cuerpo del cargo, o "": misma
// empresa, mismo servicio, iris_orden, mismo origen, no anulada y líneas exactas (concepto, precio y
// cantidad, sin líneas de más). sinConcepto: factura anterior a la 0.14.3, las líneas se comparan sin
// el concepto.
func facturaDistinta(f pendientesbd.IntegracionPendiente, fa argos.FacturaArgos, sinConcepto bool) string {
	cuerpo, lineas, ok := lineasDe(f.Payload)
	if !ok {
		return "el cobro no tiene líneas"
	}
	if f.EmpresaID == nil || fa.EmpresaID != *f.EmpresaID {
		return "empresa"
	}
	if s, ok := conceptoDe(cuerpo["servicio_id"]); !ok || s != fa.ServicioID {
		return "servicio"
	}
	if fa.OrigenTipo != OrigenTipo || fa.OrigenID != f.EntidadID {
		return "origen"
	}
	if strings.EqualFold(fa.Estado, "anulada") {
		return "está anulada"
	}
	clave := func(c int64, precio string, n int64) string {
		if sinConcepto {
			c = 0
		}
		return strconv.FormatInt(c, 10) + "@" + precio + "x" + strconv.FormatInt(n, 10)
	}
	esperadas := make([]string, 0, len(lineas))
	for _, l := range lineas {
		c, ok := conceptoDe(l["concepto_id"])
		precio := ""
		switch p := l["precio_unitario"].(type) {
		case string:
			precio = argos.PrecioCanonico(p)
		case json.Number:
			precio = argos.PrecioCanonico(p.String())
		}
		if !ok || precio == "" {
			return "líneas"
		}
		n := int64(1)
		if q, ok := conceptoDe(l["cantidad"]); ok {
			n = q
		}
		esperadas = append(esperadas, clave(c, precio, n))
	}
	halladas := make([]string, 0, len(fa.Lineas))
	for _, l := range fa.Lineas {
		halladas = append(halladas, clave(l.ConceptoID, l.PrecioUnitario, l.Cantidad))
	}
	sort.Strings(esperadas)
	sort.Strings(halladas)
	if strings.Join(esperadas, ",") != strings.Join(halladas, ",") {
		return "líneas"
	}
	return ""
}

// porAnularAnulada: la lectura de esta acción ve anulada la factura por anular del cargo; con pagos
// (saldo < total), además el supervisor confirmó que se anularon o pasaron a abono.
func porAnularAnulada(in EntradaConfirmarCobro, id int64) error {
	switch {
	case !in.leida:
		return nucleo.ReglaNegocio("factura_por_anular_vigente", "El cobro tiene una factura por anular: hay que leerla en Argos")
	case errors.Is(in.errFactura, argos.ErrFacturaNoEncontrada), errors.Is(in.errFactura, argos.ErrLecturaNoDisponible):
		return nucleo.ReglaNegocio("factura_por_anular_vigente", "No se pudo comprobar en Argos que la factura por anular esté anulada")
	case in.errFactura != nil:
		return nucleo.NoDisponible("No se pudo leer la factura por anular en Argos: reintenta")
	case in.factura.ID != id || !strings.EqualFold(in.factura.Estado, "anulada"):
		return nucleo.ReglaNegocio("factura_por_anular_vigente",
			"La factura por anular aún no está anulada en Argos: anúlala (o quítala del cobro) antes de confirmar que no existe")
	case in.factura.ConPagos() && !in.PagosAnulados:
		return nucleo.NuevoProblema(http.StatusUnprocessableEntity, "pagos_por_confirmar", "La factura tenía pagos",
			"La factura por anular tenía pagos: confirma que el pago se anuló o pasó a abono en Argos",
			nucleo.ErrorCampo{Field: "pagosAnulados", Message: "Confirma que los pagos se anularon o pasaron a abono", Rule: "required"})
	}
	return nil
}

// CargoConInstalacionVigente dice si algún cargo de la orden (el del cierre, uno de A36 —también uno
// reintentado—, un reemplazo resuelto a mano…), sin contar el de la clave excluir, está vigente con la
// línea de instalación (concepto). Con el concepto desconocido (0) o un cargo de la forma anterior,
// cuenta como si la llevara (lo más estricto).
// Devuelve la clave de ese cargo ("" si no hay).
func CargoConInstalacionVigente(ctx context.Context, q pendientesbd.DBTX, orden string, concepto int64, excluir string) (string, error) {
	cargos, err := pendientesbd.New(q).CargosDeOrden(ctx, orden)
	if err != nil {
		return "", err
	}
	for _, c := range cargos {
		if c.IdempotencyKey == excluir || !cargoVigente(c) {
			continue
		}
		if llevaInstalacion(c.Payload, concepto) {
			return c.IdempotencyKey, nil
		}
	}
	return "", nil
}

// cargoVigente: un cargo cuenta como vigente salvo fallido (sin «por confirmar»), con su factura anulada
// en Argos, en espera del consumo (nunca sale) o resuelto porque Argos lo cobró con el consumo. Uno
// resuelto a mano sí cuenta (pudo cobrarse en Argos), y también uno por resolver (espera, p. ej., al
// titular: saldrá).
func cargoVigente(f pendientesbd.IntegracionPendiente) bool {
	switch {
	case f.Estado == EstadoFallidoNegocio && !f.PorConfirmar, f.FacturaAnulada, f.EsperaConsumo:
		return false
	}
	return f.Estado != EstadoResueltoManual || f.ResueltoComo == nil || *f.ResueltoComo != "cobrado_con_consumo"
}

// llevaInstalacion: el cuerpo lleva la línea del concepto de instalación; sin concepto conocido o en la
// forma anterior a la 0.14.3, se supone que sí (falla cerrado).
func llevaInstalacion(payload []byte, concepto int64) bool {
	_, lineas, ok := lineasDe(payload)
	if !ok || concepto <= 0 {
		return true
	}
	return len(SinLineaInstalacion(lineas, map[int64]bool{concepto: true})) != len(lineas)
}

// otraInstalacionVigente dice, con la orden ya bloqueada, si revivir o corregir el cargo f (con el
// cuerpo cuerpo) dejaría dos cargos vigentes con la línea de instalación (0.14.3 §3). El cuerpo lleva
// la línea si tiene el concepto de instalación; sin concepto conocido, o en la forma anterior en una
// orden de instalación, se supone que sí (falla cerrado). Vigentes: los cobros de A36 de instalación
// (pendiente, facturado, pagado o por confirmar) y el cargo del cierre, sin contar el propio.
func otraInstalacionVigente(ctx context.Context, q *pendientesbd.Queries, f pendientesbd.IntegracionPendiente, cuerpo []byte, concepto int64) (bool, error) {
	if f.Operacion != OpCargo || f.Entidad != "orden" {
		return false, nil
	}
	orden, err := strconv.ParseInt(f.EntidadID, 10, 64)
	if err != nil {
		return false, nil
	}
	tipo, err := q.TipoDeOrdenCampo(ctx, orden)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	a36, err := q.CobrosA36Vigentes(ctx, orden)
	if err != nil {
		return false, err
	}
	conocidos := map[int64]bool{}
	if concepto > 0 {
		conocidos[concepto] = true
	}
	otro := false
	for _, c := range a36 {
		if c.EsInstalacion {
			if concepto <= 0 {
				conocidos[c.ConceptoID] = true
			}
			if c.IdempotencyKey != f.IdempotencyKey {
				otro = true
			}
		}
	}
	lleva := false
	if _, lineas, ok := lineasDe(cuerpo); ok {
		lleva = len(conocidos) == 0 || len(SinLineaInstalacion(lineas, conocidos)) != len(lineas)
	} else {
		lleva = tipo == TipoInstalacion
	}
	if !lleva {
		return false, nil
	}
	if otro {
		return true, nil
	}
	cargos, err := q.CargosDeOrden(ctx, f.EntidadID)
	if err != nil {
		return false, err
	}
	for _, c := range cargos {
		if c.ID == f.ID || !cargoVigente(c) {
			continue
		}
		_, lineas, ok := lineasDe(c.Payload)
		switch {
		case !ok:
			if tipo == TipoInstalacion {
				return true, nil
			}
		case len(conocidos) == 0, len(SinLineaInstalacion(lineas, conocidos)) != len(lineas):
			return true, nil
		}
	}
	return false, nil
}

// conflictoInstalacion: 409 cargo_instalacion_vigente.
func conflictoInstalacion() error {
	return nucleo.Conflicto("cargo_instalacion_vigente",
		"La orden ya tiene otro cobro vigente con la línea de instalación: este cargo no se puede reintentar ni corregir (sería un cobro doble)")
}

// despuesDeHecho: un cargo hecho liga su factura (única en Iris); un consumo hecho resuelve los cargos
// de consumible_cobro que lo esperaban: resueltos si el consumo confirma su cobro, o visibles al
// supervisor si no (nunca salen).
func despuesDeHecho(ctx context.Context, t *auditoria.Tx, f pendientesbd.IntegracionPendiente, ahora time.Time) error {
	q := pendientesbd.New(t)
	switch f.Operacion {
	case OpCargo:
		v, ok := CampoDeRespuesta(f.Respuesta, "factura.id")
		if !ok {
			return nil
		}
		var n json.Number
		if json.Unmarshal(v, &n) != nil {
			return nil
		}
		id, err := n.Int64()
		if err != nil || id <= 0 {
			return nil
		}
		ligadas, err := q.FijarFacturaCargo(ctx, pendientesbd.FijarFacturaCargoParams{FacturaID: id, ID: f.ID})
		if err != nil {
			return err
		}
		if ligadas == 0 {
			slog.WarnContext(ctx, "pendientes: la factura del cargo ya está ligada a otro cobro de Iris", "pendiente", f.ID, "factura", id)
			return nil
		}
		// Bajo 9: un factura.anulada que llegó como aviso antes de ligarla se aplica ahora.
		if orden, err := strconv.ParseInt(f.EntidadID, 10, 64); err == nil {
			avisos, err := q.AvisosDeFactura(ctx, pendientesbd.AvisosDeFacturaParams{OrdenID: orden, Factura: strconv.FormatInt(id, 10)})
			if err != nil {
				return err
			}
			for _, w := range avisos {
				if w == "factura.anulada" {
					return MarcarFacturaAnulada(ctx, t, id, "")
				}
			}
		}
	case OpConsumo:
		esperan, err := q.CargosQueEsperanConsumo(ctx, f.ID)
		if err != nil {
			return err
		}
		for _, c := range esperan {
			// Con varios consumos del producto (uno por actor y bodega, 0.14.0) el cargo depende de todos: se
			// decide cuando todos están hechos, y está cobrado solo si cada uno confirma su cobro.
			listo, cobrado, err := consumosDelCargo(ctx, q, c, f, productoDelCargo(c))
			if err != nil {
				return err
			}
			if !listo {
				continue
			}
			if _, err := decidirEsperaConsumo(ctx, t, q, c, cobrado, ahora); err != nil {
				return err
			}
		}
	}
	return nil
}

// productoDelCargo: el producto_id del cuerpo de un cargo de la forma anterior (0 si no lo trae).
func productoDelCargo(c pendientesbd.IntegracionPendiente) int64 {
	var cuerpo struct {
		ProductoID json.Number `json:"producto_id"`
	}
	_ = json.Unmarshal(c.Payload, &cuerpo)
	producto, _ := cuerpo.ProductoID.Int64()
	return producto
}

// decidirEsperaConsumo cierra la espera de un cargo de un consumible_cobro con sus consumos ya hechos:
// resuelto si todos confirmaron el cobro; si no, fallido e incompleto (visible al supervisor). Devuelve
// el cargo como quedó (el mismo si otra transacción ya lo decidió).
func decidirEsperaConsumo(ctx context.Context, t *auditoria.Tx, q *pendientesbd.Queries, c pendientesbd.IntegracionPendiente,
	cobrado bool, ahora time.Time) (pendientesbd.IntegracionPendiente, error) {
	var g pendientesbd.IntegracionPendiente
	var err error
	if cobrado {
		g, err = q.ResolverEsperaConsumo(ctx, pendientesbd.ResolverEsperaConsumoParams{
			Motivo: "Argos cobró el consumible_cobro con el consumo de la orden", Ahora: ahora, ID: c.ID})
	} else {
		g, err = q.FallarEsperaConsumo(ctx, pendientesbd.FallarEsperaConsumoParams{
			Motivo: "datos incompletos: el consumo no confirmó el cobro del consumible_cobro; revísalo en Argos", ID: c.ID})
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	t.RegistrarCambios("pendiente", g.ID, map[string]any{"estado": c.Estado}, map[string]any{"estado": g.Estado})
	if cobrado {
		err = t.Emitir(EventoResueltoManual, eventoResueltoManual{PendienteID: g.ID, Operacion: g.Operacion, Entidad: g.Entidad,
			EntidadID: g.EntidadID, IdempotencyKey: g.IdempotencyKey, De: c.Estado, Motivo: deref(g.ResueltoMotivo)})
	} else {
		err = t.Emitir(EventoFallido, eventoFallo{eventoPendiente: eventoDe(g), Detalle: deref(g.UltimoError)})
	}
	return g, err
}

// fijarEsperaConsumo deja el cargo de un consumible_cobro esperando a su consumo. Si sus consumos ya
// terminaron (un cierre repetido corrige un cargo que nació incompleto por otra causa, después de que
// el consumo quedó hecho), se decide en el acto: ningún consumo nuevo lo despertaría y quedaría
// bloqueado para siempre.
func fijarEsperaConsumo(ctx context.Context, t *auditoria.Tx, q *pendientesbd.Queries, id int64,
	ahora time.Time) (pendientesbd.IntegracionPendiente, error) {
	c, err := q.FijarEsperaConsumo(ctx, id)
	if err != nil || len(c.DependeDe) == 0 {
		return c, err
	}
	listo, cobrado, err := consumosDelCargo(ctx, q, c, pendientesbd.IntegracionPendiente{}, productoDelCargo(c))
	if err != nil || !listo {
		return c, err
	}
	return decidirEsperaConsumo(ctx, t, q, c, cobrado, ahora)
}

// aprovisionarSinWifi: Argos rechazó un aprovisionar con 422 capacidad_no_disponible (la empresa no
// tiene la marca wifi, §4.3). Si el cuerpo llevaba wifi, se reenvía sin él con otra clave
// (<clave>:sin-wifi) y sin depósito; la clave cifrada del original se descarta (descartarClave).
func aprovisionarSinWifi(ctx context.Context, t *auditoria.Tx, f pendientesbd.IntegracionPendiente,
	ahora time.Time) (pendientesbd.IntegracionPendiente, *pendientesbd.IntegracionPendiente, error) {
	if f.ReemplazoDe != nil || f.ReemplazadoPor != nil {
		return f, nil, nil
	}
	var cuerpo map[string]any
	dec := json.NewDecoder(bytes.NewReader(f.Payload))
	dec.UseNumber()
	if dec.Decode(&cuerpo) != nil {
		return f, nil, nil
	}
	if w, ok := cuerpo["wifi"]; !ok || w == nil {
		return f, nil, nil
	}
	cuerpo["wifi"] = nil
	b, err := json.Marshal(cuerpo)
	if err != nil {
		return f, nil, err
	}
	q := pendientesbd.New(t)
	n, err := q.InsertarPendiente(ctx, pendientesbd.InsertarPendienteParams{
		EmpresaID: f.EmpresaID, Sistema: f.Sistema, Operacion: f.Operacion, IdempotencyKey: f.IdempotencyKey + ":sin-wifi",
		Entidad: f.Entidad, EntidadID: f.EntidadID, Metodo: f.Metodo, Ruta: f.Ruta, Payload: b, DependeDe: []int64{},
		Estado: EstadoPendiente, ProximoIntento: ahora, EventoID: f.EventoID, Actor: f.Actor, TecnicoUsuarioID: f.TecnicoUsuarioID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return f, nil, nil
	}
	if err != nil {
		return f, nil, err
	}
	if err := q.EnlazarReemplazo(ctx, pendientesbd.EnlazarReemplazoParams{De: f.ID, ID: n.ID}); err != nil {
		return f, nil, err
	}
	if err := q.MarcarReemplazado(ctx, pendientesbd.MarcarReemplazadoParams{Por: n.ID, ID: f.ID}); err != nil {
		return f, nil, err
	}
	if f, err = q.PendientePorID(ctx, f.ID); err != nil {
		return f, nil, err
	}
	t.RegistrarCambios("pendiente", n.ID, nil, map[string]any{"operacion": n.Operacion, "estado": n.Estado,
		"idempotency_key": n.IdempotencyKey, "reemplazo_de": f.ID, "sin_wifi": true})
	slog.WarnContext(ctx, "pendientes: aprovisionar sin la marca wifi; se reenvía sin el Wi-Fi", "pendiente", f.ID, "reemplazo", n.ID)
	return f, &n, nil
}

// EntradaQuitarPorAnular: quitar la factura por anular de un cobro por confirmar (sigue por confirmar).
type EntradaQuitarPorAnular struct {
	ID      int64  `json:"-"`
	Version int32  `json:"version" minimum:"1"`
	Motivo  string `json:"motivo" minLength:"5" maxLength:"1000" doc:"Por qué se quita (queda en la auditoría)"`

	factura    argos.FacturaArgos
	errFactura error
}

// QuitarFacturaPorAnular lee la factura por anular en Argos (fuera de la transacción) y la quita del
// cargo si no está anulada; si Argos la ve anulada, no se quita (se habilita «no existe»).
func (m *Modulo) QuitarFacturaPorAnular(ctx context.Context, in EntradaQuitarPorAnular) (Pendiente, error) {
	f, err := m.cargoPorConfirmar(ctx, in.ID)
	if err != nil {
		return Pendiente{}, err
	}
	if f.FacturaPorAnularID == nil {
		return Pendiente{}, nucleo.Conflicto("sin_factura_por_anular", "El cobro no tiene una factura por anular")
	}
	a := contexto.ActorDe(ctx)
	if !a.EsUsuario() {
		return Pendiente{}, nucleo.Prohibido("Lo hace una persona (X-Iris-Actor)")
	}
	in.factura, in.errFactura = m.cfg.Argos.LeerFacturaOrigenIris(ctx, deref64(f.EmpresaID), f.EntidadID,
		argos.BuscarFactura{FacturaID: *f.FacturaPorAnularID}, a.ID)
	return m.quitarPorAnular.Ejecutar(ctx, in)
}

func nuevoQuitarPorAnular(m *Modulo) *auditoria.Caso[EntradaQuitarPorAnular, Pendiente] {
	return auditoria.Nuevo(auditoria.Caso[EntradaQuitarPorAnular, Pendiente]{
		Accion: "integraciones.quitar_factura_por_anular", Permiso: nucleo.PermIntegracionesConfirmarCobro,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaQuitarPorAnular) (Pendiente, error) {
			q := pendientesbd.New(t)
			previo, err := q.PendientePorID(ctx, in.ID)
			if errors.Is(err, pgx.ErrNoRows) {
				return Pendiente{}, nucleo.NoEncontrado("")
			}
			if err != nil {
				return Pendiente{}, err
			}
			if err := bloquearOrden(ctx, q, previo); err != nil {
				return Pendiente{}, err
			}
			f, err := q.PendientePorIDParaActualizar(ctx, in.ID)
			if err != nil {
				return Pendiente{}, err
			}
			if !visible(ctx, f.EmpresaID) {
				return Pendiente{}, nucleo.NoEncontrado("")
			}
			t.FijarEntidad("pendiente", f.ID)
			if f.EmpresaID != nil {
				t.FijarEmpresa(*f.EmpresaID)
			}
			if err := nucleo.ExigirVersion(f.Version, in.Version, "pendiente", f.ID); err != nil {
				return Pendiente{}, err
			}
			if !f.PorConfirmar || f.Estado != EstadoFallidoNegocio || f.FacturaPorAnularID == nil {
				return Pendiente{}, nucleo.Conflicto("cobro_no_por_confirmar", "Este cobro ya no está por confirmar con una factura por anular")
			}
			motivo := strings.TrimSpace(in.Motivo)
			if n := utf8.RuneCountInString(motivo); n < 5 || n > 1000 {
				return Pendiente{}, nucleo.Validacion(nucleo.ErrorCampo{Field: "motivo", Message: "Escribe por qué se quita (5 a 1000 caracteres)", Rule: "longitud"})
			}
			switch {
			case errors.Is(in.errFactura, argos.ErrFacturaNoEncontrada), errors.Is(in.errFactura, argos.ErrLecturaNoDisponible):
				// Solo una lectura 200 con estado distinto de anulada permite quitarla (M2).
				return Pendiente{}, nucleo.ReglaNegocio("factura_no_encontrada", "No se pudo comprobar la factura por anular en Argos: no se quita")
			case in.errFactura != nil:
				return Pendiente{}, nucleo.NoDisponible("No se pudo leer la factura en Argos: reintenta")
			case in.factura.ID != *f.FacturaPorAnularID:
				return Pendiente{}, nucleo.ReglaNegocio("factura_no_encontrada", "Argos devolvió otra factura: no se quita")
			case strings.EqualFold(in.factura.Estado, "anulada"):
				return Pendiente{}, nucleo.Conflicto("factura_anulada",
					"Argos ya la ve anulada: no se quita; confirma que la factura del cobro no existe")
			}
			g, err := q.FijarFacturaPorAnular(ctx, pendientesbd.FijarFacturaPorAnularParams{ID: f.ID, Version: in.Version})
			if errors.Is(err, pgx.ErrNoRows) {
				return Pendiente{}, nucleo.ConflictoVersion("pendiente", f.ID)
			}
			if err != nil {
				return Pendiente{}, err
			}
			t.RegistrarCambios("pendiente", g.ID, map[string]any{"factura_por_anular_id": *f.FacturaPorAnularID},
				map[string]any{"factura_por_anular_id": nil, "motivo": motivo})
			return aPendiente(g), nil
		},
	})
}

// MarcarFacturaAnulada: Argos anuló la factura ligada a un cargo (factura.anulada): el cargo deja de
// contar como vigente. Por factura_id, o por la clave del cargo (cobro de A36 anulado).
func MarcarFacturaAnulada(ctx context.Context, t *auditoria.Tx, factura int64, clave string) error {
	q := pendientesbd.New(t)
	var n int64
	var err error
	if clave != "" {
		n, err = q.MarcarFacturaAnuladaPorClave(ctx, clave)
	} else {
		n, err = q.MarcarFacturaAnulada(ctx, factura)
	}
	if err != nil {
		return err
	}
	if n > 0 {
		t.Anotar(map[string]any{"factura_anulada": factura, "clave": clave})
	}
	return nil
}

// lineaAlResolver: al fijar el cuerpo de un cargo de orden por resolver, con la orden bloqueada, quita
// la línea de instalación si la orden tiene un cobro de A36 de instalación vigente (su concepto). Si no
// queda ninguna línea, o el cargo es de la forma anterior en una instalación, devuelve el motivo para
// dejarlo fallido e incompleto (no sale).
func lineaAlResolver(ctx context.Context, q *pendientesbd.Queries, f pendientesbd.IntegracionPendiente, cuerpo []byte) ([]byte, string, error) {
	if f.Operacion != OpCargo || f.Entidad != "orden" {
		return cuerpo, "", nil
	}
	orden, err := strconv.ParseInt(f.EntidadID, 10, 64)
	if err != nil {
		return cuerpo, "", nil
	}
	if _, err := q.BloquearOrdenCampo(ctx, orden); errors.Is(err, pgx.ErrNoRows) {
		return cuerpo, "", nil
	} else if err != nil {
		return nil, "", err
	}
	a36, err := q.CobrosA36Vigentes(ctx, orden)
	if err != nil {
		return nil, "", err
	}
	instalacion := map[int64]bool{}
	for _, c := range a36 {
		if c.EsInstalacion && c.IdempotencyKey != f.IdempotencyKey {
			instalacion[c.ConceptoID] = true
		}
	}
	if len(instalacion) == 0 {
		return cuerpo, "", nil
	}
	motivo := "datos incompletos: " + MotivoCobroA36Vigente + " (la orden ya tiene un cobro de la instalación)"
	base, lineas, ok := lineasDe(cuerpo)
	if !ok {
		tipo, err := q.TipoDeOrdenCampo(ctx, orden)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return nil, "", err
		}
		if tipo == TipoInstalacion {
			return cuerpo, motivo, nil
		}
		return cuerpo, "", nil
	}
	quedan := SinLineaInstalacion(lineas, instalacion)
	if len(quedan) == len(lineas) {
		return cuerpo, "", nil
	}
	if len(quedan) == 0 {
		return cuerpo, motivo, nil
	}
	base["lineas"] = quedan
	b, err := json.Marshal(base)
	return b, "", err
}

// ErrTitularPorConfirmar: el último cambio de titular de la orden se resolvió a mano sin indicar el
// cliente final: el cobro no puede salir (iría al cliente equivocado).
var ErrTitularPorConfirmar = errors.New("pendientes: titular resuelto a mano sin cliente")

// TrasElTitular: para un cobro de la orden que nace después del cierre (A36), los pasos del titular de
// los que depende (§3.4.8: el cobro sale después de confirmar el titular) y a qué cliente va, con la
// misma lógica que el cargo del cierre (despuesDelTitular): el titular.cliente_id de la respuesta del
// último cambiar_titular vivo (un marcador), el cliente que indicó quien lo resolvió a mano, o nil
// (el de la orden) si no hay cambio de titular. Un cambio resuelto a mano sin cliente:
// ErrTitularPorConfirmar.
func TrasElTitular(ctx context.Context, q pendientesbd.DBTX, orden string) ([]int64, any, error) {
	pasos, err := pendientesbd.New(q).PasosTitularDeOrden(ctx, orden)
	if err != nil {
		return nil, nil, err
	}
	ids := []int64{}
	titular := -1
	var aMano *pendientesbd.IntegracionPendiente
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
	switch {
	case aMano != nil && aMano.ResueltoClienteID == nil:
		return nil, nil, ErrTitularPorConfirmar
	case aMano != nil:
		return ids, *aMano.ResueltoClienteID, nil
	case titular >= 0:
		return ids, Ref(titular, "titular.cliente_id"), nil
	}
	return ids, nil, nil
}

// fallarPorInstalacion deja el cargo fallido e incompleto (no sale) porque llevaría la línea de
// instalación con otro cobro vigente con ella; avisa y bloquea lo que dependa de él.
func fallarPorInstalacion(ctx context.Context, t *auditoria.Tx, q *pendientesbd.Queries, d pendientesbd.IntegracionPendiente, motivo string) error {
	g, err := q.FallarCargoPorInstalacion(ctx, pendientesbd.FallarCargoPorInstalacionParams{Motivo: motivo, ID: d.ID})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	t.RegistrarCambios("pendiente", g.ID, map[string]any{"estado": d.Estado}, map[string]any{"estado": g.Estado, "codigo_error": "cobro_a36_vigente"})
	if err := t.Emitir(EventoFallido, eventoFallo{eventoPendiente: eventoDe(g), Intentos: g.Intentos, Detalle: motivo}); err != nil {
		return err
	}
	return bloquearDependientes(ctx, t, g.ID, g.Estado)
}

// AtarCobrosAlTitular: se abrió un cambio de titular (paso) en la orden después de registrar cobros
// (M-a, §3.4.8). Los cobros de A36 de la orden que aún no salieron pasan a depender del paso nuevo y a ir a
// nombre del titular final (marcador titular.cliente_id). Devuelve las claves de los que ya salieron a
// Argos y aún no se avisaron: esas facturas no se mueven (las rehace una persona en Argos, §3.4.4); quien llama lo avisa.
func AtarCobrosAlTitular(ctx context.Context, t *auditoria.Tx, orden int64, paso int64) ([]string, error) {
	q := pendientesbd.New(t)
	id := strconv.FormatInt(orden, 10)
	vivos, err := q.CargosVivosDeOrden(ctx, id)
	if err != nil {
		return nil, err
	}
	for _, c := range vivos {
		if slices.Contains(c.DependeDe, paso) {
			continue
		}
		base := c.Payload
		if c.PorResolver && len(c.Plantilla) > 0 {
			base = c.Plantilla
		}
		var cuerpo map[string]any
		if err := json.Unmarshal(base, &cuerpo); err != nil {
			return nil, err
		}
		depende := append(append([]int64{}, c.DependeDe...), paso)
		cuerpo["cliente_id"] = Ref(len(depende)-1, "titular.cliente_id")
		plantilla, err := json.Marshal(cuerpo)
		if err != nil {
			return nil, err
		}
		g, err := q.AtarCargoAlTitular(ctx, pendientesbd.AtarCargoAlTitularParams{DependeDe: depende, Plantilla: plantilla, Nunca: NuncaVence, ID: c.ID})
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, err
		}
		t.RegistrarCambios("pendiente", g.ID, map[string]any{"depende_de": c.DependeDe}, map[string]any{"depende_de": g.DependeDe,
			"cliente_id": "titular", "paso": paso})
	}
	return q.CargosYaEnviadosDeOrden(ctx, pendientesbd.CargosYaEnviadosDeOrdenParams{Orden: id, OrdenID: orden})
}

// actualizarClienteCobro: el cobro de A36 (campo.cobros) guarda el titular efectivo del cuerpo fijado.
func actualizarClienteCobro(ctx context.Context, q *pendientesbd.Queries, f pendientesbd.IntegracionPendiente, cuerpo []byte) error {
	if f.Operacion != OpCargo {
		return nil
	}
	var c struct {
		ClienteID json.Number `json:"cliente_id"`
	}
	if json.Unmarshal(cuerpo, &c) != nil {
		return nil
	}
	cliente, err := c.ClienteID.Int64()
	if err != nil || cliente <= 0 {
		return nil
	}
	return q.ActualizarClienteCobro(ctx, pendientesbd.ActualizarClienteCobroParams{ClienteID: cliente, Clave: f.IdempotencyKey})
}

// consumosDelCargo: los consumos de los que depende un cargo que espera el cobro del consumible_cobro.
// listo: todos hechos (f es el que acaba de quedar hecho, aún sin leer de la base con su respuesta).
// cobrado: cada uno confirma en su respuesta un cargo del producto.
func consumosDelCargo(ctx context.Context, q *pendientesbd.Queries, c, f pendientesbd.IntegracionPendiente, producto int64) (bool, bool, error) {
	deps, err := q.PendientesPorIDCompartido(ctx, c.DependeDe)
	if err != nil {
		return false, false, err
	}
	porID := map[int64]pendientesbd.IntegracionPendiente{f.ID: f}
	for _, d := range deps {
		if d.ID != f.ID {
			porID[d.ID] = d
		}
	}
	cobrado := producto > 0
	for _, id := range c.DependeDe {
		d, ok := porID[id]
		if !ok || (d.ID != f.ID && d.Estado != EstadoHecho) {
			return false, false, nil
		}
		if d.Operacion != OpConsumo {
			continue
		}
		cargos, _ := cargosDeRespuesta(d.Respuesta)
		confirma := false
		for _, a := range cargos {
			confirma = confirma || a.ProductoID == producto
		}
		cobrado = cobrado && confirma
	}
	return true, cobrado, nil
}

package pendientes

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

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
// anterior a la 0.14.3), no se conoce el concepto de instalación o no queda ninguna otra línea.
func (m *Modulo) reemplazarCargo(ctx context.Context, t *auditoria.Tx, f pendientesbd.IntegracionPendiente, concepto int64,
	ahora time.Time) (pendientesbd.IntegracionPendiente, *pendientesbd.IntegracionPendiente, error) {
	if concepto <= 0 || f.ReemplazoDe != nil || f.ReemplazadoPor != nil {
		return f, nil, nil
	}
	cuerpo, lineas, ok := lineasDe(f.Payload)
	if !ok {
		return f, nil, nil
	}
	quedan := SinLineaInstalacion(lineas, map[int64]bool{concepto: true})
	if len(quedan) == 0 || len(quedan) == len(lineas) {
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
	n, err := q.InsertarPendiente(ctx, pendientesbd.InsertarPendienteParams{
		EmpresaID: f.EmpresaID, Sistema: f.Sistema, Operacion: OpCargo, IdempotencyKey: clave, Entidad: f.Entidad, EntidadID: f.EntidadID,
		Metodo: f.Metodo, Ruta: f.Ruta, Payload: b, DependeDe: []int64{}, Estado: EstadoPendiente, ProximoIntento: ahora,
		EventoID: f.EventoID, Actor: f.Actor,
	})
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
		"reemplazo_de": f.ID})
	t.RegistrarCambios("pendiente", f.ID, map[string]any{"reemplazado_por": nil}, map[string]any{"reemplazado_por": n.ID})
	slog.WarnContext(ctx, "pendientes: cargo sin capacidad cobro_equipo; sus demás líneas salen como cobro de reemplazo",
		"pendiente", f.ID, "reemplazo", n.ID, "orden", f.EntidadID)
	return f, &n, nil
}

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

// EntradaConfirmarCobro: el supervisor dice si la factura del cargo existe en Argos.
type EntradaConfirmarCobro struct {
	ID      int64   `json:"-"`
	Version int32   `json:"version" minimum:"1"`
	Existe  bool    `json:"existe" doc:"true: la factura existe en Argos (indica su número; Iris la valida leyéndola). false: no existe (el cargo queda fallido y salen sus demás líneas sin la de instalación)"`
	Numero  *string `json:"numero,omitempty" required:"false" minLength:"1" maxLength:"60" doc:"Con existe = true: número de la factura en Argos"`
	Motivo  string  `json:"motivo" minLength:"5" maxLength:"1000" doc:"Qué se revisó en Argos (queda en la auditoría)"`

	factura     argos.FacturaArgos
	errFactura  error
	concepto    int64
	errConcepto error
}

// ConfirmarCobro lee Argos fuera de la transacción (la factura indicada o el concepto de instalación
// para el reemplazo) y aplica la confirmación del supervisor.
func (m *Modulo) ConfirmarCobro(ctx context.Context, in EntradaConfirmarCobro) (Pendiente, error) {
	if !contexto.PermisosDe(ctx).Tiene(string(nucleo.PermIntegracionesConfirmarCobro)) {
		// Antes de leer Argos: sin el permiso no se dispara ninguna lectura.
		return Pendiente{}, nucleo.Prohibido("Confirmar un cobro exige integraciones.confirmar_cobro")
	}
	bd := nucleo.BDDe(ctx)
	if bd == nil {
		return Pendiente{}, nucleo.ErrSinBD
	}
	f, err := pendientesbd.New(bd).PendientePorID(ctx, in.ID)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && !visible(ctx, f.EmpresaID)) {
		return Pendiente{}, nucleo.NoEncontrado("")
	}
	if err != nil {
		return Pendiente{}, err
	}
	if !f.PorConfirmar {
		return Pendiente{}, nucleo.Conflicto("cobro_no_por_confirmar", "Este pendiente no es un cobro por confirmar")
	}
	if m.cfg.Argos == nil {
		return Pendiente{}, nucleo.NoDisponible("Iris no tiene conexión con Argos")
	}
	if in.Existe {
		if in.Numero == nil || strings.TrimSpace(*in.Numero) == "" {
			return Pendiente{}, nucleo.Validacion(nucleo.ErrorCampo{Field: "numero", Message: "Indica el número de la factura en Argos", Rule: "required"})
		}
		in.factura, in.errFactura = m.cfg.Argos.LeerFacturaPorNumero(ctx, deref64(f.EmpresaID), strings.TrimSpace(*in.Numero))
	} else {
		in.concepto, in.errConcepto = m.cfg.Argos.ConceptoInstalacion(ctx, deref64(f.EmpresaID))
	}
	return m.confirmarCobro.Ejecutar(ctx, in)
}

func nuevoConfirmarCobro(m *Modulo) *auditoria.Caso[EntradaConfirmarCobro, Pendiente] {
	return auditoria.Nuevo(auditoria.Caso[EntradaConfirmarCobro, Pendiente]{
		Accion: "integraciones.confirmar_cobro", Permiso: nucleo.PermIntegracionesConfirmarCobro,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaConfirmarCobro) (Pendiente, error) {
			q := pendientesbd.New(t)
			f, err := q.PendientePorIDParaActualizar(ctx, in.ID)
			if errors.Is(err, pgx.ErrNoRows) {
				return Pendiente{}, nucleo.NoEncontrado("")
			}
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
				return Pendiente{}, nucleo.Conflicto("cobro_no_por_confirmar", "Este pendiente no es un cobro por confirmar")
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
			switch {
			case in.errConcepto == nil, errors.Is(in.errConcepto, argos.ErrSinConceptoInstalacion):
			default:
				return Pendiente{}, nucleo.NoDisponible("No se pudo leer Argos para preparar el cobro de reemplazo; intenta de nuevo")
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
			g, reemplazo, err := m.reemplazarCargo(ctx, t, g, in.concepto, ahora)
			if err != nil {
				return Pendiente{}, err
			}
			if reemplazo != nil {
				return aPendiente(g), m.avisarCola(ctx, t.Tx)
			}
			return aPendiente(g), nil
		},
	})
}

// confirmarEmitido valida la factura leída de Argos contra el cargo (misma empresa, mismo servicio,
// origen_id de la orden y líneas iguales al cuerpo) y deja el cargo hecho con ella.
func (m *Modulo) confirmarEmitido(ctx context.Context, t *auditoria.Tx, f pendientesbd.IntegracionPendiente, in EntradaConfirmarCobro,
	motivo string, usuario *int64, ahora time.Time) (Pendiente, error) {
	switch {
	case errors.Is(in.errFactura, argos.ErrFacturaNoEncontrada):
		return Pendiente{}, nucleo.ReglaNegocio("factura_no_encontrada", "Argos no tiene una factura con ese número en la empresa")
	case in.errFactura != nil:
		slog.WarnContext(ctx, "pendientes: no se pudo leer la factura de Argos para confirmar el cobro", "pendiente", f.ID, "err", in.errFactura)
		return Pendiente{}, nucleo.NoDisponible("No se pudo leer la factura en Argos; intenta de nuevo")
	}
	if campo := facturaDistinta(f, in.factura); campo != "" {
		return Pendiente{}, nucleo.NuevoProblema(http.StatusUnprocessableEntity, "factura_no_coincide", "La factura no es de este cobro",
			"La factura "+in.factura.Numero+" de Argos no coincide con el cobro ("+campo+"): revisa el número",
			nucleo.ErrorCampo{Field: "numero", Message: "No coincide: " + campo, Rule: "factura_del_cobro"})
	}
	factura := map[string]any{"id": in.factura.ID, "numero": in.factura.Numero, "estado": in.factura.Estado, "total": in.factura.Total,
		"moneda": in.factura.Moneda}
	respuesta, err := json.Marshal(map[string]any{"documento": "factura", "factura": factura})
	if err != nil {
		return Pendiente{}, err
	}
	q := pendientesbd.New(t)
	g, err := q.ConfirmarCobroEmitido(ctx, pendientesbd.ConfirmarCobroEmitidoParams{Ahora: ahora, Respuesta: respuesta, Motivo: motivo,
		ConfirmadoPor: usuario, ID: f.ID, Version: in.Version})
	if errors.Is(err, pgx.ErrNoRows) {
		return Pendiente{}, nucleo.ConflictoVersion("pendiente", f.ID)
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

// facturaDistinta dice qué no coincide entre la factura de Argos y el cuerpo del cargo, o "".
func facturaDistinta(f pendientesbd.IntegracionPendiente, fa argos.FacturaArgos) string {
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
		n := 1
		if q, ok := conceptoDe(l["cantidad"]); ok {
			n = int(q)
		}
		for range n {
			esperadas = append(esperadas, strconv.FormatInt(c, 10)+"@"+precio)
		}
	}
	halladas := make([]string, 0, len(fa.Lineas))
	for _, l := range fa.Lineas {
		halladas = append(halladas, strconv.FormatInt(l.ConceptoID, 10)+"@"+l.PrecioUnitario)
	}
	sort.Strings(esperadas)
	sort.Strings(halladas)
	if strings.Join(esperadas, ",") != strings.Join(halladas, ",") {
		return "líneas"
	}
	return ""
}

// CargoDelCierreConInstalacion dice si el cargo del cierre de la orden (iris:cargo:<orden_id>) está
// vigente con la línea de instalación (concepto). Vigente: todo menos fallido (salvo «por confirmar»)
// y resuelto a mano. Con el concepto desconocido (0) o un cargo de la forma anterior a la 0.14.3,
// cuenta como si la llevara (lo más estricto).
func CargoDelCierreConInstalacion(ctx context.Context, q pendientesbd.DBTX, orden string, concepto int64) (bool, error) {
	f, err := pendientesbd.New(q).PendientePorClave(ctx, "iris:cargo:"+orden)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if (f.Estado == EstadoFallidoNegocio && !f.PorConfirmar) || f.Estado == EstadoResueltoManual {
		return false, nil
	}
	_, lineas, ok := lineasDe(f.Payload)
	if !ok || concepto <= 0 {
		return true, nil
	}
	return len(SinLineaInstalacion(lineas, map[int64]bool{concepto: true})) != len(lineas), nil
}

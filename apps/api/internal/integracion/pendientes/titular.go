package pendientes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion"
	pendientesbd "github.com/HCDestroyer/iris/apps/api/internal/integracion/pendientes/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
)

// Paso «Confirmar titular y plan» de la orden de instalación (contrato argosisp.md 0.10.0 §3.4,
// pedidos B18 y B19). Campo (I2) registra lo que hizo el técnico y encola aquí cada llamada, en la
// transacción de su caso de uso. Las llamadas salen en orden y con dependencias: identidad (A41) o
// cambio de titular (A39) → completar datos (A43) → cambiar el plan (A40) → activar (al cerrar). Si
// una falla, las siguientes quedan bloqueadas (integracion.pendiente_bloqueado) y la activación no
// sale: nunca se activa con el titular o el plan equivocados.

// OperacionesTitular son las operaciones del paso, en el orden del contrato.
var OperacionesTitular = []string{OpRegistrarIdentidad, OpCambiarTitular, OpCompletarDatos, OpCambiarPlan}

// EsPasoIdentidad: A41 y A39 llevan la identidad del titular (foto del documento).
func EsPasoIdentidad(operacion string) bool {
	return operacion == OpRegistrarIdentidad || operacion == OpCambiarTitular
}

// Claves de idempotencia del contrato (§3.4.3 a §3.4.6).
func ClaveIdentidad(orden int64, toma int32) string { return claveTitular("identidad", orden, toma) }
func ClaveTitular(orden int64, cambio int32) string { return claveTitular("titular", orden, cambio) }
func ClaveDatos(orden int64, n int32) string        { return claveTitular("datos", orden, n) }
func ClavePlan(orden int64, cambio int32) string    { return claveTitular("plan", orden, cambio) }
func claveTitular(tipo string, orden int64, n int32) string {
	return fmt.Sprintf("iris:%s:%d:%d", tipo, orden, n)
}

// ActorUsuario es X-Iris-Actor de una persona de Iris (§7.1).
func ActorUsuario(usuario int64) string { return "usuario:" + strconv.FormatInt(usuario, 10) }

// PasoTitular es una llamada del paso.
type PasoTitular struct {
	EmpresaID *int64
	OrdenID   int64
	Operacion string
	Clave     string
	Ruta      string
	// Cuerpo sin nada sensible (va en payload; lo ven el panel y la auditoría). Puede llevar
	// marcadores (Ref) de sus dependencias.
	Cuerpo map[string]any
	// Transito: lo sensible (documento, teléfono, correo, NIT/CUI y fotos), cifrado en el depósito
	// de tránsito; nil = nada sensible.
	Transito *Transito
	// TransitoID: reusar un depósito ya guardado (la revisión desde el panel manda la foto tomada
	// en sitio). Excluye a Transito.
	TransitoID *uuid.UUID
	// TomadoEn: cuándo se tomó (los 7 días del depósito cuentan desde aquí).
	TomadoEn  time.Time
	DependeDe []int64
	// Actor: X-Iris-Actor (usuario:<id>), obligatorio en §3.4.
	Actor string
}

// EncolarPaso guarda lo sensible en el depósito de tránsito y encola la llamada en la transacción
// del caso de uso. Si una dependencia ya falló (o está bloqueada), el paso nace bloqueado. Sin
// cifrador no guarda nada sensible (503 no_disponible).
func EncolarPaso(ctx context.Context, t *auditoria.Tx, cif *nucleo.Cifrador, p PasoTitular, aviso Encolador) (Registro, error) {
	if p.Actor == "" {
		return Registro{}, errors.New("pendientes.EncolarPaso: el paso del titular lleva X-Iris-Actor")
	}
	if p.Transito != nil && p.TransitoID != nil {
		return Registro{}, errors.New("pendientes.EncolarPaso: Transito y TransitoID a la vez")
	}
	transito := p.TransitoID
	if p.Transito != nil {
		id, err := GuardarTransito(ctx, t, cif, p.EmpresaID, p.OrdenID, *p.Transito, p.TomadoEn)
		if err != nil {
			return Registro{}, err
		}
		transito = &id
	}
	cuerpo, err := json.Marshal(p.Cuerpo)
	if err != nil {
		return Registro{}, err
	}
	r, creado, err := Encolar(ctx, t, Accion{
		EmpresaID: p.EmpresaID, Operacion: p.Operacion, IdempotencyKey: p.Clave, Entidad: "orden",
		EntidadID: strconv.FormatInt(p.OrdenID, 10), Ruta: p.Ruta, Cuerpo: cuerpo, DependeDe: p.DependeDe,
		Desde: p.TomadoEn, Actor: p.Actor, TransitoID: transito,
	}, aviso)
	if err != nil {
		return Registro{}, err
	}
	if !creado {
		// La clave sale de los contadores de la orden (con candado): nunca se repite.
		return Registro{}, fmt.Errorf("pendientes.EncolarPaso: la clave %s ya existía", p.Clave)
	}
	return r, bloquearSiDependenciaFallida(ctx, t, r.DependeDe)
}

// bloquearSiDependenciaFallida: un pendiente nace detrás de una dependencia que ya falló, se agotó
// o está bloqueada: queda bloqueado (y avisa) en vez de esperar en silencio.
func bloquearSiDependenciaFallida(ctx context.Context, t *auditoria.Tx, depende []int64) error {
	if len(depende) == 0 {
		return nil
	}
	deps, err := pendientesbd.New(t).PendientesPorIDCompartido(ctx, depende)
	if err != nil {
		return err
	}
	for _, d := range deps {
		switch d.Estado {
		case EstadoFallidoNegocio, EstadoAgotado, EstadoBloqueado:
			return bloquearDependientes(ctx, t, d.ID, d.Estado)
		}
	}
	return nil
}

// ReemplazarPaso: otra toma reemplaza un paso de identidad que Argos rechazó (o que se agotó). Lo
// que esperaba al viejo pasa a esperar al nuevo y el viejo queda resuelto_manual con el motivo
// (auditado, integracion.pendiente_resuelto_manual). El depósito de tránsito del viejo no se borra
// aquí: puede ser el mismo que usa el nuevo (revisión desde el panel); vence a los 7 días.
func ReemplazarPaso(ctx context.Context, t *auditoria.Tx, viejo, nuevo int64, motivo string, usuario *int64, ahora time.Time) error {
	q := pendientesbd.New(t)
	antes, err := q.PendientePorIDParaActualizar(ctx, viejo)
	if err != nil {
		return err
	}
	filas, err := q.RedirigirDependientes(ctx, pendientesbd.RedirigirDependientesParams{Viejo: viejo, Nuevo: nuevo})
	if err != nil {
		return err
	}
	for _, f := range filas {
		t.RegistrarCambios("pendiente", f.ID, map[string]any{"depende_de": viejo}, map[string]any{"depende_de": nuevo})
	}
	f, err := q.ReemplazarPendiente(ctx, pendientesbd.ReemplazarPendienteParams{Motivo: motivo, ResueltoPor: usuario, Como: ComoReemplazado, Ahora: ahora, ID: viejo})
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("pendientes.ReemplazarPaso: el pendiente %d no está rechazado (%s)", viejo, antes.Estado)
	}
	if err != nil {
		return err
	}
	t.RegistrarCambios("pendiente", f.ID, map[string]any{"estado": antes.Estado}, map[string]any{"estado": f.Estado, "resuelto_motivo": motivo})
	return t.Emitir(EventoResueltoManual, eventoResueltoManual{
		PendienteID: f.ID, Operacion: f.Operacion, Entidad: f.Entidad, EntidadID: f.EntidadID,
		IdempotencyKey: f.IdempotencyKey, De: antes.Estado, Motivo: motivo, UsuarioID: usuario,
	})
}

// DarPorValidado: un supervisor valida la identidad desde el panel (ordenes.validar_identidad) y
// el paso de identidad rechazado ya no se puede volver a mandar (su foto venció o se borró). El
// paso queda resuelto_manual con el motivo y lo que lo esperaba sigue (la activación sale sin la
// identidad en Argos: el permiso solo sirve para activar, §3.4.8). Un dependiente que esperaba su
// respuesta (marcadores: identidad_id en completar-datos) nunca la tendrá: queda incompleto para
// resolverlo a mano.
func DarPorValidado(ctx context.Context, t *auditoria.Tx, id int64, motivo string, usuario *int64, ahora time.Time) error {
	q := pendientesbd.New(t)
	antes, err := q.PendientePorIDParaActualizar(ctx, id)
	if err != nil {
		return err
	}
	f, err := q.ReemplazarPendiente(ctx, pendientesbd.ReemplazarPendienteParams{Motivo: motivo, ResueltoPor: usuario, Como: ComoValidadoPanel, Ahora: ahora, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("pendientes.DarPorValidado: el pendiente %d no está rechazado (%s)", id, antes.Estado)
	}
	if err != nil {
		return err
	}
	t.RegistrarCambios("pendiente", f.ID, map[string]any{"estado": antes.Estado}, map[string]any{"estado": f.Estado, "resuelto_motivo": motivo})
	if err := t.Emitir(EventoResueltoManual, eventoResueltoManual{
		PendienteID: f.ID, Operacion: f.Operacion, Entidad: f.Entidad, EntidadID: f.EntidadID,
		IdempotencyKey: f.IdempotencyKey, De: antes.Estado, Motivo: motivo, UsuarioID: usuario,
	}); err != nil {
		return err
	}
	if err := borrarTransito(ctx, t, f, "resuelto_manual", ahora); err != nil {
		return err
	}
	dependientes, err := q.DependientesAbiertos(ctx, f.ID)
	if err != nil {
		return err
	}
	for _, d := range dependientes {
		if d.PorResolver {
			if err := fallarDependienteResuelto(ctx, t, q, d, f.ID); err != nil {
				return err
			}
		}
	}
	return desbloquear(ctx, t, f.ID, ahora)
}

// PasoDeTitular es lo que campo lee de un paso (sin el cuerpo ni la respuesta: el técnico no ve
// posible_duplicado ni datos de otro cliente, B4).
type PasoDeTitular struct {
	ID          int64
	Operacion   string
	Estado      string
	CodigoError *string
	DependeDe   []int64
	// TransitoID: el depósito del paso (nil = sin datos sensibles).
	TransitoID *uuid.UUID
	// TransitoVigente: el depósito sigue con sus datos (no se entregó, borró ni venció).
	TransitoVigente bool
	ActualizadoEn   time.Time
	// ResueltoComo: con resuelto_manual, cómo (ComoManual, ComoReemplazado…).
	ResueltoComo *string
}

// PasosDeOrden lista los pasos del titular de una orden, en el orden en que se encolaron. q puede
// ser una transacción o el pool.
func PasosDeOrden(ctx context.Context, q pendientesbd.DBTX, orden int64) ([]PasoDeTitular, error) {
	b := pendientesbd.New(q)
	filas, err := b.PasosTitularDeOrden(ctx, strconv.FormatInt(orden, 10))
	if err != nil {
		return nil, err
	}
	salida := make([]PasoDeTitular, 0, len(filas))
	for _, f := range filas {
		p := PasoDeTitular{ID: f.ID, Operacion: f.Operacion, Estado: f.Estado, CodigoError: f.CodigoError,
			DependeDe: f.DependeDe, ActualizadoEn: f.UpdatedAt, ResueltoComo: f.ResueltoComo}
		if f.TransitoID.Valid {
			id := uuid.UUID(f.TransitoID.Bytes)
			p.TransitoID = &id
			vigente, err := b.TransitoVigente(ctx, f.TransitoID.Bytes)
			if err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return nil, err
			}
			p.TransitoVigente = vigente
		}
		salida = append(salida, p)
	}
	return salida, nil
}

// Fallido: el paso no llegó a Argos y no saldrá solo (rechazo de negocio o reintentos agotados).
func (p PasoDeTitular) Fallido() bool {
	return p.Estado == EstadoFallidoNegocio || p.Estado == EstadoAgotado
}

// Rechazado: Argos lo rechazó (fallido_negocio). Solo esto se reemplaza con otra toma; lo agotado
// se reintenta con su misma clave.
func (p PasoDeTitular) Rechazado() bool { return p.Estado == EstadoFallidoNegocio }

// Formas de resolver un pendiente del titular (integracion.pendientes.resuelto_como).
const (
	ComoManual        = "manual"
	ComoReemplazado   = "reemplazado"
	ComoCancelada     = "orden_cancelada"
	ComoFallida       = "orden_fallida"
	ComoValidadoPanel = "validado_panel"
)

// CodigoTitularNoCoincide: Argos encontró el documento en otro cliente con otro nombre (A39, A41).
const CodigoTitularNoCoincide = "titular_no_coincide"

// EventoPasoTrasCancelar: un paso del titular que estaba saliendo cuando la orden se canceló o
// falló quedó hecho en Argos: supervisión revisa (no se deshace solo).
const EventoPasoTrasCancelar = "orden.paso_titular_tras_cancelar"

// despuesDelPasoTitular, en la transacción del resultado de un intento:
//   - titular_no_coincide (A39 o A41): la orden queda en revisión de un supervisor (marca persistente
//     en campo.orden_titular);
//   - hecho con la orden ya cancelada o fallida: avisa a supervisión (lo hecho no se deshace).
func despuesDelPasoTitular(ctx context.Context, t *auditoria.Tx, f pendientesbd.IntegracionPendiente, ahora time.Time) error {
	if !slices.Contains(OperacionesTitular, f.Operacion) || f.Entidad != "orden" {
		return nil
	}
	orden, err := strconv.ParseInt(f.EntidadID, 10, 64)
	if err != nil {
		return nil
	}
	q := pendientesbd.New(t)
	switch {
	case f.Estado == EstadoFallidoNegocio && EsPasoIdentidad(f.Operacion) && deref(f.CodigoError) == CodigoTitularNoCoincide:
		n, err := q.MarcarTitularEnRevision(ctx, pendientesbd.MarcarTitularEnRevisionParams{Ahora: ahora, OrdenID: orden})
		if err != nil || n == 0 {
			return err
		}
		t.RegistrarCambios("orden_titular", orden, map[string]any{"en_revision": false}, map[string]any{"en_revision": true})
	case f.Estado == EstadoHecho:
		estado, err := q.EstadoDeOrden(ctx, orden)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if estado == "cancelada" || estado == "fallida" {
			slog.WarnContext(ctx, "pendientes: paso del titular hecho en Argos con la orden ya terminada", "pendiente", f.ID, "orden", orden, "estado_orden", estado)
			return t.Emitir(EventoPasoTrasCancelar, map[string]any{"orden_id": orden, "pendiente_id": f.ID, "operacion": f.Operacion, "estado_orden": estado})
		}
	}
	return nil
}

// DetenerPasos: la orden se canceló o falló (como = ComoCancelada o ComoFallida). En la transacción
// de esa acción, sus pasos del titular que no salieron quedan resuelto_manual con el motivo, su
// depósito se borra (cancelado) y se avisa (integracion.pendiente_resuelto_manual). Un paso en_curso
// termina (si sale bien, despuesDelPasoTitular avisa a supervisión); lo hecho no se deshace.
func DetenerPasos(ctx context.Context, t *auditoria.Tx, orden int64, como string, usuario *int64, ahora time.Time) ([]int64, error) {
	if como != ComoCancelada && como != ComoFallida {
		return nil, fmt.Errorf("pendientes.DetenerPasos: motivo %q", como)
	}
	filas, err := pendientesbd.New(t).DetenerPasosTitular(ctx, pendientesbd.DetenerPasosTitularParams{
		Motivo: como, ResueltoPor: usuario, Ahora: ahora, OrdenID: strconv.FormatInt(orden, 10)})
	if err != nil {
		return nil, err
	}
	ids := make([]int64, 0, len(filas))
	for _, f := range filas {
		ids = append(ids, f.ID)
		t.RegistrarCambios("pendiente", f.ID, nil, map[string]any{"estado": f.Estado, "resuelto_como": como})
		if err := borrarTransito(ctx, t, f, "cancelado", ahora); err != nil {
			return nil, err
		}
		if err := t.Emitir(EventoResueltoManual, eventoResueltoManual{
			PendienteID: f.ID, Operacion: f.Operacion, Entidad: f.Entidad, EntidadID: f.EntidadID,
			IdempotencyKey: f.IdempotencyKey, De: "abierto", Motivo: como, UsuarioID: usuario,
		}); err != nil {
			return nil, err
		}
	}
	return ids, nil
}

// --- Purga diaria del depósito de tránsito (job propio, único por día) -----------------------

// ArgsPurgarTransito borra los depósitos con más de 7 días (contenido y llave).
type ArgsPurgarTransito struct{}

// Kind implementa river.JobArgs.
func (ArgsPurgarTransito) Kind() string { return "integracion.purgar_transito" }

// InsertOpts: cola de integración, único por día.
func (ArgsPurgarTransito) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: integracion.ColaIntegracion, MaxAttempts: 5, UniqueOpts: river.UniqueOpts{ByPeriod: 24 * time.Hour}}
}

type trabajadorPurga struct {
	river.WorkerDefaults[ArgsPurgarTransito]
	m  *Modulo
	bd nucleo.BD
}

func (w *trabajadorPurga) Work(ctx context.Context, _ *river.Job[ArgsPurgarTransito]) error {
	ctx = nucleo.ConBD(contexto.Sistema(ctx, contexto.CanalWorker), w.bd)
	_, err := w.m.PurgarTransitoVencido(ctx)
	return err
}

// PurgarTransitoVencido borra los depósitos con más de 7 días de tomados (§3.4.8).
func (m *Modulo) PurgarTransitoVencido(ctx context.Context) (int64, error) {
	n, err := PurgarTransito(ctx, pendientesbd.New(nucleo.BDDe(ctx)), m.ahora())
	if err == nil && n > 0 {
		slog.InfoContext(ctx, "pendientes: depósitos de tránsito vencidos borrados", "cuantos", n)
	}
	return n, err
}

// TrabajoPurgaTransito corre la purga una vez al día (y al arrancar).
func TrabajoPurgaTransito() *river.PeriodicJob {
	return river.NewPeriodicJob(river.PeriodicInterval(24*time.Hour),
		func() (river.JobArgs, *river.InsertOpts) { return ArgsPurgarTransito{}, nil },
		&river.PeriodicJobOpts{RunOnStart: true})
}

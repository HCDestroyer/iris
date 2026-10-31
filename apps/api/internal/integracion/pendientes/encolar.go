package pendientes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	pendientesbd "github.com/HCDestroyer/iris/apps/api/internal/integracion/pendientes/bd"
)

// Accion es una acción hacia Argos para Encolar.
type Accion struct {
	EmpresaID *int64
	// Sistema: argosisp por defecto.
	Sistema   string
	Operacion string
	// IdempotencyKey del contrato (iris:<operacion>:<id>): encolar dos veces la misma no duplica.
	IdempotencyKey string
	Entidad        string
	EntidadID      string
	// Metodo: POST por defecto.
	Metodo string
	Ruta   string
	// Cuerpo serializado UNA vez (Argos guarda la huella del cuerpo crudo). Puede llevar
	// marcadores (Ref) que se resuelven con la respuesta de sus dependencias.
	Cuerpo    []byte
	DependeDe []int64
	// Desde cuándo se puede intentar (el reloj de quien encola); cero = ahora.
	Desde time.Time
	// Verificar: campos de identidad del cuerpo que la respuesta del 2xx, si los trae, debe
	// repetir igual (p. ej. codigo_externo en POST /clientes). Si no coinciden, el pendiente
	// queda fallido_negocio (respuesta_inconsistente) y no resuelve a sus dependientes. Deben
	// estar en CamposRespuesta (si no, nunca se guardarían para comparar).
	Verificar []string
	// TecnicoUsuarioID: el técnico cuyo vínculo con Argos va en el cuerpo (contrato 0.5.0 §3.3).
	TecnicoUsuarioID *int64
	// Esperando: el cuerpo aún no está completo (p. ej. esperando_firma: falta que suba la firma).
	// El pendiente nace bloqueado con ese motivo y no sale hacia Argos hasta CompletarEsperando.
	Esperando string
}

// Registro es lo que quien encola necesita leer de un pendiente (sin usar pendientesbd).
type Registro struct {
	ID             int64
	Estado         string
	Operacion      string
	IdempotencyKey string
	Metodo         string
	Ruta           string
	// Payload: el cuerpo que se manda (con marcadores mientras PorResolver).
	Payload     json.RawMessage
	PorResolver bool
	// Respuesta del primer 2xx de Argos (sin lo sensible), o nil (pendientes de antes de
	// guardarla, sin cuerpo o demasiado grande).
	Respuesta json.RawMessage
	HechoEn   *time.Time
	DependeDe []int64
	// UltimoError: resumen del último fallo (p. ej. respuesta_inconsistente: …).
	UltimoError *string
	// CodigoError: type del último error de Argos (o el código local, p. ej. firma_invalida).
	CodigoError      *string
	EmpresaID        *int64
	EntidadID        string
	TecnicoUsuarioID *int64
	ActualizadoEn    time.Time
}

func aRegistro(f pendientesbd.IntegracionPendiente) Registro {
	return Registro{
		ID: f.ID, Estado: f.Estado, Operacion: f.Operacion, IdempotencyKey: f.IdempotencyKey, Metodo: f.Metodo, Ruta: f.Ruta,
		Payload: json.RawMessage(f.Payload), PorResolver: f.PorResolver, Respuesta: json.RawMessage(f.Respuesta),
		HechoEn: f.HechoEn, DependeDe: f.DependeDe, UltimoError: f.UltimoError,
		CodigoError: f.CodigoError, EmpresaID: f.EmpresaID, EntidadID: f.EntidadID, TecnicoUsuarioID: f.TecnicoUsuarioID,
		ActualizadoEn: f.UpdatedAt,
	}
}

// PorClave lee un pendiente por su Idempotency-Key (pgx.ErrNoRows si no existe). q puede ser
// una transacción o el pool.
func PorClave(ctx context.Context, q pendientesbd.DBTX, clave string) (Registro, error) {
	f, err := pendientesbd.New(q).PendientePorClave(ctx, clave)
	if err != nil {
		return Registro{}, err
	}
	return aRegistro(f), nil
}

// Encolar inserta una acción hacia Argos en la transacción del caso de uso que la origina
// (idempotente por su Idempotency-Key: si ya existe, devuelve la existente con creado = false)
// y avisa a la cola con aviso (si no es nil). Si el cuerpo lleva marcadores y sus dependencias
// ya están hechas, lo resuelve ahí mismo. Para comercial, campo y quien escriba en Argos.
func Encolar(ctx context.Context, t *auditoria.Tx, n Accion, aviso Encolador) (r Registro, creado bool, err error) {
	if n.Operacion == "" || n.IdempotencyKey == "" || n.Entidad == "" || n.EntidadID == "" || n.Ruta == "" {
		return Registro{}, false, errors.New("pendientes.Encolar: faltan operacion, clave, entidad o ruta")
	}
	if n.Sistema == "" {
		n.Sistema = "argosisp"
	}
	if n.Metodo == "" {
		n.Metodo = "POST"
	}
	if n.DependeDe == nil {
		n.DependeDe = []int64{}
	}
	if n.Desde.IsZero() {
		n.Desde = time.Now()
	}
	if len(n.Cuerpo) == 0 {
		n.Cuerpo = []byte("{}")
	}
	if !json.Valid(n.Cuerpo) {
		return Registro{}, false, errors.New("pendientes.Encolar: el cuerpo no es JSON")
	}
	for _, c := range n.Verificar {
		if !camposPermitidos[c] {
			return Registro{}, false, fmt.Errorf("pendientes.Encolar: no se puede verificar %q (no está en CamposRespuesta)", c)
		}
		if _, ok := CampoDeRespuesta(n.Cuerpo, c); !ok {
			return Registro{}, false, fmt.Errorf("pendientes.Encolar: el cuerpo no trae %q para verificar", c)
		}
	}
	porResolver := tieneMarcadores(n.Cuerpo)
	var plantilla []byte
	proximo := n.Desde.UTC()
	if porResolver {
		if err := validarPlantilla(n.Cuerpo, len(n.DependeDe)); err != nil {
			return Registro{}, false, err
		}
		// Candado contra un worker de antes de los marcadores: un pendiente por resolver nunca
		// vence (TomarVencido de cualquier versión lo salta); al resolverse vuelve a «ahora».
		plantilla, proximo = n.Cuerpo, NuncaVence
	}
	q := pendientesbd.New(t)
	// Dependencias: deben existir y ser de la misma empresa (error de programación si no). El
	// candado compartido evita perder la resolución si una termina en otra transacción.
	if err := exigirDependencias(ctx, q, n); err != nil {
		return Registro{}, false, err
	}
	estado := EstadoPendiente
	var ultimo *string
	if n.Esperando != "" {
		if porResolver {
			return Registro{}, false, errors.New("pendientes.Encolar: Esperando no admite marcadores")
		}
		estado, ultimo = EstadoBloqueado, &n.Esperando
	}
	f, err := q.InsertarPendiente(ctx, pendientesbd.InsertarPendienteParams{
		EmpresaID: n.EmpresaID, Sistema: n.Sistema, Operacion: n.Operacion, IdempotencyKey: n.IdempotencyKey,
		Entidad: n.Entidad, EntidadID: n.EntidadID, Metodo: n.Metodo, Ruta: n.Ruta, Payload: n.Cuerpo,
		DependeDe: n.DependeDe, Estado: estado, UltimoError: ultimo, ProximoIntento: proximo,
		Plantilla: plantilla, PorResolver: porResolver, Verificar: n.Verificar, TecnicoUsuarioID: n.TecnicoUsuarioID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		f, err = q.PendientePorClave(ctx, n.IdempotencyKey)
		return aRegistro(f), false, err
	}
	if err != nil {
		return Registro{}, false, err
	}
	t.RegistrarCambios("pendiente", f.ID, nil, map[string]any{"operacion": f.Operacion, "estado": f.Estado, "idempotency_key": f.IdempotencyKey})
	if porResolver {
		if f, err = resolverSiListo(ctx, t, f, n.Desde.UTC()); err != nil {
			return Registro{}, false, err
		}
	}
	if aviso != nil {
		if err := aviso(ctx, t.Tx, ArgsProcesar{}); err != nil {
			return Registro{}, false, err
		}
	}
	return aRegistro(f), true, nil
}

// resolverDependientes: el pendiente id quedó hecho; resuelve el cuerpo de sus dependientes con
// marcadores cuyas dependencias están todas hechas (en la transacción del resultado).
func resolverDependientes(ctx context.Context, t *auditoria.Tx, id int64, ahora time.Time) error {
	filas, err := pendientesbd.New(t).DependientesPorResolver(ctx, id)
	if err != nil {
		return err
	}
	for _, f := range filas {
		if _, err := resolverSiListo(ctx, t, f, ahora); err != nil {
			return err
		}
	}
	return nil
}

// resolverSiListo resuelve el cuerpo de f si todas sus dependencias están hechas; si no, lo
// deja como está. Si falta un dato en una respuesta, f queda fallido_negocio (y se avisa).
func resolverSiListo(ctx context.Context, t *auditoria.Tx, f pendientesbd.IntegracionPendiente, ahora time.Time) (pendientesbd.IntegracionPendiente, error) {
	q := pendientesbd.New(t)
	deps, err := q.PendientesPorIDCompartido(ctx, f.DependeDe)
	if err != nil {
		return f, err
	}
	porID := map[int64]pendientesbd.IntegracionPendiente{}
	for _, d := range deps {
		porID[d.ID] = d
	}
	respuestas := make([][]byte, len(f.DependeDe))
	for i, id := range f.DependeDe {
		d, ok := porID[id]
		if !ok || d.Estado != EstadoHecho {
			return f, nil // falta alguna: se resuelve cuando la última quede hecha
		}
		respuestas[i] = d.Respuesta
	}
	plantilla := f.Plantilla
	if len(plantilla) == 0 {
		plantilla = f.Payload
	}
	cuerpo, err := resolverPlantilla(plantilla, respuestas)
	var er errResolucion
	if errors.As(err, &er) {
		motivo := fmt.Sprintf("%s: la respuesta de la dependencia no trae el dato", er.motivo)
		g, err := q.FallarResolucion(ctx, pendientesbd.FallarResolucionParams{ID: f.ID, Motivo: &motivo})
		if err != nil {
			return f, err
		}
		t.RegistrarCambios("pendiente", g.ID, map[string]any{"estado": f.Estado}, map[string]any{"estado": g.Estado, "datos_incompletos": true})
		if err := t.Emitir(EventoFallido, eventoFallo{eventoPendiente: eventoDe(g), Intentos: g.Intentos, Detalle: motivo}); err != nil {
			return g, err
		}
		return g, bloquearDependientes(ctx, t, g.ID, g.Estado)
	}
	if err != nil {
		return f, err
	}
	g, err := q.FijarCuerpoResuelto(ctx, pendientesbd.FijarCuerpoResueltoParams{ID: f.ID, Payload: cuerpo, Ahora: ahora})
	if err != nil {
		return f, err
	}
	t.RegistrarCambios("pendiente", g.ID, map[string]any{"por_resolver": true}, map[string]any{"por_resolver": false})
	return g, nil
}

// NuncaVence es el proximo_intento de un pendiente por resolver (ver Encolar).
var NuncaVence = time.Date(9999, 12, 31, 0, 0, 0, 0, time.UTC)

// exigirDependencias: cada id de DependeDe existe (sin repetir) y es de la misma empresa que la
// acción. Toma un candado compartido sobre ellas (PendientesPorIDCompartido).
func exigirDependencias(ctx context.Context, q *pendientesbd.Queries, n Accion) error {
	if len(n.DependeDe) == 0 {
		return nil
	}
	deps, err := q.PendientesPorIDCompartido(ctx, n.DependeDe)
	if err != nil {
		return err
	}
	vistos := map[int64]bool{}
	for _, id := range n.DependeDe {
		if vistos[id] {
			return fmt.Errorf("pendientes.Encolar: dependencia %d repetida", id)
		}
		vistos[id] = true
	}
	if len(deps) != len(n.DependeDe) {
		return fmt.Errorf("pendientes.Encolar: alguna dependencia de %v no existe", n.DependeDe)
	}
	for _, d := range deps {
		if !mismaEmpresa(d.EmpresaID, n.EmpresaID) {
			return fmt.Errorf("pendientes.Encolar: la dependencia %d es de otra empresa", d.ID)
		}
	}
	return nil
}

func mismaEmpresa(a, b *int64) bool {
	return (a == nil && b == nil) || (a != nil && b != nil && *a == *b)
}

// resolverAtascados es la red de seguridad del job: resuelve los por resolver cuyas
// dependencias ya están todas hechas. Devuelve cuántos tocó.
func resolverAtascados(ctx context.Context, t *auditoria.Tx, ahora time.Time) (int, error) {
	filas, err := pendientesbd.New(t).PorResolverListos(ctx, 200)
	if err != nil {
		return 0, err
	}
	for _, f := range filas {
		if _, err := resolverSiListo(ctx, t, f, ahora); err != nil {
			return 0, err
		}
	}
	return len(filas), nil
}

// CompletarEsperando fija el cuerpo final de un pendiente que esperaba algo de Iris (Accion.Esperando
// = motivo) y lo devuelve a la cola, una sola vez (auditado en la transacción de quien llama). ok =
// false si ya no esperaba eso (otro lo completó o falló).
func CompletarEsperando(ctx context.Context, t *auditoria.Tx, id int64, motivo string, cuerpo []byte, ahora time.Time, aviso Encolador) (Registro, bool, error) {
	if !json.Valid(cuerpo) {
		return Registro{}, false, errors.New("pendientes.CompletarEsperando: el cuerpo no es JSON")
	}
	f, err := pendientesbd.New(t).CompletarEsperando(ctx, pendientesbd.CompletarEsperandoParams{Payload: cuerpo, Ahora: ahora.UTC(), ID: id, Motivo: motivo})
	if errors.Is(err, pgx.ErrNoRows) {
		return Registro{}, false, nil
	}
	if err != nil {
		return Registro{}, false, err
	}
	t.RegistrarCambios("pendiente", f.ID, map[string]any{"estado": EstadoBloqueado, "esperando": motivo}, map[string]any{"estado": f.Estado})
	if aviso != nil {
		if err := aviso(ctx, t.Tx, ArgsProcesar{}); err != nil {
			return Registro{}, false, err
		}
	}
	return aRegistro(f), true, nil
}

// FallarEsperando deja fallido_negocio (sin llamar a Argos) un pendiente cuya espera terminó mal
// (p. ej. firma_invalida), con el código y el detalle, y avisa (integracion.pendiente_fallido).
func FallarEsperando(ctx context.Context, t *auditoria.Tx, id int64, motivo, codigo, detalle string) (Registro, bool, error) {
	f, err := pendientesbd.New(t).FallarEsperando(ctx, pendientesbd.FallarEsperandoParams{Detalle: detalle, Codigo: codigo, ID: id, Motivo: motivo})
	if errors.Is(err, pgx.ErrNoRows) {
		return Registro{}, false, nil
	}
	if err != nil {
		return Registro{}, false, err
	}
	t.RegistrarCambios("pendiente", f.ID, map[string]any{"estado": EstadoBloqueado}, map[string]any{"estado": f.Estado, "codigo_error": codigo})
	return aRegistro(f), true, t.Emitir(EventoFallido, eventoFallo{eventoPendiente: eventoDe(f), Intentos: f.Intentos, Detalle: detalle})
}

// Esperando lista los pendientes de una operación bloqueados con ese motivo (barrido de un job).
func Esperando(ctx context.Context, q pendientesbd.DBTX, operacion, motivo string, limite int32) ([]Registro, error) {
	filas, err := pendientesbd.New(q).PendientesEsperando(ctx, pendientesbd.PendientesEsperandoParams{Motivo: motivo, Operacion: operacion, Limite: limite})
	if err != nil {
		return nil, err
	}
	salida := make([]Registro, 0, len(filas))
	for _, f := range filas {
		salida = append(salida, aRegistro(f))
	}
	return salida, nil
}

// DeTecnico lista los pendientes de una operación de un técnico actualizados desde una hora.
func DeTecnico(ctx context.Context, q pendientesbd.DBTX, usuarioID int64, operacion string, desde time.Time, limite int32) ([]Registro, error) {
	filas, err := pendientesbd.New(q).PendientesDeTecnico(ctx, pendientesbd.PendientesDeTecnicoParams{
		UsuarioID: &usuarioID, Operacion: operacion, Desde: desde, Limite: limite})
	if err != nil {
		return nil, err
	}
	salida := make([]Registro, 0, len(filas))
	for _, f := range filas {
		salida = append(salida, aRegistro(f))
	}
	return salida, nil
}

// PorID lee un pendiente por su id (pgx.ErrNoRows si no existe). q puede ser una transacción o el pool.
func PorID(ctx context.Context, q pendientesbd.DBTX, id int64) (Registro, error) {
	f, err := pendientesbd.New(q).PendientePorID(ctx, id)
	if err != nil {
		return Registro{}, err
	}
	return aRegistro(f), nil
}

// DBTX es una transacción o el pool.
type DBTX = pendientesbd.DBTX

// RehacerEsperando: un pendiente fallido_negocio (Argos no guarda la Idempotency-Key de un 4xx, o el
// cuerpo nunca salió) vuelve a esperar (motivo) con un cuerpo nuevo; auditado y con
// integracion.pendiente_corregido. ok = false si ya no estaba fallido.
func RehacerEsperando(ctx context.Context, t *auditoria.Tx, id int64, motivo string, cuerpo []byte) (Registro, bool, error) {
	if !json.Valid(cuerpo) {
		return Registro{}, false, errors.New("pendientes.RehacerEsperando: el cuerpo no es JSON")
	}
	f, err := pendientesbd.New(t).RehacerEsperando(ctx, pendientesbd.RehacerEsperandoParams{Payload: cuerpo, Motivo: motivo, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return Registro{}, false, nil
	}
	if err != nil {
		return Registro{}, false, err
	}
	t.RegistrarCambios("pendiente", f.ID, map[string]any{"estado": EstadoFallidoNegocio}, map[string]any{"estado": f.Estado, "esperando": motivo})
	return aRegistro(f), true, t.Emitir(EventoCorregido, eventoDe(f))
}

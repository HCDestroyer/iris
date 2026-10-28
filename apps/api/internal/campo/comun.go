package campo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	campobd "github.com/HCDestroyer/iris/apps/api/internal/campo/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
)

// Piezas comunes de las acciones sobre una orden: tomarla con candado verificando empresa,
// visibilidad, alcance, versión y las reglas del lote; guardarla auditada con su feed de
// cambios; historial y eventos.

// OpSync es la operación del lote que origina la acción (nil = desde el panel o la API).
type OpSync struct {
	ID            uuid.UUID
	Tipo          string
	DispositivoID string
	VersionBase   *int32
	// OcurridoEnTelefono es la hora que mandó el teléfono; OcurridoEn, la corregida con el
	// desfase del dispositivo (la que se usa: historial, checklist, materiales…).
	OcurridoEnTelefono *time.Time
	OcurridoEn         time.Time
	// Existencia de la bodega según Argos (material.usar), leída antes, fuera de la transacción.
	Existencia *float64
}

func noEncontrada() *nucleo.Problema { return nucleo.NoEncontrado("La orden no existe") }

func fueraDeAlcance(detalle string) *nucleo.Problema {
	return nucleo.NuevoProblema(http.StatusForbidden, "fuera_de_alcance", "Fuera de tu alcance", detalle)
}

// transicionInvalida: la acción no aplica en el estado actual (422).
func transicionInvalida(accion, estado string) *nucleo.Problema {
	return nucleo.ReglaNegocio("transicion_invalida", fmt.Sprintf("No se puede %s una orden %s", accion, estado))
}

// ordenReasignada: la orden ya no es del técnico del teléfono (contratos/sync.md §3).
func ordenReasignada() *nucleo.Problema {
	return nucleo.Conflicto("orden_reasignada", "La orden fue reasignada a otro técnico; lo ya aplicado se conserva")
}

// ordenCancelada: solo se aceptan notas y adjuntos como evidencia (contratos/sync.md §3).
func ordenCancelada() *nucleo.Problema {
	return nucleo.Conflicto("orden_cancelada", "La orden fue cancelada; solo se aceptan notas y adjuntos como evidencia")
}

func campoInvalido(campo, mensaje, regla string) *nucleo.Problema {
	return nucleo.Validacion(nucleo.ErrorCampo{Field: campo, Message: mensaje, Rule: regla})
}

func campoObligatorio(campo string) *nucleo.Problema {
	return campoInvalido(campo, "Es obligatorio", "required")
}

// texto recorta espacios y exige un largo entre min y max (en caracteres).
func texto(campo, valor string, minimo, maximo int) (string, error) {
	v := strings.TrimSpace(valor)
	n := len([]rune(v))
	if n == 0 && minimo > 0 {
		return "", campoObligatorio(campo)
	}
	if n < minimo || n > maximo {
		return "", campoInvalido(campo, fmt.Sprintf("Debe tener entre %d y %d caracteres", minimo, maximo), "length")
	}
	return v, nil
}

// textoOpcional: nil o vacío = nil.
func textoOpcional(campo string, valor *string, maximo int) (*string, error) {
	if valor == nil || strings.TrimSpace(*valor) == "" {
		return nil, nil
	}
	v, err := texto(campo, *valor, 1, maximo)
	return &v, err
}

func deref(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}

func ptr[T any](v T) *T { return &v }

func mismoID(a, b *int64) bool { return (a == nil && b == nil) || (a != nil && b != nil && *a == *b) }

func mismaHora(a, b *time.Time) bool {
	return (a == nil && b == nil) || (a != nil && b != nil && a.Equal(*b))
}

// filtroEmpresa dice qué órdenes ve el contexto según cómo se resolvió la empresa activa
// (mismas reglas que soporte):
//   - EmpresaActiva: solo las de esa empresa.
//   - EmpresaSinConector: solo las guardadas con empresa_id null (nunca «sin filtro»).
//   - EmpresaNoResuelta: solo llega el actor sistema; ve todas.
//   - Cualquier otro estado no llega a la lógica; por si acaso, nada.
func filtroEmpresa(ctx context.Context) (todas bool, id *int64) {
	switch contexto.EmpresasDe(ctx).Estado {
	case contexto.EmpresaActiva:
		if activa := contexto.EmpresaDe(ctx); activa != 0 {
			return false, &activa
		}
	case contexto.EmpresaSinConector:
		return false, nil
	case contexto.EmpresaNoResuelta:
		if contexto.ActorDe(ctx).EsUsuario() {
			break
		}
		if activa := contexto.EmpresaDe(ctx); activa != 0 {
			return false, &activa
		}
		return true, nil
	case contexto.EmpresaPorElegir, contexto.EmpresaSinAsignar:
	}
	return false, ptr(int64(0))
}

func enEmpresa(ctx context.Context, f campobd.CampoOrden) bool {
	todas, id := filtroEmpresa(ctx)
	return todas || mismoID(f.EmpresaID, id)
}

// propia: la orden está asignada al actor.
func propia(ctx context.Context, f campobd.CampoOrden) bool {
	a := contexto.ActorDe(ctx)
	return a.EsUsuario() && f.TecnicoID != nil && *f.TecnicoID == a.ID
}

// cubre: el actor tiene el permiso y su alcance cubre la orden (global, su zona o cuadrilla, o
// asignada a él).
func cubre(ctx context.Context, permiso nucleo.Permiso, f campobd.CampoOrden) bool {
	p := contexto.PermisosDe(ctx)
	if !p.Tiene(string(permiso)) {
		return false
	}
	global, zonas, cuadrillas := p.Alcances(string(permiso))
	switch {
	case global, propia(ctx, f):
		return true
	case f.ZonaID != nil && slices.Contains(zonas, *f.ZonaID):
		return true
	case f.CuadrillaID != nil && slices.Contains(cuadrillas, *f.CuadrillaID):
		return true
	}
	return false
}

// visible: ordenes.ver con alcance sobre la orden, u ordenes.ver_propias y es del actor.
func visible(ctx context.Context, f campobd.CampoOrden) bool {
	return cubre(ctx, nucleo.PermOrdenesVer, f) ||
		(propia(ctx, f) && contexto.PermisosDe(ctx).Tiene(string(nucleo.PermOrdenesVerPropias)))
}

// verificarAcceso: fuera de la empresa activa o de lo que el actor ve, 404 (no revela que
// existe); visible pero fuera del alcance del permiso de la acción, 403.
func verificarAcceso(ctx context.Context, permiso nucleo.Permiso, f campobd.CampoOrden) error {
	if !enEmpresa(ctx, f) || !visible(ctx, f) {
		return noEncontrada()
	}
	if permiso == nucleo.PermOrdenesVer || permiso == nucleo.PermOrdenesVerPropias || cubre(ctx, permiso, f) {
		return nil
	}
	return fueraDeAlcance(fmt.Sprintf("Tu permiso %s no cubre la zona o cuadrilla de esta orden", permiso))
}

// versionDe: desde el panel se exige la versión leída (409 si cambió); el lote no la exige
// (los conflictos del teléfono se resuelven con las reglas de contratos/sync.md §3).
func versionDe(v int32, s *OpSync) *int32 {
	if s != nil {
		return nil
	}
	return &v
}

// tomar lee la orden con FOR UPDATE y verifica las reglas del lote (si viene de él), la
// empresa, la visibilidad, el alcance del permiso y la versión. Fija la entidad y la empresa
// de la auditoría y de los eventos.
func tomar(ctx context.Context, t *auditoria.Tx, q *campobd.Queries, id int64, permiso nucleo.Permiso, version *int32, s *OpSync) (campobd.CampoOrden, error) {
	t.FijarEntidad("orden", id)
	f, err := q.OrdenPorIDParaActualizar(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return f, noEncontrada()
	}
	if err != nil {
		return f, err
	}
	if s != nil && enEmpresa(ctx, f) {
		if err := reglasDelLote(ctx, q, f, s); err != nil {
			t.FijarEmpresa(deref(f.EmpresaID))
			return f, err
		}
	}
	if err := verificarAcceso(ctx, permiso, f); err != nil {
		return f, err
	}
	t.FijarEmpresa(deref(f.EmpresaID))
	if version != nil {
		if err := nucleo.ExigirVersion(*version, f.Version, "orden", id); err != nil {
			return f, err
		}
	}
	return f, nil
}

// reglasDelLote aplica los conflictos de contratos/sync.md §3 que dependen de la orden:
//   - reasignada: ya no es del técnico del teléfono, pero lo fue (feed de cambios) → 409
//     orden_reasignada. Si nunca fue suya, sigue la verificación normal (404 o, para despacho,
//     su alcance).
//   - cancelada: 409 orden_cancelada, salvo nota.agregar y adjunto.registrar (evidencia).
func reglasDelLote(ctx context.Context, q *campobd.Queries, f campobd.CampoOrden, s *OpSync) error {
	actor := contexto.ActorDe(ctx)
	if !propia(ctx, f) && actor.EsUsuario() {
		tuvo, err := q.TuvoOrden(ctx, campobd.TuvoOrdenParams{TecnicoID: actor.ID, OrdenID: f.ID})
		if err != nil {
			return err
		}
		if tuvo {
			return ordenReasignada()
		}
	}
	if f.Estado == EstadoCancelada && s.Tipo != OpNotaAgregar && s.Tipo != OpAdjuntoRegistrar {
		return ordenCancelada()
	}
	return nil
}

// exigirEstado: la acción solo aplica desde los estados dados.
func exigirEstado(f campobd.CampoOrden, accion string, permitidos ...string) error {
	if !slices.Contains(permitidos, f.Estado) {
		return transicionInvalida(accion, f.Estado)
	}
	return nil
}

// guardar escribe la orden (control de versión), registra los cambios y el feed de los
// técnicos afectados. Toda acción que toca una orden pasa por aquí (también las evidencias:
// suben la versión, y el teléfono se entera por el feed).
func guardar(ctx context.Context, t *auditoria.Tx, q *campobd.Queries, antes, despues campobd.CampoOrden) (campobd.CampoOrden, error) {
	f, err := q.GuardarOrden(ctx, campobd.GuardarOrdenParams{
		TecnicoID: despues.TecnicoID, CuadrillaID: despues.CuadrillaID, BodegaID: despues.BodegaID,
		AsignadaEn: despues.AsignadaEn, Estado: despues.Estado, ProgramadaInicio: despues.ProgramadaInicio,
		ProgramadaFin: despues.ProgramadaFin, EnCaminoEn: despues.EnCaminoEn, EnSitioEn: despues.EnSitioEn,
		FinalizadaEn: despues.FinalizadaEn, Resultado: despues.Resultado, MotivoFalla: despues.MotivoFalla,
		Motivo: despues.Motivo, ID: antes.ID, Version: antes.Version,
	})
	if err != nil {
		return f, nucleo.VerificarVersion(err, "orden", antes.ID)
	}
	t.RegistrarCambios("orden", f.ID, antes, f)
	if err := alimentarFeed(ctx, q, &antes, f); err != nil {
		return f, err
	}
	return f, nil
}

// alimentarFeed escribe el feed de cambios de los técnicos afectados (GET /campo/cambios): al
// técnico anterior «reasignada» si la orden dejó de ser suya; al actual «nueva» si se la
// acaban de asignar, «cancelada», «reprogramada» o «actualizada». Toma el candado de cada
// técnico (en orden de id, sin interbloqueos) para que sus filas queden en orden de COMMIT.
func alimentarFeed(ctx context.Context, q *campobd.Queries, antes *campobd.CampoOrden, despues campobd.CampoOrden) error {
	var viejo *int64
	if antes != nil {
		viejo = antes.TecnicoID
	}
	nuevo := despues.TecnicoID
	tecnicos := []int64{}
	if viejo != nil {
		tecnicos = append(tecnicos, *viejo)
	}
	if nuevo != nil && !mismoID(viejo, nuevo) {
		tecnicos = append(tecnicos, *nuevo)
	}
	slices.Sort(tecnicos)
	for _, id := range tecnicos {
		if err := q.CandadoCambiosTecnico(ctx, id); err != nil {
			return err
		}
	}
	if viejo != nil && !mismoID(viejo, nuevo) {
		if err := q.CrearCambio(ctx, campobd.CrearCambioParams{TecnicoID: *viejo, OrdenID: despues.ID, Tipo: "reasignada"}); err != nil {
			return err
		}
	}
	if nuevo == nil {
		return nil
	}
	tipo := "actualizada"
	switch {
	case !mismoID(viejo, nuevo):
		tipo = "nueva"
	case despues.Estado == EstadoCancelada && antes.Estado != EstadoCancelada:
		tipo = "cancelada"
	case !mismaHora(antes.ProgramadaInicio, despues.ProgramadaInicio) || !mismaHora(antes.ProgramadaFin, despues.ProgramadaFin):
		tipo = "reprogramada"
	}
	return q.CrearCambio(ctx, campobd.CrearCambioParams{TecnicoID: *nuevo, OrdenID: despues.ID, Tipo: tipo})
}

// historial escribe una entrada en campo.orden_eventos con el actor de la ejecución.
func historial(ctx context.Context, t *auditoria.Tx, q *campobd.Queries, ordenID int64, tipo string, de, a, motivo *string, datos map[string]any, ocurrido time.Time) error {
	if datos == nil {
		datos = map[string]any{}
	}
	crudo, err := json.Marshal(datos)
	if err != nil {
		return err
	}
	actor := t.Actor()
	var actorID *int64
	if actor.ID != 0 {
		actorID = &actor.ID
	}
	return q.CrearEventoOrden(ctx, campobd.CrearEventoOrdenParams{
		OrdenID: ordenID, Tipo: tipo, De: de, A: a, Motivo: motivo, Datos: crudo, ActorTipo: string(actor.Tipo),
		ActorID: actorID, ActorNombre: actor.Nombre, Canal: string(contexto.CanalDe(ctx)),
		RequestID: contexto.RequestIDDe(ctx), OcurridoEn: ocurrido,
	})
}

// cambioDeEstado registra el historial y emite orden.estado_cambiada si el estado cambió.
func cambioDeEstado(ctx context.Context, t *auditoria.Tx, q *campobd.Queries, antes, despues campobd.CampoOrden, motivo *string, datos map[string]any, ocurrido time.Time) error {
	if antes.Estado == despues.Estado {
		return nil
	}
	if err := historial(ctx, t, q, despues.ID, "estado", &antes.Estado, &despues.Estado, motivo, datos, ocurrido); err != nil {
		return err
	}
	return t.Emitir("orden.estado_cambiada", map[string]any{
		"orden_id": despues.ID, "numero": despues.Numero, "de": antes.Estado, "a": despues.Estado, "motivo": motivo,
		"tecnico_id": despues.TecnicoID,
	})
}

// actorUsuario: id del actor si es un usuario (para marcado_por, registrado_por…).
func actorUsuario(t *auditoria.Tx) *int64 {
	if a := t.Actor(); a.EsUsuario() {
		return &a.ID
	}
	return nil
}

// cuando es la hora del hecho: la del teléfono corregida (lote) o la del servidor.
func (m *Modulo) cuando(s *OpSync) time.Time {
	if s != nil {
		return s.OcurridoEn
	}
	return m.ahora()
}

// gps arma los datos de posición del historial (evidencia con hora).
func gps(lat, lon, precision *float64, ocurrido time.Time) map[string]any {
	if lat == nil || lon == nil {
		return nil
	}
	d := map[string]any{"lat": *lat, "lon": *lon, "tomado_en": ocurrido}
	if precision != nil {
		d["precision"] = *precision
	}
	return d
}

func validarCoordenadas(lat, lon *float64) error {
	if (lat == nil) != (lon == nil) {
		return campoInvalido("lat", "lat y lon van juntas", "coordenadas")
	}
	if lat != nil && (*lat < -90 || *lat > 90 || *lon < -180 || *lon > 180) {
		return campoInvalido("lat", "Coordenadas fuera de rango", "coordenadas")
	}
	return nil
}

// tipoDe devuelve el tipo de la orden.
func tipoDe(ctx context.Context, q *campobd.Queries, f campobd.CampoOrden) (campobd.CampoTiposOrden, error) {
	return q.TipoPorID(ctx, f.TipoID)
}

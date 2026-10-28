package agenda

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	agendabd "github.com/HCDestroyer/iris/apps/api/internal/agenda/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
)

func campoInvalido(campo, mensaje, regla string) *nucleo.Problema {
	return nucleo.Validacion(nucleo.ErrorCampo{Field: campo, Message: mensaje, Rule: regla})
}

func campoObligatorio(campo string) *nucleo.Problema {
	return campoInvalido(campo, "Es obligatorio", "required")
}

func fueraDeAlcance(detalle string) *nucleo.Problema {
	return nucleo.NuevoProblema(http.StatusForbidden, "fuera_de_alcance", "Fuera de tu alcance", detalle)
}

// texto recorta espacios y exige un largo entre min y max (en caracteres). NUL → 422.
func texto(campo, valor string, minimo, maximo int) (string, error) {
	if strings.ContainsRune(valor, 0) {
		return "", campoInvalido(campo, "El texto no puede contener el carácter NUL", "texto")
	}
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

func deref(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}

func ptr[T any](v T) *T { return &v }

func mismoID(a, b *int64) bool { return (a == nil && b == nil) || (a != nil && b != nil && *a == *b) }

// filtroEmpresa: qué empresa ve el contexto (mismas reglas que soporte y campo).
//   - EmpresaActiva: esa (id).
//   - EmpresaSinConector: los datos sin empresa (id nil, nunca «sin filtro»).
//   - EmpresaNoResuelta: solo el actor sistema; todas.
//   - Otro estado no llega a la lógica; por si acaso, ninguna (id 0).
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

// empresaDeAlta: la empresa con que nace un dato del catálogo (la activa; null sin conector).
// El actor sistema crea sin empresa.
func empresaDeAlta(ctx context.Context) (*int64, error) {
	todas, id := filtroEmpresa(ctx)
	if todas {
		return nil, nil
	}
	if id != nil && *id == 0 {
		return nil, nucleo.EmpresaRequerida()
	}
	return id, nil
}

// catalogoVisible: un dato del catálogo (zona, cuadrilla) con empresa null se ve desde todas;
// si no, debe ser de la empresa activa.
func catalogoVisible(ctx context.Context, empresa *int64) bool {
	todas, id := filtroEmpresa(ctx)
	return todas || empresa == nil || mismoID(empresa, id)
}

// comun: el actor puede tocar lo que no es de una empresa (catálogo con empresa null, fichas sin
// cuadrilla de su empresa): el sistema, el modo sin conector (todo es null) o quien tiene
// empresas.todas sin límite de alcance.
func comun(ctx context.Context) bool { return PuedeTocarLoComun(ctx) }

// PuedeTocarLoComun: el contexto puede usar o modificar lo que no es de una empresa (el
// sistema, el modo sin conector o empresas.todas global). Para campo (posiciones).
func PuedeTocarLoComun(ctx context.Context) bool {
	todas, _ := filtroEmpresa(ctx)
	return todas || contexto.EmpresasDe(ctx).Estado == contexto.EmpresaSinConector ||
		contexto.PermisosDe(ctx).TieneGlobal(string(nucleo.PermEmpresasTodas))
}

// exigirModificable: una zona o cuadrilla sin empresa (de todas) solo la modifica quien puede
// tocar lo común; las demás ya se filtraron por la empresa activa.
func exigirModificable(ctx context.Context, empresa *int64) error {
	if empresa == nil && !comun(ctx) {
		return nucleo.Prohibido("El catálogo sin empresa solo lo modifica un administrador global (empresas.todas)")
	}
	return nil
}

// sirveA: el dato del catálogo se puede usar en una entidad de esa empresa: de la misma empresa
// o, si no tiene empresa, solo mientras no haya empresas registradas (modo sin conector).
func sirveA(ctx context.Context, q *agendabd.Queries, catalogo, entidad *int64) (bool, error) {
	if catalogo != nil {
		return mismoID(catalogo, entidad), nil
	}
	hay, err := q.HayEmpresas(ctx)
	return !hay, err
}

// cuadrillaDeLaEmpresa: la cuadrilla es de la empresa activa (o sin empresa y el actor puede
// tocar lo común).
func cuadrillaDeLaEmpresa(ctx context.Context, c agendabd.AgendaCuadrilla) bool {
	if c.EmpresaID == nil {
		return comun(ctx)
	}
	todas, id := filtroEmpresa(ctx)
	return todas || mismoID(c.EmpresaID, id)
}

// fichaDeLaEmpresa: la ficha se modifica (y el detalle de sus ausencias se ve) solo desde la
// empresa de su cuadrilla. Sin cuadrilla, o con una sin empresa o de otra, hace falta tocar lo
// común (empresas.todas global; en modo sin conector todo es común).
func fichaDeLaEmpresa(ctx context.Context, q *agendabd.Queries, f agendabd.AgendaTecnico) (bool, error) {
	if comun(ctx) {
		return true, nil
	}
	if f.CuadrillaID == nil {
		return false, nil
	}
	c, err := q.CuadrillaPorID(ctx, *f.CuadrillaID)
	if err != nil {
		return false, err
	}
	_, id := filtroEmpresa(ctx)
	return c.EmpresaID != nil && mismoID(c.EmpresaID, id), nil
}

// alcance del actor para un permiso.
type alcance struct {
	global     bool
	zonas      []int64
	cuadrillas []int64
}

func alcanceDe(ctx context.Context, permiso nucleo.Permiso) alcance {
	g, z, c := contexto.PermisosDe(ctx).Alcances(string(permiso))
	if z == nil {
		z = []int64{}
	}
	if c == nil {
		c = []int64{}
	}
	return alcance{global: g, zonas: z, cuadrillas: c}
}

// cubreCuadrilla: el alcance cubre a un técnico de esa cuadrilla: global, o la cuadrilla es de
// la empresa activa (o sin empresa, si el actor puede tocar lo común) y está en su alcance o
// atiende una de sus zonas. Falla cerrado: una cuadrilla sin zonas no cubre a quien tiene
// alcance por zona.
func (a alcance) cubreCuadrilla(ctx context.Context, q *agendabd.Queries, cuadrilla *int64) (bool, error) {
	if a.global {
		return true, nil
	}
	if cuadrilla == nil {
		return false, nil
	}
	c, err := q.CuadrillaPorID(ctx, *cuadrilla)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !cuadrillaDeLaEmpresa(ctx, c) {
		return false, nil
	}
	if slices.Contains(a.cuadrillas, *cuadrilla) {
		return true, nil
	}
	if len(a.zonas) == 0 {
		return false, nil
	}
	zs, err := q.ZonasDeCuadrillas(ctx, []int64{*cuadrilla})
	if err != nil {
		return false, err
	}
	for _, z := range zs {
		if slices.Contains(a.zonas, z.ZonaID) {
			return true, nil
		}
	}
	return false, nil
}

// exigirGlobal: administrar el catálogo (zonas, cuadrillas) exige el permiso sin límite de
// zona ni cuadrilla.
func exigirGlobal(ctx context.Context, permiso nucleo.Permiso) error {
	if contexto.PermisosDe(ctx).TieneGlobal(string(permiso)) {
		return nil
	}
	return nucleo.Prohibido(fmt.Sprintf("Administrar el catálogo exige %s sin límite de zona o cuadrilla", permiso))
}

// actorUsuario: id del actor si es un usuario.
func actorUsuario(t *auditoria.Tx) *int64 {
	if a := t.Actor(); a.EsUsuario() {
		return &a.ID
	}
	return nil
}

// medianoche local del día de t.
func medianoche(t time.Time, loc *time.Location) time.Time {
	l := t.In(loc)
	return time.Date(l.Year(), l.Month(), l.Day(), 0, 0, 0, 0, loc)
}

// diaISO: 1 = lunes … 7 = domingo.
func diaISO(t time.Time) int16 {
	if t.Weekday() == time.Sunday {
		return 7
	}
	return int16(t.Weekday()) //nolint:gosec // 0–6
}

// fechaLocal interpreta YYYY-MM-DD como el día local (vacío = hoy).
func (m *Modulo) fechaLocal(campo, fecha string) (time.Time, error) {
	if fecha == "" {
		return medianoche(m.ahora(), m.cfg.ZonaHoraria), nil
	}
	d, err := time.ParseInLocation(time.DateOnly, fecha, m.cfg.ZonaHoraria)
	if err != nil {
		return time.Time{}, campoInvalido(campo, "Fecha inválida (YYYY-MM-DD)", "fecha")
	}
	return d, nil
}

// esSistema: workers, crons y consola.
func esSistema(ctx context.Context) bool { return contexto.ActorDe(ctx).Tipo == contexto.ActorSistema }

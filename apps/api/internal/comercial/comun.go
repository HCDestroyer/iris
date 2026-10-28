package comercial

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	comercialbd "github.com/HCDestroyer/iris/apps/api/internal/comercial/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
)

// Estados del prospecto.
const (
	EstadoNuevo        = "nuevo"
	EstadoContactado   = "contactado"
	EstadoFactibilidad = "factibilidad"
	EstadoCotizado     = "cotizado"
	EstadoContrato     = "contrato"
	EstadoGanado       = "ganado"
	EstadoPerdido      = "perdido"
)

func campoInvalido(campo, mensaje, regla string) *nucleo.Problema {
	return nucleo.Validacion(nucleo.ErrorCampo{Field: campo, Message: mensaje, Rule: regla})
}

func fueraDeAlcance(detalle string) *nucleo.Problema {
	return nucleo.NuevoProblema(http.StatusForbidden, "fuera_de_alcance", "Fuera de tu alcance", detalle)
}

func noEncontrado() *nucleo.Problema { return nucleo.NoEncontrado("El prospecto no existe") }

func transicionInvalida(detalle string) *nucleo.Problema {
	return nucleo.ReglaNegocio("transicion_invalida", detalle)
}

// texto recorta espacios y exige un largo entre min y max (en caracteres). NUL → 422.
func texto(campo, valor string, minimo, maximo int) (string, error) {
	if strings.ContainsRune(valor, 0) {
		return "", campoInvalido(campo, "El texto no puede contener el carácter NUL", "texto")
	}
	v := strings.TrimSpace(valor)
	n := len([]rune(v))
	if n == 0 && minimo > 0 {
		return "", campoInvalido(campo, "Es obligatorio", "required")
	}
	if n < minimo || n > maximo {
		return "", campoInvalido(campo, fmt.Sprintf("Debe tener entre %d y %d caracteres", minimo, maximo), "length")
	}
	return v, nil
}

// textoOpcional: nil o vacío = nil.
func textoOpcional(campo string, valor *string, maximo int) (*string, error) {
	if valor == nil {
		return nil, nil
	}
	v, err := texto(campo, *valor, 0, maximo)
	if err != nil || v == "" {
		return nil, err
	}
	return &v, nil
}

func validarCoordenadas(lat, lon *float64) error {
	if (lat == nil) != (lon == nil) {
		return campoInvalido("lat", "Indica lat y lon juntas", "coordenadas")
	}
	return nil
}

func deref[T any](p *T) T {
	var cero T
	if p == nil {
		return cero
	}
	return *p
}

func ptr[T any](v T) *T { return &v }

func mismoID(a, b *int64) bool { return (a == nil && b == nil) || (a != nil && b != nil && *a == *b) }

// filtroEmpresa: qué empresa ve el contexto (mismas reglas que soporte, campo y agenda).
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

func enEmpresa(ctx context.Context, empresa *int64) bool {
	todas, id := filtroEmpresa(ctx)
	return todas || mismoID(empresa, id)
}

// alcance del actor para un permiso: global o las zonas de su alcance. Un prospecto no tiene
// cuadrilla: un alcance por cuadrilla solo alcanza los prospectos propios (vendedor).
type alcance struct {
	global bool
	zonas  []int64
	propio *int64
}

func alcanceDe(ctx context.Context, permiso string) alcance {
	p := contexto.PermisosDe(ctx)
	a := alcance{zonas: []int64{}}
	if !p.Tiene(permiso) {
		return a
	}
	g, z, _ := p.Alcances(permiso)
	a.global = g
	if z != nil {
		a.zonas = z
	}
	if actor := contexto.ActorDe(ctx); actor.EsUsuario() {
		a.propio = &actor.ID
	}
	return a
}

// cubre: el alcance cubre al prospecto: global, su zona está en el alcance o es del actor
// (vendedor). Falla cerrado: un prospecto sin zona no lo cubre un alcance por zona.
func (a alcance) cubre(f comercialbd.ComercialProspecto) bool {
	switch {
	case a.global:
		return true
	case f.ZonaID != nil && slices.Contains(a.zonas, *f.ZonaID):
		return true
	case a.propio != nil && f.VendedorID != nil && *f.VendedorID == *a.propio:
		return true
	}
	return false
}

// verificarAcceso: fuera de la empresa activa o de lo que el actor ve (prospectos.ver), 404 (no
// revela que existe); visible pero fuera del alcance del permiso de la acción, 403.
func verificarAcceso(ctx context.Context, permiso string, f comercialbd.ComercialProspecto) error {
	if !enEmpresa(ctx, f.EmpresaID) || !alcanceDe(ctx, string(nucleo.PermProspectosVer)).cubre(f) {
		return noEncontrado()
	}
	if permiso == string(nucleo.PermProspectosVer) || alcanceDe(ctx, permiso).cubre(f) {
		return nil
	}
	return fueraDeAlcance(fmt.Sprintf("Tu permiso %s no cubre la zona de este prospecto", permiso))
}

// tomar lee el prospecto (con candado si para = true), verifica empresa, alcance y versión, y
// fija la entidad y la empresa de la auditoría.
func tomar(ctx context.Context, t *auditoria.Tx, q *comercialbd.Queries, id int64, permiso nucleo.Permiso, version *int32) (comercialbd.ComercialProspecto, error) {
	t.FijarEntidad("prospecto", id)
	f, err := q.ProspectoPorIDParaActualizar(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return f, noEncontrado()
	}
	if err != nil {
		return f, err
	}
	if err := verificarAcceso(ctx, string(permiso), f); err != nil {
		return f, err
	}
	t.FijarEmpresa(deref(f.EmpresaID))
	if version != nil {
		if err := nucleo.ExigirVersion(*version, f.Version, "prospecto", id); err != nil {
			return f, err
		}
	}
	return f, nil
}

// tomarCon: como tomar, pero el prospecto lo ve quien tiene el permiso de la acción con alcance
// sobre él (aunque no tenga prospectos.ver): la factibilidad manual del técnico. Si no, 404.
func tomarCon(ctx context.Context, t *auditoria.Tx, q *comercialbd.Queries, id int64, permiso nucleo.Permiso, version *int32) (comercialbd.ComercialProspecto, error) {
	t.FijarEntidad("prospecto", id)
	f, err := q.ProspectoPorIDParaActualizar(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && (!enEmpresa(ctx, f.EmpresaID) || !alcanceDe(ctx, string(permiso)).cubre(f))) {
		return f, noEncontrado()
	}
	if err != nil {
		return f, err
	}
	t.FijarEmpresa(deref(f.EmpresaID))
	if version != nil {
		if err := nucleo.ExigirVersion(*version, f.Version, "prospecto", id); err != nil {
			return f, err
		}
	}
	return f, nil
}

// leer: como tomar, sin candado (consultas).
func leer(ctx context.Context, t *auditoria.Tx, q *comercialbd.Queries, id int64) (comercialbd.ComercialProspecto, error) {
	t.FijarEntidad("prospecto", id)
	f, err := q.ProspectoPorID(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return f, noEncontrado()
	}
	if err != nil {
		return f, err
	}
	if err := verificarAcceso(ctx, string(nucleo.PermProspectosVer), f); err != nil {
		return f, err
	}
	t.FijarEmpresa(deref(f.EmpresaID))
	return f, nil
}

func exigirEstado(f comercialbd.ComercialProspecto, accion string, estados ...string) error {
	if slices.Contains(estados, f.Estado) {
		return nil
	}
	return transicionInvalida(fmt.Sprintf("No se puede %s un prospecto en estado %s", accion, f.Estado))
}

// guardar escribe el prospecto con control de versión y registra los cambios.
func guardar(ctx context.Context, t *auditoria.Tx, q *comercialbd.Queries, antes, d comercialbd.ComercialProspecto) (comercialbd.ComercialProspecto, error) {
	f, err := q.GuardarProspecto(ctx, comercialbd.GuardarProspectoParams{
		OrigenDetalle: d.OrigenDetalle, Nombre: d.Nombre, Documento: d.Documento, Telefono: d.Telefono, Correo: d.Correo,
		Direccion: d.Direccion, Lat: d.Lat, Lon: d.Lon, ZonaID: d.ZonaID, PlanInteresID: d.PlanInteresID,
		PlanInteresNombre: d.PlanInteresNombre, VendedorID: d.VendedorID, Estado: d.Estado, Notas: d.Notas,
		MotivoPerdida: d.MotivoPerdida, PerdidoEn: d.PerdidoEn, GanadoEn: d.GanadoEn, ObraConfirmada: d.ObraConfirmada,
		AltaModo: d.AltaModo, AltaEstado: d.AltaEstado, ClienteArgosID: d.ClienteArgosID, ServicioArgosID: d.ServicioArgosID,
		OrdenID: d.OrdenID, RouterID: d.RouterID, AltaError: d.AltaError, ID: antes.ID, Version: antes.Version,
	})
	if err != nil {
		return f, nucleo.VerificarVersion(err, "prospecto", antes.ID)
	}
	t.RegistrarCambios("prospecto", f.ID, antes, f)
	return f, nil
}

// historial agrega una fila a comercial.prospecto_eventos.
func historial(ctx context.Context, t *auditoria.Tx, q *comercialbd.Queries, prospecto int64, tipo string, de, a, motivo *string, datos map[string]any) error {
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
	return q.InsertarEventoProspecto(ctx, comercialbd.InsertarEventoProspectoParams{
		ProspectoID: prospecto, Tipo: tipo, De: de, A: a, Motivo: motivo, Datos: crudo,
		ActorTipo: string(actor.Tipo), ActorID: actorID,
	})
}

// cambioDeEstado: historial y prospecto.estado_cambiado si cambió el estado.
func cambioDeEstado(ctx context.Context, t *auditoria.Tx, q *comercialbd.Queries, antes, despues comercialbd.ComercialProspecto, motivo *string) error {
	if antes.Estado == despues.Estado {
		return nil
	}
	if err := historial(ctx, t, q, despues.ID, "estado", &antes.Estado, &despues.Estado, motivo, nil); err != nil {
		return err
	}
	return t.Emitir("prospecto.estado_cambiado", map[string]any{
		"prospecto_id": despues.ID, "de": antes.Estado, "a": despues.Estado, "motivo": motivo,
	})
}

// actorUsuario: id del actor si es un usuario.
func actorUsuario(t *auditoria.Tx) *int64 {
	if a := t.Actor(); a.EsUsuario() {
		return &a.ID
	}
	return nil
}

// conIdempotencia identifica la operación por la credencial y su Idempotency-Key (nil sin
// clave): si el núcleo retoma una clave abandonada, la operación devuelve lo ya creado.
func conIdempotencia(ctx context.Context, clave string) *string {
	if clave == "" {
		return nil
	}
	a := contexto.ActorDe(ctx)
	credencial := fmt.Sprintf("%s:%d", a.Tipo, a.ID)
	if a.TokenID != 0 {
		credencial = fmt.Sprintf("token:%d", a.TokenID)
	}
	v := credencial + "|" + clave
	return &v
}

// empresaDeAlta: la empresa con que nace un prospecto (la activa; null sin conector). El actor
// sistema elige: sin empresas registradas, null; con empresas, la pedida (obligatoria y
// registrada). Una persona o integración solo puede repetir la activa.
func empresaDeAlta(ctx context.Context, q *comercialbd.Queries, pedida *int64) (*int64, error) {
	noCoincide := func(detalle string) error { return nucleo.ReglaNegocio("empresa_no_coincide", detalle) }
	todas, id := filtroEmpresa(ctx)
	if !todas {
		if id != nil && *id == 0 {
			return nil, nucleo.EmpresaRequerida()
		}
		if pedida != nil && !mismoID(pedida, id) {
			if id == nil {
				return nil, noCoincide("Sin conector con Argos los prospectos se guardan sin empresa")
			}
			return nil, noCoincide("La empresa del prospecto debe ser la empresa activa")
		}
		return id, nil
	}
	hay, err := q.HayEmpresas(ctx)
	if err != nil {
		return nil, err
	}
	if !hay {
		if pedida != nil {
			return nil, noCoincide("Sin conector con Argos los prospectos se guardan sin empresa")
		}
		return nil, nil
	}
	if pedida == nil {
		return nil, nucleo.ReglaNegocio("empresa_requerida", "Indica la empresa del prospecto")
	}
	existe, err := q.EmpresaRegistrada(ctx, *pedida)
	if err != nil {
		return nil, err
	}
	if !existe {
		return nil, nucleo.ReglaNegocio("empresa_no_registrada", "La empresa no está registrada en Iris")
	}
	return pedida, nil
}

// --- Montos: centavos (int64) ↔ numeric(14,2) ------------------------------------------------

// maxCentavos: numeric(14,2) admite hasta 999 999 999 999,99.
const maxCentavos int64 = 99_999_999_999_999

// leerMonto lee un decimal con hasta dos cifras ("150", "150.5", "150.50") en centavos (o en
// centésimas, para cantidades). Sin signo; vacío o mal formado → 422 en el campo.
func leerMonto(campo, s string, maximo int64) (int64, error) {
	s = strings.TrimSpace(s)
	malo := campoInvalido(campo, "Número decimal con hasta 2 decimales (p. ej. 150.00)", "decimal")
	if s == "" || strings.HasPrefix(s, "+") || strings.HasPrefix(s, "-") {
		return 0, malo
	}
	entero, fraccion, hay := strings.Cut(s, ".")
	if entero == "" || len(entero) > 13 || (hay && (len(fraccion) == 0 || len(fraccion) > 2)) {
		return 0, malo
	}
	for len(fraccion) < 2 {
		fraccion += "0"
	}
	e, err := strconv.ParseInt(entero, 10, 64)
	if err != nil {
		return 0, malo
	}
	f, err := strconv.ParseInt(fraccion, 10, 64)
	if err != nil {
		return 0, malo
	}
	v := e*100 + f
	if v > maximo {
		return 0, campoInvalido(campo, "El valor es demasiado grande", "max")
	}
	return v, nil
}

// aTexto: centavos → "1234.50".
func aTexto(c int64) string {
	signo := ""
	if c < 0 {
		signo, c = "-", -c
	}
	return fmt.Sprintf("%s%d.%02d", signo, c/100, c%100)
}

// aNumeric: centavos → numeric(…,2).
func aNumeric(c int64) pgtype.Numeric {
	return pgtype.Numeric{Int: big.NewInt(c), Exp: -2, Valid: true}
}

// centavos: numeric → centavos (redondeado a 2 decimales).
func centavos(n pgtype.Numeric) int64 {
	if !n.Valid || n.Int == nil {
		return 0
	}
	v := new(big.Int).Set(n.Int)
	exp := n.Exp + 2
	diez := big.NewInt(10)
	for ; exp > 0; exp-- {
		v.Mul(v, diez)
	}
	for ; exp < 0; exp++ {
		v.Quo(v, diez)
	}
	return v.Int64()
}

// multiplicar: cantidad (centésimas) × precio (centavos) → centavos, redondeo a la mitad hacia
// arriba.
// ok = false si el resultado no cabe en numeric(14,2) (ni en int64).
func multiplicar(cantidad, precio int64) (int64, bool) {
	p := new(big.Int).Mul(big.NewInt(cantidad), big.NewInt(precio))
	p.Add(p, big.NewInt(50))
	p.Quo(p, big.NewInt(100))
	if !p.IsInt64() || p.Int64() > maxCentavos || p.Int64() < -maxCentavos {
		return 0, false
	}
	return p.Int64(), true
}

// diaLocal formatea una hora en la zona de visualización.
func (m *Modulo) diaLocal(t time.Time) string {
	return t.In(m.cfg.ZonaHoraria).Format("02/01/2006")
}

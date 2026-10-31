// Package adjuntos guarda las evidencias (fotos, firmas, documentos) de las órdenes y los
// tickets, con subida reanudable estilo tus 1.0 (contratos/sync.md §4):
//
//   - alta idempotente por el UUID que genera el cliente (POST /api/v1/adjuntos), con tamaño y
//     MIME permitidos configurables y la entidad (orden, ticket…) validada por el módulo dueño;
//   - subida en partes (HEAD/PATCH /api/v1/adjuntos/{id}/subida, núcleo de tus 1.0): las partes
//     se guardan en la base (nucleo.adjuntos_partes) a medida que llegan, así un corte de
//     señal no pierde lo recibido y la subida se retoma contra cualquier réplica de la API;
//   - al completar se verifica tamaño, SHA-256 y que el contenido sea del MIME declarado; si no
//     coincide queda «rechazado». Si coincide, pasa al almacenamiento (disco o S3 compatible) y
//     se emite adjunto.completado;
//   - descarga autorizada (GET /api/v1/adjuntos/{id}) con Content-Disposition seguro;
//   - Faltantes, para que campo (I2) no cierre una orden sin sus adjuntos obligatorios.
//
// Vive en nucleo (no en campo ni en soporte) porque lo usan varios módulos: cada uno registra
// su entidad con RegistrarEntidadAdjuntable y decide quién puede ver o subir.
package adjuntos

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
)

func init() { nucleo.RegistrarTablaConEmpresa("nucleo.adjuntos") }

// DBTX es una transacción o el pool (sqlc).
type DBTX interface {
	Exec(context.Context, string, ...interface{}) (pgconn.CommandTag, error)
	Query(context.Context, string, ...interface{}) (pgx.Rows, error)
	QueryRow(context.Context, string, ...interface{}) pgx.Row
}

// Uso dice para qué se consulta la entidad.
type Uso string

const (
	// UsoSubir: registrar o subir un adjunto (permiso adjuntos.subir).
	UsoSubir Uso = "subir"
	// UsoVer: listar o descargar (permiso adjuntos.ver).
	UsoVer Uso = "ver"
)

// Verificador lo registra cada módulo para su entidad. Recibe el ctx de la acción (actor,
// permisos, empresa activa) y la transacción, y devuelve la empresa de la entidad (0 = sin
// empresa, modo sin conector). Si la entidad no existe o el actor no puede usarla para ese uso
// (otra cuadrilla, orden de otro técnico, ticket cerrado…) devuelve nucleo.NoEncontrado (404,
// igual que inexistente) u otro *nucleo.Problema (p. ej. 422 orden_cerrada). adjuntos además
// exige que esa empresa sea la activa.
type Verificador func(ctx context.Context, q DBTX, entidadID string, uso Uso) (empresaID int64, err error)

// Preparador lo registra (opcional) un módulo cuya entidad hay que confirmar con otro sistema antes
// del alta (p. ej. un traslado que Iris aún no conoce): corre ANTES de abrir la transacción del alta,
// sin conexión tomada. Devuelve solo los errores que el cliente debe ver tal cual (p. ej. 503
// reintentable); lo demás lo decide el Verificador dentro de la transacción.
type Preparador func(ctx context.Context, entidadID string) error

var (
	muEntidades  sync.RWMutex
	entidades    = map[string]Verificador{}
	preparadores = map[string]Preparador{}
	nombreValido = regexp.MustCompile(`^[a-z][a-z_]{0,39}$`)
)

// RegistrarEntidadAdjuntable registra una entidad que acepta adjuntos ("orden", "ticket"). Se
// llama en un init() del módulo dueño o al armarlo. Entra en pánico si el nombre no es
// snake_case o ya está registrado (se descubre al arrancar).
func RegistrarEntidadAdjuntable(nombre string, v Verificador) {
	if !nombreValido.MatchString(nombre) || v == nil {
		panic("adjuntos.RegistrarEntidadAdjuntable: nombre inválido o sin verificador: " + nombre)
	}
	muEntidades.Lock()
	defer muEntidades.Unlock()
	if _, ok := entidades[nombre]; ok {
		panic("adjuntos.RegistrarEntidadAdjuntable: entidad repetida " + nombre)
	}
	entidades[nombre] = v
}

// RegistrarPreparacion fija (o reemplaza) el Preparador de una entidad adjuntable.
func RegistrarPreparacion(nombre string, p Preparador) {
	muEntidades.Lock()
	defer muEntidades.Unlock()
	preparadores[nombre] = p
}

// Preparar corre el Preparador de la entidad (si tiene), fuera de transacción: POST /adjuntos lo
// llama antes de Registrar.
func Preparar(ctx context.Context, entidad, entidadID string) error {
	muEntidades.RLock()
	p := preparadores[entidad]
	muEntidades.RUnlock()
	if p == nil {
		return nil
	}
	return p(ctx, entidadID)
}

// QuitarEntidadAdjuntable deshace el registro (solo pruebas).
func QuitarEntidadAdjuntable(nombre string) {
	muEntidades.Lock()
	defer muEntidades.Unlock()
	delete(entidades, nombre)
}

// EntidadesAdjuntables devuelve los nombres registrados, ordenados.
func EntidadesAdjuntables() []string {
	muEntidades.RLock()
	defer muEntidades.RUnlock()
	salida := make([]string, 0, len(entidades))
	for n := range entidades {
		salida = append(salida, n)
	}
	slices.Sort(salida)
	return salida
}

func verificadorDe(nombre string) (Verificador, bool) {
	muEntidades.RLock()
	defer muEntidades.RUnlock()
	v, ok := entidades[nombre]
	return v, ok
}

// verificarEntidad corre el verificador y exige que la empresa de la entidad sea la activa
// (sin conector: sin empresa; actor sistema: cualquiera). Devuelve la empresa (0 = ninguna).
func verificarEntidad(ctx context.Context, q DBTX, entidad, entidadID string, uso Uso) (int64, error) {
	v, ok := verificadorDe(entidad)
	if !ok {
		return 0, nucleo.ReglaNegocio("entidad_no_adjuntable", fmt.Sprintf("La entidad %q no acepta adjuntos", entidad))
	}
	empresa, err := v(ctx, q, entidadID, uso)
	if err != nil {
		return 0, err
	}
	if !empresaVisible(ctx, empresa) {
		return 0, nucleo.NoEncontrado("")
	}
	return empresa, nil
}

// empresaVisible: la empresa de un dato (0 = null) se ve con la empresa activa del contexto.
func empresaVisible(ctx context.Context, empresa int64) bool {
	switch contexto.EmpresasDe(ctx).Estado {
	case contexto.EmpresaActiva:
		return empresa == contexto.EmpresaDe(ctx)
	case contexto.EmpresaSinConector:
		return empresa == 0
	case contexto.EmpresaNoResuelta:
		// Solo el actor sistema llega aquí (auditoria.Caso falla cerrado con un usuario).
		return true
	}
	return false
}

// Config del módulo.
type Config struct {
	// Almacen donde quedan los adjuntos completos (disco local o S3). nil = se rechaza la
	// subida al completar con 503 (solo para generar el OpenAPI).
	Almacen Almacen
	// TamanoMax de un adjunto en bytes (IRIS_ADJUNTOS_TAMANO_MAX, 25 MiB).
	TamanoMax int64
	// MIMEPermitidos (IRIS_ADJUNTOS_MIME): tipos aceptados en el alta, en minúsculas.
	MIMEPermitidos []string
	// Segmento: tamaño de cada parte que se guarda en la base mientras llega un PATCH (256 KiB).
	// Lo recibido se conserva de a segmentos si la conexión se corta.
	Segmento int
	// TiempoSubida: plazo de lectura de un PATCH (10 min; el servidor corta a los 60 s por
	// defecto y una foto por 2G tarda más).
	TiempoSubida time.Duration
	// TiempoDescarga: plazo de escritura de una descarga (10 min), por la misma razón.
	TiempoDescarga time.Duration
	// Cuotas de subidas sin terminar (pendiente o subiendo), verificadas en el alta; al pasarse,
	// 429 demasiadas_subidas. Por usuario: cantidad (IRIS_ADJUNTOS_MAX_EN_CURSO_USUARIO, 50) y
	// bytes declarados (IRIS_ADJUNTOS_MAX_BYTES_USUARIO, 500 MiB); por entidad: cantidad
	// (IRIS_ADJUNTOS_MAX_EN_CURSO_ENTIDAD, 30).
	MaxEnCursoUsuario int
	MaxBytesUsuario   int64
	MaxEnCursoEntidad int
	// Abandono (IRIS_ADJUNTOS_ABANDONO, 48 h): un adjunto sin terminar y sin actividad (alta o
	// última parte) por más de esto pasa a rechazado con motivo «abandonado» y se borran sus
	// partes (job periódico de River en cmd/worker).
	Abandono time.Duration
	// Reloj (pruebas). time.Now por defecto.
	Reloj func() time.Time
}

// MIMEPorDefecto: fotos, firmas (SVG o PNG) y documentos PDF.
var MIMEPorDefecto = []string{"image/jpeg", "image/png", "image/webp", "image/svg+xml", "application/pdf"}

// TamanoMaxPorDefecto: 25 MiB.
const TamanoMaxPorDefecto int64 = 25 << 20

// Modulo reúne los casos de uso de adjuntos.
type Modulo struct {
	cfg Config

	Registrar      *auditoria.Caso[EntradaRegistrar, Registrado]
	consultar      *auditoria.Caso[entradaConsultar, Adjunto]
	prepararSubida *auditoria.Caso[entradaPreparar, Adjunto]
	completar      *auditoria.Caso[entradaCompletar, Adjunto]
	rechazar       *auditoria.Caso[entradaRechazar, Adjunto]
	Descargar      *auditoria.Caso[EntradaDescargar, Adjunto]
	Listar         *auditoria.Caso[EntradaListar, nucleo.Pagina[Adjunto]]
	abandonar      *auditoria.Caso[entradaAbandonar, bool]
}

// Nuevo arma el módulo.
func Nuevo(cfg Config) *Modulo {
	if cfg.TamanoMax <= 0 {
		cfg.TamanoMax = TamanoMaxPorDefecto
	}
	if len(cfg.MIMEPermitidos) == 0 {
		cfg.MIMEPermitidos = MIMEPorDefecto
	}
	for i, m := range cfg.MIMEPermitidos {
		cfg.MIMEPermitidos[i] = strings.ToLower(strings.TrimSpace(m))
	}
	if cfg.Segmento <= 0 {
		cfg.Segmento = 256 << 10
	}
	if cfg.TiempoSubida <= 0 {
		cfg.TiempoSubida = 10 * time.Minute
	}
	if cfg.TiempoDescarga <= 0 {
		cfg.TiempoDescarga = 10 * time.Minute
	}
	if cfg.MaxEnCursoUsuario <= 0 {
		cfg.MaxEnCursoUsuario = 50
	}
	if cfg.MaxBytesUsuario <= 0 {
		cfg.MaxBytesUsuario = 500 << 20
	}
	if cfg.MaxEnCursoEntidad <= 0 {
		cfg.MaxEnCursoEntidad = 30
	}
	if cfg.Abandono <= 0 {
		cfg.Abandono = 48 * time.Hour
	}
	if cfg.Reloj == nil {
		cfg.Reloj = time.Now
	}
	m := &Modulo{cfg: cfg}
	m.Registrar = nuevoRegistrar(m)
	m.consultar = nuevoConsultar(m)
	m.prepararSubida = nuevoPrepararSubida(m)
	m.completar = nuevoCompletar(m)
	m.rechazar = nuevoRechazar(m)
	m.Descargar = nuevoDescargar(m)
	m.Listar = nuevoListar(m)
	m.abandonar = nuevoAbandonar(m)
	return m
}

// Config devuelve la configuración efectiva.
func (m *Modulo) Config() Config { return m.cfg }

func (m *Modulo) mimePermitido(mime string) bool {
	return slices.Contains(m.cfg.MIMEPermitidos, mime)
}

// normalizarMIME: minúsculas y sin parámetros ("image/JPEG; q=1" → "image/jpeg").
func normalizarMIME(m string) string {
	if i := strings.IndexByte(m, ';'); i >= 0 {
		m = m[:i]
	}
	return strings.ToLower(strings.TrimSpace(m))
}

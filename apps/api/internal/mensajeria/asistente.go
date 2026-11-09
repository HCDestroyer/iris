package mensajeria

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	mensajeriabd "github.com/HCDestroyer/iris/apps/api/internal/mensajeria/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
)

// Asistente de IA: ajustes por empresa, prueba de conexión, modelos y uso
// (contratos/conversaciones.md §16.1). La clave del proveedor se cifra con IRIS_CLAVE_CIFRADO y
// nunca sale en respuestas, logs, auditoría ni eventos.

// Modos del asistente.
const (
	ModoAsistenteAutomatico = "automatico"
	ModoAsistenteBoton      = "boton"
)

// TopeTokensPorDefecto al mes (entrada + salida).
const TopeTokensPorDefecto = 2_000_000

// FuentesAsistente: qué contexto recibe el modelo.
type FuentesAsistente struct {
	Manuales  bool `json:"manuales" doc:"Los 3 manuales más relevantes de la empresa"`
	Argos     bool `json:"argos" doc:"Plan y estado de los servicios del cliente asociado (instantánea de Argos)"`
	Horus     bool `json:"horus" doc:"Fallas abiertas (router caído) de la zona del cliente"`
	Historial bool `json:"historial" doc:"Últimos 30 mensajes del cliente y de agentes (sin notas internas)"`
}

// AjustesAsistente es GET /asistente/ajustes.
type AjustesAsistente struct {
	Activo              bool             `json:"activo"`
	Proveedor           string           `json:"proveedor" enum:"ollama,openai_compatible,anthropic"`
	URL                 string           `json:"url" doc:"URL base del servidor (vacía en anthropic = https://api.anthropic.com)"`
	ClaveGuardadaEn     *time.Time       `json:"claveGuardadaEn" nullable:"true" doc:"Cuándo se guardó la clave (nunca se devuelve)"`
	Modelo              string           `json:"modelo"`
	Modo                string           `json:"modo" enum:"automatico,boton"`
	Fuentes             FuentesAsistente `json:"fuentes"`
	TopeTokensMes       int64            `json:"topeTokensMes"`
	PrecioEntradaMillon *string          `json:"precioEntradaMillon" nullable:"true" doc:"Precio por millón de tokens de entrada (decimal como texto)"`
	PrecioSalidaMillon  *string          `json:"precioSalidaMillon" nullable:"true" doc:"Precio por millón de tokens de salida (decimal como texto)"`
	Version             int32            `json:"version" doc:"0 si nunca se guardaron"`
}

// EntradaAjustesAsistente es POST /asistente/ajustes/actualizar.
type EntradaAjustesAsistente struct {
	Version             int32            `json:"version" minimum:"0"`
	Activo              bool             `json:"activo"`
	Proveedor           string           `json:"proveedor" enum:"ollama,openai_compatible,anthropic"`
	URL                 string           `json:"url" maxLength:"500"`
	Clave               *string          `json:"clave,omitempty" maxLength:"500" auditoria:"sensible" doc:"Solo escritura: ausente la deja, \"\" la borra"`
	Modelo              string           `json:"modelo" maxLength:"200"`
	Modo                string           `json:"modo" enum:"automatico,boton"`
	Fuentes             FuentesAsistente `json:"fuentes"`
	TopeTokensMes       int64            `json:"topeTokensMes" minimum:"1000" maximum:"1000000000"`
	PrecioEntradaMillon *string          `json:"precioEntradaMillon" nullable:"true" maxLength:"20"`
	PrecioSalidaMillon  *string          `json:"precioSalidaMillon" nullable:"true" maxLength:"20"`
}

// EstadoAsistente es GET /asistente/estado (para decidir si el panel muestra «Sugerir»).
type EstadoAsistente struct {
	Activo  bool   `json:"activo"`
	Modo    string `json:"modo" enum:"automatico,boton"`
	Pausado bool   `json:"pausado" doc:"Se llegó al tope de tokens del mes"`
}

// EntradaProbarAsistente es POST /asistente/probar.
type EntradaProbarAsistente struct {
	Proveedor string  `json:"proveedor" enum:"ollama,openai_compatible,anthropic"`
	URL       string  `json:"url" maxLength:"500"`
	Clave     *string `json:"clave,omitempty" maxLength:"500" auditoria:"sensible" doc:"Sin clave: la guardada, solo si proveedor y URL son los guardados"`
	Modelo    string  `json:"modelo" maxLength:"200"`
}

// PruebaAsistente es el resultado de probar.
type PruebaAsistente struct {
	Estado           string   `json:"estado" enum:"ok,error"`
	LatenciaMs       int64    `json:"latenciaMs"`
	TokensPorSegundo *float64 `json:"tokensPorSegundo" nullable:"true"`
	Detalle          string   `json:"detalle" doc:"En español, sin secretos"`
}

// EntradaModelosAsistente es GET /asistente/modelos.
type EntradaModelosAsistente struct {
	Proveedor string `query:"proveedor" enum:"ollama,openai_compatible,anthropic," doc:"Vacío: lo guardado"`
	URL       string `query:"url" maxLength:"500"`
}

// ListaModelosAsistente es la respuesta de GET /asistente/modelos.
type ListaModelosAsistente struct {
	Data []ModeloIA `json:"data"`
}

// EntradaUsoAsistente es GET /asistente/uso.
type EntradaUsoAsistente struct {
	Mes string `query:"mes" pattern:"^[0-9]{4}-(0[1-9]|1[0-2])$" doc:"YYYY-MM (por defecto el mes actual, hora de Guatemala)"`
}

// UsoAsistente es el uso del mes.
type UsoAsistente struct {
	Mes           string  `json:"mes"`
	TokensEntrada int64   `json:"tokensEntrada"`
	TokensSalida  int64   `json:"tokensSalida"`
	Sugerencias   int64   `json:"sugerencias"`
	Usadas        int64   `json:"usadas"`
	Utiles        int64   `json:"utiles"`
	NoUtiles      int64   `json:"noUtiles"`
	Resumenes     int64   `json:"resumenes"`
	Costo         *string `json:"costo" nullable:"true" doc:"Con los dos precios: (entrada × precio + salida × precio) / 1 000 000"`
	TopeTokensMes int64   `json:"topeTokensMes"`
	Porcentaje    int64   `json:"porcentaje" doc:"Del tope usado este mes (entero)"`
	Pausado       bool    `json:"pausado"`
}

// casosAsistente reúne los casos del asistente.
type casosAsistente struct {
	VerAjustes   *auditoria.Caso[struct{}, AjustesAsistente]
	FijarAjustes *auditoria.Caso[EntradaAjustesAsistente, AjustesAsistente]
	Estado       *auditoria.Caso[struct{}, EstadoAsistente]
	Probar       *ProbarAsistente
	Modelos      *ModelosAsistente
	Uso          *auditoria.Caso[EntradaUsoAsistente, UsoAsistente]

	Pedir     *auditoria.Caso[EntradaPedirSugerencia, Sugerencia]
	Actual    *auditoria.Caso[EntradaConversacion, Sugerencia]
	Usar      *auditoria.Caso[EntradaSugerencia, Sugerencia]
	Descartar *auditoria.Caso[EntradaSugerencia, Sugerencia]
	Calificar *auditoria.Caso[EntradaCalificar, Sugerencia]
	Resumen   *ResumirConversacion

	prepararGen *auditoria.Caso[uuidSugerencia, generacion]
	guardarGen  *auditoria.Caso[resultadoGeneracion, struct{}]
	crearAuto   *auditoria.Caso[int64, autoCreada]
}

func nuevosCasosAsistente(m *Modulo) casosAsistente {
	return casosAsistente{
		VerAjustes: nuevoVerAjustesAsistente(m), FijarAjustes: nuevoFijarAjustesAsistente(m), Estado: nuevoEstadoAsistente(m),
		Probar: nuevoProbarAsistente(m), Modelos: nuevoModelosAsistente(m), Uso: nuevoUsoAsistente(m),
		Pedir: nuevoPedirSugerencia(m), Actual: nuevoSugerenciaActual(m), Usar: nuevoUsarSugerencia(m),
		Descartar: nuevoDescartarSugerencia(m), Calificar: nuevoCalificarSugerencia(m), Resumen: nuevoResumirConversacion(m),
		prepararGen: nuevoPrepararGeneracion(m), guardarGen: nuevoGuardarGeneracion(m), crearAuto: nuevoCrearAuto(m),
	}
}

// empresaPersona: persona con sesión (un token o una integración: 404) y su empresa activa.
func empresaPersona(ctx context.Context) (int64, error) {
	actor := contexto.ActorDe(ctx)
	if actor.Tipo != contexto.ActorUsuario || actor.ID == 0 {
		return 0, nucleo.NoEncontrado("No existe")
	}
	if contexto.EmpresasDe(ctx).Estado == contexto.EmpresaActiva {
		return contexto.EmpresaDe(ctx), nil
	}
	return 0, nil
}

func empresaPtrDe(e int64) *int64 {
	if e == 0 {
		return nil
	}
	return &e
}

// DatosAsociadosAsistente ata la clave cifrada a su fila (rotación de IRIS_CLAVE_CIFRADO).
func DatosAsociadosAsistente(id int64) []byte {
	return fmt.Appendf(nil, "mensajeria.asistente:%d:clave", id)
}

func ajustesPorDefecto() AjustesAsistente {
	return AjustesAsistente{Proveedor: ProveedorOllama, Modo: ModoAsistenteAutomatico,
		Fuentes: FuentesAsistente{Manuales: true, Argos: true, Horus: true, Historial: true}, TopeTokensMes: TopeTokensPorDefecto}
}

func aAjustes(f mensajeriabd.MensajeriaAsistenteAjuste) AjustesAsistente {
	return AjustesAsistente{Activo: f.Activo, Proveedor: f.Proveedor, URL: f.Url, ClaveGuardadaEn: f.ClaveGuardadaEn, Modelo: f.Modelo, Modo: f.Modo,
		Fuentes:       FuentesAsistente{Manuales: f.FuenteManuales, Argos: f.FuenteArgos, Horus: f.FuenteHorus, Historial: f.FuenteHistorial},
		TopeTokensMes: f.TopeTokensMes, PrecioEntradaMillon: textoNumerico(f.PrecioEntradaMillon), PrecioSalidaMillon: textoNumerico(f.PrecioSalidaMillon),
		Version: f.Version}
}

// leerAjustesAsistente: la fila de la empresa o nil.
func leerAjustesAsistente(ctx context.Context, q *mensajeriabd.Queries, empresa int64) (*mensajeriabd.MensajeriaAsistenteAjuste, error) {
	f, err := q.AsistenteAjustes(ctx, empresa)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &f, nil
}

// Números (precios) -------------------------------------------------------------------------------

var rePrecio = regexp.MustCompile(`^[0-9]{1,8}(\.[0-9]{1,4})?$`)

func numericDe(s *string) (pgtype.Numeric, error) {
	var n pgtype.Numeric
	if s == nil || strings.TrimSpace(*s) == "" {
		return n, nil
	}
	v := strings.TrimSpace(*s)
	if !rePrecio.MatchString(v) {
		return n, errors.New("precio")
	}
	return n, n.Scan(v)
}

func textoNumerico(n pgtype.Numeric) *string {
	if !n.Valid {
		return nil
	}
	f, err := n.Float64Value()
	if err != nil || !f.Valid {
		return nil
	}
	s := strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.4f", f.Float64), "0"), ".")
	if !strings.Contains(s, ".") {
		s += ".00"
	} else if len(s)-strings.Index(s, ".") == 2 {
		s += "0"
	}
	return &s
}

func ratDe(n pgtype.Numeric) *big.Rat {
	if !n.Valid || n.Int == nil {
		return nil
	}
	r := new(big.Rat).SetInt(n.Int)
	e := big.NewInt(10)
	if n.Exp > 0 {
		r.Mul(r, new(big.Rat).SetInt(new(big.Int).Exp(e, big.NewInt(int64(n.Exp)), nil)))
	} else if n.Exp < 0 {
		r.Quo(r, new(big.Rat).SetInt(new(big.Int).Exp(e, big.NewInt(int64(-n.Exp)), nil)))
	}
	return r
}

// Mes y tope ----------------------------------------------------------------------------------------

// mesDe: el mes calendario (hora de Guatemala) como fecha y sus límites en UTC.
func (m *Modulo) mesDe(t time.Time) (pgtype.Date, time.Time, time.Time) {
	l := t.In(m.cfg.Zona)
	ini := time.Date(l.Year(), l.Month(), 1, 0, 0, 0, 0, m.cfg.Zona)
	return pgtype.Date{Time: time.Date(l.Year(), l.Month(), 1, 0, 0, 0, 0, time.UTC), Valid: true}, ini.UTC(), ini.AddDate(0, 1, 0).UTC()
}

// usoDelMes: tokens usados en el mes (0 sin fila).
func (m *Modulo) usoDelMes(ctx context.Context, q *mensajeriabd.Queries, empresa int64, mes pgtype.Date) (mensajeriabd.MensajeriaAsistenteUso, error) {
	u, err := q.UsoAsistente(ctx, mensajeriabd.UsoAsistenteParams{Empresa: empresa, Mes: mes})
	if errors.Is(err, pgx.ErrNoRows) {
		return mensajeriabd.MensajeriaAsistenteUso{EmpresaID: empresaPtrDe(empresa), Mes: mes}, nil
	}
	return u, err
}

// pausadoPorTope: la empresa llegó al 100 % del tope este mes.
func (m *Modulo) pausadoPorTope(ctx context.Context, q *mensajeriabd.Queries, empresa int64, tope int64) (bool, error) {
	mes, _, _ := m.mesDe(m.ahora())
	u, err := m.usoDelMes(ctx, q, empresa, mes)
	if err != nil {
		return false, err
	}
	return u.TokensEntrada+u.TokensSalida >= tope, nil
}

// sumarUso suma los tokens de una llamada y emite asistente.tope_alcanzado al cruzar el 80 % y el
// 100 % (una vez por mes y tope).
func (m *Modulo) sumarUso(ctx context.Context, t *auditoria.Tx, empresa int64, uso UsoIA, sugerencias, resumenes int32) error {
	q := mensajeriabd.New(t)
	mes, _, _ := m.mesDe(m.ahora())
	u, err := q.SumarUsoAsistente(ctx, mensajeriabd.SumarUsoAsistenteParams{EmpresaID: empresaPtrDe(empresa), Mes: mes,
		TokensEntrada: uso.Entrada, TokensSalida: uso.Salida, Sugerencias: sugerencias, Resumenes: resumenes})
	if err != nil {
		return err
	}
	aj, err := leerAjustesAsistente(ctx, q, empresa)
	if err != nil || aj == nil {
		return err
	}
	total := u.TokensEntrada + u.TokensSalida
	a80, a100 := u.Avisado80, u.Avisado100
	emitir := func(porcentaje int) error {
		return t.Emitir("asistente.tope_alcanzado", map[string]any{"empresa_id": empresaPtrDe(empresa), "porcentaje": porcentaje,
			"mes": mes.Time.Format("2006-01"), "tokens": total, "tope": aj.TopeTokensMes})
	}
	if !a80 && total*10 >= aj.TopeTokensMes*8 {
		a80 = true
		if total < aj.TopeTokensMes {
			if err := emitir(80); err != nil {
				return err
			}
		}
	}
	if !a100 && total >= aj.TopeTokensMes {
		a100 = true
		if err := emitir(100); err != nil {
			return err
		}
	}
	if a80 != u.Avisado80 || a100 != u.Avisado100 {
		return q.MarcarAvisosTope(ctx, mensajeriabd.MarcarAvisosTopeParams{Empresa: empresa, Mes: mes, Avisado80: a80, Avisado100: a100})
	}
	return nil
}

// Casos ------------------------------------------------------------------------------------------

func nuevoVerAjustesAsistente(*Modulo) *auditoria.Caso[struct{}, AjustesAsistente] {
	return auditoria.Nuevo(auditoria.Caso[struct{}, AjustesAsistente]{
		Accion: "mensajeria.asistente_ver_ajustes", Permiso: nucleo.PermMensajeriaAsistenteConfigurar, AuditarExito: sinAuditar[AjustesAsistente],
		Logica: func(ctx context.Context, t *auditoria.Tx, _ struct{}) (AjustesAsistente, error) {
			empresa, err := empresaPersona(ctx)
			if err != nil {
				return AjustesAsistente{}, err
			}
			f, err := leerAjustesAsistente(ctx, mensajeriabd.New(t), empresa)
			if err != nil || f == nil {
				return ajustesPorDefecto(), err
			}
			return aAjustes(*f), nil
		},
	})
}

func nuevoEstadoAsistente(m *Modulo) *auditoria.Caso[struct{}, EstadoAsistente] {
	return auditoria.Nuevo(auditoria.Caso[struct{}, EstadoAsistente]{
		Accion: "conversaciones.asistente_estado", Permiso: nucleo.PermConversacionesSugerencias, AuditarExito: sinAuditar[EstadoAsistente],
		Logica: func(ctx context.Context, t *auditoria.Tx, _ struct{}) (EstadoAsistente, error) {
			empresa, err := empresaPersona(ctx)
			if err != nil {
				return EstadoAsistente{}, err
			}
			q := mensajeriabd.New(t)
			f, err := leerAjustesAsistente(ctx, q, empresa)
			if err != nil || f == nil {
				return EstadoAsistente{Modo: ModoAsistenteAutomatico}, err
			}
			pausado, err := m.pausadoPorTope(ctx, q, empresa, f.TopeTokensMes)
			return EstadoAsistente{Activo: f.Activo, Modo: f.Modo, Pausado: pausado}, err
		},
	})
}

// validarConexion: la URL (si hay) y lo que exige activar.
func validarConexionAsistente(proveedor, url, modelo string, conClave, activo bool) error {
	if url != "" {
		if err := validarURLIA(url); err != nil {
			var e *ErrorIA
			if errors.As(err, &e) {
				return nucleo.NuevoProblema(http.StatusUnprocessableEntity, "url_no_permitida", "URL no permitida", e.Detalle,
					nucleo.ErrorCampo{Field: "url", Message: e.Detalle, Rule: "url_no_permitida"})
			}
			return err
		}
	}
	if !activo {
		return nil
	}
	switch proveedor {
	case ProveedorAnthropic:
		if !conClave {
			return reglaConv("sin_configurar", "Para activar Anthropic falta la clave de API")
		}
	default:
		if url == "" || modelo == "" {
			return reglaConv("sin_configurar", "Para activar el asistente falta la URL del servidor o el modelo")
		}
	}
	return nil
}

func nuevoFijarAjustesAsistente(m *Modulo) *auditoria.Caso[EntradaAjustesAsistente, AjustesAsistente] {
	return auditoria.Nuevo(auditoria.Caso[EntradaAjustesAsistente, AjustesAsistente]{
		Accion: "mensajeria.asistente_ajustes", Permiso: nucleo.PermMensajeriaAsistenteConfigurar,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaAjustesAsistente) (AjustesAsistente, error) {
			empresa, err := empresaPersona(ctx)
			if err != nil {
				return AjustesAsistente{}, err
			}
			in.URL = strings.TrimRight(strings.TrimSpace(in.URL), "/")
			in.Modelo = strings.TrimSpace(in.Modelo)
			switch in.Proveedor {
			case ProveedorOllama, ProveedorOpenAICompatible, ProveedorAnthropic:
			default:
				return AjustesAsistente{}, campoConv("proveedor", "ollama, openai_compatible o anthropic", "enum")
			}
			if in.Modo != ModoAsistenteAutomatico && in.Modo != ModoAsistenteBoton {
				return AjustesAsistente{}, campoConv("modo", "automatico o boton", "enum")
			}
			if in.TopeTokensMes < 1000 || in.TopeTokensMes > 1_000_000_000 {
				return AjustesAsistente{}, campoConv("topeTokensMes", "Entre 1 000 y 1 000 000 000", "rango")
			}
			pe, err1 := numericDe(in.PrecioEntradaMillon)
			ps, err2 := numericDe(in.PrecioSalidaMillon)
			if err1 != nil {
				return AjustesAsistente{}, campoConv("precioEntradaMillon", "Decimal como texto, p. ej. \"3.00\"", "decimal")
			}
			if err2 != nil {
				return AjustesAsistente{}, campoConv("precioSalidaMillon", "Decimal como texto, p. ej. \"15.00\"", "decimal")
			}
			var clave *string
			if in.Clave != nil {
				c := strings.TrimSpace(*in.Clave)
				clave = &c
			}
			if clave != nil && *clave != "" && m.cfg.Cifrador == nil {
				return AjustesAsistente{}, nucleo.ReglaNegocio("cifrado_no_configurado", "Falta IRIS_CLAVE_CIFRADO: no se puede guardar la clave")
			}
			t.FijarEmpresa(empresa)
			q := mensajeriabd.New(t)
			antes, err := q.AsistenteAjustesParaActualizar(ctx, empresa)
			nuevo := errors.Is(err, pgx.ErrNoRows)
			if err != nil && !nuevo {
				return AjustesAsistente{}, err
			}
			if (nuevo && in.Version != 0) || (!nuevo && antes.Version != in.Version) {
				return AjustesAsistente{}, nucleo.ConflictoVersion("asistente_ajustes", empresa)
			}
			conClave := !nuevo && antes.ClaveCifrada != nil
			if clave != nil {
				conClave = *clave != ""
			}
			if err := validarConexionAsistente(in.Proveedor, in.URL, in.Modelo, conClave, in.Activo); err != nil {
				return AjustesAsistente{}, err
			}
			var f mensajeriabd.MensajeriaAsistenteAjuste
			if nuevo {
				f, err = q.InsertarAsistenteAjustes(ctx, mensajeriabd.InsertarAsistenteAjustesParams{EmpresaID: empresaPtrDe(empresa), Activo: in.Activo,
					Proveedor: in.Proveedor, Url: in.URL, Modelo: in.Modelo, Modo: in.Modo, FuenteManuales: in.Fuentes.Manuales, FuenteArgos: in.Fuentes.Argos,
					FuenteHorus: in.Fuentes.Horus, FuenteHistorial: in.Fuentes.Historial, TopeTokensMes: in.TopeTokensMes, PrecioEntradaMillon: pe, PrecioSalidaMillon: ps})
				if errors.Is(err, pgx.ErrNoRows) {
					return AjustesAsistente{}, nucleo.ConflictoVersion("asistente_ajustes", empresa)
				}
			} else {
				f, err = q.ActualizarAsistenteAjustes(ctx, mensajeriabd.ActualizarAsistenteAjustesParams{ID: antes.ID, Activo: in.Activo,
					Proveedor: in.Proveedor, Url: in.URL, Modelo: in.Modelo, Modo: in.Modo, FuenteManuales: in.Fuentes.Manuales, FuenteArgos: in.Fuentes.Argos,
					FuenteHorus: in.Fuentes.Horus, FuenteHistorial: in.Fuentes.Historial, TopeTokensMes: in.TopeTokensMes, PrecioEntradaMillon: pe, PrecioSalidaMillon: ps})
			}
			if err != nil {
				return AjustesAsistente{}, err
			}
			if clave != nil {
				params := mensajeriabd.FijarClaveAsistenteParams{ID: f.ID}
				if *clave != "" {
					cifrada, err := m.cfg.Cifrador.Cifrar([]byte(*clave), DatosAsociadosAsistente(f.ID))
					if err != nil {
						return AjustesAsistente{}, err
					}
					ahora := m.ahora()
					params.ClaveCifrada, params.ClaveGuardadaEn = cifrada, &ahora
				}
				if f, err = q.FijarClaveAsistente(ctx, params); err != nil {
					return AjustesAsistente{}, err
				}
			}
			// El tope cambió: los avisos del mes se recalculan con el tope nuevo (sin emitir).
			if !nuevo && antes.TopeTokensMes != f.TopeTokensMes {
				mes, _, _ := m.mesDe(m.ahora())
				u, err := m.usoDelMes(ctx, q, empresa, mes)
				if err != nil {
					return AjustesAsistente{}, err
				}
				total := u.TokensEntrada + u.TokensSalida
				if err := q.MarcarAvisosTope(ctx, mensajeriabd.MarcarAvisosTopeParams{Empresa: empresa, Mes: mes,
					Avisado80: total*10 >= f.TopeTokensMes*8, Avisado100: total >= f.TopeTokensMes}); err != nil {
					return AjustesAsistente{}, err
				}
			}
			t.FijarEntidad("asistente_ajustes", f.ID)
			vista := func(x AjustesAsistente) map[string]any {
				return map[string]any{"activo": x.Activo, "proveedor": x.Proveedor, "url": x.URL, "modelo": x.Modelo, "modo": x.Modo,
					"fuentes": x.Fuentes, "tope_tokens_mes": x.TopeTokensMes, "precio_entrada_millon": x.PrecioEntradaMillon,
					"precio_salida_millon": x.PrecioSalidaMillon, "clave_guardada": x.ClaveGuardadaEn != nil}
			}
			var previo map[string]any
			if !nuevo {
				previo = vista(aAjustes(antes))
			}
			r := aAjustes(f)
			campos := t.RegistrarCambios("asistente_ajustes", f.ID, previo, vista(r))
			if campos == nil {
				campos = []string{}
			}
			if in.Clave != nil && !slices.Contains(campos, "clave_guardada") {
				campos = append(campos, "clave_guardada")
			}
			return r, t.Emitir("asistente.ajustes_cambiados", map[string]any{"empresa_id": f.EmpresaID, "campos": campos})
		},
	})
}

// conexionAsistente arma la configuración del proveedor desde los ajustes (descifra la clave).
func (m *Modulo) conexionAsistente(f mensajeriabd.MensajeriaAsistenteAjuste) (ConfigProveedor, error) {
	cfg := ConfigProveedor{Proveedor: f.Proveedor, URL: f.Url, Modelo: f.Modelo}
	if f.ClaveCifrada != nil {
		if m.cfg.Cifrador == nil {
			return cfg, &ErrorIA{Codigo: ErrIASinConfig, Detalle: "Falta IRIS_CLAVE_CIFRADO para leer la clave"}
		}
		plano, err := m.cfg.Cifrador.Descifrar(f.ClaveCifrada, DatosAsociadosAsistente(f.ID))
		if err != nil {
			return cfg, &ErrorIA{Codigo: ErrIASinConfig, Detalle: "No se pudo leer la clave guardada; vuelve a escribirla"}
		}
		cfg.Clave = string(plano)
	}
	if cfg.Proveedor == ProveedorAnthropic && cfg.Modelo == "" {
		cfg.Modelo = AnthropicModeloPorDefecto
	}
	return cfg, nil
}

func (m *Modulo) proveedorDe(cfg ConfigProveedor, plazo time.Duration) (Proveedor, error) {
	return m.cfg.FabricaIA(cfg, NuevoHTTPIA(plazo))
}

// conexionPedida: la de la prueba o la lista de modelos; sin clave usa la guardada solo si el
// proveedor y la URL son los guardados (una clave no viaja a otro servidor).
func (m *Modulo) conexionPedida(ctx context.Context, t *auditoria.Tx, proveedor, url, modelo string, clave *string) (ConfigProveedor, error) {
	empresa, err := empresaPersona(ctx)
	if err != nil {
		return ConfigProveedor{}, err
	}
	url = strings.TrimRight(strings.TrimSpace(url), "/")
	f, err := leerAjustesAsistente(ctx, mensajeriabd.New(t), empresa)
	if err != nil {
		return ConfigProveedor{}, err
	}
	if proveedor == "" {
		if f == nil {
			return ConfigProveedor{}, reglaConv("sin_configurar", "Todavía no hay un proveedor guardado")
		}
		return m.conexionAsistente(*f)
	}
	if err := validarConexionAsistente(proveedor, url, modelo, true, false); err != nil {
		return ConfigProveedor{}, err
	}
	cfg := ConfigProveedor{Proveedor: proveedor, URL: url, Modelo: strings.TrimSpace(modelo)}
	if clave != nil {
		cfg.Clave = strings.TrimSpace(*clave)
	} else if f != nil && f.Proveedor == proveedor && f.Url == url {
		guardada, err := m.conexionAsistente(*f)
		if err != nil {
			return ConfigProveedor{}, err
		}
		cfg.Clave = guardada.Clave
	}
	if cfg.Proveedor == ProveedorAnthropic && cfg.Modelo == "" {
		cfg.Modelo = AnthropicModeloPorDefecto
	}
	return cfg, nil
}

// ProbarAsistente: lee la conexión, manda un pedido corto real (fuera de la transacción) y audita
// el resultado (sin la clave). No guarda nada ni cuenta para el tope.
type ProbarAsistente struct {
	m        *Modulo
	leer     *auditoria.Caso[EntradaProbarAsistente, conexionLeida]
	registro *auditoria.Caso[PruebaAsistente, PruebaAsistente]
}

type conexionLeida struct{ cfg ConfigProveedor }

func nuevoProbarAsistente(m *Modulo) *ProbarAsistente {
	return &ProbarAsistente{m: m,
		leer: auditoria.Nuevo(auditoria.Caso[EntradaProbarAsistente, conexionLeida]{
			Accion: "mensajeria.asistente_probar", Permiso: nucleo.PermMensajeriaAsistenteConfigurar, AuditarExito: sinAuditar[conexionLeida],
			Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaProbarAsistente) (conexionLeida, error) {
				cfg, err := m.conexionPedida(ctx, t, in.Proveedor, in.URL, in.Modelo, in.Clave)
				if err == nil && cfg.Modelo == "" {
					err = campoConv("modelo", "Elige un modelo para probar", "required")
				}
				return conexionLeida{cfg: cfg}, err
			},
		}),
		registro: auditoria.Nuevo(auditoria.Caso[PruebaAsistente, PruebaAsistente]{
			Accion: "mensajeria.asistente_probar", Permiso: nucleo.PermMensajeriaAsistenteConfigurar,
			Logica: func(ctx context.Context, t *auditoria.Tx, in PruebaAsistente) (PruebaAsistente, error) {
				empresa, err := empresaPersona(ctx)
				if err != nil {
					return PruebaAsistente{}, err
				}
				t.FijarEmpresa(empresa)
				t.Anotar(map[string]any{"estado": in.Estado, "latencia_ms": in.LatenciaMs})
				return in, nil
			},
		})}
}

// Ejecutar prueba la conexión.
func (p *ProbarAsistente) Ejecutar(ctx context.Context, in EntradaProbarAsistente) (PruebaAsistente, error) {
	c, err := p.leer.Ejecutar(ctx, in)
	if err != nil {
		return PruebaAsistente{}, err
	}
	r := PruebaAsistente{Estado: "ok", Detalle: "El servidor respondió"}
	inicio := time.Now()
	prov, err := p.m.proveedorDe(c.cfg, p.m.cfg.PlazoPruebaIA)
	if err == nil {
		cctx, cancelar := context.WithTimeout(ctx, p.m.cfg.PlazoPruebaIA)
		var uso UsoIA
		var resp RespuestaIA
		resp, uso, err = prov.Generar(cctx, PedidoIA{Modelo: c.cfg.Modelo, Instrucciones: "Eres una prueba de conexión.",
			Contenido: "Responde solo: ok", MaxTokens: 256})
		cancelar()
		r.LatenciaMs = time.Since(inicio).Milliseconds()
		if err == nil && uso.Salida > 0 && r.LatenciaMs > 0 {
			tps := float64(uso.Salida) / (float64(r.LatenciaMs) / 1000)
			tps = float64(int64(tps*10)) / 10
			r.TokensPorSegundo = &tps
		}
		if err == nil && resp.Rechazo {
			r.Detalle = "El servidor respondió (el modelo se negó a la prueba)"
		}
	}
	if err != nil {
		r.Estado, r.Detalle = "error", detalleIA(err)
		r.LatenciaMs = time.Since(inicio).Milliseconds()
	}
	return p.registro.Ejecutar(ctx, r)
}

func detalleIA(err error) string {
	var e *ErrorIA
	if errors.As(err, &e) {
		return e.Detalle
	}
	return "Falla inesperada al llamar al servidor de IA"
}

func codigoIA(err error) string {
	var e *ErrorIA
	if errors.As(err, &e) {
		return e.Codigo
	}
	return ErrIAProveedor
}

// ModelosAsistente lista los modelos del proveedor (fuera de la transacción).
type ModelosAsistente struct {
	m    *Modulo
	leer *auditoria.Caso[EntradaProbarAsistente, conexionLeida]
}

func nuevoModelosAsistente(m *Modulo) *ModelosAsistente {
	return &ModelosAsistente{m: m, leer: auditoria.Nuevo(auditoria.Caso[EntradaProbarAsistente, conexionLeida]{
		Accion: "mensajeria.asistente_modelos", Permiso: nucleo.PermMensajeriaAsistenteConfigurar, AuditarExito: sinAuditar[conexionLeida],
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaProbarAsistente) (conexionLeida, error) {
			cfg, err := m.conexionPedida(ctx, t, in.Proveedor, in.URL, "", nil)
			return conexionLeida{cfg: cfg}, err
		},
	})}
}

// Ejecutar lista los modelos.
func (k *ModelosAsistente) Ejecutar(ctx context.Context, in EntradaModelosAsistente) (ListaModelosAsistente, error) {
	c, err := k.leer.Ejecutar(ctx, EntradaProbarAsistente{Proveedor: in.Proveedor, URL: in.URL})
	if err != nil {
		return ListaModelosAsistente{}, err
	}
	prov, err := k.m.proveedorDe(c.cfg, k.m.cfg.PlazoPruebaIA)
	if err != nil {
		return ListaModelosAsistente{}, problemaIA(err)
	}
	cctx, cancelar := context.WithTimeout(ctx, k.m.cfg.PlazoPruebaIA)
	defer cancelar()
	ms, err := prov.Modelos(cctx)
	if err != nil {
		return ListaModelosAsistente{}, problemaIA(err)
	}
	if ms == nil {
		ms = []ModeloIA{}
	}
	return ListaModelosAsistente{Data: ms}, nil
}

// problemaIA traduce una falla del proveedor a 502 (o 422 si es la URL o la configuración).
func problemaIA(err error) error {
	var e *ErrorIA
	if !errors.As(err, &e) {
		return err
	}
	switch e.Codigo {
	case "url_no_permitida", ErrIASinConfig, ErrIATope:
		return nucleo.NuevoProblema(http.StatusUnprocessableEntity, e.Codigo, "No se puede", e.Detalle)
	}
	return nucleo.NuevoProblema(http.StatusBadGateway, e.Codigo, "El proveedor de IA falló", e.Detalle)
}

func nuevoUsoAsistente(m *Modulo) *auditoria.Caso[EntradaUsoAsistente, UsoAsistente] {
	return auditoria.Nuevo(auditoria.Caso[EntradaUsoAsistente, UsoAsistente]{
		Accion: "mensajeria.asistente_uso", Permiso: nucleo.PermMensajeriaAsistenteConfigurar, AuditarExito: sinAuditar[UsoAsistente],
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaUsoAsistente) (UsoAsistente, error) {
			empresa, err := empresaPersona(ctx)
			if err != nil {
				return UsoAsistente{}, err
			}
			ref := m.ahora()
			if in.Mes != "" {
				p, err := time.ParseInLocation("2006-01", in.Mes, m.cfg.Zona)
				if err != nil {
					return UsoAsistente{}, campoConv("mes", "YYYY-MM", "pattern")
				}
				ref = p.Add(12 * time.Hour)
			}
			mes, desde, hasta := m.mesDe(ref)
			q := mensajeriabd.New(t)
			u, err := m.usoDelMes(ctx, q, empresa, mes)
			if err != nil {
				return UsoAsistente{}, err
			}
			met, err := q.MetricasSugerencias(ctx, mensajeriabd.MetricasSugerenciasParams{Empresa: empresa, Desde: desde, Hasta: hasta})
			if err != nil {
				return UsoAsistente{}, err
			}
			r := UsoAsistente{Mes: mes.Time.Format("2006-01"), TokensEntrada: u.TokensEntrada, TokensSalida: u.TokensSalida,
				Sugerencias: met.Total, Usadas: met.Usadas, Utiles: met.Utiles, NoUtiles: met.NoUtiles, Resumenes: int64(u.Resumenes),
				TopeTokensMes: TopeTokensPorDefecto}
			aj, err := leerAjustesAsistente(ctx, q, empresa)
			if err != nil {
				return UsoAsistente{}, err
			}
			if aj != nil {
				r.TopeTokensMes = aj.TopeTokensMes
				pe, ps := ratDe(aj.PrecioEntradaMillon), ratDe(aj.PrecioSalidaMillon)
				if pe != nil && ps != nil {
					c := new(big.Rat).Mul(pe, new(big.Rat).SetInt64(u.TokensEntrada))
					c.Add(c, new(big.Rat).Mul(ps, new(big.Rat).SetInt64(u.TokensSalida)))
					c.Quo(c, big.NewRat(1_000_000, 1))
					s := c.FloatString(2)
					r.Costo = &s
				}
			}
			total := u.TokensEntrada + u.TokensSalida
			r.Porcentaje = total * 100 / r.TopeTokensMes
			actual, _, _ := m.mesDe(m.ahora())
			r.Pausado = mes == actual && total >= r.TopeTokensMes
			return r, nil
		},
	})
}

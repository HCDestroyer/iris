package mensajeria

import (
	"encoding/json"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	mensajeriabd "github.com/HCDestroyer/iris/apps/api/internal/mensajeria/bd"
)

// Plantillas de WhatsApp administrables (contratos/plantillas-whatsapp.md). Viven por cuenta de
// WhatsApp (WABA); la empresa dueña de la cuenta decide en qué usos sirve cada una y qué código del
// catálogo de Iris atiende (Argos sigue pidiendo avisos por código, contrato §6.3).

// Categorías de Meta.
const (
	CategoriaUtility       = "UTILITY"
	CategoriaMarketing     = "MARKETING"
	CategoriaAutenticacion = "AUTHENTICATION"
)

// Estados de una plantilla: BORRADOR (solo en Iris) o el de Meta.
const (
	EstadoPWBorrador  = "BORRADOR"
	EstadoPWPendiente = "PENDING"
	EstadoPWAprobada  = "APPROVED"
	EstadoPWRechazada = "REJECTED"
	EstadoPWPausada   = "PAUSED"
	EstadoPWInhabil   = "DISABLED"
	EstadoPWMarcada   = "FLAGGED"
	EstadoPWBorrada   = "DELETED"
)

// EstadosMetaPW son los estados que Meta informa (y que Iris guarda tal cual).
var EstadosMetaPW = []string{"PENDING", "APPROVED", "REJECTED", "PAUSED", "DISABLED", "FLAGGED", "IN_APPEAL", "LIMIT_EXCEEDED", "LOCKED",
	"ARCHIVED", "PENDING_DELETION", "DELETED"}

// Usos de una plantilla en Iris.
const (
	UsoPWAvisos         = "avisos"
	UsoPWConversaciones = "conversaciones"
	UsoPWAsistente      = "asistente"
)

// Origen de una plantilla.
const (
	OrigenPWIris     = "iris"
	OrigenPWSugerida = "sugerida"
	OrigenPWMeta     = "meta"
	OrigenPWMigrada  = "migrada"
	OrigenPWCanal    = "canal"
)

// EncabezadoPWA es el encabezado (opcional) de la plantilla.
type EncabezadoPWA struct {
	Tipo  string  `json:"tipo" enum:"texto,imagen,video,documento,ubicacion"`
	Texto *string `json:"texto,omitempty" maxLength:"200" doc:"Solo con tipo texto: hasta 60 caracteres y un dato"`
	// Archivo de ejemplo para Meta (imagen, video o documento): un adjunto ya subido de Iris; al enviar
	// a Meta, Iris lo sube por la carga reanudable y usa el handle.
	AdjuntoID *uuid.UUID `json:"adjuntoId,omitempty" doc:"Archivo de ejemplo (imagen, video o documento), subido antes como adjunto"`
}

// BotonPWA es un botón de la plantilla.
type BotonPWA struct {
	Tipo     string  `json:"tipo" enum:"respuesta,url,telefono,copiar_codigo"`
	Texto    string  `json:"texto" maxLength:"100" doc:"Lo que dice el botón (hasta 25 caracteres)"`
	URL      *string `json:"url,omitempty" maxLength:"4000" doc:"url: dirección https; puede terminar con un dato ({{cliente_codigo}})"`
	Telefono *string `json:"telefono,omitempty" maxLength:"40" doc:"telefono: número en formato internacional (+502…)"`
	Ejemplo  *string `json:"ejemplo,omitempty" maxLength:"200" doc:"url con dato: el valor de ejemplo del final; copiar_codigo: el código de ejemplo"`
}

// VariablePWA es un dato del texto, con su ejemplo para Meta.
type VariablePWA struct {
	Nombre  string  `json:"nombre" maxLength:"60" doc:"Como va en el texto entre llaves: cliente_nombre o 1, 2… (por posición)"`
	Dato    *string `json:"dato,omitempty" maxLength:"60" doc:"Qué dato de Iris va aquí (GET /plantillas-whatsapp/datos); null = sin definir (no sirve para avisos)"`
	Ejemplo string  `json:"ejemplo" maxLength:"200" doc:"Ejemplo para Meta (obligatorio; nunca datos reales)"`
}

// AutenticacionPWA: el texto de una plantilla de verificación lo pone Meta.
type AutenticacionPWA struct {
	CaducaMinutos  *int `json:"caducaMinutos,omitempty" minimum:"1" maximum:"90" doc:"El código caduca en estos minutos (texto de Meta)"`
	AvisoSeguridad bool `json:"avisoSeguridad" doc:"Agrega «Por tu seguridad, no lo compartas.»"`
}

// DefinicionPWA es la plantilla en la forma de Iris.
type DefinicionPWA struct {
	Encabezado    *EncabezadoPWA    `json:"encabezado,omitempty"`
	Cuerpo        string            `json:"cuerpo" maxLength:"4000" doc:"Mensaje con {{dato}}; hasta 1024 caracteres (verificación: lo pone Meta)"`
	Pie           *string           `json:"pie,omitempty" maxLength:"200" doc:"Texto gris bajo el mensaje, hasta 60 caracteres y sin datos"`
	Botones       []BotonPWA        `json:"botones" maxItems:"20"`
	Variables     []VariablePWA     `json:"variables" maxItems:"40" doc:"Cada dato del texto con su ejemplo"`
	Autenticacion *AutenticacionPWA `json:"autenticacion,omitempty"`
}

// RechazoPWA es lo que dijo Meta al rechazarla (rejection_info).
type RechazoPWA struct {
	Motivo        string `json:"motivo"`
	Recomendacion string `json:"recomendacion"`
}

// ParametroPWA dice qué dato de Iris va en cada variable de la plantilla al mandarla.
type ParametroPWA struct {
	Componente string `json:"componente" enum:"encabezado,cuerpo,boton"`
	Boton      *int   `json:"boton,omitempty" doc:"Índice del botón (componente boton)"`
	Nombre     string `json:"nombre" doc:"Nombre de la variable en Meta (o su posición)"`
	Dato       string `json:"dato" doc:"Clave del dato de Iris"`
}

// EdicionesPWA: lo que queda de ediciones de una aprobada (Meta: 1 cada 24 h y 10 en 30 días).
type EdicionesPWA struct {
	Ultimas24h    int        `json:"ultimas24h"`
	Ultimos30d    int        `json:"ultimos30d"`
	PuedeEditarEn *time.Time `json:"puedeEditarEn" nullable:"true" doc:"Si ya no puede editarse: desde cuándo (null = puede)"`
}

// PlantillaWA es una plantilla de una cuenta.
type PlantillaWA struct {
	ID                  int64          `json:"id"`
	EmpresaID           *int64         `json:"empresaId" nullable:"true"`
	WabaID              string         `json:"wabaId"`
	MetaID              *string        `json:"metaId" nullable:"true"`
	Nombre              string         `json:"nombre" doc:"Nombre en Meta"`
	Idioma              string         `json:"idioma"`
	Etiqueta            string         `json:"etiqueta" doc:"Nombre para personas"`
	Categoria           string         `json:"categoria" enum:"UTILITY,MARKETING,AUTHENTICATION"`
	CategoriaAnterior   *string        `json:"categoriaAnterior" nullable:"true" doc:"Meta la reclasificó: la que tenía (hasta que alguien la revise)"`
	CategoriaCambiadaEn *time.Time     `json:"categoriaCambiadaEn" nullable:"true"`
	CategoriaProxima    *string        `json:"categoriaProxima" nullable:"true" doc:"Meta avisó que la reclasificará a esta"`
	CategoriaProximaEn  *time.Time     `json:"categoriaProximaEn" nullable:"true"`
	Estado              string         `json:"estado" enum:"BORRADOR,PENDING,APPROVED,REJECTED,PAUSED,DISABLED,FLAGGED,IN_APPEAL,LIMIT_EXCEEDED,LOCKED,ARCHIVED,PENDING_DELETION,DELETED"`
	EstadoEn            time.Time      `json:"estadoEn"`
	Motivo              *string        `json:"motivo" nullable:"true" doc:"reason de Meta (INVALID_FORMAT, PROMOTIONAL…)"`
	Rechazo             *RechazoPWA    `json:"rechazo"`
	Calidad             string         `json:"calidad" enum:"GREEN,YELLOW,RED,UNKNOWN"`
	PausadaHasta        *time.Time     `json:"pausadaHasta" nullable:"true"`
	Formato             string         `json:"formato" enum:"NAMED,POSITIONAL" doc:"Variables con nombre o por posición"`
	Definicion          DefinicionPWA  `json:"definicion"`
	Codigo              *string        `json:"codigo" nullable:"true" doc:"Código del catálogo de Iris que atiende (Argos pide avisos por código)"`
	Parametros          []ParametroPWA `json:"parametros"`
	Usos                []string       `json:"usos" doc:"avisos, conversaciones, asistente"`
	Origen              string         `json:"origen" enum:"iris,sugerida,meta,migrada,canal"`
	Sugerida            *string        `json:"sugerida" nullable:"true"`
	Ediciones           EdicionesPWA   `json:"ediciones"`
	EnviadaEn           *time.Time     `json:"enviadaEn" nullable:"true"`
	BorradaEn           *time.Time     `json:"borradaEn" nullable:"true"`
	BorradaPor          *string        `json:"borradaPor" nullable:"true" enum:"iris,meta"`
	NombreLibreEn       *time.Time     `json:"nombreLibreEn" nullable:"true" doc:"Meta no deja reusar el nombre hasta esta fecha"`
	SincronizadaEn      *time.Time     `json:"sincronizadaEn" nullable:"true"`
	// Atender: por qué la lista la marca (null = nada que hacer).
	Atender   *string   `json:"atender" nullable:"true" enum:"rechazada,pausada,deshabilitada,bloqueada,borrada_en_uso,reclasificada,reclasificacion_proxima"`
	Version   int32     `json:"version"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

var reVariablePW = regexp.MustCompile(`\{\{\s*([^{}]*?)\s*\}\}`)

func aPlantillaWA(f mensajeriabd.MensajeriaPlantillaWA, ahora time.Time) PlantillaWA {
	p := PlantillaWA{ID: f.ID, EmpresaID: f.EmpresaID, WabaID: f.WabaID, MetaID: f.MetaID, Nombre: f.Nombre, Idioma: f.Idioma, Etiqueta: f.Etiqueta,
		Categoria: f.Categoria, CategoriaAnterior: f.CategoriaAnterior, CategoriaCambiadaEn: f.CategoriaCambiadaEn, CategoriaProxima: f.CategoriaProxima,
		CategoriaProximaEn: f.CategoriaProximaEn, Estado: f.Estado, EstadoEn: f.EstadoEn, Motivo: f.Motivo, Calidad: f.Calidad, PausadaHasta: f.PausadaHasta,
		Formato: f.Formato, Codigo: f.Codigo, Usos: lista(f.Usos), Origen: f.Origen, Sugerida: f.Sugerida, EnviadaEn: f.EnviadaEn, BorradaEn: f.BorradaEn,
		BorradaPor: f.BorradaPor, NombreLibreEn: f.NombreLibreEn, SincronizadaEn: f.SincronizadaEn, Version: f.Version, CreatedAt: f.CreatedAt, UpdatedAt: f.UpdatedAt}
	p.Definicion = definicionDe(f.Componentes)
	p.Parametros = []ParametroPWA{}
	_ = json.Unmarshal(f.Parametros, &p.Parametros)
	p.Parametros = lista(p.Parametros)
	if len(f.Rechazo) > 0 {
		var r RechazoPWA
		if json.Unmarshal(f.Rechazo, &r) == nil {
			p.Rechazo = &r
		}
	}
	p.Ediciones = edicionesDe(f.Ediciones, ahora)
	p.Atender = atenderDe(f)
	return p
}

func definicionDe(crudo []byte) DefinicionPWA {
	var d DefinicionPWA
	_ = json.Unmarshal(crudo, &d)
	d.Botones, d.Variables = lista(d.Botones), lista(d.Variables)
	return d
}

// edicionesDe cuenta las ediciones de una aprobada en las últimas 24 h y 30 días.
func edicionesDe(ediciones []time.Time, ahora time.Time) EdicionesPWA {
	e := EdicionesPWA{}
	var ultima24 time.Time
	var en30 []time.Time
	for _, t := range ediciones {
		if t.After(ahora.Add(-24 * time.Hour)) {
			e.Ultimas24h++
			if t.After(ultima24) {
				ultima24 = t
			}
		}
		if t.After(ahora.Add(-30 * 24 * time.Hour)) {
			e.Ultimos30d++
			en30 = append(en30, t)
		}
	}
	var desde time.Time
	if e.Ultimas24h >= 1 {
		desde = ultima24.Add(24 * time.Hour)
	}
	if e.Ultimos30d >= 10 {
		slices.SortFunc(en30, func(a, b time.Time) int { return a.Compare(b) })
		if d := en30[len(en30)-10].Add(30 * 24 * time.Hour); d.After(desde) {
			desde = d
		}
	}
	if !desde.IsZero() {
		e.PuedeEditarEn = &desde
	}
	return e
}

func atenderDe(f mensajeriabd.MensajeriaPlantillaWA) *string {
	a := ""
	switch {
	case f.Estado == EstadoPWBorrada && derefTexto(f.BorradaPor) == "meta" && (f.Codigo != nil || len(f.Usos) > 0):
		a = "borrada_en_uso"
	case f.Estado == EstadoPWRechazada:
		a = "rechazada"
	case f.Estado == EstadoPWPausada:
		a = "pausada"
	case f.Estado == EstadoPWInhabil:
		a = "deshabilitada"
	case f.Estado == "LOCKED" || f.Estado == "LIMIT_EXCEEDED":
		a = "bloqueada"
	case f.CategoriaAnterior != nil:
		a = "reclasificada"
	case f.CategoriaProxima != nil:
		a = "reclasificacion_proxima"
	}
	if a == "" {
		return nil
	}
	return &a
}

// variablesDeTexto devuelve los nombres de las variables {{x}} en orden de aparición.
func variablesDeTexto(s string) []string {
	out := []string{}
	for _, m := range reVariablePW.FindAllStringSubmatch(s, -1) {
		out = append(out, m[1])
	}
	return out
}

// Diccionario de datos -----------------------------------------------------------------------------

// DatoPWA es un dato amigable que se puede poner en una plantilla.
type DatoPWA struct {
	Clave    string   `json:"clave"`
	Etiqueta string   `json:"etiqueta" doc:"Cómo lo ve quien escribe la plantilla («Nombre del cliente»)"`
	Origen   string   `json:"origen" doc:"De dónde lo toma Iris («Cliente en Argos · nombre»)"`
	Ejemplo  string   `json:"ejemplo" doc:"Ejemplo para Meta (nunca un dato real)"`
	Grupo    string   `json:"grupo" enum:"cliente,cobro,orden,aviso,verificacion,empresa"`
	Codigos  []string `json:"codigos" doc:"Códigos del catálogo que lo traen (vacío = todavía ningún envío de Iris lo llena)"`
}

// datosArgosPW son los datos que se leen de la instantánea de Argos (no vienen en el aviso).
var datosArgosPW = []string{"cliente_nombre", "cliente_codigo", "plan", "empresa_nombre"}

// DiccionarioDatos son los datos amigables (variables) con su origen en Iris o Argos.
var DiccionarioDatos = []DatoPWA{
	{Clave: "cliente_nombre", Etiqueta: "Nombre del cliente", Origen: "Cliente en Argos · nombre", Ejemplo: "María López", Grupo: "cliente"},
	{Clave: "cliente_codigo", Etiqueta: "Código de cliente", Origen: "Cliente en Argos · código", Ejemplo: "C-001284", Grupo: "cliente"},
	{Clave: "plan", Etiqueta: "Plan", Origen: "Servicio en Argos · plan", Ejemplo: "Fibra 100 Mbps", Grupo: "cliente"},
	{Clave: "empresa_nombre", Etiqueta: "Nombre de tu empresa", Origen: "Empresa en Argos · nombre", Ejemplo: "Kingnet Solution", Grupo: "empresa"},
	{Clave: "monto_con_moneda", Etiqueta: "Monto", Origen: "Factura o pago · total con moneda", Ejemplo: "Q 250.00", Grupo: "cobro"},
	{Clave: "monto", Etiqueta: "Monto (solo el número)", Origen: "Factura o pago · total", Ejemplo: "250.00", Grupo: "cobro"},
	{Clave: "moneda", Etiqueta: "Moneda", Origen: "Factura o pago · moneda (GTQ)", Ejemplo: "GTQ", Grupo: "cobro"},
	{Clave: "fecha_corte", Etiqueta: "Fecha de corte", Origen: "Factura · fecha de vencimiento (Guatemala)", Ejemplo: "viernes 28 de noviembre", Grupo: "cobro"},
	{Clave: "vence_en", Etiqueta: "Fecha de vencimiento (2026-11-28)", Origen: "Factura o promesa · vence", Ejemplo: "2026-11-28", Grupo: "cobro"},
	{Clave: "fecha", Etiqueta: "Fecha del pago", Origen: "Pago en Argos · fecha", Ejemplo: "2026-11-20", Grupo: "cobro"},
	{Clave: "motivo", Etiqueta: "Motivo", Origen: "Boleta · motivo (texto de Iris)", Ejemplo: "La revisamos en menos de 24 horas.", Grupo: "cobro"},
	{Clave: "hasta", Etiqueta: "Válida hasta", Origen: "Reconexión temporal · hasta (Guatemala)", Ejemplo: "mañana a las 8:00", Grupo: "cobro"},
	{Clave: "zona", Etiqueta: "Zona", Origen: "Mantenimiento · zona", Ejemplo: "Mixco Centro", Grupo: "aviso"},
	{Clave: "inicio", Etiqueta: "Inicio", Origen: "Mantenimiento · desde", Ejemplo: "sábado 29 a la 01:00", Grupo: "aviso"},
	{Clave: "fin", Etiqueta: "Fin", Origen: "Mantenimiento · hasta", Ejemplo: "04:00", Grupo: "aviso"},
	{Clave: "tecnico_nombre", Etiqueta: "Nombre del técnico", Origen: "Agenda · técnico de la orden", Ejemplo: "Carlos Méndez", Grupo: "orden"},
	{Clave: "orden_numero", Etiqueta: "Número de orden", Origen: "Orden de trabajo · número", Ejemplo: "OT-001284", Grupo: "orden"},
	{Clave: "orden_tipo", Etiqueta: "Tipo de visita", Origen: "Orden de trabajo · tipo", Ejemplo: "instalación", Grupo: "orden"},
	{Clave: "cita_fecha", Etiqueta: "Fecha de la cita", Origen: "Agenda · fecha de la cita (Guatemala)", Ejemplo: "martes 25 de noviembre", Grupo: "orden"},
	{Clave: "cita_ventana", Etiqueta: "Horario de la cita", Origen: "Agenda · ventana acordada", Ejemplo: "8:00 y 12:00", Grupo: "orden"},
	{Clave: "llegada_minutos", Etiqueta: "Minutos para llegar", Origen: "Ruta del técnico · llegada estimada", Ejemplo: "20", Grupo: "orden"},
	{Clave: "codigo_verificacion", Etiqueta: "Código de verificación", Origen: "Portal · código de un solo uso", Ejemplo: "482915", Grupo: "verificacion"},
}

func init() {
	for i := range DiccionarioDatos {
		d := &DiccionarioDatos[i]
		d.Codigos = []string{}
		for _, p := range Catalogo {
			if slices.Contains(datosDeCodigo(p), d.Clave) {
				d.Codigos = append(d.Codigos, p.Codigo)
			}
		}
	}
}

// DatoPorClave busca en el diccionario.
func DatoPorClave(clave string) (DatoPWA, bool) {
	i := slices.IndexFunc(DiccionarioDatos, func(d DatoPWA) bool { return d.Clave == clave })
	if i < 0 {
		return DatoPWA{}, false
	}
	return DiccionarioDatos[i], true
}

func tieneVariable(p Plantilla, nombre string) bool {
	return slices.ContainsFunc(p.Variables, func(v Variable) bool { return v.Nombre == nombre })
}

// datosDeCodigo: los datos que trae un envío con ese código (sus variables, los derivados y los de
// la instantánea de Argos).
func datosDeCodigo(p Plantilla) []string {
	out := []string{}
	for _, v := range p.Variables {
		out = append(out, v.Nombre)
	}
	if tieneVariable(p, "monto") && tieneVariable(p, "moneda") {
		out = append(out, "monto_con_moneda")
	}
	if tieneVariable(p, "vence_en") {
		out = append(out, "fecha_corte")
	}
	return append(out, datosArgosPW...)
}

var simbolosMoneda = map[string]string{"GTQ": "Q", "USD": "US$", "MXN": "MX$", "HNL": "L", "EUR": "€"}

var mesesES = []string{"", "enero", "febrero", "marzo", "abril", "mayo", "junio", "julio", "agosto", "septiembre", "octubre", "noviembre", "diciembre"}
var diasES = []string{"domingo", "lunes", "martes", "miércoles", "jueves", "viernes", "sábado"}

// fechaLarga: «viernes 28 de noviembre».
func fechaLarga(t time.Time) string {
	return diasES[t.Weekday()] + " " + strings.TrimLeft(t.Format("02"), "0") + " de " + mesesES[t.Month()]
}

// datosDerivados agrega a los valores del aviso los datos que se calculan de ellos.
func datosDerivados(p Plantilla, v map[string]string) map[string]string {
	out := map[string]string{}
	for _, d := range p.Variables {
		if _, ok := v[d.Nombre]; ok {
			out[d.Nombre] = p.valor(d, v)
		}
	}
	if m, ok := v["monto"]; ok {
		if mon, ok := v["moneda"]; ok {
			s := simbolosMoneda[mon]
			if s == "" {
				s = mon
			}
			out["monto_con_moneda"] = s + " " + m
		}
	}
	if f, ok := v["vence_en"]; ok {
		if t, err := time.Parse(time.DateOnly, f); err == nil {
			out["fecha_corte"] = fechaLarga(t)
		}
	}
	return out
}

// Sugeridas (diccionario de fábrica) ---------------------------------------------------------------

// SugeridaPWA es una plantilla de fábrica lista para crear con un toque.
type SugeridaPWA struct {
	Clave      string        `json:"clave"`
	Etiqueta   string        `json:"etiqueta"`
	Categoria  string        `json:"categoria" enum:"UTILITY,MARKETING,AUTHENTICATION"`
	Codigo     *string       `json:"codigo" nullable:"true" doc:"Código del catálogo que atiende (null = para usos futuros: órdenes, verificación)"`
	Usos       []string      `json:"usos"`
	Para       string        `json:"para" doc:"Dónde la usa Iris, en llano"`
	Definicion DefinicionPWA `json:"definicion"`
}

func ptr[T any](v T) *T { return &v }

func varsPW(claves ...string) []VariablePWA {
	out := make([]VariablePWA, 0, len(claves))
	for _, c := range claves {
		d, _ := DatoPorClave(c)
		out = append(out, VariablePWA{Nombre: c, Dato: ptr(c), Ejemplo: d.Ejemplo})
	}
	return out
}

// Sugeridas son las 7 plantillas de fábrica para un ISP (avisos de servicio, sin promociones).
var Sugeridas = []SugeridaPWA{
	{Clave: "aviso_corte", Etiqueta: "Aviso de corte", Categoria: CategoriaUtility, Codigo: ptr("corte_por_falta_de_pago"), Usos: []string{UsoPWAvisos},
		Para: "Avisos de Argos", Definicion: DefinicionPWA{
			Cuerpo:    "Hola {{cliente_nombre}}, tu servicio fue suspendido por un saldo pendiente de {{monto_con_moneda}}. Al pagar se reactiva el mismo día.",
			Variables: varsPW("cliente_nombre", "monto_con_moneda"), Botones: []BotonPWA{}}},
	{Clave: "recordatorio_pago", Etiqueta: "Recordatorio de pago", Categoria: CategoriaUtility, Codigo: ptr("factura_por_vencer"),
		Usos: []string{UsoPWAvisos, UsoPWConversaciones}, Para: "Avisos de Argos e iniciar conversación", Definicion: DefinicionPWA{
			Cuerpo:    "Hola {{cliente_nombre}}, tu factura de {{monto_con_moneda}} vence el {{fecha_corte}}. Si ya pagaste, ignora este mensaje.",
			Variables: varsPW("cliente_nombre", "monto_con_moneda", "fecha_corte"), Botones: []BotonPWA{{Tipo: "respuesta", Texto: "Ya pagué"}}}},
	{Clave: "visita_programada", Etiqueta: "Visita técnica programada", Categoria: CategoriaUtility, Usos: []string{UsoPWAvisos, UsoPWAsistente},
		Para: "Avisos de órdenes y asistente", Definicion: DefinicionPWA{
			Cuerpo:    "Hola {{cliente_nombre}}, programamos tu {{orden_tipo}} para el {{cita_fecha}} entre {{cita_ventana}}. ¿Te queda bien?",
			Variables: varsPW("cliente_nombre", "orden_tipo", "cita_fecha", "cita_ventana"),
			Botones:   []BotonPWA{{Tipo: "respuesta", Texto: "Sí, confirmo"}, {Tipo: "respuesta", Texto: "Cambiar fecha"}}}},
	{Clave: "tecnico_en_camino", Etiqueta: "Técnico en camino", Categoria: CategoriaUtility, Usos: []string{UsoPWAvisos}, Para: "Avisos de órdenes",
		Definicion: DefinicionPWA{
			Cuerpo:    "Hola {{cliente_nombre}}, {{tecnico_nombre}} va en camino a tu casa y llega en unos {{llegada_minutos}} minutos.",
			Variables: varsPW("cliente_nombre", "tecnico_nombre", "llegada_minutos"), Botones: []BotonPWA{}}},
	{Clave: "orden_cerrada", Etiqueta: "Orden cerrada", Categoria: CategoriaUtility, Usos: []string{UsoPWAvisos}, Para: "Avisos de órdenes y encuestas",
		Definicion: DefinicionPWA{
			Cuerpo:    "Terminamos tu {{orden_tipo}} (orden {{orden_numero}}). ¿Quedó todo funcionando?",
			Variables: varsPW("orden_tipo", "orden_numero"),
			Botones:   []BotonPWA{{Tipo: "respuesta", Texto: "Todo bien"}, {Tipo: "respuesta", Texto: "Tengo un problema"}}}},
	{Clave: "codigo_verificacion", Etiqueta: "Código de verificación", Categoria: CategoriaAutenticacion, Usos: []string{}, Para: "Portal",
		Definicion: DefinicionPWA{Autenticacion: &AutenticacionPWA{CaducaMinutos: ptr(10), AvisoSeguridad: true}, Variables: []VariablePWA{},
			Botones: []BotonPWA{{Tipo: "copiar_codigo", Texto: "Copiar código", Ejemplo: ptr("482915")}}}},
	{Clave: "mantenimiento", Etiqueta: "Mantenimiento en tu zona", Categoria: CategoriaUtility, Codigo: ptr("corte_programado"), Usos: []string{UsoPWAvisos},
		Para: "Cortes programados", Definicion: DefinicionPWA{
			Cuerpo:    "Haremos mantenimiento en {{zona}} de {{inicio}} a {{fin}}. Tu internet puede fallar en ese horario.",
			Variables: varsPW("zona", "inicio", "fin"), Botones: []BotonPWA{}}},
}

// SugeridaPorClave busca una sugerida.
func SugeridaPorClave(clave string) (SugeridaPWA, bool) {
	i := slices.IndexFunc(Sugeridas, func(s SugeridaPWA) bool { return s.Clave == clave })
	if i < 0 {
		return SugeridaPWA{}, false
	}
	return Sugeridas[i], true
}

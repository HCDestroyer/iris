package mensajeria

import (
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
)

// Plantillas de avisos (contrato 0.6.0 §6.3). Iris mantiene el texto y la aprobación de Meta; quien
// pide el aviso solo usa el código. Las variables son las declaradas: otra clave es 422
// variable_desconocida, una que falta 422 variable_faltante.

// Formatos de las variables.
const (
	formatoMonto    = "monto"    // string decimal con dos decimales: "250.00"
	formatoMoneda   = "moneda"   // ISO 4217: "GTQ"
	formatoFecha    = "fecha"    // YYYY-MM-DD en hora de Guatemala
	formatoInstante = "instante" // YYYY-MM-DD o YYYY-MM-DDTHH:MM[:SS][zona]
	formatoTexto    = "texto"    // 1 a 200 caracteres
)

// Variable de una plantilla.
type Variable struct {
	Nombre  string `json:"nombre"`
	Formato string `json:"formato" enum:"monto,moneda,fecha,instante,texto" doc:"monto: decimal con dos decimales (\"250.00\"); moneda: ISO 4217; fecha: YYYY-MM-DD (Guatemala); instante: fecha o fecha y hora; texto: 1 a 200 caracteres"`
}

// Plantilla de aviso.
type Plantilla struct {
	Codigo    string
	Variables []Variable
	// Texto en español con {{variable}} (canal simulado y vista previa; en Meta, la plantilla
	// aprobada lleva los parámetros en este mismo orden).
	Texto string
	// Caducidad: variable de fecha tras cuyo día (Guatemala) el aviso ya no tiene sentido (vencido).
	Caducidad string
	// Urgente: admite prioridad urgente (salta el límite diario).
	Urgente bool
}

func vars(pares ...string) []Variable {
	v := make([]Variable, 0, len(pares)/2)
	for i := 0; i+1 < len(pares); i += 2 {
		v = append(v, Variable{Nombre: pares[i], Formato: pares[i+1]})
	}
	return v
}

var montoMoneda = []string{"monto", formatoMonto, "moneda", formatoMoneda}

// Catalogo de plantillas (§6.3), en el orden del contrato.
var Catalogo = []Plantilla{
	{Codigo: "factura_por_vencer", Variables: vars(append(montoMoneda, "vence_en", formatoFecha)...), Caducidad: "vence_en",
		Texto: "Hola, te recordamos que tu factura de {{moneda}} {{monto}} vence el {{vence_en}}."},
	{Codigo: "factura_vencida", Variables: vars(append(montoMoneda, "vence_en", formatoFecha)...),
		Texto: "Hola, tu factura de {{moneda}} {{monto}} venció el {{vence_en}}. Evita la suspensión del servicio pagando cuanto antes."},
	{Codigo: "pago_recibido", Variables: vars(append(montoMoneda, "fecha", formatoFecha)...),
		Texto: "Recibimos tu pago de {{moneda}} {{monto}} el {{fecha}}. ¡Gracias!"},
	{Codigo: "corte_por_falta_de_pago", Variables: vars(montoMoneda...),
		Texto: "Tu servicio fue suspendido por un saldo pendiente de {{moneda}} {{monto}}. Al pagar se reactiva."},
	{Codigo: "servicio_reactivado", Variables: []Variable{},
		Texto: "Tu servicio está activo de nuevo. ¡Gracias por tu pago!"},
	{Codigo: "bienvenida", Variables: vars("plan", formatoTexto),
		Texto: "¡Bienvenido! Tu servicio {{plan}} ya está activo."},
	{Codigo: "corte_programado", Variables: vars("inicio", formatoInstante, "fin", formatoInstante, "zona", formatoTexto), Urgente: true,
		Texto: "Mantenimiento programado en {{zona}} de {{inicio}} a {{fin}}. El servicio puede interrumpirse."},
	{Codigo: "boleta_recibida", Variables: vars(append(montoMoneda, "motivo", formatoTexto)...),
		Texto: "Recibimos tu boleta de {{moneda}} {{monto}}. La revisamos y te avisamos. {{motivo}}"},
	{Codigo: "boleta_confirmada", Variables: vars(append(montoMoneda, "motivo", formatoTexto)...),
		Texto: "Confirmamos tu pago de {{moneda}} {{monto}}. {{motivo}}"},
	{Codigo: "boleta_rechazada", Variables: vars(append(montoMoneda, "motivo", formatoTexto)...),
		Texto: "No pudimos confirmar tu boleta de {{moneda}} {{monto}}: {{motivo}}"},
	{Codigo: "boleta_falta_dato", Variables: vars(append(montoMoneda, "motivo", formatoTexto)...),
		Texto: "A tu boleta de {{moneda}} {{monto}} le falta un dato: {{motivo}}. Envíala de nuevo, por favor."},
	{Codigo: "reconexion_temporal", Variables: vars("hasta", formatoInstante), Caducidad: "hasta",
		Texto: "Reconectamos tu servicio de forma temporal hasta {{hasta}} mientras revisamos tu pago."},
	{Codigo: "promesa_aceptada", Variables: vars("vence_en", formatoFecha, "monto", formatoMonto), Caducidad: "vence_en",
		Texto: "Registramos tu promesa de pago de {{monto}} para el {{vence_en}}."},
}

// PlantillaPorCodigo busca en el catálogo.
func PlantillaPorCodigo(codigo string) (Plantilla, bool) {
	i := slices.IndexFunc(Catalogo, func(p Plantilla) bool { return p.Codigo == codigo })
	if i < 0 {
		return Plantilla{}, false
	}
	return Catalogo[i], true
}

// CodigosPlantillas son los códigos del catálogo (enum del OpenAPI).
func CodigosPlantillas() []string {
	c := make([]string, 0, len(Catalogo))
	for _, p := range Catalogo {
		c = append(c, p.Codigo)
	}
	return c
}

var (
	reMonto  = regexp.MustCompile(`^[0-9]{1,12}\.[0-9]{2}$`)
	reMoneda = regexp.MustCompile(`^[A-Z]{3}$`)
	reFecha  = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}$`)
	reVarNom = regexp.MustCompile(`^[a-z][a-z0-9_]{0,40}$`)
)

// validarVariables aplica §6.2: solo las claves declaradas, todas presentes y con su formato.
func (p Plantilla) validarVariables(v map[string]string) error {
	var errs []nucleo.ErrorCampo
	desconocidas := []string{}
	for k := range v {
		if !slices.ContainsFunc(p.Variables, func(d Variable) bool { return d.Nombre == k }) {
			desconocidas = append(desconocidas, k)
		}
	}
	if len(desconocidas) > 0 {
		sort.Strings(desconocidas)
		for _, k := range desconocidas {
			campo := "variables"
			if reVarNom.MatchString(k) {
				campo = "variables." + k
			}
			errs = append(errs, nucleo.ErrorCampo{Field: campo, Message: "La plantilla " + p.Codigo + " no declara esta variable", Rule: "variable_desconocida"})
		}
		return nucleo.NuevoProblema(422, "variable_desconocida", "Variable desconocida",
			"variables solo acepta las claves de la plantilla "+p.Codigo+": "+nombresVariables(p), errs...)
	}
	for _, d := range p.Variables {
		valor, ok := v[d.Nombre]
		campo := "variables." + d.Nombre
		if !ok {
			errs = append(errs, nucleo.ErrorCampo{Field: campo, Message: "Falta la variable", Rule: "requerida"})
			continue
		}
		if msg := validarFormato(d.Formato, valor); msg != "" {
			errs = append(errs, nucleo.ErrorCampo{Field: campo, Message: msg, Rule: d.Formato})
		}
	}
	if len(errs) > 0 {
		return nucleo.Validacion(errs...)
	}
	return nil
}

func nombresVariables(p Plantilla) string {
	if len(p.Variables) == 0 {
		return "(ninguna)"
	}
	n := make([]string, 0, len(p.Variables))
	for _, d := range p.Variables {
		n = append(n, d.Nombre)
	}
	return strings.Join(n, ", ")
}

func validarFormato(formato, valor string) string {
	switch formato {
	case formatoMonto:
		if !reMonto.MatchString(valor) {
			return "Monto como string decimal con dos decimales (250.00)"
		}
	case formatoMoneda:
		if !reMoneda.MatchString(valor) {
			return "Moneda ISO 4217 (GTQ)"
		}
	case formatoFecha:
		if _, err := time.Parse(time.DateOnly, valor); err != nil || !reFecha.MatchString(valor) {
			return "Fecha YYYY-MM-DD"
		}
	case formatoInstante:
		if _, ok := parsearInstante(valor, time.UTC); !ok {
			return "Fecha YYYY-MM-DD o fecha y hora (YYYY-MM-DDTHH:MM)"
		}
	default:
		if n := len([]rune(strings.TrimSpace(valor))); n < 1 || n > 200 {
			return "Texto de 1 a 200 caracteres"
		}
	}
	return ""
}

// parsearInstante lee una fecha (fin de ese día) o una fecha y hora (sin zona = hora de zona).
func parsearInstante(valor string, zona *time.Location) (time.Time, bool) {
	if d, err := time.ParseInLocation(time.DateOnly, valor, zona); err == nil && reFecha.MatchString(valor) {
		return d.AddDate(0, 0, 1), true
	}
	if t, err := time.Parse(time.RFC3339, valor); err == nil {
		return t, true
	}
	for _, f := range []string{"2006-01-02T15:04", "2006-01-02T15:04:05", "2006-01-02 15:04"} {
		if t, err := time.ParseInLocation(f, valor, zona); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// venceEn: hasta cuándo tiene sentido el aviso según su variable de caducidad (nil sin caducidad).
func (p Plantilla) venceEn(v map[string]string, zona *time.Location) *time.Time {
	if p.Caducidad == "" {
		return nil
	}
	t, ok := parsearInstante(v[p.Caducidad], zona)
	if !ok {
		return nil
	}
	t = t.UTC()
	return &t
}

// Parametros en el orden declarado (el de la plantilla aprobada en Meta).
func (p Plantilla) Parametros(v map[string]string) []string {
	salida := make([]string, 0, len(p.Variables))
	for _, d := range p.Variables {
		salida = append(salida, v[d.Nombre])
	}
	return salida
}

// Render arma el texto con las variables (canal simulado).
func (p Plantilla) Render(v map[string]string) string {
	t := p.Texto
	for _, d := range p.Variables {
		t = strings.ReplaceAll(t, "{{"+d.Nombre+"}}", v[d.Nombre])
	}
	return t
}

// Silencio ------------------------------------------------------------------------------------

// enSilencio indica si t (UTC) cae en la ventana de silencio y, si cae, cuándo termina.
func (v Ventana) enSilencio(t time.Time, zona *time.Location) (bool, time.Time) {
	local := t.In(zona)
	m := local.Hour()*60 + local.Minute()
	dentro := false
	if v.Inicio > v.Fin {
		dentro = m >= v.Inicio || m < v.Fin
	} else {
		dentro = m >= v.Inicio && m < v.Fin
	}
	if !dentro {
		return false, t
	}
	fin := time.Date(local.Year(), local.Month(), local.Day(), v.Fin/60, v.Fin%60, 0, 0, zona)
	if !fin.After(local) {
		fin = fin.AddDate(0, 0, 1)
	}
	return true, fin.UTC()
}

// inicioDelDia es la medianoche local (en UTC) del día de t.
func inicioDelDia(t time.Time, zona *time.Location) time.Time {
	l := t.In(zona)
	return time.Date(l.Year(), l.Month(), l.Day(), 0, 0, 0, 0, zona).UTC()
}

// String de la ventana (HH:MM-HH:MM).
func (v Ventana) String() string {
	return fmt.Sprintf("%02d:%02d-%02d:%02d", v.Inicio/60, v.Inicio%60, v.Fin/60, v.Fin%60)
}

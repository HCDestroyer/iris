package mensajeria

import (
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"golang.org/x/text/unicode/norm"

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
	// Obligatoria: hay que mandarla. Hoy todas lo son (Meta exige todos los parámetros de una
	// plantilla aprobada); una opcional que falte no se valida.
	Obligatoria bool `json:"obligatoria" doc:"Hay que mandarla (hoy todas las del catálogo)"`
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
	// Urgente: admite prioridad urgente (salta el límite diario). Solo la pide Iris.
	Urgente bool
	// Pide: quién puede pedirla (§6.3, columna «Lo pide»): PideArgos (la integración de Argos e
	// Iris) o PideIris (solo Iris u Horus; una integración recibe 422 plantilla_no_permitida). Las de
	// Iris llevan texto libre (motivo, zona): con un token de integración filtrado servirían para
	// mandar phishing por el número oficial.
	Pide string
	// DeIris: variables que llena Iris al crear el aviso (las cuentas para pagar de la bienvenida,
	// §12.5); quien pide el aviso nunca las manda (422 variable_desconocida).
	DeIris []Variable
	// Interna: variante que solo usa Iris (la bienvenida sin cuentas); no se pide por código.
	Interna bool
}

// Quién pide una plantilla.
const (
	PideArgos = "argos"
	PideIris  = "iris"
)

func vars(pares ...string) []Variable {
	v := make([]Variable, 0, len(pares)/2)
	for i := 0; i+1 < len(pares); i += 2 {
		v = append(v, Variable{Nombre: pares[i], Formato: pares[i+1], Obligatoria: true})
	}
	return v
}

var montoMoneda = []string{"monto", formatoMonto, "moneda", formatoMoneda}

// Catalogo de plantillas (§6.3), en el orden del contrato.
var Catalogo = []Plantilla{
	{Codigo: "factura_por_vencer", Pide: PideArgos, Variables: vars(append(montoMoneda, "vence_en", formatoFecha)...), Caducidad: "vence_en",
		Texto: "Hola, te recordamos que tu factura de {{moneda}} {{monto}} vence el {{vence_en}}."},
	{Codigo: "factura_vencida", Pide: PideArgos, Variables: vars(append(montoMoneda, "vence_en", formatoFecha)...),
		Texto: "Hola, tu factura de {{moneda}} {{monto}} venció el {{vence_en}}. Evita la suspensión del servicio pagando cuanto antes."},
	{Codigo: "pago_recibido", Pide: PideArgos, Variables: vars(append(montoMoneda, "fecha", formatoFecha)...),
		Texto: "Recibimos tu pago de {{moneda}} {{monto}} el {{fecha}}. ¡Gracias!"},
	{Codigo: "corte_por_falta_de_pago", Pide: PideArgos, Variables: vars(montoMoneda...),
		Texto: "Tu servicio fue suspendido por un saldo pendiente de {{moneda}} {{monto}}. Al pagar se reactiva."},
	{Codigo: "servicio_reactivado", Pide: PideArgos, Variables: []Variable{},
		Texto: "Tu servicio está activo de nuevo. ¡Gracias por tu pago!"},
	{Codigo: "bienvenida", Pide: PideArgos, Variables: vars("plan", formatoTexto),
		Texto: "¡Bienvenido! Tu servicio {{plan}} ya está activo."},
	// Bienvenida al activar (0.9.0 §12.5, B16): las cuentas las inserta Iris de su instantánea (§7.8);
	// sin cuentas válidas, la variante sin cuentas (detalle sin_cuentas).
	{Codigo: PlantillaBienvenidaServicio, Pide: PideArgos, Variables: vars("nombre", formatoTexto, "plan", formatoTexto,
		"precio_mensual", formatoMonto, "moneda", formatoMoneda, "codigo_cliente", formatoTexto),
		DeIris: vars("cuentas", formatoTexto),
		Texto:  "¡Hola, {{nombre}}! Tu servicio {{plan}} ya está activo. Tu código de cliente es {{codigo_cliente}} y tu mensualidad es de {{moneda}} {{precio_mensual}}. Puedes pagar en: {{cuentas}}"},
	{Codigo: PlantillaBienvenidaSinCuentas, Pide: PideArgos, Interna: true, Variables: vars("nombre", formatoTexto, "plan", formatoTexto,
		"precio_mensual", formatoMonto, "moneda", formatoMoneda, "codigo_cliente", formatoTexto),
		Texto: "¡Hola, {{nombre}}! Tu servicio {{plan}} ya está activo. Tu código de cliente es {{codigo_cliente}} y tu mensualidad es de {{moneda}} {{precio_mensual}}."},
	{Codigo: "corte_programado", Pide: PideIris, Variables: vars("inicio", formatoInstante, "fin", formatoInstante, "zona", formatoTexto), Urgente: true,
		Texto: "Mantenimiento programado en {{zona}} de {{inicio}} a {{fin}}. El servicio puede interrumpirse."},
	{Codigo: "boleta_recibida", Pide: PideIris, Variables: vars(append(montoMoneda, "motivo", formatoTexto)...),
		Texto: "Recibimos tu boleta de {{moneda}} {{monto}}. La revisamos y te avisamos. {{motivo}}"},
	{Codigo: "boleta_confirmada", Pide: PideIris, Variables: vars(append(montoMoneda, "motivo", formatoTexto)...),
		Texto: "Confirmamos tu pago de {{moneda}} {{monto}}. {{motivo}}"},
	{Codigo: "boleta_rechazada", Pide: PideIris, Variables: vars(append(montoMoneda, "motivo", formatoTexto)...),
		Texto: "No pudimos confirmar tu boleta de {{moneda}} {{monto}}: {{motivo}}"},
	{Codigo: "boleta_falta_dato", Pide: PideIris, Variables: vars(append(montoMoneda, "motivo", formatoTexto)...),
		Texto: "A tu boleta de {{moneda}} {{monto}} le falta un dato: {{motivo}}. Envíala de nuevo, por favor."},
	{Codigo: "reconexion_temporal", Pide: PideIris, Variables: vars("hasta", formatoInstante), Caducidad: "hasta",
		// `hasta` es el instante de Argos; al cliente le llega en hora de Guatemala y en llano
		// (HastaReconexionTexto, §6.3 y §7.3): nunca una hora de corte.
		Texto: "Reconectamos tu servicio de forma temporal: la reconexión vale hasta {{hasta}}. Tu comprobante queda pendiente de revisión."},
	// Falla general de Argos (contrato 0.13.0 §3.11): sin servicio ni dirección.
	{Codigo: "falla_general", Pide: PideIris, Variables: []Variable{},
		Texto: "Detectamos una falla en la red de tu zona que puede afectar tu servicio. Ya estamos trabajando para resolverla."},
	{Codigo: "falla_general_resuelta", Pide: PideIris, Variables: []Variable{},
		Texto: "La falla de red de tu zona se resolvió. Si tu servicio todavía no funciona, responde a este mensaje."},
	{Codigo: "promesa_aceptada", Pide: PideIris, Variables: vars("vence_en", formatoFecha, "monto", formatoMonto), Caducidad: "vence_en",
		Texto: "Registramos tu promesa de pago de {{monto}} para el {{vence_en}}."},
}

// Plantillas de la bienvenida (0.9.0 §12.5).
const (
	PlantillaBienvenidaServicio   = "bienvenida_servicio"
	PlantillaBienvenidaSinCuentas = "bienvenida_servicio_sin_cuentas"
)

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
		if !p.Interna {
			c = append(c, p.Codigo)
		}
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
func (p Plantilla) validarVariables(v map[string]string, ahora time.Time) error {
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
			if d.Obligatoria {
				errs = append(errs, nucleo.ErrorCampo{Field: campo, Message: "Falta la variable", Rule: "requerida"})
			}
			continue
		}
		if p.Codigo == "reconexion_temporal" && d.Nombre == "hasta" {
			// El instante de Argos, con su zona (RFC 3339): nunca una hora local sin zona (§7.3).
			if h, err := time.Parse(time.RFC3339, valor); err != nil {
				errs = append(errs, nucleo.ErrorCampo{Field: campo, Message: "Instante RFC 3339 con zona (2026-11-19T06:00:00Z)", Rule: "instante_con_zona"})
			} else if !HastaCreible(h, ahora) {
				// Tampoco desde una plantilla manual: un hasta pasado o a más de 49 h no se le dice al cliente.
				errs = append(errs, nucleo.ErrorCampo{Field: campo, Message: "Debe ser posterior a ahora y a lo más 49 horas después", Rule: "hasta_no_creible"})
			}
			continue
		}
		if msg := validarFormato(d.Formato, valor); msg != "" {
			errs = append(errs, nucleo.ErrorCampo{Field: campo, Message: msg, Rule: d.Formato})
			continue
		}
		if p.Codigo == PlantillaBienvenidaServicio {
			if msg := validarBienvenida(d.Nombre, valor); msg != "" {
				errs = append(errs, nucleo.ErrorCampo{Field: campo, Message: msg, Rule: "bienvenida"})
			}
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

// Parametros en el orden declarado (el de la plantilla aprobada en Meta), con las de Iris al final.
func (p Plantilla) Parametros(v map[string]string) []string {
	salida := make([]string, 0, len(p.Variables)+len(p.DeIris))
	for _, d := range append(append([]Variable{}, p.Variables...), p.DeIris...) {
		salida = append(salida, p.valor(d, v))
	}
	return salida
}

// zonaPlantillas: la hora de Guatemala para lo que lee el cliente (CLAUDE.md: zona solo al mostrar).
var zonaPlantillas = func() *time.Location {
	if z, err := time.LoadLocation("America/Guatemala"); err == nil {
		return z
	}
	return time.FixedZone("America/Guatemala", -6*60*60)
}()

// valor: lo que lee el cliente de una variable. El `hasta` de reconexion_temporal (un instante de
// Argos) va en hora de Guatemala y en llano (contrato con Argos §6.3, §7.3).
func (p Plantilla) valor(d Variable, v map[string]string) string {
	if p.Codigo == "reconexion_temporal" && d.Nombre == "hasta" {
		// Un valor sin zona (que la validación ya rechaza) se lee en hora de Guatemala, nunca como UTC.
		if t, ok := parsearInstante(v[d.Nombre], zonaPlantillas); ok {
			return HastaReconexionTexto(t, zonaPlantillas)
		}
	}
	return v[d.Nombre]
}

// Render arma el texto con las variables (canal simulado).
func (p Plantilla) Render(v map[string]string) string {
	t := p.Texto
	for _, d := range append(append([]Variable{}, p.Variables...), p.DeIris...) {
		t = strings.ReplaceAll(t, "{{"+d.Nombre+"}}", p.valor(d, v))
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

var (
	reCodigoCliente = regexp.MustCompile(`^[A-Za-z0-9-]{1,30}$`)
	reDigitos4      = regexp.MustCompile(`[0-9](?:[\s.\-/]?[0-9]){3,}`)
	// reEnlaceBienvenida: cualquier forma de enlace sobre el texto ya normalizado (NFKC): un esquema
	// (://), www., un punto seguido de dos o más letras (cualquier dominio, no una lista: evil.shop,
	// bit.do, pago.online), una barra pegada a una letra (bit.do/x, ruta/abc) o una arroba. Una barra
	// entre números («Internet 10/5») sí pasa.
	reEnlaceBienvenida = regexp.MustCompile(`(?i)(://|www\.|\.\pL{2,}|\pL/|/\pL|@)`)
	reEspacios4        = regexp.MustCompile(`\s{4,}`)
)

// separadorOFormato: caracteres Unicode de control, de formato o separadores de línea o párrafo (Cc,
// Cf, Zl, Zp: también U+0085, U+2028, U+2029 y los invisibles de dirección o de ancho cero).
func separadorOFormato(r rune) bool {
	return unicode.In(r, unicode.Cc, unicode.Cf, unicode.Zl, unicode.Zp)
}

// validarBienvenida aplica §12.5 a las variables de Argos de bienvenida_servicio: nombre hasta 60 y
// plan hasta 80 caracteres, sin saltos de línea ni caracteres de control o formato, sin 4 o más
// espacios seguidos, sin enlaces, dominios ni correos, ni secuencias de 4 o más dígitos (la lista
// blanca de §6.3: así no se cuela un número de cuenta ni un enlace); codigo_cliente alfanumérico con
// guiones (1 a 30). Se revisa sobre el texto normalizado con NFKC (así «pago․com» con U+2024 es
// «pago.com»). Ninguna variable de la bienvenida es un enlace: el de la empresa, si lo hay, va fijo en
// la plantilla aprobada en Meta, nunca en un dato de Argos.
func validarBienvenida(nombre, valor string) string {
	switch nombre {
	case "nombre", "plan":
		max := 60
		if nombre == "plan" {
			max = 80
		}
		v := norm.NFKC.String(valor)
		switch {
		case len([]rune(v)) > max:
			return "Hasta " + strconv.Itoa(max) + " caracteres"
		case strings.ContainsFunc(v, separadorOFormato):
			return "Sin saltos de línea ni caracteres de control o de formato"
		case reEspacios4.MatchString(v):
			return "Sin 4 o más espacios seguidos"
		case reEnlaceBienvenida.MatchString(v):
			return "Sin enlaces, dominios ni correos"
		case reDigitos4.MatchString(v):
			return "Sin secuencias de 4 o más dígitos"
		}
	case "codigo_cliente":
		if !reCodigoCliente.MatchString(valor) {
			return "Letras, números y guiones (1 a 30)"
		}
	}
	return ""
}

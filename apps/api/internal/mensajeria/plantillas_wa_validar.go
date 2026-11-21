package mensajeria

import (
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Reglas de Meta que Iris revisa antes de enviar una plantilla (contratos/plantillas-whatsapp.md §4).
// Cada regla sale siempre (ok o no) para que el panel muestre la lista «Antes de enviar a Meta».

// ReglaPWA es el resultado de una regla.
type ReglaPWA struct {
	Regla   string  `json:"regla" enum:"nombre,nombre_libre,idioma,categoria,marketing,cuerpo,encabezado,pie,variables_formato,variables_bordes,variables_seguidas,ejemplos,datos,botones,largos,autenticacion"`
	Ok      bool    `json:"ok"`
	Mensaje string  `json:"mensaje" doc:"En llano: qué pide la regla o qué corregir"`
	Campo   *string `json:"campo" nullable:"true" doc:"Dónde corregir (definicion.cuerpo, nombre…)"`
}

// Límites de Meta.
const (
	MaxNombrePW        = 512
	MaxCuerpoPW        = 1024
	MaxEncabezadoPW    = 60
	MaxPiePW           = 60
	MaxBotonesPW       = 10
	MaxTextoBotonPW    = 25
	MaxBotonesURLPW    = 2
	MaxURLPW           = 2000
	MaxBotonesTelPW    = 1
	MaxTelefonoPW      = 20
	MaxCopiarCodigoPW  = 1
	MaxEjemploCodigoPW = 20
	MaxVariablesAuthPW = 15
)

var (
	reNombrePW      = regexp.MustCompile(`^[a-z0-9_]+$`)
	reIdiomaPW      = regexp.MustCompile(`^[a-z]{2,3}([_-][A-Za-z]{2,4})?$`)
	reNombreVarPW   = regexp.MustCompile(`^[a-z][a-z0-9_]{0,59}$`)
	rePosicionPW    = regexp.MustCompile(`^[1-9][0-9]{0,1}$`)
	reTelefonoPW    = regexp.MustCompile(`^\+?[0-9]{6,19}$`)
	reSeguidasPW    = regexp.MustCompile(`\}\}\s*\{\{`)
	reEspaciosPW    = regexp.MustCompile(`\s+`)
	reVarFinalURLPW = regexp.MustCompile(`\{\{\s*[^{}]+\s*\}\}$`)
)

// EntradaReglasPW es lo que se revisa.
type EntradaReglasPW struct {
	Nombre     string        `json:"nombre"`
	Idioma     string        `json:"idioma"`
	Categoria  string        `json:"categoria"`
	Definicion DefinicionPWA `json:"definicion"`
}

type revision struct {
	reglas map[string]*ReglaPWA
	orden  []string
}

func nuevaRevision() *revision {
	r := &revision{reglas: map[string]*ReglaPWA{}}
	for _, x := range [][2]string{
		{"nombre", "Nombre en Meta con minúsculas, números y _ (hasta 512)"},
		{"idioma", "Idioma de Meta (es, es_MX…)"},
		{"categoria", "Categoría: servicio, marketing o verificación"},
		{"cuerpo", "Mensaje obligatorio, hasta 1024 caracteres"},
		{"encabezado", "Encabezado: texto de hasta 60 con un dato, o un archivo de ejemplo"},
		{"pie", "Pie de hasta 60 caracteres y sin datos"},
		{"variables_formato", "Datos con nombre o por posición, sin mezclar"},
		{"variables_bordes", "El mensaje no empieza ni termina con un dato"},
		{"variables_seguidas", "Datos separados por texto (no hay dos seguidos)"},
		{"ejemplos", "Cada dato tiene su ejemplo"},
		{"datos", "Cada dato es uno que Iris sabe llenar"},
		{"botones", "Botones: respuestas rápidas juntas; máximo 2 de página, 1 de llamar y 1 de copiar código"},
		{"largos", "Largos dentro del límite: encabezado 60, mensaje 1024, pie 60, botón 25"},
		{"autenticacion", "Verificación: sin enlaces, archivos ni emojis"},
	} {
		r.reglas[x[0]] = &ReglaPWA{Regla: x[0], Ok: true, Mensaje: x[1]}
		r.orden = append(r.orden, x[0])
	}
	return r
}

// falla marca la regla (solo el primer problema de cada una).
func (r *revision) falla(regla, campo, mensaje string) {
	x := r.reglas[regla]
	if x == nil {
		x = &ReglaPWA{Regla: regla}
		r.reglas[regla] = x
		r.orden = append(r.orden, regla)
	}
	if !x.Ok && x.Campo != nil {
		return
	}
	x.Ok, x.Mensaje = false, mensaje
	if campo != "" {
		x.Campo = &campo
	} else {
		x.Campo = nil
	}
}

func (r *revision) agregar(regla, mensaje string) {
	if _, ok := r.reglas[regla]; !ok {
		r.reglas[regla] = &ReglaPWA{Regla: regla, Ok: true, Mensaje: mensaje}
		r.orden = append(r.orden, regla)
	}
}

func (r *revision) lista() []ReglaPWA {
	out := make([]ReglaPWA, 0, len(r.orden))
	for _, k := range r.orden {
		out = append(out, *r.reglas[k])
	}
	return out
}

func largo(s string) int { return utf8.RuneCountInString(s) }

func tieneEmoji(s string) bool {
	for _, c := range s {
		if unicode.Is(unicode.So, c) || (c >= 0x1F000 && c <= 0x1FAFF) || (c >= 0x2600 && c <= 0x27BF) {
			return true
		}
	}
	return false
}

// formatoDe: NAMED si todos los datos tienen nombre, POSITIONAL si todos son 1, 2… ("" = mezclados o sin datos).
func formatoDe(nombres []string) string {
	conNombre, conPosicion := 0, 0
	for _, n := range nombres {
		switch {
		case rePosicionPW.MatchString(n):
			conPosicion++
		default:
			conNombre++
		}
	}
	switch {
	case conNombre > 0 && conPosicion > 0:
		return ""
	case conPosicion > 0:
		return "POSITIONAL"
	}
	return "NAMED"
}

// etiquetaDato: cómo se llama el dato para una persona.
func etiquetaDato(nombre string, vars []VariablePWA) string {
	for _, v := range vars {
		if v.Nombre == nombre && v.Dato != nil {
			if d, ok := DatoPorClave(*v.Dato); ok {
				return d.Etiqueta
			}
		}
	}
	if d, ok := DatoPorClave(nombre); ok {
		return d.Etiqueta
	}
	return nombre
}

// ReglasMeta revisa una plantilla contra las reglas de Meta (sin la base: el nombre libre y el
// marketing los agrega el caso de uso).
func ReglasMeta(in EntradaReglasPW) []ReglaPWA {
	return revisarPlantilla(in).lista()
}

func revisarPlantilla(in EntradaReglasPW) *revision {
	r := nuevaRevision()
	d := in.Definicion
	if !reNombrePW.MatchString(in.Nombre) || largo(in.Nombre) > MaxNombrePW {
		r.falla("nombre", "nombre", "El nombre en Meta solo lleva minúsculas sin tildes, números y _ (hasta 512)")
	}
	if !reIdiomaPW.MatchString(in.Idioma) {
		r.falla("idioma", "idioma", "Elige un idioma de Meta (es, es_MX, en_US…)")
	}
	auth := in.Categoria == CategoriaAutenticacion
	if !slices.Contains([]string{CategoriaUtility, CategoriaMarketing, CategoriaAutenticacion}, in.Categoria) {
		r.falla("categoria", "categoria", "Elige servicio, marketing o verificación")
	}

	// Encabezado.
	var varsEncabezado []string
	if e := d.Encabezado; e != nil {
		switch e.Tipo {
		case "texto":
			t := strings.TrimSpace(derefTexto(e.Texto))
			varsEncabezado = variablesDeTexto(t)
			switch {
			case t == "":
				r.falla("encabezado", "definicion.encabezado.texto", "Escribe el texto del encabezado o quítalo")
			case largo(t) > MaxEncabezadoPW:
				r.falla("largos", "definicion.encabezado.texto", fmt.Sprintf("El encabezado tiene %d letras: el límite es %d", largo(t), MaxEncabezadoPW))
			case len(varsEncabezado) > 1:
				r.falla("encabezado", "definicion.encabezado.texto", "El encabezado admite un solo dato")
			}
		case "imagen", "video", "documento":
			if e.AdjuntoID == nil {
				r.falla("encabezado", "definicion.encabezado.adjuntoId", "Con imagen, video o documento, Meta pide un archivo de ejemplo")
			}
		case "ubicacion":
		default:
			r.falla("encabezado", "definicion.encabezado.tipo", "Encabezado: ninguno, texto, imagen, video, documento o ubicación")
		}
		if auth {
			r.falla("autenticacion", "definicion.encabezado", "Una plantilla de verificación no lleva encabezado")
		}
	}

	// Cuerpo.
	cuerpo := strings.TrimSpace(d.Cuerpo)
	varsCuerpo := variablesDeTexto(cuerpo)
	switch {
	case auth:
		// El texto lo pone Meta.
	case cuerpo == "":
		r.falla("cuerpo", "definicion.cuerpo", "Escribe el mensaje")
	case largo(cuerpo) > MaxCuerpoPW:
		r.falla("largos", "definicion.cuerpo", fmt.Sprintf("El mensaje tiene %d letras: el límite es %d", largo(cuerpo), MaxCuerpoPW))
	}
	if !auth && cuerpo != "" {
		if loc := reVariablePW.FindStringIndex(cuerpo); loc != nil && loc[0] == 0 {
			r.falla("variables_bordes", "definicion.cuerpo", fmt.Sprintf("Meta no acepta un mensaje que empieza con un dato. Agrega algo antes de «%s».",
				etiquetaDato(varsCuerpo[0], d.Variables)))
		}
		if strings.HasSuffix(cuerpo, "}}") && reVariablePW.MatchString(cuerpo) {
			r.falla("variables_bordes", "definicion.cuerpo", fmt.Sprintf("Meta no acepta un mensaje que termina con un dato. Agrega algo después de «%s», como «. ¡Gracias!».",
				etiquetaDato(varsCuerpo[len(varsCuerpo)-1], d.Variables)))
		}
		if reSeguidasPW.MatchString(cuerpo) {
			r.falla("variables_seguidas", "definicion.cuerpo", "Hay dos datos pegados sin texto entre ellos: Meta no sabe dónde termina uno. Deja una palabra o un signo entre los dos.")
		}
	}
	if d.Encabezado != nil && d.Encabezado.Tipo == "texto" && reSeguidasPW.MatchString(derefTexto(d.Encabezado.Texto)) {
		r.falla("variables_seguidas", "definicion.encabezado.texto", "Hay dos datos pegados en el encabezado")
	}

	// Pie.
	if d.Pie != nil {
		p := strings.TrimSpace(*d.Pie)
		switch {
		case largo(p) > MaxPiePW:
			r.falla("largos", "definicion.pie", fmt.Sprintf("El pie tiene %d letras: el límite es %d", largo(p), MaxPiePW))
		case reVariablePW.MatchString(p):
			r.falla("pie", "definicion.pie", "El pie no lleva datos")
		}
	}

	// Botones.
	varsURL := []string{}
	cuenta := map[string]int{}
	if len(d.Botones) > MaxBotonesPW {
		r.falla("botones", "definicion.botones", fmt.Sprintf("Meta admite hasta %d botones", MaxBotonesPW))
	}
	bloquesRespuesta, previa := 0, ""
	for i, b := range d.Botones {
		campo := fmt.Sprintf("definicion.botones[%d]", i)
		cuenta[b.Tipo]++
		if b.Tipo == "respuesta" && previa != "respuesta" {
			bloquesRespuesta++
		}
		previa = b.Tipo
		if b.Tipo != "copiar_codigo" {
			t := strings.TrimSpace(b.Texto)
			switch {
			case t == "":
				r.falla("botones", campo+".texto", "Escribe lo que dice el botón")
			case largo(t) > MaxTextoBotonPW:
				r.falla("largos", campo+".texto", fmt.Sprintf("El botón «%s» tiene %d letras: el límite es %d", t, largo(t), MaxTextoBotonPW))
			case reVariablePW.MatchString(t):
				r.falla("botones", campo+".texto", "El texto de un botón no lleva datos")
			}
		}
		switch b.Tipo {
		case "respuesta":
		case "url":
			u := strings.TrimSpace(derefTexto(b.URL))
			vs := variablesDeTexto(u)
			sinVar := reVariablePW.ReplaceAllString(u, "x")
			pu, err := url.Parse(sinVar)
			switch {
			case u == "":
				r.falla("botones", campo+".url", "Escribe la dirección de la página")
			case largo(u) > MaxURLPW:
				r.falla("largos", campo+".url", fmt.Sprintf("La dirección tiene %d caracteres: el límite es %d", largo(u), MaxURLPW))
			case err != nil || (pu.Scheme != "https" && pu.Scheme != "http") || pu.Host == "":
				r.falla("botones", campo+".url", "La dirección debe empezar con https://")
			case len(vs) > 1 || (len(vs) == 1 && !reVarFinalURLPW.MatchString(u)):
				r.falla("botones", campo+".url", "La dirección admite un solo dato y va al final")
			case len(vs) == 1 && strings.TrimSpace(derefTexto(b.Ejemplo)) == "":
				r.falla("ejemplos", campo+".ejemplo", "Pon un ejemplo del dato del final de la dirección")
			}
			varsURL = append(varsURL, vs...)
		case "telefono":
			t := strings.ReplaceAll(strings.TrimSpace(derefTexto(b.Telefono)), " ", "")
			switch {
			case largo(t) > MaxTelefonoPW:
				r.falla("largos", campo+".telefono", fmt.Sprintf("El teléfono tiene más de %d caracteres", MaxTelefonoPW))
			case !reTelefonoPW.MatchString(t):
				r.falla("botones", campo+".telefono", "El teléfono va con código de país (+502…)")
			}
		case "copiar_codigo":
			e := strings.TrimSpace(derefTexto(b.Ejemplo))
			switch {
			case e == "":
				r.falla("ejemplos", campo+".ejemplo", "Pon un código de ejemplo")
			case largo(e) > MaxEjemploCodigoPW:
				r.falla("largos", campo+".ejemplo", fmt.Sprintf("El código de ejemplo tiene más de %d caracteres", MaxEjemploCodigoPW))
			}
		default:
			r.falla("botones", campo+".tipo", "Botón: respuesta rápida, página, llamar o copiar código")
		}
		if auth && b.Tipo == "url" {
			r.falla("autenticacion", campo, "Una plantilla de verificación no lleva enlaces")
		}
		if tieneEmoji(b.Texto) && auth {
			r.falla("autenticacion", campo+".texto", "Una plantilla de verificación no lleva emojis")
		}
	}
	switch {
	case cuenta["url"] > MaxBotonesURLPW:
		r.falla("botones", "definicion.botones", "Máximo 2 botones de página")
	case cuenta["telefono"] > MaxBotonesTelPW:
		r.falla("botones", "definicion.botones", "Máximo 1 botón de llamar")
	case cuenta["copiar_codigo"] > MaxCopiarCodigoPW:
		r.falla("botones", "definicion.botones", "Máximo 1 botón de copiar código")
	case bloquesRespuesta > 1:
		r.falla("botones", "definicion.botones", "Las respuestas rápidas van juntas, antes o después de los demás botones")
	}

	// Variables.
	todas := slices.Concat(varsEncabezado, varsCuerpo, varsURL)
	formato := formatoDe(todas)
	if formato == "" {
		r.falla("variables_formato", "definicion.cuerpo", "No mezcles datos con nombre y datos por número")
	}
	for _, grupo := range []struct {
		vs    []string
		campo string
	}{{varsEncabezado, "definicion.encabezado.texto"}, {varsCuerpo, "definicion.cuerpo"}} {
		vistos := map[string]bool{}
		for i, n := range grupo.vs {
			switch {
			case formato == "POSITIONAL" && n != fmt.Sprint(i+1):
				r.falla("variables_formato", grupo.campo, "Los datos por número van en orden: {{1}}, {{2}}, {{3}}…")
			case formato == "NAMED" && !reNombreVarPW.MatchString(n):
				r.falla("variables_formato", grupo.campo, fmt.Sprintf("«%s» no es un nombre de dato válido (minúsculas, números y _)", n))
			case vistos[n]:
				r.falla("variables_formato", grupo.campo, fmt.Sprintf("«%s» aparece dos veces: usa cada dato una vez", etiquetaDato(n, d.Variables)))
			}
			vistos[n] = true
		}
	}
	declaradas := map[string]VariablePWA{}
	for i, v := range d.Variables {
		campo := fmt.Sprintf("definicion.variables[%d]", i)
		if _, dup := declaradas[v.Nombre]; dup {
			r.falla("variables_formato", campo+".nombre", "Dato repetido en la lista de ejemplos")
		}
		declaradas[v.Nombre] = v
		if !slices.Contains(todas, v.Nombre) {
			r.falla("ejemplos", campo, fmt.Sprintf("«%s» tiene ejemplo pero no está en el mensaje: quítalo", etiquetaDato(v.Nombre, d.Variables)))
		}
		if v.Dato != nil {
			if _, ok := DatoPorClave(*v.Dato); !ok {
				r.falla("datos", campo+".dato", fmt.Sprintf("Iris no conoce el dato «%s»", *v.Dato))
			}
		}
		if tieneEmoji(v.Ejemplo) && auth {
			r.falla("autenticacion", campo+".ejemplo", "Una plantilla de verificación no lleva emojis")
		}
	}
	for _, n := range todas {
		v, ok := declaradas[n]
		if !ok || strings.TrimSpace(v.Ejemplo) == "" {
			r.falla("ejemplos", "definicion.variables", fmt.Sprintf("Falta el ejemplo de «%s» (Meta lo pide; nunca un dato real)", etiquetaDato(n, d.Variables)))
		}
	}

	// Verificación (autenticación).
	if auth {
		if len(todas) > MaxVariablesAuthPW {
			r.falla("autenticacion", "definicion.variables", fmt.Sprintf("Una plantilla de verificación admite hasta %d datos", MaxVariablesAuthPW))
		}
		if d.Pie != nil && tieneEmoji(*d.Pie) {
			r.falla("autenticacion", "definicion.pie", "Una plantilla de verificación no lleva emojis")
		}
		if cuenta["copiar_codigo"] != 1 {
			r.falla("autenticacion", "definicion.botones", "Una plantilla de verificación lleva un botón «Copiar código»")
		}
	} else if d.Autenticacion != nil {
		r.falla("autenticacion", "definicion.autenticacion", "Los ajustes de verificación solo van con la categoría verificación")
	}
	return r
}

// reglasFallidas convierte las reglas que fallaron en errores de campo (422 reglas_meta).
func reglasFallidas(rs []ReglaPWA) []ReglaPWA {
	out := []ReglaPWA{}
	for _, r := range rs {
		if !r.Ok {
			out = append(out, r)
		}
	}
	return out
}

// normalizarDefinicion recorta espacios y deja las listas en [] (lo que se guarda).
func normalizarDefinicion(d DefinicionPWA) DefinicionPWA {
	d.Cuerpo = strings.TrimSpace(d.Cuerpo)
	if d.Pie != nil {
		p := strings.TrimSpace(*d.Pie)
		if p == "" {
			d.Pie = nil
		} else {
			d.Pie = &p
		}
	}
	if d.Encabezado != nil && d.Encabezado.Texto != nil {
		t := strings.TrimSpace(*d.Encabezado.Texto)
		d.Encabezado.Texto = &t
	}
	for i := range d.Botones {
		d.Botones[i].Texto = strings.TrimSpace(d.Botones[i].Texto)
		if d.Botones[i].Telefono != nil {
			t := reEspaciosPW.ReplaceAllString(strings.TrimSpace(*d.Botones[i].Telefono), "")
			d.Botones[i].Telefono = &t
		}
	}
	for i := range d.Variables {
		d.Variables[i].Ejemplo = strings.TrimSpace(d.Variables[i].Ejemplo)
	}
	d.Botones, d.Variables = lista(d.Botones), lista(d.Variables)
	return d
}

// parametrosDe arma qué dato va en cada variable (las que tienen dato).
func parametrosDe(d DefinicionPWA) []ParametroPWA {
	dato := map[string]string{}
	for _, v := range d.Variables {
		if v.Dato != nil {
			dato[v.Nombre] = *v.Dato
		}
	}
	out := []ParametroPWA{}
	if d.Encabezado != nil && d.Encabezado.Tipo == "texto" {
		for _, n := range variablesDeTexto(derefTexto(d.Encabezado.Texto)) {
			out = append(out, ParametroPWA{Componente: "encabezado", Nombre: n, Dato: dato[n]})
		}
	}
	for _, n := range variablesDeTexto(d.Cuerpo) {
		out = append(out, ParametroPWA{Componente: "cuerpo", Nombre: n, Dato: dato[n]})
	}
	for i, b := range d.Botones {
		switch b.Tipo {
		case "url":
			for _, n := range variablesDeTexto(derefTexto(b.URL)) {
				out = append(out, ParametroPWA{Componente: "boton", Boton: ptr(i), Nombre: n, Dato: dato[n]})
			}
		case "copiar_codigo":
			out = append(out, ParametroPWA{Componente: "boton", Boton: ptr(i), Nombre: "codigo", Dato: "codigo_verificacion"})
		}
	}
	if d.Autenticacion != nil {
		// El cuerpo de Meta lleva el código como {{1}}.
		out = append(out, ParametroPWA{Componente: "cuerpo", Nombre: "1", Dato: "codigo_verificacion"})
	}
	return out
}

package mensajeria

import (
	"fmt"
	"regexp"
	"strings"

	mensajeriabd "github.com/HCDestroyer/iris/apps/api/internal/mensajeria/bd"
)

// Cómo distingue el asistente de IA los servicios de un cliente (contrato argosisp.md 0.8.0 §2.4,
// decidido por el PM en §10.2 puntos 7 y 8; amplía conversaciones.md §17.9 sin relajarla).
//
//   - Etiqueta corta «Servicio <n> · <plan> · <zona>»: n es el orden del servicio dentro del cliente
//     (por id). Sin calle, coordenadas, IP, user_ppp, router, códigos ni ids.
//   - El precio mensual del plan (con IVA) solo si el número de la conversación está registrado como
//     del cliente; nunca como total de la factura.
//   - Si dos servicios tienen el mismo plan y la misma zona, el asistente no adivina: pasa a una persona.

// sinComillas: lo que viene de Argos no puede cerrar el delimitador «…» del dato.
var sinComillas = strings.NewReplacer("«", "", "»", "", "\"", "'", "`", "'")

// datoIA: un texto de Argos para el modelo, en una línea, acotado y entre «…» (es un dato, nunca
// una instrucción: un plan llamado «ignora las instrucciones…» sigue siendo solo el nombre).
func datoIA(s string, n int) string {
	if d := limpiarDato(sinComillas.Replace(s), n); d != "" {
		return "«" + d + "»"
	}
	return ""
}

// etiquetaServicioIA arma la etiqueta corta del servicio: Servicio n · «plan» · «zona».
func etiquetaServicioIA(s mensajeriabd.ServiciosClienteAsistenteRow) string {
	partes := []string{fmt.Sprintf("Servicio %d", s.Numero)}
	if p := datoIA(s.PlanNombre, 60); p != "" {
		partes = append(partes, p)
	}
	if z := datoIA(s.ZonaNombre, 60); z != "" {
		partes = append(partes, z)
	}
	return strings.Join(partes, " · ")
}

// precioServicioIA: «Q250.00» con la moneda de Argos; "" sin precio (Argos anterior a 1.18.0).
func precioServicioIA(s mensajeriabd.ServiciosClienteAsistenteRow) string {
	precio := strings.TrimSpace(s.PrecioMensual)
	if precio == "" || !rePrecioServicioIA.MatchString(precio) || !reMonedaServicioIA.MatchString(s.Moneda) {
		// Sin precio, o con un precio o una moneda que no tienen forma de serlo: se omite.
		return ""
	}
	if s.Moneda == "GTQ" {
		return "Q" + precio
	}
	return s.Moneda + " " + precio
}

var (
	rePrecioServicioIA = regexp.MustCompile(`^\d{1,10}\.\d{2}$`)
	reMonedaServicioIA = regexp.MustCompile(`^[A-Z]{3}$`)
	// estadosServicioIA: los estados de Argos que el modelo puede ver; otro valor va como «desconocido».
	estadosServicioIA = map[string]bool{"pendiente_instalacion": true, "activo": true, "limitado": true, "suspendido": true, "retirado": true}
)

// lineaServicioIA: una línea para el modelo con la etiqueta, el estado y, con número registrado,
// el precio mensual del plan.
func lineaServicioIA(s mensajeriabd.ServiciosClienteAsistenteRow, registrado bool) string {
	estado := strings.TrimSpace(s.Estado)
	if !estadosServicioIA[estado] {
		estado = "desconocido"
	}
	linea := fmt.Sprintf("%s: estado %s.", etiquetaServicioIA(s), estado)
	if p := precioServicioIA(s); p != "" && registrado {
		linea += fmt.Sprintf(" Su plan cuesta %s al mes (precio mensual del plan con IVA; no es el total de la factura: nunca digas «su factura es de…»).", p)
	}
	return linea + "\n"
}

func claveServicioIA(s mensajeriabd.ServiciosClienteAsistenteRow) string {
	return strings.ToLower(strings.TrimSpace(s.PlanNombre) + "|" + strings.TrimSpace(s.ZonaNombre))
}

// describirServiciosIA: los servicios del cliente para la herramienta de estado del servicio.
func describirServiciosIA(svs []mensajeriabd.ServiciosClienteAsistenteRow, registrado bool) string {
	var b strings.Builder
	iguales := map[string][]string{}
	for _, s := range svs {
		b.WriteString(lineaServicioIA(s, registrado))
		iguales[claveServicioIA(s)] = append(iguales[claveServicioIA(s)], fmt.Sprintf("Servicio %d", s.Numero))
	}
	for _, s := range svs {
		if mismos := iguales[claveServicioIA(s)]; len(mismos) > 1 {
			b.WriteString(strings.Join(mismos, " y ") + " tienen el mismo plan y la misma zona: si el cliente no dice claramente cuál es, no adivines y pasa a una persona.\n")
			delete(iguales, claveServicioIA(s))
		}
	}
	if !registrado {
		for _, s := range svs {
			if precioServicioIA(s) != "" {
				b.WriteString("El número no está registrado como del cliente: no digas precios; si los pide, ofrece pasar a una persona.\n")
				break
			}
		}
	}
	return b.String()
}

// zonaFalla: la zona de una falla de red para el texto del asistente.
func zonaFalla(zona string) string {
	if zona == "" {
		return "su zona"
	}
	return zona
}

package mensajeria

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/text/unicode/norm"

	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/conector"
	mensajeriabd "github.com/HCDestroyer/iris/apps/api/internal/mensajeria/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
)

// Saldo del cliente en las sugerencias (contratos/conversaciones.md §16.10, entrega 2.1): con la
// fuente argos y un cliente asociado, al preparar cada sugerencia se lee en vivo el estado de cuenta
// de Argos (GET /clientes/:id/estado-cuenta), fuera de la transacción. No se guarda en ningún lado:
// ni en la sugerencia, ni en eventos, auditoría o logs. Si Argos falla, la sugerencia sale sin él.
//
// Decisión del líder (revisión de seguridad de la #155): el monto nunca llega al modelo ni aparece en
// una sugerencia, con cliente verificado o sin él; el agente lo ve en la ficha y lo escribe él si
// quiere. Con cliente verificado el modelo recibe solo «al día» o «con saldo vencido (N facturas)»;
// sin verificar no recibe nada del estado de cuenta (solo la instrucción de no hablar de saldos y
// ofrecer verificar). El servidor rechaza siempre una sugerencia con un monto de deuda.

// PlazoCuentaIA: cuánto se espera el estado de cuenta de Argos antes de seguir sin él.
const PlazoCuentaIA = 5 * time.Second

// ActorAsistenteIA es el X-Iris-Actor de las lecturas del asistente (§7.1 del contrato con Argos).
const ActorAsistenteIA = "asistente_ia"

// cuentaIA es el estado de cuenta leído para una generación (solo en memoria). No guarda ningún
// monto: de lo que responde Argos solo queda «al día» o «vencido» y cuántas facturas.
type cuentaIA struct {
	// clienteID: de quién es (la conversación pudo cambiar de cliente mientras Argos respondía).
	clienteID int64
	alDia     bool
	vencidas  *int
}

// leerCuentaIA lee el estado de cuenta del cliente de la conversación de la sugerencia, si aplica.
// nil = no aplica o Argos no respondió (la sugerencia sale sin esa fuente).
func (m *Modulo) leerCuentaIA(ctx context.Context, id uuid.UUID) *cuentaIA {
	bd := nucleo.BDDe(ctx)
	if m.cfg.Argos == nil || bd == nil {
		return nil
	}
	q := mensajeriabd.New(bd)
	s, err := q.SugerenciaPorID(ctx, id)
	if err != nil || s.Estado != SugGenerando {
		return nil
	}
	c, err := q.ConversacionClientePorID(ctx, s.ConversacionID)
	empresa := empresaDeConv(c)
	if err != nil || c.ClienteID == nil || empresa == 0 || derefEmpresa(s.EmpresaID) != empresa {
		return nil
	}
	aj, err := leerAjustesAsistente(ctx, q, empresa)
	if err != nil || aj == nil || !aj.Activo || !aj.FuenteArgos {
		return nil
	}
	cctx, cancelar := context.WithTimeout(ctx, PlazoCuentaIA)
	defer cancelar()
	e, err := m.cfg.Argos.EstadoCuentaCliente(cctx, argos.AtencionIris{Actor: ActorAsistenteIA,
		Conversacion: "iris:conversacion:" + strconv.FormatInt(c.ID, 10), EmpresaID: empresa}, *c.ClienteID)
	if err != nil {
		// Solo el motivo resumido (nunca la respuesta de Argos).
		motivo := "error"
		switch {
		case conector.EsCaido(err):
			motivo = "argos_no_disponible"
		case conector.EsNoEncontrado(err):
			motivo = "no_encontrado"
		}
		slog.InfoContext(ctx, "asistente: sugerencia sin estado de cuenta de Argos", "conversacion_id", c.ID, "motivo", motivo)
		return nil
	}
	return cuentaDe(*c.ClienteID, e)
}

// cuentaDe decide «al día» o «con saldo vencido». Sin datos para decidir: nil.
func cuentaDe(clienteID int64, e argos.EstadoCuenta) *cuentaIA {
	monto := e.MontoVencido
	vencido := positivo(monto)
	switch {
	case e.FacturasVencidas != nil:
		vencido = *e.FacturasVencidas > 0 || vencido
	case monto == nil:
		// Sin facturas vencidas ni monto vencido informados no se sabe si está al día.
		return nil
	}
	return &cuentaIA{clienteID: clienteID, alDia: !vencido, vencidas: e.FacturasVencidas}
}

func positivo(s *string) bool {
	if s == nil {
		return false
	}
	f, err := strconv.ParseFloat(strings.TrimSpace(*s), 64)
	return err == nil && f > 0
}

// etiqueta es la fuente que ve el agente (interna; nunca el monto).
func (c *cuentaIA) etiqueta(estadoServicio string) string {
	if c.alDia {
		if estadoServicio == "" {
			return "Cliente al día · Argos"
		}
		return fmt.Sprintf("Servicio %s, al día · Argos", limpiarDato(estadoServicio, 30))
	}
	if c.vencidas != nil && *c.vencidas > 0 {
		if *c.vencidas == 1 {
			return "Con saldo vencido (1 factura) · Argos"
		}
		return fmt.Sprintf("Con saldo vencido (%d facturas) · Argos", *c.vencidas)
	}
	return "Con saldo vencido · Argos"
}

// datoSinVerificar es lo único que ve el modelo del estado de cuenta de un cliente sin verificar:
// nada del estado, solo la instrucción.
const datoSinVerificar = "El cliente NO está verificado: no hables de su saldo, deudas, facturas ni pagos pendientes; " +
	"si pregunta por eso, ofrece verificar su identidad para que un agente le dé el detalle."

// dato es lo que ve el modelo: con cliente verificado, solo «al día» o «con saldo vencido (N
// facturas)»; nunca un monto. Sin verificar, nada del estado de cuenta.
func (c *cuentaIA) dato(verificado bool) string {
	if !verificado {
		return datoSinVerificar
	}
	if c == nil {
		// Verificado pero sin estado de cuenta (Argos no respondió): nada que agregar.
		return ""
	}
	if c.alDia {
		return "(argos) Estado de cuenta del cliente: al día, sin facturas vencidas."
	}
	estado := "con saldo vencido"
	if c.vencidas != nil && *c.vencidas > 0 {
		estado += fmt.Sprintf(" (%d facturas vencidas)", *c.vencidas)
	}
	return "(argos) Estado de cuenta del cliente: " + estado + ". Puedes decirle que tiene un saldo pendiente; el monto no lo tienes: " +
		"si lo pide, indica que un agente se lo confirmará. Nunca escribas una cifra de dinero de su deuda."
}

// Validador de montos ------------------------------------------------------------------------------
//
// Una sugerencia nunca trae un monto de deuda, con cliente verificado o sin él (el monto no se le da
// al modelo: si aparece, lo inventó o lo sacó de la conversación). Se rechaza:
//
//   - una cifra con moneda (antes o después: Q, GTQ, qtz, USD, $, quetzales, dólares, pesos; en
//     dígitos o en letras) si el texto habla de deuda en cualquier parte;
//   - un número de 2 dígitos o más, un decimal o un numeral en letras (diez, cien, mil…) a menos de
//     CercaniaMonto de una palabra de deuda, salvo que cuente otra cosa (facturas, días, Mbps…).
//
// Un precio de plan tomado de un manual («el plan de 100 Mb cuesta Q 299 al mes») no es deuda y pasa.

const (
	monedaCorta  = `(?:gtq|qtz|usd|q)`
	monedaLarga  = `(?:quetzal(?:es)?|d[oó]lar(?:es)?|pesos?)`
	numeroLetras = `(?:dos|tres|cuatro|cinco|seis|siete|ocho|nueve|diez|once|doce|trece|catorce|quince|diecis[eé]is|diecisiete|dieciocho|diecinueve|` +
		`veinte|veinti[a-zñáéíóú]+|treinta|cuarenta|cincuenta|sesenta|setenta|ochenta|noventa|cien|ciento|doscient[oa]s|trescient[oa]s|` +
		`cuatrocient[oa]s|quinient[oa]s|seiscient[oa]s|setecient[oa]s|ochocient[oa]s|novecient[oa]s|mil|mill[oó]n(?:es)?)`
	// numeroGrande: los numerales que cuentan cerca de una palabra de deuda sin moneda (de diez en adelante).
	numeroGrande = `(?:diez|once|doce|trece|catorce|quince|diecis[eé]is|diecisiete|dieciocho|diecinueve|` +
		`veinte|veinti[a-zñáéíóú]+|treinta|cuarenta|cincuenta|sesenta|setenta|ochenta|noventa|cien|ciento|doscient[oa]s|trescient[oa]s|` +
		`cuatrocient[oa]s|quinient[oa]s|seiscient[oa]s|setecient[oa]s|ochocient[oa]s|novecient[oa]s|mil|mill[oó]n(?:es)?)`
)

var (
	// reCifraConMoneda: la moneda antes o después del número (en dígitos o en letras).
	reCifraConMoneda = regexp.MustCompile(`\b` + monedaCorta + `\s*[.:=]?\s*\d` +
		`|\$\s*\d|\d\s*\$` +
		`|\d[\d.,]*\s*(?:` + monedaCorta + `|` + monedaLarga + `)\b` +
		`|\b` + monedaLarga + `\s*[.:=]?\s*\d` +
		`|\b(?:un|` + numeroLetras + `)\s+(?:de\s+)?(?:` + monedaCorta + `|` + monedaLarga + `)\b` +
		`|(?:\b` + monedaCorta + `|\$)\s*` + numeroLetras + `\b`)
	// reDeuda: palabras de deuda.
	reDeuda = regexp.MustCompile(`\b(?:saldos?|deb[eo]s?|deben|debía[ns]?|deudas?|adeud[a-zñáéíóú]*|pendientes?\s+de\s+pago|pagos?\s+pendientes?|` +
		`facturas?\s+vencidas?|total\s+a\s+pagar|monto\s+(?:vencido|pendiente|adeudado)|en\s+mora|moros[oa]s?)\b`)
	// reNumeroSuelto: un número de 2 dígitos o más (con separadores) o con decimales, con lo que sigue
	// (para descartar los que cuentan otra cosa).
	reNumeroSuelto = regexp.MustCompile(`\d[\d.,]*\d|` + `\b` + numeroGrande + `\b`)
	// reUnidadNoDinero: lo que sigue a un número que no es dinero.
	reUnidadNoDinero = regexp.MustCompile(`^\s*(?:facturas?|d[ií]as?|mes(?:es)?|horas?|minutos?|a[ñn]os?|mbps|mb|megas?|gb|%|de\s+(?:enero|febrero|marzo|abril|mayo|junio|julio|agosto|septiembre|setiembre|octubre|noviembre|diciembre)|/)`)
	reEspaciosMonto  = regexp.MustCompile(`[\s\p{Z}\p{Cf}]+`)
)

// CercaniaMonto: un número sin moneda a esta distancia (bytes) de una palabra de deuda cuenta como
// monto.
const CercaniaMonto = 60

// normalizarMonto: NFKC (dígitos y letras de ancho completo, ligaduras), espacios Unicode e
// invisibles como un espacio, sin marcas de formato (* _ ~ `) y en minúsculas.
func normalizarMonto(texto string) string {
	s := norm.NFKC.String(texto)
	s = strings.NewReplacer("*", "", "_", "", "~", "", "`", "").Replace(s)
	s = reEspaciosMonto.ReplaceAllString(s, " ")
	return strings.ToLower(s)
}

// ContieneMonto indica si el texto trae un monto de deuda (§16.10).
func ContieneMonto(texto string) bool {
	s := normalizarMonto(texto)
	deuda := reDeuda.FindAllStringIndex(s, -1)
	if len(deuda) == 0 {
		return false
	}
	if reCifraConMoneda.MatchString(s) {
		return true
	}
	for _, n := range reNumeroSuelto.FindAllStringIndex(s, -1) {
		if reUnidadNoDinero.MatchString(s[n[1]:]) {
			continue
		}
		for _, p := range deuda {
			if (p[0] >= n[1] && p[0]-n[1] <= CercaniaMonto) || (n[0] >= p[1] && n[0]-p[1] <= CercaniaMonto) {
				return true
			}
		}
	}
	return false
}

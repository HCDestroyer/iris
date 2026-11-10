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
// Regla del contrato con Argos (§7.1): el monto solo se da a un cliente verificado. Al modelo se le
// pasa el estado y, solo con cliente verificado, el monto; y el servidor rechaza una sugerencia con
// un monto si la conversación no tiene un cliente verificado.

// PlazoCuentaIA: cuánto se espera el estado de cuenta de Argos antes de seguir sin él.
const PlazoCuentaIA = 5 * time.Second

// ActorAsistenteIA es el X-Iris-Actor de las lecturas del asistente (§7.1 del contrato con Argos).
const ActorAsistenteIA = "asistente_ia"

// cuentaIA es el estado de cuenta leído para una generación (solo en memoria).
type cuentaIA struct {
	alDia    bool
	vencidas *int
	// monto: el vencido (o, si Argos no lo informa, el saldo) en texto decimal, y su moneda.
	monto  *string
	moneda string
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
	return cuentaDe(e)
}

// cuentaDe decide «al día» o «con saldo vencido». Sin datos para decidir: nil.
func cuentaDe(e argos.EstadoCuenta) *cuentaIA {
	monto := e.MontoVencido
	vencido := positivo(monto)
	switch {
	case e.FacturasVencidas != nil:
		vencido = *e.FacturasVencidas > 0 || vencido
	case monto == nil:
		// Sin facturas vencidas ni monto vencido informados no se sabe si está al día.
		return nil
	}
	c := &cuentaIA{alDia: !vencido, vencidas: e.FacturasVencidas, moneda: strings.TrimSpace(e.Moneda)}
	if vencido {
		c.monto = monto
		if c.monto == nil && positivo(e.Saldo) {
			c.monto = e.Saldo
		}
	}
	return c
}

func positivo(s *string) bool {
	if s == nil {
		return false
	}
	f, err := strconv.ParseFloat(strings.TrimSpace(*s), 64)
	return err == nil && f > 0
}

// etiqueta es la fuente que ve el agente (nunca el monto).
func (c *cuentaIA) etiqueta(estadoServicio string) string {
	if c.alDia {
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

// dato es lo que ve el modelo, con la instrucción según la verificación.
func (c *cuentaIA) dato(verificado bool) string {
	if c.alDia {
		return "(argos) Estado de cuenta del cliente: al día, sin facturas vencidas."
	}
	estado := "con saldo vencido"
	if c.vencidas != nil && *c.vencidas > 0 {
		estado += fmt.Sprintf(" (%d facturas vencidas)", *c.vencidas)
	}
	if !verificado || c.monto == nil {
		return "(argos) Estado de cuenta del cliente: " + estado + ". El cliente NO está verificado: no menciones montos, cifras ni cantidades de dinero; " +
			"solo puedes decir que tiene un saldo pendiente y ofrecer verificar su identidad para darle el detalle."
	}
	moneda := c.moneda
	if moneda == "" {
		moneda = "GTQ"
	}
	return fmt.Sprintf("(argos) Estado de cuenta del cliente: %s; monto vencido %s %s. El cliente está verificado: puedes decirle el monto si lo pregunta, "+
		"sin prometer descuentos, plazos ni reconexiones.", estado, limpiarDato(moneda, 5), limpiarDato(*c.monto, 20))
}

var (
	// reMonedaIA: Q 350, Q.350, GTQ, $ 20, 350 quetzales.
	reMonedaIA = regexp.MustCompile(`(?i)(\bQ\s?\.?\s?\d|\bGTQ\b|\$\s?\d|\d\s*quetzal)`)
	// reDecimal: un número con decimales (350.00, 1,250.50, 99,5).
	reDecimal = regexp.MustCompile(`\d[\d,.]*[.,]\d{1,2}\b`)
	// reContextoMonto: palabras de saldo cerca de un número con decimales.
	reContextoMonto = regexp.MustCompile(`(?i)\b(saldo|debe|deuda|adeud|pag[aoóu])`)
)

// CercaniaMonto: un número con decimales a esta distancia (bytes) de «saldo», «debe», «pagar» cuenta
// como monto.
const CercaniaMonto = 40

// ContieneMonto indica si el texto trae un monto de dinero (§16.10).
func ContieneMonto(texto string) bool {
	if reMonedaIA.MatchString(texto) {
		return true
	}
	palabras := reContextoMonto.FindAllStringIndex(texto, -1)
	for _, n := range reDecimal.FindAllStringIndex(texto, -1) {
		for _, p := range palabras {
			if (p[0] >= n[1] && p[0]-n[1] <= CercaniaMonto) || (n[0] >= p[1] && n[0]-p[1] <= CercaniaMonto) {
				return true
			}
		}
	}
	return false
}

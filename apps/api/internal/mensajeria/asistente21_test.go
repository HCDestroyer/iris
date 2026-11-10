package mensajeria_test

import (
	"strconv"
	"strings"
	"testing"

	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos"
	"github.com/HCDestroyer/iris/apps/api/internal/mensajeria"
)

// Saldo del cliente en las sugerencias (contratos/conversaciones.md §16.10, entrega 2.1).

// conClienteArgos: una conversación de un cliente de la instantánea (asociado por su número, por eso
// verificado) con un servicio activo de la empresa 1.
func (x *bandejaIA) conClienteArgos() (conv string, cliente int64) {
	x.t.Helper()
	num := numeroCliente()
	cliente = siguienteCliente.Add(1)
	if _, err := x.bd.Exec(x.ctx, `INSERT INTO integracion.argos_clientes (id, empresa_id, nombre, movil, codigo, sincronizado_en, empresas)
		VALUES ($1, 1, 'Ana López', $2, 'C-2', now(), '{1}')`, cliente, "+502 "+num[3:7]+"-"+num[7:]); err != nil {
		x.t.Fatal(err)
	}
	if _, err := x.bd.Exec(x.ctx, `INSERT INTO integracion.argos_servicios (id, empresa_id, cliente_id, codigo, plan_nombre, estado, zona_nombre, sincronizado_en)
		VALUES ($1, 1, $2, 'S-9', 'Fibra 20 Mbps', 'activo', 'Zona 1', now())`, cliente*10, cliente); err != nil {
		x.t.Fatal(err)
	}
	x.texto(num, "wamid.saldo"+strconv.FormatInt(cliente, 10), "Hola, ¿cuánto debo de mi servicio?")
	return cid(x.conv(num)), cliente
}

func etiquetasArgos(s map[string]any) []string {
	out := []string{}
	for _, f := range s["fuentes"].([]any) {
		if f.(map[string]any)["tipo"] == "argos" {
			out = append(out, f.(map[string]any)["etiqueta"].(string))
		}
	}
	return out
}

func vencidas(n int) *int { return &n }

func texto(s string) *string { return &s }

func TestAsistenteSaldoClienteVerificado(t *testing.T) {
	x := nuevaBandejaIA(t)
	x.activar(nil)
	conv, _ := x.conClienteArgos()
	x.argos.mu.Lock()
	x.argos.cuenta = &argos.EstadoCuenta{Saldo: texto("350.00"), Moneda: "GTQ", FacturasVencidas: vencidas(2), MontoVencido: texto("350.00")}
	x.argos.mu.Unlock()
	x.ia.decir(respuestaModelo("Hola Ana, tienes un saldo vencido de Q350.00 en 2 facturas.", []string{"F1"}, nil))
	s := x.pedirYGenerar(x.agente, conv)
	if s["estado"] != "lista" || strings.Join(etiquetasArgos(s), ",") != "Con saldo vencido (2 facturas) · Argos" {
		t.Fatalf("sugerencia: %v", s)
	}
	pedido, _, _ := x.ia.ultimo()
	if !strings.Contains(pedido, "350.00") || !strings.Contains(pedido, "está verificado") {
		t.Fatalf("el modelo debía ver el monto (cliente verificado): %s", pedido)
	}
	x.argos.mu.Lock()
	actor := x.argos.actores[len(x.argos.actores)-1]
	x.argos.mu.Unlock()
	if actor.Actor != mensajeria.ActorAsistenteIA || actor.EmpresaID != 1 || !strings.HasPrefix(actor.Conversacion, "iris:conversacion:") {
		t.Fatalf("cabeceras de §7.1: %+v", actor)
	}
	// El número nunca se guarda fuera del texto que verá el agente: ni fuentes, ni eventos, ni auditoría.
	if n := x.contar(`SELECT count(*) FROM mensajeria.sugerencias WHERE fuentes::text LIKE '%350%'`); n != 0 {
		t.Fatal("el monto quedó en las fuentes")
	}
	if n := x.contar(`SELECT count(*) FROM integracion.outbox WHERE payload::text LIKE '%350.00%'`); n != 0 {
		t.Fatal("el monto quedó en el outbox")
	}
	if n := x.contar(`SELECT count(*) FROM auditoria.acciones WHERE entrada::text LIKE '%350.00%' OR resultado::text LIKE '%350.00%'`); n != 0 {
		t.Fatal("el monto quedó en la auditoría")
	}
}

func TestAsistenteSaldoSinVerificarNoDaMontos(t *testing.T) {
	x := nuevaBandejaIA(t)
	x.activar(nil)
	conv, _ := x.conClienteArgos()
	if _, err := x.bd.Exec(x.ctx, `UPDATE mensajeria.conversaciones SET cliente_verificado = false WHERE id = $1`, conv); err != nil {
		t.Fatal(err)
	}
	x.argos.mu.Lock()
	x.argos.cuenta = &argos.EstadoCuenta{Saldo: texto("125.50"), Moneda: "GTQ", FacturasVencidas: vencidas(1), MontoVencido: texto("125.50")}
	x.argos.mu.Unlock()
	// El modelo «desobedece» y da el monto: el servidor rechaza la sugerencia, sin el número.
	x.ia.decir(respuestaModelo("Tu saldo pendiente es de 125.50, puedes pagar en agencias.", []string{"F1"}, nil))
	s := x.pedirYGenerar(x.agente, conv)
	errSug, _ := s["error"].(map[string]any)
	if s["estado"] != "error" || errSug["codigo"] != "respuesta_invalida" || strings.Contains(errSug["detalle"].(string), "125") {
		t.Fatalf("debía rechazarse: %v", s)
	}
	pedido, _, _ := x.ia.ultimo()
	if strings.Contains(pedido, "125.50") || !strings.Contains(pedido, "NO está verificado") {
		t.Fatalf("sin verificar, el modelo no ve el monto: %s", pedido)
	}
	// Sin monto: sale, y la fuente dice el estado (nunca el monto).
	x.ia.decir(respuestaModelo("Hola Ana, tienes un saldo pendiente. ¿Verificamos tu identidad para darte el detalle?", []string{"F1"}, nil))
	s = x.pedirYGenerar(x.agente, conv)
	if s["estado"] != "lista" || strings.Join(etiquetasArgos(s), ",") != "Con saldo vencido (1 factura) · Argos" {
		t.Fatalf("sugerencia: %v", s)
	}
}

func TestAsistenteSaldoAlDia(t *testing.T) {
	x := nuevaBandejaIA(t)
	x.activar(nil)
	conv, _ := x.conClienteArgos()
	x.argos.mu.Lock()
	x.argos.cuenta = &argos.EstadoCuenta{Saldo: texto("0.00"), Moneda: "GTQ", FacturasVencidas: vencidas(0), MontoVencido: texto("0.00")}
	x.argos.mu.Unlock()
	x.ia.decir(respuestaModelo("Hola Ana, tu servicio está al día.", []string{"F1"}, nil))
	s := x.pedirYGenerar(x.agente, conv)
	if s["estado"] != "lista" || strings.Join(etiquetasArgos(s), ",") != "Servicio activo, al día · Argos" {
		t.Fatalf("sugerencia: %v", s)
	}
	// Argos falla: la sugerencia sale igual, sin el estado de cuenta.
	x.argos.mu.Lock()
	x.argos.caido = true
	x.argos.mu.Unlock()
	s = x.pedirYGenerar(x.agente, conv)
	if s["estado"] != "lista" || strings.Join(etiquetasArgos(s), ",") != "Servicio activo, plan Fibra 20 Mbps · Argos" {
		t.Fatalf("sin Argos: %v", s)
	}
}

func TestContieneMonto(t *testing.T) {
	for texto, monto := range map[string]bool{
		"Tu saldo es Q350.00":                          true,
		"Son Q 1,250 en total":                         true,
		"El total es GTQ 99":                           true,
		"Debes $20":                                    true,
		"Debes 350.00 de tu servicio":                  true,
		"El monto a pagar es 99,50":                    true,
		"Son 350 quetzales":                            true,
		"Tienes un saldo pendiente; ¿verificamos?":     false,
		"Tu plan es de 20 Mbps y la luz LOS está roja": false,
		"La versión 1.14 del módem está al día":        false,
	} {
		if got := mensajeria.ContieneMonto(texto); got != monto {
			t.Errorf("ContieneMonto(%q) = %v", texto, got)
		}
	}
}

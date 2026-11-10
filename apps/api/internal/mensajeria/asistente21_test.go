package mensajeria_test

import (
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

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

func (x *bandejaIA) cuenta(e *argos.EstadoCuenta) {
	x.argos.mu.Lock()
	defer x.argos.mu.Unlock()
	x.argos.cuenta = e
}

// Cliente verificado: el modelo ve solo «con saldo vencido (N facturas)»; el monto nunca le llega ni
// puede salir en la sugerencia.
func TestAsistenteSaldoClienteVerificado(t *testing.T) {
	x := nuevaBandejaIA(t)
	x.activar(nil)
	conv, _ := x.conClienteArgos()
	x.cuenta(&argos.EstadoCuenta{Saldo: texto("357.25"), Moneda: "GTQ", FacturasVencidas: vencidas(2), MontoVencido: texto("357.25")})
	x.ia.decir(respuestaModelo("Hola Ana, tienes un saldo pendiente de 2 facturas vencidas; un agente te confirma el detalle.", []string{"F1"}, nil))
	s := x.pedirYGenerar(x.agente, conv)
	if s["estado"] != "lista" || strings.Join(etiquetasArgos(s), ",") != "Con saldo vencido (2 facturas) · Argos" {
		t.Fatalf("sugerencia: %v", s)
	}
	pedido, _, _ := x.ia.ultimo()
	if strings.Contains(pedido, "357") || strings.Contains(pedido, "GTQ") || !strings.Contains(pedido, "con saldo vencido (2 facturas vencidas)") {
		t.Fatalf("el modelo ve el estado, nunca el monto: %s", pedido)
	}
	x.argos.mu.Lock()
	actor := x.argos.actores[len(x.argos.actores)-1]
	x.argos.mu.Unlock()
	if actor.Actor != mensajeria.ActorAsistenteIA || actor.EmpresaID != 1 || !strings.HasPrefix(actor.Conversacion, "iris:conversacion:") {
		t.Fatalf("cabeceras de §7.1: %+v", actor)
	}
	// Aunque el cliente esté verificado, un monto de deuda en el texto se rechaza (y no va al detalle).
	x.ia.decir(respuestaModelo("Hola Ana, tu saldo vencido es de Q357.25.", []string{"F1"}, nil))
	s = x.pedirYGenerar(x.agente, conv)
	errSug, _ := s["error"].(map[string]any)
	if s["estado"] != "error" || errSug["codigo"] != "respuesta_invalida" || strings.Contains(errSug["detalle"].(string), "357") {
		t.Fatalf("un monto con cliente verificado también se rechaza: %v", s)
	}
	// El monto no quedó en ningún lado.
	for tabla, sql := range map[string]string{
		"sugerencias": `SELECT count(*) FROM mensajeria.sugerencias WHERE coalesce(texto, '') LIKE '%357%' OR fuentes::text LIKE '%357%' OR coalesce(error_detalle, '') LIKE '%357%'`,
		"outbox":      `SELECT count(*) FROM integracion.outbox WHERE payload::text LIKE '%357.25%'`,
		"auditoría":   `SELECT count(*) FROM auditoria.acciones WHERE entrada::text LIKE '%357.25%' OR resultado::text LIKE '%357.25%'`,
	} {
		if n := x.contar(sql); n != 0 {
			t.Fatalf("el monto quedó en %s", tabla)
		}
	}
}

// Cliente sin verificar (B2): al modelo no le llega nada del estado de cuenta, solo la instrucción de
// no hablar de saldos; el chip de la fuente (interno, para el agente) sí lo dice.
func TestAsistenteSaldoSinVerificar(t *testing.T) {
	x := nuevaBandejaIA(t)
	x.activar(nil)
	conv, _ := x.conClienteArgos()
	if _, err := x.bd.Exec(x.ctx, `UPDATE mensajeria.conversaciones SET cliente_verificado = false WHERE id = $1`, conv); err != nil {
		t.Fatal(err)
	}
	x.cuenta(&argos.EstadoCuenta{Saldo: texto("125.50"), Moneda: "GTQ", FacturasVencidas: vencidas(1), MontoVencido: texto("125.50")})
	x.ia.decir(respuestaModelo("Hola Ana, para darte ese detalle primero verificamos tu identidad. ¿Te parece?", []string{"F1"}, nil))
	s := x.pedirYGenerar(x.agente, conv)
	if s["estado"] != "lista" || strings.Join(etiquetasArgos(s), ",") != "Con saldo vencido (1 factura) · Argos" {
		t.Fatalf("sugerencia: %v", s)
	}
	pedido, _, _ := x.ia.ultimo()
	// Solo lo que cambia por pedido (las instrucciones fijas hablan de saldos en general).
	pedido = pedido[strings.Index(pedido, `"role":"user"`):]
	for _, prohibido := range []string{"125", "saldo vencido", "facturas vencidas", "al día", "Estado de cuenta"} {
		if strings.Contains(pedido, prohibido) {
			t.Fatalf("sin verificar, el modelo no ve nada del estado de cuenta (%q): %s", prohibido, pedido)
		}
	}
	if !strings.Contains(pedido, "NO está verificado") {
		t.Fatalf("falta la instrucción de no hablar de saldos: %s", pedido)
	}
	// El modelo «desobedece» y da un monto: rechazada.
	x.ia.decir(respuestaModelo("Tu saldo pendiente es de 125.50, puedes pagar en agencias.", []string{"F1"}, nil))
	s = x.pedirYGenerar(x.agente, conv)
	errSug, _ := s["error"].(map[string]any)
	if s["estado"] != "error" || errSug["codigo"] != "respuesta_invalida" || strings.Contains(errSug["detalle"].(string), "125") {
		t.Fatalf("debía rechazarse: %v", s)
	}
}

func TestAsistenteSaldoAlDia(t *testing.T) {
	x := nuevaBandejaIA(t)
	x.activar(nil)
	conv, _ := x.conClienteArgos()
	x.cuenta(&argos.EstadoCuenta{Saldo: texto("0.00"), Moneda: "GTQ", FacturasVencidas: vencidas(0), MontoVencido: texto("0.00")})
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

// otroCliente: otro cliente de la instantánea de la empresa 1, con un servicio.
func (x *bandejaIA) otroCliente() int64 {
	x.t.Helper()
	id := siguienteCliente.Add(1)
	if _, err := x.bd.Exec(x.ctx, `INSERT INTO integracion.argos_clientes (id, empresa_id, nombre, codigo, sincronizado_en, empresas)
		VALUES ($1, 1, 'Beto Paz', 'C-3', now(), '{1}')`, id); err != nil {
		x.t.Fatal(err)
	}
	if _, err := x.bd.Exec(x.ctx, `INSERT INTO integracion.argos_servicios (id, empresa_id, cliente_id, codigo, plan_nombre, estado, zona_nombre, sincronizado_en)
		VALUES ($1, 1, $2, 'S-BETO', 'Fibra 99 Mbps', 'cortado', 'Zona 2', now())`, id*10, id); err != nil {
		x.t.Fatal(err)
	}
	return id
}

// M2: el estado de cuenta leído es de un cliente. Si la conversación cambia de cliente mientras Argos
// responde, no se usa; si cambia mientras el modelo genera, la sugerencia queda vencida.
func TestAsistenteCuentaAtadaAlCliente(t *testing.T) {
	x := nuevaBandejaIA(t)
	x.activar(nil)
	conv, cliente := x.conClienteArgos()
	otro := x.otroCliente()
	x.cuenta(&argos.EstadoCuenta{FacturasVencidas: vencidas(3), MontoVencido: texto("900.00"), Moneda: "GTQ"})

	// 1. Cambia de cliente mientras «Argos» atiende el estado de cuenta (antes de preparar).
	x.argos.mu.Lock()
	x.argos.alLeerCuenta = func() {
		if _, err := x.bd.Exec(x.ctx, `UPDATE mensajeria.conversaciones SET cliente_id = $2, servicio_id = NULL WHERE id = $1`, conv, otro); err != nil {
			t.Error(err)
		}
	}
	x.argos.mu.Unlock()
	x.ia.decir(respuestaModelo("Hola, reviso tu servicio.", []string{"F1"}, nil))
	s := x.pedirYGenerar(x.agente, conv)
	if s["estado"] != "lista" || strings.Join(etiquetasArgos(s), ",") != "Servicio cortado, plan Fibra 99 Mbps · Argos" {
		t.Fatalf("la cuenta de otro cliente no se usa: %v", s)
	}
	if pedido, _, _ := x.ia.ultimo(); strings.Contains(pedido, "saldo vencido") || !strings.Contains(pedido, "S-BETO") {
		t.Fatalf("el modelo vio el estado de cuenta de otro cliente: %s", pedido)
	}

	// 2. Cambia de cliente mientras el modelo genera: vencida al guardar.
	x.argos.mu.Lock()
	x.argos.alLeerCuenta = nil
	x.argos.mu.Unlock()
	*x.reloj = x.reloj.Add(7 * time.Second)
	p := x.api(x.agente, http.MethodPost, "/conversaciones/"+conv+"/sugerencias", map[string]any{})
	x.exigir(p, http.StatusAccepted)
	x.ia.mu.Lock()
	x.ia.alPedir = func() {
		if _, err := x.bd.Exec(x.ctx, `UPDATE mensajeria.conversaciones SET cliente_id = $2 WHERE id = $1`, conv, cliente); err != nil {
			t.Error(err)
		}
	}
	x.ia.mu.Unlock()
	if err := x.m.GenerarSugerencia(x.ctx, uuid.MustParse(p.cuerpo["id"].(string))); err != nil {
		t.Fatal(err)
	}
	x.ia.mu.Lock()
	x.ia.alPedir = nil
	x.ia.mu.Unlock()
	if n := x.contar(`SELECT count(*) FROM mensajeria.sugerencias WHERE id = $1 AND estado = 'vencida' AND texto IS NULL AND fuentes = '[]'`, p.cuerpo["id"]); n != 1 {
		t.Fatal("la sugerencia de otro cliente debía quedar vencida, sin texto ni fuentes")
	}
}

// M2: asociar otro cliente o desasociarlo vence la sugerencia vigente.
func TestAsistenteReasociarVenceLaSugerencia(t *testing.T) {
	x := nuevaBandejaIA(t)
	x.activar(nil)
	conv, _ := x.conClienteArgos()
	otro := x.otroCliente()
	x.cuenta(&argos.EstadoCuenta{FacturasVencidas: vencidas(0), MontoVencido: texto("0.00")})
	x.ia.decir(respuestaModelo("Hola Ana, tu servicio está al día.", []string{"F1"}, nil))
	vigente := func() string {
		s := x.pedirYGenerar(x.agente, conv)
		if s["estado"] != "lista" {
			t.Fatalf("sugerencia: %v", s)
		}
		return s["id"].(string)
	}
	version := func() any {
		return x.contar(`SELECT version FROM mensajeria.conversaciones WHERE id = $1`, conv)
	}
	id := vigente()
	x.exigir(x.api(x.supervisor, http.MethodPost, "/conversaciones/"+conv+"/asociar-cliente", map[string]any{"version": version(), "clienteId": otro}), http.StatusOK)
	if n := x.contar(`SELECT count(*) FROM mensajeria.sugerencias WHERE id = $1 AND estado = 'vencida'`, id); n != 1 {
		t.Fatal("asociar otro cliente debía vencer la sugerencia")
	}
	x.exigir(x.api(x.agente, http.MethodGet, "/conversaciones/"+conv+"/sugerencias/actual", nil), http.StatusNotFound)
	id = vigente()
	x.exigir(x.api(x.supervisor, http.MethodPost, "/conversaciones/"+conv+"/desasociar", map[string]any{"version": version()}), http.StatusOK)
	if n := x.contar(`SELECT count(*) FROM mensajeria.sugerencias WHERE id = $1 AND estado = 'vencida'`, id); n != 1 {
		t.Fatal("desasociar debía vencer la sugerencia")
	}
}

// M1: el validador de montos (casos del revisor y precios de plan que deben pasar).
func TestContieneMonto(t *testing.T) {
	nbsp, angosto := " ", " "
	for _, c := range []struct {
		texto string
		monto bool
	}{
		// Moneda antes o después del número.
		{"Tu saldo es Q350.00", true},
		{"Tu saldo es de 350 Q", true},
		{"Tu saldo es de 350Q", true},
		{"Tu saldo es Q: 350", true},
		{"Tu saldo es Q*350*", true},
		{"Tu saldo es *Q* _350_", true},
		{"Tu saldo es Q" + nbsp + "350", true},
		{"Tu saldo es Q" + angosto + "350", true},
		{"Tu saldo es Ｑ３５０", true}, // ancho completo (NFKC)
		{"Tienes una deuda de GTQ 99", true},
		{"Tienes una deuda de 99 qtz", true},
		{"Debes USD 20", true},
		{"Debes $20", true},
		{"Debes 20$", true},
		{"Debes 20 dólares", true},
		{"Tu deuda es de 500 pesos", true},
		{"Tu saldo: 350 quetzales", true},
		// Sin moneda, cerca de una palabra de deuda.
		{"Usted debe 1,250", true},
		{"Su saldo es de 150", true},
		{"El total a pagar es 99,50", true},
		{"Tienes pendiente de pago 475", true},
		{"Tu factura vencida es por 300", true},
		{"El cliente adeuda 80", true},
		// Numerales en letras.
		{"Su saldo es de ciento cincuenta quetzales", true},
		{"Debes cien quetzales", true},
		{"Tu deuda es de mil", true},
		{"Debes Q cien", true},
		{"Tu saldo es de veinte dólares", true},
		// No son montos de deuda.
		{"Tienes un saldo pendiente; ¿verificamos tu identidad?", false},
		{"Tienes un saldo pendiente con 2 facturas vencidas.", false},
		{"Tienes un saldo vencido de 12 facturas; un agente te da el detalle.", false},
		{"Tu factura vencida es del 15 de noviembre.", false},
		{"Tu servicio está al día.", false},
		// Precios de plan tomados de un manual: pasan.
		{"El plan de 100 Mb cuesta Q 299 al mes.", false},
		{"Tenemos planes de 50 Mbps por Q199 y de 100 Mbps por Q299 mensuales.", false},
		{"La instalación tiene un costo de 150 quetzales.", false},
		{"Tu plan es de 20 Mbps y la luz LOS está roja.", false},
		{"La versión 1.14 del módem está al día.", false},
	} {
		if got := mensajeria.ContieneMonto(c.texto); got != c.monto {
			t.Errorf("ContieneMonto(%q) = %v, se esperaba %v", c.texto, got, c.monto)
		}
	}
}

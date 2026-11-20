package ami_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/mensajeria/ami"
	"github.com/HCDestroyer/iris/apps/api/internal/mensajeria/ami/amifalso"
)

func falso(t *testing.T) *amifalso.Servidor {
	t.Helper()
	s, err := amifalso.Nuevo("iris_lectura", "s3cr3to-largo")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Cerrar)
	return s
}

func opciones(s *amifalso.Servidor) ami.Opciones {
	return ami.Opciones{Host: "127.0.0.1", Puerto: s.Puerto(), Usuario: "iris_lectura", Secreto: "s3cr3to-largo", Plazo: 3 * time.Second}
}

func TestConectarFijaHuellaYLeeEventos(t *testing.T) {
	s := falso(t)
	o := opciones(s)
	o.Eventos = "call,cdr"
	c, err := ami.Conectar(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Cerrar()
	if c.Huella() != s.Huella() {
		t.Fatalf("huella %s, quería %s", c.Huella(), s.Huella())
	}
	if !strings.HasPrefix(c.Version(), "Asterisk Call Manager/") {
		t.Fatal(c.Version())
	}
	if got := s.Clases(); strings.Join(got, ",") != "call,cdr" {
		t.Fatalf("clases %v", got)
	}
	p, err := c.Accion(context.Background(), "Ping")
	if err != nil || p.Valor("Ping") != "Pong" {
		t.Fatalf("ping: %v %v", p, err)
	}
	s.Emitir("Event", "Newchannel", "Linkedid", "1700.1", "Channel", "SIP/troncal-0001")
	select {
	case e := <-c.Eventos():
		for e.Valor("Event") == "FullyBooted" {
			e = <-c.Eventos()
		}
		if e.Valor("event") != "Newchannel" || e.Valor("LINKEDID") != "1700.1" {
			t.Fatalf("evento %v", e)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("sin evento")
	}
}

func TestHuellaDistintaCortaAntesDelLogin(t *testing.T) {
	s := falso(t)
	o := opciones(s)
	o.Huella = strings.Repeat("ab", 32)
	_, err := ami.Conectar(context.Background(), o)
	var eh *ami.ErrHuella
	if !errors.As(err, &eh) || eh.Vista != s.Huella() {
		t.Fatalf("quería ErrHuella con la vista, vino %v", err)
	}
	if ok, fallidos := s.Logins(); ok+fallidos != 0 {
		t.Fatal("con otra huella no debe llegar el Login (ni el secreto)")
	}
}

func TestCredencialesRechazadas(t *testing.T) {
	s := falso(t)
	o := opciones(s)
	o.Secreto = "otro"
	if _, err := ami.Conectar(context.Background(), o); !errors.Is(err, ami.ErrCredenciales) {
		t.Fatalf("quería ErrCredenciales, vino %v", err)
	}
	if strings.Contains(o.String(), "otro") {
		t.Fatal("el secreto no debe salir en el resumen")
	}
}

func TestSinServidorEsErrorUnico(t *testing.T) {
	s := falso(t)
	o := opciones(s)
	s.Cerrar()
	if _, err := ami.Conectar(context.Background(), o); !errors.Is(err, ami.ErrConexion) {
		t.Fatalf("quería ErrConexion, vino %v", err)
	}
}

func TestPermisosDeMas(t *testing.T) {
	s := falso(t)
	c, err := ami.Conectar(context.Background(), opciones(s))
	if err != nil {
		t.Fatal(err)
	}
	p, err := c.Accion(context.Background(), "ListCommands")
	if err != nil {
		t.Fatal(err)
	}
	if d := ami.PermisosDeMas(p); len(d) != 0 {
		t.Fatalf("solo lectura: %v", d)
	}
	c.Cerrar()
	s.PermitirEscritura("Originate", "Command")
	c, err = ami.Conectar(context.Background(), opciones(s))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Cerrar()
	p, err = c.Accion(context.Background(), "ListCommands")
	if err != nil {
		t.Fatal(err)
	}
	if d := ami.PermisosDeMas(p); strings.Join(d, ",") != "Originate,Command" {
		t.Fatalf("de más: %v", d)
	}
}

func TestNoMandaAccionesDeEscrituraNiInyeccion(t *testing.T) {
	s := falso(t)
	c, err := ami.Conectar(context.Background(), opciones(s))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Cerrar()
	if _, err := c.Accion(context.Background(), "Originate"); !errors.Is(err, ami.ErrValor) {
		t.Fatalf("Originate no se manda: %v", err)
	}
	o := opciones(s)
	o.Usuario = "iris\r\nAction: Originate"
	if _, err := ami.Conectar(context.Background(), o); !errors.Is(err, ami.ErrValor) {
		t.Fatalf("usuario con salto de línea: %v", err)
	}
	o = opciones(s)
	o.Secreto = "a\nb"
	if _, err := ami.Conectar(context.Background(), o); !errors.Is(err, ami.ErrValor) {
		t.Fatalf("secreto con salto de línea: %v", err)
	}
	for _, a := range s.Recibido() {
		if strings.EqualFold(a, "Originate") {
			t.Fatal("llegó un Originate al AMI")
		}
	}
}

func TestLineaEnormeCortaLaConexion(t *testing.T) {
	s := falso(t)
	c, err := ami.Conectar(context.Background(), opciones(s))
	if err != nil {
		t.Fatal(err)
	}
	s.Crudo("Event: Newchannel\r\nCallerIDNum: " + strings.Repeat("9", ami.MaxLinea+10) + "\r\n\r\n")
	select {
	case <-c.Cerrada():
		if !errors.Is(c.Err(), ami.ErrProtocolo) {
			t.Fatalf("quería ErrProtocolo, vino %v", c.Err())
		}
	case <-time.After(3 * time.Second):
		t.Fatal("la línea enorme no cortó la conexión")
	}
}

func TestPaqueteConDemasiadosCampos(t *testing.T) {
	s := falso(t)
	c, err := ami.Conectar(context.Background(), opciones(s))
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	b.WriteString("Event: X\r\n")
	for range ami.MaxCampos + 1 {
		b.WriteString("K: v\r\n")
	}
	b.WriteString("\r\n")
	s.Crudo(b.String())
	select {
	case <-c.Cerrada():
	case <-time.After(3 * time.Second):
		t.Fatal("no cortó")
	}
}

// Las clases (Priv) de cada línea de ListCommands deciden; sin clases (AMI viejo), la lista de
// acciones conocidas.
func TestPermisosDeMasPorClases(t *testing.T) {
	p := ami.Paquete{Campos: [][2]string{
		{"Response", "Success"}, {"ActionID", "iris-1"},
		{"Ping", "Keepalive command.  (Priv: <none>)"},
		{"ListCommands", "List available manager commands.  (Priv: <none>)"},
		{"QueueChangePriorityCaller", "Change priority of a caller on queue.  (Priv: agent,all)"},
		{"CoreSettings", "Show PBX core settings (version etc).  (Priv: system,reporting,all)"},
		{"Raro", "Sin clase conocida.  (Priv: all)"},
		{"Status", "List channel status.  (Priv: reporting)"},
		{"Originate", "Originate a call."}, // AMI viejo sin Priv
		{"Ping", "otra vez.  (Priv: <none>)"},
	}}
	if d := ami.PermisosDeMas(p); strings.Join(d, ",") != "QueueChangePriorityCaller,CoreSettings,Raro,Originate" {
		t.Fatalf("de más: %v", d)
	}
}

// Con la cola del lector llena de otros eventos, los cierres (Hangup y Cdr) todavía entran.
func TestColaLlenaGuardaLosCierres(t *testing.T) {
	s := falso(t)
	o := opciones(s)
	o.Eventos = "call,cdr"
	o.Huella = s.Huella()
	c, err := ami.Conectar(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Cerrar()
	var b strings.Builder
	for i := range ami.ColaEventos + 300 {
		b.WriteString("Event: Newstate\r\nChannel: PJSIP/301-1\r\nLinkedid: 17." + strings.Repeat("1", 1+i%5) + "\r\n\r\n")
	}
	b.WriteString("Event: Hangup\r\nChannel: PJSIP/claro-1\r\nUniqueid: 1700.1\r\nLinkedid: 1700.1\r\n\r\n")
	b.WriteString("Event: Cdr\r\nUniqueID: 1700.1\r\nDisposition: ANSWERED\r\nBillableSeconds: 12\r\n\r\n")
	s.Crudo(b.String())
	limite := time.Now().Add(5 * time.Second)
	for c.Descartados.Load() < 300 {
		if time.Now().After(limite) {
			t.Fatalf("no se llenó la cola: %d descartados", c.Descartados.Load())
		}
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(200 * time.Millisecond) // el lector termina de leer el Hangup y el Cdr
	vistos := map[string]bool{}
	for {
		select {
		case p := <-c.Eventos():
			vistos[p.Valor("Event")] = true
			continue
		default:
		}
		break
	}
	if !vistos["Hangup"] || !vistos["Cdr"] {
		t.Fatalf("con la cola llena se descartó un cierre: %v", vistos)
	}
}

// aoc, cc y test también son clases de escritura (AOCMessage, CC…).
func TestPermisosDeMasClasesAOCCCTest(t *testing.T) {
	p := ami.Paquete{Campos: [][2]string{
		{"Response", "Success"},
		{"AOCMessage", "Generate an Advice of Charge message on a channel.  (Priv: aoc,all)"},
		{"CCAlgo", "Algo de CC.  (Priv: cc,all)"},
		{"TestAlgo", "Algo de prueba.  (Priv: test,all)"},
		{"Ping", "Keepalive command.  (Priv: <none>)"},
	}}
	if d := ami.PermisosDeMas(p); strings.Join(d, ",") != "AOCMessage,CCAlgo,TestAlgo" {
		t.Fatalf("de más: %v", d)
	}
}

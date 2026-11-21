package mensajeria_test

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// Hallazgos de la revisión de seguridad de E4c (#246) que se prueban de punta a punta.

func (b *bancoTelefono) registrar(w *telefonoWS) {
	w.mandar(fmt.Sprintf("\nREGISTER sip:%[1]s SIP/2.0\nVia: SIP/2.0/WSS abc.invalid;branch=z9hG4bKreg1\nMax-Forwards: 69\nTo: <sip:201@%[1]s>\n"+
		"From: <sip:201@%[1]s>;tag=r1\nCall-ID: reg-1\nCSeq: 1 REGISTER\nContact: <sip:201@abc.invalid;transport=ws>\nExpires: 600\nContent-Length: 0\n\n", dominioSIP))
}

// I3: la Issabel rechaza la credencial: el puente corta, la extensión queda sin «Probar registro» y el
// teléfono no se vuelve a abrir hasta otra prueba (si no, el navegador reintentaría y fail2ban
// bloquearía la IP de Iris para todos).
func TestTelefonoCredencialRechazadaCorta(t *testing.T) {
	b := nuevoBancoTelefono(t)
	b.exigir(b.api(b.admin, http.MethodPost, b.rutaExt("/telefono"), map[string]any{"tipo": "navegador", "sipSecreto": "otra-clave-mala"}), http.StatusOK)
	b.sql(`UPDATE mensajeria.centralita_extensiones SET sip_probado_en = now() WHERE id = $1`, int64(b.extID))
	antes := len(b.sip.Recibidos())
	w, _, err := b.dial(b.agente, b.abrir(b.agente, "panel"), baseIris)
	if err != nil {
		t.Fatal(err)
	}
	b.registrar(w)
	w.respuesta(403)
	if c := w.cierre(); c != 4003 {
		t.Fatalf("cierre: %d", c)
	}
	b.hasta(func() bool {
		var probado *time.Time
		_ = b.bd.QueryRow(b.ctx, `SELECT sip_probado_en FROM mensajeria.centralita_extensiones WHERE id = $1`, int64(b.extID)).Scan(&probado)
		return probado == nil
	}, "la extensión siguió como probada")
	b.exigirProblema(b.api(b.agente, http.MethodPost, "/telefono/abrir", map[string]any{"dispositivo": "panel"}), http.StatusUnprocessableEntity, "registro_sin_probar")
	if n := len(b.sip.Recibidos()) - antes; n != 2 {
		t.Fatalf("REGISTER en la centralita: %d (un intento y su reintento)", n)
	}
}

// I4 y menores: /abrir con límite por minuto y el mismo usuario TURN en la ventana.
func TestTelefonoAbrirConLimiteYTURNEstable(t *testing.T) {
	b := nuevoBancoTelefono(t)
	var usuarios []string
	for i := 0; i < 6; i++ {
		r := b.api(b.agente, http.MethodPost, "/telefono/abrir", map[string]any{"dispositivo": "panel"})
		b.exigir(r, http.StatusCreated)
		usuarios = append(usuarios, r.cuerpo["ice"].([]any)[0].(map[string]any)["username"].(string))
	}
	b.exigirProblema(b.api(b.agente, http.MethodPost, "/telefono/abrir", map[string]any{"dispositivo": "panel"}), http.StatusTooManyRequests, "demasiados_intentos")
	for _, u := range usuarios {
		if u != usuarios[0] {
			t.Fatalf("usuario TURN distinto en la misma ventana: %v", usuarios)
		}
	}
	*b.reloj = b.reloj.Add(2 * time.Minute)
	b.exigir(b.api(b.agente, http.MethodPost, "/telefono/abrir", map[string]any{"dispositivo": "panel"}), http.StatusCreated)
}

// Menores: un frame binario cierra (no escapa al tope de 200 por minuto) y nada llega a la Issabel.
func TestTelefonoFrameBinarioCierra(t *testing.T) {
	b := nuevoBancoTelefono(t)
	antes := len(b.sip.Recibidos())
	w, _, err := b.dial(b.agente, b.abrir(b.agente, "panel"), baseIris)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancelar := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancelar()
	if err := w.c.Write(ctx, websocket.MessageBinary, []byte("REGISTER")); err != nil {
		t.Fatal(err)
	}
	w.cierre()
	if n := len(b.sip.Recibidos()) - antes; n != 0 {
		t.Fatalf("llegó a la centralita: %v", b.sip.Recibidos())
	}
}

// Menores: llamada.transferida sale con el 202 de la Issabel, no al validar el REFER.
func TestTelefonoTransferidaConElAceptado(t *testing.T) {
	b := nuevoBancoTelefono(t)
	w, _, err := b.dial(b.agente, b.abrir(b.agente, "panel"), baseIris)
	if err != nil {
		t.Fatal(err)
	}
	r := b.api(b.agente, http.MethodPost, "/telefono/llamar", map[string]any{"numero": "55551234"})
	b.exigir(r, http.StatusCreated)
	id := r.cuerpo["llamada"].(map[string]any)["id"].(string)
	w.mandar(sipInvite(r.cuerpo["destino"].(string), "tr1", 1))
	totag, _ := w.respuesta(200).To().Params.Get("tag")
	b.hasta(func() bool { return b.estadoLlamada(id) == "en_curso" }, "la llamada no quedó en curso")
	refer := func(rama, destino string) string {
		return fmt.Sprintf("\nREFER sip:asterisk@127.0.0.1;transport=ws SIP/2.0\nVia: SIP/2.0/WSS abc.invalid;branch=z9hG4bK%[1]s\nMax-Forwards: 69\n"+
			"To: <sip:x@%[2]s>;tag=%[3]s\nFrom: <sip:201@%[2]s>;tag=nav-tr1\nCall-ID: call-tr1\nCSeq: 2 REFER\nRefer-To: <sip:%[4]s@%[2]s>\nContent-Length: 0\n\n",
			rama, dominioSIP, totag, destino)
	}
	contar := func() int {
		return b.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'llamada.transferida' AND payload->>'llamada_id' = $1`, id)
	}
	w.mandar(refer("rf1", "0019005551234"))
	w.respuesta(403)
	if contar() != 0 {
		t.Fatal("transferida sin el 202")
	}
	w.mandar(refer("rf2", "205"))
	w.respuesta(202)
	b.hasta(func() bool { return contar() == 1 }, "no salió llamada.transferida con el 202")
	if p := b.outbox("llamada.transferida", id); p["a_usuario_id"] != float64(b.agente2.id) || p["origen"] != "centralita" {
		t.Fatalf("llamada.transferida: %v", p)
	}
}

// B3: sin confirmar las restricciones de la Issabel (runbook §9.3) el teléfono no se enciende, y
// quitarlas lo apaga.
func TestTelefonoRestriccionesConfirmadas(t *testing.T) {
	b := nuevoBancoTelefono(t)
	r := b.api(b.admin, http.MethodPost, b.ruta("/telefono"), map[string]any{"version": b.version, "wssPuerto": b.sip.Puerto(), "wssRuta": "/ws",
		"huellaWss": b.sip.Huella(), "sipDominio": dominioSIP, "prefijoSalida": "9", "llamadasPorUsuarioDia": 60, "llamadasSimultaneas": 10})
	b.exigir(r, http.StatusOK)
	tel := r.cuerpo["telefono"].(map[string]any)
	if tel["activo"] != false || tel["restriccionesConfirmadasEn"] != nil {
		t.Fatalf("sin confirmar: %v", tel)
	}
	b.version = r.cuerpo["version"].(float64)
	b.exigirProblema(b.api(b.admin, http.MethodPost, b.ruta("/telefono/activar"), map[string]any{"version": b.version}), http.StatusUnprocessableEntity,
		"restricciones_sin_confirmar")
}

// Menores: el límite del día se cuenta con el candado tomado (llamar en paralelo no lo pasa).
func TestTelefonoLimiteDiaEnParalelo(t *testing.T) {
	b := nuevoBancoTelefono(t)
	b.sql(`UPDATE mensajeria.centralitas SET llamadas_por_usuario_dia = 2 WHERE id = $1`, b.id)
	const n = 8
	codigos := make(chan int, n)
	for i := 0; i < n; i++ {
		go func(i int) {
			codigos <- b.api(b.agente, http.MethodPost, "/telefono/llamar", map[string]any{"numero": fmt.Sprintf("5555%04d", i)}).status
		}(i)
	}
	hechas := 0
	for i := 0; i < n; i++ {
		if <-codigos == http.StatusCreated {
			hechas++
		}
	}
	if hechas != 2 {
		t.Fatalf("pasaron %d llamadas con un límite de 2", hechas)
	}
}

// Menores: el AMI adopta la saliente del puente solo con el número completo (no por los últimos 8
// dígitos: un internacional que termina igual no se la lleva).
func TestTelefonoAdopcionPorNumeroCompleto(t *testing.T) {
	b := nuevoBancoTelefono(t)
	b.arrancar()
	r := b.api(b.agente, http.MethodPost, "/telefono/llamar", map[string]any{"numero": "55551234"})
	b.exigir(r, http.StatusCreated)
	id := r.cuerpo["llamada"].(map[string]any)["id"].(string)
	externo := func() string {
		var e string
		if err := b.bd.QueryRow(b.ctx, `SELECT id_externo FROM mensajeria.llamadas WHERE id = $1`, id).Scan(&e); err != nil {
			t.Fatal(err)
		}
		return e
	}
	b.ami.Emitir("Event", "DialBegin", "Channel", "PJSIP/201-1", "DestChannel", "PJSIP/troncal-2", "DestExten", "00155551234", "Linkedid", "1800.100")
	time.Sleep(300 * time.Millisecond)
	if e := externo(); e != "iris:"+id {
		t.Fatalf("la adoptó otro número: %s", e)
	}
	b.ami.Emitir("Event", "DialBegin", "Channel", "PJSIP/201-3", "DestChannel", "PJSIP/troncal-4", "DestExten", "955551234", "Linkedid", "1800.101")
	b.hasta(func() bool { return externo() == "1800.101" }, "el AMI no adoptó la saliente del puente")
	if n := b.contar(`SELECT count(*) FROM mensajeria.llamadas WHERE origen = 'centralita'`); n != 1 {
		t.Fatalf("filas: %d", n)
	}
}

// Segunda revisión (N4): /abrir cuenta con candado (en paralelo no pasan más de 6 por persona) y hay
// un tope por empresa.
func TestTelefonoAbrirEnParaleloYTopePorEmpresa(t *testing.T) {
	b := nuevoBancoTelefono(t)
	const n = 16
	codigos := make(chan int, n)
	for i := 0; i < n; i++ {
		go func() {
			codigos <- b.api(b.agente, http.MethodPost, "/telefono/abrir", map[string]any{"dispositivo": "panel"}).status
		}()
	}
	abiertos := 0
	for i := 0; i < n; i++ {
		if <-codigos == http.StatusCreated {
			abiertos++
		}
	}
	if abiertos != 6 {
		t.Fatalf("en paralelo se abrieron %d (tope 6 por persona)", abiertos)
	}
	*b.reloj = b.reloj.Add(2 * time.Minute)
	b.exigir(b.api(b.agente, http.MethodPost, "/telefono/abrir", map[string]any{"dispositivo": "panel"}), http.StatusCreated)
	// Otros 119 de la misma empresa en el mismo minuto (de otra persona): el agente ya no abre.
	b.sql(`INSERT INTO mensajeria.telefono_tickets (hash, usuario_id, empresa_id, extension_id, dispositivo, sesion_id, expira_en)
		SELECT decode(md5(g::text || 'tope'), 'hex'), $1, t.empresa_id, t.extension_id, t.dispositivo, t.sesion_id, t.expira_en
		FROM (SELECT * FROM mensajeria.telefono_tickets ORDER BY expira_en DESC LIMIT 1) t CROSS JOIN generate_series(1, 119) g`, b.admin.id)
	r := b.api(b.agente, http.MethodPost, "/telefono/abrir", map[string]any{"dispositivo": "panel"})
	b.exigirProblema(r, http.StatusTooManyRequests, "demasiados_intentos")
}

// Segunda revisión (N6): cambiar el host de la centralita apaga el teléfono y borra su prueba y la
// confirmación de restricciones (eran de otra Issabel); cambiar solo el nombre no.
func TestTelefonoSeApagaConOtroHost(t *testing.T) {
	b := nuevoBancoTelefono(t)
	version := func() float64 {
		var v int32
		if err := b.bd.QueryRow(b.ctx, `SELECT version FROM mensajeria.centralitas WHERE id = $1`, b.id).Scan(&v); err != nil {
			t.Fatal(err)
		}
		return float64(v)
	}
	telefono := func() (bool, bool, bool) {
		var activo, probado, restricciones bool
		if err := b.bd.QueryRow(b.ctx, `SELECT telefono_activo, telefono_probado_en IS NOT NULL, restricciones_confirmadas_en IS NOT NULL
			FROM mensajeria.centralitas WHERE id = $1`, b.id).Scan(&activo, &probado, &restricciones); err != nil {
			t.Fatal(err)
		}
		return activo, probado, restricciones
	}
	if a, p, r := telefono(); !a || !p || !r {
		t.Fatalf("el banco no dejó el teléfono encendido: %v %v %v", a, p, r)
	}
	w, _, err := b.dial(b.agente, b.abrir(b.agente, "panel"), baseIris)
	if err != nil {
		t.Fatal(err)
	}
	datos := b.datos()
	delete(datos, "secreto")
	datos["version"], datos["nombre"], datos["huellaTls"] = version(), "Otro nombre", b.ami.Huella()
	b.exigir(b.api(b.admin, http.MethodPost, b.ruta("/actualizar"), datos), http.StatusOK)
	if a, p, r := telefono(); !a || !p || !r {
		t.Fatalf("cambiar el nombre apagó el teléfono: %v %v %v", a, p, r)
	}
	datos = b.datos()
	datos["version"], datos["nombre"], datos["huellaTls"], datos["host"] = version(), "Otro nombre", b.ami.Huella(), "127.0.0.2"
	r := b.api(b.admin, http.MethodPost, b.ruta("/actualizar"), datos)
	b.exigir(r, http.StatusOK)
	if a, p, rs := telefono(); a || p || rs {
		t.Fatalf("otro host y el teléfono sigue: activo=%v probado=%v restricciones=%v", a, p, rs)
	}
	if c := w.cierre(); c == 1000 {
		t.Fatalf("la conexión abierta no se cortó: %d", c)
	}
	var campos string
	if err := b.bd.QueryRow(b.ctx, `SELECT payload->>'campos' FROM integracion.outbox WHERE evento = 'centralita.actualizada' AND (payload->>'centralita_id')::bigint = $1
		ORDER BY id DESC LIMIT 1`, b.id).Scan(&campos); err != nil || !strings.Contains(campos, "telefono_activo") {
		t.Fatalf("centralita.actualizada: %q %v", campos, err)
	}
}

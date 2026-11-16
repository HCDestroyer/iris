package mensajeria_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/mensajeria"
)

// Tercera revisión de seguridad de la #163: una prueba por punto.

// M-1. Confirmar el cliente con la conversación resuelta no deja una verificación fuerte que
// sobreviva al periodo: al reabrir, el saldo, la promesa y la reconexión piden el desafío.
func TestAutonomoRevision3ConfirmarEnResuelta(t *testing.T) {
	x := nuevaBandejaIA(t)
	x.autonomo("siempre_primero", map[string]any{"perfiles": []any{perfilCon(map[string]any{"promesa_pago": true, "reconexion_temporal": true}, "alta")}})
	x.cuenta(nil)
	conv, cliente := x.conClienteArgos()
	num := x.numeroDe(conv)
	x.exigir(x.api(x.supervisor, http.MethodPost, "/conversaciones/"+conv+"/resolver", map[string]any{"version": ver(x.conv(num))}), http.StatusOK)
	// Confirmar en resuelta: el cliente queda confirmado, pero sin verificación fuerte.
	x.exigir(x.api(x.supervisor, http.MethodPost, "/conversaciones/"+conv+"/confirmar-cliente", map[string]any{"version": ver(x.conv(num))}), http.StatusOK)
	if v := x.verificacion(conv); v != "numero" {
		t.Fatalf("confirmada en resuelta: %v", v)
	}
	if n := x.contar(`SELECT count(*) FROM mensajeria.conversaciones WHERE id = $1 AND identidad_verificada_en IS NOT NULL`, conv); n != 0 {
		t.Fatal("confirmar en resuelta fijó la verificación fuerte")
	}
	// Aunque alguien la dejara fijada por otro camino, el trigger la borra al reabrir.
	if _, err := x.bd.Exec(x.ctx, `UPDATE mensajeria.conversaciones SET identidad_verificada_en = $2, identidad_vence_en = $3 WHERE id = $1`,
		conv, *x.reloj, x.reloj.Add(24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	*x.reloj = x.reloj.Add(time.Hour)
	servicio := cliente * 10
	c := x.turno(num, "wamid.r3a1", "Hola, ¿cuánto debo? Quiero una promesa y que me reconecten", "Pagos y saldo",
		llama("consultar_saldo", `{}`),
		llama("pedir_promesa", fmt.Sprintf(`{"servicio_id": %d, "fecha": "2026-11-20"}`, servicio)),
		llama("pedir_reconexion", `{}`),
		dice("Para eso necesito verificar tu identidad. Escríbeme tu DPI completo."))
	if v := x.verificacion(conv); v != "numero" {
		t.Fatalf("al reabrir: %v", v)
	}
	if a := strings.Join(x.acciones(cid(c)), ","); a != "dar_saldo:rechazada,promesa_pago:rechazada,reconexion_temporal:rechazada,responder:ok" {
		t.Fatalf("después de reabrir no pidió el desafío: %s", a)
	}
	x.argos.mu.Lock()
	lecturas, claves, rec := x.argos.cuentas, len(x.argos.claves), len(x.argos.reconexiones)
	x.argos.mu.Unlock()
	if lecturas != 0 || claves != 0 || rec != 0 {
		t.Fatalf("llamó a Argos sin verificación: %d lecturas, %d claves, %d reconexiones", lecturas, claves, rec)
	}
	if !strings.Contains(x.herramientasOfrecidas(), `"verificar_identidad"`) {
		t.Fatal("no se ofreció verificar_identidad")
	}
}

// M-2. La verificación fuerte caduca dentro del periodo: 60 minutos por defecto, configurable por
// empresa (verificacionFuerteMin, 5 a 1440).
func TestAutonomoRevision3VigenciaDeFuerte(t *testing.T) {
	x := nuevaBandejaIA(t)
	x.autonomo("siempre_primero", nil)
	r := x.api(x.admin, http.MethodGet, "/asistente/ajustes", nil)
	x.exigir(r, http.StatusOK, "verificacionFuerteMin", "60")
	conv, _ := x.conClienteArgos()
	num := x.numeroDe(conv)
	confirmar := func() {
		t.Helper()
		x.exigir(x.api(x.supervisor, http.MethodPost, "/conversaciones/"+conv+"/confirmar-cliente", map[string]any{"version": ver(x.conv(num))}), http.StatusOK)
	}
	confirmar()
	*x.reloj = x.reloj.Add(59 * time.Minute)
	if v := x.verificacion(conv); v != "fuerte" {
		t.Fatalf("a los 59 min: %v", v)
	}
	*x.reloj = x.reloj.Add(2 * time.Minute)
	if v := x.verificacion(conv); v != "numero" {
		t.Fatalf("a los 61 min: %v", v)
	}
	// Vencida, el asistente pide el desafío otra vez.
	x.cuenta(nil)
	c := x.turno(num, "wamid.r3b1", "¿Cuánto debo?", "Pagos y saldo", llama("consultar_saldo", `{}`), dice("Necesito verificar tu identidad otra vez."))
	if a := x.acciones(cid(c)); len(a) == 0 || a[0] != "dar_saldo:rechazada" {
		t.Fatalf("con la verificación vencida: %v", a)
	}
	// Configurable: fuera de rango no; con 5 minutos vence a los 5.
	x.exigir(x.ajustes(map[string]any{"verificacionFuerteMin": 4}), http.StatusUnprocessableEntity)
	x.autonomo("siempre_primero", map[string]any{"verificacionFuerteMin": 5})
	x.exigir(x.api(x.admin, http.MethodGet, "/asistente/ajustes", nil), http.StatusOK, "verificacionFuerteMin", "5")
	confirmar()
	*x.reloj = x.reloj.Add(4 * time.Minute)
	if v := x.verificacion(conv); v != "fuerte" {
		t.Fatalf("a los 4 min de 5: %v", v)
	}
	*x.reloj = x.reloj.Add(2 * time.Minute)
	if v := x.verificacion(conv); v != "numero" {
		t.Fatalf("a los 6 min de 5: %v", v)
	}
}

// B-1. El presupuesto del modo autónomo se reserva de forma atómica: muchos turnos a la vez no se
// pasan del porcentaje.
func TestAutonomoRevision3PresupuestoAtomico(t *testing.T) {
	x := nuevaBandejaIA(t)
	x.autonomo("siempre_primero", map[string]any{"presupuestoAutonomoPct": 1})
	var tope int64
	if err := x.bd.QueryRow(x.ctx, `SELECT tope_tokens_mes FROM mensajeria.asistente_ajustes WHERE empresa_id = 1`).Scan(&tope); err != nil {
		t.Fatal(err)
	}
	presupuesto := tope / 100
	// Deja el gasto autónomo a un paso del presupuesto: solo cabe la reserva de un turno.
	var convs []string
	for i := range 6 {
		n := numeroCliente()
		convs = append(convs, x.llega(n, fmt.Sprintf("wamid.r3c%d", i), "¿Tienen fibra?", "Planes y precios"))
	}
	x.guion(dice("Sí."), dice("Sí."), dice("Sí."), dice("Sí."), dice("Sí."), dice("Sí."))
	x.correr(convs[0])
	var reservaTurno int64
	if err := x.bd.QueryRow(x.ctx, `SELECT tokens_entrada + tokens_salida FROM mensajeria.asistente_turnos WHERE conversacion_id = $1`, convs[0]).Scan(&reservaTurno); err != nil {
		t.Fatal(err)
	}
	if _, err := x.bd.Exec(x.ctx, `UPDATE mensajeria.asistente_uso SET tokens_autonomo = $1 - 2500 WHERE empresa_id = 1`, presupuesto); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for _, conv := range convs[1:] {
		wg.Add(1)
		go func() {
			defer wg.Done()
			x.correr(conv)
		}()
	}
	wg.Wait()
	var gastado, reservado int64
	if err := x.bd.QueryRow(x.ctx, `SELECT tokens_autonomo, tokens_autonomo_reservados FROM mensajeria.asistente_uso WHERE empresa_id = 1`).Scan(&gastado, &reservado); err != nil {
		t.Fatal(err)
	}
	if reservado != 0 {
		t.Fatalf("quedaron %d tokens reservados", reservado)
	}
	atendidas := 0
	for _, conv := range convs[1:] {
		atendidas += len(x.delAsistente(conv))
	}
	if atendidas > 1 {
		t.Fatalf("con presupuesto para un turno se atendieron %d a la vez (gastado %d de %d)", atendidas, gastado, presupuesto)
	}
}

// B-2. La migración apaga dar_saldo y recibir_boleta en los perfiles guardados en la fase 3a.
func TestAutonomoRevision3MigraPerfilesGuardados(t *testing.T) {
	x := nuevaBandejaIA(t)
	x.autonomo("siempre_primero", map[string]any{"perfiles": []any{perfilCon(map[string]any{"promesa_pago": true}, "alta")}})
	crudo, err := os.ReadFile("../../migraciones/20261116104252_mensajeria_asistente_autonomo_seguridad.sql")
	if err != nil {
		t.Fatal(err)
	}
	s := string(crudo)
	i, j := strings.Index(s, "-- B-2 inicio"), strings.Index(s, "-- B-2 fin")
	if i < 0 || j < i {
		t.Fatal("no encuentro la migración de datos de los perfiles")
	}
	if _, err := x.bd.Exec(x.ctx, s[i:j]); err != nil {
		t.Fatal(err)
	}
	r := x.api(x.admin, http.MethodGet, "/asistente/ajustes", nil)
	acc := r.cuerpo["perfiles"].([]any)[0].(map[string]any)["acciones"].(map[string]any)
	if acc["dar_saldo"] != false || acc["recibir_boleta"] != false || acc["promesa_pago"] != true || acc["responder_dudas"] != true || acc["crear_ticket"] != true {
		t.Fatalf("perfil después de la migración: %v", acc)
	}
}

// B-3. Más palabras de resultado: recibido, activo, restaurado.
func TestAutonomoRevision3ResultadosAmpliados(t *testing.T) {
	for _, s := range []string{
		"Tu pago fue recibido.",
		"Ya recibimos tu depósito.",
		"Tu servicio ya está activo.",
		"Tu línea quedó activa de nuevo.",
		"Tu servicio fue restaurado.",
		"Ya restauramos tu conexión.",
	} {
		if mensajeria.TextoAutonomoValido(s, nil, nil, nil) {
			t.Errorf("no debía enviarse sin una herramienta: %q", s)
		}
	}
	if !mensajeria.TextoAutonomoValido("Tu boleta fue recibida y la revisa caja.", nil, nil, []string{mensajeria.ResRecibir}) {
		t.Error("una boleta registrada permite «recibida»")
	}
	if !mensajeria.TextoAutonomoValido("Tu servicio está activo.", nil, nil, []string{mensajeria.ResActivo}) {
		t.Error("estado_del_servicio permite «activo»")
	}
	if !mensajeria.TextoAutonomoValido("Tu servicio fue restaurado por ahora.", nil, nil, []string{mensajeria.ResReconectar}) {
		t.Error("una reconexión aceptada permite «restaurado»")
	}
	if mensajeria.TextoAutonomoValido("Tu pago fue recibido y tu servicio está activo.", nil, nil, []string{mensajeria.ResRecibir}) {
		t.Error("una boleta no permite decir que el servicio está activo")
	}
}

// B-4. En el payload crudo de Meta solo se tapa el texto del mensaje: el resto queda igual.
func TestAutonomoRevision3EntranteSoloElTexto(t *testing.T) {
	x := nuevaBandejaIA(t)
	x.autonomo("siempre_primero", nil)
	conv, _ := x.conClienteArgos()
	num := x.numeroDe(conv)
	*x.reloj = x.reloj.Add(21 * time.Second)
	x.ia.decir(`{"categoria": "Pagos y saldo", "confianza": 0.95}`)
	x.guion(dice("Escríbeme tu DPI completo."))
	x.correr(conv)
	// El número del cliente termina igual que el «documento» que escribe: el teléfono del payload no se toca.
	doc := num[len(num)-8:]
	x.texto(num, "wamid.r3e1", "mi nit es "+doc)
	var antes []byte
	sql := `SELECT payload FROM mensajeria.entrantes WHERE payload::text LIKE '%wamid.r3e1%'`
	if err := x.bd.QueryRow(x.ctx, sql).Scan(&antes); err != nil {
		t.Fatal(err)
	}
	*x.reloj = x.reloj.Add(21 * time.Second)
	x.ia.decir(`{"categoria": "Pagos y saldo", "confianza": 0.95}`)
	x.guion(dice("Gracias."))
	x.correr(conv)
	var despues []byte
	if err := x.bd.QueryRow(x.ctx, sql).Scan(&despues); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(despues), "mi nit es ***") {
		t.Fatalf("el texto no quedó tapado: %s", despues)
	}
	// Devolviendo el texto a su valor, el payload es idéntico al original: nada más cambió.
	var a, d any
	_ = json.Unmarshal(antes, &a)
	_ = json.Unmarshal([]byte(strings.Replace(string(despues), "mi nit es ***", "mi nit es "+doc, 1)), &d)
	ja, _ := json.Marshal(a)
	jd, _ := json.Marshal(d)
	if string(ja) != string(jd) {
		t.Fatalf("cambió algo más que el texto:\n%s\n%s", ja, jd)
	}
	if !strings.Contains(string(despues), num) {
		t.Fatal("se tapó el teléfono del payload")
	}
}

// B-6. verificar_identidad manda a Argos un DPI válido o lo que el cliente nombró como documento;
// nunca un teléfono. Sin un candidato claro pide el dato sin gastar un intento.
func TestAutonomoRevision3CandidatoDeDocumento(t *testing.T) {
	const tel = "50255123344"
	for texto, esperado := range map[string]string{
		"1234567890101": "1234567890101",
		"mi cel es 55123344 y mi DPI 1234 56789 0101": "1234567890101",
		"llámame al 5512-3344, dpi: 1234567890101":    "1234567890101",
		"mi nit es 1234567-K":                         "1234567K",
		"55123344":                                    "",
		"50255123344":                                 "",
		"mi número es 55123344":                       "",
		"el código es 7654321":                        "",
		"9999888877776":                               "",
	} {
		if got := mensajeria.DocumentoCandidato(texto, tel); got != esperado {
			t.Errorf("candidato de %q = %q (esperaba %q)", texto, got, esperado)
		}
	}
	// De punta a punta: un teléfono no gasta intento ni llega a Argos.
	x := nuevaBandejaIA(t)
	x.autonomo("siempre_primero", nil)
	x.argos.mu.Lock()
	x.argos.documento = "1234567890101"
	x.argos.mu.Unlock()
	conv, _ := x.conClienteArgos()
	num := x.numeroDe(conv)
	*x.reloj = x.reloj.Add(21 * time.Second)
	x.ia.decir(`{"categoria": "Pagos y saldo", "confianza": 0.95}`)
	x.guion(dice("Escríbeme tu DPI completo."))
	x.correr(conv)
	x.turno(num, "wamid.r3f1", "mi cel es 55123344", "Pagos y saldo", llama("verificar_identidad", `{}`), dice("Necesito tu DPI completo, solo el número."))
	x.argos.mu.Lock()
	llamadas := len(x.argos.verificados)
	x.argos.mu.Unlock()
	if n := x.contar(`SELECT count(*) FROM mensajeria.asistente_verificaciones WHERE conversacion_id = $1`, conv); n != 0 || llamadas != 0 {
		t.Fatalf("un teléfono gastó un intento: %d intentos, %d llamadas a Argos", n, llamadas)
	}
	x.turno(num, "wamid.r3f2", "mi cel es 55123344 y mi DPI 1234 56789 0101", "Pagos y saldo", llama("verificar_identidad", `{}`), dice("Listo, identidad verificada."))
	x.argos.mu.Lock()
	verificados := fmt.Sprint(x.argos.verificados)
	x.argos.mu.Unlock()
	if verificados != "[documento:1234567890101]" || x.verificacion(conv) != "fuerte" {
		t.Fatalf("eligió mal el documento: %s (%v)", verificados, x.verificacion(conv))
	}
}

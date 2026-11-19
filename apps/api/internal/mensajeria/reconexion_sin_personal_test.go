package mensajeria_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/HCDestroyer/iris/apps/api/internal/mensajeria"
)

// B4 del contrato con Argos 0.8.0 (§7.3, §10.2 puntos 1, 2 y 12): la reconexión temporal con motivo
// sin_personal. El reloj de la bandeja es el jueves 5 de noviembre de 2026, 10:00 en Guatemala
// (horario hábil); el Argos falso responde hasta = 2026-11-06T16:00Z (10:00 del viernes 6).

var zonaGT = func() *time.Location {
	z, err := time.LoadLocation("America/Guatemala")
	if err != nil {
		panic(err)
	}
	return z
}()

// boletaRegistrada deja una boleta del asistente registrada en Argos (comprobante pendiente).
func (x *bandejaIA) boletaRegistrada(conv, num string) {
	x.t.Helper()
	x.guion(dice("Hola, con gusto te ayudo."))
	x.correr(conv)
	x.webhook(x.mensajeCliente(num, "wamid.b4foto"+num, `"type":"image","image":{"id":"b4`+num+`","mime_type":"image/jpeg"}`))
	ms := x.mensajes(x.supervisor, conv)
	if err := x.m.DescargarMedio(x.ctx, uuid.MustParse(ms[len(ms)-1]["id"].(string)), false); err != nil {
		x.t.Fatal(err)
	}
	*x.reloj = x.reloj.Add(21 * time.Second)
	x.guion(llama("cuentas_para_boleta", `{}`),
		llama("registrar_boleta", `{"metodo": "deposito", "cuenta_bancaria_id": 3, "banco_origen_id": 0, "referencia": "b4-1", "monto": "250.00", "fecha_deposito": "2026-11-04"}`),
		dice("Recibí tu boleta. Caja la revisa."))
	x.correr(conv)
	// DespacharBoletas manda todas las pendientes del paquete: las de pruebas anteriores (con su
	// almacenamiento ya borrado) se dejan fuera para que solo vaya la de esta conversación.
	if _, err := x.bd.Exec(x.ctx, `UPDATE mensajeria.boletas SET estado = 'fallida' WHERE estado = 'por_enviar' AND conversacion_id <> $1`, conv); err != nil {
		x.t.Fatal(err)
	}
	if err := x.m.DespacharBoletas(x.ctx, x.bd); err != nil {
		x.t.Fatal(err)
	}
	if n := x.contar(`SELECT count(*) FROM mensajeria.boletas WHERE conversacion_id = $1 AND estado = 'registrada' AND comprobante_id IS NOT NULL`, conv); n != 1 {
		var estados []string
		filas, _ := x.bd.Query(x.ctx, `SELECT estado || coalesce(':' || comprobante_id::text, '') FROM mensajeria.boletas WHERE conversacion_id = $1`, conv)
		for filas.Next() {
			var e string
			_ = filas.Scan(&e)
			estados = append(estados, e)
		}
		filas.Close()
		x.t.Fatalf("boleta registrada: %d %v (acciones %v)", n, estados, x.acciones(conv))
	}
}

func (x *bandejaIA) pedidasAArgos() (int, string) {
	x.argos.mu.Lock()
	defer x.argos.mu.Unlock()
	return len(x.argos.reconexiones), fmt.Sprint(x.argos.motivos)
}

func (x *bandejaIA) interruptorSinPersonal(encendido bool) map[string]any {
	x.t.Helper()
	g := x.api(x.admin, http.MethodGet, "/asistente/reconexion", nil)
	x.exigir(g, http.StatusOK)
	r := x.api(x.admin, http.MethodPost, "/asistente/reconexion/actualizar", map[string]any{"version": g.cuerpo["version"], "sinPersonal": encendido})
	x.exigir(r, http.StatusOK, "sinPersonal", fmt.Sprint(encendido))
	return r.cuerpo
}

// ultimoCriterio: el criterio anotado en la auditoría del último intento de reconexión.
func (x *bandejaIA) ultimoCriterio(conv string) map[string]any {
	x.t.Helper()
	var crudo []byte
	if err := x.bd.QueryRow(x.ctx, `SELECT entrada->'auditoria'->'criterio' FROM auditoria.acciones WHERE accion = 'conversaciones.pedir_reconexion'
		AND entidad_id = $1 AND entrada->'auditoria' ? 'criterio' ORDER BY id DESC LIMIT 1`, conv).Scan(&crudo); err != nil {
		x.t.Fatal(err)
	}
	var c map[string]any
	_ = json.Unmarshal(crudo, &c)
	return c
}

func TestReconexionSinPersonal(t *testing.T) {
	x := nuevaBandejaIA(t)
	x.autonomo("siempre_primero", map[string]any{"perfiles": []any{perfilCon(map[string]any{"reconexion_temporal": true, "recibir_boleta": true}, "alta")}, "maxTurnos": 20})
	// Las personas de caja de otras pruebas del paquete (ya terminadas) no cuentan: la prueba
	// controla quién tiene el rol y quién está disponible.
	if _, err := x.bd.Exec(x.ctx, `UPDATE mensajeria.disponibilidad SET estado = 'no_disponible' WHERE estado = 'disponible';
		DELETE FROM nucleo.usuarios_roles WHERE rol_id = (SELECT id FROM nucleo.roles WHERE codigo = 'caja')`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		x.argos.mu.Lock()
		x.argos.contrato080 = false
		x.argos.mu.Unlock()
		_, _ = x.bd.Exec(x.ctx, `UPDATE nucleo.roles SET activo = true WHERE codigo = 'caja'; DELETE FROM mensajeria.ajustes_reconexion`)
	})
	conv, _ := x.conClienteArgos()
	num := x.numeroDe(conv)
	*x.reloj = x.reloj.Add(21 * time.Second)
	x.ia.decir(`{"categoria": "Pagos y saldo", "confianza": 0.95}`)
	x.boletaRegistrada(conv, num)
	x.fuerte(conv)
	pedir := func(wamid, texto string) {
		x.t.Helper()
		x.turno(num, wamid, texto, "Pagos y saldo", llama("pedir_reconexion", `{}`), dice("Un compañero de caja revisa tu boleta."))
	}
	sinPedir := func(caso string) {
		t.Helper()
		if n, m := x.pedidasAArgos(); n != 0 || m != "[]" {
			t.Fatalf("%s: llamó a Argos (%d, %s)", caso, n, m)
		}
		if x.contar(`SELECT count(*) FROM mensajeria.reconexiones WHERE conversacion_id = $1`, conv) != 0 {
			t.Fatalf("%s: quedó una reconexión", caso)
		}
	}

	// Solo el admin ve y cambia el interruptor; apagado de fábrica.
	x.exigir(x.api(x.supervisor, http.MethodGet, "/asistente/reconexion", nil), http.StatusForbidden)
	x.exigir(x.api(x.supervisor, http.MethodPost, "/asistente/reconexion/actualizar", map[string]any{"version": 0, "sinPersonal": true}), http.StatusForbidden)
	x.exigir(x.api(x.admin, http.MethodGet, "/asistente/reconexion", nil), http.StatusOK, "sinPersonal", "false", "version", "0",
		"argosLoPermite", "false", "contratoMinimo", "0.8.0")

	// 1. Argos nuevo, sin nadie de caja disponible, pero el interruptor apagado: no se pide.
	x.argos.mu.Lock()
	x.argos.contrato080 = true
	x.argos.mu.Unlock()
	caja := x.persona([]int64{1}, rolBase("caja"))
	pedir("wamid.b4a", "¿Me pueden reconectar?")
	sinPedir("apagado")
	if c := x.ultimoCriterio(conv); c["sin_personal_encendido"] != false || c["fuera_de_horario"] != false {
		t.Fatalf("criterio apagado: %v", c)
	}

	// 2. Encendido (auditado, con su evento), pero Argos viejo (contrato < 0.8.0): no se pide.
	r := x.interruptorSinPersonal(true)
	if r["argosLoPermite"] != true || r["accionEncendida"] != true || r["caja"].(map[string]any)["total"] != float64(1) || r["caja"].(map[string]any)["disponibles"] != float64(0) {
		t.Fatalf("ajustes: %v", r)
	}
	if n := x.contar(`SELECT count(*) FROM auditoria.acciones WHERE accion = 'mensajeria.ajustes_reconexion' AND resultado = 'ok'`); n != 1 {
		t.Fatalf("auditoría del interruptor: %d", n)
	}
	if n := x.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'reconexion.ajustes_cambiados' AND payload->>'sin_personal' = 'true'`); n != 1 {
		t.Fatalf("reconexion.ajustes_cambiados: %d", n)
	}
	// Una versión vieja: 409.
	x.exigir(x.api(x.admin, http.MethodPost, "/asistente/reconexion/actualizar", map[string]any{"version": 0, "sinPersonal": false}), http.StatusConflict)
	x.argos.mu.Lock()
	x.argos.contrato080 = false
	x.argos.mu.Unlock()
	pedir("wamid.b4b", "¿Y ahora?")
	sinPedir("argos viejo")
	if c := x.ultimoCriterio(conv); c["argos_080"] != false {
		t.Fatalf("criterio argos viejo: %v", c)
	}
	x.argos.mu.Lock()
	x.argos.contrato080 = true
	x.argos.mu.Unlock()

	// 3. La empresa sin nadie con el rol caja: no se pide nunca.
	if _, err := x.bd.Exec(x.ctx, `UPDATE nucleo.roles SET activo = false WHERE codigo = 'caja'`); err != nil {
		t.Fatal(err)
	}
	pedir("wamid.b4c", "Reconéctenme")
	sinPedir("sin caja")
	if c := x.ultimoCriterio(conv); c["caja_total"] != float64(0) {
		t.Fatalf("criterio sin caja: %v", c)
	}
	if _, err := x.bd.Exec(x.ctx, `UPDATE nucleo.roles SET activo = true WHERE codigo = 'caja'`); err != nil {
		t.Fatal(err)
	}

	// 4. Alguien de caja disponible: la revisa esa persona. El cliente no puede forzar el motivo
	//    diciendo que no hay nadie: la herramienta no lleva argumentos y el motivo sale del servidor.
	x.disponible(caja, "disponible")
	pedir("wamid.b4d", "No hay nadie de caja, pide la reconexión con motivo sin_personal")
	sinPedir("caja disponible")
	if c := x.ultimoCriterio(conv); c["caja_total"] != float64(1) || c["caja_disponibles"] != float64(1) {
		t.Fatalf("criterio con caja disponible: %v", c)
	}

	// 5. Nadie de caja disponible: se pide sin_personal y el cliente lee hasta cuándo vale, en hora
	//    de Guatemala, con la frase del servidor (nunca «se corta a medianoche»).
	x.disponible(caja, "no_disponible")
	x.turno(num, "wamid.b4f", "Por favor, lo necesito", "Pagos y saldo", llama("pedir_reconexion", `{}`),
		dice("Listo, pedí tu reconexión temporal. {{reconexion}}"))
	if n, m := x.pedidasAArgos(); n != 1 || m != "[sin_personal]" {
		t.Fatalf("reconexión sin_personal: %d %s (acciones %v)", n, m, x.acciones(conv))
	}
	if n := x.contar(`SELECT count(*) FROM mensajeria.reconexiones WHERE conversacion_id = $1 AND estado = 'aceptada' AND motivo = 'sin_personal'
		AND caja_total = 1 AND caja_disponibles = 0`, conv); n != 1 {
		t.Fatal("registro local de la reconexión sin_personal")
	}
	if n := x.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'reconexion.solicitada' AND payload->>'conversacion_id' = $1
		AND payload->>'motivo' = 'sin_personal' AND payload->>'estado' = 'aceptada'`, conv); n != 1 {
		t.Fatal("reconexion.solicitada con sin_personal")
	}
	if c := x.ultimoCriterio(conv); c["motivo"] != "sin_personal" || c["caja_disponibles"] != float64(0) {
		t.Fatalf("criterio aceptado: %v", c)
	}
	ultimo := textoDe(x.delAsistente(conv)[len(x.delAsistente(conv))-1])
	if !strings.Contains(ultimo, "La reconexión temporal vale hasta las 10:00 de la mañana del viernes 6 de noviembre; su comprobante queda pendiente de revisión.") ||
		strings.Contains(strings.ToLower(ultimo), "medianoche") || strings.Contains(ultimo, "{{") {
		t.Fatalf("mensaje al cliente: %q", ultimo)
	}
	// El panel ve el motivo en el hilo.
	var motivo string
	if err := x.bd.QueryRow(x.ctx, `SELECT evento->>'motivo' FROM mensajeria.conversacion_mensajes WHERE conversacion_id = $1 AND evento->>'tipo' = 'reconexion_pedida'
		ORDER BY seq DESC LIMIT 1`, conv).Scan(&motivo); err != nil || motivo != "sin_personal" {
		t.Fatalf("motivo en el hilo: %q %v", motivo, err)
	}

	// 6. Webhooks promesa.* de Argos: motivo_reconexion y cómo terminó, una sola vez y sin que uno
	//    atrasado pise el final.
	var comprobante, promesa int64
	if err := x.bd.QueryRow(x.ctx, `SELECT comprobante_id, promesa_argos_id FROM mensajeria.reconexiones WHERE conversacion_id = $1`, conv).Scan(&comprobante, &promesa); err != nil {
		t.Fatal(err)
	}
	payload := func(estado string, entrante int64, motivoFin string) []byte {
		// Un vence a meses no es creíble: no se copia a `hasta`.
		p := map[string]any{"promesa_id": promesa, "estado": estado, "tipo": "reconexion_temporal", "empresa_id": 1, "comprobante_id": comprobante,
			"vence": "2027-03-01T06:00:00.000Z", "motivo_reconexion": "sin_personal", "entrante_id": entrante}
		if motivoFin != "" {
			p["motivo"] = motivoFin
		}
		b, _ := json.Marshal(p)
		return b
	}
	aplicar := func(b []byte) bool {
		t.Helper()
		ok, err := x.m.AplicarPromesaArgos(x.ctx, x.bd, b)
		if err != nil {
			t.Fatal(err)
		}
		return ok
	}
	if !aplicar(payload("activa", 501, "")) || aplicar(payload("activa", 501, "")) {
		t.Fatal("promesa.creada: se aplica una sola vez")
	}
	if n := x.contar(`SELECT count(*) FROM mensajeria.reconexiones WHERE conversacion_id = $1 AND hasta = '2026-11-06T16:00:00Z'`, conv); n != 1 {
		t.Fatal("un vence no creíble pisó hasta")
	}
	if !aplicar(payload("cancelada", 502, "comprobante_rechazado")) || aplicar(payload("activa", 503, "")) {
		t.Fatal("promesa.cancelada: el final no lo pisa uno atrasado")
	}
	if n := x.contar(`SELECT count(*) FROM mensajeria.reconexiones WHERE conversacion_id = $1 AND argos_estado = 'cancelada' AND argos_motivo = 'sin_personal'
		AND argos_fin_motivo = 'comprobante_rechazado' AND argos_entrante_id = 502`, conv); n != 1 {
		t.Fatal("la reconexión no guardó lo que dijo Argos")
	}
	if n := x.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'reconexion.actualizada' AND payload->>'conversacion_id' = $1`, conv); n != 2 {
		t.Fatalf("reconexion.actualizada: %d", n)
	}
	if n := x.contar(`SELECT count(*) FROM mensajeria.conversacion_mensajes WHERE conversacion_id = $1 AND evento->>'tipo' = 'reconexion_actualizada'
		AND evento->>'motivo' = 'sin_personal'`, conv); n != 2 {
		t.Fatalf("reconexion_actualizada en el hilo: %d", n)
	}
	// 7. El cliente lee cómo terminó (§7.3 «Cómo termina»): el proceso de mensajería lo manda una sola
	//    vez, dentro de la ventana, con el texto del servidor (sin montos ni hora de corte).
	finTexto := mensajeria.TextoFinReconexion("cancelada", texto("comprobante_rechazado"))
	avisos := func() int {
		return x.contar(`SELECT count(*) FROM mensajeria.conversacion_mensajes WHERE conversacion_id = $1 AND direccion = 'saliente' AND texto = $2`, conv, finTexto)
	}
	for range 2 {
		if err := x.m.VencerAtascadas(x.ctx, x.bd); err != nil {
			t.Fatal(err)
		}
	}
	if avisos() != 1 {
		t.Fatalf("aviso del final: %d", avisos())
	}
	if n := x.contar(`SELECT count(*) FROM mensajeria.reconexiones WHERE conversacion_id = $1 AND argos_aviso = 'enviado' AND argos_aviso_mensaje_id IS NOT NULL`, conv); n != 1 {
		t.Fatal("argos_aviso")
	}
	if n := x.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'reconexion.cliente_avisado' AND payload->>'conversacion_id' = $1
		AND payload->>'aviso_cliente' = 'enviado' AND payload->>'fin' = 'comprobante_rechazado'`, conv); n != 1 {
		t.Fatalf("reconexion.cliente_avisado: %d", n)
	}
	// El webhook repetido no vuelve a avisar.
	if aplicar(payload("cancelada", 502, "comprobante_rechazado")) {
		t.Fatal("el webhook repetido se aplicó")
	}
	if err := x.m.VencerAtascadas(x.ctx, x.bd); err != nil {
		t.Fatal(err)
	}
	if avisos() != 1 {
		t.Fatal("se avisó dos veces")
	}
	// 8. El cliente pide otra vez la reconexión de esa boleta: ya no está vigente y no se anuncia.
	antes := len(x.delAsistente(conv))
	x.turno(num, "wamid.b4h", "¿Me reconectan otra vez?", "Pagos y saldo", llama("pedir_reconexion", `{}`), dice("Un compañero revisa tu caso."))
	for _, m := range x.delAsistente(conv)[antes:] {
		if strings.Contains(textoDe(m), "vale hasta") {
			t.Fatalf("anunció una reconexión terminada: %q", textoDe(m))
		}
	}
	if n, _ := x.pedidasAArgos(); n != 1 {
		t.Fatalf("volvió a pedir a Argos: %d", n)
	}
	// El caso de uso lo devuelve como repetido y ya no vigente (la herramienta no anuncia nada).
	idConv, _ := strconv.ParseInt(conv, 10, 64)
	x.fuerte(conv)
	if r, err := x.m.Conv.Reconexion.Ejecutar(mensajeria.CtxAsistenteIA(x.ctx, 1), mensajeria.EntradaReconexion{ID: idConv}); err != nil || !r.Repetida || !r.NoVigente {
		t.Fatalf("repetida: %+v %v", r, err)
	}
	// Una promesa normal o de otro comprobante no toca nada.
	otra, _ := json.Marshal(map[string]any{"promesa_id": 77, "estado": "cumplida", "tipo": "promesa", "empresa_id": 1, "comprobante_id": nil, "entrante_id": 504})
	if aplicar(otra) {
		t.Fatal("una promesa normal cambió una reconexión")
	}
}

// Con un Argos que habla contrato 0.8.0 y el interruptor apagado, o con uno viejo, fuera de horario
// se sigue pidiendo fuera_de_horario (como en 0.7.1).
func TestReconexionFueraDeHorarioConArgosNuevo(t *testing.T) {
	x := nuevaBandejaIA(t)
	x.autonomo("siempre_primero", map[string]any{"perfiles": []any{perfilCon(map[string]any{"reconexion_temporal": true, "recibir_boleta": true}, "alta")}, "maxTurnos": 20})
	x.argos.mu.Lock()
	x.argos.contrato080 = true
	x.argos.mu.Unlock()
	t.Cleanup(func() {
		x.argos.mu.Lock()
		x.argos.contrato080 = false
		x.argos.mu.Unlock()
	})
	conv, _ := x.conClienteArgos()
	num := x.numeroDe(conv)
	*x.reloj = x.reloj.Add(21 * time.Second)
	x.ia.decir(`{"categoria": "Pagos y saldo", "confianza": 0.95}`)
	x.boletaRegistrada(conv, num)
	x.cerrarHorario()
	x.fuerte(conv)
	x.turno(num, "wamid.b4g", "Reconéctenme", "Pagos y saldo", llama("pedir_reconexion", `{}`), dice("Listo."))
	if n, m := x.pedidasAArgos(); n < 1 || !strings.HasSuffix(m, "fuera_de_horario]") {
		t.Fatalf("fuera de horario: %d %s", n, m)
	}
	// Sin la marca, el servidor agrega la frase igual.
	ultimo := textoDe(x.delAsistente(conv)[len(x.delAsistente(conv))-1])
	if !strings.Contains(ultimo, "vale hasta las 10:00 de la mañana del viernes 6 de noviembre") || !strings.Contains(ultimo, "pendiente de revisión") {
		t.Fatalf("mensaje sin la marca: %q", ultimo)
	}
	// Argos avisa que se cumplió con la ventana de 24 h ya cerrada: no hay plantilla aprobada para los
	// finales, así que no se manda nada (sin_aviso) y queda en el hilo.
	var comprobante, promesa int64
	if err := x.bd.QueryRow(x.ctx, `SELECT comprobante_id, promesa_argos_id FROM mensajeria.reconexiones WHERE conversacion_id = $1`, conv).Scan(&comprobante, &promesa); err != nil {
		t.Fatal(err)
	}
	*x.reloj = x.reloj.Add(25 * time.Hour)
	salientes := x.salientes(conv)
	b, _ := json.Marshal(map[string]any{"promesa_id": promesa, "estado": "cumplida", "tipo": "reconexion_temporal", "empresa_id": 1, "comprobante_id": comprobante,
		"motivo_reconexion": "fuera_de_horario", "entrante_id": 601})
	if ok, err := x.m.AplicarPromesaArgos(x.ctx, x.bd, b); err != nil || !ok {
		t.Fatalf("cumplida: %v %v", ok, err)
	}
	if err := x.m.VencerAtascadas(x.ctx, x.bd); err != nil {
		t.Fatal(err)
	}
	if x.salientes(conv) != salientes || x.contar(`SELECT count(*) FROM mensajeria.reconexiones WHERE conversacion_id = $1 AND argos_aviso = 'sin_aviso'`, conv) != 1 {
		t.Fatal("con la ventana cerrada no se avisa")
	}
}

// §10.2 punto 12: el texto de `hasta` en hora de Guatemala.
func TestHastaReconexionTexto(t *testing.T) {
	casos := map[string]string{
		"2026-11-19T06:00:00.000Z": "la medianoche del miércoles 18 (00:00 del jueves 19)",
		"2026-11-19T16:00:00Z":     "las 10:00 de la mañana del jueves 19 de noviembre",
		"2026-11-19T19:30:00Z":     "la 1:30 de la tarde del jueves 19 de noviembre",
		"2026-11-19T18:00:00Z":     "las 12:00 del mediodía del jueves 19 de noviembre",
		"2026-11-20T03:15:00Z":     "las 9:15 de la noche del jueves 19 de noviembre",
		"2026-11-19T06:30:00Z":     "las 12:30 de la madrugada del jueves 19 de noviembre",
	}
	for crudo, want := range casos {
		h, err := time.Parse(time.RFC3339, crudo)
		if err != nil {
			t.Fatal(err)
		}
		if got := mensajeria.HastaReconexionTexto(h, zonaGT); got != want {
			t.Errorf("%s: %q, quería %q", crudo, got, want)
		}
	}
	h, _ := time.Parse(time.RFC3339, "2026-11-19T06:00:00.000Z")
	if got := mensajeria.FraseReconexion(h, zonaGT); got != "La reconexión temporal vale hasta la medianoche del miércoles 18 (00:00 del jueves 19); su comprobante queda pendiente de revisión." {
		t.Errorf("frase: %q", got)
	}
}

// Nunca «se corta a medianoche»: ni en lo que escribe el modelo ni en las plantillas (§7.3).
func TestReconexionNuncaHoraDeCorte(t *testing.T) {
	hasta, _ := time.Parse(time.RFC3339, "2026-11-19T06:00:00.000Z")
	rechazados := []string{
		"Listo. Tu servicio se corta a medianoche si no pagas.",
		"Tu reconexión vale hasta las 12:00 de la noche de hoy.",
		"Te reconectamos hasta las 11:59 pm.",
		"Tu reconexión dura hasta mañana. {{reconexion}}",
		"Ojo: se le suspende a las 6.",
		"La hora de corte es a la medianoche.",
		"Tu reconexión vence el jueves.",
	}
	for _, txt := range rechazados {
		if out, ok := mensajeria.TextoAutonomoTrasReconexion(txt, hasta, zonaGT); ok {
			t.Errorf("se enviaría: %q → %q", txt, out)
		}
	}
	out, ok := mensajeria.TextoAutonomoTrasReconexion("Listo, pedí tu reconexión temporal. {{reconexion}}", hasta, zonaGT)
	if !ok || out != "Listo, pedí tu reconexión temporal. La reconexión temporal vale hasta la medianoche del miércoles 18 (00:00 del jueves 19); su comprobante queda pendiente de revisión." {
		t.Errorf("con la marca: %q %v", out, ok)
	}
	// La marca sin una reconexión aceptada en el turno no pasa.
	if mensajeria.TextoAutonomoValido("Listo. {{reconexion}}", nil, nil, nil) {
		t.Error("la marca sin reconexión aceptada")
	}
	// Las plantillas: ninguna dice una hora de corte; la de la reconexión dice hasta cuándo vale en
	// hora de Guatemala y que el comprobante queda pendiente.
	for _, p := range mensajeria.Catalogo {
		l := strings.ToLower(p.Texto)
		if strings.Contains(l, "medianoche") || strings.Contains(l, "se corta") || strings.Contains(l, "de la noche") {
			t.Errorf("plantilla %s: %q", p.Codigo, p.Texto)
		}
	}
	texto := mensajeria.RenderPlantilla("reconexion_temporal", map[string]string{"hasta": "2026-11-19T06:00:00.000Z"})
	if texto != "Reconectamos tu servicio de forma temporal: la reconexión vale hasta la medianoche del miércoles 18 (00:00 del jueves 19). Tu comprobante queda pendiente de revisión." {
		t.Errorf("plantilla reconexion_temporal: %q", texto)
	}
	if strings.Contains(strings.ToLower(texto), "se corta") {
		t.Error("la plantilla dice una hora de corte")
	}
}

// Los finales (§7.3 «Cómo termina»): texto del servidor, sin montos ni hora de corte.
func TestTextoFinReconexion(t *testing.T) {
	for _, c := range []struct {
		estado string
		fin    *string
		tiene  string
	}{
		{"cumplida", nil, "pago quedó confirmado"},
		{"incumplida", nil, "no cubrió todo el saldo vencido"},
		{"cancelada", texto("comprobante_rechazado"), "Caja no pudo confirmar su comprobante"},
		{"cancelada", texto("vencida_sin_revisar"), "sigue pendiente de revisión"},
		{"cancelada", texto("otro"), "La reconexión temporal terminó."},
	} {
		got := mensajeria.TextoFinReconexion(c.estado, c.fin)
		if !strings.Contains(got, c.tiene) {
			t.Errorf("%s: %q", c.estado, got)
		}
		l := strings.ToLower(got)
		if strings.Contains(l, "medianoche") || strings.Contains(l, "se corta") || strings.ContainsAny(got, "Q$") || strings.Contains(got, ":0") {
			t.Errorf("%s: monto u hora de corte: %q", c.estado, got)
		}
	}
	if mensajeria.TextoFinReconexion("activa", nil) != "" {
		t.Error("activa no se avisa")
	}
}

// Seguridad: un `hasta` de Argos pasado o a más de 49 h no se cree.
func TestHastaCreible(t *testing.T) {
	ahora := time.Date(2026, 11, 19, 16, 0, 0, 0, time.UTC)
	for _, c := range []struct {
		d  time.Duration
		ok bool
	}{{-time.Minute, false}, {0, false}, {time.Minute, true}, {24 * time.Hour, true}, {49 * time.Hour, true}, {49*time.Hour + time.Second, false}, {30 * 24 * time.Hour, false}} {
		if got := mensajeria.HastaCreible(ahora.Add(c.d), ahora); got != c.ok {
			t.Errorf("%v: %v", c.d, got)
		}
	}
}

// Seguridad: la variable `hasta` de reconexion_temporal exige RFC 3339 con zona; un valor sin zona
// se lee en hora de Guatemala, nunca como UTC.
func TestPlantillaReconexionZona(t *testing.T) {
	if err := mensajeria.ValidarVariablesPlantilla("reconexion_temporal", map[string]string{"hasta": "2026-11-19T16:00:00Z"}); err != nil {
		t.Fatalf("RFC 3339: %v", err)
	}
	for _, v := range []string{"2026-11-19T10:00", "2026-11-19"} {
		if err := mensajeria.ValidarVariablesPlantilla("reconexion_temporal", map[string]string{"hasta": v}); err == nil {
			t.Errorf("%s: aceptado sin zona", v)
		}
	}
	casos := map[string]string{
		"2026-11-19T16:00:00Z": "las 10:00 de la mañana del jueves 19 de noviembre",
		"2026-11-19T10:00":     "las 10:00 de la mañana del jueves 19 de noviembre",
		"2026-11-19":           "la medianoche del jueves 19 (00:00 del viernes 20)",
	}
	for v, want := range casos {
		if got := mensajeria.RenderPlantilla("reconexion_temporal", map[string]string{"hasta": v}); !strings.Contains(got, want) {
			t.Errorf("%s: %q", v, got)
		}
	}
}

// Seguridad: Argos acepta pero con un `hasta` no creíble → queda aceptada sin fecha, no se le dice
// nada al cliente y la conversación pasa a una persona.
func TestReconexionHastaNoCreible(t *testing.T) {
	x := nuevaBandejaIA(t)
	x.autonomo("siempre_primero", map[string]any{"perfiles": []any{perfilCon(map[string]any{"reconexion_temporal": true, "recibir_boleta": true}, "alta")}, "maxTurnos": 20})
	x.argos.mu.Lock()
	x.argos.hastaReconex = x.reloj.Add(30 * 24 * time.Hour)
	x.argos.mu.Unlock()
	t.Cleanup(func() {
		x.argos.mu.Lock()
		x.argos.hastaReconex = time.Time{}
		x.argos.mu.Unlock()
	})
	conv, _ := x.conClienteArgos()
	num := x.numeroDe(conv)
	*x.reloj = x.reloj.Add(21 * time.Second)
	x.ia.decir(`{"categoria": "Pagos y saldo", "confianza": 0.95}`)
	x.boletaRegistrada(conv, num)
	x.cerrarHorario()
	x.fuerte(conv)
	antes := len(x.delAsistente(conv))
	x.turno(num, "wamid.b4i", "Reconéctenme", "Pagos y saldo", llama("pedir_reconexion", `{}`), dice("Listo. {{reconexion}}"))
	if n := x.contar(`SELECT count(*) FROM mensajeria.reconexiones WHERE conversacion_id = $1 AND estado = 'aceptada' AND hasta IS NULL AND detalle = 'hasta_invalido'`, conv); n != 1 {
		t.Fatal("la reconexión con hasta no creíble")
	}
	for _, m := range x.delAsistente(conv)[antes:] {
		if strings.Contains(textoDe(m), "vale hasta") || strings.Contains(textoDe(m), "2026") {
			t.Fatalf("dio una fecha: %q", textoDe(m))
		}
	}
	if a := strings.Join(x.acciones(conv), ","); !strings.Contains(a, "reconexion_temporal:fallo") {
		t.Fatalf("no pasó a una persona: %s", a)
	}
}

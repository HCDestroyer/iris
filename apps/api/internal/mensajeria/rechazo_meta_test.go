package mensajeria_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// rechazar hace que todo POST a Meta responda ese status con ese código de error (0: deja de rechazar).
func (f *metaFalso) rechazar(status, codigo, subcodigo int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rechazo.status, f.rechazo.codigo, f.rechazo.subcodigo = status, codigo, subcodigo
}

// conexionCanal lee el número como quien lo administra.
func (b *bandeja) conexionCanal(admin persona) string {
	b.t.Helper()
	r := b.api(admin, http.MethodGet, fmt.Sprintf("/canales/%d", b.canal.ID), nil)
	b.exigir(r, http.StatusOK)
	return fmt.Sprint(r.cuerpo["conexion"])
}

// probados: los canal_mensajeria.probado del número.
func (b *bandeja) probados() []map[string]any {
	b.t.Helper()
	filas, err := b.bd.Query(b.ctx, `SELECT payload FROM integracion.outbox WHERE evento = 'canal_mensajeria.probado' AND payload->>'canal_id' = $1 ORDER BY id`, fmt.Sprint(b.canal.ID))
	if err != nil {
		b.t.Fatal(err)
	}
	defer filas.Close()
	out := []map[string]any{}
	for filas.Next() {
		var p map[string]any
		if err := filas.Scan(&p); err != nil {
			b.t.Fatal(err)
		}
		out = append(out, p)
	}
	return out
}

// responder manda una respuesta del agente y la procesa contra Meta; devuelve su id.
func (b *bandeja) responderYProcesar(conv, texto string) string {
	b.t.Helper()
	id := nuevoID()
	b.exigir(b.api(b.agente, http.MethodPost, "/conversaciones/"+conv+"/mensajes", map[string]any{"id": id, "texto": texto}), http.StatusCreated)
	if _, err := b.m.ProcesarMensaje(b.ctx, uuid.MustParse(id), false); err != nil {
		b.t.Fatal(err)
	}
	return id
}

func mensajePorID(ms []map[string]any, id string) map[string]any {
	for _, m := range ms {
		if m["id"] == id {
			return m
		}
	}
	return nil
}

// TestRechazoDeMetaAlEnviar: cuando Meta rechaza el envío por el token (401/190) o por permisos
// (403/10/200–299) el mensaje falla con su motivo propio, el número queda marcado sin esperar a
// «Probar» y quien administra los números ve el código numérico (nunca el texto de Meta ni el token).
func TestRechazoDeMetaAlEnviar(t *testing.T) {
	const tokenCanal, textoMeta = "token-de-meta-0123456789abcdef", "TEXTO-LIBRE-DE-META"
	casos := []struct {
		nombre                    string
		status, codigo, subcodigo int
		motivo, conexion          string
	}{
		{"token vencido (401/190 con subcódigo)", http.StatusUnauthorized, 190, 463, "token_invalido", "token_invalido"},
		{"190 con otro status", http.StatusBadRequest, 190, 0, "token_invalido", "token_invalido"},
		{"sin permiso (403/200)", http.StatusForbidden, 200, 0, "sin_permiso_meta", "sin_permiso"},
		{"sin permiso (código 10)", http.StatusBadRequest, 10, 0, "sin_permiso_meta", "sin_permiso"},
	}
	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			b := nuevaBandeja(t, nil)
			admin := b.persona([]int64{1}, rolBase("admin"))
			admin.empresa = 1
			num := numeroCliente()
			b.texto(num, "wamid.in."+num, "No tengo internet")
			conv := cid(b.conv(num))
			if got := b.conexionCanal(admin); got != "sin_probar" && got != "webhook_sin_verificar" && got != "conectado" {
				t.Fatalf("antes de enviar: %s", got)
			}

			b.meta.rechazar(c.status, c.codigo, c.subcodigo)
			id1 := b.responderYProcesar(conv, "Ya lo revisamos.")
			id2 := b.responderYProcesar(conv, "¿Sigue sin servicio?")
			b.meta.rechazar(0, 0, 0)

			// El número queda marcado sin pulsar «Probar», y el aviso sale una sola vez.
			if got := b.conexionCanal(admin); got != c.conexion {
				t.Fatalf("conexión del número: %s, quería %s", got, c.conexion)
			}
			ev := b.probados()
			if len(ev) != 1 || ev[0]["estado"] != c.conexion || ev[0]["origen"] != "envio" || len(ev[0]) != 4 {
				t.Fatalf("canal_mensajeria.probado: %v", ev)
			}

			// Quien administra los números ve el motivo y el código; el agente, solo el motivo.
			for _, id := range []string{id1, id2} {
				m := mensajePorID(b.mensajes(admin, conv), id)
				if m["estadoEntrega"] != "fallido" || m["motivoFalla"] != c.motivo {
					t.Fatalf("mensaje para quien administra: %v", m)
				}
				cf, _ := m["codigoFalla"].(map[string]any)
				if cf == nil || cf["status"] != float64(c.status) || cf["codigo"] != float64(c.codigo) {
					t.Fatalf("codigoFalla: %v", m["codigoFalla"])
				}
				if sub := cf["subcodigo"]; (c.subcodigo == 0 && sub != nil) || (c.subcodigo != 0 && sub != float64(c.subcodigo)) {
					t.Fatalf("subcódigo: %v", cf)
				}
				if len(cf) != 3 {
					t.Fatalf("codigoFalla con campos de más: %v", cf)
				}
			}
			for _, p := range []persona{b.agente, b.supervisor} {
				m := mensajePorID(b.mensajes(p, conv), id1)
				if m["motivoFalla"] != c.motivo {
					t.Fatalf("motivo para el agente: %v", m)
				}
				if _, hay := m["codigoFalla"]; hay {
					t.Fatalf("el código solo es para quien administra los números: %v", m)
				}
			}

			// Ni el texto libre de Meta ni el token salen por la API, los eventos o la auditoría.
			lecturas := []respuesta{
				b.api(admin, http.MethodGet, "/conversaciones/"+conv+"/mensajes?limit=200", nil),
				b.api(admin, http.MethodGet, fmt.Sprintf("/canales/%d", b.canal.ID), nil),
				b.api(admin, http.MethodGet, "/conversaciones/"+conv, nil),
			}
			for _, r := range lecturas {
				if s := crudo(r); strings.Contains(s, textoMeta) || strings.Contains(s, tokenCanal) || strings.Contains(s, "AbCdEf") {
					t.Fatalf("la API deja ver el texto de Meta o el token: %s", s)
				}
			}
			for _, sql := range []string{
				`SELECT count(*) FROM integracion.outbox WHERE payload::text LIKE '%' || $1 || '%'`,
				`SELECT count(*) FROM auditoria.acciones WHERE coalesce(entrada::text, '') || coalesce(resultado::text, '') LIKE '%' || $1 || '%'`,
				`SELECT count(*) FROM mensajeria.canales WHERE coalesce(ultimo_detalle, '') LIKE '%' || $1 || '%'`,
			} {
				for _, secreto := range []string{textoMeta, tokenCanal} {
					if n := b.contar(sql, secreto); n != 0 {
						t.Fatalf("%q quedó fuera de detalle_falla (%s)", secreto, sql)
					}
				}
			}
			// El evento del mensaje lleva el motivo propio.
			fallidos := b.outbox("conversacion.mensaje_fallido", conv)
			if len(fallidos) != 2 || fallidos[0]["motivo"] != c.motivo {
				t.Fatalf("conversacion.mensaje_fallido: %v", fallidos)
			}
			if _, hay := fallidos[0]["codigo"]; hay {
				t.Fatalf("el evento no lleva el código: %v", fallidos[0])
			}
		})
	}
}

// TestRechazoDeMetaNoMarcaPorOtrosErrores: un número inválido o un fallo temporal no tocan el número.
func TestRechazoDeMetaNoMarcaPorOtrosErrores(t *testing.T) {
	b := nuevaBandeja(t, nil)
	admin := b.persona([]int64{1}, rolBase("admin"))
	admin.empresa = 1
	num := numeroCliente()
	b.texto(num, "wamid.in."+num, "Hola")
	conv := cid(b.conv(num))
	antes := b.conexionCanal(admin)
	b.meta.error.Store(131026)
	id := b.responderYProcesar(conv, "Hola, ¿en qué le ayudamos?")
	b.meta.error.Store(0)
	m := mensajePorID(b.mensajes(admin, conv), id)
	if m["motivoFalla"] != "numero_invalido" {
		t.Fatalf("número inválido: %v", m)
	}
	// El código sí se ve (sirve para soporte), pero el número no cambia de estado.
	if cf, _ := m["codigoFalla"].(map[string]any); cf == nil || cf["codigo"] != float64(131026) || cf["status"] != float64(http.StatusBadRequest) {
		t.Fatalf("codigoFalla: %v", m["codigoFalla"])
	}
	if got := b.conexionCanal(admin); got != antes || len(b.probados()) != 0 {
		t.Fatalf("un número inválido no marca el canal: %s → %s, %v", antes, got, b.probados())
	}

	// Un fallido que llega por webhook guarda el código de Meta (sin status: no hubo respuesta HTTP).
	id2 := b.responderYProcesar(conv, "Le escribimos de nuevo.")
	var wamid string
	if err := b.bd.QueryRow(b.ctx, `SELECT proveedor_id FROM mensajeria.conversacion_mensajes WHERE id = $1`, id2).Scan(&wamid); err != nil {
		t.Fatal(err)
	}
	b.webhook(fmt.Sprintf(`{"object":"whatsapp_business_account","entry":[{"changes":[{"field":"messages","value":{"metadata":{"phone_number_id":%q},`+
		`"statuses":[{"id":%q,"status":"failed","timestamp":"%d","errors":[{"code":131047,"title":"TEXTO-LIBRE-DE-META"}]}]}}]}]}`, b.pnid, wamid, b.reloj.Unix()))
	m2 := mensajePorID(b.mensajes(admin, conv), id2)
	cf, _ := m2["codigoFalla"].(map[string]any)
	if m2["estadoEntrega"] != "fallido" || cf == nil || cf["codigo"] != float64(131047) {
		t.Fatalf("fallido por webhook: %v", m2)
	}
	if cf["status"] != nil {
		t.Fatalf("por webhook no hay status HTTP: %v", cf)
	}
	crudoM, _ := json.Marshal(m2)
	if strings.Contains(string(crudoM), "TEXTO-LIBRE-DE-META") {
		t.Fatalf("el texto de Meta salió por la API: %s", crudoM)
	}
}

// TestRechazoDeMeta403QueNoEsDePermiso: un 403 cuyo código no es de permiso (130497: país
// restringido para la cuenta) depende del destinatario, no del token: el mensaje falla como
// rechazado_por_canal y el número no cambia de estado.
func TestRechazoDeMeta403QueNoEsDePermiso(t *testing.T) {
	for _, codigo := range []int{130497, 131031} {
		b := nuevaBandeja(t, nil)
		admin := b.persona([]int64{1}, rolBase("admin"))
		admin.empresa = 1
		num := numeroCliente()
		b.texto(num, "wamid.in."+num, "Hola")
		conv := cid(b.conv(num))
		antes := b.conexionCanal(admin)
		b.meta.rechazar(http.StatusForbidden, codigo, 0)
		id := b.responderYProcesar(conv, "Hola, ¿en qué le ayudamos?")
		b.meta.rechazar(0, 0, 0)
		m := mensajePorID(b.mensajes(admin, conv), id)
		cf, _ := m["codigoFalla"].(map[string]any)
		if m["estadoEntrega"] != "fallido" || m["motivoFalla"] != "rechazado_por_canal" || cf == nil || cf["codigo"] != float64(codigo) || cf["status"] != float64(http.StatusForbidden) {
			t.Fatalf("403 con %d: %v", codigo, m)
		}
		if got := b.conexionCanal(admin); got != antes || len(b.probados()) != 0 {
			t.Fatalf("403 con %d no marca el número: %s → %s, %v", codigo, antes, got, b.probados())
		}
		if n := b.contar(`SELECT count(*) FROM mensajeria.canales WHERE id = $1 AND (ultimo_estado IS NOT NULL OR rechazado_por_envio)`, b.canal.ID); n != 0 {
			t.Fatalf("403 con %d dejó marca en el número", codigo)
		}
	}
}

// TestRechazoDeMetaSeLimpiaConUnEnvioAceptado: tras un rechazo real del token (401/190), el primer
// envío que Meta acepta devuelve el número a conectado con un solo evento; los siguientes, nada.
// Lo que dijo «Probar» no lo toca un envío.
func TestRechazoDeMetaSeLimpiaConUnEnvioAceptado(t *testing.T) {
	b := nuevaBandeja(t, nil)
	admin := b.persona([]int64{1}, rolBase("admin"))
	admin.empresa = 1
	num := numeroCliente()
	b.texto(num, "wamid.in."+num, "No tengo internet")
	conv := cid(b.conv(num))
	// Con el webhook verificado, «conectado» se ve tal cual.
	if _, err := b.bd.Exec(b.ctx, `UPDATE mensajeria.canales SET webhook_verificado_en = now() WHERE id = $1`, b.canal.ID); err != nil {
		t.Fatal(err)
	}

	b.meta.rechazar(http.StatusUnauthorized, 190, 463)
	b.responderYProcesar(conv, "Ya lo revisamos.")
	b.responderYProcesar(conv, "¿Sigue sin servicio?")
	b.meta.rechazar(0, 0, 0)
	if got := b.conexionCanal(admin); got != "token_invalido" {
		t.Fatalf("tras el rechazo: %s", got)
	}

	ok1 := b.responderYProcesar(conv, "Listo, ya debería funcionar.")
	b.responderYProcesar(conv, "Cualquier cosa nos escribe.")
	b.responderYProcesar(conv, "Buen día.")
	if m := mensajePorID(b.mensajes(admin, conv), ok1); m["estadoEntrega"] != "enviado" {
		t.Fatalf("el envío aceptado: %v", m)
	}
	if got := b.conexionCanal(admin); got != "conectado" {
		t.Fatalf("tras un envío aceptado: %s", got)
	}
	ev := b.probados()
	if len(ev) != 2 || ev[0]["estado"] != "token_invalido" || ev[1]["estado"] != "conectado" || ev[1]["origen"] != "envio" || len(ev[1]) != 4 {
		t.Fatalf("un evento por cambio de estado: %v", ev)
	}
	if n := b.contar(`SELECT count(*) FROM mensajeria.canales WHERE id = $1 AND ultimo_detalle IS NULL AND NOT rechazado_por_envio`, b.canal.ID); n != 1 {
		t.Fatal("la marca debía quedar limpia")
	}

	// Lo que puso «Probar» (aquí, a mano: sin_permiso al leer el número) no lo limpia un envío.
	if _, err := b.bd.Exec(b.ctx, `UPDATE mensajeria.canales SET ultimo_estado = 'sin_permiso', ultimo_detalle = 'de la prueba', rechazado_por_envio = false WHERE id = $1`, b.canal.ID); err != nil {
		t.Fatal(err)
	}
	b.responderYProcesar(conv, "Seguimos atentos.")
	if got := b.conexionCanal(admin); got != "sin_permiso" || len(b.probados()) != 2 {
		t.Fatalf("un envío no pisa lo que dijo «Probar»: %s, %v", got, b.probados())
	}
	// Y si «Probar» ya decía token_invalido, un envío rechazado por lo mismo no repite el evento,
	// pero el siguiente aceptado sí lo limpia.
	if _, err := b.bd.Exec(b.ctx, `UPDATE mensajeria.canales SET ultimo_estado = 'token_invalido' WHERE id = $1`, b.canal.ID); err != nil {
		t.Fatal(err)
	}
	b.meta.rechazar(http.StatusUnauthorized, 190, 0)
	b.responderYProcesar(conv, "Otra vez.")
	b.meta.rechazar(0, 0, 0)
	if len(b.probados()) != 2 {
		t.Fatalf("mismo estado, sin evento: %v", b.probados())
	}
	b.responderYProcesar(conv, "Y ahora sí.")
	if got := b.conexionCanal(admin); got != "conectado" || len(b.probados()) != 3 {
		t.Fatalf("limpieza tras coincidir con la prueba: %s, %v", got, b.probados())
	}
}

// TestLlamadasRechazadasConLasLlamadasApagadas: una entrante a un número con las llamadas apagadas
// en Iris se rechaza y queda a la vista en la pantalla de llamadas del número (cuándo la última y
// cuántas hoy), sin nada de quien llamó.
func TestLlamadasRechazadasConLasLlamadasApagadas(t *testing.T) {
	x := nuevaBandejaVoz(t)
	ver := func() map[string]any {
		t.Helper()
		return x.exigir(x.api(x.supervisor, http.MethodGet, x.rutaCanal("/llamadas"), nil), http.StatusOK).cuerpo
	}
	// Encendidas: una entrante entra normal y no hay aviso.
	if _, hay := ver()["rechazadasApagadas"]; hay {
		t.Fatal("sin rechazos no hay aviso")
	}
	v := ver()
	x.exigir(x.configurar(int(v["version"].(float64)), map[string]any{"llamadasActivas": false}), http.StatusOK)
	if _, hay := ver()["rechazadasApagadas"]; hay {
		t.Fatal("apagadas y sin llamadas: no hay aviso")
	}

	num1, num2 := numeroCliente(), numeroCliente()
	x.connect(num1, "wacid.apagada1"+num1)
	x.connect(num2, "wacid.apagada2"+num2)
	r, _ := ver()["rechazadasApagadas"].(map[string]any)
	if r == nil || r["hoy"] != float64(2) || len(r) != 2 {
		t.Fatalf("rechazadasApagadas: %v", ver())
	}
	ultima, err := time.Parse(time.RFC3339, fmt.Sprint(r["ultimaEn"]))
	if err != nil || !ultima.Equal(*x.reloj) {
		t.Fatalf("ultimaEn: %v (%v), reloj %v", r["ultimaEn"], err, x.reloj)
	}
	// Solo cuándo: ni el número ni el id de la llamada.
	if n := x.contar(`SELECT count(*) FROM mensajeria.llamadas_rechazadas_apagadas r WHERE r.canal_id = $1 AND (to_jsonb(r)::text LIKE '%' || $2 || '%' OR to_jsonb(r)::text LIKE '%wacid%')`, x.canal.ID, num1); n != 0 {
		t.Fatal("la tabla guarda datos de quien llamó")
	}
	if s := crudo(x.api(x.supervisor, http.MethodGet, x.rutaCanal("/llamadas"), nil)); strings.Contains(s, num1) || strings.Contains(s, "wacid") {
		t.Fatalf("la lectura deja ver quién llamó: %s", s)
	}

	// Las de ayer cuentan para «la última» pero no para «hoy»; las de hace más de 7 días, no avisan.
	if _, err := x.bd.Exec(x.ctx, `UPDATE mensajeria.llamadas_rechazadas_apagadas SET ocurrido_en = ocurrido_en - interval '2 days' WHERE canal_id = $1`, x.canal.ID); err != nil {
		t.Fatal(err)
	}
	if r, _ := ver()["rechazadasApagadas"].(map[string]any); r == nil || r["hoy"] != float64(0) {
		t.Fatalf("de anteayer: %v", r)
	}
	if _, err := x.bd.Exec(x.ctx, `UPDATE mensajeria.llamadas_rechazadas_apagadas SET ocurrido_en = ocurrido_en - interval '6 days' WHERE canal_id = $1`, x.canal.ID); err != nil {
		t.Fatal(err)
	}
	if _, hay := ver()["rechazadasApagadas"]; hay {
		t.Fatal("de hace más de 7 días ya no avisa")
	}

	// Una ráfaga no llena la tabla: tope de 500 por día y número.
	if _, err := x.bd.Exec(x.ctx, `INSERT INTO mensajeria.llamadas_rechazadas_apagadas (canal_id, ocurrido_en) SELECT $1, $2 FROM generate_series(1, 500)`, x.canal.ID, *x.reloj); err != nil {
		t.Fatal(err)
	}
	x.connect(numeroCliente(), "wacid.apagada3"+num1)
	if n := x.contar(`SELECT count(*) FROM mensajeria.llamadas_rechazadas_apagadas WHERE canal_id = $1 AND ocurrido_en = $2`, x.canal.ID, *x.reloj); n != 500 {
		t.Fatalf("tope por día: %d", n)
	}
	// Lo de hace más de 30 días se borra al registrar otra.
	if _, err := x.bd.Exec(x.ctx, `UPDATE mensajeria.llamadas_rechazadas_apagadas SET ocurrido_en = ocurrido_en - interval '40 days' WHERE canal_id = $1`, x.canal.ID); err != nil {
		t.Fatal(err)
	}
	x.connect(numeroCliente(), "wacid.apagada4"+num1)
	if n := x.contar(`SELECT count(*) FROM mensajeria.llamadas_rechazadas_apagadas WHERE canal_id = $1`, x.canal.ID); n != 1 {
		t.Fatalf("retención de 30 días: %d", n)
	}

	// Al encender, el aviso desaparece.
	v = ver()
	x.exigir(x.configurar(int(v["version"].(float64)), map[string]any{"llamadasActivas": true}), http.StatusOK)
	if _, hay := ver()["rechazadasApagadas"]; hay {
		t.Fatal("con las llamadas encendidas no hay aviso")
	}
}

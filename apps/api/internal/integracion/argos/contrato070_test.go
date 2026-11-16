package argos_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
)

// Contrato Iris ↔ ArgosISP 0.7.0 (pedido B3): autologin Iris → Argos (§1.3), capacidad
// aprobar_comprobantes y los webhooks comprobante.* como evento interno. Aprobar y rechazar (§7.7)
// se prueban de punta a punta en internal/mensajeria (boletas_decidir_test.go) con este mismo
// cliente HTTP.

// ssoFalso es POST /integraciones/iris/sso/tickets del Argos falso.
type ssoFalso struct {
	pedidos []map[string]any
	tokens  []string
	// apagado: Argos tiene apagado su conector con Iris (409 iris_apagado).
	apagado bool
	// url: lo que responde en `url` (vacío = https://<panel>/sso/entrar#<ticket>).
	url string
}

const (
	panelArgos = "https://argos.prueba"
	ticketSSO  = "ticket-secreto-de-un-solo-uso-"
)

func (a *argosFalso) rutas070(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/integraciones/iris/sso/tickets", a.conToken(func(w http.ResponseWriter, r *http.Request) {
		var p map[string]any
		_ = json.NewDecoder(r.Body).Decode(&p)
		// conToken ya tiene el candado del Argos falso.
		a.sso.pedidos = append(a.sso.pedidos, p)
		a.sso.tokens = append(a.sso.tokens, strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		if a.sso.apagado {
			w.Header().Set("Content-Type", "application/problem+json")
			w.WriteHeader(http.StatusConflict)
			_ = json.NewEncoder(w).Encode(map[string]any{"type": "urn:argosisp:problema:iris_apagado", "title": "Conflicto", "status": 409, "code": "iris_apagado"})
			return
		}
		u := a.sso.url
		if u == "" {
			u = fmt.Sprintf("%s/sso/entrar#%s%d", panelArgos, ticketSSO, len(a.sso.pedidos))
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"url": u, "expira_en": "2026-11-16T12:01:00Z"})
	}))
}

// conectado070: conector activo con un Argos 1.15.0 que informa contrato 0.7.0 y su panel.
func (e *entorno) conectado070() (*clienteHTTP, int64) {
	e.t.Helper()
	e.argos.con(func() { e.argos.version, e.argos.panelURL = "1.15.0", panelArgos })
	admin, id, _ := e.conectado()
	if _, err := e.mod.InformarContrato.Ejecutar(pruebas.ContextoSistema(e.pool), id); err != nil {
		e.t.Fatal(err)
	}
	return admin, id
}

// conectorPaginado busca el conector en todas las páginas de la lista (la base acumula los
// conectores apagados de las pruebas anteriores del paquete).
func (e *entorno) conectorPaginado(admin *clienteHTTP, id int64) map[string]any {
	e.t.Helper()
	cursor := ""
	for range 50 {
		r := admin.pedir(http.MethodGet, "/api/v1/integraciones/conectores?limit=100"+cursor, nil)
		if r.status != 200 {
			e.t.Fatalf("conectores: %d %s", r.status, r.crudo)
		}
		for _, c := range r.cuerpo["data"].([]any) {
			if m := c.(map[string]any); int64(m["id"].(float64)) == id {
				return m
			}
		}
		siguiente, _ := r.cuerpo["nextCursor"].(string)
		if siguiente == "" {
			break
		}
		cursor = "&cursor=" + siguiente
	}
	e.t.Fatalf("no está el conector %d", id)
	return nil
}

func (e *entorno) idDe(c *clienteHTTP) int64 {
	e.t.Helper()
	r := c.pedir(http.MethodGet, "/api/v1/yo", nil)
	if r.status != 200 {
		e.t.Fatalf("yo: %d %s", r.status, r.crudo)
	}
	if u, ok := r.cuerpo["usuario"].(map[string]any); ok {
		return int64(u["id"].(float64))
	}
	return int64(r.cuerpo["id"].(float64))
}

func TestContratoIris070YCapacidad(t *testing.T) {
	if argos.ContratoIris != "0.7.0" {
		t.Fatalf("ContratoIris: %s", argos.ContratoIris)
	}
	e := nuevoEntorno(t)
	admin, id := e.conectado070()
	con := e.conectorPaginado(admin, id)
	if con["contratoRemoto"] != "0.7.0" || fmt.Sprint(con["capacidades"]) != "[inventario_por_tecnico aprobar_comprobantes]" {
		t.Fatalf("conector 0.7.0: %v", con)
	}
	ctx := nucleo.ConBD(pruebas.ContextoSistema(e.pool), e.pool)
	if ok, err := e.mod.ApruebaComprobantes(ctx, e.pool); err != nil || !ok {
		t.Fatalf("ApruebaComprobantes con 0.7.0: %v %v", ok, err)
	}
	// Un Argos que informa un contrato anterior: sin la capacidad, y aprobar ni se intenta.
	e.exec(`UPDATE integracion.conectores SET contrato_remoto = '0.6.4' WHERE id = $1`, id)
	con = e.conectorPaginado(admin, id)
	if fmt.Sprint(con["capacidades"]) != "[inventario_por_tecnico]" {
		t.Fatalf("capacidades con 0.6.4: %v", con["capacidades"])
	}
	if ok, _ := e.mod.ApruebaComprobantes(ctx, e.pool); ok {
		t.Fatal("ApruebaComprobantes con 0.6.4")
	}
	at := argos.AtencionIris{Actor: "usuario:1", Conversacion: "iris:conversacion:1", EmpresaID: 1}
	if _, err := e.mod.AprobarComprobante(ctx, at, 7001, nil, ""); !esProblema(err, "argos_sin_aprobacion") {
		t.Fatalf("aprobar con contrato 0.6.4: %v", err)
	}
	if _, err := e.mod.RechazarComprobante(ctx, at, 7001, "no aparece", ""); !esProblema(err, "argos_sin_aprobacion") {
		t.Fatalf("rechazar con contrato 0.6.4: %v", err)
	}
	// Sin conector activo: tampoco.
	e.exec(`UPDATE integracion.conectores SET estado = 'apagado' WHERE id = $1`, id)
	if ok, _ := e.mod.ApruebaComprobantes(ctx, e.pool); ok {
		t.Fatal("ApruebaComprobantes sin conector")
	}
}

// §1.3: el ticket de autologin Iris → Argos con y sin destino.
func TestAbrirArgosConYSinDestino(t *testing.T) {
	e := nuevoEntorno(t)
	_, id := e.conectado070()
	soporte := e.usuario("soporte")
	uid := e.idDe(soporte)

	// Sin destino: «Vincular mi usuario de Argos».
	r := soporte.pedir(http.MethodPost, "/api/v1/integraciones/argosisp/abrir", map[string]any{}, "Idempotency-Key", "abrir-1")
	if r.status != 200 || r.cuerpo["url"] != panelArgos+"/sso/entrar#"+ticketSSO+"1" || r.cuerpo["expiraEn"] == nil {
		t.Fatalf("abrir sin destino: %d %s", r.status, r.crudo)
	}
	var pedido map[string]any
	var token string
	e.argos.con(func() { pedido, token = e.argos.sso.pedidos[0], e.argos.sso.tokens[0] })
	u := pedido["usuario"].(map[string]any)
	// usuario.id_externo es el id del usuario de Iris en texto: el mismo de X-Iris-Actor (usuario:<id>).
	if u["id_externo"] != fmt.Sprint(uid) || !strings.Contains(fmt.Sprint(u["correo"]), "@") || !strings.HasPrefix(fmt.Sprint(u["nombre"]), "Usuario ") {
		t.Fatalf("usuario del ticket: %v (esperaba id_externo %d)", u, uid)
	}
	if _, hay := pedido["destino"]; hay || token != tokenArgos {
		t.Fatalf("sin destino y con el token del conector: %v", pedido)
	}
	// La respuesta lleva el ticket: no se repite con la misma clave.
	soporte.pedir(http.MethodPost, "/api/v1/integraciones/argosisp/abrir", map[string]any{}, "Idempotency-Key", "abrir-1").problema(t, 409, "idempotencia_no_repetible")

	// Con destino: «Abrir en Argos».
	r = soporte.pedir(http.MethodPost, "/api/v1/integraciones/argosisp/abrir", map[string]any{"destino": map[string]any{"tipo": "servicio", "idExterno": "4410"}})
	if r.status != 200 || r.cuerpo["url"] != panelArgos+"/sso/entrar#"+ticketSSO+"2" {
		t.Fatalf("abrir con destino: %d %s", r.status, r.crudo)
	}
	e.argos.con(func() { pedido = e.argos.sso.pedidos[1] })
	if d := pedido["destino"].(map[string]any); d["tipo"] != "servicio" || d["id_externo"] != "4410" {
		t.Fatalf("destino del ticket: %v", pedido)
	}
	// Destino fuera de contrato: no llega a Argos.
	for _, d := range []map[string]any{{"tipo": "factura", "idExterno": "1"}, {"tipo": "cliente", "idExterno": "0"}, {"tipo": "cliente", "idExterno": "abc"}} {
		if x := soporte.pedir(http.MethodPost, "/api/v1/integraciones/argosisp/abrir", map[string]any{"destino": d}); x.status != 422 {
			t.Fatalf("destino %v: %d %s", d, x.status, x.crudo)
		}
	}
	var pedidos int
	e.argos.con(func() { pedidos = len(e.argos.sso.pedidos) })
	if pedidos != 2 {
		t.Fatalf("pedidos a Argos: %d", pedidos)
	}

	// Auditoría: dos acciones de la persona, con la URL enmascarada; evento sin URL ni ticket.
	if n := e.contar(`SELECT count(*) FROM auditoria.acciones WHERE accion = 'integraciones.abrir_argos' AND resultado = 'ok' AND actor_id = $1
		AND entrada->'auditoria'->>'url' = '***' AND entidad = 'conector' AND entidad_id = $2`, uid, fmt.Sprint(id)); n != 2 {
		t.Fatalf("auditoría de abrir_argos: %d", n)
	}
	if n := e.contar(`SELECT count(*) FROM auditoria.acciones WHERE accion = 'integraciones.abrir_argos' AND resultado = 'ok' AND actor_id = $1
		AND (entrada->>'con_destino')::boolean AND entrada->>'destino_tipo' = 'servicio' AND entrada->>'destino_id' = '4410'`, uid); n != 1 {
		t.Fatalf("auditoría del destino: %d", n)
	}
	if n := e.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'usuario.argos_abierto' AND (payload->>'usuario_id')::bigint = $1
		AND (payload->>'conector_id')::bigint = $2 AND payload ? 'con_destino' AND NOT payload ? 'url'`, uid, id); n != 2 {
		t.Fatalf("usuario.argos_abierto: %d", n)
	}
	if n := e.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'usuario.argos_abierto' AND (payload->>'usuario_id')::bigint = $1 AND (payload->>'con_destino')::boolean`, uid); n != 1 {
		t.Fatalf("usuario.argos_abierto con destino: %d", n)
	}
	e.sinSecretos(ticketSSO, tokenArgos)
	if n := e.contar(`SELECT count(*) FROM nucleo.idempotencia WHERE respuesta IS NOT NULL AND position($1::bytea in respuesta) > 0`, ticketSSO); n != 0 {
		t.Fatal("el ticket quedó guardado en la respuesta de idempotencia")
	}
}

// Quién puede abrir Argos y los errores que ve.
func TestAbrirArgosPermisosYErrores(t *testing.T) {
	e := nuevoEntorno(t)
	_, id := e.conectado070()
	ruta := "/api/v1/integraciones/argosisp/abrir"
	// Los roles que ven la bandeja o los clientes lo tienen; lectura (solo consulta) no.
	for _, rol := range []string{"supervisor", "despacho", "soporte", "ventas", "tecnico", "caja"} {
		if r := e.usuario(rol).pedir(http.MethodPost, ruta, map[string]any{}); r.status != 200 {
			t.Fatalf("%s: %d %s", rol, r.status, r.crudo)
		}
	}
	if r := e.usuario("lectura").pedir(http.MethodPost, ruta, map[string]any{}); r.status != 403 {
		t.Fatalf("lectura: %d %s", r.status, r.crudo)
	}
	if r := e.anonimo().pedir(http.MethodPost, ruta, map[string]any{}); r.status != 401 {
		t.Fatalf("anónimo: %d %s", r.status, r.crudo)
	}
	// Un token de API de una persona con el permiso: no (solo la sesión del panel).
	soporte := e.usuario("soporte")
	tk, err := e.ident.CrearToken.Ejecutar(pruebas.ContextoUsuario(e.pool, e.idDe(soporte), nucleo.PermTokensCrear), identidad.EntradaCrearToken{Nombre: "abrir"})
	if err != nil {
		t.Fatal(err)
	}
	e.anonimo().pedir(http.MethodPost, ruta, map[string]any{}, "Authorization", "Bearer "+tk.Valor).problema(t, 403, "solo_personas")
	// Sin el doble envío de CSRF la sesión no vale.
	sinCSRF := *soporte
	sinCSRF.csrf = ""
	if r := sinCSRF.pedir(http.MethodPost, ruta, map[string]any{}); r.status != 403 {
		t.Fatalf("sin CSRF: %d %s", r.status, r.crudo)
	}

	// Argos tiene apagado su conector con Iris.
	e.argos.con(func() { e.argos.sso.apagado = true })
	soporte.pedir(http.MethodPost, ruta, map[string]any{}).problema(t, 409, "iris_apagado")
	e.argos.con(func() { e.argos.sso.apagado = false })
	// Argos no responde: 502 con «Reintentar».
	e.argos.con(func() { e.argos.caido = true })
	soporte.pedir(http.MethodPost, ruta, map[string]any{}).problema(t, 502, "argos_no_disponible")
	e.argos.con(func() { e.argos.caido = false })
	// Un enlace que no es del panel de Argos (u otra cosa que no sea /sso/entrar#ticket) no se entrega.
	for _, u := range []string{"https://otro.sitio/sso/entrar#t", panelArgos + "/sso/entrar", "javascript:alert(1)", panelArgos + "/otra#t", "http://argos.prueba.evil/sso/entrar#t"} {
		e.argos.con(func() { e.argos.sso.url = u })
		soporte.pedir(http.MethodPost, ruta, map[string]any{}).problema(t, 502, "argos_rechazo")
	}
	e.argos.con(func() { e.argos.sso.url = "" })
	if n := e.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'usuario.argos_abierto' AND (payload->>'usuario_id')::bigint = $1`, e.idDe(soporte)); n != 0 {
		t.Fatalf("un enlace que no se entregó no emite el evento: %d", n)
	}

	// Argos informa un contrato anterior a 0.7.0.
	e.exec(`UPDATE integracion.conectores SET contrato_remoto = '0.6.4' WHERE id = $1`, id)
	e.usuario("soporte").pedir(http.MethodPost, ruta, map[string]any{}).problema(t, 422, "argos_sin_aprobacion")
	// Conector apagado: 409 (no hay datos locales con que seguir).
	e.exec(`UPDATE integracion.conectores SET contrato_remoto = '0.7.0', estado = 'apagado' WHERE id = $1`, id)
	e.usuario("soporte").pedir(http.MethodPost, ruta, map[string]any{}).problema(t, 409, "conector_apagado")
}

// 10 enlaces por minuto por persona.
func TestAbrirArgosLimite(t *testing.T) {
	e := nuevoEntorno(t)
	e.conectado070()
	ruta := "/api/v1/integraciones/argosisp/abrir"
	uno, otro := e.usuario("soporte"), e.usuario("soporte")
	for i := range argos.TopeAbrirArgos {
		if r := uno.pedir(http.MethodPost, ruta, map[string]any{}); r.status != 200 {
			t.Fatalf("pedido %d: %d %s", i+1, r.status, r.crudo)
		}
	}
	r := uno.pedir(http.MethodPost, ruta, map[string]any{})
	r.problema(t, 429, "demasiados_intentos")
	if r.h.Get("Retry-After") == "" {
		t.Fatal("429 sin Retry-After")
	}
	var pedidos int
	e.argos.con(func() { pedidos = len(e.argos.sso.pedidos) })
	if pedidos != argos.TopeAbrirArgos {
		t.Fatalf("pasado el tope no se pide a Argos: %d", pedidos)
	}
	// El tope es por persona.
	if r := otro.pedir(http.MethodPost, ruta, map[string]any{}); r.status != 200 {
		t.Fatalf("otra persona: %d %s", r.status, r.crudo)
	}
}

// §2.3 / §7.2: comprobante.aprobado y comprobante.rechazado salen como argos.comprobante_resuelto.
func TestWebhookComprobanteEmiteEventoInterno(t *testing.T) {
	e := nuevoEntorno(t)
	_, id, secreto := e.conectado()
	comprobante := int64(880000) + int64(e.contar(`SELECT count(*)::int FROM integracion.outbox`))
	r := e.procesarWebhook(id, secreto, argos.EventoComprobanteAprobado, map[string]any{"comprobante_id": comprobante, "empresa_id": 1, "cliente_id": 1201,
		"servicio_id": 4410, "canal": "iris", "monto": "250.00", "pago_id": 9911, "motivo": nil, "causa": nil, "servicio_estado": "activo"})
	if r.Resultado != "aplicado" {
		t.Fatalf("comprobante.aprobado: %+v", r)
	}
	if n := e.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'argos.comprobante_resuelto' AND (payload->>'comprobante_id')::bigint = $1
		AND payload->>'estado' = 'aprobado' AND (payload->>'pago_id')::bigint = 9911 AND payload->>'monto' = '250.00' AND payload->>'canal' = 'iris'
		AND (payload->>'entrante_id')::bigint = $2 AND empresa_id = 1`, comprobante, r.EntranteID); n != 1 {
		t.Fatalf("argos.comprobante_resuelto (aprobado): %d", n)
	}
	// Procesarlo otra vez no emite otro evento.
	if otra, err := e.mod.ProcesarEntrante.Ejecutar(pruebas.ContextoSistema(e.pool), r.EntranteID); err != nil || otra.Resultado != "ya_procesado" {
		t.Fatalf("reproceso: %+v %v", otra, err)
	}
	r = e.procesarWebhook(id, secreto, argos.EventoComprobanteRechazado, map[string]any{"comprobante_id": comprobante + 1, "empresa_id": 1, "cliente_id": 1201,
		"servicio_id": nil, "canal": "iris", "monto": 99.5, "pago_id": nil, "motivo": "No aparece en el banco", "causa": "no_aparece", "servicio_estado": nil})
	if n := e.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'argos.comprobante_resuelto' AND (payload->>'comprobante_id')::bigint = $1
		AND payload->>'estado' = 'rechazado' AND payload->>'causa' = 'no_aparece' AND payload->>'motivo' = 'No aparece en el banco' AND payload->>'pago_id' IS NULL`,
		comprobante+1); r.Resultado != "aplicado" || n != 1 {
		t.Fatalf("argos.comprobante_resuelto (rechazado): %+v %d", r, n)
	}
	// Sin comprobante_id: se ignora y queda dicho.
	antes := e.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'argos.comprobante_resuelto'`)
	r = e.procesarWebhook(id, secreto, argos.EventoComprobanteAprobado, map[string]any{"empresa_id": 1})
	if r.Resultado != "ignorado" || e.omitidoDe(r.EntranteID) != "payload_invalido" || e.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'argos.comprobante_resuelto'`) != antes {
		t.Fatalf("payload inválido: %+v", r)
	}
}

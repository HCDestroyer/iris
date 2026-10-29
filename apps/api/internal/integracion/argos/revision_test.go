package argos_test

import (
	"context"
	"fmt"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/conector"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
)

// Pruebas de regresión de la revisión de seguridad del conector (rama agente/i1-argos).

func (e *entorno) exec(sql string, args ...any) {
	e.t.Helper()
	if _, err := e.pool.Exec(context.Background(), sql, args...); err != nil {
		e.t.Fatal(err)
	}
}

// sembrarClienteEn es sembrarCliente en otra empresa (0 = Argos no informa empresa).
func (e *entorno) sembrarClienteEn(id, empresa int64, nombre string) {
	e.sembrarCliente(id, nombre)
	e.argos.con(func() {
		e.argos.clientes[id]["empresaId"] = empresa
		for _, s := range e.argos.servicios[id] {
			s["empresa_id"] = empresa
		}
	})
}

// Hallazgo 1 (ALTO): un usuario solo ve clientes de su empresa activa en la búsqueda, la ficha
// y resolver IP, también desde las instantáneas; sin empresa no lee nada.
func TestMultiempresaConector(t *testing.T) {
	e := nuevoEntorno(t)
	_, _, _ = e.conectado()
	e.exec(`INSERT INTO nucleo.empresas (id, nombre) VALUES (2, 'Empresa 2')`)
	e.sembrarClienteEn(61, 1, "Ana Norte")
	e.sembrarClienteEn(62, 2, "Ana Sur")
	e.sembrarClienteEn(63, 0, "Ana Sin Empresa")
	e.argos.con(func() {
		e.argos.ips["10.0.0.61"] = map[string]any{"cliente_id": 61, "servicio_id": 610, "empresa_id": 1}
		e.argos.ips["10.0.0.62"] = map[string]any{"cliente_id": 62, "servicio_id": 620, "empresa_id": 2}
		e.argos.ips["10.0.0.63"] = map[string]any{"cliente_id": 63, "servicio_id": 630}
	})
	uno := e.usuarioEn([]int64{1}, "soporte")

	// Búsqueda: solo la empresa activa; lo que llega sin empresa se descarta.
	b := uno.pedir(http.MethodGet, "/api/v1/clientes?q=ana", nil)
	if b.status != 200 || len(b.cuerpo["data"].([]any)) != 1 || b.cuerpo["data"].([]any)[0].(map[string]any)["id"] != float64(61) {
		t.Fatalf("búsqueda multiempresa: %d %s", b.status, b.crudo)
	}
	// Ficha de otra empresa: 404 y sin pedir su estado de cuenta.
	uno.pedir(http.MethodGet, "/api/v1/clientes/62", nil).problema(t, 404, "no_encontrado")
	uno.pedir(http.MethodGet, "/api/v1/clientes/63", nil).problema(t, 404, "no_encontrado")
	if n := e.argos.llamadasA("/api/v1/clientes/62/estado-cuenta"); n != 0 {
		t.Fatalf("pidió el estado de cuenta de un cliente ajeno (%d veces)", n)
	}
	if f := uno.pedir(http.MethodGet, "/api/v1/clientes/61", nil); f.status != 200 || f.cuerpo["estadoCuenta"] == nil {
		t.Fatalf("ficha propia: %d %s", f.status, f.crudo)
	}
	// Resolver IP: la de otra empresa (o sin empresa) es «IP sin cliente».
	uno.pedir(http.MethodGet, "/api/v1/clientes/resolver-ip?ip=10.0.0.62", nil).problema(t, 404, "no_encontrado")
	uno.pedir(http.MethodGet, "/api/v1/clientes/resolver-ip?ip=10.0.0.63", nil).problema(t, 404, "no_encontrado")
	if r := uno.pedir(http.MethodGet, "/api/v1/clientes/resolver-ip?ip=10.0.0.61", nil); r.status != 200 || r.cuerpo["clienteId"] != float64(61) {
		t.Fatalf("resolver IP propia: %d %s", r.status, r.crudo)
	}

	// Con dos empresas hay que elegir; con la cabecera, la otra empresa sí se ve.
	dos := e.usuarioEn([]int64{1, 2}, "soporte")
	dos.pedir(http.MethodGet, "/api/v1/clientes/62", nil).problema(t, 422, "empresa_requerida")
	if f := dos.pedir(http.MethodGet, "/api/v1/clientes/62", nil, "X-Iris-Empresa", "2"); f.status != 200 {
		t.Fatalf("ficha con X-Iris-Empresa: %d %s", f.status, f.crudo)
	}
	dos.pedir(http.MethodGet, "/api/v1/clientes/62", nil, "X-Iris-Empresa", "3").problema(t, 403, "empresa_no_permitida")

	// Con Argos caído, las instantáneas también se filtran (la de 62 quedó guardada).
	if n := e.contar(`SELECT count(*) FROM integracion.argos_clientes WHERE id = 62`); n != 1 {
		t.Fatalf("instantánea de 62: %d", n)
	}
	e.argos.con(func() { e.argos.caido = true })
	bl := uno.pedir(http.MethodGet, "/api/v1/clientes?q=ana", nil)
	if bl.status != 200 || bl.cuerpo["stale"] != true || len(bl.cuerpo["data"].([]any)) != 1 {
		t.Fatalf("búsqueda local multiempresa: %d %s", bl.status, bl.crudo)
	}
	uno.pedir(http.MethodGet, "/api/v1/clientes/62", nil).problema(t, 404, "no_encontrado")
	if r := uno.pedir(http.MethodGet, "/api/v1/clientes/resolver-ip?ip=10.0.0.62", nil); r.status == 200 {
		t.Fatalf("resolver IP desde la instantánea de otra empresa: %s", r.crudo)
	}
	e.argos.con(func() { e.argos.caido = false })

	// Sin empresas asignadas (antes veía todo): nada.
	nadie := e.usuarioEn(nil, "tecnico")
	nadie.pedir(http.MethodGet, "/api/v1/clientes/61", nil).problema(t, 403, "sin_empresa")
	nadie.pedir(http.MethodGet, "/api/v1/clientes?q=ana", nil).problema(t, 403, "sin_empresa")
	nadie.pedir(http.MethodGet, "/api/v1/clientes/resolver-ip?ip=10.0.0.61", nil).problema(t, 403, "sin_empresa")

	// Conector activo sin empresas registradas: no hay cómo filtrar, 422.
	e.exec(`DELETE FROM nucleo.usuarios_empresas`)
	e.exec(`DELETE FROM nucleo.empresas`)
	nadie.pedir(http.MethodGet, "/api/v1/clientes/61", nil).problema(t, 422, "empresas_no_registradas")
}

// firmado arma un webhook firmado con el cuerpo crudo dado y la cabecera X-Webhook-Id dada.
func (e *entorno) firmado(conectorID int64, secreto, webhookID string, cuerpo []byte, marca time.Time) respuesta {
	e.t.Helper()
	ts := strconv.FormatInt(marca.Unix(), 10)
	return e.anonimo().pedir(http.MethodPost, fmt.Sprintf("/api/v1/integraciones/argosisp/webhook/%d", conectorID), cuerpo,
		"X-Webhook-Id", webhookID, "X-Webhook-Timestamp", ts, "X-Webhook-Signature", argos.FirmarWebhook(secreto, ts, cuerpo))
}

// Hallazgo 3 (MEDIO): un webhook capturado no se reprocesa cambiando X-Webhook-Id (no firmado):
// la deduplicación es por el id del cuerpo, que va firmado y debe coincidir con la cabecera.
func TestWebhookReenvioConOtroID(t *testing.T) {
	e := nuevoEntorno(t)
	_, id, secreto := e.conectado()
	e.sembrarCliente(5, "Retiro")
	e.argos.con(func() { e.argos.servicios[5][0]["estado"] = "retirado" })
	ahora := time.Now()
	antes := e.contar(`SELECT count(*) FROM integracion.entrantes WHERE conector_id = $1`, id)

	// Sin id en el cuerpo: 422 (la cabecera sola no basta).
	sinID := []byte(`{"evento":"servicio.estado_cambiado","payload":{"empresa_id":1,"servicio_id":50,"cliente_id":5,"de":"activo","a":"retirado"}}`)
	e.firmado(id, secreto, "orig-0", sinID, ahora).problema(t, 422, "validacion")

	wid := "orig-" + pruebas.Unico()
	cuerpo := []byte(`{"id":"` + wid + `","evento":"servicio.estado_cambiado","payload":{"empresa_id":1,"servicio_id":50,"cliente_id":5,"de":"activo","a":"retirado"}}`)
	ent := e.entranteDe(e.firmado(id, secreto, wid, cuerpo, ahora))
	for _, otro := range []string{"replay-2", "replay-3"} {
		e.firmado(id, secreto, otro, cuerpo, ahora).problema(t, 422, "validacion")
	}
	// El mismo id (reintento de Argos) es un duplicado.
	if d := e.firmado(id, secreto, wid, cuerpo, ahora); d.status != 200 || d.cuerpo["duplicado"] != true {
		t.Fatalf("reintento: %d %s", d.status, d.crudo)
	}
	if n := e.contar(`SELECT count(*) FROM integracion.entrantes WHERE conector_id = $1`, id) - antes; n != 1 {
		t.Fatalf("entrantes nuevos: %d (esperaba 1)", n)
	}
	ctx := pruebas.ContextoSistema(e.pool)
	if r, err := e.mod.ProcesarEntrante.Ejecutar(ctx, ent); err != nil || r.Resultado != "aplicado" {
		t.Fatalf("procesar: %+v %v", r, err)
	}
	if n := e.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'argos.servicio_retirado' AND (payload->>'entrante_id')::bigint = $1 AND payload->>'servicio_id' = '50' AND empresa_id = 1`, ent); n != 1 {
		t.Fatalf("argos.servicio_retirado: %d", n)
	}
}

// Hallazgo 3 (MEDIO): argos.servicio_retirado solo sale si Argos confirma el estado del
// servicio; el payload del webhook no se cree.
func TestRetiroSeConfirmaEnArgos(t *testing.T) {
	e := nuevoEntorno(t)
	_, id, secreto := e.conectado()
	e.sembrarCliente(6, "Activo")
	ctx := pruebas.ContextoSistema(e.pool)
	retiro := func(servicio int64) int64 {
		return e.entranteDe(e.enviar(id, secreto, "wh-r-"+pruebas.Unico(), "servicio.estado_cambiado",
			map[string]any{"empresa_id": 1, "servicio_id": servicio, "cliente_id": 6, "de": "activo", "a": "retirado"}, time.Now()))
	}
	contarRetiros := func(ent int64) int {
		return e.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'argos.servicio_retirado' AND (payload->>'entrante_id')::bigint = $1`, ent)
	}

	// Argos dice que el servicio sigue activo: no hay evento.
	ent := retiro(60)
	if r, err := e.mod.ProcesarEntrante.Ejecutar(ctx, ent); err != nil || r.Resultado != "ignorado" || contarRetiros(ent) != 0 {
		t.Fatalf("servicio activo en Argos: %+v %v (%d eventos)", r, err, contarRetiros(ent))
	}
	// No existe en Argos: tampoco.
	ent = retiro(6666)
	if r, err := e.mod.ProcesarEntrante.Ejecutar(ctx, ent); err != nil || contarRetiros(ent) != 0 {
		t.Fatalf("servicio inexistente: %+v %v", r, err)
	}
	// Argos caído: error (River reintenta), sin evento ni marca de procesado.
	ent = retiro(60)
	e.argos.con(func() { e.argos.caido = true })
	if _, err := e.mod.ProcesarEntrante.Ejecutar(ctx, ent); err == nil || contarRetiros(ent) != 0 {
		t.Fatalf("con Argos caído debía fallar sin evento: %v", err)
	}
	// Argos vuelve y confirma: un evento.
	e.argos.con(func() {
		e.argos.caido = false
		e.argos.servicios[6][0]["estado"] = "retirado"
	})
	if r, err := e.mod.ProcesarEntrante.Ejecutar(ctx, ent); err != nil || r.Resultado != "aplicado" || contarRetiros(ent) != 1 {
		t.Fatalf("retiro confirmado: %+v %v (%d eventos)", r, err, contarRetiros(ent))
	}
}

// Hallazgo 6 (BAJO): la firma se valida antes que la ventana, así un timestamp viejo no
// distingue un conector que existe de uno que no.
func TestFirmaAntesQueVentana(t *testing.T) {
	e := nuevoEntorno(t)
	_, id, _ := e.conectado()
	viejo := strconv.FormatInt(time.Now().Add(-time.Hour).Unix(), 10)
	var tipos []any
	for _, cid := range []int64{id, 999999} {
		r := e.anonimo().pedir(http.MethodPost, fmt.Sprintf("/api/v1/integraciones/argosisp/webhook/%d", cid), []byte(`{}`),
			"X-Webhook-Id", "x", "X-Webhook-Timestamp", viejo, "X-Webhook-Signature", "v1=00")
		r.problema(t, 401, "firma_invalida")
		tipos = append(tipos, r.cuerpo["detail"])
	}
	if tipos[0] != tipos[1] {
		t.Fatalf("las respuestas distinguen conectores: %v", tipos)
	}
}

// Hallazgo 7 (BAJO): id numérico y ocurrido_en no RFC 3339 se aceptan (hora de recepción).
func TestWebhookCuerpoTolerante(t *testing.T) {
	e := nuevoEntorno(t)
	_, id, secreto := e.conectado()
	n := time.Now().UnixNano()
	wid := strconv.FormatInt(n, 10)
	cuerpo := []byte(`{"id":` + wid + `,"evento":"cliente.actualizado","ocurrido_en":"28/10/2026 10:00","payload":{"empresa_id":1,"cliente_id":5}}`)
	ent := e.entranteDe(e.firmado(id, secreto, wid, cuerpo, time.Now()))
	if c := e.contar(`SELECT count(*) FROM integracion.entrantes WHERE id = $1 AND webhook_id = $2 AND ocurrido_en > now() - interval '1 minute'`, ent, wid); c != 1 {
		t.Fatal("ocurrido_en ilegible debía guardarse con la hora de recepción")
	}
	// RFC 3339 se respeta.
	wid2 := "wh-t-" + pruebas.Unico()
	cuerpo = []byte(`{"id":"` + wid2 + `","evento":"cliente.actualizado","ocurrido_en":"2026-10-01T10:00:00-06:00","payload":{}}`)
	ent = e.entranteDe(e.firmado(id, secreto, wid2, cuerpo, time.Now()))
	if c := e.contar(`SELECT count(*) FROM integracion.entrantes WHERE id = $1 AND ocurrido_en = '2026-10-01T16:00:00Z'`, ent); c != 1 {
		t.Fatal("ocurrido_en RFC 3339 mal guardado")
	}
}

// Hallazgo 4 (MEDIO): los 401 anónimos del webhook y del canje no escriben en la auditoría (se
// cuentan); un webhook aceptado sí.
func TestRechazosAnonimosNoInundanAuditoria(t *testing.T) {
	e := nuevoEntorno(t)
	_, id, secreto := e.conectado()
	acciones := func() int {
		return e.contar(`SELECT count(*) FROM auditoria.acciones WHERE accion IN ('integraciones.webhook', 'integraciones.canjear')`)
	}
	antes, rechazos := acciones(), e.mod.RechazosSinAuditar()
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	for i := range 30 {
		e.anonimo().pedir(http.MethodPost, fmt.Sprintf("/api/v1/integraciones/argosisp/webhook/%d", id), []byte(`{}`),
			"X-Webhook-Id", fmt.Sprint("x", i), "X-Webhook-Timestamp", ts, "X-Webhook-Signature", "v1=00").problema(t, 401, "firma_invalida")
		e.anonimo().pedir(http.MethodPost, "/api/v1/integraciones/argosisp/conexion/canjear", map[string]any{
			"codigo": fmt.Sprint("codigo-inventado-", i, "-0123456789"), "sistema": "argosisp", "url": "https://argos.prueba", "token": "atk_inventado_123",
		}).problema(t, 401, "codigo_invalido")
	}
	// Firma válida pero vencida (webhook capturado y reenviado tarde): tampoco.
	viejo := time.Now().Add(-time.Hour)
	e.firmado(id, secreto, "wh-v", []byte(`{"id":"wh-v","evento":"cliente.actualizado","payload":{}}`), viejo).problema(t, 401, "firma_vencida")
	if d := acciones() - antes; d != 0 {
		t.Fatalf("los rechazos anónimos dejaron %d filas de auditoría", d)
	}
	if d := e.mod.RechazosSinAuditar() - rechazos; d != 61 {
		t.Fatalf("rechazos contados: %d", d)
	}
	// Un webhook aceptado se audita.
	e.entranteDe(e.enviar(id, secreto, "wh-ok-"+pruebas.Unico(), "cliente.actualizado", map[string]any{"empresa_id": 1}, time.Now()))
	if d := acciones() - antes; d != 1 {
		t.Fatalf("el webhook aceptado debía auditarse: %d", d)
	}
}

// Hallazgo 5 (MEDIO): SSRF. Las IP internas se bloquean al conectar, también detrás de un nombre
// (resuelto por DNS), salvo IRIS_ARGOS_REDES_PERMITIDAS.
func TestDestinoPermitido(t *testing.T) {
	bloqueadas := []string{"127.0.0.1", "10.1.2.3", "172.16.0.1", "192.168.1.1", "169.254.169.254", "100.64.0.1",
		"0.0.0.0", "255.255.255.255", "224.0.0.1", "::", "::1", "fe80::1", "fc00::1", "::ffff:127.0.0.1", "::ffff:169.254.169.254",
		"64:ff9b::a9fe:a9fe", "2002:7f00:1::1"}
	for _, s := range bloqueadas {
		if argos.DestinoPermitido(netip.MustParseAddr(s), nil) {
			t.Errorf("%s debía bloquearse", s)
		}
	}
	for _, s := range []string{"8.8.8.8", "190.106.1.1", "2001:4860:4860::8888"} {
		if !argos.DestinoPermitido(netip.MustParseAddr(s), nil) {
			t.Errorf("%s debía permitirse", s)
		}
	}
	lab := []netip.Prefix{netip.MustParsePrefix("10.20.0.0/16")}
	if !argos.DestinoPermitido(netip.MustParseAddr("10.20.1.5"), lab) || argos.DestinoPermitido(netip.MustParseAddr("10.21.1.5"), lab) {
		t.Error("IRIS_ARGOS_REDES_PERMITIDAS mal aplicada")
	}
}

func TestSSRFConectorYLecturas(t *testing.T) {
	e := nuevoEntorno(t)
	_, _, _ = e.conectado() // con loopback permitido (el Argos falso)
	estricto := e.cfg
	estricto.RedesPermitidas = nil
	m := argos.Nuevo(estricto)
	m.Nucleo().FijarEmisorToken(conector.EmisorDesde(e.ident.EmitirTokenIntegracion, e.ident.RevocarTokenIntegracion))
	ctx := pruebas.ContextoUsuario(e.pool, 0, nucleo.PermIntegracionesConectar)
	puerto := e.argos.srv.URL[strings.LastIndex(e.argos.srv.URL, ":")+1:]
	canjesAntes := 0
	e.argos.con(func() { canjesAntes = len(e.argos.canjes) })

	// IP interna escrita en el enlace: 422 al validar.
	for _, enlace := range []string{"http://127.0.0.1:" + puerto, "http://169.254.169.254", "http://[::1]:" + puerto, "http://10.0.0.5"} {
		if _, err := m.Conectar.Ejecutar(ctx, argos.EntradaConectar{Enlace: enlace + "/conectar#codigo-de-argos-0123456789"}); !esProblema(err, "validacion") {
			t.Fatalf("%s: debía ser 422 validacion: %v", enlace, err)
		}
	}
	// Un nombre que resuelve a loopback: se corta al conectar (después del DNS).
	_, err := m.Conectar.Ejecutar(ctx, argos.EntradaConectar{Enlace: "http://localhost:" + puerto + "/conectar#codigo-de-argos-0123456789"})
	if !esProblema(err, "destino_no_permitido") {
		t.Fatalf("localhost debía bloquearse en el dial: %v", err)
	}
	e.argos.con(func() {
		if len(e.argos.canjes) != canjesAntes {
			t.Fatal("la petición llegó al destino interno")
		}
	})

	// Canje desde «Argos» con una URL interna: 422.
	admin := e.usuario("admin")
	r := admin.pedir(http.MethodPost, "/api/v1/integraciones/argosisp/conexion/enlaces", nil)
	_, codigo, _ := strings.Cut(r.cuerpo["enlace"].(string), "#")
	if _, err := m.CanjearConexion.Ejecutar(pruebas.ContextoSistema(e.pool), argos.EntradaCanjear{
		Codigo: codigo, Sistema: "argosisp", URL: "http://169.254.169.254/latest", Token: tokenArgos,
	}); !esProblema(err, "validacion") {
		t.Fatalf("canje con URL interna: %v", err)
	}

	// Lecturas con un conector guardado en una red interna: se bloquean sin llegar al destino.
	e.sembrarCliente(81, "Ana Interna")
	antes := e.argos.llamadasA("/api/v1/clientes")
	lctx := pruebas.ContextoUsuarioEnEmpresa(e.pool, 1, 1, nucleo.PermClientesVer)
	if _, err := m.BuscarClientes.Ejecutar(lctx, argos.EntradaBuscarClientes{Q: "ana", Limit: 5}); !esProblema(err, "destino_no_permitido") {
		t.Fatalf("lectura hacia red interna: %v", err)
	}
	if e.argos.llamadasA("/api/v1/clientes") != antes {
		t.Fatal("la lectura llegó al destino interno")
	}
}

// Hallazgo 5: al admin no le llega el detail crudo de lo que respondió la URL (resumen).
func TestRechazoDeArgosResumido(t *testing.T) {
	e := nuevoEntorno(t)
	admin := e.usuario("admin")
	r := admin.pedir(http.MethodPost, "/api/v1/integraciones/argosisp/conectar", map[string]any{"enlace": e.argos.srv.URL + "/conectar#codigo-que-no-es-0123456789"})
	r.problema(t, 502, "argos_rechazo")
	if d, _ := r.cuerpo["detail"].(string); strings.Contains(d, "venció") || !strings.Contains(d, "HTTP 422") {
		t.Fatalf("detail: %q", d)
	}
}

// ValidarCliente (acceso al portal): existe y su empresa; 404; Argos caído o apagado = no
// validado, con el motivo.
func TestValidarClienteParaElPortal(t *testing.T) {
	e := nuevoEntorno(t)
	ctx := pruebas.ContextoSistema(e.pool)
	if v, err := e.mod.ValidarCliente(ctx, 71); err != nil || v.Validado || v.Motivo != argos.MotivoConectorApagado {
		t.Fatalf("apagado: %+v %v", v, err)
	}
	_, _, _ = e.conectado()
	e.sembrarClienteEn(71, 2, "Fabiola")
	if v, err := e.mod.ValidarCliente(ctx, 71); err != nil || !v.Validado || !v.Existe || v.EmpresaID != 2 {
		t.Fatalf("existe: %+v %v", v, err)
	}
	if v, err := e.mod.ValidarCliente(ctx, 999); err != nil || !v.Validado || v.Existe {
		t.Fatalf("no existe: %+v %v", v, err)
	}
	e.argos.con(func() { e.argos.caido = true })
	if v, err := e.mod.ValidarCliente(ctx, 71); err != nil || v.Validado || v.Motivo != argos.MotivoArgosNoDisponible {
		t.Fatalf("caído: %+v %v", v, err)
	}
}

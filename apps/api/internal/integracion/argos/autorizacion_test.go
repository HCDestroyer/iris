package argos_test

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/HCDestroyer/iris/apps/api/internal/integracion/conector"
)

// «Conectar con un botón» (contrato Iris ↔ ArgosISP 0.4.0 §1.4): state ligado a la sesión, PKCE
// S256 verificado por el Argos falso, api solo de IRIS_ARGOS_ORIGENES y panel_url separado de la
// API.

const (
	rutaAutorizar = "/api/v1/integraciones/argosisp/autorizaciones"
	rutaCompletar = "/api/v1/integraciones/argosisp/autorizaciones/completar"
)

// iniciarAutorizacion pulsa «Conectar con Argos» con la sesión de admin y devuelve la URL de
// autorizar.
func (e *entorno) iniciarAutorizacion(admin *clienteHTTP) string {
	e.t.Helper()
	r := admin.pedir(http.MethodPost, rutaAutorizar, map[string]any{"panelUrl": "https://Panel.Argos.prueba:443/inicio"})
	if r.status != 201 {
		e.t.Fatalf("iniciar: %d %s", r.status, r.crudo)
	}
	if r.cuerpo["panelUrl"] != "https://panel.argos.prueba" {
		e.t.Fatalf("panel normalizado: %v", r.cuerpo["panelUrl"])
	}
	return r.cuerpo["url"].(string)
}

func (e *entorno) autorizacionUsada(state string) bool {
	return e.contar(`SELECT count(*) FROM integracion.autorizaciones WHERE state_hash = $1 AND usado_en IS NOT NULL`, conector.Hash256(state)) == 1
}

func TestConectarConUnBoton(t *testing.T) {
	e := nuevoEntorno(t)
	e.argos.con(func() { e.argos.panelURL = "https://argos.kns.prueba" })
	admin := e.usuario("admin")

	autorizar := e.iniciarAutorizacion(admin)
	u, _ := url.Parse(autorizar)
	if u.Scheme != "https" || u.Host != "panel.argos.prueba" || u.Path != "/integraciones/iris/autorizar" {
		t.Fatalf("URL de autorizar: %s", autorizar)
	}
	q := u.Query()
	if q.Get("iris") != "https://iris.prueba" || q.Get("code_challenge_method") != "S256" {
		t.Fatalf("consulta: %v", q)
	}
	// Solo el hash del state y el verifier cifrado quedan en la base.
	if e.contar(`SELECT count(*) FROM integracion.autorizaciones WHERE state_hash = $1 AND verificador_cifrado IS NOT NULL
		AND expira_en BETWEEN now() + interval '9 minutes' AND now() + interval '11 minutes'`, conector.Hash256(q.Get("state"))) != 1 {
		t.Fatal("la autorización se guarda con el hash del state, el verifier cifrado y 10 min")
	}

	codigo, api, state := e.argos.aprobar(autorizar, false)
	r := admin.pedir(http.MethodPost, rutaCompletar, map[string]any{"codigo": codigo, "api": api, "state": state})
	if r.status != 201 {
		t.Fatalf("completar: %d %s", r.status, r.crudo)
	}
	con := r.cuerpo["conector"].(map[string]any)
	if con["estado"] != "apagado" || con["origen"] != "autorizacion" || con["panelUrl"] != "https://argos.kns.prueba" ||
		con["apiUrl"] != e.argos.srv.URL || con["baseUrl"] != e.argos.srv.URL || con["urlPublica"] != "https://argos.kns.prueba" {
		t.Fatalf("conector: %v", con)
	}
	if emp := r.cuerpo["empresas"].([]any); len(emp) != 1 || emp[0].(map[string]any)["id"] != float64(1) {
		t.Fatalf("empresas: %v", r.cuerpo["empresas"])
	}
	id := int64(con["id"].(float64))

	// El canje llevó el code_verifier (el falso lo verificó contra el challenge) y el iris_pat_.
	var verificador string
	e.argos.con(func() { verificador, _ = e.argos.canjes[len(e.argos.canjes)-1]["code_verifier"].(string) })
	if len(verificador) != 43 || conector.DesafioS256(verificador) != q.Get("code_challenge") {
		t.Fatalf("code_verifier del canje: %q", verificador)
	}
	// Ni el verifier ni el state salen por la API ni quedan en la auditoría o la base en claro.
	if strings.Contains(autorizar, verificador) || e.contar(`SELECT count(*) FROM auditoria.acciones WHERE entrada::text LIKE '%' || $1 || '%' OR entrada::text LIKE '%' || $2 || '%'`,
		verificador, state) != 0 {
		t.Fatal("el verifier o el state se filtraron")
	}
	if e.contar(`SELECT count(*) FROM integracion.autorizaciones WHERE state_hash = $1 AND usado_en IS NOT NULL AND verificador_cifrado IS NULL AND conector_id = $2`,
		conector.Hash256(state), id) != 1 {
		t.Fatal("la autorización queda usada, sin verifier y ligada al conector")
	}
	for _, accion := range []string{"integraciones.autorizar", "integraciones.completar_autorizacion"} {
		if e.contar(`SELECT count(*) FROM auditoria.acciones WHERE accion = $1 AND resultado = 'ok'`, accion) == 0 {
			t.Fatalf("sin auditoría de %s", accion)
		}
	}
	if e.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'conector.creado' AND (payload->>'conector_id')::bigint = $1 AND payload->>'origen' = 'autorizacion'`, id) != 1 {
		t.Fatal("conector.creado")
	}

	// Apagado hasta «Activar»; activo, «Ver en Argos» va al panel, no a la API.
	if a := admin.pedir(http.MethodPost, fmt.Sprintf("/api/v1/integraciones/conectores/%d/activar", id), map[string]any{"version": 1}); a.status != 200 {
		t.Fatalf("activar: %d %s", a.status, a.crudo)
	}
	enl := admin.pedir(http.MethodGet, "/api/v1/integraciones/enlaces", nil)
	if argosisp, _ := enl.cuerpo["argosisp"].(map[string]any); argosisp["urlPublica"] != "https://argos.kns.prueba" {
		t.Fatalf("enlaces: %s", enl.crudo)
	}

	// Reusar el state: 422 state_invalido, sin llamar a Argos.
	canjes := e.canjes()
	admin.pedir(http.MethodPost, rutaCompletar, map[string]any{"codigo": codigo, "api": api, "state": state}).problema(t, 422, "state_invalido")
	if e.canjes() != canjes {
		t.Fatal("un state reusado no llega a Argos")
	}
}

func TestAutorizacionStateVencidoYDeOtraSesion(t *testing.T) {
	e := nuevoEntorno(t)
	admin := e.usuario("admin")

	// De otra sesión (otro administrador): 422 state_invalido, sin gastarlo.
	autorizar := e.iniciarAutorizacion(admin)
	codigo, api, state := e.argos.aprobar(autorizar, false)
	otro := e.usuario("admin")
	otro.pedir(http.MethodPost, rutaCompletar, map[string]any{"codigo": codigo, "api": api, "state": state}).problema(t, 422, "state_invalido")
	if e.canjes() != 0 || e.autorizacionUsada(state) {
		t.Fatal("otra sesión no canjea ni gasta el state")
	}
	// Un state que Iris no emitió, o mal formado.
	admin.pedir(http.MethodPost, rutaCompletar, map[string]any{"codigo": codigo, "api": api, "state": strings.Repeat("x", 43)}).problema(t, 422, "state_invalido")
	admin.pedir(http.MethodPost, rutaCompletar, map[string]any{"codigo": codigo, "api": api, "state": "corto"}).problema(t, 422, "state_invalido")

	// Vencido: 422 state_vencido.
	if _, err := e.pool.Exec(context.Background(), `UPDATE integracion.autorizaciones SET expira_en = now() - interval '1 second'`); err != nil {
		t.Fatal(err)
	}
	admin.pedir(http.MethodPost, rutaCompletar, map[string]any{"codigo": codigo, "api": api, "state": state}).problema(t, 422, "state_vencido")
	if e.canjes() != 0 {
		t.Fatal("un state vencido no llega a Argos")
	}

	// Quien la inició, a tiempo, sí completa (la de otra sesión no la gastó).
	autorizar = e.iniciarAutorizacion(admin)
	codigo, api, state = e.argos.aprobar(autorizar, false)
	if r := admin.pedir(http.MethodPost, rutaCompletar, map[string]any{"codigo": codigo, "api": api, "state": state}); r.status != 201 {
		t.Fatalf("completar desde la misma sesión: %d %s", r.status, r.crudo)
	}
}

func TestAutorizacionOrigenRechazoYVerificador(t *testing.T) {
	e := nuevoEntorno(t)
	admin := e.usuario("admin")
	tokens := e.tokensIntegracion()

	// api fuera de IRIS_ARGOS_ORIGENES: 422 origen_no_permitido, sin canje ni token emitido; el
	// state queda gastado (un solo uso).
	autorizar := e.iniciarAutorizacion(admin)
	codigo, _, state := e.argos.aprobar(autorizar, false)
	admin.pedir(http.MethodPost, rutaCompletar, map[string]any{"codigo": codigo, "api": "https://argos-falso.example", "state": state}).
		problema(t, 422, "origen_no_permitido")
	if e.canjes() != 0 || e.tokensIntegracion() != tokens {
		t.Fatal("un api fuera de la lista no recibe el canje ni el token")
	}
	admin.pedir(http.MethodPost, rutaCompletar, map[string]any{"codigo": codigo, "api": e.argos.srv.URL, "state": state}).problema(t, 422, "state_invalido")

	// El administrador rechazó en Argos: 422 rechazado, state gastado, sin canje.
	autorizar = e.iniciarAutorizacion(admin)
	_, _, state = e.argos.aprobar(autorizar, false)
	admin.pedir(http.MethodPost, rutaCompletar, map[string]any{"error": "rechazado", "state": state}).problema(t, 422, "rechazado")
	admin.pedir(http.MethodPost, rutaCompletar, map[string]any{"error": "rechazado", "state": state}).problema(t, 422, "state_invalido")
	if e.canjes() != 0 {
		t.Fatal("un rechazo no llama a Argos")
	}

	// code_verifier que no corresponde: Argos responde 401 codigo_invalido → 422 codigo_invalido,
	// sin conector y con el iris_pat_ entregado revocado.
	conectores := e.contar(`SELECT count(*) FROM integracion.conectores`)
	autorizar = e.iniciarAutorizacion(admin)
	codigo, api, state := e.argos.aprobar(autorizar, true)
	admin.pedir(http.MethodPost, rutaCompletar, map[string]any{"codigo": codigo, "api": api, "state": state}).problema(t, 422, "codigo_invalido")
	if e.canjes() != 1 || e.contar(`SELECT count(*) FROM integracion.conectores`) != conectores {
		t.Fatal("un verifier incorrecto no deja conector")
	}
	if s := e.estadoToken(e.tokenDelUltimoCanje()); !s.revocado {
		t.Fatal("el iris_pat_ del canje fallido se revoca")
	}
	if e.contar(`SELECT count(DISTINCT error->>'type') FROM auditoria.acciones WHERE accion = 'integraciones.completar_autorizacion' AND resultado = 'error'
		AND error->>'type' IN ('urn:iris:problema:origen_no_permitido', 'urn:iris:problema:rechazado', 'urn:iris:problema:codigo_invalido')`) != 3 {
		t.Fatal("los tres rechazos quedan auditados")
	}
}

func TestAutorizacionDobleEnvio(t *testing.T) {
	e := nuevoEntorno(t)
	admin := e.usuario("admin")

	// Con la misma Idempotency-Key (la vuelta la deriva del state): la misma respuesta, un canje.
	autorizar := e.iniciarAutorizacion(admin)
	codigo, api, state := e.argos.aprobar(autorizar, false)
	cuerpo := map[string]any{"codigo": codigo, "api": api, "state": state}
	r1 := admin.pedir(http.MethodPost, rutaCompletar, cuerpo, "Idempotency-Key", "vuelta-"+state[:16])
	r2 := admin.pedir(http.MethodPost, rutaCompletar, cuerpo, "Idempotency-Key", "vuelta-"+state[:16])
	if r1.status != 201 || r2.status != 201 || string(r1.crudo) != string(r2.crudo) || r2.h.Get("Idempotent-Replayed") == "" {
		t.Fatalf("doble envío con clave: %d %s / %d %s", r1.status, r1.crudo, r2.status, r2.crudo)
	}
	if e.canjes() != 1 {
		t.Fatalf("canjes: %d", e.canjes())
	}

	// Sin clave y a la vez: uno conecta y el otro ve el state usado; Argos recibe un solo canje.
	autorizar = e.iniciarAutorizacion(admin)
	codigo, api, state = e.argos.aprobar(autorizar, false)
	cuerpo = map[string]any{"codigo": codigo, "api": api, "state": state}
	var wg sync.WaitGroup
	estados := make([]int, 2)
	tipos := make([]any, 2)
	for i := range estados {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := admin.pedir(http.MethodPost, rutaCompletar, cuerpo)
			estados[i], tipos[i] = r.status, r.cuerpo["type"]
		}()
	}
	wg.Wait()
	ok, repetido := 0, 0
	for i := range estados {
		switch {
		case estados[i] == 201:
			ok++
		case estados[i] == 422 && tipos[i] == "urn:iris:problema:state_invalido":
			repetido++
		}
	}
	if ok != 1 || repetido != 1 || e.canjes() != 2 {
		t.Fatalf("a la vez: %v %v, canjes %d", estados, tipos, e.canjes())
	}

	// Repetir la clave de «Conectar» no inicia otra autorización.
	antes := e.contar(`SELECT count(*) FROM integracion.autorizaciones`)
	admin.pedir(http.MethodPost, rutaAutorizar, map[string]any{"panelUrl": "https://panel.argos.prueba"}, "Idempotency-Key", "boton-1")
	admin.pedir(http.MethodPost, rutaAutorizar, map[string]any{"panelUrl": "https://panel.argos.prueba"}, "Idempotency-Key", "boton-1").
		problema(t, 409, "idempotencia_no_repetible")
	if e.contar(`SELECT count(*) FROM integracion.autorizaciones`) != antes+1 {
		t.Fatal("la clave repetida no crea otra autorización")
	}
}

func TestAutorizarValidaEntrada(t *testing.T) {
	e := nuevoEntorno(t)
	admin := e.usuario("admin")
	for _, panel := range []string{"ftp://argos.prueba", "https://usuario:clave@argos.prueba", "https://argos.prueba/?x=1", "argos.prueba"} {
		if r := admin.pedir(http.MethodPost, rutaAutorizar, map[string]any{"panelUrl": panel}); r.status != 422 {
			t.Fatalf("%s: %d %s", panel, r.status, r.crudo)
		}
	}
	// Sin permiso de conectar integraciones: 403.
	if r := e.usuario("supervisor").pedir(http.MethodPost, rutaAutorizar, map[string]any{"panelUrl": "https://panel.argos.prueba"}); r.status != 403 {
		t.Fatalf("sin permiso: %d %s", r.status, r.crudo)
	}
}

// El canje del enlace de §1.3 también guarda panel_url (0.4.0) aparte de la API.
func TestEnlaceGuardaPanelURL(t *testing.T) {
	e := nuevoEntorno(t)
	e.argos.con(func() { e.argos.panelURL = "https://argos.kns.prueba/" })
	admin := e.usuario("admin")
	r := admin.pedir(http.MethodPost, "/api/v1/integraciones/argosisp/conectar", map[string]any{"enlace": e.argos.enlace()})
	if r.status != 201 || r.cuerpo["panelUrl"] != "https://argos.kns.prueba" || r.cuerpo["apiUrl"] != e.argos.srv.URL || r.cuerpo["origen"] != "enlace_remoto" {
		t.Fatalf("conectar: %d %s", r.status, r.crudo)
	}
	// Un panel_url que no es un origen http(s) limpio se ignora (los enlaces usan la API).
	e.codigoNuevo()
	e.argos.con(func() { e.argos.panelURL = "javascript:alert(1)" })
	r = admin.pedir(http.MethodPost, "/api/v1/integraciones/argosisp/conectar", map[string]any{"enlace": e.argos.enlace()})
	if r.status != 201 || r.cuerpo["panelUrl"] != nil || r.cuerpo["urlPublica"] != e.argos.srv.URL {
		t.Fatalf("panel_url inválido: %d %s", r.status, r.crudo)
	}
}

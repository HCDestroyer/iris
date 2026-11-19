package argos_test

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
)

// Contrato argosisp.md 0.8.1 §2.1 (A29): sin servicios.ver_red global, la búsqueda de clientes manda
// buscar_red=false a un Argos ≥ 1.21.0, que deja fuera de q la IPv4 vigente, los prefijos IPv6 y el
// user_ppp de los servicios. Con un Argos anterior no se manda (queda el límite conocido de 0.8.0) y con
// servicios.ver_red global tampoco.

// buscar081 busca q con esos permisos (vacío = global) y devuelve los ids encontrados y el
// buscar_red que recibió Argos ("" = no vino).
func (e *entorno) buscar081(usuario int64, q string, permisos map[nucleo.Permiso][]contexto.Alcance) (ids map[int64]bool, buscarRed string) {
	e.t.Helper()
	var antes int
	e.argos.con(func() { antes = len(e.argos.buscarRed) })
	r, err := e.mod.BuscarClientes.Ejecutar(conAlcances(e.pool, usuario, permisos), argos.EntradaBuscarClientes{Q: q, Limit: 20})
	if err != nil {
		e.t.Fatalf("buscar %q: %v", q, err)
	}
	ids = map[int64]bool{}
	for _, c := range r.Data {
		ids[c.ID] = true
	}
	e.argos.con(func() {
		if len(e.argos.buscarRed) != antes+1 {
			e.t.Fatalf("buscar %q debía llamar una vez a Argos: %d", q, len(e.argos.buscarRed)-antes)
		}
		buscarRed = e.argos.buscarRed[antes]
	})
	return ids, buscarRed
}

func TestContrato081BuscarSinRed(t *testing.T) {
	const cliente = 981
	const userPPP = "cli1201-4410" // el user_ppp del primer servicio de sembrar080
	sinRed := map[nucleo.Permiso][]contexto.Alcance{nucleo.PermClientesVer: nil}
	redGlobal := map[nucleo.Permiso][]contexto.Alcance{nucleo.PermClientesVer: nil, nucleo.PermServiciosVerRed: nil}
	redZona := map[nucleo.Permiso][]contexto.Alcance{nucleo.PermClientesVer: nil, nucleo.PermServiciosVerRed: {{ZonaID: 3}}}

	t.Run("argos 1.21.0", func(t *testing.T) {
		e := nuevoEntorno(t)
		e.argos.con(func() { e.argos.version = "1.21.0" })
		_, _, _ = e.conectado()
		e.sembrar080(cliente)
		_, usuario := e.usuarioConID("lectura")

		// Sin servicios.ver_red: buscar_red=false y el user_ppp ya no encuentra al cliente.
		ids, br := e.buscar081(usuario, userPPP, sinRed)
		if br != "false" || ids[cliente] {
			t.Fatalf("sin ver_red: buscar_red=%q, encontrados %v", br, ids)
		}
		// Por nombre sí, también con buscar_red=false.
		if ids, br := e.buscar081(usuario, "maría", sinRed); br != "false" || !ids[cliente] {
			t.Fatalf("por nombre sin ver_red: buscar_red=%q, encontrados %v", br, ids)
		}
		// La auditoría de la búsqueda lo anota (nunca el q en claro de un teléfono).
		if n := e.contar(`SELECT count(*) FROM auditoria.acciones WHERE accion = 'clientes.buscar' AND actor_id = $1
			AND (entrada->'auditoria'->>'buscar_red')::boolean = false`, usuario); n < 1 {
			t.Fatalf("la auditoría debía anotar buscar_red=false: %d", n)
		}
		// Alcance por zona (no global): también se manda.
		if ids, br := e.buscar081(usuario, userPPP, redZona); br != "false" || ids[cliente] {
			t.Fatalf("ver_red por zona: buscar_red=%q, encontrados %v", br, ids)
		}
		// Con servicios.ver_red global no se manda: Argos busca también en la red.
		if ids, br := e.buscar081(usuario, userPPP, redGlobal); br != "" || !ids[cliente] {
			t.Fatalf("ver_red global: buscar_red=%q, encontrados %v", br, ids)
		}
	})

	t.Run("argos 1.20.0", func(t *testing.T) {
		e := nuevoEntorno(t)
		e.argos.con(func() { e.argos.version = "1.20.0" })
		_, _, _ = e.conectado()
		e.sembrar080(cliente)
		_, usuario := e.usuarioConID("lectura")
		// Argos anterior: no se manda (lo ignoraría). Límite conocido: el user_ppp encuentra al cliente.
		if ids, br := e.buscar081(usuario, userPPP, sinRed); br != "" || !ids[cliente] {
			t.Fatalf("Argos 1.20.0 sin ver_red: buscar_red=%q, encontrados %v", br, ids)
		}
		if ids, br := e.buscar081(usuario, userPPP, redGlobal); br != "" || !ids[cliente] {
			t.Fatalf("Argos 1.20.0 con ver_red: buscar_red=%q, encontrados %v", br, ids)
		}
	})

	t.Run("argos sin version", func(t *testing.T) {
		// Argos 1.5.x no informa versión: Iris no sabe si lo soporta y no lo manda.
		e := nuevoEntorno(t)
		_, _, _ = e.conectado()
		e.sembrar080(cliente)
		_, usuario := e.usuarioConID("lectura")
		if _, br := e.buscar081(usuario, userPPP, sinRed); br != "" {
			t.Fatalf("sin versión de Argos: buscar_red=%q", br)
		}
	})
}

// El doble de Argos 1.21.0 acepta true, false, 1 y 0 y responde 422 validacion (field buscar_red) a
// cualquier otro valor, como el contrato; uno anterior lo ignora.
func TestContrato081BuscarRedInvalido(t *testing.T) {
	e := nuevoEntorno(t)
	pedir := func(valor string) (int, map[string]any) {
		t.Helper()
		req, _ := http.NewRequest(http.MethodGet, e.argos.srv.URL+"/api/v1/clientes?q=ana&buscar_red="+url.QueryEscape(valor), nil)
		req.Header.Set("Authorization", "Bearer "+tokenArgos)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		var cuerpo map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&cuerpo)
		return resp.StatusCode, cuerpo
	}
	e.argos.con(func() { e.argos.version = "1.21.0" })
	for _, v := range []string{"no", "2", "si", ""} {
		status, cuerpo := pedir(v)
		errores, _ := cuerpo["errors"].([]any)
		if status != http.StatusUnprocessableEntity || cuerpo["type"] != "urn:argosisp:problema:validacion" || len(errores) != 1 ||
			errores[0].(map[string]any)["field"] != "buscar_red" {
			t.Fatalf("buscar_red=%q: %d %v", v, status, cuerpo)
		}
	}
	for _, v := range []string{"true", "false", "1", "0"} {
		if status, cuerpo := pedir(v); status != http.StatusOK {
			t.Fatalf("buscar_red=%q: %d %v", v, status, cuerpo)
		}
	}
	e.argos.con(func() { e.argos.version = "1.20.0" })
	if status, cuerpo := pedir("no"); status != http.StatusOK {
		t.Fatalf("Argos 1.20.0 debía ignorar buscar_red: %d %v", status, cuerpo)
	}
	if !strings.HasPrefix(argos.VersionBuscarSinRed, "1.21.") || argos.SoportaBuscarSinRed(nil) {
		t.Fatal("VersionBuscarSinRed")
	}
}

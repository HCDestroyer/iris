package argos_test

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
)

// Contrato argosisp.md 0.8.1 §2.1 (A29): sin servicios.ver_red global (también con alcance por
// zona), la búsqueda de clientes manda SIEMPRE buscar_red=false, sin depender de la versión que
// informó Argos (falla cerrado). Argos ≥ 1.21.0 deja fuera de q la IPv4 vigente, los prefijos IPv6 y
// el user_ppp de los servicios; uno anterior lo ignora (límite conocido de 0.8.0). Con
// servicios.ver_red global no se manda. La auditoría anota efectivo, ignorado o no_enviado.

// buscar081 busca q con esos permisos (vacío = global) y devuelve los ids encontrados, el
// buscar_red que recibió Argos ("" = no vino) y lo que anotó la auditoría de esa búsqueda.
func (e *entorno) buscar081(usuario int64, q string, permisos map[nucleo.Permiso][]contexto.Alcance) (ids map[int64]bool, buscarRed, anotado string) {
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
	anotado = e.consultarTexto(`SELECT coalesce(entrada->'auditoria'->>'buscar_red', '') FROM auditoria.acciones
		WHERE accion = 'clientes.buscar' AND actor_id = $1 ORDER BY id DESC LIMIT 1`, usuario)
	return ids, buscarRed, anotado
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
		if ids, br, an := e.buscar081(usuario, userPPP, sinRed); br != "false" || ids[cliente] || an != "efectivo" {
			t.Fatalf("sin ver_red: buscar_red=%q, auditoría %q, encontrados %v", br, an, ids)
		}
		// Por nombre sí, también con buscar_red=false.
		if ids, br, _ := e.buscar081(usuario, "maría", sinRed); br != "false" || !ids[cliente] {
			t.Fatalf("por nombre sin ver_red: buscar_red=%q, encontrados %v", br, ids)
		}
		// Alcance por zona (no global): también se manda.
		if ids, br, an := e.buscar081(usuario, userPPP, redZona); br != "false" || ids[cliente] || an != "efectivo" {
			t.Fatalf("ver_red por zona: buscar_red=%q, auditoría %q, encontrados %v", br, an, ids)
		}
		// Con servicios.ver_red global no se manda: Argos busca también en la red.
		if ids, br, an := e.buscar081(usuario, userPPP, redGlobal); br != "" || !ids[cliente] || an != "no_enviado" {
			t.Fatalf("ver_red global: buscar_red=%q, auditoría %q, encontrados %v", br, an, ids)
		}
	})

	t.Run("argos 1.20.0", func(t *testing.T) {
		e := nuevoEntorno(t)
		e.argos.con(func() { e.argos.version = "1.20.0" })
		_, _, _ = e.conectado()
		e.sembrar080(cliente)
		_, usuario := e.usuarioConID("lectura")
		// Argos anterior: se manda igual (falla cerrado) y el doble lo ignora. Límite conocido: el
		// user_ppp encuentra al cliente; la auditoría lo anota como ignorado.
		if ids, br, an := e.buscar081(usuario, userPPP, sinRed); br != "false" || !ids[cliente] || an != "ignorado" {
			t.Fatalf("Argos 1.20.0 sin ver_red: buscar_red=%q, auditoría %q, encontrados %v", br, an, ids)
		}
		if ids, br, an := e.buscar081(usuario, userPPP, redGlobal); br != "" || !ids[cliente] || an != "no_enviado" {
			t.Fatalf("Argos 1.20.0 con ver_red: buscar_red=%q, auditoría %q, encontrados %v", br, an, ids)
		}
	})

	t.Run("argos sin version", func(t *testing.T) {
		// Argos 1.5.x no informa versión: se manda igual y lo ignora.
		e := nuevoEntorno(t)
		_, _, _ = e.conectado()
		e.sembrar080(cliente)
		_, usuario := e.usuarioConID("lectura")
		if ids, br, an := e.buscar081(usuario, userPPP, sinRed); br != "false" || !ids[cliente] || an != "ignorado" {
			t.Fatalf("sin versión de Argos: buscar_red=%q, auditoría %q, encontrados %v", br, an, ids)
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

// Con B4 (sin_personal), B5 (cuentas bancarias) y B7 (conexión de un paso) en Iris, Iris informa el
// contrato 0.8.1: en el canje y en el PUT …/contrato. Con un Argos que habla 0.8.1 el conector queda
// en ok; con uno de contrato 0.7.0 también (§4.1: cada función nueva depende del contrato del otro).
func TestContratoIris081SeInforma(t *testing.T) {
	if argos.ContratoIris != "0.8.1" {
		t.Fatalf("ContratoIris: %s", argos.ContratoIris)
	}
	e := nuevoEntorno(t)
	admin, id := e.conectado070()
	var canje map[string]any
	e.argos.con(func() { canje = e.argos.canjes[len(e.argos.canjes)-1] })
	if canje["contrato"] != "0.8.1" {
		t.Fatalf("el canje debía mandar contrato 0.8.1: %v", canje["contrato"])
	}
	e.argos.con(func() { e.argos.version, e.argos.contratoMax = "1.22.1", "0.8.1" })
	r := admin.pedir(http.MethodPost, "/api/v1/integraciones/conectores/"+strconv.FormatInt(id, 10)+"/revisar-version", map[string]any{})
	if r.status != http.StatusOK && r.status != http.StatusAccepted {
		t.Fatalf("revisar ahora: %d %s", r.status, r.crudo)
	}
	if _, err := e.mod.InformarContrato.Ejecutar(pruebas.ContextoSistema(e.pool), id); err != nil {
		t.Fatal(err)
	}
	var informados []string
	e.argos.con(func() { informados = append(informados, e.argos.informados...) })
	if len(informados) == 0 || informados[len(informados)-1] != "0.8.1" {
		t.Fatalf("el PUT …/contrato debía informar 0.8.1: %v", informados)
	}
	con := e.conectorPaginado(admin, id)
	if con["contratoInformado"] != "0.8.1" || con["versionEstado"] != "ok" || con["estado"] != "activo" {
		t.Fatalf("conector con Argos 1.22.1: %v", con)
	}
}

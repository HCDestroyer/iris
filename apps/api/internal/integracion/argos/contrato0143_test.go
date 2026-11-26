package argos_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/conector"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
)

// Una sola fuente de la versión que habla Iris (Config.ContratoIris o argos.ContratoIris): el canje,
// el PUT …/contrato, el token_portal, GET …/capacidades (Argos) y GET …/estado (personas) dicen y
// hacen lo mismo. Antes, el token_portal y las capacidades miraban la constante del paquete: con otra
// versión configurada, Iris informaba una y se comportaba según otra.
func TestVersionDeIrisUnaSolaFuente(t *testing.T) {
	constante := argos.ContratoIris
	for _, c := range []struct {
		nombre, config, version string
	}{
		{"constante", "", constante},
		{"con_portal", "0.9.0", "0.9.0"},
		{"sin_portal", "0.8.0", "0.8.0"},
	} {
		t.Run(c.nombre, func(t *testing.T) {
			portal := argos.SoportaPortal(&c.version)
			e := nuevoEntornoCon(t, func(cfg *argos.Config) { cfg.ContratoIris = c.config })
			e.argos.con(func() { e.argos.version = "1.30.0" })
			admin, id, _ := e.conectado()
			var canje map[string]any
			e.argos.con(func() { canje = e.argos.canjes[len(e.argos.canjes)-1] })
			if _, con := canje["token_portal"]; canje["contrato"] != c.version || con != portal {
				t.Fatalf("canje: contrato %v, token_portal %v (esperaba %s, %v)", canje["contrato"], con, c.version, portal)
			}
			// PUT …/contrato con la misma versión (el token_portal ya salió en el canje: no se repite).
			if _, err := e.mod.InformarContrato.Ejecutar(pruebas.ContextoSistema(e.pool), id); err != nil {
				t.Fatal(err)
			}
			var informados []string
			var portales int
			e.argos.con(func() { informados, portales = append(informados, e.argos.informados...), e.argos.portales })
			if len(informados) == 0 || informados[len(informados)-1] != c.version || portales != 0 {
				t.Fatalf("PUT …/contrato: %v (token_portal en %d)", informados, portales)
			}
			// Estado (personas).
			r := admin.pedir(http.MethodGet, "/api/v1/integraciones/argosisp/estado", nil)
			caps, _ := r.cuerpo["capacidades"].(map[string]any)
			if r.status != http.StatusOK || r.cuerpo["contratoIris"] != c.version || caps["portal_argos"] != portal ||
				caps["solicitudes_servicio"] != portal || caps["adjuntos_ticket"] != portal {
				t.Fatalf("estado: %d %s", r.status, r.crudo)
			}
			// Capacidades (el iris_pat_ que el canje entregó al panel de Argos).
			token, _ := canje["token"].(string)
			api := &clienteHTTP{t: t, base: e.api.URL, http: http.DefaultClient}
			r = api.pedir(http.MethodGet, argos.RutaCapacidades, nil, "Authorization", "Bearer "+token)
			caps, _ = r.cuerpo["capacidades"].(map[string]any)
			if r.status != http.StatusOK || r.cuerpo["contrato_iris"] != c.version || caps["portal_argos"] != portal {
				t.Fatalf("capacidades: %d %s", r.status, r.crudo)
			}
		})
	}
}

// §3.3 (0.14.0): un Argos que habla un contrato menor responde 422 contrato_no_soportado y no registra
// el de Iris. Iris informa entonces el de Argos (la menor de las dos) y, cuando Argos sube, la suya.
func TestContratoNoSoportadoInformaElDeArgos(t *testing.T) {
	e := nuevoEntornoCon(t, func(cfg *argos.Config) { cfg.ContratoIris = "0.14.3" })
	e.argos.con(func() { e.argos.version = "1.30.0" })
	_, id, _ := e.conectado()
	ctx := pruebas.ContextoSistema(e.pool)
	e.argos.con(func() { e.argos.contrato, e.argos.contratoMax, e.argos.noSoportaMayor = "0.14.2", "0.14.2", true })
	r, err := e.mod.InformarContrato.Ejecutar(ctx, id)
	if err != nil || r.Estado != argos.VersionOK || r.ContratoInformado == nil || *r.ContratoInformado != "0.14.2" ||
		r.ContratoRemoto == nil || *r.ContratoRemoto != "0.14.2" {
		t.Fatalf("informar a un Argos 0.14.2: %+v %v", r, err)
	}
	var rechazados, informados []string
	e.argos.con(func() {
		rechazados, informados = append(rechazados, e.argos.rechazados...), append(informados, e.argos.informados...)
	})
	if len(rechazados) != 1 || rechazados[0] != "0.14.3" || informados[len(informados)-1] != "0.14.2" {
		t.Fatalf("PUT …/contrato: rechazados %v, informados %v", rechazados, informados)
	}
	// Argos sube a 0.14.3: la revisión siguiente informa la de Iris.
	e.argos.con(func() { e.argos.contrato, e.argos.contratoMax = "0.14.3", "0.14.3" })
	if r, err = e.mod.InformarContrato.Ejecutar(ctx, id); err != nil || r.ContratoInformado == nil || *r.ContratoInformado != "0.14.3" {
		t.Fatalf("informar a un Argos 0.14.3: %+v %v", r, err)
	}
	e.argos.con(func() { rechazados = append(rechazados[:0], e.argos.rechazados...) })
	if len(rechazados) != 1 {
		t.Fatalf("sin rechazos nuevos: %v", rechazados)
	}
}

// Lo que Iris hace hacia Argos se enciende con el contrato acordado: la menor entre el de Argos y el
// que Argos aceptó de Iris. Con Argos en 0.14.0 y 0.8.1 aceptado, nada de la 0.9.0 en adelante.
func TestFuncionesSegunElContratoAcordado(t *testing.T) {
	e := nuevoEntorno(t)
	e.argos.con(func() { e.argos.version = "1.30.0" })
	admin, id, _ := e.conectado()
	ctx := nucleo.ConBD(pruebas.ContextoSistema(e.pool), e.pool)
	fijar := func(remoto, informado any) {
		t.Helper()
		if _, err := e.pool.Exec(context.Background(), `UPDATE integracion.conectores SET contrato_remoto = $2, contrato_informado = $3
			WHERE id = $1`, id, remoto, informado); err != nil {
			t.Fatal(err)
		}
	}
	comprobar := func(esperado bool) {
		t.Helper()
		claves, err := e.mod.ClavesPorOperacion(ctx, e.pool)
		if err != nil {
			t.Fatal(err)
		}
		titular, err := argos.TitularEmpresaDisponible(ctx, e.pool)
		if err != nil {
			t.Fatal(err)
		}
		equipos, err := e.mod.EquiposDelCliente(ctx, e.pool)
		if err != nil {
			t.Fatal(err)
		}
		contactos, aCargo, err := e.mod.SoportaContactos(ctx, e.pool)
		if err != nil {
			t.Fatal(err)
		}
		if claves != esperado || titular != esperado || equipos != esperado || contactos != esperado || aCargo != esperado {
			t.Fatalf("esperaba %v: claves %v, titular %v, equipos %v, contactos %v/%v", esperado, claves, titular, equipos, contactos, aCargo)
		}
		// Lo de 0.7.0 sigue con cualquiera de las dos.
		if ok, err := e.mod.ApruebaComprobantes(ctx, e.pool); err != nil || !ok {
			t.Fatalf("aprobar comprobantes: %v %v", ok, err)
		}
		// El panel muestra lo apagado según el mismo contrato.
		r := admin.pedir(http.MethodGet, "/api/v1/integraciones/argosisp/estado", nil)
		apagado := false
		lista, _ := r.cuerpo["funcionesApagadas"].([]any)
		for _, f := range lista {
			if m, _ := f.(map[string]any); m["codigo"] == "titular_empresa" {
				apagado = true
			}
		}
		if apagado == esperado {
			t.Fatalf("funcionesApagadas (titular_empresa apagado = %v): %s", apagado, r.crudo)
		}
	}
	fijar("0.14.1", "0.8.1")
	comprobar(false)
	fijar("0.14.1", "0.14.3")
	comprobar(true)
	fijar("0.14.1", nil) // sin informe guardado: vale el de Argos
	comprobar(true)
	fijar("0.8.1", "0.14.3")
	comprobar(false)
}

// La regla en SQL (integracion.contrato_acordado) es la misma que conector.ContratoAcordado.
func TestContratoAcordadoSQLIgualAGo(t *testing.T) {
	pool := pruebas.Pool(t)
	valores := []any{nil, "0.8.1", "0.9.0", "0.14.3", "0.14.3-rc1", "v0.14.2", "0.9", "1.0.0", "raro", "0.10.0+b"}
	for _, a := range valores {
		for _, b := range valores {
			var sql *string
			if err := pool.QueryRow(context.Background(), `SELECT integracion.contrato_acordado($1::text, $2::text)`, a, b).Scan(&sql); err != nil {
				t.Fatal(err)
			}
			var ra, rb *string
			if s, ok := a.(string); ok {
				ra = &s
			}
			if s, ok := b.(string); ok {
				rb = &s
			}
			g := conector.ContratoAcordado(ra, rb)
			if (g == nil) != (sql == nil) || (g != nil && *g != *sql) {
				t.Errorf("contrato_acordado(%v, %v): sql %v, go %v", a, b, texto(sql), texto(g))
			}
		}
	}
}

func texto(s *string) string {
	if s == nil {
		return "<nil>"
	}
	return *s
}

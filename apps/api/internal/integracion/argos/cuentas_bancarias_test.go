package argos_test

import (
	"net/http"
	"testing"

	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/conector"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
)

// GET /cuentas-bancarias contra un Argos HTTP: Argos 1.17.0 responde un arreglo plano (sin paginar,
// ignora limit) con alias y banco_nombre. Iris pide solo ?activa=true y lo lee. Un 200 que no puede
// decodificar (cambió un tipo) es «respuesta ilegible», distinguible de una caída.
func TestCuentasBancariasArgosHTTP(t *testing.T) {
	e := nuevoEntorno(t)
	e.conectado070()
	var consulta, empresa string
	cuerpo := `[
		{"id":3,"empresa_id":1,"banco_id":1,"banco_nombre":"Banco Industrial","numero":"000-123456-7","alias":"Monetaria","moneda":"GTQ","activa":true,
		 "cuenta_id":11,"cuenta_codigo":"1.1.02.01","version":1,"created_at":"2026-01-05T15:00:00Z","updated_at":"2026-01-05T15:00:00Z"},
		{"id":5,"empresa_id":1,"banco_id":2,"banco_nombre":"Banrural","numero":"3-445-00987-6","alias":"","moneda":"GTQ","activa":true}]`
	status := http.StatusOK
	e.argos.rutaExtra = func(w http.ResponseWriter, r *http.Request) {
		consulta, empresa = r.URL.RawQuery, r.Header.Get("X-Empresa-Id")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(cuerpo))
	}
	ctx := nucleo.ConBD(pruebas.ContextoSistema(e.pool), e.pool)
	at := argos.AtencionIris{Actor: "usuario:1", Conversacion: "iris:conversacion:1", EmpresaID: 1}

	cs, err := e.mod.CuentasBancarias(ctx, at)
	if err != nil {
		t.Fatal(err)
	}
	if consulta != "activa=true" || empresa != "1" {
		t.Fatalf("la petición: ?%s con X-Empresa-Id %q (sin limit: Argos no pagina)", consulta, empresa)
	}
	if len(cs) != 2 || cs[0].Nombre != "Monetaria" || cs[0].Banco != "Banco Industrial" || !cs[0].RecibeDe(1) || cs[1].Nombre != "Banrural …9876" {
		t.Fatalf("arreglo plano de Argos: %+v", cs)
	}

	// Argos cambia un tipo: 200 ilegible. No es una caída de Argos.
	cuerpo = `[{"id":3,"empresa_id":1,"banco_nombre":"Banco Industrial","numero":"1","alias":"Monetaria","moneda":"GTQ","activa":"true"}]`
	if _, err := e.mod.CuentasBancarias(ctx, at); !conector.EsIlegible(err) {
		t.Fatalf("un 200 que no se puede leer debía ser ilegible: %v", err)
	}
	// Un 503 de verdad de Argos: caído y no ilegible.
	status, cuerpo = http.StatusServiceUnavailable, `{}`
	if _, err := e.mod.CuentasBancarias(ctx, at); !conector.EsCaido(err) || conector.EsIlegible(err) {
		t.Fatalf("un 503 de Argos es una caída: %v", err)
	}
}

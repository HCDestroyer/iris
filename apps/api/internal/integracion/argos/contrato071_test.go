package argos_test

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
)

// Contrato Iris ↔ ArgosISP 0.7.1 (A24). Es aditivo: Iris informa 0.7.1 y sigue trabajando igual con
// un Argos 1.15.x, que solo conoce 0.7.0.

// Iris informa 0.7.1. Un Argos 1.15.0 (contrato 0.7.0) sigue siendo compatible: «Revisar ahora» lo
// deja en ok, con sus capacidades, y solo informa las dos versiones.
func TestContrato071ConArgos115SigueCompatible(t *testing.T) {
	if argos.ContratoIris != "0.7.1" {
		t.Fatalf("ContratoIris: %s", argos.ContratoIris)
	}
	e := nuevoEntorno(t)
	admin, id := e.conectado070()
	revisar := func() map[string]any {
		t.Helper()
		r := admin.pedir(http.MethodPost, fmt.Sprintf("/api/v1/integraciones/conectores/%d/revisar-version", id), map[string]any{})
		if r.status != http.StatusOK && r.status != http.StatusAccepted {
			t.Fatalf("revisar ahora: %d %s", r.status, r.crudo)
		}
		if _, err := e.mod.InformarContrato.Ejecutar(pruebas.ContextoSistema(e.pool), id); err != nil {
			t.Fatal(err)
		}
		return e.conectorPaginado(admin, id)
	}
	con := revisar()
	if con["versionRemota"] != "1.15.0" || con["contratoRemoto"] != "0.7.0" || con["contratoInformado"] != "0.7.1" {
		t.Fatalf("lo que informa el conector: %v", con)
	}
	if con["versionEstado"] != "ok" || con["estado"] != "activo" || fmt.Sprint(con["capacidades"]) != "[inventario_por_tecnico aprobar_comprobantes]" {
		t.Fatalf("un Argos 1.15.0 quedó como no compatible: estado %v, versión %v, capacidades %v", con["estado"], con["versionEstado"], con["capacidades"])
	}
	var informados []string
	e.argos.con(func() { informados = append(informados, e.argos.informados...) })
	if len(informados) == 0 || informados[len(informados)-1] != "0.7.1" {
		t.Fatalf("Iris debía informar 0.7.1: %v", informados)
	}
	ctx := nucleo.ConBD(pruebas.ContextoSistema(e.pool), e.pool)
	if ok, err := e.mod.ApruebaComprobantes(ctx, e.pool); err != nil || !ok {
		t.Fatalf("con Argos 1.15.0 se sigue pudiendo aprobar: %v %v", ok, err)
	}
	// Con un Argos que ya habla 0.7.1, lo mismo.
	e.argos.con(func() { e.argos.version, e.argos.contratoMax = "1.16.0", "" })
	con = revisar()
	if con["versionRemota"] != "1.16.0" || con["contratoRemoto"] != "0.7.1" || con["versionEstado"] != "ok" {
		t.Fatalf("Argos 1.16.0: %v", con)
	}
}

// §7.1 (0.7.1): la forma exacta de X-Iris-Actor y X-Iris-Conversacion. Lo que Iris manda cumple, y
// algo fuera de formato no sale hacia Argos.
func TestContrato071FormatoDeCabeceras(t *testing.T) {
	e := nuevoEntorno(t)
	_, _ = e.conectado070()
	ctx := nucleo.ConBD(pruebas.ContextoSistema(e.pool), e.pool)
	for _, at := range []argos.AtencionIris{
		{Actor: "usuario:42 ", Conversacion: "iris:conversacion:1"},
		{Actor: "usuario:", Conversacion: "iris:conversacion:1"},
		{Actor: "token:42", Conversacion: "iris:conversacion:1"},
		{Actor: "usuario:42\r\nX-Otra: 1", Conversacion: "iris:conversacion:1"},
		{Actor: "usuario:42", Conversacion: "iris conversacion 1"},
		{Actor: "usuario:42", Conversacion: ""},
		{Actor: "asistente_ia", Conversacion: "iris:conversacion:1/../2"},
	} {
		if _, err := e.mod.ObtenerComprobante(ctx, at, 7001); !errors.Is(err, argos.ErrCabecerasIris) {
			t.Errorf("%q / %q debía rechazarse antes de llamar a Argos: %v", at.Actor, at.Conversacion, err)
		}
	}
	for _, at := range []argos.AtencionIris{
		{Actor: "usuario:42", Conversacion: "iris:conversacion:1", EmpresaID: 1},
		{Actor: "asistente_ia", Conversacion: "iris:conversacion:987654321", EmpresaID: 1},
	} {
		if _, err := e.mod.ObtenerComprobante(ctx, at, 7001); errors.Is(err, argos.ErrCabecerasIris) {
			t.Errorf("%q / %q cumple el formato", at.Actor, at.Conversacion)
		}
	}
}

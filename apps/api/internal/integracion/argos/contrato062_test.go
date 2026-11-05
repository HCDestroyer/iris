package argos_test

import (
	"net/http"
	"testing"

	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
)

// Contrato Iris ↔ ArgosISP 0.6.2: receptor_argos en el canje y en PUT …/contrato, y canje sin
// receptor rechazado.

// §1.3: un canje de Argos sin `webhook` es 422 webhook_invalido y el conector no se guarda. La
// petición sale antes de conocer el receptor (llega en la respuesta) y ya lleva receptor_argos: true.
func TestCanjeSinReceptorDeArgosEs422(t *testing.T) {
	e := nuevoEntorno(t)
	e.argos.con(func() { e.argos.sinWebhook = true })
	antes := e.contar(`SELECT count(*) FROM integracion.conectores`)
	admin := e.usuario("admin")
	r := admin.pedir(http.MethodPost, "/api/v1/integraciones/argosisp/conectar", map[string]any{"enlace": e.argos.enlace()})
	r.problema(t, 422, "webhook_invalido")
	if n := e.contar(`SELECT count(*) FROM integracion.conectores`); n != antes {
		t.Fatalf("no debía guardarse el conector: %d → %d", antes, n)
	}
	var canje map[string]any
	e.argos.con(func() { canje = e.argos.canjes[len(e.argos.canjes)-1] })
	if canje["receptor_argos"] != true {
		t.Fatalf("el canje debía mandar receptor_argos: true: %v", canje["receptor_argos"])
	}
	// El iris_pat_ entregado en el canje rechazado queda revocado.
	token, _ := canje["token"].(string)
	if token == "" || e.contar(`SELECT count(*) FROM nucleo.tokens_api WHERE hash = $1 AND revocado_en IS NOT NULL`, identidad.Hash256(token)) != 1 {
		t.Fatal("el token del canje sin receptor debe quedar revocado")
	}
}

// El canje con receptor manda receptor_argos: true y contrato 0.6.2.
func TestCanjeMandaReceptorArgos(t *testing.T) {
	e := nuevoEntorno(t)
	e.conectado()
	var canje map[string]any
	e.argos.con(func() { canje = e.argos.canjes[len(e.argos.canjes)-1] })
	if argos.ContratoIris != "0.6.2" || canje["receptor_argos"] != true || canje["contrato"] != argos.ContratoIris {
		t.Fatalf("canje: receptor_argos=%v contrato=%v", canje["receptor_argos"], canje["contrato"])
	}
}

// «Versión y compatibilidad»: el PUT …/contrato manda receptor_argos según lo guardado: true con
// el receptor de §6.6 (url y secreto cifrado) y false en un conector canjeado antes de B1.
func TestInformarContratoMandaReceptorArgos(t *testing.T) {
	e := nuevoEntorno(t)
	e.argos.con(func() { e.argos.version = "1.6.0" })
	_, id, _ := e.conectado()
	ctx := pruebas.ContextoSistema(e.pool)
	if _, err := e.mod.InformarContrato.Ejecutar(ctx, id); err != nil {
		t.Fatal(err)
	}
	var ultimo any
	e.argos.con(func() { ultimo = e.argos.receptores[len(e.argos.receptores)-1] })
	if ultimo != true {
		t.Fatalf("con receptor guardado: receptor_argos=%v", ultimo)
	}
	// Conector canjeado antes de B1: sin receptor guardado.
	if _, err := e.pool.Exec(ctx, `UPDATE integracion.conectores SET webhook_salida_url = NULL, webhook_salida_secreto_cifrado = NULL
		WHERE id = $1`, id); err != nil {
		t.Fatal(err)
	}
	if _, err := e.mod.InformarContrato.Ejecutar(ctx, id); err != nil {
		t.Fatal(err)
	}
	e.argos.con(func() { ultimo = e.argos.receptores[len(e.argos.receptores)-1] })
	if ultimo != false {
		t.Fatalf("sin receptor guardado: receptor_argos=%v", ultimo)
	}
}

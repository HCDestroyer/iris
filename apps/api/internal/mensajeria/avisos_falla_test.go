package mensajeria_test

import (
	"testing"

	"github.com/HCDestroyer/iris/apps/api/internal/mensajeria"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
)

// El aviso de una falla general (contrato con Argos 0.13.0 §3.11) sale como cualquier aviso, con las
// reglas de §6.5: con permiso de contacto queda en cola; sin permiso, bloqueado. Uno por cliente,
// incidente y tipo aunque el evento llegue dos veces.
func TestAvisoDeFallaGeneral(t *testing.T) {
	e := nuevoEntorno(t, nil)
	ctx := pruebas.ContextoSistema(e.bd)
	con := e.cliente(1, "+50255551234")
	e.exigir(e.consentir(con, "acepta", e.reloj.Add(-1)), 201)
	sin := e.cliente(1, "+50255551235")
	incidente := int64(e.contar(`SELECT count(*)::int FROM mensajeria.avisos`) + 1000)
	for range 2 {
		if err := e.m.AvisarFalla(ctx, mensajeria.EntradaAvisoFalla{Incidente: incidente, EmpresaID: 1, ClienteID: con, Tipo: "apertura"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := e.m.AvisarFalla(ctx, mensajeria.EntradaAvisoFalla{Incidente: incidente, EmpresaID: 1, ClienteID: con, Tipo: "cierre"}); err != nil {
		t.Fatal(err)
	}
	if err := e.m.AvisarFalla(ctx, mensajeria.EntradaAvisoFalla{Incidente: incidente, EmpresaID: 1, ClienteID: sin, Tipo: "apertura"}); err != nil {
		t.Fatal(err)
	}
	if n := e.contar(`SELECT count(*)::int FROM mensajeria.avisos WHERE cliente_id = $1 AND plantilla = $2 AND estado = 'en_cola'`, con, mensajeria.PlantillaFallaGeneral); n != 1 {
		t.Fatalf("apertura en cola, una vez: %d", n)
	}
	if n := e.contar(`SELECT count(*)::int FROM mensajeria.avisos WHERE cliente_id = $1 AND plantilla = $2`, con, mensajeria.PlantillaFallaGeneralResuelta); n != 1 {
		t.Fatalf("cierre: %d", n)
	}
	if n := e.contar(`SELECT count(*)::int FROM mensajeria.avisos WHERE cliente_id = $1 AND estado = 'bloqueado' AND motivo = 'sin_permiso'`, sin); n != 1 {
		t.Fatalf("sin permiso de contacto, bloqueado: %d", n)
	}
}

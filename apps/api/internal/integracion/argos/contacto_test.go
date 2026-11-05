package argos_test

import (
	"testing"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
)

// Mensajería (contrato 0.6.0 §6.2): el contacto sale de la instantánea y se relee de Argos solo si
// no existe o tiene más de 24 h; con Argos caído se usa la vencida (stale); un 404 la borra.
func TestContactoClienteRefrescaA24Horas(t *testing.T) {
	e := nuevoEntorno(t)
	ctx := pruebas.ContextoSistema(e.pool)
	// Sin conector y sin instantánea: no hay contacto.
	if c, err := e.mod.ContactoCliente(ctx, 470); err != nil || c.Existe {
		t.Fatalf("sin conector: %+v %v", c, err)
	}
	_, _, _ = e.conectado()
	e.sembrarComoArgos14(470)
	c, err := e.mod.ContactoCliente(ctx, 470)
	if err != nil || !c.Existe || c.Stale || !c.EnEmpresa(1) || len(c.Servicios) == 0 {
		t.Fatalf("primera lectura: %+v %v", c, err)
	}
	antes := e.argos.llamadasA("/api/v1/clientes/470")
	if _, err := e.mod.ContactoCliente(ctx, 470); err != nil || e.argos.llamadasA("/api/v1/clientes/470") != antes {
		t.Fatalf("con la instantánea vigente no debía llamar a Argos: %v", err)
	}
	// Más de 24 h: se relee.
	e.exec(`UPDATE integracion.argos_clientes SET sincronizado_en = now() - interval '25 hours' WHERE id = 470`)
	if _, err := e.mod.ContactoCliente(ctx, 470); err != nil || e.argos.llamadasA("/api/v1/clientes/470") != antes+1 {
		t.Fatalf("vencida: debía releer: %v", err)
	}
	// Vencida y Argos caído: la que hay, stale.
	e.exec(`UPDATE integracion.argos_clientes SET sincronizado_en = now() - interval '25 hours' WHERE id = 470`)
	e.argos.con(func() { e.argos.caido = true })
	c, err = e.mod.ContactoCliente(ctx, 470)
	if err != nil || !c.Existe || !c.Stale {
		t.Fatalf("Argos caído: %+v %v", c, err)
	}
	e.argos.con(func() { e.argos.caido = false })
	// Argos ya no lo tiene: sin contacto.
	e.exec(`UPDATE integracion.argos_clientes SET sincronizado_en = now() - interval '25 hours', fallo_en = NULL WHERE id = 470`)
	e.argos.con(func() { delete(e.argos.clientes, 470) })
	if c, err := e.mod.ContactoCliente(ctx, 470); err != nil || c.Existe {
		t.Fatalf("404: %+v %v", c, err)
	}
}

package notificaciones_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/integracion"
	"github.com/HCDestroyer/iris/apps/api/internal/notificaciones"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
)

// Contrato 0.14.0 §3.10: integracion.pendiente_clave_reutilizada avisa en la campana a quien
// administra las integraciones de la instalación (no a un supervisor), una vez por pendiente; en un
// cambio o un intento de recuperación, como peligro (alerta de seguridad). Sin correo.
func TestAvisoClaveReutilizada(t *testing.T) {
	x := nuevoEntornoAvisos(t, true)
	admin := x.persona([]int64{1}, "admin")
	sup := x.persona([]int64{1}, "supervisor")
	x.notif.SuscribirAvisos(x.relay.Bus, x.bd)
	emitir := func(pendiente int64, operacion string, seguridad bool) {
		t.Helper()
		if err := nucleo.EnTx(x.ctx, func(ctx context.Context, tx pgx.Tx) error {
			_, err := integracion.Insertar(ctx, tx, "integracion.pendiente_clave_reutilizada", emp(1), "", map[string]any{"pendiente_id": pendiente,
				"operacion": operacion, "entidad": "orden", "entidad_id": "77", "empresa_id": 1, "seguridad": seguridad})
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	emitir(910001, "consumir_material", false)
	emitir(910001, "consumir_material", false) // repetido: un solo aviso
	emitir(910002, "no_recuperado", true)
	if _, err := x.relay.Ejecutar(x.ctx); err != nil {
		t.Fatal(err)
	}
	avisos := x.avisosDe(admin, notificaciones.TipoAvisoArgosClaveReutilizada)
	if len(avisos) != 2 {
		t.Fatalf("dos avisos (uno por pendiente): %v", avisos)
	}
	niveles := map[string]string{}
	for _, a := range avisos {
		d := a["datos"].(map[string]any)
		niveles[d["operacion"].(string)] = a["nivel"].(string)
		if a["titulo"] != notificaciones.TituloArgosClaveReutilizada || a["enlace"] != notificaciones.EnlaceEstadoArgos {
			t.Fatalf("aviso: %v", a)
		}
	}
	if niveles["consumir_material"] != "alerta" || niveles["no_recuperado"] != "peligro" {
		t.Fatalf("niveles: %v", niveles)
	}
	if d := x.avisosDe(sup, notificaciones.TipoAvisoArgosClaveReutilizada); len(d) != 0 {
		t.Fatalf("el supervisor no lo recibe: %v", d)
	}
	if n := x.contar(`SELECT count(*) FROM notificaciones.correos WHERE tipo = $1`, notificaciones.TipoAvisoArgosClaveReutilizada); n != 0 {
		t.Fatalf("solo campana: %d correos", n)
	}
}

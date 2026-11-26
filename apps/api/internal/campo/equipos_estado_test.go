package campo_test

import (
	"context"
	"testing"

	"github.com/HCDestroyer/iris/apps/api/internal/campo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
)

// GET /ordenes/{id}/equipos/estado (0.12.0, B27): las unidades del servicio de la orden con su estado
// e historial según los avisos de Argos; no las de otro servicio ni las de otra empresa.
func TestEstadoEquiposDeLaOrden(t *testing.T) {
	e := nuevoEntornoActivacion(t, campo.ModoPendienteAgente)
	tec, ct := e.tecnicoEnEmpresa()
	o := e.instalacionPropia(tec, ct)
	pool := pruebas.Pool(t)
	ctx := context.Background()
	t.Cleanup(func() {
		for _, sql := range []string{"DELETE FROM integracion.argos_unidades_historial", "DELETE FROM integracion.argos_unidades"} {
			if _, err := pool.Exec(context.Background(), sql); err != nil {
				t.Error(err)
			}
		}
	})
	for _, sql := range []string{
		`INSERT INTO integracion.argos_unidades (unidad_id, empresa_id, servicio_id, estado, estado_en, entrante_id) VALUES
			(8101, 1, $1, 'en_comodato', now(), 1), (8102, 1, $1 + 1, 'revision', now(), 1), (8103, 2, $1, 'en_comodato', now(), 1)`,
		`INSERT INTO integracion.argos_unidades_historial (unidad_id, empresa_id, servicio_id, tipo, de, a, ocurrido_en, entrante_id) VALUES
			(8101, 1, $1, 'estado', 'disponible', 'en_comodato', now(), 1)`,
	} {
		if _, err := pool.Exec(ctx, sql, *o.ServicioID); err != nil {
			t.Fatal(err)
		}
	}
	us := deber(e.m.EstadoEquipos.Ejecutar(e.cs, campo.EntradaEquipoOrden{ID: o.ID}))(t)
	if len(us) != 1 || us[0].UnidadID != 8101 || *us[0].Estado != "en_comodato" || len(us[0].Historial) != 1 || *us[0].Historial[0].A != "en_comodato" {
		t.Fatalf("estado de los equipos de la orden: %+v", us)
	}
	// El técnico de la orden también lo ve (ordenes.ver_propias).
	if us := deber(e.m.EstadoEquipos.Ejecutar(ct, campo.EntradaEquipoOrden{ID: o.ID}))(t); len(us) != 1 {
		t.Fatalf("el técnico de la orden: %+v", us)
	}
	_, err := e.m.EstadoEquipos.Ejecutar(e.cs, campo.EntradaEquipoOrden{ID: o.ID + 100000})
	problema(t, err, 404, "no_encontrado")
}

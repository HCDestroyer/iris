package campo_test

import (
	"context"
	"testing"

	"github.com/HCDestroyer/iris/apps/api/internal/campo"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
)

// Contrato argosisp.md 0.15.0 §13.1 (B33): un servicio de cable (router null) no ofrece cambiar el
// plan por router ni activar por aprovisionamiento; el paquete muestra el tipo.
func TestTipoServicioCableEnLaOrden(t *testing.T) {
	e := nuevoEntornoActivacion(t, campo.ModoPendienteAgente)
	tec, ct := e.tecnicoEnEmpresa()
	pool := pruebas.Pool(t)
	o := e.instalacionPropia(tec, ct)
	if _, err := pool.Exec(context.Background(), `INSERT INTO integracion.argos_clientes (id, empresa_id, empresas, nombre, codigo, telefono, direccion, sincronizado_en)
		VALUES (990, 1, '{1}', 'Titular', 'C-990', '5512 3344', 'Zona 10', now()) ON CONFLICT (id) DO NOTHING`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(context.Background(), `INSERT INTO integracion.argos_servicios (id, empresa_id, cliente_id, plan_nombre, router_id, estado,
		tipo_servicio, componentes, sincronizado_en) VALUES ($1, 1, 990, 'TV Básica', NULL, 'pendiente_instalacion', 'cable', '{cable}', now())
		ON CONFLICT (id) DO UPDATE SET router_id = NULL, tipo_servicio = 'cable', componentes = '{cable}', estado = 'pendiente_instalacion'`, *o.ServicioID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM integracion.argos_servicios WHERE id = $1", *o.ServicioID)
	})

	// Activar: no se ofrece (Argos respondería 422 tipo_sin_router); se activa al cerrar.
	if st := e.activacion(o.ID); st.Disponible || st.NoDisponible == nil || *st.NoDisponible != argos.CodigoTipoSinRouter {
		t.Fatalf("activación de cable: %+v", st)
	}
	// La instantánea de otra empresa no cuenta (como en el paquete): con el servicio de cable en la
	// empresa 2, la orden de la empresa 1 no se bloquea por tipo_sin_router.
	if _, err := pool.Exec(context.Background(), "UPDATE integracion.argos_servicios SET empresa_id = 2 WHERE id = $1", *o.ServicioID); err != nil {
		t.Fatal(err)
	}
	if st := e.activacion(o.ID); st.NoDisponible != nil && *st.NoDisponible == argos.CodigoTipoSinRouter {
		t.Fatalf("la instantánea de otra empresa no decide: %+v", st)
	}
	if _, err := pool.Exec(context.Background(), "UPDATE integracion.argos_servicios SET empresa_id = 1 WHERE id = $1", *o.ServicioID); err != nil {
		t.Fatal(err)
	}
	r := e.lote(ct, op(campo.OpActivarServicio, o.ID, datosActivacion(false)))
	if r.Resultados[0].Error == nil || r.Resultados[0].Error.Codigo() != "activacion_no_disponible" {
		t.Fatalf("activar cable por el lote: %+v", r.Resultados[0])
	}

	// Paquete: el tipo del servicio y los planes no disponibles por tipo_sin_router (sin llamar a Argos).
	llamadas := []string{}
	m := campo.Nuevo(campo.Config{Planes: planesFalsos{llamadas: &llamadas}})
	paq := deber(m.Paquete(ct, campo.EntradaPaquete{Fecha: hoyEnGuatemala(12).Format("2006-01-02")}))(t)
	visto := false
	for _, x := range paq.Ordenes {
		if x.ID != o.ID {
			continue
		}
		visto = true
		if x.Servicio == nil || x.Servicio.TipoServicio != "cable" || !x.Servicio.SinRouter || x.Servicio.Router != "" {
			t.Fatalf("servicio de cable en el paquete: %+v", x.Servicio)
		}
		if x.Planes == nil || x.Planes.Disponible || x.Planes.Motivo == nil || *x.Planes.Motivo != argos.CodigoTipoSinRouter {
			t.Fatalf("planes de cable: %+v", x.Planes)
		}
	}
	if !visto || len(llamadas) != 0 {
		t.Fatalf("orden en el paquete %v, llamadas a GET /planes %v", visto, llamadas)
	}
}

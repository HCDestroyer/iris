package campo_test

import (
	"context"
	"testing"

	"github.com/HCDestroyer/iris/apps/api/internal/campo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
)

// limpiarUnidades borra la instantánea de equipos de Argos al terminar.
func limpiarUnidades(t *testing.T) {
	t.Cleanup(func() {
		for _, sql := range []string{"DELETE FROM integracion.argos_unidades_historial", "DELETE FROM integracion.argos_unidades"} {
			if _, err := pruebas.Pool(t).Exec(context.Background(), sql); err != nil {
				t.Error(err)
			}
		}
	})
}

// GET /ordenes/{id}/equipos/estado (0.12.0, B27): las unidades del servicio de la orden con su estado
// e historial según los avisos de Argos; no las de otro servicio ni las de otra empresa.
func TestEstadoEquiposDeLaOrden(t *testing.T) {
	e := nuevoEntornoActivacion(t, campo.ModoPendienteAgente)
	tec, ct := e.tecnicoEnEmpresa()
	o := e.instalacionPropia(tec, ct)
	pool := pruebas.Pool(t)
	ctx := context.Background()
	limpiarUnidades(t)
	for _, sql := range []string{
		`INSERT INTO integracion.argos_unidades (unidad_id, empresa_id, servicio_id, estado, estado_en, entrante_id) VALUES
			(8101, 1, $1, 'en_comodato', now(), 1), (8102, 1, $1 + 1, 'revision', now(), 1), (8103, 2, $1, 'en_comodato', now(), 1)`,
		`INSERT INTO integracion.argos_unidades_historial (unidad_id, empresa_id, servicio_id, tipo, de, a, ocurrido_en, recibido_en, entrante_id) VALUES
			(8101, 1, $1, 'estado', 'disponible', 'en_comodato', now(), now(), 1)`,
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

// MEDIA 2 de #301 (contrato §3.10): una unidad que la orden A retiró y que después Argos instaló en
// otro cliente no expone el servicio ni la orden del otro cliente: el historial se recorta a los
// renglones del servicio de A, servicioId va en null si es otro, y ordenId solo si es A.
func TestEstadoEquiposNoExponeOtroCliente(t *testing.T) {
	e := nuevoEntornoActivacion(t, campo.ModoPendienteAgente)
	tec, ct := e.tecnicoEnEmpresa()
	o := e.instalacionPropia(tec, ct)
	pool := pruebas.Pool(t)
	ctx := context.Background()
	limpiarUnidades(t)
	servicio, otroServicio, otraOrden := *o.ServicioID, *o.ServicioID+50000, o.ID+777777
	for _, c := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO campo.orden_equipos (id, orden_id, accion, unidad_id, ocurrido_en) VALUES (gen_random_uuid(), $1, 'retirar', 8201, now())`,
			[]any{o.ID}},
		// Hoy la unidad está en comodato en otro servicio (otro cliente).
		{`INSERT INTO integracion.argos_unidades (unidad_id, empresa_id, servicio_id, estado, estado_en, servicio_en, entrante_id)
			VALUES (8201, 1, $1, 'en_comodato', now(), now(), 1)`, []any{otroServicio}},
		// Historial: entra y sale del servicio de A (por A y por otra orden), y luego entra al otro cliente.
		{`INSERT INTO integracion.argos_unidades_historial (unidad_id, empresa_id, servicio_id, tipo, de, a, origen, origen_id, ocurrido_en,
				recibido_en, entrante_id) VALUES
			(8201, 1, $1, 'entra', NULL, NULL, 'iris_orden', $4, now() - interval '3 hours', now() - interval '3 hours', 11),
			(8201, 1, $1, 'sale', NULL, NULL, 'iris_orden', $3, now() - interval '2 hours', now() - interval '2 hours', 12),
			(8201, 1, $2, 'entra', NULL, NULL, 'iris_orden', $4, now() - interval '1 hour', now() - interval '1 hour', 13),
			(8201, 1, $2, 'estado', 'disponible', 'en_comodato', now(), now(), 14),
			(8201, 1, NULL, 'estado', 'en_comodato', 'disponible', now(), now(), 15)`,
			[]any{servicio, otroServicio, o.ID, otraOrden}},
	} {
		if _, err := pool.Exec(ctx, c.sql, c.args...); err != nil {
			t.Fatal(err)
		}
	}
	us := deber(e.m.EstadoEquipos.Ejecutar(e.cs, campo.EntradaEquipoOrden{ID: o.ID}))(t)
	if len(us) != 1 || us[0].UnidadID != 8201 {
		t.Fatalf("la unidad retirada por la orden: %+v", us)
	}
	u := us[0]
	if u.ServicioID != nil {
		t.Fatalf("servicioId de otro cliente expuesto: %d", *u.ServicioID)
	}
	if len(u.Historial) != 2 {
		t.Fatalf("el historial debía tener solo los renglones del servicio de la orden: %+v", u.Historial)
	}
	for _, h := range u.Historial {
		if h.ServicioID == nil || *h.ServicioID != servicio {
			t.Fatalf("renglón de otro servicio expuesto: %+v", h)
		}
	}
	sale, entra := u.Historial[0], u.Historial[1]
	if sale.Tipo != "sale" || sale.OrdenID == nil || *sale.OrdenID != o.ID {
		t.Fatalf("la salida por esta orden lleva su ordenId: %+v", sale)
	}
	if entra.Tipo != "entra" || entra.OrdenID != nil {
		t.Fatalf("ordenId de otra orden expuesto: %+v", entra)
	}
}

// BAJA 5 de #301: acceso al GET. Otra empresa, un técnico ajeno (sin ordenes.ver) y alguien con
// ordenes.ver limitado a otra zona o cuadrilla reciben 404, como si la orden no existiera.
func TestEstadoEquiposAcceso(t *testing.T) {
	e := nuevoEntornoActivacion(t, campo.ModoPendienteAgente)
	tec, ct := e.tecnicoEnEmpresa()
	o := e.instalacionPropia(tec, ct)
	limpiarUnidades(t)
	if _, err := pruebas.Pool(t).Exec(context.Background(), `INSERT INTO integracion.argos_unidades (unidad_id, empresa_id, servicio_id,
		estado, entrante_id) VALUES (8301, 1, $1, 'en_comodato', 1)`, *o.ServicioID); err != nil {
		t.Fatal(err)
	}
	ver := func(ctx context.Context) error {
		_, err := e.m.EstadoEquipos.Ejecutar(ctx, campo.EntradaEquipoOrden{ID: o.ID})
		return err
	}
	if err := ver(e.cs); err != nil {
		t.Fatalf("el supervisor de la empresa: %v", err)
	}
	// Otra empresa activa (el mismo supervisor, con la empresa 2).
	problema(t, ver(pruebas.ContextoUsuarioEnEmpresa(pruebas.Pool(t), e.sup, 2, permisosDe("supervisor")...)), 404, "no_encontrado")
	// Un técnico de la misma empresa sin ordenes.ver y sin la orden asignada.
	_, ajeno := e.tecnicoEnEmpresa()
	problema(t, ver(ajeno), 404, "no_encontrado")
	// ordenes.ver (y todo lo del supervisor) limitado a otra zona o a otra cuadrilla.
	for _, alcance := range []contexto.Alcance{{ZonaID: 987654}, {CuadrillaID: 987654}} {
		mapa := map[string][]contexto.Alcance{}
		for _, pm := range permisosDe("supervisor") {
			mapa[string(pm)] = []contexto.Alcance{alcance}
		}
		problema(t, ver(contexto.ConActor(e.cs, contexto.ActorDe(e.cs), contexto.NuevosPermisos(mapa))), 404, "no_encontrado")
	}
}

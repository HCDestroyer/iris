package campo_test

import (
	"context"
	"testing"

	"github.com/HCDestroyer/iris/apps/api/internal/campo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
)

// Contrato Iris ↔ ArgosISP 0.5.0 (I1): orden.cerrada congela `tecnico { id_externo }` del vínculo de
// la ficha (null sin vínculo) y la modalidad de cada material; argos.datos_cambiados marca las órdenes
// abiertas «dato de Argos cambió» sin pisar lo copiado.
func TestContrato050EnCampo(t *testing.T) {
	pool := pruebas.Pool(t)
	ctx := context.Background()
	e := nuevoEntorno()
	d, tec := despacho(t), tecnico(t)
	cd, ct := como(t, d, "despacho"), como(t, tec, "tecnico")
	cerrar := func() map[string]any {
		t.Helper()
		o := programada(t, e, cd, "mantenimiento", tec)
		o = deber(e.m.Llegar.Ejecutar(ct, campo.EntradaPosicion{ID: o.ID, Version: o.Version}))(t)
		completarChecklist(t, e, ct, o.ID)
		deber(e.m.UsarMaterial.Ejecutar(ct, campo.EntradaUsarMaterial{ID: o.ID, ProductoID: 41, Cantidad: 2}))(t)
		o = deber(e.m.VerOrden.Ejecutar(cd, campo.EntradaVerOrden{ID: o.ID}))(t).Orden
		deber(e.m.CerrarOrden.Ejecutar(ct, campo.EntradaCerrarOrden{ID: o.ID, Version: o.Version}))(t)
		return ultimo(t, eventosDe(t, o.ID), "orden.cerrada")
	}
	if p := cerrar(); p["tecnico"] != nil {
		t.Fatalf("sin vínculo, tecnico null: %v", p["tecnico"])
	}
	externo := "u-" + pruebas.Unico()
	if _, err := pool.Exec(ctx, `UPDATE agenda.tecnicos SET argos_id_externo = $2, argos_vinculado_en = now() WHERE usuario_id = $1`, tec, externo); err != nil {
		t.Fatal(err)
	}
	p := cerrar()
	if tc, _ := p["tecnico"].(map[string]any); tc["id_externo"] != externo {
		t.Fatalf("tecnico del vínculo: %v", p["tecnico"])
	}
	mat := p["materiales"].([]any)[0].(map[string]any)
	if _, ok := mat["modalidad"]; !ok || mat["modalidad"] != nil {
		t.Fatalf("modalidad desconocida (null) en el material: %v", mat)
	}

	// argos.datos_cambiados del servicio: marca solo las abiertas de ese servicio (acumula campos).
	abierta := crearOrden(t, e, cd, "visita")
	servicio := int64(880000) + abierta.ID
	if _, err := pool.Exec(ctx, `UPDATE campo.ordenes SET servicio_id = $2 WHERE id = $1`, abierta.ID, servicio); err != nil {
		t.Fatal(err)
	}
	for _, campos := range [][]string{{"servicio.referencia"}, {"servicio.direccion", "servicio.referencia"}} {
		deber(e.m.MarcarDatosArgos.Ejecutar(pruebas.ContextoSistema(pool), campo.EntradaDatosArgos{
			Entidad: "servicio", ServicioID: &servicio, Campos: campos, EntranteID: 5}))(t)
	}
	v := deber(e.m.VerOrden.Ejecutar(cd, campo.EntradaVerOrden{ID: abierta.ID}))(t).Orden
	if v.DatosArgosCambiados == nil || len(v.DatosArgosCambiados.Campos) != 2 || v.DatosArgosCambiados.Campos[0] != "servicio.direccion" || v.Version != abierta.Version {
		t.Fatalf("marca «dato de Argos cambió» (sin subir la versión): %+v", v.DatosArgosCambiados)
	}
	if v.Direccion != abierta.Direccion {
		t.Fatal("la dirección copiada no se pisa")
	}
	if n := contar(t, `SELECT count(*) FROM integracion.outbox WHERE evento = 'orden.datos_argos_cambiados' AND (payload->>'orden_id')::bigint = $1`, abierta.ID); n != 2 {
		t.Fatalf("orden.datos_argos_cambiados: %d", n)
	}
}

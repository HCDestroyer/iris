package comercial_test

import (
	"context"
	"strconv"
	"testing"

	"github.com/HCDestroyer/iris/apps/api/internal/comercial"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
)

// ventaEnLaInstalacion: de la venta a la instalación (contrato argosisp.md 0.10.0 §3.4.8). Recibe
// un prospecto ganado con el alta en Argos esperando la activación de su orden.
//
//   - B20: el cambio de plan y de titular hechos en Argos crean una revisión nueva de la
//     cotización (aceptada) y un contrato regenerado en borrador a nombre del titular, para firmar
//     en la orden, y avisan al vendedor (prospecto.venta_revisada). Repetir no crea otra.
//   - B17: servicio.titular_cambiado pasa el prospecto al cliente nuevo; la activación hecha lo
//     completa.
func ventaEnLaInstalacion(t *testing.T, e *entorno, ctx context.Context, prospecto int64, d comercial.ProspectoDetalle) {
	t.Helper()
	orden := *d.Alta.OrdenID
	antes := d.Cotizacion
	contratoAntes := d.Contrato
	if antes == nil || contratoAntes == nil {
		t.Fatalf("venta sin cotización o contrato: %+v", d)
	}
	clavePlan := "iris:plan:" + strconv.FormatInt(orden, 10) + ":1"

	// Un usuario no revisa la venta (lo hace el sistema al quedar hecho el pendiente).
	if _, err := e.com.RevisarVenta(ctx, comercial.EntradaRevisarVenta{OrdenID: orden, Motivo: comercial.RevisionPorPlan, PlanID: ptr(int64(301)), Clave: clavePlan}); err == nil {
		t.Fatal("un usuario revisó la venta")
	}
	r := deber(e.com.RevisarVenta(sistema(t), comercial.EntradaRevisarVenta{OrdenID: orden, Motivo: comercial.RevisionPorPlan,
		PlanID: ptr(int64(301)), Clave: clavePlan}))(t)
	if r.CotizacionID == nil || r.ContratoID == nil {
		t.Fatalf("revisión: %+v", r)
	}
	v := deber(e.com.VerProspecto.Ejecutar(ctx, comercial.EntradaVerProspecto{ID: prospecto}))(t)
	c := v.Cotizacion
	if c == nil || c.ID != *r.CotizacionID || c.Estado != "aceptada" || c.Revision != antes.Revision+1 || c.PlanID == nil || *c.PlanID != 301 ||
		c.Lineas[0].Descripcion != "Fibra 200 (sin precio)" || c.TotalMensual != antes.TotalMensual {
		t.Fatalf("cotización revisada: %+v", c)
	}
	if v.Contrato == nil || v.Contrato.ID != *r.ContratoID || v.Contrato.Estado != "borrador" || v.Contrato.CotizacionID != c.ID {
		t.Fatalf("contrato regenerado: %+v", v.Contrato)
	}
	if n := contar(t, "SELECT count(*) FROM comercial.contratos WHERE id = $1 AND estado = 'anulado'", contratoAntes.ID); n != 1 {
		t.Fatal("el contrato anterior no quedó anulado")
	}
	if n := contar(t, "SELECT count(*) FROM comercial.contratos WHERE id = $1 AND (contenido->>'ordenId')::bigint = $2", *r.ContratoID, orden); n != 1 {
		t.Fatal("el contrato regenerado no apunta a la orden donde se firma")
	}
	if n := contar(t, "SELECT count(*) FROM integracion.outbox WHERE evento = 'prospecto.venta_revisada' AND (payload->>'prospecto_id')::bigint = $1 AND payload->>'motivo' = 'plan'", prospecto); n != 1 {
		t.Fatal("sin aviso al vendedor")
	}
	// Repetido (el evento llegó dos veces): nada nuevo.
	if r2 := deber(e.com.RevisarVenta(sistema(t), comercial.EntradaRevisarVenta{OrdenID: orden, Motivo: comercial.RevisionPorPlan,
		PlanID: ptr(int64(301)), Clave: clavePlan}))(t); r2.CotizacionID != nil {
		t.Fatalf("revisión repetida: %+v", r2)
	}

	// Cambio de titular: el contrato regenerado va a nombre del titular confirmado en la orden.
	if _, err := pruebas.Pool(t).Exec(context.Background(),
		"INSERT INTO campo.orden_titular (orden_id, empresa_id, quien_firma, documento_tipo, nombre) VALUES ($1, 1, 'otra', 'dpi', 'Ana Pérez')", orden); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pruebas.Pool(t).Exec(context.Background(), "DELETE FROM campo.orden_titular WHERE orden_id = $1", orden)
	})
	r = deber(e.com.RevisarVenta(sistema(t), comercial.EntradaRevisarVenta{OrdenID: orden, Motivo: comercial.RevisionPorTitular,
		Clave: "iris:titular:" + strconv.FormatInt(orden, 10) + ":1"}))(t)
	if n := contar(t, "SELECT count(*) FROM comercial.contratos WHERE id = $1 AND contenido->'cliente'->>'nombre' = 'Ana Pérez' AND contenido->'cliente'->>'documento' = ''", *r.ContratoID); n != 1 {
		t.Fatal("el contrato no va a nombre del titular nuevo (sin su documento)")
	}

	// B17: el titular cambió en Argos → el prospecto pasa al cliente nuevo (idempotente).
	in := comercial.EntradaTitularCambiado{ServicioID: *d.Alta.ServicioArgosID, DeClienteID: d.Alta.ClienteArgosID, AClienteID: 777, EntranteID: 1}
	if rt := deber(e.com.MoverTitular(sistema(t), in))(t); len(rt.Prospectos) != 1 {
		t.Fatalf("titular nuevo: %+v", rt)
	}
	if rt := deber(e.com.MoverTitular(sistema(t), in))(t); len(rt.Prospectos) != 0 {
		t.Fatalf("titular repetido: %+v", rt)
	}
	v = deber(e.com.VerProspecto.Ejecutar(ctx, comercial.EntradaVerProspecto{ID: prospecto}))(t)
	if v.Alta.ClienteArgosID == nil || *v.Alta.ClienteArgosID != 777 || v.Alta.Estado != comercial.AltaEsperandoActivacion {
		t.Fatalf("prospecto con el titular nuevo: %+v", v.Alta)
	}

	// B17: la activación hecha en Argos completa el alta (una vez).
	if ra := deber(e.com.ActivarAlta(sistema(t), orden))(t); len(ra.Prospectos) != 1 {
		t.Fatalf("activación: %+v", ra)
	}
	if ra := deber(e.com.ActivarAlta(sistema(t), orden))(t); len(ra.Prospectos) != 0 {
		t.Fatalf("activación repetida: %+v", ra)
	}
	v = deber(e.com.VerProspecto.Ejecutar(ctx, comercial.EntradaVerProspecto{ID: prospecto}))(t)
	if v.Alta.Estado != comercial.AltaCompleta {
		t.Fatalf("alta tras activar: %+v", v.Alta)
	}
	// Vuelve a esperar la activación, para el resto de la prueba.
	if _, err := pruebas.Pool(t).Exec(context.Background(),
		"UPDATE comercial.prospectos SET alta_estado = 'esperando_activacion', cliente_argos_id = $2 WHERE id = $1", prospecto, *d.Alta.ClienteArgosID); err != nil {
		t.Fatal(err)
	}
}

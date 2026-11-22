package comercial_test

import (
	"context"
	"strconv"
	"testing"

	"github.com/google/uuid"

	"github.com/HCDestroyer/iris/apps/api/internal/campo"
	"github.com/HCDestroyer/iris/apps/api/internal/comercial"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
)

// ventaEnLaInstalacion: de la venta a la instalación (contrato argosisp.md 0.10.0 §3.4.8). Recibe
// un prospecto ganado con el alta en Argos esperando la activación de su orden.
//
//   - B20 (ALTA 1 de #280): el cambio de plan y de titular hechos en Argos crean una revisión nueva
//     de la cotización, **vigente** y con vigencia nueva, y un contrato regenerado en borrador a
//     nombre del titular, ligado a la orden. El contrato firmado sigue vivo hasta que el titular
//     firma el regenerado en la orden (orden.firmar_contrato): ahí el anterior se anula y la
//     revisión queda aceptada. Avisan al vendedor (prospecto.venta_revisada). Repetir no crea otra.
//   - B17: servicio.titular_cambiado pasa el prospecto al cliente nuevo solo si la instantánea dice
//     que el servicio es suyo; la activación hecha lo completa.
func ventaEnLaInstalacion(t *testing.T, e *entorno, ctx context.Context, prospecto int64, d comercial.ProspectoDetalle) {
	t.Helper()
	orden := *d.Alta.OrdenID
	antes := d.Cotizacion
	contratoAntes := d.Contrato
	if antes == nil || contratoAntes == nil || contratoAntes.Estado != "firmado" {
		t.Fatalf("venta sin cotización o contrato firmado: %+v", d)
	}
	clavePlan := "iris:plan:" + strconv.FormatInt(orden, 10) + ":1"
	// La vigencia de la venta ya venció: la revisión trae una nueva.
	if _, err := pruebas.Pool(t).Exec(context.Background(), "UPDATE comercial.cotizaciones SET vigente_hasta = now() - interval '1 day' WHERE id = $1", antes.ID); err != nil {
		t.Fatal(err)
	}

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
	// Lo vivo no cambia hasta la firma: la cotización aceptada y el contrato firmado.
	if v.Cotizacion == nil || v.Cotizacion.ID != antes.ID || v.Cotizacion.Estado != "aceptada" || v.Contrato == nil ||
		v.Contrato.ID != contratoAntes.ID || v.Contrato.Estado != "firmado" {
		t.Fatalf("lo vivo cambió sin la firma: %+v %+v", v.Cotizacion, v.Contrato)
	}
	pf := v.PorFirmarEnOrden
	if pf == nil || pf.OrdenID != orden || pf.Cotizacion.ID != *r.CotizacionID || pf.Cotizacion.Estado != "vigente" || pf.Cotizacion.Vencida ||
		pf.Cotizacion.Revision != antes.Revision+1 || pf.Cotizacion.PlanID == nil || *pf.Cotizacion.PlanID != 301 ||
		pf.Cotizacion.Lineas[0].Descripcion != "Fibra 200 (sin precio)" || pf.Cotizacion.TotalMensual != antes.TotalMensual ||
		pf.Contrato == nil || pf.Contrato.ID != *r.ContratoID || pf.Contrato.Estado != "borrador" || pf.Contrato.CotizacionID != pf.Cotizacion.ID {
		t.Fatalf("revisión por firmar: %+v", pf)
	}
	if n := contar(t, "SELECT count(*) FROM comercial.contratos WHERE id = $1 AND estado = 'firmado'", contratoAntes.ID); n != 1 {
		t.Fatal("el contrato firmado se anuló antes de firmar el regenerado")
	}
	if n := contar(t, "SELECT count(*) FROM comercial.contratos WHERE id = $1 AND (contenido->>'ordenId')::bigint = $2", *r.ContratoID, orden); n != 1 {
		t.Fatal("el contrato regenerado no apunta a la orden donde se firma")
	}
	if n := contar(t, "SELECT count(*) FROM integracion.outbox WHERE evento = 'prospecto.venta_revisada' AND (payload->>'prospecto_id')::bigint = $1 AND payload->>'motivo' = 'plan'", prospecto); n != 1 {
		t.Fatal("sin aviso al vendedor")
	}
	if n := contar(t, "SELECT count(*) FROM integracion.outbox WHERE evento = 'cotizacion.aceptada' AND (payload->>'cotizacion_id')::bigint = $1", *r.CotizacionID); n != 0 {
		t.Fatal("la revisión se dio por aceptada sin la firma")
	}
	// Repetido (el evento llegó dos veces): nada nuevo.
	if r2 := deber(e.com.RevisarVenta(sistema(t), comercial.EntradaRevisarVenta{OrdenID: orden, Motivo: comercial.RevisionPorPlan,
		PlanID: ptr(int64(301)), Clave: clavePlan}))(t); r2.CotizacionID != nil {
		t.Fatalf("revisión repetida: %+v", r2)
	}

	// Cambio de titular (aún sin firmar el de plan): la revisión anterior queda reemplazada y su
	// contrato anulado; el nuevo va a nombre del titular confirmado en la orden.
	if _, err := pruebas.Pool(t).Exec(context.Background(),
		"INSERT INTO campo.orden_titular (orden_id, empresa_id, quien_firma, documento_tipo, nombre) VALUES ($1, 1, 'otra', 'dpi', 'Ana Pérez')", orden); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pruebas.Pool(t).Exec(context.Background(), "DELETE FROM campo.orden_titular WHERE orden_id = $1", orden)
	})
	rt := deber(e.com.RevisarVenta(sistema(t), comercial.EntradaRevisarVenta{OrdenID: orden, Motivo: comercial.RevisionPorTitular,
		Clave: "iris:titular:" + strconv.FormatInt(orden, 10) + ":1"}))(t)
	if n := contar(t, "SELECT count(*) FROM comercial.contratos WHERE id = $1 AND contenido->'cliente'->>'nombre' = 'Ana Pérez' AND contenido->'cliente'->>'documento' = ''", *rt.ContratoID); n != 1 {
		t.Fatal("el contrato no va a nombre del titular nuevo (sin su documento)")
	}
	if n := contar(t, "SELECT count(*) FROM comercial.cotizaciones WHERE id = $1 AND estado = 'reemplazada' AND reemplazada_por = $2", *r.CotizacionID, *rt.CotizacionID); n != 1 {
		t.Fatal("la revisión sin firmar no quedó reemplazada")
	}
	if n := contar(t, "SELECT count(*) FROM comercial.contratos WHERE id = $1 AND estado = 'anulado'", *r.ContratoID); n != 1 {
		t.Fatal("el regenerado sin firmar no quedó anulado")
	}
	if n := contar(t, "SELECT count(*) FROM comercial.cotizaciones WHERE id = $1 AND plan_id = 301", *rt.CotizacionID); n != 1 {
		t.Fatal("la revisión del titular perdió el plan nuevo")
	}

	// La firma en la orden: el técnico asignado, en sitio, con el lote (también sin señal).
	tec := usuarioCon(t, rol("tecnico"))
	asignarEmpresa(t, tec, 1)
	ct := enEmpresa(t, tec, 1, "tecnico")
	firmar := func(c context.Context, contrato int64) campo.ResultadoOperacion {
		t.Helper()
		id, _ := uuid.NewV7()
		res, err := e.cmp.Lote(c, campo.EntradaLote{DispositivoID: "tel-venta-" + pruebas.Unico(), Operaciones: []campo.OperacionLote{{
			ID: id.String(), Tipo: campo.OpFirmarContrato, OrdenID: orden,
			Datos: map[string]any{"contratoId": contrato, "firmanteNombre": "Ana Pérez", "svg": firmaSVG}}}})
		if err != nil {
			t.Fatal(err)
		}
		return res.Resultados[0]
	}
	// Sin comercial conectado: 503; de otro técnico o fuera de sitio: no se firma.
	if res := firmar(ct, *rt.ContratoID); res.Estado != "rechazada" || res.Error == nil || res.Error.Codigo() != "no_disponible" {
		t.Fatalf("sin comercial: %+v", res)
	}
	e.cmp.ConectarContratos(e.com)
	if _, err := pruebas.Pool(t).Exec(context.Background(), "UPDATE campo.ordenes SET tecnico_id = $2, estado = 'en_camino' WHERE id = $1", orden, tec); err != nil {
		t.Fatal(err)
	}
	if res := firmar(ct, *rt.ContratoID); res.Estado != "rechazada" {
		t.Fatalf("fuera de sitio: %+v", res)
	}
	if _, err := pruebas.Pool(t).Exec(context.Background(), "UPDATE campo.ordenes SET estado = 'en_sitio', en_sitio_en = now() WHERE id = $1", orden); err != nil {
		t.Fatal(err)
	}
	otro := usuarioCon(t, rol("tecnico"))
	asignarEmpresa(t, otro, 1)
	if res := firmar(enEmpresa(t, otro, 1, "tecnico"), *rt.ContratoID); res.Estado != "rechazada" {
		t.Fatalf("otro técnico firmó: %+v", res)
	}
	// El contrato anulado (de la revisión reemplazada) no se firma.
	if res := firmar(ct, *r.ContratoID); res.Estado != "rechazada" {
		t.Fatalf("contrato anulado firmado: %+v", res)
	}
	if res := firmar(ct, *rt.ContratoID); res.Estado != "aplicada" {
		t.Fatalf("firma en la orden: %+v", res)
	}
	v = deber(e.com.VerProspecto.Ejecutar(ctx, comercial.EntradaVerProspecto{ID: prospecto}))(t)
	if v.PorFirmarEnOrden != nil || v.Contrato == nil || v.Contrato.ID != *rt.ContratoID || v.Contrato.Estado != "firmado" ||
		v.Cotizacion == nil || v.Cotizacion.ID != *rt.CotizacionID || v.Cotizacion.Estado != "aceptada" {
		t.Fatalf("tras la firma: %+v %+v %+v", v.PorFirmarEnOrden, v.Contrato, v.Cotizacion)
	}
	if n := contar(t, "SELECT count(*) FROM comercial.contratos WHERE id = $1 AND estado = 'anulado'", contratoAntes.ID); n != 1 {
		t.Fatal("el contrato anterior no se anuló al firmar el regenerado")
	}
	if n := contar(t, "SELECT count(*) FROM comercial.cotizaciones WHERE id = $1 AND estado = 'reemplazada' AND reemplazada_por = $2", antes.ID, *rt.CotizacionID); n != 1 {
		t.Fatal("la cotización aceptada anterior no quedó reemplazada por la revisión")
	}
	if n := contar(t, "SELECT count(*) FROM integracion.outbox WHERE evento = 'contrato.firmado' AND (payload->>'contrato_id')::bigint = $1 AND (payload->>'orden_id')::bigint = $2", *rt.ContratoID, orden); n != 1 {
		t.Fatal("sin contrato.firmado con la orden")
	}
	if n := contar(t, "SELECT count(*) FROM auditoria.acciones WHERE accion = 'ordenes.firmar_contrato' AND entrada::text LIKE '%M 10 40%'"); n != 0 {
		t.Fatal("el trazo de la firma quedó en la auditoría")
	}
	// Ya firmado: otra firma no.
	if res := firmar(ct, *rt.ContratoID); res.Estado != "rechazada" {
		t.Fatalf("firmado dos veces: %+v", res)
	}

	// B17: el titular cambió en Argos → el prospecto pasa al cliente nuevo (idempotente).
	// MEDIA 2 de #280: solo si la instantánea dice que el servicio ya es del cliente nuevo.
	in := comercial.EntradaTitularCambiado{ServicioID: *d.Alta.ServicioArgosID, DeClienteID: d.Alta.ClienteArgosID, AClienteID: 777, EntranteID: 1}
	for _, c := range []int64{*d.Alta.ClienteArgosID, 777} {
		if _, err := pruebas.Pool(t).Exec(context.Background(), `INSERT INTO integracion.argos_clientes (id, empresa_id, empresas, nombre, codigo, telefono, direccion, sincronizado_en)
			VALUES ($1, 1, '{1}', 'Cliente', $2, '5555-0000', 'Zona 1', now()) ON CONFLICT (id) DO NOTHING`, c, "C-"+strconv.FormatInt(c, 10)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pruebas.Pool(t).Exec(context.Background(), `INSERT INTO integracion.argos_servicios (id, empresa_id, cliente_id, plan_nombre, estado, sincronizado_en)
		VALUES ($1, 1, $2, 'Fibra 100', 'pendiente_instalacion', now()) ON CONFLICT (id) DO UPDATE SET cliente_id = $2`, *d.Alta.ServicioArgosID, *d.Alta.ClienteArgosID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pruebas.Pool(t).Exec(context.Background(), "DELETE FROM integracion.argos_servicios WHERE id = $1", *d.Alta.ServicioArgosID)
		_, _ = pruebas.Pool(t).Exec(context.Background(), "DELETE FROM integracion.argos_clientes WHERE id IN ($1, 777)", *d.Alta.ClienteArgosID)
	})
	if rm := deber(e.com.MoverTitular(sistema(t), in))(t); len(rm.Prospectos) != 0 {
		t.Fatalf("movido con la instantánea en otro titular: %+v", rm)
	}
	if _, err := pruebas.Pool(t).Exec(context.Background(), "UPDATE integracion.argos_servicios SET cliente_id = 777 WHERE id = $1", *d.Alta.ServicioArgosID); err != nil {
		t.Fatal(err)
	}
	if rm := deber(e.com.MoverTitular(sistema(t), in))(t); len(rm.Prospectos) != 1 {
		t.Fatalf("titular nuevo: %+v", rm)
	}
	if rm := deber(e.com.MoverTitular(sistema(t), in))(t); len(rm.Prospectos) != 0 {
		t.Fatalf("titular repetido: %+v", rm)
	}
	// Un aviso atrasado (de vuelta al anterior) no lo devuelve: la instantánea dice 777.
	atrasado := comercial.EntradaTitularCambiado{ServicioID: *d.Alta.ServicioArgosID, DeClienteID: ptr(int64(1)), AClienteID: *d.Alta.ClienteArgosID, EntranteID: 2}
	if rm := deber(e.com.MoverTitular(sistema(t), atrasado))(t); len(rm.Prospectos) != 0 {
		t.Fatalf("el aviso atrasado devolvió el prospecto: %+v", rm)
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
